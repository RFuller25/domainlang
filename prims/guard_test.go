package prims

import (
	"strings"
	"testing"

	"domain/ast"
)

// TestResolvePanicBecomesAnError pins the boundary guard. Resolution runs
// inside processes that have to outlive one bad program — the language server,
// the dev TUI, the REPL — so a bug in the resolver has to come back as an
// error, the way interp.Run already brings a bug in a primitive back. A
// malformed program stands in for the next such bug: no source parses to a nil
// statement, which is the point — nothing the resolver is handed should be
// able to end the session.
func TestResolvePanicBecomesAnError(t *testing.T) {
	pipe, err := ResolveWith(&ast.Program{Statements: []*ast.Statement{nil}}, ResolveOptions{})
	if err == nil {
		t.Fatalf("expected an error, got pipeline %v", pipe)
	}
	if !strings.Contains(err.Error(), "internal error during resolution") {
		t.Errorf("error = %v, want an internal-error report", err)
	}
	if pipe != nil {
		t.Errorf("pipeline = %v, want nil alongside the error", pipe)
	}
}
