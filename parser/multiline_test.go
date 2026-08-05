package parser

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/lexer"
	"domain/token"
)

// parseSrc lexes and parses, failing the test on either error.
func parseSrc(t *testing.T, src string) *ast.Program {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

// lambdaArg finds the Using: lambda of the program's last top-level statement.
func lambdaArg(t *testing.T, prog *ast.Program) *ast.Lambda {
	t.Helper()
	last := prog.Statements[len(prog.Statements)-1]
	for _, a := range last.Args {
		if a.Name == "Using" {
			la, ok := a.Value.(ast.LambdaArg)
			if !ok {
				t.Fatalf("Using: is %T, want a lambda", a.Value)
			}
			return la.Lambda
		}
	}
	t.Fatal("no Using: argument")
	return nil
}

// The shape the whole feature exists for: a lambda body written down the page,
// broken both inside a call's parentheses and at the outermost level, where
// there are no parentheses to break inside.
func TestLambdaBodyAcrossLines(t *testing.T) {
	src := "Cursed Energy: x\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (s, r) ->\n" +
		"        consider t as s - 1 - min(list(\n" +
		"            abs((s * s) - r),\n" +
		"            abs((s * s) - s - r)\n" +
		"        ))\n" +
		"        in if r = (s * s)\n" +
		"            then s - 1\n" +
		"            else t\n" +
		"Reveal: stdout\n"
	prog := parseSrc(t, src)

	if len(prog.Statements) != 3 {
		t.Fatalf("got %d statements, want 3: the continuation must not become one",
			len(prog.Statements))
	}
	if got := prog.Statements[2].Keyword; got != "Reveal" {
		t.Errorf("statement after the continuation is %q, want Reveal", got)
	}
	lam := lambdaArg(t, &ast.Program{Statements: prog.Statements[:2]})
	if got := strings.Join(lam.Params, ","); got != "s,r" {
		t.Errorf("params = %q, want s,r", got)
	}
	let, ok := lam.Body.(*ast.LetExpr)
	if !ok {
		t.Fatalf("body is %T, want the `consider` binding spanning every line", lam.Body)
	}
	if let.Name != "t" {
		t.Errorf("binding name = %q, want t", let.Name)
	}
	if _, ok := let.Body.(*ast.CondExpr); !ok {
		t.Errorf("`in` body is %T, want the `if` written three lines below it", let.Body)
	}
}

// The value may start on the line under the argument name, so an argument that
// is all body does not have to begin cramped against its colon.
func TestArgValueMayStartOnTheNextLine(t *testing.T) {
	prog := parseSrc(t, "Cursed Energy: x\nCursed Technique: Apply\n    Using:\n        (v) -> v + 1\nReveal: stdout\n")
	lam := lambdaArg(t, &ast.Program{Statements: prog.Statements[:2]})
	if len(lam.Params) != 1 || lam.Params[0] != "v" {
		t.Errorf("params = %v, want [v]", lam.Params)
	}
}

// Joining an argument's continuation must leave the enclosing block exactly as
// it was: the statements after it are still the statements after it.
func TestContinuationLeavesTheEnclosingBlockIntact(t *testing.T) {
	src := "Cursed Energy: x\n" +
		"Simple Domain: While\n" +
		"    Using: (v) ->\n" +
		"        v < 10\n" +
		"    Cursed Technique: Apply\n" +
		"        Using: (v) -> v + 1\n" +
		"Reveal: stdout\n"
	prog := parseSrc(t, src)
	if len(prog.Statements) != 3 {
		t.Fatalf("got %d top-level statements, want 3", len(prog.Statements))
	}
	while := prog.Statements[1]
	if len(while.Args) != 1 || while.Args[0].Name != "Using" {
		t.Errorf("While args = %+v, want one Using:", while.Args)
	}
	if len(while.Block) != 1 {
		t.Fatalf("While body has %d statements, want 1: the continuation must not eat it", len(while.Block))
	}
	if while.Block[0].Keyword != "Cursed Technique" {
		t.Errorf("body statement = %q, want Cursed Technique", while.Block[0].Keyword)
	}
}

// A continuation is a *block*, so an argument on the next line at the same
// indentation is still the next argument.
func TestSiblingArgumentIsNotAContinuation(t *testing.T) {
	prog := parseSrc(t, "Cursed Energy: x\nMaximum Technique: Fold\n    Seed: 0\n    Using: (a, b) -> a + b\nReveal: stdout\n")
	stmt := prog.Statements[1]
	if len(stmt.Args) != 2 {
		t.Fatalf("got %d arguments, want 2 (Seed: and Using:)", len(stmt.Args))
	}
}

// An expression that stops at the end of a line is incomplete rather than
// wrong, which is what puts the REPL into continuation mode instead of
// discarding the line. See Error.NeedsBlock.
func TestUnfinishedExpressionAsksForMoreLines(t *testing.T) {
	for _, src := range []string{
		"Cursed Energy: x\nCursed Technique: Apply\n    Using: (v) ->\n",
		"Cursed Energy: x\nCursed Technique: Apply\n    Using:\n",
	} {
		toks, err := lexer.Lex(src)
		if err != nil {
			t.Fatalf("lex %q: %v", src, err)
		}
		_, err = Parse(src, toks)
		pe, ok := err.(*Error)
		if !ok {
			t.Fatalf("parsing %q gave %T (%v), want *parser.Error", src, err, err)
		}
		if !pe.NeedsBlock {
			t.Errorf("parsing %q: NeedsBlock = false, want true (%s)", src, pe.Msg)
		}
	}
}

// Parse must not rewrite the caller's token slice: `domain fmt` parses a
// program and then reads the very same stream for its layout.
func TestParseDoesNotMutateTheCallersTokens(t *testing.T) {
	src := "Cursed Energy: x\nCursed Technique: Apply\n    Using: (v) ->\n        v + 1\nReveal: stdout\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	before := make([]token.Kind, len(toks))
	for i, tk := range toks {
		before[i] = tk.Kind
	}
	if _, err := Parse(src, toks); err != nil {
		t.Fatal(err)
	}
	if len(toks) != len(before) {
		t.Fatalf("token count changed from %d to %d", len(before), len(toks))
	}
	for i := range before {
		if toks[i].Kind != before[i] {
			t.Fatalf("token %d changed from %s to %s", i, before[i], toks[i].Kind)
		}
	}
}
