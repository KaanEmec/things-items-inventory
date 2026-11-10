package server

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
)

// First-run setup creates a starter home with a few rooms in the same
// transaction as the household.
func TestSetupSeedsStarterHome(t *testing.T) {
	d := dbtest.New(t)
	srv := httpapitest.Server(t, Routes(Features(d, config.Config{}))...)
	jar, _ := cookiejar.New(nil)
	c := *srv.Client()
	c.Jar = jar
	do := func(method, path, body string) (int, []byte) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := res.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return res.StatusCode, []byte(b.String())
	}
	if s, b := do("POST", "/api/v1/setup", `{"name":"Kaan","email":"k@example.com","password":"correct horse battery","household_name":"Emec"}`); s != 201 {
		t.Fatalf("setup %d %s", s, b)
	}
	s, b := do("GET", "/api/v1/homes", "")
	var homes struct{ Data []struct{ ID string } }
	if s != 200 || json.Unmarshal(b, &homes) != nil || len(homes.Data) != 1 {
		t.Fatalf("homes %d %s", s, b)
	}
	s, b = do("GET", "/api/v1/homes/"+homes.Data[0].ID+"/tree", "")
	var tree struct{ Places []struct{ Kind string } }
	if s != 200 || json.Unmarshal(b, &tree) != nil || len(tree.Places) < 3 || tree.Places[0].Kind != "room" {
		t.Fatalf("tree %d %s", s, b)
	}
}
