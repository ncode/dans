// Package webconsole serves the embedded browser application.
package webconsole

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var assets embed.FS

// Handler serves public console assets; no API requests are handled here.
func Handler() http.Handler {
	files, err := fs.Sub(assets, "dist")
	if err != nil {
		panic("embedded console directory missing")
	}
	server := http.StripPrefix("/console/", http.FileServerFS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/console/" && !strings.HasPrefix(r.URL.Path, "/console/assets/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-cache")
		if strings.HasPrefix(r.URL.Path, "/console/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		server.ServeHTTP(w, r)
	})
}

// Alongside exposes only the console routes outside the protected API handler.
func Alongside(api http.Handler) http.Handler {
	console := Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && (r.URL.Path == "/" || r.URL.Path == "/console") {
			http.Redirect(w, r, "/console/", http.StatusFound)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/console/") {
			console.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}
