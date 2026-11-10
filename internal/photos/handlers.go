package photos

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// Routes registers the photo endpoints.
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("POST /api/v1/photos", auth.Require(auth.ActionInventoryWrite, s.upload))
	mux.HandleFunc("GET /api/v1/photos/{id}/{size}", auth.Require(auth.ActionInventoryRead, s.serve))
	mux.HandleFunc("POST /api/v1/photo-links", auth.Require(auth.ActionInventoryWrite, s.createLink))
	mux.HandleFunc("PATCH /api/v1/photo-links/{id}", auth.Require(auth.ActionInventoryWrite, s.patchLink))
	mux.HandleFunc("DELETE /api/v1/photo-links/{id}", auth.Require(auth.ActionInventoryWrite, s.deleteLink))
}

// Seed creates a photo with two links in the household and returns the IDs
// and marker strings for the cross-household test (see server.Feature). No
// file is written: the test only needs the rows.
func (s *Service) Seed(ctx context.Context, d *db.DB, householdID string) (pathValues, markers []string, err error) {
	sha := strings.Repeat("ab", 32)
	err = d.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		now := s.stamp()
		p, err := q.InsertPhoto(ctx, sqlc.InsertPhotoParams{ID: id.New(), HouseholdID: householdID, CreatedAt: now, UpdatedAt: now,
			Sha256: sha, Mime: "image/jpeg", Ext: "jpg", Bytes: 10})
		if err != nil {
			return err
		}
		l1, err := q.InsertPhotoLink(ctx, sqlc.InsertPhotoLinkParams{ID: id.New(), HouseholdID: householdID, CreatedAt: now, UpdatedAt: now,
			PhotoID: p.ID, ItemID: nullStr(id.New()), Role: RoleItem})
		if err != nil {
			return err
		}
		l2, err := q.InsertPhotoLink(ctx, sqlc.InsertPhotoLinkParams{ID: id.New(), HouseholdID: householdID, CreatedAt: now, UpdatedAt: now,
			PhotoID: p.ID, PlaceID: nullStr(id.New()), Role: RolePlace})
		if err != nil {
			return err
		}
		pathValues = []string{p.ID, sha, l1.ID, l2.ID}
		markers = []string{sha}
		return nil
	})
	return pathValues, markers, err
}

// writeStatus writes the error shape with a status the shared error codes do
// not cover.
func writeStatus(w http.ResponseWriter, status int, code, msg string) {
	httpapi.WriteJSON(w, status, struct {
		Error *httpapi.Error `json:"error"`
	}{&httpapi.Error{Code: code, Message: msg}})
}

func writeUploadError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, errUnsupported):
		writeStatus(w, http.StatusUnsupportedMediaType, "unsupported_type", "Only JPEG, PNG and WebP photos are accepted.")
	case errors.Is(err, errTooLarge), errors.As(err, &tooBig):
		httpapi.WriteError(w, httpapi.NewError(httpapi.CodeTooLarge, "That photo is too large (the limit is 25 MB)."))
	case errors.Is(err, errCorrupt):
		httpapi.WriteError(w, httpapi.BadRequest("That file is damaged or is not a readable photo."))
	case errors.Is(err, errTooBig):
		httpapi.WriteError(w, httpapi.BadRequest("That photo has too many pixels."))
	default:
		httpapi.WriteError(w, err)
	}
}

// field returns a multipart text field, at most 1 KiB.
func field(p io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(p, 1025))
	if err != nil {
		return "", err
	}
	if len(b) > 1024 {
		return "", httpapi.BadRequest("A form field is too long.")
	}
	return strings.TrimSpace(string(b)), nil
}

// upload handles POST /photos: multipart with file, role, and item_id
// (+ optional place_id) or place_id, plus an optional link id.
func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "multipart/form-data" {
		httpapi.WriteError(w, httpapi.BadRequest("Send the photo as multipart/form-data with a file field."))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUpload+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		httpapi.WriteError(w, httpapi.BadRequest("The upload could not be read."))
		return
	}
	var st *staged
	defer func() { st.cleanup() }()
	fields := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeUploadError(w, uploadReadError(err))
			return
		}
		if part.FormName() == "file" {
			if st != nil {
				httpapi.WriteError(w, httpapi.BadRequest("Send one file per request."))
				return
			}
			if st, err = s.stage(part); err != nil {
				writeUploadError(w, err)
				return
			}
			continue
		}
		v, err := field(part)
		if err != nil {
			writeUploadError(w, err)
			return
		}
		fields[part.FormName()] = v
	}
	if st == nil {
		httpapi.WriteError(w, httpapi.Invalid(map[string]string{"file": "Choose a photo."}))
		return
	}
	t := Target{ItemID: fields["item_id"], PlaceID: fields["place_id"]}
	role := fields["role"]
	item, place, err := linkTarget(role, t)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	linkID := fields["id"]
	if linkID != "" && !identRe.MatchString(linkID) {
		httpapi.WriteError(w, httpapi.Invalid(map[string]string{"id": "That ID is not valid."}))
		return
	}
	if err := s.resolveTarget(ctx, hid, uid, t, role); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	res, err := s.commit(ctx, st, hid, uid, linkID, role, item, place)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	status := http.StatusCreated
	if !res.createdLink {
		status = http.StatusOK
	}
	httpapi.WriteJSON(w, status, map[string]any{"photo": photoJSON(res.photo), "link": linkJSON(res.link)})
}

func uploadReadError(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return err
	}
	return httpapi.BadRequest("The upload could not be read.")
}

// createLink handles POST /photo-links: attach an existing photo.
func (s *Service) createLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid, uid := auth.HouseholdID(ctx), auth.UserID(ctx)
	var in struct {
		ID      string `json:"id"`
		PhotoID string `json:"photo_id"`
		ItemID  string `json:"item_id"`
		PlaceID string `json:"place_id"`
		Role    string `json:"role"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if !identRe.MatchString(in.PhotoID) {
		httpapi.WriteError(w, httpapi.Invalid(map[string]string{"photo_id": "Say which photo."}))
		return
	}
	if in.ID != "" && !identRe.MatchString(in.ID) {
		httpapi.WriteError(w, httpapi.Invalid(map[string]string{"id": "That ID is not valid."}))
		return
	}
	t := Target{ItemID: in.ItemID, PlaceID: in.PlaceID}
	item, place, err := linkTarget(in.Role, t)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if _, err := sqlc.New(s.db.Read).GetPhoto(ctx, sqlc.GetPhotoParams{HouseholdID: hid, ID: in.PhotoID}); err != nil {
		writeLookupError(w, err, "No such photo.")
		return
	}
	if err := s.resolveTarget(ctx, hid, uid, t, in.Role); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	var l sqlc.PhotoLink
	var created bool
	s.mu.Lock() // the photo must not be collected between the check and the link
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.GetPhoto(ctx, sqlc.GetPhotoParams{HouseholdID: hid, ID: in.PhotoID}); err != nil {
			return err
		}
		var err error
		l, created, err = s.linkIn(ctx, q, hid, in.PhotoID, in.ID, in.Role, item, place)
		return err
	})
	s.mu.Unlock()
	if err != nil {
		writeLookupError(w, err, "No such photo.")
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	httpapi.WriteJSON(w, status, map[string]any{"link": linkJSON(l)})
}

func writeLookupError(w http.ResponseWriter, err error, msg string) {
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.WriteError(w, httpapi.NotFound(msg))
		return
	}
	httpapi.WriteError(w, err)
}

// patchLink handles PATCH /photo-links/{id}: sort is the position (0 = first,
// which for item photos makes it the main photo) within the target and role;
// role moves the link to another role of the same kind of target.
func (s *Service) patchLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hid := auth.HouseholdID(ctx)
	var in struct {
		Sort *int    `json:"sort"`
		Role *string `json:"role"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if in.Sort != nil && (*in.Sort < 0 || *in.Sort > 100000) {
		httpapi.WriteError(w, httpapi.Invalid(map[string]string{"sort": "Use a position of 0 or more."}))
		return
	}
	var out sqlc.PhotoLink
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := sqlc.New(tx)
		l, err := q.GetPhotoLink(ctx, sqlc.GetPhotoLinkParams{HouseholdID: hid, ID: r.PathValue("id")})
		if err != nil {
			return err
		}
		now := s.stamp()
		if in.Role != nil && *in.Role != l.Role {
			toItem, ok := roleTarget(*in.Role)
			if !ok || toItem != l.ItemID.Valid {
				return httpapi.Invalid(map[string]string{"role": "That role does not fit this kind of target."})
			}
			if _, err := q.FindPhotoLink(ctx, sqlc.FindPhotoLinkParams{HouseholdID: hid, PhotoID: l.PhotoID, Role: *in.Role, ItemID: l.ItemID, PlaceID: l.PlaceID}); err == nil {
				return httpapi.Conflict("That photo already has this role here.")
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			group, err := q.ListLinksInGroup(ctx, sqlc.ListLinksInGroupParams{HouseholdID: hid, Role: *in.Role, ItemID: l.ItemID, PlaceID: l.PlaceID})
			if err != nil {
				return err
			}
			sort := int64(0)
			if n := len(group); n > 0 {
				sort = group[n-1].Sort + 1
			}
			if err := q.SetPhotoLinkRole(ctx, sqlc.SetPhotoLinkRoleParams{Role: *in.Role, Sort: sort, UpdatedAt: now, HouseholdID: hid, ID: l.ID}); err != nil {
				return err
			}
			l.Role = *in.Role
		}
		if in.Sort != nil {
			group, err := q.ListLinksInGroup(ctx, sqlc.ListLinksInGroupParams{HouseholdID: hid, Role: l.Role, ItemID: l.ItemID, PlaceID: l.PlaceID})
			if err != nil {
				return err
			}
			order := make([]string, 0, len(group))
			for _, g := range group {
				if g.ID != l.ID {
					order = append(order, g.ID)
				}
			}
			pos := min(*in.Sort, len(order))
			order = append(order[:pos], append([]string{l.ID}, order[pos:]...)...)
			for i, lid := range order {
				if err := q.SetPhotoLinkSort(ctx, sqlc.SetPhotoLinkSortParams{Sort: int64(i), UpdatedAt: now, HouseholdID: hid, ID: lid}); err != nil {
					return err
				}
			}
		}
		out, err = q.GetPhotoLink(ctx, sqlc.GetPhotoLinkParams{HouseholdID: hid, ID: l.ID})
		return err
	})
	if err != nil {
		writeLookupError(w, err, "No such photo link.")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"link": linkJSON(out)})
}

func (s *Service) deleteLink(w http.ResponseWriter, r *http.Request) {
	if err := s.unlink(r.Context(), auth.HouseholdID(r.Context()), r.PathValue("id")); err != nil {
		httpapi.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serve handles GET /photos/{id}/{size}. {id} is the photo id or its sha256;
// either is looked up in the caller's household, so another household's photo
// is a plain 404. The file path comes from the row, not from the request.
func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	hid := auth.HouseholdID(r.Context())
	size := r.PathValue("size")
	if size != "orig" && size != "thumb" && size != "screen" && size != "ai" {
		httpapi.WriteError(w, httpapi.NotFound("No such photo."))
		return
	}
	ref := r.PathValue("id")
	if !identRe.MatchString(ref) {
		httpapi.WriteError(w, httpapi.NotFound("No such photo."))
		return
	}
	p, err := sqlc.New(s.db.Read).GetPhotoByRef(r.Context(), sqlc.GetPhotoByRefParams{HouseholdID: hid, Ref: ref})
	if err != nil {
		writeLookupError(w, err, "No such photo.")
		return
	}
	var f *os.File
	contentType, cache := "image/jpeg", "private, max-age=31536000, immutable"
	if size != "orig" && p.Derivatives == 1 {
		if path, err := s.derivPath(p.Sha256, size); err == nil {
			f, _ = os.Open(path)
		}
	}
	if f == nil {
		path, err := s.origPath(p.Sha256, p.Ext)
		if err == nil {
			f, err = os.Open(path)
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				err = httpapi.NotFound("No such photo.")
			}
			httpapi.WriteError(w, err)
			return
		}
		contentType = p.Mime
		if size != "orig" { // stand-in until the real size exists: do not cache for long
			cache = "private, max-age=30"
		}
	}
	defer f.Close()
	etag := `"` + p.Sha256[:16] + "-" + size + "-" + strconv.FormatInt(p.Derivatives, 10) + `"`
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", "inline")
	h.Set("Cache-Control", cache)
	h.Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	io.Copy(w, f)
}
