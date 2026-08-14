package typecheck

import (
	"strings"
	"testing"

	"domain/ir"
)

// TestAoCBuiltinTypes drives every builtin added for the AoC toolbox through
// LambdaType and checks the inferred result type.
func TestAoCBuiltinTypes(t *testing.T) {
	pt := PointType()
	cases := []struct {
		src    string
		params []*ir.Type
		want   *ir.Type
	}{
		// math / number theory
		{"(n) -> abs(n)", []*ir.Type{ir.Int()}, ir.Int()},
		{"(n) -> sign(n)", []*ir.Type{ir.Int()}, ir.Int()},
		{"(a, b) -> gcd(a, b)", []*ir.Type{ir.Int(), ir.Int()}, ir.Int()},
		{"(a, b) -> lcm(a, b)", []*ir.Type{ir.Int(), ir.Int()}, ir.Int()},
		{"(b, e) -> modpow(b, e, 97)", []*ir.Type{ir.Int(), ir.Int()}, ir.Int()},
		{"(a) -> modinv(a, 97)", []*ir.Type{ir.Int()}, ir.Int()},
		{"(a) -> solve2x2(a, 22, 8400, 67, 67, 5400)", []*ir.Type{ir.Int()}, pt},
		// text
		{"(s) -> toint(s)", []*ir.Type{ir.Text()}, ir.Int()},
		{"(s) -> occurrences(s, \"ab\")", []*ir.Type{ir.Text()}, ir.Int()},
		{"(s) -> repeats(s)", []*ir.Type{ir.Text()}, ir.Bool()},
		// points
		{"(r, c) -> point(r, c)", []*ir.Type{ir.Int(), ir.Int()}, pt},
		{"(p) -> prow(p)", []*ir.Type{pt}, ir.Int()},
		{"(p) -> pcol(p)", []*ir.Type{pt}, ir.Int()},
		{"(p, q) -> padd(p, q)", []*ir.Type{pt, pt}, pt},
		{"(p, q) -> manhattan(p, q)", []*ir.Type{pt, pt}, ir.Int()},
		{"(p) -> rotl(p)", []*ir.Type{pt}, pt},
		{"(p) -> rotr(p)", []*ir.Type{pt}, pt},
		{"(n) -> dirs4()", []*ir.Type{ir.Int()}, ir.List(pt)},
		// grid geometry
		{"(g) -> inbounds(g, 0, 0)", []*ir.Type{ir.Grid(ir.Text())}, ir.Bool()},
		{"(g) -> neighbors4(g, 0, 0)", []*ir.Type{ir.Grid(ir.Int())}, ir.List(pt)},
		{"(g) -> neighbors8(g, 0, 0)", []*ir.Type{ir.Grid(ir.Int())}, ir.List(pt)},
		// contains over a Set
		{"(s) -> contains(s, 3)", []*ir.Type{ir.Set(ir.Int())}, ir.Bool()},
		{"(s) -> contains(s, \"x\")", []*ir.Type{ir.Set(ir.Text())}, ir.Bool()},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.src)
		got, err := LambdaType(lam, c.params...)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s: got %s, want %s", c.src, got, c.want)
		}
	}
}

// TestAoCBuiltinTypeErrors checks the rejection paths.
func TestAoCBuiltinTypeErrors(t *testing.T) {
	pt := PointType()
	cases := []struct {
		src     string
		params  []*ir.Type
		wantSub string
	}{
		{"(s) -> abs(s)", []*ir.Type{ir.Text()}, "abs needs Int or Float"},
		{"(a) -> gcd(a)", []*ir.Type{ir.Int()}, "takes 2 argument(s)"},
		{"(n) -> toint(n)", []*ir.Type{ir.Int()}, "toint needs Text"},
		{"(s) -> occurrences(s, 3)", []*ir.Type{ir.Text()}, "must be Text"},
		{"(n) -> prow(n)", []*ir.Type{ir.Int()}, "must be a point"},
		{"(p, n) -> padd(p, n)", []*ir.Type{pt, ir.Int()}, "must be a point"},
		{"(n) -> inbounds(n, 0, 0)", []*ir.Type{ir.Int()}, "needs a Grid"},
		{"(s) -> contains(s, 3)", []*ir.Type{ir.Set(ir.Text())}, "contains value must be Text"},
		// contains also answers the substring question over Text, so what it
		// refuses is narrower than it used to be.
		{"(n) -> contains(n, 3)", []*ir.Type{ir.Int()}, "needs a Text, List, Set or Graph"},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.src)
		_, err := LambdaType(lam, c.params...)
		if err == nil {
			t.Errorf("%s: expected an error containing %q, got none", c.src, c.wantSub)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: error %q does not contain %q", c.src, err, c.wantSub)
		}
	}
}
