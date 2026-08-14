// Painting the editor.
//
// Only what is on screen is painted. That is not housekeeping: painting costs
// eleven times what lexing does and is paid per visible line, so a 40-line
// window repaints in 0.6ms while 200 unclipped lines take 9ms — the difference
// between a keystroke that lands instantly and one you can feel. The window is
// the unit of work, and the buffer's length does not enter into it.
package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// view paints a whole frame: the program, then one status line.
func (m devModel) view() string {
	if m.stepper != nil {
		// The stepper owns the screen while it is open, the way it does as a
		// command. It is a whole model, not a pane.
		return m.stepper.View().Content
	}
	if m.monitor != nil {
		// A run takes the whole screen, for as long as it is running and until
		// the report on it is dismissed.
		return m.monitorView()
	}
	if m.showHelp {
		return m.helpView()
	}
	if m.picker != nil {
		return m.picker.view(m.width, m.height)
	}
	if m.browser != nil {
		return m.browser.view(m.width, m.height)
	}
	if m.suggest != nil {
		return m.suggestView()
	}

	h := m.textHeight()
	width := len(strconv.Itoa(max(len(m.buf.lines), 1)))
	textW := m.textWidth()

	// The output pane takes the bottom of the screen rather than floating,
	// because it is read as a block and its length is not related to where the
	// cursor happens to be.
	var outRows []string
	if m.output != nil {
		outRows = m.outputView()
		h = max(1, h-len(outRows))
	}
	// The value bar sits between the program and the status line, and only when
	// the cursor's line produced something — a program that has not been run
	// costs no space at all.
	valueRow := m.valueBar()
	if valueRow != "" {
		h = max(1, h-1)
	}

	// The completion popup and the inspect panel are drawn over the program
	// rather than beside it: half a terminal's width is not enough for both,
	// and the lines they cover are the ones you are not reading right now.
	overlay, overlayTop := m.floatingRows()

	var b strings.Builder
	drawn := 0
	for i := m.top; i < len(m.buf.lines) && drawn < h; i++ {
		if m.hidden(i + 1) {
			continue // inside a folded block: its header stands for it
		}
		screenRow := drawn
		drawn++
		if k := screenRow - overlayTop; overlay != nil && k >= 0 && k < len(overlay) {
			b.WriteString(overlay[k])
			b.WriteByte('\n')
			continue
		}
		// The gutter is fixed and the program scrolls under it, so the two are
		// cut separately: ansi.Cut works in display columns and understands the
		// escape codes, which is what lets a painted line be sliced at all.
		text := paintLine(m.buf.lines[i], m.decorFor(i)) + m.foldMarker(i+1) + m.hintSuffix(i)
		text = ansi.Cut(text, m.leftCol, m.leftCol+textW)
		// A row never overflows the window. There is no soft wrap here by
		// design, so letting the terminal wrap a long line would push every row
		// below it down and unpin the status line from the bottom of the screen.
		b.WriteString(truncateVis(m.gutterFor(i, width)+text, m.width))
		b.WriteByte('\n')
	}
	// Past the end of the buffer: blank rows, so the status line stays pinned
	// to the bottom of the terminal rather than floating under a short program.
	for i := drawn; i < h; i++ {
		if k := i - overlayTop; overlay != nil && k >= 0 && k < len(overlay) {
			b.WriteString(overlay[k])
			b.WriteByte('\n')
			continue
		}
		b.WriteString(styDim.Render(strings.Repeat(" ", width) + " │ "))
		b.WriteByte('\n')
	}
	for _, r := range outRows {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	if valueRow != "" {
		b.WriteString(valueRow)
		b.WriteByte('\n')
	}
	b.WriteString(m.bottomLine())
	return b.String()
}

// decorFor collects what should be drawn over one line: the cursor if it is
// there, the part of the selection that falls on it, and any search hits.
func (m devModel) decorFor(row int) lineDecor {
	d := noDecor()
	if row == m.buf.row {
		d.cursor = m.buf.col
	}

	if start, end, ok := m.buf.selection(); ok && row >= start.row && row <= end.row {
		d.selStart, d.selEnd = 0, len(m.buf.lines[row])
		if row == start.row {
			d.selStart = start.col
		}
		if row == end.row {
			d.selEnd = end.col
		} else {
			// Past the end of the line, so the newline shows as selected and a
			// multi-line selection reads as one region.
			d.selEnd = len(m.buf.lines[row]) + 1
		}
	}

	if m.search != nil {
		d.matches = m.search.matchesOn(row)
		if cur, ok := m.search.current(); ok && cur.row == row {
			for i, hit := range d.matches {
				if hit == cur {
					d.currentHit = i
				}
			}
		}
	}
	return d
}

// bottomLine is the last row of the screen: whichever prompt is open, or the
// status line. A prompt takes it because a prompt is where the keyboard is
// going, and two rows competing for the bottom of a terminal is how a layout
// starts wandering.
func (m devModel) bottomLine() string {
	switch {
	case m.search != nil:
		return truncateVis(m.search.prompt(), m.width)
	case m.gotoLine != nil:
		return truncateVis(styHeading.Render(" line ")+" "+*m.gotoLine+styCursor.Render(" ")+
			styDim.Render(fmt.Sprintf("  of %d", len(m.buf.lines))), m.width)
	}
	return m.statusLine()
}

// gutterFor is a line's gutter: its number, and a mark when the analysis has
// something to say about it. The mark stands where the separator does rather
// than beside it, so the gutter is the same width either way and a program's
// text never moves because a line acquired an error.
func (m devModel) gutterFor(row, width int) string {
	n := strconv.Itoa(row + 1)
	pad := strings.Repeat(" ", width-len(n))

	// After a run the number carries the line's share of it, on the stepper's
	// own ramp so a hot line looks the same in both. Diagnostics still own the
	// separator, so the two can be read at once.
	numStyle := styDim
	if hot, ok := m.heatFor(row); ok {
		numStyle = hot
	}

	sep := " │ "
	if mark, ok := m.gutterMark(row); ok {
		sep = " " + mark + " "
	}
	return styDim.Render(pad) + numStyle.Render(n) + styDim.Render(sep)
}

// hintSuffix is the type flowing out of a line, painted past its end. It is
// only drawn where it fits: a hint that wrapped would push the program's own
// text onto a second row, which is worse than no hint.
func (m devModel) hintSuffix(row int) string {
	label := m.hintFor(row)
	if label == "" {
		return ""
	}
	// Measured against the scrolled window: a hint is worth drawing when it
	// fits beside the part of the line you can actually see.
	used := ansi.StringWidth(m.buf.lines[row]) - m.leftCol
	if used+ansi.StringWidth(label)+1 > m.textWidth() {
		return ""
	}
	return styType.Render(" " + label)
}

// floatingRows is the popup drawn over the program, and the screen row it
// starts on. Below the cursor when there is room, above it when there is not.
func (m devModel) floatingRows() ([]string, int) {
	var rows []string
	switch {
	case m.complete != nil:
		rows = m.completeView(len(strconv.Itoa(max(len(m.buf.lines), 1))) + 3)
	case m.inspect != nil:
		for _, l := range m.inspect.lines {
			rows = append(rows, truncateVis("  "+l, m.width))
		}
	default:
		return nil, 0
	}
	cursorRow := m.buf.row - m.top
	top := cursorRow + 1
	if top+len(rows) > m.textHeight() {
		top = max(0, cursorRow-len(rows))
	}
	return rows, top
}

// gutter is one line's number, right-aligned to the width the longest one
// needs. The width comes from the line count rather than from the number being
// drawn, so a program does not shift its own text sideways as it grows past
// ten lines.
func gutter(n, width int) string {
	s := strconv.Itoa(n)
	return styDim.Render(strings.Repeat(" ", width-len(s)) + s + " │ ")
}

// statusLine says what is being edited, whether it is saved, and where the
// cursor is — with anything the editor has to report taking the middle, since
// a message is worth more than a position at the moment it appears.
func (m devModel) statusLine() string {
	name := "(unnamed)"
	if m.path != "" {
		name = filepath.Base(m.path)
	}
	if m.dirty {
		name += "*"
	}

	left := styHeading.Render(" " + name + " ")
	// Columns are reported in runes rather than bytes: a byte offset is the
	// right thing for the lexer and the wrong thing for a person counting
	// characters across a line with a `—` in it.
	counts := ""
	if n := m.intel.errs + m.intel.warns + m.intel.hints_; n > 0 {
		counts = fmt.Sprintf("  %d✗ %d! %d·", m.intel.errs, m.intel.warns, m.intel.hints_)
	}
	right := styDim.Render(fmt.Sprintf(" %d:%d%s  ctrl+g keys ",
		m.buf.row+1, len([]rune(m.buf.line()[:m.buf.col]))+1, counts))

	// What the editor has to say beats what the analysis found: a status
	// message is a response to something just done and is only true now.
	mid := ""
	switch {
	case m.running:
		mid = m.spin.View() + " " + styKey.Render(m.status)
	case m.status != "":
		mid = styKey.Render(m.status)
	default:
		mid = m.diagnosticLine()
	}

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right) - ansi.StringWidth(mid)
	if gap < 1 {
		// A narrow terminal drops the position before the message: the message
		// is the part that is only true right now.
		return truncateVis(left+" "+mid, m.width)
	}
	return left + strings.Repeat(" ", gap/2) + mid + strings.Repeat(" ", gap-gap/2) + right
}

// helpView draws the key list on the whole screen, the way the stepper does.
func (m devModel) helpView() string {
	body := devHelpBody()
	h := max(1, m.height-2)

	var b strings.Builder
	b.WriteString(styTitle.Render("domain expansion: development") + styDim.Render("  keys") + "\n")
	for i := m.helpTop; i < min(m.helpTop+h, len(body)); i++ {
		b.WriteString(truncateVis(body[i], m.width) + "\n")
	}
	if m.helpTop+h < len(body) {
		b.WriteString(styDim.Render("  ↓ more"))
	}
	return b.String()
}
