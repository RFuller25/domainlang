package lexer

import (
	"strings"
	"testing"

	"domain/token"
)

// rawOf returns the single RAW token in a source, failing if there is not
// exactly one.
func rawOf(t *testing.T, src string) token.Token {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	var found []token.Token
	for _, tk := range toks {
		if tk.Kind == token.RAW {
			found = append(found, tk)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d RAW tokens, want 1, in:\n%s", len(found), src)
	}
	return found[0]
}

func TestForeignBlockCapturesVerbatim(t *testing.T) {
	// Every character below is one the Domain lexer would otherwise refuse or
	// reinterpret: braces, an `@`, a `!`, a bang-comment, an unterminated
	// quote inside a comment, and a tab.
	src := "Domain Expansion: Python\n" +
		"    import sys  # not a Domain comment: it's got a \" in it\n" +
		"    xs = {c: [i] for i, c in enumerate(sys.stdin)}\n" +
		"    print(len(xs) @ 1 != 2)\n" +
		"Reveal: stdout\n"
	got := rawOf(t, src).Literal
	want := "import sys  # not a Domain comment: it's got a \" in it\n" +
		"xs = {c: [i] for i, c in enumerate(sys.stdin)}\n" +
		"print(len(xs) @ 1 != 2)\n"
	if got != want {
		t.Errorf("raw block:\ngot  %q\nwant %q", got, want)
	}
}

func TestForeignBlockKeepsInteriorLayout(t *testing.T) {
	src := "Domain Expansion: Python\n" +
		"    def f(x):\n" +
		"        if x:\n" +
		"\n" +
		"            return 1\n" +
		"    print(f(2))\n"
	got := rawOf(t, src).Literal
	want := "def f(x):\n    if x:\n\n        return 1\nprint(f(2))\n"
	if got != want {
		t.Errorf("interior layout lost:\ngot  %q\nwant %q", got, want)
	}
}

// Tabs are a lex error in Domain's own indentation, and routine in Go's.
func TestForeignBlockAllowsTabs(t *testing.T) {
	src := "Domain Expansion: Go\n\tpackage main\n\n\tfunc main() {\n\t\tprintln(1)\n\t}\n"
	got := rawOf(t, src).Literal
	want := "package main\n\nfunc main() {\n\tprintln(1)\n}\n"
	if got != want {
		t.Errorf("tab-indented block:\ngot  %q\nwant %q", got, want)
	}
}

func TestForeignBlockEveryLanguage(t *testing.T) {
	for _, lang := range []string{"Python", "Go", "rask", "cRust", "python", "CRUST"} {
		src := "Domain Expansion: " + lang + "\n    x\n"
		if got := rawOf(t, src).Literal; got != "x\n" {
			t.Errorf("%s: got %q", lang, got)
		}
	}
}

// The keyword is optional everywhere else in Domain, so a bare language name
// opens a block too.
func TestForeignBlockBarePhrase(t *testing.T) {
	if got := rawOf(t, "Python\n    print(1)\n").Literal; got != "print(1)\n" {
		t.Errorf("bare opener: got %q", got)
	}
}

func TestForeignBlockNested(t *testing.T) {
	src := "Part \"1\":\n" +
		"    Domain Expansion: Python\n" +
		"        print(1)\n" +
		"    Reveal: stdout\n"
	if got := rawOf(t, src).Literal; got != "print(1)\n" {
		t.Errorf("nested opener: got %q", got)
	}
	// The block pushes no indentation of its own, so `Reveal:` closes the Part
	// exactly as it would have without it.
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var layout []token.Kind
	for _, tk := range toks {
		if tk.Kind == token.INDENT || tk.Kind == token.DEDENT {
			layout = append(layout, tk.Kind)
		}
	}
	want := []token.Kind{token.INDENT, token.DEDENT}
	if len(layout) != len(want) || layout[0] != want[0] || layout[1] != want[1] {
		t.Errorf("layout tokens: got %v, want %v", layout, want)
	}
}

// A blank line between two blocks belongs to neither: it is ordinary layout, so
// the formatter still owns it.
func TestForeignBlockTrailingBlankLinesExcluded(t *testing.T) {
	src := "Domain Expansion: Python\n    print(1)\n\n\nReveal: stdout\n"
	raw := rawOf(t, src)
	if raw.Literal != "print(1)\n" {
		t.Errorf("got %q", raw.Literal)
	}
	if end := strings.Index(src, "\n\n\n") + 1; raw.End != end {
		t.Errorf("block span ends at %d, want %d (trailing blanks excluded)", raw.End, end)
	}
}

// A blank line *inside* the block is part of it, because content follows.
func TestForeignBlockInteriorBlankLines(t *testing.T) {
	src := "Domain Expansion: Python\n\n    a\n\n    b\nReveal: stdout\n"
	if got := rawOf(t, src).Literal; got != "\na\n\nb\n" {
		t.Errorf("got %q", got)
	}
}

func TestForeignBlockPositionsSurvive(t *testing.T) {
	src := "Domain Expansion: Python\n    print(1)\n    print(2)\nReveal: stdout\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range toks {
		if tk.Kind == token.IDENT && tk.Literal == "Reveal" {
			if tk.Pos.Line != 4 || tk.Pos.Col != 1 {
				t.Fatalf("Reveal at %s, want 4:1", tk.Pos)
			}
			return
		}
	}
	t.Fatal("no Reveal token")
}

func TestForeignBlockRawStartsAtBlock(t *testing.T) {
	raw := rawOf(t, "Domain Expansion: Python\n    print(1)\n")
	if raw.Pos.Line != 2 || raw.Pos.Col != 1 {
		t.Errorf("RAW at %s, want 2:1", raw.Pos)
	}
}

// A block only opens when something is actually indented beneath the opener.
// Without that, the line is the ordinary statement it has always been.
func TestNoForeignBlockWithoutIndent(t *testing.T) {
	for _, src := range []string{
		"Domain Expansion: Python\n",
		"Domain Expansion: Python\nReveal: stdout\n",
		"Python\n",
		"Domain Expansion: Python", // no trailing newline
		"Domain Expansion: Python\n\n\n",
	} {
		toks, err := Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", src, err)
		}
		for _, tk := range toks {
			if tk.Kind == token.RAW {
				t.Errorf("Lex(%q) opened a block with nothing indented beneath it", src)
			}
		}
	}
}

// Only a themed keyword introduces a language name. An argument whose value
// happens to be one is an ordinary argument, and its indented block is an
// ordinary Domain sub-pipeline.
func TestNoForeignBlockForArgumentValue(t *testing.T) {
	for _, src := range []string{
		"Cursed Technique: Map Each\n    Using: Python\n        Maximum Technique: Sum\n",
		"Cursed Technique: Map Each\n    Mode: Go\n        Maximum Technique: Sum\n",
		"Not A Keyword: Python\n    Maximum Technique: Sum\n",
		"Domain Expansion: Quicksort\n    Maximum Technique: Sum\n",
		"Domain Expansion: Python Sort\n    Maximum Technique: Sum\n",
		"Domain Expansion: Python, Descending\n    Maximum Technique: Sum\n",
	} {
		toks, err := Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", src, err)
		}
		for _, tk := range toks {
			if tk.Kind == token.RAW {
				t.Errorf("Lex(%q) captured a foreign block; it names no language", src)
			}
		}
	}
}

func TestForeignBlockAtEOF(t *testing.T) {
	// No trailing newline on the last line of the block.
	if got := rawOf(t, "Domain Expansion: Python\n    print(1)").Literal; got != "print(1)\n" {
		t.Errorf("got %q", got)
	}
}

func TestForeignBlockCarriageReturns(t *testing.T) {
	if got := rawOf(t, "Domain Expansion: Python\r\n    print(1)\r\n").Literal; got != "print(1)\n" {
		t.Errorf("got %q", got)
	}
}

func TestTwoForeignBlocks(t *testing.T) {
	src := "Domain Expansion: Python\n    print(1)\n" +
		"Domain Expansion: rask\n    lines | map(+1)\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		if tk.Kind == token.RAW {
			got = append(got, tk.Literal)
		}
	}
	want := []string{"print(1)\n", "lines | map(+1)\n"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestIndentWidthOf(t *testing.T) {
	cases := map[string]int{
		"":          0,
		"x":         0,
		"    x":     4,
		"\tx":       8,
		"  \tx":     8, // a tab advances to the next multiple of 8
		"\t\tx":     16,
		"        x": 8,
	}
	for line, want := range cases {
		if got := indentWidthOf(line); got != want {
			t.Errorf("indentWidthOf(%q) = %d, want %d", line, got, want)
		}
	}
}
