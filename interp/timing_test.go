package interp

import (
	"testing"
	"time"

	"domain/ir"
)

// recordLoop builds a recording by hand: one top-level step that opened two
// frames, each holding one body step. Synthetic durations are used throughout
// because the arithmetic — not the clock — is what these tests pin, and a real
// run's timings are unrepeatable by construction.
func recordLoop(t *testing.T, loopDur, bodyDur time.Duration) *Recorder {
	t.Helper()
	rec := NewRecorder(0)
	body := node("Map Each", ir.List(ir.Int()))
	for i := 1; i <= 2; i++ {
		rec.PushFrame("Repeat 2 iter "+string(rune('0'+i))+"/2", nil)
		rec.Step(ir.StepEvent{Node: body, Depth: 1, Dur: bodyDur})
		rec.PopFrame(nil)
	}
	rec.Step(ir.StepEvent{Node: node("Repeat 2", ir.List(ir.Int())), Dur: loopDur})
	return rec
}

// The denominator is the sum of the *top-level* rows, so the shares of those
// rows add up to the whole run. This is the property the whole feature rests on.
func TestTimingTopLevelSharesSumToWhole(t *testing.T) {
	rec := NewRecorder(0)
	rec.Step(ir.StepEvent{Node: node("A", ir.Int()), Dur: 10 * time.Millisecond})
	rec.Step(ir.StepEvent{Node: node("B", ir.Int()), Dur: 30 * time.Millisecond})

	tm := rec.Timing()
	if tm.Overall() != 40*time.Millisecond {
		t.Fatalf("overall = %s, want 40ms", tm.Overall())
	}
	roots := rec.Roots()
	var sum float64
	for _, n := range roots {
		nt := tm.Of(n)
		if !nt.Known {
			t.Fatalf("row %q should have a known share", n.Label())
		}
		sum += nt.TotalPct
	}
	if sum < 99.99 || sum > 100.01 {
		t.Errorf("top-level shares sum to %.4f%%, want 100%%", sum)
	}
	if got := tm.Of(roots[0]).TotalPct; got != 25 {
		t.Errorf("A's share = %.1f%%, want 25%%", got)
	}
}

// Durations nest — a loop's own duration already contains every iteration — so
// the total must not add the body in a second time. A 100ms loop whose body
// accounts for 80ms is 100ms of run, not 180ms.
func TestTimingDoesNotDoubleCountNestedWork(t *testing.T) {
	rec := recordLoop(t, 100*time.Millisecond, 40*time.Millisecond)
	tm := rec.Timing()
	if tm.Overall() != 100*time.Millisecond {
		t.Errorf("overall = %s, want the loop's own 100ms, not the sum of every level", tm.Overall())
	}
	loop := rec.Roots()[0]
	if nt := tm.Of(loop); nt.TotalPct != 100 {
		t.Errorf("the only top-level row should be 100%% of the run, got %.1f%%", nt.TotalPct)
	}
}

// Self time is what makes a hot loop readable: `Repeat 2` at 100% total is not
// a slow primitive, it is 80% body and 20% loop overhead.
func TestTimingSelfExcludesNestedFrames(t *testing.T) {
	rec := recordLoop(t, 100*time.Millisecond, 40*time.Millisecond)
	tm := rec.Timing()
	nt := tm.Of(rec.Roots()[0])

	if nt.Total != 100*time.Millisecond {
		t.Errorf("total = %s, want 100ms", nt.Total)
	}
	if nt.Self != 20*time.Millisecond {
		t.Errorf("self = %s, want 20ms (100ms less two 40ms iterations)", nt.Self)
	}
	if nt.SelfPct != 20 || nt.TotalPct != 100 {
		t.Errorf("shares = %.1f%% self / %.1f%% total, want 20/100", nt.SelfPct, nt.TotalPct)
	}
	if !nt.Nested {
		t.Error("a row with frames under it should be flagged as nested, so self earns its column")
	}
}

// A frame is a label around a sub-pipeline, not an evaluation: it costs exactly
// what happened inside it and has no self time to report.
func TestTimingFrameCostsItsContents(t *testing.T) {
	rec := recordLoop(t, 100*time.Millisecond, 40*time.Millisecond)
	tm := rec.Timing()
	frame := rec.Roots()[0].Children[0]
	if !frame.IsFrame() {
		t.Fatalf("expected a frame under the loop, got %q", frame.Label())
	}

	nt := tm.Of(frame)
	if nt.Total != 40*time.Millisecond {
		t.Errorf("frame total = %s, want its body's 40ms", nt.Total)
	}
	if nt.Self != 0 {
		t.Errorf("frame self = %s, want 0 — a frame does no work of its own", nt.Self)
	}
	if nt.Nested {
		t.Error("a frame's self time is always zero, so it should not be flagged as a distinct number")
	}
	if nt.TotalPct != 40 {
		t.Errorf("frame share = %.1f%%, want 40%%", nt.TotalPct)
	}
}

// A leaf step's self time is its whole duration, and is not worth a second
// column saying so.
func TestTimingLeafStepIsAllSelf(t *testing.T) {
	rec := NewRecorder(0)
	rec.Step(ir.StepEvent{Node: node("Sum", ir.Int()), Dur: 5 * time.Millisecond})
	nt := rec.Timing().Of(rec.Roots()[0])
	if nt.Self != nt.Total || nt.SelfPct != nt.TotalPct {
		t.Errorf("a leaf's self should equal its total, got %s / %s", nt.Self, nt.Total)
	}
	if nt.Nested {
		t.Error("a leaf has no nested work to distinguish")
	}
}

// A recording that hit --max-steps partway through a body can leave a row's
// children summing past the row itself. Zero is the honest floor.
func TestTimingClampsSelfAtZero(t *testing.T) {
	rec := recordLoop(t, time.Millisecond, 40*time.Millisecond)
	if nt := rec.Timing().Of(rec.Roots()[0]); nt.Self != 0 {
		t.Errorf("self = %s, want 0 rather than a negative duration", nt.Self)
	}
}

// A run too fast for the clock has no denominator, and must say so rather than
// dividing by zero or claiming everything took 0%.
func TestTimingWithoutADenominator(t *testing.T) {
	rec := NewRecorder(0)
	rec.Step(ir.StepEvent{Node: node("A", ir.Int())})
	tm := rec.Timing()
	if tm.Overall() != 0 {
		t.Fatalf("overall = %s, want 0", tm.Overall())
	}
	if nt := tm.Of(rec.Roots()[0]); nt.Known {
		t.Error("percentages of a zero-length run are not known, and should not be claimed")
	}
}

// Work stranded under the synthetic "incomplete" row is still the program's
// time, so it belongs in the denominator — otherwise the shares of a failed run
// would add up to more than the run.
func TestTimingCountsOrphanedFrames(t *testing.T) {
	rec := NewRecorder(0)
	rec.Step(ir.StepEvent{Node: node("A", ir.Int()), Dur: 30 * time.Millisecond})
	rec.PushFrame("Repeat 2 iter 1/2", nil)
	rec.Step(ir.StepEvent{Node: node("Inner", ir.Int()), Depth: 1, Dur: 10 * time.Millisecond})
	rec.PopFrame(nil)
	// The enclosing loop's step never arrives.

	tm := rec.Timing()
	if tm.Overall() != 40*time.Millisecond {
		t.Errorf("overall = %s, want 40ms including the orphaned iteration", tm.Overall())
	}
	orphan := rec.Roots()[1]
	if nt := tm.Of(orphan); nt.TotalPct != 25 {
		t.Errorf("the orphaned row's share = %.1f%%, want 25%%", nt.TotalPct)
	}
}

// Timing keys its profile by node pointer, so Roots() must hand back the same
// synthetic row every time it is asked — a fresh object on each call would miss
// every lookup and silently report zeros.
func TestRootsSyntheticRowIsStable(t *testing.T) {
	rec := NewRecorder(0)
	rec.PushFrame("Repeat 2 iter 1/2", nil)
	rec.Step(ir.StepEvent{Node: node("Inner", ir.Int()), Depth: 1, Dur: time.Millisecond})
	rec.PopFrame(nil)

	first, second := rec.Roots(), rec.Roots()
	if first[0] != second[0] {
		t.Error("Roots() should return the same synthetic row each call")
	}
	if len(second[0].Children) != 1 {
		t.Errorf("the synthetic row lost its children on the second call: %d", len(second[0].Children))
	}
}

// --- the profile ---

// The whole point of aggregating by call site: a body that ran 400 times is one
// line of the profile, not 400 invisible ones.
func TestHotspotsAggregateAcrossCalls(t *testing.T) {
	rec := recordLoop(t, 100*time.Millisecond, 40*time.Millisecond)
	hot := rec.Timing().Hotspots(0)
	if len(hot) != 2 {
		t.Fatalf("expected the loop and its body, got %d: %+v", len(hot), hot)
	}
	body := hot[0]
	if body.Name != "Map Each" || body.Calls != 2 {
		t.Fatalf("the body should be one entry of 2 calls, got %q ×%d", body.Name, body.Calls)
	}
	if body.Self != 80*time.Millisecond {
		t.Errorf("body self = %s, want the two iterations' 80ms", body.Self)
	}
	if body.SelfPct != 80 {
		t.Errorf("body share = %.1f%%, want 80%%", body.SelfPct)
	}
}

// Ranking is by self time, so the row that *contains* the work does not
// outrank the row that *is* it.
func TestHotspotsRankBySelfTime(t *testing.T) {
	rec := recordLoop(t, 100*time.Millisecond, 40*time.Millisecond)
	hot := rec.Timing().Hotspots(0)
	if hot[0].Name != "Map Each" {
		t.Errorf("the body (80ms self) should outrank the loop (20ms self), got %q first", hot[0].Name)
	}
	if hot[1].Name != "Repeat 2" || hot[1].Self != 20*time.Millisecond {
		t.Errorf("the loop should be second with 20ms self, got %q %s", hot[1].Name, hot[1].Self)
	}
	if got := rec.Timing().Hotspots(1); len(got) != 1 {
		t.Errorf("a limit of 1 should keep one entry, got %d", len(got))
	}
}

// A list of zeroes is not a profile.
func TestHotspotsSkipFreeSteps(t *testing.T) {
	rec := NewRecorder(0)
	rec.Step(ir.StepEvent{Node: node("Free", ir.Int())})
	rec.Step(ir.StepEvent{Node: node("Costly", ir.Int()), Dur: time.Millisecond})
	hot := rec.Timing().Hotspots(0)
	if len(hot) != 1 || hot[0].Name != "Costly" {
		t.Errorf("only the step that cost something belongs in the profile, got %+v", hot)
	}
}

func TestHotspotsCarryFailure(t *testing.T) {
	rec := NewRecorder(0)
	rec.Step(ir.StepEvent{Node: node("Boom", ir.Int()), Dur: time.Millisecond, Err: errFake})
	if hot := rec.Timing().Hotspots(0); len(hot) != 1 || !hot[0].Failed {
		t.Errorf("a failing call site should be flagged, got %+v", hot)
	}
}

// The jump key needs a row to land on, and it should be the row that is the
// work rather than the one that merely contains it.
func TestHottestRow(t *testing.T) {
	rec := recordLoop(t, 100*time.Millisecond, 40*time.Millisecond)
	hot := rec.Timing().Hottest()
	if hot == nil {
		t.Fatal("a recording with timings should have a hottest row")
	}
	if hot.Label() != "Map Each" {
		t.Errorf("hottest = %q, want an iteration's Map Each", hot.Label())
	}
	if empty := NewRecorder(0).Timing().Hottest(); empty != nil {
		t.Error("an empty recording has no hottest row")
	}
}

// A collapsed row says what it is hiding, so its counts have to be right.
func TestTraceNodeCounts(t *testing.T) {
	rec := recordLoop(t, 100*time.Millisecond, 40*time.Millisecond)
	loop := rec.Roots()[0]
	steps, frames := loop.Counts()
	if steps != 2 || frames != 2 {
		t.Errorf("the loop hides 2 frames of 1 step each, got %d steps / %d frames", steps, frames)
	}
	if s, f := loop.Children[0].Counts(); s != 1 || f != 0 {
		t.Errorf("one iteration holds one step, got %d steps / %d frames", s, f)
	}
	leaf := rec.Roots()[0].Children[0].Children[0]
	if s, f := leaf.Counts(); s != 0 || f != 0 {
		t.Errorf("a leaf hides nothing, got %d steps / %d frames", s, f)
	}
}

func TestFormatPercent(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{0, "0%"},
		{0.02, "<0.1%"}, // ran, but too fast to round to a tenth
		{0.16, "0.2%"},
		{42.31, "42.3%"},
		{100, "100.0%"},
	}
	for _, c := range cases {
		if got := FormatPercent(c.pct); got != c.want {
			t.Errorf("FormatPercent(%v) = %q, want %q", c.pct, got, c.want)
		}
		if n := len([]rune(FormatPercent(c.pct))); n > 6 {
			t.Errorf("FormatPercent(%v) is %d columns wide, want <= 6", c.pct, n)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0"},
		{400 * time.Nanosecond, "400ns"},
		{1500 * time.Nanosecond, "1.5µs"},
		{2500 * time.Microsecond, "2.50ms"},
		{1500 * time.Millisecond, "1.500s"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.d); got != c.want {
			t.Errorf("FormatDuration(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}
