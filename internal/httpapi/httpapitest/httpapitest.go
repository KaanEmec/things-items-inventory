package httpapitest

import (
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// Server starts the full handler (middleware included) over HTTP for
// tests in any package. Seam for J1.3: build the test database in the test
// and pass it to the feature's Routes before calling this.
func Server(t testing.TB, routes ...httpapi.Routes) *httptest.Server {
	t.Helper()
	app := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}
	srv := httptest.NewServer(httpapi.New("test", "test", app, routes...))
	t.Cleanup(srv.Close)
	return srv
}

// Do sends a request to srv; body (may be empty) is sent as JSON. It returns
// the status and the response body.
func Do(t testing.TB, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}
