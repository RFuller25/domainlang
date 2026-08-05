package parser

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/lexer"
)

// parseErr lexes and parses, returning the parse error rather than failing:
// half of what bindings need to pin is which spellings are refused.
func parseErr(t *testing.T, src string) error {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	_, err = Parse(src, toks)
	return err
}

// The five shapes a binding's right-hand side can take, and how each lands in
// the tree.
func TestBindingShapes(t *testing.T) {
	src := `Cursed Energy: stdin
Domain Expansion: All Pairs
    Mode: First
    Consider accum As 3
    Consider len As (x) -> length(x)
    Consider total Of Sum
    Consider big Of (xs) -> sum(xs) * 2
    Consider parts Of
        Cursed Technique: Map Each
            Using: (n) -> n + 1
        Maximum Technique: Sum
    Using: (a, b) -> a + b + accum
`
	stmt := parseSrc(t, src).Statements[1]
	if len(stmt.Binds) != 5 {
		t.Fatalf("got %d bindings, want 5", len(stmt.Binds))
	}
	// Bindings are their own line kind: they must not land among the
	// statement's arguments, nor turn its block into a sub-pipeline (which
	// would make every stage that takes no body reject them).
	if len(stmt.Args) != 2 {
		t.Fatalf("got %d args, want 2 (Mode, Using)", len(stmt.Args))
	}
	if len(stmt.Block) != 0 {
		t.Fatalf("got %d block statements, want 0", len(stmt.Block))
	}

	for _, c := range []struct {
		name       string
		of         bool
		wantValue  bool
		wantLambda bool
		wantBody   int
	}{
		{name: "accum", wantValue: true},
		{name: "len", wantLambda: true},
		{name: "total", of: true, wantBody: 1},
		{name: "big", of: true, wantLambda: true},
		{name: "parts", of: true, wantBody: 2},
	} {
		var b *ast.Binding
		for _, cand := range stmt.Binds {
			if cand.Name == c.name {
				b = cand
			}
		}
		if b == nil {
			t.Fatalf("no binding named %q", c.name)
		}
		if b.Of != c.of {
			t.Errorf("%s: Of = %v, want %v", c.name, b.Of, c.of)
		}
		if (b.Value != nil) != c.wantValue {
			t.Errorf("%s: Value set = %v, want %v", c.name, b.Value != nil, c.wantValue)
		}
		if (b.Lambda != nil) != c.wantLambda {
			t.Errorf("%s: Lambda set = %v, want %v", c.name, b.Lambda != nil, c.wantLambda)
		}
		if len(b.Body) != c.wantBody {
			t.Errorf("%s: %d body statements, want %d", c.name, len(b.Body), c.wantBody)
		}
	}
}

// `As` takes a whole expression, not just a literal — including a
// parenthesized one, which is what makes the lambda lookahead necessary.
func TestBindingAsExpression(t *testing.T) {
	prog := parseSrc(t, `Cursed Energy: stdin
Cursed Technique: Map Each
    Consider n As (3 + 4) * 2
    Using: (x) -> x + n
`)
	b := prog.Statements[1].Binds[0]
	if b.Lambda != nil {
		t.Fatalf("a parenthesized expression was read as a lambda")
	}
	if _, ok := b.Value.(*ast.BinaryExpr); !ok {
		t.Fatalf("Value = %T, want a BinaryExpr", b.Value)
	}
}

// A binding's value continues on indented lines like any argument's.
func TestBindingValueContinuesIndented(t *testing.T) {
	prog := parseSrc(t, `Cursed Energy: stdin
Cursed Technique: Map Each
    Consider n As
        consider a as 2
        in a * 3
    Using: (x) -> x + n
`)
	if _, ok := prog.Statements[1].Binds[0].Value.(*ast.LetExpr); !ok {
		t.Fatalf("Value = %T, want a LetExpr", prog.Statements[1].Binds[0].Value)
	}
}

// The shape is contextual: only `Consider NAME As|Of` is a binding, so a
// statement whose phrase merely opens with the word is still that statement.
func TestConsiderIsContextual(t *testing.T) {
	prog := parseSrc(t, `Cursed Energy: stdin
Simple Domain: Repeat 1
    Consider Everything Twice
`)
	body := prog.Statements[1]
	if len(body.Binds) != 0 {
		t.Fatalf("a three-word phrase was read as a binding")
	}
	if len(body.Block) != 1 || body.Block[0].Op.Raw != "Consider Everything Twice" {
		t.Fatalf("phrase did not parse as a statement: %+v", body.Block)
	}
}

// Case-insensitive, like every themed keyword.
func TestBindingKeywordsCaseInsensitive(t *testing.T) {
	prog := parseSrc(t, `Cursed Energy: stdin
Cursed Technique: Map Each
    consider n as 3
    Using: (x) -> x + n
`)
	if len(prog.Statements[1].Binds) != 1 {
		t.Fatalf("lowercase `consider … as …` did not parse as a binding")
	}
}

func TestBindingParseErrors(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{
			name: "at the top level",
			src: `Cursed Energy: stdin
Consider n As 3
`,
			want: "belongs to a statement",
		},
		{
			name: "an expression keyword as the name",
			src: `Cursed Energy: stdin
Cursed Technique: Map Each
    Consider in As 3
    Using: (x) -> x
`,
			want: "expression keyword",
		},
		{
			name: "Of with nothing to run",
			src: `Cursed Energy: stdin
Cursed Technique: Map Each
    Consider n Of
    Using: (x) -> x
`,
			want: "an operation or an indented sub-pipeline",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := parseErr(t, c.src)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}
