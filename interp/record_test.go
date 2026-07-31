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
	rec.PushFrame("Repeat 2 iter 1/2", nil)
	rec.Step(ir.StepEvent{Node: inner, Depth: 1, Frame: "Repeat 2 iter 1/2"})
	rec.PopFrame(nil)
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

// --- what a block produced, and the fold that keeps its laps out of the way ---

// find returns the first row whose label matches, anywhere in the recording.
func find(rec *Recorder, label string) *TraceNode {
	var hit *TraceNode
	var walk func(nodes []*TraceNode)
	walk = func(nodes []*TraceNode) {
		for _, n := range nodes {
			if hit == nil && n.Label() == label {
				hit = n
			}
			walk(n.Children)
		}
	}
	walk(rec.Roots())
	return hit
}

// A Channel is a passthrough: the pipeline carries on with the value that went
// in, so the row's own output says nothing about what the channel computed. The
// body's result is recorded separately, which is the answer a reader wants.
func TestRecordChannelReportsWhatItsBodyProduced(t *testing.T) {
	src := listPrefix + "Channel \"total\":\n    Maximum Technique: Sum\n" +
		"Maximum Technique: Combine\n    From: total\n    Using: (t) -> t\nReveal: stdout\n"
	rec := record(t, src, "1,2,3", 0)

	ch := find(rec, `Channel "total"`)
	if ch == nil {
		t.Fatalf("no channel row:\n%s", tree(rec))
	}
	if ch.Block == nil {
		t.Fatal("the channel row should carry its body's result")
	}
	if ch.Block.Short != "6" {
		t.Errorf("block result = %q, want the sum 6", ch.Block.Short)
	}
	if ch.Block.Type != "Int" {
		t.Errorf("block type = %q, want Int", ch.Block.Type)
	}
	// The step's own output is still the value handed to the next stage.
	if ch.Step.Short == "6" {
		t.Error("a Channel passes its input on; its own output is not the body's")
	}
}

func TestRecordPartReportsWhatItsBodyProduced(t *testing.T) {
	src := listPrefix + "Part \"1\":\n    Maximum Technique: Sum\n    Reveal: stdout\n"
	rec := record(t, src, "4,5", 0)
	part := find(rec, `Part "1"`)
	if part == nil || part.Block == nil {
		t.Fatalf("the Part row should carry its body's result:\n%s", tree(rec))
	}
	if part.Block.Short != "9" {
		t.Errorf("block result = %q, want 9", part.Block.Short)
	}
}

// One lap of a loop reports what it made of what it was given, so a folded loop
// can be read without opening it.
func TestRecordIterationsCarryTheirResult(t *testing.T) {
	src := listPrefix + "Maximum Technique: Sum\nSimple Domain: Repeat 2\n" +
		"    Cursed Technique: Apply\n        Using: (v) -> v * 2\n"
	rec := record(t, src, "1,2", 0) // sum 3, doubled twice: 6 then 12
	for i, want := range []string{"6", "12"} {
		lap := find(rec, fmt.Sprintf("Repeat 2 iter %d/2", i+1))
		if lap == nil || lap.Block == nil {
			t.Fatalf("lap %d should carry its result:\n%s", i+1, tree(rec))
		}
		if lap.Block.Short != want {
			t.Errorf("lap %d result = %q, want %q", i+1, lap.Block.Short, want)
		}
		if lap.Block.Type != "Int" {
			t.Errorf("lap %d type = %q, want Int", i+1, lap.Block.Type)
		}
	}
}

// A body that failed has no result, and says so by having none: a lap that
// reported the value it was handed would be claiming work it never did.
func TestRecordUnfinishedBodyHasNoResult(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 3\n    Cursed Technique: Map Each\n        Using: (x) -> 10 / x\n"
	rec := record(t, src, "5,0", 0)
	lap := find(rec, "Repeat 3 iter 1/3")
	if lap == nil {
		t.Fatalf("the failing lap should be recorded:\n%s", tree(rec))
	}
	if lap.Block != nil {
		t.Errorf("a lap that failed has no result, got %q", lap.Block.Short)
	}
}

// Past a handful, a loop's laps are gathered into one row that opens onto all
// of them: the point is that the stages around the loop stay visible.
func TestRecordFoldsRepeatedIterations(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 4\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	rec := record(t, src, "1,2", 0)
	loop := find(rec, "Repeat 4")
	if loop == nil {
		t.Fatalf("no loop row:\n%s", tree(rec))
	}
	if len(loop.Children) != 1 {
		t.Fatalf("the loop should hold one folded row, got %d:\n%s", len(loop.Children), tree(rec))
	}
	fold := loop.Children[0]
	laps, folded := fold.Iterations()
	if !folded || laps != 4 {
		t.Errorf("fold = (%d laps, folded=%v), want 4 laps folded", laps, folded)
	}
	if fold.Label() != "4 iterations" {
		t.Errorf("fold label = %q", fold.Label())
	}
	// Nothing is summarized away: every lap is still there, under the fold.
	for i := range 4 {
		if want := fmt.Sprintf("Repeat 4 iter %d/4", i+1); find(rec, want) == nil {
			t.Errorf("lap %q should still be in the tree:\n%s", want, tree(rec))
		}
	}
	// And the fold answers for the loop, which is the last lap's value.
	if fold.Block == nil || fold.Block.Short != fold.Children[3].Block.Short {
		t.Error("the fold should report what the last lap produced")
	}
}

// Two laps read better in place than behind a row that has to be opened.
func TestRecordDoesNotFoldAShortLoop(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 2\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	rec := record(t, src, "1,2", 0)
	if find(rec, "2 iterations") != nil {
		t.Errorf("a two-lap loop should not be folded:\n%s", tree(rec))
	}
}

// A loop inside a loop body owns its own laps. They are opened from inside the
// enclosing body, so without this they would land beside the nested loop's row
// rather than under it — and collapsing that row would hide nothing.
func TestRecordNestedLoopOwnsItsIterations(t *testing.T) {
	src := listPrefix + "Maximum Technique: Sum\nSimple Domain: Repeat 2\n" +
		"    Cursed Technique: Apply\n        Using: (v) -> v + 1\n" +
		"    Simple Domain: Repeat 3\n        Cursed Technique: Apply\n            Using: (v) -> v * 2\n"
	rec := record(t, src, "1,2", 0)
	outer := find(rec, "Repeat 2 iter 1/2")
	if outer == nil {
		t.Fatalf("no outer lap:\n%s", tree(rec))
	}
	var inner *TraceNode
	for _, c := range outer.Children {
		if c.Label() == "Repeat 3" {
			inner = c
		}
		if c.IsFrame() && strings.HasPrefix(c.Label(), "Repeat 3 iter") {
			t.Errorf("the inner loop's laps belong under its own row:\n%s", tree(rec))
		}
	}
	if inner == nil {
		t.Fatalf("the nested loop should be a row of the outer lap:\n%s", tree(rec))
	}
	if laps, folded := inner.Children[0].Iterations(); !folded || laps != 3 {
		t.Errorf("the nested loop should hold its own folded laps, got (%d, %v)", laps, folded)
	}
}

// A `Using:` body runs once per element, so its steps used to land beside the
// stage that ran them — a hundred elements of a three-stage body reading as
// three hundred rows, with the stage that produced them last. Each application
// is a frame, and they fold.
func TestRecordFramesNestedUsingBodies(t *testing.T) {
	src := "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \",\"\n" +
		"Cursed Technique: Map Each\n" +
		"    Channeled Energy: Convert List to Integers\n" +
		"    Maximum Technique: Sum\n"
	rec := record(t, src, "1,2\n3,4\n5,6", 0)

	mapEach := find(rec, "Map Each")
	if mapEach == nil {
		t.Fatalf("no Map Each row:\n%s", tree(rec))
	}
	if len(mapEach.Children) != 1 {
		t.Fatalf("the body's applications belong under Map Each, folded, got %d rows:\n%s",
			len(mapEach.Children), tree(rec))
	}
	laps, folded := mapEach.Children[0].Iterations()
	if !folded || laps != 3 {
		t.Errorf("fold = (%d, %v), want the 3 elements folded", laps, folded)
	}
	// Each application is numbered, since the primitive that ran it cannot say
	// which element it was.
	body := find(rec, "Map Each body 2/3")
	if body == nil {
		t.Fatalf("body applications should be numbered:\n%s", tree(rec))
	}
	if body.Block == nil || body.Block.Type != "Int" {
		t.Errorf("a body's result is one element's worth, got %+v", body.Block)
	}
	// And the body's own steps are under it, not beside the stage.
	if len(body.Children) != 2 {
		t.Errorf("the body's steps belong to the application:\n%s", tree(rec))
	}
}

// A For loop's laps are frames like every other loop's.
func TestRecordFramesForLoops(t *testing.T) {
	src := listPrefix + "Channel \"xs\":\n    Cursed Technique: Take Item 0\n" +
		"Maximum Technique: Sum\nSimple Domain: For i in range(3)\n" +
		"    Cursed Technique: Apply\n        Using: (v, i) -> v + i\n"
	rec := record(t, src, "1,2", 0)
	loop := find(rec, "For i in range(3)")
	if loop == nil {
		t.Fatalf("no For row:\n%s", tree(rec))
	}
	if laps, folded := loop.Children[0].Iterations(); !folded || laps != 3 {
		t.Errorf("a For loop's laps should fold like any other, got (%d, %v):\n%s", laps, folded, tree(rec))
	}
	if find(rec, "For i iter 2/3") == nil {
		t.Errorf("the laps should name the loop variable:\n%s", tree(rec))
	}
}

// A fold stands in for its laps; counting it as a frame as well would report a
// loop of four laps as five frames.
func TestRecordCountsSkipTheFoldItself(t *testing.T) {
	src := listPrefix + "Simple Domain: Repeat 4\n    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
	rec := record(t, src, "1,2", 0)
	loop := find(rec, "Repeat 4")
	steps, frames := loop.Counts()
	if frames != 4 || steps != 4 {
		t.Errorf("counts = (%d steps, %d frames), want 4 and 4", steps, frames)
	}
}
