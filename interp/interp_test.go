package interp

import (
	"strings"
	"testing"

	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// TestBindingVowPassAndFail exercises a vow over a list value end-to-end.
func TestBindingVowPassAndFail(t *testing.T) {
	mkPipe := func(vow string) *ir.Pipeline {
		src := "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \",\"\n" +
			"Channeled Energy: Convert List to Integers\n" +
			vow + "\n" +
			"Reveal: stdout\n"
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
		return pipe
	}

	// Passing vow: all values > 0.
	pipe := mkPipe("Binding Vow: All Values > 0")
	ctx := &ir.Context{Stdin: strings.NewReader("1,2,3"), Stdout: &strings.Builder{}}
	if _, err := Run(pipe, ctx); err != nil {
		t.Fatalf("vow should pass: %v", err)
	}

	// Failing vow: count equals 99.
	pipe = mkPipe("Binding Vow: Count Equals 99")
	ctx = &ir.Context{Stdin: strings.NewReader("1,2,3"), Stdout: &strings.Builder{}}
	_, err := Run(pipe, ctx)
	if err == nil {
		t.Fatal("vow should have been violated")
	}
	if !strings.Contains(err.Error(), "vow violated") {
		t.Fatalf("unexpected error: %v", err)
	}
	var re *ir.RuntimeError
	if !asRuntimeError(err, &re) {
		t.Fatalf("expected *ir.RuntimeError, got %T", err)
	}
}

func asRuntimeError(err error, target **ir.RuntimeError) bool {
	if re, ok := err.(*ir.RuntimeError); ok {
		*target = re
		return true
	}
	return false
}

// TestRunMultiNodeHappyPath threads a value through several hand-built nodes
// and checks both the final result and that each node actually saw the
// previous node's output (not the original input).
func TestRunMultiNodeHappyPath(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "seed", Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			if v != nil {
				t.Fatalf("first node should see a nil initial value, got %v", v)
			}
			return int64(1), nil
		}},
		{Prim: "double", Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			n, ok := v.(int64)
			if !ok || n != 1 {
				t.Fatalf("second node should see 1 from the first node, got %v", v)
			}
			return n * 2, nil
		}},
		{Prim: "plusTen", Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			n, ok := v.(int64)
			if !ok || n != 2 {
				t.Fatalf("third node should see 2 from the second node, got %v", v)
			}
			return n + 10, nil
		}},
	}}
	got, err := Run(pipe, &ir.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got.(int64) != 12 {
		t.Fatalf("got %v want 12", got)
	}
}

// TestRunEmptyPipeline covers the degenerate zero-node case.
func TestRunEmptyPipeline(t *testing.T) {
	got, err := Run(&ir.Pipeline{}, &ir.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("empty pipeline should return a nil result, got %v", got)
	}
}

// TestRunStopsAtFirstError confirms a node's error short-circuits the
// remaining pipeline (a later node must not run).
func TestRunStopsAtFirstError(t *testing.T) {
	ranSecond := false
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "fails", Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			return nil, &ir.RuntimeError{Prim: "fails", Msg: "boom"}
		}},
		{Prim: "shouldNotRun", Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			ranSecond = true
			return v, nil
		}},
	}}
	if _, err := Run(pipe, &ir.Context{}); err == nil {
		t.Fatal("expected an error")
	}
	if ranSecond {
		t.Fatal("a node after a failing node must not run")
	}
}

// TestRunRecoversFromPanic confirms a panicking primitive surfaces as a
// clean error rather than crashing the process — the README promises "the
// interpreter recovers from internal panics so users only ever see
// positioned errors."
func TestRunRecoversFromPanic(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "panics", Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			panic("boom: internal invariant violated")
		}},
	}}
	got, err := Run(pipe, &ir.Context{})
	if err == nil {
		t.Fatal("expected an error, not a panic escaping Run")
	}
	if got != nil {
		t.Fatalf("expected a nil result alongside the error, got %v", got)
	}
	if !strings.Contains(err.Error(), "internal error during interpretation") {
		t.Fatalf("expected a clean internal-error message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the panic value to be included, got: %v", err)
	}
}

// TestRunRecoversFromNilPointerPanic covers a panic that isn't a plain
// string (a runtime error value), which is the more common real-world shape
// (index out of range, nil dereference, ...).
func TestRunRecoversFromNilPointerPanic(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "panics", Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			var xs []int
			_ = xs[5] // index out of range
			return nil, nil
		}},
	}}
	if _, err := Run(pipe, &ir.Context{}); err == nil {
		t.Fatal("expected an error, not a panic escaping Run")
	}
}

// TestRunInitializesChannelsMap confirms ctx.Channels is initialized to a
// non-nil map before any node runs, even when the caller passes a bare
// *ir.Context with Channels left at its zero value (nil) — Channel
// consumers write into this map and would otherwise panic on a nil map
// write.
func TestRunInitializesChannelsMap(t *testing.T) {
	var sawChannels map[string]ir.Value
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "checkChannels", Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			sawChannels = ctx.Channels
			ctx.Channels["probe"] = int64(1) // must not panic on a nil map
			return v, nil
		}},
	}}
	ctx := &ir.Context{} // Channels left nil
	if _, err := Run(pipe, ctx); err != nil {
		t.Fatal(err)
	}
	if sawChannels == nil {
		t.Fatal("Run must initialize ctx.Channels to a non-nil map before running nodes")
	}
	if ctx.Channels["probe"] != int64(1) {
		t.Fatalf("channel write did not persist: %v", ctx.Channels)
	}
}

// TestRunPreservesExistingChannels confirms Run does not clobber a
// Channels map the caller already populated.
func TestRunPreservesExistingChannels(t *testing.T) {
	ctx := &ir.Context{Channels: map[string]ir.Value{"seed": int64(42)}}
	pipe := &ir.Pipeline{Nodes: []*ir.Node{
		{Prim: "readChannel", Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
			return ctx.Channels["seed"], nil
		}},
	}}
	got, err := Run(pipe, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.(int64) != 42 {
		t.Fatalf("got %v want 42 (pre-populated Channels must survive)", got)
	}
}
