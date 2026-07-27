package prims

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Domain Expansion: Sliding Reduce — List<Int> -> List<Int>: the reduction of
// every fully-contained window, in one pass.
//
//	Domain Expansion: Sliding Reduce 3
//	    Mode: Sum                      # Sum | Max | Min | Product
//
// The optimizer already reaches this algorithm from below: `Window n` feeding
// a `Map Each` that reduces each window fuses into it (optimizer.fuseWindowReduce).
// This primitive is the same node asked for by name, so a program that knows
// it wants the streaming form can say so — and gets it whether or not the
// optimizer is running.
//
// Sum is prefix sums and Max/Min a monotonic deque, so both are O(n) in the
// list length no matter how wide the window is; the naive spelling is
// O(n·size) and materializes every window besides. Product has no such trick
// (a zero anywhere destroys the prefix), so it is the honest per-window scan —
// still without building the windows.
var slidingReduce = &Primitive{
	ID:      "Sliding Reduce",
	Keyword: "Domain Expansion",
	Match: func(op *ast.Operation) bool {
		return (hasWord(op, "Sliding") || hasWord(op, "Rolling")) && hasWord(op, "Reduce")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		want := ir.List(ir.Int())
		if !in.Equal(want) {
			return nil, typeErr(pos, "Sliding Reduce", want, in)
		}
		if len(op.Ints) == 0 {
			return nil, &ResolveError{Pos: pos,
				Msg: "Sliding Reduce requires a window size, e.g. Sliding Reduce 3"}
		}
		size := op.Ints[0]
		step := int64(1)
		if len(op.Ints) > 1 {
			step = op.Ints[1]
		}
		if size < 1 || step < 1 {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Sliding Reduce size and step must be >= 1, got size %d step %d", size, step)}
		}
		mode, _ := args.Ident("Mode")
		if mode == "" {
			mode = "Sum"
		}
		op2, ok := slidingOps[strings.ToLower(mode)]
		if !ok {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Sliding Reduce Mode must be Sum, Max, Min, or Product (got %q)", mode)}
		}

		display := fmt.Sprintf("Cursed Sliding-Window %s (size %d, step %d)",
			slidingDisplay[op2], size, step)
		return &ir.Node{
			// The same Prim the optimizer's fusion produces, so the two share
			// one Go lowering and one set of parity tests.
			Prim:      "WindowedReduce",
			In:        want,
			Out:       want,
			Display:   display,
			Swappable: true,
			Meta:      map[string]any{"size": size, "step": step, "op": op2},
			Pos:       pos,
			Eval:      slidingEval(op2, size, step, pos),
		}, nil
	},
}

// slidingOps maps the user-facing Mode names onto the op tags carried in Meta
// (which the optimizer and the Go backend both already speak).
var slidingOps = map[string]string{
	"sum": "sum", "max": "max", "maximum": "max",
	"min": "min", "minimum": "min", "product": "product",
}

// slidingDisplay names an op tag for --explain, matching the wording the
// optimizer's own fusion message uses.
var slidingDisplay = map[string]string{
	"sum": "Sum", "max": "Max", "min": "Min", "product": "Product",
}

func slidingEval(op string, size, step int64, pos token.Position) func(*ir.Context, ir.Value) (ir.Value, error) {
	return func(_ *ir.Context, v ir.Value) (ir.Value, error) {
		xs, err := ir.AsIntSlice(v)
		if err != nil {
			return nil, runtimeErr("Sliding Reduce", pos, "%v", err)
		}
		switch op {
		case "sum":
			return ir.IntsToValue(ir.WindowedSums(xs, size, step)), nil
		case "product":
			return ir.IntsToValue(ir.WindowedProducts(xs, size, step)), nil
		default:
			return ir.IntsToValue(ir.WindowedExtrema(xs, size, step, op == "min")), nil
		}
	}
}
