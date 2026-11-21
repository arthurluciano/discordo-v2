// Package web serves the web app (apps/web's build, embedded into the binary).
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist holds the build; the Dockerfile copies it in before compiling. Locally it is empty and the
// app is served by Vite's dev server instead.
//
//go:embed all:dist
var dist embed.FS

// Handler serves the app's files, and index.html for any other path (the app routes on the client).
// It returns nil when the binary was built without the app.
func Handler() http.Handler {
	files, _ := fs.Sub(dist, "dist")
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil
	}
	static := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" && path != "index.html" {
			if _, err := fs.Stat(files, path); err == nil {
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // Vite puts a content hash in these names
				}
				static.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache") // always pick up a new deploy
		w.Write(index)
	})
}
