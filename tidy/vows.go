// Suggesting Binding Vows from a value the program actually produced.
//
// A vow after the parse is the cheapest check a program can carry: it catches
// the wrong input file, or a template that quietly matched less than it
// should, on the line where it happened — and `--release` sheds it.
//
// Unlike the rewrites in this package, a vow is a claim about the *input*, and
// only its author knows which claims are meant. A list of 1000 lines is
// `Count Equals 1000` for today's puzzle input and wrong for the example ten
// lines above it. So these are never applied on save; they are offered, with
// the evidence, for someone to choose from.
package tidy

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"domain/ast"
	// A host registers itself from an init; without it RunScoped would run a
	// game program through the plain interpreter and see nothing.
	_ "domain/game"
	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/token"
)

// Vow is one suggested assertion.
type Vow struct {
	// Lines is the statement as it would be written, unindented.
	Lines []string
	// Why is the evidence, in a few words.
	Why string
	// Elsewhere says how the vow fared on the program's other input, when
	// there is one to compare against: it held there too, or it did not.
	// Empty when nothing was compared.
	Elsewhere string
	// Portable is true when the vow held on every input it was tried on —
	// the difference between a fact about the puzzle and one about a file.
	Portable bool
}

// SuggestVows proposes vows that hold of v, a value of type t. The first is
// the one most worth having.
func SuggestVows(v ir.Value, t *ir.Type) []Vow {
	if t == nil {
		return nil
	}
	var out []Vow
	holds := func(lambda, why string) {
		out = append(out, Vow{Lines: []string{"Binding Vow: Holds", "    Using: " + lambda}, Why: why})
	}

	switch t.Kind {
	case ir.KList:
		xs, ok := v.([]ir.Value)
		if !ok {
			return nil
		}
		n := len(xs)
		ints := t.Elem != nil && t.Elem.Kind == ir.KInt
		if ints {
			out = append(out, Vow{
				Lines: []string{fmt.Sprintf("Binding Vow: Count Equals %d", n)},
				Why:   fmt.Sprintf("the list has %d items on this input", n),
			})
		} else {
			holds(fmt.Sprintf("(xs) -> length(xs) = %d", n), fmt.Sprintf("the list has %d items on this input", n))
		}
		if n > 0 {
			holds("(xs) -> length(xs) > 0", "the list is not empty — true of any real input")
		}
		if ints && n > 0 {
			lo := xs[0].(int64)
			for _, x := range xs {
				if i, ok := x.(int64); ok && i < lo {
					lo = i
				}
			}
			switch {
			case lo > 0:
				out = append(out, Vow{Lines: []string{"Binding Vow: All Values > 0"},
					Why: fmt.Sprintf("every number is positive (the smallest is %d)", lo)})
			case lo == 0:
				out = append(out, Vow{Lines: []string{"Binding Vow: All Values >= 0"},
					Why: "no number is negative (the smallest is 0)"})
			}
		}
		// Rows of one width: a ragged parse is the commonest silent failure.
		if n > 1 && t.Elem != nil && t.Elem.Kind == ir.KList {
			w, same := -1, true
			for _, x := range xs {
				row, ok := x.([]ir.Value)
				if !ok {
					same = false
					break
				}
				if w < 0 {
					w = len(row)
				} else if len(row) != w {
					same = false
				}
			}
			if same && w > 0 {
				holds(fmt.Sprintf("(xs) -> length(first(xs)) = %d and length(last(xs)) = %d", w, w),
					fmt.Sprintf("every row has %d items; this checks the first and last", w))
			}
		}
	case ir.KGrid:
		g, ok := v.(*ir.GridValue)
		if !ok {
			return nil
		}
		holds(fmt.Sprintf("(g) -> rows(g) = %d and cols(g) = %d", g.Rows, g.Cols),
			fmt.Sprintf("the grid is %d×%d on this input", g.Rows, g.Cols))
		if g.Rows == g.Cols {
			holds("(g) -> rows(g) = cols(g)", "the grid is square")
		}
		holds("(g) -> rows(g) > 0 and cols(g) > 0", "the grid is not empty")
	case ir.KText:
		if s, ok := v.(string); ok && s != "" {
			holds("(t) -> length(t) > 0", "the text is not empty")
		}
	case ir.KInt:
		if i, ok := v.(int64); ok && i > 0 {
			holds("(n) -> n > 0", fmt.Sprintf("the number is positive (%d)", i))
		}
	case ir.KMap, ir.KSet:
		holds("(m) -> length(m) > 0", "it is not empty")
	}
	return out
}

// VowOffer is a set of suggestions and where they would go.
type VowOffer struct {
	Vows []Vow
	// AfterLine is the 1-based line the vows belong after: the last line of
	// the statement whose value they describe.
	AfterLine int
	// Stage is that statement's first line, for saying what was looked at.
	Stage string
	// Broken is true when the vows are ones the value *fails*: the offer made
	// after a run that died, of what would have said so sooner.
	Broken bool
	// Other is the second input the vows were compared against, if any.
	Other string
}

// parseWords begin the phrases that turn input text into the program's data.
// They are how "after the parse" is found in a program with no Parts to mark
// where the shared opening ends.
var parseWords = map[string]bool{
	"split": true, "match": true, "extract": true, "convert": true, "parse": true,
	"lines": true, "ints": true, "blocks": true, "digit": true, "chunk": true,
}

// SuggestVowsFor runs the program as far as the statement the vows are about
// and reports what holds of the value there.
//
// The statement is the one under the cursor when that is a top-level stage of
// the pipeline; otherwise it is the end of the parse — the last statement
// above the first Part that there is something to say about, or the end of the
// opening run of splitting and converting when there are no Parts.
//
// stop is called with a function that aborts the run, so a caller can put a
// deadline on a program that turns out to be slow before it has parsed.
//
// other, when not empty, is a second input file — the example beside the real
// input, or the other way about. Each vow is tried there too and marked with
// how it fared, and the ones that hold on both are listed first.
func SuggestVowsFor(path, src string, cursorLine int, baseDir, other string, stop func(abort func())) (VowOffer, error) {
	u, err := load(path, src)
	if err != nil {
		return VowOffer{}, fmt.Errorf("the program does not check yet: %s", firstLine(err.Error()))
	}
	starts := u.topLevelStarts()
	stmts := u.prog.Statements

	eligible := func(i int) bool {
		if i <= 0 || i >= len(stmts) {
			return false // the source statement's value is the raw text
		}
		switch stmts[i].Keyword {
		case "Part", "Channel", "Binding Vow", "Reveal", "Cursed Object", "Cursed Tool":
			return false
		}
		return true
	}
	lastLineOf := func(i int) int {
		first, last := u.tokRange(stmts[i].Pos.Offset, nextStart(starts, stmts[i].Pos.Offset))
		return u.lastLine(first, last)
	}

	firstPart := len(stmts)
	for i, s := range stmts {
		if s.Keyword == "Part" {
			firstPart = i
			break
		}
	}

	// The cursor's own statement is an explicit choice and stands alone.
	// Without one, the end of the shared opening is tried first and then each
	// statement above it: a program often ends its opening by packing the
	// parse into loop state, and the vow belongs on the parse, not the pack.
	var targets []int
	for i := range stmts {
		if i < firstPart && eligible(i) && cursorLine >= stmts[i].Pos.Line && cursorLine <= lastLineOf(i) {
			targets = []int{i}
		}
	}
	if len(targets) == 0 && firstPart < len(stmts) {
		for i := firstPart - 1; i > 0 && len(targets) < 4; i-- {
			if eligible(i) {
				targets = append(targets, i)
			}
		}
	}
	if len(targets) == 0 {
		end := 0
		for i := 1; i < len(stmts); i++ {
			s := stmts[i]
			if s.Op == nil || len(s.Op.Words) == 0 || !parseWords[strings.ToLower(s.Op.Words[0])] {
				break
			}
			end = i
		}
		for i := end; i > 0 && len(targets) < 4; i-- {
			if eligible(i) {
				targets = append(targets, i)
			}
		}
	}
	if len(targets) == 0 {
		return VowOffer{}, fmt.Errorf("put the cursor on the statement that finishes the parse")
	}

	trace := ir.NewInterrupter(nil)
	if stop != nil {
		stop(trace.Stop)
	}
	var firstErr error
	for _, target := range targets {
		offer, err := u.vowsAfter(path, baseDir, target, lastLineOf(target), trace)
		if err == nil {
			u.compareElsewhere(&offer, path, baseDir, other, target, trace)
			return offer, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if trace.Stopped() {
			break
		}
	}
	return VowOffer{}, firstErr
}

// valueAfter runs the program up to one top-level statement and returns the
// value there.
func (u *unit) valueAfter(path, baseDir string, target int, trace *ir.Interrupter) (ir.Value, *ir.Type, error) {
	stmts := u.prog.Statements
	starts := u.topLevelStarts()

	// The program up to the target: every later pipeline statement removed,
	// every definition kept wherever it was written.
	var cut []edit
	for i := target + 1; i < len(stmts); i++ {
		cut = append(cut, edit{start: stmts[i].Pos.Offset, end: nextStart(starts, stmts[i].Pos.Offset)})
	}
	head, ok := applyEdits(u.src, cut)
	if !ok {
		return nil, nil, fmt.Errorf("could not isolate the parse")
	}
	hu, err := load(path, head)
	if err != nil || len(hu.pipe.Nodes) == 0 {
		return nil, nil, fmt.Errorf("could not isolate the parse")
	}
	val, err := interp.RunScoped(hu.pipe, &ir.Context{
		Stdout: io.Discard, Stderr: io.Discard, BaseDir: baseDir, Trace: trace,
	})
	if err != nil {
		if trace.Stopped() {
			return nil, nil, fmt.Errorf("the program took too long to reach line %d", stmts[target].Pos.Line)
		}
		return nil, nil, fmt.Errorf("could not run as far as line %d: %s", stmts[target].Pos.Line, firstLine(err.Error()))
	}
	return val, hu.pipe.Nodes[len(hu.pipe.Nodes)-1].Out, nil
}

// offerFor keeps the vows that can actually be written after a statement:
// not already there, and accepted by the language.
func (u *unit) offerFor(path string, target, afterLine int, vows []Vow) VowOffer {
	stmts := u.prog.Statements
	offer := VowOffer{
		AfterLine: afterLine,
		Stage:     strings.TrimSpace(u.src[stmts[target].Pos.Offset:lineEnd(u.src, stmts[target].Pos.Offset)]),
	}
	at := lineOffset(u.src, offer.AfterLine+1)
	following := u.src[at:]
	for _, v := range vows {
		text := strings.Join(v.Lines, "\n") + "\n"
		// One already written here is not a suggestion.
		if strings.HasPrefix(strings.TrimLeft(following, "\n"), text) {
			continue
		}
		// And one the language refuses is not one either: a vow is only
		// offered if the program still checks with it in place.
		with := u.src[:at]
		if !strings.HasSuffix(with, "\n") && with != "" {
			with += "\n"
		}
		if _, err := load(path, with+text+following); err != nil {
			continue
		}
		offer.Vows = append(offer.Vows, v)
	}
	return offer
}

// vowsAfter runs the program up to one top-level statement and reports what
// holds of its value.
func (u *unit) vowsAfter(path, baseDir string, target, afterLine int, trace *ir.Interrupter) (VowOffer, error) {
	val, typ, err := u.valueAfter(path, baseDir, target, trace)
	if err != nil {
		return VowOffer{}, err
	}
	offer := u.offerFor(path, target, afterLine, SuggestVows(val, typ))
	if len(offer.Vows) == 0 {
		return offer, fmt.Errorf("nothing to vow about a %s", typ)
	}
	return offer, nil
}

// withInput is the program reading a different file.
func (u *unit) withInput(other string) (string, bool) {
	if len(u.prog.Statements) == 0 {
		return "", false
	}
	s := u.prog.Statements[0]
	if s.Keyword != "Cursed Energy" || s.Op == nil {
		return "", false
	}
	raw := strings.TrimSpace(s.Op.Raw)
	from := s.Op.Pos.Offset + len(s.Op.Raw) - len(strings.TrimLeft(s.Op.Raw, " "))
	if raw == "" || from+len(raw) > len(u.src) || u.src[from:from+len(raw)] != raw || raw == other {
		return "", false
	}
	return u.src[:from] + other + u.src[from+len(raw):], true
}

// compareElsewhere tries an offer's vows against a second input and marks
// each with the result. A vow that holds on the example and on the real input
// is a fact about the puzzle; one that holds on only one of them is a fact
// about a file, and is exactly the kind that breaks the next run.
func (u *unit) compareElsewhere(offer *VowOffer, path, baseDir, other string, target int, trace *ir.Interrupter) {
	if other == "" {
		return
	}
	src2, ok := u.withInput(other)
	if !ok {
		return
	}
	u2, err := load(path, src2)
	if err != nil || len(u2.prog.Statements) != len(u.prog.Statements) {
		return
	}
	val, typ, err := u2.valueAfter(path, baseDir, target, trace)
	if err != nil {
		return
	}
	there := map[string]bool{}
	for _, v := range SuggestVows(val, typ) {
		there[strings.Join(v.Lines, "\n")] = true
	}
	offer.Other = other
	for i := range offer.Vows {
		v := &offer.Vows[i]
		if there[strings.Join(v.Lines, "\n")] {
			v.Portable, v.Elsewhere = true, "holds on "+other+" too"
		} else {
			v.Elsewhere = "does not hold on " + other
		}
	}
	sort.SliceStable(offer.Vows, func(i, j int) bool { return offer.Vows[i].Portable && !offer.Vows[j].Portable })
}

// BrokenVows proposes vows that v *fails*: the things a sound value of its
// type would satisfy and this one does not. They are what to write above a
// stage that has just died, so that the next bad input is reported where it
// went wrong rather than where it finally mattered.
func BrokenVows(v ir.Value, t *ir.Type) []Vow {
	if t == nil {
		return nil
	}
	var out []Vow
	holds := func(lambda, why string) {
		out = append(out, Vow{Lines: []string{"Binding Vow: Holds", "    Using: " + lambda}, Why: why})
	}
	switch t.Kind {
	case ir.KText:
		s, ok := v.(string)
		if !ok {
			return nil
		}
		if strings.TrimSpace(s) == "" {
			holds("(t) -> length(t) > 0", "the text is empty")
		}
		if strings.Contains(s, "\r") {
			holds(`(t) -> ikke contains(t, "\r")`, "the text has Windows line endings, so every line ends in a stray \\r")
		}
	case ir.KList:
		xs, ok := v.([]ir.Value)
		if !ok {
			return nil
		}
		if len(xs) == 0 {
			holds("(xs) -> length(xs) > 0", "the list is empty")
			return out
		}
		if t.Elem != nil && t.Elem.Kind == ir.KText {
			// Each vow offered here must be *false* of this value, or it
			// would be written, pass, and catch nothing. An empty item can be
			// tested wherever it is; an item of only spaces can be tested
			// only where an expression can reach it, which is the end.
			n := len(xs)
			for i, x := range xs {
				if s, _ := x.(string); s == "" {
					where := fmt.Sprintf("item %d of %d is empty", i+1, n)
					if i == n-1 {
						where = "the last item is empty — a blank line at the end of the input"
					}
					holds(`(xs) -> ikke contains(xs, "")`, where)
					break
				}
			}
			if s, _ := xs[n-1].(string); s != "" && strings.TrimSpace(s) == "" {
				holds(`(xs) -> length(trim(last(xs))) > 0`, "the last item is only whitespace")
			}
		}
		if t.Elem != nil && t.Elem.Kind == ir.KList {
			first, _ := xs[0].([]ir.Value)
			last, _ := xs[len(xs)-1].([]ir.Value)
			if len(first) != len(last) {
				holds("(xs) -> length(first(xs)) = length(last(xs))",
					fmt.Sprintf("the rows are ragged: the first has %d items and the last has %d", len(first), len(last)))
			}
			if len(last) == 0 {
				holds("(xs) -> length(last(xs)) > 0", "the last row is empty — a blank line at the end of the input")
			}
		}
	case ir.KMap, ir.KSet:
		if n, ok := valueSize(v); ok && n == 0 {
			holds("(m) -> length(m) > 0", "it is empty")
		}
	}
	return out
}

func valueSize(v ir.Value) (int, bool) {
	switch x := v.(type) {
	case *ir.MapValue:
		return x.Len(), true
	case *ir.SetValue:
		return x.Len(), true
	}
	return 0, false
}

// SuggestVowsBefore is the offer made after a run that failed on failLine:
// the program is run as far as the value that *entered* the failing
// statement, and what is unsound about that value is offered as vows to put
// in front of it.
func SuggestVowsBefore(path, src string, failLine int, baseDir string, stop func(abort func())) (VowOffer, error) {
	u, err := load(path, src)
	if err != nil {
		return VowOffer{}, fmt.Errorf("the program does not check yet: %s", firstLine(err.Error()))
	}
	starts := u.topLevelStarts()
	stmts := u.prog.Statements
	lastLineOf := func(i int) int {
		first, last := u.tokRange(stmts[i].Pos.Offset, nextStart(starts, stmts[i].Pos.Offset))
		return u.lastLine(first, last)
	}
	failing := -1
	for i := range stmts {
		if failLine >= stmts[i].Pos.Line && failLine <= lastLineOf(i) {
			failing = i
		}
	}
	if failing < 0 {
		return VowOffer{}, fmt.Errorf("line %d is not in the pipeline", failLine)
	}
	// The value that entered it is what the nearest earlier stage produced;
	// Parts, Channels, vows and declarations pass the value through.
	target := -1
	for i := failing - 1; i >= 0; i-- {
		switch stmts[i].Keyword {
		case "Part", "Channel", "Binding Vow", "Reveal", "Cursed Object", "Cursed Tool":
			continue
		}
		target = i
		break
	}
	if target < 0 {
		return VowOffer{}, fmt.Errorf("line %d is where the input is read — there is nothing above it to vow about", failLine)
	}

	trace := ir.NewInterrupter(nil)
	if stop != nil {
		stop(trace.Stop)
	}
	val, typ, err := u.valueAfter(path, baseDir, target, trace)
	if err != nil {
		return VowOffer{}, err
	}
	offer := u.offerFor(path, target, lastLineOf(target), BrokenVows(val, typ))
	offer.Broken = true
	if len(offer.Vows) == 0 {
		return offer, fmt.Errorf("the %s entering line %d looks sound — nothing a vow would have caught", typ, stmts[failing].Pos.Line)
	}
	return offer, nil
}

// GlobalWrites is the lines that write the global name after it is declared:
// its `Cursed Tool` statements and every `name := …`.
func GlobalWrites(src, name string) []int {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil
	}
	seen := map[int]bool{}
	var out []int
	add := func(line int) {
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	for i, t := range toks {
		if t.Kind == token.IDENT && t.Literal == name && toks[i+1].Kind == token.ASSIGN {
			add(t.Pos.Line)
		}
	}
	if prog, err := parser.Parse(src, toks); err == nil {
		u := &unit{src: src, toks: toks, prog: prog}
		u.allStatements(func(s *ast.Statement) {
			if s.Keyword != "Cursed Tool" {
				return
			}
			for _, d := range s.Decls {
				if d.Name == name {
					add(d.Pos.Line)
				}
			}
		})
	}
	sort.Ints(out)
	return out
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}
