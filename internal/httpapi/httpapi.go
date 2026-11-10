// Package httpapi builds the HTTP handler: the JSON API and the embedded app.
package httpapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
)

// New returns the handler serving /api/* and the single-page app in app.
// Paths that match no file in app get index.html so client routes work.
func New(version, buildTime string, app fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version":   version,
			"buildTime": buildTime,
		})
	})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", spa(app))
	return mux
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
