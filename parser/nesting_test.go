package parser

import (
	"strings"
	"testing"
)

// TestDeepNestingIsRefusedNotCrashed covers the shapes that used to end the
// process rather than the parse. Go kills a goroutine whose stack outgrows its
// limit, and no recover() sees it, so a language server folding a pasted line
// like these simply vanished — which is why the bound lives in the parser,
// ahead of every walk over the tree.
func TestDeepNestingIsRefusedNotCrashed(t *testing.T) {
	deep := maxNestDepth + 50
	cases := []struct{ name, src string }{
		{"parens", "Maximum Technique: Count Matching\n    Using: (x) -> " +
			strings.Repeat("(", deep) + "1" + strings.Repeat(")", deep) + " > 0\n"},
		{"calls", "Maximum Technique: Count Matching\n    Using: (x) -> " +
			strings.Repeat("abs(", deep) + "1" + strings.Repeat(")", deep) + " > 0\n"},
		{"unary", "Maximum Technique: Count Matching\n    Using: (x) -> " +
			strings.Repeat("ikke ", deep) + "true\n"},
		// Left-associative chains: the loop that builds them is flat, but the
		// tree hanging off it is not.
		{"operators", "Maximum Technique: Count Matching\n    Using: (x) -> 1" +
			strings.Repeat(" + 1", deep) + " > 0\n"},
		{"fields", "Maximum Technique: Count Matching\n    Using: (x) -> x" +
			strings.Repeat(".f", deep) + " > 0\n"},
		{"types", "Shikigami \"X\" (k: " + strings.Repeat("List<", deep) + "Int" +
			strings.Repeat(">", deep) + ")\n    Reveal: stdout\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := parseErr(t, c.src)
			if err == nil {
				t.Fatal("expected a nesting error")
			}
			if !strings.Contains(err.Error(), "nests more than") {
				t.Errorf("error = %v, want the nesting limit", err)
			}
		})
	}
}

// TestDeepBlocksAreRefusedNotCrashed is the statement half: blocks nest by
// indentation, and every walk over a program descends through them.
func TestDeepBlocksAreRefusedNotCrashed(t *testing.T) {
	var b strings.Builder
	b.WriteString("Cursed Energy: in.txt\n")
	for i := range maxNestDepth + 50 {
		b.WriteString(strings.Repeat("    ", i+1) + "Simple Domain: Repeat 2\n")
	}
	err := parseErr(t, b.String())
	if err == nil {
		t.Fatal("expected a nesting error")
	}
	if !strings.Contains(err.Error(), "nests more than") {
		t.Errorf("error = %v, want the nesting limit", err)
	}
}

// TestOrdinaryNestingStillParses guards the bound from being felt by anything
// anyone writes: the limit is two orders of magnitude above real programs, and
// several separate expressions on one line each get the whole budget.
func TestOrdinaryNestingStillParses(t *testing.T) {
	nested := "Maximum Technique: Count Matching\n    Using: (x) -> " +
		strings.Repeat("abs(", 20) + "x" + strings.Repeat(")", 20) + " > 0\n"
	if err := parseErr(t, nested); err != nil {
		t.Errorf("20 levels should parse: %v", err)
	}
	// Sibling chains, one after another, in one program: the depth each one
	// takes has to come back when it is finished.
	var b strings.Builder
	b.WriteString("Cursed Energy: in.txt\n")
	for range 40 {
		b.WriteString("Maximum Technique: Count Matching\n        Using: (x) -> x" +
			strings.Repeat(" + 1", 100) + " > 0\n")
	}
	if err := parseErr(t, b.String()); err != nil {
		t.Errorf("40 sibling chains of 100 terms should parse: %v", err)
	}
}
