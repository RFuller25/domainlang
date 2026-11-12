package optimizer

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
)

// Idiom recognition: programs that describe the right result the long way.
//
// The passes in fuse.go match shapes somebody wrote *with* the vocabulary —
// `Filter` then `Count`. The ones here match what gets written *instead of*
// the vocabulary, by someone who knew one tool and used it for everything: a
// `Fold` that counts, a list sorted on its way into a sum three stages later,
// a `Count Matching` compared with zero to ask "is there one". Each is a
// correct program whose cost is in how it was said, and the language's promise
// is that how it was said is not what it costs.
//
// Every rule below keeps the three guarantees the rest of the package does:
// the type signature is unchanged, nothing that could fail is discarded or
// reordered (see pureTotal), and the replacement is a primitive both backends
// already run — or, for CountDistinct, one that has a codegen case.

// pureTotal reports whether a one-parameter stage lambda can be run on fewer
// elements, on different elements, or in a different order without the
// program being able to tell: it writes nothing, draws nothing, and cannot
// fail.
func pureTotal(n *ir.Node) (*ast.Lambda, bool) {
	lam := nodeLambda(n)
	if lam == nil || len(lam.Params) != 1 || n.In == nil || n.In.Kind != ir.KList {
		return nil, false
	}
	pure, total := Elementwise(lam, n.In.Elem)
	return lam, pure && total
}

// ---------------------------------------------------------------------------
// A reordering that never reaches anything order-sensitive
// ---------------------------------------------------------------------------

// elideReorderAcrossStages drops a Sort or Reverse whose ordering is spent
// before anything can observe it: every stage between it and an
// order-insensitive reduction works one element at a time.
//
//	Quicksort · Map Each · Filter · Sum      →  Map Each · Filter · Sum
//	Reverse · Count Matching                 →  Count Matching
//
// elideReorderBeforeReduce handles the adjacent case; this is the same fact
// seen through elementwise stages, and through the reductions that carry a
// lambda. The stages must be pure and total: a stage that can fail reports the
// *first* element it fails on, and which element is first is exactly what the
// reordering decides.
func elideReorderAcrossStages(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	var out []*ir.Node
	for i := 0; i < len(p.Nodes); i++ {
		n := p.Nodes[i]
		if (n.Prim != "Sort" && n.Prim != "Reverse") || n.In == nil || n.In.Kind != ir.KList || typeHasFloat(n.In) {
			out = append(out, n)
			continue
		}
		j := i + 1
		for j < len(p.Nodes) && (p.Nodes[j].Prim == "Map Each" || p.Nodes[j].Prim == "Filter") {
			if _, ok := pureTotal(p.Nodes[j]); !ok || typeHasFloat(p.Nodes[j].Out) {
				break
			}
			j++
		}
		// The adjacent plain reductions belong to elideReorderBeforeReduce.
		if j >= len(p.Nodes) || !orderBlindSink(p.Nodes[j]) ||
			(j == i+1 && isOrderInsensitiveReduce(p.Nodes[j].Prim)) {
			out = append(out, n)
			continue
		}
		names := []string{n.Prim}
		for _, m := range p.Nodes[i+1 : j+1] {
			names = append(names, m.Prim)
		}
		rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
			"Domain rewrote %s → %s (every stage after the %s works one element at a time, so its ordering never reaches the result). Guaranteed hit.",
			strings.Join(names, " + "), strings.Join(names[1:], " + "), n.Prim)})
		// n is dropped; the stages after it are untouched and re-examined.
	}
	p.Nodes = out
	return rewrites
}

// orderBlindSink reports whether n reduces a List to a value that cannot
// depend on the order of its elements — including, unlike
// isOrderInsensitiveReduce, the reductions that run a lambda, which qualify
// only when that lambda is pure and total.
func orderBlindSink(n *ir.Node) bool {
	if n.In == nil || n.In.Kind != ir.KList || typeHasFloat(n.In) || typeHasFloat(n.Out) {
		return false
	}
	switch n.Prim {
	case "Sum", "Count", "Max", "Min", "Product", "CountDistinct", "LinearMapExtremum":
		// LinearMapExtremum is Map Each + Max/Min after fuseLinearMapExtremum
		// got to it first: an extremum of the input, then one application.
		return true
	case "Sum By", "Product By", "Count Matching", "Any", "All":
		// A quantifier already folded to a constant has no lambda to ask.
		_, ok := pureTotal(n)
		return ok
	}
	return false
}

// hoistFilterBeforeSort swaps Sort ∘ Filter into Filter ∘ Sort. A filter
// keeps the relative order of what it keeps, so both orders give the same
// list — and sorting after it sorts only the survivors. The node pointers are
// reused; both are List<Int> → List<Int>, so only their order changes.
//
// Int only, like the other Sort rewrites: equal Ints are indistinguishable,
// so "the survivors, sorted" has one answer however the sort breaks ties.
func hoistFilterBeforeSort(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Sort" || b.Prim != "Filter" || !isIntList(a.In) {
			return nil, "", false
		}
		if _, ok := pureTotal(b); !ok {
			return nil, "", false
		}
		return []*ir.Node{b, a},
			"Domain rewrote Quicksort + Filter → Filter + Quicksort (filter first, sort only the survivors). Guaranteed hit.",
			true
	})
}

// ---------------------------------------------------------------------------
// Fold, used as the only tool
// ---------------------------------------------------------------------------

// fuseFoldIdioms recognizes a Fold with `Seed: 0` that spells out one of the
// named reductions by hand, and replaces it with that reduction:
//
//	(acc, x) -> acc + 1                          Count           (no loop at all)
//	(acc, x) -> acc + f(x)                       Sum By f
//	(acc, x) -> if p(x) then acc + 1 else acc    Count Matching p
//	(acc, x) -> if p(x) then acc + f(x) else acc Sum By (if p(x) then f(x) else 0)
//
// The named forms are not just shorter. Count is the list's length; and Sum By
// and Count Matching are sinks the compiler streams into — a Fold is not, so
// the `Map Each` or `Filter` in front of a hand-written one builds a list that
// the same program written with the vocabulary never does.
//
// f and p must not read the accumulator and must be total: Fold reports a
// failure as its own, on "element i", and a reduction that failed instead
// would say so under a different name. (`acc + x` alone is fuseFoldSum's.)
func fuseFoldIdioms(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	for _, list := range nodeLists(p) {
		for i, n := range list {
			if n.Prim != "Fold" || hasMeasuredArg(n) || n.In == nil || n.In.Kind != ir.KList {
				continue
			}
			if seed, ok := n.Meta["seed"].(int64); !ok || seed != 0 {
				continue
			}
			if n.Out == nil || n.Out.Kind != ir.KInt {
				continue
			}
			lam := nodeLambda(n)
			if lam == nil || len(lam.Params) != 2 || lam.Params[0] == lam.Params[1] {
				continue
			}
			if _, isBlock := lam.Body.(*ast.BlockBody); isBlock || ast.HasNondeterminism(lam.Body) {
				continue
			}
			acc, x := lam.Params[0], lam.Params[1]
			repl, what := foldIdiom(n, lam.Body, acc, x)
			if repl == nil {
				continue
			}
			list[i] = repl
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote Fold (Seed: 0, %s) → %s. Guaranteed hit.", what, repl.Prim)})
		}
	}
	return rewrites
}

// foldIdiom matches one Fold body against the shapes above and builds the
// replacement, or returns nil.
func foldIdiom(n *ir.Node, body ast.Expr, acc, x string) (*ir.Node, string) {
	elem := n.In.Elem
	total := func(e ast.Expr) bool { return !mentions(e, acc) && isTotalElementwise(e, x, elem) }
	unary := func(e ast.Expr) *ast.Lambda { return &ast.Lambda{Params: []string{x}, Body: e, Pos: exprPos(e)} }

	if term, ok := addsTo(body, acc); ok {
		if lit, isLit := term.(*ast.IntLit); isLit && lit.Value == 1 {
			return newCountNode(n), "counting by hand"
		}
		if id, isID := term.(*ast.Ident); isID && id.Name == x {
			return nil, "" // the plain running sum: fuseFoldSum
		}
		if total(term) {
			return newSumByNode(n, unary(term)), "a running sum of a mapped value"
		}
		return nil, ""
	}

	cond, ok := body.(*ast.CondExpr)
	if !ok || !total(cond.Cond) {
		return nil, ""
	}
	if id, isID := cond.Else.(*ast.Ident); !isID || id.Name != acc {
		return nil, ""
	}
	term, ok := addsTo(cond.Then, acc)
	if !ok {
		return nil, ""
	}
	if lit, isLit := term.(*ast.IntLit); isLit && lit.Value == 1 {
		return newCountMatchingNode(n, unary(cond.Cond)), "a conditional count"
	}
	if !total(term) {
		return nil, ""
	}
	guarded := &ast.CondExpr{Cond: cond.Cond, Then: term, Else: &ast.IntLit{Value: 0, Pos: cond.Pos}, Pos: cond.Pos}
	return newSumByNode(n, unary(guarded)), "a conditional running sum"
}

// addsTo recognizes `acc + e` or `e + acc` and returns e.
func addsTo(body ast.Expr, acc string) (ast.Expr, bool) {
	be, ok := body.(*ast.BinaryExpr)
	if !ok || be.Op != token.PLUS {
		return nil, false
	}
	if id, ok := be.Left.(*ast.Ident); ok && id.Name == acc {
		return be.Right, true
	}
	if id, ok := be.Right.(*ast.Ident); ok && id.Name == acc {
		return be.Left, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// A list built to be measured
// ---------------------------------------------------------------------------

// fuseUniqueCount replaces Unique ∘ Count with CountDistinct: the number of
// distinct elements is the size of the set Unique builds to find them, and
// the deduplicated list — up to the size of the input — is never built.
func fuseUniqueCount(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Unique" || b.Prim != "Count" || a.In == nil || a.In.Kind != ir.KList {
			return nil, "", false
		}
		pos := a.Pos
		fused := &ir.Node{
			Prim:    "CountDistinct",
			In:      a.In,
			Out:     ir.Int(),
			Display: "Count Distinct (from Unique + Count)",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Unique", Pos: pos, Msg: err.Error()}
				}
				return int64(ir.SetFromList(items).Len()), nil
			},
		}
		return []*ir.Node{fused},
			"Domain rewrote Unique + Count → Count Distinct (the set is counted, the deduplicated list never built). Guaranteed hit.",
			true
	})
}

// fuseCountCompare turns a count that is only compared with zero into the
// question it was asking:
//
//	Count Matching p · Apply (c) -> c > 0    →  Any p
//	Count Matching p · Apply (c) -> c = 0    →  All (not p)
//	Filter p · Apply (xs) -> length(xs) > 0  →  Any p
//
// The count visits every element; the quantifier stops at the first one that
// settles it. Stopping early skips evaluations, so p must be pure and total.
// (`Filter` + `Count` reaches here through fuseFilterCount.)
func fuseCountCompare(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if (a.Prim != "Count Matching" && a.Prim != "Filter") || b.Prim != "Apply" {
			return nil, "", false
		}
		pred, ok := pureTotal(a)
		if !ok {
			return nil, "", false
		}
		test := nodeLambda(b)
		if test == nil || len(test.Params) != 1 {
			return nil, "", false
		}
		// After Count Matching the Apply's parameter *is* the count; after a
		// Filter it is the list of matches, and the count is its length.
		some, ok := comparesWithZero(test.Body, test.Params[0], a.Prim == "Filter")
		if !ok {
			return nil, "", false
		}
		if some {
			return []*ir.Node{newQuantifierNode(a, "Any", pred)},
				fmt.Sprintf("Domain rewrote %s + Apply (count > 0) → Any (stops at the first match instead of counting them all). Guaranteed hit.", a.Prim),
				true
		}
		none := &ast.Lambda{
			Params: pred.Params,
			Body:   &ast.UnaryExpr{Op: token.NOT, X: pred.Body, Pos: pred.Pos},
			Pos:    pred.Pos,
		}
		return []*ir.Node{newQuantifierNode(a, "All", none)},
			fmt.Sprintf("Domain rewrote %s + Apply (count = 0) → All (stops at the first match instead of counting them all). Guaranteed hit.", a.Prim),
			true
	})
}

// comparesWithZero recognizes a test of a count c (which is never negative)
// against zero. some is true for the spellings of "at least one" — `c > 0`,
// `c >= 1`, `0 < c`, `1 <= c` — and false for the spellings of "none":
// `c = 0`, `0 = c`, `c < 1`, `1 > c`, `c <= 0`, `0 >= c`.
//
// With viaLength the count is spelled `length(c)` — c is the list a Filter
// produced rather than a number.
func comparesWithZero(body ast.Expr, c string, viaLength bool) (some, ok bool) {
	be, isBin := body.(*ast.BinaryExpr)
	if !isBin {
		return false, false
	}
	isCount := func(e ast.Expr) bool {
		if viaLength {
			call, isCall := e.(*ast.CallExpr)
			if !isCall || builtinName(call) != "length" || len(call.Args) != 1 {
				return false
			}
			e = call.Args[0]
		}
		id, isID := e.(*ast.Ident)
		return isID && id.Name == c
	}
	op := be.Op
	counted := isCount(be.Left)
	lit, _ := be.Right.(*ast.IntLit)
	if !counted {
		// literal on the left: mirror the operator so the count is on the left.
		counted = isCount(be.Right)
		lit, _ = be.Left.(*ast.IntLit)
		switch op {
		case token.LT:
			op = token.GT
		case token.GT:
			op = token.LT
		case token.LE:
			op = token.GE
		case token.GE:
			op = token.LE
		}
	}
	if !counted || lit == nil {
		return false, false
	}
	switch {
	case op == token.GT && lit.Value == 0, op == token.GE && lit.Value == 1:
		return true, true
	case op == token.EQ && lit.Value == 0, op == token.LT && lit.Value == 1, op == token.LE && lit.Value == 0:
		return false, true
	}
	return false, false
}

// ---------------------------------------------------------------------------
// A whole list mapped to read one element of it
// ---------------------------------------------------------------------------

// hoistTakeItemBeforeMap turns Map Each ∘ Take Item k into Take Item k ∘
// Apply: the lambda runs on the one element that is kept instead of on every
// element, and the mapped list is never built. Mapping preserves length, so
// an out-of-range index fails with the same message either way; the lambda
// must be pure and total, because n-1 of its evaluations are being dropped.
func hoistTakeItemBeforeMap(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Map Each" || b.Prim != "Take Item" || hasMeasuredArg(b) {
			return nil, "", false
		}
		idx, ok := b.Meta["index"].(int)
		if !ok {
			return nil, "", false
		}
		lam, ok := pureTotal(a)
		if !ok {
			return nil, "", false
		}
		elem, takePos, applyPos := a.In.Elem, b.Pos, a.Pos
		take := &ir.Node{
			Prim:    "Take Item",
			In:      a.In,
			Out:     elem,
			Display: b.Display,
			Meta:    map[string]any{"index": idx},
			Pos:     takePos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				items, err := ir.AsList(v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Take Item", Pos: takePos, Msg: err.Error()}
				}
				if idx < 0 || idx >= len(items) {
					return nil, &ir.RuntimeError{Prim: "Take Item", Pos: takePos,
						Msg: fmt.Sprintf("index %d out of range (length %d)", idx, len(items))}
				}
				return items[idx], nil
			},
		}
		apply := &ir.Node{
			Prim:    "Apply",
			In:      elem,
			Out:     b.Out,
			Display: "Apply (from Map Each)",
			Meta:    map[string]any{"lambda": lam},
			Pos:     applyPos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				r, err := eval.EvalLambdaTyped(lam, []*ir.Type{elem}, v)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Map Each", Pos: applyPos, Msg: err.Error()}
				}
				return r, nil
			},
		}
		return []*ir.Node{take, apply},
			fmt.Sprintf("Domain rewrote Map Each + Take Item %d → Take Item %d + Apply (the lambda runs once, on the element that is kept). Guaranteed hit.", idx, idx),
			true
	})
}

// ---------------------------------------------------------------------------
// Node constructors
// ---------------------------------------------------------------------------

func newCountNode(from *ir.Node) *ir.Node {
	pos := from.Pos
	return &ir.Node{
		Prim:    "Count",
		In:      from.In,
		Out:     ir.Int(),
		Display: "Count (from Fold)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			items, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Count", Pos: pos, Msg: err.Error()}
			}
			return int64(len(items)), nil
		},
	}
}

func newSumByNode(from *ir.Node, lam *ast.Lambda) *ir.Node {
	pos, elem := from.Pos, from.In.Elem
	return &ir.Node{
		Prim:    "Sum By",
		In:      from.In,
		Out:     ir.Int(),
		Display: "Sum By (from Fold)",
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Sum By", Pos: pos, Msg: err.Error()}
			}
			var acc int64
			for i, x := range xs {
				r, err := eval.EvalLambdaTyped(lam, []*ir.Type{elem}, x)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Sum By", Pos: pos, Msg: fmt.Sprintf("item %d: %v", i, err)}
				}
				k, ok := r.(int64)
				if !ok {
					return nil, &ir.RuntimeError{Prim: "Sum By", Pos: pos,
						Msg: fmt.Sprintf("item %d: lambda did not return an Int (got %s)", i, ir.DescribeValue(r))}
				}
				acc += k
			}
			return acc, nil
		},
	}
}

func newCountMatchingNode(from *ir.Node, lam *ast.Lambda) *ir.Node {
	pos, elem := from.Pos, from.In.Elem
	return &ir.Node{
		Prim:    "Count Matching",
		In:      from.In,
		Out:     ir.Int(),
		Display: "Count Matching (from Fold)",
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			items, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: "Count Matching", Pos: pos, Msg: err.Error()}
			}
			var c int64
			for i, e := range items {
				keep, err := evalPredicate(lam, elem, e)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: "Count Matching", Pos: pos, Msg: fmt.Sprintf("element %d: %v", i, err)}
				}
				if keep {
					c++
				}
			}
			return c, nil
		},
	}
}

// newQuantifierNode builds an Any or All over from's input, mirroring the
// primitive: Any stops on the first true, All on the first false.
func newQuantifierNode(from *ir.Node, id string, lam *ast.Lambda) *ir.Node {
	pos, elem := from.Pos, from.In.Elem
	universal := id == "All"
	return &ir.Node{
		Prim:    id,
		In:      from.In,
		Out:     ir.Bool(),
		Display: id + " (from a count compared with zero)",
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(v)
			if err != nil {
				return nil, &ir.RuntimeError{Prim: id, Pos: pos, Msg: err.Error()}
			}
			for i, e := range xs {
				hit, err := evalPredicate(lam, elem, e)
				if err != nil {
					return nil, &ir.RuntimeError{Prim: id, Pos: pos, Msg: fmt.Sprintf("element %d: %v", i, err)}
				}
				if hit != universal {
					return !universal, nil
				}
			}
			return universal, nil
		},
	}
}

// PureLambda reports whether a lambda writes nothing, reads no global something
// writes, and draws no randomness — the condition under which the compiler may
// run it per element inside a fused loop instead of stage by stage.
func PureLambda(l *ast.Lambda) bool {
	if l == nil {
		return false
	}
	if _, isBlock := l.Body.(*ast.BlockBody); isBlock {
		return false
	}
	return !lambdaImpure(l) && !ast.HasNondeterminism(l.Body)
}

// TotalExpr reports whether e can never fail at runtime (see isTotal).
func TotalExpr(e ast.Expr) bool { return isTotal(e) }

// ExprMentions reports whether name occurs anywhere in e; an expression kind it
// does not know counts as a mention.
func ExprMentions(e ast.Expr, name string) bool { return mentions(e, name) }
