// Package shape reads an input file and suggests the Domain statements that
// would take it in.
//
// The opening of an Advent-of-Code-shaped program is nearly mechanical — split
// on blank lines, convert to integers, make a grid — and it is also the part
// you have to get right before anything else can be written. That is a bad
// combination: mechanical enough to be boring, load-bearing enough that
// guessing wrong costs a debugging session.
//
// Three things make this tractable where "suggest some code" generally is not.
// The vocabulary is tiny: the prelude is five Shikigami, four of which are
// input shapes, plus a handful of openers. The specification is already
// written — docs/aoc-toolbox.md is a hand-authored table mapping input shape to
// Domain opening. And the repository carries a labelled corpus: every program
// in examples/ and challenges/ sits beside the input it reads, which makes the
// suggester testable against an oracle rather than judged by eye.
//
// It ranks rather than decides. Some inputs are genuinely ambiguous — a
// rectangle of digits is a grid or a list of numbers depending on what you
// meant, and nothing in the file says which — so the honest answer is an
// ordered list with the evidence for each, not a confident wrong one.
//
// The package is pure: text in, suggestions out. It has no terminal in it, so
// its correctness is a corpus question rather than a UI one.
package shape

import (
	"strings"
	"unicode"
)

// Shape is what was observed about an input file. It is exported because the
// evidence is worth showing next to a suggestion: "every line is an integer"
// is a better reason to trust `Shikigami: Ints` than the suggestion itself.
type Shape struct {
	Lines []string
	// Blocks is true when blank lines separate groups — the `"\n\n"` split.
	Blocks bool
	// AllInt is true when every non-empty line parses as an integer.
	AllInt bool
	// Rect is true when every non-empty line is the same width and there is
	// more than one of them.
	Rect bool
	// Width is that common width, when Rect.
	Width int
	// AllDigits is true when every character of every line is 0-9 — a grid of
	// digits, or a column of numbers, depending on what was meant.
	AllDigits bool
	// Alphabet is the distinct characters, which is what distinguishes a `.#`
	// map from prose.
	Alphabet []rune
	// Template is a `Match Pattern` template that fits every line, when one
	// does.
	Template string
	// Separator is the delimiter of a single line that is really a list.
	Separator string
}

// Candidate is one suggested opening: the statements to insert, and why.
type Candidate struct {
	// Statements are the lines to put at the top of the program, already
	// indented relative to each other.
	Statements []string
	// Why is the evidence, in a phrase — shown beside the suggestion so the
	// choice is informed rather than trusted.
	Why string
}

// First is the candidate's opening statement, which is what identifies it.
func (c Candidate) First() string {
	if len(c.Statements) == 0 {
		return ""
	}
	return c.Statements[0]
}

// Observe measures an input file.
func Observe(input string) Shape {
	text := strings.ReplaceAll(strings.TrimRight(input, "\n"), "\r\n", "\n")
	s := Shape{Lines: strings.Split(text, "\n"), Blocks: strings.Contains(text, "\n\n")}

	var nonEmpty []string
	for _, l := range s.Lines {
		if l != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	if len(nonEmpty) == 0 {
		return s
	}

	s.AllInt = true
	s.AllDigits = true
	widths := map[int]bool{}
	alphabet := map[rune]bool{}
	for _, l := range nonEmpty {
		if !isInt(l) {
			s.AllInt = false
		}
		for _, r := range l {
			alphabet[r] = true
			if !unicode.IsDigit(r) {
				s.AllDigits = false
			}
		}
		widths[len([]rune(l))] = true
	}
	for r := range alphabet {
		s.Alphabet = append(s.Alphabet, r)
	}
	if len(widths) == 1 && len(nonEmpty) > 1 {
		s.Rect = true
		for w := range widths {
			s.Width = w
		}
	}

	if t, ok := inferTemplate(nonEmpty); ok {
		s.Template = t
	}
	if len(nonEmpty) == 1 {
		s.Separator = inferSeparator(nonEmpty[0])
	}
	return s
}

// Suggest ranks the openings that would read this input, best first.
func Suggest(input string) []Candidate {
	s := Observe(input)
	if len(s.Lines) == 0 || (len(s.Lines) == 1 && s.Lines[0] == "") {
		return nil
	}
	var out []Candidate
	add := func(why string, stmts ...string) {
		out = append(out, Candidate{Statements: stmts, Why: why})
	}

	switch {
	case s.Blocks:
		// Blank-line-separated groups. Whether the groups are numbers decides
		// which of the two openings comes first, but both are offered: a block
		// of text is as common as a block of numbers.
		if s.AllInt {
			add("blank lines separate groups, and every line is an integer",
				`Cursed Technique: Split Text by "\n\n"`,
				`Cursed Technique: Split Each by "\n"`,
				"Channeled Energy: Convert Each List to Integers")
			add("the same, through the prelude", "Shikigami: Blocks")
		} else {
			add("blank lines separate groups", `Cursed Technique: Split Text by "\n\n"`)
			add("groups, split into their lines", "Shikigami: Blocks")
		}

	case s.Rect && s.AllDigits:
		// The genuine ambiguity: a rectangle of digits is a grid or a column of
		// numbers, and the file cannot say which. Width decides the order —
		// three or more digits a line reads as a grid, one or two as numbers —
		// and both are always offered, because the ordering is a guess and the
		// choice is not.
		grid := Candidate{
			Statements: []string{"Shikigami: Digit Grid"},
			Why:        "every line is the same width and all digits",
		}
		ints := Candidate{
			Statements: []string{"Shikigami: Ints"},
			Why:        "every line parses as an integer",
		}
		if s.Width >= 3 {
			out = append(out, grid, ints)
		} else {
			out = append(out, ints, grid)
		}

	case s.AllInt:
		add("every line parses as an integer", "Shikigami: Ints")
		// The same file read as text. An int-per-line input is sometimes wanted
		// as lines — to keep leading zeros, or to convert later — so the
		// alternative is offered rather than assumed away.
		add("the lines, left as text", "Shikigami: Lines")

	case s.Rect && len(s.Alphabet) <= 4:
		add("every line is the same width, over a small alphabet",
			"Shikigami: Lines", "Channeled Energy: Convert To Grid")
		add("the lines, left as text", "Shikigami: Lines")

	case s.Template != "":
		add("every line fits one pattern",
			"Shikigami: Lines",
			"Cursed Technique: Match Pattern",
			`    Using: "`+s.Template+`"`,
			"    Mode: Each")
		add("the lines, left as text", "Shikigami: Lines")

	case s.Separator != "":
		add("one line, separated by "+quote(s.Separator),
			`Cursed Technique: Split Text by "`+s.Separator+`"`)

	default:
		add("the lines, left as text", "Shikigami: Lines")
		if hasFields(s.Lines) {
			add("whitespace-separated fields on each line",
				"Shikigami: Lines", "Cursed Technique: Split Fields")
		}
	}

	// A line-per-record file almost always wants its integers eventually, and
	// `Extract Integers` is the one opening that does not care what surrounds
	// them. It goes last: it is a fallback, not a reading.
	if !s.Blocks && !s.AllInt && hasDigits(s.Lines) {
		add("there are numbers in the lines",
			"Shikigami: Lines", "Cursed Technique: Extract Integers")
	}
	return out
}

// ---------------------------------------------------------------------------
// measurements
// ---------------------------------------------------------------------------

func isInt(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func hasDigits(lines []string) bool {
	for _, l := range lines {
		for _, r := range l {
			if unicode.IsDigit(r) {
				return true
			}
		}
	}
	return false
}

// hasFields reports whether every non-empty line has interior whitespace, which
// is what makes `Split Fields` the right reading rather than a guess.
func hasFields(lines []string) bool {
	n := 0
	for _, l := range lines {
		if l == "" {
			continue
		}
		if len(strings.Fields(l)) < 2 {
			return false
		}
		n++
	}
	return n > 0
}

// inferSeparator finds the delimiter of a single line that is really a list.
// Only a few are worth guessing: a separator that appears once is punctuation,
// not structure.
func inferSeparator(line string) string {
	for _, sep := range []string{", ", ",", " | ", ";", "\t"} {
		if strings.Count(line, sep) >= 2 {
			return sep
		}
	}
	return ""
}

func quote(s string) string { return `"` + s + `"` }

// ---------------------------------------------------------------------------
// template inference
// ---------------------------------------------------------------------------

// tokenKind is what one run of a line is made of.
type tokenKind int

const (
	tokInt tokenKind = iota
	tokWord
	tokLiteral
)

type token struct {
	kind tokenKind
	text string
}

// tokenize splits a line into runs of digits, runs of letters, and everything
// else verbatim.
func tokenize(line string) []token {
	var out []token
	rs := []rune(line)
	for i := 0; i < len(rs); {
		switch {
		case unicode.IsDigit(rs[i]):
			j := i
			for j < len(rs) && unicode.IsDigit(rs[j]) {
				j++
			}
			out = append(out, token{tokInt, string(rs[i:j])})
			i = j
		case unicode.IsLetter(rs[i]):
			j := i
			for j < len(rs) && unicode.IsLetter(rs[j]) {
				j++
			}
			out = append(out, token{tokWord, string(rs[i:j])})
			i = j
		default:
			out = append(out, token{tokLiteral, string(rs[i])})
			i++
		}
	}
	return out
}

// inferTemplate finds a `Match Pattern` template that fits every line, or
// reports that none does.
//
// Two rules make the result usable rather than merely correct. Every line must
// tokenize to the same *sequence of kinds*, or the file has more than one shape
// in it and a single template would be a lie. And a *word* that is identical on
// every line is a literal, not a hole: in `alice grade 12` the middle word is
// part of the format, and turning it into `{word}` would capture a constant.
// The rule stops at words deliberately — a number that repeats is the data
// coinciding, not the format speaking.
func inferTemplate(lines []string) (string, bool) {
	if len(lines) < 2 {
		return "", false
	}
	first := tokenize(lines[0])
	if len(first) < 2 {
		return "", false
	}
	// A template with no holes describes nothing; a file of identical lines is
	// not a pattern worth matching.
	holes := 0
	for _, t := range first {
		if t.kind != tokLiteral {
			holes++
		}
	}
	if holes == 0 {
		return "", false
	}

	constant := make([]bool, len(first))
	for i := range constant {
		constant[i] = true
	}
	for _, line := range lines[1:] {
		toks := tokenize(line)
		if len(toks) != len(first) {
			return "", false
		}
		for i, t := range toks {
			if t.kind != first[i].kind {
				return "", false
			}
			if t.kind == tokLiteral && t.text != first[i].text {
				return "", false
			}
			if t.text != first[i].text {
				constant[i] = false
			}
		}
	}

	var b strings.Builder
	for i, t := range first {
		switch {
		case t.kind == tokLiteral, t.kind == tokWord && constant[i]:
			// A word that never varies is part of the format. A *number* that
			// never varies is a coincidence — numbers are the data, and two
			// lines that happen to start with the same one would otherwise
			// freeze it into the template and make it match nothing else.
			b.WriteString(t.text)
		case t.kind == tokInt:
			b.WriteString("{int}")
		default:
			b.WriteString("{word}")
		}
	}
	out := b.String()
	if !strings.Contains(out, "{") {
		return "", false
	}
	return out, true
}
