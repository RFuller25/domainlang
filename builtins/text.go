package builtins

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"domain/ir"
)

// The text builtins. Each one is a name, a count, a typing rule and an
// evaluator; the constructors below write the ones that share a shape, so the
// declaration is the whole of what makes the builtin different.

func init() {
	register(
		// Text -> Text
		unary("trim", ir.Text(), true, func(s string) ir.Value { return strings.TrimSpace(s) }),
		unary("upper", ir.Text(), true, func(s string) ir.Value { return strings.ToUpper(s) }),
		unary("lower", ir.Text(), true, func(s string) ir.Value { return strings.ToLower(s) }),
		// Text -> List<Text>
		unary("chars", ir.List(ir.Text()), true, func(s string) ir.Value {
			rs := []rune(s)
			out := make([]ir.Value, len(rs))
			for i, r := range rs {
				out[i] = string(r)
			}
			return out
		}),
		unary("words", ir.List(ir.Text()), false, func(s string) ir.Value { return textList(strings.Fields(s)) }),
		// Text -> Bool
		unary("repeats", ir.Bool(), true, func(s string) ir.Value { return isRepeatedPattern(s) }),
		unary("isdigit", ir.Bool(), false, func(s string) ir.Value { return classify("isdigit", s) }),
		unary("isalpha", ir.Bool(), false, func(s string) ir.Value { return classify("isalpha", s) }),
		unary("isupper", ir.Bool(), false, func(s string) ir.Value { return classify("isupper", s) }),
		unary("islower", ir.Bool(), false, func(s string) ir.Value { return classify("islower", s) }),

		// Text × Text -> …
		binary("startswith", ir.Bool(), func(s, p string) ir.Value { return strings.HasPrefix(s, p) }),
		binary("endswith", ir.Bool(), func(s, p string) ir.Value { return strings.HasSuffix(s, p) }),
		binary("occurrences", ir.Int(), func(s, sub string) ir.Value { return int64(strings.Count(s, sub)) }),
		binary("split", ir.List(ir.Text()), func(s, sep string) ir.Value { return textList(strings.Split(s, sep)) }),
		binary("trimprefix", ir.Text(), func(s, p string) ir.Value { return strings.TrimPrefix(s, p) }),
		binary("trimsuffix", ir.Text(), func(s, p string) ir.Value { return strings.TrimSuffix(s, p) }),

		Spec{Name: "replace", Arity: 3, Type: allText("replace", 3, ir.Text()), Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			s, old, nw, ok := threeTexts(a)
			if !ok {
				return nil, fmt.Errorf("replace: expected Text arguments")
			}
			return strings.ReplaceAll(s, old, nw), nil
		}},

		Spec{Name: "toint", Arity: 1,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if !a[0].Equal(ir.Text()) {
					return nil, fmt.Errorf("toint needs Text, got %s", a[0])
				}
				return ir.Int(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				s, ok := a[0].(string)
				if !ok {
					return nil, fmt.Errorf("toint: expected Text, got %s", ir.DescribeValue(a[0]))
				}
				n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("toint: %q is not an integer", s)
				}
				return n, nil
			}},

		Spec{Name: "totext", Arity: 1,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if a[0] == nil || (a[0].Kind != ir.KInt && a[0].Kind != ir.KFloat) {
					return nil, fmt.Errorf("totext needs Int or Float, got %s", a[0])
				}
				return ir.Text(), nil
			},
			// Renders exactly as Reveal would: FormatInt for Int, the shortest
			// round-trip 'g' form for Float.
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				switch n := a[0].(type) {
				case int64:
					return strconv.FormatInt(n, 10), nil
				case float64:
					return ir.FormatFloat(n), nil
				}
				return nil, fmt.Errorf("totext: expected a number, got %s", ir.DescribeValue(a[0]))
			}},

		Spec{Name: "ord", Arity: 1, Type: textThen("ord", ir.Int()),
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				s, ok := a[0].(string)
				if !ok {
					return nil, fmt.Errorf("ord: expected Text, got %s", ir.DescribeValue(a[0]))
				}
				if s == "" {
					return nil, fmt.Errorf("ord of the empty text is undefined")
				}
				r, _ := utf8.DecodeRuneInString(s)
				return int64(r), nil
			}},

		Spec{Name: "chr", Arity: 1,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if err := needInt("chr", a, 0); err != nil {
					return nil, err
				}
				return ir.Text(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				n, err := ir.AsInt(a[0])
				if err != nil {
					return nil, fmt.Errorf("chr: %v", err)
				}
				if n < 0 || n > utf8.MaxRune || (n >= 0xD800 && n <= 0xDFFF) {
					return nil, fmt.Errorf("chr: %d is not a character code", n)
				}
				return string(rune(n)), nil
			}},

		Spec{Name: "charat", Arity: 2,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if !a[0].Equal(ir.Text()) {
					return nil, fmt.Errorf("charat needs Text, got %s", a[0])
				}
				if err := needInt("charat", a, 1); err != nil {
					return nil, err
				}
				return ir.Text(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				s, ok := a[0].(string)
				if !ok {
					return nil, fmt.Errorf("charat: expected Text, got %s", ir.DescribeValue(a[0]))
				}
				i, err := ir.AsInt(a[1])
				if err != nil {
					return nil, fmt.Errorf("charat: %v", err)
				}
				rs := []rune(s)
				if i < 0 || i >= int64(len(rs)) {
					return nil, fmt.Errorf("charat: index %d out of range (length %d)", i, len(rs))
				}
				return string(rs[i]), nil
			}},

		Spec{Name: "withchar", Arity: 3,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if !a[0].Equal(ir.Text()) {
					return nil, fmt.Errorf("withchar needs Text, got %s", a[0])
				}
				if err := needInt("withchar", a, 1); err != nil {
					return nil, err
				}
				if !a[2].Equal(ir.Text()) {
					return nil, fmt.Errorf("withchar replacement must be Text, got %s", a[2])
				}
				return ir.Text(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				s, ok := a[0].(string)
				if !ok {
					return nil, fmt.Errorf("withchar: expected Text, got %s", ir.DescribeValue(a[0]))
				}
				i, err := ir.AsInt(a[1])
				if err != nil {
					return nil, fmt.Errorf("withchar: %v", err)
				}
				ch, ok := a[2].(string)
				if !ok {
					return nil, fmt.Errorf("withchar: expected Text, got %s", ir.DescribeValue(a[2]))
				}
				rs := []rune(s)
				if i < 0 || i >= int64(len(rs)) {
					return nil, fmt.Errorf("withchar: index %d out of range (length %d)", i, len(rs))
				}
				crs := []rune(ch)
				if len(crs) != 1 {
					return nil, fmt.Errorf("withchar: replacement must be exactly one character, got %d", len(crs))
				}
				rs[i] = crs[0]
				return string(rs), nil
			}},

		Spec{Name: "repeat", Arity: 2,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if err := needText("repeat", a, 0); err != nil {
					return nil, err
				}
				if err := needInt("repeat", a, 1); err != nil {
					return nil, err
				}
				return ir.Text(), nil
			},
			Eval: func(env Env, a []ir.Value) (ir.Value, error) {
				s, ok := a[0].(string)
				if !ok {
					return nil, fmt.Errorf("repeat: expected Text, got %s", ir.DescribeValue(a[0]))
				}
				n, err := ir.AsInt(a[1])
				if err != nil {
					return nil, fmt.Errorf("repeat: %v", err)
				}
				if n <= 0 || s == "" {
					return "", nil
				}
				if n > env.BuildLimit()/int64(len(s)) {
					return nil, fmt.Errorf("repeat: %d copies of a %d-byte text is more than can be built", n, len(s))
				}
				return strings.Repeat(s, int(n)), nil
			}},

		padding("padleft", true),
		padding("padright", false),
	)
}

// padding declares padleft/padright, which differ only in the side.
func padding(name string, left bool) Spec {
	return Spec{Name: name, Arity: 3,
		Type: func(a []*ir.Type) (*ir.Type, error) {
			if err := needText(name, a, 0); err != nil {
				return nil, err
			}
			if err := needInt(name, a, 1); err != nil {
				return nil, err
			}
			if err := needText(name, a, 2); err != nil {
				return nil, err
			}
			return ir.Text(), nil
		},
		Eval: func(env Env, a []ir.Value) (ir.Value, error) {
			s, ok := a[0].(string)
			if !ok {
				return nil, fmt.Errorf("%s: expected Text, got %s", name, ir.DescribeValue(a[0]))
			}
			width, err := ir.AsInt(a[1])
			if err != nil {
				return nil, fmt.Errorf("%s: %v", name, err)
			}
			pad, ok := a[2].(string)
			if !ok {
				return nil, fmt.Errorf("%s: pad must be Text, got %s", name, ir.DescribeValue(a[2]))
			}
			out, err := padText(env, s, width, pad, left)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", name, err)
			}
			return out, nil
		}}
}

// unary declares a one-Text-argument builtin. needs picks the wording of the
// type error — "trim needs Text" or "words argument 1 must be Text" — which the
// two generations of builtins spelled differently and tests still pin.
func unary(name string, result *ir.Type, needs bool, f func(string) ir.Value) Spec {
	return Spec{Name: name, Arity: 1,
		Type: func(a []*ir.Type) (*ir.Type, error) {
			if needs {
				if !a[0].Equal(ir.Text()) {
					return nil, fmt.Errorf("%s needs Text, got %s", name, a[0])
				}
				return result, nil
			}
			return result, needText(name, a, 0)
		},
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			s, ok := a[0].(string)
			if !ok {
				return nil, fmt.Errorf("%s: expected Text, got %s", name, ir.DescribeValue(a[0]))
			}
			return f(s), nil
		}}
}

// binary declares a builtin of two Text arguments.
func binary(name string, result *ir.Type, f func(a, b string) ir.Value) Spec {
	return Spec{Name: name, Arity: 2, Type: allText(name, 2, result),
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			x, ok1 := a[0].(string)
			y, ok2 := a[1].(string)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("%s: expected Text arguments", name)
			}
			return f(x, y), nil
		}}
}

// allText types a builtin whose n arguments are all Text.
func allText(name string, n int, result *ir.Type) func([]*ir.Type) (*ir.Type, error) {
	return func(a []*ir.Type) (*ir.Type, error) {
		for i := range n {
			if err := needText(name, a, i); err != nil {
				return nil, err
			}
		}
		return result, nil
	}
}

// textThen types a builtin of one Text argument with the argument-N wording.
func textThen(name string, result *ir.Type) func([]*ir.Type) (*ir.Type, error) {
	return allText(name, 1, result)
}

func needText(name string, a []*ir.Type, i int) error {
	if !a[i].Equal(ir.Text()) {
		return fmt.Errorf("%s argument %d must be Text, got %s", name, i+1, a[i])
	}
	return nil
}

func needInt(name string, a []*ir.Type, i int) error {
	if !a[i].Equal(ir.Int()) {
		return fmt.Errorf("%s argument %d must be Int, got %s", name, i+1, a[i])
	}
	return nil
}

func threeTexts(a []ir.Value) (x, y, z string, ok bool) {
	x, ok1 := a[0].(string)
	y, ok2 := a[1].(string)
	z, ok3 := a[2].(string)
	return x, y, z, ok1 && ok2 && ok3
}

// textList boxes a []string as a Domain List<Text> in one allocation.
func textList(ss []string) []ir.Value {
	out := make([]ir.Value, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
