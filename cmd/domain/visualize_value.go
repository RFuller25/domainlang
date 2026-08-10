// Showing a value as the shape it is.
//
// The recorder keeps two renderings of every value: a short one for the tree's
// columns, and the full one `Reveal` would print. The full one is a *string*,
// and the value pane used to put it on the screen as a string — which is right
// for a number and wrong for everything the command is named after.
//
// A grid arrives as rows separated by newlines, and read as a wall of digits
// nobody can find a coordinate in. A list of two hundred elements arrives as
// one line beginning `[` that runs off the right edge, so the two hundredth is
// not merely hard to see, it is unreachable. Text arrives with its trailing
// spaces invisible, which is the one thing about a Text value that most often
// explains a wrong answer.
//
// So the pane asks the value's *type* what it is looking at and renders
// accordingly: a grid gets row and column gutters, a collection gets one
// element per line with its index, and Text gets its whitespace made visible.
// The renderings are still derived from what the recorder captured — nothing
// new is kept, and a value it could not keep in full says so rather than
// pretending the part it has is the whole.
package main

import (
	"fmt"
	"strconv"
	"strings"

	"domain/interp"
)

// recordedValue is a captured value and everything needed to render it: the two
// renderings, why the long one might be partial, and the type that says what
// shape it is.
type recordedValue struct {
	short  string
	full   string
	fullOK bool
	spent  bool
	typ    string
	size   int
	sizeOK bool
}

// recordedOf adapts a block's captured result.
func recordedOf(r *interp.Recorded) recordedValue {
	if r == nil {
		return recordedValue{}
	}
	return recordedValue{
		short: r.Short, full: r.Full, fullOK: r.FullOK, spent: r.Spent,
		typ: r.Type, size: r.Size, sizeOK: r.SizeOK,
	}
}

// stepValue adapts a step's own output.
func stepValue(s *interp.Step) recordedValue {
	if s == nil {
		return recordedValue{}
	}
	v := recordedValue{
		short: s.Short, full: s.Full, fullOK: s.FullOK, spent: s.Spent,
		size: s.Size, sizeOK: s.SizeOK,
	}
	if s.Node != nil && s.Node.Out != nil {
		v.typ = s.Node.Out.String()
	}
	return v
}

// text is the best rendering available: the full one where there is one, the
// short one otherwise.
func (v recordedValue) text() string {
	if v.full != "" {
		return v.full
	}
	return v.short
}

// valueBody renders a captured value into the detail pane, in whatever shape
// its type says it is.
func valueBody(v recordedValue, w int) []string {
	body := v.text()
	if body == "" {
		return []string{styDim.Render("  (nothing)")}
	}

	var out []string
	switch {
	case strings.HasPrefix(v.typ, "Grid"), strings.HasPrefix(v.typ, "Sparse"):
		out = gridBody(body, w)
	case isCollectionType(v.typ):
		out = collectionBody(body, w)
	case v.typ == "Text":
		out = textBody(body, w)
	default:
		for _, line := range strings.Split(body, "\n") {
			out = append(out, "  "+styValue.Render(truncateVis(line, w-2)))
		}
	}
	return append(out, valueNote(v)...)
}

// valueNote says why a rendering is not the whole value — and says which of the
// two reasons it is. "Truncated" and "never captured" are different answers: the
// first means there is more of this value, the second means the recording ran
// out of room some steps ago and this one paid for it. Reporting both as
// "truncated" sent a reader looking for a longer value that was never built.
func valueNote(v recordedValue) []string {
	switch {
	case v.spent:
		return []string{styDim.Render(
			"  … (not captured — the recording's value budget was spent; the short form is above)")}
	case !v.fullOK && v.full != "":
		return []string{styDim.Render("  … (only the first part of this value was kept)")}
	case !v.fullOK:
		return []string{styDim.Render("  … (value shown in short form)")}
	}
	return nil
}

// isCollectionType reports whether a type renders as a bracketed sequence worth
// breaking into rows.
func isCollectionType(t string) bool {
	for _, prefix := range []string{"List", "Set", "Map"} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// gridBody renders a grid as a grid: the rows are already lines, and what they
// are missing is any way to say which row and column you are looking at. A
// coordinate is the whole reason someone opens a grid in a debugger.
func gridBody(body string, w int) []string {
	rows := strings.Split(body, "\n")
	gutter := len(strconv.Itoa(len(rows)))
	out := make([]string, 0, len(rows)+1)

	// A column ruler, when the rows are single characters and there is room for
	// it — which is the shape a Game of Life or a maze has, and exactly where
	// counting columns by eye goes wrong.
	if width := gridWidth(rows); width > 1 && width <= w-gutter-4 && singleCharCells(rows) {
		out = append(out, styDim.Render(strings.Repeat(" ", gutter+1)+columnRuler(width)))
	}
	for i, row := range rows {
		num := styDim.Render(fmt.Sprintf("%*d", gutter, i))
		out = append(out, " "+num+" "+styValue.Render(truncateVis(row, max(4, w-gutter-3))))
	}
	return out
}

// gridWidth is the width of the widest row.
func gridWidth(rows []string) int {
	w := 0
	for _, r := range rows {
		w = max(w, len([]rune(r)))
	}
	return w
}

// singleCharCells reports whether every row is a run of single characters — a
// picture rather than a table of numbers, which is what a column ruler helps
// with and what a spaced-out row of integers does not.
func singleCharCells(rows []string) bool {
	for _, r := range rows {
		if strings.Contains(r, " ") {
			return false
		}
	}
	return len(rows) > 0
}

// columnRuler draws tens and units under a grid, so a column can be counted at
// a glance rather than with a finger on the screen.
func columnRuler(width int) string {
	var tens, units strings.Builder
	for c := range width {
		if c%10 == 0 {
			tens.WriteString(strconv.Itoa(c / 10 % 10))
		} else {
			tens.WriteByte(' ')
		}
		units.WriteString(strconv.Itoa(c % 10))
	}
	if width <= 10 {
		return units.String()
	}
	return tens.String() + "\n" + units.String()
}

// collectionBody renders a list, set or map one element per line with its
// index. A two-hundred-element list rendered as one line is not merely hard to
// read: everything past the pane's width is unreachable, and the index is the
// thing a reader is looking for ("which element is wrong?").
//
// The split is over the rendered form rather than the value — the recorder kept
// a string, not the list — so it counts brackets to find the top-level commas
// and falls back to the flat rendering when the shape is not what it expects. A
// display that guesses wrong should look ordinary, not broken.
func collectionBody(body string, w int) []string {
	elems, ok := splitRendered(body)
	if !ok || len(elems) < 4 {
		return []string{"  " + styValue.Render(truncateVis(body, w-2))}
	}
	gutter := len(strconv.Itoa(len(elems) - 1))
	out := make([]string, 0, len(elems))
	for i, e := range elems {
		num := styDim.Render(fmt.Sprintf("%*d", gutter, i))
		out = append(out, " "+num+" "+styValue.Render(truncateVis(e, max(4, w-gutter-3))))
	}
	return out
}

// splitRendered breaks a rendered collection into its top-level elements. It
// reports false for anything that is not a single bracketed sequence, which is
// the caller's cue to show the text as it is.
func splitRendered(body string) ([]string, bool) {
	if len(body) < 2 {
		return nil, false
	}
	open, close := body[0], body[len(body)-1]
	if !(open == '[' && close == ']') && !(open == '{' && close == '}') {
		return nil, false
	}
	inner := body[1 : len(body)-1]
	if inner == "" {
		return nil, false
	}

	var elems []string
	depth, start, quoted := 0, 0, false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case quoted:
			// A rendered string can contain brackets and commas; skip them,
			// respecting the backslash escapes strconv.Quote produces.
			if c == '\\' {
				i++
			} else if c == '"' {
				quoted = false
			}
		case c == '"':
			quoted = true
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			elems = append(elems, strings.TrimSpace(inner[start:i]))
			start = i + 1
		}
	}
	if depth != 0 || quoted {
		return nil, false
	}
	return append(elems, strings.TrimSpace(inner[start:])), true
}

// textBody renders a Text value with its line structure kept and its trailing
// whitespace made visible — the difference that decides whether the next stage
// can parse it, and the one a terminal hides by definition.
func textBody(body string, w int) []string {
	lines := strings.Split(body, "\n")
	if len(lines) == 1 {
		return []string{"  " + styValue.Render(truncateVis(showEnds(body), w-2))}
	}
	gutter := len(strconv.Itoa(len(lines)))
	out := make([]string, 0, len(lines)+1)
	for i, line := range lines {
		num := styDim.Render(fmt.Sprintf("%*d", gutter, i+1))
		out = append(out, " "+num+" "+styValue.Render(truncateVis(showEnds(line), max(4, w-gutter-3))))
	}
	if !strings.HasSuffix(body, "\n") {
		out = append(out, styDim.Render("  (no trailing newline)"))
	}
	return out
}
