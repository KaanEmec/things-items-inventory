package inventory

import (
	"database/sql"
	"net/http"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// tx runs fn in a write transaction for the request; the live stream hears of
// the changes once it committed.
func (s *Service) tx(w http.ResponseWriter, r *http.Request, fn func(tx *sql.Tx) error) bool {
	if err := InTx(r.Context(), s.db, fn); err != nil {
		httpapi.WriteError(w, err)
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) { httpapi.WriteError(w, err) }

func (s *Service) listHomes(w http.ResponseWriter, r *http.Request) {
	archived := int64(0)
	if r.URL.Query().Get("archived") == "1" {
		archived = 1
	}
	rows, err := sqlc.New(s.db.Read).ListHomes(r.Context(), sqlc.ListHomesParams{HouseholdID: auth.HouseholdID(r.Context()), Archived: archived})
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]Home, len(rows))
	for i, h := range rows {
		out[i] = homeJSON(h)
	}
	httpapi.WriteJSON(w, http.StatusOK, newPage(out, ""))
}

func (s *Service) postHome(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string `json:"name"`
		CopyRoomsFrom string `json:"copy_rooms_from"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	var h sqlc.Home
	if s.tx(w, r, func(tx *sql.Tx) (err error) {
		h, err = CreateHome(r.Context(), tx, auth.HouseholdID(r.Context()), in.Name, in.CopyRoomsFrom)
		return
	}) {
		httpapi.WriteJSON(w, http.StatusCreated, homeJSON(h))
	}
}

func (s *Service) patchHome(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     *string `json:"name"`
		Sort     *int64  `json:"sort"`
		Archived *bool   `json:"archived"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	var h sqlc.Home
	if s.tx(w, r, func(tx *sql.Tx) (err error) {
		h, err = UpdateHome(r.Context(), tx, auth.HouseholdID(r.Context()), r.PathValue("id"), in.Name, in.Sort, in.Archived)
		return
	}) {
		httpapi.WriteJSON(w, http.StatusOK, homeJSON(h))
	}
}

func (s *Service) getTree(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	home, err := sqlc.New(s.db.Read).GetHome(ctx, sqlc.GetHomeParams{HouseholdID: hid, ID: r.PathValue("id")})
	if isNoRows(err) {
		fail(w, httpapi.NotFound("That home does not exist."))
		return
	} else if err != nil {
		fail(w, err)
		return
	}
	places, err := Tree(ctx, s.db.Read, hid, home.ID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Home   Home    `json:"home"`
		Places []Place `json:"places"`
	}{homeJSON(home), places})
}

func (s *Service) getPlace(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	p, err := GetPlace(ctx, s.db.Read, hid, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	view, err := PlaceView(ctx, s.db.Read, hid, p)
	if err != nil {
		fail(w, err)
		return
	}
	kids, err := sqlc.New(s.db.Read).ListChildPlaces(ctx, sqlc.ListChildPlacesParams{HouseholdID: hid, ParentID: nullStr(p.ID)})
	if err != nil {
		fail(w, err)
		return
	}
	children := make([]Place, len(kids))
	for i, k := range kids {
		children[i] = placeJSON(k, nil, nil)
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Place
		Children []Place `json:"children"`
	}{view, children})
}

func (s *Service) postPlace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		HomeID   string `json:"home_id"`
		ParentID string `json:"parent_id"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Sort     *int64 `json:"sort"`
		GoesToID string `json:"goes_to_id"`
		Color    string `json:"color"`
		DailyUse bool   `json:"daily_use"`
		Note     string `json:"note"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Kind == "box" {
		fail(w, invalid("kind", "Boxes are created with POST /boxes."))
		return
	}
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out Place
	if s.tx(w, r, func(tx *sql.Tx) error {
		p, err := CreatePlace(ctx, tx, hid, PlaceInput{HomeID: in.HomeID, ParentID: in.ParentID, Kind: in.Kind, Name: in.Name,
			Sort: in.Sort, GoesToID: in.GoesToID, Color: in.Color, DailyUse: in.DailyUse, Note: in.Note})
		if err != nil {
			return err
		}
		out, err = PlaceView(ctx, tx, hid, p)
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusCreated, out)
	}
}

func (s *Service) patchPlace(w http.ResponseWriter, r *http.Request) {
	var in PlaceChange
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out Place
	if s.tx(w, r, func(tx *sql.Tx) error {
		p, err := UpdatePlace(ctx, tx, hid, r.PathValue("id"), in)
		if err != nil {
			return err
		}
		out, err = PlaceView(ctx, tx, hid, p)
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusOK, out)
	}
}

func (s *Service) deletePlace(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out Place
	if s.tx(w, r, func(tx *sql.Tx) error {
		p, err := DeletePlace(ctx, tx, hid, auth.UserID(ctx), r.PathValue("id"), r.URL.Query().Get("move_to"))
		if err != nil {
			return err
		}
		out, err = PlaceView(ctx, tx, hid, p)
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusOK, out)
	}
}

func (s *Service) restorePlace(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out Place
	if s.tx(w, r, func(tx *sql.Tx) error {
		p, err := RestorePlace(ctx, tx, hid, r.PathValue("id"))
		if err != nil {
			return err
		}
		out, err = PlaceView(ctx, tx, hid, p)
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusOK, out)
	}
}
