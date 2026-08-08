package codegen_test

import (
	"testing"

	"domain/codegen"
)

// Record field declaration order is not part of a Record type's identity:
// Type.Equal compares by field set, and so do KeyOf and DeepEqual. The
// compiled backend has to agree, because two Go structs for one Domain type
// cannot meet — an `if` whose arms build the fields in different orders, a
// list holding both, a Map keyed or valued by both — without emitting Go that
// does not compile.
//
// Each program below builds the same fields in two orders and then makes them
// meet somewhere. The oracle is the usual one: byte-identical stdout, in both
// optimizer modes. Before the struct intern table was canonicalized these
// failed in `go build`, on generated source, with no Domain position.
func TestRecordFieldOrderOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`
	cases := []struct {
		name string
		src  string
	}{
		{"= over the two orders", header + `Cursed Technique: Map Each
    Using: (x) -> if record("a", x, "b", x) = record("b", x, "a", x) then 1 else 0
Maximum Technique: Sum
Reveal: stdout
`},
		{"if arms disagree on the order", header + `Cursed Technique: Map Each
    Using: (x) -> if x > 2 then record("a", x, "b", x * 2) else record("b", x * 2, "a", x)
Reveal: stdout
`},
		{"one list holds both", header + `Cursed Technique: Map Each
    Using: (x) -> list(record("a", x, "b", x * 2), record("b", x * 2, "a", x))
Reveal: stdout
`},
		{"a record built the other way round is a Map value", header + `Cursed Technique: Map Each
    Using: (x) -> tuple(x, if x > 2 then record("a", x, "b", x) else record("b", x, "a", x))
Channeled Energy: Convert To Map
Reveal: stdout
`},
		{"a set dedupes across the two orders", header + `Cursed Technique: Map Each
    Using: (x) -> if x > 2 then record("a", 1, "b", 2) else record("b", 2, "a", 1)
Cursed Technique: Unique
Reveal: stdout
`},
		{"a loop threads one and rebuilds it the other way", header + `Cursed Technique: Take Item 0
Cursed Technique: Apply
    Using: (x) -> record("a", x, "b", x * 2)
Simple Domain: Repeat 3
    Cursed Technique: Apply
        Using: (r) -> record("b", r.a + r.b, "a", r.a + 1)
Reveal: stdout
`},
		{"a binding declares one order and its reader the other", header + `Cursed Technique: Map Each
    Consider base As record("b", 10, "a", 20)
    Using: (x) -> if record("a", x, "b", x) = base then 1 else 0
Maximum Technique: Sum
Reveal: stdout
`},
	}

	for _, c := range cases {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(c.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				input := []byte("1\n2\n3\n4\n5\n")
				pipe, want := oracleFront(t, c.src, optimize, input)
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q\n\n%s", want, got, c.src)
				}
			})
		}
	}
}

// TestRecordRendersInTypeOrder is the rendering half, stated on its own because
// the oracle above would pass just as happily if both backends agreed on the
// wrong order. A compiled record is an unboxed struct with one field order, so
// the type is what decides how a value prints — and the interpreter, whose
// RecordValue carries the order it was *built* in, renders through the same
// type at the Reveal sink.
func TestRecordRendersInTypeOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	// The `if`'s type is its then arm's, so both elements print a-then-b even
	// though the else arm writes b first.
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> if x > 1 then record("a", x, "b", x * 2) else record("b", x * 2, "a", x)
Reveal: stdout
`
	const want = "[{a: 1, b: 2}, {a: 2, b: 4}, {a: 3, b: 6}]\n"

	pipe := compilePipeline(t, src, false)
	if got := runInterpreter(t, pipe, []byte("1\n2\n3\n")); got != want {
		t.Errorf("interpreter: got %q, want %q", got, want)
	}
	if got := buildAndRun(t, pipe, []byte("1\n2\n3\n"), codegen.Options{}); got != want {
		t.Errorf("binary: got %q, want %q", got, want)
	}
}
