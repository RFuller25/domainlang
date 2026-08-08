package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Interpreter/binary byte parity for the ordering group: the relational
// operators over Text and tuples, Min By / Max By over any ordered key, and
// Transpose over List<List<T>>.
//
// Ordering is the thing here worth pinning twice. There are now four
// implementations of one order — ir.Compare, eval's compareOrdered, codegen's
// lessExpr (the sort inner loop) and codegen's interned dmCmpN (the operator)
// — so a program that reaches several of them over the same values and then
// asks both backends to agree is what keeps them from drifting apart.
func TestCompiledOrderingMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			// Text through all four operators, including the prefix and
			// case boundaries where byte-wise and rune-wise could differ.
			name: "text comparison",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (w) ->
        textjoin(list(
            w,
            if w < "c" then "lt" else "ge",
            if w > "app" then "gt" else "le",
            if w <= w then "eq" else "ne",
            if w >= "Z" then "upper" else "below"
        ), ":")
Maximum Technique: Join
    Using: "|"
Reveal: stdout
`,
			input: "banana\napple\napp\nZebra\ncherry",
		},
		{
			// A tuple comparison goes through the interned three-way compare,
			// where the sort of the same keys goes through lessExpr. Both are
			// in this one program on purpose.
			name: "tuple comparison beside a tuple sort",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Domain Expansion: Sort By
    Using: (w) -> tuple(length(w), w)
Cursed Technique: Pairs
Cursed Technique: Map Each
    Using: (p) ->
        if tuple(length(item(p, 0)), item(p, 0)) <= tuple(length(item(p, 1)), item(p, 1))
        then "sorted" else "OUT OF ORDER"
Maximum Technique: Join
    Using: ","
Reveal: stdout
`,
			input: "pear\nfig\napple\nfig\nquince\ndate",
		},
		{
			// Min By / Max By over a Text key, and over a tuple key that
			// tiebreaks — the shapes that used to be Int-only.
			name: "keyed extrema over ordered keys",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{w:word} {n:int}"

Part "min":
    Maximum Technique: Min By
        Using: (r) -> tuple(r.w, r.n)
    Cursed Technique: Apply
        Using: (r) -> r.w + "/" + totext(r.n)
    Reveal: stdout

Part "max":
    Maximum Technique: Max By
        Using: (r) -> r.w
    Cursed Technique: Apply
        Using: (r) -> r.w + "/" + totext(r.n)
    Reveal: stdout
`,
			input: "b 1\na 9\nb 0\na 2\nc 5",
		},
		{
			// The fused Split Fields + Convert To Integers + Max By path,
			// which tracks the best key in a local of the key's own type.
			name: "fused keyed extremum over a text key",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Fields
Channeled Energy: Convert To Integers
Maximum Technique: Max By
    Using: (r) -> totext(first(r))
Maximum Technique: Sum
Reveal: stdout
`,
			input: "3 1\n12 2\n9 3\n100 4",
		},
		{
			name: "transpose over a list of lists",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Transpose
Cursed Technique: Map Each
    Using: (col) -> sum(col)
Reveal: stdout
`,
			input: "1 2 3\n4 5 6\n7 8 9",
		},
		{
			// The text half of the same shape, and the round trip.
			name: "transpose rows of text twice",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Cursed Technique: Transpose
Cursed Technique: Transpose
Cursed Technique: Map Each
    Using: (row) -> textjoin(row, "")
Maximum Technique: Join
    Using: "/"
Reveal: stdout
`,
			input: "abc\ndef",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}
