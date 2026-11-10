// Package httpapi builds the HTTP handler: the JSON API and the embedded app.
package httpapi

import (
	"io/fs"
	"net/http"
	"path"
)

// Routes registers a feature's endpoints on mux, using patterns such as
// "GET /api/v1/items/{id}". Each feature package exposes one and main passes
// it to New. Database access arrives later as an argument of the feature's
// own Routes function (see J1.3); httpapi itself imports no db package.
type Routes func(mux *http.ServeMux)

// New returns the handler serving /api/* and the single-page app in app.
// Paths that match no file in app get index.html so client routes work.
func New(version, buildTime string, app fs.FS, routes ...Routes) http.Handler {
	mux := http.NewServeMux()
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
	return chain(mux, requestID, logRequests, gzipAPI, recoverPanics, securityHeaders)
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
