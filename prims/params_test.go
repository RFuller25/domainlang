package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// `Params:` lets an indented body stand in for a lambda of two or more
// parameters. Before it, `Fold`, `Reduce`, `Scan` and the pair generators
// refused a body outright — and since the expression layer has no lambda-taking
// builtins, a fold whose step needed a *primitive* could not be written at all.

const rowsHead = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \"\\n\"\n" +
	"Cursed Technique: Extract Integers\n"

const rowsInput = "3 1 2\n9 9 4\n5 5 5"

// The shape the whole feature exists for: a fold whose step sorts. The body is
// over `row`, the accumulator is in scope by name, and the body's result is the
// new accumulator.
func TestFoldBodyNamesItsAccumulator(t *testing.T) {
	src := rowsHead +
		"Maximum Technique: Fold\n" +
		"    Seed: (xs) -> 0\n" +
		"    Params: acc, r\n" +
		"    Domain Expansion: Sort\n" +
		"    Cursed Technique: Apply\n        Using: (s) -> acc + first(s)\n"
	// Smallest of each sorted row: 1 + 4 + 5.
	v, _ := runPipeline(t, src, rowsInput)
	if v.(int64) != 10 {
		t.Fatalf("fold with a body: got %v, want 10", v)
	}
}

func TestScanAndReduceTakeABodyToo(t *testing.T) {
	scan := rowsHead +
		"Cursed Technique: Scan\n" +
		"    Seed: (xs) -> 0\n" +
		"    Params: acc, r\n" +
		"    Domain Expansion: Sort\n" +
		"    Cursed Technique: Apply\n        Using: (s) -> acc + first(s)\n"
	v, _ := runPipeline(t, scan, rowsInput)
	if got := ir.FormatValue(v); got != "[1, 5, 10]" {
		t.Errorf("scan with a body: got %s, want [1, 5, 10]", got)
	}

	reduce := rowsHead +
		"Maximum Technique: Reduce\n" +
		"    Params: a, b\n" +
		"    Cursed Technique: Apply\n        Using: (r) -> concat(a, r)\n"
	v, _ = runPipeline(t, reduce, rowsInput)
	if got := ir.FormatValue(v); got != "[3, 1, 2, 9, 9, 4, 5, 5, 5]" {
		t.Errorf("reduce with a body: got %s", got)
	}
}

// The last declared parameter is the one the body is *over*. For a fold that
// makes the body a pipeline over the element producing the new accumulator,
// which is the shape the lambda has anyway.
func TestBodyIsOverTheLastParameter(t *testing.T) {
	src := rowsHead +
		"Maximum Technique: Fold\n" +
		"    Seed: (xs) -> 0\n" +
		"    Params: acc, r\n" +
		"    Maximum Technique: Count\n" +
		"    Cursed Technique: Apply\n        Using: (n) -> acc + n\n"
	// Three rows of three: the body counts the *row*, not the accumulator.
	v, _ := runPipeline(t, src, rowsInput)
	if v.(int64) != 9 {
		t.Fatalf("body input: got %v, want 9 (three rows of three)", v)
	}
}

// A binding from an outer scope is still readable inside the body, beside the
// parameters — they share one mechanism, so they compose.
func TestBodyParamsSitBesideOuterBindings(t *testing.T) {
	src := rowsHead +
		"Cursed Technique: Apply\n" +
		"    Consider bonus As 100\n" +
		"    Maximum Technique: Fold\n" +
		"        Seed: (xs) -> 0\n" +
		"        Params: acc, r\n" +
		"        Domain Expansion: Sort\n" +
		"        Cursed Technique: Apply\n            Using: (s) -> acc + first(s) + bonus\n"
	v, _ := runPipeline(t, src, rowsInput)
	if v.(int64) != 310 {
		t.Fatalf("with an outer binding: got %v, want 310", v)
	}
}

// Every declared name is a binding, the last one included: it is the body's
// current value *and* readable by name, so a Params: name never turns out to
// be decoration.
func TestEveryParamsNameIsReadable(t *testing.T) {
	src := rowsHead +
		"Maximum Technique: Fold\n" +
		"    Seed: (xs) -> 0\n" +
		"    Params: acc, r\n" +
		"    Domain Expansion: Sort\n" +
		"    Cursed Technique: Apply\n        Using: (s) -> acc + first(s) + length(r)\n"
	// 1+3, then +4+3, then +5+3 — the sorted first plus the row's length.
	v, _ := runPipeline(t, src, rowsInput)
	if v.(int64) != 19 {
		t.Fatalf("reading both names: got %v, want 19", v)
	}
}

// A Params: name becomes a binding, so it obeys the rules a Consider name
// does: shadowing a builtin would change what a call means for every
// expression in scope.
func TestParamsNamesObeyTheBindingRules(t *testing.T) {
	for _, tc := range []struct{ names, want string }{
		{"acc, row", `"row" is an expression builtin`},
		{"first, r", `"first" is an expression builtin`},
		{"acc, acc", `Params: names "acc" twice`},
	} {
		src := rowsHead +
			"Maximum Technique: Fold\n    Seed: (xs) -> 0\n" +
			"    Params: " + tc.names + "\n    Domain Expansion: Sort\n" +
			"Reveal: stdout\n"
		_, err := runErr(t, src, rowsInput)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Params: %s — expected %q, got %v", tc.names, tc.want, err)
		}
	}
}

// Each refusal says what to do instead, and which of the two spellings the
// program is halfway into.
func TestParamsArgumentRules(t *testing.T) {
	for _, tc := range []struct{ name, stage, want string }{
		{"a body with no Params names the arity",
			"Maximum Technique: Fold\n    Seed: (xs) -> 0\n    Domain Expansion: Sort\n",
			"name the parameters with `Params:`"},
		{"the wrong number of names",
			"Maximum Technique: Fold\n    Seed: (xs) -> 0\n    Params: acc\n    Domain Expansion: Sort\n",
			"takes 2 parameter(s), but Params: names 1"},
		{"Params on a one-parameter stage",
			"Cursed Technique: Map Each\n    Params: row\n    Domain Expansion: Sort\n",
			"already has a name for the only value there is"},
		{"Params beside a written lambda",
			"Maximum Technique: Fold\n    Seed: (xs) -> 0\n    Params: acc, r\n" +
				"    Using: (a, r) -> a + first(r)\n",
			"this Using: is a lambda that already names its own"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runErr(t, rowsHead+tc.stage+"Reveal: stdout\n", rowsInput)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error containing %q, got %v", tc.want, err)
			}
		})
	}
}
