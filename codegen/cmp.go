package codegen

import (
	"fmt"
	"strings"

	"domain/ir"
)

// Three-way ordering, mirroring ir.Compare. Int, Float and Text order with
// Go's own `<` — byte-wise for strings, which agrees with strings.Compare and
// so with the interpreter — while a tuple gets an interned dmCmpN, the way a
// composite gets an interned dmEqN in eq.go.
//
// This backs `<` `>` `<=` `>=` over the composite half of ir.Ordered. The
// scalar half never reaches here: compileBinary emits the Go operator
// directly, because for those types it is the same comparison.

// cmpExpr returns a Go int expression over two values of type t: negative,
// zero or positive, exactly as ir.Compare answers.
func (g *gen) cmpExpr(a, b string, t *ir.Type) (string, error) {
	if t != nil && t.Kind == ir.KTuple {
		fn, err := g.cmpFunc(t)
		if err != nil {
			return "", err
		}
		return fn + "(" + a + ", " + b + ")", nil
	}
	if !ir.Ordered(t) {
		return "", fmt.Errorf("no ordering for type %s", t)
	}
	g.helper("dmCmp", declCmp)
	return "dmCmp(" + a + ", " + b + ")", nil
}

// cmpFunc interns a lexicographic comparison for a tuple type: the first
// differing element decides, and equal elements fall through to the next.
// Static types make both sides the same arity, so there is no length case.
func (g *gen) cmpFunc(t *ir.Type) (string, error) {
	key := canonicalKey(t)
	if name, ok := g.cmpFns[key]; ok {
		return name, nil
	}
	name := fmt.Sprintf("dmCmp%d", len(g.cmpFns)+1)
	g.cmpFns[key] = name

	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "func %s(a, b %s) int {\n", name, goT)
	for i, et := range t.Elems {
		f := tupleField(i)
		ce, err := g.cmpExpr("a."+f, "b."+f, et)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "\tif c := %s; c != 0 {\n\t\treturn c\n\t}\n", ce)
	}
	sb.WriteString("\treturn 0\n}")
	g.decls = append(g.decls, sb.String())
	return name, nil
}

// declCmp is the scalar three-way compare. It is written as two tests rather
// than a subtraction so it matches ir.Compare's compareFloat on NaN, which is
// ordered against nothing and therefore compares equal to everything — the
// same choice the float Sort makes.
const declCmp = `func dmCmp[T int64 | float64 | string](a, b T) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}`
