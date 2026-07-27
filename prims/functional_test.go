package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// ---------------------------------------------------------------------------
// Reduce
// ---------------------------------------------------------------------------

func TestReduceSeedlessCombines(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Reduce\n" +
		"    Using: (a, b) -> a + b\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if v.(int64) != 15 {
		t.Fatalf("reduce sum: got %v want 15", v)
	}
	// A single element never calls the lambda — it *is* the answer.
	v, _ = runPipeline(t, src, "7")
	if v.(int64) != 7 {
		t.Fatalf("reduce of one element: got %v want 7", v)
	}
}

func TestReduceIsLeftAssociative(t *testing.T) {
	// A non-commutative lambda pins the fold direction: ((1*10+2)*10+3)*10+4.
	src := intsPrelude +
		"Maximum Technique: Reduce\n" +
		"    Using: (a, b) -> a * 10 + b\n"
	v, _ := runPipeline(t, src, "1,2,3,4")
	if v.(int64) != 1234 {
		t.Fatalf("left fold: got %v want 1234", v)
	}
}

func TestReduceOverACompositeType(t *testing.T) {
	// The seedless form works over any element type — the whole point of it
	// next to Fold, whose Seed: is an Int or Text literal and so can never
	// start an accumulator that is a point.
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> point(x, 1)\n" +
		"Maximum Technique: Reduce\n" +
		"    Using: (a, b) -> padd(a, b)\n"
	v, _ := runPipeline(t, src, "1,2,3,4")
	if got := ir.FormatValue(v); got != "[10, 4]" {
		t.Fatalf("reduce points: got %s want [10, 4]", got)
	}
}

func TestReduceEmptyListIsRuntimeError(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Filter\n" +
		"    Using: (x) -> x > 100\n" +
		"Maximum Technique: Reduce\n" +
		"    Using: (a, b) -> a + b\n"
	_, err := runErr(t, src, "1,2,3")
	if err == nil || !strings.Contains(err.Error(), "empty list") {
		t.Fatalf("expected an empty-list error, got %v", err)
	}
}

func TestReduceLambdaMustReturnElementType(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Reduce\n" +
		"    Using: (a, b) -> a > b\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "must return the element type") {
		t.Fatalf("expected an accumulator-type error, got %v", err)
	}
}

func TestReduceNeedsTwoParameters(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Reduce\n" +
		"    Using: (a) -> a\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "2 parameter") {
		t.Fatalf("expected an arity error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Scan
// ---------------------------------------------------------------------------

func TestScanSeedlessRunningTotals(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Scan\n" +
		"    Using: (a, b) -> a + b\n"
	v, _ := runPipeline(t, src, "1,2,3,4")
	if got := ir.FormatValue(v); got != "[1, 3, 6, 10]" {
		t.Fatalf("seedless scan: got %s want [1, 3, 6, 10]", got)
	}
}

func TestScanSeededDoesNotReEmitTheSeed(t *testing.T) {
	// One result per input element: the seed is where the fold starts, not a
	// result of its own, so the output stays aligned with the input.
	src := intsPrelude +
		"Cursed Technique: Scan\n" +
		"    Seed: 100\n" +
		"    Using: (acc, x) -> acc + x\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[101, 103, 106]" {
		t.Fatalf("seeded scan: got %s want [101, 103, 106]", got)
	}
}

func TestScanAgreesWithFoldOnItsLastElement(t *testing.T) {
	const input = "3,1,4,1,5,9"
	scanned, _ := runPipeline(t, intsPrelude+
		"Cursed Technique: Scan\n"+
		"    Seed: 0\n"+
		"    Using: (acc, x) -> acc * 2 + x\n"+
		"Cursed Technique: Apply\n"+
		"    Using: (xs) -> last(xs)\n", input)
	folded, _ := runPipeline(t, intsPrelude+
		"Maximum Technique: Fold\n"+
		"    Seed: 0\n"+
		"    Using: (acc, x) -> acc * 2 + x\n", input)
	if scanned != folded {
		t.Fatalf("last of Scan (%v) != Fold (%v)", scanned, folded)
	}
}

func TestScanOfEmptyListIsEmpty(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Filter\n" +
		"    Using: (x) -> x > 100\n" +
		"Cursed Technique: Scan\n" +
		"    Using: (a, b) -> a + b\n"
	v, err := runErr(t, src, "1,2,3")
	if err != nil {
		t.Fatalf("scan of an empty list must not error: %v", err)
	}
	if got := ir.FormatValue(v); got != "[]" {
		t.Fatalf("scan of empty: got %s want []", got)
	}
}

func TestScanOverACompositeType(t *testing.T) {
	// Seedless, so the accumulator is the element type: a running sum of
	// points — the "walk a list of moves and record every position" shape.
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> point(x, 1)\n" +
		"Cursed Technique: Scan\n" +
		"    Using: (a, b) -> padd(a, b)\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[[1, 1], [3, 2], [6, 3]]" {
		t.Fatalf("point scan: got %s", got)
	}
}

func TestScanSeedFixesTheAccumulatorType(t *testing.T) {
	// Seed: 0 makes the accumulator an Int, so a Text-returning lambda is a
	// resolution error rather than a runtime surprise.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Cursed Technique: Scan\n" +
		"    Seed: 0\n" +
		"    Using: (acc, s) -> s\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "accumulator type") {
		t.Fatalf("expected an accumulator-type error, got %v", err)
	}
}

func TestScanChangesTypeWithASeed(t *testing.T) {
	// Scanning List<Text> with an Int seed: the result is List<Int>.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Cursed Technique: Scan\n" +
		"    Seed: 0\n" +
		"    Using: (acc, s) -> acc + toint(s)\n"
	v, _ := runPipeline(t, src, "2,3,1")
	if got := ir.FormatValue(v); got != "[2, 5, 6]" {
		t.Fatalf("running total over Text: got %s want [2, 5, 6]", got)
	}
}

// ---------------------------------------------------------------------------
// Pairs
// ---------------------------------------------------------------------------

func TestPairsAdjacent(t *testing.T) {
	src := intsPrelude + "Cursed Technique: Pairs\n"
	v, _ := runPipeline(t, src, "1,2,3,4")
	if got := ir.FormatValue(v); got != "[[1, 2], [2, 3], [3, 4]]" {
		t.Fatalf("pairs: got %s", got)
	}
}

func TestPairsShortListsYieldNone(t *testing.T) {
	src := intsPrelude + "Cursed Technique: Pairs\n" + "Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "1")
	if v.(int64) != 0 {
		t.Fatalf("pairs of one element: got %v want 0", v)
	}
}

func TestPairsFeedsPointAccessors(t *testing.T) {
	// Pairs of Ints are points, so prow/pcol read the two sides — the 2021 D1
	// "count the increases" idiom without a Window.
	src := intsPrelude +
		"Cursed Technique: Pairs\n" +
		"Maximum Technique: Count Matching\n" +
		"    Using: (p) -> pcol(p) > prow(p)\n"
	v, _ := runPipeline(t, src, "199,200,208,210,200,207,240,269,260,263")
	if v.(int64) != 7 {
		t.Fatalf("2021 D1 increases via Pairs: got %v want 7", v)
	}
}

func TestPairsOverText(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Cursed Technique: Pairs\n"
	v, _ := runPipeline(t, src, "a,b,c")
	if got := ir.FormatValue(v); got != "[[a, b], [b, c]]" {
		t.Fatalf("text pairs: got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Keyword inference — the three must resolve without their themed keyword,
// and must not have made `All Pairs` ambiguous.
// ---------------------------------------------------------------------------

func TestFunctionalPrimitivesInferTheirKeyword(t *testing.T) {
	src := "stdin\n" +
		"Split Text by \",\"\n" +
		"Convert List to Integers\n" +
		"Pairs\n" +
		"Map Each\n" +
		"    Using: (p) -> pcol(p) - prow(p)\n" +
		"Scan\n" +
		"    Using: (a, b) -> a + b\n" +
		"Reduce\n" +
		"    Using: (a, b) -> a + b\n"
	// Deltas 1,1,1 → scan 1,2,3 → reduce 6.
	v, _ := runPipeline(t, src, "1,2,3,4")
	if v.(int64) != 6 {
		t.Fatalf("keyword-free pipeline: got %v want 6", v)
	}
}

func TestAllPairsStaysUnambiguousWithoutAKeyword(t *testing.T) {
	src := "stdin\n" +
		"Split Text by \",\"\n" +
		"Convert List to Integers\n" +
		"All Pairs\n" +
		"    Mode: Count\n" +
		"    Using: (a, b) -> a + b = 5\n"
	v, err := runErr(t, src, "1,2,3,4")
	if err != nil {
		t.Fatalf("bare All Pairs must still infer Domain Expansion: %v", err)
	}
	if v.(int64) != 2 { // (1,4) and (2,3)
		t.Fatalf("all pairs summing to 5: got %v want 2", v)
	}
}

func TestShikigamiMayNotBeNamedAfterTheNewPrimitives(t *testing.T) {
	for _, name := range []string{"Reduce", "Scan", "Pairs"} {
		src := "Shikigami \"" + name + "\"\n" +
			"    Cursed Technique: Reverse\n" +
			"Cursed Energy: stdin\n"
		_, err := resolveSrc(t, src)
		if err == nil || !strings.Contains(err.Error(), "named after") {
			t.Fatalf("Shikigami %q should be rejected, got %v", name, err)
		}
	}
}
