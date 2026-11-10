// Package photos stores photos on disk, links them to items and places, makes
// the smaller sizes in a background job and serves them to the household.
//
// Files: <data>/photos/<aa>/<sha256>.<ext> for the original and
// <data>/photos/<aa>/<sha256>.<size>.jpg for the derivatives. Every path is
// built from a hash and an extension that this package computed or read from
// its own table, never from request input.
package photos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
)

// JobKind is the background job that makes the derivatives.
const JobKind = "photo_derivatives"

// MaxUpload is the size limit of one uploaded file.
const MaxUpload = 25 << 20

// Roles a photo link can have. item, label and receipt link to an item;
// contents and place link to a place.
const (
	RoleItem     = "item"
	RoleLabel    = "label"
	RoleReceipt  = "receipt"
	RoleContents = "contents"
	RolePlace    = "place"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

// Target is the thing a photo is linked to: an item or a place. For a
// capture of an item, PlaceID may carry the place the item is in.
type Target struct {
	ItemID  string
	PlaceID string
}

func (t Target) isItem() bool { return t.ItemID != "" }

// TargetHook resolves (and, for a capture of a new item, creates) the target
// before a photo is linked to it. It runs before the photos package's own
// transaction, so it may write to the database. found=false (or an error that
// is an *httpapi.Error) stops the request; found=false gives 404. Without a
// hook the package only links to items and places that already exist.
type TargetHook func(ctx context.Context, householdID, userID string, t Target, role string) (found bool, err error)

// Service is the photos feature.
type Service struct {
	db  *db.DB
	dir string // data directory
	now func() time.Time

	maxUpload int64

	mu    sync.Mutex // serialises file placement and removal against row changes
	queue *jobs.Queue
	hook  TargetHook
}

// New returns the photos service. cfg.DataDir is where files live.
func New(d *db.DB, cfg config.Config) *Service {
	return &Service{db: d, dir: cfg.DataDir, now: func() time.Time { return time.Now().UTC() }, maxUpload: MaxUpload}
}

// SetTargetHook installs the hook that resolves or creates link targets.
// Call it before serving.
func (s *Service) SetTargetHook(h TargetHook) { s.hook = h }

// UseQueue registers the derivatives job kind on q and makes uploads queue it.
// Without a queue the derivatives are made inside the upload request.
func (s *Service) UseQueue(q *jobs.Queue) {
	s.queue = q
	q.Register(JobKind, jobs.Handler{
		Timeout:     2 * time.Minute,
		MaxAttempts: 3,
		Run: func(ctx context.Context, j jobs.Job) (json.RawMessage, error) {
			var p struct {
				PhotoID string `json:"photo_id"`
			}
			if err := json.Unmarshal(j.Payload, &p); err != nil {
				return nil, jobs.Permanent(err)
			}
			err := s.MakeDerivatives(ctx, j.HouseholdID, p.PhotoID)
			if errors.Is(err, errCorrupt) || errors.Is(err, errTooBig) {
				return nil, jobs.Permanent(err)
			}
			return nil, err
		},
	})
}

func (s *Service) stamp() string { return s.now().UTC().Format(timeLayout) }

// ---- paths ----

var (
	shaRe   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	identRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

func (s *Service) photoRoot() string { return filepath.Join(s.dir, "photos") }
func (s *Service) tmpDir() string    { return filepath.Join(s.dir, "tmp") }

// origPath and derivPath build file paths from a hash and a name that must
// be well formed; anything else is a programming error, not a path.
func (s *Service) origPath(sha, ext string) (string, error) {
	if !shaRe.MatchString(sha) || (ext != "jpg" && ext != "png" && ext != "webp") {
		return "", errors.New("photos: bad file reference")
	}
	return filepath.Join(s.photoRoot(), sha[:2], sha+"."+ext), nil
}

func (s *Service) derivPath(sha, size string) (string, error) {
	if !shaRe.MatchString(sha) || (size != "thumb" && size != "screen" && size != "ai") {
		return "", errors.New("photos: bad file reference")
	}
	return filepath.Join(s.photoRoot(), sha[:2], sha+"."+size+".jpg"), nil
}

// removeFiles deletes the original and derivatives of a hash.
func (s *Service) removeFiles(sha, ext string) {
	var paths []string
	if p, err := s.origPath(sha, ext); err == nil {
		paths = append(paths, p)
	}
	for _, sz := range sizes {
		if p, err := s.derivPath(sha, sz.name); err == nil {
			paths = append(paths, p)
		}
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Error("photos: remove file", "err", err)
		}
	}
	os.Remove(filepath.Dir(paths[0])) // only succeeds when empty
}

// writeFileAtomic writes b to dest through a temp file and a rename.
func (s *Service) writeFileAtomic(dest string, b []byte) error {
	if err := os.MkdirAll(s.tmpDir(), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.tmpDir(), "derivative-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.Rename(name, dest)
}

// ---- staging an upload ----

type staged struct {
	path   string
	sha    string
	mime   string
	ext    string
	size   int64
	w, h   int
	orient int
}

func (st *staged) cleanup() {
	if st != nil && st.path != "" {
		os.Remove(st.path)
	}
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

var errTooLarge = errors.New("file too large")

// stage streams r to a temp file under <data>/tmp while hashing it, then
// checks it is a real, sane image. On any error nothing is left on disk.
func (s *Service) stage(r io.Reader) (res *staged, err error) {
	head := make([]byte, 12)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	mime, ext, ok := sniff(head)
	if !ok {
		return nil, errUnsupported
	}
	if err := os.MkdirAll(s.tmpDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(s.tmpDir(), "upload-*")
	if err != nil {
		return nil, err
	}
	st := &staged{path: f.Name(), mime: mime, ext: ext}
	defer func() {
		if err != nil {
			st.cleanup() // st is the local, not the (already nil) result
		}
	}()
	h := sha256.New()
	cr := &countingReader{r: io.LimitReader(io.MultiReader(bytes.NewReader(head), r), s.maxUpload+1)}
	_, cerr := io.Copy(io.MultiWriter(f, h), cr)
	if cerr == nil && cr.n > s.maxUpload {
		cerr = errTooLarge
	}
	if cerr != nil {
		f.Close()
		return nil, cerr
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	st.size = cr.n
	st.sha = hex.EncodeToString(h.Sum(nil))
	rf, err := os.Open(st.path)
	if err != nil {
		return nil, err
	}
	defer rf.Close()
	st.w, st.h, st.orient, err = checkImage(rf, mime)
	if err != nil {
		return nil, err
	}
	return st, nil
}

// ---- results ----

// Photo is a stored photo as the API shows it.
type Photo struct {
	ID          string `json:"id"`
	SHA256      string `json:"sha256"`
	Mime        string `json:"mime"`
	Bytes       int64  `json:"bytes"`
	Width       int64  `json:"width,omitempty"`
	Height      int64  `json:"height,omitempty"`
	Derivatives bool   `json:"derivatives"`
	CreatedAt   string `json:"created_at"`
}

// Link ties a photo to an item or a place.
type Link struct {
	ID      string  `json:"id"`
	PhotoID string  `json:"photo_id"`
	ItemID  *string `json:"item_id"`
	PlaceID *string `json:"place_id"`
	Role    string  `json:"role"`
	Sort    int64   `json:"sort"`
}

func photoJSON(p sqlc.Photo) Photo {
	return Photo{ID: p.ID, SHA256: p.Sha256, Mime: p.Mime, Bytes: p.Bytes, Width: p.Width.Int64,
		Height: p.Height.Int64, Derivatives: p.Derivatives == 1, CreatedAt: p.CreatedAt}
}

func nullPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func linkJSON(l sqlc.PhotoLink) Link {
	return Link{ID: l.ID, PhotoID: l.PhotoID, ItemID: nullPtr(l.ItemID), PlaceID: nullPtr(l.PlaceID), Role: l.Role, Sort: l.Sort}
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// roleTarget reports whether role is valid and links to an item (else a place).
func roleTarget(role string) (toItem, ok bool) {
	switch role {
	case RoleItem, RoleLabel, RoleReceipt:
		return true, true
	case RoleContents, RolePlace:
		return false, true
	}
	return false, false
}

// linkTarget checks role and target agree and returns the row target: the
// item when set, else the place.
func linkTarget(role string, t Target) (item, place sql.NullString, err error) {
	toItem, ok := roleTarget(role)
	if !ok {
		return item, place, httpapi.Invalid(map[string]string{"role": "Use item, label, receipt, contents or place."})
	}
	switch {
	case toItem && t.ItemID == "":
		return item, place, httpapi.Invalid(map[string]string{"item_id": "A " + role + " photo belongs to an item."})
	case !toItem && t.PlaceID == "":
		return item, place, httpapi.Invalid(map[string]string{"place_id": "A " + role + " photo belongs to a place."})
	case !toItem && t.ItemID != "":
		return item, place, httpapi.Invalid(map[string]string{"item_id": "A " + role + " photo belongs to a place, not an item."})
	}
	for f, v := range map[string]string{"item_id": t.ItemID, "place_id": t.PlaceID} {
		if v != "" && !identRe.MatchString(v) {
			return item, place, httpapi.Invalid(map[string]string{f: "That ID is not valid."})
		}
	}
	if toItem {
		return nullStr(t.ItemID), sql.NullString{}, nil
	}
	return sql.NullString{}, nullStr(t.PlaceID), nil
}

// resolveTarget runs the hook, or checks that the item or place exists.
func (s *Service) resolveTarget(ctx context.Context, householdID, userID string, t Target, role string) error {
	var found bool
	if s.hook != nil {
		var err error
		if found, err = s.hook(ctx, householdID, userID, t, role); err != nil {
			return err
		}
	} else {
		table, tid := "places", t.PlaceID
		if t.isItem() {
			table, tid = "items", t.ItemID
		}
		var one int
		err := s.db.Read.QueryRowContext(ctx,
			`SELECT 1 FROM `+table+` WHERE household_id = ? AND id = ? AND deleted_at IS NULL`, householdID, tid).Scan(&one)
		found = err == nil // a missing table (not built yet) counts as a missing row
	}
	if !found {
		return httpapi.NotFound("That item or place does not exist.")
	}
	return nil
}

// ---- links ----

// linkIn finds or creates the link of photo to target in tx. created is false
// for a retry. A client-chosen link id that already exists returns that row.
func (s *Service) linkIn(ctx context.Context, q *sqlc.Queries, householdID, photoID, linkID, role string, item, place sql.NullString) (l sqlc.PhotoLink, created bool, err error) {
	if linkID != "" {
		if l, err = q.GetPhotoLink(ctx, sqlc.GetPhotoLinkParams{HouseholdID: householdID, ID: linkID}); err == nil {
			return l, false, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return l, false, err
		}
	}
	if l, err = q.FindPhotoLink(ctx, sqlc.FindPhotoLinkParams{HouseholdID: householdID, PhotoID: photoID, Role: role, ItemID: item, PlaceID: place}); err == nil {
		return l, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return l, false, err
	}
	group, err := q.ListLinksInGroup(ctx, sqlc.ListLinksInGroupParams{HouseholdID: householdID, Role: role, ItemID: item, PlaceID: place})
	if err != nil {
		return l, false, err
	}
	sort := int64(0)
	if n := len(group); n > 0 {
		sort = group[n-1].Sort + 1
	}
	if linkID == "" {
		linkID = id.New()
	}
	now := s.stamp()
	l, err = q.InsertPhotoLink(ctx, sqlc.InsertPhotoLinkParams{ID: linkID, HouseholdID: householdID, CreatedAt: now, UpdatedAt: now,
		PhotoID: photoID, ItemID: item, PlaceID: place, Role: role, Sort: sort})
	return l, err == nil, err
}

// unlink removes one link; when it was the photo's last, the photo row goes,
// and the files go when no household's row points at them any more.
func (s *Service) unlink(ctx context.Context, householdID, linkID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var gone *sqlc.Photo
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		l, err := q.GetPhotoLink(ctx, sqlc.GetPhotoLinkParams{HouseholdID: householdID, ID: linkID})
		if err != nil {
			return err
		}
		gone, err = s.dropLink(ctx, q, l)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return httpapi.NotFound("No such photo link.")
	}
	if err == nil && gone != nil {
		s.collect(ctx, *gone)
	}
	return err
}

// dropLink deletes l and, if the photo has no links left, the photo row,
// returning that row. s.mu must be held.
func (s *Service) dropLink(ctx context.Context, q *sqlc.Queries, l sqlc.PhotoLink) (*sqlc.Photo, error) {
	if err := q.DeletePhotoLink(ctx, sqlc.DeletePhotoLinkParams{HouseholdID: l.HouseholdID, ID: l.ID}); err != nil {
		return nil, err
	}
	n, err := q.CountLinksOfPhoto(ctx, sqlc.CountLinksOfPhotoParams{HouseholdID: l.HouseholdID, PhotoID: l.PhotoID})
	if err != nil || n > 0 {
		return nil, err
	}
	p, err := q.GetPhoto(ctx, sqlc.GetPhotoParams{HouseholdID: l.HouseholdID, ID: l.PhotoID})
	if err != nil {
		return nil, err
	}
	if err := q.DeletePhoto(ctx, sqlc.DeletePhotoParams{HouseholdID: l.HouseholdID, ID: l.PhotoID}); err != nil {
		return nil, err
	}
	return &p, nil
}

// collect removes the files of a deleted photo row unless another row
// (another household's) still uses the same bytes. s.mu must be held.
func (s *Service) collect(ctx context.Context, p sqlc.Photo) {
	n, err := sqlc.New(s.db.Read).CountPhotosBySha(ctx, p.Sha256)
	if err != nil {
		slog.Error("photos: count references", "err", err)
		return
	}
	if n == 0 {
		s.removeFiles(p.Sha256, p.Ext)
	}
}

// UnlinkTarget removes every photo link of an item or place (for example when
// it is purged), deleting files that nothing else uses.
func (s *Service) UnlinkTarget(ctx context.Context, householdID string, t Target) error {
	item, place := nullStr(t.ItemID), nullStr(t.PlaceID)
	if item.Valid == place.Valid {
		return errors.New("photos: UnlinkTarget needs exactly one of ItemID, PlaceID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var gone []sqlc.Photo
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		links, err := q.ListLinksOfTarget(ctx, sqlc.ListLinksOfTargetParams{HouseholdID: householdID, ItemID: item, PlaceID: place})
		if err != nil {
			return err
		}
		for _, l := range links {
			p, err := s.dropLink(ctx, q, l)
			if err != nil {
				return err
			}
			if p != nil {
				gone = append(gone, *p)
			}
		}
		return nil
	})
	if err == nil {
		for _, p := range gone {
			s.collect(ctx, p)
		}
	}
	return err
}

// LinkInfo is one photo of an item or place, for the item and place JSON.
type LinkInfo struct {
	LinkID  string `json:"link_id"`
	PhotoID string `json:"photo_id"`
	SHA256  string `json:"sha256"`
	Role    string `json:"role"`
	Sort    int64  `json:"sort"`
}

// LinksOf returns the photos of the given items (or, with places, of the given
// places) keyed by item or place ID, in display order (the first `item` link
// is the main photo).
func (s *Service) LinksOf(ctx context.Context, householdID string, items bool, ids []string) (map[string][]LinkInfo, error) {
	out := map[string][]LinkInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	q := sqlc.New(s.db.Read)
	add := func(owner sql.NullString, l LinkInfo) { out[owner.String] = append(out[owner.String], l) }
	if items {
		rows, err := q.ListLinksForItems(ctx, sqlc.ListLinksForItemsParams{HouseholdID: householdID, ItemIds: nullStrs(ids)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			add(r.ItemID, LinkInfo{r.LinkID, r.PhotoID, r.Sha256, r.Role, r.Sort})
		}
		return out, nil
	}
	rows, err := q.ListLinksForPlaces(ctx, sqlc.ListLinksForPlacesParams{HouseholdID: householdID, PlaceIds: nullStrs(ids)})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		add(r.PlaceID, LinkInfo{r.LinkID, r.PhotoID, r.Sha256, r.Role, r.Sort})
	}
	return out, nil
}

func nullStrs(ids []string) []sql.NullString {
	out := make([]sql.NullString, len(ids))
	for i, v := range ids {
		out[i] = sql.NullString{String: v, Valid: true}
	}
	return out
}

// ItemLinks is LinksOf for items on any connection or transaction q. With
// mainOnly it keeps only each item's main photo (its first link when that is
// an `item` link), for lists.
func ItemLinks(ctx context.Context, q sqlc.DBTX, householdID string, ids []string, mainOnly bool) (map[string][]LinkInfo, error) {
	out := map[string][]LinkInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := sqlc.New(q).ListLinksForItems(ctx, sqlc.ListLinksForItemsParams{HouseholdID: householdID, ItemIds: nullStrs(ids)})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if mainOnly && (len(out[r.ItemID.String]) > 0 || r.Role != RoleItem) {
			continue
		}
		out[r.ItemID.String] = append(out[r.ItemID.String], LinkInfo{r.LinkID, r.PhotoID, r.Sha256, r.Role, r.Sort})
	}
	return out, nil
}

// LinkItemPhoto links an existing photo to an item with the `item` role inside
// tx. An item with no photos gets it as its first (main) one. Linking twice
// is a no-op.
func (s *Service) LinkItemPhoto(ctx context.Context, tx *sql.Tx, householdID, photoID, itemID string) error {
	_, _, err := s.linkIn(ctx, sqlc.New(tx), householdID, photoID, "", RoleItem, nullStr(itemID), sql.NullString{})
	return err
}

// PlaceMainLinks returns the main photo (the first `place` link) of each of
// the given places on any connection or transaction q, in ONE query. Places
// without a place photo are absent from the result.
func PlaceMainLinks(ctx context.Context, q sqlc.DBTX, householdID string, ids []string) (map[string]LinkInfo, error) {
	out := map[string]LinkInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := sqlc.New(q).ListLinksForPlaces(ctx, sqlc.ListLinksForPlacesParams{HouseholdID: householdID, PlaceIds: nullStrs(ids)})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if _, ok := out[r.PlaceID.String]; ok || r.Role != RolePlace {
			continue
		}
		out[r.PlaceID.String] = LinkInfo{r.LinkID, r.PhotoID, r.Sha256, r.Role, r.Sort}
	}
	return out, nil
}
