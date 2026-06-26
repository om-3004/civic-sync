package main

import (
	"io/fs"
	"net/http"

	civicstatic "github.com/civic-sync/civic-sync/web/static"
)

// RegisterLandingPage wires the embedded static file server onto mux at "GET /".
// All responses include Cache-Control: public, max-age=3600.
func RegisterLandingPage(mux *http.ServeMux) {
	sub, err := fs.Sub(civicstatic.StaticFiles, ".")
	if err != nil {
		panic("static: failed to create sub-filesystem: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("GET /", cacheControl(fileServer))
}

// cacheControl wraps h and sets Cache-Control: public, max-age=3600 on every response.
func cacheControl(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}
