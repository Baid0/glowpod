package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"glowpod/internal/access"
	"glowpod/internal/events"
	"glowpod/internal/logs"
	"glowpod/internal/pods"
	"glowpod/internal/resources"
	"glowpod/internal/scope"
	"glowpod/internal/summary"
)

const (
	logTail = 1000
	// logKeep caps the lines kept in memory for one pod.
	logKeep = 5000
)

type podTab int

const (
	tabLogs podTab = iota
	tabEvents
	tabYAML
)

var podTabs = []string{"Logs", "Events", "YAML"}

type logLine struct {
	time  string // HH:MM:SS in local time
	text  string
	level logs.Level
}

// podView is one pod: live logs, its events and its YAML.
type podView struct {
	a        *app
	sid      int
	ns, name string
	tab      podTab

	detail    *pods.Detail
	detailErr error
	container string
	previous  bool

	lines     []logLine
	back      int // logical lines scrolled up from the live end; 0 follows
	ended     bool
	logErr    string
	cancel    context.CancelFunc
	gen       int
	stamps    bool
	wrap      bool
	search    string
	searching bool
	input     textinput.Model
	height    int

	events    []summary.Event
	eventsErr error
	yaml      pager
	yamlObj   *resources.Object
	yamlErr   error
}

type (
	podDetailMsg struct {
		to     int
		detail *pods.Detail
		err    error
	}
	logChunk struct {
		lines []logs.Line
		done  bool
		err   error
	}
	logMsg struct {
		to, gen int
		chunk   logChunk
		ch      <-chan logChunk
	}
	podEventsMsg struct {
		to     int
		events []summary.Event
		err    error
	}
)

func (m podDetailMsg) target() int { return m.to }
func (m logMsg) target() int       { return m.to }
func (m podEventsMsg) target() int { return m.to }

func newPodView(a *app, ns, name string) *podView {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "highlight text in the logs"
	in.SetStyles(inputStyles())
	return &podView{a: a, sid: a.id(), ns: ns, name: name, wrap: true, input: in}
}

func (p *podView) id() int { return p.sid }

func (p *podView) init() tea.Cmd {
	a, to, ns, name := p.a, p.sid, p.ns, p.name
	return tea.Batch(
		func() tea.Msg {
			d, err := a.src.Pod(a.ctx, ns, name)
			return podDetailMsg{to: to, detail: d, err: err}
		},
		p.a.check(p.checks()...),
	)
}

func (p *podView) checks() []access.Check {
	return []access.Check{
		{Verb: "get", Group: resources.CoreGroup, Resource: "pods", Subresource: "log", Namespace: p.ns},
		{Verb: "create", Group: resources.CoreGroup, Resource: "pods", Subresource: "exec", Namespace: p.ns},
	}
}

func (p *podView) canShell() bool { return p.a.can(p.checks()[1]) }

// defaultContainer opens on the struggling container, else the first app
// container (like the web).
func defaultContainer(cs []pods.Container) string {
	first := ""
	for _, c := range cs {
		if c.Role == "init" {
			continue
		}
		if first == "" && c.Role != "sidecar" {
			first = c.Name
		}
		if c.Role != "sidecar" && (c.Restarts > 0 || !c.Ready) {
			return c.Name
		}
	}
	if first == "" && len(cs) > 0 {
		first = cs[len(cs)-1].Name
	}
	return first
}

// startLogs (re)starts following the selected container.
func (p *podView) startLogs() tea.Cmd {
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(p.a.ctx)
	p.cancel = cancel
	p.gen++
	p.lines, p.back, p.ended, p.logErr = nil, 0, false, ""
	ch := make(chan logChunk, 16)
	src, ns, name := p.a.src, p.ns, p.name
	opts := logs.Options{Container: p.container, Previous: p.previous, TailLines: logTail}
	go func() {
		defer close(ch)
		send := func(c logChunk) bool {
			select {
			case ch <- c:
				return true
			case <-ctx.Done():
				return false
			}
		}
		rc, err := src.Logs(ctx, ns, name, opts)
		if err != nil {
			_, msg := logs.Explain(err, opts)
			send(logChunk{done: true, err: errors.New(msg)})
			return
		}
		defer rc.Close()
		err = logs.Pump(ctx, rc, func(ls []logs.Line) bool { return send(logChunk{lines: ls}) })
		send(logChunk{done: true, err: err})
	}()
	return waitLog(p.sid, p.gen, ch)
}

func waitLog(to, gen int, ch <-chan logChunk) tea.Cmd {
	return func() tea.Msg {
		c, ok := <-ch
		if !ok {
			return nil
		}
		return logMsg{to: to, gen: gen, chunk: c, ch: ch}
	}
}

func (p *podView) loadEvents() tea.Cmd {
	a, to, ns, name := p.a, p.sid, p.ns, p.name
	return func() tea.Msg {
		evs, err := a.src.Events(a.ctx, scope.One(ns), events.About("Pod", name), 50)
		return podEventsMsg{to: to, events: evs, err: err}
	}
}

func (p *podView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case podDetailMsg:
		p.detail, p.detailErr = msg.detail, msg.err
		if msg.err != nil {
			return nil
		}
		if p.container == "" {
			p.container = defaultContainer(msg.detail.Containers)
		}
		return p.startLogs()
	case logMsg:
		if msg.gen != p.gen {
			return nil
		}
		p.addLines(msg.chunk.lines)
		if msg.chunk.done {
			p.ended = true
			if msg.chunk.err != nil && !errors.Is(msg.chunk.err, context.Canceled) {
				p.logErr = msg.chunk.err.Error()
			}
			return nil
		}
		return waitLog(p.sid, p.gen, msg.ch)
	case podEventsMsg:
		p.events, p.eventsErr = msg.events, msg.err
	case yamlMsg:
		p.yamlObj, p.yamlErr = msg.obj, msg.err
		if msg.obj != nil {
			p.yaml.lines = strings.Split(strings.TrimRight(msg.obj.YAML, "\n"), "\n")
		}
	}
	return nil
}

func (p *podView) addLines(ls []logs.Line) {
	for _, l := range ls {
		text := logs.StripANSI(l.Text)
		ll := logLine{text: text, level: logs.LevelOf(text)}
		if t, err := time.Parse(time.RFC3339Nano, l.Time); err == nil {
			ll.time = t.Local().Format("15:04:05")
		}
		p.lines = append(p.lines, ll)
		if p.back > 0 {
			p.back++ // keep the same lines on screen while reading back
		}
	}
	if over := len(p.lines) - logKeep; over > 0 {
		p.lines = append([]logLine(nil), p.lines[over:]...)
	}
	p.back = min(p.back, max(0, len(p.lines)-1))
}

func (p *podView) capturing() bool { return p.searching }

func (p *podView) key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if p.searching {
		switch k {
		case "enter", "esc":
			p.searching = false
			p.input.Blur()
			if k == "esc" {
				p.search = ""
			}
			return true, nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		p.search = p.input.Value()
		return true, cmd
	}
	switch k {
	case "1", "2", "3":
		return true, p.switchTab(podTab(k[0] - '1'))
	case "right", "]":
		return true, p.switchTab((p.tab + 1) % 3)
	case "left", "[":
		return true, p.switchTab((p.tab + 2) % 3)
	case "s":
		if p.canShell() {
			return true, shellCmd(p.a, p.ns, p.name, p.container)
		}
	}
	switch p.tab {
	case tabLogs:
		return p.logKey(k)
	case tabYAML:
		return p.yaml.key(k), nil
	case tabEvents:
		if k == "ctrl+r" {
			return true, p.loadEvents()
		}
	}
	return false, nil
}

func (p *podView) switchTab(t podTab) tea.Cmd {
	p.tab = t
	switch {
	case t == tabEvents:
		return p.loadEvents()
	case t == tabYAML && p.yamlObj == nil:
		return loadYAML(p.a, p.sid, p.a.pods(), p.ns, p.name)
	}
	return nil
}

func (p *podView) logKey(k string) (bool, tea.Cmd) {
	page := max(1, p.height-1)
	switch k {
	case "up", "k":
		p.back++
	case "down", "j":
		p.back--
	case "pgup", "ctrl+b":
		p.back += page
	case "pgdown", "ctrl+f", "space":
		p.back -= page
	case "home", "g":
		p.back = len(p.lines)
	case "end", "G":
		p.back = 0
	case "t":
		p.stamps = !p.stamps
	case "w":
		p.wrap = !p.wrap
	case "/":
		p.searching = true
		p.input.SetValue(p.search)
		p.input.CursorEnd()
		return true, p.input.Focus()
	case "esc":
		if p.search != "" {
			p.search = ""
			return true, nil
		}
		return false, nil
	case "p":
		p.previous = !p.previous
		return true, p.startLogs()
	case "c":
		if p.detail == nil || len(p.detail.Containers) < 2 {
			return true, nil
		}
		cs := p.detail.Containers
		for i, c := range cs {
			if c.Name == p.container {
				p.container = cs[(i+1)%len(cs)].Name
				break
			}
		}
		return true, p.startLogs()
	default:
		return false, nil
	}
	p.back = max(0, min(p.back, len(p.lines)-1))
	return true, nil
}

func (p *podView) title() string { return "pod " + p.ns + "/" + p.name }

func (p *podView) status() string {
	if p.detail == nil {
		if p.detailErr != nil {
			return status(toneCritical, "Couldn't load")
		}
		return sMuted.Render("loading…")
	}
	pod := p.detail.Pod
	if pod.Problem != nil {
		t := toneHigh
		if pod.Problem.Severity == summary.Critical {
			t = toneCritical
		}
		return status(t, pod.Problem.Title) + sMuted.Render(" ("+pod.Problem.Reason+")")
	}
	switch pod.Health {
	case summary.Running:
		return status(toneOK, "Running")
	case summary.Pending:
		return status(toneHigh, "Pending")
	case summary.Failing:
		return status(toneCritical, pod.Phase)
	}
	return status(toneNone, pod.Phase)
}

func (p *podView) hints() []keyHint {
	h := []keyHint{{"1 2 3", "tabs"}}
	if p.tab == tabLogs {
		h = append(h, keyHint{"/", "highlight"}, keyHint{"G", "follow"}, keyHint{"t", "times"}, keyHint{"w", "wrap"}, keyHint{"p", "previous run"})
		if p.detail != nil && len(p.detail.Containers) > 1 {
			h = append(h, keyHint{"c", "container"})
		}
	} else {
		h = append(h, keyHint{"↑↓", "scroll"})
	}
	if p.canShell() {
		h = append(h, keyHint{"s", "shell"})
	}
	return append(h, keyHint{"esc", "back"})
}

func (p *podView) close() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *podView) view(w, h int) string {
	if p.detailErr != nil {
		return fill([]string{"", " " + sRose.Render("✕ ") + sInk.Render("Couldn't read this pod. ") + sMuted.Render(explain(p.detailErr))}, w, h)
	}
	var tabs []string
	for i, name := range podTabs {
		if podTab(i) == p.tab {
			tabs = append(tabs, sTabOn.Render(name))
		} else {
			tabs = append(tabs, sTabOff.Render(name))
		}
	}
	right := ""
	if p.tab == tabLogs && p.container != "" {
		right = sMuted.Render("container ") + sBody.Render(p.container)
		if p.previous {
			right += sMuted.Render(" · previous run")
		}
		right += sMuted.Render(" · ") + p.followWord()
	}
	head := spread(" "+strings.Join(tabs, "   "), right, w-1)
	lines := []string{head}
	if p.searching {
		p.input.SetWidth(w - 4)
		lines = append(lines, " "+p.input.View())
	}
	body := h - len(lines)
	var content string
	switch p.tab {
	case tabLogs:
		content = p.logView(w, body)
	case tabEvents:
		content = p.eventsView(w, body)
	case tabYAML:
		switch {
		case p.yamlErr != nil:
			content = fill([]string{" " + sMuted.Render(explain(p.yamlErr))}, w, body)
		case p.yamlObj == nil:
			content = fill([]string{" " + sMuted.Render("Reading…")}, w, body)
		default:
			content = p.yaml.view(w, body, yamlLine)
		}
	}
	return strings.Join(lines, "\n") + "\n" + content
}

func (p *podView) followWord() string {
	switch {
	case p.logErr != "":
		return sRose.Render("stopped")
	case p.ended:
		return sMuted.Render("ended")
	case p.back > 0:
		return sInk.Render("paused") + sMuted.Render(" · G to follow")
	}
	return lipgloss.NewStyle().Foreground(colLime).Render("●") + sMuted.Render(" following")
}

// logView draws the lines that end p.back lines above the live end.
func (p *podView) logView(w, h int) string {
	p.height = h
	if len(p.lines) == 0 {
		msg := sMuted.Render("Waiting for log lines…")
		switch {
		case p.logErr != "":
			msg = sInk.Render(p.logErr)
		case p.ended:
			msg = sMuted.Render("No log lines.")
		case p.detail == nil:
			msg = sMuted.Render("Reading the pod…")
		}
		return fill([]string{" " + msg}, w, h)
	}
	var out []string
	if p.logErr != "" {
		out = append(out, " "+sRose.Render("✕ ")+sInk.Render(p.logErr))
	}
	room := h - len(out)
	textW := w - 1
	if p.stamps {
		textW -= 9
	}
	var visual []string
	for i := len(p.lines) - 1 - p.back; i >= 0 && len(visual) < room; i-- {
		l := p.lines[i]
		chunks := []string{l.text}
		if p.wrap && textW > 10 {
			chunks = wrapRunes(l.text, textW)
		}
		for j := len(chunks) - 1; j >= 0; j-- {
			prefix := " "
			if p.stamps {
				stamp := strings.Repeat(" ", 8)
				if j == 0 {
					stamp = pad(l.time, 8)
				}
				prefix += sMuted.Render(stamp) + " "
			}
			visual = append(visual, prefix+p.renderLog(clip(chunks[j], textW), l.level))
		}
	}
	// visual is newest first; keep the bottom room lines in order.
	if len(visual) > room {
		visual = visual[:room]
	}
	for i, j := 0, len(visual)-1; i < j; i, j = i+1, j-1 {
		visual[i], visual[j] = visual[j], visual[i]
	}
	return fill(append(out, visual...), w, h)
}

// renderLog colours a line by level and highlights the search.
func (p *podView) renderLog(text string, level logs.Level) string {
	base := sBody
	switch level {
	case logs.Warn:
		base = sWarn
	case logs.Error:
		base = sRose
	}
	if p.search == "" {
		return base.Render(text)
	}
	lower, q := strings.ToLower(text), strings.ToLower(p.search)
	if len(lower) != len(text) {
		// Lowercasing changed the byte layout; don't risk cutting a rune.
		return base.Render(text)
	}
	var b strings.Builder
	at := 0
	for {
		i := strings.Index(lower[at:], q)
		if i < 0 {
			break
		}
		b.WriteString(base.Render(text[at : at+i]))
		b.WriteString(sMatch.Render(text[at+i : at+i+len(q)]))
		at += i + len(q)
	}
	b.WriteString(base.Render(text[at:]))
	return b.String()
}

// wrapRunes splits s into pieces of at most w cells.
func wrapRunes(s string, w int) []string {
	if lipgloss.Width(s) <= w {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	cw := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if cw+rw > w {
			out = append(out, cur.String())
			cur.Reset()
			cw = 0
		}
		cur.WriteRune(r)
		cw += rw
	}
	return append(out, cur.String())
}

func (p *podView) eventsView(w, h int) string {
	switch {
	case p.eventsErr != nil:
		return fill([]string{" " + sMuted.Render(explain(p.eventsErr))}, w, h)
	case p.events == nil:
		return fill([]string{" " + sMuted.Render("Reading events…")}, w, h)
	case len(p.events) == 0:
		return fill([]string{" " + sMuted.Render("No events for this pod. Kubernetes keeps them for about an hour.")}, w, h)
	}
	now := p.a.now()
	var out []string
	for _, e := range p.events {
		icon := sMuted.Render("·")
		if e.Type == "Warning" {
			icon = sInk.Render("⚠")
		}
		count := ""
		if e.Count > 1 {
			count = sMuted.Render(fmt.Sprintf(" ×%d", e.Count))
		}
		head := " " + icon + " " + sInk.Render(e.Reason) + count + "  " + sBody.Render(e.Message)
		out = append(out, spread(clip(head, w-6), sMuted.Render(age(e.Time, now)), w-1))
	}
	return fill(out, w, h)
}
