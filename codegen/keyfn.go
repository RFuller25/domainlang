package codegen

import (
	"fmt"
	"strings"

	"domain/ir"
)

// Hashing a value that contains a List.
//
// Tuples and Records compile to Go structs of comparable fields, so they are
// map keys as they stand. A List compiles to a slice, which Go cannot compare,
// so a primitive that accepts a list-bearing key (ir.ListKeyable: Find Cycle,
// Unique) hashes it through an injective byte encoding and keeps a
// map[string]… instead. The original value is never rebuilt from the key: each
// primitive keeps the elements themselves where it has to give them back, in
// order.
//
// The encoding is injective *within one static type*, which is all a map needs
// (every key of a given map has the same type): an Int is its eight bytes, a
// Text and a List are length-prefixed, a Tuple and a Record are their fields in
// order (a Record's in name order, as the generated struct declares them).

// keyFn returns the name of a generated `func(T) string` for t, interning it.
func (g *gen) keyFn(t *ir.Type) (string, error) {
	app, err := g.keyAppender(t)
	if err != nil {
		return "", err
	}
	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	return g.listFn("key:"+canonicalKey(t), "Key", func(name string) (string, error) {
		return fmt.Sprintf("func %s(x %s) string {\n\treturn string(%s(make([]byte, 0, 32), x))\n}", name, goT, app), nil
	})
}

// keyAppender returns the name of a generated `func([]byte, T) []byte` that
// appends t's encoding.
func (g *gen) keyAppender(t *ir.Type) (string, error) {
	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	g.imp("encoding/binary")
	return g.listFn("keyapp:"+canonicalKey(t), "KeyApp", func(name string) (string, error) {
		var sb strings.Builder
		fmt.Fprintf(&sb, "func %s(b []byte, x %s) []byte {\n", name, goT)
		switch t.Kind {
		case ir.KInt:
			sb.WriteString("\treturn binary.LittleEndian.AppendUint64(b, uint64(x))\n")
		case ir.KText:
			sb.WriteString("\tb = binary.LittleEndian.AppendUint64(b, uint64(len(x)))\n\treturn append(b, x...)\n")
		case ir.KList:
			sub, err := g.keyAppenderCall(t.Elem, "b", "e")
			if err != nil {
				return "", err
			}
			sb.WriteString("\tb = binary.LittleEndian.AppendUint64(b, uint64(len(x)))\n")
			fmt.Fprintf(&sb, "\tfor _, e := range x {\n\t\tb = %s\n\t}\n\treturn b\n", sub)
		case ir.KTuple:
			for i, et := range t.Elems {
				sub, err := g.keyAppenderCall(et, "b", "x."+tupleField(i))
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&sb, "\tb = %s\n", sub)
			}
			sb.WriteString("\treturn b\n")
		case ir.KRecord:
			for _, f := range canonicalFields(t) {
				sub, err := g.keyAppenderCall(f.Type, "b", "x."+fieldName(f.Name))
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&sb, "\tb = %s\n", sub)
			}
			sb.WriteString("\treturn b\n")
		default:
			return "", fmt.Errorf("type %s has no hashable representation", t)
		}
		sb.WriteString("}")
		return sb.String(), nil
	})
}

func (g *gen) keyAppenderCall(t *ir.Type, b, x string) (string, error) {
	app, err := g.keyAppender(t)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s(%s, %s)", app, b, x), nil
}

// hashKey is how a primitive keys an internal seen-map on a value of type t
// held in the Go variable v (whose Go type is goT): the value itself when Go
// can compare it, its byte encoding when it holds a List.
func (g *gen) hashKey(t *ir.Type, v, goT string) (key, keyGo string, err error) {
	if !ir.ContainsList(t) {
		return v, goT, nil
	}
	kf, err := g.keyFn(t)
	if err != nil {
		return "", "", err
	}
	return kf + "(" + v + ")", "string", nil
}
