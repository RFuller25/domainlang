package builtins

import (
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"domain/ir"
)

// The numeric and bit builtins: integer and float arithmetic helpers, bit
// operations, base conversion, number theory, and the Bool connectives written
// as functions. int1/int2/ints and the float constructors write the ones that
// share a shape; the rest spell out what makes them different.
//
// Left in the layers' own switches, because the compiled form reads the call's
// source rather than just its arguments (mod and divmod pick a cheaper helper
// when the divisor is a literal) or because they belong with another group
// (rotl and rotr turn a point, not a bit pattern).

func init() {
	point := ir.Tuple(ir.Int(), ir.Int())
	intList := ir.List(ir.Int())

	register(
		// -- Int -> … ---------------------------------------------------------
		Spec{Name: "abs", Arity: 1,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if err := needNumeric("abs", a, 0); err != nil {
					return nil, err
				}
				return a[0], nil
			},
			// Branch form (not math.Abs) so abs(-0.0) renders identically in both
			// backends — codegen's dmAbs is the same comparison.
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				switch n := a[0].(type) {
				case int64:
					if n < 0 {
						return -n, nil
					}
					return n, nil
				case float64:
					if n < 0 {
						return -n, nil
					}
					return n, nil
				}
				return nil, fmt.Errorf("abs: expected a number, got %s", ir.DescribeValue(a[0]))
			}},
		int1("sign", ir.Int(), func(n int64) (ir.Value, error) {
			switch {
			case n < 0:
				return int64(-1), nil
			case n > 0:
				return int64(1), nil
			}
			return int64(0), nil
		}),
		int2("gcd", ir.Int(), func(a, b int64) (ir.Value, error) { return gcdInt(a, b), nil }),
		int2("lcm", ir.Int(), func(a, b int64) (ir.Value, error) { return lcmInt(a, b), nil }),
		int1("isqrt", ir.Int(), func(n int64) (ir.Value, error) { return isqrtInt(n) }),
		int1("factorial", ir.Int(), func(n int64) (ir.Value, error) { return factorialInt(n) }),
		int2("choose", ir.Int(), func(n, k int64) (ir.Value, error) { return chooseInt(n, k) }),
		ints("modpow", 3, ir.Int(), func(_ Env, x []int64) (ir.Value, error) { return modPow(x[0], x[1], x[2]) }),
		int2("modinv", ir.Int(), func(a, m int64) (ir.Value, error) { return modInverse(a, m) }),
		ints("solve2x2", 6, point, func(_ Env, x []int64) (ir.Value, error) {
			p, q, err := solve2x2(x[0], x[1], x[2], x[3], x[4], x[5])
			if err != nil {
				return nil, err
			}
			return []ir.Value{p, q}, nil
		}),

		// pow follows the operators' promotion rule rather than staying integral:
		// `pow(2, 10)` is an Int, `pow(x, 0.5)` is the square root it looks like.
		Spec{Name: "pow", Arity: 2,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				for i := range 2 {
					if err := needNumeric("pow", a, i); err != nil {
						return nil, err
					}
				}
				if a[0].Kind == ir.KFloat || a[1].Kind == ir.KFloat {
					return ir.Float(), nil
				}
				return ir.Int(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				if isFloat(a[0]) || isFloat(a[1]) {
					x, y, err := twoFloats("pow", a)
					if err != nil {
						return nil, err
					}
					return math.Pow(x, y), nil
				}
				x, y, err := twoInts("pow", a)
				if err != nil {
					return nil, err
				}
				return powInt(x, y)
			}},

		// -- bit operations -----------------------------------------------------
		int2("band", ir.Int(), func(a, b int64) (ir.Value, error) { return a & b, nil }),
		int2("bor", ir.Int(), func(a, b int64) (ir.Value, error) { return a | b, nil }),
		int2("bxor", ir.Int(), func(a, b int64) (ir.Value, error) { return a ^ b, nil }),
		int1("bnot", ir.Int(), func(n int64) (ir.Value, error) { return ^n, nil }),
		int1("popcount", ir.Int(), func(n int64) (ir.Value, error) { return int64(bits.OnesCount64(uint64(n))), nil }),
		shift("shl", func(a int64, n int64) int64 { return a << n }),
		shift("shr", func(a int64, n int64) int64 { return a >> n }),
		int2("testbit", ir.Bool(), func(n, i int64) (ir.Value, error) {
			if i < 0 || i > 63 {
				return nil, fmt.Errorf("testbit: bit %d is outside an Int (0 to 63)", i)
			}
			return n&(1<<uint(i)) != 0, nil
		}),
		// The bitwise counterparts of sum/product. Each starts from its operator's
		// identity, so the empty list leaves any later fold unchanged — for `and`
		// that is all bits set, which is -1 in two's complement.
		bitReduce("bandall", -1, func(acc, n int64) int64 { return acc & n }),
		bitReduce("borall", 0, func(acc, n int64) int64 { return acc | n }),
		bitReduce("bxorall", 0, func(acc, n int64) int64 { return acc ^ n }),

		// -- bases and digits ---------------------------------------------------
		fromBase("frombase", 2),
		fromBase("fromhex", 1),
		int2("tobase", ir.Text(), func(n, base int64) (ir.Value, error) {
			if base < 2 || base > 36 {
				return nil, fmt.Errorf("tobase: base must be between 2 and 36, got %d", base)
			}
			return strconv.FormatInt(n, int(base)), nil
		}),
		int1("tohex", ir.Text(), func(n int64) (ir.Value, error) { return strconv.FormatInt(n, 16), nil }),
		int1("tobin", ir.Text(), func(n int64) (ir.Value, error) { return strconv.FormatInt(n, 2), nil }),
		int1("digits", intList, func(n int64) (ir.Value, error) { return decimalDigits(n), nil }),
		Spec{Name: "fromdigits", Arity: 1,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if !a[0].Equal(intList) {
					return nil, fmt.Errorf("fromdigits needs List<Int>, got %s", a[0])
				}
				return ir.Int(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				ds, err := ir.AsIntSlice(a[0])
				if err != nil {
					return nil, fmt.Errorf("fromdigits: %v", err)
				}
				return fromDigits(ds)
			}},

		// -- number theory ------------------------------------------------------
		int1("isprime", ir.Bool(), func(n int64) (ir.Value, error) { return isPrime(n), nil }),
		ints("divisors", 1, intList, func(env Env, x []int64) (ir.Value, error) { return divisorsOf(env.BuildLimit(), x[0]) }),
		Spec{Name: "crt", Arity: 2,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				for i := range 2 {
					if !a[i].Equal(intList) {
						return nil, fmt.Errorf("crt needs List<Int> arguments, got %s", a[i])
					}
				}
				return ir.Int(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				rs, err1 := ir.AsIntSlice(a[0])
				ms, err2 := ir.AsIntSlice(a[1])
				if err := firstErr(err1, err2); err != nil {
					return nil, fmt.Errorf("crt: %v", err)
				}
				return crtSolve(rs, ms)
			}},

		// -- the connectives as functions ---------------------------------------
		logic("and", 2, func(b []bool) bool { return b[0] && b[1] }),
		logic("or", 2, func(b []bool) bool { return b[0] || b[1] }),
		logic("xor", 2, func(b []bool) bool { return b[0] != b[1] }),
		logic("not", 1, func(b []bool) bool { return !b[0] }),

		// -- floats -------------------------------------------------------------
		rounding("floor", math.Floor),
		rounding("ceil", math.Ceil),
		rounding("round", math.Round),
		Spec{Name: "trunc", Arity: 1,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if err := needNumeric("trunc", a, 0); err != nil {
					return nil, err
				}
				return ir.Int(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				if n, ok := a[0].(int64); ok {
					return n, nil
				}
				f, err := ir.AsFloat(a[0])
				if err != nil {
					return nil, fmt.Errorf("trunc: %v", err)
				}
				return int64(math.Trunc(f)), nil
			}},
		Spec{Name: "tofloat", Arity: 1,
			Type: func(a []*ir.Type) (*ir.Type, error) {
				if !isNumeric(a[0]) && !a[0].Equal(ir.Text()) {
					return nil, fmt.Errorf("tofloat needs Int, Float, or Text, got %s", a[0])
				}
				return ir.Float(), nil
			},
			Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
				switch x := a[0].(type) {
				case float64:
					return x, nil
				case int64:
					return float64(x), nil
				case string:
					f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
					if err != nil {
						return nil, fmt.Errorf("tofloat: %q is not a number", x)
					}
					return f, nil
				}
				return nil, fmt.Errorf("tofloat: expected Int, Float, or Text, got %s", ir.DescribeValue(a[0]))
			}},
		float1ary("sqrt", func(f float64) (ir.Value, error) {
			if f < 0 {
				return nil, fmt.Errorf("sqrt of a negative number (%s)", ir.FormatFloat(f))
			}
			return math.Sqrt(f), nil
		}),
		float1ary("log", func(f float64) (ir.Value, error) { return float1("log", f) }),
		float1ary("log2", func(f float64) (ir.Value, error) { return float1("log2", f) }),
		float1ary("log10", func(f float64) (ir.Value, error) { return float1("log10", f) }),
		float1ary("exp", func(f float64) (ir.Value, error) { return float1("exp", f) }),
		float1ary("sin", func(f float64) (ir.Value, error) { return float1("sin", f) }),
		float1ary("cos", func(f float64) (ir.Value, error) { return float1("cos", f) }),
		float1ary("tan", func(f float64) (ir.Value, error) { return float1("tan", f) }),
		float2ary("atan2", math.Atan2),
		float2ary("hypot", math.Hypot),
	)
}

// int1, int2 and ints declare a builtin of one, two or n Int arguments. The
// arguments reach f unpacked, and the first that is not an Int is the error,
// worded "name: expected an integer, got …".
func int1(name string, result *ir.Type, f func(a int64) (ir.Value, error)) Spec {
	return Spec{Name: name, Arity: 1, Type: allInt(name, 1, result),
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			x, err := ir.AsInt(a[0])
			if err != nil {
				return nil, fmt.Errorf("%s: %v", name, err)
			}
			return f(x)
		}}
}

func int2(name string, result *ir.Type, f func(a, b int64) (ir.Value, error)) Spec {
	return Spec{Name: name, Arity: 2, Type: allInt(name, 2, result),
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			x, y, err := twoInts(name, a)
			if err != nil {
				return nil, err
			}
			return f(x, y)
		}}
}

func ints(name string, n int, result *ir.Type, f func(env Env, x []int64) (ir.Value, error)) Spec {
	return Spec{Name: name, Arity: n, Type: allInt(name, n, result),
		Eval: func(env Env, a []ir.Value) (ir.Value, error) {
			x := make([]int64, n)
			for i := range x {
				v, err := ir.AsInt(a[i])
				if err != nil {
					return nil, fmt.Errorf("%s: %v", name, err)
				}
				x[i] = v
			}
			return f(env, x)
		}}
}

// shift declares shl/shr, which refuse a negative count.
func shift(name string, f func(a, n int64) int64) Spec {
	return int2(name, ir.Int(), func(a, n int64) (ir.Value, error) {
		if n < 0 {
			return nil, fmt.Errorf("%s: negative shift count %d", name, n)
		}
		return f(a, n), nil
	})
}

// bitReduce declares bandall/borall/bxorall over a List<Int>.
func bitReduce(name string, identity int64, op func(acc, n int64) int64) Spec {
	return Spec{Name: name, Arity: 1,
		Type: func(a []*ir.Type) (*ir.Type, error) {
			if a[0] == nil || a[0].Kind != ir.KList {
				return nil, fmt.Errorf("%s needs a List argument, got %s", name, a[0])
			}
			if elem := a[0].Elem; elem.Kind != ir.KInt {
				return nil, fmt.Errorf("%s needs Int elements, got %s", name, elem)
			}
			return ir.Int(), nil
		},
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(a[0])
			if err != nil {
				return nil, fmt.Errorf("%s: %v", name, err)
			}
			acc := identity
			for i, e := range xs {
				n, err := ir.AsInt(e)
				if err != nil {
					return nil, fmt.Errorf("%s: list element %d: %v", name, i, err)
				}
				acc = op(acc, n)
			}
			return acc, nil
		}}
}

// fromBase declares frombase (Text and a base) and fromhex (Text, base 16).
func fromBase(name string, arity int) Spec {
	return Spec{Name: name, Arity: arity,
		Type: func(a []*ir.Type) (*ir.Type, error) {
			if err := needText(name, a, 0); err != nil {
				return nil, err
			}
			if arity == 2 {
				if err := needInt(name, a, 1); err != nil {
					return nil, err
				}
			}
			return ir.Int(), nil
		},
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			s, ok := a[0].(string)
			if !ok {
				return nil, fmt.Errorf("%s: expected Text, got %s", name, ir.DescribeValue(a[0]))
			}
			base := int64(16)
			if arity == 2 {
				var err error
				if base, err = ir.AsInt(a[1]); err != nil {
					return nil, fmt.Errorf("frombase: %v", err)
				}
			}
			return parseBase(name, s, base)
		}}
}

// logic declares a connective over n Bools. Every argument is checked before f
// runs, because the interpreter evaluates every argument of a function call.
func logic(name string, n int, f func(b []bool) bool) Spec {
	return Spec{Name: name, Arity: n,
		Type: func(a []*ir.Type) (*ir.Type, error) {
			for i := range a {
				if !a[i].Equal(ir.Bool()) {
					if n == 1 {
						return nil, fmt.Errorf("%s needs a Bool argument, got %s", name, a[i])
					}
					return nil, fmt.Errorf("%s needs Bool arguments, got %s", name, a[i])
				}
			}
			return ir.Bool(), nil
		},
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			bs := make([]bool, len(a))
			for i, v := range a {
				b, ok := v.(bool)
				if !ok {
					return nil, fmt.Errorf("%s: expected Bool, got %s", name, ir.DescribeValue(v))
				}
				bs[i] = b
			}
			return f(bs), nil
		}}
}

// rounding declares floor/ceil/round: a Float in, the Int it rounds to out.
func rounding(name string, f func(float64) float64) Spec {
	return Spec{Name: name, Arity: 1,
		Type: func(a []*ir.Type) (*ir.Type, error) {
			if !a[0].Equal(ir.Float()) {
				return nil, fmt.Errorf("%s needs Float, got %s", name, a[0])
			}
			return ir.Int(), nil
		},
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			x, ok := a[0].(float64)
			if !ok {
				return nil, fmt.Errorf("%s: expected Float, got %s", name, ir.DescribeValue(a[0]))
			}
			return int64(f(x)), nil
		}}
}

// float1ary and float2ary declare a Float-valued builtin of one or two numbers
// (an Int is widened).
func float1ary(name string, f func(float64) (ir.Value, error)) Spec {
	return Spec{Name: name, Arity: 1, Type: allNumeric(name, 1),
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			x, err := ir.AsFloat(a[0])
			if err != nil {
				return nil, fmt.Errorf("%s: %v", name, err)
			}
			return f(x)
		}}
}

func float2ary(name string, f func(a, b float64) float64) Spec {
	return Spec{Name: name, Arity: 2, Type: allNumeric(name, 2),
		Eval: func(_ Env, a []ir.Value) (ir.Value, error) {
			x, y, err := twoFloats(name, a)
			if err != nil {
				return nil, err
			}
			return f(x, y), nil
		}}
}

func allInt(name string, n int, result *ir.Type) func([]*ir.Type) (*ir.Type, error) {
	return func(a []*ir.Type) (*ir.Type, error) {
		for i := range n {
			if err := needInt(name, a, i); err != nil {
				return nil, err
			}
		}
		return result, nil
	}
}

// allNumeric types a Float-valued builtin whose n arguments are Int or Float.
func allNumeric(name string, n int) func([]*ir.Type) (*ir.Type, error) {
	return func(a []*ir.Type) (*ir.Type, error) {
		for i := range n {
			if err := needNumeric(name, a, i); err != nil {
				return nil, err
			}
		}
		return ir.Float(), nil
	}
}

func isNumeric(t *ir.Type) bool { return t != nil && (t.Kind == ir.KInt || t.Kind == ir.KFloat) }

func needNumeric(name string, a []*ir.Type, i int) error {
	if !isNumeric(a[i]) {
		return fmt.Errorf("%s needs Int or Float, got %s", name, a[i])
	}
	return nil
}

func isFloat(v ir.Value) bool {
	_, ok := v.(float64)
	return ok
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// twoInts and twoFloats unpack two arguments, wording a failure "name: …".
func twoInts(name string, a []ir.Value) (int64, int64, error) {
	x, err1 := ir.AsInt(a[0])
	if err1 != nil {
		return 0, 0, fmt.Errorf("%s: %v", name, err1)
	}
	y, err2 := ir.AsInt(a[1])
	if err2 != nil {
		return 0, 0, fmt.Errorf("%s: %v", name, err2)
	}
	return x, y, nil
}

func twoFloats(name string, a []ir.Value) (float64, float64, error) {
	x, err1 := ir.AsFloat(a[0])
	y, err2 := ir.AsFloat(a[1])
	if err := firstErr(err1, err2); err != nil {
		return 0, 0, fmt.Errorf("%s: %v", name, err)
	}
	return x, y, nil
}
