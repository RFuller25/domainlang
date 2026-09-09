package codegen_test

import (
	"testing"

	"domain/codegen"
)

// TestCompiledForLoopsMatchInterpreter closes the one advertised
// interpreter-only gap: `Simple Domain: For` now compiles, so the repo's
// "every primitive works in both backends with oracle-pinned identical
// output" rule holds without an exception.
//
// The interesting part is not the loop but the binding: inside a For body the
// resolver appends one trailing ambient parameter per enclosing loop to every
// Using: lambda, and the compiler has to bind those to the right Go variables,
// outermost first.
func TestCompiledForLoopsMatchInterpreter(t *testing.T) {
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
			name: "for over a channel list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers

Channel "deltas":
    Cursed Technique: Take Item 0
    Cursed Technique: Apply
        Using: (n) -> list(n, n + 1, 0 - n)

Simple Domain: For d in deltas
    Cursed Technique: Map Each
        Using: (v, d) -> v + d

Cursed Technique: Map Each
    Using: (v) -> totext(v)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "2\n5\n7",
		},
		{
			name: "for over range",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Simple Domain: For i in range(4)
    Cursed Technique: Map Each
        Using: (v, i) -> v * 2 + i
Cursed Technique: Map Each
    Using: (v) -> totext(v)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// Nested loops stack outermost-first: `(v, a, b)` binds a to the
			// outer loop and b to the inner one. Getting the order backwards
			// would still compile, so the values have to distinguish it.
			name: "nested for binds outermost first",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers

Channel "as":
    Cursed Technique: Apply
        Using: (n) -> list(1, 2)

Channel "bs":
    Cursed Technique: Apply
        Using: (n) -> list(10, 20)

Simple Domain: For a in as
    Simple Domain: For b in bs
        Cursed Technique: Map Each
            Using: (v, a, b) -> v + a * 100 + b

Cursed Technique: Map Each
    Using: (v) -> totext(v)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "0\n1",
		},
		{
			// The ambient variable reaches a Filter predicate and a nested
			// range loop in the same body.
			name: "for with filter and inner range",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers

Channel "ts":
    Cursed Technique: Apply
        Using: (n) -> list(2, 4)

Simple Domain: For t in ts
    Cursed Technique: Filter
        Using: (v, t) -> v > t
    Simple Domain: For i in range(2)
        Cursed Technique: Map Each
            Using: (v, t, i) -> v + t + i

Cursed Technique: Map Each
    Using: (v) -> totext(v)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1\n3\n5\n9",
		},
		{
			// A leading lambda parameter that shares the ambient name must
			// win: the env the caller builds shadows the ambient fallback.
			name: "leading parameter shadows the ambient name",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers

Channel "ds":
    Cursed Technique: Apply
        Using: (n) -> list(5, 6)

Simple Domain: For v in ds
    Cursed Technique: Map Each
        Using: (v, w) -> v * 10 + w

Cursed Technique: Map Each
    Using: (v) -> totext(v)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1\n2",
		},
		{
			// Ambient bindings must survive a Fold, whose lambda has two of
			// its own parameters before the trailing ambient one.
			name: "for around a two-parameter fold",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers

Channel "ks":
    Cursed Technique: Apply
        Using: (n) -> list(1, 3)

Simple Domain: For k in ks
    Cursed Technique: Apply
        Using: (xs, k) -> concat(xs, list(sum(xs) + k))

Cursed Technique: Map Each
    Using: (v) -> totext(v)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1\n2",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p := p
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				// Deliberately not t.Parallel(): prims' ambient stack for For
				// loop variables is package-level, and prims/ambient.go
				// documents that Resolve is never called concurrently. Two
				// parallel subtests resolving For bodies interleave their
				// pushes and mis-count each lambda's arity.
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

// TestCompiledForEachLoopsMatchInterpreter is TestCompiledForLoopsMatchInterpreter's
// twin for `Simple Domain: For Each x In` — the source is a Using: lambda
// evaluated against the current value each time the loop runs, rather than a
// Channel fixed before it, and the body's lambdas gain two ambients (the
// element, then its index) instead of one.
func TestCompiledForEachLoopsMatchInterpreter(t *testing.T) {
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
			name: "for each over a record field, writing back with set() and the index",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> {items: xs}
Simple Domain: For Each x In
    Using: (v) -> v.items
    Cursed Technique: Apply
        Using: (v, x, i) -> with(v, "items", set(v.items, i, x * 10 + i))
Cursed Technique: Apply
    Using: (v) -> v.items
Cursed Technique: Map Each
    Using: (n) -> totext(n)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// Nested For Each: the outer loop's (element, index) land before
			// the inner loop's, same outermost-first order For x in y uses.
			name: "nested for each binds outermost first, element then index",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> {rows: list(xs, xs), total: 0}
Simple Domain: For Each row In
    Using: (v) -> v.rows
    Simple Domain: For Each cell In
        Using: (v, row, ri) -> row
        Cursed Technique: Apply
            Using: (v, row, ri, cell, ci) -> with(v, "total", v.total + cell + ri * 100 + ci)
Cursed Technique: Apply
    Using: (v) -> totext(v.total)
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// A body that reads only the element, never the index — checks
			// codegen doesn't leave an unused Go variable behind either way.
			name: "for each ignoring the index ambient entirely",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> {items: xs, total: 0}
Simple Domain: For Each x In
    Using: (v) -> v.items
    Cursed Technique: Apply
        Using: (v, x, i) -> with(v, "total", v.total + x)
Cursed Technique: Apply
    Using: (v) -> totext(v.total)
Reveal: stdout
`,
			input: "4\n5\n6",
		},
		{
			// The source list shrinks lap to lap (the body filters nothing —
			// it's captured once, up front — but a second For Each over the
			// same field after a write proves the source is re-read fresh).
			name: "a second for each re-reads the field's new length",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Apply
    Using: (xs) -> {items: xs, seen: 0}
Simple Domain: For Each x In
    Using: (v) -> v.items
    Cursed Technique: Apply
        Using: (v, x, i) -> with(v, "seen", v.seen + 1)
Cursed Technique: Apply
    Using: (v) -> with(v, "items", concat(v.items, list(99)))
Simple Domain: For Each x In
    Using: (v) -> v.items
    Cursed Technique: Apply
        Using: (v, x, i) -> with(v, "seen", v.seen + 1)
Cursed Technique: Apply
    Using: (v) -> totext(v.seen)
Reveal: stdout
`,
			input: "1\n2\n3",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p := p
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				// Deliberately not t.Parallel() — see the comment on
				// TestCompiledForLoopsMatchInterpreter above; the same
				// package-level ambient stack backs both loop kinds.
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
