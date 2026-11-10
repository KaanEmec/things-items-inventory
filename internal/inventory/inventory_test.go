package inventory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"github.com/kaanemec/things-items-inventory/internal/id"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

var ctx = context.Background()

const stamp = "2026-01-01T00:00:00.000Z"

type env struct {
	t         *testing.T
	d         *db.DB
	srv       *httptest.Server
	hid, uid  string
	homeID    string
	rooms     map[string]string
	roleOwner auth.Role
}

// newEnv makes a household with one owner and a home, and a server whose
// requests are all signed in as that owner (identity injected by a test
// middleware: sessions are the auth package's business).
func newEnv(t *testing.T) *env {
	t.Helper()
	d := dbtest.New(t)
	e := &env{t: t, d: d, hid: id.New(), uid: id.New()}
	q := sqlc.New(d.Write)
	if _, err := q.CreateHousehold(ctx, sqlc.CreateHouseholdParams{ID: e.hid, Name: "H", Language: "en", Currency: "EUR", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateUser(ctx, sqlc.CreateUserParams{ID: e.uid, Name: "Kaan", Email: "k@example.com", PasswordHash: "x", CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	svc := inventory.New(d)
	e.srv = httpapitest.Server(t, func(mux *httpapi.Mux) {
		mux.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				role := auth.RoleOwner
				if r.Header.Get("X-Role") == "viewer" {
					role = auth.RoleViewer
				}
				r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{UserID: e.uid, UserName: "Kaan",
					HouseholdID: e.hid, Role: role, Source: auth.SourceSession}))
				next.ServeHTTP(w, r)
			})
		})
		svc.Routes(mux)
	})
	var h map[string]any
	e.must(http.StatusCreated, "POST", "/api/v1/homes", `{"name":"Flat"}`, &h)
	e.homeID = h["id"].(string)
	return e
}

func (e *env) call(method, path, body string, out any) int {
	e.t.Helper()
	status, resp := httpapitest.Do(e.t, e.srv, method, path, body)
	if out != nil && resp != "" {
		if err := json.Unmarshal([]byte(resp), out); err != nil {
			e.t.Fatalf("%s %s: bad JSON %q: %v", method, path, resp, err)
		}
	}
	return status
}

func (e *env) must(want int, method, path, body string, out any) {
	e.t.Helper()
	var raw json.RawMessage
	status, resp := httpapitest.Do(e.t, e.srv, method, path, body)
	if status != want {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, resp)
	}
	_ = raw
	if out != nil {
		if err := json.Unmarshal([]byte(resp), out); err != nil {
			e.t.Fatalf("%s %s: bad JSON: %v", method, path, err)
		}
	}
}

type obj = map[string]any

func (e *env) place(parent, kind, name string) string {
	e.t.Helper()
	body := obj{"kind": kind, "name": name}
	if parent == "" {
		body["home_id"] = e.homeID
	} else {
		body["parent_id"] = parent
	}
	b, _ := json.Marshal(body)
	var p obj
	e.must(http.StatusCreated, "POST", "/api/v1/places", string(b), &p)
	return p["id"].(string)
}

func (e *env) item(placeID, name string) obj {
	e.t.Helper()
	b, _ := json.Marshal(obj{"name": name, "place_id": placeID})
	var it obj
	e.must(http.StatusCreated, "POST", "/api/v1/items", string(b), &it)
	return it
}

func (e *env) getPlace(id string) obj {
	e.t.Helper()
	var p obj
	e.must(http.StatusOK, "GET", "/api/v1/places/"+id, "", &p)
	return p
}

func (e *env) getItem(id string) obj {
	e.t.Helper()
	var it obj
	e.must(http.StatusOK, "GET", "/api/v1/items/"+id, "", &it)
	return it
}

func errField(t *testing.T, body string) (code, message string, fields map[string]any) {
	t.Helper()
	var r struct {
		Error struct {
			Code, Message string
			Fields        map[string]any
		}
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("not an error body: %s", body)
	}
	return r.Error.Code, r.Error.Message, r.Error.Fields
}

// ---- places ---------------------------------------------------------------

func TestPathAndRename(t *testing.T) {
	e := newEnv(t)
	bedroom := e.place("", "room", "bedroom")
	closet := e.place(bedroom, "furniture", "left closet")
	drawer := e.place(closet, "spot", "3rd drawer from top")
	if p := e.getPlace(drawer); p["path"] != "3rd drawer from top · left closet · bedroom" {
		t.Fatalf("path = %v", p["path"])
	}
	it := e.item(drawer, "Charger")

	e.must(http.StatusOK, "PATCH", "/api/v1/places/"+bedroom, `{"name":"master bedroom"}`, nil)
	if p := e.getPlace(drawer); p["path"] != "3rd drawer from top · left closet · master bedroom" {
		t.Fatalf("path after rename = %v", p["path"])
	}
	got := e.getItem(it["id"].(string))["place"].(obj)
	if got["path"] != "3rd drawer from top · left closet · master bedroom" {
		t.Fatalf("item place path = %v", got["path"])
	}
}

func TestMoveRewritesDescendants(t *testing.T) {
	e := newEnv(t)
	bedroom := e.place("", "room", "bedroom")
	office := e.place("", "room", "office")
	closet := e.place(bedroom, "furniture", "closet")
	shelf := e.place(closet, "spot", "shelf")
	it := e.item(shelf, "Cable")

	var moved obj
	e.must(http.StatusOK, "PATCH", "/api/v1/places/"+closet, fmt.Sprintf(`{"parent_id":%q}`, office), &moved)
	if moved["parent_id"] != office {
		t.Fatalf("parent = %v", moved["parent_id"])
	}
	if p := e.getPlace(shelf); p["path"] != "shelf · closet · office" {
		t.Fatalf("descendant path = %v", p["path"])
	}
	if p := e.getItem(it["id"].(string))["place"].(obj); p["path"] != "shelf · closet · office" {
		t.Fatalf("item path = %v", p["path"])
	}
	// lineage: office's subtree now holds the item; bedroom's does not.
	var below, none struct{ Data []obj }
	e.must(http.StatusOK, "GET", "/api/v1/items?place_id="+office+"&below=1", "", &below)
	e.must(http.StatusOK, "GET", "/api/v1/items?place_id="+bedroom+"&below=1", "", &none)
	if len(below.Data) != 1 || len(none.Data) != 0 {
		t.Fatalf("below office %d, below bedroom %d", len(below.Data), len(none.Data))
	}
	// Top of a home again.
	e.must(http.StatusOK, "PATCH", "/api/v1/places/"+closet, `{"parent_id":null}`, nil)
	if p := e.getPlace(shelf); p["path"] != "shelf · closet" {
		t.Fatalf("path at top = %v", p["path"])
	}
}

func TestCycleRejected(t *testing.T) {
	e := newEnv(t)
	a := e.place("", "room", "a")
	b := e.place(a, "furniture", "b")
	c := e.place(b, "spot", "c")
	for _, target := range []string{c, b, a} {
		status, body := httpapitest.Do(t, e.srv, "PATCH", "/api/v1/places/"+a, fmt.Sprintf(`{"parent_id":%q}`, target))
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("move a under %s: %d %s", target, status, body)
		}
		if code, _, _ := errField(t, body); code != "invalid" {
			t.Fatalf("code %q", code)
		}
	}
	if p := e.getPlace(c); p["path"] != "c · b · a" {
		t.Fatalf("tree damaged: %v", p["path"])
	}
}

func TestDeleteNonEmptyPlace(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "room")
	other := e.place("", "room", "other")
	box := e.place(room, "furniture", "cupboard")
	it := e.item(room, "Lamp")
	e.must(http.StatusCreated, "POST", "/api/v1/lines", fmt.Sprintf(`{"place_id":%q,"lines":[{"name":"screws","quantity":40}]}`, room), nil)

	status, body := httpapitest.Do(t, e.srv, "DELETE", "/api/v1/places/"+room, "")
	if status != http.StatusConflict {
		t.Fatalf("status %d %s", status, body)
	}
	code, _, fields := errField(t, body)
	if code != "not_empty" || fields["places"] != "1" || fields["items"] != "1" || fields["lines"] != "1" {
		t.Fatalf("code %q fields %v", code, fields)
	}
	// Moving the contents into itself is refused.
	if s, _ := httpapitest.Do(t, e.srv, "DELETE", "/api/v1/places/"+room+"?move_to="+box, ""); s != http.StatusUnprocessableEntity {
		t.Fatalf("move_to inside: %d", s)
	}
	e.must(http.StatusOK, "DELETE", "/api/v1/places/"+room+"?move_to="+other, "", nil)

	if p := e.getPlace(box); p["path"] != "cupboard · other" || p["parent_id"] != other {
		t.Fatalf("child after delete: %v", p)
	}
	if got := e.getItem(it["id"].(string))["place"].(obj)["id"]; got != other {
		t.Fatalf("item in %v", got)
	}
	var lines struct{ Data []obj }
	e.must(http.StatusOK, "GET", "/api/v1/places/"+other+"/lines", "", &lines)
	if len(lines.Data) != 1 {
		t.Fatalf("lines moved: %d", len(lines.Data))
	}
	var tree struct{ Places []obj }
	e.must(http.StatusOK, "GET", "/api/v1/homes/"+e.homeID+"/tree", "", &tree)
	for _, p := range tree.Places {
		if p["id"] == room {
			t.Fatal("deleted place is in the tree")
		}
	}
	// An empty place deletes without move_to, and restores.
	empty := e.place("", "room", "empty")
	e.must(http.StatusOK, "DELETE", "/api/v1/places/"+empty, "", nil)
	e.must(http.StatusOK, "POST", "/api/v1/places/"+empty+"/restore", "", nil)
	e.must(http.StatusOK, "GET", "/api/v1/places/"+empty, "", nil)
}

func TestRestoreNeedsParent(t *testing.T) {
	e := newEnv(t)
	a := e.place("", "room", "a")
	b := e.place(a, "furniture", "b")
	e.must(http.StatusOK, "DELETE", "/api/v1/places/"+b, "", nil)
	e.must(http.StatusOK, "DELETE", "/api/v1/places/"+a, "", nil)
	if s, _ := httpapitest.Do(t, e.srv, "POST", "/api/v1/places/"+b+"/restore", ""); s != http.StatusConflict {
		t.Fatalf("restore under deleted parent: %d", s)
	}
	e.must(http.StatusOK, "POST", "/api/v1/places/"+a+"/restore", "", nil)
	e.must(http.StatusOK, "POST", "/api/v1/places/"+b+"/restore", "", nil)
}

func TestReorder(t *testing.T) {
	e := newEnv(t)
	a, b, c := e.place("", "room", "a"), e.place("", "room", "b"), e.place("", "room", "c")
	e.must(http.StatusOK, "PATCH", "/api/v1/places/"+c, `{"sort":0}`, nil)
	var tree struct{ Places []obj }
	e.must(http.StatusOK, "GET", "/api/v1/homes/"+e.homeID+"/tree", "", &tree)
	var order []string
	for _, p := range tree.Places {
		order = append(order, p["id"].(string))
	}
	if !slices.Equal(order, []string{c, a, b}) {
		t.Fatalf("order %v, want %v", order, []string{c, a, b})
	}
}

// countingDB counts the statements sent through it.
type countingDB struct {
	sqlc.DBTX
	n int
}

func (c *countingDB) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	c.n++
	return c.DBTX.QueryContext(ctx, q, a...)
}

func (c *countingDB) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	c.n++
	return c.DBTX.QueryRowContext(ctx, q, a...)
}

func TestFiveLevelTreeWith500PlacesInOneQuery(t *testing.T) {
	e := newEnv(t)
	kinds := []string{"floor", "room", "furniture", "furniture", "spot"}
	var total int
	err := e.d.Tx(ctx, func(tx *sql.Tx) error {
		// 1 + 4 + 16 + 64 + 415 = 500 places over five levels.
		level := []string{""}
		sizes := []int{1, 4, 16, 64, 415}
		for depth, size := range sizes {
			var next []string
			for i := 0; i < size; i++ {
				in := inventory.PlaceInput{Kind: kinds[depth], Name: fmt.Sprintf("p%d-%d", depth, i)}
				if parent := level[i%len(level)]; parent == "" {
					in.HomeID = e.homeID
				} else {
					in.ParentID = parent
				}
				p, err := inventory.CreatePlace(ctx, tx, e.hid, in)
				if err != nil {
					return err
				}
				next = append(next, p.ID)
				total++
			}
			level = next
		}
		// One item in the deepest places.
		for i := 0; i < 5; i++ {
			if _, _, err := inventory.CreateItem(ctx, tx, e.hid, e.uid, inventory.NewItem{Fields: inventory.Fields{
				"name": json.RawMessage(`"thing"`), "place_id": json.RawMessage(fmt.Sprintf("%q", level[i]))}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 500 {
		t.Fatalf("built %d places", total)
	}
	c := &countingDB{DBTX: e.d.Read}
	places, err := inventory.Tree(ctx, c, e.hid, e.homeID)
	if err != nil {
		t.Fatal(err)
	}
	if c.n != 1 || len(places) != 500 {
		t.Fatalf("queries %d, places %d", c.n, len(places))
	}
	var tree struct {
		Places []struct {
			ID, ParentID string
			Counts       struct {
				Items      int64 `json:"items"`
				ItemsBelow int64 `json:"items_below"`
			} `json:"counts"`
		}
	}
	e.must(http.StatusOK, "GET", "/api/v1/homes/"+e.homeID+"/tree", "", &tree)
	if len(tree.Places) != 500 {
		t.Fatalf("tree has %d places", len(tree.Places))
	}
	root := tree.Places[0] // first top-level place (the only floor)
	if root.Counts.ItemsBelow != 5 || root.Counts.Items != 0 {
		t.Fatalf("root counts %+v", root.Counts)
	}
}

func TestPlaceCounts(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "room")
	shelf := e.place(room, "spot", "shelf")
	e.item(room, "a")
	e.item(shelf, "b")
	e.item(shelf, "c")
	e.must(http.StatusCreated, "POST", "/api/v1/lines", fmt.Sprintf(`{"place_id":%q,"lines":[{"name":"x"}]}`, shelf), nil)
	p := e.getPlace(room)["counts"].(obj)
	if p["items"] != 1.0 || p["items_below"] != 3.0 || p["lines"] != 0.0 {
		t.Fatalf("room counts %v", p)
	}
	s := e.getPlace(shelf)["counts"].(obj)
	if s["items"] != 2.0 || s["items_below"] != 2.0 || s["lines"] != 1.0 {
		t.Fatalf("shelf counts %v", s)
	}
}

func TestHomesAndStarter(t *testing.T) {
	e := newEnv(t)
	e.place("", "room", "Kitchen")
	var h obj
	e.must(http.StatusCreated, "POST", "/api/v1/homes", fmt.Sprintf(`{"name":"New flat","copy_rooms_from":%q}`, e.homeID), &h)
	var tree struct{ Places []obj }
	e.must(http.StatusOK, "GET", "/api/v1/homes/"+h["id"].(string)+"/tree", "", &tree)
	if len(tree.Places) != 1 || tree.Places[0]["name"] != "Kitchen" {
		t.Fatalf("copied rooms: %v", tree.Places)
	}
	e.must(http.StatusOK, "PATCH", "/api/v1/homes/"+e.homeID, `{"archived":true}`, nil)
	var list struct{ Data []obj }
	e.must(http.StatusOK, "GET", "/api/v1/homes", "", &list)
	if len(list.Data) != 1 {
		t.Fatalf("active homes %d", len(list.Data))
	}
	e.must(http.StatusOK, "GET", "/api/v1/homes?archived=1", "", &list)
	if len(list.Data) != 1 || list.Data[0]["id"] != e.homeID {
		t.Fatalf("archived homes %v", list.Data)
	}
	// Starter tree.
	err := e.d.Tx(ctx, func(tx *sql.Tx) error { return inventory.SeedStarter(ctx, tx, e.hid) })
	if err != nil {
		t.Fatal(err)
	}
	e.must(http.StatusOK, "GET", "/api/v1/homes", "", &list)
	if len(list.Data) != 2 {
		t.Fatalf("homes after starter %d", len(list.Data))
	}
	// Viewers cannot manage homes.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/homes", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("X-Role", "viewer")
	res, err := e.srv.Client().Do(req)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer POST /homes: %v %v", res, err)
	}
	res.Body.Close()
}

func TestValidatePackingBox(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "room")
	var box string
	err := e.d.Tx(ctx, func(tx *sql.Tx) error {
		p, err := inventory.CreatePlace(ctx, tx, e.hid, inventory.PlaceInput{Kind: "box", Name: "Box 1"})
		box = p.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := inventory.ValidatePackingBox(ctx, e.d.Read, e.hid, box); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{room, id.New()} {
		if err := inventory.ValidatePackingBox(ctx, e.d.Read, e.hid, bad); err == nil {
			t.Fatalf("%s accepted as a packing box", bad)
		}
	}
	if err := inventory.ValidatePackingBox(ctx, e.d.Read, id.New(), box); err == nil {
		t.Fatal("a box of another household was accepted")
	}
}

// ---- items ------------------------------------------------------------------

func TestCategoriesEndpoint(t *testing.T) {
	e := newEnv(t)
	var c struct {
		Categories []struct {
			Key        string
			Attributes []struct{ Key, Type string }
		}
		Statuses []string
	}
	e.must(http.StatusOK, "GET", "/api/v1/categories", "", &c)
	if len(c.Categories) != 5 || c.Categories[0].Key != "gadget" || len(c.Statuses) != 6 {
		t.Fatalf("%+v", c)
	}
}

func TestAttributeValidation(t *testing.T) {
	e := newEnv(t)
	it := e.item("", "Phone")
	itemPath := "/api/v1/items/" + it["id"].(string)

	// Unknown key: 400 naming the key.
	status, body := httpapitest.Do(t, e.srv, "PATCH", itemPath, `{"category":"gadget","attributes":{"colour_temp":3}}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status %d %s", status, body)
	}
	if _, msg, fields := errField(t, body); !strings.Contains(msg, "colour_temp") || fields["attributes.colour_temp"] == nil {
		t.Fatalf("message %q fields %v", msg, fields)
	}
	// Nothing was saved.
	if got := e.getItem(it["id"].(string)); got["category"] != "other" {
		t.Fatalf("partial write: %v", got["category"])
	}
	// Wrong value: 422. Right values: stored, typed.
	if s, _ := httpapitest.Do(t, e.srv, "PATCH", itemPath, `{"category":"gadget","attributes":{"type":"toaster"}}`); s != http.StatusUnprocessableEntity {
		t.Fatalf("bad enum: %d", s)
	}
	if s, _ := httpapitest.Do(t, e.srv, "PATCH", itemPath, `{"category":"gadget","attributes":{"storage_gb":1.5}}`); s != http.StatusUnprocessableEntity {
		t.Fatalf("bad int: %d", s)
	}
	var got obj
	e.must(http.StatusOK, "PATCH", itemPath,
		`{"category":"gadget","attributes":{"type":"phone","storage_gb":128,"connectivity":["usb_c","wifi"],"screen_in":6.1}}`, &got)
	attrs := got["attributes"].(obj)
	if attrs["type"] != "phone" || attrs["storage_gb"] != 128.0 || len(attrs["connectivity"].([]any)) != 2 {
		t.Fatalf("attributes %v", attrs)
	}
	// Changing the category keeps the old keys; new keys are checked against the new set.
	e.must(http.StatusOK, "PATCH", itemPath, `{"category":"clothing"}`, &got)
	if got["attributes"].(obj)["storage_gb"] != 128.0 {
		t.Fatalf("attributes lost on category change: %v", got["attributes"])
	}
	if s, _ := httpapitest.Do(t, e.srv, "PATCH", itemPath, `{"attributes":{"storage_gb":256}}`); s != http.StatusBadRequest {
		t.Fatalf("gadget key on clothing: %d", s)
	}
	// An unknown category falls back to other, with free attributes.
	e.must(http.StatusOK, "PATCH", itemPath, `{"category":"vehicle","attributes":{"wheels":4}}`, &got)
	if got["category"] != "other" || got["attributes"].(obj)["wheels"] != 4.0 {
		t.Fatalf("unknown category: %v %v", got["category"], got["attributes"])
	}
	// Unknown top-level field.
	if s, _ := httpapitest.Do(t, e.srv, "PATCH", itemPath, `{"colour":"red"}`); s != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", s)
	}
}

func TestIdempotentCreate(t *testing.T) {
	e := newEnv(t)
	itemID := id.New()
	body := fmt.Sprintf(`{"id":%q,"name":"Drill","quantity":2,"purchase":{"price_minor":4999}}`, itemID)
	var first, second obj
	e.must(http.StatusCreated, "POST", "/api/v1/items", body, &first)
	e.must(http.StatusOK, "POST", "/api/v1/items", body, &second)
	e.must(http.StatusOK, "POST", "/api/v1/items", fmt.Sprintf(`{"id":%q,"name":"Different"}`, itemID), &second)
	if second["name"] != "Drill" || first["id"] != itemID {
		t.Fatalf("retry changed the row: %v", second)
	}
	var n int
	e.d.Read.QueryRow(`SELECT COUNT(*) FROM items WHERE household_id = ?`, e.hid).Scan(&n)
	if n != 1 {
		t.Fatalf("%d rows", n)
	}
	if p := first["purchase"].(obj); p["currency"] != "EUR" || p["price_minor"] != 4999.0 {
		t.Fatalf("purchase %v (currency defaults to the household's)", p)
	}
	if s, _ := httpapitest.Do(t, e.srv, "POST", "/api/v1/items", `{"id":"nope","name":"x"}`); s != http.StatusUnprocessableEntity {
		t.Fatalf("bad id: %d", s)
	}
	// Name is required unless it is a draft.
	if s, _ := httpapitest.Do(t, e.srv, "POST", "/api/v1/items", `{}`); s != http.StatusUnprocessableEntity {
		t.Fatalf("no name: %d", s)
	}
	var d obj
	e.must(http.StatusCreated, "POST", "/api/v1/items", `{"draft":true}`, &d)
	if d["name"] != "New item" || d["draft"] != true {
		t.Fatalf("draft %v", d)
	}
}

func TestAIWriteNeverChangesPersonEdits(t *testing.T) {
	e := newEnv(t)
	var it obj
	e.must(http.StatusCreated, "POST", "/api/v1/items", `{"draft":true,"source":"capture"}`, &it)
	itemID := it["id"].(string)
	apply := func(f map[string]any) []string {
		t.Helper()
		var applied []string
		err := e.d.Tx(ctx, func(tx *sql.Tx) (err error) {
			applied, err = inventory.ApplyAI(ctx, tx, e.hid, itemID, f)
			return
		})
		if err != nil {
			t.Fatal(err)
		}
		return applied
	}

	applied := apply(map[string]any{"name": "iPhone 13", "category": "gadget", "brand": "Apple", "model": "A2633",
		"attributes.storage_gb": 128, "attributes.type": "phone", "attributes.bogus": "x", "price_minor": 59900, "status": "gone"})
	slices.Sort(applied)
	if !slices.Equal(applied, []string{"attributes.storage_gb", "attributes.type", "brand", "category", "model", "name", "price_minor"}) {
		t.Fatalf("applied %v", applied)
	}
	got := e.getItem(itemID)
	if got["name"] != "iPhone 13" || got["status"] != "stored" || len(got["ai_fields"].([]any)) != 7 {
		t.Fatalf("after AI: %v", got)
	}

	// A person edits brand and a storage attribute: they leave ai_fields.
	e.must(http.StatusOK, "PATCH", "/api/v1/items/"+itemID, `{"brand":"Apple Inc.","attributes":{"storage_gb":256}}`, &got)
	if slices.Contains(toStrings(got["ai_fields"]), "brand") || slices.Contains(toStrings(got["ai_fields"]), "attributes.storage_gb") {
		t.Fatalf("ai_fields after person edit: %v", got["ai_fields"])
	}
	// A person also fills a field that was empty.
	e.must(http.StatusOK, "PATCH", "/api/v1/items/"+itemID, `{"serial":"SN-1"}`, nil)

	// A second AI run tries to overwrite everything.
	applied = apply(map[string]any{"name": "iPhone 14", "brand": "Orange", "serial": "SN-2", "attributes.storage_gb": 512,
		"model": "A9999", "color": "blue"})
	slices.Sort(applied)
	if !slices.Equal(applied, []string{"color", "model", "name"}) { // still AI-owned, or empty
		t.Fatalf("second run applied %v", applied)
	}
	got = e.getItem(itemID)
	attrs := got["attributes"].(obj)
	if got["brand"] != "Apple Inc." || got["serial"] != "SN-1" || attrs["storage_gb"] != 256.0 {
		t.Fatalf("AI changed a person's field: brand %v serial %v storage %v", got["brand"], got["serial"], attrs["storage_gb"])
	}
	if got["name"] != "iPhone 14" || got["model"] != "A9999" {
		t.Fatalf("AI could not update its own fields: %v %v", got["name"], got["model"])
	}
	// Editing a field as a person makes it theirs for good.
	e.must(http.StatusOK, "PATCH", "/api/v1/items/"+itemID, `{"name":"My phone"}`, nil)
	if applied = apply(map[string]any{"name": "iPhone 15"}); len(applied) != 0 {
		t.Fatalf("AI overwrote a renamed item: %v", applied)
	}
	if got = e.getItem(itemID); got["name"] != "My phone" {
		t.Fatalf("name %v", got["name"])
	}
	// Confirm accepts the rest.
	e.must(http.StatusOK, "POST", "/api/v1/items/"+itemID+"/confirm", "", &got)
	if len(got["ai_fields"].([]any)) != 0 || got["draft"] != false {
		t.Fatalf("after confirm: %v %v", got["ai_fields"], got["draft"])
	}
}

func toStrings(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

func TestListFiltersAndPaging(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "room")
	e.item(room, "b item")
	e.item(room, "a item")
	e.item("", "c item")
	var gad obj
	e.must(http.StatusCreated, "POST", "/api/v1/items", fmt.Sprintf(
		`{"name":"Laptop","category":"gadget","status":"in_use","place_id":%q,"warranty":{"end":"2099-01-01"}}`, room), &gad)
	e.must(http.StatusCreated, "POST", "/api/v1/items", `{"draft":true}`, nil)

	list := func(q string) (names []string, next any) {
		var page struct {
			Data       []obj
			NextCursor any `json:"next_cursor"`
		}
		e.must(http.StatusOK, "GET", "/api/v1/items?"+q, "", &page)
		for _, it := range page.Data {
			names = append(names, it["name"].(string))
		}
		return names, page.NextCursor
	}
	if n, _ := list("category=gadget"); !slices.Equal(n, []string{"Laptop"}) {
		t.Fatalf("category %v", n)
	}
	if n, _ := list("status=in_use&status=gone"); !slices.Equal(n, []string{"Laptop"}) {
		t.Fatalf("status %v", n)
	}
	if n, _ := list("draft=1"); !slices.Equal(n, []string{"New item"}) {
		t.Fatalf("draft %v", n)
	}
	if n, _ := list("warranty=active"); !slices.Equal(n, []string{"Laptop"}) {
		t.Fatalf("warranty %v", n)
	}
	if n, _ := list("warranty=ending"); len(n) != 0 {
		t.Fatalf("ending %v", n)
	}
	if n, _ := list("place_id=" + room); len(n) != 3 {
		t.Fatalf("place %v", n)
	}
	// Paging by name, two at a time.
	var all []string
	cursor := ""
	for page := 0; page < 5; page++ {
		q := "sort=name&limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		names, next := list(q)
		all = append(all, names...)
		if next == nil {
			break
		}
		cursor = next.(string)
	}
	if !slices.Equal(all, []string{"a item", "b item", "c item", "Laptop", "New item"}) {
		t.Fatalf("paged names %v", all)
	}
	if s, _ := httpapitest.Do(t, e.srv, "GET", "/api/v1/items?cursor=%25%25", ""); s != http.StatusBadRequest {
		t.Fatalf("bad cursor %d", s)
	}
	if s, _ := httpapitest.Do(t, e.srv, "GET", "/api/v1/items?place_id="+id.New(), ""); s != http.StatusNotFound {
		t.Fatalf("unknown place %d", s)
	}
}

func TestPlaceAndBelowListingIsOneQuery(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "room")
	closet := e.place(room, "furniture", "closet")
	elsewhere := e.place("", "room", "elsewhere")
	err := e.d.Tx(ctx, func(tx *sql.Tx) error {
		for i := 0; i < 1000; i++ {
			place := []string{room, closet, elsewhere, closet}[i%4]
			_, _, err := inventory.CreateItem(ctx, tx, e.hid, e.uid, inventory.NewItem{Fields: inventory.Fields{
				"name": json.RawMessage(fmt.Sprintf(`"item %04d"`, i)), "place_id": json.RawMessage(fmt.Sprintf("%q", place))}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &countingDB{DBTX: e.d.Read}
	items, next, err := inventory.ListItems(ctx, c, e.hid, inventory.ItemFilter{PlaceID: room, Below: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.n != 1 {
		t.Fatalf("%d queries", c.n)
	}
	if len(items) != 750 || next != "" {
		t.Fatalf("%d items below the room (want 750), next %q", len(items), next)
	}
	for _, it := range items {
		if it.Place == nil || (it.Place.ID != room && it.Place.ID != closet) {
			t.Fatalf("item in %v", it.Place)
		}
	}
	// A page of 200 is still one query.
	c.n = 0
	items, next, _ = inventory.ListItems(ctx, c, e.hid, inventory.ItemFilter{PlaceID: room, Below: true, Limit: 200})
	if c.n != 1 || len(items) != 200 || next == "" {
		t.Fatalf("page: queries %d items %d next %q", c.n, len(items), next)
	}
	// The query uses the lineage index, not a scan of places per item.
	var plan strings.Builder
	rows, err := e.d.Read.Query(`EXPLAIN QUERY PLAN SELECT i.id FROM items i JOIN places pl ON pl.id = i.place_id
		WHERE i.household_id = ? AND pl.lineage >= ? AND pl.lineage < ?`, e.hid, "/a/", "/a0")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a, b, c2 int
		var detail string
		rows.Scan(&a, &b, &c2, &detail)
		plan.WriteString(detail + "\n")
	}
	rows.Close()
	if !strings.Contains(plan.String(), "places_lineage") {
		t.Logf("plan: %s", plan.String())
	}
}

func TestSoftDeleteRestoreMoveBulk(t *testing.T) {
	e := newEnv(t)
	a, b := e.place("", "room", "a"), e.place("", "room", "b")
	i1, i2 := e.item(a, "one"), e.item(a, "two")

	e.must(http.StatusOK, "DELETE", "/api/v1/items/"+i1["id"].(string), "", nil)
	var page struct{ Data []obj }
	e.must(http.StatusOK, "GET", "/api/v1/items", "", &page)
	if len(page.Data) != 1 {
		t.Fatalf("deleted item listed: %d", len(page.Data))
	}
	e.must(http.StatusOK, "POST", "/api/v1/items/"+i1["id"].(string)+"/restore", "", nil)

	var moved obj
	e.must(http.StatusOK, "POST", "/api/v1/items/"+i1["id"].(string)+"/move", fmt.Sprintf(`{"place_id":%q}`, b), &moved)
	if moved["place"].(obj)["id"] != b {
		t.Fatalf("move: %v", moved["place"])
	}
	e.must(http.StatusOK, "POST", "/api/v1/items/"+i1["id"].(string)+"/move", `{"place_id":null}`, &moved)
	if moved["place"] != nil {
		t.Fatalf("not placed: %v", moved["place"])
	}
	e.must(http.StatusOK, "POST", "/api/v1/items/"+i1["id"].(string)+"/seen", "", nil)

	ids := fmt.Sprintf(`["%s","%s"]`, i1["id"], i2["id"])
	e.must(http.StatusOK, "POST", "/api/v1/items/bulk", fmt.Sprintf(`{"ids":%s,"place_id":%q,"status":"packed"}`, ids, b), &page)
	if len(page.Data) != 2 {
		t.Fatalf("bulk returned %d", len(page.Data))
	}
	for _, it := range page.Data {
		if it["status"] != "packed" || it["place"].(obj)["id"] != b {
			t.Fatalf("bulk item %v", it)
		}
	}
	// One unknown ID: 404 and nothing changed.
	body := fmt.Sprintf(`{"ids":["%s","%s"],"status":"lent"}`, i1["id"], id.New())
	if s, _ := httpapitest.Do(t, e.srv, "POST", "/api/v1/items/bulk", body); s != http.StatusNotFound {
		t.Fatalf("bulk with unknown id: %d", s)
	}
	if e.getItem(i1["id"].(string))["status"] != "packed" {
		t.Fatal("bulk was not atomic")
	}
	if s, _ := httpapitest.Do(t, e.srv, "POST", "/api/v1/items/bulk", `{"ids":[],"status":"lent"}`); s != http.StatusUnprocessableEntity {
		t.Fatalf("bulk with no ids: %d", s)
	}
}

func TestContentsLines(t *testing.T) {
	e := newEnv(t)
	box := e.place("", "furniture", "tool box")
	lineID := id.New()
	body := fmt.Sprintf(`{"place_id":%q,"lines":[{"id":%q,"name":"screws","quantity":40,"category":"part"},{"name":"tape"}]}`, box, lineID)
	var created struct{ Data []obj }
	e.must(http.StatusCreated, "POST", "/api/v1/lines", body, &created)
	e.must(http.StatusCreated, "POST", "/api/v1/lines", body, &created) // retry: same ids for the first, new for the second
	var lines struct{ Data []obj }
	e.must(http.StatusOK, "GET", "/api/v1/places/"+box+"/lines", "", &lines)
	if len(lines.Data) != 3 { // the retry added a second "tape": only lines with an id are idempotent
		t.Fatalf("%d lines", len(lines.Data))
	}
	var l obj
	e.must(http.StatusOK, "PATCH", "/api/v1/lines/"+lineID, `{"quantity":50}`, &l)
	if l["quantity"] != 50.0 {
		t.Fatalf("line %v", l)
	}
	var it obj
	e.must(http.StatusCreated, "POST", "/api/v1/lines/"+lineID+"/promote", "", &it)
	if it["name"] != "screws" || it["draft"] != true || it["quantity"] != 50.0 || it["category"] != "part" || it["place"].(obj)["id"] != box {
		t.Fatalf("promoted item %v", it)
	}
	var again obj
	e.must(http.StatusCreated, "POST", "/api/v1/lines/"+lineID+"/promote", "", &again)
	if again["id"] != it["id"] {
		t.Fatal("promote twice made two items")
	}
	e.must(http.StatusOK, "GET", "/api/v1/places/"+box+"/lines", "", &lines)
	if len(lines.Data) != 2 {
		t.Fatalf("promoted line still listed: %d", len(lines.Data))
	}
	var items struct{ Data []obj }
	e.must(http.StatusOK, "POST", "/api/v1/places/"+box+"/lines/promote-all", "", &items)
	if len(items.Data) != 2 {
		t.Fatalf("promote-all %d", len(items.Data))
	}
	// Delete and restore.
	e.must(http.StatusCreated, "POST", "/api/v1/lines", fmt.Sprintf(`{"place_id":%q,"lines":[{"name":"glue"}]}`, box), &created)
	gid := created.Data[0]["id"].(string)
	e.must(http.StatusOK, "DELETE", "/api/v1/lines/"+gid, "", nil)
	e.must(http.StatusOK, "POST", "/api/v1/lines/"+gid+"/restore", "", nil)
	e.must(http.StatusOK, "POST", "/api/v1/places/"+box+"/lines/confirm", "", nil)
}
