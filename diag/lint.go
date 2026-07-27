// The linter: style/hygiene warnings and performance hints over a parsed
// program. Lint never blocks execution — everything here is Warning or Hint.
package diag

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/prims"
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
	})
	return ds
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
