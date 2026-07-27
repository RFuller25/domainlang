package interp

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// record runs src under a recorder and returns it. This is where the
// visualizer's real coverage lives: the tree is pure Go, so every construct can
// be asserted without a terminal.
func record(t *testing.T, src, stdin string, maxSteps int) *Recorder {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	rec := NewRecorder(maxSteps)
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &out, Trace: rec}
	// A failing run is a legitimate recording; the caller asserts on it.
	_, _ = Run(pipe, ctx)
	return rec
}

// tree renders a recording as indented labels, so a test can assert structure.
func tree(rec *Recorder) string {
	var b strings.Builder
	var walk func(nodes []*TraceNode, depth int)
	walk = func(nodes []*TraceNode, depth int) {
		for _, n := range nodes {
			fmt.Fprintf(&b, "%s%s\n", strings.Repeat("  ", depth), n.Label())
			walk(n.Children, depth+1)
		}
	}
	walk(rec.Roots(), 0)
	return b.String()
}

const listPrefix = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
`

func TestRecordFlatPipeline(t *testing.T) {
	rec := record(t, listPrefix+"Maximum Technique: Sum\nReveal: stdout\n", "1,2,3", 0)
	got := tree(rec)
	want := "Read Source <- stdin\nSplit by \",\"\nConvert List to Integers\nSum\nReveal -> stdout\n"
	if got != want {
		t.Errorf("tree =\n%s\nwant\n%s", got, want)
	}
	if rec.Steps() != 5 {
		t.Errorf("steps = %d, want 5", rec.Steps())
	}
	if rec.Truncated() {
		t.Error("a 5-step run should not be truncated")
	}
}

// A loop's iterations are frames under the loop's own row, each holding the
// body's steps — the nesting the visualizer lets you step into.
func TestRecordRepeatLoopNesting(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 2\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	rec := record(t, src, "1,2", 0)
	got := tree(rec)
	want := `Read Source <- stdin
Split by ","
Convert List to Integers
Repeat 2
  Repeat 2 iter 1/2
    Map Each
  Repeat 2 iter 2/2
    Map Each
`
	if got != want {
		t.Errorf("tree =\n%s\nwant\n%s", got, want)
	}
}

func TestRecordWhileLoopFrames(t *testing.T) {
	src := listPrefix + "Cursed Technique: Apply\n    Using: (xs) -> sum(xs)\n" +
		"Simple Domain: While\n    Using: (n) -> n > 3\n    Cursed Technique: Apply\n        Using: (n) -> n - 2\n"
	rec := record(t, src, "1,2,3,4", 0) // sum 10 -> 8 -> 6 -> 4 -> 2
	got := tree(rec)
	for _, want := range []string{"While iter 1", "While iter 4"} {
		if !strings.Contains(got, want) {
			t.Errorf("tree should contain %q:\n%s", want, got)
		}
	}
}

func TestRecordFixedPointFrames(t *testing.T) {
	src := listPrefix + "Simple Domain: Iterate Until Fixed Point\n" +
		"    Cursed Technique: Map Each\n        Using: (x) -> x / 2\n"
	rec := record(t, src, "8,4", 0)
	if !strings.Contains(tree(rec), "Fixed Point iter 1") {
		t.Errorf("tree should label fixed-point iterations:\n%s", tree(rec))
	}
}

func TestRecordChannelBody(t *testing.T) {
	src := listPrefix + "Channel \"total\":\n    Maximum Technique: Sum\n" +
		"Maximum Technique: Combine\n    From: total\n    Using: (t) -> t\nReveal: stdout\n"
	rec := record(t, src, "1,2", 0)
	got := tree(rec)
	want := `Channel "total"
  Sum`
	if !strings.Contains(got, want) {
		t.Errorf("tree should nest the channel body:\n%s", got)
	}
}

func TestRecordPartBody(t *testing.T) {
	src := listPrefix + "Part \"1\":\n    Maximum Technique: Sum\n    Reveal: stdout\n"
	rec := record(t, src, "1,2", 0)
	got := tree(rec)
	if !strings.Contains(got, "Part \"1\"\n  Sum\n  Reveal -> stdout") {
		t.Errorf("tree should nest the Part body:\n%s", got)
	}
}

// A Shikigami is inlined, so its steps appear where the call was, flat — there
// is no frame, because there is no runtime construct.
func TestRecordShikigamiIsFlat(t *testing.T) {
	rec := record(t, "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n", "1\n2", 0)
	got := tree(rec)
	if strings.Contains(got, "  ") {
		t.Errorf("an inlined Shikigami should not introduce a frame:\n%s", got)
	}
	if !strings.Contains(got, "Split by") || !strings.Contains(got, "Convert List to Integers") {
		t.Errorf("the inlined body's steps should be recorded:\n%s", got)
	}
}

// A run that fails mid-way is still explorable up to the failure, and the
// failing step carries its error — which is what makes this a debugger rather
// than a demo.
func TestRecordCapturesFailure(t *testing.T) {
	rec := record(t, listPrefix+"Reveal: stdout\n", "1,nope", 0)
	var failing *Step
	var walk func(nodes []*TraceNode)
	walk = func(nodes []*TraceNode) {
		for _, n := range nodes {
			if n.Step != nil && n.Step.Err != nil {
				failing = n.Step
			}
			walk(n.Children)
		}
	}
	walk(rec.Roots())
	if failing == nil {
		t.Fatalf("the failing step should be recorded:\n%s", tree(rec))
	}
	if failing.Node.Prim != "Convert To Integers" {
		t.Errorf("failing step = %s, want Convert To Integers", failing.Node.Prim)
	}
	if !strings.Contains(failing.Err.Error(), "nope") {
		t.Errorf("the step should carry its error, got %v", failing.Err)
	}
}

// A failure inside a loop is still reported for the loop itself — EvalNode
// records a step whether or not it succeeded — so the iteration that ran stays
// nested under it and the whole run remains explorable.
func TestRecordFailureInsideALoop(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 3\n    Cursed Technique: Map Each\n        Using: (x) -> 10 / x\n"
	rec := record(t, src, "5,0", 0) // division by zero on the second element
	got := tree(rec)
	if !strings.Contains(got, "Repeat 3\n  Repeat 3 iter 1/3\n    Map Each") {
		t.Errorf("the failing iteration should stay nested under its loop:\n%s", got)
	}
	if strings.Contains(got, "incomplete") {
		t.Errorf("nothing was orphaned here, so no synthetic row should appear:\n%s", got)
	}
	// Both the inner Map Each and the enclosing loop carry the error.
	var errs int
	var walk func(nodes []*TraceNode)
	walk = func(nodes []*TraceNode) {
		for _, n := range nodes {
			if n.Step != nil && n.Step.Err != nil {
				errs++
			}
			walk(n.Children)
		}
	}
	walk(rec.Roots())
	if errs < 2 {
		t.Errorf("both the failing step and its loop should carry the error, got %d", errs)
	}
}

// Roots() has a safety net for frames whose enclosing step never reported —
// reachable only if a node panics and interp.Run recovers, which is why it is
// exercised directly rather than through a program.
func TestRecordOrphanedFramesAreSurfaced(t *testing.T) {
	rec := NewRecorder(0)
	inner := node("Inner", nil)
	rec.PushFrame("Repeat 2 iter 1/2")
	rec.Step(ir.StepEvent{Node: inner, Depth: 1, Frame: "Repeat 2 iter 1/2"})
	rec.PopFrame()
	// The enclosing loop's Step never arrives.

	roots := rec.Roots()
	if len(roots) != 1 || !roots[0].IsFrame() {
		t.Fatalf("expected one synthetic frame row, got %d rows", len(roots))
	}
	if !strings.Contains(roots[0].Frame, "incomplete") {
		t.Errorf("row = %q, want it to say the stage did not finish", roots[0].Frame)
	}
	if len(roots[0].Children) != 1 {
		t.Errorf("the orphaned frame should still hold its iteration")
	}
}

// The step cap bounds capture on a program that would otherwise record
// millions of steps, and says so rather than pretending the run was short.
func TestRecordStepCap(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 500\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	rec := record(t, src, "1", 10)
	if rec.Steps() != 10 {
		t.Errorf("steps = %d, want the cap of 10", rec.Steps())
	}
	if !rec.Truncated() {
		t.Error("the recorder should report that it truncated")
	}
	if !strings.Contains(rec.Summary(), "capped") {
		t.Errorf("summary should admit the cap, got %q", rec.Summary())
	}
}

// Values are captured for the detail pane, with the short rendering always
// present so a step is never invisible.
func TestRecordCapturesValues(t *testing.T) {
	rec := record(t, listPrefix+"Maximum Technique: Sum\n", "1,2,3", 0)
	roots := rec.Roots()
	last := roots[len(roots)-1].Step
	if last.Node.Prim != "Sum" {
		t.Fatalf("last step = %s, want Sum", last.Node.Prim)
	}
	if last.Full != "6" || !last.FullOK {
		t.Errorf("Sum's full value = %q (ok=%v), want \"6\"", last.Full, last.FullOK)
	}
	if last.Short != "6" {
		t.Errorf("Sum's short value = %q, want \"6\"", last.Short)
	}
	// The step before it saw the list, and its input is recorded too.
	convert := roots[len(roots)-2].Step
	if !strings.Contains(convert.Short, "1") || convert.Size != 3 || !convert.SizeOK {
		t.Errorf("Convert step = %+v, want a 3-element list", convert)
	}
	if !strings.Contains(last.InShort, "1") {
		t.Errorf("Sum's input should be recorded, got %q", last.InShort)
	}
}

// A value larger than the per-value cap is kept truncated, and flagged, rather
// than either dropped or held whole.
func TestRecordTruncatesHugeValues(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&sb, "%d,", i)
	}
	rec := record(t, listPrefix+"Reveal: stdout\n", strings.TrimSuffix(sb.String(), ","), 0)
	for _, n := range rec.Roots() {
		if n.Step != nil && n.Step.SizeOK && n.Step.Size == 20000 {
			if n.Step.FullOK {
				t.Errorf("a %d-element value should be flagged as truncated", n.Step.Size)
			}
			if len(n.Step.Full) > maxValueBytes {
				t.Errorf("full capture = %d bytes, want <= %d", len(n.Step.Full), maxValueBytes)
			}
			if n.Step.Short == "" {
				t.Error("the short rendering must always be present")
			}
			return
		}
	}
	t.Fatal("never found the large value's step")
}

func TestRecordEmptyRun(t *testing.T) {
	rec := NewRecorder(0)
	if len(rec.Roots()) != 0 || rec.Steps() != 0 {
		t.Errorf("a fresh recorder should be empty")
	}
	if strings.Contains(rec.Summary(), "capped") {
		t.Errorf("summary = %q, want no cap mention", rec.Summary())
	}
}
