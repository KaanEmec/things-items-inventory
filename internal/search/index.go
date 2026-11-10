package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kaanemec/things-items-inventory/internal/db"
)

// Kinds of index rows.
const (
	KindItem  = "item"
	KindLine  = "line"
	KindPlace = "place"
	KindBox   = "box"
)

// Querier is what both *sql.Tx and *sql.DB offer.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// doc is one row of the index before it is written.
type doc struct {
	kind, ref, touched string
	display, detail    string
	path               string
	nameFold, keyFold  string
	brandFold          string
	box                sql.NullInt64
	title, body        string // folded, for the trigram table
}

// upsert writes doc into search_docs and search_fts.
func upsert(ctx context.Context, tx Querier, hid string, d doc) error {
	var rowid int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM search_docs WHERE household_id = ? AND kind = ? AND ref_id = ?`,
		hid, d.kind, d.ref).Scan(&rowid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `INSERT INTO search_docs
			(household_id, kind, ref_id, touched, display, detail, path, name_fold, key_fold, brand_fold, box_number)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			hid, d.kind, d.ref, d.touched, d.display, d.detail, d.path, d.nameFold, d.keyFold, d.brandFold, d.box)
		if err != nil {
			return err
		}
		if rowid, err = res.LastInsertId(); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE search_docs SET touched = ?, display = ?, detail = ?, path = ?,
			name_fold = ?, key_fold = ?, brand_fold = ?, box_number = ? WHERE household_id = ? AND id = ?`,
			d.touched, d.display, d.detail, d.path, d.nameFold, d.keyFold, d.brandFold, d.box, hid, rowid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_fts WHERE rowid = ?`, rowid); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO search_fts (rowid, household_id, kind, ref_id, title, body) VALUES (?, ?, ?, ?, ?, ?)`,
		rowid, hid, d.kind, d.ref, d.title, d.body)
	return err
}

func remove(ctx context.Context, tx Querier, hid, kind, ref string) error {
	var rowid int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM search_docs WHERE household_id = ? AND kind = ? AND ref_id = ?`,
		hid, kind, ref).Scan(&rowid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_fts WHERE rowid = ?`, rowid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM search_docs WHERE household_id = ? AND id = ?`, hid, rowid)
	return err
}

// RemoveItem takes an item out of the index.
func RemoveItem(ctx context.Context, tx *sql.Tx, householdID, itemID string) error {
	return remove(ctx, tx, householdID, KindItem, itemID)
}

// RemoveLine takes a contents line out of the index.
func RemoveLine(ctx context.Context, tx *sql.Tx, householdID, lineID string) error {
	return remove(ctx, tx, householdID, KindLine, lineID)
}

// RemovePlace takes a place or box out of the index.
func RemovePlace(ctx context.Context, tx *sql.Tx, householdID, placeID string) error {
	return errors.Join(
		remove(ctx, tx, householdID, KindPlace, placeID),
		remove(ctx, tx, householdID, KindBox, placeID))
}

// ---- items -----------------------------------------------------------------

const itemSelect = `SELECT i.id, i.name, COALESCE(i.brand, ''), COALESCE(i.model, ''), COALESCE(i.serial, ''),
	COALESCE(i.color, ''), i.category, COALESCE(i.notes, ''), i.attributes, i.updated_at, COALESCE(p.path, '')
FROM items i
LEFT JOIN places p ON p.household_id = i.household_id AND p.id = i.place_id
LEFT JOIN homes h ON h.household_id = p.household_id AND h.id = p.home_id
WHERE i.household_id = ? AND i.deleted_at IS NULL AND h.archived_at IS NULL `

// indexItems indexes the live items of the household that match the extra
// WHERE clause (which starts with AND) and returns their IDs.
func indexItems(ctx context.Context, tx Querier, hid, extra string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, itemSelect+extra, append([]any{hid}, args...)...)
	if err != nil {
		return nil, err
	}
	var docs []doc
	for rows.Next() {
		var id, name, brand, model, serial, color, category, notes, attrs, updated, path string
		if err := rows.Scan(&id, &name, &brand, &model, &serial, &color, &category, &notes, &attrs, &updated, &path); err != nil {
			rows.Close()
			return nil, err
		}
		av := attributeText(attrs)
		docs = append(docs, doc{
			kind: KindItem, ref: id, touched: updated,
			display: name, detail: joinNonEmpty(" · ", brand, model, serial, notes), path: path,
			nameFold: Fold(name), keyFold: Fold(model), brandFold: Fold(brand),
			title: Fold(name),
			body:  Fold(joinNonEmpty(" ", brand, model, serial, color, category, notes, av, path)),
		})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ref
		if err := upsert(ctx, tx, hid, d); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// attributeText flattens the values of the attributes JSON object, in key
// order, into one string.
func attributeText(raw string) string {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			parts = append(parts, x)
		case float64:
			parts = append(parts, strconv.FormatFloat(x, 'f', -1, 64))
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	for _, k := range keys {
		walk(m[k])
	}
	return strings.Join(parts, " ")
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// IndexItem (re)indexes one item, or removes it when it is gone, soft
// deleted or in an archived home. Call it inside the write transaction.
func IndexItem(ctx context.Context, tx *sql.Tx, householdID, itemID string) error {
	ids, err := indexItems(ctx, tx, householdID, `AND i.id = ?`, itemID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return RemoveItem(ctx, tx, householdID, itemID)
	}
	return nil
}

// ---- lines -----------------------------------------------------------------

const lineSelect = `SELECT l.id, l.name, COALESCE(l.category, ''), l.updated_at, p.path, p.kind, p.box_number
FROM contents_lines l
JOIN places p ON p.household_id = l.household_id AND p.id = l.place_id
LEFT JOIN homes h ON h.household_id = p.household_id AND h.id = p.home_id
WHERE l.household_id = ? AND l.deleted_at IS NULL AND p.deleted_at IS NULL AND h.archived_at IS NULL `

func indexLines(ctx context.Context, tx Querier, hid, extra string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, lineSelect+extra, append([]any{hid}, args...)...)
	if err != nil {
		return nil, err
	}
	var docs []doc
	for rows.Next() {
		var id, name, category, updated, path, pkind string
		var box sql.NullInt64
		if err := rows.Scan(&id, &name, &category, &updated, &path, &pkind, &box); err != nil {
			rows.Close()
			return nil, err
		}
		if pkind == "box" && box.Valid {
			path = joinNonEmpty(" · ", "Box "+strconv.FormatInt(box.Int64, 10), path)
		}
		docs = append(docs, doc{
			kind: KindLine, ref: id, touched: updated,
			display: name, detail: category, path: path,
			nameFold: Fold(name), title: Fold(name),
			body: Fold(joinNonEmpty(" ", category, path)),
		})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ref
		if err := upsert(ctx, tx, hid, d); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// IndexLine (re)indexes one contents line, or removes it when it is gone,
// soft deleted, or its place is deleted or in an archived home.
func IndexLine(ctx context.Context, tx *sql.Tx, householdID, lineID string) error {
	ids, err := indexLines(ctx, tx, householdID, `AND l.id = ?`, lineID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return RemoveLine(ctx, tx, householdID, lineID)
	}
	return nil
}

// ---- places ----------------------------------------------------------------

const placeSelect = `SELECT p.id, p.kind, p.name, p.path, COALESCE(p.note, ''), p.updated_at, p.box_number
FROM places p
LEFT JOIN homes h ON h.household_id = p.household_id AND h.id = p.home_id
WHERE p.household_id = ? AND p.deleted_at IS NULL AND h.archived_at IS NULL `

func indexPlaces(ctx context.Context, tx Querier, hid, extra string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, placeSelect+extra, append([]any{hid}, args...)...)
	if err != nil {
		return nil, err
	}
	type row struct {
		doc
		kindChange string
	}
	var docs []doc
	for rows.Next() {
		var id, pkind, name, path, note, updated string
		var box sql.NullInt64
		if err := rows.Scan(&id, &pkind, &name, &path, &note, &updated, &box); err != nil {
			rows.Close()
			return nil, err
		}
		d := doc{kind: KindPlace, ref: id, touched: updated, display: name, path: path, box: box,
			nameFold: Fold(name), title: Fold(name), body: Fold(joinNonEmpty(" ", path, note)), detail: note}
		if pkind == "box" {
			d.kind = KindBox
			label := "Box"
			if box.Valid {
				label += " " + strconv.FormatInt(box.Int64, 10)
			}
			d.display = label
			if name != "" && Fold(name) != Fold(label) {
				d.display = label + " · " + name
			}
			d.nameFold = Fold(d.display)
			d.title = d.nameFold
		}
		docs = append(docs, d)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ref
		other := KindBox
		if d.kind == KindBox {
			other = KindPlace
		}
		if err := remove(ctx, tx, hid, other, d.ref); err != nil {
			return nil, err
		}
		if err := upsert(ctx, tx, hid, d); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// IndexPlace (re)indexes one place or box, or removes it when it is gone,
// soft deleted or in an archived home. It does not touch what is inside it;
// see IndexPlaceTree for a rename or move.
func IndexPlace(ctx context.Context, tx *sql.Tx, householdID, placeID string) error {
	ids, err := indexPlaces(ctx, tx, householdID, `AND p.id = ?`, placeID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return RemovePlace(ctx, tx, householdID, placeID)
	}
	return nil
}

// IndexPlaceTree re-indexes a place, every place below it, and the items and
// contents lines in all of them: their text includes the place path, so a
// rename or move changes it.
func IndexPlaceTree(ctx context.Context, tx *sql.Tx, householdID, placeID string) error {
	var lineage string
	err := tx.QueryRowContext(ctx, `SELECT lineage FROM places WHERE household_id = ? AND id = ?`, householdID, placeID).Scan(&lineage)
	if errors.Is(err, sql.ErrNoRows) {
		return RemovePlace(ctx, tx, householdID, placeID)
	}
	if err != nil {
		return err
	}
	lo, hi := lineage, strings.TrimSuffix(lineage, "/")+"0"
	sub := ` AND p.lineage >= ? AND p.lineage < ?`
	// Places that left the index (deleted, archived) are removed first.
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_fts WHERE rowid IN (
		SELECT d.id FROM search_docs d JOIN places p ON p.household_id = d.household_id AND p.id = d.ref_id
		WHERE d.household_id = ? AND d.kind IN ('place', 'box') AND p.lineage >= ? AND p.lineage < ?)`,
		householdID, lo, hi); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_docs WHERE household_id = ? AND kind IN ('place', 'box') AND ref_id IN (
		SELECT id FROM places WHERE household_id = ? AND lineage >= ? AND lineage < ?)`,
		householdID, householdID, lo, hi); err != nil {
		return err
	}
	// Items and lines in the subtree are dropped and re-added, so deleted ones stay out.
	for _, q := range []string{
		`DELETE FROM search_fts WHERE rowid IN (SELECT d.id FROM search_docs d WHERE d.household_id = ? AND d.kind = 'item' AND d.ref_id IN (
			SELECT i.id FROM items i JOIN places p ON p.household_id = i.household_id AND p.id = i.place_id
			WHERE i.household_id = ? AND p.lineage >= ? AND p.lineage < ?))`,
		`DELETE FROM search_docs WHERE household_id = ? AND kind = 'item' AND ref_id IN (
			SELECT i.id FROM items i JOIN places p ON p.household_id = i.household_id AND p.id = i.place_id
			WHERE i.household_id = ? AND p.lineage >= ? AND p.lineage < ?)`,
		`DELETE FROM search_fts WHERE rowid IN (SELECT d.id FROM search_docs d WHERE d.household_id = ? AND d.kind = 'line' AND d.ref_id IN (
			SELECT l.id FROM contents_lines l JOIN places p ON p.household_id = l.household_id AND p.id = l.place_id
			WHERE l.household_id = ? AND p.lineage >= ? AND p.lineage < ?))`,
		`DELETE FROM search_docs WHERE household_id = ? AND kind = 'line' AND ref_id IN (
			SELECT l.id FROM contents_lines l JOIN places p ON p.household_id = l.household_id AND p.id = l.place_id
			WHERE l.household_id = ? AND p.lineage >= ? AND p.lineage < ?)`,
	} {
		if _, err := tx.ExecContext(ctx, q, householdID, householdID, lo, hi); err != nil {
			return err
		}
	}
	if _, err := indexPlaces(ctx, tx, householdID, sub, lo, hi); err != nil {
		return err
	}
	if _, err := indexItems(ctx, tx, householdID, `AND i.place_id IN (SELECT p.id FROM places p WHERE p.household_id = i.household_id`+sub+`)`, lo, hi); err != nil {
		return err
	}
	_, err = indexLines(ctx, tx, householdID, `AND l.place_id IN (SELECT p2.id FROM places p2 WHERE p2.household_id = l.household_id AND p2.lineage >= ? AND p2.lineage < ?)`, lo, hi)
	return err
}

// ---- rebuild ---------------------------------------------------------------

// Rebuild drops and recreates the index of one household, or of all of them
// when householdID is empty, from the real tables, in one transaction. It
// returns how many rows the index holds afterwards for that scope.
func Rebuild(ctx context.Context, d *db.DB, householdID string) (int, error) {
	var n int
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		n, err = RebuildTx(ctx, tx, householdID)
		return err
	})
	return n, err
}

// RebuildTx is Rebuild inside the caller's transaction.
func RebuildTx(ctx context.Context, tx *sql.Tx, householdID string) (int, error) {
	var households []string
	if householdID == "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_fts`); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_docs`); err != nil {
			return 0, err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM households`)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return 0, err
			}
			households = append(households, id)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_fts WHERE rowid IN (SELECT id FROM search_docs WHERE household_id = ?)`, householdID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM search_docs WHERE household_id = ?`, householdID); err != nil {
			return 0, err
		}
		households = []string{householdID}
	}
	total := 0
	for _, hid := range households {
		for _, f := range []func(context.Context, Querier, string, string, ...any) ([]string, error){indexPlaces, indexItems, indexLines} {
			ids, err := f(ctx, tx, hid, "")
			if err != nil {
				return 0, fmt.Errorf("rebuild search index: %w", err)
			}
			total += len(ids)
		}
	}
	return total, nil
}

// IndexHome brings the index in step after a home was archived or restored:
// everything in an archived home leaves the index, and returns with it.
// Homes are few, so it simply rebuilds the household.
func IndexHome(ctx context.Context, tx *sql.Tx, householdID string) error {
	_, err := RebuildTx(ctx, tx, householdID)
	return err
}
