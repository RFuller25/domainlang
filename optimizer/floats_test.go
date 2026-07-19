package optimizer

import (
	"strings"
	"testing"

	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// optimizeSrc resolves src and runs the optimizer, returning the rewrite
// messages joined for substring checks.
func optimizeSrc(t *testing.T, src string) string {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var msgs []string
	for _, r := range Optimize(pipe, true) {
		msgs = append(msgs, r.Message)
	}
	return strings.Join(msgs, "\n")
}

const floatList = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Floats
`

// Float pipelines must keep their written order and algorithms: the
// int-specialized rewrites (quickselect, reorder elision, lambda algebra)
// stay off, because float addition is not associative, NaN is unordered, and
// the fused helpers are int64-typed.
func TestFloatPipelinesAreNotRewritten(t *testing.T) {
	cases := []struct {
		name string
		src  string
		off  string // rewrite fragment that must NOT appear
	}{
		{
			name: "sort before float sum survives",
			src:  floatList + "Domain Expansion: Sort\nMaximum Technique: Sum\nReveal: stdout\n",
			off:  "reordering cannot change",
		},
		{
			name: "sort reverse not fused on floats",
			src:  floatList + "Domain Expansion: Sort\nReverse Cursed Technique: Reverse\nReveal: stdout\n",
			off:  "Quicksort (Descending)",
		},
		{
			name: "double float sort survives",
			src:  floatList + "Domain Expansion: Sort\nDomain Expansion: Sort, Descending\nReveal: stdout\n",
			off:  "single Quicksort",
		},
		{
			name: "float lambda not simplified",
			src: floatList + "Cursed Technique: Map Each\n    Using: (x) -> x * 1 + 0\n" +
				"Maximum Technique: Sum\nReveal: stdout\n",
			off: "simplified the Using: lambda",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if msgs := optimizeSrc(t, c.src); strings.Contains(msgs, c.off) {
				t.Fatalf("rewrite fired on a float pipeline:\n%s", msgs)
			}
		})
	}
}

// The identical shapes over List<Int> must still fire — the guards are type
// checks, not blanket disables.
func TestIntPipelinesStillRewritten(t *testing.T) {
	intList := "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nChanneled Energy: Convert To Integers\n"
	msgs := optimizeSrc(t, intList+"Domain Expansion: Sort\nReverse Cursed Technique: Reverse\nReveal: stdout\n")
	if !strings.Contains(msgs, "Quicksort (Descending)") {
		t.Fatalf("int Sort+Reverse fusion stopped firing:\n%s", msgs)
	}
	msgs = optimizeSrc(t, intList+"Cursed Technique: Map Each\n    Using: (x) -> x * 1 + 0\nMaximum Technique: Sum\nReveal: stdout\n")
	if !strings.Contains(msgs, "simplified the Using: lambda") {
		t.Fatalf("int lambda simplification stopped firing:\n%s", msgs)
	}
}
