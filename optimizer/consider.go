package optimizer

import (
	"domain/ast"
	"domain/ir"
)

// Adjacent-pair rules seen through a single-stage Consider.
//
// A stage carrying a `Consider … Of` binding sits alone inside a Consider node,
// so a pair rule never saw it. Seeing through it is sound for exactly one
// shape: the wrapped stage is the FIRST of the pair, and the rule answers with
// ONE node.
//
//   - The binding is computed from the value entering the Consider, which is
//     the value entering the wrapped stage. A rule that replaces (a, b) with a
//     node reading a's input keeps that true; the replacement goes where a was,
//     inside the scope, so the binding is still computed first, once, from the
//     same value.
//   - The scope grows to cover b. A binding name that b reads from an enclosing
//     scope would be captured by it, so b must not mention any binding name
//     (nodeMentions, which treats anything it cannot read as a mention).
//   - If b were the wrapped stage, fusing a into it would feed the binding a's
//     input instead of a's output. That is never done.
//   - A rule that answers with no node or several is declined: no node is
//     left to carry the scope, or it is unclear which of them needs it.
//
// The Consider's body is updated in place because its Eval closure reads that
// slice, not Meta["nodes"].

// soleStage returns the one stage inside a single-stage Consider.
func soleStage(n *ir.Node) (*ir.Node, bool) {
	if n == nil || n.Prim != "Consider" {
		return nil, false
	}
	sub, _ := n.Meta["nodes"].([]*ir.Node)
	if len(sub) != 1 || sub[0] == nil || sub[0].Prim == "Consider" {
		return nil, false
	}
	return sub[0], true
}

// bindingNames lists the names a Consider node binds.
func bindingNames(n *ir.Node) ([]string, bool) {
	binds, ok := n.Meta[ir.MetaBinds].([]ir.Binding)
	if !ok || len(binds) == 0 {
		return nil, false
	}
	names := make([]string, len(binds))
	for i, b := range binds {
		names[i] = b.Name()
	}
	return names, true
}

// applyPairRule runs a pair rule on (a, b), and failing that, through a when a
// is a single-stage Consider.
func applyPairRule(rule pairRewrite, a, b *ir.Node) ([]*ir.Node, string, bool) {
	if repl, msg, ok := rule(a, b); ok {
		return repl, msg, true
	}
	inner, ok := soleStage(a)
	if !ok || b.Prim == "Consider" {
		return nil, "", false
	}
	names, ok := bindingNames(a)
	if !ok || nodeMentions(b, names) {
		return nil, "", false
	}
	repl, msg, ok := rule(inner, b)
	if !ok || len(repl) != 1 {
		return nil, "", false
	}
	a.Meta["nodes"].([]*ir.Node)[0] = repl[0]
	a.Out = repl[0].Out
	return []*ir.Node{a}, msg, true
}

// nodeMentions reports whether any lambda or expression held by n (or by a
// body inside it) reads one of names. Anything it cannot read counts as a
// mention.
func nodeMentions(n *ir.Node, names []string) bool {
	for _, v := range n.Meta {
		switch m := v.(type) {
		case nil, string, bool, int, int64, ir.MeasureFn:
		case *ast.Lambda:
			if m == nil {
				continue
			}
			if bb, isBlock := m.Body.(*ast.BlockBody); isBlock {
				if bb.Pipe == nil {
					return true
				}
				for _, sub := range bb.Pipe.BlockNodes() {
					if nodeMentions(sub, names) {
						return true
					}
				}
				continue
			}
			for _, name := range names {
				if mentions(m.Body, name) {
					return true
				}
			}
		case []*ir.Node:
			for _, sub := range m {
				if nodeMentions(sub, names) {
					return true
				}
			}
		default:
			return true
		}
	}
	return false
}
