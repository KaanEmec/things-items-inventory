package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var app = fstest.MapFS{
	"index.html":    {Data: []byte("<html>app</html>")},
	"assets/app.js": {Data: []byte("console.log(1)")},
}

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	New("v1", "now", app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealth(t *testing.T) {
	rec := get(t, "/api/health")
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || body["version"] != "v1" || body["buildTime"] != "now" {
		t.Fatalf("unexpected response: %d %v", rec.Code, body)
	}
}

func TestUnknownAPIIsNotFound(t *testing.T) {
	if rec := get(t, "/api/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestStaticAndFallback(t *testing.T) {
	if rec := get(t, "/assets/app.js"); !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("asset not served: %q", rec.Body.String())
	}
	for _, p := range []string{"/", "/items/42"} {
		if rec := get(t, p); !strings.Contains(rec.Body.String(), "<html>app</html>") {
			t.Fatalf("%s did not serve index.html: %q", p, rec.Body.String())
		}
	}
}
