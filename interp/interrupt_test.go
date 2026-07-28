package interp

import (
	"errors"
	"strings"
	"testing"
	"time"

	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// A stopped Interrupter ends the run at the next node boundary, and the abort
// arrives as an ordinary error rather than as the panic it travelled on.
func TestInterrupterStopsARun(t *testing.T) {
	ran := 0
	node := func() *ir.Node {
		return &ir.Node{
			Prim: "Count", Out: ir.Int(),
			Eval: func(_ *ir.Context, in ir.Value) (ir.Value, error) {
				ran++
				return in, nil
			},
		}
	}
	pipe := &ir.Pipeline{Nodes: []*ir.Node{node(), node(), node()}}

	it := ir.NewInterrupter(nil)
	it.Stop() // stopped before the run even starts
	_, err := Run(pipe, &ir.Context{Trace: it})

	if !errors.Is(err, ir.ErrInterrupted) {
		t.Fatalf("err = %v, want ErrInterrupted", err)
	}
	if ran != 1 {
		t.Errorf("ran %d nodes, want 1 (the check is after the first evaluation)", ran)
	}
	if strings.Contains(errString(err), "internal error") {
		t.Error("an interrupt was reported as an internal error")
	}
}

// The interrupt reaches inside a loop body, which is the case it exists for: a
// While loop with a predicate that never goes false has no other way out.
func TestInterrupterStopsARunawayLoop(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Extract Integers\n" +
		"Maximum Technique: Sum\n" +
		"Simple Domain: While\n" +
		"    Using: (v) -> v > 0\n" +
		"    Cursed Technique: Apply\n" +
		"        Using: (v) -> v + 1\n"
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
		t.Fatalf("resolve: %v", err)
	}

	it := ir.NewInterrupter(nil)
	done := make(chan error, 1)
	go func() {
		_, err := Run(pipe, &ir.Context{Stdin: strings.NewReader("1"), Trace: it})
		done <- err
	}()

	// Let the loop get going, then stop it.
	time.Sleep(20 * time.Millisecond)
	it.Stop()

	select {
	case err := <-done:
		if !errors.Is(err, ir.ErrInterrupted) {
			t.Fatalf("err = %v, want ErrInterrupted", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the loop kept running after Stop")
	}
}

// An Interrupter forwards to the tracer it wraps, so a run can be profiled and
// interruptible at once — which is what `:stats` on a slow program needs.
func TestInterrupterForwardsToItsInnerTracer(t *testing.T) {
	stats := NewStats()
	it := ir.NewInterrupter(stats)
	pipe := &ir.Pipeline{Nodes: []*ir.Node{{
		Prim: "Sum", Out: ir.Int(),
		Eval: func(_ *ir.Context, in ir.Value) (ir.Value, error) { return in, nil },
	}}}

	if _, err := Run(pipe, &ir.Context{Trace: it}); err != nil {
		t.Fatal(err)
	}
	if stats.Stages() != 1 {
		t.Errorf("inner tracer saw %d stages, want 1", stats.Stages())
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
