// Ctrl+R — searching the history instead of walking it.
//
// History persists across sessions now, which makes Up a poor way to reach
// anything typed more than a few lines ago. This is the shell's answer: type a
// fragment, see the most recent line containing it, press Ctrl+R again to walk
// further back, and Enter to put the match on the prompt (where it can still
// be edited before it runs — a search that submitted for you would be a
// search you had to be sure about).
//
// Matching is case-insensitive substring, newest first. Nothing fancier is
// warranted: a REPL history is short lines of the same language, and fuzzy
// matching over that finds more surprises than statements.
package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// historySearch is the Ctrl+R overlay: a query, the matches for it, and which
// one is showing.
type historySearch struct {
	query   string
	matches []string
	idx     int
	keys    searchKeyMap
}

type searchKeyMap struct {
	Next   key.Binding
	Prev   key.Binding
	Accept key.Binding
	Cancel key.Binding
}

func defaultSearchKeys() searchKeyMap {
	return searchKeyMap{
		Next:   key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "older match")),
		Prev:   key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "newer match")),
		Accept: key.NewBinding(key.WithKeys("enter", "tab", "right"), key.WithHelp("enter", "put it on the prompt")),
		Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c", "ctrl+g"), key.WithHelp("esc", "cancel")),
	}
}

// newHistorySearch opens a search over h, seeded with whatever was already
// typed — the line in progress is usually the thing being looked for.
func newHistorySearch(h *history, seed string) *historySearch {
	s := &historySearch{keys: defaultSearchKeys()}
	s.setQuery(h, seed)
	return s
}

// setQuery re-runs the search, newest first.
func (s *historySearch) setQuery(h *history, query string) {
	s.query, s.matches, s.idx = query, nil, 0
	needle := strings.ToLower(query)
	for i := len(h.entries) - 1; i >= 0; i-- {
		if needle == "" || strings.Contains(strings.ToLower(h.entries[i]), needle) {
			s.matches = append(s.matches, h.entries[i])
		}
	}
}

// current is the match on show, or "" when nothing matches.
func (s *historySearch) current() string {
	if len(s.matches) == 0 {
		return ""
	}
	return s.matches[s.idx]
}

// update handles one keystroke. It reports whether the search is still open,
// and the line to put on the prompt when it is not (empty on cancel).
func (s *historySearch) update(msg tea.KeyPressMsg, h *history) (open bool, accepted string) {
	switch {
	case matches(msg, s.keys.Cancel):
		return false, ""
	case matches(msg, s.keys.Accept):
		return false, s.current()
	case matches(msg, s.keys.Next):
		if len(s.matches) > 0 {
			s.idx = (s.idx + 1) % len(s.matches)
		}
		return true, ""
	case matches(msg, s.keys.Prev):
		if len(s.matches) > 0 {
			s.idx = (s.idx - 1 + len(s.matches)) % len(s.matches)
		}
		return true, ""
	case msg.String() == "backspace":
		if s.query != "" {
			s.setQuery(h, s.query[:len(s.query)-1])
		}
		return true, ""
	}
	if text := msg.Text; text != "" {
		s.setQuery(h, s.query+text)
	}
	return true, ""
}

// view is the two lines the search occupies: the match, and the query with a
// count of what it found.
func (s *historySearch) view(width int) string {
	var b strings.Builder
	if line := s.current(); line != "" {
		b.WriteString(promptTop + highlightMatch(line, s.query))
	} else {
		b.WriteString(promptTop + styDim.Render("(no match)"))
	}
	b.WriteString("\n")

	count := ""
	if len(s.matches) > 1 {
		count = styDim.Render(fmt.Sprintf(" [%d/%d]", s.idx+1, len(s.matches)))
	}
	b.WriteString(styDim.Render("history search: ") + s.query + count)

	// Each line is clipped on its own: a width is a line's width, and
	// truncating the block as one string would eat the newline between them.
	lines := strings.Split(b.String(), "\n")
	for i, line := range lines {
		lines[i] = truncateVis(line, max(width, 20))
	}
	return strings.Join(lines, "\n")
}

// highlightMatch marks where the query was found, so a long line shows why it
// is the answer.
func highlightMatch(line, query string) string {
	if query == "" {
		return highlightSource(line, true)
	}
	i := strings.Index(strings.ToLower(line), strings.ToLower(query))
	if i < 0 {
		return highlightSource(line, true)
	}
	return line[:i] + styMatch.Render(line[i:i+len(query)]) + line[i+len(query):]
}
