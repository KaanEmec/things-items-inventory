package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/id"
)

// NewItemName is the name of a fresh capture draft.
const NewItemName = "New item"

// Fields is a partial item as the API receives it: field name to raw JSON.
// Allowed names: name, category, brand, model, serial, color, quantity,
// condition, notes, status, attributes, place_id, purchase, warranty, draft.
type Fields map[string]json.RawMessage

var idRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

var attrKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ---- the edit engine ------------------------------------------------------

// edit is an item being changed. Every change, by a person or by the AI,
// goes through set, so validation is in one place; who changed it decides
// what happens to ai_fields (person: the field leaves it; AI: it joins it).
type edit struct {
	row   sqlc.Item
	attrs map[string]any
	ai    []string
	free  bool // category was unknown: attributes are free-form
	// strictCategory makes an unknown category an error (the AI path) instead
	// of a fall back to "other".
	strictCategory bool
}

func newEdit(row sqlc.Item) (*edit, error) {
	e := &edit{row: row, attrs: map[string]any{}}
	if err := json.Unmarshal([]byte(row.Attributes), &e.attrs); err != nil || e.attrs == nil {
		e.attrs = map[string]any{}
	}
	if err := json.Unmarshal([]byte(row.AiFields), &e.ai); err != nil {
		e.ai = nil
	}
	return e, nil
}

func (e *edit) marshal() (attrs, ai string) {
	a, _ := json.Marshal(e.attrs)
	if e.ai == nil {
		e.ai = []string{}
	}
	f, _ := json.Marshal(e.ai)
	return string(a), string(f)
}

func (e *edit) person(path string) {
	e.ai = slices.DeleteFunc(e.ai, func(p string) bool { return p == path })
}

func (e *edit) byAI(path string) {
	if !slices.Contains(e.ai, path) {
		e.ai = append(e.ai, path)
	}
}

func fieldErr(path, msg string) *httpapi.Error { return invalid(path, msg) }

func badKey(path, msg string) *httpapi.Error {
	err := httpapi.Invalid(map[string]string{path: msg})
	err.Message = msg
	return err
}

func (e *edit) text(path string) *sql.NullString {
	switch path {
	case "brand":
		return &e.row.Brand
	case "model":
		return &e.row.Model
	case "serial":
		return &e.row.Serial
	case "color":
		return &e.row.Color
	case "seller":
		return &e.row.Seller
	case "notes":
		return &e.row.Notes
	case "condition":
		return &e.row.Condition
	case "purchase_date":
		return &e.row.PurchaseDate
	case "warranty_end":
		return &e.row.WarrantyEnd
	case "currency":
		return &e.row.Currency
	}
	return nil
}

func optText(path string, v any, max int) (sql.NullString, error) {
	if v == nil {
		return sql.NullString{}, nil
	}
	s, ok := v.(string)
	if !ok {
		return sql.NullString{}, fieldErr(path, "Must be text.")
	}
	s = strings.TrimSpace(s)
	if len(s) > max {
		return sql.NullString{}, fieldErr(path, fmt.Sprintf("Must be at most %d characters.", max))
	}
	return nullStr(s), nil
}

// set validates v (a decoded JSON value, nil = clear) and assigns it to the
// field at path ("name", "brand", "purchase_date", "attributes.size", ...).
// On error nothing is changed.
func (e *edit) set(path string, v any) error {
	switch path {
	case "name":
		s, ok := v.(string)
		s = strings.TrimSpace(s)
		if !ok || s == "" || len(s) > 200 {
			return fieldErr(path, "The name is required (up to 200 characters).")
		}
		e.row.Name = s
	case "category":
		s, ok := v.(string)
		if !ok {
			return fieldErr(path, "Must be text.")
		}
		s = strings.TrimSpace(s)
		if _, known := CategoryByKey(s); known {
			e.row.Category, e.free = s, false
		} else if e.strictCategory {
			return fieldErr(path, "Unknown category.")
		} else {
			e.row.Category, e.free = FallbackCategory, true
		}
	case "brand", "model", "serial", "color", "seller":
		n, err := optText(path, v, 200)
		if err != nil {
			return err
		}
		*e.text(path) = n
	case "notes":
		n, err := optText(path, v, 5000)
		if err != nil {
			return err
		}
		e.row.Notes = n
	case "condition":
		n, err := optText(path, v, 20)
		if err != nil {
			return err
		}
		if n.Valid && !slices.Contains(Conditions, n.String) {
			return fieldErr(path, "Use one of: "+strings.Join(Conditions, ", ")+".")
		}
		e.row.Condition = n
	case "purchase_date", "warranty_end":
		n, err := optText(path, v, 10)
		if err != nil {
			return err
		}
		if n.Valid {
			if _, err := time.Parse("2006-01-02", n.String); err != nil {
				return fieldErr(path, "Use a date like 2026-11-10.")
			}
		}
		*e.text(path) = n
		if path == "warranty_end" {
			e.row.WarrantySource = sql.NullString{}
			if n.Valid {
				e.row.WarrantySource = nullStr("stated")
			}
		}
	case "currency":
		n, err := optText(path, v, 3)
		if err != nil {
			return err
		}
		n.String = strings.ToUpper(n.String)
		if n.Valid && (len(n.String) != 3 || strings.Trim(n.String, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "") {
			return fieldErr(path, "Use a three-letter currency code such as EUR.")
		}
		e.row.Currency = n
	case "quantity":
		n, ok := asInt(v)
		if !ok || n < 0 || n > 1_000_000 {
			return fieldErr(path, "Must be a whole number from 0 to 1,000,000.")
		}
		e.row.Quantity = n
	case "status":
		s, ok := v.(string)
		if !ok || !slices.Contains(Statuses, s) {
			return fieldErr(path, "Use one of: "+strings.Join(Statuses, ", ")+".")
		}
		e.row.Status = s
	case "draft":
		b, ok := v.(bool)
		if !ok {
			return fieldErr(path, "Must be true or false.")
		}
		e.row.Draft = boolInt(b)
	case "price_minor":
		if v == nil {
			e.row.PriceMinor = sql.NullInt64{}
			return nil
		}
		n, ok := asInt(v)
		if !ok || n < 0 {
			return fieldErr(path, "Must be a whole number of minor units (cents), zero or more.")
		}
		e.row.PriceMinor = sql.NullInt64{Int64: n, Valid: true}
	default:
		key, ok := strings.CutPrefix(path, "attributes.")
		if !ok {
			return badKey(path, fmt.Sprintf("Unknown field %q.", path))
		}
		return e.setAttr(key, v)
	}
	return nil
}

func (e *edit) setAttr(key string, v any) error {
	path := "attributes." + key
	if v == nil {
		delete(e.attrs, key)
		return nil
	}
	if e.free {
		if !attrKeyRe.MatchString(key) {
			return badKey(path, fmt.Sprintf("Attribute name %q must be lowercase letters, digits and underscores.", key))
		}
		switch v.(type) {
		case string, float64, bool:
		default:
			return fieldErr(path, "Free attributes must be text, a number or true/false.")
		}
		e.attrs[key] = v
		return nil
	}
	cat, ok := CategoryByKey(e.row.Category)
	if !ok {
		cat, _ = CategoryByKey(FallbackCategory)
	}
	attr, ok := cat.Attr(key)
	if !ok {
		return badKey(path, fmt.Sprintf("Unknown attribute %q for category %s.", key, cat.Key))
	}
	val, err := ValidateAttribute(attr, v)
	if err != nil {
		return fieldErr(path, "Attribute "+key+" "+err.Error()+".")
	}
	e.attrs[key] = val
	return nil
}

// empty reports whether the field has no value yet. Fields with a default
// (name, category, quantity) count as empty while the item is still a draft
// and holds the default.
func (e *edit) empty(path string) bool {
	switch path {
	case "name":
		return e.row.Name == "" || e.row.Name == NewItemName
	case "category":
		return e.row.Category == FallbackCategory && e.row.Draft == 1
	case "quantity":
		return e.row.Quantity == 1 && e.row.Draft == 1
	case "price_minor":
		return !e.row.PriceMinor.Valid
	}
	if key, ok := strings.CutPrefix(path, "attributes."); ok {
		_, has := e.attrs[key]
		return !has
	}
	if n := e.text(path); n != nil {
		return !n.Valid
	}
	return false
}

// aiFieldPaths are the fields an AI write may fill.
var aiFieldPaths = []string{"name", "category", "brand", "model", "serial", "color", "condition", "quantity", "notes",
	"purchase_date", "price_minor", "currency", "seller", "warranty_end"}

// applyFields applies the fields of an API request as a person: the touched
// fields leave ai_fields. It returns the paths touched.
func (e *edit) applyFields(ctx context.Context, q *sqlc.Queries, hid string, f Fields, allowed []string) error {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { // category first: attributes depend on it
		if (keys[i] == "category") != (keys[j] == "category") {
			return keys[i] == "category"
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		if !slices.Contains(allowed, k) {
			return badKey(k, fmt.Sprintf("Unknown field %q.", k))
		}
		var v any
		if err := json.Unmarshal(f[k], &v); err != nil {
			return fieldErr(k, "Not valid JSON.")
		}
		switch k {
		case "attributes":
			if v == nil {
				v = map[string]any{}
			}
			obj, ok := v.(map[string]any)
			if !ok {
				return fieldErr(k, "Must be an object.")
			}
			for _, ak := range sortedKeys(obj) {
				if err := e.set("attributes."+ak, obj[ak]); err != nil {
					return err
				}
				e.person("attributes." + ak)
			}
		case "purchase", "warranty":
			if err := e.applyGroup(k, v); err != nil {
				return err
			}
		case "place_id":
			if err := e.setPlace(ctx, q, hid, v); err != nil {
				return err
			}
		default:
			if err := e.set(k, v); err != nil {
				return err
			}
			e.person(k)
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (e *edit) applyGroup(group string, v any) error {
	if v == nil {
		v = map[string]any{}
		if group == "purchase" {
			v = map[string]any{"date": nil, "price_minor": nil, "currency": nil, "seller": nil}
		} else {
			v = map[string]any{"end": nil}
		}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return fieldErr(group, "Must be an object.")
	}
	paths := map[string]string{}
	if group == "purchase" {
		paths = map[string]string{"date": "purchase_date", "price_minor": "price_minor", "currency": "currency", "seller": "seller"}
	} else {
		paths = map[string]string{"end": "warranty_end"}
	}
	for _, k := range sortedKeys(obj) {
		path, ok := paths[k]
		if !ok {
			return badKey(group+"."+k, fmt.Sprintf("Unknown field %q.", group+"."+k))
		}
		if err := e.set(path, obj[k]); err != nil {
			return err
		}
		e.person(path)
	}
	return nil
}

func (e *edit) setPlace(ctx context.Context, q *sqlc.Queries, hid string, v any) error {
	if v == nil {
		if err := e.followPlace(ctx, q, hid, nil); err != nil {
			return err
		}
		e.row.PlaceID = sql.NullString{}
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fieldErr("place_id", "Must be a place ID or null.")
	}
	if s == e.row.PlaceID.String {
		return nil
	}
	p, err := q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: hid, ID: s})
	if err != nil && !isNoRows(err) {
		return err
	}
	if isNoRows(err) || p.DeletedAt.Valid {
		return fieldErr("place_id", "No such place.")
	}
	if err := e.followPlace(ctx, q, hid, &p); err != nil {
		return err
	}
	e.row.PlaceID = nullStr(s)
	return nil
}

// followPlace applies "status follows place" (spec section 3) for a move to
// the place `to` (nil = nowhere): into a box the item becomes packed; a packed
// item leaving a box for anything else becomes stored. Called before the
// row's place changes.
func (e *edit) followPlace(ctx context.Context, q *sqlc.Queries, hid string, to *sqlc.Place) error {
	if to != nil && to.Kind == "box" {
		e.row.Status = "packed"
		return nil
	}
	if e.row.Status == "packed" && e.row.PlaceID.Valid {
		from, err := q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: hid, ID: e.row.PlaceID.String})
		if err != nil && !isNoRows(err) {
			return err
		}
		if err == nil && from.Kind == "box" {
			e.row.Status = "stored"
		}
	}
	return nil
}

// save writes the edit's row. seen also sets last_seen_at.
func (e *edit) save(ctx context.Context, q *sqlc.Queries, hid, userID string, seen bool, byPerson bool) (sqlc.Item, error) {
	// price without currency: the household currency.
	if e.row.PriceMinor.Valid && !e.row.Currency.Valid {
		cur, err := q.GetHouseholdCurrency(ctx, hid)
		if err != nil {
			return e.row, err
		}
		e.row.Currency = nullStr(cur)
	}
	t := now()
	if seen {
		e.row.LastSeenAt = t
	}
	updatedBy := e.row.UpdatedBy
	if byPerson {
		updatedBy = nullStr(userID)
	}
	attrs, ai := e.marshal()
	r := e.row
	return q.UpdateItemRow(ctx, sqlc.UpdateItemRowParams{PlaceID: r.PlaceID, Name: r.Name, Category: r.Category,
		Brand: r.Brand, Model: r.Model, Serial: r.Serial, Color: r.Color, Quantity: r.Quantity, Condition: r.Condition,
		Notes: r.Notes, Status: r.Status, Attributes: attrs, AiFields: ai, Draft: r.Draft,
		PurchaseDate: r.PurchaseDate, PriceMinor: r.PriceMinor, Currency: r.Currency, Seller: r.Seller,
		WarrantyEnd: r.WarrantyEnd, WarrantySource: r.WarrantySource, LastSeenAt: r.LastSeenAt,
		UpdatedBy: updatedBy, UpdatedAt: t, HouseholdID: hid, ID: r.ID})
}

func getItemRow(ctx context.Context, q *sqlc.Queries, hid, itemID string) (sqlc.Item, error) {
	r, err := q.GetItemRow(ctx, sqlc.GetItemRowParams{HouseholdID: hid, ID: itemID})
	if isNoRows(err) {
		return r, httpapi.NotFound("That item does not exist.")
	}
	return r, err
}

// ---- writes (each takes a transaction) --------------------------------------

var (
	patchFields  = []string{"name", "category", "brand", "model", "serial", "color", "quantity", "condition", "notes", "status", "attributes", "place_id", "purchase", "warranty", "draft"}
	createFields = patchFields
)

// NewItem is what CreateItem needs. Fields are the API fields; the rest is
// for other features (capture, imports, agents).
type NewItem struct {
	ID       string // optional client ULID; an existing one makes the call a no-op
	Source   string // default "manual"
	ImportID string
	AIState  string // "queued" for a capture draft
	Fields   Fields

	fromLine string // set by PromoteLine: the contents line this item came from
}

// CreateItem adds an item. When n.ID already exists in the household the
// existing row is returned and created is false (the API answers 200 instead
// of 201): a retried request is idempotent.
func CreateItem(ctx context.Context, tx *sql.Tx, householdID, userID string, n NewItem) (item sqlc.Item, created bool, err error) {
	q := sqlc.New(tx)
	if n.ID != "" {
		if !idRe.MatchString(n.ID) {
			return item, false, fieldErr("id", "Must be a 26-character ULID.")
		}
		if row, err := q.GetItemRow(ctx, sqlc.GetItemRowParams{HouseholdID: householdID, ID: n.ID}); err == nil {
			return row, false, nil
		} else if !isNoRows(err) {
			return item, false, err
		}
	} else {
		n.ID = id.New()
	}
	source := n.Source
	if source == "" {
		source = "manual"
	}
	if !slices.Contains(Sources, source) {
		return item, false, fieldErr("source", "Use one of: "+strings.Join(Sources, ", ")+".")
	}
	t := now()
	e, _ := newEdit(sqlc.Item{ID: n.ID, Name: "", Category: FallbackCategory, Quantity: 1, Status: "stored",
		Attributes: "{}", AiFields: "[]", Source: source, LastSeenAt: t})
	if err := e.applyFields(ctx, q, householdID, n.Fields, createFields); err != nil {
		return item, false, err
	}
	if e.row.Name == "" {
		if e.row.Draft != 1 {
			return item, false, fieldErr("name", "The name is required.")
		}
		e.row.Name = NewItemName
	}
	if e.row.PriceMinor.Valid && !e.row.Currency.Valid {
		cur, err := q.GetHouseholdCurrency(ctx, householdID)
		if err != nil {
			return item, false, err
		}
		e.row.Currency = nullStr(cur)
	}
	attrs, ai := e.marshal()
	r := e.row
	row, err := q.CreateItem(ctx, sqlc.CreateItemParams{ID: n.ID, HouseholdID: householdID, CreatedAt: t, UpdatedAt: t,
		PlaceID: r.PlaceID, Name: r.Name, Category: r.Category, Brand: r.Brand, Model: r.Model, Serial: r.Serial, Color: r.Color,
		Quantity: r.Quantity, Condition: r.Condition, Notes: r.Notes, Status: r.Status, Attributes: attrs, AiFields: ai,
		Draft: r.Draft, Source: source, ImportID: nullStr(n.ImportID), PurchaseDate: r.PurchaseDate, PriceMinor: r.PriceMinor,
		Currency: r.Currency, Seller: r.Seller, WarrantyEnd: r.WarrantyEnd, WarrantySource: r.WarrantySource,
		LastSeenAt: t, CreatedBy: nullStr(userID), UpdatedBy: nullStr(userID)})
	if err != nil {
		return row, false, fmt.Errorf("create item: %w", err)
	}
	if n.AIState != "" {
		if err := q.SetItemAIMeta(ctx, sqlc.SetItemAIMetaParams{AiState: nullStr(n.AIState), AiDraft: row.AiDraft, UpdatedAt: t,
			HouseholdID: householdID, ID: row.ID}); err != nil {
			return row, false, err
		}
		row.AiState = nullStr(n.AIState)
	}
	after := map[string]any{"name": row.Name, "category": row.Category, "status": row.Status, "source": row.Source,
		"draft": row.Draft != 0, "place_id": ns(row.PlaceID), "place_path": placePathOf(ctx, q, householdID, row.PlaceID)}
	if n.fromLine != "" {
		after["from_line_id"] = n.fromLine
	}
	if err := RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "created", ItemID: row.ID,
		PlaceID: row.PlaceID.String, UserID: userID, After: after}); err != nil {
		return row, false, err
	}
	return row, true, openIfEmpty(ctx, tx, householdID, userID, row.PlaceID)
}

// UpdateItem is a person's partial edit: the fields in f change and leave
// ai_fields. Changing place_id counts as a move and sets last_seen_at.
func UpdateItem(ctx context.Context, tx *sql.Tx, householdID, userID, itemID string, f Fields) (sqlc.Item, error) {
	q := sqlc.New(tx)
	row, err := getItemRow(ctx, q, householdID, itemID)
	if err != nil {
		return row, err
	}
	e, _ := newEdit(row)
	if err := e.applyFields(ctx, q, householdID, f, patchFields); err != nil {
		return row, err
	}
	saved, err := e.save(ctx, q, householdID, userID, e.row.PlaceID != row.PlaceID, true)
	if err != nil {
		return saved, err
	}
	if err := recordItemDiff(ctx, tx, householdID, userID, row, saved, false); err != nil {
		return saved, err
	}
	return saved, boxReceived(ctx, tx, householdID, userID, row, saved)
}

// MoveItem puts an item in a place (nil = not found / not placed) and sets
// last_seen_at.
func MoveItem(ctx context.Context, tx *sql.Tx, householdID, userID, itemID string, placeID *string) (sqlc.Item, error) {
	q := sqlc.New(tx)
	row, err := getItemRow(ctx, q, householdID, itemID)
	if err != nil {
		return row, err
	}
	e, _ := newEdit(row)
	var v any
	if placeID != nil {
		v = *placeID
	}
	if err := e.setPlace(ctx, q, householdID, v); err != nil {
		return row, err
	}
	saved, err := e.save(ctx, q, householdID, userID, true, true)
	if err != nil {
		return saved, err
	}
	if err := recordItemDiff(ctx, tx, householdID, userID, row, saved, true); err != nil {
		return saved, err
	}
	return saved, boxReceived(ctx, tx, householdID, userID, row, saved)
}

// boxReceived opens an empty box an item was just put into.
func boxReceived(ctx context.Context, tx *sql.Tx, hid, userID string, before, after sqlc.Item) error {
	if before.PlaceID == after.PlaceID {
		return nil
	}
	return openIfEmpty(ctx, tx, hid, userID, after.PlaceID)
}

// MarkSeen is "I saw it here": it sets last_seen_at.
func MarkSeen(ctx context.Context, tx *sql.Tx, householdID, userID, itemID string) (sqlc.Item, error) {
	q := sqlc.New(tx)
	row, err := getItemRow(ctx, q, householdID, itemID)
	if err != nil {
		return row, err
	}
	e, _ := newEdit(row)
	saved, err := e.save(ctx, q, householdID, userID, true, true)
	if err != nil {
		return saved, err
	}
	return saved, recordItemDiff(ctx, tx, householdID, userID, row, saved, true)
}

// ConfirmItem accepts all remaining AI fields and clears draft.
func ConfirmItem(ctx context.Context, tx *sql.Tx, householdID, userID, itemID string) (sqlc.Item, error) {
	q := sqlc.New(tx)
	row, err := getItemRow(ctx, q, householdID, itemID)
	if err != nil {
		return row, err
	}
	e, _ := newEdit(row)
	e.ai, e.row.Draft = nil, 0
	saved, err := e.save(ctx, q, householdID, userID, false, true)
	if err != nil {
		return saved, err
	}
	return saved, RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "confirmed", ItemID: row.ID,
		PlaceID: row.PlaceID.String, UserID: userID,
		Before: map[string]any{"draft": row.Draft != 0}, After: map[string]any{"draft": false}})
}

// DeleteItem soft-deletes an item.
func DeleteItem(ctx context.Context, tx *sql.Tx, householdID, userID, itemID string) (sqlc.Item, error) {
	q := sqlc.New(tx)
	row, err := getItemRow(ctx, q, householdID, itemID)
	if err != nil || row.DeletedAt.Valid {
		return row, err
	}
	t := now()
	if err := q.SetItemDeleted(ctx, sqlc.SetItemDeletedParams{DeletedAt: nullStr(t), UpdatedBy: nullStr(userID), UpdatedAt: t,
		HouseholdID: householdID, ID: itemID}); err != nil {
		return row, err
	}
	if err := RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "deleted", ItemID: itemID,
		PlaceID: row.PlaceID.String, UserID: userID}); err != nil {
		return row, err
	}
	return getItemRow(ctx, q, householdID, itemID)
}

// RestoreItem undoes DeleteItem. If its place was deleted meanwhile, the item
// comes back unplaced.
func RestoreItem(ctx context.Context, tx *sql.Tx, householdID, userID, itemID string) (sqlc.Item, error) {
	q := sqlc.New(tx)
	row, err := getItemRow(ctx, q, householdID, itemID)
	if err != nil || !row.DeletedAt.Valid {
		return row, err
	}
	placeID := row.PlaceID
	if row.PlaceID.Valid {
		if p, err := q.GetPlace(ctx, sqlc.GetPlaceParams{HouseholdID: householdID, ID: row.PlaceID.String}); err != nil || p.DeletedAt.Valid {
			if err != nil && !isNoRows(err) {
				return row, err
			}
			if _, err := q.UpdateItemRow(ctx, itemRowParams(row, sql.NullString{}, userID)); err != nil {
				return row, err
			}
			placeID = sql.NullString{}
		}
	}
	if err := q.SetItemDeleted(ctx, sqlc.SetItemDeletedParams{DeletedAt: sql.NullString{}, UpdatedBy: nullStr(userID), UpdatedAt: now(),
		HouseholdID: householdID, ID: itemID}); err != nil {
		return row, err
	}
	if err := RecordEvent(ctx, tx, EventInput{HouseholdID: householdID, Kind: "restored", ItemID: itemID,
		PlaceID: placeID.String, UserID: userID}); err != nil {
		return row, err
	}
	return getItemRow(ctx, q, householdID, itemID)
}

func itemRowParams(r sqlc.Item, place sql.NullString, userID string) sqlc.UpdateItemRowParams {
	return sqlc.UpdateItemRowParams{PlaceID: place, Name: r.Name, Category: r.Category, Brand: r.Brand, Model: r.Model,
		Serial: r.Serial, Color: r.Color, Quantity: r.Quantity, Condition: r.Condition, Notes: r.Notes, Status: r.Status,
		Attributes: r.Attributes, AiFields: r.AiFields, Draft: r.Draft, PurchaseDate: r.PurchaseDate, PriceMinor: r.PriceMinor,
		Currency: r.Currency, Seller: r.Seller, WarrantyEnd: r.WarrantyEnd, WarrantySource: r.WarrantySource,
		LastSeenAt: r.LastSeenAt, UpdatedBy: nullStr(userID), UpdatedAt: now(), HouseholdID: r.HouseholdID, ID: r.ID}
}

// BulkChange is the change applied to every item of a bulk update.
type BulkChange struct {
	Place  Optional[string] `json:"place_id"` // null = not placed
	Status Optional[string] `json:"status"`
}

// BulkUpdate changes place and/or status of the items. All IDs must exist in
// the household or nothing changes (404).
func BulkUpdate(ctx context.Context, tx *sql.Tx, householdID, userID string, ids []string, ch BulkChange) ([]sqlc.Item, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, fieldErr("ids", "Give 1 to 500 item IDs.")
	}
	if !ch.Place.Set && !ch.Status.Set {
		return nil, fieldErr("place_id", "Give a place_id or a status to change.")
	}
	q := sqlc.New(tx)
	out := make([]sqlc.Item, 0, len(ids))
	seen := map[string]bool{}
	for _, itemID := range ids {
		if seen[itemID] {
			continue
		}
		seen[itemID] = true
		row, err := getItemRow(ctx, q, householdID, itemID)
		if err != nil {
			return nil, err
		}
		e, _ := newEdit(row)
		if ch.Place.Set {
			var v any
			if !ch.Place.Null {
				v = ch.Place.V
			}
			if err := e.setPlace(ctx, q, householdID, v); err != nil {
				return nil, err
			}
		}
		if ch.Status.Set {
			var v any
			if !ch.Status.Null {
				v = ch.Status.V
			}
			if err := e.set("status", v); err != nil {
				return nil, err
			}
			e.person("status")
		}
		saved, err := e.save(ctx, q, householdID, userID, e.row.PlaceID != row.PlaceID, true)
		if err != nil {
			return nil, err
		}
		if err := recordItemDiff(ctx, tx, householdID, userID, row, saved, false); err != nil {
			return nil, err
		}
		if err := boxReceived(ctx, tx, householdID, userID, row, saved); err != nil {
			return nil, err
		}
		out = append(out, saved)
	}
	return out, nil
}

// ---- the AI write path -------------------------------------------------------

// ApplyAI is how the AI jobs write to an item. fields maps field paths
// ("name", "brand", "purchase_date", "attributes.size", ...) to JSON-decoded
// values (nil values are ignored). It fills a field only when a person has not
// edited it: the field is empty, or it is still in ai_fields from an earlier
// AI write. Filled fields join ai_fields. Fields that fail validation (an
// unknown attribute, a bad enum value) are skipped, not errors, because AI
// output is untrusted. It returns the paths it wrote.
func ApplyAI(ctx context.Context, tx *sql.Tx, householdID, itemID string, fields map[string]any) (applied []string, err error) {
	q := sqlc.New(tx)
	row, err := getItemRow(ctx, q, householdID, itemID)
	if err != nil {
		return nil, err
	}
	e, _ := newEdit(row)
	e.strictCategory = true
	paths := make([]string, 0, len(fields))
	for p := range fields {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool { // category first
		if (paths[i] == "category") != (paths[j] == "category") {
			return paths[i] == "category"
		}
		return paths[i] < paths[j]
	})
	for _, p := range paths {
		v := fields[p]
		if v == nil || !(slices.Contains(aiFieldPaths, p) || strings.HasPrefix(p, "attributes.")) {
			continue
		}
		if !slices.Contains(e.ai, p) && !e.empty(p) {
			continue // a person's value
		}
		v = normalizeJSON(v)
		if err := e.set(p, v); err != nil {
			continue
		}
		e.byAI(p)
		applied = append(applied, p)
	}
	if len(applied) == 0 {
		return nil, nil
	}
	saved, err := e.save(ctx, q, householdID, "", false, false)
	if err != nil {
		return applied, err
	}
	if _, ok := ctx.Value(aiKey).(string); !ok {
		ctx = WithAI(ctx, "")
	}
	return applied, recordItemDiff(ctx, tx, householdID, "", row, saved, false)
}

// normalizeJSON turns Go values an AI job may hand over (ints, []string)
// into what encoding/json decoding would have produced.
func normalizeJSON(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return v
}

// SetAIMeta records the state and the last answer of an AI run on an item:
// ai_state ("" leaves NULL) and ai_draft (JSON, nil = NULL).
func SetAIMeta(ctx context.Context, tx *sql.Tx, householdID, itemID, state string, draftJSON []byte) error {
	q := sqlc.New(tx)
	if _, err := getItemRow(ctx, q, householdID, itemID); err != nil {
		return err
	}
	draft := sql.NullString{}
	if draftJSON != nil {
		draft = nullStr(string(draftJSON))
	}
	if err := q.SetItemAIMeta(ctx, sqlc.SetItemAIMetaParams{AiState: nullStr(state), AiDraft: draft, UpdatedAt: now(),
		HouseholdID: householdID, ID: itemID}); err != nil {
		return err
	}
	// Not a history event (it is the machine's bookkeeping), but the open
	// screens want to know.
	notify(tx, Notice{householdID, "item", itemID, "ai_state"})
	return nil
}
