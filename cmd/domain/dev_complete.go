// Tab completion in the editor.
//
// The candidates come from `completeToken` (repl_complete.go), which is
// already the union of the language server's context-aware keyword, primitive,
// argument-label and Mode completion and the REPL's own file-path source. The
// editor adds no vocabulary of its own — a program is the same language here
// as it is at the prompt, and two lists that could drift apart would be one
// list too many.
//
// What the editor adds is a *choice*. The REPL cycles: press Tab again for the
// next candidate. That works at a prompt where the alternatives are a
// keystroke apart and the line is short. In a file you are looking at the
// program rather than the prompt, and cycling blind through fourteen
// primitives is how you end up with the wrong one — so the candidates are
// shown and picked from.
package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// devComplete is the open completion popup.
type devComplete struct {
	candidates []string
	cursor     int
	// start is the byte offset in the line where the replaced token begins.
	start int
	// row is the line it was opened on, so an edit elsewhere closes it rather
	// than inserting into the wrong place.
	row int
}

// devCompleteRows bounds how much of the screen a popup may take.
const devCompleteRows = 8

// openComplete gathers candidates for the cursor's position. It reports false
// when there is nothing to offer, so the key can fall through to inserting a
// tab — a completion that silently does nothing is worse than no completion.
func (m devModel) openComplete() (*devComplete, bool) {
	line := m.buf.line()
	// Nothing but whitespace before the cursor means the key is being used to
	// indent, not to complete. Without this, Tab at the start of a line offers
	// every keyword in the language instead of doing the obvious thing.
	if strings.TrimSpace(line[:m.buf.col]) == "" {
		return nil, false
	}
	cands, start := completeToken(line[:m.buf.col], m.buf.col, m.dir())
	if len(cands) == 0 {
		return nil, false
	}
	// A single candidate that is already what is typed is not a choice.
	if len(cands) == 1 && cands[0] == line[start:m.buf.col] {
		return nil, false
	}
	return &devComplete{candidates: cands, start: start, row: m.buf.row}, true
}

// accept replaces the token under the cursor with the highlighted candidate.
func (m devModel) acceptComplete() devModel {
	c := m.complete
	if c == nil || c.cursor >= len(c.candidates) {
		return m
	}
	pick := c.candidates[c.cursor]
	line := m.buf.line()
	m.undo.record(m.buf, false, m.now())
	m.buf.lines[m.buf.row] = line[:c.start] + pick + line[m.buf.col:]
	m.buf.col = c.start + len(pick)
	m.buf.goalCol = m.buf.col
	m.dirty = true
	m.complete = nil
	return m
}

// completeKey handles one keystroke while the popup is open, reporting whether
// it stays open.
func (m *devModel) completeKey(msg tea.KeyPressMsg) (stillOpen, accepted bool) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return false, false
	case "enter", "tab":
		return false, true
	case "down", "ctrl+n":
		m.complete.cursor = (m.complete.cursor + 1) % len(m.complete.candidates)
		return true, false
	case "up", "ctrl+p":
		n := len(m.complete.candidates)
		m.complete.cursor = (m.complete.cursor + n - 1) % n
		return true, false
	}
	// Anything else is editing again. The popup closes rather than trying to
	// re-filter: the next Tab reopens it against the text as it now is, which
	// is both simpler and always right.
	return false, false
}

// completeView draws the popup under the cursor's line, clipped to the window.
func (m devModel) completeView(gutterWidth int) []string {
	c := m.complete
	if c == nil {
		return nil
	}
	// Keep the highlighted row on screen in a list taller than the popup.
	start := max(0, min(c.cursor-devCompleteRows/2, len(c.candidates)-devCompleteRows))
	end := min(start+devCompleteRows, len(c.candidates))

	widest := 0
	for _, s := range c.candidates[start:end] {
		widest = max(widest, len(s))
	}
	indent := strings.Repeat(" ", min(gutterWidth+c.start, max(0, m.width-widest-4)))

	var out []string
	for i := start; i < end; i++ {
		label := pad(" "+c.candidates[i]+" ", widest+2)
		if i == c.cursor {
			label = styCursor.Render(label)
		} else {
			label = styFrame.Render(label)
		}
		out = append(out, indent+label)
	}
	if len(c.candidates) > devCompleteRows {
		out = append(out, indent+styDim.Render(pad(fmt.Sprintf(" %d more ", len(c.candidates)-devCompleteRows), widest+2)))
	}
	return out
}
