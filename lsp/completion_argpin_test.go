package lsp

import (
	"slices"
	"testing"

	"domain/prims"
)

// argLabels is a curated subset of the language's named arguments, so it cannot
// be generated — but every entry in it must still be an argument the vocabulary
// actually reads, or the completion offers a key that nothing will ever look at.
// (prims.ArgNames() has its own drift test against the registry's call sites.)
func TestArgLabelsAreRealArguments(t *testing.T) {
	known := prims.ArgNames()
	for _, a := range argLabels {
		if !slices.Contains(known, a.Label) {
			t.Errorf("completion offers %q, which prims.ArgNames() does not list — "+
				"either the argument was renamed or this entry was invented", a.Label)
		}
	}
}

// Mode: values are validated per primitive, so this pins only what a missing
// entry actually costs: the value the user is most likely to be typing. Try is
// here because it was the one the list missed.
func TestModeValuesCoverEveryPrimitivesModes(t *testing.T) {
	for _, want := range []string{
		"One", "Each", "Try", "Scan", // Match Pattern
		"Filter", "Count", "First", "Map", // All Pairs, Combinations
		"Collect", "Distances", "Steps", "Cheapest", "Costs", "Tally", // Explore
		"Sum", "Max", "Min", "Product", // Sliding Reduce
		"Right", "Left", "Half", "Horizontal", "Vertical", // Rotate/Flip Grid
	} {
		if !slices.Contains(modeValues, want) {
			t.Errorf("Mode: completion does not offer %q", want)
		}
	}
}
