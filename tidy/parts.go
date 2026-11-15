// Sharing work above the Parts.
//
// A Part runs on the value the pipeline had when it was reached, so anything
// two Parts both do first belongs above them, where it runs once:
//
//	Part "1":                         Lines
//	    Lines                         Convert To Integers
//	    Convert To Integers
//	    Sum                     →     Part "1":
//	    Reveal: stdout                    Sum
//	Part "2":                             Reveal: stdout
//	    Lines                         Part "2":
//	    Convert To Integers               Max
//	    Max                               Reveal: stdout
//	    Reveal: stdout
//
// What moves is the longest run of leading statements that every Part spells
// identically, token for token. It stops at the first statement that differs,
// and before anything that prints — a `Reveal` inside a Part is labelled with
// the Part, and above the Parts it would not be.
//
// Two Parts that agree except for one expression in the middle are not
// handled. Sharing those means naming the difference — a Shikigami taking the
// expression as a parameter — and both the name and the parameter's type are
// the author's to choose.
package tidy

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/prims"
	"domain/token"
)

// chanceWords are the builtins that draw from the seeded random stream. A
// statement that draws once above the Parts and one that draws once in each
// leave the stream in different places, so those stay where they are.
var chanceWords = map[string]bool{
	"random": true, "randomf": true, "pick": true,
	"pending": true, "elapsed": true, "frame": true,
}

func passHoist(u *unit) (edits []edit, summary string, notes []string) {
	stmts := u.prog.Statements
	firstPart := -1
	for i, s := range stmts {
		if s.Keyword == "Part" {
			firstPart = i
			break
		}
	}
	if firstPart < 0 {
		return nil, "", nil
	}
	parts := stmts[firstPart:]
	if len(parts) < 2 {
		return nil, "", nil
	}
	// Hoisted statements change the value everything after them starts from.
	// That is the point for a Part, and a change of meaning for anything else.
	for _, p := range parts {
		if p.Keyword != "Part" || len(p.Block) == 0 {
			return nil, "", nil
		}
	}

	starts := u.topLevelStarts()
	type body struct {
		part   *ast.Statement
		prints []string
		lasts  []int // last line of each statement
	}
	bodies := make([]body, len(parts))
	shortest := -1
	for i, p := range parts {
		end := nextStart(starts, p.Pos.Offset)
		b := body{part: p}
		for j, s := range p.Block {
			limit := end
			if j+1 < len(p.Block) {
				limit = p.Block[j+1].Pos.Offset
			}
			first, last := u.tokRange(s.Pos.Offset, limit)
			b.prints = append(b.prints, u.spell(first, last))
			b.lasts = append(b.lasts, u.lastLine(first, last))
		}
		bodies[i] = b
		if shortest < 0 || len(p.Block) < shortest {
			shortest = len(p.Block)
		}
	}

	// Every Part keeps at least one statement of its own.
	k := 0
	for k < shortest-1 {
		s := parts[0].Block[k]
		if !u.hoistable(s, bodies[0].prints[k]) {
			break
		}
		same := true
		for _, b := range bodies[1:] {
			if b.prints[k] != bodies[0].prints[k] {
				same = false
			}
		}
		if !same {
			break
		}
		k++
	}
	if k == 0 {
		return nil, "", nil
	}
	// The first Part's copy moves, comments and all. The other Parts' copies
	// are deleted, so a comment among those lines would go with them — and a
	// rewrite that costs someone a comment is not one to make uninvited.
	all := strings.Split(u.src, "\n")
	for _, b := range bodies[1:] {
		for ln := b.part.Pos.Line + 1; ln <= b.lasts[k-1] && ln <= len(all); ln++ {
			if token.CommentStart(all[ln-1]) >= 0 {
				return nil, "", []string{fmt.Sprintf(
					"the Parts share their first %s, but line %d carries a comment that moving them would lose",
					plural(k, "statement", "statements"), ln)}
			}
		}
	}

	lines := strings.Split(u.src, "\n")
	// The shared statements, taken from the first Part with whatever comments
	// sit among them, and brought out to the Part's own indentation.
	head := parts[0].Pos.Line
	from, to := head+1, bodies[0].lasts[k-1]
	shift := indentOf(lines[parts[0].Block[0].Pos.Line-1]) - indentOf(lines[head-1])
	var moved []string
	for _, l := range lines[from-1 : to] {
		n := min(shift, indentOf(l))
		moved = append(moved, l[n:])
	}
	for len(moved) > 0 && strings.TrimSpace(moved[0]) == "" {
		moved = moved[1:]
	}

	// They go above the Part — and above the comment that introduces it, which
	// is about the Part and should stay touching it.
	at := head
	for at > 1 && token.IsCommentLine(lines[at-2]) && indentOf(lines[at-2]) == indentOf(lines[head-1]) {
		at--
	}
	edits = append(edits, edit{start: lineOffset(u.src, at), end: lineOffset(u.src, at),
		text: strings.Join(moved, "\n") + "\n\n"})
	for _, b := range bodies {
		edits = append(edits, deleteLines(u.src, b.part.Pos.Line+1, b.lasts[k-1]))
	}
	return edits, fmt.Sprintf("moved %s above the Parts", plural(k, "shared statement", "shared statements")), nil
}

// spell is a statement's tokens, which is what "the same statement" means:
// two that differ only in spacing or comments are the same, and two that
// differ in anything the compiler reads are not.
func (u *unit) spell(first, last int) string {
	var b strings.Builder
	depth := 0
	for _, t := range u.toks[first:last] {
		switch t.Kind {
		case token.INDENT:
			depth++
		case token.DEDENT:
			depth--
		}
		fmt.Fprintf(&b, "%d:%d:%q ", depth, t.Kind, t.Literal)
	}
	return b.String()
}

// hoistable reports whether running a statement once above the Parts is the
// same as running it at the top of each.
func (u *unit) hoistable(s *ast.Statement, spelling string) bool {
	ok := true
	var walk func(s *ast.Statement)
	walk = func(s *ast.Statement) {
		switch s.Keyword {
		case "Reveal", "Part", "Channel":
			ok = false
		}
		if s.Foreign != nil {
			ok = false // another language's code may do anything, including print
		}
		for _, c := range s.Block {
			walk(c)
		}
		for _, b := range append(append([]*ast.Binding(nil), s.Binds...), s.Decls...) {
			for _, c := range b.Body {
				walk(c)
			}
		}
	}
	walk(s)
	if !ok {
		return false
	}
	// A call is only as repeatable as what it calls. The prelude is known
	// not to draw from the random stream; a Shikigami in this file can be
	// read; one from an imported library cannot, so it stays where it is.
	if !u.callsAreRepeatable(s) {
		return false
	}
	for w := range chanceWords {
		if strings.Contains(spelling, fmt.Sprintf("%q", w)) {
			return false
		}
	}
	return true
}

// callsAreRepeatable reports whether every Shikigami a statement calls can be
// run once in place of once per Part.
func (u *unit) callsAreRepeatable(s *ast.Statement) bool {
	prelude := map[string]bool{}
	for _, n := range prims.PreludeNames() {
		prelude[strings.ToLower(n)] = true
	}
	local := map[string]bool{}
	for _, d := range u.prog.Shikigamis {
		local[strings.ToLower(d.Name)] = true
	}
	// One definition that draws is enough to distrust every local call: a
	// definition may call another, and following that chain buys nothing a
	// blanket refusal does not.
	localDraws := false
	starts := u.topLevelStarts()
	for _, d := range u.prog.Shikigamis {
		f, l := u.tokRange(d.Pos.Offset, nextStart(starts, d.Pos.Offset))
		for _, t := range u.toks[f:l] {
			if t.Kind == token.IDENT && chanceWords[t.Literal] {
				localDraws = true
			}
		}
	}
	ok := true
	var walk func(s *ast.Statement)
	walk = func(s *ast.Statement) {
		if s.Keyword == "Shikigami" && s.Op != nil {
			name := strings.ToLower(strings.TrimSpace(s.Op.Raw))
			switch {
			case local[name]:
				if localDraws {
					ok = false
				}
			case prelude[name]:
			default:
				ok = false
			}
		}
		for _, c := range s.Block {
			walk(c)
		}
		for _, b := range append(append([]*ast.Binding(nil), s.Binds...), s.Decls...) {
			for _, c := range b.Body {
				walk(c)
			}
		}
	}
	walk(s)
	return ok
}
