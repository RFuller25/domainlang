package optimizer

import (
	"domain/ast"
	"domain/ir"
)

// normalizeListBuiltins turns an `Apply` whose lambda is one whole-list builtin
// applied to its parameter into the primitive of the same job:
//
//	Apply (x) -> sort(x)     →  Sort           (List<Int>)
//	Apply (x) -> reverse(x)  →  Reverse        (List)
//	Apply (x) -> unique(x)   →  Unique         (List of a keyable type)
//	Apply (x) -> sum(x)      →  Sum            (List<Int>)
//	Apply (x) -> length(x)   →  Count          (List)
//
// The passes that know Sort, Reverse, Unique, Sum and Count — a hundred rules
// between them — are keyed on the primitive, not on a lambda that happens to
// call the builtin, so a program written the second way got none of them.
//
// Only the builtins that agree with their primitive exactly, and cannot fail:
// both orderings are ir.Compare, both Unique keep first-seen order, both Sum
// wrap an int64, a List's length is its Count. Deliberately absent: `first`,
// `last`, `max`, `min` (partial — the builtin says "max of an empty list is
// undefined (in Apply)", the primitive "Max of an empty list is undefined
// (in Max)", so swapping would change the words of a failure), `take`
// (there is no primitive that takes a prefix), and Float lists (the passes
// that follow are Int-only anyway).
func normalizeListBuiltins(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, list := range nodeLists(p) {
		for i, n := range list {
			name, ok := wholeListBuiltin(n)
			if !ok {
				continue
			}
			var repl *ir.Node
			switch name {
			case "sort":
				if isIntList(n.In) {
					repl = newSortNode(n, false)
					repl.Display = "Quicksort, Ascending (from Apply)"
				}
			case "sum":
				if isIntList(n.In) {
					repl = newSumNode(n)
				}
			case "reverse":
				repl = newReverseNode(n)
			case "unique":
				if ir.ListKeyable(n.In.Elem) {
					repl = newUniqueNode(n)
				}
			case "length":
				repl = newCountNode(n)
				repl.Display = "Count (from Apply)"
			}
			if repl == nil {
				continue
			}
			repl.Out = n.Out
			list[i] = repl
			rewrites = append(rewrites, Rewrite{Message: "Domain rewrote Apply (" + name + ") → " + repl.Prim +
				" (a whole-list builtin, spelled as the primitive so the passes that know it can see it). Guaranteed hit."})
		}
	}
	return rewrites
}

// wholeListBuiltin matches `Apply (x) -> name(x)` over a List.
func wholeListBuiltin(n *ir.Node) (string, bool) {
	if n.Prim != "Apply" || n.In == nil || n.In.Kind != ir.KList || n.In.Elem == nil {
		return "", false
	}
	lam := nodeLambda(n)
	if lam == nil || len(lam.Params) != 1 {
		return "", false
	}
	call, ok := lam.Body.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	if !ok || arg.Name != lam.Params[0] {
		return "", false
	}
	switch name := builtinName(call); name {
	case "sort", "reverse", "unique", "sum", "length":
		return name, true
	}
	return "", false
}

func newSumNode(from *ir.Node) *ir.Node {
	pos := from.Pos
	return &ir.Node{
		Prim:    "Sum",
		In:      from.In,
		Out:     ir.Int(),
		Display: "Sum (from Apply)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsIntSlice(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Sum", Pos: pos, Msg: err.Error()}
			}
			var s int64
			for _, x := range xs {
				s += x
			}
			return s, nil
		},
	}
}

func newReverseNode(from *ir.Node) *ir.Node {
	pos := from.Pos
	return &ir.Node{
		Prim:    "Reverse",
		In:      from.In,
		Out:     from.In,
		Display: "Reverse (from Apply)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			items, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Reverse", Pos: pos, Msg: err.Error()}
			}
			out := make([]ir.Value, len(items))
			for i, e := range items {
				out[len(items)-1-i] = e
			}
			return out, nil
		},
	}
}

func newUniqueNode(from *ir.Node) *ir.Node {
	pos := from.Pos
	return &ir.Node{
		Prim:    "Unique",
		In:      from.In,
		Out:     from.In,
		Display: "Unique (from Apply)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			items, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Unique", Pos: pos, Msg: err.Error()}
			}
			return ir.SetFromList(items).Elems(), nil
		},
	}
}
