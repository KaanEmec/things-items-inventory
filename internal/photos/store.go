package photos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

// ErrNotFound is returned for a photo that does not exist in the household.
var ErrNotFound = errors.New("photos: photo not found")

type uploadResult struct {
	photo        sqlc.Photo
	link         sqlc.PhotoLink
	createdPhoto bool
	createdLink  bool
}

// commit moves a staged file into place and records the photo and its link in
// one transaction. The same bytes uploaded twice by a household are one row;
// the same link twice is a no-op.
func (s *Service) commit(ctx context.Context, st *staged, householdID, userID, linkID, role string, item, place sql.NullString) (res uploadResult, err error) {
	dest, err := s.origPath(st.sha, st.ext)
	if err != nil {
		return res, err
	}
	s.mu.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			s.mu.Unlock()
		}
	}
	defer unlock()

	placed := false
	if _, statErr := os.Stat(dest); errors.Is(statErr, fs.ErrNotExist) {
		if err = os.MkdirAll(dirOf(dest), 0o755); err != nil {
			return res, err
		}
		if err = os.Rename(st.path, dest); err != nil {
			return res, err
		}
		placed = true
	} else if statErr != nil {
		return res, statErr
	}

	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		p, err := q.GetPhotoBySha(ctx, sqlc.GetPhotoByShaParams{HouseholdID: householdID, Sha256: st.sha})
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			now := s.stamp()
			p, err = q.InsertPhoto(ctx, sqlc.InsertPhotoParams{ID: newID(), HouseholdID: householdID, CreatedAt: now, UpdatedAt: now,
				Sha256: st.sha, Mime: st.mime, Ext: st.ext, Bytes: st.size,
				Width: sql.NullInt64{Int64: int64(st.w), Valid: true}, Height: sql.NullInt64{Int64: int64(st.h), Valid: true},
				CreatedBy: nullStr(userID)})
			if err != nil {
				return err
			}
			res.createdPhoto = true
			if s.queue != nil {
				if _, err = s.queue.Enqueue(ctx, tx, jobs.Job{HouseholdID: householdID, Kind: JobKind,
					Payload: []byte(`{"photo_id":"` + p.ID + `"}`), CreatedBy: userID}); err != nil {
					return err
				}
			}
		default:
			return err
		}
		res.photo = p
		res.link, res.createdLink, err = s.linkIn(ctx, q, householdID, p.ID, linkID, role, item, place)
		return err
	})
	if err != nil {
		if placed {
			s.collectSha(ctx, st.sha, st.ext)
		}
		return res, err
	}
	unlock()
	if res.createdPhoto && s.queue == nil {
		if derr := s.MakeDerivatives(ctx, householdID, res.photo.ID); derr != nil {
			slog.Error("photos: derivatives", "err", derr)
		} else if p, gerr := sqlc.New(s.db.Read).GetPhoto(ctx, sqlc.GetPhotoParams{HouseholdID: householdID, ID: res.photo.ID}); gerr == nil {
			res.photo = p
		}
	}
	return res, nil
}

// collectSha removes the files of a hash when no photo row uses it. s.mu held.
func (s *Service) collectSha(ctx context.Context, sha, ext string) {
	if n, err := sqlc.New(s.db.Read).CountPhotosBySha(ctx, sha); err == nil && n == 0 {
		s.removeFiles(sha, ext)
	}
}

// loadImage decodes a stored original after the bomb check.
func (s *Service) loadImage(p sqlc.Photo) (image.Image, int, error) {
	path, err := s.origPath(p.Sha256, p.Ext)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, 0, errCorrupt
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide || cfg.Width*cfg.Height > maxPixels {
		return nil, 0, errTooBig
	}
	if _, err = f.Seek(0, 0); err != nil {
		return nil, 0, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, 0, errCorrupt
	}
	return img, orientationOf(f, p.Mime), nil
}

// MakeDerivatives creates the thumb, screen and ai sizes of a photo and marks
// it. It is the body of the photo_derivatives job and is safe to repeat. A
// photo that no longer exists is not an error.
func (s *Service) MakeDerivatives(ctx context.Context, householdID, photoID string) error {
	p, err := sqlc.New(s.db.Read).GetPhoto(ctx, sqlc.GetPhotoParams{HouseholdID: householdID, ID: photoID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if p.Derivatives == 1 {
		return nil
	}
	img, orient, err := s.loadImage(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // removed meanwhile
	} else if err != nil {
		return err
	}
	out := map[string][]byte{}
	var aiImg image.Image
	for _, sz := range sizes {
		if sz.name == "thumb" {
			continue
		}
		b, oriented, err := render(img, orient, sz.side, sz.quality)
		if err != nil {
			return err
		}
		out[sz.name] = b
		if sz.name == "ai" {
			aiImg = oriented
		}
	}
	if out["thumb"], _, err = render(aiImg, 1, 320, 80); err != nil { // already upright
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := sqlc.New(s.db.Read).GetPhoto(ctx, sqlc.GetPhotoParams{HouseholdID: householdID, ID: photoID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // unlinked while we worked; write nothing
		}
		return err
	}
	for name, b := range out {
		dest, err := s.derivPath(p.Sha256, name)
		if err != nil {
			return err
		}
		if err := s.writeFileAtomic(dest, b); err != nil {
			return err
		}
	}
	return sqlc.New(s.db.Write).SetPhotoDerivatives(ctx, sqlc.SetPhotoDerivativesParams{UpdatedAt: s.stamp(), HouseholdID: householdID, ID: photoID})
}

// AIBytes returns the AI-sized JPEG (longest side 1568, no EXIF) of a photo of
// the household for a vision request. It reads the derivative when it exists
// and otherwise makes the size in memory from the original.
func (s *Service) AIBytes(ctx context.Context, householdID, photoID string) ([]byte, error) {
	p, err := sqlc.New(s.db.Read).GetPhoto(ctx, sqlc.GetPhotoParams{HouseholdID: householdID, ID: photoID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if p.Derivatives == 1 {
		if path, err := s.derivPath(p.Sha256, "ai"); err == nil {
			if b, err := os.ReadFile(path); err == nil {
				return b, nil
			}
		}
	}
	img, orient, err := s.loadImage(p)
	if err != nil {
		return nil, fmt.Errorf("photos: read original: %w", err)
	}
	b, _, err := render(img, orient, 1568, 85)
	return b, err
}

func dirOf(p string) string { return filepath.Dir(p) }

func newID() string { return id.New() }
