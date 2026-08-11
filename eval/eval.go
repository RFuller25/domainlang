// Package eval is the dynamic evaluator for the plain expression layer used
// inside Using: lambdas and vow predicates. It sits over ast + ir alongside
// typecheck (the static counterpart, which it consults for the few builtins
// whose result type is ambiguous from runtime values alone), so both prims
// and interp can run lambdas without importing each other.
package eval

import (
	"fmt"
	"maps"
	"math"
	"math/bits"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

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
//
// Every recursive step goes through here, which is what lets TraceLambda
// (trace.go) watch a whole application without a single case below knowing it
// can be watched. What that costs an ordinary run is the nil check — the same
// bargain ir.EvalNode makes for the pipeline layer.
func evalExpr(e ast.Expr, env Env, types typecheck.Env) (ir.Value, error) {
	if tracing != nil {
		return tracing.step(e, env, types)
	}
	return evalExprStep(e, env, types)
}

// evalExprStep is evalExpr's body: one node, recursing back through evalExpr
// so that a trace sees its children too.
func evalExprStep(e ast.Expr, env Env, types typecheck.Env) (ir.Value, error) {
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
		return Deref(v), nil
	case *ast.BlockBody:
		// The sub-pipeline form of a lambda body: run the resolved body over
		// the value bound to the parameter it reads.
		v, ok := env[x.Param]
		if !ok {
			return nil, fmt.Errorf("%s: block body has no input binding", x.Pos)
		}
		// The lambda's other parameters are in scope by name for the whole
		// body, on the same stack `Consider` pushes to — so the body's own
		// lambdas read them without any primitive knowing they exist.
		for _, b := range x.Extra {
			bv, ok := env[b.Param]
			if !ok {
				return nil, fmt.Errorf("%s: block body has no binding for %q", x.Pos, b.Name)
			}
			PushBinding(b.Name, Deref(bv), types[b.Param])
		}
		out, err := x.Pipe.RunBlock(Deref(v))
		PopBindings(len(x.Extra))
		return out, err
	case *ast.UnaryExpr:
		v, err := evalExpr(x.X, env, types)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case token.MINUS:
			switch n := v.(type) {
			case int64:
				return -n, nil
			case float64:
				return -n, nil
			}
			return nil, fmt.Errorf("%s: unary minus: expected a number, got %s", x.Pos, ir.DescribeValue(v))
		case token.NOT:
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%s: ikke: expected a Bool, got %s", x.Pos, ir.DescribeValue(v))
			}
			return !b, nil
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
	case *ast.LetExpr:
		// Evaluated once, then bound for the body — which is the point: the
		// alternative spelling writes the subexpression twice and computes it
		// twice, since lambda-body CSE is not an implemented optimizer pass.
		v, err := evalExpr(x.Value, env, types)
		if err != nil {
			return nil, err
		}
		inner := make(Env, len(env)+1)
		maps.Copy(inner, env)
		if updates.Load() {
			// A box, so a `:=` anywhere in the body writes somewhere the rest
			// of the body can see. It is this scope's own box: an inner
			// `consider` of the same name gets another one, so shadowing keeps
			// meaning what it means everywhere else in the language.
			inner[x.Name] = &Cell{V: v}
		} else {
			inner[x.Name] = v
		}
		var innerTypes typecheck.Env
		if types != nil {
			innerTypes = make(typecheck.Env, len(types)+1)
			maps.Copy(innerTypes, types)
			if vt, err := typecheck.ExprType(x.Value, types); err == nil {
				innerTypes[x.Name] = vt
			}
		}
		return evalExpr(x.Body, inner, innerTypes)
	case *ast.AssignExpr:
		// The value first, then the write, then the value again as the result:
		// `n := n + 1` reads the old n while computing, and yields the new one.
		v, err := evalExpr(x.Value, env, types)
		if err != nil {
			return nil, err
		}
		if err := assignTo(x, env, v); err != nil {
			return nil, err
		}
		return v, nil
	case *ast.AlsoExpr:
		// The body's value is taken before the clauses run, so a clause that
		// updates what the body read cannot change what the body already
		// yielded — the clauses are for the *next* reader of the name.
		v, err := evalExpr(x.Body, env, types)
		if err != nil {
			return nil, err
		}
		for _, c := range x.Clauses {
			if _, err := evalExpr(c, env, types); err != nil {
				return nil, err
			}
		}
		return v, nil
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
	// Every case below reads its arguments by position, so the count has to be
	// right before any of them runs. The type checker enforces the same rule,
	// but it is not always what got here first: constant folding evaluates an
	// expression while the program is still being lowered, ahead of any typing,
	// and an editor asks for that on every keystroke of a half-written call.
	if err := typecheck.CheckArity(name, len(x.Args)); err != nil {
		return nil, fmt.Errorf("%s: %v", x.Pos, err)
	}
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
		// Runes, not bytes — the unit `Split Text by ""` already uses.
		if s, ok := args[0].(string); ok {
			return int64(utf8.RuneCountInString(s)), nil
		}
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
		n = min(max(n, 0), int64(len(xs)))
		if name == "take" {
			return xs[:n], nil
		}
		return xs[n:], nil
	// -- first-order list operations ---------------------------------------------
	// Each one mirrors the primitive of the same job exactly: sort is the Sort
	// ordering, unique keeps first-seen order like Unique, chunk keeps a short
	// final block like Chunk, windows drops a trailing partial one like Window,
	// and transpose refuses a ragged input like Transpose.
	case "sort":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("sort: %v", err)
		}
		out := append([]ir.Value(nil), xs...)
		slices.SortStableFunc(out, ir.Compare)
		return out, nil
	case "unique":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("unique: %v", err)
		}
		return ir.SetFromList(xs).Elems(), nil
	case "flatten":
		groups, err := ir.AsList(args[0])
		if err != nil {
			return fail("flatten: %v", err)
		}
		out := []ir.Value{}
		for i, g := range groups {
			inner, err := ir.AsList(g)
			if err != nil {
				return fail("flatten: group %d: %v", i, err)
			}
			out = append(out, inner...)
		}
		return out, nil
	case "bandall", "borall", "bxorall":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		// The identity of each operator, so the empty list is the value that
		// leaves any later fold unchanged — the rule sum(0)/product(1) follow.
		// For `and` that is all bits set, which is -1 in two's complement.
		acc := int64(0)
		if name == "bandall" {
			acc = -1
		}
		for i, e := range xs {
			n, err := ir.AsInt(e)
			if err != nil {
				return fail("%s: list element %d: %v", name, i, err)
			}
			switch name {
			case "bandall":
				acc &= n
			case "borall":
				acc |= n
			default:
				acc ^= n
			}
		}
		return acc, nil
	case "and", "or", "xor", "not":
		bs := make([]bool, len(args))
		for i, a := range args {
			b, ok := a.(bool)
			if !ok {
				return fail("%s: expected Bool, got %s", name, ir.DescribeValue(a))
			}
			bs[i] = b
		}
		switch name {
		case "and":
			return bs[0] && bs[1], nil
		case "or":
			return bs[0] || bs[1], nil
		case "xor":
			return bs[0] != bs[1], nil
		default:
			return !bs[0], nil
		}
	case "product":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("product: %v", err)
		}
		if sumIsFloat(x.Args[0], types, xs) {
			p := 1.0
			for i, e := range xs {
				f, err := ir.AsFloat(e)
				if err != nil {
					return fail("product: list element %d: %v", i, err)
				}
				p *= f
			}
			return p, nil
		}
		p := int64(1)
		for i, e := range xs {
			n, err := ir.AsInt(e)
			if err != nil {
				return fail("product: list element %d: %v", i, err)
			}
			p *= n
		}
		return p, nil
	case "zip":
		a, err := ir.AsList(args[0])
		if err != nil {
			return fail("zip: %v", err)
		}
		b, err := ir.AsList(args[1])
		if err != nil {
			return fail("zip: %v", err)
		}
		n := min(len(a), len(b))
		out := make([]ir.Value, n)
		for i := range n {
			out[i] = []ir.Value{a[i], b[i]}
		}
		return out, nil
	case "enumerate":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("enumerate: %v", err)
		}
		out := make([]ir.Value, len(xs))
		for i, v := range xs {
			out[i] = []ir.Value{int64(i), v}
		}
		return out, nil
	case "chunk", "windows":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		n, err := ir.AsInt(args[1])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		if n < 1 {
			return fail("%s: size must be >= 1, got %d", name, n)
		}
		out := []ir.Value{}
		if name == "chunk" {
			for i := int64(0); i < int64(len(xs)); i += n {
				out = append(out, append([]ir.Value(nil), xs[i:min(i+n, int64(len(xs)))]...))
			}
			return out, nil
		}
		for i := int64(0); i+n <= int64(len(xs)); i++ {
			out = append(out, append([]ir.Value(nil), xs[i:i+n]...))
		}
		return out, nil
	case "transpose":
		rows, err := ir.AsList(args[0])
		if err != nil {
			return fail("transpose: %v", err)
		}
		cells := make([][]ir.Value, len(rows))
		cols := 0
		for r, row := range rows {
			if cells[r], err = ir.AsList(row); err != nil {
				return fail("transpose: row %d: %v", r, err)
			}
			if r == 0 {
				cols = len(cells[0])
			} else if len(cells[r]) != cols {
				return fail("transpose: grid is not rectangular: row %d has %d cells, expected %d",
					r, len(cells[r]), cols)
			}
		}
		out := make([]ir.Value, cols)
		for c := range cols {
			col := make([]ir.Value, len(cells))
			for r := range cells {
				col[r] = cells[r][c]
			}
			out[c] = col
		}
		return out, nil
	case "reverse":
		// By rune, like every other text position in the language.
		if s, ok := args[0].(string); ok {
			rs := []rune(s)
			slices.Reverse(rs)
			return string(rs), nil
		}
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
		// Two arguments is the scalar form; one is the list reduction.
		if len(args) == 2 {
			if isFloatOperand(args[0]) || isFloatOperand(args[1]) {
				a, err1 := ir.AsFloat(args[0])
				b, err2 := ir.AsFloat(args[1])
				if err := firstErr(err1, err2); err != nil {
					return fail("%s: %v", name, err)
				}
				if name == "min" {
					return math.Min(a, b), nil
				}
				return math.Max(a, b), nil
			}
			a, b, err := twoInts(args, name)
			if err != nil {
				return fail("%v", err)
			}
			if (name == "min") == (a < b) {
				return a, nil
			}
			return b, nil
		}
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
		if s, ok := args[0].(string); ok {
			sub, ok := args[1].(string)
			if !ok {
				return fail("contains: expected a Text needle, got %s", ir.DescribeValue(args[1]))
			}
			return strings.Contains(s, sub), nil
		}
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
		out := sp
		if !x.InPlace {
			out = sp.Clone()
		}
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
	case "mod":
		a, b, err := twoInts(args, "mod")
		if err != nil {
			return fail("%v", err)
		}
		r, err := euclidMod(a, b)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "divmod":
		a, b, err := twoInts(args, "divmod")
		if err != nil {
			return fail("%v", err)
		}
		q, err := euclidDiv(a, b)
		if err != nil {
			return fail("divmod by zero")
		}
		m, err := euclidMod(a, b)
		if err != nil {
			return fail("divmod by zero")
		}
		return []ir.Value{q, m}, nil
	case "pow":
		// Follows the operators' promotion rule: integral unless an operand is
		// a Float, in which case it is math.Pow like every other float op.
		if isFloatOperand(args[0]) || isFloatOperand(args[1]) {
			a, err1 := ir.AsFloat(args[0])
			b, err2 := ir.AsFloat(args[1])
			if err := firstErr(err1, err2); err != nil {
				return fail("pow: %v", err)
			}
			return math.Pow(a, b), nil
		}
		a, b, err := twoInts(args, "pow")
		if err != nil {
			return fail("%v", err)
		}
		r, err := powInt(a, b)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "isqrt":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("isqrt: %v", err)
		}
		r, err := isqrtInt(n)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "factorial":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("factorial: %v", err)
		}
		r, err := factorialInt(n)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "choose":
		n, k, err := twoInts(args, "choose")
		if err != nil {
			return fail("%v", err)
		}
		r, err := chooseInt(n, k)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "clamp":
		// Float if any operand is, matching the arithmetic promotion rule.
		if isFloatOperand(args[0]) || isFloatOperand(args[1]) || isFloatOperand(args[2]) {
			v, err1 := ir.AsFloat(args[0])
			lo, err2 := ir.AsFloat(args[1])
			hi, err3 := ir.AsFloat(args[2])
			if err := firstErr(err1, err2, err3); err != nil {
				return fail("clamp: %v", err)
			}
			if lo > hi {
				return fail("clamp: low bound %v exceeds high bound %v", lo, hi)
			}
			return math.Min(math.Max(v, lo), hi), nil
		}
		v, err1 := ir.AsInt(args[0])
		lo, err2 := ir.AsInt(args[1])
		hi, err3 := ir.AsInt(args[2])
		if err := firstErr(err1, err2, err3); err != nil {
			return fail("clamp: %v", err)
		}
		if lo > hi {
			return fail("clamp: low bound %d exceeds high bound %d", lo, hi)
		}
		if v < lo {
			return lo, nil
		}
		if v > hi {
			return hi, nil
		}
		return v, nil
	case "tuple":
		// Heterogeneous by construction; the static type carries the shape.
		out := make([]ir.Value, len(args))
		copy(out, args)
		return out, nil
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
	case "trim":
		s, ok := args[0].(string)
		if !ok {
			return fail("trim: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		return strings.TrimSpace(s), nil
	case "upper", "lower":
		s, ok := args[0].(string)
		if !ok {
			return fail("%s: expected Text, got %s", name, ir.DescribeValue(args[0]))
		}
		if name == "upper" {
			return strings.ToUpper(s), nil
		}
		return strings.ToLower(s), nil
	case "chars":
		s, ok := args[0].(string)
		if !ok {
			return fail("chars: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		rs := []rune(s)
		out := make([]ir.Value, len(rs))
		for i, r := range rs {
			out[i] = string(r)
		}
		return out, nil
	case "startswith", "endswith":
		s, ok1 := args[0].(string)
		sub, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return fail("%s: expected Text arguments", name)
		}
		if name == "startswith" {
			return strings.HasPrefix(s, sub), nil
		}
		return strings.HasSuffix(s, sub), nil
	case "indexof":
		// Over a List: element position. Over Text: substring position, in
		// runes so it lines up with charat/slice. -1 when absent, either way.
		if s, ok := args[0].(string); ok {
			sub, ok2 := args[1].(string)
			if !ok2 {
				return fail("indexof: expected a Text needle, got %s", ir.DescribeValue(args[1]))
			}
			b := strings.Index(s, sub)
			if b < 0 {
				return int64(-1), nil
			}
			return int64(utf8.RuneCountInString(s[:b])), nil
		}
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("indexof: %v", err)
		}
		for i, e := range xs {
			if valuesEqual(e, args[1]) {
				return int64(i), nil
			}
		}
		return int64(-1), nil
	case "replace":
		s, ok1 := args[0].(string)
		old, ok2 := args[1].(string)
		nw, ok3 := args[2].(string)
		if !ok1 || !ok2 || !ok3 {
			return fail("replace: expected Text arguments")
		}
		return strings.ReplaceAll(s, old, nw), nil
	case "charat":
		s, ok := args[0].(string)
		if !ok {
			return fail("charat: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		i, err := ir.AsInt(args[1])
		if err != nil {
			return fail("charat: %v", err)
		}
		rs := []rune(s)
		if i < 0 || i >= int64(len(rs)) {
			return fail("charat: index %d out of range (length %d)", i, len(rs))
		}
		return string(rs[i]), nil
	case "slice":
		// Half-open and clamped, like take/drop: slice never errors on a
		// range that runs off either end.
		lo, err1 := ir.AsInt(args[1])
		hi, err2 := ir.AsInt(args[2])
		if err := firstErr(err1, err2); err != nil {
			return fail("slice: %v", err)
		}
		if s, ok := args[0].(string); ok {
			rs := []rune(s)
			l, h := clampRange(lo, hi, int64(len(rs)))
			return string(rs[l:h]), nil
		}
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("slice: %v", err)
		}
		l, h := clampRange(lo, hi, int64(len(xs)))
		out := make([]ir.Value, h-l)
		copy(out, xs[l:h])
		return out, nil
	case "textjoin":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("textjoin: %v", err)
		}
		sep, ok := args[1].(string)
		if !ok {
			return fail("textjoin: separator must be Text, got %s", ir.DescribeValue(args[1]))
		}
		parts := make([]string, len(xs))
		for i, e := range xs {
			s, ok := e.(string)
			if !ok {
				return fail("textjoin: element %d is not Text", i)
			}
			parts[i] = s
		}
		return strings.Join(parts, sep), nil

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
	case "psub":
		a, err1 := ir.AsList(args[0])
		b, err2 := ir.AsList(args[1])
		if err := firstErr(err1, err2); err != nil {
			return fail("psub: %v", err)
		}
		ar, _ := ir.AsInt(a[0])
		ac, _ := ir.AsInt(a[1])
		br, _ := ir.AsInt(b[0])
		bc, _ := ir.AsInt(b[1])
		return []ir.Value{ar - br, ac - bc}, nil
	case "pscale":
		p, err := ir.AsList(args[0])
		if err != nil {
			return fail("pscale: %v", err)
		}
		n, err := ir.AsInt(args[1])
		if err != nil {
			return fail("pscale: %v", err)
		}
		pr, _ := ir.AsInt(p[0])
		pc, _ := ir.AsInt(p[1])
		return []ir.Value{pr * n, pc * n}, nil
	case "chebyshev":
		a, err1 := ir.AsList(args[0])
		b, err2 := ir.AsList(args[1])
		if err := firstErr(err1, err2); err != nil {
			return fail("chebyshev: %v", err)
		}
		ar, _ := ir.AsInt(a[0])
		ac, _ := ir.AsInt(a[1])
		br, _ := ir.AsInt(b[0])
		bc, _ := ir.AsInt(b[1])
		return max(absInt(ar-br), absInt(ac-bc)), nil
	case "dirs8":
		out := make([]ir.Value, 0, len(dirs8Deltas))
		for _, d := range dirs8Deltas {
			out = append(out, []ir.Value{d[0], d[1]})
		}
		return out, nil
	case "around4", "around8":
		// Neighbours of a point with no grid and no bounds — what a Sparse
		// automaton needs, since neighbors4/8 require a dense Grid.
		p, err := ir.AsList(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		pr, _ := ir.AsInt(p[0])
		pc, _ := ir.AsInt(p[1])
		deltas := dirs4Deltas[:]
		if name == "around8" {
			deltas = dirs8Deltas[:]
		}
		out := make([]ir.Value, 0, len(deltas))
		for _, d := range deltas {
			out = append(out, []ir.Value{pr + d[0], pc + d[1]})
		}
		return out, nil
	case "haskey":
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("haskey: expected a Map, got %s", ir.DescribeValue(args[0]))
		}
		return m.Has(args[1]), nil
	case "getor":
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("getor: expected a Map, got %s", ir.DescribeValue(args[0]))
		}
		if v, ok := m.Get(args[1]); ok {
			return v, nil
		}
		return args[2], nil
	case "keys":
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("keys: expected a Map, got %s", ir.DescribeValue(args[0]))
		}
		return append([]ir.Value(nil), m.Keys()...), nil
	case "values":
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("values: expected a Map, got %s", ir.DescribeValue(args[0]))
		}
		out := make([]ir.Value, 0, m.Len())
		for _, k := range m.Keys() {
			v, _ := m.Get(k)
			out = append(out, v)
		}
		return out, nil
	case "tolist":
		s, ok := args[0].(*ir.SetValue)
		if !ok {
			return fail("tolist: expected a Set, got %s", ir.DescribeValue(args[0]))
		}
		return append([]ir.Value(nil), s.Elems()...), nil
	case "size":
		switch x := args[0].(type) {
		case *ir.MapValue:
			return int64(x.Len()), nil
		case *ir.SetValue:
			return int64(x.Len()), nil
		}
		return fail("size: expected a Map or Set, got %s", ir.DescribeValue(args[0]))
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
		out := make([]ir.Value, 0, len(dirs4Deltas))
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
			for c := range grid.Cols {
				out[c], _ = grid.At(int(i), c)
			}
			return out, nil
		}
		if i < 0 || i >= int64(grid.Cols) {
			return fail("col: column %d out of range (grid %dx%d)", i, grid.Rows, grid.Cols)
		}
		out := make([]ir.Value, grid.Rows)
		for r := range grid.Rows {
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

	// -- collection construction, update and enumeration -------------------------
	//
	// Every one of these is *functional*: the argument is never mutated, because
	// a lambda may be applied to the same value more than once (the optimizer
	// folds constants by doing exactly that) and an in-place update would make
	// the second application see the first one's work.
	case "toset":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("toset: %v", err)
		}
		return ir.SetFromList(xs), nil
	case "emptyset":
		return ir.NewSetValue(), nil
	case "emptylist":
		return []ir.Value{}, nil
	case "emptymap":
		return ir.NewMapValue(), nil
	case "tomap":
		xs, err := ir.AsList(args[0])
		if err != nil {
			return fail("tomap: %v", err)
		}
		m := ir.NewMapSized(len(xs))
		for i, e := range xs {
			p, ok := e.([]ir.Value)
			if !ok || len(p) != 2 {
				return fail("tomap: element %d is not a (key, value) pair", i)
			}
			m.Put(p[0], p[1])
		}
		return m, nil
	case "entries":
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("entries: expected a Map, got %s", ir.DescribeValue(args[0]))
		}
		out := make([]ir.Value, 0, m.Len())
		for _, k := range m.Keys() {
			v, _ := m.Get(k)
			out = append(out, []ir.Value{k, v})
		}
		return out, nil
	case "insert":
		// x.InPlace is the optimizer's proof that nothing can read the
		// copied-from value after this call, so the copy is unobservable —
		// see optimizer/linear.go. Every branch below is its own functional
		// form minus the Clone, which is what makes the two agree.
		if s, ok := args[0].(*ir.SetValue); ok {
			if x.InPlace {
				s.Add(args[1])
				return s, nil
			}
			return s.With(args[1]), nil
		}
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("insert: expected a Set or Map, got %s", ir.DescribeValue(args[0]))
		}
		if x.InPlace {
			m.Put(args[1], args[2])
			return m, nil
		}
		return m.With(args[1], args[2]), nil
	case "del":
		if s, ok := args[0].(*ir.SetValue); ok {
			return s.Without(args[1]), nil
		}
		m, ok := args[0].(*ir.MapValue)
		if !ok {
			return fail("del: expected a Set or Map, got %s", ir.DescribeValue(args[0]))
		}
		return m.Without(args[1]), nil
	case "union", "intersect", "difference":
		a, ok1 := args[0].(*ir.SetValue)
		b, ok2 := args[1].(*ir.SetValue)
		if !ok1 || !ok2 {
			return fail("%s: expected two Sets", name)
		}
		switch name {
		case "union":
			return ir.SetUnion(a, b), nil
		case "intersect":
			return ir.SetIntersect(a, b), nil
		}
		return ir.SetDifference(a, b), nil
	case "setat":
		grid, ok := args[0].(*ir.GridValue)
		if !ok {
			return fail("setat: expected a Grid, got %s", ir.DescribeValue(args[0]))
		}
		r, err1 := ir.AsInt(args[1])
		c, err2 := ir.AsInt(args[2])
		if err := firstErr(err1, err2); err != nil {
			return fail("setat: %v", err)
		}
		if !grid.InBounds(int(r), int(c)) {
			return fail("setat: position (%d, %d) out of range (grid %dx%d)", r, c, grid.Rows, grid.Cols)
		}
		if x.InPlace {
			grid.SetAt(int(r), int(c), args[3])
			return grid, nil
		}
		return grid.With(int(r), int(c), args[3]), nil
	case "cellpoints":
		sp, ok := args[0].(*ir.SparseValue)
		if !ok {
			return fail("cellpoints: expected a Sparse grid, got %s", ir.DescribeValue(args[0]))
		}
		pts := sp.Points()
		out := make([]ir.Value, len(pts))
		for i, p := range pts {
			out[i] = []ir.Value{p[0], p[1]}
		}
		return out, nil

	// -- list generation ---------------------------------------------------------
	case "range":
		lo, hi, err := twoInts(args, "range")
		if err != nil {
			return fail("%v", err)
		}
		out, err := intRange(lo, hi)
		if err != nil {
			return fail("%v", err)
		}
		return out, nil
	case "fill":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("fill: %v", err)
		}
		// Total, like take and drop: a negative count is the empty list rather
		// than an error, because there is a sensible answer to give.
		if n <= 0 {
			return []ir.Value{}, nil
		}
		if n > buildLimit() {
			return fail("fill: %d elements is more than can be built", n)
		}
		out := make([]ir.Value, n)
		for i := range out {
			out[i] = args[1]
		}
		return out, nil

	// -- text --------------------------------------------------------------------
	case "split":
		s, sep, err := twoTexts(args, "split")
		if err != nil {
			return fail("%v", err)
		}
		return textList(strings.Split(s, sep)), nil
	case "words":
		s, ok := args[0].(string)
		if !ok {
			return fail("words: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		return textList(strings.Fields(s)), nil
	case "ord":
		s, ok := args[0].(string)
		if !ok {
			return fail("ord: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		if s == "" {
			return fail("ord of the empty text is undefined")
		}
		r, _ := utf8.DecodeRuneInString(s)
		return int64(r), nil
	case "chr":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("chr: %v", err)
		}
		if n < 0 || n > utf8.MaxRune || (n >= 0xD800 && n <= 0xDFFF) {
			return fail("chr: %d is not a character code", n)
		}
		return string(rune(n)), nil
	case "repeat":
		s, ok := args[0].(string)
		if !ok {
			return fail("repeat: expected Text, got %s", ir.DescribeValue(args[0]))
		}
		n, err := ir.AsInt(args[1])
		if err != nil {
			return fail("repeat: %v", err)
		}
		if n <= 0 || s == "" {
			return "", nil
		}
		if n > buildLimit()/int64(len(s)) {
			return fail("repeat: %d copies of a %d-byte text is more than can be built", n, len(s))
		}
		return strings.Repeat(s, int(n)), nil
	case "padleft", "padright":
		s, ok := args[0].(string)
		if !ok {
			return fail("%s: expected Text, got %s", name, ir.DescribeValue(args[0]))
		}
		width, err := ir.AsInt(args[1])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		padding, ok := args[2].(string)
		if !ok {
			return fail("%s: pad must be Text, got %s", name, ir.DescribeValue(args[2]))
		}
		out, err := padText(s, width, padding, name == "padleft")
		if err != nil {
			return fail("%s: %v", name, err)
		}
		return out, nil
	case "trimprefix", "trimsuffix":
		s, p, err := twoTexts(args, name)
		if err != nil {
			return fail("%v", err)
		}
		if name == "trimprefix" {
			return strings.TrimPrefix(s, p), nil
		}
		return strings.TrimSuffix(s, p), nil
	case "isdigit", "isalpha", "isupper", "islower":
		s, ok := args[0].(string)
		if !ok {
			return fail("%s: expected Text, got %s", name, ir.DescribeValue(args[0]))
		}
		return classify(name, s), nil

	// -- floats ------------------------------------------------------------------
	case "log", "log2", "log10", "exp", "sin", "cos", "tan":
		f, err := ir.AsFloat(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		r, err := float1(name, f)
		if err != nil {
			return fail("%v", err)
		}
		return r, nil
	case "atan2", "hypot":
		a, err1 := ir.AsFloat(args[0])
		b, err2 := ir.AsFloat(args[1])
		if err := firstErr(err1, err2); err != nil {
			return fail("%s: %v", name, err)
		}
		if name == "atan2" {
			return math.Atan2(a, b), nil
		}
		return math.Hypot(a, b), nil
	case "trunc":
		if n, ok := args[0].(int64); ok {
			return n, nil
		}
		f, err := ir.AsFloat(args[0])
		if err != nil {
			return fail("trunc: %v", err)
		}
		return int64(math.Trunc(f)), nil

	// -- records -----------------------------------------------------------------
	case "record":
		rec := ir.NewRecordValueSized(len(args) / 2)
		for i := 0; i+1 < len(args); i += 2 {
			field, ok := args[i].(string)
			if !ok {
				return fail("record: field name %d is not Text", i/2+1)
			}
			rec.Set(field, args[i+1])
		}
		return rec, nil
	case "with":
		rec, ok := args[0].(*ir.RecordValue)
		if !ok {
			return fail("with: expected a Record, got %s", ir.DescribeValue(args[0]))
		}
		field, ok := args[1].(string)
		if !ok {
			return fail("with: field name is not Text")
		}
		if _, ok := rec.Get(field); !ok {
			return fail("with: record has no field %q", field)
		}
		return rec.With(field, args[2]), nil

	// -- bases, bits, number theory ----------------------------------------------
	case "frombase", "fromhex":
		s, ok := args[0].(string)
		if !ok {
			return fail("%s: expected Text, got %s", name, ir.DescribeValue(args[0]))
		}
		base := int64(16)
		if name == "frombase" {
			var err error
			if base, err = ir.AsInt(args[1]); err != nil {
				return fail("frombase: %v", err)
			}
		}
		n, err := parseBase(name, s, base)
		if err != nil {
			return fail("%v", err)
		}
		return n, nil
	case "tobase", "tohex", "tobin":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("%s: %v", name, err)
		}
		base := int64(16)
		switch name {
		case "tobin":
			base = 2
		case "tobase":
			if base, err = ir.AsInt(args[1]); err != nil {
				return fail("tobase: %v", err)
			}
			if base < 2 || base > 36 {
				return fail("tobase: base must be between 2 and 36, got %d", base)
			}
		}
		return strconv.FormatInt(n, int(base)), nil
	case "bnot":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("bnot: %v", err)
		}
		return ^n, nil
	case "popcount":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("popcount: %v", err)
		}
		return int64(bits.OnesCount64(uint64(n))), nil
	case "testbit":
		n, i, err := twoInts(args, "testbit")
		if err != nil {
			return fail("%v", err)
		}
		if i < 0 || i > 63 {
			return fail("testbit: bit %d is outside an Int (0 to 63)", i)
		}
		return n&(1<<uint(i)) != 0, nil
	case "digits":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("digits: %v", err)
		}
		return decimalDigits(n), nil
	case "fromdigits":
		ds, err := ir.AsIntSlice(args[0])
		if err != nil {
			return fail("fromdigits: %v", err)
		}
		n, err := fromDigits(ds)
		if err != nil {
			return fail("%v", err)
		}
		return n, nil
	case "isprime":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("isprime: %v", err)
		}
		return isPrime(n), nil
	case "divisors":
		n, err := ir.AsInt(args[0])
		if err != nil {
			return fail("divisors: %v", err)
		}
		out, err := divisorsOf(n)
		if err != nil {
			return fail("%v", err)
		}
		return out, nil
	case "crt":
		rs, err1 := ir.AsIntSlice(args[0])
		ms, err2 := ir.AsIntSlice(args[1])
		if err := firstErr(err1, err2); err != nil {
			return fail("crt: %v", err)
		}
		n, err := crtSolve(rs, ms)
		if err != nil {
			return fail("%v", err)
		}
		return n, nil

	default:
		return fail("unknown function %q", name)
	}
}

// dirs4Deltas are the four orthogonal direction vectors (up, down, left,
// right), in the same order GridValue.Neighbors visits them.
var dirs4Deltas = [4][2]int64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}

// dirs8Deltas adds the diagonals, in reading order. dirs8 and around8 both
// emit exactly this order, so it lives in one place.
var dirs8Deltas = [8][2]int64{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}

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

// clampRange normalizes a half-open [lo, hi) against a length, the way take
// and drop clamp their counts: out-of-range bounds narrow to the collection
// instead of erroring, and an inverted range yields empty.
func clampRange(lo, hi, n int64) (int64, int64) {
	lo = min(max(lo, 0), n)
	return lo, min(max(hi, lo), n)
}

func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// euclidMod is Euclidean remainder: the result always has the sign of the
// divisor's magnitude — it is non-negative for a positive modulus, whatever
// the sign of a. That is deliberately *not* Go's `%`, because the dominant
// use is wrap-around indexing (`mod(i - 1, length(xs))`), where truncated
// remainder gives a negative index at exactly the interesting boundary.
func euclidMod(a, b int64) (int64, error) {
	if b == 0 {
		return 0, fmt.Errorf("mod by zero")
	}
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r, nil
}

// euclidDiv is the quotient matching euclidMod, so that
// euclidDiv(a,b)*b + euclidMod(a,b) == a for every a and every non-zero b.
func euclidDiv(a, b int64) (int64, error) {
	m, err := euclidMod(a, b)
	if err != nil {
		return 0, err
	}
	return (a - m) / b, nil
}

// powInt is integer exponentiation by squaring. A negative exponent is an
// error rather than a silent 0: Domain has no rationals, so there is no
// answer to hand back.
func powInt(b, e int64) (int64, error) {
	if e < 0 {
		return 0, fmt.Errorf("pow: exponent must be non-negative, got %d", e)
	}
	r := int64(1)
	for e > 0 {
		if e&1 == 1 {
			r *= b
		}
		b *= b
		e >>= 1
	}
	return r, nil
}

// isqrtInt is the integer square root: the largest n with n*n <= x. Newton's
// method on int64, so it never rounds the way float sqrt does near a perfect
// square — isqrt(k*k) is exactly k for every k in range.
func isqrtInt(x int64) (int64, error) {
	if x < 0 {
		return 0, fmt.Errorf("isqrt: negative input %d", x)
	}
	if x < 2 {
		return x, nil
	}
	n := x
	g := x/2 + 1
	for g < n {
		n = g
		g = (g + x/g) / 2
	}
	return n, nil
}

// factorialInt errors on overflow rather than wrapping: 21! exceeds int64,
// and a silently wrapped factorial is a wrong answer that looks like a right
// one.
func factorialInt(n int64) (int64, error) {
	if n < 0 {
		return 0, fmt.Errorf("factorial: negative input %d", n)
	}
	if n > 20 {
		return 0, fmt.Errorf("factorial: %d! overflows Int (max is 20!)", n)
	}
	r := int64(1)
	for i := int64(2); i <= n; i++ {
		r *= i
	}
	return r, nil
}

// chooseInt is the binomial coefficient, computed by a multiplicative loop
// that divides at every step, so it stays in range far past where factorial
// would overflow.
func chooseInt(n, k int64) (int64, error) {
	if n < 0 {
		return 0, fmt.Errorf("choose: negative n %d", n)
	}
	if k < 0 || k > n {
		return 0, nil
	}
	if k > n-k {
		k = n - k
	}
	// After step i the running value is C(n-k+i, i), always an integer, so
	// the division is exact at every step and nothing is lost to truncation.
	r := int64(1)
	for i := int64(1); i <= k; i++ {
		r = r * (n - k + i) / i
	}
	return r, nil
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

// assignTo writes v to the name x updates. The environment is searched first
// (a `consider` local, or a stage binding seeded into this application's
// environment), and the binding stack second (a stage binding whose scope
// opened outside the expression being evaluated — a binding's own value, say).
//
// Writing through the environment's box is what reaches the *right* one when a
// name is bound twice: the innermost scope put its own cell in the map, so the
// map lookup finds that one and the outer binding is untouched.
func assignTo(x *ast.AssignExpr, env Env, v ir.Value) error {
	if cur, ok := env[x.Name]; ok {
		c, ok := cur.(*Cell)
		if !ok {
			// Reachable only if a name was bound without a cell while updates
			// were off — i.e. the resolver did not see the := that is running.
			return fmt.Errorf("%s: %q cannot be updated here", x.Pos, x.Name)
		}
		c.V = v
		return nil
	}
	if assign(x.Name, v) {
		return nil
	}
	return fmt.Errorf("%s: unknown identifier %q", x.Pos, x.Name)
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
	// Reported before the body runs, so a watcher sees the application even if
	// evaluating it is what fails. Never during a trace: TraceLambda's whole
	// job is re-running an application a watcher already saw.
	//
	// The slices are *copied* rather than handed over, and that is load-bearing
	// for a run with no watcher at all: passing one to a function variable
	// makes escape analysis give up on it, and both of these are built fresh at
	// call sites that otherwise keep them on the stack. Twelve thousand
	// applications would become twenty-four thousand heap allocations for a
	// watcher nobody installed (BenchmarkTraceUntraced pins that). Copying also
	// means a watcher may keep what it is given, which is what the recorder
	// needs anyway.
	if watching != nil && tracing == nil {
		types := make([]*ir.Type, len(paramTypes))
		copy(types, paramTypes)
		vals := make([]ir.Value, len(args))
		copy(vals, args)
		watching(l, types, vals)
	}
	// Bindings in scope are seeded first and parameters second, so a parameter
	// of the same name shadows the binding (see eval/bindings.go). The length
	// check keeps an application that uses none exactly as cheap as before.
	env := make(Env, len(l.Params)+len(bindings))
	var bindTypes typecheck.Env
	if len(bindings) > 0 {
		bindTypes = make(typecheck.Env, len(bindings))
		for _, b := range bindings {
			env[b.name] = b.bindValue()
			bindTypes[b.name] = b.typ
		}
	}
	for i, p := range l.Params {
		env[p] = args[i]
	}
	var types typecheck.Env
	if len(paramTypes) == len(l.Params) {
		types = make(typecheck.Env, len(l.Params)+len(bindTypes))
		maps.Copy(types, bindTypes)
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
		// `+` over two Texts is concatenation.
		if x.Op == token.PLUS {
			if ls, ok := lv.(string); ok {
				rs, ok := rv.(string)
				if !ok {
					return nil, fmt.Errorf("%s: cannot add Text and %s", x.Pos, ir.DescribeValue(rv))
				}
				return ls + rs, nil
			}
		}
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
	case token.PERCENT:
		a, err := ir.AsInt(lv)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.Pos, err)
		}
		b, err := ir.AsInt(rv)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", x.Pos, err)
		}
		r, err := euclidMod(a, b)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", x.Pos, err)
		}
		return r, nil
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
		if a, ok := lv.(int64); ok {
			if b, ok := rv.(int64); ok {
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
		}
		// Text and tuples order through ir.Compare — the same ordering Sort
		// and Sort By use, so `a < b` in a lambda and a Sort of the same
		// values agree on what "before" means.
		return compareOrdered(x.Op, lv, rv, x.Pos)
	}
	return nil, fmt.Errorf("%s: unsupported operator %s", x.Pos, x.Op)
}

// compareOrdered applies a relational operator to two values the resolver has
// typed as one Ordered type. It guards the runtime shapes itself rather than
// trusting that: ir.Compare answers 0 for anything it does not recognize, so
// that a sort stays stable instead of panicking mid-run, and silently
// reporting "equal" is the wrong answer for an operator.
func compareOrdered(op token.Kind, lv, rv ir.Value, pos token.Position) (ir.Value, error) {
	for _, v := range []ir.Value{lv, rv} {
		switch v.(type) {
		case int64, float64, string, []ir.Value:
		default:
			return nil, fmt.Errorf("%s: %s has no ordering, so it cannot be compared",
				pos, ir.DescribeValue(v))
		}
	}
	c := ir.Compare(lv, rv)
	switch op {
	case token.LT:
		return c < 0, nil
	case token.GT:
		return c > 0, nil
	case token.LE:
		return c <= 0, nil
	case token.GE:
		return c >= 0, nil
	}
	return nil, fmt.Errorf("%s: unsupported operator %s", pos, op)
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
