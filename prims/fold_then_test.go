package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

const foldThenBindings = intsPrelude +
	"Maximum Technique: Fold\n" +
	"    Consider depth As 0\n" +
	"    Consider dist As 0\n" +
	"    Seed: 0\n" +
	"    Using: (acc, x) -> if x > 0 then (dist := dist + x) else (depth := depth - x)\n" +
	"    Then: (acc) -> depth * dist\n"

// The motivating program: Then reads the stage bindings the body updated.
func TestFoldThenReadsStageBindings(t *testing.T) {
	v, _ := runPipeline(t, foldThenBindings, "5,-3,2,-4")
	if got := ir.FormatValue(v); got != "49" {
		t.Fatalf("got %s, want 49", got)
	}
}

func TestFoldThenAppliesToFinalAccumulator(t *testing.T) {
	src := intsPrelude + "Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x\n    Then: (acc) -> acc * 10\n"
	if v, _ := runPipeline(t, src, "1,2,3"); ir.FormatValue(v) != "60" {
		t.Fatalf("got %s", ir.FormatValue(v))
	}
}

// Then changes the stage's type to the lambda's.
func TestFoldThenChangesType(t *testing.T) {
	src := intsPrelude + "Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x\n    Then: (acc) -> text(acc * 2)\n"
	if v, _ := runPipeline(t, src, "1,2,3"); ir.FormatValue(v) != "12" {
		t.Fatalf("got %s", ir.FormatValue(v))
	}
}

func TestFoldThenOnEmptyListAppliesToSeed(t *testing.T) {
	src := intsPrelude + "Cursed Technique: Filter\n    Using: (x) -> x > 100\n" +
		"Maximum Technique: Fold\n    Seed: 7\n    Using: (acc, x) -> acc + x\n    Then: (acc) -> acc * 2\n"
	if v, _ := runPipeline(t, src, "1,2,3"); ir.FormatValue(v) != "14" {
		t.Fatalf("got %s", ir.FormatValue(v))
	}
}

func TestFoldThenErrors(t *testing.T) {
	base := intsPrelude + "Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x\n"
	for _, c := range []struct{ then, want string }{
		{"    Then: (a, b) -> a\n", "Fold Then: lambda must take 1 parameter"},
		{"    Then: 5\n", "Fold Then: must be a lambda"},
		{"    Then: (acc) -> acc + \"x\"\n", "Fold Then:"},
	} {
		_, err := resolveSrc(t, base+c.then)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: expected %q, got %v", c.then, c.want, err)
		}
	}
}
