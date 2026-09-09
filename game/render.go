// Painting a View on a terminal.
//
// The geometry is already decided: ir.LayoutView reduced the render tree to a
// rectangle of styled runs, and this only chooses how each run is drawn. That
// split is the whole reason a replayed frame and a played one are the same
// picture — if styling could change where a column lands, a golden frame would
// be evidence about the renderer rather than about the program.
//
// So there is no layout here, and there must never be any.
package game

import (
	"strings"

	"charm.land/lipgloss/v2"

	"domain/ir"
)

// viewColours maps the names a program may write (ir.ParseViewStyle keeps the
// list closed) to the terminal's own palette. They are ANSI indices rather
// than hex values on purpose: a program says "red" and the player sees the red
// they chose for their terminal, not one this program guessed at.
var viewColours = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3",
	"blue": "4", "magenta": "5", "cyan": "6", "white": "7",
	"bright-black": "8", "bright-red": "9", "bright-green": "10", "bright-yellow": "11",
	"bright-blue": "12", "bright-magenta": "13", "bright-cyan": "14", "bright-white": "15",
}

// renderViewStyled draws a laid-out View with colour and emphasis.
func renderViewStyled(v *ir.ViewValue) string {
	b := ir.LayoutView(v)
	lines := make([]string, len(b.Lines))
	for i, line := range b.Lines {
		var sb strings.Builder
		for _, run := range line {
			sb.WriteString(styleRun(run))
		}
		// Trailing spaces are trimmed here for the same reason the plain
		// renderer trims them: they are invisible, and on a terminal they also
		// paint a background colour across the rest of the row.
		lines[i] = strings.TrimRight(sb.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// styleRun renders one run. An unstyled run — which is most of a frame — is
// written through untouched, so an ordinary picture costs no escape codes at
// all.
func styleRun(run ir.ViewRun) string {
	st := run.Style
	if st == (ir.ViewStyle{}) {
		return run.Text
	}
	s := lipgloss.NewStyle()
	if c, ok := viewColours[st.FG]; ok {
		s = s.Foreground(lipgloss.Color(c))
	}
	if c, ok := viewColours[st.BG]; ok {
		s = s.Background(lipgloss.Color(c))
	}
	if st.Bold {
		s = s.Bold(true)
	}
	if st.Dim {
		s = s.Faint(true)
	}
	if st.Underline {
		s = s.Underline(true)
	}
	if st.Reverse {
		s = s.Reverse(true)
	}
	return s.Render(run.Text)
}
