package interp

import (
	"encoding/json"
	"strings"
	"testing"
)

// exported records a program and returns its Recording, plus the JSON it
// marshals to — both matter, since the point of the schema is that something
// else reads it.
func exported(t *testing.T, src, stdin string, maxSteps int) (Recording, map[string]any) {
	t.Helper()
	rec := record(t, src, stdin, maxSteps)
	out := rec.Export()
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out, doc
}

func TestExportShapeOfAFlatRun(t *testing.T) {
	out, doc := exported(t, listPrefix+"Maximum Technique: Sum\nReveal: stdout\n", "1,2,3", 0)

	if out.Steps != 5 || out.Capped {
		t.Errorf("steps = %d, capped = %v, want 5 and false", out.Steps, out.Capped)
	}
	if len(out.Rows) != 5 {
		t.Fatalf("rows = %d, want the 5 top-level stages", len(out.Rows))
	}
	for _, r := range out.Rows {
		if r.Kind != "step" {
			t.Errorf("a flat pipeline has no frames, got kind %q", r.Kind)
		}
		if r.Line <= 0 {
			t.Errorf("row %q should carry its source line", r.Label)
		}
	}
	sum := out.Rows[3]
	if sum.Label != "Sum" || sum.Out != "6" || sum.Type != "Int" {
		t.Errorf("Sum row = %+v", sum)
	}
	// A scalar has no meaningful size, and the field is absent rather than 0 —
	// which is why it is a pointer.
	if sum.Size != nil {
		t.Errorf("Sum should report no size, got %d", *sum.Size)
	}
	if _, present := doc["rows"].([]any)[3].(map[string]any)["size"]; present {
		t.Error("an absent size should not appear in the JSON at all")
	}
	if split := out.Rows[1]; split.Size == nil || *split.Size != 3 {
		t.Errorf("Split's size should be the 3 elements it produced, got %v", split.Size)
	}
}

// The tree is a tree in the document too: an iteration's steps are nested under
// it, not flattened into a list that has lost the structure.
func TestExportNestsFrames(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 2\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	out, _ := exported(t, src, "1,2", 0)

	loop := out.Rows[len(out.Rows)-1]
	if loop.Kind != "step" || !strings.HasPrefix(loop.Label, "Repeat 2") {
		t.Fatalf("last row = %+v, want the loop", loop)
	}
	if len(loop.Children) != 2 {
		t.Fatalf("the loop should hold 2 iteration frames, got %d", len(loop.Children))
	}
	iter := loop.Children[0]
	if iter.Kind != "frame" || len(iter.Children) != 1 {
		t.Errorf("an iteration is a frame holding its steps, got %+v", iter)
	}
	if iter.Children[0].Label != "Map Each" {
		t.Errorf("the body step = %q, want Map Each", iter.Children[0].Label)
	}
}

// The numbers are the reason to export at all: a CI job asserting a stage
// stayed under its share of the run has to be able to read that share.
func TestExportCarriesTheTimings(t *testing.T) {
	out, _ := exported(t, listPrefix+"Maximum Technique: Sum\nReveal: stdout\n", "1,2,3", 0)
	if out.TotalNs <= 0 || out.Total == "" {
		t.Fatalf("the run's total should be reported both ways, got %d / %q", out.TotalNs, out.Total)
	}
	var sum float64
	for _, r := range out.Rows {
		if r.TimeNs < 0 || r.SelfNs < 0 {
			t.Errorf("row %q has a negative duration", r.Label)
		}
		sum += r.Pct
	}
	if sum < 99 || sum > 101 {
		t.Errorf("top-level shares sum to %.1f%%, want ~100%%", sum)
	}
	if len(out.Hotspots) == 0 {
		t.Error("the profile should be exported alongside the tree")
	}
	// Ranked worst-first, the same order the UI shows.
	for i := 1; i < len(out.Hotspots); i++ {
		if out.Hotspots[i-1].SelfNs < out.Hotspots[i].SelfNs {
			t.Errorf("hotspots are out of order at %d", i)
		}
	}
}

// Percentages are rounded to the tenth the UI displays, so a report and a test
// of that report cannot disagree about what a step cost.
func TestExportRoundsToTheDisplayedPrecision(t *testing.T) {
	out, _ := exported(t, listPrefix+"Maximum Technique: Sum\n", "1,2,3", 0)
	for _, r := range out.Rows {
		if r.Pct*10 != float64(int(r.Pct*10)) {
			t.Errorf("row %q has share %v, want one decimal place", r.Label, r.Pct)
		}
	}
}

// A capped recording says so in the document, for the same reason the header
// says so on screen: percentages are shares of what was kept.
func TestExportReportsTheCap(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 500\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	out, _ := exported(t, src, "1", 5)
	if !out.Capped || out.Steps != 5 {
		t.Errorf("capped = %v, steps = %d, want true and 5", out.Capped, out.Steps)
	}
}

// A failed run exports the failure on the step that raised it, which is what
// makes the document usable for the same job the UI is: finding what broke.
func TestExportCarriesErrors(t *testing.T) {
	out, _ := exported(t, listPrefix+"Reveal: stdout\n", "1,nope", 0)
	var found bool
	for _, r := range out.Rows {
		if r.Err != "" && strings.Contains(r.Err, "nope") {
			found = true
		}
	}
	if !found {
		t.Errorf("the failing step's error should be exported: %+v", out.Rows)
	}
}

// Inlined prelude and library nodes carry positions from a file the user is not
// looking at, so the document names that source instead of implying the line is
// theirs.
func TestExportMarksForeignSourceLines(t *testing.T) {
	out, _ := exported(t, "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n", "1\n2", 0)
	var foreign int
	for _, r := range out.Rows {
		if r.From != "" {
			foreign++
		}
	}
	if foreign == 0 {
		t.Errorf("the inlined prelude Shikigami's steps should name their source: %+v", out.Rows)
	}
}

// A block's row carries both answers: what it passed on, and what its body
// produced. A reader asking what a Channel computed wants the second one, and
// nothing else in the document holds it.
func TestExportCarriesBlockResults(t *testing.T) {
	src := listPrefix + "Channel \"total\":\n    Maximum Technique: Sum\n" +
		"Maximum Technique: Combine\n    From: total\n    Using: (t) -> t\nReveal: stdout\n"
	out, doc := exported(t, src, "1,2,3", 0)

	var ch *Row
	for i, r := range out.Rows {
		if strings.HasPrefix(r.Label, "Channel") {
			ch = &out.Rows[i]
		}
	}
	if ch == nil {
		t.Fatal("no channel row in the document")
	}
	if ch.Result != "6" || ch.ResultType != "Int" {
		t.Errorf("channel result = %q (%q), want 6 (Int)", ch.Result, ch.ResultType)
	}
	if ch.Out == ch.Result {
		t.Error("a Channel's out is the value it passed on, not its body's result")
	}
	// A row with no body of its own leaves the field out entirely.
	for _, r := range doc["rows"].([]any) {
		row := r.(map[string]any)
		if row["label"] == "Split by \",\"" {
			if _, present := row["result"]; present {
				t.Error("a step with no body should not carry a result")
			}
		}
	}
}

// The fold is in the document too, marked as what it is: a reader walking the
// tree can either treat it as a row or skip straight through to the laps.
func TestExportMarksFoldedIterations(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 4\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	out, _ := exported(t, src, "1,2", 0)

	loop := out.Rows[len(out.Rows)-1]
	if len(loop.Children) != 1 {
		t.Fatalf("the loop should hold one folded row, got %d", len(loop.Children))
	}
	fold := loop.Children[0]
	if !fold.Folded || fold.Kind != "frame" || fold.Label != "4 iterations" {
		t.Errorf("fold row = %+v", fold)
	}
	if len(fold.Children) != 4 {
		t.Errorf("the laps are kept in full, got %d", len(fold.Children))
	}
}
