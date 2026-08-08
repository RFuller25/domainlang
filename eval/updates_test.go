package eval

import (
	"strings"
	"testing"

	"domain/ir"
)

// Evaluating `:=` and `also` (docs/expressions.md).
//
// Every test here turns updates on the way prims.Resolve does for a program
// that contains one: with them off a binding holds its value directly and
// there is nothing to write to, which is the representation every process that
// has never seen a `:=` keeps.

// withUpdates runs f with the interpreter's boxing enabled. Enabling it is
// one-way in a running process (see eval/bindings.go), so the tests restore
// the flag directly rather than through the exported door.
func withUpdates(t *testing.T, f func()) {
	t.Helper()
	was := updates.Load()
	updates.Store(true)
	defer updates.Store(was)
	f()
}

// evalBody evaluates a one-parameter lambda body over arg, with the named
// stage bindings in scope, and returns the result plus the bindings' values
// after the application.
func evalBody(t *testing.T, body string, arg ir.Value, binds map[string]ir.Value) (ir.Value, map[string]ir.Value) {
	t.Helper()
	lam := parseLambda(t, body)
	names := make([]string, 0, len(binds))
	for name, v := range binds {
		PushBinding(name, v, ir.Int())
		names = append(names, name)
	}
	defer PopBindings(len(names))

	got, err := EvalLambda(lam, arg)
	if err != nil {
		t.Fatalf("eval %s: %v", body, err)
	}
	after := map[string]ir.Value{}
	for _, name := range names {
		for _, b := range bindings {
			if b.name == name {
				after[name] = b.cell.V
			}
		}
	}
	return got, after
}

// TestAssignYieldsWhatItWrites is the operator's own contract: the value of
// `n := e` is e's value, and n holds it afterwards.
func TestAssignYieldsWhatItWrites(t *testing.T) {
	withUpdates(t, func() {
		got, after := evalBody(t, "(x) -> n := x * 2", int64(21), map[string]ir.Value{"n": int64(0)})
		if got != int64(42) {
			t.Fatalf("value: got %v, want 42", got)
		}
		if after["n"] != int64(42) {
			t.Fatalf("binding: got %v, want 42", after["n"])
		}
	})
}

// TestAssignReadsBeforeItWrites: `n := n + 1` computes against the old value.
func TestAssignReadsBeforeItWrites(t *testing.T) {
	withUpdates(t, func() {
		got, after := evalBody(t, "(x) -> n := n + x", int64(5), map[string]ir.Value{"n": int64(10)})
		if got != int64(15) || after["n"] != int64(15) {
			t.Fatalf("got %v / %v, want 15 / 15", got, after["n"])
		}
	})
}

// TestOperandOrder pins left-to-right evaluation around a write, which is the
// property the compiler backend has to reproduce (see codegen's ordering).
func TestOperandOrder(t *testing.T) {
	withUpdates(t, func() {
		// The left n is read before the write, the trailing n after it.
		got, _ := evalBody(t, "(x) -> n + (n := x) + n", int64(10), map[string]ir.Value{"n": int64(1)})
		if got != int64(21) {
			t.Fatalf("got %v, want 21 (1 + 10 + 10)", got)
		}
		// Arguments are evaluated in written order too.
		got, _ = evalBody(t, "(x) -> item(list(n, (n := x), n), 0)", int64(7), map[string]ir.Value{"n": int64(3)})
		if got != int64(3) {
			t.Fatalf("got %v, want 3", got)
		}
	})
}

// TestUnrunWrites: the two constructs that do not evaluate everything they
// contain do not run the writes inside the parts they skip.
func TestUnrunWrites(t *testing.T) {
	withUpdates(t, func() {
		// `and` short-circuits, so the write on its right never happens.
		_, after := evalBody(t, "(x) -> x > 100 and (n := 9) > 0", int64(1), map[string]ir.Value{"n": int64(0)})
		if after["n"] != int64(0) {
			t.Fatalf("short-circuited write ran: n = %v", after["n"])
		}
		// An `if` evaluates only the arm it takes.
		_, after = evalBody(t, "(x) -> if x > 0 then 1 else n := 9", int64(1), map[string]ir.Value{"n": int64(0)})
		if after["n"] != int64(0) {
			t.Fatalf("unselected arm ran: n = %v", after["n"])
		}
	})
}

// TestAlsoRunsAfterTheBody: the clauses cannot change the value already
// yielded, only what the next reader of the name will see.
func TestAlsoRunsAfterTheBody(t *testing.T) {
	withUpdates(t, func() {
		got, after := evalBody(t, "(x) -> n also n := n + 1, n := n * 10",
			int64(0), map[string]ir.Value{"n": int64(4)})
		if got != int64(4) {
			t.Fatalf("value: got %v, want 4 — the body's value predates the clauses", got)
		}
		if after["n"] != int64(50) {
			t.Fatalf("binding: got %v, want 50 — clauses run in written order", after["n"])
		}
	})
}

// TestAlsoInsideAnExpression is the form that makes `also` observable: the
// clauses run before the rest of the surrounding expression reads the name.
func TestAlsoInsideAnExpression(t *testing.T) {
	withUpdates(t, func() {
		got, _ := evalBody(t, "(x) -> (x also n := 100) + n", int64(1), map[string]ir.Value{"n": int64(0)})
		if got != int64(101) {
			t.Fatalf("got %v, want 101", got)
		}
	})
}

// TestConsiderLocalIsWritable, and the write dies with the expression: the
// local is a box of this evaluation's own, so a second application of the same
// lambda starts from the same place the first one did.
func TestConsiderLocalIsWritable(t *testing.T) {
	withUpdates(t, func() {
		lam := parseLambda(t, "(x) -> consider t as x in ((t := t * 2) + t)")
		for range 2 {
			got, err := EvalLambda(lam, int64(5))
			if err != nil {
				t.Fatal(err)
			}
			if got != int64(20) {
				t.Fatalf("got %v, want 20", got)
			}
		}
	})
}

// TestShadowedLocalWritesToTheInnerOne: an inner `consider` gets its own box,
// so a write inside it leaves the outer binding of the same name alone.
func TestShadowedLocalWritesToTheInnerOne(t *testing.T) {
	withUpdates(t, func() {
		got, after := evalBody(t,
			"(x) -> (consider n as 100 in (n := n + 1)) + n",
			int64(0), map[string]ir.Value{"n": int64(7)})
		if got != int64(108) {
			t.Fatalf("value: got %v, want 108", got)
		}
		if after["n"] != int64(7) {
			t.Fatalf("outer binding was written through the shadow: %v", after["n"])
		}
	})
}

// TestStageBindingOutlivesTheApplication is the difference between the two
// kinds of target: a stage binding's box lives in the binding stack, so the
// next application sees the last one's write.
func TestStageBindingOutlivesTheApplication(t *testing.T) {
	withUpdates(t, func() {
		lam := parseLambda(t, "(x) -> n := n + x")
		PushBinding("n", int64(0), ir.Int())
		defer PopBindings(1)
		var last ir.Value
		for _, v := range []int64{1, 2, 3} {
			got, err := EvalLambda(lam, v)
			if err != nil {
				t.Fatal(err)
			}
			last = got
		}
		if last != int64(6) {
			t.Fatalf("got %v, want 6 — the writes accumulate across applications", last)
		}
	})
}

// TestCellsNeverEscape: a name always reads as its value, never as the box
// holding it, however deep the scopes are nested.
func TestCellsNeverEscape(t *testing.T) {
	withUpdates(t, func() {
		got, _ := evalBody(t, "(x) -> consider a as n in consider b as a in b + n",
			int64(0), map[string]ir.Value{"n": int64(21)})
		if got != int64(42) {
			t.Fatalf("got %v (%T), want 42", got, got)
		}
	})
}

// TestReplayRefusesAnUpdatingLambda: the visualizer replays an application to
// show what each subexpression came to, which re-runs the body — sound only
// while the body is a function of its arguments.
func TestReplayRefusesAnUpdatingLambda(t *testing.T) {
	withUpdates(t, func() {
		lam := parseLambda(t, "(x) -> n := x")
		PushBinding("n", int64(0), ir.Int())
		defer PopBindings(1)
		_, err := TraceLambda(lam, nil, int64(5))
		if err == nil || !strings.Contains(err.Error(), "cannot be replayed") {
			t.Fatalf("got %v, want a refusal", err)
		}
		for _, b := range bindings {
			if b.name == "n" && b.cell.V != int64(0) {
				t.Fatalf("the refused replay still wrote: n = %v", b.cell.V)
			}
		}
	})
}
