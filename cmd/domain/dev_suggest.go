// Offering the opening that reads the input you just chose.
//
// The analysis is in package shape, which is pure and tested against the
// repository's own programs. This is only its presentation: a list, the
// evidence beside each row, and Enter to put the statements in the program.
//
// It offers rather than decides, because some inputs are genuinely ambiguous —
// a rectangle of digits is a grid or a column of numbers and the file cannot
// say which — and because inserting code into someone's program uninvited is a
// bad way to be right. Esc leaves the program alone.
package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"domain/shape"
)

// devSuggest is the overlay listing candidate openings.
type devSuggest struct {
	candidates []shape.Candidate
	cursor     int
	input      string // the file the suggestions are for
}

// suggestFor builds the overlay, or reports that there is nothing worth
// offering — an empty file, or one whose shape says nothing.
func suggestFor(inputPath, contents string) (*devSuggest, bool) {
	cands := shape.Suggest(contents)
	if len(cands) == 0 {
		return nil, false
	}
	return &devSuggest{candidates: cands, input: inputPath}, true
}

// suggestKey handles one keystroke, reporting whether the overlay stays open
// and whether the highlighted suggestion was accepted.
func (s *devSuggest) key(msg tea.KeyPressMsg) (open, accepted bool) {
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		return false, false
	case "enter":
		return false, true
	case "down", "ctrl+n", "j":
		s.cursor = (s.cursor + 1) % len(s.candidates)
	case "up", "ctrl+p", "k":
		s.cursor = (s.cursor + len(s.candidates) - 1) % len(s.candidates)
	}
	return true, false
}

// insertSuggestion puts the chosen statements into the program, after the
// source stage where they belong.
//
// After the source stage rather than at the top, because the opening reads the
// value the source produced: a program whose first statement split text that
// had not been read yet would not resolve, and the editor would have written a
// broken program on the user's behalf.
func (m devModel) insertSuggestion(c shape.Candidate) devModel {
	m.undo.record(m.buf, false, m.now())

	at := 0
	for i, line := range m.buf.lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimLeft(line, " \t")), "cursed energy:") {
			at = i + 1
			break
		}
	}

	rest := append([]string{}, m.buf.lines[at:]...)
	m.buf.lines = append(m.buf.lines[:at], append(append([]string{}, c.Statements...), rest...)...)
	m.buf.row = at
	m.buf.col, m.buf.goalCol = 0, 0
	m.buf.anchor = nil
	m.dirty = true
	m.status = "inserted: " + c.First()
	return m
}

// suggestView draws the overlay: each opening, with the evidence for it.
func (m devModel) suggestView() string {
	s := m.suggest
	var b strings.Builder
	b.WriteString(styTitle.Render("opening for "+s.input) + "\n")
	b.WriteString(styDim.Render("what would read this input — the order is a guess, the choice is not") + "\n\n")

	for i, c := range s.candidates {
		marker := "  "
		if i == s.cursor {
			marker = styCursor.Render("› ")
		}
		head := c.First()
		if i == s.cursor {
			head = styKeyword.Render(head)
		}
		b.WriteString(truncateVis(marker+head, m.width) + "\n")
		b.WriteString(truncateVis("    "+styDim.Render(c.Why), m.width) + "\n")

		// The rest of the statements, so a multi-line opening is not a surprise
		// once it lands in the program.
		if i == s.cursor {
			for _, st := range c.Statements[1:] {
				b.WriteString(truncateVis("    "+styFrame.Render(st), m.width) + "\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(styDim.Render("↑/↓ choose · enter insert · esc leave the program alone"))
	return b.String()
}
