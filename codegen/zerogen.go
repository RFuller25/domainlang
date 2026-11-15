// The zero value of a type, compiled.
//
// This mirrors ir.ZeroValue, and it exists for the same one reason: a request
// that fails still has to hand the program something of the type it declared.
// `Part Reply` is typed against `{ok, error, value}`, and when `ok` is false
// there is no value to put there.
//
// It is not Go's own zero value, and the difference is not cosmetic. Go's zero
// for a Map is a nil map and for a List a nil slice — reading either is fine
// and writing to the first panics, so a program that took a failed reply and
// then put something in its map would die on the one path it was written to
// survive. The interpreter's zero is an empty container, so this one is too.
package codegen

import (
	"fmt"
	"strings"

	"domain/ir"
)

// zeroExpr is a Go expression for the zero value of t.
func (g *gen) zeroExpr(t *ir.Type) string {
	if t == nil {
		panic(failf("no type to take the zero of"))
	}
	goT := g.goType(t)
	switch t.Kind {
	case ir.KInt:
		return "int64(0)"
	case ir.KFloat:
		return "float64(0)"
	case ir.KText:
		return `""`
	case ir.KBool:
		return "false"
	case ir.KList:
		return goT + "{}"
	case ir.KMap:
		key := g.goType(t.Key)
		elem := g.goType(t.Elem)
		return g.newMapExpr(t.Key, key, elem, "")
	case ir.KSet:
		elem := g.goType(t.Elem)
		return g.newSetExpr(t.Elem, elem)
	case ir.KGraph:
		elem := g.goType(t.Elem)
		return g.newGraphExpr(t.Elem, elem)
	case ir.KSparse:
		elem := g.goType(t.Elem)
		def := g.zeroExpr(t.Elem)
		return fmt.Sprintf("dmNewSparse[%s](%s)", elem, def)
	case ir.KGrid:
		// An empty grid rather than a one-cell one, as ir.ZeroValue says: a
		// picture of nothing is nothing.
		return goT + "{}"
	case ir.KView:
		return "dmViewBlank()"
	case ir.KTuple:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			z := g.zeroExpr(e)
			parts[i] = fmt.Sprintf("F%d: %s", i, z)
		}
		return goT + "{" + strings.Join(parts, ", ") + "}"
	case ir.KRecord:
		// Named fields rather than a bare literal: a record whose field is a
		// Map needs that field built, not left nil.
		fields := canonicalFields(t)
		parts := make([]string, len(fields))
		for i, f := range fields {
			z := g.zeroExpr(f.Type)
			parts[i] = fmt.Sprintf("%s: %s", fieldName(f.Name), z)
		}
		return goT + "{" + strings.Join(parts, ", ") + "}"
	}
	panic(failf("%s has no zero value in compiled code", t))
}
