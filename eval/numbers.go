package eval

import (
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

// The number-theory, base-conversion and text helpers behind the v0.6
// builtins. They are here rather than in eval.go because several of them are
// real algorithms rather than one-line wrappers, and because the compiled
// backend emits the same algorithms (codegen/runtime.go) — keeping them in one
// readable place is what makes the two implementations checkable against each
// other.
//
// Every one of them is written for the size of input a Domain program actually
// has: `isprime` is deterministic Miller-Rabin rather than trial division
// because a 19-digit Int is perfectly legal to write, `divisors` builds its
// result in order rather than sorting one, and `digits` sizes its slice before
// filling it.

// maxBuildable bounds the collections a single builtin will construct. Like
// ir.maxRepresentableCells it is a *physical* limit rather than a policy one —
// a slice this long cannot be allocated on any machine, and Go's makeslice
// panics rather than returning an error, so a clean message is the only way a
// program gets told what happened.
const maxBuildable = 1 << 40

// intRange builds the half-open [lo, hi), matching the Range primitive.
func intRange(lo, hi int64) ([]any, error) {
	if hi <= lo {
		return []any{}, nil
	}
	// Computed in uint64 so a range spanning zero (say, math.MinInt64 to
	// math.MaxInt64) reports its size instead of overflowing to a negative
	// count and silently building nothing.
	n := uint64(hi) - uint64(lo)
	if n > uint64(buildLimit()) {
		return nil, fmt.Errorf("range: [%d, %d) has %d elements, which is more than can be built", lo, hi, n)
	}
	out := make([]any, n)
	for i := range out {
		out[i] = lo + int64(i)
	}
	return out, nil
}

// textList boxes a []string as a Domain List<Text> in one allocation.
func textList(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// twoTexts unpacks two Text arguments for a builtin.
func twoTexts(args []any, name string) (string, string, error) {
	a, ok1 := args[0].(string)
	b, ok2 := args[1].(string)
	if !ok1 || !ok2 {
		return "", "", fmt.Errorf("%s: expected Text arguments", name)
	}
	return a, b, nil
}

// padText widens s to width *runes* by repeating pad on one side, truncating
// the last copy so the result is exactly width runes. Text already at least
// that wide is returned untouched, and an empty pad has nothing to widen with.
//
// Runes, not bytes, because every other position in the language is runes —
// padding by bytes would disagree with `length` on the very input that makes
// padding worth doing.
func padText(s string, width int64, pad string, left bool) (string, error) {
	if pad == "" || width <= 0 {
		return s, nil
	}
	have := int64(0)
	for range s {
		have++
	}
	if have >= width {
		return s, nil
	}
	// The width is a number in the program rather than the size of anything it
	// was given, so it is the one input here that can ask for more than exists.
	// Like fill and repeat it is answered with a message instead of an
	// allocation Go would abort the process over.
	if width-have > buildLimit() {
		return "", fmt.Errorf("padding to %d is more than can be built", width)
	}
	need := int(width - have)
	padRunes := []rune(pad)

	var b strings.Builder
	// The pad is built once and sized exactly: len(s) plus the bytes of the
	// runes actually used, so neither Grow nor the writes reallocate.
	fill := make([]rune, need)
	for i := range fill {
		fill[i] = padRunes[i%len(padRunes)]
	}
	filled := string(fill)
	b.Grow(len(s) + len(filled))
	if left {
		b.WriteString(filled)
		b.WriteString(s)
	} else {
		b.WriteString(s)
		b.WriteString(filled)
	}
	return b.String(), nil
}

// classify implements isdigit/isalpha/isupper/islower over a whole Text.
//
// The empty text is false for all four: "every rune is a digit" is vacuously
// true of it, which is never what a guard means. isupper/islower ask about the
// *cased* runes — "A1" is upper, "1" is neither — so a mixed token still
// answers usefully.
func classify(name, s string) bool {
	if s == "" {
		return false
	}
	cased := false
	for _, r := range s {
		switch name {
		case "isdigit":
			if r < '0' || r > '9' {
				return false
			}
		case "isalpha":
			if !isASCIILetter(r) && !isNonASCIILetter(r) {
				return false
			}
		case "isupper":
			if isLowerRune(r) {
				return false
			}
			cased = cased || isUpperRune(r)
		case "islower":
			if isUpperRune(r) {
				return false
			}
			cased = cased || isLowerRune(r)
		}
	}
	if name == "isupper" || name == "islower" {
		return cased
	}
	return true
}

// The rune predicates are spelled out rather than taken from `unicode` so the
// compiled backend can emit exactly the same tests without pulling the Unicode
// tables into every binary that happens to call isdigit.
func isASCIILetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isNonASCIILetter(r rune) bool {
	return r > 127 && (isUpperRune(r) || isLowerRune(r))
}

func isUpperRune(r rune) bool {
	if r < 128 {
		return r >= 'A' && r <= 'Z'
	}
	return r != toLowerRune(r)
}

func isLowerRune(r rune) bool {
	if r < 128 {
		return r >= 'a' && r <= 'z'
	}
	return r != toUpperRune(r)
}

func toLowerRune(r rune) rune { return []rune(strings.ToLower(string(r)))[0] }
func toUpperRune(r rune) rune { return []rune(strings.ToUpper(string(r)))[0] }

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
func divisorsOf(n int64) ([]any, error) {
	if n <= 0 {
		return nil, fmt.Errorf("divisors: needs a positive number, got %d", n)
	}
	// The pass is √n long, which for a 19-digit Int is three billion steps: the
	// one builtin whose cost is a number in the program rather than the size of
	// a value. Written as a division so the comparison cannot overflow, this is
	// the same budget the builders spend — unreachable at run time, and what
	// stops a fold from stalling an editor mid-line.
	if n/buildLimit() >= buildLimit() {
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
	for i := len(large) - 1; i >= 0; i-- {
		out = append(out, large[i])
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
