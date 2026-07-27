package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// scalarPrelude reduces stdin to a single Int, the input shape both generators
// start from.
const scalarPrelude = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \",\"\n" +
	"Channeled Energy: Convert List to Integers\n" +
	"Cursed Technique: Take Item 0\n"

// ---------------------------------------------------------------------------
// Iterate
// ---------------------------------------------------------------------------

func TestIterateKeepsTheTrajectory(t *testing.T) {
	src := scalarPrelude +
		"Cursed Technique: Iterate 5\n" +
		"    Using: (x) -> x * 2\n"
	// n results, the starting value not re-emitted — the same rule as Scan.
	v, _ := runPipeline(t, src, "1")
	if got := ir.FormatValue(v); got != "[2, 4, 8, 16, 32]" {
		t.Fatalf("iterate: got %s want [2, 4, 8, 16, 32]", got)
	}
}

func TestIterateZeroStepsIsEmpty(t *testing.T) {
	src := scalarPrelude +
		"Cursed Technique: Iterate 0\n" +
		"    Using: (x) -> x * 2\n"
	v, _ := runPipeline(t, src, "7")
	if got := ir.FormatValue(v); got != "[]" {
		t.Fatalf("iterate 0: got %s want []", got)
	}
}

func TestIterateAgreesWithRepeat(t *testing.T) {
	// The last element of an Iterate is where the equivalent Repeat loop ends.
	iterated, _ := runPipeline(t, scalarPrelude+
		"Cursed Technique: Iterate 4\n"+
		"    Using: (x) -> x * 3 + 1\n"+
		"Cursed Technique: Apply\n"+
		"    Using: (xs) -> last(xs)\n", "2")
	looped, _ := runPipeline(t, scalarPrelude+
		"Simple Domain: Repeat 4\n"+
		"    Cursed Technique: Apply\n"+
		"        Using: (x) -> x * 3 + 1\n", "2")
	if iterated != looped {
		t.Fatalf("last of Iterate (%v) != Repeat (%v)", iterated, looped)
	}
}

func TestIterateOverAList(t *testing.T) {
	// The step only has to preserve its own type, so a whole list can be the
	// iterated state.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Iterate 2\n" +
		"    Using: (xs) -> concat(xs, list(sum(xs)))\n"
	v, _ := runPipeline(t, src, "1,2")
	if got := ir.FormatValue(v); got != "[[1, 2, 3], [1, 2, 3, 6]]" {
		t.Fatalf("iterate over a list: got %s", got)
	}
}

func TestIterateStepMustPreserveItsType(t *testing.T) {
	src := scalarPrelude +
		"Cursed Technique: Iterate 3\n" +
		"    Using: (x) -> x > 1\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "return its own input type") {
		t.Fatalf("expected a step-type error, got %v", err)
	}
}

func TestIterateResolveErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{scalarPrelude + "Cursed Technique: Iterate\n    Using: (x) -> x\n", "requires a step count"},
		{scalarPrelude + "Cursed Technique: Iterate -2\n    Using: (x) -> x\n", "must be >= 0"},
	} {
		if _, err := resolveSrc(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("expected %q, got %v", c.want, err)
		}
	}
}

func TestIterateIsNotTheFixedPointLoop(t *testing.T) {
	// `Iterate Until Fixed Point` is a loop head and must stay one, even
	// without its keyword; `Iterate n` is this generator.
	src := "stdin\n" +
		"Split Text by \",\"\n" +
		"Convert List to Integers\n" +
		"Take Item 0\n" +
		"Iterate Until Fixed Point\n" +
		"    Apply\n" +
		"        Using: (x) -> x / 2\n"
	v, err := runErr(t, src, "40")
	if err != nil {
		t.Fatalf("bare Iterate Until Fixed Point must still be a loop: %v", err)
	}
	if v.(int64) != 0 {
		t.Fatalf("halving to a fixed point: got %v want 0", v)
	}
}

// ---------------------------------------------------------------------------
// Unfold
// ---------------------------------------------------------------------------

func TestUnfoldGrowsAValueIntoAList(t *testing.T) {
	src := scalarPrelude +
		"Cursed Technique: Unfold\n" +
		"    While: (x) -> x > 1\n" +
		"    Using: (x) -> x / 2\n"
	v, _ := runPipeline(t, src, "20")
	if got := ir.FormatValue(v); got != "[20, 10, 5, 2]" {
		t.Fatalf("unfold: got %s want [20, 10, 5, 2]", got)
	}
}

func TestUnfoldEmitsNothingWhenThePredicateStartsFalse(t *testing.T) {
	src := scalarPrelude +
		"Cursed Technique: Unfold\n" +
		"    While: (x) -> x > 100\n" +
		"    Using: (x) -> x / 2\n"
	v, _ := runPipeline(t, src, "5")
	if got := ir.FormatValue(v); got != "[]" {
		t.Fatalf("unfold with a false start: got %s want []", got)
	}
}

func TestUnfoldIsTheDualOfFold(t *testing.T) {
	// Unfold grows the list; folding it back with the inverse step recovers a
	// count of the digits — the round trip that names the pair.
	src := scalarPrelude +
		"Cursed Technique: Unfold\n" +
		"    While: (x) -> x > 0\n" +
		"    Using: (x) -> x / 10\n" +
		"Maximum Technique: Fold\n" +
		"    Seed: 0\n" +
		"    Using: (acc, x) -> acc + 1\n"
	v, _ := runPipeline(t, src, "9057")
	if v.(int64) != 4 {
		t.Fatalf("digit count via Unfold + Fold: got %v want 4", v)
	}
}

func TestUnfoldBoundsANonTerminatingStep(t *testing.T) {
	old := maxLoopIterations
	maxLoopIterations = 100
	defer func() { maxLoopIterations = old }()

	src := scalarPrelude +
		"Cursed Technique: Unfold\n" +
		"    While: (x) -> x > 0\n" +
		"    Using: (x) -> x\n" // never falsifies the predicate
	_, err := runErr(t, src, "1")
	if err == nil || !strings.Contains(err.Error(), "non-terminating") {
		t.Fatalf("expected a runaway-Unfold error, got %v", err)
	}
}

func TestUnfoldResolveErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{scalarPrelude + "Cursed Technique: Unfold\n    Using: (x) -> x / 2\n",
			"requires a While: predicate"},
		{scalarPrelude + "Cursed Technique: Unfold\n    While: (x) -> x\n    Using: (x) -> x / 2\n",
			"must return Bool"},
		{scalarPrelude + "Cursed Technique: Unfold\n    While: (x) -> x > 1\n",
			"requires a Using: lambda"},
		{scalarPrelude + "Cursed Technique: Unfold\n    While: (x) -> x > 1\n    Using: (x) -> x > 1\n",
			"return its own input type"},
	} {
		if _, err := resolveSrc(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("expected %q, got %v", c.want, err)
		}
	}
}
