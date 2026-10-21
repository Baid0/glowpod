package metrics

import (
	"context"
	"sync"
	"time"
)

// PromCache remembers, per identity, which Prometheus to use (or that there
// is none) so discovery doesn't run on every Overview refresh.
type PromCache struct {
	TTL time.Duration

	mu      sync.Mutex
	entries map[string]promEntry
	now     func() time.Time
}

type promEntry struct {
	prom Prometheus
	at   time.Time
}

// NewPromCache returns a cache that rediscovers after ttl.
func NewPromCache(ttl time.Duration) *PromCache {
	return &PromCache{TTL: ttl, entries: map[string]promEntry{}, now: time.Now}
}

// Get returns the cached Prometheus for key, calling find when the entry is
// missing or old. A nil Prometheus means none was found. Errors aren't cached.
func (c *PromCache) Get(ctx context.Context, key string, find func(context.Context) (Prometheus, error)) (Prometheus, error) {
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok && c.now().Sub(e.at) < c.TTL {
		return e.prom, nil
	}
	p, err := find(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[key] = promEntry{prom: p, at: c.now()}
	c.mu.Unlock()
	return p, nil
}
