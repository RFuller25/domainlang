package prims

import (
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

func TestAmbientStackPushPopOutermostFirst(t *testing.T) {
	if got := ambientDepth(); got != 0 {
		t.Fatalf("depth = %d, want 0 before any push", got)
	}
	pushAmbient("x", ir.Int())
	pushAmbient("y", ir.Text())
	if got := ambientDepth(); got != 2 {
		t.Fatalf("depth = %d, want 2", got)
	}
	types := ambientTypes()
	if len(types) != 2 || !types[0].Equal(ir.Int()) || !types[1].Equal(ir.Text()) {
		t.Fatalf("ambientTypes() = %v, want [Int, Text] outermost first", types)
	}
	popAmbient()
	popAmbient()
	if got := ambientDepth(); got != 0 {
		t.Fatalf("depth = %d, want 0 after popping both", got)
	}
}

func TestAmbientValuesPushPopOutermostFirst(t *testing.T) {
	pushAmbientValue(int64(1), ir.Int())
	pushAmbientValue("two", ir.Text())
	args := ambientArgs()
	if len(args) != 2 || args[0] != int64(1) || args[1] != "two" {
		t.Fatalf("ambientArgs() = %v, want [1, \"two\"] outermost first", args)
	}
	popAmbientValue()
	popAmbientValue()
	if got := ambientArgs(); len(got) != 0 {
		t.Fatalf("ambientArgs() = %v, want none after popping both", got)
	}
}

// TestAmbientTypesFallsBackToRuntimeStack is the regression test for the
// final review's finding: ambientStack (resolve time) is always fully
// unwound by the time any Eval closure runs, so ambientTypes() must answer
// from the runtime value stack in that case, not go nil. A nil result here
// would silently defeat static-type-dependent eval-time logic (e.g. sum()
// of a runtime-empty List<Float> defaulting to Int instead of Float) for any
// lambda lexically inside a For body.
func TestAmbientTypesFallsBackToRuntimeStack(t *testing.T) {
	if got := ambientTypes(); got != nil {
		t.Fatalf("ambientTypes() = %v, want nil before any push", got)
	}
	// Simulates Eval time: ambientStack already popped by resolveForLoop,
	// only the runtime value stack (pushed by resolveForLoop's Eval) is live.
	pushAmbientValue(int64(3), ir.Int())
	pushAmbientValue(3.5, ir.Float())
	types := ambientTypes()
	if len(types) != 2 || !types[0].Equal(ir.Int()) || !types[1].Equal(ir.Float()) {
		t.Fatalf("ambientTypes() = %v, want [Int, Float] outermost first from the runtime stack", types)
	}
	popAmbientValue()
	popAmbientValue()
	if got := ambientTypes(); got != nil {
		t.Fatalf("ambientTypes() = %v, want nil after popping both", got)
	}
}

func TestAmbientTypesReturnsFreshSlice(t *testing.T) {
	pushAmbient("x", ir.Int())
	defer popAmbient()
	a := ambientTypes()
	a = append(a, ir.Bool()) // must not corrupt the internal stack
	b := ambientTypes()
	if len(b) != 1 {
		t.Fatalf("ambientTypes() = %v, want exactly 1 (append on a returned slice leaked into the stack)", b)
	}
}

func TestRequireLambdaArityIncludesAmbientDepth(t *testing.T) {
	pushAmbient("x", ir.Int())
	defer popAmbient()
	// A lambda written with 2 params: 1 base + 1 ambient satisfies "arity 1".
	lam := &ast.Lambda{Params: []string{"v", "x"}}
	args := ArgSet{args: []*ast.Arg{{Name: "Using", Value: ast.LambdaArg{Lambda: lam}}}}
	if _, err := requireLambda(args, 1, "Test", token.Position{}); err != nil {
		t.Errorf("requireLambda with matching ambient-adjusted arity: %v", err)
	}
	lamTooFew := &ast.Lambda{Params: []string{"v"}}
	argsTooFew := ArgSet{args: []*ast.Arg{{Name: "Using", Value: ast.LambdaArg{Lambda: lamTooFew}}}}
	if _, err := requireLambda(argsTooFew, 1, "Test", token.Position{}); err == nil {
		t.Error("requireLambda should still reject a lambda missing the ambient param")
	}
}
