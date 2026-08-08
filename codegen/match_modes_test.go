package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Repetition holes and `Mode: Try` both change what Match Pattern lowers to,
// and each has two lowerings that must agree: the interpreter matches with a
// named-group regex, while the compiler emits either a hand-rolled scanner or
// an unnamed-group regex. A repeated hole is not eligible for the scanner and
// a Try stage cannot fuse into its consumer, so these programs take the paths
// the ordinary Match Pattern tests never reach.
//
// The oracle is the usual one: byte-identical stdout from interpreter and
// binary, in both optimizer modes.
func TestMatchRepetitionAndTryOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const lines = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
`
	cases := []struct {
		name  string
		src   string
		input string
	}{
		{"a named repeated hole is a List field", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{id:word}: {vals:int+ sep=\",\"}"
Reveal: stdout
`, "target: 1,2,3\nother: 40,50\nsolo: 7\n"},

		{"the List field is summed", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{id:word}: {vals:int+ sep=\", \"}"
Cursed Technique: Map Each
    Using: (r) -> sum(r.vals)
Maximum Technique: Sum
Reveal: stdout
`, "a: 1, 2, 3\nb: 40, 50\nc: 7\n"},

		// A lone repeated hole is the whole template, so the capture spans the
		// line and the separator is the only structure there is.
		{"a positional repeated hole is a bare List", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{int+ sep=\" \"}"
Reveal: stdout
`, "1 2 3\n4 5\n6\n"},

		// A word element is \S+, which would eat a separator with no space in
		// it; the compiled split has to break the run the same way the
		// interpreter's does.
		{"repeated words split on a spaceless separator", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{ws:word+ sep=\",\"}"
Reveal: stdout
`, "a,b,c\nd\ne,f\n"},

		// The Day 6 shape: one file, two line kinds, one pass per kind.
		{"Try keeps the lines of one shape", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "toggle {a:int},{b:int} through {c:int},{d:int}"
Reveal: stdout
`, "turn on 0,0 through 9,9\ntoggle 1,1 through 2,2\nturn off 3,3 through 4,4\ntoggle 5,5 through 6,6\n"},

		{"Try counts what it kept", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "turn {what:word} {a:int},{b:int} through {c:int},{d:int}"
Maximum Technique: Count
Reveal: stdout
`, "turn on 0,0 through 9,9\ntoggle 1,1 through 2,2\nturn off 3,3 through 4,4\n"},

		// Nothing matching is a legitimate answer for Try, and an empty list
		// is the one value a length-prefixed lowering is most likely to get
		// wrong.
		{"Try that keeps nothing yields an empty list", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "nothing here {a:int}"
Reveal: stdout
`, "turn on 0,0\ntoggle 1,1\n"},

		{"Try feeding a fusible consumer", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "{w:word}={n:int}"
Cursed Technique: Map Each
    Using: (r) -> r.n * 2
Maximum Technique: Sum
Reveal: stdout
`, "a=1\nnot a pair\nb=2\nalso not\nc=3\n"},

		{"Try and repetition together", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "{id:word}: {vals:int+ sep=\",\"}"
Cursed Technique: Map Each
    Using: (r) -> length(r.vals)
Reveal: stdout
`, "a: 1,2,3\ngarbage\nb: 4\n\nc: 5,6\n"},

		// Mode: One over a whole-text input is the other side of the same
		// change: repetition has to work off the pipeline's Text branch too.
		{"Mode: One with a repeated hole", `Cursed Energy: stdin
Cursed Technique: Match Pattern
    Mode: One
    Using: "seeds: {vals:int+ sep=\" \"}"
Cursed Technique: Apply
    Using: (r) -> sum(r.vals)
Reveal: stdout
`, "seeds: 79 14 55 13\n"},
	}

	for _, c := range cases {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				input := []byte(c.input)
				pipe, want := oracleFront(t, c.src, optimize, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q\n\n%s", want, got, c.src)
				}
			})
		}
	}
}
