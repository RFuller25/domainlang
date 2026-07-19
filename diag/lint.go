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
func lintReveal(prog *ast.Program, add func(Diagnostic)) {
	if len(prog.Statements) == 0 {
		return
	}
	lastReveal := -1
	for i, s := range prog.Statements {
		if s.Keyword == "Reveal" {
			lastReveal = i
		}
	}
	if lastReveal == -1 {
		last := prog.Statements[len(prog.Statements)-1]
		add(Diagnostic{
			Severity: Warning, Code: "style", Pos: last.Pos,
			Msg:  "the pipeline's result is never revealed",
			Help: "add `Reveal: stdout` as the final statement to print the result",
		})
		return
	}
	for _, s := range prog.Statements[lastReveal+1:] {
		if s.Keyword == "Reveal" || s.Keyword == "Binding Vow" {
			continue
		}
		add(Diagnostic{
			Severity: Warning, Code: "dead-code", Pos: s.Pos,
			Msg:  "this statement runs after the last Reveal; its result is never observed",
			Help: "move it before the Reveal, or delete it",
		})
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
				Msg:  "Sort followed by Reverse is one sort in the opposite direction",
				Help: fmt.Sprintf("write `Domain Expansion: %s, %s` and drop the Reverse", sortName(s), flip),
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
				Msg:  "sorting the whole list to take the first item is O(n log n) for an O(n) question",
				Help: fmt.Sprintf("replace both lines with `Maximum Technique: %s`", want),
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
					Msg:  "Filter followed by Count can be a single pass",
					Help: "write `Maximum Technique: Count Matching` with the same Using: lambda",
					Notes: []string{
						"the optimizer fuses this pair automatically; the hint is about source clarity",
					},
				})
			}
		}
	}
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
