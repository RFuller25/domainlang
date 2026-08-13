package optimizer

import (
	"fmt"

	"domain/ast"
	"domain/eval"
	"domain/ir"
)

// streamMaxIterations mirrors prims.maxLoopIterations' default (unexported,
// so it cannot be referenced directly across the package boundary) — the
// same duplication codegen already carries as dmMaxLoopIterations, and for
// the same reason: a fused Stream node's bound must match what the unfused
// Unfold it replaces would have enforced, on both backends.
const streamMaxIterations = 1_000_000_000

// fuseUnfoldStream fuses a bounded `Unfold` and the elementwise `Map Each` /
// `Filter` nodes immediately following it — optionally terminated by
// `Apply Using: (x) -> take(x, N)` — into a single `Stream` node: one loop
// that generates, maps, and filters each raw value without ever
// materializing the intermediate lists Unfold/Map Each/Filter otherwise
// would. Terminated by `take`, it stops the instant N values have survived,
// rather than running Unfold's full `While:` bound.
//
// This is the fix for the AoC 2017 day 15 "dueling generators" idiom: a
// `Channel` unfolding tens of millions of raw values, filtering most of
// them out, then taking a fixed number of what's left. See
// docs/superpowers/specs/2026-08-12-lazy-unfold-fusion-design.md.
//
// Unlike the other length-changing passes, this one also runs inside
// `Channel` bodies — that's where day 15's chains actually live. It's safe
// to do only because Channel's own Eval reads its node list from
// Meta["nodes"] at call time rather than closing over it (prims/channel.go),
// so rewriting that slice here is visible to whatever runs it.
func fuseUnfoldStream(p *ir.Pipeline) []Rewrite {
	var rewrites []Rewrite
	var rs []Rewrite
	p.Nodes, rs = fuseUnfoldStreamList(p.Nodes)
	rewrites = append(rewrites, rs...)

	for _, n := range p.Nodes {
		if n.Prim != "Channel" {
			continue
		}
		sub, ok := n.Meta["nodes"].([]*ir.Node)
		if !ok {
			continue
		}
		var subRewrites []Rewrite
		sub, subRewrites = fuseUnfoldStreamList(sub)
		if len(subRewrites) > 0 {
			n.Meta["nodes"] = sub
			rewrites = append(rewrites, subRewrites...)
		}
	}
	return rewrites
}

// fuseUnfoldStreamList applies the rewrite within one node list (the top
// level, or one Channel's body).
func fuseUnfoldStreamList(nodes []*ir.Node) ([]*ir.Node, []Rewrite) {
	var rewrites []Rewrite
	var out []*ir.Node
	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		if n.Prim != "Unfold" {
			out = append(out, n)
			continue
		}
		steps, consumed, takeN, earlyExit, matched := matchStreamRun(nodes[i+1:])
		if !matched {
			out = append(out, n)
			continue
		}
		out = append(out, newStreamNode(n, steps, takeN, earlyExit))
		i += consumed // the matched run is absorbed into the fused node too

		label := "Unfold"
		for _, s := range steps {
			label += " + " + s.Prim
		}
		if earlyExit {
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote %s + Apply (take %d) → Cursed Stream (early exit). Guaranteed hit.",
				label, takeN)})
		} else {
			rewrites = append(rewrites, Rewrite{Message: fmt.Sprintf(
				"Domain rewrote %s → Cursed Stream. Guaranteed hit.", label)})
		}
	}
	return out, rewrites
}

// matchStreamRun walks forward from just after an Unfold, collecting
// elementwise-safe Map Each/Filter nodes, then checks whether the node right
// after them is the take(x, N) terminator. matched is false when there is
// nothing worth fusing (no elementwise steps and no terminator) — firing on
// a bare Unfold would just wrap it for no benefit.
func matchStreamRun(rest []*ir.Node) (steps []*ir.Node, consumed int, takeN int64, earlyExit, matched bool) {
	for consumed < len(rest) && isElementwiseSafe(rest[consumed]) {
		steps = append(steps, rest[consumed])
		consumed++
	}
	if consumed < len(rest) {
		if n, ok := takeApply(rest[consumed]); ok {
			takeN = n
			earlyExit = true
			consumed++
		}
	}
	matched = len(steps) > 0 || earlyExit
	return
}

// isElementwiseSafe reports whether n is a Map Each or Filter that this pass
// may run per-generated-element instead of over a materialized list: a
// single-parameter, non-effectful lambda whose body cannot fail. Requiring
// totality is what keeps the fused loop's error behavior identical to the
// naive pipeline's — see the design doc's "Semantics parity" section: since
// none of these lambdas can ever error, there is no question of which stage
// would have reported an error first.
func isElementwiseSafe(n *ir.Node) bool {
	if n.Prim != "Map Each" && n.Prim != "Filter" {
		return false
	}
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil || len(lam.Params) != 1 || effectful(lam) {
		return false
	}
	if _, isBlock := lam.Body.(*ast.BlockBody); isBlock {
		return false
	}
	return isTotalElementwise(lam.Body, lam.Params[0], n.In.Elem)
}

// isTotalElementwise extends isTotal with the one idiom day 15 actually
// needs: `item(param, k)` on the lambda's own parameter, where the
// parameter's static type is a Tuple and k is a literal in bounds. isTotal
// treats every `item` call as partial because it cannot see element types —
// but a fixed-arity Tuple's arity is known here, in the optimizer, so an
// in-bounds literal index on the parameter itself can never fail.
func isTotalElementwise(body ast.Expr, param string, paramType *ir.Type) bool {
	if call, ok := body.(*ast.CallExpr); ok {
		if id, ok := call.Fn.(*ast.Ident); ok && id.Name == "item" && len(call.Args) == 2 {
			if p, ok := call.Args[0].(*ast.Ident); ok && p.Name == param {
				if lit, ok := call.Args[1].(*ast.IntLit); ok &&
					paramType != nil && paramType.Kind == ir.KTuple &&
					lit.Value >= 0 && int(lit.Value) < len(paramType.Elems) {
					return true
				}
			}
		}
	}
	return isTotal(body)
}

// takeApply recognizes `Apply Using: (x) -> take(x, N)` — the shape day 15
// uses to cap a filtered channel — and reports N. take() is a whole-list
// transform, not elementwise (unlike Map Each, Apply's lambda runs once over
// the whole current value), so it is matched separately from
// isElementwiseSafe rather than folded into it.
func takeApply(n *ir.Node) (int64, bool) {
	if n.Prim != "Apply" {
		return 0, false
	}
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil || len(lam.Params) != 1 {
		return 0, false
	}
	call, ok := lam.Body.(*ast.CallExpr)
	if !ok {
		return 0, false
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != "take" || len(call.Args) != 2 {
		return 0, false
	}
	p, ok := call.Args[0].(*ast.Ident)
	if !ok || p.Name != lam.Params[0] {
		return 0, false
	}
	lit, ok := call.Args[1].(*ast.IntLit)
	if !ok || lit.Value < 0 {
		return 0, false
	}
	return lit.Value, true
}

// streamErr builds a runtime error tagged like the primitive it stands in
// for — mirroring prims.runtimeErr, which this package cannot call (it's
// unexported across the package boundary).
func streamErr(prim string, n *ir.Node, format string, args ...any) error {
	return &ir.RuntimeError{Prim: prim, Pos: n.Pos, Msg: fmt.Sprintf(format, args...)}
}

// newStreamNode builds the fused node. unfoldNode supplies the seed type,
// the While:/step lambdas and their position; steps is the elementwise run
// (each still its own Map Each/Filter *ir.Node, read for its lambda and
// types only — never added to the output); takeN/earlyExit describe the
// terminator, if any.
func newStreamNode(unfoldNode *ir.Node, steps []*ir.Node, takeN int64, earlyExit bool) *ir.Node {
	stepLam, _ := unfoldNode.Meta["lambda"].(*ast.Lambda)
	whileLam, _ := unfoldNode.Meta["while"].(*ast.Lambda)
	elemT := unfoldNode.In // Unfold: T -> List<T>

	outT := elemT
	if len(steps) > 0 {
		outT = steps[len(steps)-1].Out.Elem
	}

	display := "Cursed Stream"
	meta := map[string]any{
		"unfold": unfoldNode, "steps": steps, "earlyExit": earlyExit, "take": takeN,
	}

	return &ir.Node{
		Prim:    "Stream",
		In:      unfoldNode.In,
		Out:     ir.List(outT),
		Display: display,
		Meta:    meta,
		Pos:     unfoldNode.Pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			out := []ir.Value{}
			cur := v
			emitted := 0 // mirrors Unfold's own len(out): raw elements generated so far
			for {
				keep, err := evalPredicate(whileLam, elemT, cur)
				if err != nil {
					return nil, streamErr("Unfold", unfoldNode, "While: after %d step(s): %v", emitted, err)
				}
				if !keep {
					return out, nil
				}

				val := cur
				survived := true
				for _, s := range steps {
					lam, _ := s.Meta["lambda"].(*ast.Lambda)
					switch s.Prim {
					case "Map Each":
						r, err := eval.EvalLambdaTyped(lam, []*ir.Type{s.In.Elem}, val)
						if err != nil {
							// Unreachable when isElementwiseSafe held at fuse
							// time (the lambda was proven total) — kept as a
							// defensive match for prims' own error shape.
							return nil, streamErr("Map Each", s, "element %d: %v", emitted, err)
						}
						val = r
					case "Filter":
						r, err := eval.EvalLambdaTyped(lam, []*ir.Type{s.In.Elem}, val)
						if err != nil {
							return nil, streamErr("Filter", s, "element %d: %v", emitted, err)
						}
						keep, ok := r.(bool)
						if !ok || !keep {
							survived = false
						}
					}
					if !survived {
						break
					}
				}
				if survived {
					out = append(out, val)
				}
				emitted++
				if earlyExit && int64(len(out)) >= takeN {
					return out, nil
				}
				if streamMaxIterations > 0 && emitted > streamMaxIterations {
					return nil, streamErr("Unfold", unfoldNode,
						"produced more than %d elements (non-terminating While:?)", streamMaxIterations)
				}
				if cur, err = eval.EvalLambdaTyped(stepLam, []*ir.Type{elemT}, cur); err != nil {
					return nil, streamErr("Unfold", unfoldNode, "step %d: %v", emitted, err)
				}
			}
		},
	}
}
