package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

func (s *Service) getCategories(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Categories []Category `json:"categories"`
		Statuses   []string   `json:"statuses"`
		Conditions []string   `json:"conditions"`
		PlaceKinds []string   `json:"place_kinds"`
		RoomColors []string   `json:"room_colors"`
	}{Categories, Statuses, Conditions, PlaceKinds, RoomColors})
}

func (s *Service) listItems(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	cursor, limit, err := httpapi.ParsePaging(r)
	if err != nil {
		fail(w, err)
		return
	}
	qs := r.URL.Query()
	f := ItemFilter{Category: qs.Get("category"), Statuses: qs["status"], PlaceID: qs.Get("place_id"),
		Below: qs.Get("below") == "1", Draft: qs.Get("draft") == "1", Warranty: qs.Get("warranty"),
		ImportID: qs.Get("import_id"), Sort: qs.Get("sort"), Cursor: cursor, Limit: limit}
	if len(f.Statuses) == 1 && strings.Contains(f.Statuses[0], ",") {
		f.Statuses = strings.Split(f.Statuses[0], ",")
	}
	if f.PlaceID != "" {
		if _, err := GetPlace(ctx, s.db.Read, hid, f.PlaceID); err != nil {
			fail(w, err)
			return
		}
	}
	items, next, err := ListItems(ctx, s.db.Read, hid, f)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, newPage(items, next))
}

func (s *Service) getItem(w http.ResponseWriter, r *http.Request) {
	it, err := GetItem(r.Context(), s.db.Read, auth.HouseholdID(r.Context()), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, it)
}

// writeItem runs change in a transaction and answers with the changed item.
func (s *Service) writeItem(w http.ResponseWriter, r *http.Request, status int, change func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error)) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out Item
	if s.tx(w, r, func(tx *sql.Tx) error {
		itemID, err := change(ctx, tx, hid, auth.UserID(ctx))
		if err != nil {
			return err
		}
		out, err = GetItem(ctx, tx, hid, itemID)
		return err
	}) {
		httpapi.WriteJSON(w, status, out)
	}
}

func (s *Service) postItem(w http.ResponseWriter, r *http.Request) {
	var f Fields
	if err := httpapi.DecodeJSON(w, r, &f); err != nil {
		fail(w, err)
		return
	}
	n := NewItem{Fields: f}
	if raw, ok := f["id"]; ok {
		if err := jsonString(raw, &n.ID); err != nil {
			fail(w, invalid("id", "Must be a 26-character ULID."))
			return
		}
		delete(f, "id")
	}
	if raw, ok := f["source"]; ok {
		if err := jsonString(raw, &n.Source); err != nil {
			fail(w, invalid("source", "Must be text."))
			return
		}
		delete(f, "source")
	}
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out Item
	status := http.StatusCreated
	if s.tx(w, r, func(tx *sql.Tx) error {
		row, created, err := CreateItem(ctx, tx, hid, auth.UserID(ctx), n)
		if err != nil {
			return err
		}
		if !created {
			status = http.StatusOK
		}
		out, err = GetItem(ctx, tx, hid, row.ID)
		return err
	}) {
		httpapi.WriteJSON(w, status, out)
	}
}

func jsonString(raw json.RawMessage, dst *string) error { return json.Unmarshal(raw, dst) }

func (s *Service) patchItem(w http.ResponseWriter, r *http.Request) {
	var f Fields
	if err := httpapi.DecodeJSON(w, r, &f); err != nil {
		fail(w, err)
		return
	}
	s.writeItem(w, r, http.StatusOK, func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error) {
		row, err := UpdateItem(ctx, tx, hid, uid, r.PathValue("id"), f)
		return row.ID, err
	})
}

func (s *Service) deleteItem(w http.ResponseWriter, r *http.Request) {
	s.writeItem(w, r, http.StatusOK, func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error) {
		row, err := DeleteItem(ctx, tx, hid, uid, r.PathValue("id"))
		return row.ID, err
	})
}

func (s *Service) restoreItem(w http.ResponseWriter, r *http.Request) {
	s.writeItem(w, r, http.StatusOK, func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error) {
		row, err := RestoreItem(ctx, tx, hid, uid, r.PathValue("id"))
		return row.ID, err
	})
}

func (s *Service) moveItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PlaceID *string `json:"place_id"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	s.writeItem(w, r, http.StatusOK, func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error) {
		row, err := MoveItem(ctx, tx, hid, uid, r.PathValue("id"), in.PlaceID)
		return row.ID, err
	})
}

func (s *Service) seenItem(w http.ResponseWriter, r *http.Request) {
	s.writeItem(w, r, http.StatusOK, func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error) {
		row, err := MarkSeen(ctx, tx, hid, uid, r.PathValue("id"))
		return row.ID, err
	})
}

func (s *Service) confirmItem(w http.ResponseWriter, r *http.Request) {
	s.writeItem(w, r, http.StatusOK, func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error) {
		row, err := ConfirmItem(ctx, tx, hid, uid, r.PathValue("id"))
		return row.ID, err
	})
}

func (s *Service) bulkItems(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
		BulkChange
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out []Item
	if s.tx(w, r, func(tx *sql.Tx) error {
		rows, err := BulkUpdate(ctx, tx, hid, auth.UserID(ctx), in.IDs, in.BulkChange)
		if err != nil {
			return err
		}
		ids := make([]string, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		out, _, err = ListItems(ctx, tx, hid, ItemFilter{IDs: ids})
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusOK, newPage(out, ""))
	}
}

// ---- contents lines ---------------------------------------------------------

func (s *Service) getLines(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	p, err := GetPlace(ctx, s.db.Read, hid, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	rows, err := sqlc.New(s.db.Read).ListLines(ctx, sqlc.ListLinesParams{HouseholdID: hid, PlaceID: p.ID})
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, newPage(linesJSON(rows), ""))
}

func linesJSON(rows []sqlc.ContentsLine) []Line {
	out := make([]Line, len(rows))
	for i, l := range rows {
		out[i] = LineJSON(l)
	}
	return out
}

func (s *Service) confirmLines(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var rows []sqlc.ContentsLine
	if s.tx(w, r, func(tx *sql.Tx) (err error) {
		rows, err = ConfirmLines(ctx, tx, hid, r.PathValue("id"))
		return
	}) {
		httpapi.WriteJSON(w, http.StatusOK, newPage(linesJSON(rows), ""))
	}
}

func (s *Service) promoteAllLines(w http.ResponseWriter, r *http.Request) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out []Item
	if s.tx(w, r, func(tx *sql.Tx) error {
		rows, err := PromoteAllLines(ctx, tx, hid, auth.UserID(ctx), r.PathValue("id"))
		if err != nil {
			return err
		}
		ids := make([]string, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		if len(ids) > 0 {
			out, _, err = ListItems(ctx, tx, hid, ItemFilter{IDs: ids})
		}
		return err
	}) {
		httpapi.WriteJSON(w, http.StatusOK, newPage(out, ""))
	}
}

func (s *Service) postLines(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PlaceID string      `json:"place_id"`
		Lines   []LineInput `json:"lines"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var rows []sqlc.ContentsLine
	if s.tx(w, r, func(tx *sql.Tx) (err error) {
		rows, err = CreateLines(ctx, tx, hid, auth.UserID(ctx), in.PlaceID, in.Lines)
		return
	}) {
		httpapi.WriteJSON(w, http.StatusCreated, newPage(linesJSON(rows), ""))
	}
}

func (s *Service) lineOp(w http.ResponseWriter, r *http.Request, op func(ctx context.Context, tx *sql.Tx, hid string) (sqlc.ContentsLine, error)) {
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var row sqlc.ContentsLine
	if s.tx(w, r, func(tx *sql.Tx) (err error) {
		row, err = op(ctx, tx, hid)
		return
	}) {
		httpapi.WriteJSON(w, http.StatusOK, LineJSON(row))
	}
}

func (s *Service) patchLine(w http.ResponseWriter, r *http.Request) {
	var ch LineChange
	if err := httpapi.DecodeJSON(w, r, &ch); err != nil {
		fail(w, err)
		return
	}
	s.lineOp(w, r, func(ctx context.Context, tx *sql.Tx, hid string) (sqlc.ContentsLine, error) {
		return UpdateLine(ctx, tx, hid, r.PathValue("id"), ch)
	})
}

func (s *Service) deleteLine(w http.ResponseWriter, r *http.Request) {
	s.lineOp(w, r, func(ctx context.Context, tx *sql.Tx, hid string) (sqlc.ContentsLine, error) {
		return DeleteLine(ctx, tx, hid, r.PathValue("id"))
	})
}

func (s *Service) restoreLine(w http.ResponseWriter, r *http.Request) {
	s.lineOp(w, r, func(ctx context.Context, tx *sql.Tx, hid string) (sqlc.ContentsLine, error) {
		return RestoreLine(ctx, tx, hid, r.PathValue("id"))
	})
}

func (s *Service) promoteLine(w http.ResponseWriter, r *http.Request) {
	s.writeItem(w, r, http.StatusCreated, func(ctx context.Context, tx *sql.Tx, hid, uid string) (string, error) {
		row, err := PromoteLine(ctx, tx, hid, uid, r.PathValue("id"))
		return row.ID, err
	})
}
