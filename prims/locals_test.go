package prims

import (
	"strings"
	"testing"

	"domain/interp"
	"domain/ir"
)

// The three kinds of binding, together, on one stage.
func TestBindingKindsEndToEnd(t *testing.T) {
	src := intsPrelude +
		"Domain Expansion: All Pairs\n" +
		"    Mode: Count\n" +
		"    Consider accum As 3\n" +
		"    Consider double As (x) -> x * 2\n" +
		"    Consider total Of Sum\n" +
		"    Using: (a, b) -> double(a) + b + accum > total\n"
	// total = 15, so the predicate is 2a + b > 12: only (4, 5) qualifies.
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if got := ir.FormatValue(v); got != "1" {
		t.Fatalf("got %s, want 1", got)
	}
}

func TestBindingOfFormsAgree(t *testing.T) {
	// The same value, written three ways: an operation phrase, a lambda over
	// the current value, and a sub-pipeline.
	for _, of := range []string{
		"Consider total Of Sum\n",
		"Consider total Of (xs) -> sum(xs)\n",
		"Consider total Of\n        Maximum Technique: Sum\n",
	} {
		src := intsPrelude +
			"Cursed Technique: Map Each\n" +
			"    " + of +
			"    Using: (x) -> x + total\n"
		v, _ := runPipeline(t, src, "1,2,3")
		if got := ir.FormatValue(v); got != "[7, 8, 9]" {
			t.Fatalf("%q: got %s, want [7, 8, 9]", of, got)
		}
	}
}

// A binding may be written in terms of the ones above it, whichever kind they
// are — which is also why a cycle cannot be written.
func TestBindingSeesEarlierBindings(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider base As 10\n" +
		"    Consider bumped As base * 2\n" +
		"    Consider total Of Sum\n" +
		"    Consider mix As bumped + total\n" +
		"    Using: (x) -> x + mix\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[27, 28, 29]" {
		t.Fatalf("got %s, want [27, 28, 29]", got)
	}
}

// An inner block rebinding a name shadows the outer one for its own scope, and
// the outer binding is intact again afterwards.
func TestBindingShadowsOuterScope(t *testing.T) {
	src := intsPrelude +
		"Simple Domain: Repeat 1\n" +
		"    Consider n As 1\n" +
		"    Cursed Technique: Map Each\n" +
		"        Consider n As 100\n" +
		"        Using: (x) -> x + n\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x + n\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[102, 103, 104]" {
		t.Fatalf("got %s, want [102, 103, 104]", got)
	}
}

// A lambda parameter shadows a binding of the same name — and a function
// binding still sees the binding its own body was written against, not the
// caller's parameter.
func TestLambdaParameterShadowsBinding(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider x As 900\n" +
		"    Consider addx As (v) -> v + x\n" +
		"    Using: (x) -> addx(x)\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[901, 902, 903]" {
		t.Fatalf("got %s, want [901, 902, 903]", got)
	}
}

// Beta-reduction binds the arguments in order, so a naive expansion of
// `sub(a + 10, a)` would bind a to a+10 and then read that back for b. The
// parameters are renamed when an argument mentions one.
func TestFunctionBindingAvoidsCapture(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider sub As (a, b) -> a - b\n" +
		"    Using: (a) -> sub(a + 10, a)\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[10, 10, 10]" {
		t.Fatalf("got %s, want [10, 10, 10] (capture during inlining)", got)
	}
}

// A binding is in scope for every lambda-valued argument of its statement, not
// just Using: — here a loop's predicate and a measured count.
func TestBindingReachesEveryArgument(t *testing.T) {
	// While's predicate: doubling stops once the sum reaches the bound.
	src := intsPrelude +
		"Simple Domain: While\n" +
		"    Consider limit As 20\n" +
		"    Using: (xs) -> sum(xs) < limit\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x * 2\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[4, 8, 12]" {
		t.Fatalf("While predicate: got %s, want [4, 8, 12]", got)
	}

	// A measured argument — a lambda over the current value in an Int slot —
	// reaches the binding the same way.
	src = intsPrelude +
		"Simple Domain: Repeat\n" +
		"    Consider laps As 3\n" +
		"    Times: (xs) -> laps\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x * 2\n"
	v, _ = runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[8, 16, 24]" {
		t.Fatalf("measured Times: got %s, want [8, 16, 24]", got)
	}
}

// A value that has no literal form still binds: it takes the runtime path and
// is computed once when the scope opens.
func TestNonScalarBinding(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider ds As list(10, 20, 30)\n" +
		"    Using: (x) -> x + item(ds, 1)\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[21, 22, 23]" {
		t.Fatalf("got %s, want [21, 22, 23]", got)
	}
}

// Bindings at the top of a Shikigami body scope over the whole body, and see
// the definition's parameters.
func TestBindingInShikigamiBody(t *testing.T) {
	src := "Shikigami \"Bumped\" (k: Int) : List<Int> -> List<Int>\n" +
		"    Consider bump Of Sum\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x + bump + k\n" +
		"\n" +
		intsPrelude +
		"Shikigami: Bumped\n" +
		"    k: 100\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if got := ir.FormatValue(v); got != "[107, 108, 109]" {
		t.Fatalf("got %s, want [107, 108, 109]", got)
	}
}

// A constant folds into the lambda rather than binding at runtime, which is
// what keeps the optimizer's body patterns matchable. Nothing observable
// changes either way, so the shape of the pipeline is what says which path ran.
func TestConstantBindingLeavesNoNode(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider n As 2 * 3\n" +
		"    Using: (x) -> x + n\n"
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, n := range pipe.Nodes {
		if n.Prim == "Consider" {
			t.Fatalf("a constant binding built a Consider node: %s", n.Display)
		}
	}

	// …while one that cannot fold does build one.
	pipe, err = resolveSrc(t, intsPrelude+
		"Cursed Technique: Map Each\n"+
		"    Consider n Of Sum\n"+
		"    Using: (x) -> x + n\n")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	found := false
	for _, n := range pipe.Nodes {
		found = found || n.Prim == "Consider"
	}
	if !found {
		t.Fatalf("an Of binding built no Consider node")
	}
}

func TestBindingErrors(t *testing.T) {
	for _, c := range []struct{ name, binds, using, want string }{
		{
			name:  "a function binding used bare",
			binds: "    Consider double As (x) -> x * 2\n",
			using: "    Using: (x) -> x + double\n",
			want:  "has to be called",
		},
		{
			name:  "a value called",
			binds: "    Consider n As 3\n",
			using: "    Using: (x) -> x + n(1)\n",
			want:  "not a function",
		},
		{
			name:  "the wrong number of arguments",
			binds: "    Consider add As (a, b) -> a + b\n",
			using: "    Using: (x) -> x + add(1)\n",
			want:  "takes 2 argument(s), got 1",
		},
		{
			name:  "a builtin's name",
			binds: "    Consider length As 3\n",
			using: "    Using: (x) -> x + length\n",
			want:  "is an expression builtin",
		},
		{
			name:  "the same name twice",
			binds: "    Consider n As 3\n    Consider n As 4\n",
			using: "    Using: (x) -> x + n\n",
			want:  "bound twice",
		},
		{
			name:  "an Of lambda of the wrong arity",
			binds: "    Consider total Of (a, b) -> a + b\n",
			using: "    Using: (x) -> x + total\n",
			want:  "takes a 1-parameter lambda over the current value",
		},
		{
			name:  "a type error in the binding itself",
			binds: "    Consider oops As \"text\" + 1\n",
			using: "    Using: (x) -> x\n",
			want:  "Consider oops As",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := intsPrelude + "Cursed Technique: Map Each\n" + c.binds + c.using
			_, err := resolveSrc(t, src)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// A binding that fails at runtime reports against the line it was written on,
// naming the binding rather than the stage it happens to precede.
func TestBindingRuntimeErrorNamesTheBinding(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Consider oops Of (xs) -> item(xs, 99)\n" +
		"    Using: (x) -> x + oops\n"
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	_, err = interp.Run(pipe, &ir.Context{Stdin: strings.NewReader("1,2,3")})
	if err == nil || !strings.Contains(err.Error(), "Consider oops") {
		t.Fatalf("error %v does not name the binding", err)
	}
}
