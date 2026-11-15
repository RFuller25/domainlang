// Turning a global that is set once, late, into a constant.
//
//	Cursed Object: n As 0
//	…the parse…
//	Cursed Tool: n Of Count
//
// becomes
//
//	…the parse…
//	Cursed Object: n Of Count
//
// A global that something writes after it is declared costs every stage that
// reads it its optimizer rewrites; one that is only ever declared costs
// nothing. The placeholder-then-assign shape above is a written global by
// spelling only: the first value is never read, so the write can simply *be*
// the declaration.
//
// This is the one written-global shape with a mechanical answer. A counter or
// a cursor — a global a loop writes on every lap — can only be removed by
// carrying its value in the loop's own state, which changes the type flowing
// through every statement of the body and how each of them is written. That
// is a redesign of the loop, and it is left to its author; `domain expansion:
// lint` names the stages each one is costing.
package tidy

import (
	"fmt"
	"sort"
	"strings"

	"domain/ast"
	"domain/token"
)

func passLateGlobals(u *unit) (edits []edit, summary string, notes []string) {
	type decl struct {
		stmt *ast.Statement
		d    *ast.Binding
	}
	decls := map[string][]decl{}
	tools := map[string][]*ast.Statement{}
	nestedUse := map[string]bool{}
	topLevel := map[*ast.Statement]bool{}
	for _, s := range u.prog.Statements {
		topLevel[s] = true
	}

	u.eachStatement(func(s *ast.Statement, def *ast.ShikigamiDef) {
		top := topLevel[s] && def == nil
		switch s.Keyword {
		case "Cursed Object":
			for _, d := range s.Decls {
				decls[d.Name] = append(decls[d.Name], decl{s, d})
				if !top {
					nestedUse[d.Name] = true
				}
			}
		case "Cursed Tool":
			for _, d := range s.Decls {
				tools[d.Name] = append(tools[d.Name], s)
				if !top || len(s.Decls) != 1 {
					nestedUse[d.Name] = true
				}
			}
		}
	})

	// A `:=` anywhere on the name is a write this pass cannot place.
	assigned := map[string]bool{}
	for i, t := range u.toks {
		if t.Kind == token.ASSIGN && i > 0 && u.toks[i-1].Kind == token.IDENT {
			assigned[u.toks[i-1].Literal] = true
		}
	}

	starts := u.topLevelStarts()
	done := 0
	converted := map[string]bool{}
	for name, ds := range decls {
		ts := tools[name]
		if len(ds) != 1 || len(ts) != 1 || nestedUse[name] || assigned[name] {
			continue
		}
		dc, tool := ds[0], ts[0]
		if tool.Pos.Offset <= dc.stmt.Pos.Offset {
			continue
		}

		// The declaration must be one whole line that can go: either the
		// entire single-line statement, or one line of a block of several.
		dLine := dc.d.Pos.Line
		dStart := lineOffset(u.src, dLine)
		dEnd := lineEnd(u.src, dStart)
		oneLiner := len(dc.stmt.Decls) == 1 && dc.stmt.Pos.Line == dLine
		inBlock := len(dc.stmt.Decls) >= 2 && dc.stmt.Pos.Line != dLine
		if !oneLiner && !inBlock {
			continue
		}
		if token.CommentStart(u.src[dStart:dEnd]) >= 0 {
			continue
		}
		// Its value must be a single line too, with no write hiding in it.
		clean := true
		nameToks := 0
		for _, t := range u.toks {
			if t.Pos.Offset < dStart || t.Pos.Offset >= dEnd {
				continue
			}
			if t.Kind == token.ASSIGN {
				clean = false
			}
		}
		first, last := u.tokRange(dc.stmt.Pos.Offset, nextStart(starts, dc.stmt.Pos.Offset))
		if oneLiner && u.lastLine(first, last) != dLine {
			clean = false
		}
		if inBlock {
			// The next declaration, or the statement's end, must start on the
			// very next line: a value continued onto a second line stays put.
			for i, d := range dc.stmt.Decls {
				if d != dc.d {
					continue
				}
				if i+1 < len(dc.stmt.Decls) {
					clean = clean && dc.stmt.Decls[i+1].Pos.Line == dLine+1
				} else {
					clean = clean && u.lastLine(first, last) == dLine
				}
			}
		}
		if !clean {
			continue
		}

		// Nothing may read the placeholder: not between the two lines, not in
		// the new value, and not from a Shikigami, whose body runs wherever it
		// is called from rather than where it is written.
		toolEnd := nextStart(starts, tool.Pos.Offset)
		for _, t := range u.toks {
			if t.Kind != token.IDENT || t.Literal != name {
				continue
			}
			if t.Pos.Offset >= dEnd && t.Pos.Offset < toolEnd {
				nameToks++
			}
		}
		if nameToks != 1 {
			continue
		}
		inDef := false
		for _, def := range u.prog.Shikigamis {
			f, l := u.tokRange(def.Pos.Offset, nextStart(starts, def.Pos.Offset))
			for _, t := range u.toks[f:l] {
				if t.Kind == token.IDENT && t.Literal == name {
					inDef = true
				}
			}
		}
		if inDef {
			continue
		}

		// `Cursed Tool` → `Cursed Object`.
		ti, ok := u.tokAt[tool.Pos.Offset]
		if !ok || ti+1 >= len(u.toks) || u.toks[ti+1].Kind != token.IDENT || !strings.EqualFold(u.toks[ti+1].Literal, "Tool") {
			continue
		}
		edits = append(edits,
			deleteLines(u.src, dLine, dLine),
			edit{start: u.toks[ti+1].Pos.Offset, end: u.toks[ti+1].End, text: "Object"})
		done++
		converted[name] = true
	}

	// What is left is the advice this pass cannot act on.
	var written []string
	for name := range decls {
		if !converted[name] && (len(tools[name]) > 0 || assigned[name] || nestedUse[name]) {
			written = append(written, name)
		}
	}
	if len(written) > 0 {
		sort.Strings(written)
		count := len(written)
		if len(written) > 3 {
			written = append(written[:3], "…")
		}
		notes = append(notes, fmt.Sprintf(
			"%s written after being declared (%s): every stage reading one loses its optimizer rewrites — carry the value in the loop's own state if you can",
			plural(count, "global is", "globals are"), strings.Join(written, ", ")))
	}
	if done == 0 {
		return nil, "", notes
	}
	return edits, "made " + plural(done, "late-set global a constant", "late-set globals constants"), notes
}
