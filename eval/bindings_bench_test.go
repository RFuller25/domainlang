package eval

import (
	"fmt"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// What the bindings in scope cost an application that does not read one.
//
// EvalLambdaTyped seeds every application's environment with every binding in
// scope before it knows whether the body reads any of them (see bindings.go),
// so this is the floor a stage pays for merely being inside a `Consider`
// scope — not the cost of using it.
//
// The numbers are load-bearing for the global-variables design
// (docs/superpowers/specs/2026-08-19-global-variables-design.md §2.1): globals
// are program-scoped, so "in scope" would mean *all of them, in every lambda*.
// The measured growth here is why they are resolved to slot indices at compile
// time and never enter an Env at all. Keep this benchmark green with that
// design: a globals implementation that shows up on these rows has put them in
// the wrong place.
func benchBindingsLambda() *ast.Lambda {
	p := token.Position{}
	return &ast.Lambda{
		Params: []string{"x"},
		Body: &ast.BinaryExpr{
			Op:    token.PLUS,
			Left:  &ast.Ident{Name: "x", Pos: p},
			Right: &ast.IntLit{Value: 1, Pos: p},
			Pos:   p,
		},
		Pos: p,
	}
}

func benchWithBindings(b *testing.B, n int) {
	ResetBindings()
	defer ResetBindings()
	for i := range n {
		PushBinding(fmt.Sprintf("bind%d", i), int64(i), ir.Int())
	}
	lam := benchBindingsLambda()
	types := []*ir.Type{ir.Int()}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if _, err := EvalLambdaTyped(lam, types, int64(i)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyBindings0(b *testing.B)  { benchWithBindings(b, 0) }
func BenchmarkApplyBindings1(b *testing.B)  { benchWithBindings(b, 1) }
func BenchmarkApplyBindings4(b *testing.B)  { benchWithBindings(b, 4) }
func BenchmarkApplyBindings8(b *testing.B)  { benchWithBindings(b, 8) }
func BenchmarkApplyBindings16(b *testing.B) { benchWithBindings(b, 16) }

// What a *global* read costs, against the binding read it replaces.
//
// Both bodies are `(x) -> x + n`: one reads n through the binding stack, which
// means n was seeded into the environment this application built; the other
// reads it through a slot, which means the environment never heard of it. The
// gap between them is the whole argument for resolving global reads at compile
// time, and BenchmarkApplyGlobalRead should sit on BenchmarkApplyBindings0's
// row rather than BenchmarkApplyBindings1's.
func benchReadLambda(read ast.Expr) *ast.Lambda {
	p := token.Position{}
	return &ast.Lambda{
		Params: []string{"x"},
		Body: &ast.BinaryExpr{
			Op:    token.PLUS,
			Left:  &ast.Ident{Name: "x", Pos: p},
			Right: read,
			Pos:   p,
		},
		Pos: p,
	}
}

func BenchmarkApplyBindingRead(b *testing.B) {
	ResetBindings()
	defer ResetBindings()
	PushBinding("n", int64(7), ir.Int())
	lam := benchReadLambda(&ast.Ident{Name: "n"})
	types := []*ir.Type{ir.Int()}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if _, err := EvalLambdaTyped(lam, types, int64(i)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyGlobalRead(b *testing.B) {
	ResetBindings()
	defer ResetBindings()
	ResetGlobals(1)
	SetGlobal(0, int64(7))
	lam := benchReadLambda(&ast.GlobalRef{Slot: 0, Name: "n", Type: ir.Int()})
	types := []*ir.Type{ir.Int()}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if _, err := EvalLambdaTyped(lam, types, int64(i)); err != nil {
			b.Fatal(err)
		}
	}
}

// Eight of each, which is where the binding table's cost turns sharply
// (BenchmarkApplyBindings8) and where a program-scoped name would land.
func BenchmarkApplyBindingRead8(b *testing.B) {
	ResetBindings()
	defer ResetBindings()
	for i := range 8 {
		PushBinding(fmt.Sprintf("g%d", i), int64(i), ir.Int())
	}
	lam := benchReadLambda(&ast.Ident{Name: "g0"})
	types := []*ir.Type{ir.Int()}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if _, err := EvalLambdaTyped(lam, types, int64(i)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyGlobalRead8(b *testing.B) {
	ResetBindings()
	defer ResetBindings()
	ResetGlobals(8)
	for i := range 8 {
		SetGlobal(i, int64(i))
	}
	lam := benchReadLambda(&ast.GlobalRef{Slot: 0, Name: "g0", Type: ir.Int()})
	types := []*ir.Type{ir.Int()}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if _, err := EvalLambdaTyped(lam, types, int64(i)); err != nil {
			b.Fatal(err)
		}
	}
}
