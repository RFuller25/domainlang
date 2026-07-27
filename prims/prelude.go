package prims

import (
	"fmt"
	"sync"

	"domain/ast"
	"domain/lexer"
	"domain/parser"
)

// preludeSource is the standard library, defined in Domain itself. These are
// ordinary Shikigami — named compositions of primitives — demonstrating that
// the language extends without new built-in magic. They are loaded before every
// program.
// Each definition declares its signature. All five are monomorphic, so this
// costs nothing in generality and is the feature's dogfood case: the same check
// a user's Shikigami gets applies to the standard library, and hover shows a
// declared type rather than a reconstructed one.
const preludeSource = `
Shikigami "Lines" : Text -> List<Text>
    Cursed Technique: Split Text by "\n"

Shikigami "Blocks" : Text -> List<List<Text>>
    Cursed Technique: Split Text by "\n\n"
    Cursed Technique: Split Each by "\n"

Shikigami "Ints" : Text -> List<Int>
    Cursed Technique: Split Text by "\n"
    Channeled Energy: Convert List to Integers

Shikigami "Digit Grid" : Text -> Grid<Int>
    Cursed Technique: Split Text by "\n"
    Cursed Technique: Split Each by ""
    Channeled Energy: Convert Each List to Integers
    Channeled Energy: Convert To Grid

Shikigami "Top K Sum" (k: Int) : List<Int> -> Int
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top k, Sum
`

var (
	preludeOnce  sync.Once
	preludeCache []*ast.ShikigamiDef
	preludeErr   error
)

// PreludeNames returns the names of the prelude's Shikigami definitions, for
// diagnostics ("did you mean" suggestions against callable names).
func PreludeNames() []string {
	defs, err := preludeDefs()
	if err != nil {
		return nil
	}
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

// preludeDefs parses the prelude once and returns its Shikigami definitions.
func preludeDefs() ([]*ast.ShikigamiDef, error) {
	preludeOnce.Do(func() {
		toks, err := lexer.Lex(preludeSource)
		if err != nil {
			preludeErr = fmt.Errorf("prelude: %w", err)
			return
		}
		prog, err := parser.Parse(preludeSource, toks)
		if err != nil {
			preludeErr = fmt.Errorf("prelude: %w", err)
			return
		}
		preludeCache = prog.Shikigamis
	})
	return preludeCache, preludeErr
}
