package eval

import (
	"strings"
	"testing"

	"domain/ir"
)

// evalSrc evaluates a lambda source against arguments and fails the test on
// error.
func evalSrc(t *testing.T, src string, args ...ir.Value) ir.Value {
	t.Helper()
	lam := parseLambda(t, src)
	v, err := EvalLambda(lam, args...)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return v
}

// evalErr evaluates a lambda source expecting an error containing wantSub.
func evalErr(t *testing.T, src string, wantSub string, args ...ir.Value) {
	t.Helper()
	lam := parseLambda(t, src)
	_, err := EvalLambda(lam, args...)
	if err == nil {
		t.Fatalf("%s: expected an error containing %q, got none", src, wantSub)
	}
	if !strings.Contains(err.Error(), wantSub) {
		t.Fatalf("%s: error %q does not contain %q", src, err, wantSub)
	}
}

func TestMathBuiltins(t *testing.T) {
	cases := []struct {
		src  string
		args []ir.Value
		want int64
	}{
		{"(n) -> abs(n)", []ir.Value{int64(-7)}, 7},
		{"(n) -> abs(n)", []ir.Value{int64(7)}, 7},
		{"(n) -> abs(n)", []ir.Value{int64(0)}, 0},
		{"(n) -> sign(n)", []ir.Value{int64(-42)}, -1},
		{"(n) -> sign(n)", []ir.Value{int64(0)}, 0},
		{"(n) -> sign(n)", []ir.Value{int64(99)}, 1},
		{"(a, b) -> gcd(a, b)", []ir.Value{int64(12), int64(18)}, 6},
		{"(a, b) -> gcd(a, b)", []ir.Value{int64(-12), int64(18)}, 6},
		{"(a, b) -> gcd(a, b)", []ir.Value{int64(0), int64(5)}, 5},
		{"(a, b) -> gcd(a, b)", []ir.Value{int64(0), int64(0)}, 0},
		{"(a, b) -> lcm(a, b)", []ir.Value{int64(4), int64(6)}, 12},
		{"(a, b) -> lcm(a, b)", []ir.Value{int64(7), int64(0)}, 0},
		{"(a, b) -> lcm(a, b)", []ir.Value{int64(-4), int64(6)}, 12},
		{"(b, e) -> modpow(b, e, 1000)", []ir.Value{int64(2), int64(10)}, 24},
		{"(b, e) -> modpow(b, e, 97)", []ir.Value{int64(5), int64(0)}, 1},
		{"(b, e) -> modpow(b, e, 97)", []ir.Value{int64(-2), int64(3)}, 89}, // (-8 mod 97)
		{"(a, m) -> modinv(a, m)", []ir.Value{int64(3), int64(11)}, 4},      // 3*4 = 12 ≡ 1 (mod 11)
		{"(a, m) -> modinv(a, m)", []ir.Value{int64(-3), int64(11)}, 7},     // 8*7 = 56 ≡ 1 (mod 11)
	}
	for _, c := range cases {
		got := evalSrc(t, c.src, c.args...)
		if got != c.want {
			t.Errorf("%s%v = %v, want %d", c.src, c.args, got, c.want)
		}
	}
}

func TestMathBuiltinErrors(t *testing.T) {
	evalErr(t, "(e) -> modpow(2, e, 97)", "exponent must be non-negative", int64(-1))
	evalErr(t, "(m) -> modpow(2, 3, m)", "modulus must be positive", int64(0))
	evalErr(t, "(a) -> modinv(a, 10)", "no inverse", int64(4)) // gcd(4,10)=2
	evalErr(t, "(m) -> modinv(3, m)", "modulus must be positive", int64(-5))
}

func TestSolve2x2(t *testing.T) {
	// AoC 2024 D13 shape: 94x + 22y = 8400, 34x + 67y = 5400 → x=80, y=40.
	got := evalSrc(t, "(a) -> solve2x2(a, 22, 8400, 34, 67, 5400)", int64(94))
	p, ok := got.([]ir.Value)
	if !ok || len(p) != 2 || p[0] != int64(80) || p[1] != int64(40) {
		t.Fatalf("solve2x2 = %v, want (80, 40)", got)
	}
	evalErr(t, "(a) -> solve2x2(a, 2, 3, 2, 4, 6)", "determinant is zero", int64(1))
	evalErr(t, "(a) -> solve2x2(a, 0, 3, 0, 2, 3)", "not integral", int64(2)) // 2x=3
}

func TestTextBuiltins(t *testing.T) {
	if got := evalSrc(t, "(s) -> toint(s)", " -42 "); got != int64(-42) {
		t.Fatalf("toint = %v, want -42", got)
	}
	evalErr(t, "(s) -> toint(s)", "is not an integer", "12ab")

	if got := evalSrc(t, "(s) -> occurrences(s, \"ab\")", "abcabab"); got != int64(3) {
		t.Fatalf("occurrences = %v, want 3", got)
	}
	if got := evalSrc(t, "(s) -> occurrences(s, \"aa\")", "aaaa"); got != int64(2) {
		t.Fatalf("occurrences should count non-overlapping: got %v, want 2", got)
	}

	for s, want := range map[string]bool{
		"abab": true, "aaa": true, "abcabc": true,
		"aba": false, "a": false, "": false, "abcd": false,
	} {
		if got := evalSrc(t, "(s) -> repeats(s)", s); got != want {
			t.Errorf("repeats(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestPointBuiltins(t *testing.T) {
	pt := func(r, c int64) []ir.Value { return []ir.Value{r, c} }

	got := evalSrc(t, "(r, c) -> point(r, c)", int64(2), int64(3))
	if !ir.DeepEqual(got, pt(2, 3)) {
		t.Fatalf("point = %v", got)
	}
	if got := evalSrc(t, "(p) -> prow(p)", pt(2, 3)); got != int64(2) {
		t.Fatalf("prow = %v", got)
	}
	if got := evalSrc(t, "(p) -> pcol(p)", pt(2, 3)); got != int64(3) {
		t.Fatalf("pcol = %v", got)
	}
	if got := evalSrc(t, "(p, q) -> padd(p, q)", pt(1, 2), pt(10, -5)); !ir.DeepEqual(got, pt(11, -3)) {
		t.Fatalf("padd = %v", got)
	}
	if got := evalSrc(t, "(p, q) -> manhattan(p, q)", pt(1, 2), pt(4, -2)); got != int64(7) {
		t.Fatalf("manhattan = %v, want 7", got)
	}
	// Rotating "up" right yields "right"; left yields "left". Four right
	// rotations are the identity.
	up, right, left := pt(-1, 0), pt(0, 1), pt(0, -1)
	if got := evalSrc(t, "(p) -> rotr(p)", up); !ir.DeepEqual(got, right) {
		t.Fatalf("rotr(up) = %v, want right", got)
	}
	if got := evalSrc(t, "(p) -> rotl(p)", up); !ir.DeepEqual(got, left) {
		t.Fatalf("rotl(up) = %v, want left", got)
	}
	if got := evalSrc(t, "(p) -> rotr(rotr(rotr(rotr(p))))", pt(3, 7)); !ir.DeepEqual(got, pt(3, 7)) {
		t.Fatalf("rotr^4 = %v, want identity", got)
	}
	if got := evalSrc(t, "(n) -> dirs4()", int64(0)); !ir.DeepEqual(got,
		[]ir.Value{pt(-1, 0), pt(1, 0), pt(0, -1), pt(0, 1)}) {
		t.Fatalf("dirs4 = %v", got)
	}
	evalErr(t, "(p) -> prow(p)", "expected a point", int64(3))
}

func TestGridGeometryBuiltins(t *testing.T) {
	g := ir.NewGridValue(2, 3) // 2 rows, 3 cols
	for i := range g.Cells {
		g.Cells[i] = int64(i)
	}
	if got := evalSrc(t, "(g) -> inbounds(g, 1, 2)", g); got != true {
		t.Fatal("inbounds(1,2) should be true")
	}
	if got := evalSrc(t, "(g) -> inbounds(g, 2, 0)", g); got != false {
		t.Fatal("inbounds(2,0) should be false")
	}
	if got := evalSrc(t, "(g) -> inbounds(g, 0 - 1, 0)", g); got != false {
		t.Fatal("inbounds(-1,0) should be false")
	}
	// Corner (0,0) has 2 orthogonal and 3 total neighbors.
	n4 := evalSrc(t, "(g) -> neighbors4(g, 0, 0)", g).([]ir.Value)
	if len(n4) != 2 {
		t.Fatalf("neighbors4 corner = %d, want 2", len(n4))
	}
	n8 := evalSrc(t, "(g) -> neighbors8(g, 0, 0)", g).([]ir.Value)
	if len(n8) != 3 {
		t.Fatalf("neighbors8 corner = %d, want 3", len(n8))
	}
	// Interior of a 3x3 grid has 4 and 8.
	g3 := ir.NewGridValue(3, 3)
	if got := evalSrc(t, "(g) -> length(neighbors4(g, 1, 1))", g3); got != int64(4) {
		t.Fatalf("neighbors4 interior = %v, want 4", got)
	}
	if got := evalSrc(t, "(g) -> length(neighbors8(g, 1, 1))", g3); got != int64(8) {
		t.Fatalf("neighbors8 interior = %v, want 8", got)
	}
}

func TestContainsOnSet(t *testing.T) {
	s := ir.SetFromList([]ir.Value{int64(1), int64(2), int64(3)})
	if got := evalSrc(t, "(s) -> contains(s, 2)", s); got != true {
		t.Fatal("contains(set, 2) should be true")
	}
	if got := evalSrc(t, "(s) -> contains(s, 9)", s); got != false {
		t.Fatal("contains(set, 9) should be false")
	}
}
