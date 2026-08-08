package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// The two things Match Pattern could not do: capture a repeated group, and
// survive a file whose lines are not all the same shape.

const linesHead = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n"

func TestRepetitionHoleCapturesAList(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"{id:word}: {vals:int+ sep=\\\",\\\"}\"\n"
	v, _ := runPipeline(t, src, "target: 1,2,3\nother: 40,50")
	if got := ir.FormatValue(v); got != "[{id: target, vals: [1, 2, 3]}, {id: other, vals: [40, 50]}]" {
		t.Fatalf("repetition: got %s", got)
	}
}

// `+` is one-or-more, so a single element still matches — the case a template
// author is most likely to get wrong by assuming a list needs two.
func TestRepetitionMatchesASingleElement(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"{vals:int+ sep=\\\",\\\"}\"\n" +
		"Cursed Technique: Map Each\n    Using: (r) -> length(r.vals)\n"
	v, _ := runPipeline(t, src, "7\n1,2")
	if got := ir.FormatValue(v); got != "[1, 2]" {
		t.Fatalf("single element: got %s, want [1, 2]", got)
	}
}

// Mode: Try keeps the lines that fit and drops the rest, which is what makes a
// file of two shapes parseable at all — one pass per shape.
func TestModeTryKeepsTheLinesThatFit(t *testing.T) {
	const input = "turn on 0,0 through 9,9\ntoggle 1,1 through 2,2\nturn off 3,3 through 4,4"
	toggles := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Try\n" +
		"    Using: \"toggle {a:int},{b:int} through {c:int},{d:int}\"\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, toggles, input)
	if v.(int64) != 1 {
		t.Errorf("toggles: got %v, want 1", v)
	}
	turns := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Try\n" +
		"    Using: \"turn {what:word} {a:int},{b:int} through {c:int},{d:int}\"\n" +
		"Maximum Technique: Count\n"
	v, _ = runPipeline(t, turns, input)
	if v.(int64) != 2 {
		t.Errorf("turns: got %v, want 2", v)
	}
}

// Mode: Each still refuses a line that does not fit. Try is the opt-in, and
// the difference between them is the whole point of having both.
func TestModeEachStillRefusesAMismatch(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"toggle {a:int},{b:int}\"\n" +
		"Reveal: stdout\n"
	_, err := runErr(t, src, "toggle 1,1\nturn on 0,0")
	if err == nil || !strings.Contains(err.Error(), "does not match template") {
		t.Fatalf("expected a mismatch error, got %v", err)
	}
}

// Try drops a line of the wrong *shape*. A line of the right shape whose
// capture then fails to convert is a broken line rather than a different kind
// of line, so it still stops the program — silently skipping it would turn a
// corrupt input into a quietly short answer.
func TestModeTryStillFailsOnABadCapture(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Try\n" +
		"    Using: \"n={v:int}\"\n" +
		"Reveal: stdout\n"
	// The second line fits the shape; its integer does not fit an int64.
	_, err := runErr(t, src, "n=1\nn=99999999999999999999")
	if err == nil || !strings.Contains(err.Error(), "not a valid integer") {
		t.Fatalf("expected a conversion error, got %v", err)
	}
}

// Try is never *inferred* — dropping input is something a program has to ask
// for, or a typo in a template would quietly parse nothing instead of failing.
func TestModeTryIsNeverInferred(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"toggle {a:int},{b:int}\"\n" +
		"Reveal: stdout\n"
	_, err := runErr(t, src, "toggle 1,1\nturn on 0,0")
	if err == nil || !strings.Contains(err.Error(), "does not match template") {
		t.Fatalf("an omitted Mode: should infer Each, not Try; got %v", err)
	}
}

func TestMatchModeRefusals(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"Maybe", "Mode must be One, Each, Try or Scan"},
		{"Try", ""}, // valid; the input-type check below is the subject
	} {
		src := "Cursed Energy: stdin\n" +
			"Cursed Technique: Match Pattern\n" +
			"    Mode: " + tc.mode + "\n" +
			"    Using: \"{a:int}\"\nReveal: stdout\n"
		_, err := runErr(t, src, "1")
		if tc.want == "" {
			// Try over a bare Text input names the type it wanted.
			if err == nil || !strings.Contains(err.Error(), "expects List<Text> input") {
				t.Errorf("Mode: Try over Text: got %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Mode: %s: expected %q, got %v", tc.mode, tc.want, err)
		}
	}
}
