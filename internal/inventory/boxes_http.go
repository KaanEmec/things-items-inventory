package inventory

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// boxRoutes registers the box endpoints and the packing-now box.
func (s *Service) boxRoutes(mux *httpapi.Mux) {
	read := func(h http.HandlerFunc) http.HandlerFunc { return auth.Require(auth.ActionInventoryRead, h) }
	write := func(h http.HandlerFunc) http.HandlerFunc { return auth.Require(auth.ActionInventoryWrite, h) }
	mux.HandleFunc("GET /api/v1/boxes", read(s.listBoxes))
	mux.HandleFunc("POST /api/v1/boxes", write(s.postBoxes))
	mux.HandleFunc("GET /api/v1/boxes/number/{n}", read(s.getBoxByNumber))
	mux.HandleFunc("GET /api/v1/boxes/{id}", read(s.getBox))
	mux.HandleFunc("PATCH /api/v1/boxes/{id}", write(s.patchBox))
	mux.HandleFunc("POST /api/v1/boxes/{id}/close", write(s.closeBox))
	mux.HandleFunc("PUT /api/v1/me/packing-box", write(s.putPackingBox))
}

func (s *Service) listBoxes(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := httpapi.ParsePaging(r)
	if err != nil {
		fail(w, err)
		return
	}
	qs := r.URL.Query()
	boxes, next, err := ListBoxes(r.Context(), s.db.Read, auth.HouseholdID(r.Context()),
		BoxFilter{Status: qs.Get("status"), GoesToID: qs.Get("goes_to_id"), Cursor: cursor, Limit: limit})
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, newPage(boxes, next))
}

// postBoxes creates one box ({}), several ({count}) or a range ({from, to}).
// The answer is {data: [created boxes in number order]}.
func (s *Service) postBoxes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Count    int64    `json:"count"`
		From     int64    `json:"from"`
		To       int64    `json:"to"`
		GoesToID string   `json:"goes_to_id"`
		Handling []string `json:"handling"`
		Note     string   `json:"note"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Count == 0 && in.From == 0 && in.To == 0 {
		in.Count = 1
	}
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out []Place
	if s.tx(w, r, func(tx *sql.Tx) error {
		rows, err := CreateBoxes(ctx, tx, hid, in.Count, in.From, in.To, BoxInput{GoesToID: in.GoesToID, Handling: in.Handling, Note: in.Note})
		if err != nil {
			return err
		}
		out = make([]Place, len(rows))
		for i, p := range rows {
			if out[i], err = PlaceView(ctx, tx, hid, p); err != nil {
				return err
			}
		}
		return nil
	}) {
		httpapi.WriteJSON(w, http.StatusCreated, newPage(out, ""))
	}
}

func (s *Service) getBox(w http.ResponseWriter, r *http.Request) {
	b, err := GetBox(r.Context(), s.db.Read, auth.HouseholdID(r.Context()), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, b)
}

func (s *Service) getBoxByNumber(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.ParseInt(r.PathValue("n"), 10, 64)
	if err != nil {
		fail(w, httpapi.NotFound("There is no box with that number."))
		return
	}
	b, err := GetBoxByNumber(r.Context(), s.db.Read, auth.HouseholdID(r.Context()), n)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, b)
}

// boxWrite runs a box change and answers with the box (full place JSON).
func (s *Service) boxWrite(w http.ResponseWriter, r *http.Request, fn func(tx *sql.Tx, hid, uid string) (sqlc.Place, error)) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out Place
	if s.tx(w, r, func(tx *sql.Tx) error {
		p, err := fn(tx, hid, auth.UserID(ctx))
		if err != nil {
			return err
		}
		out, err = PlaceView(ctx, tx, hid, p)
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusOK, out)
	}
}

func (s *Service) patchBox(w http.ResponseWriter, r *http.Request) {
	var in BoxChange
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	s.boxWrite(w, r, func(tx *sql.Tx, hid, uid string) (sqlc.Place, error) {
		return UpdateBox(r.Context(), tx, hid, uid, r.PathValue("id"), in)
	})
}

func (s *Service) closeBox(w http.ResponseWriter, r *http.Request) {
	s.boxWrite(w, r, func(tx *sql.Tx, hid, uid string) (sqlc.Place, error) {
		return CloseBox(r.Context(), tx, hid, uid, r.PathValue("id"))
	})
}

// putPackingBox sets ({box_id} or {number}) or clears (null) the caller's
// packing-now box. It never creates a box. The answer is {packing_box}.
func (s *Service) putPackingBox(w http.ResponseWriter, r *http.Request) {
	var in *struct {
		BoxID  string `json:"box_id"`
		Number *int64 `json:"number"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	ctx, hid, uid := r.Context(), auth.HouseholdID(r.Context()), auth.UserID(r.Context())
	boxID, number := "", (*int64)(nil)
	if in != nil {
		if in.BoxID == "" && in.Number == nil {
			fail(w, invalid("box_id", "Give box_id or number, or null to clear."))
			return
		}
		boxID, number = in.BoxID, in.Number
	}
	var pb *PackingBox
	if s.tx(w, r, func(tx *sql.Tx) (err error) {
		if err = SetPackingBox(ctx, tx, hid, uid, boxID, number); err != nil {
			return err
		}
		pb, err = PackingBoxOf(ctx, tx, hid, uid)
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusOK, struct {
			PackingBox *PackingBox `json:"packing_box"`
		}{pb})
	}
}
