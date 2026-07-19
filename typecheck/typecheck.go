// Package typecheck statically computes the result type of an expression-layer
// expression given the types of its free identifiers. It is the foundation for
// "type inference through Using: lambdas": a lambda-consuming primitive binds
// its parameters to known types and asks for the body's result type, which then
// becomes (part of) the primitive's output type.
//
// Per the v0.2 decision, this is best-effort: a primitive whose output type
// cannot be inferred this way is expected to carry an explicit annotation. The
// expression layer here mirrors what the evaluator in package interp supports.
package typecheck

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Env maps expression-layer identifiers (lambda parameters) to their types.
type Env map[string]*ir.Type

// ExprType computes the static type of e under env.
func ExprType(e ast.Expr, env Env) (*ir.Type, error) {
	switch x := e.(type) {
	case *ast.IntLit:
		return ir.Int(), nil
	case *ast.FloatLit:
		return ir.Float(), nil
	case *ast.BoolLit:
		return ir.Bool(), nil
	case *ast.StringLit:
		return ir.Text(), nil
	case *ast.Ident:
		t, ok := env[x.Name]
		if !ok {
			return nil, fmt.Errorf("%s: unknown identifier %q", x.Pos, x.Name)
		}
		return t, nil
	case *ast.UnaryExpr:
		return unaryType(x, env)
	case *ast.BinaryExpr:
		return binaryType(x, env)
	case *ast.FieldAccess:
		return fieldType(x, env)
	case *ast.CallExpr:
		return callType(x, env)
	case *ast.CondExpr:
		return condType(x, env)
	default:
		return nil, fmt.Errorf("unsupported expression %T", e)
	}
}

// condType types `if c then a else b`: the condition must be Bool and the
// arms must agree; the result is the arms' shared type.
func condType(x *ast.CondExpr, env Env) (*ir.Type, error) {
	ct, err := ExprType(x.Cond, env)
	if err != nil {
		return nil, err
	}
	if !ct.Equal(ir.Bool()) {
		return nil, fmt.Errorf("%s: if condition must be Bool, got %s", x.Pos, ct)
	}
	tt, err := ExprType(x.Then, env)
	if err != nil {
		return nil, err
	}
	et, err := ExprType(x.Else, env)
	if err != nil {
		return nil, err
	}
	if !tt.Equal(et) {
		return nil, fmt.Errorf("%s: if arms must have the same type, got %s and %s", x.Pos, tt, et)
	}
	return tt, nil
}

// Builtins is the fixed expression-layer function table.
// Every entry is implemented in all three layers — here (typing), eval
// (interpreter), and codegen (compiled) — with oracle tests pinning
// interpreter/binary parity. Keep the layers in sync when extending the
// table.
var Builtins = []string{
	"abs", "at", "band", "bor", "bxor", "ceil", "cells", "col", "cols",
	"concat", "contains", "dirs4", "drop", "first", "floor", "frombin",
	"gcd", "get", "has", "inbounds", "item", "last", "lcm", "length", "list",
	"manhattan", "max", "maxcol", "maxrow", "min", "mincol", "minrow",
	"modinv", "modpow", "neighbors4", "neighbors8", "occurrences", "padd",
	"pcol", "point", "prow", "put", "repeats", "reverse", "rotl", "rotr",
	"round", "row", "rows", "set", "shl", "shr", "sign", "solve2x2",
	"sparse", "sqrt", "sum", "take", "tofloat", "toint", "totext",
}

// PointType is the expression-layer representation of a 2D point: an
// (Int, Int) tuple of (row, col), matching the grid coordinate system.
func PointType() *ir.Type { return ir.Tuple(ir.Int(), ir.Int()) }

// callType types a builtin call. Several builtins are polymorphic in the
// list element type, so the rules pattern-match on the argument types.
func callType(x *ast.CallExpr, env Env) (*ir.Type, error) {
	id, ok := x.Fn.(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("%s: only builtin functions can be called", x.Pos)
	}
	name := id.Name

	arity := map[string]int{
		"length": 1, "item": 2, "take": 2, "drop": 2, "reverse": 1,
		"concat": 2, "first": 1, "last": 1, "sum": 1, "min": 1, "max": 1,
		"contains": 2, "get": 2, "at": 3,
		// math / number theory
		"abs": 1, "sign": 1, "gcd": 2, "lcm": 2, "modpow": 3, "modinv": 2,
		"solve2x2": 6,
		// text
		"toint": 1, "occurrences": 2, "repeats": 1, "totext": 1,
		// floats (H)
		"tofloat": 1, "floor": 1, "ceil": 1, "round": 1, "sqrt": 1,
		// points (tuples of (row, col)) and grid geometry
		"point": 2, "prow": 1, "pcol": 1, "padd": 2, "manhattan": 2,
		"rotl": 1, "rotr": 1, "dirs4": 0,
		"inbounds": 3, "neighbors4": 3, "neighbors8": 3,
		// list/grid construction and access (A.2)
		"set": 3, "row": 2, "col": 2, "rows": 1, "cols": 1,
		"list": -1, // variadic, >= 1
		// sparse grids (H)
		"sparse": 1, "put": 4, "has": 3, "cells": 1,
		"minrow": 1, "maxrow": 1, "mincol": 1, "maxcol": 1,
		// bit operations (2021 D3 and friends)
		"band": 2, "bor": 2, "bxor": 2, "shl": 2, "shr": 2, "frombin": 1,
	}
	want, known := arity[name]
	if !known {
		return nil, fmt.Errorf("%s: unknown function %q (builtins: %s)",
			x.Pos, name, strings.Join(Builtins, ", "))
	}
	if want == -1 {
		if len(x.Args) < 1 {
			return nil, fmt.Errorf("%s: %s takes at least 1 argument, got 0", x.Pos, name)
		}
	} else if len(x.Args) != want {
		return nil, fmt.Errorf("%s: %s takes %d argument(s), got %d", x.Pos, name, want, len(x.Args))
	}
	args := make([]*ir.Type, len(x.Args))
	for i, a := range x.Args {
		t, err := ExprType(a, env)
		if err != nil {
			return nil, err
		}
		args[i] = t
	}

	needList := func(i int) (*ir.Type, error) {
		if args[i] == nil || args[i].Kind != ir.KList {
			return nil, fmt.Errorf("%s: %s needs a List argument, got %s", x.Pos, name, args[i])
		}
		return args[i].Elem, nil
	}
	needInt := func(i int) error {
		if !args[i].Equal(ir.Int()) {
			return fmt.Errorf("%s: %s argument %d must be Int, got %s", x.Pos, name, i+1, args[i])
		}
		return nil
	}

	switch name {
	case "length":
		if _, err := needList(0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "item":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return elem, nil
	case "take", "drop":
		if _, err := needList(0); err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return args[0], nil
	case "reverse":
		if _, err := needList(0); err != nil {
			return nil, err
		}
		return args[0], nil
	case "concat":
		if _, err := needList(0); err != nil {
			return nil, err
		}
		if !args[1].Equal(args[0]) {
			return nil, fmt.Errorf("%s: concat needs two lists of the same type, got %s and %s",
				x.Pos, args[0], args[1])
		}
		return args[0], nil
	case "first", "last":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		return elem, nil
	case "sum", "min", "max":
		if args[0] == nil || args[0].Kind != ir.KList || !numeric(args[0].Elem) {
			return nil, fmt.Errorf("%s: %s needs List<Int> or List<Float>, got %s", x.Pos, name, args[0])
		}
		return args[0].Elem, nil
	case "contains":
		if args[0] == nil || (args[0].Kind != ir.KList && args[0].Kind != ir.KSet) {
			return nil, fmt.Errorf("%s: contains needs a List or Set argument, got %s", x.Pos, args[0])
		}
		elem := args[0].Elem
		if !ir.Keyable(elem) {
			return nil, fmt.Errorf("%s: contains needs keyable elements (Int, Text, or Tuples/Records of them), got %s", x.Pos, elem)
		}
		if !args[1].Equal(elem) {
			return nil, fmt.Errorf("%s: contains value must be %s, got %s", x.Pos, elem, args[1])
		}
		return ir.Bool(), nil
	case "get":
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: get needs a Map argument, got %s", x.Pos, args[0])
		}
		if !args[1].Equal(args[0].Key) {
			return nil, fmt.Errorf("%s: get key must be %s, got %s", x.Pos, args[0].Key, args[1])
		}
		return args[0].Elem, nil
	case "at":
		if args[0] == nil || (args[0].Kind != ir.KGrid && args[0].Kind != ir.KSparse) {
			return nil, fmt.Errorf("%s: at needs a Grid or Sparse argument, got %s", x.Pos, args[0])
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		if err := needInt(2); err != nil {
			return nil, err
		}
		return args[0].Elem, nil

	// -- sparse grids (H) --------------------------------------------------------
	case "sparse":
		return ir.Sparse(args[0]), nil
	case "put":
		if args[0] == nil || args[0].Kind != ir.KSparse {
			return nil, fmt.Errorf("%s: put needs a Sparse argument, got %s", x.Pos, args[0])
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		if err := needInt(2); err != nil {
			return nil, err
		}
		if !args[3].Equal(args[0].Elem) {
			return nil, fmt.Errorf("%s: put value must be %s, got %s", x.Pos, args[0].Elem, args[3])
		}
		return args[0], nil
	case "has":
		if args[0] == nil || args[0].Kind != ir.KSparse {
			return nil, fmt.Errorf("%s: has needs a Sparse argument, got %s", x.Pos, args[0])
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		if err := needInt(2); err != nil {
			return nil, err
		}
		return ir.Bool(), nil
	case "cells", "minrow", "maxrow", "mincol", "maxcol":
		if args[0] == nil || args[0].Kind != ir.KSparse {
			return nil, fmt.Errorf("%s: %s needs a Sparse argument, got %s", x.Pos, name, args[0])
		}
		return ir.Int(), nil

	// -- math / number theory ------------------------------------------------
	case "abs":
		if !numeric(args[0]) {
			return nil, fmt.Errorf("%s: abs needs Int or Float, got %s", x.Pos, args[0])
		}
		return args[0], nil
	case "sign":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "gcd", "lcm", "modinv":
		for i := 0; i < 2; i++ {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "modpow":
		for i := 0; i < 3; i++ {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "solve2x2":
		for i := 0; i < 6; i++ {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return PointType(), nil

	// -- floats (H) --------------------------------------------------------------
	case "tofloat":
		if !numeric(args[0]) && !args[0].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: tofloat needs Int, Float, or Text, got %s", x.Pos, args[0])
		}
		return ir.Float(), nil
	case "floor", "ceil", "round":
		if !args[0].Equal(ir.Float()) {
			return nil, fmt.Errorf("%s: %s needs Float, got %s", x.Pos, name, args[0])
		}
		return ir.Int(), nil
	case "sqrt":
		if !numeric(args[0]) {
			return nil, fmt.Errorf("%s: sqrt needs Int or Float, got %s", x.Pos, args[0])
		}
		return ir.Float(), nil

	// -- text ------------------------------------------------------------------
	case "toint":
		if !args[0].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: toint needs Text, got %s", x.Pos, args[0])
		}
		return ir.Int(), nil
	case "totext":
		if !numeric(args[0]) {
			return nil, fmt.Errorf("%s: totext needs Int or Float, got %s", x.Pos, args[0])
		}
		return ir.Text(), nil
	case "occurrences":
		for i := 0; i < 2; i++ {
			if !args[i].Equal(ir.Text()) {
				return nil, fmt.Errorf("%s: occurrences argument %d must be Text, got %s", x.Pos, i+1, args[i])
			}
		}
		return ir.Int(), nil
	case "repeats":
		if !args[0].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: repeats needs Text, got %s", x.Pos, args[0])
		}
		return ir.Bool(), nil

	// -- points and grid geometry ---------------------------------------------
	case "point":
		for i := 0; i < 2; i++ {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return PointType(), nil
	case "prow", "pcol":
		if err := needPoint(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "padd":
		for i := 0; i < 2; i++ {
			if err := needPoint(x, name, args, i); err != nil {
				return nil, err
			}
		}
		return PointType(), nil
	case "manhattan":
		for i := 0; i < 2; i++ {
			if err := needPoint(x, name, args, i); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "rotl", "rotr":
		if err := needPoint(x, name, args, 0); err != nil {
			return nil, err
		}
		return PointType(), nil
	case "dirs4":
		return ir.List(PointType()), nil
	case "inbounds", "neighbors4", "neighbors8":
		if args[0] == nil || args[0].Kind != ir.KGrid {
			return nil, fmt.Errorf("%s: %s needs a Grid argument, got %s", x.Pos, name, args[0])
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		if err := needInt(2); err != nil {
			return nil, err
		}
		if name == "inbounds" {
			return ir.Bool(), nil
		}
		return ir.List(PointType()), nil

	// -- list/grid construction and access (A.2) --------------------------------
	case "list":
		for i := 1; i < len(args); i++ {
			if !args[i].Equal(args[0]) {
				return nil, fmt.Errorf("%s: list elements must share one type, got %s and %s",
					x.Pos, args[0], args[i])
			}
		}
		return ir.List(args[0]), nil
	case "set":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		if !args[2].Equal(elem) {
			return nil, fmt.Errorf("%s: set value must be %s, got %s", x.Pos, elem, args[2])
		}
		return args[0], nil
	case "row", "col":
		if args[0] == nil || args[0].Kind != ir.KGrid {
			return nil, fmt.Errorf("%s: %s needs a Grid argument, got %s", x.Pos, name, args[0])
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return ir.List(args[0].Elem), nil
	case "rows", "cols":
		if args[0] == nil || args[0].Kind != ir.KGrid {
			return nil, fmt.Errorf("%s: %s needs a Grid argument, got %s", x.Pos, name, args[0])
		}
		return ir.Int(), nil

	// -- bit operations ----------------------------------------------------------
	case "band", "bor", "bxor", "shl", "shr":
		for i := 0; i < 2; i++ {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "frombin":
		if !args[0].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: frombin needs Text, got %s", x.Pos, args[0])
		}
		return ir.Int(), nil
	}
	return nil, fmt.Errorf("%s: unknown function %q", x.Pos, name)
}

// needPoint requires argument i to be an (Int, Int) tuple — the expression
// layer's point representation.
func needPoint(x *ast.CallExpr, name string, args []*ir.Type, i int) error {
	if !args[i].Equal(PointType()) {
		return fmt.Errorf("%s: %s argument %d must be a point (Int, Int), got %s",
			x.Pos, name, i+1, args[i])
	}
	return nil
}

func unaryType(x *ast.UnaryExpr, env Env) (*ir.Type, error) {
	t, err := ExprType(x.X, env)
	if err != nil {
		return nil, err
	}
	if x.Op == token.MINUS {
		if !numeric(t) {
			return nil, fmt.Errorf("%s: unary minus needs Int or Float, got %s", x.Pos, t)
		}
		return t, nil
	}
	return nil, fmt.Errorf("%s: unsupported unary operator %s", x.Pos, x.Op)
}

func binaryType(x *ast.BinaryExpr, env Env) (*ir.Type, error) {
	lt, err := ExprType(x.Left, env)
	if err != nil {
		return nil, err
	}
	rt, err := ExprType(x.Right, env)
	if err != nil {
		return nil, err
	}

	switch x.Op {
	case token.AND, token.OR:
		if !lt.Equal(ir.Bool()) || !rt.Equal(ir.Bool()) {
			return nil, fmt.Errorf("%s: logical operator needs Bool operands, got %s and %s", x.Pos, lt, rt)
		}
		return ir.Bool(), nil
	case token.PLUS, token.MINUS, token.STAR, token.SLASH:
		if !numeric(lt) || !numeric(rt) {
			return nil, fmt.Errorf("%s: arithmetic needs Int or Float operands, got %s and %s", x.Pos, lt, rt)
		}
		return promote(lt, rt), nil
	case token.LT, token.GT, token.LE, token.GE:
		if !numeric(lt) || !numeric(rt) {
			return nil, fmt.Errorf("%s: comparison needs Int or Float operands, got %s and %s", x.Pos, lt, rt)
		}
		return ir.Bool(), nil
	case token.EQ:
		if numeric(lt) && numeric(rt) {
			return ir.Bool(), nil // mixed Int/Float compares through promotion
		}
		if !lt.Equal(rt) {
			return nil, fmt.Errorf("%s: cannot compare %s = %s (different types)", x.Pos, lt, rt)
		}
		return ir.Bool(), nil
	default:
		return nil, fmt.Errorf("%s: unsupported operator %s", x.Pos, x.Op)
	}
}

func fieldType(x *ast.FieldAccess, env Env) (*ir.Type, error) {
	tt, err := ExprType(x.Target, env)
	if err != nil {
		return nil, err
	}
	if tt.Kind != ir.KRecord {
		return nil, fmt.Errorf("%s: field access .%s on non-record type %s", x.Pos, x.Field, tt)
	}
	for _, f := range tt.Fields {
		if f.Name == x.Field {
			return f.Type, nil
		}
	}
	return nil, fmt.Errorf("%s: record %s has no field %q", x.Pos, tt, x.Field)
}

// numeric reports whether t participates in arithmetic: Int or Float.
func numeric(t *ir.Type) bool {
	return t != nil && (t.Kind == ir.KInt || t.Kind == ir.KFloat)
}

// promote is the numeric tower's single rule: mixing Int with Float yields
// Float; otherwise the shared type wins.
func promote(a, b *ir.Type) *ir.Type {
	if a.Kind == ir.KFloat || b.Kind == ir.KFloat {
		return ir.Float()
	}
	return ir.Int()
}

// LambdaType computes the result type of a lambda body given its parameter
// types (in declaration order). The arity must match.
func LambdaType(l *ast.Lambda, paramTypes ...*ir.Type) (*ir.Type, error) {
	if len(paramTypes) != len(l.Params) {
		return nil, fmt.Errorf("%s: lambda expects %d parameter type(s), got %d",
			l.Pos, len(l.Params), len(paramTypes))
	}
	env := make(Env, len(l.Params))
	for i, p := range l.Params {
		env[p] = paramTypes[i]
	}
	return ExprType(l.Body, env)
}
