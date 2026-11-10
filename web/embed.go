// Package web embeds the built frontend.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:embed placeholder
var placeholder embed.FS

// Dist returns the built frontend rooted at its index.html. Before the
// frontend is built (`make build`), it returns a one-page placeholder, so Go
// builds and tests work on a fresh checkout.
func Dist() fs.FS {
	if app, err := fs.Sub(dist, "dist/app"); err == nil {
		if _, err := fs.Stat(app, "index.html"); err == nil {
			return app
		}
	}
	page, err := fs.Sub(placeholder, "placeholder")
	if err != nil {
		panic(err)
	}
	return page
}
