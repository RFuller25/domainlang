package prims

import (
	"fmt"
	"regexp"

	"domain/ast"
	"domain/ir"
	"domain/pattern"
	"domain/token"
)

// `Case:` — several templates on one stage, in priority order, all producing
// one type:
//
//	Cursed Technique: Match Pattern
//	    Mode: Each
//	    Case: on     "turn on {a:int},{b:int} through {c:int},{d:int}"
//	    Case: off    "turn off {a:int},{b:int} through {c:int},{d:int}"
//	    Case: toggle "toggle {a:int},{b:int} through {c:int},{d:int}"
//
// This is what alternation looks like in a language with no sum types. Regex
// alternation *inside* a template would let two branches capture different
// field sets, and the output type could then only be a union — so the
// alternation lives at the stage instead, where the rule "every case produces
// the same fields" is checkable at resolve time and the output stays one
// Record. What varies is recorded in a `kind` field naming the case that won.
//
// It is the ordered, one-pass answer to what Mode: Try can only do as several
// passes: Try over three verbs reads the file three times and concatenates the
// results by verb, losing the input's own order, which a simulation needs.

// caseTemplate is one parsed alternative.
type caseTemplate struct {
	tag  string
	src  string
	tmpl *pattern.Template
	re   *regexp.Regexp
}

// kindField is the field naming which case matched. It is the one thing every
// case has in common besides its fields.
const kindField = "kind"

func buildMatchCases(op *ast.Operation, args ArgSet, cases []ast.CaseArg, in *ir.Type, pos token.Position) (*ir.Node, error) {
	if args.Has("Using") || len(op.Strings) > 0 {
		return nil, &ResolveError{Pos: pos, Msg: "Match Pattern takes either one template or " +
			"a list of Case: alternatives, not both — delete the Using:, or fold it in as another Case:"}
	}
	parsed, elemType, err := parseCases(cases, pos)
	if err != nil {
		return nil, err
	}

	mode, err := matchMode(args, in, pos)
	if err != nil {
		return nil, err
	}
	if mode == matchModeScan {
		// Scan asks "where does this template occur inside the line", and with
		// several templates the occurrences would have to be interleaved by
		// position across regexes to stay in reading order. That is a real
		// feature rather than a missing line of code, so it is refused instead
		// of guessed at.
		return nil, &ResolveError{Pos: pos,
			Msg: "Match Pattern: Mode: Scan takes a single template, not Case: alternatives"}
	}

	meta := map[string]any{"cases": caseMeta(parsed), "each": mode != matchModeOne}
	if mode == matchModeTry {
		meta["try"] = true
	}
	out := elemType
	if mode != matchModeOne {
		out = ir.List(elemType)
	}
	return &ir.Node{
		Prim:    "Match Pattern",
		In:      in,
		Out:     out,
		Display: fmt.Sprintf("Match Pattern (%s) %d cases", mode, len(parsed)),
		Meta:    meta,
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			if mode == matchModeOne {
				return matchCaseOne(parsed, v, pos)
			}
			lines, err := ir.AsList(v)
			if err != nil {
				return nil, runtimeErr("Match Pattern", pos, "%v", err)
			}
			res := make([]ir.Value, 0, len(lines))
			for _, line := range lines {
				rv, err := matchCaseOne(parsed, line, pos)
				if err != nil {
					if mode == matchModeTry && isMatchMiss(err) {
						continue
					}
					return nil, err
				}
				res = append(res, rv)
			}
			return res, nil
		},
	}, nil
}

// parseCases parses every alternative and checks the one rule that keeps the
// output a single type: they must all produce the same fields.
func parseCases(cases []ast.CaseArg, pos token.Position) ([]caseTemplate, *ir.Type, error) {
	seen := map[string]bool{}
	parsed := make([]caseTemplate, 0, len(cases))
	var want *ir.Type
	for _, c := range cases {
		if seen[c.Tag] {
			return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Match Pattern names the case %q twice; each tag has to say which template matched", c.Tag)}
		}
		seen[c.Tag] = true

		tmpl, err := pattern.ParseTemplate(c.Template)
		if err != nil {
			return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Match Pattern case %q: %v", c.Tag, err)}
		}
		if !tmpl.Named {
			// A positional case would produce a tuple, and the kind field has
			// nowhere to go in one — a tuple's slots are positions, so adding a
			// name to it is a different shape rather than one more field.
			return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Match Pattern case %q uses positional holes; Case: alternatives need named holes, "+
					"so the %s field has somewhere to live", c.Tag, kindField)}
		}
		for _, h := range tmpl.Holes {
			if h.Name == kindField {
				return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Match Pattern case %q has a hole named %q, which is the field naming which case "+
						"matched — rename the hole", c.Tag, kindField)}
			}
		}
		got := tmpl.OutputType()
		if want == nil {
			want = got
		} else if !got.Equal(want) {
			return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Match Pattern case %q produces %s, but an earlier case produces %s — every case has to "+
					"produce the same fields, or the stage would have no one output type", c.Tag, got, want)}
		}
		re, err := tmpl.CompileRegex()
		if err != nil {
			return nil, nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Match Pattern case %q: %v", c.Tag, err)}
		}
		parsed = append(parsed, caseTemplate{tag: c.Tag, src: c.Template, tmpl: tmpl, re: re})
	}

	// kind first: a reader wants to know which shape the line was before its
	// fields mean anything.
	fields := append([]ir.Field{{Name: kindField, Type: ir.Text()}}, want.Fields...)
	return parsed, ir.Record(fields...), nil
}

// caseMeta is the compiler's view of the alternatives: tag and template
// source, in priority order.
func caseMeta(parsed []caseTemplate) [][2]string {
	out := make([][2]string, len(parsed))
	for i, c := range parsed {
		out[i] = [2]string{c.tag, c.src}
	}
	return out
}

// matchCaseOne tries each alternative in order and assembles the first hit.
// Order is the program's: two templates that could both match a line resolve
// to whichever the program wrote first.
func matchCaseOne(parsed []caseTemplate, v ir.Value, pos token.Position) (ir.Value, error) {
	s, ok := v.(string)
	if !ok {
		return nil, runtimeErr("Match Pattern", pos, "expected Text, got %s", ir.DescribeValue(v))
	}
	for _, c := range parsed {
		m := c.re.FindStringSubmatchIndex(s)
		if m == nil {
			continue
		}
		vals, err := holeValues(c.tmpl, m, s, pos)
		if err != nil {
			return nil, err
		}
		rec := ir.NewRecordValue()
		rec.Set(kindField, c.tag)
		for i, h := range c.tmpl.Holes {
			rec.Set(h.Name, vals[i])
		}
		return rec, nil
	}
	return nil, matchMiss{runtimeErr("Match Pattern", pos,
		"input %q matches none of the %d Case: templates", s, len(parsed))}
}
