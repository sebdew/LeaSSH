package main

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/leassh-dev/leassh/web"
)

// webHandler sert l'interface embarquée; toute route inconnue retombe sur index.html (SPA).
func webHandler() http.Handler {
	root := web.Dist()
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "interface non construite (make build)", http.StatusNotFound)
		})
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			r.URL.Path = "/" // SPA
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
