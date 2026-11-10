// Package httpapi builds the HTTP handler: the JSON API and the embedded app.
package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Routes registers a feature's endpoints on mux, using patterns such as
// "GET /api/v1/items/{id}". Each feature package exposes one and main passes
// it to New. Database access arrives later as an argument of the feature's
// own Routes function (see J1.3); httpapi itself imports no db package.
type Routes func(mux *Mux)

// Mux is an http.ServeMux that remembers the patterns registered on it (so
// tests can walk every route) and lets a feature add middleware for all
// /api/ requests with Use.
type Mux struct {
	*http.ServeMux
	patterns []string
	mws      []func(http.Handler) http.Handler
}

// Handle registers h for pattern and records the pattern.
func (m *Mux) Handle(pattern string, h http.Handler) {
	m.patterns = append(m.patterns, pattern)
	m.ServeMux.Handle(pattern, h)
}

// HandleFunc registers f for pattern and records the pattern.
func (m *Mux) HandleFunc(pattern string, f func(http.ResponseWriter, *http.Request)) {
	m.Handle(pattern, http.HandlerFunc(f))
}

// Use adds middleware that wraps every /api/ request. The first one added is
// the outermost.
func (m *Mux) Use(mw func(http.Handler) http.Handler) { m.mws = append(m.mws, mw) }

// Patterns returns every pattern the given routes register, in order.
func Patterns(routes ...Routes) []string {
	m := &Mux{ServeMux: http.NewServeMux()}
	for _, r := range routes {
		r(m)
	}
	return m.patterns
}

// New returns the handler serving /api/* and the single-page app in app.
// Paths that match no file in app get index.html so client routes work.
func New(version, buildTime string, app fs.FS, routes ...Routes) http.Handler {
	mux := &Mux{ServeMux: http.NewServeMux()}
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{
			"version":   version,
			"buildTime": buildTime,
		})
	})
	for _, r := range routes {
		r(mux)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, NotFound("No such endpoint."))
	})
	mux.Handle("/", spa(app))
	api := chain(mux.ServeMux, mux.mws...)
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return chain(root, requestID, logRequests, gzipAPI, recoverPanics, securityHeaders)
}

func spa(app fs.FS) http.Handler {
	files := http.FileServerFS(app)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(r.URL.Path)[1:]
		if name == "" {
			name = "."
		}
		if info, err := fs.Stat(app, name); err != nil || (info.IsDir() && name != ".") {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
