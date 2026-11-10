// Package export streams everything a household owns as one zip (J10.2):
// CSV files, inventory.json and the original photos. It reads only
// household-scoped queries (export.sql) and never touches AI profiles,
// password hashes, sessions, access tokens or invites.
package export

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// Service is the export feature.
type Service struct {
	d    *db.DB
	busy sync.Map // household id -> running export
	dir  string   // data directory; originals live in <dir>/photos/<aa>/<sha>.<ext>
}

// New returns the export service.
func New(d *db.DB, cfg config.Config) *Service { return &Service{d: d, dir: cfg.DataDir} }

// Routes registers GET /api/v1/export (owner and admin, session only).
func (s *Service) Routes(mux *httpapi.Mux) {
	mux.HandleFunc("GET /api/v1/export", auth.RequireSession(auth.ActionExport, s.handle))
}

func (s *Service) handle(w http.ResponseWriter, r *http.Request) {
	hid := auth.HouseholdID(r.Context())
	if _, running := s.busy.LoadOrStore(hid, true); running {
		httpapi.WriteError(w, httpapi.Conflict("An export is already running for this household. Try again in a few minutes."))
		return
	}
	defer s.busy.Delete(hid)
	name := "inventory-export-" + time.Now().UTC().Format("2006-01-02") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := s.Write(r.Context(), w, hid); err != nil {
		// Headers are sent; the truncated zip is the signal.
		slog.Error("export", "err", err)
	}
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Write writes the household's zip to w, one entry at a time. Photos are
// copied from disk, never held in memory.
func (s *Service) Write(ctx context.Context, w io.Writer, hid string) error {
	q := sqlc.New(s.d.Read)
	homes, err := q.ExportHomes(ctx, hid)
	if err != nil {
		return err
	}
	places, err := q.ExportPlaces(ctx, hid)
	if err != nil {
		return err
	}
	items, err := q.ExportItems(ctx, hid)
	if err != nil {
		return err
	}
	lines, err := q.ExportLines(ctx, hid)
	if err != nil {
		return err
	}
	photos, err := q.ExportPhotos(ctx, hid)
	if err != nil {
		return err
	}
	links, err := q.ExportPhotoLinks(ctx, hid)
	if err != nil {
		return err
	}
	members, err := q.ExportMembers(ctx, hid)
	if err != nil {
		return err
	}
	hh, err := q.GetHousehold(ctx, hid)
	if err != nil {
		return err
	}

	photoFile := map[string]string{} // photo id -> zip path
	for _, p := range photos {
		photoFile[p.ID] = "photos/" + p.ID + "." + p.Ext
	}
	placePath := map[string]string{}
	for _, p := range places {
		placePath[p.ID] = p.Path
	}
	itemPhotos, placePhotos := map[string][]string{}, map[string][]string{}
	var linkRows []map[string]any
	for _, l := range links {
		f, ok := photoFile[l.PhotoID]
		if !ok {
			continue
		}
		if l.ItemID.Valid {
			itemPhotos[l.ItemID.String] = append(itemPhotos[l.ItemID.String], f)
		} else if l.PlaceID.Valid {
			placePhotos[l.PlaceID.String] = append(placePhotos[l.PlaceID.String], f)
		}
		m := flat(l)
		m["file"] = f
		linkRows = append(linkRows, m)
	}

	var itemRows, placeRows, boxRows, lineRows, photoRows, homeRows []map[string]any
	for _, h := range homes {
		homeRows = append(homeRows, flat(h))
	}
	for _, it := range items {
		m := flat(it)
		delete(m, "ai_draft")
		m["place"] = placePath[it.PlaceID.String]
		m["photos"] = strings.Join(itemPhotos[it.ID], " ")
		if it.PriceMinor.Valid {
			m["price"] = fmt.Sprintf("%d.%02d", it.PriceMinor.Int64/100, it.PriceMinor.Int64%100)
		}
		itemRows = append(itemRows, m)
	}
	for _, p := range places {
		m := flat(p)
		m["goes_to"] = placePath[p.GoesToID.String]
		m["photos"] = strings.Join(placePhotos[p.ID], " ")
		if p.Kind == "box" {
			boxRows = append(boxRows, m)
		} else {
			placeRows = append(placeRows, m)
		}
	}
	for _, l := range lines {
		m := flat(l)
		m["place"] = placePath[l.PlaceID]
		lineRows = append(lineRows, m)
	}
	for _, p := range photos {
		m := flat(p)
		m["file"] = photoFile[p.ID]
		photoRows = append(photoRows, m)
	}
	memberRows := make([]map[string]any, 0, len(members))
	for _, m := range members {
		memberRows = append(memberRows, flat(m))
	}

	zw := zip.NewWriter(w)
	csvFile := func(name string, cols []string, rows []map[string]any) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		cw := csv.NewWriter(f)
		if err := cw.Write(cols); err != nil {
			return err
		}
		for _, r := range rows {
			rec := make([]string, len(cols))
			for i, c := range cols {
				rec[i] = cell(r[c])
			}
			if err := cw.Write(rec); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()
	}
	if err := csvFile("items.csv", []string{"name", "category", "brand", "model", "serial", "color", "quantity", "condition",
		"status", "place", "notes", "purchase_date", "price", "currency", "seller", "warranty_end", "photos", "id"}, itemRows); err != nil {
		return err
	}
	if err := csvFile("places.csv", []string{"name", "kind", "path", "color", "daily_use", "note", "photos", "id"}, placeRows); err != nil {
		return err
	}
	if err := csvFile("boxes.csv", []string{"box_number", "name", "path", "box_status", "goes_to", "color", "note", "handling", "photos", "id"}, boxRows); err != nil {
		return err
	}
	if err := csvFile("contents.csv", []string{"place", "name", "quantity", "category", "worth_item", "item_id", "id"}, lineRows); err != nil {
		return err
	}
	histCols := []string{"at", "kind", "actor_kind", "token_name", "item_id", "place_id", "data", "id"}
	hf, err := zw.Create("history.csv")
	if err != nil {
		return err
	}
	hw := csv.NewWriter(hf)
	if err := hw.Write(histCols); err != nil {
		return err
	}
	if err := s.eachEvent(ctx, hid, func(m map[string]any) error {
		rec := make([]string, len(histCols))
		for i, c := range histCols {
			rec[i] = cell(m[c])
		}
		return hw.Write(rec)
	}); err != nil {
		return err
	}
	hw.Flush()
	if err := hw.Error(); err != nil {
		return err
	}
	jf, err := zw.Create("inventory.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(jf)
	all := []struct {
		key string
		val any
	}{
		{"exported_at", time.Now().UTC().Format(time.RFC3339)},
		{"household", map[string]any{"name": hh.Name, "language": hh.Language, "currency": hh.Currency,
			"default_warranty_months": hh.DefaultWarrantyMonths}},
		{"members", memberRows}, {"homes", homeRows}, {"places", placeRows}, {"boxes", boxRows}, {"items", itemRows},
		{"contents_lines", lineRows}, {"photos", photoRows}, {"photo_links", linkRows},
	}
	io.WriteString(jf, "{\n")
	for _, e := range all {
		fmt.Fprintf(jf, "%q: ", e.key)
		if err := enc.Encode(e.val); err != nil {
			return err
		}
		io.WriteString(jf, ",\n")
	}
	io.WriteString(jf, `"history": [`)
	first := true
	if err := s.eachEvent(ctx, hid, func(m map[string]any) error {
		if !first {
			io.WriteString(jf, ",")
		}
		first = false
		return enc.Encode(m)
	}); err != nil {
		return err
	}
	io.WriteString(jf, "]\n}\n")
	if err := textFile(zw, "README.txt", readme); err != nil {
		return err
	}
	for _, p := range photos {
		if !shaRe.MatchString(p.Sha256) {
			continue
		}
		src, err := os.Open(filepath.Join(s.dir, "photos", p.Sha256[:2], p.Sha256+"."+p.Ext))
		if err != nil {
			slog.Warn("export: photo file missing", "photo", p.ID, "err", err)
			continue
		}
		// Photos are already compressed: store them.
		f, err := zw.CreateHeader(&zip.FileHeader{Name: photoFile[p.ID], Method: zip.Store, Modified: time.Now()})
		if err == nil {
			_, err = io.Copy(f, src)
		}
		src.Close()
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return zw.Close()
}

func textFile(zw *zip.Writer, name, body string) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, body)
	return err
}

const readme = `Household inventory export

items.csv       Every item, one row per item. "place" is the full place name; "photos" lists files in photos/.
places.csv      Rooms, furniture and spots.
boxes.csv       Moving boxes with number, status and destination.
contents.csv    Contents lines (cheap or bulky things logged per place or box).
history.csv     Every change: when, what, who (AI or person), with details as JSON.
inventory.json  All of the above in one file, plus homes, photo links, category
                attributes and the household settings.
photos/         Original photo files, named <photo id>.<ext>.

Left out on purpose: AI tokens, password hashes, sessions, access tokens and invite links.
`

// safeCell defuses spreadsheet formulas: a text starting with = + - @ tab or
// CR gets a leading apostrophe.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// cell formats one value for CSV.
func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return safeCell(x)
	case json.RawMessage:
		return safeCell(string(x))
	default:
		return fmt.Sprint(x)
	}
}

var nullTypes = map[reflect.Type]string{
	reflect.TypeOf(sql.NullString{}): "String", reflect.TypeOf(sql.NullInt64{}): "Int64",
	reflect.TypeOf(sql.NullFloat64{}): "Float64", reflect.TypeOf(sql.NullBool{}): "Bool",
}

// jsonColumns hold JSON text and are embedded as JSON, not as strings.
var jsonColumns = map[string]bool{"attributes": true, "ai_fields": true, "handling": true, "data": true}

// flat turns an sqlc row struct into a snake_case map: sql.Null* become
// value or nil, JSON columns become raw JSON, household_id is dropped.
func flat(row any) map[string]any {
	v := reflect.ValueOf(row)
	out := map[string]any{}
	for i := 0; i < v.NumField(); i++ {
		key := snake(v.Type().Field(i).Name)
		if key == "household_id" {
			continue
		}
		f := v.Field(i)
		if field, ok := nullTypes[f.Type()]; ok {
			if !f.FieldByName("Valid").Bool() {
				out[key] = nil
				continue
			}
			f = f.FieldByName(field)
		}
		val := f.Interface()
		if s, ok := val.(string); ok && jsonColumns[key] && json.Valid([]byte(s)) {
			val = json.RawMessage(s)
		}
		out[key] = val
	}
	return out
}

func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return strings.Replace(b.String(), "sha_256", "sha256", 1)
}

// Seed creates one item with a unique name for the cross-household test.
func (s *Service) Seed(ctx context.Context, d *db.DB, hid string) ([]string, error) {
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		_, _, err := inventory.CreateItem(ctx, tx, hid, "", inventory.NewItem{Fields: inventory.Fields{
			"name": json.RawMessage(`"Secret-ExportItem-7Q"`)}})
		return err
	})
	return []string{"Secret-ExportItem-7Q"}, err
}

// eachEvent reads the household's events one row at a time, oldest first.
func (s *Service) eachEvent(ctx context.Context, hid string, fn func(map[string]any) error) error {
	rows, err := s.d.Read.QueryContext(ctx, `SELECT id, household_id, at, actor_kind, user_id, token_id, token_name, kind, item_id, place_id, data
FROM events WHERE household_id = ? ORDER BY at, id`, hid)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e sqlc.Event
		if err := rows.Scan(&e.ID, &e.HouseholdID, &e.At, &e.ActorKind, &e.UserID, &e.TokenID, &e.TokenName, &e.Kind, &e.ItemID, &e.PlaceID, &e.Data); err != nil {
			return err
		}
		m := flat(e)
		delete(m, "token_id")
		if err := fn(m); err != nil {
			return err
		}
	}
	return rows.Err()
}
