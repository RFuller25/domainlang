package builtins

import (
	"strings"
	"testing"

	"domain/ir"
)

type fixedEnv int64

func (e fixedEnv) BuildLimit() int64 { return int64(e) }

// evalText calls a registered builtin the way the interpreter does.
func evalText(t *testing.T, name string, limit int64, args ...ir.Value) (ir.Value, error) {
	t.Helper()
	s := Lookup(name)
	if s == nil {
		t.Fatalf("%s is not registered", name)
	}
	if len(args) != s.Arity {
		t.Fatalf("%s takes %d arguments, test passed %d", name, s.Arity, len(args))
	}
	return s.Eval(fixedEnv(limit), args)
}

func TestSpecsAreComplete(t *testing.T) {
	for _, name := range Names() {
		s := Lookup(name)
		if s.Name != name || s.Type == nil || s.Eval == nil || s.Arity < 0 {
			t.Errorf("%s: incomplete spec %+v", name, s)
		}
	}
}

func TestTypingRulesAndWording(t *testing.T) {
	for _, c := range []struct {
		name string
		args []*ir.Type
		want string // "" = types cleanly
	}{
		{"trim", []*ir.Type{ir.Text()}, ""},
		{"trim", []*ir.Type{ir.Int()}, "trim needs Text, got Int"},
		{"toint", []*ir.Type{ir.Int()}, "toint needs Text"},
		{"split", []*ir.Type{ir.Int(), ir.Text()}, "split argument 1 must be Text"},
		{"occurrences", []*ir.Type{ir.Text(), ir.Int()}, "occurrences argument 2 must be Text"},
		{"charat", []*ir.Type{ir.Text(), ir.Text()}, "charat argument 2 must be Int"},
		{"withchar", []*ir.Type{ir.Text(), ir.Int(), ir.Int()}, "withchar replacement must be Text"},
		{"totext", []*ir.Type{ir.Text()}, "totext needs Int or Float"},
		{"totext", []*ir.Type{ir.Float()}, ""},
		{"padleft", []*ir.Type{ir.Text(), ir.Int(), ir.Int()}, "padleft argument 3 must be Text"},
		{"trim", []*ir.Type{nil}, "trim needs Text"}, // an unresolved argument must not panic
	} {
		_, err := Lookup(c.name).Type(c.args)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s%v: unexpected error %v", c.name, c.args, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s%v: error %v, want it to contain %q", c.name, c.args, err, c.want)
		}
	}
}

func TestEvaluators(t *testing.T) {
	for _, c := range []struct {
		name string
		args []ir.Value
		want ir.Value
	}{
		{"trim", []ir.Value{"  a b \n"}, "a b"},
		{"upper", []ir.Value{"héllo"}, "HÉLLO"},
		{"occurrences", []ir.Value{"aaaa", "aa"}, int64(2)},
		{"startswith", []ir.Value{"abc", "ab"}, true},
		{"toint", []ir.Value{" 42 "}, int64(42)},
		{"totext", []ir.Value{int64(7)}, "7"},
		{"ord", []ir.Value{"é"}, int64(233)},
		{"chr", []ir.Value{int64(98)}, "b"},
		{"charat", []ir.Value{"héllo", int64(1)}, "é"},
		{"withchar", []ir.Value{"abc", int64(1), "X"}, "aXc"},
		{"padleft", []ir.Value{"ab", int64(5), "xy"}, "xyxab"},
		{"padright", []ir.Value{"ab", int64(5), "xy"}, "abxyx"},
		{"repeat", []ir.Value{"ab", int64(-1)}, ""},
		{"isdigit", []ir.Value{""}, false},
		{"isupper", []ir.Value{"AB1"}, true},
		{"islower", []ir.Value{"1"}, false},
		{"repeats", []ir.Value{"abab"}, true},
	} {
		got, err := evalText(t, c.name, 1<<20, c.args...)
		if err != nil || got != c.want {
			t.Errorf("%s%v = %v, %v; want %v", c.name, c.args, got, err, c.want)
		}
	}
}

func TestEvaluatorErrors(t *testing.T) {
	for _, c := range []struct {
		name  string
		limit int64
		args  []ir.Value
		want  string
	}{
		{"toint", 1 << 20, []ir.Value{"x"}, `toint: "x" is not an integer`},
		{"ord", 1 << 20, []ir.Value{""}, "ord of the empty text is undefined"},
		{"charat", 1 << 20, []ir.Value{"ab", int64(5)}, "charat: index 5 out of range (length 2)"},
		{"withchar", 1 << 20, []ir.Value{"ab", int64(0), "xy"}, "replacement must be exactly one character"},
		{"chr", 1 << 20, []ir.Value{int64(0xD800)}, "is not a character code"},
		{"trim", 1 << 20, []ir.Value{int64(1)}, "trim: expected Text, got"},
		// The build limit comes from the environment, not a global: the same
		// call is fine under one limit and refused under another.
		{"repeat", 10, []ir.Value{"abc", int64(100)}, "more than can be built"},
		{"padleft", 10, []ir.Value{"a", int64(1000), "x"}, "more than can be built"},
	} {
		_, err := evalText(t, c.name, c.limit, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s%v: error %v, want it to contain %q", c.name, c.args, err, c.want)
		}
	}
}
