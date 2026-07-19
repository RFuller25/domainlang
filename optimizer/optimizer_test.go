package optimizer

import (
	"math/rand"
	"sort"
	"testing"

	"domain/ir"
)

// naiveTopKSum sorts fully, takes the first k, and sums — the oracle the
// PartialSelect rewrite must match exactly.
func naiveTopKSum(xs []int64, k int, desc bool) int64 {
	a := append([]int64(nil), xs...)
	if desc {
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	} else {
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	}
	if k > len(a) {
		k = len(a)
	}
	var s int64
	for _, x := range a[:k] {
		s += x
	}
	return s
}

func naiveTopKList(xs []int64, k int, desc bool) []int64 {
	a := append([]int64(nil), xs...)
	if desc {
		sort.Slice(a, func(i, j int) bool { return a[i] > a[j] })
	} else {
		sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	}
	if k > len(a) {
		k = len(a)
	}
	return a[:k]
}

func equalSlices(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTopKMatchesNaiveOracle is the correctness proof for the rewrite: across
// many random inputs, PartialSelect's TopK must produce identical output to the
// naive sort+take, for both list and sum forms.
func TestTopKMatchesNaiveOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 2000; iter++ {
		n := rng.Intn(30)
		xs := make([]int64, n)
		for i := range xs {
			xs[i] = int64(rng.Intn(21) - 10) // include duplicates and negatives
		}
		k := rng.Intn(n + 3) // sometimes k > len
		for _, desc := range []bool{true, false} {
			got := TopK(xs, k, desc)
			wantList := naiveTopKList(xs, k, desc)
			if !equalSlices(got, wantList) {
				t.Fatalf("iter %d: TopK(%v,%d,desc=%v)=%v want %v", iter, xs, k, desc, got, wantList)
			}
			var sum int64
			for _, x := range got {
				sum += x
			}
			if sum != naiveTopKSum(xs, k, desc) {
				t.Fatalf("iter %d: sum mismatch", iter)
			}
		}
	}
}

func TestFusePassFiresAndDisables(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
		{Prim: "Emit", In: ir.Int(), Out: ir.Int()},
	}}

	rewrites := Optimize(pipe, true)
	if len(rewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %d", len(rewrites))
	}
	if len(pipe.Nodes) != 2 || pipe.Nodes[0].Prim != "PartialSelect" {
		t.Fatalf("expected fused PartialSelect node, got %+v", pipe.Nodes)
	}

	// With optimization disabled the pipeline is untouched.
	pipe2 := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", Meta: map[string]any{"k": int64(3), "sum": true}},
	}}
	if r := Optimize(pipe2, false); len(r) != 0 {
		t.Fatalf("expected no rewrites when disabled, got %d", len(r))
	}
	if len(pipe2.Nodes) != 2 {
		t.Fatalf("disabled optimizer must not modify the pipeline")
	}
}

func TestFusedEvalMatchesNaive(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
	}}
	Optimize(pipe, true)
	in := ir.IntsToValue([]int64{6000, 4000, 11000, 24000, 10000})
	got, err := pipe.Nodes[0].Eval(&ir.Context{}, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.(int64) != 45000 {
		t.Fatalf("fused eval: got %d want 45000", got)
	}
}
