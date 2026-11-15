package codegen

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"domain/ast"
	"domain/builtins"
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
func (g *gen) compileExpr(e ast.Expr, env exprEnv) (string, *ir.Type) {
	switch x := e.(type) {
	case *ast.IntLit:
		// Wrapped for the same reason FloatLit is: an unwrapped integer literal
		// is an *untyped constant* in Go, and a generic helper whose type
		// parameter is fixed only by a scalar argument then infers `int` rather
		// than the int64 the rest of the program agrees on. That is a backend
		// divergence, not a type error in the program — the interpreter has no
		// such split — and it bit fill, abs, clamp and the two-argument min/max
		// before the literal was pinned here instead of at each call site.
		return "int64(" + strconv.FormatInt(x.Value, 10) + ")", ir.Int()
	case *ast.FloatLit:
		// Wrapped so the literal stays float64-typed in any Go context, even
		// when FormatFloat prints an integer-looking "2".
		return "float64(" + strconv.FormatFloat(x.Value, 'g', -1, 64) + ")", ir.Float()
	case *ast.BoolLit:
		return strconv.FormatBool(x.Value), ir.Bool()
	case *ast.StringLit:
		return strconv.Quote(x.Value), ir.Text()
	case *ast.BlockBody:
		// A lambda body written as a pipeline: lowered to a top-level function
		// and called here, so every emitter that wanted an expression gets one
		// (see blockgen.go).
		b, ok := env[x.Param]
		if !ok {
			panic(failf("block body has no input binding"))
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
					panic(failf("block body has no binding for %q", e.Name))
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
					panic(failf("unknown identifier %q", x.Name))
				}
			}
		}
		return b.expr, b.typ
	case *ast.GlobalRef:
		// No environment is consulted at all: the read was resolved to a slot
		// while the program was lowered, and the slot is a package-level
		// variable here. See codegen/globalgen.go.
		b := g.globalRef(x)
		return b.expr, b.typ
	case *ast.UnaryExpr:
		switch x.Op {
		case token.MINUS:
			v, vt := g.compileExpr(x.X, env)
			return "(-" + v + ")", vt
		case token.NOT:
			v, _ := g.compileExpr(x.X, env)
			return "(!" + v + ")", ir.Bool()
		}
		panic(failf("unsupported unary operator %s", x.Op))
	case *ast.BinaryExpr:
		return g.compileBinary(x, env)
	case *ast.FieldAccess:
		tv, tt := g.compileExpr(x.Target, env)
		if tt == nil || tt.Kind != ir.KRecord {
			panic(failf("field access .%s on non-record type %s", x.Field, tt))
		}
		for _, f := range tt.Fields {
			if f.Name == x.Field {
				return tv + "." + fieldName(f.Name), f.Type
			}
		}
		panic(failf("record %s has no field %q", tt, x.Field))
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
		panic(failf("unsupported expression %T", e))
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
func (g *gen) ordered(code string, t *ir.Type) string {
	goT := g.goType(t)
	return "func() " + goT + " { return " + code + " }()"
}

// orderArgs forces left-to-right evaluation across an argument list, in place.
// One writing argument puts *every* argument under the rule, in both
// directions: an argument to its left must be read before the write, and one
// to its right after it.
func (g *gen) orderArgs(exprs []ast.Expr, args []string, types []*ir.Type) {
	writes := slices.ContainsFunc(exprs, ast.HasUpdate)
	if !writes || len(args) < 2 {
		return
	}
	for i := range args {
		w := g.ordered(args[i], types[i])
		args[i] = w
	}
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
func (g *gen) compileAssign(x *ast.AssignExpr, env exprEnv) (string, *ir.Type) {
	var b exprBinding
	if x.Target != nil {
		// A global: the resolver set Target only when nothing nearer shadowed
		// the name, so the environment is not consulted — the same reason a
		// read of one does not consult it.
		gb := g.globalRef(x.Target)
		b = gb
	} else {
		var ok bool
		if b, ok = env[x.Name]; !ok {
			if b, ok = g.bindNames[x.Name]; !ok {
				panic(failf("unknown identifier %q", x.Name))
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
		panic(failf("%q is not a binding this backend can update", x.Name))
	}
	v, vt := g.compileExpr(x.Value, env)
	t := b.typ
	if t == nil {
		t = vt
	}
	goT := g.goType(t)
	return "func() " + goT + " {\n\t\t" + b.expr + " = " + v +
		"\n\t\treturn " + b.expr + "\n\t}()", t
}

// compileAlso lowers `body also c1, c2` to an immediately-invoked function
// that takes the body's value first and then runs the clauses for their
// effects. The body's value is held in a local before a clause can run,
// which is what makes a clause that updates what the body read change the
// *next* reader rather than this one.
func (g *gen) compileAlso(x *ast.AlsoExpr, env exprEnv) (string, *ir.Type) {
	body, bodyT := g.compileExpr(x.Body, env)
	goT := g.goType(bodyT)
	local := g.fresh("dmAlso")
	var b strings.Builder
	b.WriteString("func() " + goT + " {\n\t\tvar " + local + " " + goT + " = " + body + "\n")
	for _, c := range x.Clauses {
		cv, _ := g.compileExpr(c, env)
		// Assigned to the blank identifier: Go has no expression statements,
		// and the clause is written to be evaluated rather than used.
		b.WriteString("\t\t_ = " + cv + "\n")
	}
	b.WriteString("\t\treturn " + local + "\n\t}()")
	return b.String(), bodyT
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
func (g *gen) compileLet(x *ast.LetExpr, env exprEnv) (string, *ir.Type) {
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
	maps.Copy(inner, env)
	// Locals declared in this block, so a binding that shadows an earlier one
	// gets a fresh Go name. Nested closures gave each binding its own scope for
	// free; in one block a repeated `consider x` would be a redeclaration.
	declared := map[string]int{}

	cur := x
	for {
		// The value is compiled in the scope *before* its own binding, which is
		// what `inner` holds at this point: earlier bindings of the chain are
		// visible, this one is not.
		val, valT := g.compileExpr(cur.Value, inner)
		goValT := g.goType(valT)
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

	body, bodyT := g.compileExpr(cur.Body, inner)
	goBodyT := g.goType(bodyT)
	return "func() " + goBodyT + " {\n" + decls.String() +
		"\t\treturn " + body + "\n\t}()", bodyT
}

// compileCond lowers `if c then a else b` to an immediately-invoked func
// literal — the only Go expression form with lazy arms (there is no ternary),
// which the Go compiler inlines. Laziness is load-bearing: the unselected arm
// may be partial (`if length(xs) = 0 then 0 else first(xs)`).
func (g *gen) compileCond(x *ast.CondExpr, env exprEnv) (string, *ir.Type) {
	cond, _ := g.compileExpr(x.Cond, env)
	thenV, thenT := g.compileExpr(x.Then, env)
	elseV, _ := g.compileExpr(x.Else, env)
	goT := g.goType(thenT)
	return "func() " + goT + " {\n\t\tif " + cond + " {\n\t\t\treturn " + thenV +
		"\n\t\t}\n\t\treturn " + elseV + "\n\t}()", thenT
}

// compileCall lowers an expression-layer builtin (typecheck.Builtins) to a
// direct Go expression or a call to a generic dm* runtime helper. Names,
// arity, and argument types were validated by typecheck at resolve time.
func (g *gen) compileCall(x *ast.CallExpr, env exprEnv) (string, *ir.Type) {
	id, ok := x.Fn.(*ast.Ident)
	if !ok {
		panic(failf("only builtin functions can be called"))
	}
	name := id.Name
	// max/min/sum of a list *literal* lower to direct scalar ops (no slice, no
	// loop). Falls through to the general path when the shape doesn't match.
	if (name == "max" || name == "min" || name == "sum") && len(x.Args) == 1 {
		if inner, iname := callName(x.Args[0]); iname == "list" && len(inner.Args) >= 1 {
			if r, rt, ok := g.reduceLiteral(name, inner.Args, env); ok {
				return r, rt
			}
		}
	}
	args := make([]string, len(x.Args))
	types := make([]*ir.Type, len(x.Args))
	for i, a := range x.Args {
		av, at := g.compileExpr(a, env)
		args[i], types[i] = av, at
	}
	// Same ordering hazard as compileBinary's, one argument list wider: an
	// argument that writes must not be able to run before an argument written
	// to its left has been read.
	g.orderArgs(x.Args, args, types)
	listElem := func(i int) (*ir.Type, error) {
		if types[i] == nil || types[i].Kind != ir.KList {
			return nil, fmt.Errorf("%s needs a List argument, got %s", name, types[i])
		}
		return types[i].Elem, nil
	}

	// A builtin in the registry (package builtins) is emitted by the function
	// codegen attached to it (textbuiltins.go); the rest are the arms below.
	if spec := builtins.Lookup(name); spec != nil && spec.Emit != nil {
		return must2(spec.Emit(g, args, types))
	}

	switch name {
	case "tojson":
		fn := g.jsonFunc(types[0])
		return fn + "(" + args[0] + ")", ir.Text()
	case "pending":
		// Inside a game this is a lookup in the box that bounds requests to
		// one in flight per tag. Outside one there is no run to ask about and
		// nothing can be pending, which is the same answer the interpreter
		// gives when a context has no host to ask (eval/eval.go).
		if !g.game {
			return "false", ir.Bool()
		}
		return "dmPending(" + args[0] + ")", ir.Bool()

	// -- the run's stream and clock (see codegen/randgen.go) -------------------
	case "random":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmRand", declRand)
		return "dmRandInt(" + args[0] + ")", ir.Int()
	case "randomf":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmRand", declRand)
		return "dmRandFloat()", ir.Float()
	case "pick":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmRand", declRand)
		elem, err := listElem(0)
		if err != nil {
			// A Set reads as its elements everywhere else in the language.
			if types[0] != nil && types[0].Kind == ir.KSet {
				elem = types[0].Elem
			} else {
				panic(fail(err))
			}
		}
		fn := g.pickHelper(types[0], elem)
		return fn + "(" + args[0] + ")", elem
	case "frame":
		g.helper("dmRand", declRand)
		return "dmFrames", ir.Int()
	case "elapsed":
		g.helper("dmRand", declRand)
		return "dmElapsedMS", ir.Int()

	// -- View: the render tree (see codegen/viewgen.go) ------------------------
	case "text", "blank", "style", "stack", "beside", "box", "margin", "align", "fit", "draw":
		return g.compileViewCall(name, args, types)

	case "length":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.imp("unicode/utf8")
			return "int64(utf8.RuneCountInString(" + args[0] + "))", ir.Int()
		}
		if types[0] != nil && types[0].Kind == ir.KTuple {
			return fmt.Sprintf("int64(%d)", len(types[0].Elems)), ir.Int()
		}
		must(listElem(0))
		return "int64(len(" + args[0] + "))", ir.Int()
	case "item":
		// Tuple access compiles to a direct struct field — typecheck already
		// proved the index is a literal in range, so there is nothing to check
		// at runtime.
		if types[0] != nil && types[0].Kind == ir.KTuple {
			lit, ok := x.Args[1].(*ast.IntLit)
			if !ok {
				panic(failf("item over a Tuple needs a literal index"))
			}
			return "(" + args[0] + ")." + tupleField(int(lit.Value)), types[0].Elems[lit.Value]
		}
		elem := must(listElem(0))
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmItem", declItem)
		return "dmItem(" + args[0] + ", " + args[1] + ")", elem
	case "take":
		must(listElem(0))
		g.helper("dmTake", declTake)
		return "dmTake(" + args[0] + ", " + args[1] + ")", types[0]
	case "drop":
		must(listElem(0))
		g.helper("dmDrop", declDrop)
		return "dmDrop(" + args[0] + ", " + args[1] + ")", types[0]
	// -- first-order list operations ---------------------------------------------
	case "sort":
		elem := must(listElem(0))
		fn := g.sortFn(elem)
		return fn + "(" + args[0] + ")", types[0]
	case "unique":
		uelem := must(listElem(0))
		if ir.ContainsList(uelem) {
			kf := g.keyFn(uelem)
			g.helper("dmUniqueBy", declUniqueBy)
			return "dmUniqueBy(" + args[0] + ", " + kf + ")", types[0]
		}
		g.helper("dmUniqueList", declUniqueList)
		return "dmUniqueList(" + args[0] + ")", types[0]
	case "flatten":
		elem := must(listElem(0))
		g.helper("dmFlatten", declFlatten)
		return "dmFlatten(" + args[0] + ")", elem
	case "product":
		elem := must(listElem(0))
		g.helper("dmProduct", declProduct)
		return "dmProduct(" + args[0] + ")", elem
	case "zip":
		a := must(listElem(0))
		b := must(listElem(1))
		fn := g.pairFn("zip", a, b)
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.List(ir.Tuple(a, b))
	case "enumerate":
		elem := must(listElem(0))
		fn := g.pairFn("enumerate", ir.Int(), elem)
		return fn + "(" + args[0] + ")", ir.List(ir.Tuple(ir.Int(), elem))
	case "chunk", "windows":
		must(listElem(0))
		g.helper("dmFail", declFail, "fmt", "os")
		decl, fn := declChunk, "dmChunk"
		if name == "windows" {
			decl, fn = declWindows, "dmWindows"
		}
		g.helper(fn, decl)
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.List(types[0])
	case "transpose":
		must(listElem(0))
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmTransposeList", declTransposeList)
		return "dmTransposeList(" + args[0] + ")", types[0]
	case "reverse":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.helper("dmReverseText", declReverseText)
			return "dmReverseText(" + args[0] + ")", ir.Text()
		}
		must(listElem(0))
		g.helper("dmRev", declRev)
		return "dmRev(" + args[0] + ")", types[0]
	case "concat":
		must(listElem(0))
		g.helper("dmConcat", declConcat)
		return "dmConcat(" + args[0] + ", " + args[1] + ")", types[0]
	case "first":
		elem := must(listElem(0))
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFirst", declFirst)
		return "dmFirst(" + args[0] + ")", elem
	case "last":
		elem := must(listElem(0))
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmLast", declLast)
		return "dmLast(" + args[0] + ")", elem
	case "sum":
		elem := must(listElem(0))
		g.helper("dmSum", declSumInts)
		return "dmSum(" + args[0] + ")", elem
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
			return fn + "(" + a + ", " + b + ")", res
		}
		elem := must(listElem(0))
		g.helper("dmFail", declFail, "fmt", "os")
		if name == "min" {
			g.helper("dmMin", declMinInts)
			return "dmMin(" + args[0] + ")", elem
		}
		g.helper("dmMax", declMaxInts)
		return "dmMax(" + args[0] + ")", elem
	case "contains":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.imp("strings")
			return "strings.Contains(" + args[0] + ", " + args[1] + ")", ir.Bool()
		}
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.chelper(types[0].Elem, "dmSet", declSet)
			return g.chelper(types[0].Elem, "dmSetHas", declSetHas) + "(" + args[0] + ", " + args[1] + ")", ir.Bool()
		}
		if types[0] != nil && types[0].Kind == ir.KGraph {
			g.chelper(types[0].Elem, "dmGraph", declGraph)
			return "func() bool { _, ok := " + nodeIndex(types[0].Elem, "("+args[0]+")", args[1]) + "; return ok }()",
				ir.Bool()
		}
		must(listElem(0))
		if elem := types[0].Elem; lKeyed(elem) {
			kf := g.keyFn(elem)
			g.helper("dmLContains", declLContains)
			return "dmLContains(" + args[0] + ", " + args[1] + ", " + kf + ")", ir.Bool()
		}
		g.helper("dmContains", declContains)
		return "dmContains(" + args[0] + ", " + args[1] + ")", ir.Bool()
	case "get":
		if types[0] == nil || types[0].Kind != ir.KMap {
			panic(failf("get needs a Map argument, got %s", types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.chelper(types[0].Key, "dmMap", declMap)
		return g.chelper(types[0].Key, "dmMapGet", declMapGet) + "(" + args[0] + ", " + args[1] + ")", types[0].Elem
	case "at":
		if types[0] != nil && types[0].Kind == ir.KSparse {
			// Total on the infinite plane: unset cells read the default.
			return "(" + args[0] + ").at(" + args[1] + ", " + args[2] + ")", types[0].Elem
		}
		if types[0] == nil || types[0].Kind != ir.KGrid {
			panic(failf("at needs a Grid argument, got %s", types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGrid", declGrid)
		g.helper("dmGridAt", declGridAt)
		return "dmGridAt(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0].Elem

	// -- sparse grids (H) --------------------------------------------------------
	case "sparse":
		elemGo := g.goType(types[0])
		g.helper("dmSparse", declSparse, "slices")
		return "dmNewSparse[" + elemGo + "](" + args[0] + ")", ir.Sparse(types[0])
	case "put":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			panic(failf("put needs a Sparse argument, got %s", types[0]))
		}
		g.helper("dmSparse", declSparse, "slices")
		if x.InPlace {
			g.helper("dmSparsePutIn", declSparsePutIn)
			return "dmSparsePutIn(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
				types[0]
		}
		g.helper("dmSparsePut", declSparsePut)
		return "dmSparsePut(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
			types[0]
	case "has":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			panic(failf("has needs a Sparse argument, got %s", types[0]))
		}
		return "(" + args[0] + ").has(" + args[1] + ", " + args[2] + ")", ir.Bool()
	case "cells":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			panic(failf("cells needs a Sparse argument, got %s", types[0]))
		}
		return "int64((" + args[0] + ").count())", ir.Int()
	case "minrow", "maxrow", "mincol", "maxcol":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			panic(failf("%s needs a Sparse argument, got %s", name, types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmSparseBound", declSparseBound)
		return "dmSparseBound(" + args[0] + ", " + goStr(name) + ")", ir.Int()

	// -- mod and divmod read the divisor's source, so they stay here --------------
	case "mod":
		return g.modGo(args[0], args[1], x.Args[1]), ir.Int()
	case "divmod":
		// Built inline rather than via a helper: the result is a generated
		// tuple struct whose name is only known here.
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmDiv", declDiv)
		pt := g.pointGo()
		a, b := args[0], args[1]
		mod := g.modGo(a, b, x.Args[1])
		return pt + "{dmDiv(" + a + " - " + mod + ", " + b + "), " + mod + "}",
			irPoint()
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
		return "dmClamp(" + a[0] + ", " + a[1] + ", " + a[2] + ")", res
	// -- text (fully compiled) --------------------------------------------------
	case "indexof":
		if types[0] != nil && types[0].Kind == ir.KText {
			g.helper("dmIndexOfText", declIndexOfText, "strings", "unicode/utf8")
			return "dmIndexOfText(" + args[0] + ", " + args[1] + ")", ir.Int()
		}
		must(listElem(0))
		if elem := types[0].Elem; lKeyed(elem) {
			kf := g.keyFn(elem)
			g.helper("dmLIndexOf", declLIndexOf)
			return "dmLIndexOf(" + args[0] + ", " + args[1] + ", " + kf + ")", ir.Int()
		}
		g.helper("dmIndexOf", declIndexOf)
		return "dmIndexOf(" + args[0] + ", " + args[1] + ")", ir.Int()
	case "slice":
		g.helper("dmClampRange", declClampRange)
		if types[0] != nil && types[0].Kind == ir.KText {
			g.helper("dmASCII", declASCII, "unicode/utf8")
			g.helper("dmSliceText", declSliceText)
			return "dmSliceText(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Text()
		}
		must(listElem(0))
		g.helper("dmSliceList", declSliceList)
		return "dmSliceList(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0]
	case "textjoin":
		elemT := must(listElem(0))
		if elemT != nil && elemT.Kind == ir.KText {
			g.imp("strings")
			return "strings.Join(" + args[0] + ", " + args[1] + ")", ir.Text()
		}
		fn := g.joinFn(elemT)
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.Text()

	// -- grid geometry ----------------------------------------------------------
	case "inbounds":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			panic(failf("inbounds needs a Grid argument, got %s", types[0]))
		}
		g.helper("dmGrid", declGrid)
		g.helper("dmInBounds", declInBounds)
		return "dmInBounds(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Bool()

	// -- points (interned (Int, Int) tuple struct) ------------------------------
	case "point":
		pt := g.pointGo()
		return pt + "{" + args[0] + ", " + args[1] + "}", irPoint()
	case "tuple":
		tt := ir.Tuple(types...)
		gt := g.tupleType(tt)
		return gt + "{" + strings.Join(args, ", ") + "}", tt
	case "prow":
		return "(" + args[0] + ").f0", ir.Int()
	case "pcol":
		return "(" + args[0] + ").f1", ir.Int()
	case "padd":
		pt := g.pointGo()
		g.helper("dmPAdd", fmt.Sprintf(`func dmPAdd(a, b %[1]s) %[1]s {
	return %[1]s{a.f0 + b.f0, a.f1 + b.f1}
}`, pt))
		return "dmPAdd(" + args[0] + ", " + args[1] + ")", irPoint()
	case "psub":
		pt := g.pointGo()
		g.helper("dmPSub", fmt.Sprintf(`func dmPSub(a, b %[1]s) %[1]s {
	return %[1]s{a.f0 - b.f0, a.f1 - b.f1}
}`, pt))
		return "dmPSub(" + args[0] + ", " + args[1] + ")", irPoint()
	case "pscale":
		pt := g.pointGo()
		g.helper("dmPScale", fmt.Sprintf(`func dmPScale(p %[1]s, n int64) %[1]s {
	return %[1]s{p.f0 * n, p.f1 * n}
}`, pt))
		return "dmPScale(" + args[0] + ", " + args[1] + ")", irPoint()
	case "chebyshev":
		pt := g.pointGo()
		g.helper("dmAbs", declAbs)
		g.helper("dmChebyshev", fmt.Sprintf(`func dmChebyshev(a, b %[1]s) int64 {
	dr, dc := dmAbs(a.f0-b.f0), dmAbs(a.f1-b.f1)
	if dr > dc {
		return dr
	}
	return dc
}`, pt))
		return "dmChebyshev(" + args[0] + ", " + args[1] + ")", ir.Int()
	case "nearby4", "nearby8":
		must(listElem(0))
		pt := g.pointGo()
		g.helper("dmAbs", declAbs)
		if name == "nearby4" {
			g.helper("dmNearby4", fmt.Sprintf(`func dmNearby4(ps []%[1]s, center %[1]s, radius int64) []int64 {
	var out []int64
	for i, p := range ps {
		if d := dmAbs(p.f0-center.f0) + dmAbs(p.f1-center.f1); radius >= 0 && d <= radius {
			out = append(out, int64(i))
		}
	}
	return out
}`, pt))
			return "dmNearby4(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.List(ir.Int())
		}
		g.helper("dmNearby8", fmt.Sprintf(`func dmNearby8(ps []%[1]s, center %[1]s, radius int64) []int64 {
	var out []int64
	for i, p := range ps {
		dr, dc := dmAbs(p.f0-center.f0), dmAbs(p.f1-center.f1)
		d := dr
		if dc > d {
			d = dc
		}
		if radius >= 0 && d <= radius {
			out = append(out, int64(i))
		}
	}
	return out
}`, pt))
		return "dmNearby8(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.List(ir.Int())
	case "dirs8":
		pt := g.pointGo()
		g.helper("dmDirs8", fmt.Sprintf(`func dmDirs8() []%[1]s {
	return []%[1]s{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
}`, pt))
		return "dmDirs8()", ir.List(irPoint())
	case "around4", "around8":
		// Neighbours of a point with no grid and no bounds — what a Sparse
		// automaton needs, since neighbors4/8 require a dense Grid.
		pt := g.pointGo()
		if name == "around4" {
			g.helper("dmAround4", fmt.Sprintf(`func dmAround4(p %[1]s) []%[1]s {
	return []%[1]s{{p.f0 - 1, p.f1}, {p.f0 + 1, p.f1}, {p.f0, p.f1 - 1}, {p.f0, p.f1 + 1}}
}`, pt))
			return "dmAround4(" + args[0] + ")", ir.List(irPoint())
		}
		g.helper("dmAround8", fmt.Sprintf(`func dmAround8(p %[1]s) []%[1]s {
	return []%[1]s{
		{p.f0 - 1, p.f1 - 1}, {p.f0 - 1, p.f1}, {p.f0 - 1, p.f1 + 1},
		{p.f0, p.f1 - 1}, {p.f0, p.f1 + 1},
		{p.f0 + 1, p.f1 - 1}, {p.f0 + 1, p.f1}, {p.f0 + 1, p.f1 + 1},
	}
}`, pt))
		return "dmAround8(" + args[0] + ")", ir.List(irPoint())
	case "haskey":
		g.chelper(types[0].Key, "dmMap", declMap)
		if lKeyed(types[0].Key) {
			return "func() bool { m := " + args[0] + "; return m.has(" + args[1] + ") }()", ir.Bool()
		}
		return "func() bool { _, ok := (" + args[0] + ").idx[" + args[1] + "]; return ok }()", ir.Bool()
	case "getor":
		// The total lookup: `get` errors on a missing key and there was no way
		// to guard it, which made a Count By map unreadable.
		g.chelper(types[0].Key, "dmMap", declMap)
		valGo := g.goType(types[0].Elem)
		return "func() " + valGo + " { if v, ok := (" + args[0] + ").get(" + args[1] +
			"); ok { return v }; return " + args[2] + " }()", types[0].Elem
	case "keys":
		g.chelper(types[0].Key, "dmMap", declMap)
		return "(" + args[0] + ").keys", ir.List(types[0].Key)
	case "pluck":
		if types[0] == nil || types[0].Kind != ir.KMap {
			panic(failf("pluck needs a Map argument, got %s", types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.chelper(types[0].Key, "dmMap", declMap)
		return g.chelper(types[0].Key, "dmPluck", `func dmPluck[K comparable, V any](m dmMap[K, V], ks []K) []V {
	out := make([]V, len(ks))
	for i, k := range ks {
		v, ok := m.get(k)
		if !ok {
			dmFail("pluck: no entry for %v", k)
		}
		out[i] = v
	}
	return out
}`) + "(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem)
	case "values":
		g.chelper(types[0].Key, "dmMap", declMap)
		// The key and value types are not named by the (generic) helper, but
		// interning them here keeps their struct declarations emitted and
		// surfaces an uncompilable element type at the same point it used to.
		g.goType(types[0].Key)
		g.goType(types[0].Elem)
		return g.chelper(types[0].Key, "dmMapValues", `func dmMapValues[K comparable, V any](m dmMap[K, V]) []V {
	return append([]V(nil), m.vs...)
}`) + "(" + args[0] + ")", ir.List(types[0].Elem)
	case "tolist":
		g.chelper(types[0].Elem, "dmSet", declSet)
		return "(" + args[0] + ").elems", ir.List(types[0].Elem)
	case "size":
		if types[0] != nil && types[0].Kind == ir.KGraph {
			g.chelper(types[0].Elem, "dmGraph", declGraph)
			return "int64(len((" + args[0] + ").nodes))", ir.Int()
		}
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.chelper(types[0].Elem, "dmSet", declSet)
			return "int64(len((" + args[0] + ").elems))", ir.Int()
		}
		g.chelper(types[0].Key, "dmMap", declMap)
		return "int64(len((" + args[0] + ").keys))", ir.Int()
	case "manhattan":
		pt := g.pointGo()
		g.helper("dmAbs", declAbs)
		g.helper("dmManhattan", fmt.Sprintf(`func dmManhattan(a, b %s) int64 {
	return dmAbs(a.f0-b.f0) + dmAbs(a.f1-b.f1)
}`, pt))
		return "dmManhattan(" + args[0] + ", " + args[1] + ")", ir.Int()
	case "rotl":
		pt := g.pointGo()
		g.helper("dmRotL", fmt.Sprintf(`func dmRotL(p %[1]s) %[1]s {
	return %[1]s{-p.f1, p.f0}
}`, pt))
		return "dmRotL(" + args[0] + ")", irPoint()
	case "rotr":
		pt := g.pointGo()
		g.helper("dmRotR", fmt.Sprintf(`func dmRotR(p %[1]s) %[1]s {
	return %[1]s{p.f1, -p.f0}
}`, pt))
		return "dmRotR(" + args[0] + ")", irPoint()
	case "dirs4":
		pt := g.pointGo()
		g.helper("dmDirs4", fmt.Sprintf(`func dmDirs4() []%[1]s {
	return []%[1]s{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
}`, pt))
		return "dmDirs4()", ir.List(irPoint())
	case "neighbors4", "neighbors8":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			panic(failf("%s needs a Grid argument, got %s", name, types[0]))
		}
		pt := g.pointGo()
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
			ir.List(irPoint())
	// -- list/grid construction and access (A.2) --------------------------------
	case "list":
		elemGo := g.goType(types[0])
		return "[]" + elemGo + "{" + strings.Join(args, ", ") + "}", ir.List(types[0])
	case "set":
		must(listElem(0))
		g.helper("dmFail", declFail, "fmt", "os")
		// x.InPlace is the optimizer's proof that nothing reads the copied-from
		// value after this call and that no subslice of it escaped — see
		// optimizer/linear.go, and note that the List guard is stronger than
		// the one the Map and Grid updates need.
		if x.InPlace {
			g.helper("dmSetAtIn", declSetAtIn)
			return "dmSetAtIn(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0]
		}
		g.helper("dmSetAt", declSetAt)
		return "dmSetAt(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0]
	case "row", "col":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			panic(failf("%s needs a Grid argument, got %s", name, types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGrid", declGrid)
		if name == "row" {
			g.helper("dmGridRow", declGridRow)
			return "dmGridRow(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem)
		}
		g.helper("dmGridCol", declGridCol)
		return "dmGridCol(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem)
	case "rows":
		return "int64((" + args[0] + ").rows)", ir.Int()
	case "cols":
		return "int64((" + args[0] + ").cols)", ir.Int()

	// -- bit operations ----------------------------------------------------------
	case "frombin":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmFromBin", declFromBin, "strconv", "strings")
		return "dmFromBin(" + args[0] + ")", ir.Int()

	// -- collection construction, update and enumeration -------------------------
	case "toset":
		elem := must(listElem(0))
		g.chelper(elem, "dmSet", declSet)
		if lKeyed(elem) {
			kf := g.keyFn(elem)
			return g.chelper(elem, "dmToSet", declToSet) + "(" + args[0] + ", " + kf + ")", ir.Set(elem)
		}
		return g.chelper(elem, "dmToSet", declToSet) + "(" + args[0] + ")", ir.Set(elem)
	case "emptyset":
		// The witness contributes nothing but its type — but it is still
		// *evaluated*, because the interpreter evaluates every argument before
		// dispatching and `emptyset(first(xs))` on an empty list has to fail in
		// both backends or the two disagree. Passing it to a func that ignores
		// it keeps the evaluation and discards the value.
		elemGo := g.goType(types[0])
		newSet := g.newSetExpr(types[0], elemGo)
		setGo := g.goType(ir.Set(types[0]))
		return "func(" + elemGo + ") " + setGo + " { return " + newSet + " }(" + args[0] + ")", ir.Set(types[0])
	case "emptylist":
		// Same witness discipline as emptyset: the value is discarded but still
		// evaluated, so `emptylist(first(xs))` on an empty list fails in both
		// backends rather than one.
		elemGo := g.goType(types[0])
		return "func(" + elemGo + ") []" + elemGo + " { return []" + elemGo +
			"{} }(" + args[0] + ")", ir.List(types[0])
	case "emptymap":
		keyGo := g.goType(types[0])
		valGo := g.goType(types[1])
		newMap := g.newMapExpr(types[0], keyGo, valGo, "")
		mapGo := g.goType(ir.Map(types[0], types[1]))
		return "func(" + keyGo + ", " + valGo + ") " + mapGo + " { return " + newMap + " }(" + args[0] + ", " + args[1] + ")",
			ir.Map(types[0], types[1])
	case "tomap":
		elem := must(listElem(0))
		if elem == nil || elem.Kind != ir.KTuple || len(elem.Elems) != 2 {
			panic(failf("tomap needs a List of (key, value) pairs, got List<%s>", elem))
		}
		mapT := ir.Map(elem.Elems[0], elem.Elems[1])
		mapGo := g.goType(mapT)
		pairGo := g.goType(elem)
		newMap := g.newMapExpr(elem.Elems[0], mustGo(g, elem.Elems[0]), mustGo(g, elem.Elems[1]), "len(ps)")
		// Parameterized rather than closed over, so the argument expression is
		// evaluated exactly once however big it is.
		return "func(ps []" + pairGo + ") " + mapGo + " { m := " + newMap +
			"; for _, p := range ps { m.put(p.f0, p.f1) }; return m }(" +
			args[0] + ")", mapT
	case "entries":
		if types[0] == nil || types[0].Kind != ir.KMap {
			panic(failf("entries needs a Map argument, got %s", types[0]))
		}
		pairT := ir.Tuple(types[0].Key, types[0].Elem)
		pairGo := g.goType(pairT)
		mapGo := g.goType(types[0])
		g.chelper(types[0].Key, "dmMap", declMap)
		return "func(m " + mapGo + ") []" + pairGo + " { out := make([]" + pairGo +
				", 0, len(m.keys)); for i, k := range m.keys { out = append(out, " + pairGo +
				"{f0: k, f1: m.vs[i]}) }; return out }(" + args[0] + ")",
			ir.List(pairT)
	case "insert":
		// x.InPlace is the optimizer's proof that nothing reads the
		// copied-from value after this call, so the clone is unobservable —
		// see optimizer/linear.go. The in-place helper is the functional one
		// minus the clone.
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.chelper(types[0].Elem, "dmSet", declSet)
			if x.InPlace {
				return g.chelper(types[0].Elem, "dmSetAddIn", declSetAddIn) + "(" + args[0] + ", " + args[1] + ")", types[0]
			}
			g.chelper(types[0].Elem, "dmSetClone", declSetClone)
			return g.chelper(types[0].Elem, "dmSetWith", declSetWith) + "(" + args[0] + ", " + args[1] + ")", types[0]
		}
		if types[0] == nil || types[0].Kind != ir.KMap {
			panic(failf("insert needs a Set or Map argument, got %s", types[0]))
		}
		g.chelper(types[0].Key, "dmMap", declMap)
		if x.InPlace {
			return g.chelper(types[0].Key, "dmMapPutIn", declMapPutIn) + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0]
		}
		g.chelper(types[0].Key, "dmMapClone", declMapClone)
		return g.chelper(types[0].Key, "dmMapWith", declMapWith) + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0]
	case "del":
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.chelper(types[0].Elem, "dmSet", declSet)
			g.chelper(types[0].Elem, "dmSetClone", declSetClone)
			return g.chelper(types[0].Elem, "dmSetWithout", declSetWithout) + "(" + args[0] + ", " + args[1] + ")", types[0]
		}
		if types[0] == nil || types[0].Kind != ir.KMap {
			panic(failf("del needs a Set or Map argument, got %s", types[0]))
		}
		g.chelper(types[0].Key, "dmMap", declMap)
		g.chelper(types[0].Key, "dmMapClone", declMapClone)
		return g.chelper(types[0].Key, "dmMapWithout", declMapWithout) + "(" + args[0] + ", " + args[1] + ")", types[0]
	case "union", "intersect", "difference":
		g.chelper(types[0].Elem, "dmSet", declSet)
		switch name {
		case "union":
			return g.chelper(types[0].Elem, "dmSetUnion", declSetUnion) + "(" + args[0] + ", " + args[1] + ")", types[0]
		case "intersect":
			return g.chelper(types[0].Elem, "dmSetIntersect", declSetIntersect) + "(" + args[0] + ", " + args[1] + ")", types[0]
		}
		return g.chelper(types[0].Elem, "dmSetDiff", declSetDiff) + "(" + args[0] + ", " + args[1] + ")", types[0]
	case "setat":
		if types[0] == nil || types[0].Kind != ir.KGrid {
			panic(failf("setat needs a Grid argument, got %s", types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmGrid", declGrid)
		if x.InPlace {
			g.helper("dmGridSetIn", declGridSetIn)
			return "dmGridSetIn(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
				types[0]
		}
		g.helper("dmGridWith", declGridWith)
		return "dmGridWith(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
			types[0]
	case "cellpoints":
		if types[0] == nil || types[0].Kind != ir.KSparse {
			panic(failf("cellpoints needs a Sparse argument, got %s", types[0]))
		}
		ptGo := g.goType(irPoint())
		return "func(ps []dmSPt) []" + ptGo + " { out := make([]" + ptGo +
				", len(ps)); for i, p := range ps { out[i] = " + ptGo +
				"{f0: p.r, f1: p.c} }; return out }((" + args[0] + ").pts())",
			ir.List(irPoint())

	// -- graphs ------------------------------------------------------------------
	//
	// The updates go through dmGraphAddNode/AddEdge/DelEdge, which clone first:
	// the expression layer's graph updates are functional, exactly like the Map
	// and Sparse ones, and the dead-receiver pass is what takes the copy back.
	case "graph":
		gt := g.graphOut(x, types)
		fn := g.graphBuildFn(gt, types[0])
		return fn + "(" + args[0] + ")", gt
	case "emptygraph":
		gt := ir.Graph(types[0])
		goT := g.goType(gt)
		// The argument is a type witness, but the interpreter evaluates every
		// argument before dispatching, so it has to be evaluated here too or the
		// two backends disagree about whether a failing witness runs.
		nodeGo := g.goType(types[0])
		ng := g.newGraphExpr(types[0], nodeGo)
		return "func(_ " + nodeGo + ") " + goT + " { return " + ng + " }(" +
			args[0] + ")", gt
	case "addnode":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("addnode needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		if x.InPlace {
			g.chelper(types[0].Elem, "dmGraphAddNodeIn", declGraphAddNodeIn)
			return cname(types[0].Elem, "dmGraphAddNodeIn") + "(" + args[0] + ", " + args[1] + ")", types[0]
		}
		g.chelper(types[0].Elem, "dmGraphAddNode", declGraphAddNode)
		return cname(types[0].Elem, "dmGraphAddNode") + "(" + args[0] + ", " + args[1] + ")", types[0]
	case "addedge":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("addedge needs a Graph argument, got %s", types[0]))
		}
		w := "1"
		if len(args) == 4 {
			w = args[3]
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		if x.InPlace {
			g.chelper(types[0].Elem, "dmGraphAddEdgeIn", declGraphAddEdgeIn)
			return cname(types[0].Elem, "dmGraphAddEdgeIn") + "(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + w + ")",
				types[0]
		}
		g.chelper(types[0].Elem, "dmGraphAddEdge", declGraphAddEdge)
		return cname(types[0].Elem, "dmGraphAddEdge") + "(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + w + ")",
			types[0]
	case "deledge":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("deledge needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphDelEdge", declGraphDelEdge)
		return cname(types[0].Elem, "dmGraphDelEdge") + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", types[0]
	case "nodes":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("nodes needs a Graph argument, got %s", types[0]))
		}
		nodeGo := g.goType(types[0].Elem)
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		// Copied, because the interpreter hands back a copy and a later append
		// on the result must not be visible through the graph.
		return "append([]" + nodeGo + "{}, (" + args[0] + ").nodes...)",
			ir.List(types[0].Elem)
	case "edges":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("edges needs a Graph argument, got %s", types[0]))
		}
		fn := g.graphEdgesFn(types[0])
		return fn + "(" + args[0] + ")", ir.List(ir.Tuple(types[0].Elem, types[0].Elem, ir.Int()))
	case "neighbors":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("neighbors needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphNeighbors", declGraphNeighbors)
		return cname(types[0].Elem, "dmGraphNeighbors") + "(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem)
	case "edgesof":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("edgesof needs a Graph argument, got %s", types[0]))
		}
		fn := g.graphEdgesOfFn(types[0])
		return fn + "(" + args[0] + ", " + args[1] + ")", ir.List(ir.Tuple(types[0].Elem, ir.Int()))
	case "hasedge":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("hasedge needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		return "func() bool { _, ok := (" + args[0] + ").weight(" + args[1] + ", " + args[2] +
			"); return ok }()", ir.Bool()
	case "weight":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("weight needs a Graph argument, got %s", types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphWeight", declGraphWeightAt)
		return cname(types[0].Elem, "dmGraphWeight") + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", ir.Int()
	case "weightor":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("weightor needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphWeightOr", declGraphWeightOr)
		return cname(types[0].Elem, "dmGraphWeightOr") + "(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")",
			ir.Int()
	case "degree":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("degree needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphDegree", declGraphDegree)
		return cname(types[0].Elem, "dmGraphDegree") + "(" + args[0] + ", " + args[1] + ")", ir.Int()
	case "weightof":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("weightof needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphWeightOf", declGraphWeightOf)
		return cname(types[0].Elem, "dmGraphWeightOf") + "(" + args[0] + ", " + args[1] + ")", ir.Int()
	case "root":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("root needs a Graph argument, got %s", types[0]))
		}
		g.helper("dmFail", declFail, "fmt", "os")
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphRoot", declGraphRoot, "fmt")
		return cname(types[0].Elem, "dmGraphRoot") + "(" + args[0] + ")", types[0].Elem
	case "indegree":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("indegree needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphInDegree", declGraphInDegree)
		return cname(types[0].Elem, "dmGraphInDegree") + "(" + args[0] + ", " + args[1] + ")", ir.Int()
	case "roots", "leaves":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("%s needs a Graph argument, got %s", name, types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		fn, decl := "dmGraphRoots", declGraphRoots
		if name == "leaves" {
			fn, decl = "dmGraphLeaves", declGraphLeaves
		}
		fn = g.chelper(types[0].Elem, fn, decl)
		return fn + "(" + args[0] + ")", ir.List(types[0].Elem)
	case "reachable":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("reachable needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphReachable", declGraphReachable)
		return cname(types[0].Elem, "dmGraphReachable") + "(" + args[0] + ", " + args[1] + ")", ir.List(types[0].Elem)
	case "delnode":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("delnode needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphSub", declGraphSub) // dmGraphDelNode is written in terms of it
		g.chelper(types[0].Elem, "dmGraphDelNode", declGraphDelNode)
		return cname(types[0].Elem, "dmGraphDelNode") + "(" + args[0] + ", " + args[1] + ")", types[0]
	case "hascycle":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("hascycle needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphHasCycle", declGraphHasCycle)
		return cname(types[0].Elem, "dmGraphHasCycle") + "(" + args[0] + ")", ir.Bool()
	case "weightsum":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("weightsum needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphWeightSum", declGraphWeightSum)
		return cname(types[0].Elem, "dmGraphWeightSum") + "(" + args[0] + ")", ir.Int()
	case "undirected":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("undirected needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphUndirected", declGraphUndirected)
		return cname(types[0].Elem, "dmGraphUndirected") + "(" + args[0] + ")", types[0]
	case "mergegraphs":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("mergegraphs needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphMerge", declGraphMerge)
		return cname(types[0].Elem, "dmGraphMerge") + "(" + args[0] + ", " + args[1] + ")", types[0]
	case "flipedges":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("flipedges needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphFlip", declGraphFlip)
		return cname(types[0].Elem, "dmGraphFlip") + "(" + args[0] + ")", types[0]
	case "subgraph":
		if types[0] == nil || types[0].Kind != ir.KGraph {
			panic(failf("subgraph needs a Graph argument, got %s", types[0]))
		}
		g.chelper(types[0].Elem, "dmGraph", declGraph)
		g.chelper(types[0].Elem, "dmGraphSub", declGraphSub)
		return cname(types[0].Elem, "dmGraphSub") + "(" + args[0] + ", " + args[1] + ")", types[0]

	// -- list generation ---------------------------------------------------------
	case "range":
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmRange", declRange)
		return "dmRange(" + args[0] + ", " + args[1] + ")", ir.List(ir.Int())
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
		elem := g.goType(types[1])
		return "dmFill[" + elem + "](" + args[0] + ", " + args[1] + ")", ir.List(types[1])

	// -- records -----------------------------------------------------------------
	case "record":
		// The Record type was built from the literal field names at resolve
		// time; the struct declaration is interned by shape like every other
		// record, so this is a plain composite literal.
		recT := recordTypeOf(x, types)
		recGo := g.goType(recT)
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
		return b.String(), recT
	case "with":
		if types[0] == nil || types[0].Kind != ir.KRecord {
			panic(failf("with needs a Record argument, got %s", types[0]))
		}
		lit, ok := x.Args[1].(*ast.StringLit)
		if !ok {
			panic(failf("with needs a literal field name"))
		}
		recGo := g.goType(types[0])
		// A Go struct is a value, so the copy is the assignment: no allocation
		// and nothing shared with the original.
		return "func(r " + recGo + ") " + recGo + " { r." + fieldName(lit.Value) + " = " +
			args[2] + "; return r }(" + args[0] + ")", types[0]

	default:
		panic(failf("unknown function %q", name))
	}
}

// mustGo is goType where the type has already been proved compilable by an
// earlier call in the same emitter; it keeps a composite literal readable
// instead of threading four error checks through it.
func mustGo(g *gen, t *ir.Type) string {
	s, err := try1(func() string { return g.goType(t) })
	if err != nil {
		return "any"
	}
	return s
}

// recordTypeOf rebuilds the Record type of a `record(...)` call from its
// literal field names — the same rule typecheck applied, restated here because
// the compiled backend must not depend on the resolver having stashed it.
func recordTypeOf(x *ast.CallExpr, types []*ir.Type) *ir.Type {
	if len(types)%2 != 0 {
		panic(failf("record takes name/value pairs, so an even number of arguments"))
	}
	fields := make([]ir.Field, 0, len(types)/2)
	for i := 0; i < len(types); i += 2 {
		lit, ok := x.Args[i].(*ast.StringLit)
		if !ok {
			panic(failf("record field name %d must be a literal", i/2+1))
		}
		fields = append(fields, ir.Field{Name: lit.Value, Type: types[i+1]})
	}
	return ir.Record(fields...)
}

// irPoint is the expression layer's point type: an (Int, Int) tuple of
// (row, col) — see typecheck.PointType.
func irPoint() *ir.Type { return ir.Tuple(ir.Int(), ir.Int()) }

// pointGo interns and returns the Go struct name for the point tuple type.
func (g *gen) pointGo() string { return g.tupleType(irPoint()) }

// reduceLiteral lowers max/min/sum over a list literal to inline scalar code:
// sum -> (e0 + e1 + ...), max/min -> nested dmMax2/dmMin2. Returns ok=false
// (so the caller uses the general slice path) when an element is non-numeric or
// the elements mix int and float.
func (g *gen) reduceLiteral(name string, elems []ast.Expr, env exprEnv) (string, *ir.Type, bool) {
	parts := make([]string, len(elems))
	var et *ir.Type
	for i, e := range elems {
		v, t := g.compileExpr(e, env)
		if !numericType(t) {
			return "", nil, false
		}
		if et != nil && isFloatType(et) != isFloatType(t) {
			return "", nil, false // mixed int/float: let the general path promote
		}
		parts[i], et = v, t
	}
	if name == "sum" {
		return "(" + strings.Join(parts, " + ") + ")", et, true
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
	return expr, et, true
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
	sv, st, err := try2(func() (string, *ir.Type) { return g.compileExpr(scalarExpr, env) })
	if err != nil || !numericType(st) {
		return "", nil, false
	}
	nv, _, err := try2(func() (string, *ir.Type) { return g.compileExpr(td.Args[1], env) })
	if err != nil {
		return "", nil, false
	}

	// max(range) >= s for LT/GE, max(range) > s for LE/GT; negate for LT/LE.
	useAtLeast := op == token.LT || op == token.GE
	negate := op == token.LT || op == token.LE

	var elem *ir.Type
	var call string
	if seqCall, sn := callName(td.Args[0]); sn == "col" && len(seqCall.Args) == 2 {
		gexpr, gt, gerr := try2(func() (string, *ir.Type) { return g.compileExpr(seqCall.Args[0], env) })
		cexpr, _, cerr := try2(func() (string, *ir.Type) { return g.compileExpr(seqCall.Args[1], env) })
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
		seqv, seqt, serr := try2(func() (string, *ir.Type) { return g.compileExpr(td.Args[0], env) })
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

// tryDivisibility lowers `a % K = 0` (K a nonzero literal, 0 on either side)
// to Go's own remainder. Euclidean and truncated remainders differ only in the
// sign they give a nonzero result; they are zero for exactly the same a, so a
// divisibility test needs neither dmModNZ's fix-up nor its branch — and Go
// compiles `a % K == 0` for a constant K to a multiply and a compare, with no
// divide at all. It is the commonest thing a `%` is written for.
func (g *gen) tryDivisibility(x *ast.BinaryExpr, env exprEnv) (string, bool) {
	if x.Op != token.EQ {
		return "", false
	}
	mod, zero := x.Left, x.Right
	if lit, ok := mod.(*ast.IntLit); ok && lit.Value == 0 {
		mod, zero = zero, mod
	}
	if lit, ok := zero.(*ast.IntLit); !ok || lit.Value != 0 {
		return "", false
	}
	rem, ok := mod.(*ast.BinaryExpr)
	if !ok || rem.Op != token.PERCENT {
		return "", false
	}
	k, ok := rem.Right.(*ast.IntLit)
	if !ok || k.Value == 0 {
		return "", false
	}
	a, at := g.compileExpr(rem.Left, env)
	if at == nil || at.Kind != ir.KInt {
		return "", false
	}
	return fmt.Sprintf("(%s%%%d == 0)", a, k.Value), true
}

func (g *gen) compileBinary(x *ast.BinaryExpr, env exprEnv) (string, *ir.Type) {
	// The fusion below rewrites the comparison into a different shape; an
	// operand that writes to a binding must be compiled as written.
	if !ast.HasUpdate(x) {
		if fused, ft, ok := g.tryMaxCompare(x, env); ok {
			return fused, ft
		}
		if test, ok := g.tryDivisibility(x, env); ok {
			return test, ir.Bool()
		}
	}
	l, lt := g.compileExpr(x.Left, env)
	r, rt := g.compileExpr(x.Right, env)
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
		l = g.ordered(l, lt)
		r = g.ordered(r, rt)
	}
	switch x.Op {
	case token.AND:
		return "(" + l + " && " + r + ")", ir.Bool()
	case token.OR:
		return "(" + l + " || " + r + ")", ir.Bool()
	case token.PLUS, token.MINUS, token.STAR, token.SLASH:
		// Text + Text is concatenation, which Go spells the same way.
		if x.Op == token.PLUS && lt != nil && lt.Kind == ir.KText && rt != nil && rt.Kind == ir.KText {
			return "(" + l + " + " + r + ")", ir.Text()
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
			return "(" + l + " + " + r + ")", res
		case token.MINUS:
			return "(" + l + " - " + r + ")", res
		case token.STAR:
			return "(" + l + " * " + r + ")", res
		}
		// The interpreter reports division by zero as a clean error; a bare
		// Go `/` would panic instead, so route through a guarded helper.
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmDiv", declDiv)
		return "dmDiv(" + l + ", " + r + ")", res
	case token.PERCENT:
		// Euclidean, and guarded — Go's % is truncated and panics on zero.
		return g.modGo(l, r, x.Right), ir.Int()
	case token.EQ:
		if (isFloatType(lt) || isFloatType(rt)) && numericType(lt) && numericType(rt) {
			if !isFloatType(lt) {
				l = "float64(" + l + ")"
			}
			if !isFloatType(rt) {
				r = "float64(" + r + ")"
			}
			return "(" + l + " == " + r + ")", ir.Bool()
		}
		if lt == nil || rt == nil || !lt.Equal(rt) {
			panic(failf("cannot compile = over %s and %s", lt, rt))
		}
		eq := g.eqExpr(l, r, lt)
		return eq, ir.Bool()
	case token.LT, token.GT, token.LE, token.GE:
		op := map[token.Kind]string{token.LT: "<", token.GT: ">", token.LE: "<=", token.GE: ">="}[x.Op]
		// A tuple orders lexicographically, which Go has no operator for, so
		// it goes through an interned three-way compare. Int, Float and Text
		// all order with Go's own operator — byte-wise for strings, which is
		// what ir.Compare's strings.Compare does too.
		if lt != nil && lt.Kind == ir.KTuple {
			c := g.cmpExpr(l, r, lt)
			return "(" + c + " " + op + " 0)", ir.Bool()
		}
		if isFloatType(lt) != isFloatType(rt) {
			if !isFloatType(lt) {
				l = "float64(" + l + ")"
			}
			if !isFloatType(rt) {
				r = "float64(" + r + ")"
			}
		}
		return "(" + l + " " + op + " " + r + ")", ir.Bool()
	default:
		panic(failf("unsupported operator %s", x.Op))
	}
}

// isFloatType reports whether t is exactly Float.
func isFloatType(t *ir.Type) bool { return t != nil && t.Kind == ir.KFloat }

// numericType reports whether t is Int or Float.
func numericType(t *ir.Type) bool {
	return t != nil && (t.Kind == ir.KInt || t.Kind == ir.KFloat)
}

// compileViewCall emits the render builtins. They are gathered here rather
// than spread through compileCall's switch because every one of them needs the
// same runtime declaration and produces the same type, so the shared parts
// would otherwise be repeated ten times.
func (g *gen) compileViewCall(name string, args []string, types []*ir.Type) (string, *ir.Type) {
	g.helper("dmView", declView, "strings", "unicode/utf8")
	view := ir.View()
	switch name {
	case "text":
		// `text` renders its argument the way Reveal would, so it goes
		// through the same formatter the sink uses for that type.
		rendered := g.scalarFmt(args[0], types[0])
		return "dmViewText(" + rendered + ")", view
	case "blank":
		return "dmViewBlank()", view
	case "style":
		return "dmViewStyled(" + args[0] + ", " + args[1] + ")", view
	case "stack":
		return "dmViewStack(" + strings.Join(args, ", ") + ")", view
	case "beside":
		return "dmViewBeside(" + strings.Join(args, ", ") + ")", view
	case "box":
		return "dmViewBox(" + args[0] + ")", view
	case "margin":
		return "dmViewMargin(" + args[0] + ", " + args[1] + ", " + args[2] + ")", view
	case "align":
		return "dmViewAlign(" + args[0] + ", " + args[1] + ", " + args[2] + ", " + args[3] + ")", view
	case "fit":
		return "dmViewFit(" + args[0] + ", " + args[1] + ", " + args[2] + ")", view
	case "draw":
		return g.compileDraw(args[0], types[0])
	}
	panic(failf("%s is not a View builtin", name))
}

// compileDraw emits a whole board as a View. It mirrors eval.drawBoard: each
// cell rendered as it would print, rows written side by side — unless the
// cells are themselves Views (built with `style`, in a `Map Cells`), in which
// case each row is `dmViewBeside` of its cells and the board is `dmViewStack`
// of its rows, preserving colour instead of flattening it away through
// FormatValue's plain rendering.
func (g *gen) compileDraw(arg string, t *ir.Type) (string, *ir.Type) {
	if t == nil {
		panic(failf("draw() takes a Grid, a Sparse or a List of rows"))
	}
	view := ir.View()
	switch t.Kind {
	case ir.KGrid:
		if t.Elem != nil && t.Elem.Kind == ir.KView {
			fn := g.drawHelper("GridStyled", t, func(goT string) string {
				return `func %s(b ` + goT + `) dmView {
	rows := make([]dmView, b.rows)
	for r := 0; r < b.rows; r++ {
		cells := make([]dmView, b.cols)
		for c := 0; c < b.cols; c++ {
			cells[c] = b.cells[r*b.cols+c]
		}
		rows[r] = dmViewBeside(cells...)
	}
	return dmViewStack(rows...)
}`
			})
			return fn + "(" + arg + ")", view
		}
		// dmGrid keeps its cells in one flat slice, row-major, and has no
		// accessor: the emitted code indexes it the way every other grid
		// traversal in this package does.
		cell := g.scalarFmt("b.cells[r*b.cols+c]", t.Elem)
		fn := g.drawHelper("Grid", t, func(goT string) string {
			return `func %s(b ` + goT + `) dmView {
	lines := make([]string, b.rows)
	for r := 0; r < b.rows; r++ {
		var sb strings.Builder
		for c := 0; c < b.cols; c++ {
			sb.WriteString(` + cell + `)
		}
		lines[r] = sb.String()
	}
	return dmViewText(strings.Join(lines, "\n"))
}`
		})
		return fn + "(" + arg + ")", view
	case ir.KSparse:
		if t.Elem != nil && t.Elem.Kind == ir.KView {
			fn := g.drawHelper("SparseStyled", t, func(goT string) string {
				return `func %s(b ` + goT + `) dmView {
	if len(b.cells) == 0 {
		return dmViewBlank()
	}
	var rows []dmView
	for r := b.minR; r <= b.maxR; r++ {
		var cells []dmView
		for c := b.minC; c <= b.maxC; c++ {
			cells = append(cells, b.at(r, c))
		}
		rows = append(rows, dmViewBeside(cells...))
	}
	return dmViewStack(rows...)
}`
			})
			return fn + "(" + arg + ")", view
		}
		// A sparse plane is drawn over its occupied extent, which is what
		// densifying it already means.
		cell := g.scalarFmt("b.at(r, c)", t.Elem)
		fn := g.drawHelper("Sparse", t, func(goT string) string {
			return `func %s(b ` + goT + `) dmView {
	if len(b.cells) == 0 {
		return dmViewBlank()
	}
	var lines []string
	for r := b.minR; r <= b.maxR; r++ {
		var sb strings.Builder
		for c := b.minC; c <= b.maxC; c++ {
			sb.WriteString(` + cell + `)
		}
		lines = append(lines, sb.String())
	}
	return dmViewText(strings.Join(lines, "\n"))
}`
		})
		return fn + "(" + arg + ")", view
	case ir.KList:
		nested := t.Elem != nil && t.Elem.Kind == ir.KList
		if t.Elem != nil && t.Elem.Kind == ir.KView {
			// A flat List<View>: each element is already a whole row, the
			// View twin of a List<Text> of already-rendered lines.
			fn := g.drawHelper("ListStyled", t, func(goT string) string {
				return `func %s(b ` + goT + `) dmView {
	return dmViewStack(b...)
}`
			})
			return fn + "(" + arg + ")", view
		}
		if nested && t.Elem.Elem != nil && t.Elem.Elem.Kind == ir.KView {
			fn := g.drawHelper("NestedListStyled", t, func(goT string) string {
				return `func %s(b ` + goT + `) dmView {
	rows := make([]dmView, len(b))
	for i, row := range b {
		rows[i] = dmViewBeside(row...)
	}
	return dmViewStack(rows...)
}`
			})
			return fn + "(" + arg + ")", view
		}
		// A list of rows, or a list of already-rendered lines.
		var cell string
		var err error
		if nested {
			cell, err = try1(func() string { return g.scalarFmt("x", t.Elem.Elem) })
		} else {
			cell, err = try1(func() string { return g.scalarFmt("row", t.Elem) })
		}
		if err != nil {
			panic(fail(err))
		}
		fn := g.drawHelper("List", t, func(goT string) string {
			if nested {
				return `func %s(b ` + goT + `) dmView {
	lines := make([]string, len(b))
	for i, row := range b {
		var sb strings.Builder
		for _, x := range row {
			sb.WriteString(` + cell + `)
		}
		lines[i] = sb.String()
	}
	return dmViewText(strings.Join(lines, "\n"))
}`
			}
			return `func %s(b ` + goT + `) dmView {
	lines := make([]string, len(b))
	for i, row := range b {
		_ = i
		lines[i] = ` + cell + `
	}
	return dmViewText(strings.Join(lines, "\n"))
}`
		})
		return fn + "(" + arg + ")", view
	}
	panic(failf("draw() takes a Grid, a Sparse or a List of rows, got %s", t))
}

// drawHelper interns one draw function per board type. body is given the Go
// type of the board and returns the function source with a single %s where its
// own name goes, which is how every other interned helper in this package is
// shaped.
func (g *gen) drawHelper(kind string, t *ir.Type, body func(goT string) string) string {
	key := "draw:" + kind + ":" + canonicalKey(t)
	if name, ok := g.listFns[key]; ok {
		return name
	}
	name := fmt.Sprintf("dmDraw%d", len(g.listFns)+1)
	g.listFns[key] = name
	goT, err := try1(func() string { return g.goType(t) })
	if err != nil {
		// goType already succeeded for this type when the argument compiled;
		// a failure here would mean the type changed underfoot.
		goT = "any"
	}
	g.imp("strings")
	g.decls = append(g.decls, fmt.Sprintf(body(goT), name))
	return name
}

// pickHelper interns the one-element draw for a container type. It is a
// helper rather than an inline expression because the argument is evaluated
// once — `pick(expensive())` must not compute its list twice — and because a
// Set has to be read as its elements first.
func (g *gen) pickHelper(container, elem *ir.Type) string {
	key := "pick:" + canonicalKey(container)
	if name, ok := g.listFns[key]; ok {
		return name
	}
	name := fmt.Sprintf("dmPick%d", len(g.listFns)+1)
	g.listFns[key] = name
	goC, _ := try1(func() string { return g.goType(container) })
	goE, _ := try1(func() string { return g.goType(elem) })
	body := "xs"
	if container != nil && container.Kind == ir.KSet {
		body = "xs.items()"
	}
	g.decls = append(g.decls, fmt.Sprintf(`func %s(xs %s) %s {
	items := %s
	if len(items) == 0 {
		dmFail("pick() has nothing to choose from: the list is empty")
	}
	return items[dmRandInt(int64(len(items)))]
}`, name, goC, goE, body))
	return name
}
