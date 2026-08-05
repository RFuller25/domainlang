package parser

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/lexer"
)

func TestParseForeignBlock(t *testing.T) {
	src := "Cursed Energy: input.txt\n" +
		"Domain Expansion: Python\n" +
		"    import sys\n" +
		"    print(sum(int(x) for x in sys.stdin))\n" +
		"Reveal: stdout\n"
	prog := parseSrc(t, src)
	if len(prog.Statements) != 3 {
		t.Fatalf("got %d statements, want 3", len(prog.Statements))
	}
	stmt := prog.Statements[1]
	if stmt.Foreign == nil {
		t.Fatal("statement carries no foreign block")
	}
	if stmt.Foreign.Language != "Python" {
		t.Errorf("language %q, want Python", stmt.Foreign.Language)
	}
	want := "import sys\nprint(sum(int(x) for x in sys.stdin))\n"
	if stmt.Foreign.Source != want {
		t.Errorf("source:\ngot  %q\nwant %q", stmt.Foreign.Source, want)
	}
	if stmt.Foreign.Pos.Line != 3 {
		t.Errorf("block position %s, want line 3", stmt.Foreign.Pos)
	}
	// The block is not an indented Domain block, so neither of the forms it
	// replaces may have picked anything up.
	if len(stmt.Block) > 0 || len(stmt.Args) > 0 {
		t.Errorf("foreign statement also parsed %d block statements and %d args",
			len(stmt.Block), len(stmt.Args))
	}
	if stmt.Keyword != "Domain Expansion" {
		t.Errorf("keyword %q", stmt.Keyword)
	}
	// The statement after the block is an ordinary top-level statement.
	if prog.Statements[2].Keyword != "Reveal" {
		t.Errorf("statement after the block is %+v", prog.Statements[2])
	}
}

func TestParseForeignCanonicalizesLanguage(t *testing.T) {
	prog := parseSrc(t, "Domain Expansion: crust\n    deliver(1)\n")
	if got := prog.Statements[0].Foreign.Language; got != "cRust" {
		t.Errorf("language %q, want the canonical cRust", got)
	}
}

func TestParseForeignBareKeyword(t *testing.T) {
	prog := parseSrc(t, "rask\n    lines | map(+1)\n")
	stmt := prog.Statements[0]
	if stmt.Foreign == nil || stmt.Foreign.Language != "rask" {
		t.Fatalf("bare opener did not carry a rask block: %+v", stmt)
	}
	if stmt.Keyword != "" || stmt.KeywordInferred {
		t.Errorf("bare opener should keep an empty keyword for prims.Infer, got %q", stmt.Keyword)
	}
}

func TestParseForeignNested(t *testing.T) {
	src := "Part \"1\":\n" +
		"    Domain Expansion: Python\n" +
		"        print(1)\n" +
		"    Reveal: stdout\n"
	prog := parseSrc(t, src)
	part := prog.Statements[0]
	if part.PartName != "1" || len(part.Block) != 2 {
		t.Fatalf("Part parsed as %+v with %d children", part, len(part.Block))
	}
	if part.Block[0].Foreign == nil {
		t.Error("nested foreign block not attached")
	}
	if part.Block[1].Keyword != "Reveal" {
		t.Errorf("statement after a nested block: %+v", part.Block[1])
	}
}

func TestParseForeignInShikigamiBody(t *testing.T) {
	src := "Shikigami \"Squish\"\n" +
		"    Domain Expansion: Python\n" +
		"        print(1)\n"
	prog := parseSrc(t, src)
	if len(prog.Shikigamis) != 1 || len(prog.Shikigamis[0].Body) != 1 {
		t.Fatalf("Shikigami body: %+v", prog.Shikigamis)
	}
	if prog.Shikigamis[0].Body[0].Foreign == nil {
		t.Error("foreign block in a Shikigami body not attached")
	}
}

// A language name with no block beneath it is a layout mistake, and one the
// REPL should wait out rather than drop.
func TestParseForeignMissingBlock(t *testing.T) {
	for _, src := range []string{
		"Domain Expansion: Python\n",
		"Domain Expansion: Python\nReveal: stdout\n",
		"Python\n",
	} {
		toks, err := lexer.Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", src, err)
		}
		_, err = Parse(src, toks)
		if err == nil {
			t.Errorf("Parse(%q) accepted an opener with no block", src)
			continue
		}
		var pe *Error
		if el, ok := err.(ErrorList); ok {
			pe = el[0]
		} else if e, ok := err.(*Error); ok {
			pe = e
		} else {
			t.Errorf("Parse(%q): unexpected error type %T", src, err)
			continue
		}
		if !pe.NeedsBlock {
			t.Errorf("Parse(%q): error is not flagged NeedsBlock: %v", src, pe)
		}
		if !strings.Contains(pe.Msg, "Python") {
			t.Errorf("Parse(%q): error does not name the language: %v", src, pe)
		}
	}
}

// Nothing about an ordinary program changes: a phrase that merely contains a
// language name is not an opener, and an argument value is never one.
func TestParseNonForeignUnaffected(t *testing.T) {
	for _, src := range []string{
		"Cursed Technique: Map Each\n    Using: (x) -> x + 1\n",
		"Domain Expansion: Quicksort, Descending\n",
		"Cursed Technique: Map Each\n    Using: Python\n",
	} {
		prog := parseSrc(t, src)
		var walk func([]*ast.Statement)
		walk = func(stmts []*ast.Statement) {
			for _, s := range stmts {
				if s.Foreign != nil {
					t.Errorf("Parse(%q) attached a foreign block to %+v", src, s)
				}
				walk(s.Block)
			}
		}
		walk(prog.Statements)
	}
}

// ---------------------------------------------------------------------------
// Declared signatures
// ---------------------------------------------------------------------------

func TestParseForeignSignature(t *testing.T) {
	prog := parseSrc(t, "Domain Expansion: Python : List<Int> -> Int\n    print(1)\n")
	fb := prog.Statements[0].Foreign
	if fb == nil || fb.Sig == nil {
		t.Fatalf("no signature parsed: %+v", fb)
	}
	if fb.Sig.In.Name != "List" || len(fb.Sig.In.Args) != 1 || fb.Sig.In.Args[0].Name != "Int" {
		t.Errorf("input type parsed as %+v", fb.Sig.In)
	}
	if fb.Sig.Out.Name != "Int" {
		t.Errorf("output type parsed as %+v", fb.Sig.Out)
	}
	// The phrase stays the language alone: the signature is types, and letting
	// it into the operation phrase would collect `<` and `>` as comparisons.
	if op := prog.Statements[0].Op; op.Raw != "Python" || len(op.OpSyms) > 0 {
		t.Errorf("signature leaked into the phrase: %+v", op)
	}
}

func TestParseForeignSignatureOnBareOpener(t *testing.T) {
	prog := parseSrc(t, "Python : Text -> List<Text>\n    print(1)\n")
	fb := prog.Statements[0].Foreign
	if fb == nil || fb.Sig == nil {
		t.Fatalf("no signature parsed: %+v", fb)
	}
	if fb.Sig.In.Name != "Text" || fb.Sig.Out.Name != "List" {
		t.Errorf("signature parsed as %+v -> %+v", fb.Sig.In, fb.Sig.Out)
	}
}

func TestParseForeignWithoutSignature(t *testing.T) {
	prog := parseSrc(t, "Domain Expansion: Python\n    print(1)\n")
	if fb := prog.Statements[0].Foreign; fb == nil || fb.Sig != nil {
		t.Errorf("expected a block with no signature, got %+v", fb)
	}
}

// A half-written signature is a parse error, not a silently ignored one.
func TestParseForeignBadSignature(t *testing.T) {
	for _, src := range []string{
		"Domain Expansion: Python : List<Int>\n    print(1)\n", // no arrow
		"Domain Expansion: Python :\n    print(1)\n",           // nothing at all
		"Domain Expansion: Python : -> Int\n    print(1)\n",    // no input side
	} {
		toks, err := lexer.Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", src, err)
		}
		if _, err := Parse(src, toks); err == nil {
			t.Errorf("Parse(%q) accepted a malformed signature", src)
		}
	}
}

// The block still opens when a signature is written, which is the lexer's half
// of the same rule: the opener line ends after the language name or turns into
// a signature, and nothing else.
func TestForeignSignatureStillOpensTheBlock(t *testing.T) {
	src := "Domain Expansion: Python : Text -> Int\n    xs = {1: 2}\n    print(xs)\n"
	prog := parseSrc(t, src)
	if got := prog.Statements[0].Foreign.Source; got != "xs = {1: 2}\nprint(xs)\n" {
		t.Errorf("block source %q", got)
	}
}

// A phrase that merely starts with a language name is an ordinary operation
// phrase, block or no block.
func TestForeignOpenerRequiresNothingElseOnTheLine(t *testing.T) {
	for _, src := range []string{
		"Domain Expansion: Python Sort\n    Maximum Technique: Sum\n",
		"Domain Expansion: Go Fast, Descending\n    Maximum Technique: Sum\n",
	} {
		prog := parseSrc(t, src)
		if prog.Statements[0].Foreign != nil {
			t.Errorf("Parse(%q) opened a foreign block", src)
		}
	}
}
