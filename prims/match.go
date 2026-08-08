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
		if cases := args.Cases("Case"); len(cases) > 0 {
			return buildMatchCases(op, args, cases, in, pos)
		}
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

		mode, err := matchMode(args, in, pos)
		if err != nil {
			return nil, err
		}

		elemType := tmpl.OutputType()
		if mode == matchModeScan {
			// Scan reads the template as a *description of a fragment* rather
			// than of a whole line: it takes every occurrence inside each line
			// and concatenates them, so a line contributes as many values as it
			// contains and a line with none contributes nothing. That is the
			// answer to input the template does not describe exhaustively —
			// `mul(2,4)` scattered through noise — which Extract Integers can
			// only serve by throwing the structure away.
			scan, err := tmpl.CompileScan()
			if err != nil {
				return nil, &ResolveError{Pos: pos, Msg: "Match Pattern: " + err.Error()}
			}
			return &ir.Node{
				Prim:    "Match Pattern",
				In:      in,
				Out:     ir.List(elemType),
				Display: fmt.Sprintf("Match Pattern (Scan) %q", tmplStr),
				Meta:    map[string]any{"template": tmplStr, "each": true, "scan": true},
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					lines, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Match Pattern", pos, "%v", err)
					}
					var out []ir.Value
					for _, line := range lines {
						vs, err := scanOne(scan, tmpl, line, pos)
						if err != nil {
							return nil, err
						}
						out = append(out, vs...)
					}
					if out == nil {
						out = []ir.Value{}
					}
					return out, nil
				},
			}, nil
		}
		if mode != matchModeOne {
			try := mode == matchModeTry
			return &ir.Node{
				Prim:    "Match Pattern",
				In:      in,
				Out:     ir.List(elemType),
				Display: fmt.Sprintf("Match Pattern (%s) %q", mode, tmplStr),
				Meta:    map[string]any{"template": tmplStr, "each": true, "try": try},
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					lines, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Match Pattern", pos, "%v", err)
					}
					out := make([]ir.Value, 0, len(lines))
					for _, line := range lines {
						rv, err := matchOne(re, tmpl, line, tmplStr, pos)
						if err != nil {
							// Mode: Try keeps the lines that fit and drops the
							// rest, which is what makes a file of two shapes
							// parseable at all — one pass per shape. A
							// conversion failure is not a shape mismatch, so
							// it still stops the program.
							if try && isMatchMiss(err) {
								continue
							}
							return nil, err
						}
						out = append(out, rv)
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

// matchModeKind is how a template applies to the current value.
type matchModeKind string

const (
	matchModeOne  matchModeKind = "One"  // Text -> one value
	matchModeEach matchModeKind = "Each" // List<Text> -> List, every line must match
	matchModeTry  matchModeKind = "Try"  // List<Text> -> List of the lines that did
	matchModeScan matchModeKind = "Scan" // every occurrence *inside* each line
)

func (m matchModeKind) String() string { return string(m) }

// matchMode decides which of the four, honoring an explicit Mode: and
// otherwise inferring One or Each from the input type. Try and Scan are never
// inferred: each *drops* input a template did not describe, which is something
// a program has to ask for, or a typo in a template would quietly parse
// nothing instead of failing.
func matchMode(args ArgSet, in *ir.Type, pos token.Position) (matchModeKind, error) {
	textIn := in.Equal(ir.Text())
	listTextIn := in.Equal(ir.List(ir.Text()))

	if mode, ok := args.Ident("Mode"); ok {
		switch matchModeKind(mode) {
		case matchModeEach, matchModeTry, matchModeScan:
			if !listTextIn {
				return "", &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Match Pattern Mode: %s expects List<Text> input, got %s", mode, in)}
			}
			return matchModeKind(mode), nil
		case matchModeOne:
			if !textIn {
				return "", &ResolveError{Pos: pos,
					Msg: fmt.Sprintf("Match Pattern Mode: One expects Text input, got %s", in)}
			}
			return matchModeOne, nil
		default:
			return "", &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Match Pattern Mode must be One, Each, Try or Scan, got %q", mode)}
		}
	}

	switch {
	case listTextIn:
		return matchModeEach, nil
	case textIn:
		return matchModeOne, nil
	default:
		return "", &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("Match Pattern expects Text or List<Text> input, got %s", in)}
	}
}

// matchOne matches one line and assembles the captured value.
//
// The match is read by *index* rather than through FindStringSubmatch, because
// an optional group that did not participate has to be told from one that
// matched the empty string, and the text form reports both as "".
func matchOne(re *regexp.Regexp, tmpl *pattern.Template, v ir.Value, tmplStr string, pos token.Position) (ir.Value, error) {
	s, ok := v.(string)
	if !ok {
		return nil, runtimeErr("Match Pattern", pos, "expected Text, got %s", ir.DescribeValue(v))
	}
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return nil, matchMiss{runtimeErr("Match Pattern", pos,
			"input %q does not match template %q", s, tmplStr)}
	}
	vals, err := holeValues(tmpl, m, s, pos)
	if err != nil {
		return nil, err
	}

	if tmpl.Named {
		rec := ir.NewRecordValue()
		for i, h := range tmpl.Holes {
			rec.Set(h.Name, vals[i])
		}
		return rec, nil
	}
	return vals, nil
}

// scanOne finds every occurrence of the template inside one line.
//
// A non-match is not an error here, unlike every other mode: `Mode: Scan` is
// asked for precisely when the line is *not* expected to be all template, so
// "none in this line" is an answer rather than a failure. A capture that then
// fails to convert still stops the program, the same rule Try follows.
func scanOne(re *regexp.Regexp, tmpl *pattern.Template, v ir.Value, pos token.Position) ([]ir.Value, error) {
	s, ok := v.(string)
	if !ok {
		return nil, runtimeErr("Match Pattern", pos, "expected Text, got %s", ir.DescribeValue(v))
	}
	var out []ir.Value
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		vals, err := holeValues(tmpl, m, s, pos)
		if err != nil {
			return nil, err
		}
		if !tmpl.Named {
			out = append(out, vals)
			continue
		}
		rec := ir.NewRecordValue()
		for i, h := range tmpl.Holes {
			rec.Set(h.Name, vals[i])
		}
		out = append(out, rec)
	}
	return out, nil
}

// holeValues reads every hole of the template out of one match, in the order
// tmpl.Holes declares — which is the order the output's fields or tuple slots
// are in, an optional group's holes included.
//
// Each value starts at its hole's zero and is overwritten by whatever capture
// claims its slot. A hole whose optional group stood down is never claimed, so
// its zero is the answer; that is the whole of "absent" in a language with no
// sum types, and {?flag} exists for when the zero and a real zero have to be
// told apart.
func holeValues(tmpl *pattern.Template, m []int, s string, pos token.Position) ([]ir.Value, error) {
	out := make([]ir.Value, len(tmpl.Holes))
	for i, h := range tmpl.Holes {
		out[i] = h.Zero()
	}
	for _, c := range tmpl.Captures() {
		present := m[2*c.Group] >= 0
		switch c.Kind {
		case pattern.CapOpt:
			if c.Slot >= 0 {
				out[c.Slot] = present
			}
		case pattern.CapHole:
			if !present {
				continue
			}
			cv, err := convertHole(*c.Hole, s[m[2*c.Group]:m[2*c.Group+1]], pos)
			if err != nil {
				return nil, err
			}
			out[c.Slot] = cv
		}
	}
	return out, nil
}

// convertHole turns one capture into its value: a scalar, the list a repeated
// hole captures, or the list of records a repeated group does. The regex
// matched the whole run as one group — a Go regexp keeps only the last match of
// a repeated group — so the split happens here.
func convertHole(h pattern.Hole, capture string, pos token.Position) (ir.Value, error) {
	if h.Group != nil {
		return convertGroup(h, capture, pos)
	}
	if !h.Rep {
		return convertCapture(h.Type, capture, pos)
	}
	parts := h.Split(capture)
	out := make([]ir.Value, len(parts))
	for i, p := range parts {
		cv, err := convertCapture(h.Type, p, pos)
		if err != nil {
			return nil, err
		}
		out[i] = cv
	}
	return out, nil
}

// convertGroup splits a repeated group's run and re-matches each element
// against the inner template — the same machinery one level down, which is why
// the inner template is a plain *Template rather than a second kind of thing.
func convertGroup(h pattern.Hole, capture string, pos token.Position) (ir.Value, error) {
	re, err := h.Group.CompileRegex()
	if err != nil {
		return nil, runtimeErr("Match Pattern", pos, "group template: %v", err)
	}
	parts := h.Split(capture)
	out := make([]ir.Value, len(parts))
	for i, p := range parts {
		ev, err := matchOne(re, h.Group, p, h.Group.Raw, pos)
		if err != nil {
			return nil, err
		}
		out[i] = ev
	}
	return out, nil
}

func convertCapture(ht pattern.HoleType, s string, pos token.Position) (ir.Value, error) {
	base, ok := pattern.CaptureBase(ht)
	if !ok {
		return s, nil
	}
	n, err := strconv.ParseInt(s, base, 64)
	if err != nil {
		return nil, runtimeErr("Match Pattern", pos, "captured %q is not a valid integer", s)
	}
	return n, nil
}

// matchMiss marks the one failure Mode: Try may swallow — a line that does not
// fit the template's *shape*. A capture that fits the shape and then fails to
// convert (an integer out of int64 range) is a broken line rather than a
// different kind of line, so it still stops the program in every mode.
type matchMiss struct{ error }

func isMatchMiss(err error) bool {
	_, ok := err.(matchMiss)
	return ok
}
