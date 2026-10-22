package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// clip cuts plain or styled s to width cells, ending in "…" when cut.
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

// pad cuts or fills s to exactly width cells.
func pad(s string, width int) string {
	s = clip(s, width)
	if w := lipgloss.Width(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// padLeft is pad with the text on the right, for numbers.
func padLeft(s string, width int) string {
	s = clip(s, width)
	if w := lipgloss.Width(s); w < width {
		s = strings.Repeat(" ", width-w) + s
	}
	return s
}

// spread puts left and right at the two ends of width cells.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return clip(left+" "+right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// fill pads each of lines to width and adds blank lines up to height, so a
// pane always covers its area.
func fill(lines []string, width, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, 0, height)
	for _, l := range lines {
		out = append(out, pad(l, width))
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return strings.Join(out, "\n")
}

// age is how long ago t was, kubectl style: "40s", "5m", "3h", "2d".
func age(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	return shortDuration(now.Sub(t))
}
