package codegen

import (
	"fmt"
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
}

type exprEnv map[string]exprBinding

// compileExpr returns a parenthesized Go expression and its Domain type.
func (g *gen) compileExpr(e ast.Expr, env exprEnv) (string, *ir.Type, error) {
	switch x := e.(type) {
	case *ast.IntLit:
		return strconv.FormatInt(x.Value, 10), ir.Int(), nil
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
		return g.emitBlockCall(x, b.expr, b.typ)
	case *ast.Ident:
		b, ok := env[x.Name]
		if !ok {
			// Fall back to the enclosing For loops' variables — the lambda's
			// trailing ambient parameters. The caller's env wins, so a leading
			// parameter of the same name still shadows.
			if b, ok = g.ambientNames[x.Name]; !ok {
				return "", nil, fmt.Errorf("unknown identifier %q", x.Name)
			}
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
	default:
		return "", nil, fmt.Errorf("unsupported expression %T", e)
	}
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
	val, valT, err := g.compileExpr(x.Value, env)
	if err != nil {
		return "", nil, err
	}
	goValT, err := g.goType(valT)
	if err != nil {
		return "", nil, err
	}
	local := "dmLet" + fieldName(x.Name)
	// Shadow in a copy so sibling expressions keep the outer binding.
	inner := make(exprEnv, len(env)+1)
	for k, v := range env {
		inner[k] = v
	}
	inner[x.Name] = exprBinding{expr: local, typ: valT}
	body, bodyT, err := g.compileExpr(x.Body, inner)
	if err != nil {
		return "", nil, err
	}
	goBodyT, err := g.goType(bodyT)
	if err != nil {
		return "", nil, err
	}
	return "func() " + goBodyT + " {\n\t\tvar " + local + " " + goValT + " = " + val +
		"\n\t\t_ = " + local + "\n\t\treturn " + body + "\n\t}()", bodyT, nil
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
		if types[0] != nil && types[0].Kind == ir.KSet {
			g.helper("dmSet", declSet)
			g.helper("dmSetHas", declSetHas)
			return "dmSetHas(" + args[0] + ", " + args[1] + ")", ir.Bool(), nil
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
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmMod", declMod)
		return "dmMod(" + args[0] + ", " + args[1] + ")", ir.Int(), nil
	case "divmod":
		// Built inline rather than via a helper: the result is a generated
		// tuple struct whose name is only known here.
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmMod", declMod)
		g.helper("dmDiv", declDiv)
		pt, err := g.pointGo()
		if err != nil {
			return "", nil, err
		}
		a, b := args[0], args[1]
		return pt + "{dmDiv(" + a + " - dmMod(" + a + ", " + b + "), " + b + "), dmMod(" + a + ", " + b + ")}",
			irPoint(), nil
	case "pow":
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
		g.imp("strings")
		return "strings.Join(" + args[0] + ", " + args[1] + ")", ir.Text(), nil

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
	default:
		return "", nil, fmt.Errorf("unknown function %q", name)
	}
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

func (g *gen) compileBinary(x *ast.BinaryExpr, env exprEnv) (string, *ir.Type, error) {
	if fused, ft, ok := g.tryMaxCompare(x, env); ok {
		return fused, ft, nil
	}
	l, lt, err := g.compileExpr(x.Left, env)
	if err != nil {
		return "", nil, err
	}
	r, rt, err := g.compileExpr(x.Right, env)
	if err != nil {
		return "", nil, err
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
		g.helper("dmFail", declFail, "fmt", "os")
		g.helper("dmMod", declMod)
		return "dmMod(" + l + ", " + r + ")", ir.Int(), nil
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
		if !scalarKind(lt.Kind) {
			// Structural type equality is field-order-insensitive for
			// records, but the generated structs are not — require the two
			// sides to share one Go representation.
			lg, err := g.goType(lt)
			if err != nil {
				return "", nil, err
			}
			rg, err := g.goType(rt)
			if err != nil {
				return "", nil, err
			}
			if lg != rg {
				return "", nil, fmt.Errorf("cannot compile = over %s and %s (same fields, different declaration order)", lt, rt)
			}
		}
		eq, err := g.eqExpr(l, r, lt)
		if err != nil {
			return "", nil, err
		}
		return eq, ir.Bool(), nil
	case token.LT, token.GT, token.LE, token.GE:
		if isFloatType(lt) != isFloatType(rt) {
			if !isFloatType(lt) {
				l = "float64(" + l + ")"
			}
			if !isFloatType(rt) {
				r = "float64(" + r + ")"
			}
		}
		op := map[token.Kind]string{token.LT: "<", token.GT: ">", token.LE: "<=", token.GE: ">="}[x.Op]
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

// numericType reports whether t is Int or Float.
func numericType(t *ir.Type) bool {
	return t != nil && (t.Kind == ir.KInt || t.Kind == ir.KFloat)
}
