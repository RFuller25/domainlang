package interp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"domain/ir"
)

// findStep returns the first recorded step the predicate accepts.
func findStep(rec *Recorder, ok func(*Step) bool) *Step {
	var found *Step
	var walk func([]*TraceNode)
	walk = func(nodes []*TraceNode) {
		for _, n := range nodes {
			if found != nil {
				return
			}
			if !n.IsFrame() && ok(n.Step) {
				found = n.Step
				return
			}
			walk(n.Children)
		}
	}
	walk(rec.Roots())
	return found
}

func failingStep(rec *Recorder) *Step {
	return findStep(rec, func(s *Step) bool { return s.Err != nil })
}

// A step whose lambda failed keeps the application that *failed*, not the first
// one it made. Element 1 divides cleanly; element 4 is the one with 0 in it, and
// that is the arithmetic a reader opening the expression pane is there for.
func TestRecordKeepsTheFailingApplication(t *testing.T) {
	src := listPrefix + "Cursed Technique: Map Each\n    Using: (x) -> 100 / (x - 4)\n"
	rec := recordWithApplications(t, src, "1,2,3,4")

	s := failingStep(rec)
	if s == nil {
		t.Fatalf("the failing step should be recorded:\n%s", tree(rec))
	}
	if s.Apply == nil {
		t.Fatal("the failing step recorded no application")
	}
	if s.Apply.Count != 4 {
		t.Errorf("Count = %d, want 4 applications", s.Apply.Count)
	}
	if s.Apply.Index != 4 {
		t.Errorf("Index = %d, want the fourth — the one that divided by zero", s.Apply.Index)
	}
	if got := s.Apply.Args[0]; got != int64(4) {
		t.Errorf("bound argument = %v, want 4 — the element that failed", got)
	}
}

// A step that succeeded keeps the *first* application: they all worked, and the
// first is the one a reader can be shown without being asked which.
func TestRecordKeepsTheFirstApplicationOnSuccess(t *testing.T) {
	src := listPrefix + "Cursed Technique: Map Each\n    Using: (x) -> x + 1\n"
	rec := recordWithApplications(t, src, "7,8,9")

	s := findStep(rec, func(s *Step) bool { return s.Apply != nil })
	if s == nil {
		t.Fatalf("no step recorded an application:\n%s", tree(rec))
	}
	if s.Apply.Index != 1 || s.Apply.Count != 3 {
		t.Errorf("Index/Count = %d/%d, want 1/3", s.Apply.Index, s.Apply.Count)
	}
	if got := s.Apply.Args[0]; got != int64(7) {
		t.Errorf("bound argument = %v, want 7 — the first element", got)
	}
}

// The same rule for a foreign block, which cannot be replayed: a run that failed
// displaces the healthy first one, because a block that dies on its forty-first
// input is the bug and its fortieth tidy stdout is not.
func TestRecordKeepsTheFailingForeignRun(t *testing.T) {
	rec := NewRecorder(0)
	ok := func(i int) ir.ForeignRun {
		return ir.ForeignRun{
			Command: fmt.Sprintf("python3 run%d.py", i),
			Stdout:  ir.Capture{Text: "fine\n", Bytes: 5},
			Dur:     time.Millisecond,
		}
	}
	rec.ForeignRan(ok(1))
	rec.ForeignRan(ok(2))
	rec.ForeignRan(ir.ForeignRun{
		Command: "python3 run3.py",
		Stderr:  ir.Capture{Text: "Traceback…\n", Bytes: 11},
		Err:     errors.New("exit status 1"),
		Dur:     time.Millisecond,
	})
	rec.ForeignRan(ok(4))

	rec.Step(ir.StepEvent{Node: &ir.Node{Prim: "Foreign Block"}, Out: int64(1)})
	s := findStep(rec, func(s *Step) bool { return s.Foreign != nil })
	if s == nil {
		t.Fatal("the step recorded no foreign run")
	}
	if s.Foreign.Run.Err == nil {
		t.Errorf("kept the run from %q; the failing one should have displaced it", s.Foreign.Run.Command)
	}
	if s.Foreign.Index != 3 || s.Foreign.Count != 4 {
		t.Errorf("Index/Count = %d/%d, want 3/4", s.Foreign.Index, s.Foreign.Count)
	}
	if !strings.Contains(s.Foreign.Run.Stderr.Text, "Traceback") {
		t.Errorf("the failing run's stderr was not kept: %q", s.Foreign.Run.Stderr.Text)
	}
}

// A later *successful* run does not displace an earlier successful one — the
// first is still the representative sample.
func TestRecordKeepsTheFirstForeignRunOnSuccess(t *testing.T) {
	rec := NewRecorder(0)
	rec.ForeignRan(ir.ForeignRun{Command: "python3 first.py"})
	rec.ForeignRan(ir.ForeignRun{Command: "python3 second.py"})
	rec.Step(ir.StepEvent{Node: &ir.Node{Prim: "Foreign Block"}, Out: int64(1)})

	s := findStep(rec, func(s *Step) bool { return s.Foreign != nil })
	if s == nil {
		t.Fatal("the step recorded no foreign run")
	}
	if s.Foreign.Run.Command != "python3 first.py" {
		t.Errorf("kept %q, want the first run", s.Foreign.Run.Command)
	}
	if s.Foreign.Index != 1 || s.Foreign.Count != 2 {
		t.Errorf("Index/Count = %d/%d, want 1/2", s.Foreign.Index, s.Foreign.Count)
	}
}

// A failure past the step cap is still recorded. Without this a run that dies
// deep into a long program records its opening stretch, reports "capped", and
// has no failing row to jump to — the one row the tool exists to reach.
func TestRecordKeepsAFailureBeyondTheCap(t *testing.T) {
	rec := NewRecorder(3)
	node := &ir.Node{Prim: "Add"}
	for range 10 {
		rec.Step(ir.StepEvent{Node: node, Out: int64(1)})
	}
	boom := errors.New("everything went wrong at the end")
	rec.Step(ir.StepEvent{Node: &ir.Node{Prim: "Sum"}, Out: nil, Err: boom})

	if !rec.Truncated() {
		t.Error("a 11-step run under a cap of 3 should report truncated")
	}
	s := failingStep(rec)
	if s == nil {
		t.Fatalf("the failing step should survive the cap:\n%s", tree(rec))
	}
	if !errors.Is(s.Err, boom) {
		t.Errorf("failing step carries %v", s.Err)
	}
	if rec.Steps() != 4 {
		t.Errorf("Steps() = %d, want 3 capped + 1 failure", rec.Steps())
	}
	if !strings.Contains(rec.Summary(), "--max-steps 0") {
		t.Errorf("a capped summary should name the way out, got %q", rec.Summary())
	}
}

// A failure recorded past the cap must not adopt the frames waiting at its
// level: the steps that would have owned them were dropped, so hanging them
// under whichever step happened to fail reports a dropped loop's whole cost as
// that step's own — which showed up as a row at 671% of the run.
func TestRecordFailureBeyondTheCapAdoptsNothing(t *testing.T) {
	rec := NewRecorder(4)
	loop := &ir.Node{Prim: "Repeat"}

	// A loop's laps, recorded, which fills the cap. Their owning step — the
	// loop itself — reports afterwards and so is dropped, leaving the laps
	// waiting at the top level for a step that will never claim them.
	for range 4 {
		rec.PushFrame("lap", nil)
		rec.Step(ir.StepEvent{Node: loop, Out: int64(2), Dur: 10 * time.Microsecond})
		rec.PopFrame(int64(2))
	}
	rec.Step(ir.StepEvent{Node: loop, Out: nil, Dur: 50 * time.Microsecond}) // dropped

	// And then something fails.
	rec.Step(ir.StepEvent{Node: &ir.Node{Prim: "Sum"}, Err: errors.New("boom"), Dur: time.Microsecond})

	s := failingStep(rec)
	if s == nil {
		t.Fatalf("the failure should be recorded:\n%s", tree(rec))
	}
	var failing *TraceNode
	var walk func([]*TraceNode)
	walk = func(nodes []*TraceNode) {
		for _, n := range nodes {
			if !n.IsFrame() && n.Step == s {
				failing = n
			}
			walk(n.Children)
		}
	}
	walk(rec.Roots())
	if failing == nil {
		t.Fatal("the failing row is not in the tree")
	}
	if len(failing.Children) != 0 {
		t.Errorf("the failing row adopted %d frames it did not open", len(failing.Children))
	}

	// The orphaned laps are still reachable, under the row that says what they
	// are, and no row claims more of the run than there was.
	t2 := rec.Timing()
	for node, nt := range t2.nodes {
		if nt.TotalPct > 100.5 {
			t.Errorf("row %q reports %.1f%% of the run", node.Label(), nt.TotalPct)
		}
	}
	if !strings.Contains(tree(rec), "incomplete") {
		t.Errorf("the orphaned laps should be under the incomplete row:\n%s", tree(rec))
	}
}

// Unlimited means unlimited: the cap is a memory bound, not a policy, and a
// caller can decline it.
func TestRecordUnlimited(t *testing.T) {
	rec := NewRecorder(Unlimited)
	node := &ir.Node{Prim: "Add"}
	for range DefaultMaxSteps + 100 {
		rec.Step(ir.StepEvent{Node: node, Out: int64(1)})
	}
	if rec.Truncated() {
		t.Error("an unlimited recorder should never report truncated")
	}
	if rec.Steps() != DefaultMaxSteps+100 {
		t.Errorf("Steps() = %d, want every one of them", rec.Steps())
	}
}

// A value the budget was never spent on is a different answer from one too big
// to keep whole, and the recording says which.
func TestRecordDistinguishesSpentFromTruncated(t *testing.T) {
	rec := NewRecorder(0)
	big := make([]ir.Value, 40000)
	for i := range big {
		big[i] = int64(i)
	}
	// The first value is far past the per-value cap: kept, but not whole.
	rec.Step(ir.StepEvent{Node: &ir.Node{Prim: "Build"}, Out: big})
	first := findStep(rec, func(s *Step) bool { return s.Node.Prim == "Build" })
	if first.FullOK {
		t.Error("a value past the per-value cap should not report FullOK")
	}
	if first.Spent {
		t.Error("the first value should not report a spent budget")
	}
	if len(first.Full) == 0 {
		t.Error("a value past the per-value cap should still keep its head")
	}

	// Spend the budget, then check the next value says so rather than claiming
	// to have been truncated.
	rec.budget = 0
	rec.Step(ir.StepEvent{Node: &ir.Node{Prim: "After"}, Out: big})
	after := findStep(rec, func(s *Step) bool { return s.Node.Prim == "After" })
	if !after.Spent {
		t.Error("a value the budget could not pay for should report Spent")
	}
	if after.Full != "" {
		t.Error("nothing should have been captured with no budget left")
	}
	if after.Short == "" {
		t.Error("the short rendering is always kept, so a value is never invisible")
	}
}

// The per-value cap bounds what is *built*, not only what is kept: a recorder
// that rendered a huge value in full and then sliced it would cost the whole
// rendering per step.
func TestRecordDoesNotBuildBeyondTheValueCap(t *testing.T) {
	rec := NewRecorder(0)
	huge := make([]ir.Value, 3000000)
	for i := range huge {
		huge[i] = int64(i)
	}
	rec.Step(ir.StepEvent{Node: &ir.Node{Prim: "Build"}, Out: huge})
	s := findStep(rec, func(s *Step) bool { return s.Node.Prim == "Build" })
	if len(s.Full) > maxValueBytes {
		t.Errorf("kept %d bytes, more than the per-value cap", len(s.Full))
	}
	if got := defaultValueBudget - rec.budget; got > maxValueBytes {
		t.Errorf("charged %d bytes for one value, more than the cap allows", got)
	}
}

// Progress is reported while a long run records, so a command can show the
// program is still going rather than leaving a blank terminal.
func TestRecordReportsProgress(t *testing.T) {
	rec := NewRecorder(0)
	var reports []Progress
	rec.OnProgress(func(p Progress) { reports = append(reports, p) })
	node := &ir.Node{Prim: "Add"}
	for range progressEvery * 3 {
		rec.Step(ir.StepEvent{Node: node, Out: int64(1)})
	}
	if len(reports) != 3 {
		t.Fatalf("got %d progress reports, want 3", len(reports))
	}
	for i, p := range reports {
		if want := progressEvery * (i + 1); p.Steps != want {
			t.Errorf("report %d: Steps = %d, want %d", i, p.Steps, want)
		}
	}
}
