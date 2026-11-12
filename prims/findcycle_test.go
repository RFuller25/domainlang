package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// The step form must agree with `Iterate n` + `Find Cycle` for any n long
// enough — that equivalence is its definition.
func TestFindCycleStepFormMatchesIterate(t *testing.T) {
	for _, start := range []string{"0", "1", "3", "6", "7"} {
		step := "mod(n * 3 + 1, 7)"
		want, _ := runPipeline(t, "Cursed Energy: stdin\n"+
			"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n"+
			"Cursed Technique: Iterate 50\n    Using: (n) -> "+step+"\n"+
			"Maximum Technique: Find Cycle\n", start)
		got, _ := runPipeline(t, "Cursed Energy: stdin\n"+
			"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n"+
			"Maximum Technique: Find Cycle\n    Using: (n) -> "+step+"\n", start)
		if ir.FormatValue(got) != ir.FormatValue(want) {
			t.Fatalf("start %s: step form %s, Iterate form %s", start, ir.FormatValue(got), ir.FormatValue(want))
		}
	}
}

func TestFindCycleStepFormIndexing(t *testing.T) {
	// 0 -> 1 -> 0: the trajectory (start excluded) is 1, 0, 1, so it
	// repeats at index 0 with period 2.
	v, _ := runPipeline(t, "Cursed Energy: stdin\n"+
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n"+
		"Maximum Technique: Find Cycle\n    Using: (n) -> 1 - n\n", "0")
	if got := ir.FormatValue(v); got != "[0, 2]" {
		t.Fatalf("got %s", got)
	}
}

func TestFindCycleStepFormLimitAndKey(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
		"Maximum Technique: Find Cycle\n    Using: (n) -> n + 1\n    Limit: 5\n"
	if v, _ := runPipeline(t, src, "0"); ir.FormatValue(v) != "[-1, 0]" {
		t.Fatalf("limit with no repeat: got %s", ir.FormatValue(v))
	}
	// Key: collapses states (n mod 4), so n+1 repeats a key after 4 steps.
	src = "Cursed Energy: stdin\n" +
		"Cursed Technique: Apply\n    Using: (s) -> toint(s)\n" +
		"Maximum Technique: Find Cycle\n    Using: (n) -> n + 1\n    Key: (n) -> mod(n, 4)\n"
	if v, _ := runPipeline(t, src, "0"); ir.FormatValue(v) != "[0, 4]" {
		t.Fatalf("Key: got %s", ir.FormatValue(v))
	}
}

func TestFindCycleStepFormErrors(t *testing.T) {
	head := "Cursed Energy: stdin\nCursed Technique: Apply\n    Using: (s) -> toint(s)\n"
	for _, c := range []struct{ tail, want string }{
		{"Maximum Technique: Find Cycle\n    Using: (n) -> text(n)\n", "must return its own input type"},
		{"Maximum Technique: Find Cycle\n    Using: (n) -> n\n    Key: (n) -> 1.5\n", "keyable"},
		{"Maximum Technique: Find Cycle\n    Using: (n) -> n\n    Limit: -1\n", "Limit"},
		{"Maximum Technique: Find Cycle\n    Key: (n) -> n\n", "Key: and Limit:"},
	} {
		_, err := resolveSrc(t, head+c.tail)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: expected %q, got %v", c.tail, c.want, err)
		}
	}
}
