package middleware

import (
	"net/http"
	"os"
	"path/filepath"
)

const demoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"><rect width="200" height="200" fill="#f0f0f0"/><path d="M100 60c-22.1 0-40 17.9-40 40s17.9 40 40 40 40-17.9 40-40-17.9-40-40-40zm0 65c-13.8 0-25-11.2-25-25s11.2-25 25-25 25 11.2 25 25-11.2 25-25 25z" fill="#999"/><text x="100" y="170" text-anchor="middle" font-family="Arial" font-size="14" fill="#666">BANK</text></svg>`

func StaticFileServer(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))
		
		if _, err := os.Stat(path); err == nil {
			w.Header().Set("Cache-Control", "public, max-age=2592000")
			http.ServeFile(w, r, path)
			return
		}
		
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write([]byte(demoSVG))
	})
}
