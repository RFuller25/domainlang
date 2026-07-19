package prims

import (
	"fmt"
	"regexp"
	"strconv"

	"domain/ast"
	"domain/ir"
	"domain/pattern"
	"domain/token"
)

// Cursed Technique: Match Pattern — the parsing workhorse. It parses each input
// line against a typed-hole template (see docs/match-pattern.md and the pattern
// package) and emits a Record (named holes) or a tuple/list (positional holes).
//
//	Cursed Technique: Match Pattern
//	    Mode: Each                       # or One; inferred from input if omitted
//	    Using: "{a:int}-{b:int}"
//
// Mode One consumes a single Text -> one Record/tuple.
// Mode Each consumes List<Text>   -> List<Record/tuple>.
var matchPattern = &Primitive{
	ID:      "Match Pattern",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Match") && hasWord(op, "Pattern") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		tmplStr, err := matchTemplateString(op, args, pos)
		if err != nil {
			return nil, err
		}
		tmpl, err := pattern.ParseTemplate(tmplStr)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Match Pattern: " + err.Error()}
		}
		re, err := tmpl.CompileRegex()
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Match Pattern: " + err.Error()}
		}

		each, err := matchMode(args, in, pos)
		if err != nil {
			return nil, err
		}

		elemType := tmpl.OutputType()
		if each {
			return &ir.Node{
				Prim:    "Match Pattern",
				In:      in,
				Out:     ir.List(elemType),
				Display: fmt.Sprintf("Match Pattern (Each) %q", tmplStr),
				Meta:    map[string]any{"template": tmplStr, "each": true},
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					lines, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Match Pattern", pos, "%v", err)
					}
					out := make([]ir.Value, len(lines))
					for i, line := range lines {
						rv, err := matchOne(re, tmpl, line, tmplStr, pos)
						if err != nil {
							return nil, err
						}
						out[i] = rv
					}
					return out, nil
				},
			}, nil
		}
		return &ir.Node{
			Prim:    "Match Pattern",
			In:      in,
			Out:     elemType,
			Display: fmt.Sprintf("Match Pattern (One) %q", tmplStr),
			Meta:    map[string]any{"template": tmplStr, "each": false},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				return matchOne(re, tmpl, v, tmplStr, pos)
			},
		}, nil
	},
}

// matchTemplateString gets the template from Using: "..." (preferred) or an
// inline string in the operation phrase.
func matchTemplateString(op *ast.Operation, args ArgSet, pos token.Position) (string, error) {
	if s, ok := args.Text("Using"); ok {
		return s, nil
	}
	if len(op.Strings) > 0 {
		return op.Strings[0], nil
	}
	return "", &ResolveError{Pos: pos,
		Msg: `Match Pattern requires a template, e.g. Using: "{a:int}-{b:int}"`}
}

// matchMode decides single vs each, honoring an explicit Mode: and otherwise
// inferring from the input type.
func matchMode(args ArgSet, in *ir.Type, pos token.Position) (each bool, err error) {
	textIn := in.Equal(ir.Text())
	listTextIn := in.Equal(ir.List(ir.Text()))

	if mode, ok := args.Ident("Mode"); ok {
		switch mode {
		case "Each":
			if !listTextIn {
				return false, &ResolveError{Pos: pos,
					Msg: fmt.Sprintf("Match Pattern Mode: Each expects List<Text> input, got %s", in)}
			}
			return true, nil
		case "One":
			if !textIn {
				return false, &ResolveError{Pos: pos,
					Msg: fmt.Sprintf("Match Pattern Mode: One expects Text input, got %s", in)}
			}
			return false, nil
		default:
			return false, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Match Pattern Mode must be One or Each, got %q", mode)}
		}
	}

	switch {
	case listTextIn:
		return true, nil
	case textIn:
		return false, nil
	default:
		return false, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Match Pattern expects Text or List<Text> input, got %s", in)}
	}
}

// matchOne matches one line and assembles the captured value.
func matchOne(re *regexp.Regexp, tmpl *pattern.Template, v ir.Value, tmplStr string, pos token.Position) (ir.Value, error) {
	s, ok := v.(string)
	if !ok {
		return nil, runtimeErr("Match Pattern", pos, "expected Text, got %s", ir.DescribeValue(v))
	}
	m := re.FindStringSubmatch(s)
	if m == nil {
		return nil, runtimeErr("Match Pattern", pos, "input %q does not match template %q", s, tmplStr)
	}
	caps := m[1:]

	if tmpl.Named {
		rec := ir.NewRecordValue()
		for i, h := range tmpl.Holes {
			cv, err := convertCapture(h.Type, caps[i], pos)
			if err != nil {
				return nil, err
			}
			rec.Set(h.Name, cv)
		}
		return rec, nil
	}

	out := make([]ir.Value, len(tmpl.Holes))
	for i, h := range tmpl.Holes {
		cv, err := convertCapture(h.Type, caps[i], pos)
		if err != nil {
			return nil, err
		}
		out[i] = cv
	}
	return out, nil
}

func convertCapture(ht pattern.HoleType, s string, pos token.Position) (ir.Value, error) {
	if ht == pattern.HoleInt {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, runtimeErr("Match Pattern", pos, "captured %q is not a valid integer", s)
		}
		return n, nil
	}
	return s, nil
}
