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
func (g *gen) zeroExpr(t *ir.Type) (string, error) {
	if t == nil {
		return "", fmt.Errorf("no type to take the zero of")
	}
	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	switch t.Kind {
	case ir.KInt:
		return "int64(0)", nil
	case ir.KFloat:
		return "float64(0)", nil
	case ir.KText:
		return `""`, nil
	case ir.KBool:
		return "false", nil
	case ir.KList:
		return goT + "{}", nil
	case ir.KMap:
		key, kerr := g.goType(t.Key)
		if kerr != nil {
			return "", kerr
		}
		elem, eerr := g.goType(t.Elem)
		if eerr != nil {
			return "", eerr
		}
		return fmt.Sprintf("dmNewMap[%s, %s]()", key, elem), nil
	case ir.KSet:
		elem, eerr := g.goType(t.Elem)
		if eerr != nil {
			return "", eerr
		}
		return fmt.Sprintf("dmNewSet[%s]()", elem), nil
	case ir.KGraph:
		elem, eerr := g.goType(t.Elem)
		if eerr != nil {
			return "", eerr
		}
		return fmt.Sprintf("dmNewGraph[%s]()", elem), nil
	case ir.KSparse:
		elem, eerr := g.goType(t.Elem)
		if eerr != nil {
			return "", eerr
		}
		def, derr := g.zeroExpr(t.Elem)
		if derr != nil {
			return "", derr
		}
		return fmt.Sprintf("dmNewSparse[%s](%s)", elem, def), nil
	case ir.KGrid:
		// An empty grid rather than a one-cell one, as ir.ZeroValue says: a
		// picture of nothing is nothing.
		return goT + "{}", nil
	case ir.KView:
		return "dmViewBlank()", nil
	case ir.KTuple:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			z, zerr := g.zeroExpr(e)
			if zerr != nil {
				return "", zerr
			}
			parts[i] = fmt.Sprintf("F%d: %s", i, z)
		}
		return goT + "{" + strings.Join(parts, ", ") + "}", nil
	case ir.KRecord:
		// Named fields rather than a bare literal: a record whose field is a
		// Map needs that field built, not left nil.
		fields := canonicalFields(t)
		parts := make([]string, len(fields))
		for i, f := range fields {
			z, zerr := g.zeroExpr(f.Type)
			if zerr != nil {
				return "", zerr
			}
			parts[i] = fmt.Sprintf("%s: %s", fieldName(f.Name), z)
		}
		return goT + "{" + strings.Join(parts, ", ") + "}", nil
	}
	return "", fmt.Errorf("%s has no zero value in compiled code", t)
}
