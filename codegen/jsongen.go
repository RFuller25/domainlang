// A value as JSON, and JSON as a value, compiled.
//
// Both directions mirror ir/json.go. They are not symmetrical: writing is a
// function of the value, reading needs a declared type — so the writers are
// interned per type and so are the readers, and `Convert From JSON` takes an
// `Into:` while `Convert To JSON` takes nothing.
package codegen

import (
	"fmt"
	"strings"

	"domain/ir"
)

// jsonFunc interns a JSON writer for a type, mirroring ir.ToJSON's rules: a
// Record is an object in declared field order, a List is an array, a Map is an
// object whose keys are rendered and sorted.
func (g *gen) jsonFunc(t *ir.Type) (string, error) {
	key := "json:" + canonicalKey(t)
	if name, ok := g.fmtFns[key]; ok {
		return name, nil
	}
	name := fmt.Sprintf("dmJSON%d", len(g.fmtFns)+1)
	g.fmtFns[key] = name
	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	g.helper("dmJSONString", declJSONString, "strconv")
	body, err := g.jsonBody(t, "v")
	if err != nil {
		return "", err
	}
	g.imp("strings")
	g.decls = append(g.decls, fmt.Sprintf(`func %s(v %s) string {
	var sb strings.Builder
%s
	return sb.String()
}`, name, goT, body))
	return name, nil
}

// jsonBody emits the statements that write expr into sb.
func (g *gen) jsonBody(t *ir.Type, expr string) (string, error) {
	if t == nil {
		return "", fmt.Errorf("a value of unknown type has no JSON form")
	}
	switch t.Kind {
	case ir.KInt:
		g.imp("strconv")
		return "\tsb.WriteString(strconv.FormatInt(" + expr + ", 10))", nil
	case ir.KFloat:
		g.helper("dmFmtFloat", declFmtFloat, "strconv")
		return "\tsb.WriteString(dmFmtFloat(" + expr + "))", nil
	case ir.KBool:
		g.imp("strconv")
		return "\tsb.WriteString(strconv.FormatBool(" + expr + "))", nil
	case ir.KText:
		return "\tsb.WriteString(dmJSONString(" + expr + "))", nil
	case ir.KList, ir.KSet:
		elem := t.Elem
		fn, err := g.jsonFunc(elem)
		if err != nil {
			return "", err
		}
		items := expr
		if t.Kind == ir.KSet {
			items = expr + ".items()"
		}
		return fmt.Sprintf(`	sb.WriteByte('[')
	for i, e := range %s {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(%s(e))
	}
	sb.WriteByte(']')`, items, fn), nil
	case ir.KTuple:
		var out string
		out = "\tsb.WriteByte('[')\n"
		for i, e := range t.Elems {
			fn, err := g.jsonFunc(e)
			if err != nil {
				return "", err
			}
			if i > 0 {
				out += "\tsb.WriteByte(',')\n"
			}
			out += fmt.Sprintf("\tsb.WriteString(%s(%s.F%d))\n", fn, expr, i)
		}
		// A tuple is a JSON array, so it closes with a bracket. It read ']'
		// as '}' until a game decoded one back, which nothing did while the
		// scope was refused whole.
		return out + "\tsb.WriteByte(']')", nil
	case ir.KMap:
		// A Map is an object, which means its keys must be strings — so an
		// Int-keyed map is written with its keys rendered, the way it already
		// prints. Sorted, because a JSON object a server compares has to be
		// the same bytes for the same value. (ir/json.go says the same.)
		keyFmt, err := g.scalarFmt("k", t.Key)
		if err != nil {
			return "", err
		}
		valFn, err := g.jsonFunc(t.Elem)
		if err != nil {
			return "", err
		}
		g.imp("sort")
		return fmt.Sprintf(`	names := make([]string, 0, len(%[1]s.keys))
	byName := map[string]string{}
	for _, k := range %[1]s.keys {
		name := %[2]s
		names = append(names, name)
		byName[name] = %[3]s(%[1]s.vals[k])
	}
	sort.Strings(names)
	sb.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(dmJSONString(name))
		sb.WriteByte(':')
		sb.WriteString(byName[name])
	}
	sb.WriteByte('}')`, expr, keyFmt, valFn), nil
	case ir.KRecord:
		var out string
		out = "\tsb.WriteByte('{')\n"
		for i, f := range t.Fields {
			fn, err := g.jsonFunc(f.Type)
			if err != nil {
				return "", err
			}
			if i > 0 {
				out += "\tsb.WriteByte(',')\n"
			}
			out += fmt.Sprintf("\tsb.WriteString(dmJSONString(%q))\n\tsb.WriteByte(':')\n", f.Name)
			out += fmt.Sprintf("\tsb.WriteString(%s(%s.%s))\n", fn, expr, fieldName(f.Name))
		}
		return out + "\tsb.WriteByte('}')", nil
	}
	return "", fmt.Errorf("%s has no JSON form", t)
}

// declJSONString quotes a string the way encoding/json does. strconv.Quote is
// Go's escaping, not JSON's, and the two differ — so this is the small subset
// that matters rather than a call to a package the emitted program would
// otherwise not need.
const declJSONString = `func dmJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(` + "`" + `\"` + "`" + `)
		case '\\':
			b.WriteString(` + "`" + `\\` + "`" + `)
		case '\n':
			b.WriteString(` + "`" + `\n` + "`" + `)
		case '\r':
			b.WriteString(` + "`" + `\r` + "`" + `)
		case '\t':
			b.WriteString(` + "`" + `\t` + "`" + `)
		default:
			if r < 0x20 {
				b.WriteString("\\u")
				const hex = "0123456789abcdef"
				b.WriteByte('0')
				b.WriteByte('0')
				b.WriteByte(hex[(r>>4)&0xf])
				b.WriteByte(hex[r&0xf])
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}`

// ---------------------------------------------------------------------------
// reading JSON
// ---------------------------------------------------------------------------

// Decoding is not symmetrical with encoding, and the asymmetry is the reason
// this half is generated per type rather than written once. Out is a function
// of the value; in needs a *declared* type, because there is no dynamic value
// in this language to decode onto and then look at. So `Convert From JSON`
// takes a written type and `Request` takes an `Into:`, and each distinct type
// gets a converter of its own.
//
// The rules are ir.convertJSON's, transliterated: strict about a field the
// program declared and the document lacks, forgiving of a field the document
// has and the type does not. A server may send more than a client asked for,
// and a client that broke when it did would break on every deployment.

// jsonInFunc interns a decoder for a type: `func(raw any, path string) (T, error)`.
func (g *gen) jsonInFunc(t *ir.Type) (string, error) {
	key := "fromjson:" + canonicalKey(t)
	if name, ok := g.fmtFns[key]; ok {
		return name, nil
	}
	name := fmt.Sprintf("dmFromJSON%d", len(g.fmtFns)+1)
	g.fmtFns[key] = name
	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	g.helper("dmJSONRead", declJSONRead, "encoding/json", "fmt", "strings")
	zero, err := g.zeroExpr(t)
	if err != nil {
		return "", err
	}
	body, err := g.jsonInBody(t, goT, zero)
	if err != nil {
		return "", err
	}
	g.imp("fmt")
	g.decls = append(g.decls, fmt.Sprintf(`func %s(raw any, path string) (%s, error) {
	var zero %s = %s
%s
}`, name, goT, goT, zero, body))
	return name, nil
}

// jsonInBody is the converter's body: the statements that turn raw into a
// value of t, or return an error saying what was there instead.
func (g *gen) jsonInBody(t *ir.Type, goT, zero string) (string, error) {
	// bad is the one error shape ir.convertJSON uses, so the two backends fail
	// with the same sentence about the same document.
	bad := func(want string) string {
		return fmt.Sprintf("\treturn zero, fmt.Errorf(\"expected %s%%s, got %%s\", dmJSONWhere(path), dmJSONKind(raw))", want)
	}
	switch t.Kind {
	case ir.KInt:
		return `	n, ok := raw.(json.Number)
	if !ok {
` + bad("a whole number") + `
	}
	i, err := n.Int64()
	if err != nil {
		return zero, fmt.Errorf("%s is not a whole number%s", n.String(), dmJSONWhere(path))
	}
	return i, nil`, nil
	case ir.KFloat:
		return `	n, ok := raw.(json.Number)
	if !ok {
` + bad("a number") + `
	}
	f, err := n.Float64()
	if err != nil {
		return zero, fmt.Errorf("%s is not a number%s", n.String(), dmJSONWhere(path))
	}
	return f, nil`, nil
	case ir.KText:
		return `	s, ok := raw.(string)
	if !ok {
` + bad("some text") + `
	}
	return s, nil`, nil
	case ir.KBool:
		return `	b, ok := raw.(bool)
	if !ok {
` + bad("true or false") + `
	}
	return b, nil`, nil
	case ir.KList, ir.KSet:
		elem, err := g.jsonInFunc(t.Elem)
		if err != nil {
			return "", err
		}
		gather := "\t\tout = append(out, v)"
		open := "\tout := " + goT + "{}"
		if t.Kind == ir.KSet {
			open = "\tout := " + zero
			gather = "\t\tout.add(v)"
		}
		return `	arr, ok := raw.([]any)
	if !ok {
` + bad("a list") + `
	}
` + open + `
	for i, e := range arr {
		v, err := ` + elem + `(e, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return zero, err
		}
` + gather + `
	}
	return out, nil`, nil
	case ir.KTuple:
		var sb strings.Builder
		sb.WriteString("\tarr, ok := raw.([]any)\n\tif !ok {\n" + bad("a list") + "\n\t}\n")
		fmt.Fprintf(&sb, "\tif len(arr) != %d {\n\t\treturn zero, fmt.Errorf(\"expected %d values%%s, got %%d\", dmJSONWhere(path), len(arr))\n\t}\n",
			len(t.Elems), len(t.Elems))
		sb.WriteString("\tvar out " + goT + "\n")
		for i, e := range t.Elems {
			fn, err := g.jsonInFunc(e)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&sb, "\tv%d, err%d := %s(arr[%d], fmt.Sprintf(\"%%s[%d]\", path))\n", i, i, fn, i, i)
			fmt.Fprintf(&sb, "\tif err%d != nil {\n\t\treturn zero, err%d\n\t}\n", i, i)
			fmt.Fprintf(&sb, "\tout.F%d = v%d\n", i, i)
		}
		sb.WriteString("\treturn out, nil")
		return sb.String(), nil
	case ir.KRecord:
		var sb strings.Builder
		sb.WriteString("\tobj, ok := raw.(map[string]any)\n\tif !ok {\n" + bad("an object") + "\n\t}\n")
		sb.WriteString("\tout := " + zero + "\n")
		for i, f := range canonicalFields(t) {
			fn, err := g.jsonInFunc(f.Type)
			if err != nil {
				return "", err
			}
			// Strict in this direction on purpose (ir/json.go): a field the
			// program declared and the document lacks is an error naming it,
			// because a silently-zero score is worse than a refusal.
			fmt.Fprintf(&sb, "\te%d, ok%d := obj[%q]\n\tif !ok%d {\n\t\treturn zero, fmt.Errorf(\"no %%q%%s\", %q, dmJSONWhere(path))\n\t}\n",
				i, i, f.Name, i, f.Name)
			fmt.Fprintf(&sb, "\tv%d, err%d := %s(e%d, path+%q)\n\tif err%d != nil {\n\t\treturn zero, err%d\n\t}\n",
				i, i, fn, i, "."+f.Name, i, i)
			fmt.Fprintf(&sb, "\tout.%s = v%d\n", fieldName(f.Name), i)
		}
		sb.WriteString("\treturn out, nil")
		return sb.String(), nil
	case ir.KMap:
		elem, err := g.jsonInFunc(t.Elem)
		if err != nil {
			return "", err
		}
		// A JSON object's keys are strings, so an Int-keyed Map decodes from
		// the rendered key — which is exactly how one is written out again.
		g.imp("sort")
		key := "\t\tk := name"
		if t.Key != nil && t.Key.Kind == ir.KInt {
			g.imp("strconv")
			key = `		k, kerr := strconv.ParseInt(name, 10, 64)
		if kerr != nil {
			return zero, fmt.Errorf("the key %q%s is not a whole number", name, dmJSONWhere(path))
		}`
		}
		return `	obj, ok := raw.(map[string]any)
	if !ok {
` + bad("an object") + `
	}
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	// Sorted, so that decoding one document twice gives one insertion order —
	// which is the order a Map renders and iterates in.
	sort.Strings(names)
	out := ` + zero + `
	for _, name := range names {
` + key + `
		v, err := ` + elem + `(obj[name], path+"."+name)
		if err != nil {
			return zero, err
		}
		out.put(k, v)
	}
	return out, nil`, nil
	}
	return "", fmt.Errorf("%s cannot be decoded from JSON", t)
}

// declJSONRead is the parser and the two helpers every generated converter
// shares. Numbers stay as written until the target type says what they are, so
// a big Int does not lose its low bits passing through a float64 on the way to
// being an Int.
const declJSONRead = `func dmJSONParse(text string) (any, error) {
	var raw any
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("not valid JSON: %v", err)
	}
	return raw, nil
}

func dmJSONWhere(path string) string {
	if path == "" {
		return ""
	}
	return " at " + path
}

func dmJSONKind(raw any) string {
	switch raw.(type) {
	case nil:
		return "null"
	case bool:
		return "true or false"
	case json.Number:
		return "a number"
	case string:
		return "some text"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}
	return "something else"
}`
