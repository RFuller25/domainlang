package codegen

import (
	"strings"
	"testing"

	"domain/ir"
	"domain/token"
)

// A scope this backend has no emitter for is refused outright rather than
// compiled as if it were an ordinary pipeline. The same discipline the package
// already applies to a primitive with no lowering: fail with a position, and
// keep working under `domain run`.
func TestUncompilableScopeIsRefused(t *testing.T) {
	p := &ir.Pipeline{Scope: "Some Future Realm", Nodes: []*ir.Node{{
		Prim: "Read Source", Out: ir.Text(), Pos: token.Position{Line: 3, Col: 1},
	}}}
	_, err := EmitProgram(p, Options{})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"Some Future Realm", "does not compile yet", "domain run", "3:1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

// The scopes this backend does have an emitter for compile as they always did.
func TestCompilableScopesAreNotRefused(t *testing.T) {
	for _, scope := range []string{"", "Advent of Code"} {
		p := &ir.Pipeline{Scope: scope}
		if err := checkScope(p); err != nil {
			t.Errorf("scope %q was refused: %v", scope, err)
		}
	}
}
