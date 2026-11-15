package builtins

import (
	"fmt"
	"strings"
	"testing"

	"domain/ir"
)

func ints64(xs ...int64) []ir.Value { return ir.IntsToValue(xs) }

// Every numeric and bit builtin through its evaluator: one success and, where
// there is one, the failure whose wording the tests and the oracle pin. want is
// the value as %v prints it; a want starting "!" is a substring of the error.
func TestNumericEval(t *testing.T) {
	I := func(n int64) ir.Value { return n }
	for _, c := range []struct {
		name string
		args []ir.Value
		want string
	}{
		{"abs", []ir.Value{I(-7)}, "7"},
		{"abs", []ir.Value{-2.5}, "2.5"},
		{"abs", []ir.Value{"x"}, "!abs: expected a number, got Text"},
		{"sign", []ir.Value{I(-9)}, "-1"},
		{"sign", []ir.Value{2.5}, "!sign: expected an integer"},
		{"gcd", []ir.Value{I(-12), I(18)}, "6"},
		{"lcm", []ir.Value{I(4), I(6)}, "12"},
		{"isqrt", []ir.Value{I(99)}, "9"},
		{"isqrt", []ir.Value{I(-1)}, "!isqrt: negative input -1"},
		{"factorial", []ir.Value{I(10)}, "3628800"},
		{"factorial", []ir.Value{I(21)}, "!overflows Int"},
		{"choose", []ir.Value{I(10), I(3)}, "120"},
		{"modpow", []ir.Value{I(2), I(10), I(1000)}, "24"},
		{"modpow", []ir.Value{I(2), I(1), I(0)}, "!modulus must be positive"},
		{"modinv", []ir.Value{I(3), I(11)}, "4"},
		{"modinv", []ir.Value{I(2), I(4)}, "!not coprime"},
		{"pow", []ir.Value{I(2), I(10)}, "1024"},
		{"pow", []ir.Value{I(2), 0.5}, "1.4142135623730951"},
		{"pow", []ir.Value{I(2), I(-1)}, "!exponent must be non-negative"},
		{"band", []ir.Value{I(12), I(10)}, "8"},
		{"bor", []ir.Value{I(12), I(10)}, "14"},
		{"bxor", []ir.Value{I(12), I(10)}, "6"},
		{"bnot", []ir.Value{I(5)}, "-6"},
		{"shl", []ir.Value{I(1), I(10)}, "1024"},
		{"shr", []ir.Value{I(1024), I(3)}, "128"},
		{"shl", []ir.Value{I(1), I(-1)}, "!shl: negative shift count -1"},
		{"shr", []ir.Value{I(1), I(-2)}, "!shr: negative shift count -2"},
		{"popcount", []ir.Value{I(255)}, "8"},
		{"testbit", []ir.Value{I(5), I(2)}, "true"},
		{"testbit", []ir.Value{I(5), I(64)}, "!bit 64 is outside an Int"},
		{"bandall", []ir.Value{ints64(12, 10)}, "8"},
		{"bandall", []ir.Value{ints64()}, "-1"},
		{"borall", []ir.Value{ints64(1, 2, 4)}, "7"},
		{"bxorall", []ir.Value{ints64(5, 3)}, "6"},
		{"bxorall", []ir.Value{[]ir.Value{I(1), "a"}}, "!bxorall: list element 1:"},
		{"frombase", []ir.Value{"ff", I(16)}, "255"},
		{"frombase", []ir.Value{"1", I(1)}, "!base must be between 2 and 36"},
		{"fromhex", []ir.Value{"0xFF"}, "255"},
		{"fromhex", []ir.Value{"xyz"}, `!fromhex: "xyz" is not a base-16 number`},
		{"tobase", []ir.Value{I(255), I(2)}, "11111111"},
		{"tobase", []ir.Value{I(5), I(37)}, "!tobase: base must be between 2 and 36, got 37"},
		{"tohex", []ir.Value{I(255)}, "ff"},
		{"tobin", []ir.Value{I(10)}, "1010"},
		{"digits", []ir.Value{I(-907)}, "[9 0 7]"},
		{"fromdigits", []ir.Value{ints64(1, 2, 3)}, "123"},
		{"fromdigits", []ir.Value{ints64(1, 10)}, "!not a decimal digit"},
		{"isprime", []ir.Value{I(97)}, "true"},
		{"isprime", []ir.Value{I(91)}, "false"},
		{"divisors", []ir.Value{I(36)}, "[1 2 3 4 6 9 12 18 36]"},
		{"divisors", []ir.Value{I(0)}, "!needs a positive number"},
		{"crt", []ir.Value{ints64(2, 3, 2), ints64(3, 5, 7)}, "23"},
		{"crt", []ir.Value{ints64(1, 2), ints64(2, 4)}, "!disagree"},
		{"solve2x2", []ir.Value{I(94), I(22), I(8400), I(34), I(67), I(5400)}, "[80 40]"},
		{"solve2x2", []ir.Value{I(1), I(2), I(3), I(2), I(4), I(6)}, "!determinant is zero"},
		{"and", []ir.Value{true, false}, "false"},
		{"or", []ir.Value{true, false}, "true"},
		{"xor", []ir.Value{true, true}, "false"},
		{"not", []ir.Value{false}, "true"},
		{"and", []ir.Value{true, I(1)}, "!and: expected Bool, got Int"},
		{"floor", []ir.Value{-2.5}, "-3"},
		{"ceil", []ir.Value{2.1}, "3"},
		{"round", []ir.Value{-2.5}, "-3"},
		{"floor", []ir.Value{I(2)}, "!floor: expected Float, got Int"},
		{"trunc", []ir.Value{-2.9}, "-2"},
		{"trunc", []ir.Value{I(5)}, "5"},
		{"sqrt", []ir.Value{I(16)}, "4"},
		{"sqrt", []ir.Value{-4.0}, "!sqrt of a negative number (-4)"},
		{"tofloat", []ir.Value{I(3)}, "3"},
		{"tofloat", []ir.Value{" 3.5 "}, "3.5"},
		{"tofloat", []ir.Value{"abc"}, `!tofloat: "abc" is not a number`},
		{"log", []ir.Value{I(1)}, "0"},
		{"log", []ir.Value{I(0)}, "!log of a non-positive number (0)"},
		{"log2", []ir.Value{I(8)}, "3"},
		{"log10", []ir.Value{I(1000)}, "3"},
		{"exp", []ir.Value{I(0)}, "1"},
		{"exp", []ir.Value{I(1000)}, "!exp(1000) has no finite value"},
		{"sin", []ir.Value{I(0)}, "0"},
		{"cos", []ir.Value{I(0)}, "1"},
		{"tan", []ir.Value{I(0)}, "0"},
		{"atan2", []ir.Value{I(0), I(1)}, "0"},
		{"hypot", []ir.Value{I(3), I(4)}, "5"},
		{"hypot", []ir.Value{I(3), "a"}, "!hypot: expected a number"},
	} {
		got, err := evalText(t, c.name, 1000, c.args...)
		if rest, isErr := strings.CutPrefix(c.want, "!"); isErr {
			if err == nil || !strings.Contains(err.Error(), rest) {
				t.Errorf("%s%v = %v, %v; want an error containing %q", c.name, c.args, got, err, rest)
			}
			continue
		}
		if err != nil || fmt.Sprint(got) != c.want {
			t.Errorf("%s%v = %v, %v; want %s", c.name, c.args, got, err, c.want)
		}
	}
}

// divisors reads its work limit from the Env rather than a global.
func TestDivisorsHonoursTheEnvLimit(t *testing.T) {
	if _, err := evalText(t, "divisors", 10, int64(1000)); err == nil || !strings.Contains(err.Error(), "too large to factor") {
		t.Errorf("divisors(1000) under a limit of 10: %v, want a too-large error", err)
	}
}

func TestNumericTypingRules(t *testing.T) {
	I, F, T, B, L := ir.Int(), ir.Float(), ir.Text(), ir.Bool(), ir.List(ir.Int())
	for _, c := range []struct {
		name string
		args []*ir.Type
		want string // "" = types cleanly
		ret  *ir.Type
	}{
		{"abs", []*ir.Type{F}, "", F},
		{"abs", []*ir.Type{T}, "abs needs Int or Float, got Text", nil},
		{"sign", []*ir.Type{F}, "sign argument 1 must be Int, got Float", nil},
		{"pow", []*ir.Type{I, I}, "", I},
		{"pow", []*ir.Type{I, F}, "", F},
		{"pow", []*ir.Type{T, I}, "pow needs Int or Float, got Text", nil},
		{"modpow", []*ir.Type{I, I, F}, "modpow argument 3 must be Int", nil},
		{"solve2x2", []*ir.Type{I, I, I, I, I, I}, "", ir.Tuple(I, I)},
		{"bandall", []*ir.Type{L}, "", I},
		{"bandall", []*ir.Type{I}, "bandall needs a List argument, got Int", nil},
		{"bxorall", []*ir.Type{ir.List(F)}, "bxorall needs Int elements, got Float", nil},
		{"and", []*ir.Type{B, I}, "and needs Bool arguments, got Int", nil},
		{"not", []*ir.Type{I}, "not needs a Bool argument, got Int", nil},
		{"floor", []*ir.Type{I}, "floor needs Float, got Int", nil},
		{"tofloat", []*ir.Type{T}, "", F},
		{"tofloat", []*ir.Type{B}, "tofloat needs Int, Float, or Text, got Bool", nil},
		{"frombase", []*ir.Type{T, F}, "frombase argument 2 must be Int", nil},
		{"fromhex", []*ir.Type{I}, "fromhex argument 1 must be Text", nil},
		{"tobase", []*ir.Type{I, I}, "", T},
		{"digits", []*ir.Type{I}, "", L},
		{"fromdigits", []*ir.Type{I}, "fromdigits needs List<Int>, got Int", nil},
		{"crt", []*ir.Type{L, I}, "crt needs List<Int> arguments, got Int", nil},
		{"atan2", []*ir.Type{I, T}, "atan2 needs Int or Float, got Text", nil},
		{"trunc", []*ir.Type{nil}, "trunc needs Int or Float", nil}, // an unresolved argument must not panic
		{"isprime", []*ir.Type{nil}, "isprime argument 1 must be Int", nil},
	} {
		got, err := Lookup(c.name).Type(c.args)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s%v: unexpected error %v", c.name, c.args, err)
		case c.want == "" && !got.Equal(c.ret):
			t.Errorf("%s%v: typed %v, want %v", c.name, c.args, got, c.ret)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s%v: error %v, want it to contain %q", c.name, c.args, err, c.want)
		}
	}
}
