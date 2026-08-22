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
	"maps"
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
	case *ast.GlobalRef:
		// A global's type is fixed where it was declared and travels on the
		// node, so there is no second slot table here to fall out of step with
		// the resolver's. The environment is not consulted at all, which is
		// also why a global needs no seeding into it.
		return x.Type, nil
	case *ast.BlockBody:
		// A sub-pipeline standing in for an expression: its result type is
		// whatever the body resolves to against the type bound to the
		// parameter it reads. Which parameter that is comes from the node
		// itself, so a block works in whichever slot the primitive binds.
		in, ok := env[x.Param]
		if !ok {
			return nil, fmt.Errorf("%s: block body has no input binding", x.Pos)
		}
		// A lambda of two or more parameters has one the body is *over*; the
		// rest are in scope by name for every expression in it, through the
		// binding stack `Consider` already uses.
		for _, b := range x.Extra {
			t, ok := env[b.Param]
			if !ok {
				return nil, fmt.Errorf("%s: block body has no binding for %q", x.Pos, b.Name)
			}
			PushBinding(b.Name, t)
		}
		out, err := x.Pipe.BindBlock(in)
		PopBindings(len(x.Extra))
		return out, err
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
	case *ast.LetExpr:
		return letType(x, env)
	case *ast.AssignExpr:
		return assignType(x, env)
	case *ast.AlsoExpr:
		return alsoType(x, env)
	default:
		return nil, fmt.Errorf("unsupported expression %T", e)
	}
}

// assignType types `n := v`: the name must be in scope, and the value must
// have exactly the type that name holds. Which *kinds* of name can be written
// to is settled elsewhere — the parser refuses a write to a lambda parameter
// and the resolver refuses one to a function binding or a Shikigami parameter,
// each where the thing that makes it impossible is visible.
//
// The type may not change, and the rule is deliberately stricter than the
// numeric tower's: a binding's type is fixed when its scope opens and is what
// every other expression in that scope was typed against, so an Int binding
// that took a Float halfway through a stage would make the *reader* of the
// name wrong rather than the writer. Widen at the binding instead.
func assignType(x *ast.AssignExpr, env Env) (*ir.Type, error) {
	// A global's type came from its declaration rather than from the scope the
	// write is written in; everything else is looked up by name. The rule
	// below — the type may not change — is the same one either way.
	var want *ir.Type
	if x.Target != nil {
		want = x.Target.Type
	} else {
		t, ok := env[x.Name]
		if !ok {
			return nil, fmt.Errorf("%s: unknown identifier %q", x.Pos, x.Name)
		}
		want = t
	}
	got, err := ExprType(x.Value, env)
	if err != nil {
		return nil, err
	}
	if !got.Equal(want) {
		return nil, fmt.Errorf("%s: %q holds %s, so := cannot write %s to it", x.Pos, x.Name, want, got)
	}
	return want, nil
}

// alsoType types `body also c1, c2`: every clause must typecheck, none of them
// contributes to the result, and the result is the body's type.
func alsoType(x *ast.AlsoExpr, env Env) (*ir.Type, error) {
	bt, err := ExprType(x.Body, env)
	if err != nil {
		return nil, err
	}
	for _, c := range x.Clauses {
		if _, err := ExprType(c, env); err != nil {
			return nil, err
		}
	}
	return bt, nil
}

// letType types `consider n as v in body`: the body is typed in an
// environment extended with n. The binding shadows an outer name of the same
// spelling and is restored afterwards, so sibling expressions are unaffected.
func letType(x *ast.LetExpr, env Env) (*ir.Type, error) {
	vt, err := ExprType(x.Value, env)
	if err != nil {
		return nil, err
	}
	inner := make(Env, len(env)+1)
	maps.Copy(inner, env)
	inner[x.Name] = vt
	return ExprType(x.Body, inner)
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
	"abs", "at", "band", "bor", "bxor", "ceil", "cells", "choose", "clamp",
	"col", "cols", "concat", "contains", "dirs4", "divmod", "drop",
	"factorial", "first", "floor", "frombin", "gcd", "get", "has", "inbounds",
	"isqrt", "item", "last", "lcm", "length", "list", "manhattan", "max",
	"maxcol", "maxrow", "min", "mincol", "minrow", "mod", "modinv", "modpow",
	"neighbors4", "neighbors8", "occurrences", "padd", "pcol", "point", "pow",
	"prow", "put", "repeats", "reverse", "rotl", "rotr", "round", "row",
	"rows", "set", "shl", "shr", "sign", "solve2x2", "sparse", "sqrt", "sum",
	"take", "textjoin", "tofloat", "toint", "totext", "trim", "tuple",
	"upper", "lower", "slice", "charat", "chars", "indexof", "startswith",
	"endswith", "replace",
	"psub", "pscale", "chebyshev", "dirs8", "around4", "around8",
	"haskey", "getor", "keys", "values", "size", "tolist",
	// v0.6: the collections stop being read-only. Sparse was the only kind
	// with a constructor and a functional update, which is why a sparse
	// automaton was writable as a Fold and a frequency map was not.
	"toset", "tomap", "entries", "insert", "del", "union", "intersect",
	"difference", "emptyset", "emptymap", "emptylist", "setat", "cellpoints",
	// v0.6: list generation. Every list used to have to arrive from outside
	// the expression.
	"range", "fill",
	// v0.6: text splitting, codepoints, padding and classification.
	"split", "words", "ord", "chr", "repeat",
	"padleft", "padright", "trimprefix", "trimsuffix",
	"isdigit", "isalpha", "isupper", "islower",
	// v0.6: the float tower past sqrt.
	"log", "log2", "log10", "exp", "sin", "cos", "tan", "atan2", "hypot", "trunc",
	// v0.6: named-field construction and update.
	"record", "with",
	// v0.7: bitwise reducers, and the logical connectives as functions.
	"bandall", "borall", "bxorall",
	"and", "or", "xor", "not",
	// v0.6: bases, bits and number theory.
	"frombase", "tobase", "fromhex", "tohex", "tobin",
	"bnot", "popcount", "testbit", "digits", "fromdigits",
	"isprime", "divisors", "crt",
	// The first-order list operations. None takes a function argument, so
	// none of them is a higher-order builtin — they were simply absent, and
	// each absence forced a nested pipeline body where an expression would
	// have done. Inside a Fold, where a body cannot stand in for a
	// 2-parameter lambda at all, the absence was total.
	"sort", "unique", "flatten", "product", "zip", "enumerate",
	"chunk", "windows", "transpose",
	// Graph<K>: the explicit graph gets a value, so the three vocabularies
	// that described one — Grid geometry, Explore's implicit state space,
	// Topological Sort's adjacency map — stop being the only ways to ask.
	"graph", "emptygraph", "addnode", "addedge", "deledge",
	"nodes", "edges", "neighbors", "edgesof", "hasedge",
	"weight", "weightor", "degree", "flipedges", "subgraph",
	// The two questions an edge list parsed out of text asks that the readers
	// above could not: which node is the top of it, and what a node's arcs
	// weigh in total.
	"root", "weightof",
	// The rest of the node-level vocabulary: the total twin of root and its
	// mirror, the arcs coming in, a node-level delete to match deledge, and the
	// four whole-graph questions an edge list makes worth asking.
	"roots", "leaves", "indegree", "delnode", "reachable",
	"hascycle", "undirected", "mergegraphs", "weightsum",
}

// PointType is the expression-layer representation of a 2D point: an
// (Int, Int) tuple of (row, col), matching the grid coordinate system.
func PointType() *ir.Type { return ir.Tuple(ir.Int(), ir.Int()) }

// builtinArity is each builtin's argument count; -1 means variadic, with the
// permitted range in variadicArity.
var builtinArity = map[string]int{
	"length": 1, "item": 2, "take": 2, "drop": 2, "reverse": 1,
	"concat": 2, "first": 1, "last": 1, "sum": 1,
	"min": -1, "max": -1, // 1 = list reduction, 2 = scalar form
	"contains": 2, "get": 2, "at": 3,
	// math / number theory
	"abs": 1, "sign": 1, "gcd": 2, "lcm": 2, "modpow": 3, "modinv": 2,
	"solve2x2": 6,
	"mod":      2, "divmod": 2, "pow": 2, "isqrt": 1, "clamp": 3,
	"factorial": 1, "choose": 2,
	// heterogeneous tuple construction
	"tuple": -1, // variadic, >= 2
	// text
	"toint": 1, "occurrences": 2, "repeats": 1, "totext": 1,
	"slice": 3, "charat": 2, "chars": 1, "indexof": 2,
	"startswith": 2, "endswith": 2, "replace": 3, "trim": 1,
	"upper": 1, "lower": 1, "textjoin": 2,
	// floats (H)
	"tofloat": 1, "floor": 1, "ceil": 1, "round": 1, "sqrt": 1,
	// points (tuples of (row, col)) and grid geometry
	"point": 2, "prow": 1, "pcol": 1, "padd": 2, "manhattan": 2,
	"rotl": 1, "rotr": 1, "dirs4": 0, "dirs8": 0,
	"psub": 2, "pscale": 2, "chebyshev": 2, "around4": 1, "around8": 1,
	// map / set escape hatches
	"haskey": 2, "getor": 3, "keys": 1, "values": 1, "size": 1, "tolist": 1,
	"inbounds": 3, "neighbors4": 3, "neighbors8": 3,
	// list/grid construction and access (A.2)
	"set": 3, "row": 2, "col": 2, "rows": 1, "cols": 1,
	"list": -1, // variadic, >= 1
	// sparse grids (H)
	"sparse": 1, "put": 4, "has": 3, "cells": 1,
	"minrow": 1, "maxrow": 1, "mincol": 1, "maxcol": 1,
	// bit operations (2021 D3 and friends)
	"band": 2, "bor": 2, "bxor": 2, "shl": 2, "shr": 2, "frombin": 1,
	// collection construction, update and enumeration
	"toset": 1, "tomap": 1, "entries": 1,
	// first-order list operations
	"sort": 1, "unique": 1, "flatten": 1, "product": 1, "zip": 2,
	"enumerate": 1, "chunk": 2, "windows": 2, "transpose": 1,
	// graph. addedge is variadic: 3 arguments weigh the arc 1, 4 name a weight.
	"graph": 1, "emptygraph": 1, "addnode": 2, "addedge": -1, "deledge": 3,
	"nodes": 1, "edges": 1, "neighbors": 2, "edgesof": 2, "hasedge": 3,
	"weight": 3, "weightor": 4, "degree": 2, "flipedges": 1, "subgraph": 2,
	"root": 1, "weightof": 2,
	"roots": 1, "leaves": 1, "indegree": 2, "delnode": 2, "reachable": 2,
	"hascycle": 1, "undirected": 1, "mergegraphs": 2, "weightsum": 1,
	"insert": -1, // 2 over a Set, 3 over a Map
	"del":    2,  // Set × elem, or Map × key
	"union":  2, "intersect": 2, "difference": 2,
	"emptyset": 1, "emptymap": 2, "emptylist": 1, "setat": 4, "cellpoints": 1,
	"bandall": 1, "borall": 1, "bxorall": 1,
	"and": 2, "or": 2, "xor": 2, "not": 1,
	// list generation
	"range": 2, "fill": 2,
	// text
	"split": 2, "words": 1, "ord": 1, "chr": 1, "repeat": 2,
	"padleft": 3, "padright": 3, "trimprefix": 2, "trimsuffix": 2,
	"isdigit": 1, "isalpha": 1, "isupper": 1, "islower": 1,
	// floats
	"log": 1, "log2": 1, "log10": 1, "exp": 1,
	"sin": 1, "cos": 1, "tan": 1, "atan2": 2, "hypot": 2, "trunc": 1,
	// records
	"record": -1, // variadic, >= 2 and even: name, value, name, value, …
	"with":   3,
	// bases, bits, number theory
	"frombase": 2, "tobase": 2, "fromhex": 1, "tohex": 1, "tobin": 1,
	"bnot": 1, "popcount": 1, "testbit": 2, "digits": 1, "fromdigits": 1,
	"isprime": 1, "divisors": 1, "crt": 2,
}

// variadicArity bounds the builtins whose builtinArity is -1, as {min, max}
// with -1 for unbounded. `tuple` needs two (a 1-tuple is just the value);
// `min`/`max` are either the list reduction (1) or the two-scalar form;
// `insert` is the Set form (2) or the Map form (3); `record` takes name/value
// pairs, so it also has to be *even* — checked in its own typing rule, where
// the message can say so.
var variadicArity = map[string][2]int{
	"list": {1, -1}, "tuple": {2, -1}, "min": {1, 2}, "max": {1, 2},
	"insert": {2, 3}, "record": {2, -1},
	// addedge weighs the arc 1 when no weight is given.
	"addedge": {3, 4},
}

// CheckArity reports whether the builtin name accepts n arguments, wording the
// error the one way both layers say it. The evaluator asks too: it indexes
// args[0], args[1], … positionally, and it is not always reached through the
// type checker — constant folding (prims.foldLiteral) evaluates an expression
// *before* it is typed, so a miscounted call would panic there rather than be
// reported. An unknown name is not this function's business (each caller words
// that differently), so it passes.
func CheckArity(name string, n int) error {
	want, known := builtinArity[name]
	if !known {
		return nil
	}
	if want == -1 {
		rng := variadicArity[name]
		if n < rng[0] || (rng[1] != -1 && n > rng[1]) {
			if rng[1] == -1 {
				return fmt.Errorf("%s takes at least %d argument(s), got %d", name, rng[0], n)
			}
			return fmt.Errorf("%s takes %d or %d argument(s), got %d", name, rng[0], rng[1], n)
		}
		return nil
	}
	if n != want {
		return fmt.Errorf("%s takes %d argument(s), got %d", name, want, n)
	}
	return nil
}

// callType types a builtin call. Several builtins are polymorphic in the
// list element type, so the rules pattern-match on the argument types.
func callType(x *ast.CallExpr, env Env) (*ir.Type, error) {
	id, ok := x.Fn.(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("%s: only builtin functions can be called", x.Pos)
	}
	name := id.Name

	if _, known := builtinArity[name]; !known {
		return nil, fmt.Errorf("%s: unknown function %q (builtins: %s)",
			x.Pos, name, strings.Join(Builtins, ", "))
	}
	if err := CheckArity(name, len(x.Args)); err != nil {
		return nil, fmt.Errorf("%s: %v", x.Pos, err)
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
		// Text counts runes, matching `Split Text by ""` and charat/slice, so
		// the two layers agree about what position 3 means.
		if args[0] != nil && (args[0].Kind == ir.KText || args[0].Kind == ir.KTuple) {
			return ir.Int(), nil
		}
		if _, err := needList(0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "item":
		// Over a Tuple, `item` is the general element accessor (prow/pcol are
		// the (Int, Int) special case). The index must be a literal: a tuple's
		// elements have different types, so the result type is only knowable
		// statically when the position is.
		if args[0] != nil && args[0].Kind == ir.KTuple {
			lit, ok := x.Args[1].(*ast.IntLit)
			if !ok {
				return nil, fmt.Errorf("%s: item over a Tuple needs a literal index (its elements have different types)", x.Pos)
			}
			if lit.Value < 0 || lit.Value >= int64(len(args[0].Elems)) {
				return nil, fmt.Errorf("%s: item index %d out of range for %s", x.Pos, lit.Value, args[0])
			}
			return args[0].Elems[lit.Value], nil
		}
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
	case "sort":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		// The same reach as the Sort primitive and the relational operators:
		// ir.Ordered, over ir.Compare. One ordering, however it is spelled.
		if !ir.Ordered(elem) {
			return nil, fmt.Errorf("%s: sort needs an ordered element type "+
				"(Int, Float, Text, or a Tuple of them), got %s", x.Pos, elem)
		}
		return args[0], nil
	case "unique":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if !ir.Keyable(elem) {
			return nil, fmt.Errorf("%s: unique needs keyable elements "+
				"(Int, Text, or Tuples/Records of them), got %s", x.Pos, elem)
		}
		return args[0], nil
	case "flatten", "transpose":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if elem == nil || elem.Kind != ir.KList {
			return nil, fmt.Errorf("%s: %s needs a List<List<T>>, got %s", x.Pos, name, args[0])
		}
		if name == "flatten" {
			return elem, nil
		}
		return args[0], nil
	case "bandall", "borall", "bxorall":
		// The bitwise counterparts of sum/product: one reducer per operator,
		// Int-only because a bit pattern is what they are about.
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if elem.Kind != ir.KInt {
			return nil, fmt.Errorf("%s: %s needs Int elements, got %s", x.Pos, name, elem)
		}
		return ir.Int(), nil
	case "and", "or", "xor":
		for i := range args {
			if !args[i].Equal(ir.Bool()) {
				return nil, fmt.Errorf("%s: %s needs Bool arguments, got %s", x.Pos, name, args[i])
			}
		}
		return ir.Bool(), nil
	case "not":
		if !args[0].Equal(ir.Bool()) {
			return nil, fmt.Errorf("%s: not needs a Bool argument, got %s", x.Pos, args[0])
		}
		return ir.Bool(), nil
	case "product":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if !numeric(elem) {
			return nil, fmt.Errorf("%s: product needs Int or Float elements, got %s", x.Pos, elem)
		}
		return elem, nil
	case "zip":
		a, err := needList(0)
		if err != nil {
			return nil, err
		}
		b, err := needList(1)
		if err != nil {
			return nil, err
		}
		return ir.List(ir.Tuple(a, b)), nil
	case "enumerate":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		return ir.List(ir.Tuple(ir.Int(), elem)), nil
	case "chunk", "windows":
		if _, err := needList(0); err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return ir.List(args[0]), nil
	case "reverse":
		if args[0] != nil && args[0].Kind == ir.KText {
			return ir.Text(), nil
		}
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
		// Two-argument min/max is the scalar form: min(a, b). One argument is
		// the list reduction.
		if len(args) == 2 {
			if !numeric(args[0]) || !numeric(args[1]) {
				return nil, fmt.Errorf("%s: %s of two values needs Int or Float, got %s and %s",
					x.Pos, name, args[0], args[1])
			}
			return promote(args[0], args[1]), nil
		}
		if args[0] == nil || args[0].Kind != ir.KList || !numeric(args[0].Elem) {
			return nil, fmt.Errorf("%s: %s needs List<Int> or List<Float>, got %s", x.Pos, name, args[0])
		}
		return args[0].Elem, nil
	case "contains":
		// Over Text it is the substring test. `indexof(s, sub) >= 0` said the
		// same thing, but a membership question should read the same whatever
		// it is asked of.
		if args[0] != nil && args[0].Kind == ir.KText {
			if err := needText(x, name, args, 1); err != nil {
				return nil, err
			}
			return ir.Bool(), nil
		}
		if args[0] == nil || (args[0].Kind != ir.KList && args[0].Kind != ir.KSet && args[0].Kind != ir.KGraph) {
			return nil, fmt.Errorf("%s: contains needs a Text, List, Set or Graph argument, got %s", x.Pos, args[0])
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
	case "gcd", "lcm", "modinv", "mod", "choose":
		for i := range 2 {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "pow":
		// The one builtin that follows the operators' promotion rule rather
		// than staying integral: `pow(2, 10)` is an Int, `pow(x, 0.5)` is the
		// square root it looks like. Int × Int was the whole of it before, so
		// nothing that used to typecheck changes meaning.
		for i := range 2 {
			if !numeric(args[i]) {
				return nil, fmt.Errorf("%s: pow needs Int or Float, got %s", x.Pos, args[i])
			}
		}
		return promote(args[0], args[1]), nil
	case "divmod":
		for i := range 2 {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return PointType(), nil // (quotient, remainder) — an (Int, Int) pair
	case "isqrt", "factorial":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "clamp":
		// Polymorphic over the numeric tower, like the arithmetic operators:
		// all three arguments promote together.
		for i := range 3 {
			if !numeric(args[i]) {
				return nil, fmt.Errorf("%s: clamp needs Int or Float arguments, got %s", x.Pos, args[i])
			}
		}
		return promote(promote(args[0], args[1]), args[2]), nil
	case "modpow":
		for i := range 3 {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "solve2x2":
		for i := range 6 {
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
		for i := range 2 {
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
	case "trim", "upper", "lower", "chars":
		if !args[0].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: %s needs Text, got %s", x.Pos, name, args[0])
		}
		if name == "chars" {
			return ir.List(ir.Text()), nil
		}
		return ir.Text(), nil
	case "startswith", "endswith":
		for i := range 2 {
			if !args[i].Equal(ir.Text()) {
				return nil, fmt.Errorf("%s: %s argument %d must be Text, got %s", x.Pos, name, i+1, args[i])
			}
		}
		return ir.Bool(), nil
	case "indexof":
		// Over a List this is "position of an element"; over Text, "position
		// of a substring". Both answer -1 when absent, like Find Index.
		if args[0] != nil && args[0].Kind == ir.KList {
			if !ir.Keyable(args[0].Elem) {
				return nil, fmt.Errorf("%s: indexof needs keyable elements, got %s", x.Pos, args[0].Elem)
			}
			if !args[1].Equal(args[0].Elem) {
				return nil, fmt.Errorf("%s: indexof value must be %s, got %s", x.Pos, args[0].Elem, args[1])
			}
			return ir.Int(), nil
		}
		for i := range 2 {
			if !args[i].Equal(ir.Text()) {
				return nil, fmt.Errorf("%s: indexof needs Text arguments or a List and an element, got %s", x.Pos, args[i])
			}
		}
		return ir.Int(), nil
	case "replace":
		for i := range 3 {
			if !args[i].Equal(ir.Text()) {
				return nil, fmt.Errorf("%s: replace argument %d must be Text, got %s", x.Pos, i+1, args[i])
			}
		}
		return ir.Text(), nil
	case "charat":
		if !args[0].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: charat needs Text, got %s", x.Pos, args[0])
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return ir.Text(), nil
	case "slice":
		// Also slices a List, so the two collection kinds stay symmetric.
		if args[0] != nil && args[0].Kind == ir.KList {
			for i := 1; i < 3; i++ {
				if err := needInt(i); err != nil {
					return nil, err
				}
			}
			return args[0], nil
		}
		if !args[0].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: slice needs Text or a List, got %s", x.Pos, args[0])
		}
		for i := 1; i < 3; i++ {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Text(), nil
	case "textjoin":
		// Any element type is fine: each is rendered exactly as Reveal would
		// render it (Int/Float/Text/Bool/Record/...), then joined.
		if args[0] == nil || args[0].Kind != ir.KList {
			return nil, fmt.Errorf("%s: textjoin needs a List, got %s", x.Pos, args[0])
		}
		if !args[1].Equal(ir.Text()) {
			return nil, fmt.Errorf("%s: textjoin separator must be Text, got %s", x.Pos, args[1])
		}
		return ir.Text(), nil

	// -- points and grid geometry ---------------------------------------------
	case "point":
		for i := range 2 {
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
	case "padd", "psub":
		for i := range 2 {
			if err := needPoint(x, name, args, i); err != nil {
				return nil, err
			}
		}
		return PointType(), nil
	case "pscale":
		if err := needPoint(x, name, args, 0); err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return PointType(), nil
	case "around4", "around8":
		// Neighbours of a *point*, with no grid and no bounds — what a Sparse
		// automaton needs, since neighbors4/8 require a dense Grid.
		if err := needPoint(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.List(PointType()), nil
	case "dirs8":
		return ir.List(PointType()), nil
	case "chebyshev":
		for i := range 2 {
			if err := needPoint(x, name, args, i); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "haskey":
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: haskey needs a Map argument, got %s", x.Pos, args[0])
		}
		if !args[1].Equal(args[0].Key) {
			return nil, fmt.Errorf("%s: haskey key must be %s, got %s", x.Pos, args[0].Key, args[1])
		}
		return ir.Bool(), nil
	case "getor":
		// The total lookup. `get` errors on a missing key and there was no way
		// to guard it, which made a Count By map unreadable.
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: getor needs a Map argument, got %s", x.Pos, args[0])
		}
		if !args[1].Equal(args[0].Key) {
			return nil, fmt.Errorf("%s: getor key must be %s, got %s", x.Pos, args[0].Key, args[1])
		}
		if !args[2].Equal(args[0].Elem) {
			return nil, fmt.Errorf("%s: getor default must be %s, got %s", x.Pos, args[0].Elem, args[2])
		}
		return args[0].Elem, nil
	case "keys":
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: keys needs a Map argument, got %s", x.Pos, args[0])
		}
		return ir.List(args[0].Key), nil
	case "values":
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: values needs a Map argument, got %s", x.Pos, args[0])
		}
		return ir.List(args[0].Elem), nil
	case "tolist":
		if args[0] == nil || args[0].Kind != ir.KSet {
			return nil, fmt.Errorf("%s: tolist needs a Set argument, got %s", x.Pos, args[0])
		}
		return ir.List(args[0].Elem), nil
	case "size":
		// Count, without leaving the lambda. Over a Graph it is the node count;
		// `length(edges(g))` is the arc count, which is a different question.
		if args[0] == nil || (args[0].Kind != ir.KMap && args[0].Kind != ir.KSet && args[0].Kind != ir.KGraph) {
			return nil, fmt.Errorf("%s: size needs a Map, Set or Graph argument, got %s", x.Pos, args[0])
		}
		return ir.Int(), nil
	case "manhattan":
		for i := range 2 {
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
	case "tuple":
		// Heterogeneous, unlike `list`: the whole point is a pair whose sides
		// differ, which is what Group By keys and Sort By tiebreaks need.
		return ir.Tuple(args...), nil
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
		for i := range 2 {
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

	// -- collection construction, update and enumeration -------------------------
	//
	// Sparse used to be the only collection with a constructor and a functional
	// update, which is exactly why a sparse automaton was writable as a Fold and
	// a frequency map was not. These close that.
	case "toset":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if err := needKeyable(x, name, elem); err != nil {
			return nil, err
		}
		return ir.Set(elem), nil
	case "emptyset":
		// The argument is a *type witness*, never stored — the same trick
		// `sparse(d)` plays with its default, since a collection's element type
		// cannot be inferred from an absence.
		if err := needKeyable(x, name, args[0]); err != nil {
			return nil, err
		}
		return ir.Set(args[0]), nil
	case "emptylist":
		// The same witness trick, and no keyable constraint: a list holds
		// anything. `list()` cannot stand in for this — with no arguments there
		// is nothing to read the element type from, and the output type of every
		// expression is fixed at resolve time.
		return ir.List(args[0]), nil
	case "emptymap":
		if err := needKeyable(x, name, args[0]); err != nil {
			return nil, err
		}
		return ir.Map(args[0], args[1]), nil
	case "tomap":
		elem, err := needList(0)
		if err != nil {
			return nil, err
		}
		if elem == nil || elem.Kind != ir.KTuple || len(elem.Elems) != 2 {
			return nil, fmt.Errorf("%s: tomap needs a List of (key, value) pairs, got List<%s>", x.Pos, elem)
		}
		if err := needKeyable(x, name, elem.Elems[0]); err != nil {
			return nil, err
		}
		return ir.Map(elem.Elems[0], elem.Elems[1]), nil
	case "entries":
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: entries needs a Map argument, got %s", x.Pos, args[0])
		}
		return ir.List(ir.Tuple(args[0].Key, args[0].Elem)), nil
	case "insert":
		// Two shapes, told apart by the collection: a Set takes the element, a
		// Map takes a key and a value.
		if args[0] != nil && args[0].Kind == ir.KSet {
			if len(args) != 2 {
				return nil, fmt.Errorf("%s: insert into a Set takes 2 arguments, got %d", x.Pos, len(args))
			}
			if !args[1].Equal(args[0].Elem) {
				return nil, fmt.Errorf("%s: insert value must be %s, got %s", x.Pos, args[0].Elem, args[1])
			}
			return args[0], nil
		}
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: insert needs a Set or Map argument, got %s", x.Pos, args[0])
		}
		if len(args) != 3 {
			return nil, fmt.Errorf("%s: insert into a Map takes 3 arguments (map, key, value), got %d", x.Pos, len(args))
		}
		if !args[1].Equal(args[0].Key) {
			return nil, fmt.Errorf("%s: insert key must be %s, got %s", x.Pos, args[0].Key, args[1])
		}
		if !args[2].Equal(args[0].Elem) {
			return nil, fmt.Errorf("%s: insert value must be %s, got %s", x.Pos, args[0].Elem, args[2])
		}
		return args[0], nil
	case "del":
		if args[0] != nil && args[0].Kind == ir.KSet {
			if !args[1].Equal(args[0].Elem) {
				return nil, fmt.Errorf("%s: del value must be %s, got %s", x.Pos, args[0].Elem, args[1])
			}
			return args[0], nil
		}
		if args[0] == nil || args[0].Kind != ir.KMap {
			return nil, fmt.Errorf("%s: del needs a Set or Map argument, got %s", x.Pos, args[0])
		}
		if !args[1].Equal(args[0].Key) {
			return nil, fmt.Errorf("%s: del key must be %s, got %s", x.Pos, args[0].Key, args[1])
		}
		return args[0], nil
	case "union", "intersect", "difference":
		for i := range 2 {
			if args[i] == nil || args[i].Kind != ir.KSet {
				return nil, fmt.Errorf("%s: %s needs Set arguments, got %s", x.Pos, name, args[i])
			}
		}
		if !args[0].Equal(args[1]) {
			return nil, fmt.Errorf("%s: %s needs two sets of the same type, got %s and %s",
				x.Pos, name, args[0], args[1])
		}
		return args[0], nil
	case "setat":
		if args[0] == nil || args[0].Kind != ir.KGrid {
			return nil, fmt.Errorf("%s: setat needs a Grid argument, got %s (a Sparse grid uses put)", x.Pos, args[0])
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		if err := needInt(2); err != nil {
			return nil, err
		}
		if !args[3].Equal(args[0].Elem) {
			return nil, fmt.Errorf("%s: setat value must be %s, got %s", x.Pos, args[0].Elem, args[3])
		}
		return args[0], nil
	case "cellpoints":
		if args[0] == nil || args[0].Kind != ir.KSparse {
			return nil, fmt.Errorf("%s: cellpoints needs a Sparse argument, got %s", x.Pos, args[0])
		}
		return ir.List(PointType()), nil

	// -- graphs ------------------------------------------------------------------
	//
	// Every update here is functional, like insert/put/setat: it returns a new
	// graph and leaves its receiver alone. The optimizer's dead-receiver pass is
	// what recovers the copy when nothing can observe the original.
	case "graph":
		// From an edge list: (from, to) pairs, or (from, to, weight) triples.
		// This is the shape a positional Match Pattern lands on, which is why it
		// is the constructor rather than an adjacency map.
		node, err := graphEdgeListNode(x, args[0])
		if err != nil {
			return nil, err
		}
		return ir.Graph(node), nil
	case "emptygraph":
		// Witness form, like emptyset: the argument is a value of the node type,
		// evaluated but unused, because the result type has to come from
		// somewhere.
		if err := needKeyable(x, name, args[0]); err != nil {
			return nil, err
		}
		return ir.Graph(args[0]), nil
	case "addnode":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: addnode value must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return g, nil
	case "addedge":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		for i := 1; i < 3; i++ {
			if !args[i].Equal(g.Elem) {
				return nil, fmt.Errorf("%s: addedge endpoint %d must be %s, got %s", x.Pos, i, g.Elem, args[i])
			}
		}
		if len(args) == 4 {
			if err := needInt(3); err != nil {
				return nil, err
			}
		}
		return g, nil
	case "deledge":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		for i := 1; i < 3; i++ {
			if !args[i].Equal(g.Elem) {
				return nil, fmt.Errorf("%s: deledge endpoint %d must be %s, got %s", x.Pos, i, g.Elem, args[i])
			}
		}
		return g, nil
	case "nodes":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		return ir.List(g.Elem), nil
	case "edges":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		return ir.List(ir.Tuple(g.Elem, g.Elem, ir.Int())), nil
	case "neighbors":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: neighbors node must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return ir.List(g.Elem), nil
	case "edgesof":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: edgesof node must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return ir.List(ir.Tuple(g.Elem, ir.Int())), nil
	case "hasedge":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		for i := 1; i < 3; i++ {
			if !args[i].Equal(g.Elem) {
				return nil, fmt.Errorf("%s: hasedge endpoint %d must be %s, got %s", x.Pos, i, g.Elem, args[i])
			}
		}
		return ir.Bool(), nil
	case "weight", "weightor":
		// weight is the partial one — it errors on a missing arc — and weightor
		// is its total twin, exactly as get/getor pair over a Map.
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		for i := 1; i < 3; i++ {
			if !args[i].Equal(g.Elem) {
				return nil, fmt.Errorf("%s: %s endpoint %d must be %s, got %s", x.Pos, name, i, g.Elem, args[i])
			}
		}
		if name == "weightor" {
			if err := needInt(3); err != nil {
				return nil, err
			}
		}
		return ir.Int(), nil
	case "degree":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: degree node must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return ir.Int(), nil
	case "weightof":
		// degree with the weights counted rather than the arcs, and total for
		// the same reason: a node that is not in the graph weighs 0.
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: weightof node must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return ir.Int(), nil
	case "root":
		// The one node with no incoming arc. Partial, like weight: "not
		// exactly one root" is a fact about the data that a caller has to
		// hear about, and a total twin would have to invent a node to answer
		// with. Filtering nodes(g) is the way to ask the total question.
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		return g.Elem, nil
	case "indegree":
		// Degree's mirror. It walks the adjacency rather than keeping a reverse
		// index, which is still cheaper than the flipedges the vocabulary made
		// people write to ask it.
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: indegree node must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return ir.Int(), nil
	case "roots", "leaves":
		// roots is root's total twin, the way weightor is weight's: the same
		// question, answered with however many there are.
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		return ir.List(g.Elem), nil
	case "reachable":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: reachable node must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return ir.List(g.Elem), nil
	case "delnode":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if !args[1].Equal(g.Elem) {
			return nil, fmt.Errorf("%s: delnode value must be %s, got %s", x.Pos, g.Elem, args[1])
		}
		return g, nil
	case "hascycle":
		if _, err := needGraph(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.Bool(), nil
	case "weightsum":
		if _, err := needGraph(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "undirected":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		return g, nil
	case "mergegraphs":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if args[1] == nil || args[1].Kind != ir.KGraph || !args[1].Elem.Equal(g.Elem) {
			return nil, fmt.Errorf("%s: mergegraphs needs two graphs of the same node type, got %s and %s",
				x.Pos, g, args[1])
		}
		return g, nil
	case "flipedges":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		return g, nil
	case "subgraph":
		g, err := needGraph(x, name, args, 0)
		if err != nil {
			return nil, err
		}
		if args[1] == nil || args[1].Kind != ir.KList || !args[1].Elem.Equal(g.Elem) {
			return nil, fmt.Errorf("%s: subgraph needs List<%s> of nodes to keep, got %s", x.Pos, g.Elem, args[1])
		}
		return g, nil

	// -- list generation ---------------------------------------------------------
	case "range":
		for i := range 2 {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.List(ir.Int()), nil
	case "fill":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.List(args[1]), nil

	// -- text --------------------------------------------------------------------
	case "split", "trimprefix", "trimsuffix":
		for i := range 2 {
			if err := needText(x, name, args, i); err != nil {
				return nil, err
			}
		}
		if name == "split" {
			return ir.List(ir.Text()), nil
		}
		return ir.Text(), nil
	case "words":
		if err := needText(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.List(ir.Text()), nil
	case "ord":
		if err := needText(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "chr":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.Text(), nil
	case "repeat":
		if err := needText(x, name, args, 0); err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return ir.Text(), nil
	case "padleft", "padright":
		if err := needText(x, name, args, 0); err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		if err := needText(x, name, args, 2); err != nil {
			return nil, err
		}
		return ir.Text(), nil
	case "isdigit", "isalpha", "isupper", "islower":
		if err := needText(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.Bool(), nil

	// -- floats ------------------------------------------------------------------
	case "log", "log2", "log10", "exp", "sin", "cos", "tan":
		if !numeric(args[0]) {
			return nil, fmt.Errorf("%s: %s needs Int or Float, got %s", x.Pos, name, args[0])
		}
		return ir.Float(), nil
	case "atan2", "hypot":
		for i := range 2 {
			if !numeric(args[i]) {
				return nil, fmt.Errorf("%s: %s needs Int or Float, got %s", x.Pos, name, args[i])
			}
		}
		return ir.Float(), nil
	case "trunc":
		if !numeric(args[0]) {
			return nil, fmt.Errorf("%s: trunc needs Int or Float, got %s", x.Pos, args[0])
		}
		return ir.Int(), nil

	// -- records -----------------------------------------------------------------
	//
	// Field names are literals rather than a new argument form in the grammar:
	// `record("a", 1, "b", 2)` is typeable exactly the way `item(t, 0)` over a
	// Tuple already is — by reading the literal at resolve time — and it costs
	// the language no new syntax at all. Both compile to a plain struct.
	case "record":
		return recordType(x, args)
	case "with":
		if args[0] == nil || args[0].Kind != ir.KRecord {
			return nil, fmt.Errorf("%s: with needs a Record argument, got %s", x.Pos, args[0])
		}
		lit, ok := x.Args[1].(*ast.StringLit)
		if !ok {
			return nil, fmt.Errorf("%s: with needs a literal field name (the result's type depends on it)", x.Pos)
		}
		for _, f := range args[0].Fields {
			if f.Name != lit.Value {
				continue
			}
			if !args[2].Equal(f.Type) {
				return nil, fmt.Errorf("%s: with field %q must be %s, got %s",
					x.Pos, lit.Value, f.Type, args[2])
			}
			return args[0], nil
		}
		return nil, fmt.Errorf("%s: record %s has no field %q", x.Pos, args[0], lit.Value)

	// -- bases, bits, number theory ----------------------------------------------
	case "frombase":
		if err := needText(x, name, args, 0); err != nil {
			return nil, err
		}
		if err := needInt(1); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "fromhex":
		if err := needText(x, name, args, 0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "tobase":
		for i := range 2 {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Text(), nil
	case "tohex", "tobin":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.Text(), nil
	case "bnot", "popcount":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.Int(), nil
	case "testbit":
		for i := range 2 {
			if err := needInt(i); err != nil {
				return nil, err
			}
		}
		return ir.Bool(), nil
	case "isprime":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.Bool(), nil
	case "digits", "divisors":
		if err := needInt(0); err != nil {
			return nil, err
		}
		return ir.List(ir.Int()), nil
	case "fromdigits":
		if !args[0].Equal(ir.List(ir.Int())) {
			return nil, fmt.Errorf("%s: fromdigits needs List<Int>, got %s", x.Pos, args[0])
		}
		return ir.Int(), nil
	case "crt":
		for i := range 2 {
			if !args[i].Equal(ir.List(ir.Int())) {
				return nil, fmt.Errorf("%s: crt needs List<Int> arguments, got %s", x.Pos, args[i])
			}
		}
		return ir.Int(), nil
	}
	return nil, fmt.Errorf("%s: unknown function %q", x.Pos, name)
}

// recordType builds the Record type of a `record("a", v, "b", w)` call. The
// field names must be string literals, for the reason item-over-a-Tuple's index
// must be an int literal: the result type is only knowable when they are.
func recordType(x *ast.CallExpr, args []*ir.Type) (*ir.Type, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("%s: record takes name/value pairs, so an even number of arguments; got %d",
			x.Pos, len(args))
	}
	fields := make([]ir.Field, 0, len(args)/2)
	seen := make(map[string]bool, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		lit, ok := x.Args[i].(*ast.StringLit)
		if !ok {
			return nil, fmt.Errorf("%s: record field name %d must be a literal (the result's type depends on it)",
				x.Pos, i/2+1)
		}
		if lit.Value == "" {
			return nil, fmt.Errorf("%s: record field names cannot be empty", x.Pos)
		}
		if seen[lit.Value] {
			return nil, fmt.Errorf("%s: record has a duplicate field %q", x.Pos, lit.Value)
		}
		seen[lit.Value] = true
		fields = append(fields, ir.Field{Name: lit.Value, Type: args[i+1]})
	}
	return ir.Record(fields...), nil
}

// needGraph requires argument i to be a Graph, returning its type so the
// caller can reach the node type through Elem.
func needGraph(x *ast.CallExpr, name string, args []*ir.Type, i int) (*ir.Type, error) {
	if args[i] == nil || args[i].Kind != ir.KGraph {
		return nil, fmt.Errorf("%s: %s needs a Graph argument, got %s", x.Pos, name, args[i])
	}
	return args[i], nil
}

// graphEdgeListNode types `graph(edges)`: the argument is a list of (from, to)
// pairs or (from, to, weight) triples, and the node type is the one both
// endpoints share.
//
// Two shapes rather than one because a positional Match Pattern produces
// List<List<K>> — two entries per row — while tuple(...) and zip produce
// List<(K, K)>. Topological Sort already accepts both for the same reason, so
// the constructor matches the shape the parse actually lands on.
func graphEdgeListNode(x *ast.CallExpr, t *ir.Type) (*ir.Type, error) {
	bad := func() (*ir.Type, error) {
		return nil, fmt.Errorf("%s: graph needs an edge list — List<(K, K)>, List<(K, K, Int)>, "+
			"or the List<List<K>> a positional Match Pattern produces — got %s", x.Pos, t)
	}
	if t == nil || t.Kind != ir.KList || t.Elem == nil {
		return bad()
	}
	switch e := t.Elem; e.Kind {
	case ir.KTuple:
		if len(e.Elems) != 2 && len(e.Elems) != 3 {
			return bad()
		}
		if !e.Elems[0].Equal(e.Elems[1]) {
			return nil, fmt.Errorf("%s: graph's edge endpoints must have the same type, got %s and %s",
				x.Pos, e.Elems[0], e.Elems[1])
		}
		if len(e.Elems) == 3 && !e.Elems[2].Equal(ir.Int()) {
			return nil, fmt.Errorf("%s: graph's edge weight must be Int, got %s", x.Pos, e.Elems[2])
		}
		if err := needKeyable(x, "graph", e.Elems[0]); err != nil {
			return nil, err
		}
		return e.Elems[0], nil
	case ir.KList:
		// The ragged form: the length is only known at run time, so the weight
		// cannot be typed here. An unweighted edge list is what this shape is
		// for, and every arc gets weight 1.
		if err := needKeyable(x, "graph", e.Elem); err != nil {
			return nil, err
		}
		return e.Elem, nil
	}
	return bad()
}

// needKeyable requires a type that can be a Map key or Set element.
func needKeyable(x *ast.CallExpr, name string, t *ir.Type) error {
	if !ir.Keyable(t) {
		return fmt.Errorf("%s: %s needs keyable elements (Int, Text, or Tuples/Records of them), got %s",
			x.Pos, name, t)
	}
	return nil
}

// needText requires argument i to be Text.
func needText(x *ast.CallExpr, name string, args []*ir.Type, i int) error {
	if !args[i].Equal(ir.Text()) {
		return fmt.Errorf("%s: %s argument %d must be Text, got %s", x.Pos, name, i+1, args[i])
	}
	return nil
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
	switch x.Op {
	case token.MINUS:
		if !numeric(t) {
			return nil, fmt.Errorf("%s: unary minus needs Int or Float, got %s", x.Pos, t)
		}
		return t, nil
	case token.NOT:
		if !t.Equal(ir.Bool()) {
			return nil, fmt.Errorf("%s: ikke needs a Bool operand, got %s", x.Pos, t)
		}
		return ir.Bool(), nil
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
		// `+` doubles as Text concatenation — the one non-numeric operator
		// overload, and the obvious meaning for two strings.
		if x.Op == token.PLUS && lt.Equal(ir.Text()) && rt.Equal(ir.Text()) {
			return ir.Text(), nil
		}
		if !numeric(lt) || !numeric(rt) {
			return nil, fmt.Errorf("%s: arithmetic needs Int or Float operands, got %s and %s", x.Pos, lt, rt)
		}
		return promote(lt, rt), nil
	case token.PERCENT:
		// Modulo is Int-only: Euclidean remainder over floats is not the
		// operation anyone means, and IEEE rounding would make it lie.
		if !lt.Equal(ir.Int()) || !rt.Equal(ir.Int()) {
			return nil, fmt.Errorf("%s: %% needs Int operands, got %s and %s", x.Pos, lt, rt)
		}
		return ir.Int(), nil
	case token.LT, token.GT, token.LE, token.GE:
		// The relational operators reach exactly as far as ir.Ordered — the
		// ordering Sort and Sort By already use. They used to stop at the
		// numeric types, which left `Sort` able to order a List<Text> that no
		// lambda could then compare, and made a text tiebreak inside a
		// predicate unwritable.
		if numeric(lt) && numeric(rt) {
			return ir.Bool(), nil // mixed Int/Float compares through promotion
		}
		if !lt.Equal(rt) {
			return nil, fmt.Errorf("%s: cannot compare %s %s %s (different types)",
				x.Pos, lt, relSymbol(x.Op), rt)
		}
		if !ir.Ordered(lt) {
			return nil, fmt.Errorf("%s: %s has no ordering, so %s cannot compare it "+
				"(Int, Float, Text, and tuples of those do)", x.Pos, lt, relSymbol(x.Op))
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

// relSymbol spells a relational operator the way it was written. token.Kind's
// String is the symbolic name ("LT"), which is right for a parser trace and
// wrong in a message a user reads next to their own source line.
func relSymbol(op token.Kind) string {
	switch op {
	case token.LT:
		return "<"
	case token.GT:
		return ">"
	case token.LE:
		return "<="
	case token.GE:
		return ">="
	}
	return op.String()
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
	env := make(Env, len(l.Params)+BindingDepth())
	// Bindings first, parameters second: a parameter of the same name shadows
	// the binding, which is the rule every other scope in the language uses.
	seedBindings(env)
	for i, p := range l.Params {
		env[p] = paramTypes[i]
	}
	return ExprType(l.Body, env)
}
