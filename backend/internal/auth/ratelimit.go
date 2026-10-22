package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// limiter allows Max attempts per key within Window. It slows down
// password and token guessing; it isn't a substitute for strong secrets.
type limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter(maxAttempts int, window time.Duration) *limiter {
	return &limiter{max: maxAttempts, window: window, now: time.Now, hits: map[string][]time.Time{}}
}

// allow records an attempt and reports whether it's within the limit.
func (l *limiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) > 10000 {
		l.hits = map[string][]time.Time{} // bounded memory beats perfect accounting
	}
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

// clientIP is who is asking. Behind trusted proxies (an ingress
// controller) it's the nearest address in X-Forwarded-For that isn't one of
// them; otherwise the connection's own address, since anyone can send that
// header. Used for rate limits and sign-in logs.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !within(addr.Unmap(), trusted) {
		return host
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" {
			continue
		}
		a, err := netip.ParseAddr(hop)
		if err != nil {
			return hop // not an address: the proxy passed it on as it came
		}
		if !within(a.Unmap(), trusted) {
			return a.Unmap().String()
		}
	}
	return host
}

func within(a netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
