package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// ---------------------------------------------------------------------------
// A Map Each body, and the stage after it, as one loop
// ---------------------------------------------------------------------------

// mergeEachBodies joins a `Map Each` whose lambda is (or may be treated as) a
// per-element pipeline with the per-element stage that follows it:
//
//	Map Each A · Map Each B | Sum By B | Count Matching B | Any B | All B
//	  →  Map Each | Sum By | Count Matching | Any | All over the body A·B
//
// That is one loop running A and then B on each element instead of two loops
// and a list between them — and, as importantly, it puts A's last stage next
// to B's first, where the other passes can match (an `All Pairs` mapped to
// flags in A, an `Any` over the flags opening B: fuseDuplicateCheck).
//
// The two loops interleave A and B across elements, which changes which
// failure is reported first and how many times A runs when B stops early
// (Any, All). So A must be provably total and both must be pure (totalNodes,
// pureStage): then A can neither fail nor be observed, and the first failure
// B reports is on the same element, in the same words, as before. B is kept
// exactly as it was written; an expression B becomes an Apply that returns
// the lambda's own error, so a failure is worded as the stage reported it.
//
// At least one side must be an indented body (two expressions are
// fuseMapMap's), neither may name extra parameters (`Params:`), and both must
// take exactly one parameter, which rules out the ambient For variables.
func mergeEachBodies(p *ir.Pipeline) []Rewrite {
	return rewritePairs(p, func(a, b *ir.Node) ([]*ir.Node, string, bool) {
		if a.Prim != "Map Each" || a.In == nil || a.In.Kind != ir.KList || a.Out == nil || a.Out.Kind != ir.KList {
			return nil, "", false
		}
		var resultType *ir.Type
		switch b.Prim {
		case "Map Each":
			if b.Out == nil || b.Out.Kind != ir.KList {
				return nil, "", false
			}
			resultType = b.Out.Elem
		case "Sum By":
			resultType = ir.Int()
		case "Count Matching", "Any", "All":
			resultType = ir.Bool()
		default:
			return nil, "", false
		}
		if b.In == nil || b.In.Kind != ir.KList || !b.In.Elem.Equal(a.Out.Elem) || b.Out == nil {
			return nil, "", false
		}
		if (b.Prim == "Sum By" && b.Out.Kind != ir.KInt) || hasMeasuredArg(a) || hasMeasuredArg(b) {
			return nil, "", false
		}
		la, lb := nodeLambda(a), nodeLambda(b)
		if la == nil || lb == nil || len(la.Params) != 1 || len(lb.Params) != 1 {
			return nil, "", false
		}
		_, aBlock := la.Body.(*ast.BlockBody)
		_, bBlock := lb.Body.(*ast.BlockBody)
		if !aBlock && !bBlock {
			return nil, "", false
		}
		if !pureStage(a) || !pureStage(b) {
			return nil, "", false
		}
		// A second stage written as an expression runs inside the merged body
		// as an Apply. The interpreter reports its failure in the original
		// stage's words, but a compiled binary names the stage it emitted — so
		// an expression that could fail is left where it is. A second stage
		// that is itself a body keeps its own stages, and their names.
		if !bBlock && !isTotalElementwise(lb.Body, lb.Params[0], b.In.Elem) {
			return nil, "", false
		}
		aNodes, ok := lambdaBodyAsNodes(la, a.In.Elem, a.Out.Elem, a.Pos)
		if !ok || !totalNodes(aNodes) {
			return nil, "", false
		}
		bNodes, ok := lambdaBodyAsNodes(lb, b.In.Elem, resultType, b.Pos)
		if !ok {
			return nil, "", false
		}
		nodes := append(append([]*ir.Node{}, aNodes...), bNodes...)
		pipe := &mergedBlock{nodes: nodes, in: a.In.Elem, out: resultType, pos: b.Pos, prim: b.Prim}
		const param = "$block0"
		lam := &ast.Lambda{
			Params: []string{param},
			Body:   &ast.BlockBody{Param: param, Pipe: pipe, Pos: b.Pos},
			Pos:    b.Pos,
		}
		from := &ir.Node{In: a.In, Out: b.Out, Pos: b.Pos}
		var merged *ir.Node
		switch b.Prim {
		case "Map Each":
			merged = newMapEachNode(from, lam, "Map Each (merged body)")
		case "Sum By":
			merged = newSumByNode(from, lam)
		case "Count Matching":
			merged = newCountMatchingNode(from, lam)
		default:
			merged = newQuantifierNode(from, b.Prim, lam)
		}
		if b.Prim != "Map Each" {
			merged.Display = b.Display + " (merged body)"
		}
		return []*ir.Node{merged},
			fmt.Sprintf("Domain rewrote Map Each + %s → one %s over the joined bodies (one loop; the first body cannot fail, so no failure moves). Guaranteed hit.", b.Prim, b.Prim),
			true
	})
}

// lambdaBodyAsNodes is a one-parameter lambda as the node list of a body: an
// indented body's own nodes (declining one with extra parameters), or an
// expression as one Apply that returns the lambda's error as it came.
func lambdaBodyAsNodes(lam *ast.Lambda, in, out *ir.Type, pos token.Position) ([]*ir.Node, bool) {
	if bb, isBlock := lam.Body.(*ast.BlockBody); isBlock {
		if len(bb.Extra) > 0 || bb.Pipe == nil || bb.Pipe.BlockNodes() == nil {
			return nil, false
		}
		return bb.Pipe.BlockNodes(), true
	}
	return []*ir.Node{{
		Prim:    "Apply",
		In:      in,
		Out:     out,
		Display: "Apply",
		Meta:    map[string]any{"lambda": lam},
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			return eval.EvalLambdaTyped(lam, []*ir.Type{in}, v)
		},
	}}, true
}

// totalNodes reports whether running these stages can never fail. It is an
// allowlist: each primitive below is one whose evaluator has no failure path
// other than its lambdas' (a type assertion the resolver already discharged,
// or an argument bound it checked at resolve time), and a lambda counts only
// when the typed totality analysis proves it. Everything else — Take Item,
// Max, Min, Product, Convert To Integers, Extract Integers (overflow), a
// measured argument — is left out.
func totalNodes(nodes []*ir.Node) bool {
	if len(nodes) == 0 {
		return false
	}
	for _, n := range nodes {
		if !totalNode(n) {
			return false
		}
	}
	return true
}

func totalNode(n *ir.Node) bool {
	if n == nil || n.In == nil || hasMeasuredArg(n) {
		return false
	}
	switch n.Prim {
	case "Count", "Reverse", "Unique", "Pairs", "Flatten", "Enumerate", "Sort", "Sum",
		"Split", "Split Each", "Join", "Window", "Chunk", "CountDistinct":
		return true
	case "Map Each", "Filter", "Count Matching", "Sum By", "Any", "All":
		return n.In.Kind == ir.KList && totalLambda(n, 1, n.In.Elem)
	case "Apply":
		return totalLambda(n, 1, n.In)
	case "All Pairs", "Combinations":
		mode, _ := n.Meta["mode"].(string)
		k, _ := n.Meta["k"].(int)
		if (mode != "Map" && mode != "Count" && mode != "Filter") || k < 1 || n.In.Kind != ir.KList {
			return false
		}
		return totalLambda(n, k, n.In.Elem)
	}
	return false
}

// totalLambda: the node's lambda takes arity parameters, all of type elem, is
// not effectful, and cannot fail.
func totalLambda(n *ir.Node, arity int, elem *ir.Type) bool {
	lam := nodeLambda(n)
	if lam == nil || len(lam.Params) != arity || elem == nil {
		return false
	}
	if bb, isBlock := lam.Body.(*ast.BlockBody); isBlock {
		return len(bb.Extra) == 0 && bb.Pipe != nil && totalNodes(bb.Pipe.BlockNodes())
	}
	env := typecheck.Env{}
	for _, p := range lam.Params {
		env[p] = elem
	}
	return isTotalIn(lam.Body, env)
}

// mergedBlock is the pipeline behind a joined body: the stages of the first
// body followed by the stages of the second. It is a block pipeline in the
// sense prims.blockPipeline is — resolved already, so BindBlock only checks
// the type — and takes rewritten stage lists through SetBlockNodes, which is
// how the other passes reach inside it.
type mergedBlock struct {
	nodes []*ir.Node
	in    *ir.Type
	out   *ir.Type
	pos   token.Position
	prim  string
}

func (m *mergedBlock) BindBlock(in *ir.Type) (*ir.Type, error) {
	if in == nil || !m.in.Equal(in) {
		return nil, fmt.Errorf("%s: body was already resolved for input %s, cannot re-resolve for %s", m.pos, m.in, in)
	}
	return m.out, nil
}

func (m *mergedBlock) RunBlock(v ir.Value) (ir.Value, error) {
	ctx := ir.CurrentContext()
	if ctx.Tracing() {
		ctx.PushFrame(m.prim+" body", m.out)
		var res ir.Value
		var err error
		defer func() { ctx.PopFrame(res) }()
		res, err = m.run(ctx, v)
		if err != nil {
			res = nil
		}
		return res, err
	}
	return m.run(ctx, v)
}

func (m *mergedBlock) run(ctx *ir.Context, v ir.Value) (ir.Value, error) {
	var err error
	for _, n := range m.nodes {
		if v, err = ir.EvalNode(ctx, n, v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func (m *mergedBlock) BlockNodes() []*ir.Node     { return m.nodes }
func (m *mergedBlock) SetBlockNodes(l []*ir.Node) { m.nodes = l }
