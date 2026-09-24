package diag

import (
	"strings"
	"testing"
)

// The compiler's opinions: a perf warning for each cost it found and could not
// remove itself, and silence for everything it could.
func TestOpinions(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct {
		name string
		body string
		want string // a substring of the one perf warning expected; "" for none
		help string // a substring its help must carry
	}{
		{"invariant work that can fail stays, and is reported",
			"Cursed Technique: Map Each\n    Consider all Of Itself\n    Using: (x) -> x + max(all)\n",
			"`max(all)` does not depend on the element", "Consider once As max(all)"},
		{"invariant work the compiler hoisted is not reported",
			"Cursed Technique: Map Each\n    Consider all Of Itself\n    Using: (x) -> x + sum(sort(all))\n",
			"", ""},
		{"a membership the compiler made a set is not reported",
			"Cursed Technique: Filter\n    Consider all Of Itself\n    Using: (x) -> contains(all, x + 1)\n",
			"", ""},
		{"first of a sorted list is min",
			"Cursed Technique: Apply\n    Using: (xs) -> first(sort(xs))\n",
			"sorts the whole list to read the smallest element", "min(xs)"},
		{"item 0 of a sorted list is min too",
			"Cursed Technique: Apply\n    Using: (xs) -> item(sort(xs), 0)\n",
			"smallest element", "min(xs)"},
		{"last of a sorted list is max",
			"Cursed Technique: Apply\n    Using: (xs) -> last(sort(xs))\n",
			"largest element", "max(xs)"},
		{"first of a reversed list is last",
			"Cursed Technique: Apply\n    Using: (xs) -> first(reverse(xs))\n",
			"copies the whole list, reversed", "last(xs)"},
		{"min itself is fine",
			"Cursed Technique: Apply\n    Using: (xs) -> min(xs)\n",
			"", ""},
		{"a pair scan whose target is not a literal stays quadratic",
			"Domain Expansion: All Pairs\n    Mode: Count\n    Consider target Of (xs) -> first(xs)\n    Using: (a, b) -> a + b = target\n",
			"tries every combination — O(n²)", "O(n) hash scan"},
		{"a triple scan likewise",
			"Domain Expansion: Combinations 3\n    Mode: Count\n    Consider target Of (xs) -> first(xs)\n    Using: (a, b, c) -> a + b + c = target\n",
			"O(n³)", "O(n²) hash scan"},
		{"a pair scan with a literal target is rewritten, so says nothing",
			"Domain Expansion: All Pairs\n    Mode: Count\n    Using: (a, b) -> a + b = 2020\n",
			"", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := analyze(t, head+c.body+"Reveal: stdout\n")
			var perf []Diagnostic
			for _, d := range r.Diags {
				if d.Severity == Error {
					t.Fatalf("the program does not analyze cleanly: %s", d.Msg)
				}
				if d.Code == "perf" && d.Severity == Warning {
					perf = append(perf, d)
				}
			}
			if c.want == "" {
				if len(perf) != 0 {
					t.Fatalf("expected no perf warning, got %q", perf[0].Msg)
				}
				return
			}
			if len(perf) != 1 {
				t.Fatalf("expected one perf warning, got %d: %v", len(perf), perf)
			}
			if !strings.Contains(perf[0].Msg, c.want) {
				t.Errorf("message %q does not contain %q", perf[0].Msg, c.want)
			}
			if !strings.Contains(perf[0].Help, c.help) {
				t.Errorf("help %q does not contain %q", perf[0].Help, c.help)
			}
			// Every one says why the compiler did not make the change itself.
			if len(perf[0].Notes) == 0 {
				t.Error("no note saying why the compiler left it")
			}
		})
	}
}

// The patterns the optimizer fuses on its own are reported for clarity, not as
// a cost: nothing is left to pay.
func TestHandledPatternsAreNotPerf(t *testing.T) {
	r := analyze(t, "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"+
		"Domain Expansion: Quicksort\nReverse Cursed Technique: Reverse\nReveal: stdout\n")
	for _, d := range r.Diags {
		if d.Code == "perf" {
			t.Errorf("a pattern the optimizer handles is reported as a cost: %q", d.Msg)
		}
	}
	if diagWith(r, Hint, "Sort followed by Reverse") == nil {
		t.Error("the clarity hint is gone")
	}
}
