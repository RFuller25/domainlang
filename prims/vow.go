package prims

import (
	"fmt"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Binding Vow: a debug-time assertion over the current pipeline value. A vow
// never changes the value it passes through; on violation it throws, reporting
// the vow, the stage, and the actual offending value.
//
// v0.1 supports two inline predicate forms over the current value:
//
//	Binding Vow: Count Equals N        # len(list) == N
//	Binding Vow: All Values > N        # every element compares true (>,>=,<,<=,=)
var bindingVow = &Primitive{
	ID:      "Binding Vow",
	Keyword: "Binding Vow",
	Match:   func(op *ast.Operation) bool { return true },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Binding Vow has no value to assert over"}
		}
		check, display, meta, err := buildVowCheck(op, pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Binding Vow",
			In:      in,
			Out:     in, // passthrough: a vow never changes the value
			Display: "Binding Vow: " + display,
			Meta:    meta,
			Pos:     pos,
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				if ctx != nil && ctx.Release {
					return v, nil // release mode: the vow is shed
				}
				if err := check(v); err != nil {
					return nil, &ir.RuntimeError{
						Prim: "Binding Vow",
						Pos:  pos,
						Msg: fmt.Sprintf("vow violated [%s]: %v; actual value: %s",
							op.Raw, err, ir.FormatShort(v)),
					}
				}
				return v, nil
			},
		}, nil
	},
}

type vowCheck func(v ir.Value) error

func buildVowCheck(op *ast.Operation, pos token.Position) (vowCheck, string, map[string]any, error) {
	switch {
	case hasWord(op, "Count") && (hasWord(op, "Equals") || hasSym(op, "=")):
		if len(op.Ints) == 0 {
			return nil, "", nil, &ResolveError{Pos: pos, Msg: "Count vow requires a number, e.g. Count Equals 200"}
		}
		want := op.Ints[0]
		return func(v ir.Value) error {
			l, err := ir.AsList(v)
			if err != nil {
				return err
			}
			if int64(len(l)) != want {
				return fmt.Errorf("expected count %d, got %d", want, len(l))
			}
			return nil
		}, fmt.Sprintf("Count Equals %d", want), map[string]any{"kind": "count", "want": want, "raw": op.Raw}, nil

	case hasWord(op, "All") && hasWord(op, "Values"):
		if len(op.OpSyms) == 0 || len(op.Ints) == 0 {
			return nil, "", nil, &ResolveError{Pos: pos,
				Msg: "All Values vow requires a comparison, e.g. All Values > 0"}
		}
		sym := op.OpSyms[0]
		bound := op.Ints[0]
		cmp, err := compareFunc(sym)
		if err != nil {
			return nil, "", nil, &ResolveError{Pos: pos, Msg: err.Error()}
		}
		return func(v ir.Value) error {
			xs, err := ir.AsIntSlice(v)
			if err != nil {
				return err
			}
			for i, x := range xs {
				if !cmp(x, bound) {
					return fmt.Errorf("element %d (%d) violates value %s %d", i, x, sym, bound)
				}
			}
			return nil
		}, fmt.Sprintf("All Values %s %d", sym, bound), map[string]any{"kind": "allvalues", "sym": sym, "bound": bound, "raw": op.Raw}, nil

	default:
		return nil, "", nil, &ResolveError{Pos: pos,
			Msg: fmt.Sprintf("unsupported Binding Vow %q (v0.1 supports 'Count Equals N' and 'All Values <cmp> N')", op.Raw)}
	}
}

func hasSym(op *ast.Operation, s string) bool {
	for _, x := range op.OpSyms {
		if x == s {
			return true
		}
	}
	return false
}

func compareFunc(sym string) (func(a, b int64) bool, error) {
	switch sym {
	case ">":
		return func(a, b int64) bool { return a > b }, nil
	case ">=":
		return func(a, b int64) bool { return a >= b }, nil
	case "<":
		return func(a, b int64) bool { return a < b }, nil
	case "<=":
		return func(a, b int64) bool { return a <= b }, nil
	case "=":
		return func(a, b int64) bool { return a == b }, nil
	default:
		return nil, fmt.Errorf("unsupported comparison %q in vow", sym)
	}
}
