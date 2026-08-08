package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Resolving a stage binding that something writes to (docs/expressions.md).
//
// The resolver's part of `:=` is deciding what a binding *is*. A binding
// nothing writes to keeps every shortcut it ever had — a constant is folded
// into the lambdas that read it, a function is inlined at its call sites — and
// a binding something writes to gives the first of those up, because a literal
// substituted into a body has nowhere to put a new value.

// resolveUpdateErr resolves a program expected to fail, returning the message.
func resolveUpdateErr(t *testing.T, src string) string {
	t.Helper()
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatalf("expected a resolve error:\n%s", src)
	}
	return err.Error()
}

// TestWrittenConstantIsDemoted: `Consider n As 0` would normally be folded
// into the body as the literal 0. Written to, it has to survive as a value.
func TestWrittenConstantIsDemoted(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider n As 0\n" +
		"    Using: (x) -> x + (n := n + 1)\n"
	v, _ := runPipeline(t, src, "10,20,30")
	if got := ir.FormatValue(v); got != "[11, 22, 33]" {
		t.Fatalf("got %s, want [11, 22, 33]", got)
	}
}

// TestUnwrittenConstantStillFolds is the other half of that: a binding no one
// writes to keeps the folding, and with it the optimizer's body patterns.
func TestUnwrittenConstantStillFolds(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider n As 7\n" +
		"    Using: (x) -> x + n\n"
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	// A folded binding leaves no Consider node behind: there is nothing to
	// compute when the scope opens, because the value is already in the body.
	for _, n := range pipe.Nodes {
		if n.Prim == "Consider" {
			t.Fatalf("an unwritten constant should not have become a runtime binding")
		}
	}
}

// TestWriteFromANestedStatement: a binding scopes over everything nested
// beneath it, so the scan that decides how to lower it has to look there too.
func TestWriteFromANestedStatement(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider n As 0\n" +
		"    Cursed Technique: Apply\n" +
		"        Using: (x) -> x + (n := n + 1)\n"
	v, _ := runPipeline(t, src, "10,20,30")
	if got := ir.FormatValue(v); got != "[11, 22, 33]" {
		t.Fatalf("got %s, want [11, 22, 33]", got)
	}
}

// TestWriteToAnOfBinding: an `Of` binding was always a runtime value, so a
// write needs nothing special from the resolver — it just has to work.
func TestWriteToAnOfBinding(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider total Of Sum\n" +
		"    Using: (x) -> total := total - x\n"
	// 60 is the sum; each element takes it down in turn.
	v, _ := runPipeline(t, src, "10,20,30")
	if got := ir.FormatValue(v); got != "[50, 30, 0]" {
		t.Fatalf("got %s, want [50, 30, 0]", got)
	}
}

// TestWriteInABindingValue: a binding may be written in terms of the ones
// above it, so its value is an expression like any other and may update one.
func TestWriteInABindingValue(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider a Of Sum\n" +
		"    Consider b As (a := a * 2) + 1\n" +
		"    Using: (x) -> x + a + b\n"
	// a becomes 120, b is 121, and every element gets both.
	v, _ := runPipeline(t, src, "10,20,30")
	if got := ir.FormatValue(v); got != "[251, 261, 271]" {
		t.Fatalf("got %s, want [251, 261, 271]", got)
	}
}

// TestWriteToFunctionBindingRefused: a function binding is inlined at its call
// sites, so the name never stands for a value that could be replaced.
func TestWriteToFunctionBindingRefused(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider double As (y) -> y * 2\n" +
		"    Using: (x) -> double := 3\n"
	if msg := resolveUpdateErr(t, src); !strings.Contains(msg, "function binding") {
		t.Fatalf("got %q", msg)
	}
}

// TestWriteToShikigamiParameterRefused: a parameter is substituted into the
// body at each call site, so by the time the body runs the name is a literal.
func TestWriteToShikigamiParameterRefused(t *testing.T) {
	src := "Shikigami \"Bump\" (k: Int)\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x + (k := k + 1)\n" +
		intsPrelude +
		"Shikigami: Bump\n    k: 3\n"
	msg := resolveUpdateErr(t, src)
	if !strings.Contains(msg, "writes to its parameter") {
		t.Fatalf("got %q", msg)
	}
}

// TestShikigamiBindingIsWritable is the fix that message recommends: a
// `Consider` at the top of the body is a real binding, and can be updated.
func TestShikigamiBindingIsWritable(t *testing.T) {
	src := "Shikigami \"Running\" (start: Int)\n" +
		"    Consider n As start\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x + (n := n + 1)\n" +
		intsPrelude +
		"Shikigami: Running\n    start: 100\n"
	v, _ := runPipeline(t, src, "10,20,30")
	if got := ir.FormatValue(v); got != "[111, 122, 133]" {
		t.Fatalf("got %s, want [111, 122, 133]", got)
	}
}

// TestWriteToUnknownName is caught by typing, wherever it is written.
func TestWriteToUnknownName(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> zz := 1\n"
	if msg := resolveUpdateErr(t, src); !strings.Contains(msg, `unknown identifier "zz"`) {
		t.Fatalf("got %q", msg)
	}
}

// TestAlsoWithoutAnyWrite is legal and does nothing observable: the clauses
// are evaluated and discarded, so the body's value comes through untouched.
func TestAlsoWithoutAnyWrite(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> x * 2 also x + 1, length(list(x))\n"
	v, _ := runPipeline(t, src, "10,20,30")
	if got := ir.FormatValue(v); got != "[20, 40, 60]" {
		t.Fatalf("got %s, want [20, 40, 60]", got)
	}
}

// TestAlsoClauseErrorsPropagate: a discarded value is still computed, so a
// clause that fails fails the expression.
func TestAlsoClauseErrorsPropagate(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (x) -> x also item(list(x), 5)\n"
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	ctx := &ir.Context{Stdin: strings.NewReader("1,2"), Stdout: &out}
	var cur ir.Value
	for _, n := range pipe.Nodes {
		v, evalErr := n.Eval(ctx, cur)
		if evalErr != nil {
			if !strings.Contains(evalErr.Error(), "item") {
				t.Fatalf("got %v", evalErr)
			}
			return
		}
		cur = v
	}
	t.Fatal("expected the failing clause to fail the expression")
}
