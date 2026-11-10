package inventory_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/db/sqlc"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
)

type boxPage struct {
	Data []obj `json:"data"`
}

func (e *env) boxes(body string) []obj {
	e.t.Helper()
	var p boxPage
	e.must(http.StatusCreated, "POST", "/api/v1/boxes", body, &p)
	return p.Data
}

func numbers(boxes []obj) []int {
	out := make([]int, len(boxes))
	for i, b := range boxes {
		out[i] = int(b["box_number"].(float64))
	}
	return out
}

func (e *env) member() {
	e.t.Helper()
	if err := sqlc.New(e.d.Write).CreateMembership(ctx, sqlc.CreateMembershipParams{HouseholdID: e.hid, UserID: e.uid, Role: "owner", CreatedAt: stamp}); err != nil {
		e.t.Fatal(err)
	}
}

func boxWriteCases() []writeCase {
	var box string
	prep := func(f *fx) {
		var p boxPage
		f.e.must(http.StatusCreated, "POST", "/api/v1/boxes", `{}`, &p)
		box = p.Data[0]["id"].(string)
		f.e.member()
	}
	return []writeCase{
		{fn: "CreateBoxes", kind: "created", run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.CreateBoxes(ctx, tx, hid, 2, 0, 0, inventory.BoxInput{})
			return err
		}},
		{fn: "UpdateBox", kind: "edited", prep: prep, run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.UpdateBox(ctx, tx, hid, uid, box, inventory.BoxChange{Note: inventory.Optional[string]{Set: true, V: "kitchen things"}})
			return err
		}},
		{fn: "CloseBox", kind: "packed", prep: prep, run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.CloseBox(ctx, tx, hid, uid, box)
			return err
		}},
		{fn: "BulkBoxes", kind: "edited", prep: prep, run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.BulkBoxes(ctx, tx, hid, uid, []string{box}, inventory.BoxChange{Note: inventory.Optional[string]{Set: true, V: "x"}})
			return err
		}},
		{fn: "UnpackBox", kind: "unpacked", prep: func(f *fx) {
			prep(f)
			f.e.must(http.StatusOK, "POST", "/api/v1/boxes/"+box+"/close", `{}`, nil)
		}, run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			_, err := inventory.UnpackBox(ctx, tx, hid, uid, box, nil, nil)
			return err
		}},
		{fn: "SetPackingBox", kind: "", prep: prep, run: func(ctx context.Context, tx *sql.Tx, f *fx, hid, uid string) error {
			return inventory.SetPackingBox(ctx, tx, hid, uid, box, nil)
		}},
	}
}

func TestFortyBoxesInOneStep(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Kitchen")
	got := e.boxes(fmt.Sprintf(`{"from":1,"to":40,"goes_to_id":%q,"handling":["fragile","heavy","heavy"]}`, room))
	if len(got) != 40 {
		t.Fatalf("%d boxes", len(got))
	}
	seen := map[int]bool{}
	for i, n := range numbers(got) {
		if n != i+1 || seen[n] {
			t.Fatalf("box %d has number %d", i, n)
		}
		seen[n] = true
	}
	b := got[11]
	if b["name"] != "Box 12" || b["box_status"] != "empty" || b["kind"] != "box" || b["home_id"] != nil {
		t.Fatalf("box 12: %v", b)
	}
	if h := fmt.Sprint(b["handling"]); h != "[heavy fragile]" {
		t.Fatalf("handling %s", h)
	}
	if gt := b["goes_to"].(obj); gt["id"] != room || gt["color"] == nil {
		t.Fatalf("goes_to %v", gt)
	}
	// The next box continues the numbering; the range cannot go back.
	if n := numbers(e.boxes(`{}`)); n[0] != 41 {
		t.Fatalf("next number %v", n)
	}
	if st := e.call("POST", "/api/v1/boxes", `{"from":10,"to":12}`, nil); st != http.StatusConflict {
		t.Fatalf("old range: %d", st)
	}
	if st := e.call("POST", "/api/v1/boxes", `{"count":201}`, nil); st != http.StatusUnprocessableEntity {
		t.Fatalf("too many: %d", st)
	}
	if st := e.call("POST", "/api/v1/boxes", fmt.Sprintf(`{"count":1,"goes_to_id":%q}`, e.homeID), nil); st != http.StatusUnprocessableEntity {
		t.Fatalf("bad destination: %d", st)
	}
	// List with filters and paging.
	var p struct {
		Data       []obj   `json:"data"`
		NextCursor *string `json:"next_cursor"`
	}
	e.must(http.StatusOK, "GET", "/api/v1/boxes?limit=30", "", &p)
	if len(p.Data) != 30 || p.NextCursor == nil || numbers(p.Data)[29] != 30 {
		t.Fatalf("page 1: %d %v", len(p.Data), p.NextCursor)
	}
	e.must(http.StatusOK, "GET", "/api/v1/boxes?limit=30&cursor="+*p.NextCursor, "", &p)
	if len(p.Data) != 11 || p.NextCursor != nil {
		t.Fatalf("page 2: %d", len(p.Data))
	}
	e.must(http.StatusOK, "GET", "/api/v1/boxes?goes_to_id="+room, "", &p)
	if len(p.Data) != 40 {
		t.Fatalf("by room: %d", len(p.Data))
	}
	e.must(http.StatusOK, "GET", "/api/v1/boxes?status=packed", "", &p)
	if len(p.Data) != 0 {
		t.Fatalf("packed: %d", len(p.Data))
	}
	if st := e.call("GET", "/api/v1/boxes?status=nope", "", nil); st != http.StatusUnprocessableEntity && st != http.StatusBadRequest {
		t.Fatalf("bad status filter: %d", st)
	}
}

func TestBoxNumbersSurviveDeletes(t *testing.T) {
	e := newEnv(t)
	got := e.boxes(`{"count":3}`)
	e.must(http.StatusOK, "DELETE", "/api/v1/places/"+got[2]["id"].(string), "", nil)
	if n := numbers(e.boxes(`{"count":2}`)); !slices.Equal(n, []int{4, 5}) {
		t.Fatalf("numbers after delete: %v", n)
	}
	// A range above the counter is allowed and burns the numbers in between.
	if n := numbers(e.boxes(`{"from":10,"to":11}`)); !slices.Equal(n, []int{10, 11}) {
		t.Fatalf("range: %v", n)
	}
	if n := numbers(e.boxes(`{}`)); n[0] != 12 {
		t.Fatalf("after range: %v", n)
	}
	if st := e.call("GET", "/api/v1/boxes/number/3", "", nil); st != http.StatusNotFound {
		t.Fatalf("deleted box by number: %d", st)
	}
}

func TestConcurrentBoxCreatesNeverCollide(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var all []int
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var p boxPage
			if st := e.call("POST", "/api/v1/boxes", `{"count":5}`, &p); st != http.StatusCreated {
				t.Errorf("status %d", st)
				return
			}
			mu.Lock()
			all = append(all, numbers(p.Data)...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.Sort(all)
	if len(all) != 40 {
		t.Fatalf("%d numbers", len(all))
	}
	for i, n := range all {
		if n != i+1 {
			t.Fatalf("numbers %v", all)
		}
	}
}

func TestPackingNowByNumber(t *testing.T) {
	e := newEnv(t)
	e.member()
	room := e.place("", "room", "Bedroom")
	got := e.boxes(fmt.Sprintf(`{"count":3,"goes_to_id":%q}`, room))
	put := func(body string, want int) (out struct {
		PackingBox obj `json:"packing_box"`
	}) {
		t.Helper()
		e.must(want, "PUT", "/api/v1/me/packing-box", body, &out)
		return out
	}
	if pb := put(`{"number":2}`, http.StatusOK).PackingBox; pb["box_number"] != float64(2) || pb["goes_to"].(obj)["name"] != "Bedroom" {
		t.Fatalf("packing box %v", pb)
	}
	if pb, err := inventory.PackingBoxOf(ctx, e.d.Read, e.hid, e.uid); err != nil || pb == nil || pb.ID != got[1]["id"] {
		t.Fatalf("PackingBoxOf %v %v", pb, err)
	}
	put(`{"number":99}`, http.StatusUnprocessableEntity) // creates nothing
	put(`{"box_id":"`+room+`"}`, http.StatusUnprocessableEntity)
	if n := len(e.listBoxes()); n != 3 {
		t.Fatalf("%d boxes after a bad number", n)
	}
	if pb := put(`{"box_id":"`+got[0]["id"].(string)+`"}`, http.StatusOK).PackingBox; pb["box_number"] != float64(1) {
		t.Fatalf("by id: %v", pb)
	}
	// Deleting the box clears it.
	e.must(http.StatusOK, "DELETE", "/api/v1/places/"+got[0]["id"].(string), "", nil)
	if pb, _ := inventory.PackingBoxOf(ctx, e.d.Read, e.hid, e.uid); pb != nil {
		t.Fatalf("deleted box still packing: %v", pb)
	}
	put(`{"number":3}`, http.StatusOK)
	if pb := put(`null`, http.StatusOK).PackingBox; pb != nil {
		t.Fatalf("not cleared: %v", pb)
	}
	put(`{}`, http.StatusUnprocessableEntity)
}

func (e *env) listBoxes() []obj {
	e.t.Helper()
	var p boxPage
	e.must(http.StatusOK, "GET", "/api/v1/boxes?limit=200", "", &p)
	return p.Data
}

func historyKinds(e *env, path string) []string {
	e.t.Helper()
	var h struct{ Data []struct{ Kind string } }
	e.must(http.StatusOK, "GET", path, "", &h)
	var out []string
	for _, x := range h.Data {
		out = append(out, x.Kind)
	}
	return out
}

func TestClosingABoxShowsInBothHistories(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Office")
	drawer := e.place(room, "furniture", "Drawer")
	box := e.boxes(`{}`)[0]["id"].(string)
	it := e.item(drawer, "Stapler")
	itemID := it["id"].(string)

	e.must(http.StatusOK, "POST", "/api/v1/items/"+itemID+"/move", fmt.Sprintf(`{"place_id":%q}`, box), nil)
	if st := e.getItem(itemID)["status"]; st != "packed" {
		t.Fatalf("item status %v", st)
	}
	var b obj
	e.must(http.StatusOK, "GET", "/api/v1/boxes/"+box, "", &b)
	if b["box_status"] != "open" {
		t.Fatalf("box status %v", b["box_status"])
	}
	e.must(http.StatusOK, "POST", "/api/v1/boxes/"+box+"/close", "", &b)
	if b["box_status"] != "packed" || b["packed_at"] == nil {
		t.Fatalf("closed box %v", b)
	}
	var pb string
	if err := e.d.Read.QueryRow(`SELECT packed_by FROM places WHERE id = ?`, box).Scan(&pb); err != nil || pb != e.uid {
		t.Fatalf("packed_by %q %v", pb, err)
	}
	// Closing again changes nothing and records nothing.
	before := e.countEvents()
	e.must(http.StatusOK, "POST", "/api/v1/boxes/"+box+"/close", "", nil)
	if e.countEvents() != before {
		t.Fatal("closing twice recorded an event")
	}

	boxKinds := historyKinds(e, "/api/v1/places/"+box+"/history")
	for _, k := range []string{"created", "opened", "packed", "moved"} {
		if !slices.Contains(boxKinds, k) {
			t.Errorf("box history %v lacks %s", boxKinds, k)
		}
	}
	itemKinds := historyKinds(e, "/api/v1/items/"+itemID+"/history")
	for _, k := range []string{"moved", "status_changed"} {
		if !slices.Contains(itemKinds, k) {
			t.Errorf("item history %v lacks %s", itemKinds, k)
		}
	}
	if !slices.Contains(historyKinds(e, "/api/v1/history"), "packed") {
		t.Error("household history lacks packed")
	}
	// Taking the item out puts it back to stored.
	e.must(http.StatusOK, "POST", "/api/v1/items/"+itemID+"/move", fmt.Sprintf(`{"place_id":%q}`, drawer), nil)
	if st := e.getItem(itemID)["status"]; st != "stored" {
		t.Fatalf("item status after unpacking %v", st)
	}
	// Edits and explicit status.
	e.must(http.StatusOK, "PATCH", "/api/v1/boxes/"+box, `{"handling":["soft"],"note":"books","box_status":"arrived"}`, &b)
	if b["box_status"] != "arrived" || b["note"] != "books" || fmt.Sprint(b["handling"]) != "[soft]" {
		t.Fatalf("patched %v", b)
	}
	if st := e.call("POST", "/api/v1/boxes/"+box+"/close", "", nil); st != http.StatusConflict {
		t.Fatalf("close after arrived: %d", st)
	}
	if st := e.call("PATCH", "/api/v1/boxes/"+box, `{"handling":["wet"]}`, nil); st != http.StatusUnprocessableEntity {
		t.Fatalf("bad handling: %d", st)
	}
}

func TestBoxReadInOneRequest(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Living room")
	var gt obj
	e.must(http.StatusOK, "GET", "/api/v1/places/"+room, "", &gt)
	box := e.boxes(fmt.Sprintf(`{"from":12,"to":12,"goes_to_id":%q}`, room))[0]["id"].(string)
	e.item(box, "Kettle")
	e.item(box, "Mug")
	e.must(http.StatusCreated, "POST", "/api/v1/lines", fmt.Sprintf(`{"place_id":%q,"lines":[{"name":"spoons","quantity":6}]}`, box), nil)

	for _, path := range []string{"/api/v1/boxes/number/12", "/api/v1/boxes/" + box} {
		var b obj
		e.must(http.StatusOK, "GET", path, "", &b)
		items, _ := b["items"].([]any)
		lines, _ := b["lines"].([]any)
		if b["box_number"] != float64(12) || len(items) != 2 || len(lines) != 1 {
			t.Fatalf("%s: %v", path, b)
		}
		g := b["goes_to"].(obj)
		if g["name"] != "Living room" || g["color"] != gt["color"] || g["color"] == nil {
			t.Fatalf("goes_to %v", g)
		}
		first := items[0].(obj)
		if first["name"] != "Kettle" || first["status"] != "packed" {
			t.Fatalf("items %v", items)
		}
	}
	if st := e.call("GET", "/api/v1/boxes/number/13", "", nil); st != http.StatusNotFound {
		t.Fatalf("missing number: %d", st)
	}
	if st := e.call("GET", "/api/v1/boxes/number/abc", "", nil); st != http.StatusNotFound {
		t.Fatalf("bad number: %d", st)
	}
	if st := e.call("GET", "/api/v1/boxes/"+room, "", nil); st != http.StatusNotFound {
		t.Fatalf("a room is not a box: %d", st)
	}
	// Putting lines into an empty box opens it.
	box2 := e.boxes(`{}`)[0]["id"].(string)
	e.must(http.StatusCreated, "POST", "/api/v1/lines", fmt.Sprintf(`{"place_id":%q,"lines":[{"name":"tape"}]}`, box2), nil)
	if st := e.getPlace(box2)["box_status"]; st != "open" {
		t.Fatalf("box with lines is %v", st)
	}
}

func TestRoomColoursComeFromThePalette(t *testing.T) {
	e := newEnv(t)
	seen := map[string]bool{}
	for i := 0; i < len(inventory.RoomColors); i++ {
		c, _ := e.getPlace(e.place("", "room", fmt.Sprintf("Room %d", i)))["color"].(string)
		if c == "" || seen[c] || !slices.Contains(inventory.RoomColors, c) {
			t.Fatalf("room %d got colour %q (seen %v)", i, c, seen)
		}
		seen[c] = true
	}
	// An explicit colour wins; the palette starts over when all are taken.
	var p obj
	e.must(http.StatusCreated, "POST", "/api/v1/places", fmt.Sprintf(`{"home_id":%q,"kind":"room","name":"Blue one","color":"blue"}`, e.homeID), &p)
	if p["color"] != "blue" {
		t.Fatalf("explicit colour %v", p["color"])
	}
	if c := e.getPlace(e.place("", "room", "Another"))["color"]; c == nil {
		t.Fatal("no colour once the palette is used up")
	}
	if c := e.getPlace(e.place("", "floor", "Upstairs"))["color"]; c != nil {
		t.Fatalf("a floor got colour %v", c)
	}
}

func TestPlacesCarryTheirMainPhoto(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Hall")
	shelf := e.place(room, "furniture", "Shelf")
	calls := 0
	old := inventory.PlacePhotoLoader
	t.Cleanup(func() { inventory.PlacePhotoLoader = old })
	inventory.PlacePhotoLoader = func(ctx context.Context, q sqlc.DBTX, hid string, ids []string) (map[string]inventory.ItemPhoto, error) {
		calls++
		if hid != e.hid {
			t.Errorf("household %s", hid)
		}
		return map[string]inventory.ItemPhoto{shelf: {LinkID: "l1", PhotoID: "p1", SHA256: "abc", Role: "place"}}, nil
	}
	var tree struct{ Places []obj }
	e.must(http.StatusOK, "GET", "/api/v1/homes/"+e.homeID+"/tree", "", &tree)
	if calls != 1 {
		t.Fatalf("%d photo lookups for a tree", calls)
	}
	for _, p := range tree.Places {
		switch p["id"] {
		case shelf:
			if ph, _ := p["photo"].(obj); ph["sha256"] != "abc" {
				t.Fatalf("shelf photo %v", p["photo"])
			}
		default:
			if p["photo"] != nil {
				t.Fatalf("room photo %v", p["photo"])
			}
		}
	}
	if ph, _ := e.getPlace(shelf)["photo"].(obj); ph["photo_id"] != "p1" {
		t.Fatalf("single place photo %v", ph)
	}
	var kids obj
	calls = 0
	e.must(http.StatusOK, "GET", "/api/v1/places/"+room, "", &kids)
	if calls != 2 { // the place itself, its children
		t.Fatalf("%d lookups for a place and its children", calls)
	}
	if ph, _ := kids["children"].([]any)[0].(obj)["photo"].(obj); ph["sha256"] != "abc" {
		t.Fatalf("child photo %v", kids["children"])
	}
}

func TestBoxesOfOtherHouseholdsAreInvisible(t *testing.T) {
	e := newEnv(t)
	other := newEnv(t)
	got := other.boxes(`{"count":2}`)
	if st := e.call("GET", "/api/v1/boxes/"+got[0]["id"].(string), "", nil); st != http.StatusNotFound {
		t.Fatalf("other household's box: %d", st)
	}
	if n := len(e.listBoxes()); n != 0 {
		t.Fatalf("%d boxes leaked", n)
	}
	e.member()
	if st := e.call("PUT", "/api/v1/me/packing-box", fmt.Sprintf(`{"box_id":%q}`, got[0]["id"]), nil); st != http.StatusUnprocessableEntity {
		t.Fatalf("packing another household's box: %d", st)
	}
	e.boxes(`{"count":1}`)
	if st := e.call("PUT", "/api/v1/me/packing-box", `{"number":1}`, nil); st != http.StatusOK {
		t.Fatalf("own box 1: %d", st)
	}
}
