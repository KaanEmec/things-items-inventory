package inventory_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/id"
)

type ovResp struct {
	Counts    map[string]int64 `json:"counts"`
	Places    int64            `json:"places"`
	Recent    []obj            `json:"recent"`
	NeedsLook []struct {
		Kind  string `json:"kind"`
		Count int64  `json:"count"`
		Rows  []obj  `json:"rows"`
	} `json:"needs_look"`
	Moving *struct {
		Total    int64            `json:"total"`
		ByStatus map[string]int64 `json:"by_status"`
		Rooms    []obj            `json:"rooms"`
	} `json:"moving"`
}

func (e *env) listCount(q string) int64 {
	e.t.Helper()
	var p obj
	e.must(http.StatusOK, "GET", "/api/v1/items?limit=200&"+q, "", &p)
	return int64(len(p["data"].([]any)))
}

func TestOverviewEmpty(t *testing.T) {
	e := newEnv(t)
	var o ovResp
	e.must(http.StatusOK, "GET", "/api/v1/overview", "", &o)
	if o.Counts["all"] != 0 || o.Places != 0 || o.Moving != nil || len(o.Recent) != 0 || len(o.NeedsLook) != 4 {
		t.Fatalf("%+v", o)
	}
	for _, g := range o.NeedsLook {
		if g.Count != 0 || g.Rows == nil {
			t.Fatalf("group %+v", g)
		}
	}
}

func TestOverviewCountsMatchLists(t *testing.T) {
	e := newEnv(t)
	room := e.place("", "room", "Office")
	desk := e.place(room, "furniture", "Desk")
	e.must(http.StatusOK, "PATCH", "/api/v1/places/"+desk, `{"daily_use":true}`, nil)
	now := time.Now().UTC()
	soon := now.AddDate(0, 0, 20).Format("2006-01-02")
	later := now.AddDate(0, 0, 200).Format("2006-01-02")
	past := now.AddDate(-1, 0, 0).Format("2006-01-02")
	mk := func(body string) string {
		var it obj
		e.must(http.StatusCreated, "POST", "/api/v1/items", body, &it)
		return it["id"].(string)
	}
	old := mk(fmt.Sprintf(`{"name":"Old","place_id":%q}`, desk))
	mk(fmt.Sprintf(`{"name":"Fresh","place_id":%q}`, desk))
	mk(fmt.Sprintf(`{"name":"Soon","place_id":%q,"warranty":{"end":%q}}`, room, soon))
	mk(fmt.Sprintf(`{"name":"Later","status":"packed","warranty":{"end":%q}}`, later))
	mk(fmt.Sprintf(`{"name":"Past","status":"to_sell","warranty":{"end":%q}}`, past))
	mk(`{"draft":true}`)
	gone := mk(`{"name":"Gone","status":"to_sell"}`)
	e.must(http.StatusOK, "DELETE", "/api/v1/items/"+gone, "", nil)
	long := now.AddDate(0, 0, -300).Format("2006-01-02T15:04:05.000Z")
	if _, err := e.d.Write.Exec(`UPDATE items SET last_seen_at = ? WHERE id = ?`, long, old); err != nil {
		t.Fatal(err)
	}
	// A failed AI job, a failed derivative job (ignored), and one box.
	for _, k := range []string{"draft_item", "photo_derivatives"} {
		if _, err := e.d.Write.Exec(`INSERT INTO jobs (id, household_id, created_at, updated_at, kind, run_after, state, last_error, item_id)
			VALUES (?, ?, ?, ?, ?, ?, 'failed', 'boom', ?)`, id.New(), e.hid, stamp, stamp, k, stamp, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.d.Write.Exec(`INSERT INTO places (id, household_id, created_at, updated_at, kind, name, path, lineage, box_number, box_status, goes_to_id)
		VALUES (?, ?, ?, ?, 'box', 'Box 1', 'Box 1', 'x', 1, 'packed', ?)`, id.New(), e.hid, stamp, stamp, room); err != nil {
		t.Fatal(err)
	}

	var o ovResp
	e.must(http.StatusOK, "GET", "/api/v1/overview", "", &o)
	for name, q := range map[string]string{
		"all": "", "drafts": "draft=1", "packed": "status=packed", "to_sell": "status=to_sell", "in_warranty": "warranty=active",
	} {
		if got := e.listCount(q); o.Counts[name] != got {
			t.Errorf("%s: overview %d, list %d", name, o.Counts[name], got)
		}
	}
	want := map[string]int64{"drafts": 1, "warranty_ending": e.listCount("warranty=ending"), "unseen": 1, "failed_jobs": 1}
	for _, g := range o.NeedsLook {
		if g.Count != want[g.Kind] || int64(len(g.Rows)) != g.Count {
			t.Errorf("group %s: count %d rows %d, want %d", g.Kind, g.Count, len(g.Rows), want[g.Kind])
		}
	}
	if want["warranty_ending"] != 1 || o.Counts["in_warranty"] != 2 || o.Counts["all"] != 6 {
		t.Errorf("fixture: %+v", o.Counts)
	}
	if o.Places != 2 || len(o.Recent) == 0 {
		t.Errorf("places %d recent %d", o.Places, len(o.Recent))
	}
	if o.Moving == nil || o.Moving.Total != 1 || o.Moving.ByStatus["packed"] != 1 || len(o.Moving.Rooms) != 1 || o.Moving.Rooms[0]["name"] != "Office" {
		t.Errorf("moving %+v", o.Moving)
	}
	// Viewers may read it.
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/overview", nil)
	req.Header.Set("X-Role", "viewer")
	if res, err := e.srv.Client().Do(req); err != nil || res.StatusCode != http.StatusOK {
		t.Errorf("viewer: %v %v", err, res)
	}
}

func TestOverviewRecentLimit(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 12; i++ {
		e.item("", fmt.Sprintf("Thing %d", i))
	}
	var o ovResp
	e.must(http.StatusOK, "GET", "/api/v1/overview", "", &o)
	if len(o.Recent) != 8 {
		t.Fatalf("recent %d", len(o.Recent))
	}
}
