package api

import (
	"net/http"

	"glowpod/internal/web"
)

func securityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			h.Set("Content-Security-Policy", web.ContentSecurityPolicy(""))
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			next.ServeHTTP(w, r)
		})
	}
}
