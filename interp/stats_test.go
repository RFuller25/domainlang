package interp

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"domain/ir"
)

// node builds a throwaway node whose Eval is the identity.
func node(prim string, out *ir.Type) *ir.Node {
	return &ir.Node{
		Prim: prim, Display: prim, Out: out,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) { return v, nil },
	}
}

func TestStatsTopLevelStages(t *testing.T) {
	s := NewStats()
	a, b := node("A", ir.List(ir.Int())), node("B", ir.Int())

	s.Step(ir.StepEvent{Node: a, Out: []ir.Value{int64(1), int64(2)}, Dur: 10 * time.Millisecond})
	s.Step(ir.StepEvent{Node: b, Out: int64(3), Dur: 30 * time.Millisecond})

	if s.Stages() != 2 {
		t.Fatalf("stages = %d, want 2", s.Stages())
	}
	if s.Total() != 40*time.Millisecond {
		t.Errorf("total = %v, want 40ms", s.Total())
	}

	var buf bytes.Buffer
	s.Report(&buf, false)
	out := buf.String()
	for _, want := range []string{"A", "B", "List<Int>", "Int", "25.0", "75.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	// A list reports its length; a scalar reports no size at all.
	if !strings.Contains(out, "2") || !strings.Contains(out, "—") {
		t.Errorf("report should show a list size and an em dash for the scalar:\n%s", out)
	}
}

// Nested steps are attributed to the stage that encloses them, and the
// percentages still sum over top-level stages only — so the total is not
// double-counted.
func TestStatsAttributesNestedWorkToItsLoop(t *testing.T) {
	s := NewStats()
	body := node("Body", ir.Int())
	loop := node("Repeat 3", ir.Int())

	// A loop's Eval reports its children first, then itself.
	for i := 0; i < 3; i++ {
		s.PushFrame("Repeat 3 iter", nil)
		s.Step(ir.StepEvent{Node: body, Out: int64(1), Dur: time.Millisecond})
		s.PopFrame(nil)
	}
	s.Step(ir.StepEvent{Node: loop, Out: int64(1), Dur: 5 * time.Millisecond})

	if s.Stages() != 1 {
		t.Fatalf("stages = %d, want 1 (nested steps are not stages)", s.Stages())
	}
	if s.Total() != 5*time.Millisecond {
		t.Errorf("total = %v, want 5ms (nested time is inside the loop's own)", s.Total())
	}

	var buf bytes.Buffer
	s.Report(&buf, false)
	if !strings.Contains(buf.String(), "(3 frames, 3 steps)") {
		t.Errorf("report should summarize the loop's frames:\n%s", buf.String())
	}

	buf.Reset()
	s.Report(&buf, true)
	if !strings.Contains(buf.String(), "↳ Body") || !strings.Contains(buf.String(), "×3") {
		t.Errorf("--verbose should list the nested node and its call count:\n%s", buf.String())
	}
}

// Two sibling loops must not have their nested work merged.
func TestStatsSeparatesSiblingLoops(t *testing.T) {
	s := NewStats()
	b1, b2 := node("B1", ir.Int()), node("B2", ir.Int())
	l1, l2 := node("Loop1", ir.Int()), node("Loop2", ir.Int())

	s.PushFrame("iter", nil)
	s.Step(ir.StepEvent{Node: b1, Dur: time.Millisecond})
	s.PopFrame(nil)
	s.Step(ir.StepEvent{Node: l1, Dur: 2 * time.Millisecond})

	s.PushFrame("iter", nil)
	s.Step(ir.StepEvent{Node: b2, Dur: time.Millisecond})
	s.Step(ir.StepEvent{Node: b2, Dur: time.Millisecond})
	s.PopFrame(nil)
	s.Step(ir.StepEvent{Node: l2, Dur: 3 * time.Millisecond})

	var buf bytes.Buffer
	s.Report(&buf, true)
	out := buf.String()
	if strings.Count(out, "↳ B1") != 1 || strings.Count(out, "↳ B2") != 1 {
		t.Errorf("each loop should list only its own body:\n%s", out)
	}
	if !strings.Contains(out, "×2") {
		t.Errorf("B2 ran twice:\n%s", out)
	}
}

// A failing run still reports: the stage that failed is the interesting one.
func TestStatsRecordsFailures(t *testing.T) {
	s := NewStats()
	a := node("A", ir.Int())
	s.Step(ir.StepEvent{Node: a, Dur: time.Millisecond, Err: errFake})
	var buf bytes.Buffer
	s.Report(&buf, false)
	if !strings.Contains(buf.String(), "A") {
		t.Errorf("a failed stage should still appear:\n%s", buf.String())
	}
}

var errFake = fakeErr{}

type fakeErr struct{}

func (fakeErr) Error() string { return "boom" }

func TestStatsEmptyRun(t *testing.T) {
	var buf bytes.Buffer
	NewStats().Report(&buf, false)
	if !strings.Contains(buf.String(), "0 stages") {
		t.Errorf("an empty run should report zero stages:\n%s", buf.String())
	}
}

// The header has to say these numbers are the interpreter's, so nobody
// benchmarks the language with them.
func TestStatsHeaderNamesTheInterpreter(t *testing.T) {
	s := NewStats()
	s.Step(ir.StepEvent{Node: node("A", ir.Int()), Dur: time.Millisecond})
	var buf bytes.Buffer
	s.Report(&buf, false)
	if !strings.Contains(buf.String(), "interpreter") ||
		!strings.Contains(buf.String(), "not the compiled binary") {
		t.Errorf("header should disclaim the compiled binary:\n%s", buf.String())
	}
}

func TestSizeOf(t *testing.T) {
	grid := ir.NewGridValue(2, 3)
	sparse := ir.NewSparseValue(int64(0))
	sparse.Put(0, 0, int64(1))
	sparse.Put(5, 5, int64(1))
	m := ir.NewMapValue()
	m.Put(int64(1), int64(2))
	set := ir.NewSetValue()
	set.Add(int64(1))
	set.Add(int64(2))
	set.Add(int64(1)) // duplicate
	rec := ir.NewRecordValue()
	rec.Set("a", int64(1))

	cases := []struct {
		name  string
		v     ir.Value
		want  int
		known bool
	}{
		{"text", "hello", 5, true},
		{"empty text", "", 0, true},
		{"list", []ir.Value{int64(1), int64(2), int64(3)}, 3, true},
		{"grid cells", grid, 6, true},
		{"sparse set cells", sparse, 2, true},
		{"map entries", m, 1, true},
		{"set elements", set, 2, true},
		{"record fields", rec, 1, true},
		{"int has no size", int64(7), 0, false},
		{"float has no size", 1.5, 0, false},
		{"bool has no size", true, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, known := ir.SizeOf(c.v)
			if known != c.known || got != c.want {
				t.Errorf("SizeOf = (%d, %v), want (%d, %v)", got, known, c.want, c.known)
			}
		})
	}
}
