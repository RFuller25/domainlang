package optimizer

import (
	"math/rand"
	"reflect"
	"runtime"
	"sort"
	"strings"
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

// Every pass declares the name of the function it holds.
//
// The name is written out in the passes table rather than derived at run time,
// so that Optimize stays plain and a Rewrite's Pass is a constant a reader can
// grep for. This test is what keeps the two halves of that duplication from
// drifting: it does the reflection the production path deliberately does not.
func TestPassNamesMatchFunctions(t *testing.T) {
	seen := map[string]bool{}
	for i, ps := range passes {
		if ps.name == "" {
			t.Fatalf("passes[%d] has no name", i)
		}
		if seen[ps.name] {
			t.Fatalf("passes[%d]: duplicate name %q", i, ps.name)
		}
		seen[ps.name] = true

		full := runtime.FuncForPC(reflect.ValueOf(ps.run).Pointer()).Name()
		// "domain/optimizer.fuseSortThenTopK" — compare the last segment.
		got := full[strings.LastIndex(full, ".")+1:]
		if got != ps.name {
			t.Errorf("passes[%d]: declared %q but holds %q", i, ps.name, got)
		}
	}
}

// Every rewrite that reaches a caller carries the pass that produced it.
//
// Passes built on the shared rewritePairs helper construct their Rewrite deep
// inside code that cannot know which pass is calling it, which is exactly why
// Optimize stamps the name on the way out. This asserts the stamp covers the
// whole surface, including markLinearAccumulators, which runs outside the
// cascade loop.
func TestEveryRewriteNamesItsPass(t *testing.T) {
	// A program with something for several different passes to find: a
	// descending sort feeding a Top K, and an identity Map Each.
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
	}}
	rewrites := Optimize(pipe, true)
	if len(rewrites) == 0 {
		t.Fatal("expected at least one rewrite to check")
	}
	known := map[string]bool{"markLinearAccumulators": true}
	for _, ps := range passes {
		known[ps.name] = true
	}
	for i, r := range rewrites {
		if r.Pass == "" {
			t.Errorf("rewrite %d (%q) has no Pass", i, r.Message)
			continue
		}
		if !known[r.Pass] {
			t.Errorf("rewrite %d names unknown pass %q", i, r.Pass)
		}
	}
}

// The zero Schedule must be exactly what every ordinary run and build gets,
// or mahoraga's baseline would not be the thing it claims to be measuring.
func TestZeroScheduleIsTheDefaultPipeline(t *testing.T) {
	build := func() *ir.Pipeline {
		return &ir.Pipeline{Nodes: []*ir.Node{
			{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
			{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
		}}
	}
	a, b := build(), build()
	viaOptimize := Optimize(a, true)
	viaSchedule := OptimizeWith(b, Schedule{})

	if len(viaOptimize) != len(viaSchedule) {
		t.Fatalf("rewrite counts differ: %d vs %d", len(viaOptimize), len(viaSchedule))
	}
	for i := range viaOptimize {
		if viaOptimize[i] != viaSchedule[i] {
			t.Errorf("rewrite %d differs:\n  Optimize:     %+v\n  OptimizeWith: %+v",
				i, viaOptimize[i], viaSchedule[i])
		}
	}
	if len(a.Nodes) != len(b.Nodes) {
		t.Errorf("pipelines differ: %d nodes vs %d", len(a.Nodes), len(b.Nodes))
	}
}

// Withholding a pass must withhold exactly that pass — this is the whole
// mechanism behind ablation, and a schedule that quietly ran everything would
// make every ablation measurement a lie.
func TestScheduleRunsOnlyTheNamedPasses(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
	}}
	// Everything except the pass that fuses this pair.
	var without []string
	for _, name := range PassNames() {
		if name != "fuseSortThenTopK" {
			without = append(without, name)
		}
	}
	rewrites := OptimizeWith(pipe, Schedule{Passes: without})
	for _, r := range rewrites {
		if r.Pass == "fuseSortThenTopK" {
			t.Fatalf("a pass excluded from the schedule ran anyway")
		}
	}
	if len(pipe.Nodes) != 2 {
		t.Errorf("the pair fused without the pass that fuses it: %d nodes", len(pipe.Nodes))
	}

	// And with it, the fusion happens — so the ablation measured something.
	pipe2 := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
	}}
	OptimizeWith(pipe2, Schedule{Passes: PassNames()})
	if len(pipe2.Nodes) != 1 {
		t.Errorf("the full schedule did not fuse the pair: %d nodes", len(pipe2.Nodes))
	}
}

// An empty (non-nil) pass list is a meaningful request, not a mistake: it
// isolates what the post-cascade linear pass alone is worth.
func TestEmptyScheduleRunsNoCascadePasses(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
	}}
	rewrites := OptimizeWith(pipe, Schedule{Passes: []string{}})
	for _, r := range rewrites {
		if r.Pass != LinearPassName {
			t.Errorf("an empty schedule ran %q", r.Pass)
		}
	}
	if len(pipe.Nodes) != 2 {
		t.Errorf("an empty schedule rewrote the pipeline: %d nodes", len(pipe.Nodes))
	}
}

func TestScheduleSkipLinear(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int())}}}
	for _, r := range OptimizeWith(pipe, Schedule{SkipLinear: true}) {
		if r.Pass == LinearPassName {
			t.Error("SkipLinear did not stand the linear pass down")
		}
	}
}

// A misspelled pass name that silently did nothing would make a measurement
// mean the opposite of what it appeared to, so callers can check first.
func TestUnknownPassesAreReportable(t *testing.T) {
	s := Schedule{Passes: []string{"fuseMapMap", "fuseNothingAtAll", "elideRedundantSort", "typo"}}
	bad := s.UnknownPasses()
	if len(bad) != 2 || bad[0] != "fuseNothingAtAll" || bad[1] != "typo" {
		t.Errorf("UnknownPasses = %v, want the two names that are not passes", bad)
	}
	if got := (Schedule{}).UnknownPasses(); got != nil {
		t.Errorf("the default schedule reported unknown passes: %v", got)
	}
	if got := (Schedule{Passes: PassNames()}).UnknownPasses(); len(got) != 0 {
		t.Errorf("the full pass list reported unknown passes: %v", got)
	}
}

func TestPassNamesMatchTheTable(t *testing.T) {
	names := PassNames()
	if len(names) != len(passes) {
		t.Fatalf("PassNames returned %d names for %d passes", len(names), len(passes))
	}
	for i, ps := range passes {
		if names[i] != ps.name {
			t.Errorf("PassNames[%d] = %q want %q", i, names[i], ps.name)
		}
	}
	// It must hand back a copy; a caller shuffling the order to search it
	// must not reorder the compiler's own pass list.
	names[0] = "clobbered"
	if PassNames()[0] == "clobbered" {
		t.Error("PassNames exposes the underlying table")
	}
}

// The rounds cap is searchable too, and has to actually bound the cascade.
func TestScheduleMaxRounds(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "Sort", In: ir.List(ir.Int()), Out: ir.List(ir.Int()), Meta: map[string]any{"desc": true}},
		{Prim: "SelectTopK", In: ir.List(ir.Int()), Out: ir.Int(), Meta: map[string]any{"k": int64(3), "sum": true}},
	}}
	// One round is enough for this fusion, so it still happens.
	OptimizeWith(pipe, Schedule{MaxRounds: 1})
	if len(pipe.Nodes) != 1 {
		t.Errorf("one round did not apply the fusion: %d nodes", len(pipe.Nodes))
	}
}
