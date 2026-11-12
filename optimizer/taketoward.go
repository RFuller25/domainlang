package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
)

// pushTakeBack moves a prefix cut in front of the stage that was made to feed
// it:
//
//	Map Each f · Apply (xs) -> take(xs, N)   →  Apply (xs) -> take(xs, N) · Map Each f
//	Window s t · Apply (xs) -> take(xs, N)   →  Apply (xs) -> take(xs, L) · Window s t
//
// A map preserves length and order, so the first N mapped values are the map of
// the first N values — f runs on N elements, not on all of them. f must be
// pure and total: the evaluations being dropped are the ones past N, and one
// that can fail would report an error the cut makes disappear.
//
// A window list is built from the input in order, window k covering the
// elements from k·t to k·t+s. The first N windows (N ≥ 1) need exactly the
// first L = (N-1)·t + s elements, and windows over that prefix are those N —
// or, when the input is shorter than L, every window there is, which is what
// take of a shorter list is. So the Window runs over L elements instead of
// the lot and the trailing take has nothing left to do. Size and step must be
// literals (a measured one is read from the value entering the Window, which
// the cut changes), and N ≤ 0 is L = 0: nothing in, nothing out. Window has no
// failure the cut could hide — a literal size and step were checked ≥ 1 when
// the program was resolved.
//
// take clamps (a negative count is 0, an oversized one is the whole list), and
// both rewrites inherit that: the cut is the same builtin, with the same N.
func pushTakeBack(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		n, ok := takeCount(b)
		if !ok || a.In == nil || a.In.Kind != ir.KList || b.In == nil || b.In.Kind != ir.KList {
			return nil, "", false
		}
		switch a.Prim {
		case "Map Each":
			if _, ok := pureTotal(a); !ok {
				return nil, "", false
			}
			return []*ir.Node{newTakeNode(a.In, n, b.Pos), a},
				fmt.Sprintf("Domain rewrote Map Each + Apply (take %d) → Apply (take %d) + Map Each (the lambda runs on the elements kept, not on all of them). Guaranteed hit.", n, n),
				true
		case "Window":
			if hasMeasuredArg(a) {
				return nil, "", false
			}
			size, sizeOK := readArg(a, "size")
			step, stepOK := readArg(a, "step")
			if !sizeOK || !stepOK || size.measured() || step.measured() {
				return nil, "", false
			}
			const limit = 1 << 30
			if size.lit < 1 || step.lit < 1 || size.lit > limit || step.lit > limit || n > limit {
				return nil, "", false
			}
			need := int64(0)
			if n >= 1 {
				need = (n-1)*step.lit + size.lit
			}
			return []*ir.Node{newTakeNode(a.In, need, b.Pos), a},
				fmt.Sprintf("Domain rewrote Window + Apply (take %d) → Apply (take %d) + Window (only the windows that are kept get built). Guaranteed hit.", n, need),
				true
		}
		return nil, "", false
	})
}

// takeCount recognizes `Apply (x) -> take(x, N)` for any literal N, negative
// included (take clamps it to 0).
func takeCount(n *ir.Node) (int64, bool) {
	if n.Prim != "Apply" {
		return 0, false
	}
	lam := nodeLambda(n)
	if lam == nil || len(lam.Params) != 1 {
		return 0, false
	}
	call, ok := lam.Body.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || builtinName(call) != "take" {
		return 0, false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	if !ok || arg.Name != lam.Params[0] {
		return 0, false
	}
	switch c := call.Args[1].(type) {
	case *ast.IntLit:
		return c.Value, true
	case *ast.UnaryExpr:
		if lit, isLit := c.X.(*ast.IntLit); isLit && c.Op == token.MINUS {
			return -lit.Value, true
		}
	}
	return 0, false
}

// newTakeNode builds `Apply (xs) -> take(xs, n)` over a List of type in.
func newTakeNode(in *ir.Type, n int64, pos token.Position) *ir.Node {
	lam := &ast.Lambda{
		Params: []string{"xs"},
		Body: &ast.CallExpr{
			Fn:   &ast.Ident{Name: "take", Pos: pos},
			Args: []ast.Expr{&ast.Ident{Name: "xs", Pos: pos}, &ast.IntLit{Value: n, Pos: pos}},
			Pos:  pos,
		},
		Pos: pos,
	}
	return &ir.Node{
		Prim:    "Apply",
		In:      in,
		Out:     in,
		Display: fmt.Sprintf("Apply (take %d)", n),
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			r, err := eval.EvalLambdaTyped(lam, []*ir.Type{in}, v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Apply", Pos: pos, Msg: err.Error()}
			}
			return r, nil
		},
	}
}
