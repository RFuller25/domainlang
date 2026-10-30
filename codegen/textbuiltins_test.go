package codegen

import (
	"testing"

	"domain/builtins"
)

// A builtin declared in package builtins is typed and interpreted from its Spec;
// only the compiler's half is attached from here. This is the check that the
// third half is not missing.
func TestEveryRegisteredBuiltinHasAnEmitter(t *testing.T) {
	for _, name := range builtins.Names() {
		if builtins.Lookup(name).Emit == nil {
			t.Errorf("builtin %q is registered in package builtins but codegen has no emitter for it", name)
		}
	}
}
