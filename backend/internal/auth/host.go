package auth

import (
	"net"
	"net/http"
	"strings"
)

// LoopbackHosts lists the Host header values a loopback listener on port
// answers to.
func LoopbackHosts(port string) []string {
	return []string{
		net.JoinHostPort("127.0.0.1", port),
		net.JoinHostPort("localhost", port),
		net.JoinHostPort("::1", port),
	}
}

// RequireHost rejects requests whose Host header isn't one we serve. This
// blocks DNS-rebinding pages from reaching the local API.
func RequireHost(allowed []string) func(http.Handler) http.Handler {
	set := make(map[string]struct{}, len(allowed))
	for _, h := range allowed {
		set[strings.ToLower(h)] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := set[strings.ToLower(r.Host)]; !ok {
				http.Error(w, "Unknown host. Open Glowpod from the link in your terminal.", http.StatusMisdirectedRequest)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
