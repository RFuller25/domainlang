// The stepper's palette.
//
// The colors are the CLI's own — the same 256-color numbers banner.go paints
// the `domain expansion:` banners with — so the visualizer looks like the rest
// of the tool rather than a different program that happens to ship in the same
// binary. banner.go writes raw escapes because it emits into an ordinary
// stdout; here lipgloss does it, because bubbletea downsamples a lipgloss style
// to whatever the terminal actually supports (and drops it entirely under
// NO_COLOR) where a hardcoded escape would just be wrong.
//
// One rule holds the layout together: **style last**. Padding and truncation
// are computed on plain strings, and color is applied to finished cells, so an
// escape sequence can never be mistaken for a column of text. The width helpers
// (pad, truncateVis) measure printable width for the same reason.
package main

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	// Chrome.
	styTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("135")) // Hollow Purple
	styHeading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))  // Limitless blue
	styDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styRule    = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // the keys in the footer

	// Tree rows.
	styCursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("60"))
	styFrame  = lipgloss.NewStyle().Foreground(lipgloss.Color("141")) // structure, not data
	styLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styType   = lipgloss.NewStyle().Foreground(lipgloss.Color("51")) // Six Eyes cyan
	styErr    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("197"))
	styMatch  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("220"))
	styMarker = lipgloss.NewStyle().Foreground(lipgloss.Color("135"))

	// Values.
	styValue = lipgloss.NewStyle().Foreground(lipgloss.Color("84")) // Reverse Cursed Technique green
)

// heatRamp maps a share of the run to a color, coolest first. A profile is read
// by scanning for the hot end, so the ramp has to be legible at a glance and
// monotonic — not merely distinct.
var heatRamp = []struct {
	upTo  float64
	color string
}{
	{1, "244"},  // noise
	{5, "84"},   // green
	{15, "51"},  // cyan
	{35, "220"}, // yellow
	{60, "208"}, // orange
	{101, "197"},
}

// heat styles a percentage by how large it is.
func heat(pct float64, known bool) lipgloss.Style {
	if !known {
		return styDim
	}
	for _, band := range heatRamp {
		if pct < band.upTo {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(band.color))
		}
	}
	return styErr
}

// --- width, measured the way a terminal measures it ---
//
// These count *printable* width, not bytes and not runes: an already-styled
// cell carries escape sequences that occupy no columns, and a box-drawing
// character occupies one however many bytes it takes. Getting this wrong shows
// up as a pane divider that wanders, which is why it is one helper rather than
// a rule everyone has to remember.

// pad right-pads a line to w columns, clipping anything longer.
func pad(s string, w int) string {
	n := ansi.StringWidth(s)
	if n >= w {
		return ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", w-n)
}

// truncateVis shortens a line to w columns, marking the cut with an ellipsis.
func truncateVis(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return ansi.Truncate(s, w, "…")
}

// wrapVis breaks a message into lines of at most w columns, on word boundaries.
func wrapVis(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case ansi.StringWidth(line)+1+ansi.StringWidth(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
