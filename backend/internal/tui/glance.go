package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"glowpod/internal/overview"
	"glowpod/internal/scope"
	"glowpod/internal/summary"
)

const (
	glanceRefresh = 15 * time.Second
	glanceEvents  = 12
	// eventsDebounce gathers a burst of event changes into one read.
	eventsDebounce = 2 * time.Second
)

// glance is the start screen: gauges, what needs attention, recent events.
type glance struct {
	a        *app
	sid      int
	resp     *overview.Response
	err      error
	loadedAt time.Time
	events   []summary.Event
	sel      int

	// The events watch for the current namespace.
	cancel    context.CancelFunc
	watchGen  int
	evPending bool
}

type (
	overviewMsg struct {
		to   int
		resp *overview.Response
		err  error
	}
	glanceEventsMsg struct {
		to     int
		events []summary.Event
		err    error
	}
	glanceTick       struct{ to int }
	glanceEvReload   struct{ to int }
	glanceWatchReady struct {
		to, gen int
		ch      <-chan struct{}
	}
)

func (m overviewMsg) target() int      { return m.to }
func (m glanceEventsMsg) target() int  { return m.to }
func (m glanceTick) target() int       { return m.to }
func (m glanceEvReload) target() int   { return m.to }
func (m glanceWatchReady) target() int { return m.to }

func newGlance(a *app) *glance {
	return &glance{a: a, sid: a.id()}
}

func (g *glance) id() int { return g.sid }

func (g *glance) init() tea.Cmd {
	return tea.Batch(g.loadOverview(), g.loadEvents(), g.watch())
}

func (g *glance) loadOverview() tea.Cmd {
	a, to := g.a, g.sid
	ns := scope.One(a.ns)
	return func() tea.Msg {
		resp, err := a.src.Overview(a.ctx, ns)
		return overviewMsg{to: to, resp: resp, err: err}
	}
}

func (g *glance) loadEvents() tea.Cmd {
	a, to := g.a, g.sid
	ns := scope.One(a.ns)
	return func() tea.Msg {
		evs, err := a.src.Events(a.ctx, ns, "", glanceEvents)
		return glanceEventsMsg{to: to, events: evs, err: err}
	}
}

// watch (re)starts the events watch for the app's namespace.
func (g *glance) watch() tea.Cmd {
	if g.cancel != nil {
		g.cancel()
	}
	ctx, cancel := context.WithCancel(g.a.ctx)
	g.cancel = cancel
	g.watchGen++
	ch := make(chan struct{}, 1)
	src, ns := g.a.src, g.a.ns
	go func() {
		defer close(ch)
		src.WatchEvents(ctx, ns, notify(ch))
	}()
	return waitChanged(ch, func() tea.Msg { return glanceWatchReady{to: g.sid, gen: g.watchGen, ch: ch} })
}

// notify returns a callback that signals ch without blocking.
func notify(ch chan<- struct{}) func() {
	return func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// waitChanged waits for the next change on ch, then sends next().
func waitChanged(ch <-chan struct{}, next func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		return next()
	}
}

func (g *glance) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case overviewMsg:
		g.err = msg.err
		if msg.err == nil {
			g.resp = msg.resp
			g.loadedAt = g.a.now()
			g.sel = min(g.sel, max(0, len(g.resp.Problems)-1))
		}
		return tea.Tick(glanceRefresh, func(time.Time) tea.Msg { return glanceTick{to: g.sid} })
	case glanceTick:
		return g.loadOverview()
	case glanceEventsMsg:
		if msg.err == nil {
			g.events = msg.events
		}
		return nil
	case glanceWatchReady:
		if msg.gen != g.watchGen {
			return nil
		}
		next := waitChanged(msg.ch, func() tea.Msg { return msg })
		if g.evPending {
			return next
		}
		g.evPending = true
		return tea.Batch(next, tea.Tick(eventsDebounce, func(time.Time) tea.Msg { return glanceEvReload{to: g.sid} }))
	case glanceEvReload:
		g.evPending = false
		return g.loadEvents()
	case nsChanged:
		g.sel = 0
		return tea.Batch(g.loadOverview(), g.loadEvents(), g.watch())
	}
	return nil
}

func (g *glance) key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	n := 0
	if g.resp != nil {
		n = len(g.resp.Problems)
	}
	switch msg.String() {
	case "down", "j":
		g.sel = min(g.sel+1, max(0, n-1))
	case "up", "k":
		g.sel = max(g.sel-1, 0)
	case "enter":
		if n == 0 {
			return true, nil
		}
		return true, g.open(g.resp.Problems[g.sel])
	case "r":
		return true, tea.Batch(g.loadOverview(), g.loadEvents())
	default:
		return false, nil
	}
	return true, nil
}

// open goes to what a problem is about.
func (g *glance) open(p summary.Problem) tea.Cmd {
	a := g.a
	switch p.Kind {
	case "Pod":
		return func() tea.Msg { return openMsg{newPodView(a, p.Namespace, p.Name)} }
	case "Node":
		return func() tea.Msg {
			return openMsg{newTable(a, a.pods(), tableScope{field: "spec.nodeName=" + p.Name, about: "on node " + p.Name})}
		}
	case "Job":
		return func() tea.Msg {
			return openMsg{newTable(a, a.pods(), tableScope{namespace: p.Namespace, label: "job-name=" + p.Name, about: "of job " + p.Name})}
		}
	}
	return nil
}

func (g *glance) title() string { return "Overview" }

func (g *glance) status() string {
	if g.resp == nil {
		if g.err != nil {
			return status(toneCritical, "Couldn't load")
		}
		return sMuted.Render("loading…")
	}
	since := g.a.now().Sub(g.loadedAt)
	t := toneOK
	if since > time.Minute {
		t = toneNone
	}
	return lipgloss.NewStyle().Foreground(t.color()).Render("●") + " " +
		sMuted.Render("updated "+shortDuration(since)+" ago")
}

func (g *glance) hints() []keyHint {
	if g.resp != nil && len(g.resp.Problems) > 0 {
		return []keyHint{{"↑↓", "select"}, {"enter", "open"}, {"r", "refresh"}}
	}
	return []keyHint{{"r", "refresh"}}
}

func (g *glance) capturing() bool { return false }

func (g *glance) close() {
	if g.cancel != nil {
		g.cancel()
	}
}

func (g *glance) view(w, h int) string {
	if g.resp == nil {
		msg := sMuted.Render("Reading the cluster…")
		if g.err != nil {
			msg = sRose.Render("✕ ") + sInk.Render("Couldn't load the overview. ") + sMuted.Render(explain(g.err))
		}
		return fill([]string{"", " " + msg}, w, h)
	}
	r := g.resp
	var out []string

	c := r.Cluster
	out = append(out, " "+sMuted.Render(fmt.Sprintf("%s · Kubernetes %s · %d nodes · %d namespaces", c.Name, c.Version, c.Nodes, c.Namespaces)))

	gauges := []gaugeView{
		describeGauge("CPU", r.Gauges.CPU, r.Thresholds),
		describeGauge("Memory", r.Gauges.Memory, r.Thresholds),
		describeGauge("Pods", r.Gauges.Pods, r.Thresholds),
		describeGauge("Storage", r.Gauges.Storage, r.Thresholds),
	}
	if r.Gauges.Network != nil {
		gauges = append(gauges, describeGauge("Network", *r.Gauges.Network, r.Thresholds))
	}
	out = append(out, g.gauges(gauges, r.Thresholds, w, h)...)

	health := r.Health
	out = append(out, " "+status(toneOK, fmt.Sprintf("%d running", health.Running))+sMuted.Render(" · ")+
		status(toneHigh, fmt.Sprintf("%d pending", health.Pending))+sMuted.Render(" · ")+
		status(failTone(health.Failing), fmt.Sprintf("%d failing", health.Failing)), "")

	rest := max(0, h-len(out))
	if w >= 110 {
		leftW := w * 3 / 5
		left := fill(g.attention(r, leftW, rest), leftW, rest)
		right := fill(g.recentEvents(w-leftW-1, rest), w-leftW-1, rest)
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right))
	} else {
		attention := g.attention(r, w, rest)
		out = append(out, attention...)
		out = append(out, "")
		out = append(out, g.recentEvents(w, max(0, rest-len(attention)-1))...)
	}
	return fill(strings.Split(strings.Join(out, "\n"), "\n"), w, h)
}

func failTone(n int) tone {
	if n > 0 {
		return toneCritical
	}
	return toneOK
}

// gauges lays the gauges out as cards when there's room, else as lines.
func (g *glance) gauges(views []gaugeView, thresholds [2]float64, w, h int) []string {
	n := len(views)
	perRow := n
	cardW := (w - 1) / perRow
	for cardW < 22 && perRow > 2 {
		perRow--
		cardW = (w - 1) / perRow
	}
	rows := (n + perRow - 1) / perRow
	// A card is about 10 lines; leave room for the lists below.
	if cardW < 22 || rows*10 > h-8 {
		lines := make([]string, 0, n+1)
		for _, v := range views {
			lines = append(lines, " "+gaugeLine(v, min(w-2, 72)))
		}
		return append(lines, "")
	}
	var out []string
	for i := 0; i < n; i += perRow {
		var cards []string
		for _, v := range views[i:min(n, i+perRow)] {
			cards = append(cards, gaugeCard(v, thresholds, cardW))
		}
		out = append(out, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, cards...), "\n")...)
	}
	return out
}

// attention is "Needs attention": plain title first, the Kubernetes term in
// brackets, then what and where.
func (g *glance) attention(r *overview.Response, w, h int) []string {
	out := []string{" " + label("Needs attention") + sMuted.Render(fmt.Sprintf(" · %d", r.ProblemCount))}
	if len(r.Problems) == 0 {
		return append(out, " "+status(toneOK, "All good")+sMuted.Render(" · nothing needs attention"))
	}
	now := g.a.now()
	for i, p := range r.Problems {
		if len(out)+2 > h {
			out = append(out, " "+sMuted.Render(fmt.Sprintf("… and %d more", len(r.Problems)-i)))
			break
		}
		t := toneHigh
		if p.Severity == summary.Critical {
			t = toneCritical
		}
		marker := "  "
		titleStyle := sBody
		if i == g.sel {
			marker = sLime.Render("▌") + " "
			titleStyle = sTitle
		}
		head := marker + pad(status(t, sevWord(p.Severity)), 11) + " " + titleStyle.Render(p.Title) + sMuted.Render(" ("+p.Reason+")")
		out = append(out, spread(clip(head, w-6), sMuted.Render(age(p.Since, now)), w-1))
		where := p.Name
		if p.Namespace != "" {
			where += sMuted.Render(" · " + p.Namespace)
		}
		if p.Detail != "" {
			where += sMuted.Render(" · " + p.Detail)
		}
		out = append(out, clip("              "+sBody.Render(where), w))
		if i == g.sel && p.Hint != "" && len(out)+1 < h {
			out = append(out, clip("              "+sMuted.Render("→ "+p.Hint), w))
		}
	}
	return out
}

func sevWord(s summary.Severity) string {
	if s == summary.Critical {
		return "Critical"
	}
	return "High"
}

// recentEvents lists the newest events: warnings first in mind, all in time order.
func (g *glance) recentEvents(w, h int) []string {
	out := []string{" " + spread(label("Recent events"), status(toneOK, "live"), w-2)}
	if len(g.events) == 0 {
		return append(out, " "+sMuted.Render("No events yet."))
	}
	now := g.a.now()
	for _, e := range g.events {
		if len(out)+2 > h {
			break
		}
		icon := sMuted.Render("·")
		if e.Type == "Warning" {
			icon = sInk.Render("⚠")
		}
		head := " " + icon + " " + sInk.Render(e.Reason) + " " + sBody.Render(strings.TrimSpace(e.Message))
		out = append(out, spread(clip(head, w-6), sMuted.Render(age(e.Time, now)), w-1))
		obj := strings.ToLower(e.Object.Kind) + "/" + e.Object.Name
		out = append(out, clip("   "+sMuted.Render(obj), w))
	}
	return out
}
