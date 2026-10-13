// Package web embeds the built client and serves it as an SPA.
package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

func init() {
	// Go's built-in table lacks the PWA manifest type; browsers warn on text/plain.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// SPAHandler serves embedded static files, falling back to index.html for
// client-side routes. /api, /files, /avatars are mounted before this.
func SPAHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			// Client-side route: serve the app shell.
			r.URL.Path = "/"
		}
		// Hashed assets can cache forever; index.html must revalidate.
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}
