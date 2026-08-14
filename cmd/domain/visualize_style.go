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
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The palette is a set of package-level styles rather than a value threaded
// through every renderer, because it is genuinely global: one terminal, one
// background, one set of colors for as long as the program runs. It is
// installed once — see useTheme — from the event loop, which is also the only
// goroutine that renders, so the swap cannot race a paint.

var (
	// Chrome.
	styTitle   lipgloss.Style // Hollow Purple
	styHeading lipgloss.Style // Limitless blue
	styDim     lipgloss.Style
	styRule    lipgloss.Style
	styKey     lipgloss.Style // the keys in the footer

	// Tree rows.
	styCursor lipgloss.Style
	stySelect lipgloss.Style // the editor's selection
	styFrame  lipgloss.Style // structure, not data
	styLabel  lipgloss.Style
	styType   lipgloss.Style // Six Eyes cyan
	styErr    lipgloss.Style
	styMatch  lipgloss.Style
	styMarker lipgloss.Style

	// Values.
	styValue lipgloss.Style

	// Source, for the REPL's syntax highlighting.
	styKeyword lipgloss.Style
	styArgName lipgloss.Style
	styNumber  lipgloss.Style
	styString  lipgloss.Style
	styPunct   lipgloss.Style
	styComment lipgloss.Style
	styFix     lipgloss.Style

	// heatRamp maps a share of the run to a color, coolest first. A profile is
	// read by scanning for the hot end, so the ramp has to be legible at a
	// glance and monotonic — not merely distinct.
	heatRamp []heatBand

	// Mahoraga's wheel. The spoke styles are four steps of one ramp rather than
	// four unrelated colors: the sweep rotating around the wheel is read as a
	// comet trail, and a trail whose brightness is not monotonic reads as eight
	// separate things blinking.
	styWheelBase   lipgloss.Style // a spoke at rest
	styWheelMid    lipgloss.Style
	styWheelBright lipgloss.Style
	styWheelPeak   lipgloss.Style // the cell the sweep is on
	styWheelHub    lipgloss.Style
	styWheelPend   lipgloss.Style // a handle the search has not reached
	styWheelSpent  lipgloss.Style // a handle that ran and kept nothing
	styWheelAbsent lipgloss.Style // a turn the catalogue has not reached
	styWheelFlash  lipgloss.Style // the whole wheel, for the frames after an adaptation

	// wheelEffectRamp colors a lit handle by how much it won, crimson through
	// gold. It is deliberately the *opposite* direction to heatRamp: there, a
	// big number is a problem; here it is the point.
	wheelEffectRamp []lipgloss.Style
)

type heatBand struct {
	upTo  float64
	color string
}

// lightTheme reports which palette is installed, so a caller can tell whether
// a background report actually changes anything.
var lightTheme bool

func init() { useTheme(false) }

// useTheme installs the dark or the light palette. The two differ in more than
// brightness: on a light background the eye needs *darker* ink, so the same
// roles move to the low end of the 256-color cube rather than being dimmed.
//
// Call it only from the goroutine that renders. The REPL asks the terminal for
// its background color at startup (tea.RequestBackgroundColor) and installs
// the answer; anything the terminal does not report leaves the dark default,
// which is what a terminal that answers nothing has almost always got.
func useTheme(light bool) {
	lightTheme = light
	fg := func(dark, lightC string) lipgloss.Style {
		c := dark
		if light {
			c = lightC
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	bold := func(dark, lightC string) lipgloss.Style { return fg(dark, lightC).Bold(true) }

	styTitle = bold("135", "91")
	styHeading = bold("69", "26")
	styDim = fg("244", "241")
	styRule = fg("238", "247")
	styKey = fg("220", "130")

	styCursor = lipgloss.NewStyle().Bold(true)
	styMatch = lipgloss.NewStyle().Bold(true)
	// A selection is a background alone: it has to sit under syntax highlighting
	// without replacing it, so unlike the cursor it never sets a foreground.
	if light {
		stySelect = lipgloss.NewStyle().Background(lipgloss.Color("253"))
	} else {
		stySelect = lipgloss.NewStyle().Background(lipgloss.Color("238"))
	}
	if light {
		styCursor = styCursor.Foreground(lipgloss.Color("232")).Background(lipgloss.Color("189"))
		styMatch = styMatch.Foreground(lipgloss.Color("232")).Background(lipgloss.Color("222"))
	} else {
		styCursor = styCursor.Foreground(lipgloss.Color("231")).Background(lipgloss.Color("60"))
		styMatch = styMatch.Foreground(lipgloss.Color("232")).Background(lipgloss.Color("220"))
	}

	styFrame = fg("141", "92")
	styLabel = fg("252", "238")
	styType = fg("51", "30")
	styErr = bold("197", "160")
	styMarker = fg("135", "91")
	styValue = fg("84", "28")

	styKeyword = bold("135", "91")
	styArgName = fg("69", "26")
	styNumber = fg("51", "30")
	styString = fg("84", "28")
	styPunct = fg("244", "242")
	styComment = fg("240", "245")
	styFix = fg("84", "28")

	styWheelBase = fg("240", "250")
	styWheelMid = fg("61", "104")
	styWheelBright = fg("99", "62")
	styWheelPeak = bold("183", "57")
	styWheelHub = bold("135", "91")
	styWheelPend = fg("242", "248")
	styWheelSpent = fg("245", "243")
	// An unbuilt turn has to be clearly darker than a turn that simply has not
	// been reached: the wheel is claiming there are eight of them, and half of
	// them not existing yet is the honest part of that claim.
	styWheelAbsent = fg("235", "253")
	styWheelFlash = bold("231", "17")
	wheelEffectRamp = []lipgloss.Style{
		bold("197", "160"), bold("203", "166"), bold("209", "172"),
		bold("214", "130"), bold("220", "94"),
	}

	if light {
		heatRamp = []heatBand{
			{1, "245"}, {5, "28"}, {15, "30"}, {35, "130"}, {60, "166"}, {101, "160"},
		}
		return
	}
	heatRamp = []heatBand{
		{1, "244"},  // noise
		{5, "84"},   // green
		{15, "51"},  // cyan
		{35, "220"}, // yellow
		{60, "208"}, // orange
		{101, "197"},
	}
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

// truncateVisLeft shortens a line to w columns by cutting from the *left*,
// marking the cut with a leading ellipsis. Use it for a line whose meaning
// accumulates towards its end — a path, or a command line, where the last
// components name the thing and the leading ones only say where it lives.
// truncateVis on one of those keeps the least identifying part and drops the
// name, which is the wrong half to lose.
func truncateVisLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := ansi.StringWidth(s)
	if n <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return ansi.TruncateLeft(s, n-(w-1), "…")
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

// isLightColor reports whether a terminal background is light enough to need
// dark ink, by relative luminance (ITU-R BT.709). A terminal that reports no
// background at all is treated as dark, which is what an unanswering terminal
// has almost always got.
func isLightColor(c color.Color) bool {
	if c == nil {
		return false
	}
	r, g, b, _ := c.RGBA()
	lum := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 65535
	return lum > 0.5
}
