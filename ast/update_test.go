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
