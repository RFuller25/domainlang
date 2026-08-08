package lsp

import (
	"encoding/json"

	"domain/ast"
	"domain/ir"
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

// inlayHints answers textDocument/inlayHint. The hints themselves come from
// the shared analysis (api.go); this places them at the end of their lines and
// says what they mean.
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

	// Ranges are 0-based and half-open; a zero range means "the whole file".
	inRange := func(line int) bool {
		if p.Range.End.Line == 0 && p.Range.Start.Line == 0 {
			return true
		}
		return line-1 >= p.Range.Start.Line && line-1 <= p.Range.End.Line
	}

	hints := []any{}
	for _, h := range doc.analyze().TypeHints() {
		if !inRange(h.Line) {
			continue
		}
		tooltip := "the value type flowing out of this statement"
		if h.Binding {
			tooltip = "the type of the value this binding holds"
		}
		hints = append(hints, map[string]any{
			"position":    map[string]int{"line": h.Line - 1, "character": lineLength(doc.text, h.Line)},
			"label":       h.Label,
			"kind":        1, // Type
			"paddingLeft": true,
			"tooltip":     tooltip,
		})
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
