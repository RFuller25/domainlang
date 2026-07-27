package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

func TestSlidingReduceModes(t *testing.T) {
	cases := []struct{ mode, want string }{
		{"Sum", "[6, 9, 12]"},
		{"Max", "[3, 4, 5]"},
		{"Min", "[1, 2, 3]"},
		{"Product", "[6, 24, 60]"},
	}
	for _, c := range cases {
		src := intsPrelude +
			"Domain Expansion: Sliding Reduce 3\n" +
			"    Mode: " + c.mode + "\n"
		v, _ := runPipeline(t, src, "1,2,3,4,5")
		if got := ir.FormatValue(v); got != c.want {
			t.Fatalf("Sliding Reduce Mode: %s: got %s want %s", c.mode, got, c.want)
		}
	}
}

func TestSlidingReduceDefaultsToSum(t *testing.T) {
	src := intsPrelude + "Domain Expansion: Sliding Reduce 2\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[3, 5]" {
		t.Fatalf("default mode: got %s want [3, 5]", got)
	}
}

func TestSlidingReduceStep(t *testing.T) {
	src := intsPrelude + "Domain Expansion: Sliding Reduce 2 2\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if got := ir.FormatValue(v); got != "[3, 7]" {
		t.Fatalf("step 2: got %s want [3, 7]", got)
	}
}

func TestSlidingReduceAgreesWithWindowThenMap(t *testing.T) {
	// The naive spelling the optimizer fuses into this exact node. The two
	// must agree element for element, whichever way the program reached it.
	const input = "5,1,9,3,7,2,8"
	for _, mode := range []string{"Sum", "Max", "Min"} {
		named, _ := runPipeline(t, intsPrelude+
			"Domain Expansion: Sliding Reduce 3\n"+
			"    Mode: "+mode+"\n", input)
		naive, _ := runPipeline(t, intsPrelude+
			"Cursed Technique: Window 3\n"+
			"Cursed Technique: Map Each\n"+
			"    Using: (w) -> "+strings.ToLower(mode)+"(w)\n", input)
		if ir.FormatValue(named) != ir.FormatValue(naive) {
			t.Fatalf("%s: Sliding Reduce %s != Window + Map Each %s",
				mode, ir.FormatValue(named), ir.FormatValue(naive))
		}
	}
}

func TestSlidingReduceUndersizedListGivesNoWindows(t *testing.T) {
	src := intsPrelude + "Domain Expansion: Sliding Reduce 9\n"
	v, _ := runPipeline(t, src, "1,2")
	if got := ir.FormatValue(v); got != "[]" {
		t.Fatalf("oversized window: got %s want []", got)
	}
}

func TestSlidingReduceResolveErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{intsPrelude + "Domain Expansion: Sliding Reduce\n", "requires a window size"},
		{intsPrelude + "Domain Expansion: Sliding Reduce 0\n", ">= 1"},
		{intsPrelude + "Domain Expansion: Sliding Reduce 2\n    Mode: Median\n", "Mode must be"},
		{"Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n" +
			"Domain Expansion: Sliding Reduce 2\n", "expects input of type"},
	} {
		if _, err := resolveSrc(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("expected %q, got %v", c.want, err)
		}
	}
}

func TestSlidingReduceInfersItsKeyword(t *testing.T) {
	src := "stdin\n" +
		"Split Text by \",\"\n" +
		"Convert List to Integers\n" +
		"Sliding Reduce 2\n" +
		"    Mode: Max\n"
	v, err := runErr(t, src, "1,5,3")
	if err != nil {
		t.Fatalf("bare Sliding Reduce: %v", err)
	}
	if got := ir.FormatValue(v); got != "[5, 5]" {
		t.Fatalf("bare Sliding Reduce: got %s want [5, 5]", got)
	}
}
