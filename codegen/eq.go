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
func (g *gen) eqExpr(a, b string, t *ir.Type) (string, error) {
	switch t.Kind {
	case ir.KInt, ir.KFloat, ir.KText, ir.KBool:
		return "(" + a + " == " + b + ")", nil
	default:
		fn, err := g.eqFunc(t)
		if err != nil {
			return "", err
		}
		return fn + "(" + a + ", " + b + ")", nil
	}
}

// eqFunc interns a structural-equality function for a composite type.
func (g *gen) eqFunc(t *ir.Type) (string, error) {
	key := t.String()
	if name, ok := g.eqFns[key]; ok {
		return name, nil
	}
	name := fmt.Sprintf("dmEq%d", len(g.eqFns)+1)
	g.eqFns[key] = name

	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	switch t.Kind {
	case ir.KList:
		elemEq, err := g.eqExpr("a[i]", "b[i]", t.Elem)
		if err != nil {
			return "", err
		}
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
			fe, err := g.eqExpr("a."+fn, "b."+fn, f.Type)
			if err != nil {
				return "", err
			}
			parts[i] = fe
		}
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	return %s
}`, name, goT, joinAnd(parts))
	case ir.KTuple:
		parts := make([]string, len(t.Elems))
		for i, et := range t.Elems {
			fn := tupleField(i)
			fe, err := g.eqExpr("a."+fn, "b."+fn, et)
			if err != nil {
				return "", err
			}
			parts[i] = fe
		}
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	return %s
}`, name, goT, joinAnd(parts))
	case ir.KGrid:
		cellEq, err := g.eqExpr("a.cells[i]", "b.cells[i]", t.Elem)
		if err != nil {
			return "", err
		}
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
		valEq, err := g.eqExpr("a.vals[k]", "bv", t.Elem)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if len(a.keys) != len(b.keys) {
		return false
	}
	for _, k := range a.keys {
		bv, ok := b.vals[k]
		if !ok || !%s {
			return false
		}
	}
	return true
}`, name, goT, valEq)
	case ir.KSet:
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
		defEq, err := g.eqExpr("a.def", "b.def", t.Elem)
		if err != nil {
			return "", err
		}
		cellEq, err := g.eqExpr("av", "bv", t.Elem)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, `func %s(a, b %s) bool {
	if !%s || len(a.cells) != len(b.cells) {
		return false
	}
	for k, av := range a.cells {
		bv, ok := b.cells[k]
		if !ok || !%s {
			return false
		}
	}
	return true
}`, name, goT, defEq, cellEq)
	default:
		return "", fmt.Errorf("no equality for type %s", t)
	}
	g.decls = append(g.decls, sb.String())
	return name, nil
}

// joinAnd renders a conjunction, degenerating to true for zero terms (a
// fieldless record cannot occur today, but never emit `return `).
func joinAnd(parts []string) string {
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, " && ")
}
