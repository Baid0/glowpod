// Package api wires Glowpod's HTTP routes and middleware.
//
// API routes live under /api/v1/clusters/{cluster}/... so a second cluster can
// be added without changing URLs. Everything outside /api is the frontend.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Options configures the router.
type Options struct {
	// Frontend serves the single-page app for every non-API path.
	Frontend http.Handler
}

// NewRouter returns the root HTTP handler.
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/healthz", healthz)
	r.Handle("/api/*", http.HandlerFunc(apiNotFound))
	if opts.Frontend != nil {
		r.Handle("/*", opts.Frontend)
	}
	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "There's nothing at this address.")
}

// errorBody is the JSON shape of every API error.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]errorBody{"error": {Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the status line is already sent; nothing useful to do on error
}
