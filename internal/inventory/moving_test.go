package inventory_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestUnpackBoxToUnpacked(t *testing.T) {
	e := newEnv(t)
	e.member()
	room := e.place("", "room", "Office")
	shelf := e.place(room, "furniture", "Shelf")
	b := e.boxes(`{}`)[0]["id"].(string)
	a, c := e.item(b, "Lamp"), e.item(b, "Cable")
	ids := fmt.Sprintf("[%q]", b)
	e.must(http.StatusOK, "POST", "/api/v1/boxes/bulk", `{"ids":`+ids+`,"handling":["fragile"]}`, nil)
	// An open box cannot be unpacked; closing it and marking it arrived can.
	if st := e.call("POST", "/api/v1/boxes/"+b+"/unpack", `{}`, nil); st != http.StatusConflict {
		t.Fatalf("open box: %d", st)
	}
	e.must(http.StatusOK, "POST", "/api/v1/boxes/"+b+"/close", `{}`, nil)
	e.must(http.StatusOK, "POST", "/api/v1/boxes/bulk", `{"ids":`+ids+`,"box_status":"arrived"}`, nil)
	e.must(http.StatusOK, "POST", "/api/v1/boxes/"+b+"/unpack", fmt.Sprintf(`{"moves":[{"item_id":%q,"place_id":null}]}`, c["id"]), nil)
	if got := e.getItem(c["id"].(string)); got["place"] != nil {
		t.Fatalf("not found item has a place: %v", got["place_id"])
	}
	var box obj
	e.must(http.StatusOK, "GET", "/api/v1/boxes/"+b, "", &box)
	if box["box_status"] != "arrived" {
		t.Fatalf("status %v with an item left", box["box_status"])
	}
	e.must(http.StatusOK, "POST", "/api/v1/boxes/"+b+"/unpack", fmt.Sprintf(`{"rest_to":%q}`, shelf), &box)
	if box["box_status"] != "unpacked" {
		t.Fatalf("status %v, want unpacked", box["box_status"])
	}
	if got := e.getItem(a["id"].(string)); got["place"].(obj)["id"] != shelf {
		t.Fatalf("lamp in %v", got["place_id"])
	}
	var ov struct {
		NeedsLook []struct {
			Kind  string
			Count int
		} `json:"needs_look"`
	}
	e.must(http.StatusOK, "GET", "/api/v1/overview", "", &ov)
	for _, g := range ov.NeedsLook {
		if g.Kind == "not_found" && g.Count != 1 {
			t.Fatalf("not_found %d", g.Count)
		}
	}
}

func TestArchiveHomeRefusedWhileItHoldsThings(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Hall")
	it := e.item(room, "Coat")
	if st := e.call("PATCH", "/api/v1/homes/"+e.homeID, `{"archived":true}`, nil); st != http.StatusConflict {
		t.Fatalf("archive with items: %d", st)
	}
	e.must(http.StatusOK, "DELETE", "/api/v1/items/"+it["id"].(string), "", nil)
	e.must(http.StatusOK, "PATCH", "/api/v1/homes/"+e.homeID, `{"archived":true}`, nil)
}
