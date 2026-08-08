package typecheck

import (
	"strings"
	"testing"

	"domain/ir"
)

// Typing `:=` and `also` (docs/expressions.md).

// typeWithBinding types a one-parameter lambda body with a stage binding of
// the given name and type in scope, which is what makes a write legal at all.
func typeWithBinding(t *testing.T, src, name string, bt *ir.Type, param *ir.Type) (*ir.Type, error) {
	t.Helper()
	PushBinding(name, bt)
	defer PopBindings(1)
	return LambdaType(parseLambda(t, src), param)
}

// TestAssignTyping: a write's type is the type it wrote, which is the type the
// name already held.
func TestAssignTyping(t *testing.T) {
	cases := []struct {
		src   string
		bind  *ir.Type
		param *ir.Type
		want  *ir.Type
	}{
		{"(x) -> n := x", ir.Int(), ir.Int(), ir.Int()},
		{"(x) -> n := x + 1", ir.Int(), ir.Int(), ir.Int()},
		{"(s) -> n := s", ir.Text(), ir.Text(), ir.Text()},
		// The write is an operand like any other once it has a type.
		{"(x) -> (n := x) + 1", ir.Int(), ir.Int(), ir.Int()},
		{"(x) -> (n := x) > 2", ir.Int(), ir.Int(), ir.Bool()},
		// A `consider` local can be written to, and takes the local's type.
		{"(x) -> consider t as x * 2 in (t := t + 1)", ir.Int(), ir.Int(), ir.Int()},
		// A local may shadow the binding with a different type entirely.
		{"(x) -> consider n as \"a\" in (n := \"b\")", ir.Int(), ir.Int(), ir.Text()},
	}
	for _, c := range cases {
		got, err := typeWithBinding(t, c.src, "n", c.bind, c.param)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s: got %s want %s", c.src, got, c.want)
		}
	}
}

// TestAssignTypingErrors: the type of a binding is fixed when its scope opens,
// so a write that would change it is refused rather than allowed to make every
// *reader* of the name wrong.
func TestAssignTypingErrors(t *testing.T) {
	cases := []struct {
		src     string
		bind    *ir.Type
		param   *ir.Type
		wantSub string
	}{
		{"(x) -> n := \"text\"", ir.Int(), ir.Int(), `"n" holds Int, so := cannot write Text`},
		{"(s) -> n := s", ir.Int(), ir.Text(), `"n" holds Int, so := cannot write Text`},
		// No promotion, either: the numeric tower widens an expression's
		// result, not a binding's declared type.
		{"(x) -> n := 1.5", ir.Int(), ir.Int(), `cannot write Float`},
		{"(x) -> n := list(1, 2)", ir.Int(), ir.Int(), `cannot write List<Int>`},
		// A name that is not in scope at all.
		{"(x) -> zz := 1", ir.Int(), ir.Int(), `unknown identifier "zz"`},
	}
	for _, c := range cases {
		_, err := typeWithBinding(t, c.src, "n", c.bind, c.param)
		if err == nil {
			t.Errorf("%s: expected an error", c.src)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: got %q, want it to contain %q", c.src, err, c.wantSub)
		}
	}
}

// TestAlsoTyping: the clauses are typed and discarded; the result is the
// body's type whatever the clauses are.
func TestAlsoTyping(t *testing.T) {
	cases := []struct {
		src   string
		bind  *ir.Type
		param *ir.Type
		want  *ir.Type
	}{
		{"(x) -> x also n := x", ir.Int(), ir.Int(), ir.Int()},
		{"(x) -> x > 1 also n := x", ir.Int(), ir.Int(), ir.Bool()},
		// A clause need not agree with the body about anything.
		{"(x) -> \"kept\" also n := x, n := n + 1", ir.Int(), ir.Int(), ir.Text()},
		// A clause that is not a write at all still has to typecheck.
		{"(x) -> x also length(list(1))", ir.Int(), ir.Int(), ir.Int()},
	}
	for _, c := range cases {
		got, err := typeWithBinding(t, c.src, "n", c.bind, c.param)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s: got %s want %s", c.src, got, c.want)
		}
	}
}

// TestAlsoTypingErrors: a discarded value is still a checked one.
func TestAlsoTypingErrors(t *testing.T) {
	cases := []struct{ src, wantSub string }{
		{"(x) -> x also n := \"text\"", `cannot write Text`},
		{"(x) -> x also zz + 1", `unknown identifier "zz"`},
		{"(x) -> x also 1 + \"a\"", `arithmetic needs Int or Float`},
	}
	for _, c := range cases {
		_, err := typeWithBinding(t, c.src, "n", ir.Int(), ir.Int())
		if err == nil {
			t.Errorf("%s: expected an error", c.src)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: got %q, want it to contain %q", c.src, err, c.wantSub)
		}
	}
}
