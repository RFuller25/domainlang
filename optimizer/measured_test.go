package optimizer

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
)

// A measured argument (prims/measure.go) has no value at optimize time: its
// literal key is absent from Meta entirely. Every pass that folds such a
// literal reads it with a type assertion whose zero value is a plausible
// number, so the hazard is not a pass that fails to fire — it is a pass that
// fires with a fabricated constant.
//
// Two passes carry the argument instead, because their fused nodes take it as
// data (TopK's k, the sliding helpers' size and step). The rest stand down.
// These cover both halves, and the property that matters either way: the
// optimized pipeline agrees with the naive one, value and error alike.

const measuredTopK = listHeader + `Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top
    Count: (xs) -> length(xs) / 2
Reveal: stdout
`

const measuredWindowSum = listHeader + `Cursed Technique: Window
    Size: (xs) -> length(xs) / 3
Cursed Technique: Map Each
    Using: (w) -> sum(w)
Reveal: stdout
`

// sameBothWays runs a program with and without the optimizer and requires the
// two to agree — the oracle every rewrite is held to.
func sameBothWays(t *testing.T, src, input string) string {
	t.Helper()
	fast, _ := resolveProgram(t, src, true)
	naive, _ := resolveProgram(t, src, false)
	got, gotErr := interpret(fast, input)
	want, wantErr := interpret(naive, input)
	switch {
	case (gotErr == nil) != (wantErr == nil):
		t.Fatalf("optimized and naive disagree on failure: optimized %v, naive %v", gotErr, wantErr)
	case gotErr != nil && gotErr.Error() != wantErr.Error():
		t.Fatalf("optimized and naive report different errors\n got: %v\nwant: %v", gotErr, wantErr)
	case got != want:
		t.Fatalf("optimized output diverges from naive\n got: %q\nwant: %q", got, want)
	}
	if wantErr != nil {
		return wantErr.Error()
	}
	return want
}

func TestMeasuredSelectTopKFusesAndAgrees(t *testing.T) {
	pipe, rewrites := resolveProgram(t, measuredTopK, true)
	if !firedContaining(rewrites, "Quickselect") {
		t.Fatalf("the quickselect rewrite should carry a measured count, got %v", rewrites)
	}
	if !hasPrim(pipe, "PartialSelect") {
		t.Fatal("expected a fused PartialSelect node")
	}
	// The regression the carry replaced: folding the missing literal to 0 made
	// this return the empty list, silently and only when optimized.
	if got := strings.TrimSpace(sameBothWays(t, measuredTopK, "3\n1\n4\n1\n5\n9\n2\n6\n")); got != "[9, 6, 5, 4]" {
		t.Fatalf("measured quickselect: got %q", got)
	}
}

func TestMeasuredWindowFusesIntoWindowedReduce(t *testing.T) {
	pipe, rewrites := resolveProgram(t, measuredWindowSum, true)
	if !firedContaining(rewrites, "Sliding-Window") {
		t.Fatalf("the sliding-window fusion should carry a measured size, got %v", rewrites)
	}
	if !hasPrim(pipe, "WindowedReduce") {
		t.Fatal("expected a fused WindowedReduce node")
	}
	if got := strings.TrimSpace(sameBothWays(t, measuredWindowSum, "1\n2\n3\n4\n5\n6\n")); got != "[3, 5, 7, 9, 11]" {
		t.Fatalf("measured sliding window: got %q", got)
	}
}

// Safety rule 2 in the presence of a measured argument: the fused node resolves
// it through the primitive's own resolver, so a bound the naive pipeline fails
// fails identically — same wording, same position — rather than becoming a
// silent success on a fabricated number.
func TestMeasuredBoundErrorSurvivesFusion(t *testing.T) {
	msg := sameBothWays(t, measuredWindowSum, "7\n")
	if !strings.Contains(msg, "must be >= 1") || !strings.Contains(msg, "measured 0") {
		t.Fatalf("expected the measured-bound error from both modes, got %q", msg)
	}
}

// A literal in the named slot is still a literal, so it folds as it always did.
func TestNamedLiteralArgumentStillFolds(t *testing.T) {
	src := listHeader + `Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top
    Count: 3
Reveal: stdout
`
	pipe, rewrites := resolveProgram(t, src, true)
	if !firedContaining(rewrites, "Quickselect") {
		t.Fatalf("expected the quickselect rewrite for a literal Count:, got %v", rewrites)
	}
	for _, n := range pipe.Nodes {
		if n.Prim != "PartialSelect" {
			continue
		}
		if k, ok := n.Meta["k"].(int64); !ok || k != 3 {
			t.Fatalf("a folded literal should stay literal on the fused node: %#v", n.Meta)
		}
	}
}

// The passes that cannot carry a measured value must refuse it. Their
// primitives take only literals today, so the guard is exercised directly —
// which is the point of testing it here rather than through a program: it has
// to hold for the *next* primitive that grows a measured argument, before any
// program can be written against it.
func TestMeasuredGuardRefusesUnknownArguments(t *testing.T) {
	lit := &ir.Node{Prim: "Take Item", Meta: map[string]any{"index": 0}}
	if hasMeasuredArg(lit) {
		t.Fatal("a literal-only node is not measured")
	}
	measured := &ir.Node{Prim: "Take Item", Meta: map[string]any{
		"indexExpr": &ast.Lambda{},
		"indexFn":   ir.MeasureFn(func(ir.Value) (int64, error) { return 0, nil }),
	}}
	if !hasMeasuredArg(measured) {
		t.Fatal("a node carrying a measured argument must be recognized whatever the key")
	}
	// readArg is the other half: a node with neither form must report "no
	// argument" rather than a plausible zero.
	if _, ok := readArg(&ir.Node{Prim: "SelectTopK", Meta: map[string]any{}}, "k"); ok {
		t.Fatal("a missing argument must not read as a value")
	}
	if a, ok := readArg(lit, "index"); ok || a.lit != 0 {
		// index is an int, not an int64: readArg deliberately only accepts the
		// int64 shape measured arguments share, so this must not half-match.
		t.Fatalf("readArg must not accept a differently-typed literal: %v %v", a, ok)
	}
}

func firedContaining(rewrites []Rewrite, substr string) bool {
	for _, r := range rewrites {
		if strings.Contains(r.Message, substr) {
			return true
		}
	}
	return false
}

func hasPrim(p *ir.Pipeline, prim string) bool {
	for _, n := range p.Nodes {
		if n.Prim == prim {
			return true
		}
	}
	return false
}

// The early-exit search rewrite carries a measured start for the same reason
// the other two carry theirs: the fused node takes the start as data.
func TestMeasuredSearchStartFuses(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Lines
Channeled Energy: Convert To Grid
Domain Expansion: BFS
    Row: (g) -> 0
    Col: (g) -> 0
    Using: (c) -> c = "."
Cursed Technique: Apply
    Using: (g) -> at(g, 2, 2)
Reveal: stdout
`
	pipe, rewrites := resolveProgram(t, src, true)
	if !firedContaining(rewrites, "early-exit search") {
		t.Fatalf("the early-exit rewrite should carry a measured start, got %v", rewrites)
	}
	if !hasPrim(pipe, "SearchTarget") {
		t.Fatal("expected a fused SearchTarget node")
	}
	if got := strings.TrimSpace(sameBothWays(t, src, "...\n.#.\n...")); got != "4" {
		t.Fatalf("measured early-exit search: got %q", got)
	}
}
