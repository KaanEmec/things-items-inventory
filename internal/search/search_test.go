package search_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/search"
)

var ctx = context.Background()

const stamp = "2026-01-01T00:00:00.000Z"

type env struct {
	t    *testing.T
	d    *db.DB
	hid  string
	home string
}

func newHousehold(t *testing.T, d *db.DB) string {
	t.Helper()
	hid := id.New()
	if _, err := sqlc.New(d.Write).CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: hid, Name: "H", Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	return hid
}

func newEnv(t *testing.T) *env {
	t.Helper()
	search.Subscribe()
	d := dbtest.New(t)
	e := &env{t: t, d: d, hid: newHousehold(t, d)}
	e.tx(func(tx *sql.Tx) error {
		h, err := inventory.CreateHome(ctx, tx, e.hid, "Flat", "")
		e.home = h.ID
		return err
	})
	return e
}

func (e *env) tx(fn func(tx *sql.Tx) error) {
	e.t.Helper()
	if err := e.d.Tx(ctx, fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) place(parent, kind, name string) string {
	e.t.Helper()
	var pid string
	e.tx(func(tx *sql.Tx) error {
		in := inventory.PlaceInput{Kind: kind, Name: name, ParentID: parent}
		if parent == "" && kind != "box" {
			in.HomeID = e.home
		}
		p, err := inventory.CreatePlace(ctx, tx, e.hid, in)
		pid = p.ID
		return err
	})
	return pid
}

func (e *env) box(n int64, name string) string {
	e.t.Helper()
	var pid string
	e.tx(func(tx *sql.Tx) error {
		p, err := inventory.CreatePlace(ctx, tx, e.hid, inventory.PlaceInput{Kind: "box", Name: name, BoxNumber: &n, BoxStatus: "open"})
		pid = p.ID
		return err
	})
	return pid
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func (e *env) item(place string, fields map[string]any) string {
	e.t.Helper()
	f := inventory.Fields{}
	for k, v := range fields {
		f[k] = raw(v)
	}
	if place != "" {
		f["place_id"] = raw(place)
	}
	var iid string
	e.tx(func(tx *sql.Tx) error {
		it, _, err := inventory.CreateItem(ctx, tx, e.hid, "", inventory.NewItem{Fields: f})
		iid = it.ID
		return err
	})
	return iid
}

func (e *env) search(q string) search.Results {
	e.t.Helper()
	r, err := search.Search(ctx, e.d.Read, e.hid, q, search.Options{})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func ids(ms []search.Match) []string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func TestSubstringAndModelFolding(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Bedroom")
	hp := e.item(room, map[string]any{"name": "Sony headphones", "brand": "Sony", "model": "WH-1000XM4", "serial": "S01-ABC123"})
	e.item(room, map[string]any{"name": "Winter jacket"})

	for _, q := range []string{"wh1000", "WH-1000XM4", "wh1000xm4", "1000xm", "abc123", "sony"} {
		if got := ids(e.search(q).Items); !slices.Contains(got, hp) {
			t.Errorf("%q: items %v do not include the headphones", q, got)
		}
	}
	r := e.search("wh1000")
	m := r.Items[0]
	if m.Path != "Bedroom" || len(m.SnippetMarks) != 1 {
		t.Fatalf("%+v", m)
	}
	s := []rune(m.Snippet)
	if got := string(s[m.SnippetMarks[0][0]:m.SnippetMarks[0][1]]); got != "WH-1000" {
		t.Fatalf("marked %q in %q", got, m.Snippet)
	}
	if got := ids(e.search("zzzzz").Items); len(got) != 0 {
		t.Fatalf("unexpected %v", got)
	}
}

func TestTurkishMeetsAscii(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Oda")
	a := e.item(room, map[string]any{"name": "Isıtıcı"})
	b := e.item(room, map[string]any{"name": "Kış ISI bandı"})
	for _, q := range []string{"ısı", "ISI", "isi", "İSİ"} {
		got := ids(e.search(q).Items)
		if !slices.Contains(got, a) || !slices.Contains(got, b) {
			t.Errorf("%q: %v lacks one of the two items", q, got)
		}
	}
	for _, q := range []string{"ISITICI", "isitici", "ısıtıcı"} {
		if got := ids(e.search(q).Items); !slices.Equal(got, []string{a}) {
			t.Errorf("%q: %v", q, got)
		}
	}
	// all spellings give the same answer
	base := e.search("isi")
	for _, q := range []string{"ısı", "ISI", "İSİ"} {
		if !reflect.DeepEqual(ids(base.Items), ids(e.search(q).Items)) {
			t.Errorf("%q differs from isi", q)
		}
	}
	if m := e.search("sise").Items; len(m) != 0 {
		t.Fatal("no item has sise yet")
	}
	c := e.item(room, map[string]any{"name": "Şişe açacağı"})
	if got := ids(e.search("sise").Items); !slices.Equal(got, []string{c}) {
		t.Fatalf("sise: %v", got)
	}
}

func TestShortQueriesUsePrefix(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Kitchen")
	pan := e.item(room, map[string]any{"name": "Pan"})
	e.item(room, map[string]any{"name": "Frying span"}) // contains "pa" but not at a word start
	e.item(room, map[string]any{"name": "Hammer", "notes": "pa pa"})
	got := ids(e.search("pa").Items)
	if !slices.Equal(got, []string{pan}) {
		t.Fatalf("pa: %v", got)
	}
	if got := ids(e.search("p").Items); !slices.Contains(got, pan) {
		t.Fatalf("p: %v", got)
	}
}

func TestBareNumberPutsBoxFirst(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Hall")
	e.item(room, map[string]any{"name": "iPhone 12 case", "model": "12"})
	b120 := e.box(120, "Books")
	b12 := e.box(12, "Kitchen things")
	b3 := e.box(3, "Shoes")
	_ = b3
	r := e.search("12")
	if got := ids(r.Boxes); len(got) < 2 || got[0] != b12 || got[1] != b120 {
		t.Fatalf("boxes %v", got)
	}
	if r.Boxes[0].BoxNumber == nil || *r.Boxes[0].BoxNumber != 12 {
		t.Fatalf("box number %+v", r.Boxes[0])
	}
	if r.Top == nil || r.Top.ID != b12 {
		t.Fatalf("top %+v", r.Top)
	}
	// three digits go through the trigram index and still put the box first
	r = e.search("120")
	if r.Top == nil || r.Top.ID != b120 {
		t.Fatalf("top %+v", r.Top)
	}
	if got := ids(e.search("box 12").Boxes); len(got) == 0 || got[0] != b12 {
		t.Fatalf("box 12: %v", got)
	}
}

func TestPlacesLinesAndPaths(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Bedroom")
	closet := e.place(room, "furniture", "Left closet")
	drawer := e.place(closet, "furniture", "3rd drawer")
	it := e.item(drawer, map[string]any{"name": "Socks"})
	var line string
	e.tx(func(tx *sql.Tx) error {
		ls, err := inventory.CreateLines(ctx, tx, e.hid, "", drawer, []inventory.LineInput{{Name: "Spare buttons"}})
		line = ls[0].ID
		return err
	})
	r := e.search("closet")
	if !slices.Contains(ids(r.Places), closet) || !slices.Contains(ids(r.Places), drawer) {
		t.Fatalf("places %v", ids(r.Places))
	}
	// items and lines are found by the path of their place
	if !slices.Contains(ids(r.Items), it) || !slices.Contains(ids(r.Lines), line) {
		t.Fatalf("items %v lines %v", ids(r.Items), ids(r.Lines))
	}
	if got := e.search("buttons").Lines; len(got) != 1 || got[0].PlaceID != drawer {
		t.Fatalf("line place_id %+v, want %s", got, drawer)
	}
	if got := e.search("buttons").Lines; len(got) != 1 || got[0].Path != "3rd drawer · Left closet · Bedroom" {
		t.Fatalf("%+v", got)
	}

	// renaming the closet re-indexes everything below
	e.tx(func(tx *sql.Tx) error {
		_, err := inventory.UpdatePlace(ctx, tx, e.hid, closet, inventory.PlaceChange{Name: inventory.Optional[string]{Set: true, V: "Right wardrobe"}})
		return err
	})
	r = e.search("closet")
	if len(r.Places)+len(r.Items)+len(r.Lines) != 0 {
		t.Fatalf("old name still found: %+v", r)
	}
	r = e.search("wardrobe")
	if !slices.Contains(ids(r.Places), drawer) || !slices.Contains(ids(r.Items), it) || !slices.Contains(ids(r.Lines), line) {
		t.Fatalf("new name not found below: %+v", r)
	}
	assertRebuildEquals(t, e)
}

func TestDeletedRowsVanishAndRestoredReturn(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Garage")
	it := e.item(room, map[string]any{"name": "Cordless drill"})
	var line string
	e.tx(func(tx *sql.Tx) error {
		ls, err := inventory.CreateLines(ctx, tx, e.hid, "", room, []inventory.LineInput{{Name: "Loose screws"}})
		line = ls[0].ID
		return err
	})
	found := func(q string) bool {
		r := e.search(q)
		return len(r.Items)+len(r.Lines) > 0
	}
	if !found("drill") || !found("screws") {
		t.Fatal("not indexed")
	}
	e.tx(func(tx *sql.Tx) error { _, err := inventory.DeleteItem(ctx, tx, e.hid, "", it); return err })
	e.tx(func(tx *sql.Tx) error { _, err := inventory.DeleteLine(ctx, tx, e.hid, line); return err })
	if found("drill") || found("screws") {
		t.Fatal("deleted rows still found")
	}
	assertRebuildEquals(t, e)
	e.tx(func(tx *sql.Tx) error { _, err := inventory.RestoreItem(ctx, tx, e.hid, "", it); return err })
	e.tx(func(tx *sql.Tx) error { _, err := inventory.RestoreLine(ctx, tx, e.hid, line); return err })
	if !found("drill") || !found("screws") {
		t.Fatal("restored rows not found")
	}
	assertRebuildEquals(t, e)
}

func TestHouseholdsAreSeparate(t *testing.T) {
	e := newEnv(t)
	other := newHousehold(t, e.d)
	e.tx(func(tx *sql.Tx) error {
		h, _ := inventory.CreateHome(ctx, tx, other, "Other flat", "")
		p, _ := inventory.CreatePlace(ctx, tx, other, inventory.PlaceInput{HomeID: h.ID, Kind: "room", Name: "Walnut room"})
		_, _, err := inventory.CreateItem(ctx, tx, other, "", inventory.NewItem{Fields: inventory.Fields{
			"name": raw("Walnut gadget"), "model": raw("WG-99"), "place_id": raw(p.ID)}})
		return err
	})
	e.item(e.place("", "room", "Den"), map[string]any{"name": "Walnut bowl", "model": "WB-1"})
	for _, q := range []string{"walnut", "wg99", "99", "wg", "room"} {
		r := e.search(q)
		for _, m := range append(append(append(r.Items, r.Places...), r.Lines...), r.Boxes...) {
			if strings.Contains(strings.ToLower(m.Title+m.Path), "gadget") || strings.Contains(m.Path, "Walnut room") {
				t.Fatalf("%q leaked: %+v", q, m)
			}
		}
	}
	// the other household sees its own
	r, err := search.Search(ctx, e.d.Read, other, "walnut", search.Options{})
	if err != nil || len(r.Items) != 1 || r.Items[0].Title != "Walnut gadget" {
		t.Fatalf("%v %+v", err, r.Items)
	}
	// Similar stays inside the household too
	sim, err := search.Similar(ctx, e.d.Read, e.hid, "", "WG-99", "Walnut gadget")
	if err != nil || len(sim) != 0 {
		t.Fatalf("%v %v", err, sim)
	}
}

func TestIndexRowsAllCarryTheirHousehold(t *testing.T) {
	e := newEnv(t)
	e.item(e.place("", "room", "Den"), map[string]any{"name": "Lamp"})
	var bad int
	err := e.d.Read.QueryRow(`SELECT count(*) FROM search_fts f LEFT JOIN search_docs d
		ON d.id = f.rowid AND d.household_id = f.household_id AND d.kind = f.kind AND d.ref_id = f.ref_id
		WHERE d.id IS NULL`).Scan(&bad)
	if err != nil || bad != 0 {
		t.Fatalf("%v: %d fts rows without a matching doc", err, bad)
	}
}

// dump returns the whole index as sorted strings, ignoring rowids.
func dump(t *testing.T, d *db.DB) []string {
	t.Helper()
	rows, err := d.Read.Query(`SELECT d.household_id, d.kind, d.ref_id, d.display, d.detail, d.path, d.name_fold, d.key_fold,
		d.brand_fold, COALESCE(d.box_number, ''), f.title, f.body FROM search_docs d JOIN search_fts f ON f.rowid = d.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c [12]string
		ptrs := make([]any, 12)
		for i := range c {
			ptrs[i] = &c[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join(c[:], "|"))
	}
	var nd, nf int
	d.Read.QueryRow(`SELECT count(*) FROM search_docs`).Scan(&nd)
	d.Read.QueryRow(`SELECT count(*) FROM search_fts`).Scan(&nf)
	if nd != nf || nd != len(out) {
		t.Fatalf("docs %d, fts %d, joined %d", nd, nf, len(out))
	}
	sort.Strings(out)
	return out
}

func assertRebuildEquals(t *testing.T, e *env) {
	t.Helper()
	before := dump(t, e.d)
	if _, err := search.Rebuild(ctx, e.d, ""); err != nil {
		t.Fatal(err)
	}
	after := dump(t, e.d)
	if !slices.Equal(before, after) {
		t.Fatalf("incremental index differs from rebuild:\nincremental:\n%s\nrebuilt:\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

func TestRebuildEqualsIncremental(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Study")
	desk := e.place(room, "furniture", "Desk")
	b := e.box(7, "Cables")
	a := e.item(desk, map[string]any{"name": "Keyboard", "brand": "Keychron", "model": "K2", "attributes": map[string]any{}, "notes": "red switches"})
	e.item(b, map[string]any{"name": "HDMI cable"})
	e.tx(func(tx *sql.Tx) error {
		_, err := inventory.CreateLines(ctx, tx, e.hid, "", b, []inventory.LineInput{{Name: "USB-C cables"}, {Name: "Mouse"}})
		return err
	})
	e.tx(func(tx *sql.Tx) error {
		_, err := inventory.UpdateItem(ctx, tx, e.hid, "", a, inventory.Fields{"notes": raw("blue switches"), "place_id": raw(room)})
		return err
	})
	e.tx(func(tx *sql.Tx) error {
		_, err := inventory.UpdatePlace(ctx, tx, e.hid, room, inventory.PlaceChange{Name: inventory.Optional[string]{Set: true, V: "Office"}})
		return err
	})
	assertRebuildEquals(t, e)
	if n, err := search.Rebuild(ctx, e.d, e.hid); err != nil || n == 0 {
		t.Fatalf("%d %v", n, err)
	}
	assertRebuildEquals(t, e)
}

func TestFiveThousandItemsUnder50ms(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Storage")
	adj := []string{"red", "blue", "black", "small", "large", "old", "new", "spare", "travel", "kitchen"}
	noun := []string{"cable", "charger", "jacket", "lamp", "battery", "drill", "speaker", "router", "camera", "bottle"}
	e.tx(func(tx *sql.Tx) error {
		for i := 0; i < 5000; i++ {
			name := fmt.Sprintf("%s %s %d", adj[i%10], noun[(i/10)%10], i)
			_, _, err := inventory.CreateItem(ctx, tx, e.hid, "", inventory.NewItem{Fields: inventory.Fields{
				"name": raw(name), "brand": raw(fmt.Sprintf("Brand%d", i%50)), "model": raw(fmt.Sprintf("M-%04d", i)), "place_id": raw(room)}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	var n int
	e.d.Read.QueryRow(`SELECT count(*) FROM search_docs WHERE kind = 'item'`).Scan(&n)
	if n != 5000 {
		t.Fatalf("indexed %d", n)
	}
	e.search("charger") // warm up
	worst := time.Duration(0)
	for _, q := range []string{"charger", "m-4999", "brand7 lamp", "kitchen", "jacket 4", "xyz"} {
		start := time.Now()
		r := e.search(q)
		if d := time.Since(start); d > worst {
			worst = d
		}
		if q == "m-4999" && (len(r.Items) == 0 || r.Items[0].Title != "black bottle 4999") && len(r.Items) == 0 {
			t.Fatalf("m-4999: %+v", r.Items)
		}
	}
	if !raceEnabled && worst > 50*time.Millisecond {
		t.Fatalf("slowest search took %v", worst)
	}
	t.Logf("slowest search %v", worst)
}

func TestSimilar(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Office")
	hp := e.item(room, map[string]any{"name": "Sony headphones", "brand": "Sony", "model": "WH-1000XM4"})
	cable := e.item(room, map[string]any{"name": "Cable"})
	e.item(room, map[string]any{"name": "Cable", "brand": "Anker", "model": "A8012"})
	mx := e.item(room, map[string]any{"name": "Logitech MX Master 3"})
	e.item(room, map[string]any{"name": "Winter jacket"})

	sim := func(brand, model, name string, excl ...string) []string {
		ms, err := search.Similar(ctx, e.d.Read, e.hid, brand, model, name, excl...)
		if err != nil {
			t.Fatal(err)
		}
		return ids(ms)
	}
	// same brand and model, spelled differently
	if got := sim("SONY", "wh1000xm4", "Whatever"); !slices.Equal(got, []string{hp}) {
		t.Fatalf("brand+model: %v", got)
	}
	// a generic name matches nothing, not even the item with exactly that name
	if got := sim("", "", "cable"); len(got) != 0 {
		t.Fatalf("generic: %v", got)
	}
	if got := sim("Ugreen", "X1", "Cable"); len(got) != 0 {
		t.Fatalf("generic with other brand/model: %v", got)
	}
	// unless brand and model do
	if got := sim("Anker", "A8012", "Cable"); len(got) != 1 {
		t.Fatalf("generic with brand+model: %v", got)
	}
	_ = cable
	// a close, specific name
	if got := sim("", "", "logitech mx master 3"); !slices.Equal(got, []string{mx}) {
		t.Fatalf("close name: %v", got)
	}
	if got := sim("", "", "Logitech MX Master"); !slices.Equal(got, []string{mx}) {
		t.Fatalf("close name: %v", got)
	}
	if got := sim("", "", "Logitech keyboard"); len(got) != 0 {
		t.Fatalf("different product: %v", got)
	}
	if got := sim("Sony", "WH-1000XM4", "", hp); len(got) != 0 {
		t.Fatalf("excluded: %v", got)
	}
}

// ---- HTTP ---------------------------------------------------------------------

func serve(e *env, role auth.Role) *httptest.Server {
	svc := search.New(e.d)
	return httpapitest.Server(e.t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: "u", HouseholdID: e.hid, Role: role, Source: auth.SourceSession}))
				next.ServeHTTP(w, r)
			})
		})
		svc.Routes(mux)
	})
}

func TestEndpoint(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Bedroom")
	e.item(room, map[string]any{"name": "Sony headphones", "brand": "Sony", "model": "WH-1000XM4"})
	e.box(12, "Kitchen")
	srv := serve(e, auth.RoleViewer)

	status, body := httpapitest.Do(t, srv, "GET", "/api/v1/search?q=wh1000&limit=5", "")
	if status != 200 {
		t.Fatal(status, body)
	}
	var r struct {
		Q     string
		Top   map[string]any
		Items []map[string]any
		Lines []any
		Boxes []any
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if r.Q != "wh1000" || len(r.Items) != 1 || r.Lines == nil || r.Boxes == nil || r.Top == nil || r.Top["kind"] != "item" {
		t.Fatalf("%s", body)
	}
	if r.Items[0]["path"] != "Bedroom" || r.Items[0]["snippet_marks"].([]any)[0].([]any)[1].(float64) != 14 {
		t.Fatalf("%s", body)
	}
	status, body = httpapitest.Do(t, srv, "GET", "/api/v1/search?q=12&kinds=boxes", "")
	if status != 200 || !strings.Contains(body, `"box_number":12`) || strings.Contains(body, "Sony") {
		t.Fatal(status, body)
	}
	for _, bad := range []string{"/api/v1/search", "/api/v1/search?q=", "/api/v1/search?q=a&limit=0", "/api/v1/search?q=a&kinds=cats"} {
		if status, body := httpapitest.Do(t, srv, "GET", bad, ""); status != 400 {
			t.Errorf("%s: %d %s", bad, status, body)
		}
	}
	// punctuation-only queries are fine and empty
	if status, body := httpapitest.Do(t, srv, "GET", "/api/v1/search?q=%21%21", ""); status != 200 || !strings.Contains(body, `"items":[]`) {
		t.Fatal(status, body)
	}
}
