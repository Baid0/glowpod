package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"glowpod/internal/metrics"
	"glowpod/internal/pods"
	"glowpod/internal/resources"
)

func TestDrawArc(t *testing.T) {
	tests := []struct {
		name       string
		spec       arcSpec
		wantHeight int
		wantCenter string
	}{
		{name: "small", spec: arcSpec{width: 8, frac: 0.5}, wantHeight: 2},
		{name: "card size", spec: arcSpec{width: 19, frac: 0.62, req: 0.75, center: "62%"}, wantHeight: 5, wantCenter: "62%"},
		{name: "empty", spec: arcSpec{width: 19, frac: 0}, wantHeight: 5},
		{name: "full and over", spec: arcSpec{width: 19, frac: 1.4}, wantHeight: 5},
		{name: "no data", spec: arcSpec{width: 19, dotted: true, center: "—"}, wantHeight: 5, wantCenter: "—"},
		{name: "too narrow is widened", spec: arcSpec{width: 2}, wantHeight: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.spec.thresholds = [2]float64{0.7, 0.85}
			lines := drawArc(tt.spec)
			if len(lines) != tt.wantHeight {
				t.Fatalf("height = %d, want %d", len(lines), tt.wantHeight)
			}
			w := max(tt.spec.width, 6)
			for i, l := range lines {
				if got := lipgloss.Width(l); got != w {
					t.Errorf("line %d width = %d, want %d", i, got, w)
				}
			}
			last := ansi.Strip(lines[len(lines)-1])
			if tt.wantCenter != "" && !strings.Contains(last, tt.wantCenter) {
				t.Errorf("bottom row %q doesn't show %q", last, tt.wantCenter)
			}
			if strings.TrimSpace(ansi.Strip(strings.Join(lines, ""))) == "" {
				t.Error("arc is blank")
			}
		})
	}
}

func TestBar(t *testing.T) {
	for _, tt := range []struct {
		frac      float64
		wantFills int
	}{{0, 0}, {0.5, 5}, {1, 10}, {2, 10}, {-1, 0}} {
		got := ansi.Strip(bar(10, tt.frac, toneOK))
		if n := strings.Count(got, "█"); n != tt.wantFills || lipgloss.Width(got) != 10 {
			t.Errorf("bar(%v) = %q, want %d filled of 10", tt.frac, got, tt.wantFills)
		}
	}
}

func ptr(f float64) *float64 { return &f }

func TestDescribeGauge(t *testing.T) {
	th := [2]float64{0.7, 0.85}
	tests := []struct {
		name       string
		g          metrics.Gauge
		wantWord   string
		wantCenter string
		wantUsed   string
		wantFooter string
	}{
		{
			name:     "cpu ok with requests and trend",
			g:        metrics.Gauge{State: metrics.Live, Value: 24.8, Max: 40, Unit: "cores", Requested: ptr(30), Trend: ptr(0.04), TrendWindow: 3600},
			wantWord: "OK", wantCenter: "62%", wantUsed: "24.8 of 40 cores", wantFooter: "75% requested · ↑ 4% in 1h",
		},
		{
			name:     "memory high in GiB",
			g:        metrics.Gauge{State: metrics.Live, Value: 49.9 * (1 << 30), Max: 64 * (1 << 30), Unit: "bytes"},
			wantWord: "High", wantCenter: "78%", wantUsed: "49.9 of 64 GiB",
		},
		{
			name:     "pods critical",
			g:        metrics.Gauge{State: metrics.Live, Value: 90, Max: 100, Unit: "pods", Trend: ptr(-0.02), TrendWindow: 1500},
			wantWord: "Critical", wantCenter: "90%", wantUsed: "90 of 100 pods", wantFooter: "↓ 2% in 25m",
		},
		{
			name:     "network against its peak",
			g:        metrics.Gauge{State: metrics.Live, Value: 120e6, Max: 200e6, Unit: "bytes/s"},
			wantWord: "OK", wantCenter: "60%", wantUsed: "120 of 200 MB/s", wantFooter: "of 7-day peak",
		},
		{
			name:     "no metrics",
			g:        metrics.Gauge{State: metrics.NoMetrics},
			wantWord: "No metrics", wantCenter: "—", wantUsed: "Install metrics-server",
		},
		{
			name:     "no access keeps the server's words",
			g:        metrics.Gauge{State: metrics.NoAccess, Message: "You can't read nodes."},
			wantWord: "No access", wantCenter: "—", wantUsed: "You can't read nodes.",
		},
		{
			name:     "error",
			g:        metrics.Gauge{State: metrics.Failed},
			wantWord: "Error", wantCenter: "—", wantUsed: "Couldn't load",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := describeGauge("X", tt.g, th)
			if v.word != tt.wantWord || v.center != tt.wantCenter || v.used != tt.wantUsed || v.footer != tt.wantFooter {
				t.Errorf("got word=%q center=%q used=%q footer=%q, want %q %q %q %q",
					v.word, v.center, v.used, v.footer, tt.wantWord, tt.wantCenter, tt.wantUsed, tt.wantFooter)
			}
		})
	}
}

func TestGaugeCardIsEvenlySized(t *testing.T) {
	th := [2]float64{0.7, 0.85}
	live := gaugeCard(describeGauge("CPU", metrics.Gauge{State: metrics.Live, Value: 1, Max: 4, Unit: "cores"}, th), th, 24)
	empty := gaugeCard(describeGauge("Storage", metrics.Gauge{State: metrics.NoMetrics, Message: "These volumes don't report usage, so only capacity is known."}, th), th, 24)
	if lipgloss.Height(live) != lipgloss.Height(empty) || lipgloss.Width(live) != 24 || lipgloss.Width(empty) != 24 {
		t.Errorf("cards differ: %dx%d and %dx%d", lipgloss.Width(live), lipgloss.Height(live), lipgloss.Width(empty), lipgloss.Height(empty))
	}
}

func TestStatusTone(t *testing.T) {
	tests := []struct {
		in   string
		want tone
	}{
		{"Running", toneOK},
		{"Completed", toneOK},
		{"Bound", toneOK},
		{"Pending", toneHigh},
		{"ContainerCreating", toneHigh},
		{"Terminating", toneHigh},
		{"Init:0/2", toneHigh},
		{"Init:CrashLoopBackOff", toneCritical},
		{"CrashLoopBackOff", toneCritical},
		{"OOMKilled", toneCritical},
		{"ImagePullBackOff", toneCritical},
		{"NotReady", toneCritical},
		{"Ready,SchedulingDisabled", toneHigh},
		{"NotReady,SchedulingDisabled", toneCritical},
		{"SomethingNew", toneNone},
	}
	for _, tt := range tests {
		if got := statusTone(tt.in); got != tt.want {
			t.Errorf("statusTone(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestScore(t *testing.T) {
	keys := []string{"deployments", "daemonsets", "deep", "podtemplates", "endpoints"}
	tests := []struct {
		query string
		first string
	}{
		{"dep", "deployments"},
		{"deep", "deep"},
		{"ds", "daemonsets"},
		{"dpl", "deployments"},
		{"pt", "podtemplates"},
	}
	for _, tt := range tests {
		best, bestScore := "", 0
		for _, k := range keys {
			if s := score(tt.query, k); s > bestScore {
				best, bestScore = k, s
			}
		}
		if best != tt.first {
			t.Errorf("score(%q) ranks %q first, want %q", tt.query, best, tt.first)
		}
	}
	if score("zzz", "pods") != 0 {
		t.Error("unrelated query matched")
	}
}

func TestPodSelector(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{
			name: "deployment matchLabels",
			obj:  map[string]any{"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": "web"}}}},
			want: "app=web",
		},
		{
			name: "matchExpressions",
			obj: map[string]any{"spec": map[string]any{"selector": map[string]any{"matchExpressions": []any{
				map[string]any{"key": "tier", "operator": "In", "values": []any{"a", "b"}},
			}}}},
			want: "tier in (a,b)",
		},
		{
			name: "service map",
			obj:  map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "web", "tier": "front"}}},
			want: "app=web,tier=front",
		},
		{name: "no selector", obj: map[string]any{"spec": map[string]any{}}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := podSelector(tt.obj)
			if err != nil || got != tt.want {
				t.Errorf("podSelector = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestReplicasOf(t *testing.T) {
	tests := []struct {
		name string
		cols []string
		cell []any
		want int64
	}{
		{"deployment ready", []string{"Name", "Ready"}, []any{"web", "2/3"}, 3},
		{"replicaset desired", []string{"Name", "Desired", "Current"}, []any{"web-1", float64(4), float64(4)}, 4},
		{"unknown", []string{"Name"}, []any{"x"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cols []resources.Column
			for _, c := range tt.cols {
				cols = append(cols, resources.Column{Name: c})
			}
			got := replicasOf(&resources.Table{Columns: cols}, resources.Row{Cells: tt.cell})
			if got != tt.want {
				t.Errorf("replicasOf = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCellText(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"Running", "Running"},
		{float64(3), "3"},
		{1.5, "1.5"},
		{true, "true"},
		{[]any{"a"}, `["a"]`},
	}
	for _, tt := range tests {
		if got := cellText(tt.in); got != tt.want {
			t.Errorf("cellText(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTypeOf(t *testing.T) {
	a := &app{types: []resources.Type{
		{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Verbs: []string{"list"}, ShortNames: []string{"po"}},
		{Group: "", Version: "v1", Resource: "events", Kind: "Event", Verbs: []string{"list"}, ShortNames: []string{"ev"}},
		{Group: "events.k8s.io", Version: "v1", Resource: "events", Kind: "Event", Verbs: []string{"list"}, ShortNames: []string{"ev"}},
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Verbs: []string{"list"}, ShortNames: []string{"deploy"}},
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Verbs: []string{"list"}, ShortNames: []string{"ing"}},
		{Group: "cert-manager.io", Version: "v1", Resource: "certificates", Kind: "Certificate", Verbs: []string{"list"}, ShortNames: []string{"cert"}},
		{Group: "", Version: "v1", Resource: "bindings", Kind: "Binding", Verbs: []string{"create"}},
	}}
	tests := []struct {
		in        string
		wantGroup string
		wantRes   string
		ok        bool
	}{
		{"pods", "", "pods", true},
		{"po", "", "pods", true},
		{"pod", "", "pods", true},
		{"Pod", "", "pods", true},
		{"deploy", "apps", "deployments", true},
		{"deployment", "apps", "deployments", true},
		{"ingress", "networking.k8s.io", "ingresses", true},
		{"events", "", "events", true},
		{"events.events.k8s.io", "events.k8s.io", "events", true},
		{"cert", "cert-manager.io", "certificates", true},
		{"bindings", "", "", false},
		{"nope", "", "", false},
	}
	for _, tt := range tests {
		got, ok := a.typeOf(tt.in)
		if ok != tt.ok || (ok && (got.Group != tt.wantGroup || got.Resource != tt.wantRes)) {
			t.Errorf("typeOf(%q) = %s/%s %v, want %s/%s %v", tt.in, got.Group, got.Resource, ok, tt.wantGroup, tt.wantRes, tt.ok)
		}
	}
}

func TestDefaultContainer(t *testing.T) {
	tests := []struct {
		name string
		cs   []pods.Container
		want string
	}{
		{"struggling app container", []pods.Container{{Name: "a", Role: "app", Ready: true}, {Name: "b", Role: "app", Restarts: 3, Ready: true}}, "b"},
		{"first app container", []pods.Container{{Name: "proxy", Role: "sidecar", Ready: true}, {Name: "app", Role: "app", Ready: true}}, "app"},
		{"skips init", []pods.Container{{Name: "init", Role: "init"}, {Name: "app", Role: "app", Ready: true}}, "app"},
		{"only sidecars", []pods.Container{{Name: "proxy", Role: "sidecar", Ready: true}}, "proxy"},
		{"none", nil, ""},
	}
	for _, tt := range tests {
		if got := defaultContainer(tt.cs); got != tt.want {
			t.Errorf("%s: defaultContainer = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestWrapRunes(t *testing.T) {
	tests := []struct {
		in   string
		w    int
		want []string
	}{
		{"short", 10, []string{"short"}},
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"日本語テキスト", 4, []string{"日本", "語テ", "キス", "ト"}},
	}
	for _, tt := range tests {
		if got := wrapRunes(tt.in, tt.w); !slices.Equal(got, tt.want) {
			t.Errorf("wrapRunes(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}

func TestShortDuration(t *testing.T) {
	for _, tt := range []struct {
		secs float64
		want string
	}{{5, "5s"}, {90, "1m"}, {3600, "1h"}, {50 * 3600, "2d"}, {-3, "0s"}} {
		d := time.Duration(tt.secs * float64(time.Second))
		if got := shortDuration(d); got != tt.want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, tt.want)
		}
	}
}

func TestPad(t *testing.T) {
	for _, tt := range []struct {
		in   string
		w    int
		want string
	}{{"abc", 5, "abc  "}, {"abcdef", 4, "abc…"}, {"", 2, "  "}} {
		if got := pad(tt.in, tt.w); got != tt.want {
			t.Errorf("pad(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
	if got := padLeft("7", 3); got != "  7" {
		t.Errorf("padLeft = %q", got)
	}
	if got := ansi.Strip(spread("a", "b", 5)); got != "a   b" {
		t.Errorf("spread = %q", got)
	}
}
