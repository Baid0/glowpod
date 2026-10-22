package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"glowpod/internal/metrics"
)

// The half-gauge is drawn with Braille dots: each terminal cell holds 2×4
// dots, which are close to square, so the arc stays round.

// dot bits of a Braille cell, by [x][y].
var brailleBit = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// Dot classes, in the order a cell picks its colour.
const (
	clsNone = iota
	clsTick
	clsReq
	clsTrack
	clsValue
	clsText
)

type arcSpec struct {
	width int     // in cells; the height follows
	frac  float64 // value as a fraction of max, 0..1
	// req is the requested fraction for the thin inner arc; < 0 for none.
	req        float64
	thresholds [2]float64
	tone       tone
	// dotted draws only a dotted track: no data to show.
	dotted bool
	// center is written in the middle of the bottom row, e.g. "62%".
	center string
}

type arcCell struct {
	bits rune
	cls  int
	text rune
}

// drawArc returns the gauge's lines.
func drawArc(a arcSpec) []string {
	w := max(a.width, 6)
	pw := 2 * w
	r := float64(pw) / 2
	h := int(math.Ceil(r / 4))
	cx, cy := r, float64(h*4)
	ringOut := r - 1.5
	thick := max(2.0, math.Round(r/5))
	ringIn := ringOut - thick
	frac := math.Max(0, math.Min(1, a.frac))

	cells := make([][]arcCell, h)
	for y := range cells {
		cells[y] = make([]arcCell, w)
	}
	mark := func(px, py, cls int) {
		c := &cells[py/4][px/2]
		c.bits |= brailleBit[px%2][py%4]
		c.cls = max(c.cls, cls)
	}
	near := func(f, at float64) bool { return math.Abs(f-at) < 0.012 }

	for py := range h * 4 {
		for px := range pw {
			dx, dy := float64(px)+0.5-cx, cy-(float64(py)+0.5)
			if dy < 0 {
				continue
			}
			d := math.Hypot(dx, dy)
			// f runs from 0 at the left end to 1 at the right end.
			f := (math.Pi - math.Atan2(dy, dx)) / math.Pi
			switch {
			case d >= ringIn && d <= ringOut:
				switch {
				case a.dotted:
					if (px+py)%2 == 0 {
						mark(px, py, clsTrack)
					}
				case f <= frac:
					mark(px, py, clsValue)
				default:
					mark(px, py, clsTrack)
				}
			case d > ringOut && d <= r && !a.dotted:
				if near(f, a.thresholds[0]) || near(f, a.thresholds[1]) {
					mark(px, py, clsTick)
				}
			case d >= ringIn-2 && d < ringIn-1 && !a.dotted && a.req >= 0 && f <= a.req:
				mark(px, py, clsReq)
			}
		}
	}

	if a.center != "" {
		row := cells[h-1]
		text := []rune(a.center)
		start := max(0, (w-len(text))/2)
		for i, ch := range text {
			if start+i < w {
				row[start+i] = arcCell{cls: clsText, text: ch}
			}
		}
	}

	styles := map[int]lipgloss.Style{
		clsTick:  sMuted,
		clsReq:   sMuted,
		clsTrack: lipgloss.NewStyle().Foreground(colHairlineStrong),
		clsValue: lipgloss.NewStyle().Foreground(a.tone.color()),
		clsText:  sTitle,
	}
	if a.dotted {
		styles[clsTrack] = sSoft
	}
	lines := make([]string, h)
	for y, row := range cells {
		var b strings.Builder
		var run strings.Builder
		cls := -1
		flush := func() {
			if run.Len() == 0 {
				return
			}
			if st, ok := styles[cls]; ok {
				b.WriteString(st.Render(run.String()))
			} else {
				b.WriteString(run.String())
			}
			run.Reset()
		}
		for _, c := range row {
			if c.cls != cls {
				flush()
				cls = c.cls
			}
			switch {
			case c.cls == clsText:
				run.WriteRune(c.text)
			case c.bits == 0:
				run.WriteRune(' ')
			default:
				run.WriteRune(0x2800 + c.bits)
			}
		}
		flush()
		lines[y] = b.String()
	}
	return lines
}

// bar is the narrow-terminal gauge: ████░░░░.
func bar(width int, frac float64, t tone) string {
	frac = math.Max(0, math.Min(1, frac))
	n := int(math.Round(frac * float64(width)))
	return lipgloss.NewStyle().Foreground(t.color()).Render(strings.Repeat("█", n)) +
		lipgloss.NewStyle().Foreground(colHairlineStrong).Render(strings.Repeat("░", width-n))
}

// gaugeView is what a gauge says, worked out once for the arc and the bar.
type gaugeView struct {
	name    string
	tone    tone
	word    string // status word next to the dot
	center  string // "62%" or "—"
	used    string // "1.3 of 6 cores", or why there's no data
	footer  string // "75% requested · ↑ 4% in 1h"
	frac    float64
	req     float64
	hasData bool
}

func describeGauge(name string, g metrics.Gauge, thresholds [2]float64) gaugeView {
	v := gaugeView{name: name, req: -1}
	switch g.State {
	case metrics.Live:
		v.hasData = true
		v.frac = g.Fraction()
		v.tone = zone(v.frac, thresholds)
		v.word = zoneWord(v.tone)
		v.center = fmt.Sprintf("%.0f%%", v.frac*100)
		v.used = usedOf(g)
		var foot []string
		if g.Requested != nil && g.Max > 0 {
			v.req = *g.Requested / g.Max
			foot = append(foot, fmt.Sprintf("%.0f%% requested", v.req*100))
		}
		if g.Trend != nil && g.TrendWindow > 0 {
			arrow := "↑"
			if *g.Trend < 0 {
				arrow = "↓"
			}
			win := time.Duration(g.TrendWindow) * time.Second
			foot = append(foot, fmt.Sprintf("%s %.0f%% in %s", arrow, math.Abs(*g.Trend)*100, shortDuration(win)))
		}
		if g.Unit == "bytes/s" {
			foot = append(foot, "of 7-day peak")
		}
		v.footer = strings.Join(foot, " · ")
	case metrics.NoMetrics:
		v.word, v.center, v.used = "No metrics", "—", "Install metrics-server"
	case metrics.NoAccess:
		v.word, v.center, v.used = "No access", "—", "You can't read this"
	default:
		v.word, v.center, v.used = "Error", "—", "Couldn't load"
	}
	if !v.hasData && g.Message != "" {
		v.used = g.Message
	}
	return v
}

// usedOf is the "used of total" line in the gauge's unit.
func usedOf(g metrics.Gauge) string {
	switch g.Unit {
	case "cores":
		return fmt.Sprintf("%s of %s cores", trimFloat(g.Value), trimFloat(g.Max))
	case "bytes":
		unit, div := binaryUnit(g.Max)
		return fmt.Sprintf("%s of %s %s", trimFloat(g.Value/div), trimFloat(g.Max/div), unit)
	case "bytes/s":
		unit, div := rateUnit(g.Max)
		return fmt.Sprintf("%s of %s %s", trimFloat(g.Value/div), trimFloat(g.Max/div), unit)
	case "pods":
		return fmt.Sprintf("%.0f of %.0f pods", g.Value, g.Max)
	}
	return fmt.Sprintf("%s of %s %s", trimFloat(g.Value), trimFloat(g.Max), g.Unit)
}

func trimFloat(f float64) string {
	if f >= 100 || f == math.Trunc(f) {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%.1f", f)
}

func binaryUnit(b float64) (string, float64) {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	div := 1.0
	i := 0
	for b/div >= 1024 && i < len(units)-1 {
		div *= 1024
		i++
	}
	return units[i], div
}

func rateUnit(bps float64) (string, float64) {
	units := []string{"B/s", "kB/s", "MB/s", "GB/s"}
	div := 1.0
	i := 0
	for bps/div >= 1000 && i < len(units)-1 {
		div *= 1000
		i++
	}
	return units[i], div
}

// shortDuration is "1h", "25m" or "40s".
func shortDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
}

// gaugeCard is a gauge in its card: name and status, the arc, used of total,
// and requested share and trend.
func gaugeCard(v gaugeView, thresholds [2]float64, width int) string {
	inner := width - 4 // border and padding
	head := spread(sTitle.Render(v.name), status(v.tone, v.word), inner)
	arc := drawArc(arcSpec{
		width: inner - 2, frac: v.frac, req: v.req, thresholds: thresholds,
		tone: v.tone, dotted: !v.hasData, center: v.center,
	})
	lines := []string{head}
	for _, l := range arc {
		lines = append(lines, lipgloss.PlaceHorizontal(inner, lipgloss.Center, l))
	}
	if v.hasData {
		lines = append(lines,
			lipgloss.PlaceHorizontal(inner, lipgloss.Center, sMuted.Render(clip(v.used, inner))),
			sMuted.Render(clip(v.footer, inner)))
	} else {
		// No data: the reason gets both lines.
		why := strings.Split(lipgloss.Wrap(v.used, inner, " "), "\n")
		for i := range 2 {
			l := ""
			if i < len(why) {
				l = why[i]
				if i == 1 && len(why) > 2 {
					l = clip(l+" "+why[2], inner)
				}
			}
			lines = append(lines, lipgloss.PlaceHorizontal(inner, lipgloss.Center, sMuted.Render(l)))
		}
	}
	return sCard.Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}

// gaugeLine is the one-line gauge for narrow terminals.
func gaugeLine(v gaugeView, width int) string {
	name := sTitle.Render(fmt.Sprintf("%-8s", v.name))
	pct := sInk.Render(fmt.Sprintf("%4s", v.center))
	st := status(v.tone, v.word)
	barW := max(4, width-lipgloss.Width(name)-lipgloss.Width(pct)-lipgloss.Width(st)-3)
	if !v.hasData {
		return name + sSoft.Render(strings.Repeat("·", barW)) + " " + pct + " " + st
	}
	return name + bar(barW, v.frac, v.tone) + " " + pct + " " + st
}
