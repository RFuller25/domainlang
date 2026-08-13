package optimizer

import (
	"strings"
	"testing"
)

// Small-scale differential tests for fuseUnfoldStream: the day 15 "dueling
// generators" idiom, shrunk to bounds that run in milliseconds so the naive
// (unfused) interpreter stays a viable oracle — the real-scale version
// (40M/60M raw steps) is codegen.TestStreamFusionDay15, which checks the
// compiled backend against the known-correct answer instead, since the
// naive interpreter at that scale is exactly the problem this pass exists
// to fix.

const streamProgramHeader = `Cursed Energy: stdin
Split "\n"
Cursed Technique: Match Pattern
    Using: "Generator {id:word} starts with {numa:int}"
`

// dueling generators, at bounds small enough for the naive interpreter.
func duelingGeneratorsSrc(mapEachBody string) string {
	return streamProgramHeader + `Channel "a":
    Cursed Technique: Apply
        Using: (x) -> tuple((first(x).numa * 16807) % 2147483647, 0)
    Cursed Technique: Unfold
        Consider t As 40
        While: (x) -> item(x, 1) < t
        Using: (x) -> tuple((item(x, 0) * 16807) % 2147483647, item(x, 1) + 1)
    Map Each
        Using: ` + mapEachBody + `
    Filter
        Using: (x) -> x % 4 = 0
    Apply
        Using: (x) -> take(x, 3)

Channel "b":
    Cursed Technique: Apply
        Using: (x) -> tuple((last(x).numa * 48271) % 2147483647, 0)
    Cursed Technique: Unfold
        Consider t As 60
        While: (x) -> item(x, 1) < t
        Using: (x) -> tuple((item(x, 0) * 48271) % 2147483647, item(x, 1) + 1)
    Map Each
        Using: ` + mapEachBody + `
    Filter
        Using: (x) -> x % 8 = 0
    Apply
        Using: (x) -> take(x, 3)
Zip
    From: a, b

Filter
    Using: (x) -> band(item(x, 0), 65535) = band(item(x, 1), 65535)

Count
Reveal: stdout
`
}

const streamTestInput = "Generator A starts with 65\nGenerator B starts with 8921"

// TestUnfoldStreamMatchesNaive checks the fused chain against the same
// program with the optimizer disabled, across several seeds — the
// differential oracle every other pass in this package uses.
func TestUnfoldStreamMatchesNaive(t *testing.T) {
	src := duelingGeneratorsSrc("(x) -> item(x, 0)")
	naive, _ := resolveProgram(t, src, false)
	opt, rewrites := resolveProgram(t, src, true)

	if !containsMessage(rewrites, "Cursed Stream") {
		t.Fatalf("expected a Cursed Stream rewrite, got %v", messages(rewrites))
	}

	inputs := []string{
		streamTestInput,
		"Generator A starts with 1\nGenerator B starts with 1",
		"Generator A starts with 999\nGenerator B starts with 1",
	}
	for _, input := range inputs {
		wantOut, wantErr := interpret(naive, input)
		gotOut, gotErr := interpret(opt, input)
		if (gotErr != nil) != (wantErr != nil) {
			t.Fatalf("input %q: error divergence\noptimized err: %v\nnaive err: %v", input, gotErr, wantErr)
		}
		if wantErr == nil && gotOut != wantOut {
			t.Fatalf("input %q: output divergence\noptimized: %q\nnaive:     %q", input, gotOut, wantOut)
		}
	}
}

// TestUnfoldStreamStandsDownOnPartialMapEach checks that a Map Each whose
// lambda isn't provably total refuses the fusion entirely — the naive
// unfused Unfold stays in place rather than risking an error the naive
// pipeline would report differently.
func TestUnfoldStreamStandsDownOnPartialMapEach(t *testing.T) {
	// 10 / x is partial (x is not a nonzero literal), so isTotal refuses it —
	// unlike item(x, 0), which is only total here because of the
	// tuple-arity special case in isTotalElementwise.
	src := duelingGeneratorsSrc("(x) -> 10 / item(x, 0)")
	_, rewrites := resolveProgram(t, src, true)
	if containsMessage(rewrites, "Cursed Stream") {
		t.Fatalf("Stream must not fire on a partial Map Each, got %v", messages(rewrites))
	}
}

// TestUnfoldStreamRequiresElementwiseRun checks that an Unfold with nothing
// fusible immediately after it (no Map Each/Filter, no take terminator)
// is left alone rather than wrapped in a no-op Stream.
func TestUnfoldStreamRequiresElementwiseRun(t *testing.T) {
	src := streamProgramHeader + `Cursed Technique: Apply
    Using: (x) -> tuple((first(x).numa * 16807) % 2147483647, 0)
Cursed Technique: Unfold
    Consider t As 10
    While: (x) -> item(x, 1) < t
    Using: (x) -> tuple((item(x, 0) * 16807) % 2147483647, item(x, 1) + 1)
Cursed Technique: Take Item 0
Reveal: stdout
`
	_, rewrites := resolveProgram(t, src, true)
	if containsMessage(rewrites, "Cursed Stream") {
		t.Fatalf("Stream must not fire with nothing elementwise to fuse, got %v", messages(rewrites))
	}
}

// TestUnfoldStreamRunsInsideChannel is the specific regression this whole
// pass exists to fix: before prims/channel.go's Eval read Meta["nodes"] live,
// a length-changing rewrite of a Channel body was invisible to the
// interpreter, however it was reached in the source, since the prior
// closure had already captured the pre-optimization slice.
func TestUnfoldStreamRunsInsideChannel(t *testing.T) {
	src := duelingGeneratorsSrc("(x) -> item(x, 0)")
	_, rewrites := resolveProgram(t, src, true)
	n := 0
	for _, r := range rewrites {
		if strings.Contains(r.Message, "Cursed Stream") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("expected both channels' Unfold chains to fuse, got %d Cursed Stream rewrites in %v", n, messages(rewrites))
	}
}
