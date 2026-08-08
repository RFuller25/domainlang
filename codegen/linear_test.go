package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Interpreter/binary byte parity for the linear-accumulator pass.
//
// Every program here is compiled and run in **both** optimizer modes, and
// `--no-optimize` is the copying semantics — so each case is simultaneously a
// four-way agreement: interpreter and binary, copying and in-place. That is
// the whole verification story for a pass whose entire job is to make a copy
// disappear without anyone noticing.
func TestCompiledLinearAccumulatorsMatchInterpreter(t *testing.T) {
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
			// The frequency map: reads of the accumulator are arguments, so
			// they run before the write.
			name: "frequency map fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Fold
    Seed: (xs) -> emptymap("", 0)
    Using: (acc, w) -> insert(acc, w, getor(acc, w, 0) + 1)
Channeled Energy: Convert To Entries
Cursed Technique: Map Each
    Using: (e) -> item(e, 0) + "=" + totext(item(e, 1))
Maximum Technique: Join
    Using: ","
Reveal: stdout
`,
			input: "a\nb\na\nc\na\nb",
		},
		{
			// The conditional record, and a chained insert into a Set.
			name: "conditional and chained set inserts",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> emptyset(0)
    Using: (acc, x) -> if x % 2 = 0 then insert(insert(acc, x), 0 - x) else acc
Cursed Technique: Apply
    Using: (s) -> totext(size(s)) + "/" + totext(sum(tolist(s)))
Reveal: stdout
`,
			input: "1\n2\n3\n4\n2\n5\n6",
		},
		{
			// A sparse plane grown one cell at a time, then densified.
			name: "sparse plotted in a fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> sparse("-")
    Using: (acc, x) -> put(acc, x % 3, x % 4, "#")
Channeled Energy: Convert To Grid
Reveal: stdout
`,
			input: "0\n5\n7\n11\n2",
		},
		{
			// FoldOver: the accumulator is the current pipeline value, and a
			// sibling Part reads the same grid. If the compiled fold skipped
			// its one-time clone, the second Part would show the writes.
			name: "grid mutated by a FoldOver a sibling Part also reads",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid

Channel "steps":
    Cursed Technique: Apply
        Using: (g) -> range(0, 3)

Part "written":
    Maximum Technique: Fold
        From: steps
        Using: (g, i) -> setat(g, i, i, "Z")
    Reveal: stdout

Part "original":
    Reveal: stdout
`,
			input: "abcd\nefgh\nijkl",
		},
		{
			// The shape the pass must refuse: the pre-update value is still
			// read, so the copy has to survive into the binary too.
			name: "a fold that reads the accumulator afterwards",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> emptymap(0, 0)
    Using: (acc, x) -> tomap(list(tuple(size(insert(acc, x, 1)), 0), tuple(size(acc), 1)))
Cursed Technique: Apply
    Using: (m) -> totext(size(m)) + "/" + totext(sum(keys(m)))
Reveal: stdout
`,
			input: "1\n2\n3\n4",
		},
		{
			// Reduce is seedless, so its accumulator is an element of the
			// input list — which the pipeline still holds.
			name: "reduce over sets the pipeline still holds",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> toset(list(x % 4))

Part "reduced":
    Maximum Technique: Reduce
        Using: (a, b) -> insert(a, first(tolist(b)))
    Cursed Technique: Apply
        Using: (s) -> size(s)
    Reveal: stdout

Part "first":
    Cursed Technique: Take Item 0
    Cursed Technique: Apply
        Using: (s) -> size(s)
    Reveal: stdout
`,
			input: "1\n2\n3\n5\n6",
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
