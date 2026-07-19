package typecheck

import (
	"strings"
	"testing"

	"domain/ir"
)

func TestCondTyping(t *testing.T) {
	cases := []struct {
		src    string
		params []*ir.Type
		want   *ir.Type
	}{
		{"(n) -> if n > 0 then n else 0 - n", []*ir.Type{ir.Int()}, ir.Int()},
		{"(s) -> if s = \"x\" then \"yes\" else \"no\"", []*ir.Type{ir.Text()}, ir.Text()},
		{"(xs) -> if length(xs) = 0 then -1 else first(xs)", []*ir.Type{ir.List(ir.Int())}, ir.Int()},
		{"(n) -> if n < 0 then \"neg\" else if n = 0 then \"zero\" else \"pos\"",
			[]*ir.Type{ir.Int()}, ir.Text()},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.src)
		got, err := LambdaType(lam, c.params...)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s: got %s want %s", c.src, got, c.want)
		}
	}
}

func TestCondTypingErrors(t *testing.T) {
	cases := []struct {
		src     string
		params  []*ir.Type
		wantSub string
	}{
		{"(n) -> if n then 1 else 2", []*ir.Type{ir.Int()}, "condition must be Bool"},
		{"(n) -> if n > 0 then 1 else \"x\"", []*ir.Type{ir.Int()}, "arms must have the same type"},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.src)
		_, err := LambdaType(lam, c.params...)
		if err == nil || !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: error %v does not contain %q", c.src, err, c.wantSub)
		}
	}
}

func TestListSetRowColTyping(t *testing.T) {
	gi := ir.Grid(ir.Int())
	cases := []struct {
		src    string
		params []*ir.Type
		want   *ir.Type
	}{
		{"(a, b) -> list(a, b)", []*ir.Type{ir.Int(), ir.Int()}, ir.List(ir.Int())},
		{"(s) -> list(s)", []*ir.Type{ir.Text()}, ir.List(ir.Text())},
		{"(xs) -> set(xs, 0, 9)", []*ir.Type{ir.List(ir.Int())}, ir.List(ir.Int())},
		{"(g) -> row(g, 0)", []*ir.Type{gi}, ir.List(ir.Int())},
		{"(g) -> col(g, 0)", []*ir.Type{ir.Grid(ir.Text())}, ir.List(ir.Text())},
		{"(g) -> rows(g) + cols(g)", []*ir.Type{gi}, ir.Int()},
	}
	for _, c := range cases {
		lam := parseLambda(t, c.src)
		got, err := LambdaType(lam, c.params...)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s: got %s want %s", c.src, got, c.want)
		}
	}

	errCases := []struct {
		src     string
		params  []*ir.Type
		wantSub string
	}{
		{"(a, s) -> list(a, s)", []*ir.Type{ir.Int(), ir.Text()}, "share one type"},
		{"(xs) -> set(xs, 0, \"x\")", []*ir.Type{ir.List(ir.Int())}, "set value must be Int"},
		{"(n) -> row(n, 0)", []*ir.Type{ir.Int()}, "needs a Grid"},
	}
	for _, c := range errCases {
		lam := parseLambda(t, c.src)
		_, err := LambdaType(lam, c.params...)
		if err == nil || !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: error %v does not contain %q", c.src, err, c.wantSub)
		}
	}
}
