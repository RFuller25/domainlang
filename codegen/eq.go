package codegen

import (
	"fmt"
	"strings"

	"domain/ir"
)

// Structural equality, mirroring ir.DeepEqual. Scalars compare inline with
// ==; composites get an interned dmEqN function. This backs both `=` on
// composite values inside compiled lambdas and the Fixed Point loop's
// convergence test.

// eqExpr returns a Go boolean expression comparing a and b, both of type t.
func (g *gen) eqExpr(a, b string, t *ir.Type) string {
	switch t.Kind {
	case ir.KInt, ir.KFloat, ir.KText, ir.KBool:
		return "(" + a + " == " + b + ")"
	case ir.KGraph:
		// One generic helper rather than an interned per-type function: a
		// graph's nodes are keyable, so they are already a Go comparable, and
		// the comparison never has to descend into an element type. It also
		// keeps the order-insensitivity (ir.GraphEqual's contract) written
		// once instead of re-emitted per node type.
		g.goType(t)
		g.chelper(t.Elem, "dmGraph", declGraph)
		return g.chelper(t.Elem, "dmGraphEq", declGraphEq) + "(" + a + ", " + b + ")"
	default:
		fn := g.eqFunc(t)
		return fn + "(" + a + ", " + b + ")"
	}
}

// eqFunc interns a structural-equality function for a composite type.
func (g *gen) eqFunc(t *ir.Type) string {
	// Canonical, so two Record declaration orders share one equality function
	// exactly as they share one struct.
	key := canonicalKey(t)
	if name, ok := g.eqFns[key]; ok {
		return name
	}
	name := fmt.Sprintf("dmEq%d", len(g.eqFns)+1)
	g.eqFns[key] = name

	goT := g.goType(t)
	var sb strings.Builder
	switch t.Kind {
	case ir.KList:
		elemEq := g.eqExpr("a[i]", "b[i]", t.Elem)
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !%s {
			return false
		}
	}
	return true
}`, name, goT, elemEq)
	case ir.KRecord:
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			fn := fieldName(f.Name)
			fe := g.eqExpr("a."+fn, "b."+fn, f.Type)
			parts[i] = fe
		}
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	return %s
}`, name, goT, joinAnd(parts))
	case ir.KTuple:
		parts := make([]string, len(t.Elems))
		for i, et := range t.Elems {
			fn := tupleField(i)
			fe := g.eqExpr("a."+fn, "b."+fn, et)
			parts[i] = fe
		}
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	return %s
}`, name, goT, joinAnd(parts))
	case ir.KGrid:
		cellEq := g.eqExpr("a.cells[i]", "b.cells[i]", t.Elem)
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if a.rows != b.rows || a.cols != b.cols {
		return false
	}
	for i := range a.cells {
		if !%s {
			return false
		}
	}
	return true
}`, name, goT, cellEq)
	case ir.KMap:
		valEq := g.eqExpr("a.vs[i]", "bv", t.Elem)
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if len(a.keys) != len(b.keys) {
		return false
	}
	for i, k := range a.keys {
		bv, ok := b.get(k)
		if !ok || !%s {
			return false
		}
	}
	return true
}`, name, goT, valEq)
	case ir.KSet:
		if lKeyed(t.Elem) {
			fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if len(a.elems) != len(b.elems) {
		return false
	}
	for _, e := range a.elems {
		if !b.contains(e) {
			return false
		}
	}
	return true
}`, name, goT)
			break
		}
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if len(a.elems) != len(b.elems) {
		return false
	}
	for _, e := range a.elems {
		if _, ok := b.has[e]; !ok {
			return false
		}
	}
	return true
}`, name, goT)
	case ir.KSparse:
		defEq := g.eqExpr("a.def", "b.def", t.Elem)
		cellEq := g.eqExpr("av", "bv", t.Elem)
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if !%s || a.count() != b.count() {
		return false
	}
	for _, k := range a.allPts() {
		av := a.val(k)
		bv, ok := b.get(k.r, k.c)
		if !ok || !%s {
			return false
		}
	}
	return true
}`, name, goT, defEq, cellEq)
	default:
		panic(failf("no equality for type %s", t))
	}
	g.decls = append(g.decls, sb.String())
	return name
}

// joinAnd renders a conjunction, degenerating to true for zero terms (a
// fieldless record cannot occur today, but never emit `return `).
func joinAnd(parts []string) string {
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, " && ")
}
