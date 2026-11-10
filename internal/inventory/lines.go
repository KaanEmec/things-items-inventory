package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// Line is the contents line JSON.
type Line struct {
	ID        string  `json:"id"`
	PlaceID   string  `json:"place_id"`
	Name      string  `json:"name"`
	Quantity  int64   `json:"quantity"`
	Category  *string `json:"category"`
	WorthItem bool    `json:"worth_item"`
	AI        bool    `json:"ai"`
	PhotoID   *string `json:"photo_id"`
	ItemID    *string `json:"item_id,omitempty"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	CreatedAt string  `json:"created_at"`
}

// LineJSON returns the JSON of a contents line row.
func LineJSON(l sqlc.ContentsLine) Line {
	return Line{ID: l.ID, PlaceID: l.PlaceID, Name: l.Name, Quantity: l.Quantity, Category: strPtr(l.Category),
		WorthItem: l.WorthItem != 0, AI: l.Ai != 0, PhotoID: strPtr(l.PhotoID), ItemID: strPtr(l.ItemID),
		DeletedAt: strPtr(l.DeletedAt), CreatedAt: l.CreatedAt}
}

// LineInput is one new contents line.
type LineInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Quantity *int64 `json:"quantity"`
	Category string `json:"category"`
	// For the AI contents job.
	AI      bool   `json:"-"`
	PhotoID string `json:"-"`
}

func checkLine(name string, qty int64, category string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return "", invalid("name", "The name is required (up to 200 characters).")
	}
	if qty < 0 || qty > 1_000_000 {
		return "", invalid("quantity", "Must be a whole number from 0 to 1,000,000.")
	}
	if category != "" {
		if _, ok := CategoryByKey(category); !ok {
			return "", invalid("category", "Unknown category.")
		}
	}
	return name, nil
}

// CreateLines adds contents lines to a container. A line whose id exists
// already is returned as it is (idempotent retries).
func CreateLines(ctx context.Context, tx *sql.Tx, householdID, userID, placeID string, in []LineInput) ([]sqlc.ContentsLine, error) {
	q := sqlc.New(tx)
	if _, err := getLivePlace(ctx, tx, householdID, placeID); err != nil {
		return nil, invalid("place_id", "No such place.")
	}
	if len(in) == 0 || len(in) > 200 {
		return nil, invalid("lines", "Give 1 to 200 lines.")
	}
	out := make([]sqlc.ContentsLine, 0, len(in))
	t := now()
	for _, l := range in {
		if l.ID != "" {
			if !idRe.MatchString(l.ID) {
				return nil, invalid("id", "Must be a 26-character ULID.")
			}
			if row, err := q.GetLine(ctx, sqlc.GetLineParams{HouseholdID: householdID, ID: l.ID}); err == nil {
				out = append(out, row)
				continue
			} else if !isNoRows(err) {
				return nil, err
			}
		} else {
			l.ID = id.New()
		}
		qty := int64(1)
		if l.Quantity != nil {
			qty = *l.Quantity
		}
		name, err := checkLine(l.Name, qty, l.Category)
		if err != nil {
			return nil, err
		}
		row, err := q.CreateLine(ctx, sqlc.CreateLineParams{ID: l.ID, HouseholdID: householdID, CreatedAt: t, UpdatedAt: t,
			PlaceID: placeID, Name: name, Quantity: qty, Category: nullStr(l.Category), WorthItem: 0,
			PhotoID: nullStr(l.PhotoID), Ai: boolInt(l.AI), CreatedBy: nullStr(userID)})
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func getLine(ctx context.Context, q *sqlc.Queries, householdID, lineID string) (sqlc.ContentsLine, error) {
	l, err := q.GetLine(ctx, sqlc.GetLineParams{HouseholdID: householdID, ID: lineID})
	if isNoRows(err) {
		return l, httpapi.NotFound("That contents line does not exist.")
	}
	return l, err
}

// LineChange is a partial update of a line.
type LineChange struct {
	Name     Optional[string] `json:"name"`
	Quantity Optional[int64]  `json:"quantity"`
	Category Optional[string] `json:"category"`
}

// UpdateLine edits a line; a person's edit also clears its ai mark.
func UpdateLine(ctx context.Context, tx *sql.Tx, householdID, lineID string, ch LineChange) (sqlc.ContentsLine, error) {
	q := sqlc.New(tx)
	l, err := getLine(ctx, q, householdID, lineID)
	if err != nil {
		return l, err
	}
	name, qty, cat := l.Name, l.Quantity, l.Category
	if ch.Name.Set {
		name = ch.Name.V
	}
	if ch.Quantity.Set {
		if ch.Quantity.Null {
			return l, invalid("quantity", "The quantity is required.")
		}
		qty = ch.Quantity.V
	}
	if ch.Category.Set {
		cat = sql.NullString{String: ch.Category.V, Valid: !ch.Category.Null && ch.Category.V != ""}
	}
	if name, err = checkLine(name, qty, cat.String); err != nil {
		return l, err
	}
	return q.UpdateLine(ctx, sqlc.UpdateLineParams{Name: name, Quantity: qty, Category: cat, UpdatedAt: now(), HouseholdID: householdID, ID: lineID})
}

// DeleteLine soft-deletes a line.
func DeleteLine(ctx context.Context, tx *sql.Tx, householdID, lineID string) (sqlc.ContentsLine, error) {
	q := sqlc.New(tx)
	l, err := getLine(ctx, q, householdID, lineID)
	if err != nil || l.DeletedAt.Valid {
		return l, err
	}
	t := now()
	if err := q.SetLineDeleted(ctx, sqlc.SetLineDeletedParams{DeletedAt: nullStr(t), UpdatedAt: t, HouseholdID: householdID, ID: lineID}); err != nil {
		return l, err
	}
	return getLine(ctx, q, householdID, lineID)
}

// RestoreLine undoes DeleteLine. A promoted line stays gone: its item lives on.
func RestoreLine(ctx context.Context, tx *sql.Tx, householdID, lineID string) (sqlc.ContentsLine, error) {
	q := sqlc.New(tx)
	l, err := getLine(ctx, q, householdID, lineID)
	if err != nil || !l.DeletedAt.Valid {
		return l, err
	}
	if l.ItemID.Valid {
		return l, httpapi.Conflict("That line became an item; restore or edit the item instead.")
	}
	if err := q.SetLineDeleted(ctx, sqlc.SetLineDeletedParams{DeletedAt: sql.NullString{}, UpdatedAt: now(), HouseholdID: householdID, ID: lineID}); err != nil {
		return l, err
	}
	return getLine(ctx, q, householdID, lineID)
}

// ConfirmLines clears the ai mark on every line of a container.
func ConfirmLines(ctx context.Context, tx *sql.Tx, householdID, placeID string) ([]sqlc.ContentsLine, error) {
	q := sqlc.New(tx)
	if _, err := GetPlace(ctx, tx, householdID, placeID); err != nil {
		return nil, err
	}
	if _, err := q.ConfirmLines(ctx, sqlc.ConfirmLinesParams{UpdatedAt: now(), HouseholdID: householdID, PlaceID: placeID}); err != nil {
		return nil, err
	}
	return q.ListLines(ctx, sqlc.ListLinesParams{HouseholdID: householdID, PlaceID: placeID})
}

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// PromoteLine turns a contents line into a draft item in the same container
// and soft-deletes the line (it keeps item_id). Promoting twice returns the
// same item. Linking the contents photo to the item is the photos feature's
// job; the line's photo_id is still readable on the line.
func PromoteLine(ctx context.Context, tx *sql.Tx, householdID, userID, lineID string) (sqlc.Item, error) {
	q := sqlc.New(tx)
	l, err := getLine(ctx, q, householdID, lineID)
	if err != nil {
		return sqlc.Item{}, err
	}
	if l.ItemID.Valid {
		return getItemRow(ctx, q, householdID, l.ItemID.String)
	}
	if l.DeletedAt.Valid {
		return sqlc.Item{}, httpapi.NotFound("That contents line was deleted.")
	}
	f := Fields{"name": rawJSON(l.Name), "quantity": rawJSON(l.Quantity), "place_id": rawJSON(l.PlaceID), "draft": rawJSON(true)}
	if l.Category.Valid {
		f["category"] = rawJSON(l.Category.String)
	}
	item, _, err := CreateItem(ctx, tx, householdID, userID, NewItem{Fields: f})
	if err != nil {
		return item, err
	}
	t := now()
	if err := q.SetLinePromoted(ctx, sqlc.SetLinePromotedParams{ItemID: nullStr(item.ID), DeletedAt: nullStr(t), UpdatedAt: t,
		HouseholdID: householdID, ID: lineID}); err != nil {
		return item, err
	}
	return item, nil
}

// PromoteAllLines promotes every live line of a container.
func PromoteAllLines(ctx context.Context, tx *sql.Tx, householdID, userID, placeID string) ([]sqlc.Item, error) {
	q := sqlc.New(tx)
	if _, err := GetPlace(ctx, tx, householdID, placeID); err != nil {
		return nil, err
	}
	lines, err := q.ListLines(ctx, sqlc.ListLinesParams{HouseholdID: householdID, PlaceID: placeID})
	if err != nil {
		return nil, err
	}
	out := make([]sqlc.Item, 0, len(lines))
	for _, l := range lines {
		it, err := PromoteLine(ctx, tx, householdID, userID, l.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}
