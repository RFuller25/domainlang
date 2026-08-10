// Finding a row.
//
// The search started as a substring match over the row's label, which answers
// exactly one question: where is the `Map Each`. The questions a reader
// actually arrives with are mostly about something else — which stage produced
// a `List<Text>`, which one is slow, which one touched line 12, which one has
// the 47 in it — and none of them are answerable by the label, because a label
// is a primitive's name and every one of them is called the same thing.
//
// So a search term can name a field: `type:List<Int>`, `line:12`, `>5ms`,
// `err:`, `out:47`, `#42`. A term with no field is still a substring of the
// label, so nothing anyone already types has changed meaning. Terms are ANDed,
// which is how a person narrows: `map >1ms` is the slow one of the four.
//
// Filtering is also not the only thing to do with a match. `/` narrows the tree
// to matching rows and the paths to them, which is the right shape when there
// are many; `n` and `N` step between matches with the tree intact, which is the
// right shape when you want to see each one in context. They share the matcher.
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"domain/interp"
)

// searchTerm is one space-separated piece of a query.
type searchTerm struct {
	field string // "", "type", "line", "prim", "err", "out", "in", "step"
	value string
	// Comparison terms (>5ms, <1%) carry a parsed bound instead of a value.
	cmp   byte // '>' or '<', 0 when this is not a comparison
	dur   time.Duration
	pct   float64
	isPct bool
}

// parseQuery breaks a query into terms.
func parseQuery(q string) []searchTerm {
	var terms []searchTerm
	for _, word := range strings.Fields(q) {
		terms = append(terms, parseTerm(word))
	}
	return terms
}

func parseTerm(word string) searchTerm {
	if len(word) > 1 && (word[0] == '>' || word[0] == '<') {
		if t, ok := parseBound(word[0], word[1:]); ok {
			return t
		}
	}
	// `#42` is the step number, which is what --json prints and what a reader
	// arrives holding.
	if strings.HasPrefix(word, "#") {
		return searchTerm{field: "step", value: word[1:]}
	}
	if i := strings.IndexByte(word, ':'); i > 0 {
		field := strings.ToLower(word[:i])
		switch field {
		case "type", "line", "prim", "err", "error", "out", "in", "step":
			if field == "error" {
				field = "err"
			}
			return searchTerm{field: field, value: word[i+1:]}
		}
	}
	return searchTerm{value: word}
}

// parseBound reads a `>5ms` or `<10%` comparison.
func parseBound(cmp byte, rest string) (searchTerm, bool) {
	if strings.HasSuffix(rest, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(rest, "%"), 64)
		if err != nil {
			return searchTerm{}, false
		}
		return searchTerm{cmp: cmp, pct: f, isPct: true}, true
	}
	d, err := time.ParseDuration(rest)
	if err != nil {
		return searchTerm{}, false
	}
	return searchTerm{cmp: cmp, dur: d}, true
}

// matches reports whether a row satisfies every term of the query.
func (m *visualModel) matchesQuery(n *interp.TraceNode, terms []searchTerm) bool {
	for _, t := range terms {
		if !m.matchesTerm(n, t) {
			return false
		}
	}
	return len(terms) > 0
}

func (m *visualModel) matchesTerm(n *interp.TraceNode, t searchTerm) bool {
	if t.cmp != 0 {
		nt := m.view.times().Of(n)
		if t.isPct {
			if t.cmp == '>' {
				return nt.SelfPct > t.pct
			}
			return nt.SelfPct < t.pct
		}
		if t.cmp == '>' {
			return nt.Self > t.dur
		}
		return nt.Self < t.dur
	}
	needle := strings.ToLower(t.value)
	switch t.field {
	case "":
		return strings.Contains(strings.ToLower(n.Label()), needle)
	case "err":
		// A bare `err:` is "anything that failed", which is the common case;
		// with text after it, that text has to be in the message.
		if n.IsFrame() || n.Step.Err == nil {
			return false
		}
		return needle == "" || strings.Contains(strings.ToLower(n.Step.Err.Error()), needle)
	}
	if n.IsFrame() {
		return false
	}
	s := n.Step
	switch t.field {
	case "type":
		typ := typeOf(s)
		if n.Block != nil {
			typ = n.Block.Type
		}
		return strings.Contains(strings.ToLower(typ), needle)
	case "prim":
		return strings.Contains(strings.ToLower(s.Node.Prim), needle)
	case "line":
		want, err := strconv.Atoi(t.value)
		if err != nil {
			return false
		}
		if _, foreign := s.Node.Foreign(); foreign {
			return false
		}
		return s.Node.Pos.Line == want
	case "step":
		want, err := strconv.Atoi(t.value)
		return err == nil && s.Index == want
	case "out":
		return strings.Contains(strings.ToLower(s.Short), needle) ||
			strings.Contains(strings.ToLower(s.Full), needle)
	case "in":
		return strings.Contains(strings.ToLower(s.InShort), needle)
	}
	return false
}

// matching is the set of rows the filter admits: every row that matches, every
// ancestor that leads to one (so the path stays readable), and everything
// underneath a match (so a matched frame can still be stepped into). It returns
// nil when no filter is active, which means "keep everything".
func (m *visualModel) matching() map[*interp.TraceNode]bool {
	if m.filter == "" {
		return nil
	}
	terms := m.terms()
	keep := map[*interp.TraceNode]bool{}

	var walk func(n *interp.TraceNode, underMatch bool) bool
	walk = func(n *interp.TraceNode, underMatch bool) bool {
		hit := m.matchesQuery(n, terms)
		found := hit || underMatch
		for _, c := range n.Children {
			if walk(c, found) {
				found = true
			}
		}
		if found {
			keep[n] = true
		}
		return found
	}
	for _, n := range m.view.rec.Roots() {
		walk(n, false)
	}
	return keep
}

// terms is the parsed filter, re-parsed only when the filter text changes.
// isMatch is asked of every visible row on every frame, and re-parsing a query
// forty times a redraw to get the same answer is work for nothing.
func (m *visualModel) terms() []searchTerm {
	if m.filter != m.parsedFrom {
		m.parsedFrom, m.parsed = m.filter, parseQuery(m.filter)
	}
	return m.parsed
}

// isMatch reports whether a row matched the query itself, as opposed to being
// an ancestor kept only to show the path to one.
func (m *visualModel) isMatch(n *interp.TraceNode) bool {
	if m.filter == "" {
		return false
	}
	return m.matchesQuery(n, m.terms())
}

// nextMatch moves the cursor to the next (or previous) row matching the query,
// wrapping, without narrowing the tree. It is the other half of `/`: sometimes
// you want the matches alone, and sometimes you want each one where it sits.
func (m *visualModel) nextMatch(dir int) {
	if m.filter == "" {
		m.status = "no search to step through — / starts one"
		return
	}
	terms := m.terms()
	start := m.order[m.selectedNode()]
	for i := 1; i <= len(m.flat); i++ {
		idx := ((start+dir*i)%len(m.flat) + len(m.flat)) % len(m.flat)
		n := m.flat[idx]
		if !m.matchesQuery(n, terms) {
			continue
		}
		m.reveal(n)
		m.status = fmt.Sprintf("match %d of %d for /%s",
			m.matchOrdinal(n, terms), m.matchCount(terms), m.filter)
		return
	}
	m.status = fmt.Sprintf("nothing else matches /%s", m.filter)
}

func (m *visualModel) matchCount(terms []searchTerm) int {
	n := 0
	for _, node := range m.flat {
		if m.matchesQuery(node, terms) {
			n++
		}
	}
	return n
}

func (m *visualModel) matchOrdinal(target *interp.TraceNode, terms []searchTerm) int {
	n := 0
	for _, node := range m.flat {
		if m.matchesQuery(node, terms) {
			n++
		}
		if node == target {
			return n
		}
	}
	return n
}

// searchHint is the line under a search box, naming the fields — a query
// language nobody is told about is a query language nobody uses.
const searchHint = "type: prim: line: out: in: err: #step >5ms <1%"

// updateSearch handles keys while the filter is being typed. The tree updates
// on every keystroke, so the search is the result rather than a prelude to it.
func (m *visualModel) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, m.quit()
	case "esc":
		m.searching, m.filter = false, ""
		m.rebuild()
	case "enter":
		// Accept: the filter stays, the typing stops, the tree is navigable.
		m.searching = false
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.rebuild()
		}
	default:
		if t := msg.Text; t != "" && !strings.ContainsFunc(t, unicodeIsControl) {
			m.filter += t
			m.rebuild()
		}
	}
	return m, nil
}

// updateJump handles keys while a step number is being typed after `:`.
//
// It exists because --json reports a step index and the UI had no way to look
// one up: the documented `visualize --json | jq '.hotspots[0]'` workflow ended
// at a number that named a row nobody could find.
func (m *visualModel) updateJump(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, m.quit()
	case "esc":
		m.jumping, m.jumpBuf = false, ""
	case "enter":
		m.jumping = false
		m.jumpToStep(m.jumpBuf)
		m.jumpBuf = ""
	case "backspace":
		if r := []rune(m.jumpBuf); len(r) > 0 {
			m.jumpBuf = string(r[:len(r)-1])
		}
	default:
		if t := msg.Text; len(t) == 1 && t[0] >= '0' && t[0] <= '9' {
			m.jumpBuf += t
		}
	}
	return m, nil
}

// jumpToStep moves the cursor to a step by its recorded index.
func (m *visualModel) jumpToStep(s string) {
	want, err := strconv.Atoi(s)
	if err != nil {
		m.status = "a step number is a number"
		return
	}
	for _, n := range m.flat {
		if n.IsFrame() || n.Step.Index != want {
			continue
		}
		m.reveal(n)
		m.status = fmt.Sprintf("step #%d · %s", want, n.Label())
		return
	}
	m.status = fmt.Sprintf("no step #%d in this recording", want)
}
