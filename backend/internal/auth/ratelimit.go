package auth

import (
	"net"
	"net/http"
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

// clientIP is the connection's address. Behind a proxy every request
// shares the proxy's address, so limits are also kept per username.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
