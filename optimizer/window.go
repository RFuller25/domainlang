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
		size, _ := a.Meta["size"].(int64)
		step, _ := a.Meta["step"].(int64)
		if size < 1 || step < 1 {
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
		fused := &ir.Node{
			Prim:    "WindowedReduce",
			In:      a.In,
			Out:     b.Out,
			Display: fmt.Sprintf("Cursed Sliding-Window %s (size %d, step %d)", opName, size, step),
			Meta:    map[string]any{"size": size, "step": step, "op": op},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsIntSlice(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "WindowedReduce", Pos: pos, Msg: err.Error()}
				}
				if op == "sum" {
					return ir.IntsToValue(WindowedSums(xs, size, step)), nil
				}
				return ir.IntsToValue(WindowedExtrema(xs, size, step, op == "min")), nil
			},
		}
		return []*ir.Node{fused},
			fmt.Sprintf("Domain rewrote Window %d + Map Each (%s) → Cursed Sliding-Window %s (one pass, no window lists). Guaranteed hit.",
				size, op, opName),
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

// WindowedSums returns the sums of every fully-contained window, via prefix
// sums: O(n) regardless of window size or step.
func WindowedSums(xs []int64, size, step int64) []int64 {
	pre := make([]int64, len(xs)+1)
	for i, x := range xs {
		pre[i+1] = pre[i] + x
	}
	out := []int64{}
	for i := int64(0); i+size <= int64(len(xs)); i += step {
		out = append(out, pre[i+size]-pre[i])
	}
	return out
}

// WindowedExtrema returns the max (or min) of every fully-contained window
// using a monotonic deque of candidate indices: every element is pushed and
// popped at most once, so the whole scan is O(n) regardless of window size.
func WindowedExtrema(xs []int64, size, step int64, min bool) []int64 {
	beats := func(a, b int64) bool {
		if min {
			return a < b
		}
		return a > b
	}
	out := []int64{}
	deque := []int64{} // indices into xs; xs[deque[0]] is the current extremum
	next := int64(0)   // next index to admit into the deque
	for s := int64(0); s+size <= int64(len(xs)); s += step {
		for ; next < s+size; next++ {
			for len(deque) > 0 && !beats(xs[deque[len(deque)-1]], xs[next]) {
				deque = deque[:len(deque)-1]
			}
			deque = append(deque, next)
		}
		for deque[0] < s {
			deque = deque[1:]
		}
		out = append(out, xs[deque[0]])
	}
	return out
}
