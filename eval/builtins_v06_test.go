package eval

import (
	"math"
	"strings"
	"testing"

	"domain/ir"
)

// applySrc parses a lambda and applies it to args.
func applySrc(t *testing.T, src string, args ...ir.Value) (ir.Value, error) {
	t.Helper()
	return EvalLambda(parseLambda(t, src), args...)
}

// mustEval is applySrc, failing the test on error.
func mustEval(t *testing.T, src string, args ...ir.Value) ir.Value {
	t.Helper()
	v, err := applySrc(t, src, args...)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return v
}

// wantErr evaluates and requires a failure whose message contains sub.
func wantErr(t *testing.T, src, sub string, args ...ir.Value) {
	t.Helper()
	_, err := applySrc(t, src, args...)
	if err == nil {
		t.Fatalf("%s: expected an error containing %q, got none", src, sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Errorf("%s: error %q does not contain %q", src, err, sub)
	}
}

// intList boxes int64s as a Domain List<Int> argument.
func intList(vs ...int64) []ir.Value {
	out := make([]ir.Value, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

// --- collections -------------------------------------------------------------

// The updates are functional: a lambda may be applied to the same value twice
// (the optimizer folds constants by doing exactly that), so an in-place update
// would let the second application see the first one's work.
func TestCollectionUpdatesAreFunctional(t *testing.T) {
	base := ir.SetFromList(intList(1, 2, 3))
	if got := mustEval(t, "(s) -> size(insert(s, 9))", base); got != int64(4) {
		t.Errorf("insert gave a set of %v, want 4", got)
	}
	if base.Len() != 3 {
		t.Errorf("insert mutated its argument: now %d elements", base.Len())
	}
	if got := mustEval(t, "(s) -> size(del(s, 2))", base); got != int64(2) {
		t.Errorf("del gave a set of %v, want 2", got)
	}
	if base.Len() != 3 {
		t.Errorf("del mutated its argument: now %d elements", base.Len())
	}

	m := ir.NewMapValue()
	m.Put(int64(1), "a")
	if got := mustEval(t, "(m) -> size(insert(m, 2, \"b\"))", m); got != int64(2) {
		t.Errorf("insert gave a map of %v, want 2", got)
	}
	if m.Len() != 1 {
		t.Errorf("insert mutated its argument: now %d entries", m.Len())
	}

	g := ir.NewGridValue(2, 2)
	for i := range g.Cells {
		g.Cells[i] = int64(0)
	}
	if got := mustEval(t, "(g) -> at(setat(g, 0, 0, 7), 0, 0)", g); got != int64(7) {
		t.Errorf("setat gave %v, want 7", got)
	}
	if v, _ := g.At(0, 0); v != int64(0) {
		t.Errorf("setat mutated its argument: (0,0) is now %v", v)
	}
}

// Deleting something that is not there is not an error — it is the same
// collection, which is what makes del usable in a fold without a guard.
func TestDeleteOfAnAbsentKeyIsTheSameCollection(t *testing.T) {
	s := ir.SetFromList(intList(1, 2))
	if got := mustEval(t, "(s) -> size(del(s, 99))", s); got != int64(2) {
		t.Errorf("got %v, want 2", got)
	}
	m := ir.NewMapValue()
	m.Put(int64(1), int64(1))
	if got := mustEval(t, "(m) -> size(del(m, 99))", m); got != int64(1) {
		t.Errorf("got %v, want 1", got)
	}
}

// Insertion order is the contract every collection in the language keeps, and
// the one the compiled backend has to reproduce.
func TestCollectionOrderIsInsertionOrder(t *testing.T) {
	got := mustEval(t, `(xs) -> textjoin(list(`+
		`totext(item(tolist(toset(xs)), 0)), `+
		`totext(item(tolist(toset(xs)), 1)), `+
		`totext(item(tolist(union(toset(xs), toset(list(9, 1)))), 3))), ",")`,
		intList(5, 1, 5, 2))
	if got != "5,1,9" {
		t.Errorf("got %q, want \"5,1,9\"", got)
	}
}

func TestSetAtOutOfRange(t *testing.T) {
	g := ir.NewGridValue(2, 2)
	for i := range g.Cells {
		g.Cells[i] = int64(0)
	}
	wantErr(t, "(g) -> at(setat(g, 5, 0, 1), 0, 0)", "setat: position (5, 0) out of range (grid 2x2)", g)
}

// --- generation --------------------------------------------------------------

func TestRangeAndFill(t *testing.T) {
	cases := map[string]ir.Value{
		"(n) -> sum(range(1, 11))":       int64(55),
		"(n) -> length(range(5, 5))":     int64(0),
		"(n) -> length(range(3, 1))":     int64(0),
		"(n) -> first(range(0 - 3, 3))":  int64(-3),
		"(n) -> length(range(0 - 2, 2))": int64(4),
		"(n) -> length(fill(3, 7))":      int64(3),
		"(n) -> length(fill(0 - 5, 7))":  int64(0),
		"(n) -> sum(fill(4, 2))":         int64(8),
	}
	for src, want := range cases {
		if got := mustEval(t, src, int64(0)); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// A range spanning zero must report its size rather than overflowing to a
// negative count and silently building nothing.
func TestRangeSpanningZeroIsRefusedNotSilentlyEmpty(t *testing.T) {
	// Half the Int range either side of zero: the count is 2^63, which a
	// signed subtraction would report as a negative number.
	wantErr(t, "(n) -> length(range(0 - 4611686018427387904, 4611686018427387904))",
		"more than can be built", int64(0))
}

// --- text --------------------------------------------------------------------

func TestV06TextBuiltins(t *testing.T) {
	cases := map[string]ir.Value{
		`(s) -> textjoin(split(s, ","), "/")`:    "a/b/c",
		`(s) -> textjoin(words("  x  y "), "+")`: "x+y",
		`(s) -> ord("A")`:                        int64(65),
		`(s) -> chr(97)`:                         "a",
		`(s) -> repeat("ab", 3)`:                 "ababab",
		`(s) -> repeat("ab", 0 - 1)`:             "",
		`(s) -> padleft("7", 4, "0")`:            "0007",
		`(s) -> padright("7", 4, "xy")`:          "7xyx",
		`(s) -> padleft("toolong", 3, "0")`:      "toolong",
		`(s) -> trimprefix("xxay", "xx")`:        "ay",
		`(s) -> trimsuffix("axx", "xx")`:         "a",
		`(s) -> contains(s, "b")`:                true,
		`(s) -> contains(s, "zz")`:               false,
	}
	for src, want := range cases {
		if got := mustEval(t, src, "a,b,c"); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// Padding counts runes, like every other text position in the language — by
// bytes it would disagree with `length` on exactly the input that makes padding
// worth doing.
func TestPaddingCountsRunes(t *testing.T) {
	if got := mustEval(t, `(s) -> length(padleft(s, 8, "-"))`, "héllo"); got != int64(8) {
		t.Errorf("padded length = %v, want 8", got)
	}
}

// The empty text is false for all four: "every rune is a digit" is vacuously
// true of it, which is never what a guard means.
func TestTextClassification(t *testing.T) {
	cases := []struct {
		s                          string
		digit, alpha, upper, lower bool
	}{
		{"123", true, false, false, false},
		{"abc", false, true, false, true},
		{"ABC", false, true, true, false},
		{"Ab1", false, false, false, false},
		{"A1", false, false, true, false},
		{"a1", false, false, false, true},
		{"1", true, false, false, false},
		{"", false, false, false, false},
	}
	for _, c := range cases {
		got := [4]bool{
			mustEval(t, "(s) -> isdigit(s)", c.s).(bool),
			mustEval(t, "(s) -> isalpha(s)", c.s).(bool),
			mustEval(t, "(s) -> isupper(s)", c.s).(bool),
			mustEval(t, "(s) -> islower(s)", c.s).(bool),
		}
		want := [4]bool{c.digit, c.alpha, c.upper, c.lower}
		if got != want {
			t.Errorf("%q: digit/alpha/upper/lower = %v, want %v", c.s, got, want)
		}
	}
}

func TestOrdOfEmptyTextFails(t *testing.T) {
	wantErr(t, "(s) -> ord(s)", "ord of the empty text is undefined", "")
}

func TestChrRejectsNonCharacters(t *testing.T) {
	wantErr(t, "(n) -> chr(0 - 1)", "is not a character code", int64(0))
	wantErr(t, "(n) -> chr(55296)", "is not a character code", int64(0)) // a surrogate
}

// --- floats ------------------------------------------------------------------

func TestFloatTower(t *testing.T) {
	near := func(src string, want float64) {
		t.Helper()
		got, ok := mustEval(t, src, int64(0)).(float64)
		if !ok || math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want ~%v", src, got, want)
		}
	}
	near("(n) -> log2(1024.0)", 10)
	near("(n) -> log10(1000.0)", 3)
	near("(n) -> log(exp(2.0))", 2)
	near("(n) -> hypot(3.0, 4.0)", 5)
	near("(n) -> atan2(1.0, 1.0)", math.Pi/4)
	near("(n) -> sin(0.0) + cos(0.0)", 1)
	near("(n) -> pow(2.0, 0.5)", math.Sqrt2)
	// Ints promote, exactly as they do through the operators.
	near("(n) -> log2(8)", 3)
	near("(n) -> hypot(3, 4)", 5)
}

// pow stays integral on two Ints, so nothing that used to typecheck changed.
func TestPowStaysIntegralOnInts(t *testing.T) {
	if got := mustEval(t, "(n) -> pow(2, 10)", int64(0)); got != int64(1024) {
		t.Errorf("pow(2, 10) = %v (%T), want the Int 1024", got, got)
	}
}

func TestTrunc(t *testing.T) {
	cases := map[string]ir.Value{
		"(n) -> trunc(2.7)":       int64(2),
		"(n) -> trunc(0.0 - 2.7)": int64(-2),
		"(n) -> trunc(5)":         int64(5),
	}
	for src, want := range cases {
		if got := mustEval(t, src, int64(0)); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// Domain has no infinity or NaN, so leaving the reals is an error where it
// happens rather than a poison value three stages later.
func TestNonFiniteFloatsAreErrors(t *testing.T) {
	wantErr(t, "(n) -> log(0.0)", "log of a non-positive number", int64(0))
	wantErr(t, "(n) -> log(0.0 - 1.0)", "log of a non-positive number", int64(0))
	wantErr(t, "(n) -> exp(1000.0)", "has no finite value", int64(0))
}

// --- records -----------------------------------------------------------------

func TestRecordConstructionAndUpdate(t *testing.T) {
	got := mustEval(t, `(n) -> consider a as record("k", "one", "v", 1) in `+
		`consider b as with(a, "v", 99) in `+
		`textjoin(list(a.k, totext(a.v), b.k, totext(b.v)), "|")`, int64(0))
	if got != "one|1|one|99" {
		t.Errorf("got %q, want \"one|1|one|99\" (with must not alias)", got)
	}
}

// textjoin no longer needs List<Text>: any element renders exactly as Reveal
// would — Int, Float, Bool and Record included — and the results are joined.
func TestTextJoinOverNonTextElements(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`(_) -> textjoin(list(1, 2, 3), ",")`, "1,2,3"},
		{`(_) -> textjoin(list(1.5, 2.0), ",")`, "1.5,2"},
		{`(_) -> textjoin(list(1 = 1, 1 = 2), ",")`, "true,false"},
		{`(_) -> textjoin(list(record("a", 1, "b", "x")), ",")`, "{a: 1, b: x}"},
		{`(_) -> textjoin(list(list(1, 2), list(3)), "|")`, "[1, 2]|[3]"},
	} {
		got := mustEval(t, c.src, int64(0))
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.src, got, c.want)
		}
	}
}

// --- number theory -----------------------------------------------------------

// Deterministic Miller-Rabin, so these are exact rather than probable — and the
// large cases are past where a 32-bit modular multiply would silently wrap.
func TestIsPrime(t *testing.T) {
	primes := []int64{2, 3, 5, 97, 7919, 2147483647, 67280421310721}
	composites := []int64{-7, 0, 1, 4, 91, 7917, 2147483649, 4295098369}
	for _, n := range primes {
		if got := mustEval(t, "(n) -> isprime(n)", n); got != true {
			t.Errorf("isprime(%d) = false, want true", n)
		}
	}
	for _, n := range composites {
		if got := mustEval(t, "(n) -> isprime(n)", n); got != false {
			t.Errorf("isprime(%d) = true, want false", n)
		}
	}
}

// Ascending, complete, and with no sort in the implementation — the pairing
// walk is what makes that true, so the order is worth pinning.
func TestDivisors(t *testing.T) {
	got := mustEval(t, `(n) -> textjoin(list(`+
		`totext(length(divisors(n))), totext(first(divisors(n))), totext(last(divisors(n))), `+
		`totext(item(divisors(n), 1)), totext(item(divisors(n), 2))), ",")`, int64(28))
	if got != "6,1,28,2,4" {
		t.Errorf("divisors(28) summary = %q, want \"6,1,28,2,4\"", got)
	}
	// A perfect square must not repeat its root.
	if got := mustEval(t, "(n) -> length(divisors(n))", int64(36)); got != int64(9) {
		t.Errorf("divisors(36) has %v entries, want 9", got)
	}
	if got := mustEval(t, "(n) -> length(divisors(n))", int64(1)); got != int64(1) {
		t.Errorf("divisors(1) has %v entries, want 1", got)
	}
	wantErr(t, "(n) -> divisors(n)", "needs a positive number", int64(0))
}

func TestDigitsRoundTrip(t *testing.T) {
	cases := map[int64]int64{0: 1, 7: 1, 90: 2, 9081: 4, -450: 3}
	for n, wantLen := range cases {
		if got := mustEval(t, "(n) -> length(digits(n))", n); got != wantLen {
			t.Errorf("digits(%d) has %v elements, want %d", n, got, wantLen)
		}
		if got := mustEval(t, "(n) -> fromdigits(digits(n))", n); got != abs64(n) {
			t.Errorf("fromdigits(digits(%d)) = %v, want %d", n, got, abs64(n))
		}
	}
	wantErr(t, "(n) -> fromdigits(list(1, 20))", "not a decimal digit", int64(0))
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// The moduli need not be coprime, which is what makes crt usable on a system
// read out of a puzzle rather than one constructed to be coprime.
func TestCRT(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"(n) -> crt(list(2, 3), list(3, 5))", 8},
		{"(n) -> crt(list(0), list(7))", 0},
		{"(n) -> crt(list(1, 1), list(4, 6))", 1},        // non-coprime, consistent
		{"(n) -> crt(list(6, 13), list(7, 15))", 13},     // 13 mod 7 = 6, 13 mod 15 = 13
		{"(n) -> crt(list(2, 3, 2), list(3, 5, 7))", 23}, // the classic
	}
	for _, c := range cases {
		if got := mustEval(t, c.src, int64(0)); got != c.want {
			t.Errorf("%s = %v, want %d", c.src, got, c.want)
		}
	}
	wantErr(t, "(n) -> crt(list(0, 1), list(4, 6))", "disagree", int64(0))
	wantErr(t, "(n) -> crt(list(1), list(0))", "must be positive", int64(0))
}

func TestBasesAndBits(t *testing.T) {
	cases := map[string]ir.Value{
		`(n) -> tohex(48879)`:       "beef",
		`(n) -> tobin(5)`:           "101",
		`(n) -> tobase(255, 3)`:     "100110",
		`(n) -> fromhex("0xFF")`:    int64(255),
		`(n) -> fromhex("ff")`:      int64(255),
		`(n) -> frombase("zz", 36)`: int64(1295),
		`(n) -> popcount(255)`:      int64(8),
		`(n) -> popcount(0 - 1)`:    int64(64),
		`(n) -> bnot(0)`:            int64(-1),
		`(n) -> testbit(5, 2)`:      true,
		`(n) -> testbit(5, 1)`:      false,
	}
	for src, want := range cases {
		if got := mustEval(t, src, int64(0)); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
	wantErr(t, `(n) -> frombase("12", 99)`, "base must be between 2 and 36", int64(0))
	wantErr(t, `(n) -> frombase("xyz", 2)`, "is not a base-2 number", int64(0))
	wantErr(t, `(n) -> testbit(1, 64)`, "outside an Int", int64(0))
}
