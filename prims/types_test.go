package prims

import (
	"strings"
	"testing"

	"domain/ir"
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
		{"Graph<Text>", "Graph<Text>"},
		{"Graph<(Int, Int)>", "Graph<(Int, Int)>"},
		{"List<Graph<Int>>", "List<Graph<Int>>"},
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
		{"unkeyable graph node", "Graph<Float>", "Graph nodes must be keyable"},
		{"graph node is a list", "Graph<List<Int>>", "Graph nodes must be keyable"},
		{"graph arity", "Graph<Int, Int>", "takes 1 type argument(s), got 2"},
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
		// Records render the way ir.Type.String() prints them, so a written
		// type and the type a message compares it against read identically.
		"{a:Int}", "{a:Int, b:Text}", "List<{a:Int}>", "({a:Int}) -> Bool",
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

// Record types are writable in a signature. They used to be absent because the
// lexer had no brace tokens; the record-literal syntax brought them, and the
// written form is the one ir.Type.String() has always printed — so a declared
// type and the type it is compared against now read identically.
func TestRecordTypeGrammar(t *testing.T) {
	for _, c := range []struct{ written, want string }{
		{"{a: Int}", "{a:Int}"},
		{"{a: Int, b: Text}", "{a:Int, b:Text}"},
		{"List<{a: Int, b: Text}>", "List<{a:Int, b:Text}>"},
		{"{a: List<Int>}", "{a:List<Int>}"},
		{"{outer: {inner: Int}}", "{outer:{inner:Int}}"},
		{"Map<Text, {a: Int}>", "Map<Text, {a:Int}>"},
		{"{p: (Int, Int)}", "{p:(Int, Int)}"},
	} {
		got, err := parseType(t, c.written)
		if err != nil {
			t.Errorf("%s: %v", c.written, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s lowered to %s, want %s", c.written, got, c.want)
		}
	}
}

// lowerWritten is parseType returning the lowered type rather than its string.
func lowerWritten(t *testing.T, written string) *ir.Type {
	t.Helper()
	src := "Shikigami \"T\" : " + written + " -> Int\n    Maximum Technique: Sum\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex %q: %v", written, err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse %q: %v", written, err)
	}
	ty, err := lowerTypeExpr(prog.Shikigamis[0].Sig.In, token.Position{Line: 1, Col: 1})
	if err != nil {
		t.Fatalf("lower %q: %v", written, err)
	}
	return ty
}

// ir.Type.Equal compares records by field set, so the order a signature writes
// its fields in cannot make a matching pipeline fail to match. This is the kind
// of thing a reader would not assume, so it is pinned rather than left implied.
func TestRecordTypeFieldOrderDoesNotMatter(t *testing.T) {
	a := lowerWritten(t, "{a: Int, b: Text}")
	b := lowerWritten(t, "{b: Text, a: Int}")

	// They render in the order they were written...
	if a.String() == b.String() {
		t.Fatalf("both orders rendered as %s; the test cannot tell them apart", a)
	}
	// ...and still describe the same type.
	if !a.Equal(b) {
		t.Errorf("%s and %s compared unequal; field order is not part of a record's identity", a, b)
	}
	// A genuinely different field set must still differ.
	if a.Equal(lowerWritten(t, "{a: Int, b: Int}")) {
		t.Error("records differing in a field's type compared equal")
	}
	if a.Equal(lowerWritten(t, "{a: Int}")) {
		t.Error("records differing in field count compared equal")
	}
}

// Keyability is enforced on the way in for Set and Map, and a record of
// keyable fields is itself keyable — so a record may be a Set element.
func TestRecordTypeKeyability(t *testing.T) {
	if _, err := parseType(t, "Set<{a: Int, b: Text}>"); err != nil {
		t.Errorf("a record of keyable fields should be a valid Set element: %v", err)
	}
	if _, err := parseType(t, "Set<{a: List<Int>}>"); err == nil {
		t.Error("a record holding a List is not keyable and should have been refused")
	}
}

func TestRecordTypeErrors(t *testing.T) {
	for _, c := range []struct{ written, want string }{
		{"{}", "an empty record type has no fields"},
		{"{a: Int, a: Text}", `duplicate field "a"`},
		{"{a Int}", "needs a colon before its type"},
		{"{1: Int}", "field needs a name"},
		{"{a: Int,}", "no trailing comma"},
		{"{a: Int", "expected , or }"},
	} {
		_, err := parseType(t, c.written)
		if err == nil {
			t.Errorf("%s: expected an error", c.written)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not contain %q", c.written, err, c.want)
		}
	}
}
