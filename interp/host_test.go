package interp

import (
	"strings"
	"testing"

	"domain/ir"
)

// A pipeline whose scope registered no host runs on Run, which is what every
// program does today and what every pipeline built without a scope means.
func TestUnregisteredScopeRunsOnBatch(t *testing.T) {
	for _, scope := range []string{"", "Advent of Code"} {
		p := &ir.Pipeline{Scope: scope, Nodes: []*ir.Node{{
			Prim: "Const", Out: ir.Int(),
			Eval: func(*ir.Context, ir.Value) (ir.Value, error) { return int64(7), nil },
		}}}
		v, err := RunScoped(p, &ir.Context{})
		if err != nil {
			t.Fatalf("scope %q: %v", scope, err)
		}
		if v != int64(7) {
			t.Errorf("scope %q: value = %v, want 7", scope, v)
		}
	}
}

// A scope with a host of its own gets it, and the linear walk never happens.
// This is the property F4 depends on: a program that is not a chain of nodes
// must not be walked as one.
func TestRegisteredHostReplacesTheLinearWalk(t *testing.T) {
	const scope = "Test Host Realm"
	walked := false
	RegisterHost(scope, func(*ir.Pipeline, *ir.Context) (ir.Value, error) {
		return "hosted", nil
	})
	p := &ir.Pipeline{Scope: scope, Nodes: []*ir.Node{{
		Prim: "Const", Out: ir.Int(),
		Eval: func(*ir.Context, ir.Value) (ir.Value, error) { walked = true; return int64(1), nil },
	}}}
	v, err := RunScoped(p, &ir.Context{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if v != "hosted" {
		t.Errorf("value = %v, want the host's own result", v)
	}
	if walked {
		t.Error("the node was evaluated; a scope with its own host must not be walked linearly")
	}
}

// Two runners answering for one Innate Domain is a mistake in how the binary
// was assembled, and it is caught loudly rather than resolved arbitrarily.
func TestRegisteringTwiceForOneScopePanics(t *testing.T) {
	const scope = "Twice Realm"
	RegisterHost(scope, func(*ir.Pipeline, *ir.Context) (ir.Value, error) { return nil, nil })
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, scope) {
			t.Errorf("panic = %v, want it to name the scope", r)
		}
	}()
	RegisterHost(scope, func(*ir.Pipeline, *ir.Context) (ir.Value, error) { return nil, nil })
}
