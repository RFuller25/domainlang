// Folding a block, and indenting into one.
//
// Blocks are the one structure Domain has. A `Channel`, a `Simple Domain`
// loop, a `Part` and a `Shikigami` definition each own an indented
// sub-pipeline, and a program with three channels in it is mostly bodies you
// are not currently reading.
//
// The extent of a block is not guessed from indentation: the parser already
// knows it, and the analysis already has the parse. That matters for the one
// case indentation would get wrong — a blank line inside a body, which looks
// like the end of it and is not.
//
// Auto-indent goes the other way, and is deliberately narrower. Pressing Enter
// after a *structural* block opener indents, because those four keywords always
// take a body: it is a fact about the language rather than a guess. It does not
// try to predict which operations want a `Using:` line, because nothing in the
// vocabulary declares that — a primitive validates its arguments when it builds,
// and there is no table to consult. Guessing there would indent two lines out of
// three and be worse than not trying.
package main

import (
	"strings"

	"domain/ast"
)

// devBlock is one foldable region: the header line and the last line of its
// body, both 1-based.
type devBlock struct {
	Head int
	Last int
}

// devBlocks finds every block in the program, from the parse rather than from
// the indentation.
func devBlocks(prog *ast.Program) map[int]devBlock {
	out := map[int]devBlock{}
	if prog == nil {
		return out
	}

	var visit func(stmts []*ast.Statement)
	visit = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			if len(st.Block) > 0 {
				out[st.Pos.Line] = devBlock{Head: st.Pos.Line, Last: lastLineOf(st)}
			}
			visit(st.Block)
		}
	}
	visit(prog.Statements)
	for _, def := range prog.Shikigamis {
		if len(def.Body) > 0 {
			last := def.Pos.Line
			for _, st := range def.Body {
				last = max(last, lastLineOf(st))
			}
			out[def.Pos.Line] = devBlock{Head: def.Pos.Line, Last: last}
		}
		visit(def.Body)
	}
	return out
}

// lastLineOf is the last source line a statement covers, including everything
// nested under it.
func lastLineOf(st *ast.Statement) int {
	last := st.Pos.Line
	for _, a := range st.Args {
		last = max(last, a.Pos.Line)
	}
	for _, b := range st.Binds {
		last = max(last, b.Pos.Line)
	}
	for _, sub := range st.Block {
		last = max(last, lastLineOf(sub))
	}
	return last
}

// hidden reports whether a line is inside a folded block.
//
// Only the *innermost* fold needs to match for a line to be hidden, so folding
// an outer block hides inner ones without having to fold them too.
func (m devModel) hidden(line int) bool {
	for head, b := range m.blocks {
		if m.folded[head] && line > b.Head && line <= b.Last {
			return true
		}
	}
	return false
}

// toggleFold folds or unfolds the block the cursor is in.
//
// The cursor's own line first, so pressing it on a header folds that block;
// otherwise the innermost block containing the cursor, so pressing it inside a
// body folds the body you are in rather than nothing.
func (m devModel) toggleFold() (devModel, bool) {
	line := m.buf.row + 1
	if _, ok := m.blocks[line]; ok {
		m.setFold(line, !m.folded[line])
		return m, true
	}

	best, found := 0, false
	for head, b := range m.blocks {
		if line > b.Head && line <= b.Last && (!found || head > best) {
			best, found = head, true
		}
	}
	if !found {
		m.status = "no block here to fold"
		return m, false
	}
	m.setFold(best, !m.folded[best])
	// Folding the block you are inside would leave the cursor hidden, so it
	// comes out to the header — which is the line the fold is now standing for.
	if m.folded[best] {
		m.buf.gotoLine(best)
	}
	return m, true
}

func (m *devModel) setFold(head int, on bool) {
	if m.folded == nil {
		m.folded = map[int]bool{}
	}
	if on {
		m.folded[head] = true
		m.status = ""
		return
	}
	delete(m.folded, head)
}

// unfoldAll opens everything, which is the way out of a program folded past
// recognition.
func (m devModel) unfoldAll() devModel {
	m.folded = map[int]bool{}
	m.status = "unfolded"
	return m
}

// foldMarker is what a folded header shows in place of its body.
func (m devModel) foldMarker(line int) string {
	b, ok := m.blocks[line]
	if !ok || !m.folded[line] {
		return ""
	}
	n := b.Last - b.Head
	return styFrame.Render(strings.Repeat(" ", 1)) +
		styDim.Render("⋯ "+plural(n, "line")+" folded")
}

// ---------------------------------------------------------------------------
// auto-indent
// ---------------------------------------------------------------------------

// devBlockOpeners are the keywords that *always* take an indented body. This is
// a fact about the language — see language.md — rather than a heuristic, which
// is why auto-indent is limited to them.
var devBlockOpeners = []string{"Channel", "Simple Domain", "Part", `Shikigami "`}

// opensABlock reports whether a line is a statement whose body must be indented
// under it.
//
// `Shikigami "` is matched with its quote on purpose: `Shikigami "X"` defines
// an operation and takes a body, while `Shikigami: X` calls one and does not.
// One character apart, opposite answers.
func opensABlock(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	for _, kw := range devBlockOpeners {
		if strings.HasPrefix(trimmed, kw) {
			return true
		}
	}
	return false
}

// autoIndentFor is the indentation a new line should start with, given the line
// it was split from: the current indentation, plus one level when that line
// opens a block.
func autoIndentFor(line string) string {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	if opensABlock(line) {
		return indent + devIndent
	}
	return indent
}
