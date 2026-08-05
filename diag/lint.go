// The linter: style/hygiene warnings and performance hints over a parsed
// program. Lint never blocks execution — everything here is Warning or Hint.
package diag

import (
	"fmt"
	"slices"
	"strings"

	"domain/ast"
	"domain/prims"
	"domain/typecheck"
)

// Lint inspects a parsed program for hygiene problems (unused definitions,
// unreachable statements, missing output) and performance smells the
// optimizer cannot silently repair at the source level.
func Lint(prog *ast.Program, src string) []Diagnostic {
	var ds []Diagnostic
	add := func(d Diagnostic) {
		d.LineText = lineAt(src, d.Pos.Line)
		ds = append(ds, d)
	}

	lintChannels(prog, add)
	lintShikigami(prog, add)
	lintReveal(prog, add)
	lintParts(prog, add)
	lintImports(prog, add)
	forEachSequence(prog, func(stmts []*ast.Statement) {
		lintPatterns(stmts, add)
		lintPhraseExpressions(stmts, add)
	})
	return ds
}

// lintResolved is the half of the linter that needs a *resolved* program: it
// reads back what the resolver recorded on the tree. Analyze calls it only
// when resolution succeeded, since a statement that never resolved never had
// the chance to read its arguments.
func lintResolved(prog *ast.Program, src string) []Diagnostic {
	var ds []Diagnostic
	add := func(d Diagnostic) {
		d.LineText = lineAt(src, d.Pos.Line)
		ds = append(ds, d)
	}
	lintUnusedArgs(prog, add)
	lintBindings(prog, add)
	return ds
}

// lintBindings warns about a `Consider` binding nothing reads. An `As`
// binding that nothing names is dead weight; an `Of` one computes a value on
// every pass through its stage and throws it away.
//
// Like the unused-argument lint this asks the resolver what happened
// (ast.Binding.Used, set when an expression actually resolves the name)
// rather than keeping a second opinion about what counts as a read. That also
// catches the near miss for free: a binding shadowed by a lambda parameter of
// the same name everywhere it could have been read was never resolved, so it
// arrives here as exactly what it is — a binding nothing reads.
//
// Shikigami definition bodies are skipped for the same reason arguments are:
// their statements resolve as substituted copies, and the originals are
// marked wholesale at substitution.
func lintBindings(prog *ast.Program, add func(Diagnostic)) {
	check := func(binds []*ast.Binding, where string) {
		for _, b := range binds {
			if b.Used {
				continue
			}
			d := Diagnostic{
				Severity: Warning, Code: "style", Pos: b.Pos,
				Msg: fmt.Sprintf("nothing reads the binding %q", b.Name),
				Help: fmt.Sprintf("delete the `Consider %s …` line — no expression in %s names it, so it has no effect",
					b.Name, where),
			}
			if b.Of {
				d.Help = fmt.Sprintf(
					"delete the `Consider %s Of …` line — no expression in %s names it, so its value is computed and thrown away",
					b.Name, where)
			}
			add(d)
		}
	}
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			check(s.Binds, "this stage")
			walk(s.Block)
			for _, b := range s.Binds {
				walk(b.Body)
			}
		}
	}
	walk(prog.Statements)
}

// lintUnusedArgs warns about a named argument the primitive on that line never
// read — `Size:` on a primitive that takes no size, or a misspelled `Usng:`.
// Both are silently ignored at runtime, which makes them the language's
// quietest way to write a program that does something other than what it says.
//
// prims.ArgSet marks each argument as it is looked up, so this asks the
// resolver what happened rather than keeping a second table of accepted names.
// Shikigami definition bodies are skipped: their statements are resolved as
// substituted copies, and the originals are marked wholesale at substitution.
func lintUnusedArgs(prog *ast.Program, add func(Diagnostic)) {
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			for _, a := range s.Args {
				if a.Used {
					continue
				}
				d := Diagnostic{
					Severity: Warning, Code: "style", Pos: a.Pos,
					Msg: fmt.Sprintf("%q ignores the argument %q", phraseOf(s), a.Name),
					Help: fmt.Sprintf("delete the `%s:` line — nothing reads it, so it has no effect",
						a.Name),
				}
				if want, ok := suggestArgName(s, a.Name); ok {
					d.Help = fmt.Sprintf("did you mean `%s:`? nothing reads `%s:`, so it has no effect",
						want, a.Name)
				}
				add(d)
			}
			if len(s.Block) > 0 {
				walk(s.Block)
			}
		}
	}
	walk(prog.Statements)
}

// suggestArgName offers the argument the line probably meant: the closest
// argument name in the language, by edit distance, excluding the ones this
// statement already supplies. The candidate set is deliberately the whole
// language rather than one primitive's own — which arguments a primitive reads
// is a property of its Build function, not a declared list, and a misspelling
// is the case this suggestion exists for.
func suggestArgName(s *ast.Statement, got string) (string, bool) {
	var open []string
	for _, c := range prims.ArgNames() {
		if !slices.ContainsFunc(s.Args, func(a *ast.Arg) bool { return a.Name == c }) {
			open = append(open, c)
		}
	}
	best, dist := closest(got, open)
	if best == "" || dist > len(got)/2+1 {
		return "", false
	}
	return best, true
}

// lintPhraseExpressions warns about an expression written into an operation
// phrase, which the phrase scanner silently takes apart: `Window length(xs) /
// 2` parses to the words [Window length xs] and the int 2, and every primitive
// reads only the int — so the line runs as `Window 2`.
//
// The test is deliberately narrow: a phrase word that both names an
// expression-layer builtin *and* is immediately followed by `(` in the source
// text. A channel or loop variable that happens to be called `cells` is not a
// call, and does not fire.
func lintPhraseExpressions(stmts []*ast.Statement, add func(Diagnostic)) {
	for _, s := range stmts {
		if s.Op == nil {
			continue
		}
		for _, w := range s.Op.Words {
			if !isBuiltinCall(s.Op.Raw, w) {
				continue
			}
			add(Diagnostic{
				Severity: Warning, Code: "style", Pos: s.Pos,
				Msg: fmt.Sprintf("%q looks like an expression, but an operation phrase holds literals only", s.Op.Raw),
				Help: "the phrase layer keeps only the literal words and numbers here, " +
					"so the rest of this line is discarded",
				Notes: []string{
					"expressions live in an indented lambda argument: `Using: (xs) -> …`",
				},
			})
			break // one warning per line, however many calls it contains
		}
	}
}

// isBuiltinCall reports whether w names an expression builtin and appears in
// raw as a call, `w(`. Builtin names are lowercase and themed phrase words are
// capitalized, so the comparison is case-sensitive on purpose: the phrase word
// `Sum` is the reduction, and `sum` is the builtin.
func isBuiltinCall(raw, w string) bool {
	return slices.Contains(typecheck.Builtins, w) && strings.Contains(raw, w+"(")
}

// phraseOf names the operation on a statement, for a message that points at
// the line the reader is looking at.
func phraseOf(s *ast.Statement) string {
	if s.Op == nil {
		return s.Keyword
	}
	return strings.TrimSpace(s.Op.Raw)
}

// forEachSequence visits every straight-line statement sequence: the top
// level, each Channel body, and each Shikigami body.
func forEachSequence(prog *ast.Program, visit func([]*ast.Statement)) {
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		visit(stmts)
		for _, s := range stmts {
			if len(s.Block) > 0 {
				walk(s.Block)
			}
		}
	}
	walk(prog.Statements)
	for _, def := range prog.Shikigamis {
		walk(def.Body)
	}
}

// lintChannels warns about Channels that are defined but never consumed.
func lintChannels(prog *ast.Program, add func(Diagnostic)) {
	used := map[string]bool{}
	forEachSequence(prog, func(stmts []*ast.Statement) {
		for _, s := range stmts {
			for _, a := range s.Args {
				if a.Name != "From" {
					continue
				}
				switch v := a.Value.(type) {
				case ast.IdentArg:
					used[v.Value] = true
				case ast.IdentListArg:
					for _, n := range v.Values {
						used[n] = true
					}
				}
			}
		}
	})
	for _, s := range prog.Statements {
		if s.Keyword == "Channel" && s.ChannelName != "" && !used[s.ChannelName] {
			add(Diagnostic{
				Severity: Warning, Code: "style", Pos: s.Pos,
				Msg:  fmt.Sprintf("Channel %q is defined but never consumed", s.ChannelName),
				Help: "consume it with a `From: " + s.ChannelName + "` argument, or delete the Channel",
			})
		}
	}
}

// lintImports warns about an `Innate Domain` library none of whose operations
// are used, and about importing the same library twice.
//
// "Used" is judged by name: a library's definitions are not in the AST, so the
// check asks whether the program summons any Shikigami the file does not define
// itself. That is deliberately conservative — it cannot name *which* import is
// unused when several are present, so it only fires when the program summons no
// foreign Shikigami at all.
func lintImports(prog *ast.Program, add func(Diagnostic)) {
	seen := map[string]bool{}
	for _, imp := range prog.Imports {
		if seen[imp.Target] {
			add(Diagnostic{
				Severity: Warning, Code: "style", Pos: imp.Pos,
				Msg:  fmt.Sprintf("library %q is imported more than once", imp.Target),
				Help: "delete the duplicate `Innate Domain` line",
			})
		}
		seen[imp.Target] = true
	}
	if len(prog.Imports) == 0 {
		return
	}

	local := map[string]bool{}
	for _, def := range prog.Shikigamis {
		local[def.Name] = true
	}
	foreign := false
	forEachSequence(prog, func(stmts []*ast.Statement) {
		for _, s := range stmts {
			if s.Keyword == "Shikigami" && s.Op != nil && !local[strings.TrimSpace(s.Op.Raw)] {
				foreign = true
			}
		}
	})
	if foreign {
		return
	}
	for _, imp := range prog.Imports {
		add(Diagnostic{
			Severity: Warning, Code: "style", Pos: imp.Pos,
			Msg:  fmt.Sprintf("library %q is imported but nothing from it is summoned", imp.Target),
			Help: "call one of its Shikigami, or delete the `Innate Domain` line",
		})
	}
}

// lintShikigami warns about user definitions that are never summoned, are
// defined twice, or shadow a prelude name.
func lintShikigami(prog *ast.Program, add func(Diagnostic)) {
	called := map[string]bool{}
	forEachSequence(prog, func(stmts []*ast.Statement) {
		for _, s := range stmts {
			if s.Keyword == "Shikigami" && s.Op != nil {
				called[strings.TrimSpace(s.Op.Raw)] = true
			}
		}
	})
	prelude := map[string]bool{}
	for _, n := range prims.PreludeNames() {
		prelude[n] = true
	}
	seen := map[string]bool{}
	for _, def := range prog.Shikigamis {
		if seen[def.Name] {
			add(Diagnostic{
				Severity: Warning, Code: "style", Pos: def.Pos,
				Msg:  fmt.Sprintf("Shikigami %q is defined more than once; the later definition wins", def.Name),
				Help: "rename or remove one of the definitions",
			})
		}
		seen[def.Name] = true
		if prelude[def.Name] {
			add(Diagnostic{
				Severity: Warning, Code: "style", Pos: def.Pos,
				Msg:  fmt.Sprintf("Shikigami %q shadows the prelude definition of the same name", def.Name),
				Help: "rename it unless shadowing the prelude is intentional",
			})
		}
		if !called[def.Name] {
			add(Diagnostic{
				Severity: Warning, Code: "style", Pos: def.Pos,
				Msg:  fmt.Sprintf("Shikigami %q is defined but never summoned", def.Name),
				Help: "call it with `Shikigami: " + def.Name + "`, or delete the definition",
			})
		}
	}
}

// lintReveal warns when a program computes but never outputs, and when
// top-level statements sit beyond the last Reveal with no observable effect.
//
// Part blocks make this per-scope. A Part is a passthrough whose body does the
// revealing, so a program whose Parts all reveal is complete even with no
// top-level Reveal, and top-level statements after a Part are not dead.
func lintReveal(prog *ast.Program, add func(Diagnostic)) {
	if len(prog.Statements) == 0 {
		return
	}

	lastReveal, lastPart := -1, -1
	for i, s := range prog.Statements {
		switch s.Keyword {
		case "Reveal":
			lastReveal = i
		case "Part":
			lastPart = i
		}
	}

	// A Part whose body never reveals computes nothing observable — the one
	// hazard of Parts printing only what they explicitly Reveal.
	for _, s := range prog.Statements {
		if s.Keyword != "Part" || revealsSomewhere(s.Block) {
			continue
		}
		add(Diagnostic{
			Severity: Warning, Code: "style", Pos: s.Pos,
			Msg:  fmt.Sprintf("Part %q never reveals anything, so it produces no output", s.PartName),
			Help: "add `Reveal: stdout` at the end of the Part's body",
		})
	}

	if lastReveal == -1 && lastPart == -1 {
		last := prog.Statements[len(prog.Statements)-1]
		add(Diagnostic{
			Severity: Warning, Code: "style", Pos: last.Pos,
			Msg:  "the pipeline's result is never revealed",
			Help: "add `Reveal: stdout` as the final statement to print the result",
		})
		return
	}

	// A Part's own body is its own scope: work after its final Reveal is dead
	// within the Part, because a Part's result is discarded.
	for _, s := range prog.Statements {
		if s.Keyword == "Part" {
			lintDeadAfterReveal(s.Block, add)
		}
	}

	// Only a top-level Reveal makes what follows dead; a Part does not, since
	// the main pipeline value flows past it untouched.
	if lastReveal == -1 {
		return
	}
	lintDeadAfterReveal(prog.Statements, add)
}

// lintDeadAfterReveal warns about statements in one sequence that run after its
// final Reveal. Reveals, vows and Parts are exempt: all three are observable.
func lintDeadAfterReveal(stmts []*ast.Statement, add func(Diagnostic)) {
	last := -1
	for i, s := range stmts {
		if s.Keyword == "Reveal" {
			last = i
		}
	}
	if last == -1 {
		return
	}
	for _, s := range stmts[last+1:] {
		switch s.Keyword {
		case "Reveal", "Binding Vow", "Part":
			continue
		}
		add(Diagnostic{
			Severity: Warning, Code: "dead-code", Pos: s.Pos,
			Msg:  "this statement runs after the last Reveal; its result is never observed",
			Help: "move it before the Reveal, or delete it",
		})
	}
}

// revealsSomewhere reports whether a statement sequence contains a Reveal,
// looking into nested blocks (a Reveal inside a loop body still prints).
func revealsSomewhere(stmts []*ast.Statement) bool {
	for _, s := range stmts {
		if s.Keyword == "Reveal" {
			return true
		}
		if len(s.Block) > 0 && revealsSomewhere(s.Block) {
			return true
		}
	}
	return false
}

// lintParts warns about two Part blocks sharing a label, which makes their
// output indistinguishable.
func lintParts(prog *ast.Program, add func(Diagnostic)) {
	seen := map[string]bool{}
	for _, s := range prog.Statements {
		if s.Keyword != "Part" || s.PartName == "" {
			continue
		}
		if seen[s.PartName] {
			add(Diagnostic{
				Severity: Warning, Code: "style", Pos: s.Pos,
				Msg:  fmt.Sprintf("Part %q is defined more than once", s.PartName),
				Help: "give each Part a distinct label so its output can be told apart",
			})
		}
		seen[s.PartName] = true
	}
}

// lintPatterns scans one statement sequence for adjacent-statement smells.
func lintPatterns(stmts []*ast.Statement, add func(Diagnostic)) {
	for i, s := range stmts {
		desc, isSorted := sortDirection(s)
		if !isSorted || i+1 >= len(stmts) {
			continue
		}
		next := stmts[i+1]

		// Sort followed by Reverse: one sort in the opposite direction.
		if next.Keyword == "Reverse Cursed Technique" {
			flip := "Descending"
			if desc {
				flip = "Ascending"
			}
			add(Diagnostic{
				Severity: Hint, Code: "perf", Pos: s.Pos,
				Msg: "Sort followed by Reverse is one sort in the opposite direction",
				Help: fmt.Sprintf("write `%s%s, %s` and drop the Reverse",
					keywordPrefix(s, "Domain Expansion"), sortName(s), flip),
				Notes: []string{
					"the optimizer already fuses this pair; the hint is about source clarity",
				},
			})
		}

		// Sort followed by another Sort: the first is wasted work.
		if _, second := sortDirection(next); second {
			add(Diagnostic{
				Severity: Warning, Code: "perf", Pos: s.Pos,
				Msg:  "two sorts in a row; the second ordering wins and this sort is wasted work",
				Help: "delete this line",
			})
		}

		// Sort followed by Take Item 0: that is Min (or Max when descending).
		if next.Keyword == "Cursed Technique" && next.Op != nil &&
			phraseStartsWith(next.Op.Words, []string{"Take", "Item"}) &&
			len(next.Op.Ints) == 1 && next.Op.Ints[0] == 0 {
			want := "Min"
			if desc {
				want = "Max"
			}
			add(Diagnostic{
				Severity: Hint, Code: "perf", Pos: s.Pos,
				Msg: "sorting the whole list to take the first item is O(n log n) for an O(n) question",
				Help: fmt.Sprintf("replace both lines with `%s%s`",
					keywordPrefix(s, "Maximum Technique"), want),
			})
		}
	}

	// Filter followed by Count: one fused pass exists.
	for i, s := range stmts {
		if s.Keyword != "Cursed Technique" || s.Op == nil || !phraseStartsWith(s.Op.Words, []string{"Filter"}) {
			continue
		}
		if i+1 < len(stmts) {
			next := stmts[i+1]
			if next.Keyword == "Maximum Technique" && next.Op != nil &&
				len(next.Op.Words) == 1 && strings.EqualFold(next.Op.Words[0], "Count") {
				add(Diagnostic{
					Severity: Hint, Code: "perf", Pos: s.Pos,
					Msg: "Filter followed by Count can be a single pass",
					Help: "write `" + keywordPrefix(next, "Maximum Technique") +
						"Count Matching` with the same Using: lambda",
					Notes: []string{
						"the optimizer fuses this pair automatically; the hint is about source clarity",
					},
				})
			}
		}
	}
}

// keywordPrefix renders the keyword a suggested replacement line should
// carry, in the style of the statement being advised: a program that leaves
// the themed keywords out gets advice that leaves them out too.
func keywordPrefix(s *ast.Statement, keyword string) string {
	if s.KeywordInferred {
		return ""
	}
	return keyword + ": "
}

// sortDirection reports whether a statement is a plain sort (Sort/Quicksort,
// optionally By) and whether it sorts descending.
func sortDirection(s *ast.Statement) (desc, ok bool) {
	if s.Keyword != "Domain Expansion" || s.Op == nil {
		return false, false
	}
	hasSort := false
	for _, w := range s.Op.Words {
		if strings.EqualFold(w, "Sort") || strings.EqualFold(w, "Quicksort") {
			hasSort = true
		}
	}
	if !hasSort {
		return false, false
	}
	for _, m := range s.Op.Modifiers {
		if strings.EqualFold(strings.TrimSpace(m), "Descending") {
			return true, true
		}
	}
	return false, true
}

// sortName recovers the sort word the user wrote (Sort or Quicksort), for
// suggestions that preserve their vocabulary.
func sortName(s *ast.Statement) string {
	for _, w := range s.Op.Words {
		if strings.EqualFold(w, "Quicksort") {
			return "Quicksort"
		}
	}
	return "Sort"
}
