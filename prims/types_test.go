package prims

import (
	"strings"
	"testing"

	"domain/lexer"
	"domain/parser"
	"domain/token"
)

// parseType parses a written type by borrowing a Shikigami signature, which is
// the only place the type grammar appears in source.
func parseType(t *testing.T, written string) (string, error) {
	t.Helper()
	src := "Shikigami \"T\" : " + written + " -> Int\n    Maximum Technique: Sum\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		return "", err
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return "", err
	}
	if len(prog.Shikigamis) != 1 || prog.Shikigamis[0].Sig == nil {
		t.Fatalf("no signature parsed from %q", written)
	}
	ty, err := lowerTypeExpr(prog.Shikigamis[0].Sig.In, token.Position{Line: 1, Col: 1})
	if err != nil {
		return "", err
	}
	return ty.String(), nil
}

func TestTypeGrammar(t *testing.T) {
	cases := []struct{ written, want string }{
		{"Int", "Int"},
		{"Float", "Float"},
		{"Text", "Text"},
		{"Bool", "Bool"},
		{"List<Int>", "List<Int>"},
		{"List<List<Text>>", "List<List<Text>>"},
		{"Set<Text>", "Set<Text>"},
		{"Grid<Int>", "Grid<Int>"},
		{"Sparse<Int>", "Sparse<Int>"},
		{"Map<Text, Int>", "Map<Text, Int>"},
		{"Map<(Int, Int), Int>", "Map<(Int, Int), Int>"},
		{"(Int, Int)", "(Int, Int)"},
		{"(Int, Text, Bool)", "(Int, Text, Bool)"},
		{"List<(Int, Int)>", "List<(Int, Int)>"},
		{"(Int)", "Int"}, // a single parenthesized type is grouping
		{"Grid<List<Int>>", "Grid<List<Int>>"},
	}
	for _, c := range cases {
		t.Run(c.written, func(t *testing.T) {
			got, err := parseType(t, c.written)
			if err != nil {
				t.Fatalf("parse/lower %q: %v", c.written, err)
			}
			if got != c.want {
				t.Errorf("%q lowered to %s, want %s", c.written, got, c.want)
			}
		})
	}
}

func TestTypeGrammarErrors(t *testing.T) {
	cases := []struct{ name, written, want string }{
		{"unknown name", "Nope", `unknown type "Nope"`},
		{"scalar with arguments", "Int<Text>", "takes no type arguments"},
		{"wrong generic arity", "Map<Int>", "takes 2 type argument(s), got 1"},
		{"list arity", "List<Int, Int>", "takes 1 type argument(s), got 2"},
		{"unkeyable map key", "Map<Float, Int>", "Map keys must be keyable"},
		{"unkeyable set element", "Set<Float>", "Set elements must be keyable"},
		{"unkeyable nested key", "Map<List<Int>, Int>", "Map keys must be keyable"},
		{"lambda as a pipeline type", "((Int) -> Bool)", "only valid as a Shikigami parameter"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseType(t, c.written)
			if err == nil {
				t.Fatalf("expected an error for %q", c.written)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.want)
			}
		})
	}
}

// In a signature the top-level arrow separates input from output, so a
// parenthesized list on the left is a tuple, never a lambda type.
func TestSignatureArrowBindsToTheSignature(t *testing.T) {
	src := "Shikigami \"T\" : (Int, Int) -> Int\n    Maximum Technique: Sum\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	sig := prog.Shikigamis[0].Sig
	if sig == nil {
		t.Fatal("no signature")
	}
	if sig.In.Lambda != nil {
		t.Fatal("the left side should be a tuple, not a lambda type")
	}
	in, err := lowerTypeExpr(sig.In, token.Position{})
	if err != nil {
		t.Fatal(err)
	}
	if in.String() != "(Int, Int)" {
		t.Errorf("input = %s, want (Int, Int)", in)
	}
	out, err := lowerTypeExpr(sig.Out, token.Position{})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "Int" {
		t.Errorf("output = %s, want Int", out)
	}
}

func TestSignatureNeedsBothSides(t *testing.T) {
	src := "Shikigami \"T\" : Int\n    Maximum Technique: Sum\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(src, toks); err == nil {
		t.Fatal("expected a parse error")
	} else if !strings.Contains(err.Error(), "needs both sides") {
		t.Errorf("error = %q, want it to ask for both sides", err.Error())
	}
}

func TestTypeStringRoundTrip(t *testing.T) {
	cases := []string{
		"Int", "List<Int>", "Map<Text, Int>", "(Int, Int)", "List<(Int, Int)>",
		"(Int) -> Bool", "(Int, Int) -> Int",
	}
	for _, written := range cases {
		src := "Shikigami \"T\" (p: " + written + ")\n    Maximum Technique: Sum\n"
		toks, err := lexer.Lex(src)
		if err != nil {
			t.Fatalf("%s: %v", written, err)
		}
		prog, err := parser.Parse(src, toks)
		if err != nil {
			t.Fatalf("%s: %v", written, err)
		}
		if got := TypeString(prog.Shikigamis[0].Params[0].Type); got != written {
			t.Errorf("TypeString = %q, want %q", got, written)
		}
	}
}
