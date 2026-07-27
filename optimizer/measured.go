package optimizer

import (
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
)

// Measured arguments and the optimizer.
//
// A primitive's Int argument may be a literal, which lands in Meta as an
// int64, or *measured* — a lambda over the current value, which lands under
// the same key with an "Expr" suffix and has no value until the program runs
// (prims/measure.go). Every rewrite in this package that reads a literal does
// so with a type assertion whose zero value is a perfectly plausible number,
// so a measured argument does not make a pass fail to fire: it makes the pass
// fire with a fabricated constant. `Select Top (measured)` fused as `Top 0`
// returns the empty list, silently.
//
// hasMeasuredArg is the guard against that. A pass consults it before reading
// any literal out of a node, and a measured argument added later to a
// primitive this pass has never heard of is refused by default rather than
// silently mis-folded. Making a pass work *with* a measured argument is then a
// deliberate change at that one call site, which is how the two fusions worth
// keeping (Window+reduce, Sort+Top K) opt back in.
func hasMeasuredArg(n *ir.Node) bool {
	if n == nil {
		return false
	}
	for k, v := range n.Meta {
		if !strings.HasSuffix(k, "Expr") {
			continue
		}
		if _, ok := v.(*ast.Lambda); ok {
			return true
		}
	}
	return false
}

// arg is one of a node's Int arguments as a pass sees it: a literal it may
// fold, or a measured one it may only carry. A pass that can work either way
// reads its arguments through this and never touches Meta directly, so the
// fused node it builds speaks about the argument exactly the way the node it
// replaced did — same value, same errors, same wording.
type arg struct {
	lit int64
	fn  ir.MeasureFn // nil for a literal
	lam *ast.Lambda  // the source lambda, for the compiler to compile
}

func (a arg) measured() bool { return a.fn != nil }

// value resolves the argument for the value flowing into the node. For a
// measured one this is the primitive's own resolver, bound check included, so
// a rewrite cannot turn its error into a success — optimizer safety rule 2.
func (a arg) value(v ir.Value) (int64, error) {
	if a.fn == nil {
		return a.lit, nil
	}
	return a.fn(v)
}

// describe renders the argument for a Display or an --explain message.
func (a arg) describe() string {
	if a.fn == nil {
		return strconv.FormatInt(a.lit, 10)
	}
	return "(measured)"
}

// writeMeta puts the argument on a fused node under key, in whichever of the
// two shapes it arrived in.
func (a arg) writeMeta(meta map[string]any, key string) {
	if a.fn == nil {
		meta[key] = a.lit
		return
	}
	meta[key+"Expr"] = a.lam
	meta[key+"Fn"] = a.fn
}

// readArg reads one Int argument off a node. ok is false when the node carries
// neither form, which is the state a pass must never mistake for zero.
func readArg(n *ir.Node, key string) (a arg, ok bool) {
	if n == nil {
		return arg{}, false
	}
	if lit, isLit := n.Meta[key].(int64); isLit {
		return arg{lit: lit}, true
	}
	fn, hasFn := n.Meta[key+"Fn"].(ir.MeasureFn)
	lam, hasLam := n.Meta[key+"Expr"].(*ast.Lambda)
	if !hasFn || !hasLam {
		return arg{}, false
	}
	return arg{fn: fn, lam: lam}, true
}
