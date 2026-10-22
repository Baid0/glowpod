// Package tui is Glowpod in the terminal (`glowpod term`): Glance first,
// Deep resource tables one key away, pod logs, YAML and RBAC-aware actions.
// It reads and changes the cluster through a Source, as the user.
package tui

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"glowpod/internal/access"
	"glowpod/internal/kerr"
	"glowpod/internal/resources"
)

// app is what every screen shares.
type app struct {
	src   Source
	ctx   context.Context
	now   func() time.Time
	perms *perms
	types []resources.Type
	// ns is the namespace Glance and Deep's top table show; "" is all.
	ns         string
	namespaces []string
	nextID     int
}

func (a *app) id() int {
	a.nextID++
	return a.nextID
}

// typeOf finds a listable type by resource, kind, short name or
// "resource.group"; core and apps win ties.
func (a *app) typeOf(name string) (resources.Type, bool) {
	name = strings.ToLower(name)
	var best resources.Type
	found := false
	for _, t := range a.types {
		if !t.Can("list") || !matchesType(t, name) {
			continue
		}
		if !found || rank(t) < rank(best) {
			best, found = t, true
		}
	}
	return best, found
}

func matchesType(t resources.Type, name string) bool {
	if name == t.Resource || name == strings.ToLower(t.Kind) || name == t.Resource+"."+t.Group {
		return true
	}
	// Singular, roughly: "pod", "deployment", "ingress".
	if strings.TrimSuffix(t.Resource, "s") == name || strings.TrimSuffix(t.Resource, "es") == name {
		return true
	}
	for _, s := range t.ShortNames {
		if s == name {
			return true
		}
	}
	return false
}

// rank orders types that share a name: core first, then apps, then the rest.
func rank(t resources.Type) int {
	switch t.Group {
	case "":
		return 0
	case "apps":
		return 1
	case "batch", "networking.k8s.io", "rbac.authorization.k8s.io", "storage.k8s.io":
		return 2
	}
	if strings.HasSuffix(t.Group, ".k8s.io") {
		return 3
	}
	return 4
}

// pods is the pods type, for screens that list a workload's or a node's pods.
func (a *app) pods() resources.Type {
	for _, t := range a.types {
		if t.Group == "" && t.Resource == "pods" {
			return t
		}
	}
	// Discovery hasn't answered yet; pods are the same everywhere.
	return resources.Type{
		Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true,
		Verbs: []string{"get", "list", "watch", "delete"}, Subresources: []string{"log", "exec"},
	}
}

// perms caches access reviews for 30 s, per check.
type perms struct {
	mu      sync.Mutex
	entries map[access.Check]permEntry
}

type permEntry struct {
	allowed bool
	pending bool
	at      time.Time
}

const permTTL = 30 * time.Second

func newPerms() *perms { return &perms{entries: map[access.Check]permEntry{}} }

// can reports whether c is allowed, and whether that's known yet.
func (p *perms) can(c access.Check, now time.Time) (allowed, known bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[c]
	if !ok || e.pending || now.Sub(e.at) > permTTL {
		return false, false
	}
	return e.allowed, true
}

// permsMsg says new answers are in; screens redraw their keys.
type permsMsg struct{}

// check asks about the checks that aren't known or pending yet.
func (a *app) check(checks ...access.Check) tea.Cmd {
	now := a.now()
	var ask []access.Check
	a.perms.mu.Lock()
	for _, c := range checks {
		e, ok := a.perms.entries[c]
		if ok && (e.pending || now.Sub(e.at) <= permTTL) {
			continue
		}
		a.perms.entries[c] = permEntry{pending: true}
		ask = append(ask, c)
	}
	a.perms.mu.Unlock()
	if len(ask) == 0 {
		return nil
	}
	ctx, src, p := a.ctx, a.src, a.perms
	return func() tea.Msg {
		allowed, err := src.Allowed(ctx, ask)
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, c := range ask {
			// A failed review hides the action rather than guessing.
			p.entries[c] = permEntry{allowed: err == nil && allowed[i], at: time.Now()}
		}
		return permsMsg{}
	}
}

// can is a.perms.can at the app's clock.
func (a *app) can(c access.Check) bool {
	ok, known := a.perms.can(c, a.now())
	return known && ok
}

// keyHint is one key in the footer and in help.
type keyHint struct {
	key, what string
}

// screen is one page: Glance, a table, a pod, a YAML view.
type screen interface {
	// id addresses async messages to this screen.
	id() int
	init() tea.Cmd
	// key handles a key press; false lets the app handle it.
	key(msg tea.KeyPressMsg) (bool, tea.Cmd)
	// update handles messages addressed to this screen and broadcasts.
	update(msg tea.Msg) tea.Cmd
	view(width, height int) string
	title() string
	// status is the short line next to the title: "● live · 6 pods".
	status() string
	hints() []keyHint
	// capturing is true while the screen takes typed text (a filter).
	capturing() bool
	close()
}

// addressed is an async message for one screen.
type addressed interface{ target() int }

type mode int

const (
	modeGlance mode = iota
	modeDeep
)

// Model is the Bubble Tea model for the whole app.
type Model struct {
	app    *app
	width  int
	height int
	mode   mode
	glance screen
	// deep is Deep mode's stack of screens; the last one shows.
	deep    []screen
	overlay overlay
	flash   flash
	quit    bool
	// pendingResource opens once discovery answers.
	pendingResource string
}

type flash struct {
	text  string
	tone  tone
	until time.Time
}

// overlay sits on top of the screens and takes every key: the command
// palette, help, a dialog.
type overlay interface {
	key(m *Model, msg tea.KeyPressMsg) tea.Cmd
	view(width, height int) string
}

// Options start the app.
type Options struct {
	// Namespace to start in; "" is all.
	Namespace string
	// Resource opens Deep on this resource (e.g. "pods") instead of Glance.
	Resource string
	Now      func() time.Time
}

// New builds the model.
func New(ctx context.Context, src Source, opts Options) *Model {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	a := &app{src: src, ctx: ctx, now: now, perms: newPerms(), ns: opts.Namespace}
	m := &Model{app: a}
	m.glance = newGlance(a)
	if opts.Resource != "" {
		m.mode = modeDeep
		m.pendingResource = opts.Resource
	}
	return m
}

type (
	typesMsg struct {
		types []resources.Type
		err   error
	}
	namespacesMsg struct {
		names []string
	}
	clockMsg   struct{}
	actionDone struct {
		text string
		err  error
	}
	// openMsg pushes a screen onto Deep's stack.
	openMsg struct{ s screen }
)

func clock() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockMsg{} })
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	a := m.app
	return tea.Batch(
		m.glance.init(),
		clock(),
		func() tea.Msg {
			types, err := a.src.Types(a.ctx)
			return typesMsg{types: types, err: err}
		},
		func() tea.Msg {
			names, _, _ := a.src.Namespaces(a.ctx)
			return namespacesMsg{names: names}
		},
	)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m, m.onKey(msg)
	case typesMsg:
		if msg.err != nil {
			m.say(toneCritical, "Couldn't list resource types: "+explain(msg.err))
		}
		m.app.types = msg.types
		if m.pendingResource != "" {
			r := m.pendingResource
			m.pendingResource = ""
			return m, m.openResource(r, m.app.ns)
		}
		return m, nil
	case namespacesMsg:
		m.app.namespaces = msg.names
		return m, nil
	case clockMsg:
		if !m.flash.until.IsZero() && m.app.now().After(m.flash.until) {
			m.flash = flash{}
		}
		return m, clock()
	case actionDone:
		if msg.err != nil {
			m.say(toneCritical, explain(msg.err))
		} else {
			m.say(toneOK, msg.text)
		}
		return m, nil
	case openMsg:
		return m, m.push(msg.s)
	case openDialogMsg:
		m.overlay = msg.d
		return m, nil
	case setNamespaceMsg:
		return m, m.setNamespace(msg.ns)
	case flashMsg:
		m.say(msg.tone, msg.text)
		return m, nil
	case quitMsg:
		m.quit = true
		return m, tea.Quit
	}
	if a, ok := msg.(addressed); ok {
		if s := m.screenByID(a.target()); s != nil {
			return m, s.update(msg)
		}
		return m, nil
	}
	// Broadcasts (permsMsg and the like) go to every live screen.
	cmds := []tea.Cmd{m.glance.update(msg)}
	for _, s := range m.deep {
		cmds = append(cmds, s.update(msg))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) screenByID(id int) screen {
	if m.glance.id() == id {
		return m.glance
	}
	for _, s := range m.deep {
		if s.id() == id {
			return s
		}
	}
	return nil
}

// active is the screen on show.
func (m *Model) active() screen {
	if m.mode == modeDeep && len(m.deep) > 0 {
		return m.deep[len(m.deep)-1]
	}
	return m.glance
}

func (m *Model) onKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		m.quit = true
		return tea.Quit
	}
	if m.overlay != nil {
		return m.overlay.key(m, msg)
	}
	s := m.active()
	if s.capturing() {
		_, cmd := s.key(msg)
		return cmd
	}
	if handled, cmd := s.key(msg); handled {
		return cmd
	}
	switch k {
	case "tab":
		return m.toggleMode()
	case ":", "ctrl+k":
		m.overlay = newPalette(m, "")
	case "n":
		m.overlay = newPalette(m, "ns ")
	case "?":
		m.overlay = newHelp(m)
	case "esc", "q":
		if m.mode == modeDeep && len(m.deep) > 1 {
			m.pop()
			return nil
		}
		if k == "q" {
			m.quit = true
			return tea.Quit
		}
	}
	return nil
}

func (m *Model) toggleMode() tea.Cmd {
	if m.mode == modeGlance {
		m.mode = modeDeep
		if len(m.deep) == 0 {
			return m.openResource("pods", m.app.ns)
		}
		return nil
	}
	m.mode = modeGlance
	return nil
}

// push shows s on top of Deep's stack.
func (m *Model) push(s screen) tea.Cmd {
	m.mode = modeDeep
	m.deep = append(m.deep, s)
	return s.init()
}

func (m *Model) pop() {
	if len(m.deep) == 0 {
		return
	}
	top := m.deep[len(m.deep)-1]
	top.close()
	m.deep = m.deep[:len(m.deep)-1]
}

// openResource shows a table of resource in place of Deep's stack. It
// follows the app's namespace.
func (m *Model) openResource(resource, ns string) tea.Cmd {
	t, ok := m.app.typeOf(resource)
	if !ok {
		if len(m.app.types) == 0 {
			m.pendingResource = resource
			m.mode = modeDeep
			return nil
		}
		m.say(toneHigh, "No resource called "+resource+". Press : to see them all.")
		return nil
	}
	for len(m.deep) > 0 {
		m.pop()
	}
	return m.push(newTable(m.app, t, tableScope{namespace: ns, followsApp: true}))
}

// setNamespace switches the app's namespace; Glance and Deep's top table
// follow it.
func (m *Model) setNamespace(ns string) tea.Cmd {
	m.app.ns = ns
	cmds := []tea.Cmd{m.glance.update(nsChanged{})}
	for _, s := range m.deep {
		cmds = append(cmds, s.update(nsChanged{}))
	}
	if ns == "" {
		m.say(toneOK, "Showing all namespaces")
	} else {
		m.say(toneOK, "Namespace: "+ns)
	}
	return tea.Batch(cmds...)
}

// nsChanged tells screens the app's namespace changed.
type nsChanged struct{}

type (
	flashMsg struct {
		tone tone
		text string
	}
	quitMsg struct{}
)

func say(t tone, text string) tea.Cmd {
	return func() tea.Msg { return flashMsg{tone: t, text: text} }
}

func (m *Model) say(t tone, text string) {
	m.flash = flash{text: text, tone: t, until: m.app.now().Add(6 * time.Second)}
}

// explain turns an error into plain words for the footer.
func explain(err error) string {
	c := kerr.Classify(err)
	if c.Detail != "" {
		return c.Message + " " + c.Detail
	}
	return c.Message
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.BackgroundColor = colCanvas
	v.WindowTitle = "Glowpod"
	return v
}

func (m *Model) render() string {
	if m.quit || m.width == 0 {
		return ""
	}
	w, h := m.width, m.height
	s := m.active()
	bodyH := max(1, h-5)

	header := m.header(w)
	title := spread(sTitle.Render(s.title())+"  "+s.status(), "", w)
	rule := sHair.Render(strings.Repeat("─", w))
	body := s.view(w, bodyH)
	if m.overlay != nil {
		body = placeOverlay(body, m.overlay.view(w, bodyH), w, bodyH)
	}
	return strings.Join([]string{header, title, rule, body, rule, m.footer(s, w)}, "\n")
}

func (m *Model) header(w int) string {
	info := m.app.src.Cluster()
	left := sLime.Render("◉") + " " + sTitle.Render("glowpod") + "  " + sMuted.Render(info.Name)
	ns := "all namespaces"
	if m.app.ns != "" {
		ns = m.app.ns
	}
	glance, deep := sTabOff.Render("Glance"), sTabOff.Render("Deep")
	if m.mode == modeGlance {
		glance = sTabOn.Render("Glance")
	} else {
		deep = sTabOn.Render("Deep")
	}
	right := sBody.Render(ns) + sMuted.Render(" ▾") + "    " + glance + "  " + deep + "    " + hint("?", "help")
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > w {
		// Narrow: the context name goes first.
		left = sLime.Render("◉") + " " + sTitle.Render("glowpod")
	}
	return spread(left, right, w)
}

func (m *Model) footer(s screen, w int) string {
	if m.flash.text != "" {
		icon := lipgloss.NewStyle().Foreground(m.flash.tone.color()).Render("●")
		if m.flash.tone == toneCritical {
			icon = sRose.Render("✕")
		}
		return clip(icon+" "+sInk.Render(m.flash.text), w)
	}
	var parts []string
	for _, h := range s.hints() {
		parts = append(parts, hint(h.key, h.what))
	}
	parts = append(parts, hint("tab", tabWord(m.mode)), hint(":", "command"), hint("n", "namespace"))
	return clip(strings.Join(parts, "  "), w)
}

func tabWord(md mode) string {
	if md == modeGlance {
		return "deep"
	}
	return "glance"
}

// placeOverlay draws box centred over body.
func placeOverlay(body, box string, w, h int) string {
	lines := strings.Split(body, "\n")
	boxLines := strings.Split(box, "\n")
	bw := lipgloss.Width(box)
	top := max(0, (h-len(boxLines))/3)
	left := max(0, (w-bw)/2)
	for i, bl := range boxLines {
		y := top + i
		if y >= len(lines) {
			break
		}
		line := lines[y]
		prefix := ansi.Cut(line, 0, left)
		suffix := ansi.Cut(line, left+bw, w)
		lines[y] = pad(prefix, left) + pad(bl, bw) + suffix
	}
	return strings.Join(lines, "\n")
}
