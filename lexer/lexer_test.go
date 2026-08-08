package lexer

import (
	"strings"
	"testing"

	"domain/token"
)

func kinds(toks []token.Token) []token.Kind {
	out := make([]token.Kind, len(toks))
	for i, t := range toks {
		out[i] = t.Kind
	}
	return out
}

func TestBasicLine(t *testing.T) {
	toks, err := Lex("Cursed Energy: input.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []token.Kind{
		token.IDENT, token.IDENT, token.COLON,
		token.IDENT, token.DOT, token.IDENT,
		token.NEWLINE, token.EOF,
	}
	got := kinds(toks)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

func TestStringEscapes(t *testing.T) {
	toks, err := Lex(`Split by "\n\n"` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	var str string
	found := false
	for _, tk := range toks {
		if tk.Kind == token.STRING {
			str = tk.Literal
			found = true
		}
	}
	if !found {
		t.Fatal("no string token produced")
	}
	if str != "\n\n" {
		t.Fatalf("escape not interpreted: got %q", str)
	}
}

func TestIndentation(t *testing.T) {
	src := "A:\n    B: x\n    C: y\nD: z\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var indents, dedents int
	for _, tk := range toks {
		switch tk.Kind {
		case token.INDENT:
			indents++
		case token.DEDENT:
			dedents++
		}
	}
	if indents != 1 || dedents != 1 {
		t.Fatalf("expected 1 indent / 1 dedent, got %d / %d", indents, dedents)
	}
}

func TestCommentsAndBlankLinesSkipped(t *testing.T) {
	src := "# a comment\n\nReveal: stdout\n# trailing\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	// First meaningful token must be the IDENT "Reveal".
	if toks[0].Kind != token.IDENT || toks[0].Literal != "Reveal" {
		t.Fatalf("expected first token Reveal, got %s", toks[0])
	}
}

func TestTabIndentationRejected(t *testing.T) {
	_, err := Lex("A:\n\tB: x\n")
	if err == nil {
		t.Fatal("expected error for tab indentation")
	}
}

func TestOperators(t *testing.T) {
	toks, err := Lex("(a, b) -> a + b = 2020\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []token.Kind{
		token.LPAREN, token.IDENT, token.COMMA, token.IDENT, token.RPAREN,
		token.ARROW, token.IDENT, token.PLUS, token.IDENT, token.EQ, token.INT,
		token.NEWLINE, token.EOF,
	}
	got := kinds(toks)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: got %s want %s", i, got[i], want[i])
		}
	}
}

// TestAssignToken pins `:=` as one token, and pins the adjacency rule that
// keeps it from swallowing a statement's colon: `Mode: First` is a COLON, and
// only the two characters written together are an ASSIGN.
func TestAssignToken(t *testing.T) {
	cases := []struct {
		src  string
		want []token.Kind
	}{
		{"n := 1\n", []token.Kind{token.IDENT, token.ASSIGN, token.INT, token.NEWLINE, token.EOF}},
		{"Mode: First\n", []token.Kind{token.IDENT, token.COLON, token.IDENT, token.NEWLINE, token.EOF}},
		// A colon followed by `=` with a space between is still two tokens.
		{"n : = 1\n", []token.Kind{token.IDENT, token.COLON, token.EQ, token.INT, token.NEWLINE, token.EOF}},
		// `=` alone stays equality: there is exactly one assignment spelling.
		{"a = b\n", []token.Kind{token.IDENT, token.EQ, token.IDENT, token.NEWLINE, token.EOF}},
	}
	for _, c := range cases {
		toks, err := Lex(c.src)
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		got := kinds(toks)
		if len(got) != len(c.want) {
			t.Fatalf("%q: got %v, want %v", c.src, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Fatalf("%q: token %d: got %s want %s", c.src, i, got[i], c.want[i])
			}
		}
	}
}

// TestColumnTrackingWithMultiByteRunes is a regression test: advance() used
// to increment the column once per byte, not once per rune, so any line
// containing a multi-byte UTF-8 character (string literals are documented
// and tested elsewhere to carry arbitrary unicode content) drifted the
// column of every token that followed it on the same line.
func TestColumnTrackingWithMultiByteRunes(t *testing.T) {
	// "café" — 'é' is a 2-byte UTF-8 sequence. Byte-based counting would
	// place the following IDENT at column 17; rune-based counting places it
	// at column 16 (one column per character: `Reveal: "café" ` is 16 runes).
	toks, err := Lex(`Reveal: "café" test` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	var identTok token.Token
	found := false
	for _, tk := range toks {
		if tk.Kind == token.IDENT && tk.Literal == "test" {
			identTok = tk
			found = true
		}
	}
	if !found {
		t.Fatal("expected an IDENT token \"test\"")
	}
	if identTok.Pos.Col != 16 {
		t.Fatalf("column after a multi-byte rune: got %d want 16", identTok.Pos.Col)
	}
}

func TestColumnTrackingPlainASCII(t *testing.T) {
	toks, err := Lex("Reveal: stdout\n")
	if err != nil {
		t.Fatal(err)
	}
	// "Reveal" (6) + " " + ":" + " " + "stdout" -> stdout starts at column 9.
	for _, tk := range toks {
		if tk.Kind == token.IDENT && tk.Literal == "stdout" {
			if tk.Pos.Col != 9 {
				t.Fatalf("column: got %d want 9", tk.Pos.Col)
			}
			return
		}
	}
	t.Fatal("expected an IDENT token \"stdout\"")
}

// TestUnexpectedCharacterMessageIsAccurate is a regression test: the
// "unexpected character" diagnostic used to convert the raw byte to a string
// via `string(c)` where c is a byte — for non-ASCII bytes (128-255) that
// performs a *rune* conversion of the byte's numeric value, producing a
// completely different character in the message than what was actually in
// the source.
func TestUnexpectedCharacterMessageIsAccurate(t *testing.T) {
	// U+00A9 COPYRIGHT SIGN (©), encoded as UTF-8 (0xC2 0xA9), used where an
	// operator is expected — not a valid identifier-start or operator byte.
	_, err := Lex("Reveal: © stdout\n")
	if err == nil {
		t.Fatal("expected a lex error for an unsupported character")
	}
	if !strings.Contains(err.Error(), "©") {
		t.Fatalf("error message should mention the actual character '©', got: %v", err)
	}
}

func TestDeepNestedIndentation(t *testing.T) {
	src := "A:\n    B:\n        C:\n            D: x\nE: y\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var indents, dedents int
	for _, tk := range toks {
		switch tk.Kind {
		case token.INDENT:
			indents++
		case token.DEDENT:
			dedents++
		}
	}
	if indents != 3 || dedents != 3 {
		t.Fatalf("expected 3 indents / 3 dedents, got %d / %d", indents, dedents)
	}
}

func TestInconsistentDedentIsAnError(t *testing.T) {
	// Dedents to a column (2) that doesn't match any enclosing indent level
	// (0 or 4).
	src := "A:\n    B:\n        C: x\n  D: y\n"
	if _, err := Lex(src); err == nil {
		t.Fatal("expected an error for a dedent that matches no enclosing block")
	}
}

func TestTrailingWhitespaceLine(t *testing.T) {
	src := "Reveal: stdout   \n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	// Trailing spaces before the newline should not produce spurious tokens.
	for _, tk := range toks {
		if tk.Kind != token.IDENT && tk.Kind != token.COLON &&
			tk.Kind != token.NEWLINE && tk.Kind != token.EOF {
			t.Fatalf("unexpected token kind %s from trailing whitespace", tk.Kind)
		}
	}
}

func TestTrailingWhitespaceOnlyFinalLineNoNewline(t *testing.T) {
	// A content line followed by a final line that is only whitespace, with
	// no trailing newline at all, must not produce a spurious extra NEWLINE:
	// the whitespace-only line was never anything but blank.
	src := "a\n   "
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var newlines int
	for _, tk := range toks {
		if tk.Kind == token.NEWLINE {
			newlines++
		}
	}
	if newlines != 1 {
		t.Fatalf("expected exactly 1 NEWLINE, got %d (toks: %v)", newlines, toks)
	}
	if toks[len(toks)-1].Kind != token.EOF {
		t.Fatalf("expected final token to be EOF, got %s", toks[len(toks)-1].Kind)
	}
}

func TestCRLFLineEndings(t *testing.T) {
	src := "Reveal: stdout\r\nCursed Energy: x\r\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var newlines int
	for _, tk := range toks {
		if tk.Kind == token.NEWLINE {
			newlines++
		}
	}
	if newlines != 2 {
		t.Fatalf("expected 2 NEWLINE tokens with CRLF input, got %d", newlines)
	}
}

func TestCommentAfterCodeOnSameLine(t *testing.T) {
	toks, err := Lex("Reveal: stdout # trailing comment\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range toks {
		if tk.Kind == token.IDENT && strings.Contains(tk.Literal, "#") {
			t.Fatalf("comment leaked into a token: %v", tk)
		}
	}
	// The literal "trailing"/"comment" words must not appear as tokens.
	for _, tk := range toks {
		if tk.Literal == "trailing" || tk.Literal == "comment" {
			t.Fatal("comment content was tokenized instead of skipped")
		}
	}
}

func TestStringEscapeErrors(t *testing.T) {
	cases := []struct{ src, wantSubstr string }{
		{`"unterminated` + "\n", "unterminated string literal"},
		{"\"newline\nin string\"\n", "unterminated string literal"},
		// A backslash as the absolute last byte of the source (nothing
		// follows, not even a newline) is "unterminated escape".
		{`"trailing backslash \`, "unterminated escape"},
		{`"bad escape \q"` + "\n", "unknown escape sequence"},
	}
	for _, c := range cases {
		_, err := Lex(c.src)
		if err == nil {
			t.Fatalf("%q: expected an error", c.src)
		}
		if !strings.Contains(err.Error(), c.wantSubstr) {
			t.Fatalf("%q: error %q does not contain %q", c.src, err.Error(), c.wantSubstr)
		}
	}
}

func TestAllStandardEscapesInterpreted(t *testing.T) {
	toks, err := Lex(`"\n\t\r\\\"\0"` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "\n\t\r\\\"\x00"
	for _, tk := range toks {
		if tk.Kind == token.STRING {
			if tk.Literal != want {
				t.Fatalf("got %q want %q", tk.Literal, want)
			}
			return
		}
	}
	t.Fatal("no string token produced")
}

func TestBlankAndCommentOnlyLinesDoNotAffectIndentation(t *testing.T) {
	src := "A:\n    B: x\n\n    # a comment, still inside the block\n    C: y\nD: z\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var indents, dedents int
	for _, tk := range toks {
		switch tk.Kind {
		case token.INDENT:
			indents++
		case token.DEDENT:
			dedents++
		}
	}
	if indents != 1 || dedents != 1 {
		t.Fatalf("blank/comment lines should not affect indentation: got %d indents, %d dedents", indents, dedents)
	}
}

func TestLexNeverPanicsOnArbitraryBytes(t *testing.T) {
	inputs := []string{
		"",
		"\x00\x01\x02",
		"\xff\xfe",
		string([]byte{0xC3}), // truncated multi-byte lead
		"Reveal:\n" + string(rune(0)),
		strings8x("A:\n", 200), // deeply nested-looking but flat garbage
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Lex panicked on %q: %v", in, r)
				}
			}()
			_, _ = Lex(in)
		}()
	}
}

func strings8x(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestLexFloatLiterals(t *testing.T) {
	toks, err := Lex("Using: (x) -> x * 1.5 + 0.25\n")
	if err != nil {
		t.Fatal(err)
	}
	var floats []string
	for _, tok := range toks {
		if tok.Kind == token.FLOAT {
			floats = append(floats, tok.Literal)
		}
	}
	if len(floats) != 2 || floats[0] != "1.5" || floats[1] != "0.25" {
		t.Fatalf("float literals: got %v, want [1.5 0.25]", floats)
	}
}

// A digit-led dotted target must keep lexing as INT DOT IDENT so file names
// like `2.txt` stay valid Cursed Energy targets.
func TestLexDottedFileTargetIsNotAFloat(t *testing.T) {
	toks, err := Lex("Cursed Energy: 2.txt\n")
	if err != nil {
		t.Fatal(err)
	}
	kinds := []token.Kind{}
	for _, tok := range toks {
		kinds = append(kinds, tok.Kind)
	}
	want := []token.Kind{token.IDENT, token.IDENT, token.COLON, token.INT, token.DOT, token.IDENT, token.NEWLINE, token.EOF}
	if len(kinds) != len(want) {
		t.Fatalf("kinds: got %v want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kind %d: got %s want %s", i, kinds[i], want[i])
		}
	}
}
