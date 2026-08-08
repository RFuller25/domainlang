package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// `Mode: Scan`, `Case:`, the extra hole types and the `{~}` gap, through both
// backends. Each changes what the generated parse function looks like — Scan
// emits an unanchored `dmScanN(s) []T` rather than `dmParseN(s) (T, bool)`,
// and a Case: stage emits one regexp per alternative filling one struct — so
// the oracle is the usual one: byte-identical stdout in both optimizer modes.
func TestMatchScanAndCasesOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const lines = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
`
	cases := []struct {
		name, src, input string
	}{
		// AoC 2024 D3, the shape Scan exists for.
		{"Scan finds every occurrence in noise", lines + `Cursed Technique: Match Pattern
    Mode: Scan
    Using: "mul({a:int},{b:int})"
Cursed Technique: Map Each
    Using: (m) -> m.a * m.b
Maximum Technique: Sum
Reveal: stdout
`, "xmul(2,4)%&mul[3,7]!@^do_not_mul(5,5)+mul(32,64]then(mul(11,8)mul(8,5))\n"},

		// A line contributes as many values as it holds, including none — and
		// a program where *no* line holds any must still produce an empty
		// list rather than differ between a nil and an empty slice.
		{"Scan over lines with none, one and many", lines + `Cursed Technique: Match Pattern
    Mode: Scan
    Using: "<{n:int}>"
Reveal: stdout
`, "a<1>b<2>\nnothing here\n<3>\n"},

		{"Scan that finds nothing at all", lines + `Cursed Technique: Match Pattern
    Mode: Scan
    Using: "<{n:int}>"
Reveal: stdout
`, "nothing\nat all\n"},

		{"Scan with a word hole", lines + `Cursed Technique: Match Pattern
    Mode: Scan
    Using: "[{k:word}={v:int}]"
Reveal: stdout
`, "junk[a=1]more[bb=22]\n[c=3]\n"},

		// AoC 2015 D6, in one ordered pass rather than three Try passes.
		{"Case: tags each line", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Case: on     "turn on {a:int},{b:int} through {c:int},{d:int}"
    Case: off    "turn off {a:int},{b:int} through {c:int},{d:int}"
    Case: toggle "toggle {a:int},{b:int} through {c:int},{d:int}"
Reveal: stdout
`, "turn on 0,0 through 9,9\ntoggle 1,1 through 2,2\nturn off 3,3 through 4,4\n"},

		{"Case: order decides which of two matches", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Case: specific "turn on {n:int}"
    Case: general  "turn {n:int}"
Cursed Technique: Map Each
    Using: (r) -> r.kind
Reveal: stdout
`, "turn on 5\nturn 7\n"},

		{"Case: under Try drops what no case matched", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Case: a "a {n:int}"
    Case: b "b {n:int}"
Maximum Technique: Count
Reveal: stdout
`, "a 1\nnope\nb 2\n"},

		{"Case: over a whole-text input", `Cursed Energy: stdin
Cursed Technique: Match Pattern
    Mode: One
    Case: add "add {n:int}"
    Case: sub "sub {n:int}"
Reveal: stdout
`, "sub 12\n"},

		// A case whose holes include a list keeps the field-set rule honest:
		// every case must produce the same *types*, not just the same names.
		{"Case: with a repeated hole in every case", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Case: keep "keep {ns:int+ sep=\",\"}"
    Case: drop "drop {ns:int+ sep=\",\"}"
Reveal: stdout
`, "keep 1,2,3\ndrop 4\n"},

		// The extra hole types, and the gap.
		{"hex, digits, char and a flexible gap", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{k:char} #{c:hex}{~}{d:digits}"
Reveal: stdout
`, "R #70c710   007\nL #0dc571 42\n"},

		{"a repeated hex hole", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{cs:hex+ sep=\",\"}"
Cursed Technique: Map Each
    Using: (r) -> sum(r.cs)
Maximum Technique: Sum
Reveal: stdout
`, "ff,10\nbeef\n"},

		{"a char hole inside an optional group", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "{n:int}[?{~}{sign:char}]"
Reveal: stdout
`, "5 +\n7\n9   -\n"},
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

// Scan stands parse-then-reduce fusion down for the reason Try does, one step
// further: a fused loop produces exactly one value per line, and Scan produces
// however many the line contains — including none. The observable is the
// `[]string` the fused Split never builds.
func TestModeScanStandsFusionDown(t *testing.T) {
	prog := func(mode string) string {
		return `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: ` + mode + `
    Using: "<{a:int}>"
Maximum Technique: Count Matching
    Using: (r) -> r.a > 0
Reveal: stdout
`
	}
	emit := func(mode string) string {
		src, err := codegen.EmitProgram(compilePipeline(t, prog(mode), true), codegen.Options{})
		if err != nil {
			t.Fatalf("Mode: %s: EmitProgram: %v", mode, err)
		}
		return src
	}
	if got := emit("Each"); strings.Contains(got, "strings.Split(") {
		t.Errorf("Mode: Each should still fuse:\n%s", got)
	}
	if got := emit("Scan"); !strings.Contains(got, "strings.Split(") {
		t.Errorf("Mode: Scan must not fuse — a line yields any number of values:\n%s", got)
	}
}
