package lsp

import (
	"encoding/json"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Inlay hints: the type flowing out of each statement, shown at end of line.
//
//	Cursed Energy: input.txt                    : Text
//	Cursed Technique: Split Text by "\n\n"      : List<Text>
//	Maximum Technique: Sum Each Group           : List<Int>
//
// This is the REPL's `=> value : Type` feedback — the most useful thing about
// the REPL — brought into the editor.
//
// Two traps, both about which pipeline the hints come from:
//
//   - They must come from an **unoptimized** resolve. The optimizer replaces,
//     fuses and deletes nodes, so an optimized list cannot be mapped back to
//     source lines. The server never optimizes (it only ever resolves for
//     diagnostics), so reusing that pipeline is both correct and free.
//   - **One statement can produce many nodes.** A Shikigami call inlines its
//     whole body, and those nodes carry positions from the *definition* — which
//     may live in the embedded prelude or an imported library, so they are not
//     coordinates in this file at all. The resolver therefore tags the last node
//     of an inlined group with the call site (Meta["callPos"]), and that is what
//     gives the call's line its type. For ordinary statements the last node at a
//     position wins.

// inlayHints answers textDocument/inlayHint.
func (s *Server) inlayHints(params json.RawMessage) any {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Range struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
			End struct {
				Line int `json:"line"`
			} `json:"end"`
		} `json:"range"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	doc, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return nil
	}
	pipe, prog := doc.resolve()
	if pipe == nil || prog == nil {
		return []any{}
	}

	// The last node at a line wins. A Shikigami call is the exception: its
	// inlined nodes carry positions from the definition's body — possibly in the
	// prelude or a library file — so the resolver tags the group's last node
	// with the call site, and that is what the call's line reports.
	outByLine := map[int]*ir.Type{}
	// A `Consider` binding's line reports the type of the value it binds
	// rather than a statement's output — it is the one line in a block that
	// introduces a name, and its type is the thing a reader cannot infer.
	bindByLine := map[int]*ir.Type{}
	var walk func(nodes []*ir.Node)
	walk = func(nodes []*ir.Node) {
		for _, n := range nodes {
			if n.Out != nil {
				outByLine[n.Pos.Line] = n.Out
			}
			if binds, _ := n.Meta[ir.MetaBinds].([]ir.Binding); binds != nil {
				for _, b := range binds {
					bindByLine[b.Pos().Line] = b.Type()
					walk(b.BlockNodes())
				}
			}
			if pos, ok := n.Meta["callPos"].(token.Position); ok && n.Out != nil {
				outByLine[pos.Line] = n.Out
			}
			// A Channel's own type is its passthrough input, which says nothing.
			// Its body's result is what consumers will see, so recurse — the
			// body's last node lands on the Channel's own line only if they
			// share it, which they never do.
			if sub, _ := n.Meta["nodes"].([]*ir.Node); sub != nil {
				walk(sub)
			}
		}
	}
	walk(pipe.Nodes)

	// Statements decide which lines get a hint at all; the resolved types decide
	// what it says. Going through the AST keeps Part/Vow/Channel special cases
	// in one place.
	hints := []any{}
	inRange := func(line int) bool {
		// Ranges are 0-based and half-open; a zero range means "the whole file".
		if p.Range.End.Line == 0 && p.Range.Start.Line == 0 {
			return true
		}
		return line-1 >= p.Range.Start.Line && line-1 <= p.Range.End.Line
	}

	var visit func(stmts []*ast.Statement)
	visit = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			if label, ok := hintFor(st, outByLine); ok && inRange(st.Pos.Line) {
				hints = append(hints, map[string]any{
					"position":    map[string]int{"line": st.Pos.Line - 1, "character": lineLength(doc.text, st.Pos.Line)},
					"label":       label,
					"kind":        1, // Type
					"paddingLeft": true,
					"tooltip":     "the value type flowing out of this statement",
				})
			}
			for _, b := range st.Binds {
				t, ok := bindByLine[b.Pos.Line]
				if !ok || t == nil || !inRange(b.Pos.Line) {
					continue
				}
				hints = append(hints, map[string]any{
					"position":    map[string]int{"line": b.Pos.Line - 1, "character": lineLength(doc.text, b.Pos.Line)},
					"label":       ": " + t.String(),
					"kind":        1, // Type
					"paddingLeft": true,
					"tooltip":     "the type of the value this binding holds",
				})
			}
			visit(st.Block)
		}
	}
	visit(prog.Statements)
	for _, def := range prog.Shikigamis {
		visit(def.Body)
	}
	return hints
}

// hintFor decides whether a statement gets a hint, and what it says.
func hintFor(st *ast.Statement, outByLine map[int]*ir.Type) (string, bool) {
	switch st.Keyword {
	case "Binding Vow":
		// A vow never changes the value; a hint would just repeat the line above.
		return "", false
	case "Part":
		// A Part is a passthrough whose body does the work. Its own type says
		// nothing, and its body's statements get their own hints.
		return "", false
	case "Channel":
		// The channel's *result* is what a From: consumer will see, which is the
		// useful thing to show — not the passthrough input type.
		if t := channelResultType(st, outByLine); t != nil {
			return ": " + t.String(), true
		}
		return "", false
	}
	t, ok := outByLine[st.Pos.Line]
	if !ok || t == nil {
		return "", false
	}
	return ": " + t.String(), true
}

// channelResultType finds the type a Channel body ends with, by looking at the
// last statement in it that resolved.
func channelResultType(st *ast.Statement, outByLine map[int]*ir.Type) *ir.Type {
	var last *ir.Type
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			if t, ok := outByLine[s.Pos.Line]; ok && t != nil {
				last = t
			}
			walk(s.Block)
		}
	}
	walk(st.Block)
	return last
}

// lineLength returns the length of a 1-based source line, where an end-of-line
// hint is anchored.
func lineLength(text string, line int) int {
	cur, start := 1, 0
	for i := range len(text) {
		if text[i] != '\n' {
			continue
		}
		if cur == line {
			return i - start
		}
		cur++
		start = i + 1
	}
	if cur == line {
		return len(text) - start
	}
	return 0
}
