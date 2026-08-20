package codegen_test

import (
	"testing"

	"domain/codegen"
)

// The interpreter-vs-binary oracle for `Cursed Object` / `Cursed Tool`.
//
// The two backends hold a global in completely different places — a slot in a
// slice for the interpreter, a package-level Go variable for the binary — so
// every one of these is a case where they could plausibly disagree and must
// not. Both optimizer modes run, since a global read is a value the optimizer
// has to leave alone in a stage it might otherwise rewrite.
func TestGlobalsOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`
	cases := []struct{ name, src, input string }{
		{"declare and read", header + `Cursed Object: bump As 10
Cursed Technique: Map Each
    Using: (x) -> x + bump
Reveal: stdout
`, ""},
		{"walrus accumulates across elements", header + `Cursed Object: total As 0
Cursed Technique: Map Each
    Using: (x) -> total := total + x
Reveal: stdout
`, ""},
		{"read after the stage that wrote it", header + `Cursed Object: total As 0
Cursed Technique: Map Each
    Using: (x) -> x also total := total + x
Cursed Technique: Apply
    Using: (xs) -> total
Reveal: stdout
`, ""},
		{"Cursed Tool assigns", header + `Cursed Object: n As 1
Cursed Tool: n As n * 41
Cursed Technique: Apply
    Using: (xs) -> n
Reveal: stdout
`, ""},
		{"declarations see earlier ones", header + `Cursed Object:
    a As 2
    b As a * 5
    c As a + b
Cursed Technique: Apply
    Using: (xs) -> c
Reveal: stdout
`, ""},
		{"Of reads the current value", header + `Cursed Object: n Of (xs) -> length(xs)
Cursed Technique: Apply
    Using: (xs) -> n
Reveal: stdout
`, ""},
		{"Of an operation phrase", header + `Cursed Object: total Of Sum
Cursed Technique: Apply
    Using: (xs) -> total
Reveal: stdout
`, ""},
		{"Of a sub-pipeline", header + `Cursed Object: doubled Of
    Cursed Technique: Map Each
        Using: (x) -> x * 2
    Maximum Technique: Sum
Cursed Technique: Apply
    Using: (xs) -> doubled
Reveal: stdout
`, ""},
		{"survives the loop that wrote it", `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(trim(s))
Cursed Object: laps As 0
Simple Domain: While
    Using: (v) -> v > 1
    Cursed Technique: Apply
        Using: (n) -> (n / 2) also laps := laps + 1
Cursed Technique: Apply
    Using: (v) -> laps
Reveal: stdout
`, "20\n"},
		{"Cursed Tool inside a loop body", `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(trim(s))
Cursed Object:
    a As 1
    seen As 0
Simple Domain: Repeat 3
    Cursed Tool:
        a As a * 10
        seen As seen + a
Cursed Technique: Apply
    Using: (v) -> seen
Reveal: stdout
`, "0\n"},
		{"declaration inside a loop body re-runs", `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(trim(s))
Cursed Object: total As 0
Simple Domain: Repeat 3
    Cursed Object: perLap As 1
    Cursed Tool: total As total + perLap
Cursed Technique: Apply
    Using: (v) -> total
Reveal: stdout
`, "0\n"},
		{"read and write race in one expression", header + `Cursed Object: n As 0
Cursed Technique: Map Each
    Using: (x) -> n + (n := x) + n
Reveal: stdout
`, ""},
		{"shadowed by a lambda parameter", header + `Cursed Object: x As 99
Cursed Technique: Map Each
    Using: (x) -> x
Reveal: stdout
`, ""},
		{"shadowed by a stage binding", header + `Cursed Object: n As 99
Cursed Technique: Map Each
    Consider n As 7
    Using: (x) -> x + n
Reveal: stdout
`, ""},
		{"a global inside a pipeline body", `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ","
Channeled Energy: Convert Each List to Integers
Cursed Object: bump As 10
Cursed Technique: Map Each
    Maximum Technique: Sum By
        Using: (x) -> x + bump
Reveal: stdout
`, "1,2\n3,4\n"},
		{"a global written from inside a pipeline body", `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ","
Channeled Energy: Convert Each List to Integers
Cursed Object: seen As 0
Cursed Technique: Map Each
    Maximum Technique: Sum By
        Using: (x) -> x also seen := seen + 1
Cursed Technique: Apply
    Using: (xs) -> seen
Reveal: stdout
`, "1,2\n3,4,5\n"},
		{"a global written from inside a For body", `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers

Channel "ds":
    Cursed Technique: Apply
        Using: (xs) -> list(1, 2, 3)

Cursed Object: seen As 0
Simple Domain: For d in ds
    Cursed Technique: Apply
        Using: (xs, d) -> xs also seen := seen + d
Cursed Technique: Apply
    Using: (xs) -> seen
Reveal: stdout
`, ""},
		{"a global read from a local Shikigami", `Shikigami "Show N"
    Cursed Technique: Map Each
        Using: (x) -> x + n

Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Object: n As 100
Shikigami: Show N
Reveal: stdout
`, ""},
		{"Parts do not see each other's writes", header + `Cursed Object: n As 0

Part "1":
    Cursed Tool: n As 100
    Cursed Technique: Apply
        Using: (xs) -> n
    Reveal: stdout

Part "2":
    Cursed Technique: Apply
        Using: (xs) -> n
    Reveal: stdout
`, ""},
		{"a Text global", header + `Cursed Object: tag As "n="
Cursed Technique: Map Each
    Using: (x) -> tag + totext(x)
Reveal: stdout
`, ""},
		{"a Float global", header + `Cursed Object: half As 0.5
Cursed Technique: Map Each
    Using: (x) -> tofloat(x) * half
Reveal: stdout
`, ""},
		{"a List global", header + `Cursed Object: xs Of Itself
Cursed Technique: Apply
    Using: (v) -> sum(xs) + length(xs)
Reveal: stdout
`, ""},
	}

	for _, c := range cases {
		input := []byte("1\n2\n3\n4\n5\n")
		if c.input != "" {
			input = []byte(c.input)
		}
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe, want := oracleFront(t, c.src, optimize, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q\n\n%s", want, got, c.src)
				}
			})
		}
	}
}
