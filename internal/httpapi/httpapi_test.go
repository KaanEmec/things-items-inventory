package httpapi_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/httpapi/httpapitest"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var app = fstest.MapFS{
	"index.html":           {Data: []byte("<html>app</html>")},
	"assets/app.js":        {Data: []byte("console.log(1)")},
	"sw.js":                {Data: []byte("self.skipWaiting")},
	"manifest.webmanifest": {Data: []byte("{}")},
	"icons/icon-192.png":   {Data: []byte("png")},
}

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	httpapi.New("v1", "now", app).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
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

type thing struct {
	Name string `json:"name"`
}

func sampleRoutes(mux *httpapi.Mux) {
	mux.HandleFunc("POST /api/v1/sample", func(w http.ResponseWriter, r *http.Request) {
		var in thing
		if err := httpapi.DecodeJSON(w, r, &in); err != nil {
			httpapi.WriteError(w, err)
			return
		}
		if in.Name == "" {
			httpapi.WriteError(w, httpapi.Invalid(map[string]string{"name": "Name is required."}))
			return
		}
		httpapi.WriteJSON(w, http.StatusCreated, in)
	})
	mux.HandleFunc("GET /api/v1/sample", func(w http.ResponseWriter, r *http.Request) {
		_, limit, err := httpapi.ParsePaging(r)
		if err != nil {
			httpapi.WriteError(w, err)
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.NewPage([]int{limit}, ""))
	})
	mux.HandleFunc("GET /api/v1/page-next", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, httpapi.NewPage([]int(nil), "abc"))
	})
	mux.HandleFunc("GET /api/v1/jpeg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(bytes.Repeat([]byte{0xff, 0xd8, 0xff}, 100))
	})
	mux.HandleFunc("GET /api/v1/panic", func(http.ResponseWriter, *http.Request) { panic("boom secret") })
	mux.HandleFunc("GET /api/v1/plain-error", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteError(w, io.ErrClosedPipe)
	})
}

func errBody(t *testing.T, body string) httpapi.Error {
	t.Helper()
	var env struct{ Error httpapi.Error }
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not the error shape: %q", body)
	}
	return env.Error
}

func TestSampleEndpoint(t *testing.T) {
	srv := httpapitest.Server(t, sampleRoutes)

	status, body := httpapitest.Do(t, srv, "POST", "/api/v1/sample", `{"name":"lamp"}`)
	if status != 201 || strings.TrimSpace(body) != `{"name":"lamp"}` {
		t.Fatalf("success: %d %q", status, body)
	}

	status, body = httpapitest.Do(t, srv, "POST", "/api/v1/sample", `{"name":""}`)
	if e := errBody(t, body); status != 400 || e.Code != "bad_request" || e.Fields["name"] == "" {
		t.Fatalf("validation: %d %q", status, body)
	}

	status, body = httpapitest.Do(t, srv, "POST", "/api/v1/sample", `{"name":"a","extra":1}`)
	if e := errBody(t, body); status != 400 || e.Code != "bad_request" {
		t.Fatalf("unknown field: %d %q", status, body)
	}

	status, body = httpapitest.Do(t, srv, "POST", "/api/v1/sample", `{"name":"`+strings.Repeat("x", 2<<20)+`"}`)
	if e := errBody(t, body); status != 413 || e.Code != "too_large" {
		t.Fatalf("too large: %d %.100q", status, body)
	}

	status, body = httpapitest.Do(t, srv, "GET", "/api/v1/panic", "")
	if e := errBody(t, body); status != 500 || e.Code != "internal" || strings.Contains(body, "boom") {
		t.Fatalf("panic: %d %q", status, body)
	}

	status, body = httpapitest.Do(t, srv, "GET", "/api/v1/plain-error", "")
	if status != 500 || strings.Contains(body, "closed pipe") {
		t.Fatalf("plain error leaked: %d %q", status, body)
	}

	status, body = httpapitest.Do(t, srv, "GET", "/api/v1/missing", "")
	if e := errBody(t, body); status != 404 || e.Code != "not_found" {
		t.Fatalf("not found: %d %q", status, body)
	}
}

func TestPaging(t *testing.T) {
	srv := httpapitest.Server(t, sampleRoutes)
	if _, body := httpapitest.Do(t, srv, "GET", "/api/v1/sample", ""); !strings.Contains(body, `"data":[50]`) || !strings.Contains(body, `"next_cursor":null`) {
		t.Fatalf("default: %q", body)
	}
	if status, body := httpapitest.Do(t, srv, "GET", "/api/v1/sample?limit=201", ""); status != 400 || errBody(t, body).Fields["limit"] == "" {
		t.Fatalf("max: %d %q", status, body)
	}
}

func TestMiddlewareHeadersAndGzip(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	httpapi.New("v", "b", app).ServeHTTP(rec, req)
	h := rec.Header()
	if h.Get("X-Request-ID") == "" || h.Get("Content-Encoding") != "gzip" ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("headers: %v", h)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	if !strings.Contains(string(b), `"version":"v"`) {
		t.Fatalf("body: %q", b)
	}
	if rec := get(t, "/"); rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("app should not be gzipped")
	}
}

func TestPageNextCursor(t *testing.T) {
	srv := httpapitest.Server(t, sampleRoutes)
	if _, body := httpapitest.Do(t, srv, "GET", "/api/v1/page-next", ""); !strings.Contains(body, `"data":[]`) || !strings.Contains(body, `"next_cursor":"abc"`) {
		t.Fatalf("next: %q", body)
	}
}

func TestGzipSkipsImages(t *testing.T) {
	for path, want := range map[string]string{"/api/v1/jpeg": "", "/api/v1/sample": "gzip"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		httpapi.New("v", "b", app, sampleRoutes).ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Encoding"); got != want {
			t.Errorf("%s: Content-Encoding %q, want %q", path, got, want)
		}
		if want == "" && rec.Body.Len() != 300 {
			t.Errorf("%s: body changed (%d bytes)", path, rec.Body.Len())
		}
	}
}

func TestLogsHideSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	srv := httpapitest.Server(t, sampleRoutes)
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/sample", strings.NewReader(`{"name":"body-secret"}`))
	req.Header.Set("Authorization", "Bearer auth-secret")
	req.Header.Set("Cookie", "session=cookie-secret")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	logs := buf.String()
	if !strings.Contains(logs, "method=POST") || !strings.Contains(logs, "status=201") || !strings.Contains(logs, "request_id=") {
		t.Fatalf("missing request log: %q", logs)
	}
	for _, s := range []string{"auth-secret", "cookie-secret", "body-secret"} {
		if strings.Contains(logs, s) {
			t.Fatalf("logs contain %q: %s", s, logs)
		}
	}
}

func TestPWAHeaders(t *testing.T) {
	cases := []struct {
		path, cache, ctype string
		swAllowed          bool
	}{
		{"/", "no-cache", "text/html", false},
		{"/items/42", "no-cache", "text/html", false},
		{"/assets/missing.js", "no-cache", "text/html", false}, // falls back to index.html: never immutable
		{"/sw.js", "no-cache", "text/javascript", true},
		{"/manifest.webmanifest", "no-cache", "application/manifest+json", false},
		{"/assets/app.js", "public, max-age=31536000, immutable", "javascript", false},
		{"/icons/icon-192.png", "public, max-age=86400", "image/png", false},
	}
	for _, c := range cases {
		rec := get(t, c.path)
		if got := rec.Header().Get("Cache-Control"); got != c.cache {
			t.Errorf("%s: Cache-Control %q, want %q", c.path, got, c.cache)
		}
		if got := rec.Header().Get("Content-Type"); !strings.Contains(got, c.ctype) {
			t.Errorf("%s: Content-Type %q, want %q", c.path, got, c.ctype)
		}
		if got := rec.Header().Get("Service-Worker-Allowed") == "/"; got != c.swAllowed {
			t.Errorf("%s: Service-Worker-Allowed set = %v", c.path, got)
		}
	}
}

func TestCSPAllowsWorkerAndManifest(t *testing.T) {
	csp := get(t, "/").Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "worker-src 'self'", "manifest-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
}

func TestAPINotCacheable(t *testing.T) {
	if cc := get(t, "/api/health").Header().Get("Cache-Control"); strings.Contains(cc, "immutable") || strings.Contains(cc, "max-age") {
		t.Fatalf("API got Cache-Control %q", cc)
	}
}
