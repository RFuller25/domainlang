package codegen

import (
	"fmt"
	"strconv"

	"domain/builtins"
	"domain/ir"
)

// The compiled half of the numeric and bit builtins declared in
// builtins/numeric.go; see textbuiltins.go for the arrangement. Most are a call
// to a generated dm* helper, so they are written with viaHelper.

func (g *gen) PointGo() (string, error) { return try1(func() string { return g.pointGo() }) }

// asFloat spells an Int or Float argument as a float64 expression.
func asFloat(arg string, t *ir.Type) string {
	if isFloatType(t) {
		return arg
	}
	return "float64(" + arg + ")"
}

// infix emits a Go binary operator over two Int arguments.
func infix(op string) builtins.EmitFunc {
	return func(_ builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
		return "(" + args[0] + " " + op + " " + args[1] + ")", ir.Int(), nil
	}
}

var (
	absDecl      = helperDecl{"dmAbs", declAbs, nil}
	gcdDecl      = helperDecl{"dmGcd", declGcd, nil}
	mulModDecl   = helperDecl{"dmMulMod", declMulMod, []string{"math/bits"}}
	modArithDecl = helperDecl{"dmModArith", declModArith, []string{"math/bits"}}
	fmtFloatDecl = helperDecl{"dmFmtFloat", declFmtFloat, []string{"strconv"}}
)

func init() {
	integer, text, boolean, intList := ir.Int(), ir.Text(), ir.Bool(), ir.List(ir.Int())

	for name, f := range map[string]builtins.EmitFunc{
		"sign":      viaHelper("dmSign", integer, helperDecl{"dmSign", declSign, nil}),
		"gcd":       viaHelper("dmGcd", integer, absDecl, gcdDecl),
		"lcm":       viaHelper("dmLcm", integer, absDecl, gcdDecl, helperDecl{"dmLcm", declLcm, nil}),
		"modpow":    viaHelper("dmModPow", integer, failDecl, mulModDecl, helperDecl{"dmModPow", declModPow, nil}),
		"modinv":    viaHelper("dmModInv", integer, failDecl, mulModDecl, helperDecl{"dmModInv", declModInv, nil}),
		"isqrt":     viaHelper("dmISqrt", integer, failDecl, helperDecl{"dmISqrt", declISqrt, []string{"math"}}),
		"factorial": viaHelper("dmFactorial", integer, failDecl, helperDecl{"dmFactorial", declFactorial, nil}),
		"choose":    viaHelper("dmChoose", integer, failDecl, helperDecl{"dmChoose", declChoose, []string{"math", "math/bits"}}),

		"band": infix("&"),
		"bor":  infix("|"),
		"bxor": infix("^"),
		"shl":  viaHelper("dmShl", integer, failDecl, helperDecl{"dmShl", declShl, nil}),
		"shr":  viaHelper("dmShr", integer, failDecl, helperDecl{"dmShr", declShr, nil}),
		"bnot": func(_ builtins.Emitter, a []string, _ []*ir.Type) (string, *ir.Type, error) {
			return "(^" + a[0] + ")", integer, nil
		},
		"xor": func(_ builtins.Emitter, a []string, _ []*ir.Type) (string, *ir.Type, error) {
			return "(" + a[0] + " != " + a[1] + ")", boolean, nil
		},
		"not": func(_ builtins.Emitter, a []string, _ []*ir.Type) (string, *ir.Type, error) {
			return "(!" + a[0] + ")", boolean, nil
		},
		"tobase": viaHelper("dmToBase", text, failDecl, helperDecl{"dmToBase", declToBase, []string{"strconv"}}),

		"testbit":    viaHelper("dmTestBit", boolean, failDecl, helperDecl{"dmTestBit", declTestBit, nil}),
		"digits":     viaHelper("dmDigits", intList, helperDecl{"dmDigits", declDigits, nil}),
		"fromdigits": viaHelper("dmFromDigits", integer, failDecl, helperDecl{"dmFromDigits", declFromDigits, []string{"math"}}),
		"isprime":    viaHelper("dmIsPrime", boolean, modArithDecl, helperDecl{"dmIsPrime", declIsPrime, []string{"math/bits"}}),
		"divisors":   viaHelper("dmDivisors", intList, failDecl, helperDecl{"dmDivisors", declDivisors, nil}),
		"crt":        viaHelper("dmCRT", integer, failDecl, modArithDecl, helperDecl{"dmCRT", declCRT, nil}),

		"floor": direct("math", "Floor", integer, func(s string) string { return "int64(" + s + ")" }),
		"ceil":  direct("math", "Ceil", integer, func(s string) string { return "int64(" + s + ")" }),
		"round": direct("math", "Round", integer, func(s string) string { return "int64(" + s + ")" }),
	} {
		builtins.SetEmit(name, f)
	}

	// abs keeps its argument's type.
	builtins.SetEmit("abs", func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
		e.Helper(absDecl.name, absDecl.src)
		return "dmAbs(" + args[0] + ")", types[0], nil
	})

	builtins.SetEmit("tofloat", func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
		if types[0] != nil && types[0].Kind == ir.KText {
			e.Helper("dmFail", declFail, "fmt", "os")
			e.Helper("dmParseFloat", declParseFloat, "strconv", "strings")
			return "dmParseFloat(" + args[0] + ")", ir.Float(), nil
		}
		return "float64(" + args[0] + ")", ir.Float(), nil
	})

	builtins.SetEmit("sqrt", func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
		e.Imp("math")
		for _, d := range []helperDecl{failDecl, fmtFloatDecl, {"dmSqrt", declSqrt, []string{"math"}}} {
			e.Helper(d.name, d.src, d.imports...)
		}
		return "dmSqrt(" + asFloat(args[0], types[0]) + ")", ir.Float(), nil
	})

	// pow is integral unless an operand is a Float, as the operators are.
	builtins.SetEmit("pow", func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
		if isFloatType(types[0]) || isFloatType(types[1]) {
			e.Imp("math")
			return "math.Pow(" + asFloat(args[0], types[0]) + ", " + asFloat(args[1], types[1]) + ")", ir.Float(), nil
		}
		return viaHelper("dmPow", integer, failDecl, helperDecl{"dmPow", declPow, nil})(e, args, types)
	})

	for _, name := range []string{"bandall", "borall", "bxorall"} {
		fn := bitReduceName(name)
		builtins.SetEmit(name, func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
			if types[0] == nil || types[0].Kind != ir.KList {
				return "", nil, fmt.Errorf("%s needs a List argument, got %s", name, types[0])
			}
			e.Helper(fn, declBitReduce(name))
			return fn + "(" + args[0] + ")", integer, nil
		})
	}

	// A *function*, so both arguments are evaluated before it runs — the
	// interpreter evaluates every argument before dispatching, and a compiled
	// `&&` would skip a failure the interpreter raises. The infix operators keep
	// `&&`/`||` and keep short-circuiting; docs/expressions.md states the
	// difference.
	builtins.SetEmit("and", viaHelper("dmAnd", boolean, helperDecl{"dmAnd", declAnd, nil}))
	builtins.SetEmit("or", viaHelper("dmOr", boolean, helperDecl{"dmOr", declOr, nil}))

	// frombase takes its base from the second argument, fromhex fixes it at 16.
	for _, name := range []string{"frombase", "fromhex"} {
		builtins.SetEmit(name, func(e builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
			e.Helper("dmFail", declFail, "fmt", "os")
			e.Helper("dmParseBase", declParseBase, "strconv", "strings")
			base := "16"
			if name == "frombase" {
				base = args[1]
			}
			return "dmParseBase(" + strconv.Quote(name) + ", " + args[0] + ", " + base + ")", integer, nil
		})
	}
	for name, base := range map[string]string{"tohex": "16", "tobin": "2"} {
		builtins.SetEmit(name, func(e builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
			e.Imp("strconv")
			return "strconv.FormatInt(" + args[0] + ", " + base + ")", text, nil
		})
	}

	builtins.SetEmit("popcount", func(e builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
		e.Imp("math/bits")
		return "int64(bits.OnesCount64(uint64(" + args[0] + ")))", integer, nil
	})

	builtins.SetEmit("trunc", func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
		if !isFloatType(types[0]) {
			return args[0], integer, nil
		}
		e.Imp("math")
		return "int64(math.Trunc(" + args[0] + "))", integer, nil
	})

	for _, name := range []string{"log", "log2", "log10", "exp", "sin", "cos", "tan"} {
		builtins.SetEmit(name, func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
			e.Helper("dmFail", declFail, "fmt", "os")
			e.Helper("dmFloat1", declFloat1, "math", "strconv")
			return "dmFloat1(" + strconv.Quote(name) + ", " + asFloat(args[0], types[0]) + ")", ir.Float(), nil
		})
	}
	for name, fn := range map[string]string{"atan2": "math.Atan2", "hypot": "math.Hypot"} {
		builtins.SetEmit(name, func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
			e.Imp("math")
			return fn + "(" + asFloat(args[0], types[0]) + ", " + asFloat(args[1], types[1]) + ")", ir.Float(), nil
		})
	}

	// The solution is a point, whose Go struct is interned by the generator.
	builtins.SetEmit("solve2x2", func(e builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
		pt, err := e.PointGo()
		if err != nil {
			return "", nil, err
		}
		e.Helper("dmFail", declFail, "fmt", "os")
		e.Helper("dmSolve2x2", fmt.Sprintf(`func dmSolve2x2(a, b, c, d, e, f int64) %[1]s {
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
	})
}
