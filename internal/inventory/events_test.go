package inventory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

// fx is the fixture every write-path case starts from.
type fx struct {
	e              *env
	room, drawer   string
	other          string
	itemID, lineID string
	deletedItem    string
	box            string
}

func (e *env) countEvents() int {
	e.t.Helper()
	var n int
	if err := e.d.Read.QueryRow(`SELECT count(*) FROM events WHERE household_id = ?`, e.hid).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// lastEvents returns the n newest events oldest first.
func (e *env) lastEvents(n int) []sqlc.Event {
	e.t.Helper()
	rows, err := e.d.Read.Query(`SELECT id, household_id, at, actor_kind, user_id, token_id, token_name, kind, item_id, place_id, data
		FROM events WHERE household_id = ? ORDER BY at DESC, id DESC LIMIT ?`, e.hid, n)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []sqlc.Event
	for rows.Next() {
		var ev sqlc.Event
		if err := rows.Scan(&ev.ID, &ev.HouseholdID, &ev.At, &ev.ActorKind, &ev.UserID, &ev.TokenID, &ev.TokenName, &ev.Kind, &ev.ItemID, &ev.PlaceID, &ev.Data); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, ev)
	}
	slices.Reverse(out)
	return out
}

func newFx(t *testing.T) *fx {
	e := newEnv(t)
	f := &fx{e: e}
	f.room = e.place("", "room", "bedroom")
	f.drawer = e.place(f.room, "furniture", "drawer")
	f.other = e.place("", "room", "office")
	f.itemID = e.item(f.drawer, "Charger")["id"].(string)
	f.deletedItem = e.item(f.drawer, "Old cable")["id"].(string)
	e.must(http.StatusOK, "DELETE", "/api/v1/items/"+f.deletedItem, "", nil)
	var lines struct{ Data []obj }
	e.must(http.StatusCreated, "POST", "/api/v1/lines", fmt.Sprintf(`{"place_id":%q,"lines":[{"name":"screws"}]}`, f.drawer), &lines)
	f.lineID = lines.Data[0]["id"].(string)
	return f
}

// TestEveryWriteRecordsOneEvent calls each exported write function of the
// package and checks the right kind (and actor) was recorded in the same
// transaction. TestWriteFunctionsAreCovered makes sure none is missing here.
func TestEveryWriteRecordsOneEvent(t *testing.T) {
	for _, c := range writeCases() {
		t.Run(c.fn, func(t *testing.T) { runWriteCase(t, c) })
	}
}

type writeCase struct {
	fn   string
	kind string // want: the kind of the last event; "" = records nothing (exempt)
	prep func(f *fx)
	run  func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error
}

func writeCases() []writeCase {
	return append(baseWriteCases(), boxWriteCases()...)
}

func baseWriteCases() []writeCase {
	str := func(s string) *string { return &s }
	type tc = writeCase
	return []tc{
		{fn: "CreateItem", kind: "created", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, _, err := inventory.CreateItem(ctx, tx, hid, uid, inventory.NewItem{Fields: inventory.Fields{"name": []byte(`"Lamp"`), "place_id": []byte(fmt.Sprintf("%q", f.drawer))}})
			return err
		}},
		{fn: "UpdateItem", kind: "edited", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.UpdateItem(ctx, tx, hid, uid, f.itemID, inventory.Fields{"name": []byte(`"Charger 2"`)})
			return err
		}},
		{fn: "MoveItem", kind: "moved", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.MoveItem(ctx, tx, hid, uid, f.itemID, &f.other)
			return err
		}},
		{fn: "MarkSeen", kind: "seen", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.MarkSeen(ctx, tx, hid, uid, f.itemID)
			return err
		}},
		{fn: "ConfirmItem", kind: "confirmed", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.ConfirmItem(ctx, tx, hid, uid, f.itemID)
			return err
		}},
		{fn: "DeleteItem", kind: "deleted", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.DeleteItem(ctx, tx, hid, uid, f.itemID)
			return err
		}},
		{fn: "RestoreItem", kind: "restored", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.RestoreItem(ctx, tx, hid, uid, f.deletedItem)
			return err
		}},
		{fn: "BulkUpdate", kind: "status_changed", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.BulkUpdate(ctx, tx, hid, uid, []string{f.itemID}, inventory.BulkChange{Status: inventory.Optional[string]{Set: true, V: "to_sell"}})
			return err
		}},
		{fn: "ApplyAI", kind: "edited", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.ApplyAI(ctx, tx, hid, f.itemID, map[string]any{"brand": "Anker"})
			return err
		}},
		{fn: "SetAIMeta", kind: "", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			return inventory.SetAIMeta(ctx, tx, hid, f.itemID, "ready", nil)
		}},
		{fn: "CreatePlace", kind: "created", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.CreatePlace(ctx, tx, hid, inventory.PlaceInput{ParentID: f.room, Kind: "furniture", Name: "shelf"})
			return err
		}},
		{fn: "UpdatePlace", kind: "edited", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.UpdatePlace(ctx, tx, hid, f.drawer, inventory.PlaceChange{Name: inventory.Optional[string]{Set: true, V: "big drawer"}})
			return err
		}},
		{fn: "DeletePlace", kind: "deleted", prep: func(f *fx) { f.box = f.e.place(f.room, "furniture", "empty shelf") },
			run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
				_, err := inventory.DeletePlace(ctx, tx, hid, uid, f.box, "")
				return err
			}},
		{fn: "RestorePlace", kind: "restored", prep: func(f *fx) {
			f.box = f.e.place(f.room, "furniture", "empty shelf")
			f.e.must(http.StatusOK, "DELETE", "/api/v1/places/"+f.box, "", nil)
		}, run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.RestorePlace(ctx, tx, hid, f.box)
			return err
		}},
		{fn: "CreateLines", kind: "created", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.CreateLines(ctx, tx, hid, uid, f.drawer, []inventory.LineInput{{Name: "nails"}, {Name: "tape"}})
			return err
		}},
		{fn: "UpdateLine", kind: "edited", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.UpdateLine(ctx, tx, hid, f.lineID, inventory.LineChange{Name: inventory.Optional[string]{Set: true, V: "big screws"}})
			return err
		}},
		{fn: "DeleteLine", kind: "deleted", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.DeleteLine(ctx, tx, hid, f.lineID)
			return err
		}},
		{fn: "RestoreLine", kind: "restored", prep: func(f *fx) { f.e.must(http.StatusOK, "DELETE", "/api/v1/lines/"+f.lineID, "", nil) },
			run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
				_, err := inventory.RestoreLine(ctx, tx, hid, f.lineID)
				return err
			}},
		{fn: "ConfirmLines", kind: "confirmed", prep: func(f *fx) {
			if _, err := f.e.d.Write.Exec(`UPDATE contents_lines SET ai = 1 WHERE id = ?`, f.lineID); err != nil {
				f.e.t.Fatal(err)
			}
		}, run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.ConfirmLines(ctx, tx, hid, f.drawer)
			return err
		}},
		{fn: "PromoteLine", kind: "created", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.PromoteLine(ctx, tx, hid, uid, f.lineID)
			return err
		}},
		{fn: "PromoteAllLines", kind: "created", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.PromoteAllLines(ctx, tx, hid, uid, f.drawer)
			return err
		}},
		{fn: "CreateHome", kind: "", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.CreateHome(ctx, tx, hid, "Cabin", "")
			return err
		}},
		{fn: "UpdateHome", kind: "", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.UpdateHome(ctx, tx, hid, f.e.homeID, str("Flat 2"), nil, nil)
			return err
		}},
		{fn: "SeedStarter", kind: "", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			return inventory.SeedStarter(ctx, tx, hid)
		}},
	}
}

func runWriteCase(t *testing.T, c writeCase) {
	f := newFx(t)
	if c.prep != nil {
		c.prep(f)
	}
	before := f.e.countEvents()
	ctx := auth.WithIdentity(context.Background(), auth.Identity{UserID: f.e.uid, HouseholdID: f.e.hid, Role: auth.RoleMember, Source: auth.SourceSession})
	if err := f.e.d.Tx(ctx, func(tx *sql.Tx) error { return c.run(ctx, tx, f, f.e.hid, f.e.uid) }); err != nil {
		t.Fatal(err)
	}
	got := f.e.countEvents() - before
	if c.kind == "" {
		if got != 0 {
			t.Fatalf("%d events, want none (exempt)", got)
		}
		return
	}
	if got < 1 {
		t.Fatalf("%s wrote without recording an event", c.fn)
	}
	evs := f.e.lastEvents(got)
	last := evs[len(evs)-1]
	if last.Kind != c.kind {
		t.Fatalf("kind %q, want %q", last.Kind, c.kind)
	}
	if c.fn == "ApplyAI" {
		if last.ActorKind != "ai" {
			t.Fatalf("actor %q, want ai", last.ActorKind)
		}
	} else if last.ActorKind != "user" || last.UserID.String != f.e.uid {
		t.Fatalf("actor %q %q, want user %s", last.ActorKind, last.UserID.String, f.e.uid)
	}
}

// TestWriteFunctionsAreCovered fails when a new exported function taking a
// *sql.Tx appears in this package without a case in
// TestEveryWriteRecordsOneEvent.
func TestWriteFunctionsAreCovered(t *testing.T) {
	known := []string{"RecordEvent", "SeedTestData"} // the choke point itself; the test seeder
	for _, c := range writeCases() {
		known = append(known, c.fn)
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || !fd.Name.IsExported() {
					continue
				}
				for _, par := range fd.Type.Params.List {
					if st, ok := par.Type.(*ast.StarExpr); ok {
						if sel, ok := st.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Tx" && !slices.Contains(known, fd.Name.Name) {
							t.Errorf("%s takes a *sql.Tx but has no case in TestEveryWriteRecordsOneEvent; record an event or list it as exempt", fd.Name.Name)
						}
					}
				}
			}
		}
	}
}

func TestEditRecordsOnlyChangedFields(t *testing.T) {
	f := newFx(t)
	e := f.e
	n := e.countEvents()
	e.must(http.StatusOK, "PATCH", "/api/v1/items/"+f.itemID, `{"name":"Charger","brand":"Anker","notes":"USB-C"}`, nil) // name unchanged
	evs := e.lastEvents(e.countEvents() - n)
	if len(evs) != 1 || evs[0].Kind != "edited" {
		t.Fatalf("events %+v", evs)
	}
	var d struct{ Before, After map[string]any }
	mustJSON(t, evs[0].Data, &d)
	if len(d.After) != 2 || d.After["brand"] != "Anker" || d.After["notes"] != "USB-C" || d.Before["brand"] != nil {
		t.Fatalf("data %s", evs[0].Data)
	}
	// Same values again: nothing changed, nothing recorded.
	n = e.countEvents()
	e.must(http.StatusOK, "PATCH", "/api/v1/items/"+f.itemID, `{"brand":"Anker"}`, nil)
	if e.countEvents() != n {
		t.Fatal("a no-op edit recorded an event")
	}
	// A place change inside a PATCH is a move, a status change its own event.
	e.must(http.StatusOK, "PATCH", "/api/v1/items/"+f.itemID, fmt.Sprintf(`{"place_id":%q,"status":"in_use"}`, f.other), nil)
	evs = e.lastEvents(2)
	kinds := []string{evs[0].Kind, evs[1].Kind}
	slices.Sort(kinds)
	if kinds[0] != "moved" || kinds[1] != "status_changed" {
		t.Fatalf("kinds %v", kinds)
	}
}

func TestMoveAndSeenSetLastSeen(t *testing.T) {
	f := newFx(t)
	e := f.e
	if _, err := e.d.Write.Exec(`UPDATE items SET last_seen_at = '2020-01-01T00:00:00.000Z' WHERE id = ?`, f.itemID); err != nil {
		t.Fatal(err)
	}
	e.must(http.StatusOK, "POST", "/api/v1/items/"+f.itemID+"/seen", "", nil)
	if ls := e.getItem(f.itemID)["last_seen_at"]; ls == "2020-01-01T00:00:00.000Z" {
		t.Fatal("seen did not refresh last_seen_at")
	}
	if ev := e.lastEvents(1)[0]; ev.Kind != "seen" || ev.ItemID.String != f.itemID {
		t.Fatalf("event %+v", ev)
	}
	if _, err := e.d.Write.Exec(`UPDATE items SET last_seen_at = '2020-01-01T00:00:00.000Z' WHERE id = ?`, f.itemID); err != nil {
		t.Fatal(err)
	}
	e.must(http.StatusOK, "POST", "/api/v1/items/"+f.itemID+"/move", fmt.Sprintf(`{"place_id":%q}`, f.other), nil)
	if ls := e.getItem(f.itemID)["last_seen_at"]; ls == "2020-01-01T00:00:00.000Z" {
		t.Fatal("move did not refresh last_seen_at")
	}
	ev := e.lastEvents(1)[0]
	var d struct{ Before, After map[string]any }
	mustJSON(t, ev.Data, &d)
	if ev.Kind != "moved" || ev.PlaceID.String != f.other || d.Before["place_path"] != "drawer · bedroom" || d.After["place_path"] != "office" {
		t.Fatalf("event %+v %s", ev, ev.Data)
	}
	// Moving to where it already is counts as seen.
	e.must(http.StatusOK, "POST", "/api/v1/items/"+f.itemID+"/move", fmt.Sprintf(`{"place_id":%q}`, f.other), nil)
	if ev := e.lastEvents(1)[0]; ev.Kind != "seen" {
		t.Fatalf("same-place move recorded %q", ev.Kind)
	}
}

func TestMovingAContainerIsOneEvent(t *testing.T) {
	f := newFx(t)
	e := f.e
	for i := range 5 {
		e.item(f.drawer, fmt.Sprintf("thing %d", i))
	}
	n := e.countEvents()
	e.must(http.StatusOK, "PATCH", "/api/v1/places/"+f.drawer, fmt.Sprintf(`{"parent_id":%q}`, f.other), nil)
	if got := e.countEvents() - n; got != 1 {
		t.Fatalf("%d events for moving a drawer with 6 items, want 1", got)
	}
	ev := e.lastEvents(1)[0]
	if ev.Kind != "moved" || ev.PlaceID.String != f.drawer || ev.ItemID.Valid {
		t.Fatalf("event %+v", ev)
	}
}

func TestTokenActor(t *testing.T) {
	f := newFx(t)
	ctx := auth.WithIdentity(context.Background(), auth.Identity{UserID: f.e.uid, HouseholdID: f.e.hid, Role: auth.RoleMember,
		Source: auth.SourceToken, TokenID: "tok1", TokenName: "Claude agent"})
	if err := f.e.d.Tx(ctx, func(tx *sql.Tx) error {
		_, err := inventory.MarkSeen(ctx, tx, f.e.hid, f.e.uid, f.itemID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ev := f.e.lastEvents(1)[0]
	if ev.ActorKind != "token" || ev.TokenName.String != "Claude agent" || ev.UserID.String != f.e.uid {
		t.Fatalf("event %+v", ev)
	}
	var page histPage
	f.e.must(http.StatusOK, "GET", "/api/v1/items/"+f.itemID+"/history?limit=1", "", &page)
	a := page.Data[0].Actor
	if a.Kind != "token" || a.TokenName == nil || *a.TokenName != "Claude agent" || a.Name != "Kaan" || a.Initials != "K" {
		t.Fatalf("actor %+v", a)
	}
}

type histPage struct {
	Data       []inventory.HistoryEntry `json:"data"`
	NextCursor *string                  `json:"next_cursor"`
}

func TestHistoryAPIAndPaging(t *testing.T) {
	f := newFx(t)
	e := f.e
	for i := range 6 {
		e.must(http.StatusOK, "PATCH", "/api/v1/items/"+f.itemID, fmt.Sprintf(`{"notes":"n%d"}`, i), nil)
	}
	e.must(http.StatusOK, "POST", "/api/v1/items/"+f.itemID+"/move", fmt.Sprintf(`{"place_id":%q}`, f.other), nil)

	// Paging: 2 per page, walk to the end; no duplicates, strictly newest first.
	var all []inventory.HistoryEntry
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 20 {
			t.Fatal("paging does not end")
		}
		var p histPage
		e.must(http.StatusOK, "GET", "/api/v1/items/"+f.itemID+"/history?limit=2&cursor="+cursor, "", &p)
		if len(p.Data) > 2 {
			t.Fatalf("page of %d", len(p.Data))
		}
		all = append(all, p.Data...)
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	// created, 6 edits, moved
	if len(all) != 8 {
		t.Fatalf("%d entries, want 8", len(all))
	}
	seen := map[string]bool{}
	for i, en := range all {
		if seen[en.ID] {
			t.Fatalf("duplicate %s", en.ID)
		}
		seen[en.ID] = true
		if i > 0 && (en.At > all[i-1].At || (en.At == all[i-1].At && en.ID > all[i-1].ID)) {
			t.Fatalf("not newest first at %d", i)
		}
	}
	oldest, newest := all[len(all)-1], all[0]
	if oldest.Kind != "created" || newest.Kind != "moved" {
		t.Fatalf("oldest %q newest %q", oldest.Kind, newest.Kind)
	}
	if newest.Summary["subject"] != "item" || newest.Summary["from"] != "drawer · bedroom" || newest.Summary["to"] != "office" {
		t.Fatalf("summary %v", newest.Summary)
	}
	if newest.Actor.Kind != "user" || newest.Actor.Name != "Kaan" || newest.Actor.Initials != "K" ||
		newest.ItemName == nil || *newest.ItemName != "Charger" || newest.PlaceName == nil || *newest.PlaceName != "office" {
		t.Fatalf("entry %+v", newest)
	}
	var edit inventory.HistoryEntry
	for _, en := range all {
		if en.Kind == "edited" {
			edit = en
			break
		}
	}
	if f, _ := edit.Summary["fields"].([]any); len(f) != 1 || f[0] != "notes" {
		t.Fatalf("edit summary %v", edit.Summary)
	}

	// The place feed: the office saw the item arrive, the drawer saw it leave.
	for _, place := range []string{f.other, f.drawer} {
		var p histPage
		e.must(http.StatusOK, "GET", "/api/v1/places/"+place+"/history", "", &p)
		found := false
		for _, en := range p.Data {
			found = found || (en.Kind == "moved" && en.ItemID != nil && *en.ItemID == f.itemID)
		}
		if !found {
			t.Fatalf("place %s history lacks the move: %+v", place, p.Data)
		}
	}
	// The household feed has everything of this household, newest first.
	var feed histPage
	e.must(http.StatusOK, "GET", "/api/v1/history?limit=200", "", &feed)
	if len(feed.Data) != e.countEvents() || feed.Data[0].ID != newest.ID {
		t.Fatalf("feed %d entries, %d events", len(feed.Data), e.countEvents())
	}
	// Errors.
	if st := e.call("GET", "/api/v1/history?cursor=garbage", "", nil); st != http.StatusBadRequest && st != http.StatusUnprocessableEntity {
		t.Fatalf("bad cursor: %d", st)
	}
	if st := e.call("GET", "/api/v1/items/nope/history", "", nil); st != http.StatusNotFound {
		t.Fatalf("unknown item: %d", st)
	}
	if st := e.call("GET", "/api/v1/places/nope/history", "", nil); st != http.StatusNotFound {
		t.Fatalf("unknown place: %d", st)
	}
	// A deleted item keeps its history.
	e.must(http.StatusOK, "DELETE", "/api/v1/items/"+f.itemID, "", nil)
	var p histPage
	e.must(http.StatusOK, "GET", "/api/v1/items/"+f.itemID+"/history?limit=1", "", &p)
	if p.Data[0].Kind != "deleted" {
		t.Fatalf("newest %q", p.Data[0].Kind)
	}
}

func TestLineEventsBelongToTheContainer(t *testing.T) {
	f := newFx(t)
	var p histPage
	f.e.must(http.StatusOK, "GET", "/api/v1/places/"+f.drawer+"/history", "", &p)
	var ev *inventory.HistoryEntry
	for i, en := range p.Data {
		if en.Summary["subject"] == "lines" {
			ev = &p.Data[i]
		}
	}
	if ev == nil || ev.Kind != "created" || ev.ItemID != nil || ev.Summary["count"] != 1.0 {
		t.Fatalf("history %+v", p.Data)
	}
}

func TestCommitPublishesRollbackDoesNot(t *testing.T) {
	f := newFx(t)
	e := f.e
	var got atomic.Int32
	var last atomic.Value
	unsub := inventory.OnCommit(func(n inventory.Notice) { got.Add(1); last.Store(n) })
	defer unsub()
	ctx := auth.WithIdentity(context.Background(), auth.Identity{UserID: e.uid, HouseholdID: e.hid, Role: auth.RoleMember, Source: auth.SourceSession})

	boom := errors.New("boom")
	n := e.countEvents()
	err := inventory.InTx(ctx, e.d, func(tx *sql.Tx) error {
		if _, err := inventory.MarkSeen(ctx, tx, e.hid, e.uid, f.itemID); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || got.Load() != 0 || e.countEvents() != n {
		t.Fatalf("rolled back: err %v, %d notices, %d new events", err, got.Load(), e.countEvents()-n)
	}

	if err := inventory.InTx(ctx, e.d, func(tx *sql.Tx) error {
		_, err := inventory.MoveItem(ctx, tx, e.hid, e.uid, f.itemID, &f.other)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	nt, _ := last.Load().(inventory.Notice)
	if got.Load() != 1 || nt != (inventory.Notice{HouseholdID: e.hid, Type: "item", ID: f.itemID, Kind: "moved"}) {
		t.Fatalf("committed: %d notices, last %+v", got.Load(), nt)
	}

	// Through the API too: a failing request (bad place) emits nothing.
	got.Store(0)
	if st := e.call("POST", "/api/v1/items/"+f.itemID+"/move", `{"place_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`, nil); st < 400 {
		t.Fatalf("status %d", st)
	}
	e.must(http.StatusOK, "POST", "/api/v1/items/"+f.itemID+"/seen", "", nil)
	if got.Load() != 1 {
		t.Fatalf("%d notices, want 1 (the successful seen)", got.Load())
	}
}

func TestOnChangeHook(t *testing.T) {
	f := newFx(t)
	e := f.e
	var changes []inventory.Change
	inTx := false
	unsub := inventory.OnChange(func(ctx context.Context, tx *sql.Tx, c inventory.Change) error {
		// The hook runs inside the transaction: the event row is visible to it.
		var n int
		inTx = tx.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE household_id = ? AND kind = ? AND item_id = ?`, c.HouseholdID, c.Kind, c.ItemID).Scan(&n) == nil && n > 0
		changes = append(changes, c)
		return nil
	})
	e.must(http.StatusOK, "POST", "/api/v1/items/"+f.itemID+"/move", fmt.Sprintf(`{"place_id":%q}`, f.other), nil)
	unsub()
	if len(changes) != 1 || changes[0] != (inventory.Change{HouseholdID: e.hid, Kind: "moved", ItemID: f.itemID, PlaceID: f.other}) || !inTx {
		t.Fatalf("changes %+v inTx %v", changes, inTx)
	}
	// An error from a hook rolls the change back.
	unsub = inventory.OnChange(func(context.Context, *sql.Tx, inventory.Change) error { return errors.New("index broke") })
	defer unsub()
	n := e.countEvents()
	if st := e.call("POST", "/api/v1/items/"+f.itemID+"/seen", "", nil); st < 500 {
		t.Fatalf("status %d", st)
	}
	if e.countEvents() != n {
		t.Fatal("event survived a failing hook")
	}
}

func mustJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
}
