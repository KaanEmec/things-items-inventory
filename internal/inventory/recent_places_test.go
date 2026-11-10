package inventory_test

import (
	"net/http"
	"testing"
	"time"
)

func TestRecentPlaces(t *testing.T) {
	e := newEnv(t)
	var places []string
	var first obj
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"} {
		p := e.place("", "room", n)
		places = append(places, p)
		it := e.item(p, "thing "+n)
		if n == "A" {
			first = it
		}
		time.Sleep(3 * time.Millisecond)
	}
	// Moving an item back into A makes A the newest again; A appears once.
	e.must(http.StatusOK, "POST", "/api/v1/items/"+first["id"].(string)+"/move", `{"place_id":"`+places[1]+`"}`, nil)
	time.Sleep(3 * time.Millisecond)
	e.must(http.StatusOK, "POST", "/api/v1/items/"+first["id"].(string)+"/move", `{"place_id":"`+places[0]+`"}`, nil)

	var out struct {
		Data []obj `json:"data"`
	}
	e.must(http.StatusOK, "GET", "/api/v1/me/recent-places", "", &out)
	if len(out.Data) != 8 {
		t.Fatalf("got %d places, want 8", len(out.Data))
	}
	want := []string{"A", "B", "J", "I", "H", "G", "F", "E"} // B got the item on its way back to A
	for i, p := range out.Data {
		if p["name"] != want[i] {
			t.Fatalf("place %d is %v, want %s (all: %v)", i, p["name"], want[i], out.Data)
		}
	}
}
