// Package web embeds the built React front end (frontend/ → web/dist via
// `npm run build`).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Static returns the built front end, rooted at index.html.
func Static() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the dist directory is embedded at compile time
	}
	return sub
}
