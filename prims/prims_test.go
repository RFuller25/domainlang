package prims

import (
	"bytes"
	"strings"
	"testing"

	"domain/ir"
	"domain/lexer"
	"domain/parser"
)

func resolveSrc(t *testing.T, src string) (*ir.Pipeline, error) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Resolve(prog)
}

const day1Src = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`

func TestResolveDay1TypeChecks(t *testing.T) {
	pipe, err := resolveSrc(t, day1Src)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	wantPrims := []string{
		"Read Source", "Split", "Split Each", "Convert To Integers",
		"Sum Each Group", "Sort", "SelectTopK", "Emit",
	}
	if len(pipe.Nodes) != len(wantPrims) {
		t.Fatalf("expected %d nodes, got %d", len(wantPrims), len(pipe.Nodes))
	}
	for i, w := range wantPrims {
		if pipe.Nodes[i].Prim != w {
			t.Fatalf("node %d: got %q want %q", i, pipe.Nodes[i].Prim, w)
		}
	}
	// Final output type before Emit must be Int (Top 3 then Sum).
	if !pipe.Nodes[6].Out.Equal(ir.Int()) {
		t.Fatalf("SelectTopK output: got %s want Int", pipe.Nodes[6].Out)
	}
}

func TestResolveTypeMismatch(t *testing.T) {
	// Feeding Sort (wants List<Int>) directly raw Text must be rejected.
	src := `Cursed Energy: input.txt
Domain Expansion: Quicksort, Descending
Reveal: stdout
`
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected a type-mismatch error")
	}
	if !strings.Contains(err.Error(), "Sort expects input of type List<Int>") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveUnknownOperation(t *testing.T) {
	src := "Cursed Energy: input.txt\nMaximum Technique: Frobnicate\nReveal: stdout\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected unknown-operation error")
	}
	if !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// runNode is a tiny helper to evaluate a single node in isolation.
func runNode(t *testing.T, node *ir.Node, in ir.Value) ir.Value {
	t.Helper()
	ctx := &ir.Context{Stdout: &bytes.Buffer{}}
	out, err := node.Eval(ctx, in)
	if err != nil {
		t.Fatalf("eval %s: %v", node.Prim, err)
	}
	return out
}

func TestSplitPrimitive(t *testing.T) {
	pos := tokenPos()
	node, err := split.Build(opWithString("Split Text", "\n\n"), ArgSet{}, ir.Text(), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, node, "a\n\nb\n\nc").([]ir.Value)
	if len(out) != 3 || out[0] != "a" || out[2] != "c" {
		t.Fatalf("split result: %v", out)
	}
}

func TestSumEachGroupPrimitive(t *testing.T) {
	pos := tokenPos()
	op := opWords("Sum", "Each", "Group")
	node, err := sumEachGroup.Build(op, ArgSet{}, ir.List(ir.List(ir.Int())), pos)
	if err != nil {
		t.Fatal(err)
	}
	in := []ir.Value{
		[]ir.Value{int64(1000), int64(2000), int64(3000)},
		[]ir.Value{int64(4000)},
	}
	out := runNode(t, node, in).([]ir.Value)
	if len(out) != 2 || out[0].(int64) != 6000 || out[1].(int64) != 4000 {
		t.Fatalf("sum each group: %v", out)
	}
}

func TestSortDescendingPrimitive(t *testing.T) {
	pos := tokenPos()
	op := opWords("Quicksort")
	op.Modifiers = []string{"Descending"}
	node, err := sortPrim.Build(op, ArgSet{}, ir.List(ir.Int()), pos)
	if err != nil {
		t.Fatal(err)
	}
	if !node.Swappable {
		t.Fatal("Sort node should be marked Swappable (Domain Expansion)")
	}
	in := []ir.Value{int64(3), int64(1), int64(2)}
	out := runNode(t, node, in).([]ir.Value)
	if out[0].(int64) != 3 || out[2].(int64) != 1 {
		t.Fatalf("descending sort: %v", out)
	}
}

func TestSelectTopKSumPrimitive(t *testing.T) {
	pos := tokenPos()
	op := opWords("Select", "Top")
	op.Ints = []int64{2}
	op.Modifiers = []string{"Sum"}
	node, err := selectTopK.Build(op, ArgSet{}, ir.List(ir.Int()), pos)
	if err != nil {
		t.Fatal(err)
	}
	if !node.Out.Equal(ir.Int()) {
		t.Fatalf("expected Int output, got %s", node.Out)
	}
	in := []ir.Value{int64(24000), int64(11000), int64(10000), int64(6000)}
	out := runNode(t, node, in).(int64)
	if out != 35000 {
		t.Fatalf("top-2 sum: got %d want 35000", out)
	}
}
