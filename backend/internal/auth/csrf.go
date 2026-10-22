package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// CSRFHeader carries the session's CSRF token on every write.
const CSRFHeader = "X-CSRF-Token"

type csrfKey struct{}

// WithCSRFToken returns ctx carrying the session's CSRF token.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfKey{}, token)
}

// CSRFTokenFrom returns the token Require attached.
func CSRFTokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(csrfKey{}).(string)
	return t
}

// RequireCSRF guards requests that change things. They must come from this
// origin (when the browser says where they come from) and carry the
// session's token in CSRFHeader, which another site's page can't read.
// The SameSite=Strict cookie is a first line; this is the second.
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		want := CSRFTokenFrom(r.Context())
		if !sameOrigin(r) || want == "" || !equal(r.Header.Get(CSRFHeader), want) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
				"code":    "csrf",
				"message": "This request didn't come from Glowpod. Reload the page and try again.",
			}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects a cross-site Origin. Browsers always send Origin on
// writes; tools like curl don't, and are left to the token check.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}
