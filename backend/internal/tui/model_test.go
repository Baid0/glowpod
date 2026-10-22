package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"glowpod/internal/access"
	"glowpod/internal/cluster"
	"glowpod/internal/logs"
	"glowpod/internal/metrics"
	"glowpod/internal/overview"
	"glowpod/internal/pods"
	"glowpod/internal/resources"
	"glowpod/internal/scope"
	"glowpod/internal/summary"
	"glowpod/internal/terminal"
)

var testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

var (
	podsType    = resources.Type{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch", "delete"}, ShortNames: []string{"po"}, Subresources: []string{"log", "exec"}}
	deploysType = resources.Type{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list", "watch", "patch", "delete"}, ShortNames: []string{"deploy"}, Subresources: []string{"scale"}}
)

// fakeSource is a small cluster: two pods and a deployment in "team".
type fakeSource struct {
	mu    sync.Mutex
	allow func(access.Check) bool
	calls []string
	lists []resources.ListOptions
}

func (f *fakeSource) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeSource) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeSource) Cluster() cluster.Info { return cluster.Info{ID: "default", Name: "lab"} }

func (f *fakeSource) Overview(_ context.Context, ns scope.Namespaces) (*overview.Response, error) {
	live := func(v, m float64, unit string) metrics.Gauge {
		return metrics.Gauge{State: metrics.Live, Value: v, Max: m, Unit: unit}
	}
	return &overview.Response{
		Cluster:    overview.ClusterInfo{Info: f.Cluster(), Version: "v1.36.5", Nodes: 3, Namespaces: 2},
		Namespace:  ns.String(),
		Thresholds: [2]float64{0.7, 0.85},
		Gauges: overview.Gauges{
			CPU: live(2.4, 6, "cores"), Memory: live(4<<30, 8<<30, "bytes"),
			Pods: live(12, 330, "pods"), Storage: metrics.Gauge{State: metrics.NoMetrics},
		},
		Health: overview.Health{Running: 1, Failing: 1},
		Problems: []summary.Problem{{
			Severity: summary.Critical, Reason: "CrashLoopBackOff", Title: "Keeps restarting",
			Detail: "restarted 14 times · exit code 1", Hint: "Check the logs.", Kind: "Pod", Namespace: "team", Name: "api-1", Since: testNow.Add(-23 * time.Minute),
		}},
		ProblemCount: 1,
	}, nil
}

func (f *fakeSource) Events(context.Context, scope.Namespaces, string, int) ([]summary.Event, error) {
	return []summary.Event{{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed container", Object: summary.ObjectRef{Kind: "Pod", Name: "api-1"}, Time: testNow.Add(-time.Minute)}}, nil
}

func (f *fakeSource) Namespaces(context.Context) ([]string, bool, error) {
	return []string{"other", "team"}, false, nil
}

func (f *fakeSource) Types(context.Context) ([]resources.Type, error) {
	return []resources.Type{podsType, deploysType}, nil
}

func (f *fakeSource) List(_ context.Context, t resources.Type, opts resources.ListOptions) (*resources.Table, error) {
	f.mu.Lock()
	f.lists = append(f.lists, opts)
	f.mu.Unlock()
	row := func(name, ns string, cells ...any) resources.Row {
		return resources.Row{Cells: append([]any{name}, cells...), Object: resources.RowObject{Name: name, Namespace: ns}}
	}
	switch t.Resource {
	case "pods":
		return &resources.Table{
			Columns: []resources.Column{{Name: "Name"}, {Name: "Ready"}, {Name: "Status"}, {Name: "Restarts", Type: "integer"}, {Name: "IP", Priority: 1}},
			Rows: []resources.Row{
				row("web-1", "team", "1/1", "Running", float64(0), "10.0.0.1"),
				row("api-1", "team", "0/1", "CrashLoopBackOff", float64(14), "10.0.0.2"),
			},
		}, nil
	case "deployments":
		return &resources.Table{
			Columns: []resources.Column{{Name: "Name"}, {Name: "Ready"}},
			Rows:    []resources.Row{row("web", "team", "2/2")},
		}, nil
	}
	return &resources.Table{}, nil
}

func (f *fakeSource) Get(_ context.Context, t resources.Type, _, name string) (*resources.Object, error) {
	return &resources.Object{
		Object: map[string]any{"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": name}}}},
		YAML:   "kind: " + t.Kind + "\nmetadata:\n  name: " + name + "\n",
	}, nil
}

func (f *fakeSource) Pod(_ context.Context, ns, name string) (*pods.Detail, error) {
	return &pods.Detail{
		Pod:        summary.Pod{Namespace: ns, Name: name, Phase: "Running", Health: summary.Running},
		Containers: []pods.Container{{Name: "app", Role: "app", Ready: true}},
	}, nil
}

func (f *fakeSource) Logs(context.Context, string, string, logs.Options) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(
		"2026-10-22T10:00:00Z starting up\n2026-10-22T10:00:01Z WARN cache cold\n2026-10-22T10:00:02Z ERROR db refused\n")), nil
}

func (f *fakeSource) Watch(ctx context.Context, _ resources.Type, _, _, _ string, _ func()) {
	<-ctx.Done()
}
func (f *fakeSource) WatchEvents(ctx context.Context, _ string, _ func()) { <-ctx.Done() }

func (f *fakeSource) Allowed(_ context.Context, checks []access.Check) ([]bool, error) {
	out := make([]bool, len(checks))
	for i, c := range checks {
		out[i] = f.allow == nil || f.allow(c)
	}
	return out, nil
}

func (f *fakeSource) Scale(_ context.Context, t resources.Type, ns, name string, n int64) error {
	f.record(fmt.Sprintf("scale %s %s/%s %d", t.Resource, ns, name, n))
	return nil
}

func (f *fakeSource) Restart(_ context.Context, t resources.Type, ns, name string) error {
	f.record(fmt.Sprintf("restart %s %s/%s", t.Resource, ns, name))
	return nil
}

func (f *fakeSource) Delete(_ context.Context, t resources.Type, ns, name string) error {
	f.record(fmt.Sprintf("delete %s %s/%s", t.Resource, ns, name))
	return nil
}

func (f *fakeSource) Shell(context.Context, terminal.Request, terminal.Streams) error {
	return errors.New("no shell in tests")
}

// harness drives the model like Bubble Tea does, running commands and
// feeding their messages back. Commands that block (ticks, watches) are
// left behind after a short wait.
type harness struct {
	t   *testing.T
	m   *Model
	src *fakeSource
}

func newHarness(t *testing.T, src *fakeSource, opts Options) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	opts.Now = func() time.Time { return testNow }
	h := &harness{t: t, m: New(ctx, src, opts), src: src}
	h.update(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(h.m.Init())
	return h
}

func (h *harness) update(msg tea.Msg) {
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

func (h *harness) run(cmd tea.Cmd) {
	pending := []tea.Cmd{cmd}
	for round := 0; len(pending) > 0 && round < 20; round++ {
		results := make(chan tea.Msg, len(pending))
		n := 0
		for _, c := range pending {
			if c == nil {
				continue
			}
			n++
			go func() { results <- c() }()
		}
		pending = nil
		timeout := time.After(150 * time.Millisecond)
	collect:
		for range n {
			select {
			case msg := <-results:
				switch msg := msg.(type) {
				case nil:
				case tea.BatchMsg:
					pending = append(pending, msg...)
				case tea.QuitMsg:
				default:
					_, next := h.m.Update(msg)
					pending = append(pending, next)
				}
			case <-timeout:
				break collect
			}
		}
	}
}

func (h *harness) press(keys ...string) {
	for _, k := range keys {
		h.update(keyMsg(k))
	}
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func (h *harness) screen() string {
	return ansi.Strip(h.m.View().Content)
}

func (h *harness) wantScreen(want ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range want {
		if !strings.Contains(s, w) {
			h.t.Fatalf("screen doesn't show %q:\n%s", w, s)
		}
	}
}

func (h *harness) wantNot(not ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range not {
		if strings.Contains(s, w) {
			h.t.Fatalf("screen shows %q but shouldn't:\n%s", w, s)
		}
	}
}

func TestGlanceFirst(t *testing.T) {
	t.Parallel()
	h := newHarness(t, &fakeSource{}, Options{})
	h.wantScreen("Overview", "CPU", "Memory", "40%", "Storage", "No metrics",
		"NEEDS ATTENTION", "Keeps restarting (CrashLoopBackOff)", "api-1", "Check the logs.",
		"RECENT EVENTS", "BackOff", "1 running", "1 failing")

	// Enter opens the problem's pod, in Deep mode.
	h.press("enter")
	h.wantScreen("pod team/api-1", "Logs", "starting up", "ERROR db refused")
}

func TestDeepTables(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		allow func(access.Check) bool
		keys  []string
		text  string
		want  []string
		not   []string
		calls []string
	}{
		{
			name: "tab opens pods with the API's columns",
			keys: []string{"tab"},
			want: []string{"pods · all namespaces", "NAMESPACE", "NAME", "STATUS", "web-1", "● Running", "● CrashLoopBackOff", "l logs", "ctrl+d delete"},
			not:  []string{"IP"},
		},
		{
			name: "w adds the wide columns",
			keys: []string{"tab", "w"},
			want: []string{"IP", "10.0.0.1"},
		},
		{
			name: "filter as you type",
			keys: []string{"tab", "/"},
			text: "api",
			want: []string{"api-1", "1 of 2"},
			not:  []string{"web-1"},
		},
		{
			name:  "actions hidden without RBAC",
			allow: func(c access.Check) bool { return c.Verb == "get" },
			keys:  []string{"tab", "ctrl+d"},
			want:  []string{"l logs"},
			not:   []string{"delete", "shell", "Type web-1"},
		},
		{
			name:  "delete needs the typed name",
			keys:  []string{"tab", "ctrl+d"},
			text:  "web-1",
			want:  []string{"Delete pod web-1?", "Type web-1 to confirm."},
			calls: nil,
		},
		{
			name: "palette opens deployments",
			keys: []string{":"},
			text: "deploy",
			want: []string{"deployments", "Deployment · apps/v1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{allow: tt.allow}
			h := newHarness(t, src, Options{})
			h.press(tt.keys...)
			h.typeText(tt.text)
			h.wantScreen(tt.want...)
			h.wantNot(tt.not...)
			if got := src.called(); len(got) != len(tt.calls) {
				t.Errorf("calls = %v, want %v", got, tt.calls)
			}
		})
	}
}

func TestDeleteFlow(t *testing.T) {
	t.Parallel()
	src := &fakeSource{}
	h := newHarness(t, src, Options{Resource: "pods"})
	h.press("ctrl+d")
	h.typeText("web")
	h.press("enter")
	h.wantScreen("The name doesn't match.")
	if len(src.called()) != 0 {
		t.Fatalf("deleted with the wrong name: %v", src.called())
	}
	h.typeText("-1")
	h.press("enter")
	h.wantScreen("Deleted web-1")
	if got := src.called(); len(got) != 1 || got[0] != "delete pods team/web-1" {
		t.Fatalf("calls = %v", got)
	}
}

func TestScaleAndRestart(t *testing.T) {
	t.Parallel()
	src := &fakeSource{}
	h := newHarness(t, src, Options{Resource: "deploy"})
	h.wantScreen("deployments · all namespaces", "s scale", "r restart")

	h.press("s")
	h.wantScreen("Scale deployment web", "It wants 2 now.")
	h.update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}) // clear the input
	h.typeText("x")
	h.press("enter")
	h.wantScreen("Type a number from 0 to 10000.")
	h.update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	h.typeText("3")
	h.press("enter")

	h.press("r")
	h.wantScreen("Restart deployment web?")
	h.press("y")

	want := []string{"scale deployments team/web 3", "restart deployments team/web"}
	got := src.called()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestNamespaceSwitch(t *testing.T) {
	t.Parallel()
	src := &fakeSource{}
	h := newHarness(t, src, Options{Resource: "pods"})
	h.press("n")
	h.typeText("team")
	h.press("enter")
	h.wantScreen("pods · team", "Namespace: team")
	src.mu.Lock()
	last := src.lists[len(src.lists)-1]
	src.mu.Unlock()
	if last.Namespace != "team" {
		t.Errorf("last list in %q, want team", last.Namespace)
	}
	h.wantNot("NAMESPACE")
}

func TestWorkloadOpensItsPods(t *testing.T) {
	t.Parallel()
	src := &fakeSource{}
	h := newHarness(t, src, Options{Resource: "deployments"})
	h.press("enter")
	h.wantScreen("pods of deployment/web · team", "labels app=web")
	src.mu.Lock()
	last := src.lists[len(src.lists)-1]
	src.mu.Unlock()
	if last.LabelSelector != "app=web" || last.Namespace != "team" {
		t.Errorf("listed %+v, want app=web in team", last)
	}
	h.press("esc")
	h.wantScreen("deployments · all namespaces")
}

func TestYAML(t *testing.T) {
	t.Parallel()
	h := newHarness(t, &fakeSource{}, Options{Resource: "pods"})
	h.press("y")
	h.wantScreen("pod web-1 · YAML", "kind: Pod", "name: web-1")
}

func TestHelpShowsOnlyAllowedActions(t *testing.T) {
	t.Parallel()
	src := &fakeSource{allow: func(c access.Check) bool { return c.Verb != "delete" }}
	h := newHarness(t, src, Options{Resource: "pods"})
	h.press("?")
	h.wantScreen("Keys", "THIS SCREEN", "l          logs", "EVERYWHERE")
	h.wantNot("ctrl+d")
}

func TestLogLevels(t *testing.T) {
	t.Parallel()
	p := newPodView(&app{now: func() time.Time { return testNow }}, "team", "web-1")
	p.addLines([]logs.Line{{Text: "plain"}, {Text: "WARN slow"}, {Text: "\x1b[31mERROR\x1b[0m boom"}})
	want := []logs.Level{logs.Plain, logs.Warn, logs.Error}
	for i, l := range p.lines {
		if l.level != want[i] {
			t.Errorf("line %d level = %q, want %q", i, l.level, want[i])
		}
	}
	if p.lines[2].text != "ERROR boom" {
		t.Errorf("colour codes kept: %q", p.lines[2].text)
	}
}
