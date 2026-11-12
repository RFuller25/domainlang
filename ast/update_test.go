package ast

import "testing"

// A write to a `consider` local is not a write to any outer name, so it adds
// none — but it must still make the set non-empty, because that is what turns
// on the interpreter's boxed bindings. Without the marker a program whose only
// `:=` targets a local ran with bare values and failed at run time with
// `"t" cannot be updated here`, while the compiled binary ran it fine.
func TestUpdatedNamesMarksALocalWrite(t *testing.T) {
	local := &LetExpr{
		Name:  "t",
		Value: &Ident{Name: "x"},
		Body:  &AssignExpr{Name: "t", Value: &Ident{Name: "x"}},
	}
	names := map[string]bool{}
	UpdatedNames(local, names)
	if names["t"] {
		t.Errorf("the local's own name leaked out as a write to an outer binding: %v", names)
	}
	if !names[LocalWriteMarker] {
		t.Errorf("a write to a local left the set empty, so updates would stay off: %v", names)
	}

	// A local that is only read adds nothing.
	reader := &LetExpr{Name: "t", Value: &Ident{Name: "x"}, Body: &Ident{Name: "t"}}
	names = map[string]bool{}
	UpdatedNames(reader, names)
	if len(names) != 0 {
		t.Errorf("a read-only local reported writes: %v", names)
	}
}

// Every builtin whose value is not decided by its arguments must be on the
// list, or the optimizer folds or reorders a lambda that calls it. `pending`
// reads the run's table of requests still out, so it belongs with the clock.
func TestNondeterministicBuiltins(t *testing.T) {
	for _, name := range []string{"random", "randomf", "pick", "frame", "elapsed", "pending"} {
		if !Nondeterministic(name) {
			t.Errorf("%s answers differently across a run but is not marked nondeterministic", name)
		}
	}
	call := &CallExpr{Fn: &Ident{Name: "pending"}, Args: []Expr{&StringLit{Value: "guess"}}}
	if !HasNondeterminism(call) {
		t.Error("an expression calling pending() is not seen as nondeterministic")
	}
	if Nondeterministic("length") {
		t.Error("length is a function of its argument")
	}
}
