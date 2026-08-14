package parser

import (
	"testing"

	"domain/lexer"
)

// FuzzParse asserts the parser never panics on any token stream the lexer
// can produce from arbitrary input — it must either return a program or a
// well-formed *Error.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"Reveal: stdout\n",
		"Cursed Energy: input.txt\nDomain Expansion: Quicksort, Descending\n",
		"Domain Expansion: All Pairs\n    Using: (a, b) -> a + b = 2020\n",
		"Shikigami \"X\" (k: Int)\n    Reveal: stdout\n",
		"Channel \"moves\":\n    Cursed Technique: Take Item 1\n",
		"Cursed Energy input.txt\n", // missing colon
		"Binding Vow: All Values > -5\n",
		"Domain Expansion: Python\n    print(1)\nReveal: stdout\n",
		"Domain Expansion: Python\n", // an opener with no block
		"Shikigami \"X\"\n    Python\n        x\n",
		// Record literals: a well-formed one, and the malformed shapes whose
		// error branches are new (empty, reserved non-identifier key, unclosed).
		"X:\n    Using: (v) -> {a: 1, b: v}\n",
		"X:\n    Using: (v) -> {}\n",
		"X:\n    Using: (v) -> {1: 2}\n",
		"X:\n    Using: (v) -> {a: 1\n",
		"X:\n    Using: (v) -> {a: {b: {c: 1}}}\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks, err := lexer.Lex(src)
		if err != nil {
			return // not our concern here; the lexer's own fuzz target covers it
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Parse panicked on %q: %v", src, r)
			}
		}()
		_, _ = Parse(src, toks)
	})
}
