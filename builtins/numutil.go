package builtins

import (
	"fmt"
	"math"
	"math/bits"
	"slices"
	"strconv"
	"strings"
)

// The algorithms behind the numeric builtins. They were the interpreter's
// helpers (eval/numbers.go and the foot of eval/eval.go) until the registry
// needed them from here; the compiled backend emits the same algorithms
// (codegen/runtime.go), and keeping them in one readable place is what makes
// the two implementations checkable against each other.
//
// Every one of them is written for the size of input a Domain program actually
// has: `isprime` is deterministic Miller-Rabin rather than trial division
// because a 19-digit Int is perfectly legal to write, `divisors` builds its
// result in order rather than sorting one, and `digits` sizes its slice before
// filling it.

func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// float1 applies a one-argument float function, refusing a result the value
// model cannot carry. Domain has no infinity or NaN — there is no way to write
// one and no way to print one usefully — so a computation that leaves the reals
// is an error where it happens rather than a poison value that surfaces three
// stages later.
func float1(name string, f float64) (float64, error) {
	var r float64
	switch name {
	case "log", "log2", "log10":
		if f <= 0 {
			return 0, fmt.Errorf("%s of a non-positive number (%s)", name, formatF(f))
		}
		switch name {
		case "log":
			r = math.Log(f)
		case "log2":
			r = math.Log2(f)
		default:
			r = math.Log10(f)
		}
	case "exp":
		r = math.Exp(f)
	case "sin":
		r = math.Sin(f)
	case "cos":
		r = math.Cos(f)
	case "tan":
		r = math.Tan(f)
	}
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return 0, fmt.Errorf("%s(%s) has no finite value", name, formatF(f))
	}
	return r, nil
}

func formatF(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// parseBase implements frombase and fromhex. A leading sign is accepted (so
// tobase round-trips a negative), and fromhex tolerates the `0x` a hex literal
// is usually written with.
func parseBase(name, s string, base int64) (int64, error) {
	if base < 2 || base > 36 {
		return 0, fmt.Errorf("frombase: base must be between 2 and 36, got %d", base)
	}
	t := strings.TrimSpace(s)
	if name == "fromhex" {
		if rest, ok := strings.CutPrefix(t, "0x"); ok {
			t = rest
		} else if rest, ok := strings.CutPrefix(t, "0X"); ok {
			t = rest
		}
	}
	n, err := strconv.ParseInt(t, int(base), 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a base-%d number", name, s, base)
	}
	return n, nil
}

// decimalDigits returns the decimal digits of |n|, most significant first.
// Zero is [0], not the empty list.
//
// The count is computed first so the slice is allocated once and filled from
// the back — the obvious "append then reverse" does the same work twice.
func decimalDigits(n int64) []any {
	if n == 0 {
		return []any{int64(0)}
	}
	// Negated into uint64 so math.MinInt64, whose magnitude has no int64
	// representation, still counts and divides correctly.
	u := uint64(n)
	if n < 0 {
		u = -u
	}
	count := 0
	for v := u; v > 0; v /= 10 {
		count++
	}
	out := make([]any, count)
	for i := count - 1; i >= 0; i-- {
		out[i] = int64(u % 10)
		u /= 10
	}
	return out
}

// fromDigits is the inverse of digits: the number a most-significant-first
// digit list spells. It is the inverse, so it takes decimal digits only.
func fromDigits(ds []int64) (int64, error) {
	var n int64
	for i, d := range ds {
		if d < 0 || d > 9 {
			return 0, fmt.Errorf("fromdigits: element %d is %d, not a decimal digit", i, d)
		}
		// Checked rather than wrapped: a silently overflowed number is a wrong
		// answer that looks like a right one, the same reason factorial refuses
		// past 20!.
		if n > (math.MaxInt64-d)/10 {
			return 0, fmt.Errorf("fromdigits: %d digits overflow Int", len(ds))
		}
		n = n*10 + d
	}
	return n, nil
}

// mulmod computes a*b mod m without overflowing, via the 128-bit product.
// Miller-Rabin on int64-sized inputs is impossible without it: the naive
// product overflows for any modulus past 2^32.
func mulmod(a, b, m uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	// Div64 requires its high word to be below the divisor, which hi%m is; the
	// reduction is exact because (hi·2^64 + lo) mod m = ((hi mod m)·2^64 + lo) mod m.
	_, r := bits.Div64(hi%m, lo, m)
	return r
}

// powmod is b^e mod m by binary exponentiation over mulmod.
func powmod(b, e, m uint64) uint64 {
	r := uint64(1) % m
	b %= m
	for e > 0 {
		if e&1 == 1 {
			r = mulmod(r, b, m)
		}
		b = mulmod(b, b, m)
		e >>= 1
	}
	return r
}

// millerRabinBases is a witness set that makes the test *deterministic* for
// every uint64 — the first twelve primes suffice past 3.3×10^24, which covers
// every Int a Domain program can hold. So `isprime` is exact, not probabilistic.
var millerRabinBases = [...]uint64{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37}

// isPrime reports primality in O(log³ n) rather than the O(√n) a trial division
// would take: a 19-digit Int is legal to write, and √(9×10^18) is three billion
// divisions.
func isPrime(n int64) bool {
	if n < 2 {
		return false
	}
	u := uint64(n)
	for _, p := range millerRabinBases {
		if u == p {
			return true
		}
		if u%p == 0 {
			return false
		}
	}
	// n - 1 = d·2^s with d odd.
	d := u - 1
	s := bits.TrailingZeros64(d)
	d >>= uint(s)

	for _, a := range millerRabinBases {
		x := powmod(a, d, u)
		if x == 1 || x == u-1 {
			continue
		}
		composite := true
		for range s - 1 {
			x = mulmod(x, x, u)
			if x == u-1 {
				composite = false
				break
			}
		}
		if composite {
			return false
		}
	}
	return true
}

// divisorsOf returns every positive divisor of n in ascending order.
//
// One pass to √n, collecting each pair as it is found: the small half lands in
// order, the large half in reverse, and appending the second backwards produces
// a sorted result with no sort at all.
func divisorsOf(limit, n int64) ([]any, error) {
	if n <= 0 {
		return nil, fmt.Errorf("divisors: needs a positive number, got %d", n)
	}
	// The pass is √n long, which for a 19-digit Int is three billion steps: the
	// one builtin whose cost is a number in the program rather than the size of
	// a value. Written as a division so the comparison cannot overflow, this is
	// the same budget the builders spend — unreachable at run time, and what
	// stops a fold from stalling an editor mid-line.
	if n/limit >= limit {
		return nil, fmt.Errorf("divisors: %d is too large to factor here", n)
	}
	var small, large []int64
	for d := int64(1); d <= n/d; d++ {
		if n%d != 0 {
			continue
		}
		small = append(small, d)
		if q := n / d; q != d {
			large = append(large, q)
		}
	}
	out := make([]any, 0, len(small)+len(large))
	for _, d := range small {
		out = append(out, d)
	}
	for _, l := range slices.Backward(large) {
		out = append(out, l)
	}
	return out, nil
}

// crtSolve is the Chinese Remainder Theorem over a system of congruences
// x ≡ rs[i] (mod ms[i]), returning the smallest non-negative solution.
//
// The moduli need not be coprime: the pairwise combination checks that the
// residues agree modulo the gcd and merges on the lcm, which is what makes it
// usable on a system read out of a puzzle rather than one constructed to be
// coprime. An inconsistent system is an error, not a wrong answer.
func crtSolve(rs, ms []int64) (int64, error) {
	if len(rs) != len(ms) {
		return 0, fmt.Errorf("crt: %d residues but %d moduli", len(rs), len(ms))
	}
	if len(rs) == 0 {
		return 0, fmt.Errorf("crt: needs at least one congruence")
	}
	var r, m int64 = 0, 1
	for i := range rs {
		if ms[i] <= 0 {
			return 0, fmt.Errorf("crt: modulus %d must be positive, got %d", i, ms[i])
		}
		var err error
		if r, m, err = crtPair(r, m, ((rs[i]%ms[i])+ms[i])%ms[i], ms[i]); err != nil {
			return 0, err
		}
	}
	return r, nil
}

// crtPair merges x ≡ r1 (mod m1) with x ≡ r2 (mod m2).
func crtPair(r1, m1, r2, m2 int64) (int64, int64, error) {
	g, p, _ := extendedGCD(m1, m2)
	diff := r2 - r1
	if diff%g != 0 {
		return 0, 0, fmt.Errorf("crt: no solution — %d (mod %d) and %d (mod %d) disagree", r1, m1, r2, m2)
	}
	lcm, err := mulChecked(m1, m2/g)
	if err != nil {
		return 0, 0, fmt.Errorf("crt: the combined modulus overflows Int")
	}
	// step = (diff/g · p) mod (m2/g), reduced before multiplying by m1 so the
	// product below stays in range whenever the answer itself does.
	unit := m2 / g
	step := mulmodSigned(((diff/g)%unit+unit)%unit, ((p%unit)+unit)%unit, unit)
	delta, err := mulChecked(step, m1)
	if err != nil {
		return 0, 0, fmt.Errorf("crt: the combined modulus overflows Int")
	}
	r := r1 + delta
	return ((r % lcm) + lcm) % lcm, lcm, nil
}

// extendedGCD returns g = gcd(a, b) with x, y satisfying a·x + b·y = g.
func extendedGCD(a, b int64) (g, x, y int64) {
	if b == 0 {
		return a, 1, 0
	}
	g, x1, y1 := extendedGCD(b, a%b)
	return g, y1, x1 - (a/b)*y1
}

// mulmodSigned multiplies two non-negative values below m, via the 128-bit
// product so a modulus past 2^31 does not overflow the way a plain a*b would.
func mulmodSigned(a, b, m int64) int64 {
	if m <= 0 {
		return 0
	}
	return int64(mulmod(uint64(a), uint64(b), uint64(m)))
}

// mulChecked multiplies, reporting overflow rather than wrapping.
func mulChecked(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	p := a * b
	if p/b != a {
		return 0, fmt.Errorf("integer overflow")
	}
	return p, nil
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

// isqrtInt is the integer square root: the largest n with n*n <= x, exact for
// every x in range — including near a perfect square, where the float root
// alone can land one off.
func isqrtInt(x int64) (int64, error) {
	if x < 0 {
		return 0, fmt.Errorf("isqrt: negative input %d", x)
	}
	if x < 2 {
		return x, nil
	}
	// The float root is within one of the answer; the corrections compare by
	// division so they cannot overflow near MaxInt64. (The Newton iteration
	// this replaced started at x/2+1, which is already below x only from 3 on,
	// so isqrt(2) came back as 2.)
	r := int64(math.Sqrt(float64(x)))
	for r > x/r {
		r--
	}
	for r+1 <= x/(r+1) {
		r++
	}
	return r, nil
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
	// The product before that division is taken in 128 bits: it can exceed
	// int64 when the quotient does not — C(62, 31) fits, its last product
	// does not — and each quotient is at most C(n, k), so a quotient that
	// does not fit means the answer does not either.
	r := uint64(1)
	for i := int64(1); i <= k; i++ {
		hi, lo := bits.Mul64(r, uint64(n-k+i))
		if hi >= uint64(i) {
			return 0, fmt.Errorf("choose: C(%d, %d) overflows Int", n, k)
		}
		r, _ = bits.Div64(hi, lo, uint64(i))
		if r > math.MaxInt64 {
			return 0, fmt.Errorf("choose: C(%d, %d) overflows Int", n, k)
		}
	}
	return int64(r), nil
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
	base = floorMod(base, m)
	for exp > 0 {
		if exp&1 == 1 {
			result = mulMod(result, base, m)
		}
		base = mulMod(base, base, m)
		exp >>= 1
	}
	return result, nil
}

// mulMod is a*b mod m for a and b in [0, m). Below the square root of
// MaxInt64 the product fits and plain arithmetic is fastest; above it the
// product is taken in 128 bits, which a modulus like AoC 2019 day 22's
// (~1.2e14) needs — plain int64 multiplication wrapped there and handed back
// a negative "remainder".
func mulMod(a, b, m int64) int64 {
	if m <= 3037000499 {
		return a * b % m
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	return int64(bits.Rem64(hi, lo, uint64(m)))
}

// floorMod is a mod m in [0, m) for a positive m, without the overflow of
// ((a % m) + m) % m when m is above MaxInt64/2.
func floorMod(a, m int64) int64 {
	r := a % m
	if r < 0 {
		r += m
	}
	return r
}

// modInverse computes the multiplicative inverse of a modulo m (extended
// Euclid). a and m must be coprime and m positive; the result is in [0, m).
func modInverse(a, m int64) (int64, error) {
	if m <= 0 {
		return 0, fmt.Errorf("modinv: modulus must be positive, got %d", m)
	}
	a = floorMod(a, m)
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
	return floorMod(x0, m), nil
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
