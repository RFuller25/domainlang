// Ctrl+F — finding a line in the program you are editing.
//
// Incremental, so the match is the feedback for what has been typed rather
// than something that happens when you stop; wrapping, because a program is a
// loop of a few dozen lines and running off the end of it is not a result;
// case-insensitive, because that is what a search for `sum` means when the
// language capitalizes its operations and you are looking rather than
// checking.
//
// Matches are found once per query and held as positions. The alternative —
// re-scanning while painting — would be cheap too, but this way the match
// count in the prompt and the highlighting on screen cannot disagree.
package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// devMatch is one occurrence: a line, and the byte range of the hit in it.
type devMatch struct {
	row        int
	start, end int
}

// devSearch is the find overlay: what has been typed, what it matched, and
// which match is current.
type devSearch struct {
	query   string
	matches []devMatch
	idx     int
	// origin is where the cursor was when the search opened, so that cancelling
	// puts it back rather than leaving it wherever the search wandered.
	origin pos
}

// find recomputes the matches for the current query.
func (s *devSearch) find(b *devBuffer) {
	s.matches = nil
	if s.query == "" {
		return
	}
	needle := strings.ToLower(s.query)
	for row, line := range b.lines {
		hay := strings.ToLower(line)
		for i := 0; ; {
			j := strings.Index(hay[i:], needle)
			if j < 0 {
				break
			}
			at := i + j
			s.matches = append(s.matches, devMatch{row: row, start: at, end: at + len(needle)})
			// Advance by one byte rather than by the needle so that overlapping
			// occurrences ("aa" in "aaa") are both found.
			i = at + 1
		}
	}
	s.idx = min(s.idx, max(0, len(s.matches)-1))
}

// nearestFrom points idx at the first match at or after a position, wrapping —
// so opening the search jumps forward from the cursor rather than back to the
// top of the file.
func (s *devSearch) nearestFrom(p pos) {
	for i, m := range s.matches {
		if m.row > p.row || (m.row == p.row && m.start >= p.col) {
			s.idx = i
			return
		}
	}
	s.idx = 0
}

func (s *devSearch) step(delta int) {
	if len(s.matches) == 0 {
		return
	}
	s.idx = (s.idx + delta + len(s.matches)) % len(s.matches)
}

// current is the match the cursor should be sitting on.
func (s *devSearch) current() (devMatch, bool) {
	if len(s.matches) == 0 {
		return devMatch{}, false
	}
	return s.matches[s.idx], true
}

// matchesOn returns the hits on one line, for painting.
func (s *devSearch) matchesOn(row int) []devMatch {
	var out []devMatch
	for _, m := range s.matches {
		if m.row == row {
			out = append(out, m)
		}
	}
	return out
}

// prompt is the line the search draws at the bottom of the screen.
func (s *devSearch) prompt() string {
	var where string
	switch {
	case s.query == "":
		where = ""
	case len(s.matches) == 0:
		where = styErr.Render("  no matches")
	default:
		where = styDim.Render(fmt.Sprintf("  %d of %d", s.idx+1, len(s.matches)))
	}
	return styHeading.Render(" find ") + " " + s.query + styCursor.Render(" ") + where
}

// devSearchKey handles one keystroke while the search is open. It reports
// whether the search is still open.
func (m *devModel) devSearchKey(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "esc", "ctrl+c":
		// Cancelling puts the cursor back where the search started: a search
		// that moved you and then gave up would be worse than no search.
		m.buf.row, m.buf.col = m.search.origin.row, m.search.origin.col
		m.buf.clampCol()
		return false

	case "enter":
		// Enter keeps the cursor on the match — the search was the way of
		// getting there, and now you are there.
		return false

	case "down", "ctrl+n":
		m.search.step(1)
	case "up", "ctrl+p":
		m.search.step(-1)

	case "backspace":
		if m.search.query != "" {
			m.search.query = m.search.query[:len(m.search.query)-1]
			m.search.find(m.buf)
			m.search.nearestFrom(m.search.origin)
		}

	default:
		if msg.Text == "" {
			return true // a modifier or a function key is not a query
		}
		m.search.query += msg.Text
		m.search.find(m.buf)
		m.search.nearestFrom(m.search.origin)
	}

	if hit, ok := m.search.current(); ok {
		m.buf.row, m.buf.col = hit.row, hit.start
		m.buf.goalCol = m.buf.col
	}
	return true
}
