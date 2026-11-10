// Package inventory is the places tree and the items in it: homes, places,
// items, contents lines and the category attribute sets (J3.1, J3.2).
//
// Style: plain functions over sqlc. Every write is a function taking a
// *sql.Tx (CreatePlace, UpdatePlace, DeletePlace, CreateItem, UpdateItem,
// MoveItem, ApplyAI, ...), and the HTTP handlers only decode, call one and
// encode, so recording events (J3.6) means touching those functions and
// nothing else. Reads take a sqlc.DBTX so they run on the read pool or inside
// a transaction.
package inventory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

// clock is replaced by tests.
var clock = func() time.Time { return time.Now().UTC() }

func now() string { return clock().Format(timeLayout) }

// Service registers the inventory endpoints.
type Service struct{ db *db.DB }

// New returns the inventory service.
func New(d *db.DB) *Service { return &Service{db: d} }

// Routes registers every endpoint of this package.
func (s *Service) Routes(mux *httpapi.Mux) {
	read := func(h http.HandlerFunc) http.HandlerFunc { return auth.Require(auth.ActionInventoryRead, h) }
	write := func(h http.HandlerFunc) http.HandlerFunc { return auth.Require(auth.ActionInventoryWrite, h) }
	admin := func(h http.HandlerFunc) http.HandlerFunc { return auth.Require(auth.ActionHomesManage, h) }

	mux.HandleFunc("GET /api/v1/categories", read(s.getCategories))
	mux.HandleFunc("GET /api/v1/me/recent-places", read(s.recentPlaces))
	s.boxRoutes(mux)

	mux.HandleFunc("GET /api/v1/homes", read(s.listHomes))
	mux.HandleFunc("POST /api/v1/homes", admin(s.postHome))
	mux.HandleFunc("PATCH /api/v1/homes/{id}", admin(s.patchHome))
	mux.HandleFunc("GET /api/v1/homes/{id}/tree", read(s.getTree))

	mux.HandleFunc("GET /api/v1/overview", read(s.getOverview))
	mux.HandleFunc("GET /api/v1/history", read(s.householdHistory))
	mux.HandleFunc("GET /api/v1/items/{id}/history", read(s.itemHistory))
	mux.HandleFunc("GET /api/v1/places/{id}/history", read(s.placeHistory))

	mux.HandleFunc("GET /api/v1/places/{id}", read(s.getPlace))
	mux.HandleFunc("POST /api/v1/places", write(s.postPlace))
	mux.HandleFunc("PATCH /api/v1/places/{id}", write(s.patchPlace))
	mux.HandleFunc("DELETE /api/v1/places/{id}", write(s.deletePlace))
	mux.HandleFunc("POST /api/v1/places/{id}/restore", write(s.restorePlace))
	mux.HandleFunc("GET /api/v1/places/{id}/lines", read(s.getLines))
	mux.HandleFunc("POST /api/v1/places/{id}/lines/confirm", write(s.confirmLines))
	mux.HandleFunc("POST /api/v1/places/{id}/lines/promote-all", write(s.promoteAllLines))

	mux.HandleFunc("GET /api/v1/items", read(s.listItems))
	mux.HandleFunc("POST /api/v1/items", write(s.postItem))
	mux.HandleFunc("POST /api/v1/items/bulk", write(s.bulkItems))
	mux.HandleFunc("GET /api/v1/items/{id}", read(s.getItem))
	mux.HandleFunc("PATCH /api/v1/items/{id}", write(s.patchItem))
	mux.HandleFunc("DELETE /api/v1/items/{id}", write(s.deleteItem))
	mux.HandleFunc("POST /api/v1/items/{id}/restore", write(s.restoreItem))
	mux.HandleFunc("POST /api/v1/items/{id}/move", write(s.moveItem))
	mux.HandleFunc("POST /api/v1/items/{id}/seen", write(s.seenItem))
	mux.HandleFunc("POST /api/v1/items/{id}/confirm", write(s.confirmItem))

	mux.HandleFunc("POST /api/v1/lines", write(s.postLines))
	mux.HandleFunc("PATCH /api/v1/lines/{id}", write(s.patchLine))
	mux.HandleFunc("DELETE /api/v1/lines/{id}", write(s.deleteLine))
	mux.HandleFunc("POST /api/v1/lines/{id}/restore", write(s.restoreLine))
	mux.HandleFunc("POST /api/v1/lines/{id}/promote", write(s.promoteLine))
}

// ---- small helpers ------------------------------------------------------

// Optional is a JSON field that tells absent from null from a value, for
// partial updates.
type Optional[T any] struct {
	Set  bool
	Null bool
	V    T
}

// UnmarshalJSON implements json.Unmarshaler.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.V)
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func strPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func newPage[T any](data []T, next string) httpapi.Page[T] { return httpapi.NewPage(data, next) }

func invalid(field, msg string) *httpapi.Error {
	return httpapi.Unprocessable(msg, map[string]string{field: msg})
}
