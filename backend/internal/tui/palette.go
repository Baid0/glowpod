package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const paletteRows = 10

// item is one palette suggestion.
type item struct {
	// text is what Tab completes to; keys are what it matches.
	text string
	keys []string
	desc string
	run  func(m *Model, args []string) tea.Cmd
}

// palette is the command palette: resources, namespaces, screens and the
// selected row's actions, matched as you type.
type palette struct {
	input   textinput.Model
	items   []item
	matches []item
	sel     int
}

func newPalette(m *Model, start string) *palette {
	in := textinput.New()
	in.Prompt = ": "
	in.Placeholder = "pods, deploy, ns kube-system, logs, help…"
	in.SetStyles(inputStyles())
	in.SetValue(start)
	in.CursorEnd()
	in.Focus()
	p := &palette{input: in, items: paletteItems(m)}
	p.refresh()
	return p
}

func paletteItems(m *Model) []item {
	var items []item
	for _, s := range []struct{ name, desc string }{
		{"glance", "Overview: gauges, what needs attention"},
		{"deep", "Resource tables"},
		{"help", "Every key"},
		{"quit", "Leave Glowpod"},
	} {
		name := s.name
		items = append(items, item{text: name, keys: []string{name}, desc: s.desc, run: func(m *Model, _ []string) tea.Cmd {
			switch name {
			case "glance":
				m.mode = modeGlance
			case "deep":
				m.mode = modeGlance
				return m.toggleMode()
			case "help":
				m.overlay = newHelp(m)
			case "quit":
				m.quit = true
				return tea.Quit
			}
			return nil
		}})
	}
	// Actions on the selected row, only when allowed.
	if t, ok := m.active().(*table); ok {
		for _, a := range []struct{ name, key, desc string }{
			{"logs", "l", "Logs of the selected pod"},
			{"yaml", "y", "YAML of the selected row"},
			{"shell", "s", "Shell in the selected pod"},
			{"scale", "s", "Scale the selected workload"},
			{"restart", "r", "Restart the selected workload"},
			{"delete", "ctrl+d", "Delete the selected row"},
		} {
			if a.name != "yaml" && !t.allowed(a.name) {
				continue
			}
			key := a.key
			items = append(items, item{text: a.name, keys: []string{a.name}, desc: a.desc, run: func(*Model, []string) tea.Cmd {
				_, cmd := t.key(keyPress(key))
				return cmd
			}})
		}
	}
	items = append(items, item{text: "ns all", keys: []string{"ns all", "namespace all"}, desc: "All namespaces", run: func(m *Model, _ []string) tea.Cmd {
		return m.setNamespace("")
	}})
	for _, ns := range m.app.namespaces {
		items = append(items, item{text: "ns " + ns, keys: []string{"ns " + ns, "namespace " + ns}, desc: "Namespace", run: func(m *Model, _ []string) tea.Cmd {
			return m.setNamespace(ns)
		}})
	}
	for _, t := range m.app.types {
		if !t.Can("list") {
			continue
		}
		keys := append([]string{t.Resource, strings.ToLower(t.Kind)}, t.ShortNames...)
		gv := t.Version
		if t.Group != "" {
			gv = t.Group + "/" + t.Version
			keys = append(keys, t.Resource+"."+t.Group)
		}
		desc := t.Kind + " · " + gv
		if len(t.ShortNames) > 0 {
			desc += " · " + strings.Join(t.ShortNames, ", ")
		}
		res := t.Resource
		if t.Group != "" && rank(t) > 1 {
			if other, ok := m.app.typeOf(t.Resource); ok && other.Group != t.Group {
				res = t.Resource + "." + t.Group
			}
		}
		items = append(items, item{text: res, keys: keys, desc: desc, run: func(m *Model, args []string) tea.Cmd {
			return openWithNamespace(m, res, args)
		}})
	}
	return items
}

// keyPress makes a key event for a palette action that reuses a key binding.
func keyPress(k string) tea.KeyPressMsg {
	if k == "ctrl+d" {
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// score ranks how well query matches key: exact, prefix, substring, then
// letters in order. 0 means no match.
func score(query, key string) int {
	switch {
	case query == "":
		return 1
	case key == query:
		return 1000
	case strings.HasPrefix(key, query):
		return 500 - len(key)
	case strings.Contains(key, query):
		return 200 - len(key)
	}
	i := 0
	for _, r := range key {
		if i < len(query) && r == rune(query[i]) {
			i++
		}
	}
	if i < len(query) {
		return 0
	}
	// Letters in order, better when the first one starts the word.
	if key[0] == query[0] {
		return 75 - len(key)
	}
	return 50 - len(key)
}

func (p *palette) refresh() {
	q := strings.ToLower(strings.TrimLeft(p.input.Value(), " "))
	// "pods kube-system": match the first word, the rest are arguments.
	if first, _, ok := strings.Cut(q, " "); ok && first != "ns" && first != "namespace" {
		q = first
	}
	type scored struct {
		it item
		s  int
	}
	var all []scored
	for _, it := range p.items {
		best := 0
		for _, k := range it.keys {
			best = max(best, score(q, strings.ToLower(k)))
		}
		if best > 0 {
			all = append(all, scored{it, best})
		}
	}
	slices.SortStableFunc(all, func(a, b scored) int { return cmp.Compare(b.s, a.s) })
	p.matches = p.matches[:0]
	for _, s := range all {
		p.matches = append(p.matches, s.it)
	}
	p.sel = min(p.sel, max(0, len(p.matches)-1))
}

func (p *palette) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.overlay = nil
		return nil
	case "down", "ctrl+n":
		p.sel = min(p.sel+1, max(0, len(p.matches)-1))
		return nil
	case "up", "ctrl+p":
		p.sel = max(p.sel-1, 0)
		return nil
	case "tab":
		if len(p.matches) > 0 {
			p.input.SetValue(p.matches[p.sel].text + " ")
			p.input.CursorEnd()
			p.refresh()
		}
		return nil
	case "enter":
		m.overlay = nil
		return p.run(m)
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refresh()
	return cmd
}

// run does what was typed: an exact command or resource with its
// arguments, else the highlighted suggestion.
func (p *palette) run(m *Model) tea.Cmd {
	fields := strings.Fields(p.input.Value())
	if len(fields) == 0 {
		return nil
	}
	word := strings.ToLower(fields[0])
	switch word {
	case "q", "q!", "quit", "exit":
		m.quit = true
		return tea.Quit
	case "ns", "namespace":
		if len(fields) < 2 || fields[1] == "all" || fields[1] == "-A" {
			return m.setNamespace("")
		}
		return m.setNamespace(fields[1])
	}
	for _, it := range p.items {
		if slices.Contains(it.keys, word) {
			return it.run(m, fields[1:])
		}
	}
	if _, ok := m.app.typeOf(word); ok {
		return openWithNamespace(m, word, fields[1:])
	}
	if len(p.matches) > 0 {
		return p.matches[p.sel].run(m, fields[1:])
	}
	return say(toneHigh, fmt.Sprintf("Nothing called %q. Press : and start typing to see what there is.", fields[0]))
}

// openWithNamespace opens resource's table; a first argument picks the
// namespace ("all" or -A for every one).
func openWithNamespace(m *Model, resource string, args []string) tea.Cmd {
	if len(args) == 0 {
		return m.openResource(resource, m.app.ns)
	}
	ns := args[0]
	if ns == "all" || ns == "-A" {
		ns = ""
	}
	return tea.Batch(m.setNamespace(ns), m.openResource(resource, ns))
}

func (p *palette) view(w, _ int) string {
	width := min(78, w-4)
	p.input.SetWidth(width - 8)
	lines := []string{p.input.View(), sHair.Render(strings.Repeat("─", width-4))}
	start := max(0, p.sel-paletteRows+1)
	for i := start; i < min(len(p.matches), start+paletteRows); i++ {
		it := p.matches[i]
		marker := "  "
		text := sBody.Render(it.text)
		if i == p.sel {
			marker = sLime.Render("▌") + " "
			text = sTitle.Render(it.text)
		}
		lines = append(lines, spread(marker+text, sMuted.Render(clip(it.desc, width/2)), width-4))
	}
	if len(p.matches) == 0 {
		lines = append(lines, sMuted.Render("  No matches."))
	}
	lines = append(lines, "", hint("enter", "run")+"  "+hint("tab", "complete")+"  "+hint("↑↓", "pick")+"  "+hint("esc", "close"))
	return sDialog.Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}

// help lists every key for the screen on show.
type help struct {
	lines []string
}

func newHelp(m *Model) *help {
	section := func(title string, hs []keyHint) []string {
		out := []string{label(title)}
		for _, h := range hs {
			out = append(out, "  "+pad(sKey.Render(h.key), 10)+" "+sBody.Render(h.what))
		}
		return append(out, "")
	}
	var lines []string
	lines = append(lines, section("This screen", m.active().hints())...)
	lines = append(lines, section("Everywhere", []keyHint{
		{"tab", "Glance ↔ Deep"},
		{": ctrl+k", "command palette: pods, deploy, ns <name>, help…"},
		{"n", "pick a namespace"},
		{"/", "filter the list (-l key=value for labels)"},
		{"esc q", "back; q on the first screen quits"},
		{"ctrl+c", "quit"},
	})...)
	lines = append(lines, section("Lists", []keyHint{
		{"↑↓ j k", "move"}, {"g G", "top, bottom"}, {"enter", "open"}, {"y", "YAML"}, {"w", "wide columns"},
	})...)
	lines = append(lines, sMuted.Render("Actions you can't take here aren't shown."))
	return &help{lines: lines}
}

func (h *help) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "?", "enter":
		m.overlay = nil
	}
	return nil
}

func (h *help) view(w, _ int) string {
	width := min(72, w-4)
	return sDialog.Padding(0, 2).Width(width).Render(sTitle.Render("Keys") + "\n" + lipgloss.JoinVertical(lipgloss.Left, h.lines...))
}
