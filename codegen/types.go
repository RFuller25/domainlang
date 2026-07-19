package codegen

import (
	"fmt"
	gotoken "go/token"
	"strings"

	"domain/ir"
)

// goType maps a Domain type to its unboxed Go representation, interning
// generated struct declarations as a side effect.
func (g *gen) goType(t *ir.Type) (string, error) {
	if t == nil {
		return "", fmt.Errorf("missing type")
	}
	switch t.Kind {
	case ir.KInt:
		return "int64", nil
	case ir.KFloat:
		return "float64", nil
	case ir.KText:
		return "string", nil
	case ir.KBool:
		return "bool", nil
	case ir.KList:
		elem, err := g.goType(t.Elem)
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	case ir.KRecord:
		return g.recordType(t)
	case ir.KTuple:
		return g.tupleType(t)
	case ir.KGrid:
		elem, err := g.goType(t.Elem)
		if err != nil {
			return "", err
		}
		g.helper("dmGrid", declGrid)
		return "dmGrid[" + elem + "]", nil
	case ir.KMap:
		key, err := g.goType(t.Key)
		if err != nil {
			return "", err
		}
		val, err := g.goType(t.Elem)
		if err != nil {
			return "", err
		}
		g.helper("dmMap", declMap)
		return "dmMap[" + key + ", " + val + "]", nil
	case ir.KSet:
		elem, err := g.goType(t.Elem)
		if err != nil {
			return "", err
		}
		g.helper("dmSet", declSet)
		return "dmSet[" + elem + "]", nil
	case ir.KSparse:
		elem, err := g.goType(t.Elem)
		if err != nil {
			return "", err
		}
		g.helper("dmSparse", declSparse, "sort")
		return "dmSparse[" + elem + "]", nil
	default:
		return "", fmt.Errorf("type %s has no compiled representation yet", t)
	}
}

// recordType interns a struct declaration for a Record type. The intern table
// is an ir.Memo: the declaration is generated once per structural type key.
func (g *gen) recordType(t *ir.Type) (string, error) {
	var err error
	name := g.records.Get(t.String(), func() string {
		name := fmt.Sprintf("R%d", g.records.Len()+1)
		var sb strings.Builder
		fmt.Fprintf(&sb, "type %s struct {\n", name)
		for _, f := range t.Fields {
			ft, ferr := g.goType(f.Type)
			if ferr != nil {
				err = ferr
				return name
			}
			fmt.Fprintf(&sb, "\t%s %s\n", fieldName(f.Name), ft)
		}
		sb.WriteString("}")
		g.decls = append(g.decls, sb.String())
		return name
	})
	if err != nil {
		return "", err
	}
	return name, nil
}

// tupleType interns a struct declaration for a Tuple type, with positional
// fields f0..fn. (The interpreter reuses []Value for tuples; the compiled
// representation is a struct because the element types differ.)
func (g *gen) tupleType(t *ir.Type) (string, error) {
	var err error
	name := g.tuples.Get(t.String(), func() string {
		name := fmt.Sprintf("Tup%d", g.tuples.Len()+1)
		var sb strings.Builder
		fmt.Fprintf(&sb, "type %s struct {\n", name)
		for i, et := range t.Elems {
			ft, ferr := g.goType(et)
			if ferr != nil {
				err = ferr
				return name
			}
			fmt.Fprintf(&sb, "\t%s %s\n", tupleField(i), ft)
		}
		sb.WriteString("}")
		g.decls = append(g.decls, sb.String())
		return name
	})
	if err != nil {
		return "", err
	}
	return name, nil
}

func tupleField(i int) string { return fmt.Sprintf("f%d", i) }

// fieldName maps a Domain record field to a Go struct field. Field names come
// from Match Pattern hole names, which are regex group names ([A-Za-z0-9_]);
// anything that would not be a legal Go identifier is sanitized and Go
// keywords are prefixed.
func fieldName(name string) string {
	var sb strings.Builder
	for i, r := range name {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			sb.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				sb.WriteByte('f')
			}
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	out := sb.String()
	if out == "" || gotoken.IsKeyword(out) {
		return "f_" + out
	}
	return out
}

// ---------------------------------------------------------------------------
// value rendering (the Reveal sink)
// ---------------------------------------------------------------------------

// scalarFmt returns a Go expression rendering expr (of scalar type t) as the
// same string ir.FormatValue produces.
func (g *gen) scalarFmt(expr string, t *ir.Type) (string, error) {
	switch t.Kind {
	case ir.KInt:
		g.imp("strconv")
		return "strconv.FormatInt(" + expr + ", 10)", nil
	case ir.KFloat:
		g.helper("dmFmtFloat", declFmtFloat, "strconv")
		return "dmFmtFloat(" + expr + ")", nil
	case ir.KText:
		return expr, nil
	case ir.KBool:
		g.imp("strconv")
		return "strconv.FormatBool(" + expr + ")", nil
	default:
		fn, err := g.fmtFunc(t)
		if err != nil {
			return "", err
		}
		return fn + "(" + expr + ")", nil
	}
}

// fmtFunc interns a formatter function for a composite type, mirroring
// ir.FormatValue's rendering exactly.
func (g *gen) fmtFunc(t *ir.Type) (string, error) {
	key := t.String()
	if name, ok := g.fmtFns[key]; ok {
		return name, nil
	}
	name := fmt.Sprintf("dmFmt%d", len(g.fmtFns)+1)
	g.fmtFns[key] = name

	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	switch t.Kind {
	case ir.KList:
		elemFmt, err := g.scalarFmt("e", t.Elem)
		if err != nil {
			return "", err
		}
		g.imp("strings")
		fmt.Fprintf(&sb, `func %s(v %s) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, e := range v {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(%s)
	}
	sb.WriteByte(']')
	return sb.String()
}`, name, goT, elemFmt)
	case ir.KRecord:
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			ff, err := g.scalarFmt("v."+fieldName(f.Name), f.Type)
			if err != nil {
				return "", err
			}
			parts[i] = goStr(f.Name+": ") + " + " + ff
		}
		fmt.Fprintf(&sb, `func %s(v %s) string {
	return "{" + %s + "}"
}`, name, goT, strings.Join(parts, ` + ", " + `))
	case ir.KTuple:
		// The interpreter renders tuples list-style ([]Value under the hood).
		parts := make([]string, len(t.Elems))
		for i, et := range t.Elems {
			ff, err := g.scalarFmt("v."+tupleField(i), et)
			if err != nil {
				return "", err
			}
			parts[i] = ff
		}
		fmt.Fprintf(&sb, `func %s(v %s) string {
	return "[" + %s + "]"
}`, name, goT, strings.Join(parts, ` + ", " + `))
	case ir.KMap:
		keyFmt, err := g.scalarFmt("k", t.Key)
		if err != nil {
			return "", err
		}
		valFmt, err := g.scalarFmt("v.vals[k]", t.Elem)
		if err != nil {
			return "", err
		}
		g.imp("strings")
		fmt.Fprintf(&sb, `func %s(v %s) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range v.keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(%s)
		sb.WriteString(": ")
		sb.WriteString(%s)
	}
	sb.WriteByte('}')
	return sb.String()
}`, name, goT, keyFmt, valFmt)
	case ir.KSet:
		elemFmt, err := g.scalarFmt("e", t.Elem)
		if err != nil {
			return "", err
		}
		g.imp("strings")
		fmt.Fprintf(&sb, `func %s(v %s) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, e := range v.elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(%s)
	}
	sb.WriteByte('}')
	return sb.String()
}`, name, goT, elemFmt)
	case ir.KGrid:
		cellFmt, err := g.scalarFmt("v.cells[r*v.cols+c]", t.Elem)
		if err != nil {
			return "", err
		}
		sep := ""
		if t.Elem.Kind != ir.KText {
			sep = `
		if c > 0 {
			sb.WriteByte(' ')
		}`
		}
		g.imp("strings")
		fmt.Fprintf(&sb, `func %s(v %s) string {
	var sb strings.Builder
	for r := 0; r < v.rows; r++ {
		if r > 0 {
			sb.WriteByte('\n')
		}
		for c := 0; c < v.cols; c++ {%s
			sb.WriteString(%s)
		}
	}
	return sb.String()
}`, name, goT, sep, cellFmt)
	case ir.KSparse:
		// Mirrors ir.FormatValue: a map-style listing of the set cells in
		// sorted row-major order, keys rendered as points ([r, c]).
		cellFmt, err := g.scalarFmt("v.cells[p]", t.Elem)
		if err != nil {
			return "", err
		}
		g.imp("strings", "strconv")
		fmt.Fprintf(&sb, `func %s(v %s) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, p := range v.pts() {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('[')
		sb.WriteString(strconv.FormatInt(p.r, 10))
		sb.WriteString(", ")
		sb.WriteString(strconv.FormatInt(p.c, 10))
		sb.WriteString("]: ")
		sb.WriteString(%s)
	}
	sb.WriteByte('}')
	return sb.String()
}`, name, goT, cellFmt)
	default:
		return "", fmt.Errorf("no renderer for type %s yet", t)
	}
	g.decls = append(g.decls, sb.String())
	return name, nil
}
