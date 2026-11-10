package inventory

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
)

// ---- item JSON (spec §5) -----------------------------------------------------

// ItemPhoto is one photo link of an item.
type ItemPhoto struct {
	LinkID  string `json:"link_id"`
	PhotoID string `json:"photo_id"`
	SHA256  string `json:"sha256"`
	Role    string `json:"role"`
	Sort    int64  `json:"sort"`
}

// PhotoLoader fills Item.Photos. The photos feature sets it at start-up;
// until then items carry no photos. mainOnly asks for the main photo only
// (lists). It must do one query for all the items, never one per item.
var PhotoLoader func(ctx context.Context, q sqlc.DBTX, householdID string, itemIDs []string, mainOnly bool) (map[string][]ItemPhoto, error)

// Purchase is the purchase part of the item JSON.
type Purchase struct {
	Date       *string `json:"date"`
	PriceMinor *int64  `json:"price_minor"`
	Currency   *string `json:"currency"`
	Seller     *string `json:"seller"`
}

// Warranty is the warranty part of the item JSON.
type Warranty struct {
	End    *string `json:"end"`
	Source *string `json:"source"`
}

// UserRef names the person who last changed an item.
type UserRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Item is the item JSON.
type Item struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Category   string          `json:"category"`
	Brand      *string         `json:"brand"`
	Model      *string         `json:"model"`
	Serial     *string         `json:"serial"`
	Color      *string         `json:"color"`
	Quantity   int64           `json:"quantity"`
	Condition  *string         `json:"condition"`
	Notes      *string         `json:"notes"`
	Status     string          `json:"status"`
	Attributes map[string]any  `json:"attributes"`
	AIFields   []string        `json:"ai_fields"`
	AIState    *string         `json:"ai_state"`
	AIDraft    json.RawMessage `json:"ai_draft,omitempty"` // single reads only
	Draft      bool            `json:"draft"`
	Source     string          `json:"source"`
	ImportID   *string         `json:"import_id"`
	Place      *PlaceShort     `json:"place"`
	Purchase   Purchase        `json:"purchase"`
	Warranty   Warranty        `json:"warranty"`
	LastSeenAt string          `json:"last_seen_at"`
	Photos     []ItemPhoto     `json:"photos"`
	CreatedBy  *string         `json:"created_by"`
	UpdatedBy  *UserRef        `json:"updated_by"`
	DeletedAt  *string         `json:"deleted_at,omitempty"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

// ItemFilter selects items for ListItems.
type ItemFilter struct {
	IDs      []string // exactly these items (deleted ones too)
	Category string
	Statuses []string
	PlaceID  string
	Below    bool // with PlaceID: the place and everything below it
	Draft    bool
	Warranty string // "active", "ending" (within 60 days), "any"
	ImportID string
	Sort     string // "touched" (default), "name", "place"
	Cursor   string
	Limit    int // 0 = no limit
}

type itemRow struct {
	sqlc.Item
	placeID, placeKind, placeName, placePath, placeHome sql.NullString
	placeBox                                            sql.NullInt64
	placeColor                                          sql.NullString
	userID, userName                                    sql.NullString
	sortKey                                             string
}

const itemSelect = `SELECT i.id, i.household_id, i.created_at, i.updated_at, i.deleted_at, i.place_id, i.name, i.category,
  i.brand, i.model, i.serial, i.color, i.quantity, i.condition, i.notes, i.status, i.attributes, i.ai_fields,
  i.ai_draft, i.ai_state, i.draft, i.source, i.import_id, i.purchase_date, i.price_minor, i.currency, i.seller,
  i.warranty_end, i.warranty_source, i.last_seen_at, i.created_by, i.updated_by,
  pl.id, pl.kind, pl.name, pl.path, pl.home_id, pl.box_number, pl.color, u.id, u.name,
  %s AS sort_key
FROM items i
LEFT JOIN places pl ON pl.id = i.place_id AND pl.household_id = i.household_id
LEFT JOIN users u ON u.id = i.updated_by
WHERE i.household_id = ?`

func (r *itemRow) scan(rows *sql.Rows) error {
	i := &r.Item
	return rows.Scan(&i.ID, &i.HouseholdID, &i.CreatedAt, &i.UpdatedAt, &i.DeletedAt, &i.PlaceID, &i.Name, &i.Category,
		&i.Brand, &i.Model, &i.Serial, &i.Color, &i.Quantity, &i.Condition, &i.Notes, &i.Status, &i.Attributes, &i.AiFields,
		&i.AiDraft, &i.AiState, &i.Draft, &i.Source, &i.ImportID, &i.PurchaseDate, &i.PriceMinor, &i.Currency, &i.Seller,
		&i.WarrantyEnd, &i.WarrantySource, &i.LastSeenAt, &i.CreatedBy, &i.UpdatedBy,
		&r.placeID, &r.placeKind, &r.placeName, &r.placePath, &r.placeHome, &r.placeBox, &r.placeColor,
		&r.userID, &r.userName, &r.sortKey)
}

func (r *itemRow) json(single bool) Item {
	i := r.Item
	it := Item{ID: i.ID, Name: i.Name, Category: i.Category, Brand: strPtr(i.Brand), Model: strPtr(i.Model),
		Serial: strPtr(i.Serial), Color: strPtr(i.Color), Quantity: i.Quantity, Condition: strPtr(i.Condition),
		Notes: strPtr(i.Notes), Status: i.Status, AIState: strPtr(i.AiState), Draft: i.Draft != 0, Source: i.Source,
		ImportID: strPtr(i.ImportID), LastSeenAt: i.LastSeenAt, Photos: []ItemPhoto{}, CreatedBy: strPtr(i.CreatedBy),
		Purchase:  Purchase{Date: strPtr(i.PurchaseDate), PriceMinor: intPtr(i.PriceMinor), Currency: strPtr(i.Currency), Seller: strPtr(i.Seller)},
		Warranty:  Warranty{End: strPtr(i.WarrantyEnd), Source: strPtr(i.WarrantySource)},
		DeletedAt: strPtr(i.DeletedAt), CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt}
	if json.Unmarshal([]byte(i.Attributes), &it.Attributes) != nil || it.Attributes == nil {
		it.Attributes = map[string]any{}
	}
	if json.Unmarshal([]byte(i.AiFields), &it.AIFields) != nil || it.AIFields == nil {
		it.AIFields = []string{}
	}
	if single {
		it.AIDraft = json.RawMessage("null")
		if i.AiDraft.Valid {
			it.AIDraft = json.RawMessage(i.AiDraft.String)
		}
	}
	if r.placeID.Valid {
		it.Place = &PlaceShort{ID: r.placeID.String, Kind: r.placeKind.String, Name: r.placeName.String, Path: r.placePath.String,
			HomeID: strPtr(r.placeHome), BoxNumber: intPtr(r.placeBox), Color: strPtr(r.placeColor)}
	}
	if r.userID.Valid {
		it.UpdatedBy = &UserRef{ID: r.userID.String, Name: r.userName.String}
	}
	return it
}

type itemCursor struct {
	K string `json:"k"`
	I string `json:"i"`
}

// ListItems returns items matching f in ONE query (place-and-below included:
// the lineage range is a subquery, not a second round trip), plus the cursor
// of the next page ("" at the end). q may be the read pool or a transaction.
func ListItems(ctx context.Context, q sqlc.DBTX, householdID string, f ItemFilter) ([]Item, string, error) {
	var key string
	desc := false
	switch f.Sort {
	case "", "touched":
		key, desc = "i.updated_at", true
	case "name":
		key = "lower(i.name)"
	case "place":
		key = "COALESCE(pl.path, '')"
	default:
		return nil, "", httpapi.Invalid(map[string]string{"sort": "Use touched, name or place."})
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, itemSelect, key)
	args := []any{householdID}
	and := func(cond string, a ...any) {
		sb.WriteString("\n  AND " + cond)
		args = append(args, a...)
	}
	if len(f.IDs) > 0 {
		and("i.id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.IDs)), ",")+")", toAny(f.IDs)...)
	} else {
		and("i.deleted_at IS NULL")
	}
	if f.Category != "" {
		and("i.category = ?", f.Category)
	}
	if len(f.Statuses) > 0 {
		and("i.status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")+")", toAny(f.Statuses)...)
	}
	if f.PlaceID != "" {
		if f.Below {
			and("pl.lineage >= (SELECT lineage FROM places WHERE household_id = ? AND id = ?)"+
				" AND pl.lineage < (SELECT substr(lineage, 1, length(lineage) - 1) || '0' FROM places WHERE household_id = ? AND id = ?)",
				householdID, f.PlaceID, householdID, f.PlaceID)
		} else {
			and("i.place_id = ?", f.PlaceID)
		}
	}
	if f.Draft {
		and("i.draft = 1")
	}
	if f.ImportID != "" {
		and("i.import_id = ?", f.ImportID)
	}
	today := clock().Format("2006-01-02")
	switch f.Warranty {
	case "":
	case "any":
		and("i.warranty_end IS NOT NULL")
	case "active":
		and("i.warranty_end >= ?", today)
	case "ending":
		and("i.warranty_end >= ? AND i.warranty_end <= ?", today, clock().AddDate(0, 0, 60).Format("2006-01-02"))
	default:
		return nil, "", httpapi.Invalid(map[string]string{"warranty": "Use active, ending or any."})
	}
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c itemCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, "", httpapi.Invalid(map[string]string{"cursor": "That cursor is not valid."})
		}
		op := ">"
		if desc {
			op = "<"
		}
		and("("+key+", i.id) "+op+" (?, ?)", c.K, c.I)
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	fmt.Fprintf(&sb, "\nORDER BY %s %s, i.id %s", key, dir, dir)
	if f.Limit > 0 {
		fmt.Fprintf(&sb, "\nLIMIT %d", f.Limit+1)
	}

	rows, err := q.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var got []itemRow
	for rows.Next() {
		var r itemRow
		if err := r.scan(rows); err != nil {
			return nil, "", err
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if f.Limit > 0 && len(got) > f.Limit {
		got = got[:f.Limit]
		last := got[len(got)-1]
		b, _ := json.Marshal(itemCursor{K: last.sortKey, I: last.ID})
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	out := make([]Item, len(got))
	single := len(f.IDs) == 1
	for i := range got {
		out[i] = got[i].json(single)
	}
	if PhotoLoader != nil && len(out) > 0 {
		ids := make([]string, len(out))
		for i, it := range out {
			ids[i] = it.ID
		}
		photos, err := PhotoLoader(ctx, q, householdID, ids, !single)
		if err != nil {
			return nil, "", err
		}
		for i := range out {
			if p := photos[out[i].ID]; p != nil {
				out[i].Photos = p
			}
		}
	}
	return out, next, nil
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// GetItem loads one item's JSON (deleted ones too), with place, photos and
// ai_draft.
func GetItem(ctx context.Context, q sqlc.DBTX, householdID, itemID string) (Item, error) {
	items, _, err := ListItems(ctx, q, householdID, ItemFilter{IDs: []string{itemID}})
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, httpapi.NotFound("That item does not exist.")
	}
	return items[0], nil
}
