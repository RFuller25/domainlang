package typecheck

import (
	"strings"
	"testing"

	"domain/ir"
)

// TestSparseBuiltinTypes drives the sparse-grid builtins (H) through
// LambdaType and checks the inferred result type.
func TestSparseBuiltinTypes(t *testing.T) {
	spInt := ir.Sparse(ir.Int())
	spText := ir.Sparse(ir.Text())
	cases := []struct {
		src    string
		params []*ir.Type
		want   *ir.Type
	}{
		{"(n) -> sparse(n)", []*ir.Type{ir.Int()}, spInt},
		{"(s) -> sparse(s)", []*ir.Type{ir.Text()}, spText},
		{"(g) -> at(g, 0, 0)", []*ir.Type{spInt}, ir.Int()},
		{"(g) -> at(g, -5, -5)", []*ir.Type{spText}, ir.Text()},
		{"(g) -> put(g, 1, 2, 7)", []*ir.Type{spInt}, spInt},
		{"(g) -> put(g, 1, 2, \"#\")", []*ir.Type{spText}, spText},
		{"(g) -> has(g, 1, 2)", []*ir.Type{spInt}, ir.Bool()},
		{"(g) -> cells(g)", []*ir.Type{spInt}, ir.Int()},
		{"(g) -> minrow(g)", []*ir.Type{spInt}, ir.Int()},
		{"(g) -> maxrow(g)", []*ir.Type{spInt}, ir.Int()},
		{"(g) -> mincol(g)", []*ir.Type{spText}, ir.Int()},
		{"(g) -> maxcol(g)", []*ir.Type{spText}, ir.Int()},
		// totext (added alongside the challenge suite)
		{"(n) -> totext(n)", []*ir.Type{ir.Int()}, ir.Text()},
		{"(f) -> totext(f)", []*ir.Type{ir.Float()}, ir.Text()},
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

func TestSparseBuiltinTypeErrors(t *testing.T) {
	spInt := ir.Sparse(ir.Int())
	cases := []struct {
		src     string
		params  []*ir.Type
		wantSub string
	}{
		{"(n) -> at(n, 0, 0)", []*ir.Type{ir.Int()}, "at needs a Grid or Sparse"},
		{"(g) -> put(g, 0, 0, \"#\")", []*ir.Type{spInt}, "put value must be Int"},
		{"(n) -> put(n, 0, 0, 1)", []*ir.Type{ir.Int()}, "put needs a Sparse"},
		{"(g) -> has(g, 0, 0)", []*ir.Type{ir.Grid(ir.Int())}, "has needs a Sparse"},
		{"(n) -> cells(n)", []*ir.Type{ir.List(ir.Int())}, "cells needs a Sparse"},
		{"(n) -> minrow(n)", []*ir.Type{ir.Grid(ir.Int())}, "minrow needs a Sparse"},
		{"(g) -> put(g, \"x\", 0, 1)", []*ir.Type{spInt}, "must be Int"},
		{"(s) -> totext(s)", []*ir.Type{ir.Text()}, "totext needs Int or Float"},
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
