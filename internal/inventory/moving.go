package inventory

// Moving, second half (J8.4): changing many boxes at once and unpacking one.

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// MaxBulkBoxes bounds one bulk call.
const MaxBulkBoxes = 200

// BulkBoxes applies one BoxChange to several boxes (all arrived, one room,
// handling marks). Each box gets its own events, as if changed one by one.
func BulkBoxes(ctx context.Context, tx *sql.Tx, householdID, userID string, ids []string, ch BoxChange) ([]sqlc.Place, error) {
	if len(ids) == 0 || len(ids) > MaxBulkBoxes {
		return nil, invalid("ids", "Give between 1 and 200 boxes.")
	}
	out := make([]sqlc.Place, 0, len(ids))
	for _, id := range ids {
		p, err := UpdateBox(ctx, tx, householdID, userID, id, ch)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// UnpackMove says where one thing of a box goes: an item or a line, to a
// place, or to nowhere (nil PlaceID = "not found").
type UnpackMove struct {
	ItemID  string  `json:"item_id"`
	LineID  string  `json:"line_id"`
	PlaceID *string `json:"place_id"`
}

// UnpackBox moves things out of a box. Moves name items or lines of this box;
// RestTo (optional) is a place for everything the moves do not mention. An
// item with no place stays unplaced and shows up under "needs a look"; a line
// with no place is deleted (it can be restored). When the box is empty
// afterwards it becomes unpacked; its number is never reused.
func UnpackBox(ctx context.Context, tx *sql.Tx, householdID, userID, boxID string, moves []UnpackMove, restTo *string) (sqlc.Place, error) {
	box, err := liveBox(ctx, tx, householdID, boxID)
	if err != nil {
		return box, err
	}
	switch box.BoxStatus.String {
	case "empty", "open":
		return box, httpapi.Conflict("Close the box before unpacking it.")
	}
	d, err := boxDetail(ctx, tx, householdID, box)
	if err != nil {
		return box, err
	}
	inBox := map[string]bool{}
	for _, i := range d.Items {
		inBox[i.ID] = true
	}
	for _, l := range d.Lines {
		inBox[l.ID] = true
	}
	done := map[string]bool{}
	do := func(m UnpackMove) error {
		id := m.ItemID
		if id == "" {
			id = m.LineID
		}
		if (m.ItemID == "") == (m.LineID == "") || !inBox[id] || done[id] {
			return invalid("moves", "Each move names one item or line of this box.")
		}
		done[id] = true
		if m.PlaceID != nil && *m.PlaceID == box.ID {
			return invalid("moves", "That is the same box.")
		}
		if m.ItemID != "" {
			_, err := MoveItem(ctx, tx, householdID, userID, m.ItemID, m.PlaceID)
			return err
		}
		l, err := DeleteLine(ctx, tx, householdID, m.LineID)
		if err != nil || m.PlaceID == nil {
			return err
		}
		q := l.Quantity
		_, err = CreateLines(ctx, tx, householdID, userID, *m.PlaceID, []LineInput{{Name: l.Name, Quantity: &q, Category: l.Category.String}})
		return err
	}
	for _, m := range moves {
		if err := do(m); err != nil {
			return box, err
		}
	}
	if restTo != nil {
		for _, i := range d.Items {
			if !done[i.ID] {
				if err := do(UnpackMove{ItemID: i.ID, PlaceID: restTo}); err != nil {
					return box, err
				}
			}
		}
		for _, l := range d.Lines {
			if !done[l.ID] {
				if err := do(UnpackMove{LineID: l.ID, PlaceID: restTo}); err != nil {
					return box, err
				}
			}
		}
	}
	left, err := boxDetail(ctx, tx, householdID, box)
	if err != nil {
		return box, err
	}
	if len(left.Items)+len(left.Lines) == 0 {
		return setBoxStatus(ctx, tx, householdID, userID, box, "unpacked")
	}
	return liveBox(ctx, tx, householdID, boxID)
}

func (s *Service) bulkBoxes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
		BoxChange
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	ctx, hid := r.Context(), auth.HouseholdID(r.Context())
	var out []Place
	if s.tx(w, r, func(tx *sql.Tx) error {
		rows, err := BulkBoxes(ctx, tx, hid, auth.UserID(ctx), in.IDs, in.BoxChange)
		if err != nil {
			return err
		}
		for _, p := range rows {
			v, err := PlaceView(ctx, tx, hid, p)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	}) {
		httpapi.WriteJSON(w, http.StatusOK, struct {
			Data []Place `json:"data"`
		}{out})
	}
}

func (s *Service) unpackBox(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Moves  []UnpackMove `json:"moves"`
		RestTo *string      `json:"rest_to"`
	}
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	s.boxWrite(w, r, func(tx *sql.Tx, hid, uid string) (sqlc.Place, error) {
		return UnpackBox(r.Context(), tx, hid, uid, r.PathValue("id"), in.Moves, in.RestTo)
	})
}
