package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db/dbtest"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
)

// TestMainFlowsWorkWithoutAI (J5.6): with no AI profile at all, the main
// flows save normally and the AI controls answer "not set up" instead of failing.
func TestMainFlowsWorkWithoutAI(t *testing.T) {
	d := dbtest.New(t)
	srv := httpapitest.Server(t, Routes(Features(d, config.Config{DataDir: t.TempDir()}))...)
	jar, _ := cookiejar.New(nil)
	c := *srv.Client()
	c.Jar = jar
	call := func(want int, method, path, ctype string, body []byte) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
		if ctype == "" {
			ctype = "application/json"
		}
		req.Header.Set("Content-Type", ctype)
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d (want %d) %s", method, path, res.StatusCode, want, b)
		}
		return b
	}
	js := func(want int, method, path, body string) map[string]any {
		t.Helper()
		var out map[string]any
		_ = json.Unmarshal(call(want, method, path, "", []byte(body)), &out)
		return out
	}
	js(201, "POST", "/api/v1/setup", `{"name":"Kaan","email":"k@example.com","password":"correct horse battery","household_name":"Emec"}`)

	// A place, an item by hand, a second item moved into a box.
	homes := js(200, "GET", "/api/v1/homes", "")["data"].([]any)
	home := homes[0].(map[string]any)["id"].(string)
	room := js(201, "POST", "/api/v1/places", `{"home_id":"`+home+`","kind":"room","name":"study"}`)["id"].(string)
	item := js(201, "POST", "/api/v1/items", `{"name":"Desk lamp","place_id":"`+room+`"}`)["id"].(string)

	// A photo on the item (no AI to read it).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "lamp.png")
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	fw.Write(img.Bytes())
	mw.WriteField("role", "item")
	mw.WriteField("item_id", item)
	mw.Close()
	call(201, "POST", "/api/v1/photos", mw.FormDataContentType(), buf.Bytes())

	// Quick add: rows save through the normal items/lines endpoints.
	js(201, "POST", "/api/v1/items", `{"name":"HDMI cable","place_id":"`+room+`"}`)
	call(201, "POST", "/api/v1/lines", "", []byte(`{"place_id":"`+room+`","lines":[{"name":"screws"}]}`))

	// Pack a box.
	boxes := js(201, "POST", "/api/v1/boxes", `{}`)["data"].([]any)
	box := boxes[0].(map[string]any)["id"].(string)
	js(200, "POST", "/api/v1/items/"+item+"/move", `{"place_id":"`+box+`"}`)

	// Search finds what was saved.
	res := js(200, "GET", "/api/v1/search?q="+url.QueryEscape("HDMI"), "")
	if !strings.Contains(toJSON(res), "HDMI cable") {
		t.Fatalf("search: %v", res)
	}

	// AI controls report "not set up" (409), never a server error.
	for _, p := range []struct{ path, body string }{
		{"/api/v1/items/ai/read-waiting", "{}"},
		{"/api/v1/ai/complete-rows", `{"rows":[{"id":"1","name":"wh1000xm4"}]}`},
	} {
		got := toJSON(js(409, "POST", p.path, p.body))
		if !strings.Contains(got, "not_set_up") {
			t.Fatalf("%s: %s", p.path, got)
		}
	}
	st := toJSON(js(200, "GET", "/api/v1/settings/ai", ""))
	if strings.Contains(st, `"token_set":true`) || strings.Contains(st, `"model":"`+"x") {
		t.Fatalf("unexpected profile: %s", st)
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
