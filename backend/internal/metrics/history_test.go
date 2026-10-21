package metrics

import (
	"testing"
	"time"
)

func TestHistoryTrend(t *testing.T) {
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		samples   map[time.Duration]float64 // offset from start → fraction
		now       time.Duration
		value     float64
		wantOK    bool
		wantDelta float64
		wantSpan  time.Duration
	}{
		{name: "no history", now: 0, value: 0.5},
		{name: "too short", samples: map[time.Duration]float64{0: 0.5}, now: 4 * time.Minute, value: 0.6},
		{name: "five minutes", samples: map[time.Duration]float64{0: 0.5}, now: 5 * time.Minute, value: 0.6, wantOK: true, wantDelta: 0.1, wantSpan: 5 * time.Minute},
		{
			name:    "uses the oldest sample inside the hour",
			samples: map[time.Duration]float64{0: 0.1, 30 * time.Minute: 0.4, 50 * time.Minute: 0.5},
			now:     70 * time.Minute, value: 0.62,
			wantOK: true, wantDelta: 0.22, wantSpan: 40 * time.Minute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHistory()
			for _, off := range []time.Duration{0, 30 * time.Minute, 50 * time.Minute} {
				if v, ok := tt.samples[off]; ok {
					h.Record("cpu", start.Add(off), v)
				}
			}
			delta, span, ok := h.Trend("cpu", start.Add(tt.now), tt.value)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && (!near(delta, tt.wantDelta) || span != tt.wantSpan) {
				t.Fatalf("delta = %v span = %v, want %v %v", delta, span, tt.wantDelta, tt.wantSpan)
			}
		})
	}
}

func TestHistoryDropsOldAndCloseSamples(t *testing.T) {
	h := NewHistory()
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	h.Record("cpu", start, 0.1)
	h.Record("cpu", start.Add(3*time.Second), 0.9) // too close: ignored
	if n := len(h.samples["cpu"]); n != 1 {
		t.Fatalf("samples = %d, want 1", n)
	}
	h.Record("cpu", start.Add(2*time.Hour), 0.5)
	if n := len(h.samples["cpu"]); n != 1 {
		t.Fatalf("samples = %d, want old ones dropped", n)
	}
}

func TestHistoryApply(t *testing.T) {
	h := NewHistory()
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	g := Gauge{State: Live, Value: 20, Max: 40}
	h.Apply("cpu", &g, start)
	if g.Trend != nil {
		t.Fatal("no trend on the first sample")
	}
	g = Gauge{State: Live, Value: 24, Max: 40}
	h.Apply("cpu", &g, start.Add(10*time.Minute))
	if g.Trend == nil || !near(*g.Trend, 0.1) || g.TrendWindow != 600 {
		t.Fatalf("trend = %v window = %d", g.Trend, g.TrendWindow)
	}

	off := Gauge{State: NoMetrics}
	h.Apply("memory", &off, start)
	if len(h.samples["memory"]) != 0 {
		t.Fatal("non-live gauges are not recorded")
	}
}
