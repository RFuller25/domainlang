package codegen

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// The expression compiler lowers a Using: lambda body to a plain Go
// expression. Types were already validated at resolve time by the typecheck
// package; the types recomputed here only steer code shape (e.g. `=` on
// scalars becomes `==`).

// exprBinding maps a lambda parameter to the Go expression holding its value.
type exprBinding struct {
	expr string
	typ  *ir.Type
	// cell, when non-empty, is a Go expression of type *T pointing at the
	// variable behind expr: `&dmBind1` for a local of the enclosing function,
	// or the parameter itself for a binding a block function already reaches
	// through a pointer. Two things need it, and nothing else may have it: a
	// `:=` writes through `expr` (which is an lvalue exactly when there is a
	// cell), and a pipeline body takes the cell as its parameter so a write
	// inside it lands on the binding rather than on a copy.
	//
	// A lambda parameter, an ambient `For` variable and a channel's value are
	// bound without one — they are not variables this package owns, and the
	// resolver refuses a write to any of them long before the backend runs.
	cell string

	// lit says expr is a compile-time constant rather than a variable — a
	// `Consider` binding pinned to a measured value (see Tuning.Constants).
	//
	// It matters at exactly one place: a block body's function receives the
	// bindings in scope as parameters, and a parameter is not a constant. A
	// pinned binding is substituted into the body instead and no parameter is
	// declared for it, which is the difference between a modulus the Go
	// compiler can turn into a mask and one it has to read from an argument.
	lit bool
}

type exprEnv map[string]exprBinding

// compileExpr returns a parenthesized Go expression and its Domain type.
func (g *gen) compileExpr(e ast.Expr, env exprEnv) (string, *ir.Type, error) {
	switch x := e.(type) {
	case *ast.IntLit:
		// Wrapped for the same reason FloatLit is: an unwrapped integer literal
		// is an *untyped constant* in Go, and a generic helper whose type
		// parameter is fixed only by a scalar argument then infers `int` rather
		// than the int64 the rest of the program agrees on. That is a backend
		// divergence, not a type error in the program — the interpreter has no
		// such split — and it bit fill, abs, clamp and the two-argument min/max
		// before the literal was pinned here instead of at each call site.
		return "int64(" + strconv.FormatInt(x.Value, 10) + ")", ir.Int(), nil
	case *ast.FloatLit:
		// Wrapped so the literal stays float64-typed in any Go context, even
		// when FormatFloat prints an integer-looking "2".
		return "float64(" + strconv.FormatFloat(x.Value, 'g', -1, 64) + ")", ir.Float(), nil
	case *ast.BoolLit:
		return strconv.FormatBool(x.Value), ir.Bool(), nil
	case *ast.StringLit:
		return strconv.Quote(x.Value), ir.Text(), nil
	case *ast.BlockBody:
		// A lambda body written as a pipeline: lowered to a top-level function
		// and called here, so every emitter that wanted an expression gets one
		// (see blockgen.go).
		b, ok := env[x.Param]
		if !ok {
			return "", nil, fmt.Errorf("block body has no input binding")
		}
		// A lambda of two or more parameters has one the body is *over*; the
		// rest are in scope by name inside it. Putting them in g.bindNames is
		// the whole implementation: emitBlockCall already passes every binding
		// in scope into the block's function, because a `Consider` in an outer
		// scope had the same problem first.
		if len(x.Extra) > 0 {
			saved := g.bindNames
			scoped := make(exprEnv, len(saved)+len(x.Extra))
			maps.Copy(scoped, saved)
			for _, e := range x.Extra {
				eb, ok := env[e.Param]
				if !ok {
					return "", nil, fmt.Errorf("block body has no binding for %q", e.Name)
				}
				scoped[e.Name] = eb
			}
			g.bindNames = scoped
			defer func() { g.bindNames = saved }()
		}
		return g.emitBlockCall(x, b.expr, b.typ)
	case *ast.Ident:
		b, ok := env[x.Name]
		if !ok {
			// Fall back to the enclosing For loops' variables — the lambda's
			// trailing ambient parameters — and then to the `Consider`
			// bindings in scope. The caller's env wins, so a leading
			// parameter of the same name still shadows.
			if b, ok = g.ambientNames[x.Name]; !ok {
				if b, ok = g.bindNames[x.Name]; !ok {
					return "", nil, fmt.Errorf("unknown identifier %q", x.Name)
				}
			}
		}
		return b.expr, b.typ, nil
	case *ast.GlobalRef:
		// No environment is consulted at all: the read was resolved to a slot
		// while the program was lowered, and the slot is a package-level
		// variable here. See codegen/globalgen.go.
		b, err := g.globalRef(x)
		if err != nil {
			return "", nil, err
		}
		return b.expr, b.typ, nil
	case *ast.UnaryExpr:
		switch x.Op {
		case token.MINUS:
			v, vt, err := g.compileExpr(x.X, env)
			if err != nil {
				return "", nil, err
			}
			return "(-" + v + ")", vt, nil
		case token.NOT:
			v, _, err := g.compileExpr(x.X, env)
			if err != nil {
				return "", nil, err
			}
			return "(!" + v + ")", ir.Bool(), nil
		}
		return "", nil, fmt.Errorf("unsupported unary operator %s", x.Op)
	case *ast.BinaryExpr:
		return g.compileBinary(x, env)
	case *ast.FieldAccess:
		tv, tt, err := g.compileExpr(x.Target, env)
		if err != nil {
			return "", nil, err
		}
		if tt == nil || tt.Kind != ir.KRecord {
			return "", nil, fmt.Errorf("field access .%s on non-record type %s", x.Field, tt)
		}
		for _, f := range tt.Fields {
			if f.Name == x.Field {
				return tv + "." + fieldName(f.Name), f.Type, nil
			}
		}
		return "", nil, fmt.Errorf("record %s has no field %q", tt, x.Field)
	case *ast.CallExpr:
		return g.compileCall(x, env)
	case *ast.CondExpr:
		return g.compileCond(x, env)
	case *ast.LetExpr:
		return g.compileLet(x, env)
	case *ast.AssignExpr:
		return g.compileAssign(x, env)
	case *ast.AlsoExpr:
		return g.compileAlso(x, env)
	default:
		return "", nil, fmt.Errorf("unsupported expression %T", e)
	}
}

// ordered wraps a compiled operand in an immediately-invoked function, which
// makes it a *function call* — and function calls are the one thing Go's
// evaluation order does guarantee, left to right in lexical order. Wrapping an
// operand that would otherwise be a bare variable read is how an expression
// containing a write gets the same order out of both backends.
//
// It is applied only where a write is actually present, so every program that
// does not use `:=` compiles to exactly the Go it compiled to before.
func (g *gen) ordered(code string, t *ir.Type) (string, error) {
	goT, err := g.goType(t)
	if err != nil {
		return "", err
	}
	return "func() " + goT + " { return " + code + " }()", nil
}

// orderArgs forces left-to-right evaluation across an argument list, in place.
// One writing argument puts *every* argument under the rule, in both
// directions: an argument to its left must be read before the write, and one
// to its right after it.
func (g *gen) orderArgs(exprs []ast.Expr, args []string, types []*ir.Type) error {
	writes := false
	for _, a := range exprs {
		if ast.HasUpdate(a) {
			writes = true
			break
		}
	}
	if !writes || len(args) < 2 {
		return nil
	}
	for i := range args {
		w, err := g.ordered(args[i], types[i])
		if err != nil {
			return err
		}
		args[i] = w
	}
	return nil
}

// compileAssign lowers `n := v` to an assignment to the Go local the name is
// bound to, wrapped in an immediately-invoked function so the whole thing is
// still an *expression* — which is what every caller here wants — and so its
// value is the value written.
//
// The local is whatever the name resolves to: compileLet's `dmLet…` for a
// `consider`, emitConsider's `dmBind…` for a stage binding, or a block
// function's `*T` parameter for a binding written to from inside a pipeline
// body. All three are variables this package owns, and each carries a cell
// saying so, which is what makes its `expr` an lvalue. The names that are
// *not* variables — a lambda parameter, an inlined function binding, a
// substituted Shikigami parameter — were refused at resolve time and reach
// here without a cell.
func (g *gen) compileAssign(x *ast.AssignExpr, env exprEnv) (string, *ir.Type, error) {
	var b exprBinding
	if x.Target != nil {
		// A global: the resolver set Target only when nothing nearer shadowed
		// the name, so the environment is not consulted — the same reason a
		// read of one does not consult it.
		gb, err := g.globalRef(x.Target)
		if err != nil {
			return "", nil, err
		}
		b = gb
	} else {
		var ok bool
		if b, ok = env[x.Name]; !ok {
			if b, ok = g.bindNames[x.Name]; !ok {
				return "", nil, fmt.Errorf("unknown identifier %q", x.Name)
			}
		}
	}
	// Nothing else in an environment is a variable: a parameter may be an
	// element of a slice being ranged over, an ambient loop variable is the
	// loop's own. Writing to either would be legal Go and wrong, so the cell is
	// checked rather than assumed — the resolve-time refusals mean this cannot
	// fire, and if one of them ever stops covering a case the compiler must
	// stop, not diverge.
	if b.cell == "" {
		return "", nil, fmt.Errorf("%q is not a binding this backend can update", x.Name)
	}
	v, vt, err := g.compileExpr(x.Value, env)
	if err != nil {
		return "", nil, err
	}
	t := b.typ
	if t == nil {
		t = vt
	}
	goT, err := g.goType(t)
	if err != nil {
		return "", nil, err
	}
	return "func() " + goT + " {\n\t\t" + b.expr + " = " + v +
		"\n\t\treturn " + b.expr + "\n\t}()", t, nil
}

// compileAlso lowers `body also c1, c2` to an immediately-invoked function
// that takes the body's value first and then runs the clauses for their
// effects. The body's value is held in a local before a clause can run,
// which is what makes a clause that updates what the body read change the
// *next* reader rather than this one.
func (g *gen) compileAlso(x *ast.AlsoExpr, env exprEnv) (string, *ir.Type, error) {
	body, bodyT, err := g.compileExpr(x.Body, env)
	if err != nil {
		return "", nil, err
	}
	goT, err := g.goType(bodyT)
	if err != nil {
		return "", nil, err
	}
	local := g.fresh("dmAlso")
	var b strings.Builder
	b.WriteString("func() " + goT + " {\n\t\tvar " + local + " " + goT + " = " + body + "\n")
	for _, c := range x.Clauses {
		cv, _, err := g.compileExpr(c, env)
		if err != nil {
			return "", nil, err
		}
		// Assigned to the blank identifier: Go has no expression statements,
		// and the clause is written to be evaluated rather than used.
		b.WriteString("\t\t_ = " + cv + "\n")
	}
	b.WriteString("\t\treturn " + local + "\n\t}()")
	return b.String(), bodyT, nil
}

// compileLet lowers `consider n as v in body` to a Go local inside an
// immediately-invoked function — the same shape compileCond already uses, and
// one the Go compiler inlines. The binding is evaluated exactly once, which is
// the whole point of the form.
//
// The Go variable is name-mangled rather than used verbatim: a Domain binding
// may legally be called `len`, `string`, or the name of a dm* helper, and
// shadowing one of those inside the generated function would break unrelated
// code in the same expression.
func (g *gen) compileLet(x *ast.LetExpr, env exprEnv) (string, *ir.Type, error) {
	// A chain of `consider a as … in consider b as … in …` becomes *one*
	// closure with sequential declarations, not one closure per binding.
	//
	// Nesting them is what a naive lowering does and it does not survive
	// contact with a real program: the decode step of an instruction
	// interpreter is a chain of forty-odd bindings, and forty nested closures
	// took the Go compiler a hundred seconds and then the OOM killer — on a
	// program the interpreter runs without complaint. Flattening removes the
	// limit rather than reporting it.
	//
	// It buys no speed. Hand-flattening the five-deep chain in a tight loop
	// measured 75.4 ms against 73.8 ms, because Go inlines the nest perfectly
	// well when it is small enough to compile at all. This is entirely about
	// the compiler's own appetite.
	var decls strings.Builder
	inner := make(exprEnv, len(env)+4)
	for k, v := range env {
		inner[k] = v
	}
	// Locals declared in this block, so a binding that shadows an earlier one
	// gets a fresh Go name. Nested closures gave each binding its own scope for
	// free; in one block a repeated `consider x` would be a redeclaration.
	declared := map[string]int{}

	cur := x
	for {
		// The value is compiled in the scope *before* its own binding, which is
		// what `inner` holds at this point: earlier bindings of the chain are
		// visible, this one is not.
		val, valT, err := g.compileExpr(cur.Value, inner)
		if err != nil {
			return "", nil, err
		}
		goValT, err := g.goType(valT)
		if err != nil {
			return "", nil, err
		}
		local := "dmLet" + fieldName(cur.Name)
		if n := declared[local]; n > 0 {
			local += "_" + itoa(n)
		}
		declared["dmLet"+fieldName(cur.Name)]++
		decls.WriteString("\t\tvar " + local + " " + goValT + " = " + val + "\n")
		decls.WriteString("\t\t_ = " + local + "\n")
		inner[cur.Name] = exprBinding{expr: local, typ: valT, cell: "&" + local}

		next, ok := cur.Body.(*ast.LetExpr)
		if !ok {
			break
		}
		cur = next
	}

	body, bodyT, err := g.compileExpr(cur.Body, inner)
	if err != nil {
		return "", nil, err
	}
	goBodyT, err := g.goType(bodyT)
	if err != nil {
		return "", nil, err
	}
	return "func() " + goBodyT + " {\n" + decls.String() +
		"\t\treturn " + body + "\n\t}()", bodyT, nil
}

// compileCond lowers `if c then a else b` to an immediately-invoked func
// literal — the only Go expression form with lazy arms (there is no ternary),
// which the Go compiler inlines. Laziness is load-bearing: the unselected arm
// may be partial (`if length(xs) = 0 then 0 else first(xs)`).
func (g *gen) compileCond(x *ast.CondExpr, env exprEnv) (string, *ir.Type, error) {
	cond, _, err := g.compileExpr(x.Cond, env)
	if err != nil {
		return "", nil, err
	}
	thenV, thenT, err := g.compileExpr(x.Then, env)
	if err != nil {
		return "", nil, err
	}
	elseV, _, err := g.compileExpr(x.Else, env)
	if err != nil {
		return "", nil, err
	}
	goT, err := g.goType(thenT)
	if err != nil {
		return "", nil, err
	}
	return "func() " + goT + " {\n\t\tif " + cond + " {\n\t\t\treturn " + thenV +
		"\n\t\t}\n\t\treturn " + elseV + "\n\t}()", thenT, nil
}

// compileCall lowers an expression-layer builtin (typecheck.Builtins) to a
// direct Go expression or a call to a generic dm* runtime helper. Names,
// arity, and argument types were validated by typecheck at resolve time.
func (g *gen) compileCall(x *ast.CallExpr, env exprEnv) (string, *ir.Type, error) {
	id, ok := x.Fn.(*ast.Ident)
	if !ok {
		return "", nil, fmt.Errorf("only builtin functions can be called")
	}
	name := id.Name
	// max/min/sum of a list *literal* lower to direct scalar ops (no slice, no
	// loop). Falls through to the general path when the shape doesn't match.
	if (name == "max" || name == "min" || name == "sum") && len(x.Args) == 1 {
		if inner, iname := callName(x.Args[0]); iname == "list" && len(inner.Args) >= 1 {
			if r, rt, ok, err := g.reduceLiteral(name, inner.Args, env); err != nil {
				return "", nil, err
			} else if ok {
				return r, rt, nil
			}
		}
	}
	args := make([]string, len(x.Args))
	types := make([]*ir.Type, len(x.Args))
	for i, a := range x.Args {
		av, at, err := g.compileExpr(a, env)
		if err != nil {
			return "", nil, err
		}
		args[i], types[i] = av, at
	}
	// Same ordering hazard as compileBinary's, one argument list wider: an
	// argument that writes must not be able to run before an argument written
	// to its left has been read.
	if err := g.orderArgs(x.Args, args, types); err != nil {
		return "", nil, err
	}
	listElem := func(i int) (*ir.Type, error) {
		if types[i] == nil || types[i].Kind != ir.KList {
			return nil, fmt.Errorf("%s needs a List argument, got %s", name, types[i])
		}
		return types[i].Elem, nil
	}

	switch name {
	case "length":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.imp("unicode/utf8")
			return "int64(utf8.RuneCountInString(" + args[0] + "))", ir.Int(), nil
		}
		if types[0] != nil && types[0].Kind == ir.KTuple {
			return fmt.Sprintf("int64(%d)", len(types[0].Elems)), ir.Int(), nil
		}
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		return "int64(len(" + args[0] + "))", ir.Int(), nil
	case "item":
		// Tuple access compiles to a direct struct field — typecheck already
		// proved the index is a literal in range, so there is nothing to check
		// at runtime.
		if types[0] != nil && types[0].Kind == ir.KTuple {
			lit, ok := x.Args[1].(*ast.IntLit)
			if !ok {
				return "", nil, fmt.Errorf("item over a Tuple needs a literal index")
			}
			return "(" + args[0] + ")." + tupleField(int(lit.Value)), types[0].Elems[lit.Value], nil
		}
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmItem", declItem)
		return "dmItem(" + args[0] + ", " + args[1] + ")", elem, nil
	case "take":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmTake", declTake)
		return "dmTake(" + args[0] + ", " + args[1] + ")", types[0], nil
	case "drop":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmDrop", declDrop)
		return "dmDrop(" + args[0] + ", " + args[1] + ")", types[0], nil
	// -- first-order list operations ---------------------------------------------
	case "sort":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		fn, err := g.sortFn(elem)
		if err != nil {
			return "", nil, err
		}
		return fn + "(" + args[0] + ")", types[0], nil
	case "unique":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmUniqueList", declUniqueList)
		return "dmUniqueList(" + args[0] + ")", types[0], nil
	case "flatten":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmFlatten", declFlatten)
		return "dmFlatten(" + args[0] + ")", elem, nil
	case "bandall", "borall", "bxorall":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dm"+strings.Title(name), declBitReduce(name))
		return "dm" + strings.Title(name) + "(" + args[0] + ")", ir.Int(), nil
	case "and", "or":
		// A *function*, so both arguments are evaluated before it runs — the
		// interpreter evaluates every argument before dispatching, and a
		// compiled `&&` would skip a failure the interpreter raises. Passing
		// them to a helper is what forces that; Go's non-short-circuiting `&`
		// and `|` are not defined on the untyped bool a comparison produces.
		// The infix operators keep `&&`/`||` and keep short-circuiting — that
		// difference is the point, and docs/expressions.md states it.
		fn := "dmAnd"
		decl := declAnd
		if name == "or" {
			fn, decl = "dmOr", declOr
		}
		g.helper(fn, decl)
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
	case "xor":
		return "(" + args[0] + " != " + args[1] + ")", ir.Bool(), nil
	case "not":
		return "(!" + args[0] + ")", ir.Bool(), nil
	case "product":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmProduct", declProduct)
		return "dmProduct(" + args[0] + ")", elem, nil
	case "zip":
		a, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		b, err := listElem(1)
		if err != nil {
			return "", nil, err
		}
		fn, err := g.pairFn("zip", a, b)
		if err != nil {
			return "", nil, err
		}
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.List(ir.Tuple(a, b)), nil
	case "enumerate":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		fn, err := g.pairFn("enumerate", ir.Int(), elem)
		if err != nil {
			return "", nil, err
		}
		return fn + "(" + args[0] + ")", ir.List(ir.Tuple(ir.Int(), elem)), nil
	case "chunk", "windows":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		decl, fn := declChunk, "dmChunk"
		if name == "windows" {
			decl, fn = declWindows, "dmWindows"
		}
		g.helper(fn, decl)
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.List(types[0]), nil
	case "transpose":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmTransposeList", declTransposeList)
		return "dmTransposeList(" + args[0] + ")", types[0], nil
	case "reverse":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.helper("dmReverseText", declReverseText)
			return "dmReverseText(" + args[0] + ")", ir.Text(), nil
		}
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmRev", declRev)
		return "dmRev(" + args[0] + ")", types[0], nil
	case "concat":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmConcat", declConcat)
		return "dmConcat(" + args[0] + ", " + args[1] + ")", types[0], nil
	case "first":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFirst", declFirst)
		return "dmFirst(" + args[0] + ")", elem, nil
	case "last":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmLast", declLast)
		return "dmLast(" + args[0] + ")", elem, nil
	case "sum":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmSum", declSumInts)
		return "dmSum(" + args[0] + ")", elem, nil
	case "min", "max":
		// Two arguments is the scalar form; one is the list reduction.
		if len(args) == 2 {
			decl, fn := declMin2, "dmMin2"
			if name == "max" {
				decl, fn = declMax2, "dmMax2"
			}
			g.helper(fn, decl)
			res, a, b := ir.Int(), args[0], args[1]
			if isFloatType(types[0]) || isFloatType(types[1]) {
				res = ir.Float()
				if !isFloatType(types[0]) {
					a = "float64(" + a + ")"
				}
				if !isFloatType(types[1]) {
					b = "float64(" + b + ")"
				}
			}
			return fn + "(" + a + ", " + b + ")", res, nil
		}
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		if name == "min" {
			g.helper("dmMin", declMinInts)
			return "dmMin(" + args[0] + ")", elem, nil
		}
		g.helper("dmMax", declMaxInts)
		return "dmMax(" + args[0] + ")", elem, nil
	case "contains":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.imp("strings")
			return "strings.Contains(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
		}
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.helper("dmSet", declSet)
			g.helper("dmSetHas", declSetHas)
			return "dmSetHas(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
		}
		if types[0] != nil && types[0].Kind == ir.KGraph {
			g.helper("dmGraph", declGraph)
			return "func() bool { _, ok := (" + args[0] + ").index[" + args[1] + "]; return ok }()",
				ir.Bool(), nil
		}
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmContains", declContains)
		return "dmContains(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
	case "get":
		if types[0] == nil || types[0].Kind != ir.KMap {
			return "", nil, fmt.Errorf("get needs a Map argument, got %s", types[0])
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmMap", declMap)
		g.helper("dmMapGet", declMapGet)
		return "dmMapGet(" + args[0] + ", " + args[1] + ")", types[0].Elem, nil
	case "at":
		if types[0] != nil && types[0].Kind == ir.KSparse {
			// Total on the infinite plane: unset cells read the default.
			return "(" + args[0] + ").at(" + args[1] + ", " + args[2] + ")", types[0].Elem, nil
		}
		if types[0] == nil || types[0].Kind != ir.KGrid {
			return "", nil, fmt.Errorf("at needs a Grid argument, got %s", types[0])
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGrid", declGrid)
		g.helper("dmGridAt", declGridAt)
		return "dmGridAt(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0].Elem, nil

	// -- sparse grids (H) --------------------------------------------------------
	case "sparse":
		elemGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		g.helper("dmSparse", declSparse, "slices")
		return "dmNewSparse[" + elemGo + "](" + args[0] + ")", ir.Sparse(types[0]), nil
	case "put":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			return "", nil, fmt.Errorf("put needs a Sparse argument, got %s", types[0])
		}
		g.helper("dmSparse", declSparse, "slices")
		if x.InPlace {
			g.helper("dmSparsePutIn", declSparsePutIn)
			return "dmSparsePutIn(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
				types[0], nil
		}
		g.helper("dmSparsePut", declSparsePut)
		return "dmSparsePut(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
			types[0], nil
	case "has":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			return "", nil, fmt.Errorf("has needs a Sparse argument, got %s", types[0])
		}
		return "(" + args[0] + ").has(" + args[1] + ", " + args[2] + ")", ir.Bool(), nil
	case "cells":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			return "", nil, fmt.Errorf("cells needs a Sparse argument, got %s", types[0])
		}
		return "int64(len((" + args[0] + ").cells))", ir.Int(), nil
	case "minrow", "maxrow", "mincol", "maxcol":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			return "", nil, fmt.Errorf("%s needs a Sparse argument, got %s", name, types[0])
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmSparseBound", declSparseBound)
		return "dmSparseBound(" + args[0] + ", " + goStr(name) + ")", ir.Int(), nil

	// -- math / number theory (scalar: fully compiled) -------------------------
	case "abs":
		g.helper("dmAbs", declAbs)
		return "dmAbs(" + args[0] + ")", types[0], nil
	case "tofloat":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.helper("dmFail", declFail, "fmt", "os")
			g.helper("dmParseFloat", declParseFloat, "strconv", "strings")
			return "dmParseFloat(" + args[0] + ")", ir.Float(), nil
		}
		return "float64(" + args[0] + ")", ir.Float(), nil
	case "floor":
		g.imp("math")
		return "int64(math.Floor(" + args[0] + "))", ir.Int(), nil
	case "ceil":
		g.imp("math")
		return "int64(math.Ceil(" + args[0] + "))", ir.Int(), nil
	case "round":
		g.imp("math")
		return "int64(math.Round(" + args[0] + "))", ir.Int(), nil
	case "sqrt":
		g.imp("math")
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFmtFloat", declFmtFloat, "strconv")
		g.helper("dmSqrt", declSqrt, "math")
		arg := args[0]
		if !isFloatType(types[0]) {
			arg = "float64(" + arg + ")"
		}
		return "dmSqrt(" + arg + ")", ir.Float(), nil
	case "sign":
		g.helper("dmSign", declSign)
		return "dmSign(" + args[0] + ")", ir.Int(), nil
	case "gcd":
		g.helper("dmAbs", declAbs)
		g.helper("dmGcd", declGcd)
		return "dmGcd(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "lcm":
		g.helper("dmAbs", declAbs)
		g.helper("dmGcd", declGcd)
		g.helper("dmLcm", declLcm)
		return "dmLcm(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "modpow":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmModPow", declModPow)
		return "dmModPow(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Int(), nil
	case "mod":
		return g.modGo(args[0], args[1], x.Args[1]), ir.Int(), nil
	case "divmod":
		// Built inline rather than via a helper: the result is a generated
		// tuple struct whose name is only known here.
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmDiv", declDiv)
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		a, b := args[0], args[1]
		mod := g.modGo(a, b, x.Args[1])
		return pt + "{dmDiv(" + a + " - " + mod + ", " + b + "), " + mod + "}",
			irPoint(), nil
	case "pow":
		// Follows the operators' promotion rule: integral unless an operand is
		// a Float, in which case it is math.Pow like every other float op.
		if isFloatType(types[0]) || isFloatType(types[1]) {
			g.imp("math")
			return "math.Pow(" + g.asFloat(args[0], types[0]) + ", " +
				g.asFloat(args[1], types[1]) + ")", ir.Float(), nil
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmPow", declPow)
		return "dmPow(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "isqrt":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmISqrt", declISqrt)
		return "dmISqrt(" + args[0] + ")", ir.Int(), nil
	case "factorial":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFactorial", declFactorial)
		return "dmFactorial(" + args[0] + ")", ir.Int(), nil
	case "choose":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmChoose", declChoose)
		return "dmChoose(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "clamp":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmClamp", declClamp)
		res := ir.Int()
		a := args
		if isFloatType(types[0]) || isFloatType(types[1]) || isFloatType(types[2]) {
			res = ir.Float()
			a = make([]string, 3)
			for i := range 3 {
				if isFloatType(types[i]) {
					a[i] = args[i]
				} else {
					a[i] = "float64(" + args[i] + ")"
				}
			}
		}
		return "dmClamp(" + a[0] + ", " + a[1] + ", " + a[2] + ")", res, nil
	case "modinv":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmModInv", declModInv)
		return "dmModInv(" + args[0] + ", " + args[1] + ")", ir.Int(), nil

	// -- text (fully compiled) --------------------------------------------------
	case "toint":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmToInt", declToInt, "strconv", "strings")
		return "dmToInt(" + args[0] + ")", ir.Int(), nil
	case "totext":
		if isFloatType(types[0]) {
			g.helper("dmFmtFloat", declFmtFloat, "strconv")
			return "dmFmtFloat(" + args[0] + ")", ir.Text(), nil
		}
		g.imp("strconv")
		return "strconv.FormatInt(" + args[0] + ", 10)", ir.Text(), nil
	case "occurrences":
		g.imp("strings")
		return "int64(strings.Count(" + args[0] + ", " + args[1] + "))", ir.Int(), nil
	case "repeats":
		g.helper("dmRepeats", declRepeats, "strings")
		return "dmRepeats(" + args[0] + ")", ir.Bool(), nil
	case "trim":
		g.imp("strings")
		return "strings.TrimSpace(" + args[0] + ")", ir.Text(), nil
	case "upper":
		g.imp("strings")
		return "strings.ToUpper(" + args[0] + ")", ir.Text(), nil
	case "lower":
		g.imp("strings")
		return "strings.ToLower(" + args[0] + ")", ir.Text(), nil
	case "chars":
		g.helper("dmChars", declChars)
		return "dmChars(" + args[0] + ")", ir.List(ir.Text()), nil
	case "startswith":
		g.imp("strings")
		return "strings.HasPrefix(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
	case "endswith":
		g.imp("strings")
		return "strings.HasSuffix(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
	case "replace":
		g.imp("strings")
		return "strings.ReplaceAll(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Text(), nil
	case "indexof":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.helper("dmIndexOfText", declIndexOfText, "strings", "unicode/utf8")
			return "dmIndexOfText(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
		}
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmIndexOf", declIndexOf)
		return "dmIndexOf(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "charat":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmASCII", declASCII, "unicode/utf8")
		g.helper("dmCharAt", declCharAt)
		return "dmCharAt(" + args[0] + ", " + args[1] + ")", ir.Text(), nil
	case "slice":
		g.helper("dmClampRange", declClampRange)
		if types[0] != nil && types[0].Kind == ir.KText {
			g.helper("dmASCII", declASCII, "unicode/utf8")
			g.helper("dmSliceText", declSliceText)
			return "dmSliceText(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Text(), nil
		}
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmSliceList", declSliceList)
		return "dmSliceList(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0], nil
	case "textjoin":
		elemT, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		if elemT != nil && elemT.Kind == ir.KText {
			g.imp("strings")
			return "strings.Join(" + args[0] + ", " + args[1] + ")", ir.Text(), nil
		}
		fn, err := g.joinFn(elemT)
		if err != nil {
			return "", nil, err
		}
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.Text(), nil

	// -- grid geometry ----------------------------------------------------------
	case "inbounds":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			return "", nil, fmt.Errorf("inbounds needs a Grid argument, got %s", types[0])
		}
		g.helper("dmGrid", declGrid)
		g.helper("dmInBounds", declInBounds)
		return "dmInBounds(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Bool(), nil

	// -- points (interned (Int, Int) tuple struct) ------------------------------
	case "point":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		return pt + "{" + args[0] + ", " + args[1] + "}", irPoint(), nil
	case "tuple":
		tt := ir.Tuple(types...)
		gt, err := g.tupleType(tt)
		if err != nil {
			return "", nil, err
		}
		return gt + "{" + strings.Join(args, ", ") + "}", tt, nil
	case "prow":
		return "(" + args[0] + ").f0", ir.Int(), nil
	case "pcol":
		return "(" + args[0] + ").f1", ir.Int(), nil
	case "padd":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmPAdd", fmt.Sprintf(`func dmPAdd(a, b %[1]s) %[1]s {
	return %[1]s{a.f0 + b.f0, a.f1 + b.f1}
}`, pt))
		return "dmPAdd(" + args[0] + ", " + args[1] + ")", irPoint(), nil
	case "psub":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmPSub", fmt.Sprintf(`func dmPSub(a, b %[1]s) %[1]s {
	return %[1]s{a.f0 - b.f0, a.f1 - b.f1}
}`, pt))
		return "dmPSub(" + args[0] + ", " + args[1] + ")", irPoint(), nil
	case "pscale":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmPScale", fmt.Sprintf(`func dmPScale(p %[1]s, n int64) %[1]s {
	return %[1]s{p.f0 * n, p.f1 * n}
}`, pt))
		return "dmPScale(" + args[0] + ", " + args[1] + ")", irPoint(), nil
	case "chebyshev":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmAbs", declAbs)
		g.helper("dmChebyshev", fmt.Sprintf(`func dmChebyshev(a, b %[1]s) int64 {
	dr, dc := dmAbs(a.f0-b.f0), dmAbs(a.f1-b.f1)
	if dr > dc {
		return dr
	}
	return dc
}`, pt))
		return "dmChebyshev(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "dirs8":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmDirs8", fmt.Sprintf(`func dmDirs8() []%[1]s {
	return []%[1]s{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
}`, pt))
		return "dmDirs8()", ir.List(irPoint()), nil
	case "around4", "around8":
		// Neighbours of a point with no grid and no bounds — what a Sparse
		// automaton needs, since neighbors4/8 require a dense Grid.
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		if name == "around4" {
			g.helper("dmAround4", fmt.Sprintf(`func dmAround4(p %[1]s) []%[1]s {
	return []%[1]s{{p.f0 - 1, p.f1}, {p.f0 + 1, p.f1}, {p.f0, p.f1 - 1}, {p.f0, p.f1 + 1}}
}`, pt))
			return "dmAround4(" + args[0] + ")", ir.List(irPoint()), nil
		}
		g.helper("dmAround8", fmt.Sprintf(`func dmAround8(p %[1]s) []%[1]s {
	return []%[1]s{
		{p.f0 - 1, p.f1 - 1}, {p.f0 - 1, p.f1}, {p.f0 - 1, p.f1 + 1},
		{p.f0, p.f1 - 1}, {p.f0, p.f1 + 1},
		{p.f0 + 1, p.f1 - 1}, {p.f0 + 1, p.f1}, {p.f0 + 1, p.f1 + 1},
	}
}`, pt))
		return "dmAround8(" + args[0] + ")", ir.List(irPoint()), nil
	case "haskey":
		g.helper("dmMap", declMap)
		return "func() bool { _, ok := (" + args[0] + ").vals[" + args[1] + "]; return ok }()", ir.Bool(), nil
	case "getor":
		// The total lookup: `get` errors on a missing key and there was no way
		// to guard it, which made a Count By map unreadable.
		g.helper("dmMap", declMap)
		valGo, err := g.goType(types[0].Elem)
		if err != nil {
			return "", nil, err
		}
		return "func() " + valGo + " { if v, ok := (" + args[0] + ").vals[" + args[1] +
			"]; ok { return v }; return " + args[2] + " }()", types[0].Elem, nil
	case "keys":
		g.helper("dmMap", declMap)
		return "(" + args[0] + ").keys", ir.List(types[0].Key), nil
	case "values":
		g.helper("dmMap", declMap)
		// The key and value types are not named by the (generic) helper, but
		// interning them here keeps their struct declarations emitted and
		// surfaces an uncompilable element type at the same point it used to.
		if _, err := g.goType(types[0].Key); err != nil {
			return "", nil, err
		}
		if _, err := g.goType(types[0].Elem); err != nil {
			return "", nil, err
		}
		g.helper("dmMapValues", `func dmMapValues[K comparable, V any](m dmMap[K, V]) []V {
	out := make([]V, 0, len(m.keys))
	for _, k := range m.keys {
		out = append(out, m.vals[k])
	}
	return out
}`)
		return "dmMapValues(" + args[0] + ")", ir.List(types[0].Elem), nil
	case "tolist":
		g.helper("dmSet", declSet)
		return "(" + args[0] + ").elems", ir.List(types[0].Elem), nil
	case "size":
		if types[0] != nil && types[0].Kind == ir.KGraph {
			g.helper("dmGraph", declGraph)
			return "int64(len((" + args[0] + ").nodes))", ir.Int(), nil
		}
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.helper("dmSet", declSet)
			return "int64(len((" + args[0] + ").elems))", ir.Int(), nil
		}
		g.helper("dmMap", declMap)
		return "int64(len((" + args[0] + ").keys))", ir.Int(), nil
	case "manhattan":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmAbs", declAbs)
		g.helper("dmManhattan", fmt.Sprintf(`func dmManhattan(a, b %s) int64 {
	return dmAbs(a.f0-b.f0) + dmAbs(a.f1-b.f1)
}`, pt))
		return "dmManhattan(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "rotl":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmRotL", fmt.Sprintf(`func dmRotL(p %[1]s) %[1]s {
	return %[1]s{-p.f1, p.f0}
}`, pt))
		return "dmRotL(" + args[0] + ")", irPoint(), nil
	case "rotr":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmRotR", fmt.Sprintf(`func dmRotR(p %[1]s) %[1]s {
	return %[1]s{p.f1, -p.f0}
}`, pt))
		return "dmRotR(" + args[0] + ")", irPoint(), nil
	case "dirs4":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmDirs4", fmt.Sprintf(`func dmDirs4() []%[1]s {
	return []%[1]s{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
}`, pt))
		return "dmDirs4()", ir.List(irPoint()), nil
	case "neighbors4", "neighbors8":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			return "", nil, fmt.Errorf("%s needs a Grid argument, got %s", name, types[0])
		}
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmGrid", declGrid)
		g.helper("dmNeighbors", fmt.Sprintf(`func dmNeighbors[T any](g dmGrid[T], r, c int64, diag bool) []%[1]s {
	deltas := []%[1]s{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	if diag {
		deltas = append(deltas, %[1]s{-1, -1}, %[1]s{-1, 1}, %[1]s{1, -1}, %[1]s{1, 1})
	}
	out := []%[1]s{}
	for _, d := range deltas {
		nr, nc := r+d.f0, c+d.f1
		if nr >= 0 && nr < int64(g.rows) && nc >= 0 && nc < int64(g.cols) {
			out = append(out, %[1]s{nr, nc})
		}
	}
	return out
}`, pt))
		diag := "false"
		if name == "neighbors8" {
			diag = "true"
		}
		return "dmNeighbors(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + diag + ")",
			ir.List(irPoint()), nil
	case "solve2x2":
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmSolve2x2", fmt.Sprintf(`func dmSolve2x2(a, b, c, d, e, f int64) %[1]s {
	det := a*e - b*d
	if det == 0 {
		dmFail("solve2x2: system has no unique solution (determinant is zero)")
	}
	xNum := c*e - b*f
	yNum := a*f - c*d
	if xNum%%det != 0 || yNum%%det != 0 {
		dmFail("solve2x2: solution is not integral")
	}
	return %[1]s{xNum / det, yNum / det}
}`, pt))
		return "dmSolve2x2(" + args[0] + ", " + args[1] + ", " + args[2] + ", " +
			args[3] + ", " + args[4] + ", " + args[5] + ")", irPoint(), nil
	// -- list/grid construction and access (A.2) --------------------------------
	case "list":
		elemGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		return "[]" + elemGo + "{" + strings.Join(args, ", ") + "}", ir.List(types[0]), nil
	case "set":
		if _, err := listElem(0); err != nil {
			return "", nil, err
		}
		g.helper("dmFail", declFail, "fmt", "os")
		// x.InPlace is the optimizer's proof that nothing reads the copied-from
		// value after this call and that no subslice of it escaped — see
		// optimizer/linear.go, and note that the List guard is stronger than
		// the one the Map and Grid updates need.
		if x.InPlace {
			g.helper("dmSetAtIn", declSetAtIn)
			return "dmSetAtIn(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0], nil
		}
		g.helper("dmSetAt", declSetAt)
		return "dmSetAt(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0], nil
	case "row", "col":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			return "", nil, fmt.Errorf("%s needs a Grid argument, got %s", name, types[0])
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGrid", declGrid)
		if name == "row" {
			g.helper("dmGridRow", declGridRow)
			return "dmGridRow(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem), nil
		}
		g.helper("dmGridCol", declGridCol)
		return "dmGridCol(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem), nil
	case "rows":
		return "int64((" + args[0] + ").rows)", ir.Int(), nil
	case "cols":
		return "int64((" + args[0] + ").cols)", ir.Int(), nil

	// -- bit operations ----------------------------------------------------------
	case "band":
		return "(" + args[0] + " & " + args[1] + ")", ir.Int(), nil
	case "bor":
		return "(" + args[0] + " | " + args[1] + ")", ir.Int(), nil
	case "bxor":
		return "(" + args[0] + " ^ " + args[1] + ")", ir.Int(), nil
	case "shl":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmShl", declShl)
		return "dmShl(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "shr":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmShr", declShr)
		return "dmShr(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "frombin":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFromBin", declFromBin, "strconv", "strings")
		return "dmFromBin(" + args[0] + ")", ir.Int(), nil

	// -- collection construction, update and enumeration -------------------------
	case "toset":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmSet", declSet)
		g.helper("dmToSet", declToSet)
		return "dmToSet(" + args[0] + ")", ir.Set(elem), nil
	case "emptyset":
		// The witness contributes nothing but its type — but it is still
		// *evaluated*, because the interpreter evaluates every argument before
		// dispatching and `emptyset(first(xs))` on an empty list has to fail in
		// both backends or the two disagree. Passing it to a func that ignores
		// it keeps the evaluation and discards the value.
		elemGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		g.helper("dmSet", declSet)
		return "func(" + elemGo + ") dmSet[" + elemGo + "] { return dmNewSet[" + elemGo +
			"]() }(" + args[0] + ")", ir.Set(types[0]), nil
	case "emptylist":
		// Same witness discipline as emptyset: the value is discarded but still
		// evaluated, so `emptylist(first(xs))` on an empty list fails in both
		// backends rather than one.
		elemGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		return "func(" + elemGo + ") []" + elemGo + " { return []" + elemGo +
			"{} }(" + args[0] + ")", ir.List(types[0]), nil
	case "emptymap":
		keyGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		valGo, err := g.goType(types[1])
		if err != nil {
			return "", nil, err
		}
		g.helper("dmMap", declMap)
		mapGo := "dmMap[" + keyGo + ", " + valGo + "]"
		return "func(" + keyGo + ", " + valGo + ") " + mapGo + " { return dmNewMap[" +
				keyGo + ", " + valGo + "]() }(" + args[0] + ", " + args[1] + ")",
			ir.Map(types[0], types[1]), nil
	case "tomap":
		elem, err := listElem(0)
		if err != nil {
			return "", nil, err
		}
		if elem == nil || elem.Kind != ir.KTuple || len(elem.Elems) != 2 {
			return "", nil, fmt.Errorf("tomap needs a List of (key, value) pairs, got List<%s>", elem)
		}
		mapT := ir.Map(elem.Elems[0], elem.Elems[1])
		mapGo, err := g.goType(mapT)
		if err != nil {
			return "", nil, err
		}
		pairGo, err := g.goType(elem)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmMap", declMap)
		// Parameterized rather than closed over, so the argument expression is
		// evaluated exactly once however big it is.
		return "func(ps []" + pairGo + ") " + mapGo + " { m := " + mapGo +
			"{keys: make([]" + mustGo(g, elem.Elems[0]) + ", 0, len(ps)), vals: make(map[" +
			mustGo(g, elem.Elems[0]) + "]" + mustGo(g, elem.Elems[1]) +
			", len(ps))}; for _, p := range ps { m.put(p.f0, p.f1) }; return m }(" +
			args[0] + ")", mapT, nil
	case "entries":
		if types[0] == nil || types[0].Kind != ir.KMap {
			return "", nil, fmt.Errorf("entries needs a Map argument, got %s", types[0])
		}
		pairT := ir.Tuple(types[0].Key, types[0].Elem)
		pairGo, err := g.goType(pairT)
		if err != nil {
			return "", nil, err
		}
		mapGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		g.helper("dmMap", declMap)
		return "func(m " + mapGo + ") []" + pairGo + " { out := make([]" + pairGo +
				", 0, len(m.keys)); for _, k := range m.keys { out = append(out, " + pairGo +
				"{f0: k, f1: m.vals[k]}) }; return out }(" + args[0] + ")",
			ir.List(pairT), nil
	case "insert":
		// x.InPlace is the optimizer's proof that nothing reads the
		// copied-from value after this call, so the clone is unobservable —
		// see optimizer/linear.go. The in-place helper is the functional one
		// minus the clone.
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.helper("dmSet", declSet)
			if x.InPlace {
				g.helper("dmSetAddIn", declSetAddIn)
				return "dmSetAddIn(" + args[0] + ", " + args[1] + ")", types[0], nil
			}
			g.helper("dmSetClone", declSetClone)
			g.helper("dmSetWith", declSetWith)
			return "dmSetWith(" + args[0] + ", " + args[1] + ")", types[0], nil
		}
		if types[0] == nil || types[0].Kind != ir.KMap {
			return "", nil, fmt.Errorf("insert needs a Set or Map argument, got %s", types[0])
		}
		g.helper("dmMap", declMap)
		if x.InPlace {
			g.helper("dmMapPutIn", declMapPutIn)
			return "dmMapPutIn(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0], nil
		}
		g.helper("dmMapClone", declMapClone)
		g.helper("dmMapWith", declMapWith)
		return "dmMapWith(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0], nil
	case "del":
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.helper("dmSet", declSet)
			g.helper("dmSetClone", declSetClone)
			g.helper("dmSetWithout", declSetWithout)
			return "dmSetWithout(" + args[0] + ", " + args[1] + ")", types[0], nil
		}
		if types[0] == nil || types[0].Kind != ir.KMap {
			return "", nil, fmt.Errorf("del needs a Set or Map argument, got %s", types[0])
		}
		g.helper("dmMap", declMap)
		g.helper("dmMapClone", declMapClone)
		g.helper("dmMapWithout", declMapWithout)
		return "dmMapWithout(" + args[0] + ", " + args[1] + ")", types[0], nil
	case "union", "intersect", "difference":
		g.helper("dmSet", declSet)
		switch name {
		case "union":
			g.helper("dmSetUnion", declSetUnion)
			return "dmSetUnion(" + args[0] + ", " + args[1] + ")", types[0], nil
		case "intersect":
			g.helper("dmSetIntersect", declSetIntersect)
			return "dmSetIntersect(" + args[0] + ", " + args[1] + ")", types[0], nil
		}
		g.helper("dmSetDiff", declSetDiff)
		return "dmSetDiff(" + args[0] + ", " + args[1] + ")", types[0], nil
	case "setat":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			return "", nil, fmt.Errorf("setat needs a Grid argument, got %s", types[0])
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGrid", declGrid)
		if x.InPlace {
			g.helper("dmGridSetIn", declGridSetIn)
			return "dmGridSetIn(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
				types[0], nil
		}
		g.helper("dmGridWith", declGridWith)
		return "dmGridWith(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
			types[0], nil
	case "cellpoints":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			return "", nil, fmt.Errorf("cellpoints needs a Sparse argument, got %s", types[0])
		}
		ptGo, err := g.goType(irPoint())
		if err != nil {
			return "", nil, err
		}
		return "func(ps []dmSPt) []" + ptGo + " { out := make([]" + ptGo +
				", len(ps)); for i, p := range ps { out[i] = " + ptGo +
				"{f0: p.r, f1: p.c} }; return out }((" + args[0] + ").pts())",
			ir.List(irPoint()), nil

	// -- graphs ------------------------------------------------------------------
	//
	// The updates go through dmGraphAddNode/AddEdge/DelEdge, which clone first:
	// the expression layer's graph updates are functional, exactly like the Map
	// and Sparse ones, and the dead-receiver pass is what takes the copy back.
	case "graph":
		gt, err := g.graphOut(x, types)
		if err != nil {
			return "", nil, err
		}
		fn, err := g.graphBuildFn(gt, types[0])
		if err != nil {
			return "", nil, err
		}
		return fn + "(" + args[0] + ")", gt, nil
	case "emptygraph":
		gt := ir.Graph(types[0])
		goT, err := g.goType(gt)
		if err != nil {
			return "", nil, err
		}
		// The argument is a type witness, but the interpreter evaluates every
		// argument before dispatching, so it has to be evaluated here too or the
		// two backends disagree about whether a failing witness runs.
		nodeGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		g.helper("dmGraph", declGraph)
		return "func(_ " + nodeGo + ") " + goT + " { return dmNewGraph[" + nodeGo + "]() }(" +
			args[0] + ")", gt, nil
	case "addnode":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("addnode needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		if x.InPlace {
			g.helper("dmGraphAddNodeIn", declGraphAddNodeIn)
			return "dmGraphAddNodeIn(" + args[0] + ", " + args[1] + ")", types[0], nil
		}
		g.helper("dmGraphAddNode", declGraphAddNode)
		return "dmGraphAddNode(" + args[0] + ", " + args[1] + ")", types[0], nil
	case "addedge":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("addedge needs a Graph argument, got %s", types[0])
		}
		w := "1"
		if len(args) == 4 {
			w = args[3]
		}
		g.helper("dmGraph", declGraph)
		if x.InPlace {
			g.helper("dmGraphAddEdgeIn", declGraphAddEdgeIn)
			return "dmGraphAddEdgeIn(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + w + ")",
				types[0], nil
		}
		g.helper("dmGraphAddEdge", declGraphAddEdge)
		return "dmGraphAddEdge(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + w + ")",
			types[0], nil
	case "deledge":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("deledge needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphDelEdge", declGraphDelEdge)
		return "dmGraphDelEdge(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0], nil
	case "nodes":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("nodes needs a Graph argument, got %s", types[0])
		}
		nodeGo, err := g.goType(types[0].Elem)
		if err != nil {
			return "", nil, err
		}
		g.helper("dmGraph", declGraph)
		// Copied, because the interpreter hands back a copy and a later append
		// on the result must not be visible through the graph.
		return "append([]" + nodeGo + "{}, (" + args[0] + ").nodes...)",
			ir.List(types[0].Elem), nil
	case "edges":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("edges needs a Graph argument, got %s", types[0])
		}
		fn, err := g.graphEdgesFn(types[0])
		if err != nil {
			return "", nil, err
		}
		return fn + "(" + args[0] + ")", ir.List(ir.Tuple(types[0].Elem, types[0].Elem, ir.Int())), nil
	case "neighbors":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("neighbors needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphNeighbors", declGraphNeighbors)
		return "dmGraphNeighbors(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem), nil
	case "edgesof":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("edgesof needs a Graph argument, got %s", types[0])
		}
		fn, err := g.graphEdgesOfFn(types[0])
		if err != nil {
			return "", nil, err
		}
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.List(ir.Tuple(types[0].Elem, ir.Int())), nil
	case "hasedge":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("hasedge needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		return "func() bool { _, ok := (" + args[0] + ").weight(" + args[1] + ", " + args[2] +
			"); return ok }()", ir.Bool(), nil
	case "weight":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("weight needs a Graph argument, got %s", types[0])
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphWeight", declGraphWeightAt)
		return "dmGraphWeight(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Int(), nil
	case "weightor":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("weightor needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphWeightOr", declGraphWeightOr)
		return "dmGraphWeightOr(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
			ir.Int(), nil
	case "degree":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("degree needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphDegree", declGraphDegree)
		return "dmGraphDegree(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "weightof":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("weightof needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphWeightOf", declGraphWeightOf)
		return "dmGraphWeightOf(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "root":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("root needs a Graph argument, got %s", types[0])
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphRoot", declGraphRoot, "fmt")
		return "dmGraphRoot(" + args[0] + ")", types[0].Elem, nil
	case "indegree":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("indegree needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphInDegree", declGraphInDegree)
		return "dmGraphInDegree(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "roots", "leaves":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("%s needs a Graph argument, got %s", name, types[0])
		}
		g.helper("dmGraph", declGraph)
		fn, decl := "dmGraphRoots", declGraphRoots
		if name == "leaves" {
			fn, decl = "dmGraphLeaves", declGraphLeaves
		}
		g.helper(fn, decl)
		return fn + "(" + args[0] + ")", ir.List(types[0].Elem), nil
	case "reachable":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("reachable needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphReachable", declGraphReachable)
		return "dmGraphReachable(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem), nil
	case "delnode":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("delnode needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphSub", declGraphSub) // dmGraphDelNode is written in terms of it
		g.helper("dmGraphDelNode", declGraphDelNode)
		return "dmGraphDelNode(" + args[0] + ", " + args[1] + ")", types[0], nil
	case "hascycle":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("hascycle needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphHasCycle", declGraphHasCycle)
		return "dmGraphHasCycle(" + args[0] + ")", ir.Bool(), nil
	case "weightsum":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("weightsum needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphWeightSum", declGraphWeightSum)
		return "dmGraphWeightSum(" + args[0] + ")", ir.Int(), nil
	case "undirected":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("undirected needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphUndirected", declGraphUndirected)
		return "dmGraphUndirected(" + args[0] + ")", types[0], nil
	case "mergegraphs":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("mergegraphs needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphMerge", declGraphMerge)
		return "dmGraphMerge(" + args[0] + ", " + args[1] + ")", types[0], nil
	case "flipedges":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("flipedges needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphFlip", declGraphFlip)
		return "dmGraphFlip(" + args[0] + ")", types[0], nil
	case "subgraph":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			return "", nil, fmt.Errorf("subgraph needs a Graph argument, got %s", types[0])
		}
		g.helper("dmGraph", declGraph)
		g.helper("dmGraphSub", declGraphSub)
		return "dmGraphSub(" + args[0] + ", " + args[1] + ")", types[0], nil

	// -- list generation ---------------------------------------------------------
	case "range":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmRange", declRange)
		return "dmRange(" + args[0] + ", " + args[1] + ")", ir.List(ir.Int()), nil
	case "fill":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFill", declFill)
		// Instantiate explicitly rather than letting Go infer T from the
		// value expression. An Int literal is an untyped constant, so
		// inference gives `int` and the result is a []int where the rest of
		// the program has agreed on []int64 — which compiles everywhere the
		// list is only ranged over, and fails the moment it is stored in a
		// tuple field. The interpreter has no such split, so this was a
		// backend divergence rather than a type error in the program.
		elem, terr := g.goType(types[1])
		if terr != nil {
			return "", nil, terr
		}
		return "dmFill[" + elem + "](" + args[0] + ", " + args[1] + ")", ir.List(types[1]), nil

	// -- text --------------------------------------------------------------------
	case "split":
		g.imp("strings")
		return "strings.Split(" + args[0] + ", " + args[1] + ")", ir.List(ir.Text()), nil
	case "words":
		g.imp("strings")
		return "strings.Fields(" + args[0] + ")", ir.List(ir.Text()), nil
	case "ord":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmOrd", declOrd, "unicode/utf8")
		return "dmOrd(" + args[0] + ")", ir.Int(), nil
	case "chr":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmChr", declChr)
		return "dmChr(" + args[0] + ")", ir.Text(), nil
	case "repeat":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmRepeatText", declRepeatText, "strings")
		return "dmRepeatText(" + args[0] + ", " + args[1] + ")", ir.Text(), nil
	case "padleft", "padright":
		g.helper("dmPadText", declPadText, "strings")
		return "dmPadText(" + args[0] + ", " + args[1] + ", " + args[2] + ", " +
			strconv.FormatBool(name == "padleft") + ")", ir.Text(), nil
	case "trimprefix":
		g.imp("strings")
		return "strings.TrimPrefix(" + args[0] + ", " + args[1] + ")", ir.Text(), nil
	case "trimsuffix":
		g.imp("strings")
		return "strings.TrimSuffix(" + args[0] + ", " + args[1] + ")", ir.Text(), nil
	case "isdigit", "isalpha", "isupper", "islower":
		g.helper("dmClassify", declClassify, "strings")
		fn := map[string]string{
			"isdigit": "dmIsDigit", "isalpha": "dmIsAlpha",
			"isupper": "dmIsUpper", "islower": "dmIsLower",
		}[name]
		return fn + "(" + args[0] + ")", ir.Bool(), nil

	// -- floats ------------------------------------------------------------------
	case "log", "log2", "log10", "exp", "sin", "cos", "tan":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFloat1", declFloat1, "math", "strconv")
		return "dmFloat1(" + strconv.Quote(name) + ", " + g.asFloat(args[0], types[0]) + ")",
			ir.Float(), nil
	case "atan2", "hypot":
		g.imp("math")
		fn := "math.Atan2"
		if name == "hypot" {
			fn = "math.Hypot"
		}
		return fn + "(" + g.asFloat(args[0], types[0]) + ", " + g.asFloat(args[1], types[1]) + ")",
			ir.Float(), nil
	case "trunc":
		if !isFloatType(types[0]) {
			return args[0], ir.Int(), nil
		}
		g.imp("math")
		return "int64(math.Trunc(" + args[0] + "))", ir.Int(), nil

	// -- records -----------------------------------------------------------------
	case "record":
		// The Record type was built from the literal field names at resolve
		// time; the struct declaration is interned by shape like every other
		// record, so this is a plain composite literal.
		recT, err := recordTypeOf(x, types)
		if err != nil {
			return "", nil, err
		}
		recGo, err := g.goType(recT)
		if err != nil {
			return "", nil, err
		}
		var b strings.Builder
		b.WriteString(recGo)
		b.WriteByte('{')
		for i, f := range recT.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fieldName(f.Name) + ": " + args[i*2+1])
		}
		b.WriteByte('}')
		return b.String(), recT, nil
	case "with":
		if types[0] == nil || types[0].Kind != ir.KRecord {
			return "", nil, fmt.Errorf("with needs a Record argument, got %s", types[0])
		}
		lit, ok := x.Args[1].(*ast.StringLit)
		if !ok {
			return "", nil, fmt.Errorf("with needs a literal field name")
		}
		recGo, err := g.goType(types[0])
		if err != nil {
			return "", nil, err
		}
		// A Go struct is a value, so the copy is the assignment: no allocation
		// and nothing shared with the original.
		return "func(r " + recGo + ") " + recGo + " { r." + fieldName(lit.Value) + " = " +
			args[2] + "; return r }(" + args[0] + ")", types[0], nil

	// -- bases, bits, number theory ----------------------------------------------
	case "frombase", "fromhex":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmParseBase", declParseBase, "strconv", "strings")
		base := "16"
		if name == "frombase" {
			base = args[1]
		}
		return "dmParseBase(" + strconv.Quote(name) + ", " + args[0] + ", " + base + ")",
			ir.Int(), nil
	case "tobase":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmToBase", declToBase, "strconv")
		return "dmToBase(" + args[0] + ", " + args[1] + ")", ir.Text(), nil
	case "tohex", "tobin":
		g.imp("strconv")
		base := "16"
		if name == "tobin" {
			base = "2"
		}
		return "strconv.FormatInt(" + args[0] + ", " + base + ")", ir.Text(), nil
	case "bnot":
		return "(^" + args[0] + ")", ir.Int(), nil
	case "popcount":
		g.imp("math/bits")
		return "int64(bits.OnesCount64(uint64(" + args[0] + ")))", ir.Int(), nil
	case "testbit":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmTestBit", declTestBit)
		return "dmTestBit(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
	case "digits":
		g.helper("dmDigits", declDigits)
		return "dmDigits(" + args[0] + ")", ir.List(ir.Int()), nil
	case "fromdigits":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFromDigits", declFromDigits, "math")
		return "dmFromDigits(" + args[0] + ")", ir.Int(), nil
	case "isprime":
		g.helper("dmModArith", declModArith, "math/bits")
		g.helper("dmIsPrime", declIsPrime, "math/bits")
		return "dmIsPrime(" + args[0] + ")", ir.Bool(), nil
	case "divisors":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmDivisors", declDivisors)
		return "dmDivisors(" + args[0] + ")", ir.List(ir.Int()), nil
	case "crt":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmModArith", declModArith, "math/bits")
		g.helper("dmCRT", declCRT)
		return "dmCRT(" + args[0] + ", " + args[1] + ")", ir.Int(), nil

	default:
		return "", nil, fmt.Errorf("unknown function %q", name)
	}
}

// mustGo is goType where the type has already been proved compilable by an
// earlier call in the same emitter; it keeps a composite literal readable
// instead of threading four error checks through it.
func mustGo(g *gen, t *ir.Type) string {
	s, err := g.goType(t)
	if err != nil {
		return "any"
	}
	return s
}

// recordTypeOf rebuilds the Record type of a `record(...)` call from its
// literal field names — the same rule typecheck applied, restated here because
// the compiled backend must not depend on the resolver having stashed it.
func recordTypeOf(x *ast.CallExpr, types []*ir.Type) (*ir.Type, error) {
	if len(types)%2 != 0 {
		return nil, fmt.Errorf("record takes name/value pairs, so an even number of arguments")
	}
	fields := make([]ir.Field, 0, len(types)/2)
	for i := 0; i < len(types); i += 2 {
		lit, ok := x.Args[i].(*ast.StringLit)
		if !ok {
			return nil, fmt.Errorf("record field name %d must be a literal", i/2+1)
		}
		fields = append(fields, ir.Field{Name: lit.Value, Type: types[i+1]})
	}
	return ir.Record(fields...), nil
}

// irPoint is the expression layer's point type: an (Int, Int) tuple of
// (row, col) — see typecheck.PointType.
func irPoint() *ir.Type { return ir.Tuple(ir.Int(), ir.Int()) }

// pointGo interns and returns the Go struct name for the point tuple type.
func (g *gen) pointGo() (string, error) { return g.tupleType(irPoint()) }

// reduceLiteral lowers max/min/sum over a list literal to inline scalar code:
// sum -> (e0 + e1 + ...), max/min -> nested dmMax2/dmMin2. Returns ok=false
// (so the caller uses the general slice path) when an element is non-numeric or
// the elements mix int and float.
func (g *gen) reduceLiteral(name string, elems []ast.Expr, env exprEnv) (string, *ir.Type, bool, error) {
	parts := make([]string, len(elems))
	var et *ir.Type
	for i, e := range elems {
		v, t, err := g.compileExpr(e, env)
		if err != nil {
			return "", nil, false, err
		}
		if !numericType(t) {
			return "", nil, false, nil
		}
		if et != nil && isFloatType(et) != isFloatType(t) {
			return "", nil, false, nil // mixed int/float: let the general path promote
		}
		parts[i], et = v, t
	}
	if name == "sum" {
		return "(" + strings.Join(parts, " + ") + ")", et, true, nil
	}
	base, decl := "dmMax2", declMax2
	if name == "min" {
		base, decl = "dmMin2", declMin2
	}
	expr := parts[len(parts)-1]
	if len(parts) > 1 {
		g.helper(base, decl)
		for i := len(parts) - 2; i >= 0; i-- {
			expr = base + "(" + parts[i] + ", " + expr + ")"
		}
	}
	return expr, et, true, nil
}

func mirrorCmp(op token.Kind) token.Kind {
	switch op {
	case token.LT:
		return token.GT
	case token.GT:
		return token.LT
	case token.LE:
		return token.GE
	case token.GE:
		return token.LE
	}
	return op
}

func callName(e ast.Expr) (*ast.CallExpr, string) {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, ""
	}
	id, ok := c.Fn.(*ast.Ident)
	if !ok {
		return nil, ""
	}
	return c, id.Name
}

// tryMaxCompare fuses `max(take|drop(seq, n)) OP scalar` (in either operand
// order, for <, <=, >, >=) into a short-circuiting, allocation-free scan. When
// seq is `col(g, c)` the scan strides the grid directly, so no column is
// materialized; when it is `row(g, r)` (or any list) the take/drop is a
// no-copy subslice. The helper fails on an empty range exactly as max does, so
// guarded uses behave identically. Returns ok=false (shape not matched) to let
// the ordinary comparison path run — including surfacing type errors there.
func (g *gen) tryMaxCompare(x *ast.BinaryExpr, env exprEnv) (string, *ir.Type, bool) {
	switch x.Op {
	case token.LT, token.GT, token.LE, token.GE:
	default:
		return "", nil, false
	}
	isMax := func(e ast.Expr) *ast.CallExpr {
		if c, n := callName(e); n == "max" && len(c.Args) == 1 {
			return c
		}
		return nil
	}
	lMax, rMax := isMax(x.Left), isMax(x.Right)
	var maxCall *ast.CallExpr
	var scalarExpr ast.Expr
	op := x.Op
	switch {
	case lMax != nil && rMax == nil:
		maxCall, scalarExpr = lMax, x.Right
	case rMax != nil && lMax == nil:
		maxCall, scalarExpr, op = rMax, x.Left, mirrorCmp(x.Op)
	default:
		return "", nil, false
	}
	td, tdName := callName(maxCall.Args[0])
	if (tdName != "take" && tdName != "drop") || len(td.Args) != 2 {
		return "", nil, false
	}
	isDrop := tdName == "drop"

	// Shape confirmed; compile the pieces we need.
	sv, st, err := g.compileExpr(scalarExpr, env)
	if err != nil || !numericType(st) {
		return "", nil, false
	}
	nv, _, err := g.compileExpr(td.Args[1], env)
	if err != nil {
		return "", nil, false
	}

	// max(range) >= s for LT/GE, max(range) > s for LE/GT; negate for LT/LE.
	useAtLeast := op == token.LT || op == token.GE
	negate := op == token.LT || op == token.LE

	var elem *ir.Type
	var call string
	if seqCall, sn := callName(td.Args[0]); sn == "col" && len(seqCall.Args) == 2 {
		gexpr, gt, gerr := g.compileExpr(seqCall.Args[0], env)
		cexpr, _, cerr := g.compileExpr(seqCall.Args[1], env)
		if gerr == nil && cerr == nil && gt != nil && gt.Kind == ir.KGrid && numericType(gt.Elem) {
			elem = gt.Elem
			g.helper("dmFail", declFail, "fmt", "os")
			g.helper("dmGrid", declGrid)
			base, decl := "dmColMaxAtLeast", declColMaxAtLeast
			if !useAtLeast {
				base, decl = "dmColMaxAbove", declColMaxAbove
			}
			g.helper(base, decl, "fmt", "os")
			call = fmt.Sprintf("%s(%s, %s, %s, %t, %s)", base, gexpr, cexpr, nv, isDrop, sv)
		}
	}
	if call == "" { // generic list (includes row(g,r), a no-copy subslice)
		seqv, seqt, serr := g.compileExpr(td.Args[0], env)
		if serr != nil || seqt == nil || seqt.Kind != ir.KList || !numericType(seqt.Elem) {
			return "", nil, false
		}
		elem = seqt.Elem
		sub, subDecl := "dmTake", declTake
		if isDrop {
			sub, subDecl = "dmDrop", declDrop
		}
		g.helper(sub, subDecl)
		g.helper("dmFail", declFail, "fmt", "os")
		base, decl := "dmMaxAtLeast", declMaxAtLeast
		if !useAtLeast {
			base, decl = "dmMaxAbove", declMaxAbove
		}
		g.helper(base, decl, "fmt", "os")
		call = fmt.Sprintf("%s(%s(%s, %s), %s)", base, sub, seqv, nv, sv)
	}
	if isFloatType(elem) != isFloatType(st) {
		return "", nil, false // mixed int/float: leave to the normal path
	}
	if negate {
		call = "(!" + call + ")"
	}
	return call, ir.Bool(), true
}

// modGo emits Euclidean `a % b`, choosing between the guarded helper and the
// unguarded one by whether the divisor can be zero. `divisor` is the source
// expression b was compiled from; a literal is the case worth recognising,
// because it is also the case where inlining buys the most — see declModNZ.
func (g *gen) modGo(a, b string, divisor ast.Expr) string {
	g.helper("dmModNZ", declModNZ)
	if lit, ok := divisor.(*ast.IntLit); ok && lit.Value != 0 {
		return "dmModNZ(" + a + ", " + b + ")"
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmMod", declMod)
	return "dmMod(" + a + ", " + b + ")"
}

func (g *gen) compileBinary(x *ast.BinaryExpr, env exprEnv) (string, *ir.Type, error) {
	// The fusion below rewrites the comparison into a different shape; an
	// operand that writes to a binding must be compiled as written.
	if !ast.HasUpdate(x) {
		if fused, ft, ok := g.tryMaxCompare(x, env); ok {
			return fused, ft, nil
		}
	}
	l, lt, err := g.compileExpr(x.Left, env)
	if err != nil {
		return "", nil, err
	}
	r, rt, err := g.compileExpr(x.Right, env)
	if err != nil {
		return "", nil, err
	}
	// Go orders the *function calls* in an expression left to right, but says
	// nothing about when a bare variable is read relative to them. With a
	// write anywhere in the operands that is a real disagreement in both
	// directions: `x + (x := 3)` could read x after the write, and
	// `(x := 3) + x` could read it before. Making both operands calls puts
	// them both under the rule Go does guarantee.
	//
	// and/or are exempt: Go evaluates a binary logical operation's operands in
	// order and short-circuits, exactly as eval does.
	if x.Op != token.AND && x.Op != token.OR && (ast.HasUpdate(x.Left) || ast.HasUpdate(x.Right)) {
		if l, err = g.ordered(l, lt); err != nil {
			return "", nil, err
		}
		if r, err = g.ordered(r, rt); err != nil {
			return "", nil, err
		}
	}
	switch x.Op {
	case token.AND:
		return "(" + l + " && " + r + ")", ir.Bool(), nil
	case token.OR:
		return "(" + l + " || " + r + ")", ir.Bool(), nil
	case token.PLUS, token.MINUS, token.STAR, token.SLASH:
		// Text + Text is concatenation, which Go spells the same way.
		if x.Op == token.PLUS && lt != nil && lt.Kind == ir.KText && rt != nil && rt.Kind == ir.KText {
			return "(" + l + " + " + r + ")", ir.Text(), nil
		}
		// Numeric promotion: mixing Int with Float computes in Float, so the
		// integer side is wrapped in a float64 conversion.
		res := ir.Int()
		if isFloatType(lt) || isFloatType(rt) {
			res = ir.Float()
			if !isFloatType(lt) {
				l = "float64(" + l + ")"
			}
			if !isFloatType(rt) {
				r = "float64(" + r + ")"
			}
		}
		switch x.Op {
		case token.PLUS:
			return "(" + l + " + " + r + ")", res, nil
		case token.MINUS:
			return "(" + l + " - " + r + ")", res, nil
		case token.STAR:
			return "(" + l + " * " + r + ")", res, nil
		}
		// The interpreter reports division by zero as a clean error; a bare
		// Go `/` would panic instead, so route through a guarded helper.
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmDiv", declDiv)
		return "dmDiv(" + l + ", " + r + ")", res, nil
	case token.PERCENT:
		// Euclidean, and guarded — Go's % is truncated and panics on zero.
		return g.modGo(l, r, x.Right), ir.Int(), nil
	case token.EQ:
		if (isFloatType(lt) || isFloatType(rt)) && numericType(lt) && numericType(rt) {
			if !isFloatType(lt) {
				l = "float64(" + l + ")"
			}
			if !isFloatType(rt) {
				r = "float64(" + r + ")"
			}
			return "(" + l + " == " + r + ")", ir.Bool(), nil
		}
		if lt == nil || rt == nil || !lt.Equal(rt) {
			return "", nil, fmt.Errorf("cannot compile = over %s and %s", lt, rt)
		}
		eq, err := g.eqExpr(l, r, lt)
		if err != nil {
			return "", nil, err
		}
		return eq, ir.Bool(), nil
	case token.LT, token.GT, token.LE, token.GE:
		op := map[token.Kind]string{token.LT: "<", token.GT: ">", token.LE: "<=", token.GE: ">="}[x.Op]
		// A tuple orders lexicographically, which Go has no operator for, so
		// it goes through an interned three-way compare. Int, Float and Text
		// all order with Go's own operator — byte-wise for strings, which is
		// what ir.Compare's strings.Compare does too.
		if lt != nil && lt.Kind == ir.KTuple {
			c, err := g.cmpExpr(l, r, lt)
			if err != nil {
				return "", nil, err
			}
			return "(" + c + " " + op + " 0)", ir.Bool(), nil
		}
		if isFloatType(lt) != isFloatType(rt) {
			if !isFloatType(lt) {
				l = "float64(" + l + ")"
			}
			if !isFloatType(rt) {
				r = "float64(" + r + ")"
			}
		}
		return "(" + l + " " + op + " " + r + ")", ir.Bool(), nil
	default:
		return "", nil, fmt.Errorf("unsupported operator %s", x.Op)
	}
}

func scalarKind(k ir.TypeKind) bool {
	return k == ir.KInt || k == ir.KFloat || k == ir.KText || k == ir.KBool
}

// isFloatType reports whether t is exactly Float.
func isFloatType(t *ir.Type) bool { return t != nil && t.Kind == ir.KFloat }

// asFloat widens an already-compiled argument to float64 when its Domain type
// is Int — the promotion the float builtins share with the operators.
func (g *gen) asFloat(arg string, t *ir.Type) string {
	if isFloatType(t) {
		return arg
	}
	return "float64(" + arg + ")"
}

// numericType reports whether t is Int or Float.
func numericType(t *ir.Type) bool {
	return t != nil && (t.Kind == ir.KInt || t.Kind == ir.KFloat)
}
