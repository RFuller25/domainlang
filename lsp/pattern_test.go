package lsp

import (
	"strings"
	"testing"
)

// A destructuring lambda parameter declares the names inside the pattern, not
// the synthesized parameter the parser gives it.
func TestSymbolsIndexesPatternNames(t *testing.T) {
	const src = `Cursed Energy: input.txt
Shikigami: Ints
Cursed Technique: Enumerate
Cursed Technique: Map Each
    Using: ((i, x)) -> i + x
Reveal: stdout
`
	a := Analyze("prog.domain", src)
	got := map[string]bool{}
	for _, s := range a.Symbols() {
		if strings.HasPrefix(s.Name, "$") {
			t.Errorf("synthesized name %q leaked into the index", s.Name)
		}
		got[s.Name] = true
	}
	if !got["i"] || !got["x"] {
		t.Errorf("pattern names missing from %v", got)
	}
}
