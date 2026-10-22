package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"glowpod/internal/access"
	"glowpod/internal/resources"
)

const (
	// maxRows caps one table; past it, people narrow with a namespace or filter.
	maxRows     = 5000
	tableReload = time.Second
)

// tableScope says which objects a table lists.
type tableScope struct {
	namespace string // "" is all namespaces
	label     string // label selector
	field     string // field selector
	// about is shown after the resource name, e.g. "of deploy/web".
	about string
	// followsApp tables switch with the app's namespace.
	followsApp bool
}

// table is a Deep-mode list of any resource, with the API server's columns.
type table struct {
	a   *app
	sid int
	t   resources.Type
	sc  tableScope

	data      *resources.Table
	err       error
	loaded    bool
	loadedAt  time.Time
	truncated bool

	rows      []int // indexes into data.Rows that pass the filter
	cursor    int
	offset    int
	filter    string
	filtering bool
	input     textinput.Model
	wide      bool
	height    int

	cancel   context.CancelFunc
	gen      int
	reloadAt bool
}

type (
	tableMsg struct {
		to, gen   int
		data      *resources.Table
		truncated bool
		err       error
	}
	tableWatch struct {
		to, gen int
		ch      <-chan struct{}
	}
	tableReloadMsg struct{ to, gen int }
	// setNamespaceMsg asks the app to switch namespace.
	setNamespaceMsg struct{ ns string }
)

func (m tableMsg) target() int       { return m.to }
func (m tableWatch) target() int     { return m.to }
func (m tableReloadMsg) target() int { return m.to }

func newTable(a *app, t resources.Type, sc tableScope) *table {
	if !t.Namespaced {
		sc.namespace = ""
	}
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "filter, or -l key=value"
	in.SetStyles(inputStyles())
	return &table{a: a, sid: a.id(), t: t, sc: sc, input: in}
}

func (t *table) id() int { return t.sid }

func (t *table) init() tea.Cmd {
	return tea.Batch(t.load(), t.watch())
}

func (t *table) load() tea.Cmd {
	a, typ, sc, to, gen := t.a, t.t, t.sc, t.sid, t.gen
	return func() tea.Msg {
		opts := resources.ListOptions{Namespace: sc.namespace, LabelSelector: sc.label, FieldSelector: sc.field, Limit: resources.MaxLimit}
		var all *resources.Table
		for {
			page, err := a.src.List(a.ctx, typ, opts)
			if err != nil {
				return tableMsg{to: to, gen: gen, err: err}
			}
			if all == nil {
				all = page
			} else {
				all.Rows = append(all.Rows, page.Rows...)
			}
			if page.Continue == "" {
				return tableMsg{to: to, gen: gen, data: all}
			}
			if len(all.Rows) >= maxRows {
				return tableMsg{to: to, gen: gen, data: all, truncated: true}
			}
			opts.Continue = page.Continue
		}
	}
}

// watch (re)starts the watch that keeps the table live.
func (t *table) watch() tea.Cmd {
	if t.cancel != nil {
		t.cancel()
	}
	ctx, cancel := context.WithCancel(t.a.ctx)
	t.cancel = cancel
	ch := make(chan struct{}, 1)
	src, typ, sc := t.a.src, t.t, t.sc
	go func() {
		defer close(ch)
		src.Watch(ctx, typ, sc.namespace, sc.label, sc.field, notify(ch))
	}()
	to, gen := t.sid, t.gen
	return waitChanged(ch, func() tea.Msg { return tableWatch{to: to, gen: gen, ch: ch} })
}

func (t *table) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tableMsg:
		if msg.gen != t.gen {
			return nil
		}
		t.loaded, t.err = true, msg.err
		if msg.err == nil {
			t.data, t.truncated, t.loadedAt = msg.data, msg.truncated, t.a.now()
		}
		t.refilter()
		return t.permCheck()
	case tableWatch:
		if msg.gen != t.gen {
			return nil
		}
		next := waitChanged(msg.ch, func() tea.Msg { return msg })
		if t.reloadAt {
			return next
		}
		t.reloadAt = true
		to, gen := t.sid, t.gen
		return tea.Batch(next, tea.Tick(tableReload, func(time.Time) tea.Msg { return tableReloadMsg{to: to, gen: gen} }))
	case tableReloadMsg:
		if msg.gen != t.gen {
			return nil
		}
		t.reloadAt = false
		return t.load()
	case nsChanged:
		if !t.sc.followsApp || !t.t.Namespaced {
			return nil
		}
		t.sc.namespace = t.a.ns
		return t.restart()
	}
	return nil
}

// restart reloads from scratch after the scope changed.
func (t *table) restart() tea.Cmd {
	t.gen++
	t.loaded, t.data, t.err, t.cursor, t.offset, t.reloadAt = false, nil, nil, 0, 0, false
	t.rows = nil
	return tea.Batch(t.load(), t.watch())
}

// refilter applies the filter and keeps the cursor on a row.
func (t *table) refilter() {
	t.rows = t.rows[:0]
	if t.data == nil {
		return
	}
	q := strings.ToLower(t.filter)
	for i, r := range t.data.Rows {
		if q == "" || rowMatches(r, q) {
			t.rows = append(t.rows, i)
		}
	}
	t.cursor = min(t.cursor, max(0, len(t.rows)-1))
}

func rowMatches(r resources.Row, q string) bool {
	if strings.Contains(strings.ToLower(r.Object.Name), q) || strings.Contains(strings.ToLower(r.Object.Namespace), q) {
		return true
	}
	for _, c := range r.Cells {
		if strings.Contains(strings.ToLower(cellText(c)), q) {
			return true
		}
	}
	return false
}

// current is the selected row, if any.
func (t *table) current() (resources.Row, bool) {
	if len(t.rows) == 0 || t.data == nil {
		return resources.Row{}, false
	}
	return t.data.Rows[t.rows[t.cursor]], true
}

// actions are the row actions this type has, with the access check each needs.
func actions(t resources.Type, ns string) map[string]access.Check {
	g, r := t.URLGroup(), t.Resource
	out := map[string]access.Check{}
	if t.Group == "" && r == "pods" {
		out["logs"] = access.Check{Verb: "get", Group: resources.CoreGroup, Resource: "pods", Subresource: "log", Namespace: ns}
		out["shell"] = access.Check{Verb: "create", Group: resources.CoreGroup, Resource: "pods", Subresource: "exec", Namespace: ns}
	}
	if t.Has("scale") {
		out["scale"] = access.Check{Verb: "patch", Group: g, Resource: r, Subresource: "scale", Namespace: ns}
	}
	if resources.CanRestart(t) && t.Can("patch") {
		out["restart"] = access.Check{Verb: "patch", Group: g, Resource: r, Namespace: ns}
	}
	if t.Can("delete") {
		out["delete"] = access.Check{Verb: "delete", Group: g, Resource: r, Namespace: ns}
	}
	return out
}

// allowed reports whether action is offered on the selected row.
func (t *table) allowed(action string) bool {
	row, ok := t.current()
	if !ok {
		return false
	}
	c, ok := actions(t.t, row.Object.Namespace)[action]
	return ok && t.a.can(c)
}

// permCheck asks about the selected row's namespace.
func (t *table) permCheck() tea.Cmd {
	row, ok := t.current()
	if !ok {
		return nil
	}
	var checks []access.Check
	for _, c := range actions(t.t, row.Object.Namespace) {
		checks = append(checks, c)
	}
	return t.a.check(checks...)
}

func (t *table) capturing() bool { return t.filtering }

func (t *table) key(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if t.filtering {
		return true, t.filterKey(msg)
	}
	k := msg.String()
	page := max(1, t.height-2)
	moved := true
	switch k {
	case "down", "j":
		t.cursor++
	case "up", "k":
		t.cursor--
	case "pgdown", "ctrl+f":
		t.cursor += page
	case "pgup", "ctrl+b":
		t.cursor -= page
	case "home", "g":
		t.cursor = 0
	case "end", "G":
		t.cursor = len(t.rows) - 1
	default:
		moved = false
	}
	if moved {
		t.cursor = max(0, min(t.cursor, len(t.rows)-1))
		return true, t.permCheck()
	}

	switch k {
	case "/":
		t.filtering = true
		t.input.SetValue(t.filter)
		if t.sc.label != "" && t.filter == "" {
			t.input.SetValue("-l " + t.sc.label)
		}
		t.input.CursorEnd()
		return true, t.input.Focus()
	case "esc":
		if t.filter != "" {
			t.filter = ""
			t.refilter()
			return true, nil
		}
		return false, nil
	case "w":
		t.wide = !t.wide
		return true, nil
	case "enter":
		return true, t.open()
	case "y":
		row, ok := t.current()
		if !ok {
			return true, nil
		}
		return true, openScreen(newYAML(t.a, t.t, row.Object.Namespace, row.Object.Name))
	case "l":
		if t.allowed("logs") {
			row, _ := t.current()
			return true, openScreen(newPodView(t.a, row.Object.Namespace, row.Object.Name))
		}
	case "s":
		row, _ := t.current()
		if t.allowed("shell") {
			return true, shellCmd(t.a, row.Object.Namespace, row.Object.Name, "")
		}
		if t.allowed("scale") {
			return true, openDialog(scaleDialog(t.a, t.t, row.Object.Namespace, row.Object.Name, replicasOf(t.data, row)))
		}
	case "r":
		if t.allowed("restart") {
			row, _ := t.current()
			return true, openDialog(restartDialog(t.a, t.t, row.Object.Namespace, row.Object.Name))
		}
	case "ctrl+d":
		if t.allowed("delete") {
			row, _ := t.current()
			return true, openDialog(deleteDialog(t.a, t.t, row.Object.Namespace, row.Object.Name))
		}
	case "ctrl+r":
		return true, t.load()
	}
	return false, nil
}

func (t *table) filterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		t.filtering = false
		t.input.Blur()
		v := strings.TrimSpace(t.input.Value())
		if sel, ok := strings.CutPrefix(v, "-l"); ok {
			sel = strings.TrimSpace(sel)
			if _, err := labels.Parse(sel); err != nil {
				return say(toneCritical, "That isn't a valid label selector: "+sel)
			}
			t.filter = ""
			if sel != t.sc.label {
				t.sc.label = sel
				return t.restart()
			}
			return nil
		}
		t.filter = v
		t.refilter()
		return t.permCheck()
	case "esc":
		t.filtering = false
		t.input.Blur()
		t.filter = ""
		t.refilter()
		return nil
	}
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	if v := t.input.Value(); !strings.HasPrefix(v, "-l") {
		t.filter = v
		t.refilter()
	}
	return tea.Batch(cmd, t.permCheck())
}

// open drills into the selected row.
func (t *table) open() tea.Cmd {
	row, ok := t.current()
	if !ok {
		return nil
	}
	a, typ := t.a, t.t
	ns, name := row.Object.Namespace, row.Object.Name
	switch {
	case typ.Group == "" && typ.Resource == "pods":
		return openScreen(newPodView(a, ns, name))
	case typ.Group == "" && typ.Resource == "namespaces":
		return func() tea.Msg { return setNamespaceMsg{ns: name} }
	case typ.Group == "" && typ.Resource == "nodes":
		return openScreen(newTable(a, a.pods(), tableScope{field: "spec.nodeName=" + name, about: "on node " + name}))
	case hasPods(typ):
		return func() tea.Msg {
			obj, err := a.src.Get(a.ctx, typ, ns, name)
			if err != nil {
				return actionDone{err: err}
			}
			sel, err := podSelector(obj.Object)
			if err != nil || sel == "" {
				return openMsg{newYAML(a, typ, ns, name)}
			}
			about := fmt.Sprintf("of %s/%s", strings.ToLower(typ.Kind), name)
			return openMsg{newTable(a, a.pods(), tableScope{namespace: ns, label: sel, about: about})}
		}
	}
	return openScreen(newYAML(a, typ, ns, name))
}

// hasPods is true for types whose spec.selector picks pods.
func hasPods(t resources.Type) bool {
	switch t.Group + "/" + t.Resource {
	case "apps/deployments", "apps/statefulsets", "apps/daemonsets", "apps/replicasets", "batch/jobs", "/services", "/replicationcontrollers":
		return true
	}
	return false
}

// podSelector reads spec.selector: a LabelSelector for workloads, a plain
// map for Services and ReplicationControllers.
func podSelector(obj map[string]any) (string, error) {
	spec, _ := obj["spec"].(map[string]any)
	raw, ok := spec["selector"]
	if !ok {
		return "", nil
	}
	if m, ok := raw.(map[string]any); ok {
		_, hasLabels := m["matchLabels"]
		_, hasExpr := m["matchExpressions"]
		if !hasLabels && !hasExpr {
			set := labels.Set{}
			for k, v := range m {
				s, _ := v.(string)
				set[k] = s
			}
			return labels.SelectorFromSet(set).String(), nil
		}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("read selector: %w", err)
	}
	var ls metav1.LabelSelector
	if err := json.Unmarshal(b, &ls); err != nil {
		return "", fmt.Errorf("read selector: %w", err)
	}
	sel, err := metav1.LabelSelectorAsSelector(&ls)
	if err != nil {
		return "", fmt.Errorf("read selector: %w", err)
	}
	return sel.String(), nil
}

// replicasOf reads the desired count from a "Ready 2/3"-style or
// "Desired" column, for the scale dialog's starting value.
func replicasOf(data *resources.Table, row resources.Row) int64 {
	for i, c := range data.Columns {
		if i >= len(row.Cells) {
			break
		}
		switch strings.ToLower(c.Name) {
		case "desired", "replicas":
			if f, ok := row.Cells[i].(float64); ok {
				return int64(f)
			}
		case "ready":
			var ready, want int64
			if _, err := fmt.Sscanf(cellText(row.Cells[i]), "%d/%d", &ready, &want); err == nil {
				return want
			}
		}
	}
	return 1
}

func openScreen(s screen) tea.Cmd {
	return func() tea.Msg { return openMsg{s} }
}

func (t *table) title() string {
	where := "all namespaces"
	if !t.t.Namespaced {
		where = "cluster"
	} else if t.sc.namespace != "" {
		where = t.sc.namespace
	}
	title := t.t.Resource
	if t.sc.about != "" {
		title += " " + t.sc.about
	}
	return title + " · " + where
}

func (t *table) status() string {
	if !t.loaded {
		return sMuted.Render("loading…")
	}
	if t.err != nil {
		return status(toneCritical, "Couldn't load")
	}
	n := len(t.data.Rows)
	count := fmt.Sprintf("%d", n)
	if len(t.rows) != n {
		count = fmt.Sprintf("%d of %d", len(t.rows), n)
	}
	if t.truncated {
		count += "+"
	}
	return status(toneOK, "live") + sMuted.Render(" · "+count)
}

func (t *table) hints() []keyHint {
	h := []keyHint{{"enter", "open"}, {"/", "filter"}, {"y", "YAML"}}
	for _, a := range []struct{ action, key, what string }{
		{"logs", "l", "logs"}, {"shell", "s", "shell"}, {"scale", "s", "scale"},
		{"restart", "r", "restart"}, {"delete", "ctrl+d", "delete"},
	} {
		if t.allowed(a.action) && (a.action != "scale" || !t.allowed("shell")) {
			h = append(h, keyHint{a.key, a.what})
		}
	}
	return append(h, keyHint{"w", "wide"})
}

func (t *table) close() {
	if t.cancel != nil {
		t.cancel()
	}
}

// column is one column on screen.
type column struct {
	name    string
	idx     int // into Cells; -1 for the namespace
	numeric bool
	status  bool
	// age cells are worked out from the creation time, so they stay current
	// between reloads.
	age   bool
	width int
}

func (t *table) columns(w int) []column {
	var cols []column
	if t.t.Namespaced && t.sc.namespace == "" {
		cols = append(cols, column{name: "NAMESPACE", idx: -1})
	}
	for i, c := range t.data.Columns {
		if c.Priority > 0 && !t.wide {
			continue
		}
		cols = append(cols, column{
			name:    strings.ToUpper(c.Name),
			idx:     i,
			numeric: c.Type == "integer" || c.Type == "number",
			status:  strings.EqualFold(c.Name, "status"),
			age:     strings.EqualFold(c.Name, "age"),
		})
	}
	for i := range cols {
		cols[i].width = len(cols[i].name)
		limit := 40
		if i == 0 || (i == 1 && cols[0].idx == -1) {
			limit = 64 // the name
		}
		for n, ri := range t.rows {
			if n > 2000 {
				break
			}
			cw := len([]rune(t.cellOf(t.data.Rows[ri], cols[i])))
			if cols[i].status {
				cw += 2
			}
			cols[i].width = min(limit, max(cols[i].width, cw))
		}
	}
	// Shrink the widest columns until the table fits, then drop columns.
	avail := func() int { return w - 2 - 2*(len(cols)-1) }
	total := func() int {
		s := 0
		for _, c := range cols {
			s += c.width
		}
		return s
	}
	for total() > avail() {
		widest := 0
		for i, c := range cols {
			if c.width > cols[widest].width {
				widest = i
			}
		}
		if cols[widest].width <= 8 {
			if len(cols) == 1 {
				break
			}
			cols = cols[:len(cols)-1]
			continue
		}
		cols[widest].width--
	}
	return cols
}

func (t *table) cellOf(r resources.Row, c column) string {
	if c.idx < 0 {
		return r.Object.Namespace
	}
	if c.idx >= len(r.Cells) {
		return ""
	}
	if c.age && r.Object.CreationTimestamp != nil {
		return age(*r.Object.CreationTimestamp, t.a.now())
	}
	return cellText(r.Cells[c.idx])
}

// cellText formats a Table cell the way kubectl does.
func cellText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (t *table) view(w, h int) string {
	var out []string
	if t.filtering {
		t.input.SetWidth(w - 4)
		out = append(out, " "+t.input.View())
	} else if t.filter != "" || t.sc.label != "" || t.sc.field != "" {
		var parts []string
		if t.filter != "" {
			parts = append(parts, sInk.Render("/ "+t.filter))
		}
		if t.sc.label != "" {
			parts = append(parts, sMuted.Render("labels ")+sBody.Render(t.sc.label))
		}
		if t.sc.field != "" {
			parts = append(parts, sMuted.Render("fields ")+sBody.Render(t.sc.field))
		}
		out = append(out, " "+strings.Join(parts, sMuted.Render("  ·  ")))
	}
	t.height = h - len(out) - 1

	switch {
	case !t.loaded:
		out = append(out, "", " "+sMuted.Render("Reading "+t.t.Resource+"…"))
		return fill(out, w, h)
	case t.err != nil:
		out = append(out, "", " "+sRose.Render("✕ ")+sInk.Render("Couldn't list "+t.t.Resource+". ")+sMuted.Render(explain(t.err)))
		return fill(out, w, h)
	case len(t.data.Rows) == 0:
		out = append(out, "", " "+sMuted.Render("No "+t.t.Resource+" "+t.where()+"."))
		return fill(out, w, h)
	case len(t.rows) == 0:
		out = append(out, "", " "+sMuted.Render("Nothing matches “"+t.filter+"”. Esc clears the filter."))
		return fill(out, w, h)
	}

	cols := t.columns(w)
	var head []string
	for _, c := range cols {
		if c.numeric {
			head = append(head, padLeft(c.name, c.width))
		} else {
			head = append(head, pad(c.name, c.width))
		}
	}
	out = append(out, "  "+sLabel.Render(strings.Join(head, "  ")))

	visible := max(1, t.height)
	if t.truncated {
		visible--
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}
	t.offset = max(0, min(t.offset, len(t.rows)-visible))
	for i := t.offset; i < min(len(t.rows), t.offset+visible); i++ {
		out = append(out, t.renderRow(t.data.Rows[t.rows[i]], cols, i == t.cursor))
	}
	if t.truncated {
		out = append(out, " "+sMuted.Render(fmt.Sprintf("Showing the first %d. Pick a namespace or use / -l to narrow it.", len(t.data.Rows))))
	}
	return fill(out, w, h)
}

func (t *table) where() string {
	switch {
	case !t.t.Namespaced:
		return "in this cluster"
	case t.sc.namespace == "":
		return "in any namespace"
	}
	return "in " + t.sc.namespace
}

func (t *table) renderRow(r resources.Row, cols []column, selected bool) string {
	text := sBody
	marker := "  "
	if selected {
		text = sInk
		marker = sLime.Render("▌") + " "
	}
	cells := make([]string, 0, len(cols))
	for i, c := range cols {
		v := t.cellOf(r, c)
		switch {
		case c.status && v != "":
			cells = append(cells, pad(status(statusTone(v), v), c.width))
		case c.numeric:
			cells = append(cells, text.Render(padLeft(v, c.width)))
		case i == 0 && selected:
			cells = append(cells, sTitle.Render(pad(v, c.width)))
		default:
			cells = append(cells, text.Render(pad(v, c.width)))
		}
	}
	return marker + strings.Join(cells, "  ")
}

// statusTone colours a STATUS cell: lime when fine, white while waiting,
// rose when broken. Unknown words stay muted.
func statusTone(s string) tone {
	worst := toneNone
	for _, part := range strings.Split(s, ",") {
		worst = max(worst, wordTone(strings.TrimSpace(part)))
	}
	return worst
}

func wordTone(s string) tone {
	w := strings.ToLower(s)
	if rest, ok := strings.CutPrefix(w, "init:"); ok {
		if t := wordTone(rest); t == toneCritical {
			return t
		}
		return toneHigh
	}
	switch w {
	case "running", "ready", "succeeded", "completed", "complete", "bound", "active", "available",
		"true", "healthy", "synced", "deployed", "established":
		return toneOK
	case "pending", "containercreating", "podinitializing", "terminating", "waiting", "progressing",
		"schedulingdisabled", "released", "unknown", "suspended":
		return toneHigh
	case "crashloopbackoff", "error", "oomkilled", "failed", "imagepullbackoff", "errimagepull",
		"createcontainerconfigerror", "createcontainererror", "runcontainererror", "invalidimagename",
		"evicted", "notready", "lost", "backoff", "deadlineexceeded", "containerstatusunknown",
		"false", "degraded", "outofsync":
		return toneCritical
	}
	return toneNone
}
