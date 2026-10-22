package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"glowpod/internal/resources"
)

// pager scrolls a block of lines.
type pager struct {
	lines  []string
	offset int
	height int
}

func (p *pager) key(k string) bool {
	page := max(1, p.height-1)
	switch k {
	case "down", "j":
		p.offset++
	case "up", "k":
		p.offset--
	case "pgdown", "ctrl+f", "space":
		p.offset += page
	case "pgup", "ctrl+b":
		p.offset -= page
	case "home", "g":
		p.offset = 0
	case "end", "G":
		p.offset = len(p.lines)
	default:
		return false
	}
	return true
}

func (p *pager) view(w, h int, render func(string) string) string {
	p.height = h
	p.offset = max(0, min(p.offset, len(p.lines)-h))
	end := min(len(p.lines), p.offset+h)
	out := make([]string, 0, h)
	for _, l := range p.lines[p.offset:end] {
		out = append(out, " "+render(clip(l, w-1)))
	}
	return fill(out, w, h)
}

// yamlLine tints a YAML line: keys in ink, values in body, comments muted.
func yamlLine(l string) string {
	trimmed := strings.TrimLeft(l, " ")
	indent := l[:len(l)-len(trimmed)]
	if strings.HasPrefix(trimmed, "#") {
		return indent + sMuted.Render(trimmed)
	}
	body := trimmed
	dash := ""
	if rest, ok := strings.CutPrefix(body, "- "); ok {
		dash, body = sMuted.Render("- "), rest
	}
	if k, v, ok := strings.Cut(body, ":"); ok && !strings.ContainsAny(k, " \"'") {
		return indent + dash + sInk.Render(k) + sMuted.Render(":") + sBody.Render(v)
	}
	return indent + dash + sBody.Render(body)
}

// yamlView shows one object's YAML, read-only. Secret values stay hidden.
type yamlView struct {
	a        *app
	sid      int
	t        resources.Type
	ns, name string
	obj      *resources.Object
	err      error
	p        pager
}

type yamlMsg struct {
	to  int
	obj *resources.Object
	err error
}

func (m yamlMsg) target() int { return m.to }

func newYAML(a *app, t resources.Type, ns, name string) *yamlView {
	return &yamlView{a: a, sid: a.id(), t: t, ns: ns, name: name}
}

func (y *yamlView) id() int { return y.sid }

func (y *yamlView) init() tea.Cmd { return loadYAML(y.a, y.sid, y.t, y.ns, y.name) }

func loadYAML(a *app, to int, t resources.Type, ns, name string) tea.Cmd {
	return func() tea.Msg {
		obj, err := a.src.Get(a.ctx, t, ns, name)
		return yamlMsg{to: to, obj: obj, err: err}
	}
}

func (y *yamlView) update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(yamlMsg); ok {
		y.obj, y.err = m.obj, m.err
		if m.obj != nil {
			y.p.lines = strings.Split(strings.TrimRight(m.obj.YAML, "\n"), "\n")
		}
	}
	return nil
}

func (y *yamlView) key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if msg.String() == "ctrl+r" {
		return true, y.init()
	}
	return y.p.key(msg.String()), nil
}

func (y *yamlView) view(w, h int) string {
	switch {
	case y.err != nil:
		return fill([]string{"", " " + sRose.Render("✕ ") + sInk.Render("Couldn't read "+y.name+". ") + sMuted.Render(explain(y.err))}, w, h)
	case y.obj == nil:
		return fill([]string{"", " " + sMuted.Render("Reading "+y.name+"…")}, w, h)
	}
	return y.p.view(w, h, yamlLine)
}

func (y *yamlView) title() string { return kindOf(y.t) + " " + y.name + " · YAML" }

func (y *yamlView) status() string {
	s := sMuted.Render("read-only")
	if y.obj != nil && y.obj.Redacted {
		s += sMuted.Render(" · values hidden")
	}
	return s
}

func (y *yamlView) hints() []keyHint {
	return []keyHint{{"↑↓", "scroll"}, {"g G", "top/end"}, {"esc", "back"}}
}

func (y *yamlView) capturing() bool { return false }
func (y *yamlView) close()          {}
