package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Glowpod's dark palette (CLAUDE.md design tokens). Lip Gloss and Bubble Tea
// bring them down to 256 or 16 colours, or none with NO_COLOR.
var (
	colCanvas         = lipgloss.Color("#06110D") // Bio Black
	colHairline       = lipgloss.Color("#2A2A2A")
	colHairlineStrong = lipgloss.Color("#3A3A3A")
	colInk            = lipgloss.Color("#FFFFFF")
	colBody           = lipgloss.Color("#CCCCCC")
	colMuted          = lipgloss.Color("#888888")
	colMutedSoft      = lipgloss.Color("#5A5A5A")
	colLime           = lipgloss.Color("#C8FF00") // Synthetic Lime: ok, live, selection marker
	colRose           = lipgloss.Color("#EF4444") // critical only
	colWarn           = lipgloss.Color("#FB923C") // warning log lines only
)

// tone is a state's colour: ok (lime), high (white), critical (rose), or
// none (muted, for stale and unknown).
type tone int

const (
	toneNone tone = iota
	toneOK
	toneHigh
	toneCritical
)

func (t tone) color() color.Color {
	switch t {
	case toneOK:
		return colLime
	case toneHigh:
		return colInk
	case toneCritical:
		return colRose
	}
	return colMuted
}

var (
	sInk     = lipgloss.NewStyle().Foreground(colInk)
	sTitle   = lipgloss.NewStyle().Foreground(colInk).Bold(true)
	sBody    = lipgloss.NewStyle().Foreground(colBody)
	sMuted   = lipgloss.NewStyle().Foreground(colMuted)
	sSoft    = lipgloss.NewStyle().Foreground(colMutedSoft)
	sLime    = lipgloss.NewStyle().Foreground(colLime)
	sRose    = lipgloss.NewStyle().Foreground(colRose)
	sWarn    = lipgloss.NewStyle().Foreground(colWarn)
	sLabel   = lipgloss.NewStyle().Foreground(colMuted).Bold(true)
	sKey     = lipgloss.NewStyle().Foreground(colInk).Bold(true)
	sMatch   = lipgloss.NewStyle().Background(colLime).Foreground(colCanvas)
	sHair    = lipgloss.NewStyle().Foreground(colHairline)
	sCard    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colHairlineStrong)
	sTabOn   = lipgloss.NewStyle().Foreground(colInk).Bold(true).Underline(true)
	sTabOff  = lipgloss.NewStyle().Foreground(colMuted)
	sDialog  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colHairlineStrong).Padding(1, 2)
	sCritBox = sDialog.BorderForeground(colRose)
)

// status renders a state the Glowpod way: a coloured dot and the word, so it
// never relies on colour alone.
func status(t tone, word string) string {
	return lipgloss.NewStyle().Foreground(t.color()).Render("●") + " " + sInk.Render(word)
}

// label renders a section label: uppercase, muted, bold.
func label(s string) string {
	return sLabel.Render(strings.ToUpper(s))
}

// hint renders one footer key hint: the key in ink, what it does in muted.
func hint(key, what string) string {
	return sKey.Render(key) + " " + sMuted.Render(what)
}

// zone is a gauge fraction's tone with the High and Critical thresholds.
func zone(frac float64, thresholds [2]float64) tone {
	switch {
	case frac >= thresholds[1]:
		return toneCritical
	case frac >= thresholds[0]:
		return toneHigh
	}
	return toneOK
}

// zoneWord is the word next to a gauge's dot.
func zoneWord(t tone) string {
	switch t {
	case toneCritical:
		return "Critical"
	case toneHigh:
		return "High"
	case toneOK:
		return "OK"
	}
	return ""
}
