// Package eval is the dynamic evaluator for the plain expression layer used
// inside Using: lambdas and vow predicates. It sits over ast + ir alongside
// typecheck (the static counterpart, which it consults for the few builtins
// whose result type is ambiguous from runtime values alone), so both prims
// and interp can run lambdas without importing each other.
package eval

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Env binds expression-layer identifiers (lambda parameters) to values.
type Env map[string]ir.Value

// EvalExpr evaluates an expression against an environment, with no static
// type information (see EvalLambdaTyped for why callers that know the
// parameter types should provide them).
func EvalExpr(e ast.Expr, env Env) (ir.Value, error) {
	return evalExpr(e, env, nil)
}

// evalExpr evaluates an expression against a value environment plus an
// optional static type environment. types maps the same identifiers as env
// to their statically inferred types; it may be nil (unknown), in which case
// builtins whose result type is ambiguous at runtime — sum of an empty list
// — fall back to sniffing the runtime values.
func evalExpr(e ast.Expr, env Env, types typecheck.Env) (ir.Value, error) {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.Value, nil
	case *ast.FloatLit:
		return x.Value, nil
	case *ast.BoolLit:
		return x.Value, nil
	case *ast.StringLit:
		return x.Value, nil
	case *ast.Ident:
		v, ok := env[x.Name]
		if !ok {
			return nil, fmt.Errorf("%s: unknown identifier %q", x.Pos, x.Name)
		}
		return v, nil
	case *ast.UnaryExpr:
		v, err := evalExpr(x.X, env, types)
		if err != nil {
			return nil, err
		}
		if x.Op == token.MINUS {
			switch n := v.(type) {
			case int64:
				return -n, nil
			case float64:
				return -n, nil
			}
			return nil, fmt.Errorf("%s: unary minus: expected a number, got %s", x.Pos, ir.DescribeValue(v))
		}
		return nil, fmt.Errorf("%s: unsupported unary operator %s", x.Pos, x.Op)
	case *ast.BinaryExpr:
		return evalBinary(x, env, types)
	case *ast.FieldAccess:
		return evalField(x, env, types)
	case *ast.CallExpr:
		return evalCall(x, env, types)
	case *ast.CondExpr:
		// Arms are lazy: only the selected branch runs, so guard idioms like
		// `if length(xs) = 0 then 0 else first(xs)` never trip the partial arm.
		cv, err := evalExpr(x.Cond, env, types)
		if err != nil {
			return nil, err
		}
		b, ok := cv.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: if condition must be Bool, got %s", x.Pos, ir.DescribeValue(cv))
		}
		if b {
			return evalExpr(x.Then, env, types)
		}
		return evalExpr(x.Else, env, types)
	default:
		return nil, fmt.Errorf("unsupported expression %T", e)
	}
}

// evalCall runs an expression-layer builtin (see typecheck.Builtins for the
// table; typecheck validated names, arity, and argument types at resolve
// time, so the assertions here only guard against internal inconsistency).
// Partial builtins (item, first, get, at, ...) error with the same wording
// the compiled backend uses, so behavior stays aligned across backends.
func evalCall(x *ast.CallExpr, env Env, types typecheck.Env) (ir.Value, error) {
	id, ok := x.Fn.(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("%s: only builtin functions can be called", x.Pos)
	}
	name := id.Name
	args := make([]ir.Value, len(x.Args))
	for i, a := range x.Args {
		v, err := evalExpr(a, env, types)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	fail := func(format string, fa ...any) (ir.Value, error) {
		return nil, fmt.Errorf("%s: %s", x.Pos, fmt.Sprintf(format, fa...))
	}

	switch name {
	case "length":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("length: %v", err)
		}
		return int64(len(xs)), nil
	case "item":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("item: %v", err)
		}
		i, err := ir.AsInt(args[1])
		if err != nil {
			return fail("item: %v", err)
		}
		if i < 0 || i >= int64(len(xs)) {
			return fail("item: index %d out of range (length %d)", i, len(xs))
		}
		return xs[i], nil
	case "take", "drop":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		n, err := ir.AsInt(args[1])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		if n < 0 {
			n = 0
		}
		if n > int64(len(xs)) {
			n = int64(len(xs))
		}
		if name == "take" {
			return xs[:n], nil
		}
		return xs[n:], nil
	case "reverse":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("reverse: %v", err)
		}
		out := make([]ir.Value, len(xs))
		for i, e := range xs {
			out[len(xs)-1-i] = e
		}
		return out, nil
	case "concat":
		a, err := ir.AsList(args[0])
		if err != nil {
			return fail("concat: %v", err)
		}
		b, err := ir.AsList(args[1])
		if err != nil {
			return fail("concat: %v", err)
		}
		out := make([]ir.Value, 0, len(a)+len(b))
		out = append(out, a...)
		out = append(out, b...)
		return out, nil
	case "first", "last":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		if len(xs) == 0 {
			return fail("%s of an empty list is undefined", name)
		}
		if name == "first" {
			return xs[0], nil
		}
		return xs[len(xs)-1], nil
	case "sum":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("sum: %v", err)
		}
		if sumIsFloat(x.Args[0], types, xs) {
			var s float64
			for i, e := range xs {
				f, err := ir.AsFloat(e)
				if err != nil {
					return fail("sum: list element %d: %v", i, err)
				}
				s += f
			}
			return s, nil
		}
		var s int64
		for i, e := range xs {
			n, err := ir.AsInt(e)
			if err != nil {
				return fail("sum: list element %d: %v", i, err)
			}
			s += n
		}
		return s, nil
	case "min", "max":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		if len(xs) == 0 {
			return fail("%s of an empty list is undefined", name)
		}
		if listHasFloat(xs) {
			fs, err := ir.AsFloatSlice(args[0])
			if err != nil {
				return fail("%s: %v", name, err)
			}
			acc := fs[0]
			for _, f := range fs[1:] {
				if (name == "min" && f < acc) || (name == "max" && f > acc) {
					acc = f
				}
			}
			return acc, nil
		}
		ns, err := ir.AsIntSlice(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		acc := ns[0]
		for _, n := range ns[1:] {
			if (name == "min" && n < acc) || (name == "max" && n > acc) {
				acc = n
			}
		}
		return acc, nil
	case "contains":
		if s, ok := args[0].(*ir.SetValue); ok {
			return s.Has(args[1]), nil
		}
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("contains: %v", err)
		}
		for _, e := range xs {
			if ir.DeepEqual(e, args[1]) {
				return true, nil
			}
		}
		return false, nil
	case "get":
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("get: expected a Map, got %s", ir.DescribeValue(args[0]))
		}
		v, ok := m.Get(args[1])
		if !ok {
			return fail("get: map has no key %s", ir.FormatValue(args[1]))
		}
		return v, nil
	case "at":
		r, err := ir.AsInt(args[1])
		if err != nil {
			return fail("at: %v", err)
		}
		c, err := ir.AsInt(args[2])
		if err != nil {
			return fail("at: %v", err)
		}
		// Sparse grids are an infinite plane: at is total (unset reads the
		// default). Dense grids keep their bounds-checked partial behavior.
		if sp, ok := args[0].(*ir.SparseValue); ok {
			return sp.At(r, c), nil
		}
		grid, ok := args[0].(*ir.GridValue)
		if !ok {
			return fail("at: expected a Grid, got %s", ir.DescribeValue(args[0]))
		}
		v, ok := grid.At(int(r), int(c))
		if !ok {
			return fail("at: position (%d, %d) out of range (grid %dx%d)", r, c, grid.Rows, grid.Cols)
		}
		return v, nil

	// -- sparse grids (H) --------------------------------------------------------
	case "sparse":
		return ir.NewSparseValue(args[0]), nil
	case "put":
		sp, ok := args[0].(*ir.SparseValue)
		if !ok {
			return fail("put: expected a Sparse grid, got %s", ir.DescribeValue(args[0]))
		}
		r, err1 := ir.AsInt(args[1])
		c, err2 := ir.AsInt(args[2])
		if err := firstErr(err1, err2); err != nil {
			return fail("put: %v", err)
		}
		out := sp.Clone()
		out.Put(r, c, args[3])
		return out, nil
	case "has":
		sp, ok := args[0].(*ir.SparseValue)
		if !ok {
			return fail("has: expected a Sparse grid, got %s", ir.DescribeValue(args[0]))
		}
		r, err1 := ir.AsInt(args[1])
		c, err2 := ir.AsInt(args[2])
		if err := firstErr(err1, err2); err != nil {
			return fail("has: %v", err)
		}
		return sp.Has(r, c), nil
	case "cells":
		sp, ok := args[0].(*ir.SparseValue)
		if !ok {
			return fail("cells: expected a Sparse grid, got %s", ir.DescribeValue(args[0]))
		}
		return int64(sp.Len()), nil
	case "minrow", "maxrow", "mincol", "maxcol":
		sp, ok := args[0].(*ir.SparseValue)
		if !ok {
			return fail("%s: expected a Sparse grid, got %s", name, ir.DescribeValue(args[0]))
		}
		minR, minC, maxR, maxC, ok := sp.Bounds()
		if !ok {
			return fail("%s of an empty sparse grid is undefined", name)
		}
		switch name {
		case "minrow":
			return minR, nil
		case "maxrow":
			return maxR, nil
		case "mincol":
			return minC, nil
		}
		return maxC, nil

	// -- math / number theory ------------------------------------------------
	case "abs":
		switch n := args[0].(type) {
		case int64:
			if n < 0 {
				return -n, nil
			}
			return n, nil
		case float64:
			// Branch form (not math.Abs) so abs(-0.0) renders identically in
			// both backends — codegen's dmAbs is the same comparison.
			if n < 0 {
				return -n, nil
			}
			return n, nil
		}
		return fail("abs: expected a number, got %s", ir.DescribeValue(args[0]))
	case "sign":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("sign: %v", err)
		}
		switch {
		case n < 0:
			return int64(-1), nil
		case n > 0:
			return int64(1), nil
		}
		return int64(0), nil
	case "gcd":
		a, b, err := twoInts(args, "gcd")
		if err != nil {
			return fail("%v", err)
		}
		return gcdInt(a, b), nil
	case "lcm":
		a, b, err := twoInts(args, "lcm")
		if err != nil {
			return fail("%v", err)
		}
		return lcmInt(a, b), nil
	case "modpow":
		base, err1 := ir.AsInt(args[0])
		exp, err2 := ir.AsInt(args[1])
		mod, err3 := ir.AsInt(args[2])
		if err := firstErr(err1, err2, err3); err != nil {
			return fail("modpow: %v", err)
		}
		r, err := modPow(base, exp, mod)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "modinv":
		a, m, err := twoInts(args, "modinv")
		if err != nil {
			return fail("%v", err)
		}
		r, err := modInverse(a, m)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "solve2x2":
		ns := make([]int64, 6)
		for i := range ns {
			n, err := ir.AsInt(args[i])
			if err != nil {
				return fail("solve2x2: %v", err)
			}
			ns[i] = n
		}
		x, y, err := solve2x2(ns[0], ns[1], ns[2], ns[3], ns[4], ns[5])
		if err != nil {
			return fail("%v", err)
		}
		return []ir.Value{x, y}, nil

	// -- floats (H) --------------------------------------------------------------
	case "tofloat":
		switch x := args[0].(type) {
		case float64:
			return x, nil
		case int64:
			return float64(x), nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return fail("tofloat: %q is not a number", x)
			}
			return f, nil
		}
		return fail("tofloat: expected Int, Float, or Text, got %s", ir.DescribeValue(args[0]))
	case "floor", "ceil", "round":
		f, ok := args[0].(float64)
		if !ok {
			return fail("%s: expected Float, got %s", name, ir.DescribeValue(args[0]))
		}
		switch name {
		case "floor":
			return int64(math.Floor(f)), nil
		case "ceil":
			return int64(math.Ceil(f)), nil
		default:
			return int64(math.Round(f)), nil
		}
	case "sqrt":
		f, err := ir.AsFloat(args[0])
		if err != nil {
			return fail("sqrt: %v", err)
		}
		if f < 0 {
			return fail("sqrt of a negative number (%s)", ir.FormatFloat(f))
		}
		return math.Sqrt(f), nil

	// -- text ------------------------------------------------------------------
	case "toint":
		s, ok := args[0].(string)
		if !ok {
			return fail("toint: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return fail("toint: %q is not an integer", s)
		}
		return n, nil
	case "totext":
		// Renders exactly as Reveal would: FormatInt for Int, the shortest
		// round-trip 'g' form for Float.
		switch n := args[0].(type) {
		case int64:
			return strconv.FormatInt(n, 10), nil
		case float64:
			return ir.FormatFloat(n), nil
		}
		return fail("totext: expected a number, got %s", ir.DescribeValue(args[0]))
	case "occurrences":
		s, ok1 := args[0].(string)
		sub, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return fail("occurrences: expected Text arguments")
		}
		return int64(strings.Count(s, sub)), nil
	case "repeats":
		s, ok := args[0].(string)
		if !ok {
			return fail("repeats: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		return isRepeatedPattern(s), nil

	// -- points and grid geometry ---------------------------------------------
	case "point":
		r, c, err := twoInts(args, "point")
		if err != nil {
			return fail("%v", err)
		}
		return []ir.Value{r, c}, nil
	case "prow", "pcol":
		r, c, err := asPoint(args[0], name)
		if err != nil {
			return fail("%v", err)
		}
		if name == "prow" {
			return r, nil
		}
		return c, nil
	case "padd":
		r1, c1, err1 := asPoint(args[0], name)
		r2, c2, err2 := asPoint(args[1], name)
		if err := firstErr(err1, err2); err != nil {
			return fail("%v", err)
		}
		return []ir.Value{r1 + r2, c1 + c2}, nil
	case "manhattan":
		r1, c1, err1 := asPoint(args[0], name)
		r2, c2, err2 := asPoint(args[1], name)
		if err := firstErr(err1, err2); err != nil {
			return fail("%v", err)
		}
		return absInt(r1-r2) + absInt(c1-c2), nil
	case "rotl", "rotr":
		r, c, err := asPoint(args[0], name)
		if err != nil {
			return fail("%v", err)
		}
		if name == "rotl" {
			return []ir.Value{-c, r}, nil
		}
		return []ir.Value{c, -r}, nil
	case "dirs4":
		out := make([]ir.Value, 0, 4)
		for _, d := range dirs4Deltas {
			out = append(out, []ir.Value{d[0], d[1]})
		}
		return out, nil
	case "inbounds", "neighbors4", "neighbors8":
		grid, ok := args[0].(*ir.GridValue)
		if !ok {
			return fail("%s: expected a Grid, got %s", name, ir.DescribeValue(args[0]))
		}
		r, err1 := ir.AsInt(args[1])
		c, err2 := ir.AsInt(args[2])
		if err := firstErr(err1, err2); err != nil {
			return fail("%s: %v", name, err)
		}
		if name == "inbounds" {
			return grid.InBounds(int(r), int(c)), nil
		}
		coords := grid.Neighbors(int(r), int(c), name == "neighbors8")
		out := make([]ir.Value, len(coords))
		for i, rc := range coords {
			out[i] = []ir.Value{int64(rc[0]), int64(rc[1])}
		}
		return out, nil

	// -- list/grid construction and access (A.2) --------------------------------
	case "list":
		return append([]ir.Value{}, args...), nil
	case "set":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("set: %v", err)
		}
		i, err := ir.AsInt(args[1])
		if err != nil {
			return fail("set: %v", err)
		}
		if i < 0 || i >= int64(len(xs)) {
			return fail("set: index %d out of range (length %d)", i, len(xs))
		}
		out := append([]ir.Value(nil), xs...)
		out[i] = args[2]
		return out, nil
	case "row", "col":
		grid, ok := args[0].(*ir.GridValue)
		if !ok {
			return fail("%s: expected a Grid, got %s", name, ir.DescribeValue(args[0]))
		}
		i, err := ir.AsInt(args[1])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		if name == "row" {
			if i < 0 || i >= int64(grid.Rows) {
				return fail("row: row %d out of range (grid %dx%d)", i, grid.Rows, grid.Cols)
			}
			out := make([]ir.Value, grid.Cols)
			for c := 0; c < grid.Cols; c++ {
				out[c], _ = grid.At(int(i), c)
			}
			return out, nil
		}
		if i < 0 || i >= int64(grid.Cols) {
			return fail("col: column %d out of range (grid %dx%d)", i, grid.Rows, grid.Cols)
		}
		out := make([]ir.Value, grid.Rows)
		for r := 0; r < grid.Rows; r++ {
			out[r], _ = grid.At(r, int(i))
		}
		return out, nil
	case "rows", "cols":
		grid, ok := args[0].(*ir.GridValue)
		if !ok {
			return fail("%s: expected a Grid, got %s", name, ir.DescribeValue(args[0]))
		}
		if name == "rows" {
			return int64(grid.Rows), nil
		}
		return int64(grid.Cols), nil

	// -- bit operations ----------------------------------------------------------
	case "band", "bor", "bxor":
		a, b, err := twoInts(args, name)
		if err != nil {
			return fail("%v", err)
		}
		switch name {
		case "band":
			return a & b, nil
		case "bor":
			return a | b, nil
		}
		return a ^ b, nil
	case "shl", "shr":
		a, n, err := twoInts(args, name)
		if err != nil {
			return fail("%v", err)
		}
		if n < 0 {
			return fail("%s: negative shift count %d", name, n)
		}
		if name == "shl" {
			return a << n, nil
		}
		return a >> n, nil
	case "frombin":
		s, ok := args[0].(string)
		if !ok {
			return fail("frombin: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 2, 64)
		if err != nil {
			return fail("frombin: %q is not a binary number", s)
		}
		return n, nil
	default:
		return fail("unknown function %q", name)
	}
}

// dirs4Deltas are the four orthogonal direction vectors (up, down, left,
// right), in the same order GridValue.Neighbors visits them.
var dirs4Deltas = [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}

// twoInts unpacks two Int arguments for a builtin.
func twoInts(args []ir.Value, name string) (int64, int64, error) {
	a, err := ir.AsInt(args[0])
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %v", name, err)
	}
	b, err := ir.AsInt(args[1])
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %v", name, err)
	}
	return a, b, nil
}

// asPoint unpacks an (Int, Int) tuple value.
func asPoint(v ir.Value, name string) (int64, int64, error) {
	xs, ok := v.([]ir.Value)
	if !ok || len(xs) != 2 {
		return 0, 0, fmt.Errorf("%s: expected a point (Int, Int), got %s", name, ir.DescribeValue(v))
	}
	r, ok1 := xs[0].(int64)
	c, ok2 := xs[1].(int64)
	if !ok1 || !ok2 {
		return 0, 0, fmt.Errorf("%s: expected a point (Int, Int)", name)
	}
	return r, c, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// gcdInt is the non-negative greatest common divisor; gcd(0, 0) = 0.
func gcdInt(a, b int64) int64 {
	a, b = absInt(a), absInt(b)
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// lcmInt is the non-negative least common multiple; lcm(a, 0) = 0.
func lcmInt(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	return absInt(a / gcdInt(a, b) * b)
}

// modPow computes base^exp mod m by binary exponentiation. The exponent must
// be non-negative and the modulus positive; the result is in [0, m).
func modPow(base, exp, m int64) (int64, error) {
	if m <= 0 {
		return 0, fmt.Errorf("modpow: modulus must be positive, got %d", m)
	}
	if exp < 0 {
		return 0, fmt.Errorf("modpow: exponent must be non-negative, got %d", exp)
	}
	result := int64(1) % m
	base = ((base % m) + m) % m
	for exp > 0 {
		if exp&1 == 1 {
			result = result * base % m
		}
		base = base * base % m
		exp >>= 1
	}
	return result, nil
}

// modInverse computes the multiplicative inverse of a modulo m (extended
// Euclid). a and m must be coprime and m positive; the result is in [0, m).
func modInverse(a, m int64) (int64, error) {
	if m <= 0 {
		return 0, fmt.Errorf("modinv: modulus must be positive, got %d", m)
	}
	a = ((a % m) + m) % m
	// Extended Euclid on (a, m): track x with a*x ≡ r (mod m).
	r0, r1 := a, m
	x0, x1 := int64(1), int64(0)
	for r1 != 0 {
		q := r0 / r1
		r0, r1 = r1, r0-q*r1
		x0, x1 = x1, x0-q*x1
	}
	if r0 != 1 {
		return 0, fmt.Errorf("modinv: %d has no inverse modulo %d (not coprime)", a, m)
	}
	return ((x0 % m) + m) % m, nil
}

// solve2x2 solves the integer linear system a*x + b*y = c, d*x + e*y = f by
// Cramer's rule. It errors when the determinant is zero (no unique solution)
// or when the unique solution is not integral.
func solve2x2(a, b, c, d, e, f int64) (int64, int64, error) {
	det := a*e - b*d
	if det == 0 {
		return 0, 0, fmt.Errorf("solve2x2: system has no unique solution (determinant is zero)")
	}
	xNum := c*e - b*f
	yNum := a*f - c*d
	if xNum%det != 0 || yNum%det != 0 {
		return 0, 0, fmt.Errorf("solve2x2: solution is not integral")
	}
	return xNum / det, yNum / det, nil
}

// isRepeatedPattern reports whether s is a shorter pattern repeated two or
// more times ("abab", "aaa"), via the classic rotation trick: s is periodic
// iff it occurs in (s+s) minus that string's first and last characters.
func isRepeatedPattern(s string) bool {
	if len(s) < 2 {
		return false
	}
	doubled := (s + s)[1 : 2*len(s)-1]
	return strings.Contains(doubled, s)
}

// EvalLambda evaluates a lambda body with positional arguments bound to params.
// Callers that know the statically inferred parameter types should use
// EvalLambdaTyped instead, so type-ambiguous builtin results (sum of an empty
// list) match the compiled backend.
func EvalLambda(l *ast.Lambda, args ...ir.Value) (ir.Value, error) {
	return EvalLambdaTyped(l, nil, args...)
}

// EvalLambdaTyped evaluates a lambda like EvalLambda, additionally binding
// the statically inferred parameter types (in declaration order — the same
// types the caller passed to typecheck.LambdaType at resolve time). The types
// let builtins whose result type is ambiguous from runtime values alone —
// sum of an empty list, whose zero must carry the list's element type —
// agree with the compiled backend, where the static types always decide.
// paramTypes may be nil or contain nils; missing types degrade gracefully to
// EvalLambda's dynamic behavior.
func EvalLambdaTyped(l *ast.Lambda, paramTypes []*ir.Type, args ...ir.Value) (ir.Value, error) {
	if len(args) != len(l.Params) {
		return nil, fmt.Errorf("lambda expects %d argument(s), got %d", len(l.Params), len(args))
	}
	env := make(Env, len(l.Params))
	for i, p := range l.Params {
		env[p] = args[i]
	}
	var types typecheck.Env
	if len(paramTypes) == len(l.Params) {
		types = make(typecheck.Env, len(l.Params))
		for i, p := range l.Params {
			if paramTypes[i] == nil {
				types = nil
				break
			}
			types[p] = paramTypes[i]
		}
	}
	return evalExpr(l.Body, env, types)
}

func evalField(x *ast.FieldAccess, env Env, types typecheck.Env) (ir.Value, error) {
	tv, err := evalExpr(x.Target, env, types)
	if err != nil {
		return nil, err
	}
	rec, ok := tv.(*ir.RecordValue)
	if !ok {
		return nil, fmt.Errorf("%s: field access .%s on non-record value (%s)",
			x.Pos, x.Field, ir.DescribeValue(tv))
	}
	v, ok := rec.Get(x.Field)
	if !ok {
		return nil, fmt.Errorf("%s: record has no field %q", x.Pos, x.Field)
	}
	return v, nil
}

func evalBinary(x *ast.BinaryExpr, env Env, types typecheck.Env) (ir.Value, error) {
	// and/or short-circuit: the right operand is only evaluated when needed,
	// so guard-clause idioms like `n = 0 or 10 / n = 5` don't error on the
	// unevaluated branch.
	if x.Op == token.AND || x.Op == token.OR {
		lv, err := evalExpr(x.Left, env, types)
		if err != nil {
			return nil, err
		}
		a, ok := lv.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: logical operator needs Bool operands, left is %s", x.Pos, ir.DescribeValue(lv))
		}
		if x.Op == token.AND && !a {
			return false, nil
		}
		if x.Op == token.OR && a {
			return true, nil
		}
		rv, err := evalExpr(x.Right, env, types)
		if err != nil {
			return nil, err
		}
		b, ok := rv.(bool)
		if !ok {
			return nil, fmt.Errorf("%s: logical operator needs Bool operands, right is %s", x.Pos, ir.DescribeValue(rv))
		}
		return b, nil
	}

	lv, err := evalExpr(x.Left, env, types)
	if err != nil {
		return nil, err
	}
	rv, err := evalExpr(x.Right, env, types)
	if err != nil {
		return nil, err
	}

	switch x.Op {
	case token.PLUS, token.MINUS, token.STAR, token.SLASH:
		// The numeric tower's one implicit conversion: mixing an Int with a
		// Float computes in Float.
		if isFloatOperand(lv) || isFloatOperand(rv) {
			a, err := ir.AsFloat(lv)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", x.Pos, err)
			}
			b, err := ir.AsFloat(rv)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", x.Pos, err)
			}
			switch x.Op {
			case token.PLUS:
				return a + b, nil
			case token.MINUS:
				return a - b, nil
			case token.STAR:
				return a * b, nil
			case token.SLASH:
				if b == 0 {
					return nil, fmt.Errorf("%s: division by zero", x.Pos)
				}
				return a / b, nil
			}
		}
		a, err := ir.AsInt(lv)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.Pos, err)
		}
		b, err := ir.AsInt(rv)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.Pos, err)
		}
		switch x.Op {
		case token.PLUS:
			return a + b, nil
		case token.MINUS:
			return a - b, nil
		case token.STAR:
			return a * b, nil
		case token.SLASH:
			if b == 0 {
				return nil, fmt.Errorf("%s: division by zero", x.Pos)
			}
			return a / b, nil
		}
	case token.EQ:
		if isFloatOperand(lv) || isFloatOperand(rv) {
			a, errA := ir.AsFloat(lv)
			b, errB := ir.AsFloat(rv)
			if errA == nil && errB == nil {
				return a == b, nil
			}
		}
		return valuesEqual(lv, rv), nil
	case token.LT, token.GT, token.LE, token.GE:
		if isFloatOperand(lv) || isFloatOperand(rv) {
			a, err := ir.AsFloat(lv)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", x.Pos, err)
			}
			b, err := ir.AsFloat(rv)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", x.Pos, err)
			}
			switch x.Op {
			case token.LT:
				return a < b, nil
			case token.GT:
				return a > b, nil
			case token.LE:
				return a <= b, nil
			case token.GE:
				return a >= b, nil
			}
		}
		a, err := ir.AsInt(lv)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.Pos, err)
		}
		b, err := ir.AsInt(rv)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.Pos, err)
		}
		switch x.Op {
		case token.LT:
			return a < b, nil
		case token.GT:
			return a > b, nil
		case token.LE:
			return a <= b, nil
		case token.GE:
			return a >= b, nil
		}
	}
	return nil, fmt.Errorf("%s: unsupported operator %s", x.Pos, x.Op)
}

// valuesEqual implements the `=` operator. It defers to ir.DeepEqual so `=`
// on composite values (List/Record/Map/Set/Grid) — which typecheck.go's
// binaryType legally allows for any pair of structurally-equal static types —
// compares structurally instead of always reporting false.
func valuesEqual(a, b ir.Value) bool {
	return ir.DeepEqual(a, b)
}

// isFloatOperand reports whether a runtime value routes arithmetic through
// the Float path.
func isFloatOperand(v ir.Value) bool {
	_, ok := v.(float64)
	return ok
}

// sumIsFloat decides which accumulator sum uses. When the caller supplied the
// lambda's static parameter types (EvalLambdaTyped), the argument's inferred
// List element type is authoritative — exactly how codegen instantiates
// dmSum, whose zero value carries the slice's element type even for an empty
// slice, so sum of a runtime-empty List<Float> is float64(0) in both
// backends. Without static types the runtime elements are sniffed, which
// preserves the dynamic behavior for direct EvalExpr/EvalLambda callers but
// cannot tell an empty List<Float> from an empty List<Int>.
func sumIsFloat(arg ast.Expr, types typecheck.Env, xs []ir.Value) bool {
	if types != nil {
		if t, err := typecheck.ExprType(arg, types); err == nil &&
			t != nil && t.Kind == ir.KList && t.Elem != nil &&
			(t.Elem.Kind == ir.KFloat || t.Elem.Kind == ir.KInt) {
			return t.Elem.Kind == ir.KFloat
		}
	}
	return listHasFloat(xs)
}

// listHasFloat reports whether a list carries Float elements — the dynamic
// mirror of the typechecker's List<Float>. A well-typed list is homogeneous,
// so checking the first element suffices; the scan keeps ill-typed values on
// the honest error path instead of silently treating them as ints.
func listHasFloat(xs []ir.Value) bool {
	for _, e := range xs {
		if _, ok := e.(float64); ok {
			return true
		}
	}
	return false
}
