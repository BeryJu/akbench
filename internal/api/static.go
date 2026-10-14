package api

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the web UI's built assets (web/ui's `vite build`
// output) from fsys, falling back to index.html for any path that isn't a
// file - those are client-side routes (/deployments/foo, /shell, ...) the
// SPA's own router resolves.
//
// Unauthenticated: the bundle holds no secrets, and the SPA has to load
// before it can ask anyone to log in.
//
// Every response carries csp (spaCSP), not just index.html: Monaco's worker
// scripts are governed by their own response's policy.
func spaHandler(fsys fs.FS, csp string) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
				// Vite content-hashes everything under assets/, so those can
				// be cached forever; anything else must be revalidated so a
				// deploy is picked up on the next load.
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		// A missing hashed asset is a stale tab after a deploy, not a client
		// route - answering it with index.html would only produce a confusing
		// MIME-type error in the browser.
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, fsys, "index.html")
	})
}
