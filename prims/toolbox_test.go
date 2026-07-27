package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

func TestExtractIntegersFromText(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Extract Integers\n"
	v, _ := runPipeline(t, src, "move 12 from -3 to 5, x=-7")
	want := []int64{12, -3, 5, -7}
	got, err := ir.AsIntSlice(v)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestExtractIntegersDigitAdjacentMinusIsSeparator(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Extract Integers\n"
	v, _ := runPipeline(t, src, "36-92")
	got, _ := ir.AsIntSlice(v)
	if len(got) != 2 || got[0] != 36 || got[1] != 92 {
		t.Fatalf("36-92 should mine [36 92], got %v", got)
	}
}

func TestExtractIntegersEachLine(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Extract Integers\n" +
		"Maximum Technique: Sum Each Group\n"
	v, _ := runPipeline(t, src, "a 1 b 2\nnothing\n3 and -4")
	got, _ := ir.AsIntSlice(v)
	if len(got) != 3 || got[0] != 3 || got[1] != 0 || got[2] != -1 {
		t.Fatalf("per-line sums: got %v want [3 0 -1]", got)
	}
}

func TestExtractIntegersRejectsWrongInput(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Extract Integers\n"
	_, err := runErr(t, src, "1\n2")
	if err == nil || !strings.Contains(err.Error(), "Extract Integers expects Text or List<Text>") {
		t.Fatalf("expected a resolve-time type error, got %v", err)
	}
}

func TestSplitFieldsText(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Fields\n"
	v, _ := runPipeline(t, src, "  alpha\tbeta   gamma ")
	xs, _ := ir.AsList(v)
	if len(xs) != 3 || xs[0] != "alpha" || xs[1] != "beta" || xs[2] != "gamma" {
		t.Fatalf("fields: got %v", xs)
	}
}

func TestSplitFieldsEachLine(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Fields\n" +
		"Channeled Energy: Convert Each List to Integers\n" +
		"Maximum Technique: Sum Each Group\n"
	v, _ := runPipeline(t, src, "1 2 3\n 4   5 ")
	got, _ := ir.AsIntSlice(v)
	if len(got) != 2 || got[0] != 6 || got[1] != 9 {
		t.Fatalf("got %v want [6 9]", got)
	}
}

func TestConvertToSetAndContains(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Set\n"
	v, _ := runPipeline(t, src, "a,b,a,c,b")
	s, ok := v.(*ir.SetValue)
	if !ok {
		t.Fatalf("expected Set, got %T", v)
	}
	if s.Len() != 3 {
		t.Fatalf("set size: got %d want 3", s.Len())
	}
	if !s.Has("a") || s.Has("z") {
		t.Fatal("membership wrong")
	}
	// Insertion order is preserved for rendering.
	if got := ir.FormatValue(s); got != "{a, b, c}" {
		t.Fatalf("render: got %q", got)
	}
}

func TestConvertToSetRequiresKeyable(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \",\"\n" +
		"Channeled Energy: Convert To Set\n"
	_, err := runErr(t, src, "a,b\nc,d")
	// List<Text> elements stay unkeyable even with composite keys (M25):
	// lists lower to Go slices, which cannot be map keys.
	if err == nil || !strings.Contains(err.Error(), "keyable keys/elements") {
		t.Fatalf("expected a keyable-element error, got %v", err)
	}
}

func TestMergeRangesTuples(t *testing.T) {
	// Pairs of ints via positional Match Pattern (tuple-typed).
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{int}-{int}\"\n" +
		"Maximum Technique: Merge Ranges\n"
	v, _ := runPipeline(t, src, "5-7\n1-3\n2-4\n10-12\n8-8")
	// 1-3 + 2-4 merge to 1-4; 5-7 is adjacent to 8-8 (and 8-8 to 10-12? no:
	// 8+1 < 10) — expected: [1,4] [5,8] [10,12]... 4+1=5 so 1-4 and 5-7 are
	// adjacent too: everything up to 8 coalesces. Expected: [1,8] [10,12].
	if got := ir.FormatValue(v); got != "[[1, 8], [10, 12]]" {
		t.Fatalf("merged: got %s", got)
	}
}

func TestMergeRangesRecords(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{lo:int}..{hi:int}\"\n" +
		"Maximum Technique: Merge Ranges\n"
	v, _ := runPipeline(t, src, "1..2\n7..9\n3..3")
	if got := ir.FormatValue(v); got != "[{lo: 1, hi: 3}, {hi: 9, lo: 7}]" &&
		got != "[{lo: 1, hi: 3}, {lo: 7, hi: 9}]" {
		t.Fatalf("merged records: got %s", got)
	}
}

func TestMergeRangesInvertedRangeErrors(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{int}-{int}\"\n" +
		"Maximum Technique: Merge Ranges\n"
	_, err := runErr(t, src, "9-3")
	if err == nil || !strings.Contains(err.Error(), "inverted") {
		t.Fatalf("expected an inverted-range error, got %v", err)
	}
}

func TestMergeRangesNearMaxInt64NoOverflow(t *testing.T) {
	// Regression test: the adjacency check used to compute
	// merged[n-1].hi+1, which overflows (wraps to MinInt64) when hi is
	// math.MaxInt64, wrongly making the ranges look non-adjacent. A range
	// fully contained within [1, MaxInt64] must still be absorbed into it.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{int} {int}\"\n" +
		"Maximum Technique: Merge Ranges\n"
	v, _ := runPipeline(t, src, "1 9223372036854775807\n5 10")
	if got := ir.FormatValue(v); got != "[[1, 9223372036854775807]]" {
		t.Fatalf("merged near MaxInt64: got %s want [[1, 9223372036854775807]]", got)
	}
}

func TestMergeRangesRejectsWrongElement(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Maximum Technique: Merge Ranges\n"
	_, err := runErr(t, src, "x")
	if err == nil || !strings.Contains(err.Error(), "Merge Ranges expects") {
		t.Fatalf("expected a resolve-time type error, got %v", err)
	}
}

func TestPermutations(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Domain Expansion: Permutations\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "a,b,c,d")
	if v.(int64) != 24 {
		t.Fatalf("4! = 24, got %v", v)
	}
}

func TestPermutationsOrderAndContent(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Domain Expansion: Permutations\n"
	v, _ := runPipeline(t, src, "a,b,c")
	if got := ir.FormatValue(v); got !=
		"[[a, b, c], [a, c, b], [b, a, c], [b, c, a], [c, a, b], [c, b, a]]" {
		t.Fatalf("permutations: got %s", got)
	}
}

// Permutations is unbounded by default: a 10-element input used to be
// refused by a hard-coded ceiling of 9, though 3.6M orderings are perfectly
// computable. The ceiling still exists as an opt-in.
func TestPermutationsUnboundedByDefault(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Domain Expansion: Permutations\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "a,b,c,d,e,f,g,h,i,j") // 10 elements
	if v.(int64) != 3628800 {
		t.Fatalf("10! = 3628800, got %v", v)
	}
}

func TestPermutationsBoundWhenConfigured(t *testing.T) {
	old := MaxPermutationInput
	MaxPermutationInput = 9
	defer func() { MaxPermutationInput = old }()
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Domain Expansion: Permutations\n"
	_, err := runErr(t, src, "a,b,c,d,e,f,g,h,i,j") // 10 elements
	if err == nil || !strings.Contains(err.Error(), "refusing to permute") {
		t.Fatalf("expected the n! bound error, got %v", err)
	}
}

func TestSubsets(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Domain Expansion: Subsets\n"
	v, _ := runPipeline(t, src, "a,b")
	if got := ir.FormatValue(v); got != "[[], [a], [b], [a, b]]" {
		t.Fatalf("subsets: got %s", got)
	}
}

func TestSubsetsCountAndBound(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Domain Expansion: Subsets\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if v.(int64) != 32 {
		t.Fatalf("2^5 = 32, got %v", v)
	}
	// 17 elements is fine now; the old hard ceiling of 16 refused it.
	long := strings.Repeat("1,", 16) + "1"
	v, _ = runPipeline(t, src, long)
	if v.(int64) != 131072 {
		t.Fatalf("2^17 = 131072, got %v", v)
	}
}

func TestSubsetsBoundWhenConfigured(t *testing.T) {
	old := MaxSubsetInput
	MaxSubsetInput = 16
	defer func() { MaxSubsetInput = old }()
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Domain Expansion: Subsets\n"
	long := strings.Repeat("1,", 16) + "1" // 17 elements
	_, err := runErr(t, src, long)
	if err == nil || !strings.Contains(err.Error(), "refusing the power set") {
		t.Fatalf("expected the 2^n bound error, got %v", err)
	}
}

func TestFindCells(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Grid\n" +
		"Cursed Technique: Find Cells\n" +
		"    Using: (c) -> c = \"X\"\n"
	v, _ := runPipeline(t, src, "X.O\n.XX")
	if got := ir.FormatValue(v); got != "[[0, 0], [1, 1], [1, 2]]" {
		t.Fatalf("find cells: got %s", got)
	}
}

func TestFindCellsFeedsPointBuiltins(t *testing.T) {
	// Find both markers, then measure their Manhattan distance: the
	// Find Cells output tuples are points the expression layer understands.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Grid\n" +
		"Cursed Technique: Find Cells\n" +
		"    Using: (c) -> c = \"X\"\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (ps) -> manhattan(first(ps), last(ps))\n"
	v, _ := runPipeline(t, src, "X..\n...\n..X")
	if v.(int64) != 4 {
		t.Fatalf("manhattan between corners: got %v want 4", v)
	}
}
