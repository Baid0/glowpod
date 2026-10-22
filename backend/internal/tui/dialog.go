package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"glowpod/internal/resources"
)

// dialog asks before an action: a yes/no, a number, or a typed name.
type dialog struct {
	title  string
	body   []string
	danger bool
	// input is nil for a yes/no question.
	input *textinput.Model
	// check says what's wrong with the typed value, or "" when it's fine.
	check func(v string) string
	run   func(v string) tea.Cmd
	yes   string // the confirm key's word: "restart", "delete"
	err   string
}

type openDialogMsg struct{ d *dialog }

func openDialog(d *dialog) tea.Cmd {
	return func() tea.Msg { return openDialogMsg{d} }
}

func inputStyles() textinput.Styles {
	s := textinput.DefaultStyles(true)
	s.Focused.Prompt = sMuted
	s.Focused.Text = sInk
	s.Focused.Placeholder = sSoft
	s.Blurred = s.Focused
	s.Cursor.Color = colLime
	return s
}

func newInput(value string) *textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.SetStyles(inputStyles())
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	return &in
}

func (d *dialog) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.overlay = nil
		return nil
	case "enter":
		v := ""
		if d.input != nil {
			v = strings.TrimSpace(d.input.Value())
		}
		if d.check != nil {
			if d.err = d.check(v); d.err != "" {
				return nil
			}
		}
		m.overlay = nil
		return d.run(v)
	case "y":
		if d.input == nil {
			m.overlay = nil
			return d.run("")
		}
	case "n":
		if d.input == nil {
			m.overlay = nil
			return nil
		}
	}
	if d.input == nil {
		return nil
	}
	in, cmd := d.input.Update(msg)
	*d.input = in
	d.err = ""
	return cmd
}

func (d *dialog) view(w, _ int) string {
	width := min(64, w-4)
	lines := []string{sTitle.Render(d.title), ""}
	for _, b := range d.body {
		lines = append(lines, lipgloss.NewStyle().Width(width-6).Foreground(colBody).Render(b))
	}
	if d.input != nil {
		d.input.SetWidth(width - 10)
		lines = append(lines, "", d.input.View())
	}
	if d.err != "" {
		lines = append(lines, sRose.Render("✕ ")+sInk.Render(d.err))
	}
	keys := hint("enter", d.yes) + "  " + hint("esc", "cancel")
	if d.input == nil {
		keys = hint("y", d.yes) + "  " + hint("esc", "cancel")
	}
	lines = append(lines, "", keys)
	box := sDialog
	if d.danger {
		box = sCritBox
	}
	return box.Width(width).Render(strings.Join(lines, "\n"))
}

func kindOf(t resources.Type) string { return strings.ToLower(t.Kind) }

func scaleDialog(a *app, t resources.Type, ns, name string, now int64) *dialog {
	return &dialog{
		title: fmt.Sprintf("Scale %s %s", kindOf(t), name),
		body:  []string{fmt.Sprintf("How many replicas? It wants %d now.", now)},
		input: newInput(strconv.FormatInt(now, 10)),
		yes:   "scale",
		check: func(v string) string {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 || n > resources.MaxReplicas {
				return fmt.Sprintf("Type a number from 0 to %d.", resources.MaxReplicas)
			}
			return ""
		},
		run: func(v string) tea.Cmd {
			n, _ := strconv.ParseInt(v, 10, 64)
			return func() tea.Msg {
				err := a.src.Scale(a.ctx, t, ns, name, n)
				return actionDone{text: fmt.Sprintf("Scaled %s to %d", name, n), err: err}
			}
		},
	}
}

func restartDialog(a *app, t resources.Type, ns, name string) *dialog {
	return &dialog{
		title: fmt.Sprintf("Restart %s %s?", kindOf(t), name),
		body:  []string{"Replaces every pod with a rolling update, like kubectl rollout restart."},
		yes:   "restart",
		run: func(string) tea.Cmd {
			return func() tea.Msg {
				err := a.src.Restart(a.ctx, t, ns, name)
				return actionDone{text: "Restarting " + name, err: err}
			}
		},
	}
}

func deleteDialog(a *app, t resources.Type, ns, name string) *dialog {
	body := []string{"This can't be undone."}
	if hasPods(t) && t.Resource != "services" {
		body[0] += " Its pods go too."
	}
	body = append(body, "Type "+name+" to confirm.")
	return &dialog{
		title:  fmt.Sprintf("Delete %s %s?", kindOf(t), name),
		body:   body,
		danger: true,
		input:  newInput(""),
		yes:    "delete",
		check: func(v string) string {
			if v != name {
				return "The name doesn't match."
			}
			return ""
		},
		run: func(string) tea.Cmd {
			return func() tea.Msg {
				err := a.src.Delete(a.ctx, t, ns, name)
				return actionDone{text: "Deleted " + name, err: err}
			}
		},
	}
}
