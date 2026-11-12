package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
)

// The long way round, second helping.
//
// idioms.go matches what someone writes when they know one tool. The rules
// here came from reading programs written against real puzzles by someone who
// knew most of the tools and still took the long way, because the short way
// was not obvious from where they stood:
//
//   - a list checked for a duplicate by sorting it, windowing it in twos and
//     asking whether any window holds the same thing twice — or by comparing
//     every pair;
//   - `Window 2` used as "each element and the next", then picked apart again
//     with first/last or item;
//   - a Bool turned into a 0 or a 1 so that it can be summed;
//   - empty lists filtered out on the way into a Flatten that would have
//     ignored them;
//   - a trajectory iterated fifty thousand times because the program cannot
//     know when it first repeats, and then searched for where it did.
//
// Each is a correct program. None of them is slow because of what it asks for.

// ---------------------------------------------------------------------------
// Window 2, read by position: Pairs
// ---------------------------------------------------------------------------

// canonicalizeWindowPairs turns `Window 2` (step 1) into `Pairs` when the
// stage after it only ever reads the two ends of each window — first(w),
// last(w), item(w, 0), item(w, 1).
//
// The two primitives produce the same n-1 neighbours; the difference is the
// type. A window is a List, so every read of it is partial as far as any
// analysis can tell, and each window is its own allocation. A pair is a Tuple:
// its projections are total (isTotalIn), so the rewrites gated on totality
// reach the stage, and the compiler builds it as a loop-local and — for a
// count — not at all (emitPairsCount).
//
// Only a consumer whose *output* does not contain the window may follow, since
// the element type changes underneath it: Map Each, Count Matching, Sum By,
// Any and All. Their lambdas are rebuilt over the tuple with every positional
// read spelled as item(w, k). Nothing is dropped or reordered, so the lambda
// needs to be pure only in the sense every rewrite needs — nodeLambda's.
func canonicalizeWindowPairs(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Window" || hasMeasuredArg(a) || a.In == nil || a.In.Kind != ir.KList {
			return nil, "", false
		}
		size, sizeOK := readArg(a, "size")
		step, stepOK := readArg(a, "step")
		if !sizeOK || !stepOK || size.measured() || step.measured() || size.lit != 2 || step.lit != 1 {
			return nil, "", false
		}
		lam := nodeLambda(b)
		if lam == nil || len(lam.Params) != 1 {
			return nil, "", false
		}
		if _, isBlock := lam.Body.(*ast.BlockBody); isBlock {
			return nil, "", false
		}
		body, ok := endsOnly(lam.Body, lam.Params[0])
		if !ok {
			return nil, "", false
		}
		elem := a.In.Elem
		pairType := ir.Tuple(elem, elem)
		pairs := newPairsNode(a, pairType)
		onPairs := &ast.Lambda{Params: lam.Params, Body: body, Pos: lam.Pos}
		from := &ir.Node{In: pairs.Out, Out: b.Out, Pos: b.Pos, Display: b.Display}
		var consumer *ir.Node
		switch b.Prim {
		case "Map Each":
			consumer = newMapEachNode(from, onPairs, b.Display)
		case "Count Matching":
			consumer = newCountMatchingNode(from, onPairs)
			consumer.Display = b.Display
		case "Sum By":
			if b.Out == nil || b.Out.Kind != ir.KInt {
				return nil, "", false
			}
			consumer = newSumByNode(from, onPairs)
			consumer.Display = b.Display
		case "Any", "All":
			consumer = newQuantifierNode(from, b.Prim, onPairs)
			consumer.Display = b.Display
		default:
			return nil, "", false
		}
		return []*ir.Node{pairs, consumer},
			fmt.Sprintf("Domain rewrote Window 2 + %s → Pairs + %s (each window was only read at its two ends; a pair is a tuple, not a list). Guaranteed hit.", b.Prim, b.Prim),
			true
	})
}

// newPairsNode builds the Pairs primitive over from's input, mirroring
// prims.pairs: every element with the one after it.
func newPairsNode(from *ir.Node, pairType *ir.Type) *ir.Node {
	pos := from.Pos
	return &ir.Node{
		Prim:    "Pairs",
		In:      from.In,
		Out:     ir.List(pairType),
		Display: "Pairs (from Window 2)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Pairs", Pos: pos, Msg: err.Error()}
			}
			if len(xs) < 2 {
				return []ir.Value{}, nil
			}
			out := make([]ir.Value, len(xs)-1)
			for i := 0; i+1 < len(xs); i++ {
				out[i] = []ir.Value{xs[i], xs[i+1]}
			}
			return out, nil
		},
	}
}

// newMapEachNode builds a Map Each of lam over from.In, producing from.Out.
func newMapEachNode(from *ir.Node, lam *ast.Lambda, display string) *ir.Node {
	pos, elem := from.Pos, from.In.Elem
	return &ir.Node{
		Prim:    "Map Each",
		In:      from.In,
		Out:     from.Out,
		Display: display,
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			items, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Map Each", Pos: pos, Msg: err.Error()}
			}
			out := make([]ir.Value, len(items))
			for i, e := range items {
				r, err := eval.EvalLambdaTyped(lam, []*ir.Type{elem}, e)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Map Each", Pos: pos,
						Msg: fmt.Sprintf("element %d: %v", i, err)}
				}
				out[i] = r
			}
			return out, nil
		},
	}
}

// ---------------------------------------------------------------------------
// A Bool summed as a number
// ---------------------------------------------------------------------------

// fuseCountByIf recognizes a Sum By whose lambda only ever answers 1 or 0:
//
//	Sum By (x) -> if p(x) then 1 else 0   →  Count Matching p
//	Sum By (x) -> if p(x) then 0 else 1   →  Count Matching (not p)
//
// which is how a count is written by someone summing: there is no number in
// the list to add, so the Bool is turned into one first. (`Map Each` + `Sum`
// arrives here through fuseMapReduceBy, and a `Fold` through fuseFoldIdioms.)
//
// p must be total, for the reason fuseFoldIdioms gives: Sum By reports a
// failure under its own name, and Count Matching would report it under
// another.
func fuseCountByIf(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, list := range nodeLists(p) {
		for i, n := range list {
			if n.Prim != "Sum By" || n.In == nil || n.In.Kind != ir.KList || n.Out == nil || n.Out.Kind != ir.KInt {
				continue
			}
			lam := nodeLambda(n)
			if lam == nil || len(lam.Params) != 1 {
				continue
			}
			cond, ok := lam.Body.(*ast.CondExpr)
			if !ok || ast.HasNondeterminism(lam.Body) || !isTotalElementwise(cond.Cond, lam.Params[0], n.In.Elem) {
				continue
			}
			then, tOK := cond.Then.(*ast.IntLit)
			els, eOK := cond.Else.(*ast.IntLit)
			if !tOK || !eOK {
				continue
			}
			var pred ast.Expr
			switch {
			case then.Value == 1 && els.Value == 0:
				pred = cond.Cond
			case then.Value == 0 && els.Value == 1:
				pred = &ast.UnaryExpr{Op: token.NOT, X: cond.Cond, Pos: cond.Pos}
			default:
				continue
			}
			repl := newCountMatchingNode(n, &ast.Lambda{Params: lam.Params, Body: pred, Pos: lam.Pos})
			repl.Display = "Count Matching (from Sum By)"
			list[i] = repl
			rewrites = append(rewrites, Rewrite{Message: "Domain rewrote Sum By (if … then 1 else 0) → Count Matching (a Bool was being turned into a number to be summed). Guaranteed hit."})
		}
	}
	return rewrites
}

// ---------------------------------------------------------------------------
// Empty lists filtered out of a Flatten
// ---------------------------------------------------------------------------

// elideFilterBeforeFlatten drops `Filter (xs) -> length(xs) > 0` in front of
// a Flatten. An empty list contributes nothing to a concatenation, so the
// filter decides nothing — but it reads as necessary, and it builds a list.
func elideFilterBeforeFlatten(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Filter" || b.Prim != "Flatten" || a.In == nil || !a.In.Equal(b.In) {
			return nil, "", false
		}
		if a.In.Kind != ir.KList || a.In.Elem == nil || a.In.Elem.Kind != ir.KList {
			return nil, "", false
		}
		lam := nodeLambda(a)
		if lam == nil || len(lam.Params) != 1 {
			return nil, "", false
		}
		if some, ok := comparesWithZero(lam.Body, lam.Params[0], true); !ok || !some {
			return nil, "", false
		}
		return []*ir.Node{b},
			"Domain rewrote Filter (non-empty) + Flatten → Flatten (an empty list adds nothing to a concatenation). Guaranteed hit.",
			true
	})
}

// ---------------------------------------------------------------------------
// "Is anything in here twice?"
// ---------------------------------------------------------------------------

// fuseDuplicateCheck recognizes the two ways a duplicate is looked for without
// a set, and replaces each with one hash pass:
//
//	Sort · Pairs · Any (p) -> item(p, 0) = item(p, 1)       sort, then compare neighbours
//	All Pairs (Mode: Map) f · Any g, where g(f(a, b)) is a = b    compare everything with everything
//
// (and their `All … ikke a = b` mirror images, which ask whether everything is
// distinct). `Window 2` reaches the first shape through
// canonicalizeWindowPairs. The second is matched after composing the two
// lambdas and simplifying, so `if a = b then 0 else 1` followed by
// `(x) -> x = 0` is recognized for what it asks.
//
// The replacement is an Apply of `length(unique(xs)) < length(xs)` — builtins
// both backends already have — so O(n log n) plus a list of windows, or
// O(n²), becomes O(n). The element type must be keyable for `unique`, and
// Float-free: a NaN is not equal to itself, which a sort-and-compare notices
// and a set does not.
func fuseDuplicateCheck(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	var out []*ir.Node
	for i := 0; i < len(p.Nodes); i++ {
		n := p.Nodes[i]
		if repl, used, msg := duplicateCheck(p.Nodes[i:]); repl != nil {
			out = append(out, repl)
			rewrites = append(rewrites, Rewrite{Message: msg})
			i += used - 1
			continue
		}
		out = append(out, n)
	}
	p.Nodes = out
	return rewrites
}

func duplicateCheck(nodes []*ir.Node) (repl *ir.Node, used int, msg string) {
	first := nodes[0]
	if first.In == nil || first.In.Kind != ir.KList || !ir.ListKeyable(first.In.Elem) || typeHasFloat(first.In) {
		return nil, 0, ""
	}
	switch {
	case len(nodes) >= 3 && first.Prim == "Sort" && nodes[1].Prim == "Pairs":
		q := nodes[2]
		lam := nodeLambda(q)
		if (q.Prim != "Any" && q.Prim != "All") || lam == nil || len(lam.Params) != 1 {
			return nil, 0, ""
		}
		op, ok := comparesEnds(lam.Body, lam.Params[0])
		if !ok {
			return nil, 0, ""
		}
		if want, ok := duplicateQuestion(q.Prim, op); ok {
			return newDuplicateNode(first, q, want), 3,
				fmt.Sprintf("Domain rewrote Sort + Window 2 + %s (neighbours equal) → one hash pass (a duplicate check needs a set, not an ordering). Guaranteed hit.", q.Prim)
		}
	case first.Prim == "All Pairs" && (first.Meta["mode"] == "Any" || first.Meta["mode"] == "All"):
		// `Mode: Any` / `Mode: All` with the comparison written directly:
		// (a, b) -> a = b is "has a duplicate", All of (a, b) -> not(a = b)
		// is "all distinct". The lambda is two names compared, so it is
		// total and needs no further check.
		lam := nodeLambda(first)
		if k, _ := first.Meta["k"].(int); k != 2 || hasMeasuredArg(first) || lam == nil || len(lam.Params) != 2 || lam.Params[0] == lam.Params[1] {
			return nil, 0, ""
		}
		op, ok := comparesNames(lam.Body, lam.Params[0], lam.Params[1])
		if !ok {
			return nil, 0, ""
		}
		mode, _ := first.Meta["mode"].(string)
		if want, ok := duplicateQuestion(mode, op); ok {
			return newDuplicateNode(first, first, want), 1,
				fmt.Sprintf("Domain rewrote All Pairs Mode: %s (a = b) → one hash pass (a duplicate check needs a set, not every pair). Guaranteed hit.", mode)
		}
	case len(nodes) >= 2 && first.Prim == "All Pairs":
		q := nodes[1]
		if mode, _ := first.Meta["mode"].(string); mode != "Map" {
			return nil, 0, ""
		}
		if k, _ := first.Meta["k"].(int); k != 2 || hasMeasuredArg(first) {
			return nil, 0, ""
		}
		f, g := nodeLambda(first), nodeLambda(q)
		if (q.Prim != "Any" && q.Prim != "All") || f == nil || g == nil || len(f.Params) != 2 || len(g.Params) != 1 || f.Params[0] == f.Params[1] {
			return nil, 0, ""
		}
		if _, isBlock := f.Body.(*ast.BlockBody); isBlock {
			return nil, 0, ""
		}
		if _, isBlock := g.Body.(*ast.BlockBody); isBlock {
			return nil, 0, ""
		}
		// Both lambdas are about to be replaced by something that evaluates
		// neither, so both must be total — and g must not capture one of f's
		// parameter names from an enclosing binding.
		if !isTotal(f.Body) || !isTotal(g.Body) || ast.HasNondeterminism(f.Body) || ast.HasNondeterminism(g.Body) {
			return nil, 0, ""
		}
		if mentions(g.Body, f.Params[0]) || mentions(g.Body, f.Params[1]) {
			return nil, 0, ""
		}
		composed := (&simplifier{}).simplify(substIdent(g.Body, g.Params[0], f.Body))
		op, ok := comparesNames(composed, f.Params[0], f.Params[1])
		if !ok {
			return nil, 0, ""
		}
		if want, ok := duplicateQuestion(q.Prim, op); ok {
			return newDuplicateNode(first, q, want), 2,
				fmt.Sprintf("Domain rewrote All Pairs (a = b) + %s → one hash pass (a duplicate check needs a set, not every pair). Guaranteed hit.", q.Prim)
		}
	}
	return nil, 0, ""
}

// duplicateQuestion maps a quantifier over an (in)equality of two elements to
// what it asks of the list: Any(=) is "has a duplicate", All(!=) is "all
// distinct". The other two combinations ask something else.
func duplicateQuestion(quantifier string, equal bool) (wantDuplicate, ok bool) {
	switch {
	case quantifier == "Any" && equal:
		return true, true
	case quantifier == "All" && !equal:
		return false, true
	}
	return false, false
}

// equality unwraps `l = r` (equal) or its negation (not equal) — the language
// has no inequality operator, so `ikke l = r` or `not(l = r)` is how one is
// written.
func equality(body ast.Expr) (be *ast.BinaryExpr, equal, ok bool) {
	equal = true
	if u, isNot := body.(*ast.UnaryExpr); isNot && u.Op == token.NOT {
		body, equal = u.X, false
	} else if c, isCall := body.(*ast.CallExpr); isCall && builtinName(c) == "not" && len(c.Args) == 1 {
		body, equal = c.Args[0], false
	}
	be, ok = body.(*ast.BinaryExpr)
	if !ok || be.Op != token.EQ {
		return nil, false, false
	}
	return be, equal, true
}

// comparesEnds recognizes `item(p, 0) = item(p, 1)` (either order, or negated).
func comparesEnds(body ast.Expr, p string) (equal, ok bool) {
	be, equal, ok := equality(body)
	if !ok {
		return false, false
	}
	end := func(e ast.Expr) int64 {
		call, ok := e.(*ast.CallExpr)
		if !ok || builtinName(call) != "item" || len(call.Args) != 2 {
			return -1
		}
		id, isID := call.Args[0].(*ast.Ident)
		lit, isLit := call.Args[1].(*ast.IntLit)
		if !isID || id.Name != p || !isLit {
			return -1
		}
		return lit.Value
	}
	l, r := end(be.Left), end(be.Right)
	if (l == 0 && r == 1) || (l == 1 && r == 0) {
		return equal, true
	}
	return false, false
}

// comparesNames recognizes `a = b` (either order, or negated) over two names.
func comparesNames(body ast.Expr, a, b string) (equal, ok bool) {
	be, equal, ok := equality(body)
	if !ok {
		return false, false
	}
	l, lok := be.Left.(*ast.Ident)
	r, rok := be.Right.(*ast.Ident)
	if !lok || !rok {
		return false, false
	}
	if (l.Name == a && r.Name == b) || (l.Name == b && r.Name == a) {
		return equal, true
	}
	return false, false
}

// newDuplicateNode builds `Apply (xs) -> length(unique(xs)) < length(xs)`, or
// `=` for the all-distinct question.
func newDuplicateNode(first, last *ir.Node, wantDuplicate bool) *ir.Node {
	pos, in := first.Pos, first.In
	call := func(name string, args ...ast.Expr) ast.Expr {
		return &ast.CallExpr{Fn: &ast.Ident{Name: name, Pos: pos}, Args: args, Pos: pos}
	}
	xs := &ast.Ident{Name: "xs", Pos: pos}
	op, display := token.Kind(token.LT), "Has Duplicate"
	if !wantDuplicate {
		op, display = token.EQ, "All Distinct"
	}
	lam := &ast.Lambda{
		Params: []string{"xs"},
		Body:   &ast.BinaryExpr{Op: op, Left: call("length", call("unique", xs)), Right: call("length", xs), Pos: pos},
		Pos:    pos,
	}
	return &ir.Node{
		Prim:    "Apply",
		In:      in,
		Out:     last.Out,
		Display: "Apply (" + display + ")",
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

// ---------------------------------------------------------------------------
// A trajectory run long enough to be sure it repeats
// ---------------------------------------------------------------------------

// MetaUntilRepeat marks an Iterate that may stop as soon as its own state
// repeats (see stopIterateAtRepeat). Both backends read it.
const MetaUntilRepeat = "untilRepeat"

// stopIterateAtRepeat marks an Iterate whose trajectory is only ever searched
// for its first repeat:
//
//	Iterate 50000 · [Map Each …] · Find Cycle
//
// The count is there because the program cannot know when the repeat comes, so
// it is written large enough to be sure. But a deterministic step that has
// reached a state it was in before will retrace everything it did since —
// every later element of the trajectory is a copy of an earlier one. So the
// marked Iterate stops at the first state that equals an earlier one (found by
// Brent's algorithm: one comparison per step, no hashing, no memory beyond the
// trajectory it was building anyway) and returns the trajectory so far.
//
// That shorter list gives every later stage the same answer, including the
// failing ones, and needs no totality from anyone:
//
//   - a step that would fail fails on a lap the marked Iterate still runs: the
//     laps it skips repeat laps that succeeded;
//   - an elementwise Map Each over the prefix fails on the same first element,
//     since any element it would have failed on later is a copy of one in the
//     prefix;
//   - Find Cycle reports the first repeat, and the prefix ends in one.
//
// What it does need is determinism: the step and the maps must be pure
// (pureStage), which for an indented body means every stage in it is.
// The state must be Float-free, since -0.0 = 0.0 is an equality between two
// states that do not behave alike.
func stopIterateAtRepeat(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for i, outer := range p.Nodes {
		// A stage that carries a `Consider … Of` binding sits alone inside the
		// Consider node that holds the binding, so the Iterate is looked for
		// one level down as well. The binding is computed once, from the
		// Iterate's input, and must be as pure as the step that reads it.
		n, inConsider := outer, false
		if outer.Prim == "Consider" {
			sub, _ := outer.Meta["nodes"].([]*ir.Node)
			if len(sub) != 1 || !pureNodes([]*ir.Node{outer}) {
				continue
			}
			n, inConsider = sub[0], true
		}
		if n.Prim != "Iterate" || n.In == nil || typeHasFloat(n.In) || !comparable(n.In) {
			continue
		}
		if done, _ := n.Meta[MetaUntilRepeat].(bool); done {
			continue
		}
		// The count rides along as data, measured or literal; one in neither
		// form would arrive as a plausible zero (see readArg).
		times, ok := readArg(n, "n")
		if !ok || !pureStage(n) {
			continue
		}
		j := i + 1
		for j < len(p.Nodes) && p.Nodes[j].Prim == "Map Each" && pureStage(p.Nodes[j]) {
			j++
		}
		// Find Cycle's step form (a lambda in Meta) treats its input as a
		// start state, not a trajectory; this rewrite is only for the list form.
		if j >= len(p.Nodes) || p.Nodes[j].Prim != "Find Cycle" || p.Nodes[j].Meta["lambda"] != nil {
			continue
		}
		if inConsider {
			// In place: the Consider reads this slice each time it runs.
			outer.Meta["nodes"].([]*ir.Node)[0] = newIterateUntilRepeat(n, times)
		} else {
			p.Nodes[i] = newIterateUntilRepeat(n, times)
		}
		rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
			"Domain rewrote %s + Find Cycle → an Iterate that stops at its first repeated state (everything after a repeat is a copy of what came before). Guaranteed hit.",
			n.Display)})
	}
	return rewrites
}

// comparable reports whether two values of type t can be compared for
// equality by both backends: scalars, and Lists, Tuples and Records of them.
func comparable(t *ir.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case ir.KInt, ir.KText, ir.KBool:
		return true
	case ir.KList:
		return comparable(t.Elem)
	case ir.KTuple:
		for _, e := range t.Elems {
			if !comparable(e) {
				return false
			}
		}
		return true
	case ir.KRecord:
		for _, f := range t.Fields {
			if !comparable(f.Type) {
				return false
			}
		}
		return true
	}
	return false
}

// pureStage reports whether a stage's lambda is a deterministic function of
// its input with no effect anyone could observe: it writes nothing, reads no
// global something writes, draws no randomness — and, when it is an indented
// body, is built only from stages of which the same is true.
func pureStage(n *ir.Node) bool {
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil || lambdaImpure(lam) {
		return false
	}
	bb, isBlock := lam.Body.(*ast.BlockBody)
	if !isBlock {
		return !ast.HasNondeterminism(lam.Body)
	}
	return bb.Pipe != nil && pureNodes(bb.Pipe.BlockNodes())
}

// pureStages are the primitives a pure body may be built from: each computes
// its output from its input and its lambdas and touches nothing else. It is a
// list of what is known to be safe rather than of what is known not to be —
// a primitive added later (one that prints, saves, asks the network) is
// refused until someone says otherwise.
var pureStages = map[string]bool{
	"Apply": true, "Map Each": true, "Filter": true, "Fold": true, "Reduce": true, "Scan": true,
	"Sum": true, "Count": true, "Max": true, "Min": true, "Product": true, "Sum Each Group": true,
	"Sort": true, "Sort By": true, "Reverse": true, "Unique": true, "Take Item": true,
	"Window": true, "Pairs": true, "Chunk": true, "Flatten": true, "Enumerate": true,
	"Count Matching": true, "Sum By": true, "Product By": true, "Any": true, "All": true,
	"Find": true, "Find Index": true, "Min By": true, "Max By": true,
	"Take While": true, "Drop While": true, "Partition": true, "Group By": true, "Count By": true,
	"Iterate": true, "Unfold": true, "SelectTopK": true, "PartialSelect": true, "QuickselectItem": true,
	"WindowedReduce": true, "WindowPositions": true, "CountDistinct": true, "Zip With": true,
	"Split": true, "Split Each": true, "Join": true, "Convert To Integers": true,
	"Simple Domain (While)": true, "Simple Domain (Repeat)": true, "Simple Domain (For)": true,
	"Simple Domain (For Each)": true, "Simple Domain (Fixed Point)": true,
	"Consider": true,
	// The pair scans compute from their input and their lambda alone, and
	// pureNodes asks the lambda; the rewritten forms are no different.
	"All Pairs": true, "Combinations": true,
	"HashSetPairScan": true, "HashSetDiffScan": true, "DivisorPairScan": true, "HashSetTripleScan": true,
}

func pureNodes(nodes []*ir.Node) bool {
	if nodes == nil {
		return false
	}
	for _, n := range nodes {
		if n == nil || !pureStages[n.Prim] {
			return false
		}
		for _, v := range n.Meta {
			switch m := v.(type) {
			case *ast.Lambda:
				if m == nil {
					continue
				}
				if lambdaImpure(m) {
					return false
				}
				if bb, isBlock := m.Body.(*ast.BlockBody); isBlock {
					if bb.Pipe == nil || !pureNodes(bb.Pipe.BlockNodes()) {
						return false
					}
				} else if ast.HasNondeterminism(m.Body) {
					return false
				}
			case []*ir.Node:
				if !pureNodes(m) {
					return false
				}
			case [][]*ir.Node:
				for _, sub := range m {
					if !pureNodes(sub) {
						return false
					}
				}
			case []ir.Binding:
				for _, b := range m {
					if l, ok := b.Lambda().(*ast.Lambda); ok && l != nil && (lambdaImpure(l) || ast.HasNondeterminism(l.Body)) {
						return false
					}
					if e, ok := b.Expr().(ast.Expr); ok && e != nil && (impure(e) || ast.HasNondeterminism(e)) {
						return false
					}
				}
			}
		}
	}
	return true
}

// newIterateUntilRepeat rebuilds an Iterate node with the early stop. It is
// still an Iterate — same types, same lambda, same count — so everything that
// knows the primitive still does; the flag in Meta is what the compiler reads.
func newIterateUntilRepeat(n *ir.Node, times arg) *ir.Node {
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	params := []*ir.Type{n.In}
	pos := n.Pos
	meta := map[string]any{}
	for k, v := range n.Meta {
		meta[k] = v
	}
	meta[MetaUntilRepeat] = true
	return &ir.Node{
		Prim:      "Iterate",
		In:        n.In,
		Out:       n.Out,
		Display:   n.Display + " (until a state repeats)",
		Meta:      meta,
		Pos:       pos,
		Swappable: n.Swappable,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			count, err := times.value(v)
			if err != nil {
				return nil, err
			}
			out := make([]ir.Value, 0, min(count, 1024))
			cur := v
			// Brent: `mark` is an index into the trajectory that jumps forward
			// to the current element each time the gap since the last jump
			// reaches a power of two. Once it is on the cycle and the power
			// has outgrown the period, the current element meets it.
			mark, gap, power := 0, int64(0), int64(1)
			for i := int64(0); i < count; i++ {
				cur, err = eval.EvalLambdaTyped(lam, params, cur)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Iterate", Pos: pos, Msg: fmt.Sprintf("step %d: %v", i+1, err)}
				}
				out = append(out, cur)
				if i == 0 {
					continue
				}
				gap++
				if ir.DeepEqual(out[mark], cur) {
					break
				}
				if gap == power {
					mark, gap, power = int(i), 0, power*2
				}
			}
			return out, nil
		},
	}
}
