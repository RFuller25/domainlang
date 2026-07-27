package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/ir"
)

// fuseWindowReduce recognizes `Window size [step]` over List<Int> feeding a
// `Map Each` whose lambda is a plain per-window reduction — `(w) -> sum(w)`,
// `(w) -> max(w)`, or `(w) -> min(w)` — and replaces the pair with a single
// streaming node: prefix sums for sum, a monotonic deque for max/min. The
// naive pipeline materializes every window (O(n·size) time and space); the
// rewrite is one O(n) pass that never builds a window list. Windows always
// hold size ≥ 1 elements, so the partial max/min builtins cannot hit their
// empty-list error and no failure path is discarded. Like the other
// arithmetic rewrites, sum assumes Domain's numeric model (values stay
// within int64).
func fuseWindowReduce(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Window" || b.Prim != "Map Each" {
			return nil, "", false
		}
		if a.In == nil || !a.In.Equal(ir.List(ir.Int())) {
			return nil, "", false
		}
		// The streaming helpers take the size and step as ordinary runtime
		// arguments, so a measured one rides along instead of standing the
		// rewrite down — the naive pipeline it replaces measures the same two
		// numbers from the same value, with the same bound checks.
		size, sizeOK := readArg(a, "size")
		step, stepOK := readArg(a, "step")
		if !sizeOK || !stepOK {
			return nil, "", false
		}
		if !size.measured() && size.lit < 1 || !step.measured() && step.lit < 1 {
			return nil, "", false
		}
		lam := nodeLambda(b)
		if lam == nil {
			return nil, "", false
		}
		op, ok := matchWindowReduce(lam)
		if !ok {
			return nil, "", false
		}

		opName := map[string]string{"sum": "Sum", "max": "Max", "min": "Min"}[op]
		pos := a.Pos
		meta := map[string]any{"op": op}
		size.writeMeta(meta, "size")
		step.writeMeta(meta, "step")
		fused := &ir.Node{
			Prim:    "WindowedReduce",
			In:      a.In,
			Out:     b.Out,
			Display: fmt.Sprintf("Cursed Sliding-Window %s (size %s, step %s)", opName, size.describe(), step.describe()),
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsIntSlice(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "WindowedReduce", Pos: pos, Msg: err.Error()}
				}
				// Measured in the order Window itself measures them, so a
				// program whose size and step would both fail reports the same
				// one either way.
				sz, err := size.value(v)
				if err != nil {
					return nil, err
				}
				st, err := step.value(v)
				if err != nil {
					return nil, err
				}
				if op == "sum" {
					return ir.IntsToValue(ir.WindowedSums(xs, sz, st)), nil
				}
				return ir.IntsToValue(ir.WindowedExtrema(xs, sz, st, op == "min")), nil
			},
		}
		return []*ir.Node{fused},
			fmt.Sprintf("Domain rewrote Window %s + Map Each (%s) → Cursed Sliding-Window %s (one pass, no window lists). Guaranteed hit.",
				size.describe(), op, opName),
			true
	})
}

// matchWindowReduce recognizes a one-parameter lambda whose whole body is
// `sum(w)`, `max(w)`, or `min(w)` applied to the parameter itself.
func matchWindowReduce(lam *ast.Lambda) (op string, ok bool) {
	if len(lam.Params) != 1 {
		return "", false
	}
	call, isCall := lam.Body.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return "", false
	}
	fn, fok := identName(call.Fn)
	arg, aok := identName(call.Args[0])
	if !fok || !aok || arg != lam.Params[0] {
		return "", false
	}
	switch fn {
	case "sum", "max", "min":
		return fn, true
	}
	return "", false
}
