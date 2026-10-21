package metrics

import (
	"sync"
	"time"
)

const (
	// TrendWindow is how far back trends look.
	TrendWindow = time.Hour
	// MinTrendSpan hides trends until there's enough history to mean anything.
	MinTrendSpan = 5 * time.Minute
	// minSampleGap keeps several open tabs from flooding the history.
	minSampleGap = 10 * time.Second
)

type sample struct {
	at    time.Time
	value float64
}

// History keeps an hour of gauge fractions in memory, to compute trends.
// Keep one per identity: it holds cluster data.
type History struct {
	mu      sync.Mutex
	samples map[string][]sample
}

// NewHistory returns an empty history.
func NewHistory() *History {
	return &History{samples: map[string][]sample{}}
}

// Record adds a sample for gauge name.
func (h *History) Record(name string, at time.Time, value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.samples[name]
	if n := len(s); n > 0 && at.Sub(s[n-1].at) < minSampleGap {
		return
	}
	s = append(s, sample{at: at, value: value})
	cut := 0
	for cut < len(s) && at.Sub(s[cut].at) > TrendWindow {
		cut++
	}
	h.samples[name] = append(s[:0:0], s[cut:]...)
}

// Trend returns value minus the oldest sample in the window, and the span
// it covers. ok is false until the history spans MinTrendSpan.
func (h *History) Trend(name string, now time.Time, value float64) (delta float64, span time.Duration, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, s := range h.samples[name] {
		if now.Sub(s.at) > TrendWindow {
			continue
		}
		span = now.Sub(s.at)
		if span < MinTrendSpan {
			return 0, 0, false
		}
		return value - s.value, span, true
	}
	return 0, 0, false
}

// Apply records g's fraction and fills in its trend.
func (h *History) Apply(name string, g *Gauge, now time.Time) {
	if g.State != Live {
		return
	}
	f := g.Fraction()
	if delta, span, ok := h.Trend(name, now, f); ok {
		g.Trend = &delta
		g.TrendWindow = int(span.Seconds())
	}
	h.Record(name, now, f)
}
