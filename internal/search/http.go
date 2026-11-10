package search

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// Service registers the search endpoint.
type Service struct{ db *db.DB }

// New returns the search service and makes sure the index follows every
// inventory write (see Subscribe).
func New(d *db.DB) *Service {
	Subscribe()
	return &Service{db: d}
}

// Routes registers GET /api/v1/search.
//
// Response (200):
//
//	{"q": "...",
//	 "top": <match>|null,            // best hit overall; a bare number puts that box here
//	 "items": [<match>], "lines": [<match>], "places": [<match>], "boxes": [<match>]}
//
//	match = {"kind": "item|line|place|box", "id": "...",
//	         "title": "plain text", "title_marks": [[start, end], ...],
//	         "snippet": "plain text", "snippet_marks": [[start, end], ...],
//	         "path": "place path", "box_number": 12 (boxes only),
//	         "place_id": "..." (lines only: the place or box holding the line)}
//
// Text is never HTML. A mark is a [start, end) range in UTF-16 code units
// of the string beside it, so the page can cut with text.slice(start, end)
// and wrap the pieces in <mark> itself. Marks are sorted and do not overlap.
//
// Parameters: q (required), limit (per group, default 8, max 50), kinds
// (comma list of items,lines,places,boxes).
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("GET /api/v1/search", auth.Require(auth.ActionInventoryRead, s.get))
	mux.HandleFunc("GET /api/v1/search/similar", auth.Require(auth.ActionInventoryRead, s.similar))
}

// similar answers GET /api/v1/search/similar?brand=&model=&name=&exclude=id,id
// with {"items": [<match>]}: live items of the household that look like the
// one being added (same brand and model, or a distinctive close name). Each
// match has why = brand_model | name. Generic names match nothing.
func (s *Service) similar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var exclude []string
	for _, id := range strings.Split(q.Get("exclude"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			exclude = append(exclude, id)
		}
	}
	out, err := Similar(r.Context(), s.db.Read, auth.HouseholdID(r.Context()), q.Get("brand"), q.Get("model"), q.Get("name"), exclude...)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	if out == nil {
		out = []Match{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

var kindByGroup = map[string]string{"items": KindItem, "lines": KindLine, "places": KindPlace, "boxes": KindBox}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpapi.WriteError(w, httpapi.BadRequest("Type something to search for (q)."))
		return
	}
	opt := Options{}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpapi.WriteError(w, httpapi.BadRequest("limit must be a positive number."))
			return
		}
		opt.Limit = n
	}
	if v := r.URL.Query().Get("kinds"); v != "" {
		for _, g := range strings.Split(v, ",") {
			k, ok := kindByGroup[strings.TrimSpace(g)]
			if !ok {
				httpapi.WriteError(w, httpapi.BadRequest("kinds takes items, lines, places and boxes."))
				return
			}
			opt.Kinds = append(opt.Kinds, k)
		}
	}
	res, err := Search(r.Context(), s.db.Read, auth.HouseholdID(r.Context()), q, opt)
	if err != nil {
		httpapi.WriteError(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
}

// Seed creates a few rows owned by householdID and indexes them, for the
// cross-household test. It returns marker strings that must never appear in
// another household's responses.
func (s *Service) Seed(ctx context.Context, d *db.DB, householdID string) ([]string, error) {
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		h, err := inventory.CreateHome(ctx, tx, householdID, "Search-Home-9Z", "")
		if err != nil {
			return err
		}
		room, err := inventory.CreatePlace(ctx, tx, householdID, inventory.PlaceInput{HomeID: h.ID, Kind: "room", Name: "Search-Room-9Z"})
		if err != nil {
			return err
		}
		item, _, err := inventory.CreateItem(ctx, tx, householdID, "", inventory.NewItem{Fields: inventory.Fields{
			"name": []byte(`"Search-Item-9Z"`), "place_id": []byte(`"` + room.ID + `"`), "brand": []byte(`"Search-Brand-9Z"`)}})
		if err != nil {
			return err
		}
		_ = item
		_, err = RebuildTx(ctx, tx, householdID)
		return err
	})
	return []string{"Search-Home-9Z", "Search-Room-9Z", "Search-Item-9Z", "Search-Brand-9Z", "search-item-9z"}, err
}
