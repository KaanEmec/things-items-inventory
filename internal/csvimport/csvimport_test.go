package csvimport

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/kaanemec/things-items-inventory/internal/ai/prompt/prompttest"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/export"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
	"github.com/kaanemec/things-items-inventory/internal/search"
)

const stamp = "2026-01-01T00:00:00.000Z"

type env struct {
	t    *testing.T
	d    *db.DB
	svc  *Service
	q    *jobs.Queue
	srv  *httptest.Server
	hid  string
	room string
}

func newEnv(t *testing.T) *env {
	d := dbtest.New(t)
	e := &env{t: t, d: d, hid: e2id()}
	search.New(d) // keeps the search index current
	e.house(e.hid)
	e.q = jobs.New(d, jobs.Options{})
	e.svc = New(d, ai.NewService(d, config.Config{}), e.q)
	e.srv = httpapitest.Server(t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "", HouseholdID: e.hid, Role: auth.RoleMember, Source: auth.SourceSession})))
			})
		})
	}, e.svc.Routes)
	e.room = e.place(e.hid, "Study")
	return e
}

func e2id() string { return id.New() }

func (e *env) house(hid string) {
	if _, err := sqlc.New(e.d.Write).CreateHousehold(context.Background(), sqlc.CreateHouseholdParams{
		ID: hid, Name: "H", Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) place(hid, name string) string {
	var pid string
	err := inventory.InTx(context.Background(), e.d, func(tx *sql.Tx) error {
		h, err := inventory.CreateHome(context.Background(), tx, hid, "Home "+name, "")
		if err != nil {
			return err
		}
		p, err := inventory.CreatePlace(context.Background(), tx, hid, inventory.PlaceInput{HomeID: h.ID, Kind: "room", Name: name})
		pid = p.ID
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return pid
}

func (e *env) post(path string, file []byte, mapping any) (int, []byte) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "x.csv")
	fw.Write(file)
	if mapping != nil {
		b, _ := json.Marshal(mapping)
		mw.WriteField("mapping", string(b))
	}
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

// runImport previews, takes the proposed mapping, imports and runs the job.
func (e *env) runImport(file []byte, edit func(*Mapping)) (previewResponse, Result) {
	e.t.Helper()
	st, b := e.post("/api/v1/imports/preview", file, nil)
	var pv previewResponse
	if st != 200 || json.Unmarshal(b, &pv) != nil {
		e.t.Fatalf("preview %d %s", st, b)
	}
	m := pv.Mapping
	if edit != nil {
		edit(&m)
	}
	st, b = e.post("/api/v1/imports", file, m)
	var start struct {
		ImportID string `json:"import_id"`
		JobID    string `json:"job_id"`
	}
	if st != 202 || json.Unmarshal(b, &start) != nil {
		e.t.Fatalf("start %d %s", st, b)
	}
	job, err := e.q.Get(context.Background(), e.hid, start.JobID)
	if err != nil {
		e.t.Fatal(err)
	}
	raw, err := e.svc.run(context.Background(), job)
	if err != nil {
		e.t.Fatal(err)
	}
	var res Result
	json.Unmarshal(raw, &res)
	return pv, res
}

func (e *env) items(hid string) []inventory.Item {
	its, _, err := inventory.ListItems(context.Background(), e.d.Read, hid, inventory.ItemFilter{})
	if err != nil {
		e.t.Fatal(err)
	}
	return its
}

func TestDecodeEncodingsAndDelimiters(t *testing.T) {
	if s, enc := decode([]byte("\xEF\xBB\xBFname;adet\nçay;2")); enc != "utf-8" || !strings.HasPrefix(s, "name") {
		t.Fatal(enc, s)
	}
	if s, enc := decode([]byte("\xFE\xFDi\xF0")); enc != "windows-1254" || s != "şıiğ" {
		t.Fatal(enc, s)
	}
	if s, enc := decode([]byte("caf\xE9 \x80")); enc != "windows-1252" || s != "café €" {
		t.Fatal(enc, s)
	}
	if d := sniffDelimiter("a;b;c\n1;2;3\n"); d != ';' {
		t.Fatal(string(d))
	}
	if d := sniffDelimiter("a\tb\n1\t2\n"); d != '\t' {
		t.Fatal(string(d))
	}
}

func TestOddSpreadsheetImportsAndUndoes(t *testing.T) {
	e := newEnv(t)
	csv := "Item;Marka;Adet;Fiyat;Date;Konum\n" +
		"Sony kulaklık;Sony;2;1.299,50;14.03.2024;Study\n" +
		";Boş;1;;;\n" +
		"Kettle;;1;19,99;nonsense;Cellar\n"
	pv, res := e.runImport([]byte(csv), func(m *Mapping) {
		if m.Columns[0].Target != "name" || m.Columns[1].Target != "brand" || m.Columns[2].Target != "quantity" || m.Columns[5].Target != "place" {
			t.Fatalf("%+v", m.Columns)
		}
		if !m.DecimalComma || m.DateFormat != "DD.MM.YYYY" {
			t.Fatalf("%+v", m)
		}
	})
	if pv.RowCount != 3 || pv.Delimiter != ";" || !pv.HasHeader || res.Added != 2 {
		t.Fatalf("%+v %+v", pv, res)
	}
	its := e.items(e.hid)
	var sony *inventory.Item
	for i := range its {
		if its[i].Name == "Sony kulaklık" {
			sony = &its[i]
		}
	}
	if sony == nil || !sony.Draft || sony.Source != "import" || sony.Quantity != 2 || *sony.Purchase.PriceMinor != 129950 ||
		*sony.Purchase.Date != "2024-03-14" || sony.Place == nil || sony.Place.ID != e.room || sony.Warranty.End == nil || *sony.Warranty.Source != "default" {
		t.Fatalf("%+v", sony)
	}
	// dry run: row 3 has no name; the kettle's bad date is a warning; Cellar is unmatched
	m := pv.Mapping
	st, b := e.post("/api/v1/imports/dry-run", []byte(csv), m)
	var dr dryRunResponse
	json.Unmarshal(b, &dr)
	if st != 200 || dr.WillAdd != 2 || len(dr.Skipped) != 1 || dr.Skipped[0].Row != 3 || len(dr.Warnings) != 1 || len(dr.Unmatched) != 1 || dr.Unmatched[0].Value != "Cellar" {
		t.Fatalf("%d %s", st, b)
	}
	// a dry run sees a saved item of the same brand and name as a duplicate
	inventory.InTx(context.Background(), e.d, func(tx *sql.Tx) error {
		_, _, err := inventory.CreateItem(context.Background(), tx, e.hid, "", inventory.NewItem{Fields: inventory.Fields{
			"name": []byte(`"Sony kulaklık"`), "brand": []byte(`"Sony"`)}})
		return err
	})
	_, b = e.post("/api/v1/imports/dry-run", []byte("Item;Marka\nSony kulaklık;Sony\n"), Mapping{HasHeader: true, Delimiter: ";",
		Columns: []Column{{Target: "name"}, {Target: "brand"}}})
	json.Unmarshal(b, &dr)
	if len(dr.Duplicates) != 1 || dr.Duplicates[0].Row != 2 || len(dr.Duplicates[0].Matches) == 0 {
		t.Fatalf("%s", b)
	}
	// undo removes exactly the import's items, not an item typed by hand
	var manual string
	e.d.Write.Exec(`DELETE FROM items WHERE name = 'Sony kulaklık' AND import_id IS NULL`)
	inventory.InTx(context.Background(), e.d, func(tx *sql.Tx) error {
		it, _, err := inventory.CreateItem(context.Background(), tx, e.hid, "", inventory.NewItem{Fields: inventory.Fields{"name": []byte(`"Mine"`)}})
		manual = it.ID
		return err
	})
	imp := ""
	for _, it := range e.items(e.hid) {
		if it.ImportID != nil {
			imp = *it.ImportID
		}
	}
	if st, _ := e.post("/api/v1/imports/"+imp+"/undo", nil, nil); st != 409 {
		t.Fatalf("undo while queued: %d", st)
	}
	e.d.Write.Exec(`UPDATE jobs SET state = 'done'`)
	if st, b := e.post("/api/v1/imports/"+imp+"/undo", nil, nil); st != 200 || !strings.Contains(string(b), `"removed":2`) {
		t.Fatalf("%d %s", st, b)
	}
	left := e.items(e.hid)
	if len(left) != 1 || left[0].ID != manual {
		t.Fatalf("%+v", left)
	}
}

func TestPlaceChoiceCreates(t *testing.T) {
	e := newEnv(t)
	csv := "name,place\nKettle,Cellar\n"
	e.runImport([]byte(csv), func(m *Mapping) { m.Places = map[string]string{"Cellar": "create"} })
	its := e.items(e.hid)
	if len(its) != 1 || its[0].Place == nil || its[0].Place.Name != "Cellar" {
		t.Fatalf("%+v", its)
	}
}

// J10.2's round trip: items.csv of an export imports back with the proposed
// mapping and no corrections.
func TestExportRoundTrip(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	err := inventory.InTx(ctx, e.d, func(tx *sql.Tx) error {
		for _, f := range []inventory.Fields{
			{"name": []byte(`"Sony headphones"`), "category": []byte(`"gadget"`), "brand": []byte(`"Sony"`), "model": []byte(`"WH-1000XM4"`),
				"serial": []byte(`"S1,2"`), "quantity": []byte(`2`), "condition": []byte(`"good"`), "notes": []byte(`"line one\nline \"two\""`),
				"place_id": []byte(`"` + e.room + `"`), "purchase": []byte(`{"date":"2024-03-14","price_minor":27950,"currency":"EUR","seller":"MediaMarkt"}`)},
			{"name": []byte(`"Kettle"`), "warranty": []byte(`{"end":"2027-01-01"}`)},
		} {
			if _, _, err := inventory.CreateItem(ctx, tx, e.hid, "", inventory.NewItem{Fields: f}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var zbuf bytes.Buffer
	if err := export.New(e.d, config.Config{DataDir: t.TempDir()}).Write(ctx, &zbuf, e.hid); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zbuf.Bytes()), int64(zbuf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var csv []byte
	for _, f := range zr.File {
		if f.Name == "items.csv" {
			rc, _ := f.Open()
			csv, _ = io.ReadAll(rc)
			rc.Close()
		}
	}
	// import into a second household that has a place of the same name
	other := id.New()
	e.house(other)
	otherRoom := e.place(other, "Study")
	e.hid = other // the test server acts for the other household now
	pv, res := e.runImport(csv, nil)
	want := []string{"name", "category", "brand", "model", "serial", "color", "quantity", "condition", "status", "place", "notes",
		"purchase_date", "price", "currency", "seller", "warranty_end", "ignore", "ignore"}
	for i, c := range pv.Mapping.Columns {
		if c.Target != want[i] {
			t.Fatalf("column %d (%s) mapped to %s, want %s", i, pv.Header[i], c.Target, want[i])
		}
	}
	if res.Added != 2 || res.Failed != 0 {
		t.Fatalf("%+v", res)
	}
	byName := map[string]inventory.Item{}
	for _, it := range e.items(other) {
		byName[it.Name] = it
	}
	s := byName["Sony headphones"]
	if str(s.Brand) != "Sony" || str(s.Serial) != "S1,2" || s.Quantity != 2 || str(s.Condition) != "good" || s.Category != "gadget" ||
		!strings.Contains(str(s.Notes), `line "two"`) || s.Place == nil || s.Place.ID != otherRoom ||
		str(s.Purchase.Date) != "2024-03-14" || *s.Purchase.PriceMinor != 27950 || str(s.Purchase.Seller) != "MediaMarkt" {
		t.Fatalf("%+v", s)
	}
	if k := byName["Kettle"]; str(k.Warranty.End) != "2027-01-01" || str(k.Warranty.Source) != "stated" {
		t.Fatalf("%+v", k.Warranty)
	}
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestAIMappingProposal(t *testing.T) {
	e := newEnv(t)
	ep := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		out, _ := json.Marshal(map[string]any{"model": "m", "choices": []any{map[string]any{"message": map[string]any{"content": string(prompttest.Answer("csv_mapping", "tr_sheet"))}}}})
		w.Write(out)
	}))
	defer ep.Close()
	cfg := config.Config{}
	cfg.AI.Text = config.AIEndpoint{BaseURL: ep.URL, Token: "t", Model: "m"}
	e.svc.ai = ai.NewService(e.d, cfg)
	file := "Ürün adı,Beden,Kumaş,Renk,Nerede,Fiyat\nKışlık mont,M,%100 polyester,Siyah,Gardırop,\"1.299,90\"\n"
	st, b := e.post("/api/v1/imports/preview", []byte(file), nil)
	var pv previewResponse
	json.Unmarshal(b, &pv)
	if st != 200 || !pv.AI || pv.Mapping.Columns[0].Target != "name" || pv.Mapping.Columns[1].Target != "attribute" || pv.Mapping.Columns[1].Attribute != "size" {
		t.Fatalf("%d %s", st, b)
	}
}
