// Source-level optimization rewrites behind `domain expansion: optimize`.
//
// The 26-pass IR optimizer stays the engine of record — it runs on every
// compile and interpretation. But some of its rewrites have an exact source
// spelling, and a few hygiene cleanups (dead statements, unused Channels) are
// only expressible at the source level. This file applies that subset as
// textual rewrites, one at a time, re-parsing and re-resolving after each so
// a rewrite that would break the program is rolled back instead of applied.
package diag

import (
	"regexp"
	"strings"

	"domain/lexer"
	"domain/parser"
	"domain/prims"

	"domain/ast"
)

// SourceRewrite records one applied source-level optimization.
type SourceRewrite struct {
	Line int
	Desc string
}

// maxSourceRewrites bounds the rewrite loop; each round applies exactly one
// rewrite, so cascades (Sort+Reverse exposing a double Sort) still converge.
const maxSourceRewrites = 25

// OptimizeSource repeatedly applies the first available source-level rewrite
// until none remain. The input must already resolve cleanly; a rewrite whose
// result stops resolving is rolled back and ends the loop.
func OptimizeSource(src string) (string, []SourceRewrite) {
	var all []SourceRewrite
	for round := 0; round < maxSourceRewrites; round++ {
		prog := parseClean(src)
		if prog == nil {
			break
		}
		op := findRewrite(prog, src)
		if op == nil {
			break
		}
		next := op.apply(src)
		if parseClean(next) == nil {
			break // the rewrite broke the program; keep the last good source
		}
		src = next
		all = append(all, op.info)
	}
	return src, all
}

// parseClean returns the parsed program when src lexes, parses, and resolves;
// nil otherwise.
func parseClean(src string) *ast.Program {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return nil
	}
	if _, err := prims.Resolve(prog); err != nil {
		return nil
	}
	return prog
}

// rewriteOp is one concrete rewrite: lines to delete and lines to replace.
type rewriteOp struct {
	info        SourceRewrite
	deleteLines map[int]bool
	replaceLine map[int]string
}

func (op *rewriteOp) apply(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		n := i + 1
		if op.deleteLines[n] {
			continue
		}
		if r, ok := op.replaceLine[n]; ok {
			l = r
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// findRewrite scans the program for the first applicable rewrite, in priority
// order: redundant work first (double sort), then fusions (Sort+Reverse),
// then dead code, then unused Channels.
func findRewrite(prog *ast.Program, src string) *rewriteOp {
	var found *rewriteOp
	forEachSequence(prog, func(stmts []*ast.Statement) {
		if found != nil {
			return
		}
		found = rewriteInSequence(stmts, src)
	})
	if found != nil {
		return found
	}
	if op := deadCodeRewrite(prog, src); op != nil {
		return op
	}
	return unusedChannelRewrite(prog, src)
}

func rewriteInSequence(stmts []*ast.Statement, src string) *rewriteOp {
	for i, s := range stmts {
		desc, isSort := sortDirection(s)
		if !isSort || i+1 >= len(stmts) {
			continue
		}
		next := stmts[i+1]

		if _, second := sortDirection(next); second {
			start, end := stmtExtent(s)
			return &rewriteOp{
				info:        SourceRewrite{Line: s.Pos.Line, Desc: "removed a redundant Sort (the following Sort's ordering wins)"},
				deleteLines: lineRange(start, end),
			}
		}

		if next.Keyword == "Reverse Cursed Technique" {
			line := lineAt(src, s.Pos.Line)
			flipped, ok := flipSortLine(line, desc)
			if !ok {
				continue
			}
			start, end := stmtExtent(next)
			dir := "Descending"
			if desc {
				dir = "Ascending"
			}
			return &rewriteOp{
				info: SourceRewrite{Line: s.Pos.Line,
					Desc: "fused Sort + Reverse into one " + dir + " sort"},
				replaceLine: map[int]string{s.Pos.Line: flipped},
				deleteLines: lineRange(start, end),
			}
		}
	}
	return nil
}

var reDescendingMod = regexp.MustCompile(`,\s*[Dd]escending`)

// flipSortLine rewrites a sort statement line to the opposite direction.
func flipSortLine(line string, desc bool) (string, bool) {
	if desc {
		if !reDescendingMod.MatchString(line) {
			return "", false
		}
		return reDescendingMod.ReplaceAllString(line, ""), true
	}
	return strings.TrimRight(line, " ") + ", Descending", true
}

// deadCodeRewrite deletes top-level statements after the last Reveal whose
// results are never observed.
func deadCodeRewrite(prog *ast.Program, src string) *rewriteOp {
	lastReveal := -1
	for i, s := range prog.Statements {
		if s.Keyword == "Reveal" {
			lastReveal = i
		}
	}
	if lastReveal == -1 {
		return nil
	}
	for _, s := range prog.Statements[lastReveal+1:] {
		if s.Keyword == "Reveal" || s.Keyword == "Binding Vow" {
			continue
		}
		start, end := stmtExtent(s)
		return &rewriteOp{
			info:        SourceRewrite{Line: s.Pos.Line, Desc: "removed dead code after the final Reveal"},
			deleteLines: lineRange(start, end),
		}
	}
	return nil
}

// unusedChannelRewrite deletes a Channel definition nothing consumes.
func unusedChannelRewrite(prog *ast.Program, src string) *rewriteOp {
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
			start, end := stmtExtent(s)
			return &rewriteOp{
				info: SourceRewrite{Line: s.Pos.Line,
					Desc: "removed unused Channel \"" + s.ChannelName + "\""},
				deleteLines: lineRange(start, end),
			}
		}
	}
	return nil
}

// stmtExtent computes the inclusive line range a statement occupies,
// including its indented arguments and nested block.
func stmtExtent(s *ast.Statement) (int, int) {
	start, end := s.Pos.Line, s.Pos.Line
	var visit func(s *ast.Statement)
	visit = func(s *ast.Statement) {
		if s.Pos.Line > end {
			end = s.Pos.Line
		}
		for _, a := range s.Args {
			if a.Pos.Line > end {
				end = a.Pos.Line
			}
		}
		for _, c := range s.Block {
			visit(c)
		}
	}
	visit(s)
	return start, end
}

func lineRange(start, end int) map[int]bool {
	m := map[int]bool{}
	for l := start; l <= end; l++ {
		m[l] = true
	}
	return m
}
