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

// The trace hook must be free when nobody is tracing: ir.EvalNode is n.Eval
// plus one nil check, and PushFrame/PopFrame return immediately. These
// benchmarks are the guard — BenchmarkUntraced is what every ordinary
// `domain run` pays, and it must not drift from the pre-hook cost.
//
//	go test ./interp -bench Trace -run XXX

const benchProgram = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Simple Domain: Repeat 5
    Cursed Technique: Map Each
        Using: (x) -> x + 1
Cursed Technique: Filter
    Using: (x) -> x > 10
Maximum Technique: Sum
`

func benchPipeline(b *testing.B) (*ir.Pipeline, string) {
	b.Helper()
	toks, err := lexer.Lex(benchProgram)
	if err != nil {
		b.Fatal(err)
	}
	prog, err := parser.Parse(benchProgram, toks)
	if err != nil {
		b.Fatal(err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		b.Fatal(err)
	}
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "%d\n", i%97)
	}
	return pipe, strings.TrimRight(sb.String(), "\n")
}

func BenchmarkTraceUntraced(b *testing.B) {
	pipe, input := benchPipeline(b)
	var out bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out.Reset()
		ctx := &ir.Context{Stdin: strings.NewReader(input), Stdout: &out}
		if _, err := Run(pipe, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTraceWithStats(b *testing.B) {
	pipe, input := benchPipeline(b)
	var out bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out.Reset()
		ctx := &ir.Context{Stdin: strings.NewReader(input), Stdout: &out}
		ctx.Trace = NewStats()
		if _, err := Run(pipe, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// countingTracer is the cheapest possible tracer, isolating the hook's own cost
// from whatever a real consumer does with the events.
type countingTracer struct{ steps, frames int }

func (c *countingTracer) Step(ir.StepEvent) { c.steps++ }
func (c *countingTracer) PushFrame(string)  { c.frames++ }
func (c *countingTracer) PopFrame()         {}

func BenchmarkTraceWithCounter(b *testing.B) {
	pipe, input := benchPipeline(b)
	var out bytes.Buffer
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out.Reset()
		ctx := &ir.Context{Stdin: strings.NewReader(input), Stdout: &out}
		ctx.Trace = &countingTracer{}
		if _, err := Run(pipe, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// A tracer must see every construct: top-level statements, loop bodies (through
// the shared runBody), Channel bodies and Part bodies. If a future construct
// adds a fifth evaluation site without instrumenting it, this test is what
// notices.
func TestTracerSeesEveryConstruct(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers

Channel "total":
    Maximum Technique: Sum

Simple Domain: Repeat 2
    Cursed Technique: Map Each
        Using: (x) -> x + 1

Part "1":
    Maximum Technique: Count
    Reveal: stdout
`
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		t.Fatal(err)
	}

	rec := &recordingTracer{}
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader("1,2,3"), Stdout: &out, Trace: rec}
	if _, err := Run(pipe, ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Nested nodes must be reported at depth 1, inside a labelled frame.
	wantFrames := []string{`Channel "total"`, "Repeat 2 iter 1/2", "Repeat 2 iter 2/2", `Part "1"`}
	for _, want := range wantFrames {
		if !rec.sawFrame(want) {
			t.Errorf("tracer never saw frame %q; frames = %v", want, rec.frames)
		}
	}
	for _, want := range []string{"Sum", "Map Each", "Count", "Emit"} {
		if !rec.sawNestedPrim(want) {
			t.Errorf("%s ran inside a body but was reported at depth 0", want)
		}
	}
	// The loop body ran twice, once per iteration.
	if n := rec.countPrim("Map Each"); n != 2 {
		t.Errorf("Map Each reported %d times, want 2 (once per iteration)", n)
	}
	// Every frame that was pushed was popped.
	if rec.depth != 0 {
		t.Errorf("frame stack ended at depth %d, want 0", rec.depth)
	}
}

// recordingTracer keeps enough of the trace to assert on structure.
type recordingTracer struct {
	steps  []ir.StepEvent
	frames []string
	depth  int
}

func (r *recordingTracer) Step(e ir.StepEvent) { r.steps = append(r.steps, e) }
func (r *recordingTracer) PushFrame(label string) {
	r.frames = append(r.frames, label)
	r.depth++
}
func (r *recordingTracer) PopFrame() { r.depth-- }

func (r *recordingTracer) sawFrame(label string) bool {
	for _, f := range r.frames {
		if f == label {
			return true
		}
	}
	return false
}

func (r *recordingTracer) sawNestedPrim(prim string) bool {
	for _, s := range r.steps {
		if s.Node.Prim == prim && s.Depth > 0 && s.Frame != "" {
			return true
		}
	}
	return false
}

func (r *recordingTracer) countPrim(prim string) int {
	n := 0
	for _, s := range r.steps {
		if s.Node.Prim == prim {
			n++
		}
	}
	return n
}
