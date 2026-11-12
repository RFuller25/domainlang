package parser_test

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/format"
)

// Destructuring lambda parameters: `((a, b)) -> …` binds the fields of one
// Tuple parameter. The parser desugars it, so what is pinned here is the
// shape handed to everything else.
func TestPatternDesugarsToLets(t *testing.T) {
	lam := lambdaOf(t, "(acc, (name, (q, _))) -> acc + q")
	if len(lam.Params) != 2 || lam.Params[0] != "acc" || !strings.HasPrefix(lam.Params[1], "$p") {
		t.Fatalf("params = %v, want [acc $pN]", lam.Params)
	}
	if lam.Patterns == nil || lam.Patterns[0] != nil || lam.Patterns[1].String() != "(name, (q, _))" {
		t.Fatalf("patterns = %v", lam.Patterns)
	}
	if got := strings.Join(lam.BoundNames(), ","); got != "acc,name,q" {
		t.Errorf("bound names = %s", got)
	}
	// name is outermost; q sits under the synthesized inner parameter.
	let, ok := lam.Body.(*ast.LetExpr)
	if !ok || let.Name != "name" || let.Pattern != "(name, (q, _))" || let.PatternLen != 2 {
		t.Fatalf("outer let = %#v", lam.Body)
	}
	if got := format.Expr(let.Value); got != "item("+lam.Params[1]+", 0)" {
		t.Errorf("name is bound to %s", got)
	}
}

func TestPlainLambdasCarryNoPatterns(t *testing.T) {
	lam := lambdaOf(t, "(a, b) -> a + b")
	if lam.Patterns != nil {
		t.Errorf("a lambda with no pattern has Patterns = %v", lam.Patterns)
	}
	// `_` stays an ordinary identifier outside a pattern.
	if lam := lambdaOf(t, "(_, x) -> x"); lam.Params[0] != "_" {
		t.Errorf("params = %v", lam.Params)
	}
}

func TestPatternErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"((a)) -> a", "at least two fields"},
		{"((_, _)) -> 1", "binds no names"},
		{"((a, a)) -> a", `duplicate lambda parameter "a"`},
		{"(a, (a, b)) -> a", `duplicate lambda parameter "a"`},
		{"((a, b)) -> a := 1", "has nothing to write to"},
		{"((a, 3)) -> a", "expected a name"},
		{"((a, b) -> a", "unclosed"},
	} {
		got := parseErr(t, "X:\n    Using: "+c.src+"\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: error %q, want it to contain %q", c.src, got, c.want)
		}
	}
}

// The formatter works on the source's own tokens, so a pattern prints back as
// written; this pins the spacing and that formatting is idempotent.
func TestPatternFormatRoundTrip(t *testing.T) {
	const src = "Cursed Energy: stdin\nCursed Technique: Map Each\n    Using: ((a,b), (c, _), d) -> a + b + c + d\nReveal: stdout\n"
	want := "Cursed Energy: stdin\nCursed Technique: Map Each\n    Using: ((a, b), (c, _), d) -> a + b + c + d\nReveal: stdout\n"
	got, err := format.Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("format = %q\nwant     %q", got, want)
	}
	again, err := format.Format(got)
	if err != nil || again != got {
		t.Errorf("not idempotent: %q (%v)", again, err)
	}
}
