package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
)

// A Set and a Map are sequences: every primitive that reads its input as one
// accepts them wherever it accepts a List, and this is the test that keeps
// codegen's seqConsumers list in step with the resolver's listElem.
//
// The correspondence is not decorative. The list had drifted: Take While, the
// quantifiers and the keyed reductions all resolved happily over a Set and
// then emitted Go that did not build, because nothing bridged dmSet to a
// slice for them. A Set input is the cheap way to exercise every one of them
// at once — the resolver already accepted it, so any refusal or build failure
// here is a missing bridge rather than a missing feature.
func TestEverySequencePrimitiveCompilesOverASet(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	// One stage per list-shaped primitive, over Set<Int>, each ending in
	// something printable. The head builds the Set; the tail is per case.
	const head = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Channeled Energy: Convert To Set
`
	stages := map[string]string{
		"Any":            "Maximum Technique: Any\n    Using: (x) -> x > 2\n",
		"All":            "Maximum Technique: All\n    Using: (x) -> x > 0\n",
		"Chunk":          "Cursed Technique: Chunk 2\n",
		"Count":          "Maximum Technique: Count\n",
		"Count By":       "Maximum Technique: Count By\n    Using: (x) -> x % 2\n",
		"Count Matching": "Maximum Technique: Count Matching\n    Using: (x) -> x > 2\n",
		"Drop While":     "Cursed Technique: Drop While\n    Using: (x) -> x < 3\n",
		"Enumerate":      "Cursed Technique: Enumerate\n",
		"Filter":         "Cursed Technique: Filter\n    Using: (x) -> x > 2\n",
		"Find":           "Maximum Technique: Find\n    Using: (x) -> x > 2\n",
		"Find Index":     "Maximum Technique: Find Index\n    Using: (x) -> x > 2\n",
		"Fold":           "Maximum Technique: Fold\n    Seed: (xs) -> 0\n    Using: (a, x) -> a + x\n",
		"Group By":       "Maximum Technique: Group By\n    Using: (x) -> x % 2\n",
		"Map Each":       "Cursed Technique: Map Each\n    Using: (x) -> x * 2\n",
		"Max By":         "Maximum Technique: Max By\n    Using: (x) -> x % 3\n",
		"Min By":         "Maximum Technique: Min By\n    Using: (x) -> x % 3\n",
		"Pairs":          "Cursed Technique: Pairs\n",
		"Partition":      "Cursed Technique: Partition\n    Using: (x) -> x > 2\n",
		"Permutations":   "Domain Expansion: Permutations\n",
		"Product By":     "Maximum Technique: Product By\n    Using: (x) -> x % 4\n",
		"Reduce":         "Maximum Technique: Reduce\n    Using: (a, b) -> a + b\n",
		"Scan":           "Cursed Technique: Scan\n    Seed: (xs) -> 0\n    Using: (a, x) -> a + x\n",
		"Sort By":        "Domain Expansion: Sort By\n    Using: (x) -> 0 - x\n",
		"Subsets":        "Domain Expansion: Subsets\n",
		"Sum By":         "Maximum Technique: Sum By\n    Using: (x) -> x % 4\n",
		"Take Item":      "Cursed Technique: Take Item 0\n",
		"Take While":     "Cursed Technique: Take While\n    Using: (x) -> x < 3\n",
		"Unique":         "Cursed Technique: Unique\n",
		"Window":         "Cursed Technique: Window 2\n",
	}
	const input = "1\n2\n3\n4\n2"
	for name, stage := range stages {
		t.Run(name, func(t *testing.T) {
			src := head + stage + "Reveal: stdout\n"
			pipe := compilePipeline(t, src, false)
			want := runInterpreter(t, pipe, []byte(input))
			got := buildAndRun(t, pipe, []byte(input), codegen.Options{})
			if got != want {
				t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
			}
		})
	}
}

// The same primitives over a Map, whose element is an entry tuple rather than
// a bare value. That distinction is the one a Map can get wrong on its own: a
// Map's .Elem is its *value* type, so an emitter typing a lambda parameter
// from it would bind half the pair and compile a program that reads the wrong
// column.
func TestSequencePrimitivesOverAMap(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count By
    Using: (w) -> length(w)
`
	stages := []struct{ name, stage string }{
		{"Count", "Maximum Technique: Count\n"},
		{"Map Each reads both halves",
			"Cursed Technique: Map Each\n    Using: (e) -> item(e, 0) * 100 + item(e, 1)\n"},
		{"Filter on the key",
			"Cursed Technique: Filter\n    Using: (e) -> item(e, 0) > 4\n" +
				"Cursed Technique: Map Each\n    Using: (e) -> item(e, 1)\n"},
		{"Max By on the value — the most common element",
			"Maximum Technique: Max By\n    Using: (e) -> item(e, 1)\n" +
				"Cursed Technique: Apply\n    Using: (e) -> item(e, 0)\n"},
		{"Sort By on the value",
			"Domain Expansion: Sort By, Descending\n    Using: (e) -> item(e, 1)\n" +
				"Cursed Technique: Map Each\n    Using: (e) -> item(e, 0)\n"},
		{"Sum By over the values",
			"Maximum Technique: Sum By\n    Using: (e) -> item(e, 1)\n"},
		{"Fold over the entries",
			"Maximum Technique: Fold\n    Seed: (xs) -> 0\n" +
				"    Using: (a, e) -> a + item(e, 0) * item(e, 1)\n"},
		{"Any over the entries",
			"Maximum Technique: Any\n    Using: (e) -> item(e, 1) > 1\n"},
		{"Enumerate the entries",
			"Cursed Technique: Enumerate\n" +
				"Cursed Technique: Map Each\n    Using: (p) -> item(p, 0)\n"},
	}
	const input = "aa\nbbb\ncc\ndddd\nee\nfff"
	for _, tc := range stages {
		t.Run(tc.name, func(t *testing.T) {
			src := head + tc.stage + "Reveal: stdout\n"
			pipe := compilePipeline(t, src, false)
			want := runInterpreter(t, pipe, []byte(input))
			got := buildAndRun(t, pipe, []byte(input), codegen.Options{})
			if got != want {
				t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
			}
		})
	}
}

// A Set or a Map that flows through a shape-preserving primitive comes out a
// List, because Filter drops elements and Sort By imposes an order — neither
// of which a Set or a Map has a place to put. Claiming the input type back was
// a lie the interpreter already told (it rendered a filtered Set as a list)
// and one the compiler could not tell, so it emitted Go that did not build.
func TestShapePreservingPrimitivesYieldAListFromASet(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Channeled Energy: Convert To Set
`
	for _, tc := range []struct{ name, stage, want string }{
		{"Filter", "Cursed Technique: Filter\n    Using: (x) -> x > 2\n", "[3, 4]"},
		{"Unique", "Cursed Technique: Unique\n", "[1, 2, 3, 4]"},
		{"Take While", "Cursed Technique: Take While\n    Using: (x) -> x < 3\n", "[1, 2]"},
		{"Sort By", "Domain Expansion: Sort By\n    Using: (x) -> 0 - x\n", "[4, 3, 2, 1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := head + tc.stage + "Reveal: stdout\n"
			pipe := compilePipeline(t, src, false)
			want := runInterpreter(t, pipe, []byte("1\n2\n3\n4\n2"))
			if strings.TrimSpace(want) != tc.want {
				t.Errorf("interpreter rendered %q, want %q — a List, not a Set",
					strings.TrimSpace(want), tc.want)
			}
			got := buildAndRun(t, pipe, []byte("1\n2\n3\n4\n2"), codegen.Options{})
			if got != want {
				t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
			}
		})
	}
}

// A loop body may consume a Channel. The compiler side is free rather than
// clever: channels are emitted as top-level variables and a loop body is
// emitted *inline*, so the value is already in scope where the body runs.
// That is exactly why a Shikigami and a `Using:` body still refuse one — the
// first is inlined at call sites that need not share a scope, and the second
// compiles to a function of its own.
func TestLoopBodyConsumingAChannelCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers

Channel "rules":
    Cursed Technique: Apply
        Using: (xs) -> list(10, 100)

Cursed Technique: Apply
    Using: (xs) -> 0
Simple Domain: Repeat 3
    Maximum Technique: Fold
        From: rules
        Using: (acc, r) -> acc + r
Reveal: stdout
`
	for _, optimize := range []bool{true, false} {
		mode := "naive"
		if optimize {
			mode = "optimized"
		}
		t.Run(mode, func(t *testing.T) {
			pipe := compilePipeline(t, src, optimize)
			want := runInterpreter(t, pipe, []byte("1\n2\n3"))
			got := buildAndRun(t, pipe, []byte("1\n2\n3"), codegen.Options{})
			if got != want {
				t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
			}
		})
	}
}

// A `Params:` body — a lambda of two or more parameters written as an indented
// pipeline. The extra parameters reach the body as bindings, and the compiler
// threads them into the block's function exactly as it already threaded a
// `Consider` from an outer scope. This is the parity check on that reuse.
func TestCompiledParamsBodiesMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
`
	progs := []struct{ name, stage string }{
		{"fold whose step sorts",
			"Maximum Technique: Fold\n" +
				"    Seed: (xs) -> 0\n" +
				"    Params: acc, r\n" +
				"    Domain Expansion: Sort\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> acc + first(s)\n"},
		{"scan whose step groups",
			"Cursed Technique: Scan\n" +
				"    Seed: (xs) -> 0\n" +
				"    Params: acc, r\n" +
				"    Maximum Technique: Count By\n        Using: (n) -> n % 3\n" +
				"    Cursed Technique: Apply\n        Using: (m) -> acc + size(m)\n"},
		{"reduce whose step concatenates",
			"Maximum Technique: Reduce\n" +
				"    Params: a, b\n" +
				"    Domain Expansion: Sort\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> concat(a, s)\n"},
		{"beside a binding from an outer scope",
			"Cursed Technique: Apply\n" +
				"    Consider bonus As 100\n" +
				"    Maximum Technique: Fold\n" +
				"        Seed: (xs) -> 0\n" +
				"        Params: acc, r\n" +
				"        Domain Expansion: Sort\n" +
				"        Cursed Technique: Apply\n            Using: (s) -> acc + first(s) + bonus\n"},
	}
	const input = "3 1 2\n9 9 4\n5 5 5"
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				pipe := compilePipeline(t, head+p.stage+"Reveal: stdout\n", optimize)
				want := runInterpreter(t, pipe, []byte(input))
				got := buildAndRun(t, pipe, []byte(input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}
