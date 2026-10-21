// Package web serves the built frontend, which is embedded in the binary.
//
// `make build` copies frontend/dist into ./dist before compiling Go.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the embedded frontend build.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a valid literal path; this can't fail
	}
	return sub
}

const notBuiltMessage = "The Glowpod frontend isn't built into this binary yet. " +
	"Run `make build`, or `make dev` while developing.\n"

// Handler serves static files from fsys. Paths that aren't files and have no
// extension get index.html, so client-side routes survive a reload.
func Handler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "index.html" {
			serveIndex(w, r, fsys)
			return
		}
		if strings.HasPrefix(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// Vite puts a content hash in every asset file name.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		serveIndex(w, r, fsys)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	page, err := fs.ReadFile(fsys, "index.html")
	if errors.Is(err, fs.ErrNotExist) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(notBuiltMessage))
		return
	}
	if err != nil {
		http.Error(w, "couldn't read index.html", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(page)
}
