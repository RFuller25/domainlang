package interp

import (
	"bytes"
	"strings"
	"testing"

	"domain/eval"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// recordWithApplications is record() with the lambda watcher wired in, the way
// `domain expansion: visualize` wires it.
func recordWithApplications(t *testing.T, src, stdin string) *Recorder {
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
	rec := NewRecorder(0)
	defer eval.WatchApplications(rec.Applied)()
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &out, Trace: rec}
	_, _ = Run(pipe, ctx)
	return rec
}

// steps flattens a recording to its step rows, in tree order.
func steps(rec *Recorder) []*Step {
	var out []*Step
	var walk func([]*TraceNode)
	walk = func(nodes []*TraceNode) {
		for _, n := range nodes {
			if !n.IsFrame() {
				out = append(out, n.Step)
			}
			walk(n.Children)
		}
	}
	walk(rec.Roots())
	return out
}

const applyProgram = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> x + 1
Maximum Technique: Sum
Reveal: stdout
`

// A lambda is applied inside a primitive, below the trace hook, so the only
// thing that can attribute one to a stage is the step that reports next.
func TestRecorderAttributesApplicationsToTheirStep(t *testing.T) {
	rec := recordWithApplications(t, applyProgram, "1,2,3")

	var withApply []*Step
	for _, s := range steps(rec) {
		if s.Apply != nil {
			withApply = append(withApply, s)
		}
	}
	if len(withApply) != 1 {
		labels := make([]string, 0, len(withApply))
		for _, s := range withApply {
			labels = append(labels, s.Node.Prim)
		}
		t.Fatalf("%d steps carry an application (%v), want only the one that ran the lambda",
			len(withApply), labels)
	}
	a := withApply[0].Apply
	if a.Count != 3 {
		t.Errorf("Count = %d, want 3 (one per element)", a.Count)
	}
	if len(a.Args) != 1 {
		t.Fatalf("Args = %v, want one", a.Args)
	}
	if n, _ := a.Args[0].(int64); n != 1 {
		t.Errorf("the kept application is on %v, want the first element (1)", a.Args[0])
	}
	if a.Lambda == nil || len(a.Lambda.Params) != 1 {
		t.Errorf("the lambda should come through with its parameters: %+v", a.Lambda)
	}
}

// A step that ran no lambda must not inherit the previous step's, which is what
// clearing on every step — not only on the ones that had one — buys.
func TestRecorderClearsApplicationsBetweenSteps(t *testing.T) {
	rec := recordWithApplications(t, applyProgram, "1,2,3")
	all := steps(rec)
	last := all[len(all)-1]
	if last.Apply != nil {
		t.Errorf("%s ran no expression but carries one", last.Node.Prim)
	}
}

// Without a watcher nothing is recorded, so an ordinary run pays nothing and a
// recording made without wiring eval up is simply expression-less.
func TestRecorderWithoutTheWatcherRecordsNoApplications(t *testing.T) {
	rec := record(t, applyProgram, "1,2,3", 0)
	for _, s := range steps(rec) {
		if s.Apply != nil {
			t.Fatalf("%s carries an application with no watcher installed", s.Node.Prim)
		}
	}
}

// The application is replayable: that is the whole contract between the
// recorder and eval, and the reason the recording stays this small.
func TestRecordedApplicationReplays(t *testing.T) {
	rec := recordWithApplications(t, applyProgram, "1,2,3")
	for _, s := range steps(rec) {
		if s.Apply == nil {
			continue
		}
		root, err := eval.TraceLambda(s.Apply.Lambda, s.Apply.Types, s.Apply.Args...)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if n, _ := root.Value.(int64); n != 2 {
			t.Errorf("replaying (x) -> x + 1 on 1 gave %v, want 2", root.Value)
		}
		return
	}
	t.Fatal("no step carried an application")
}
