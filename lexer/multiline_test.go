package lexer

import (
	"strings"
	"testing"

	"domain/token"
)

// Inside a parenthesis the source is one logical line: no NEWLINE, and the next
// line's leading spaces are alignment rather than indentation.
func TestNewlineInsideParensIsNotLayout(t *testing.T) {
	src := "Cursed Technique: Map Each\n" +
		"    Using: (x) -> min(list(\n" +
		"        x,\n" +
		"        0 - x\n" +
		"    ))\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	// One NEWLINE for the statement, one for the whole argument: the three
	// physical line breaks inside `min(list(` are not line ends.
	var newlines, indents, dedents int
	for _, tk := range toks {
		switch tk.Kind {
		case token.NEWLINE:
			newlines++
		case token.INDENT:
			indents++
		case token.DEDENT:
			dedents++
		}
	}
	if newlines != 2 {
		t.Errorf("NEWLINE count = %d, want 2 (the statement and the argument)", newlines)
	}
	if indents != 1 || dedents != 1 {
		t.Errorf("INDENT/DEDENT = %d/%d, want 1/1: only the argument block is layout", indents, dedents)
	}
}

// Positions keep tracking real lines and columns across a joined break, so a
// diagnostic still points at the source the reader is looking at.
func TestPositionsSurviveAJoinedLine(t *testing.T) {
	toks, err := Lex("Using: (x) -> min(\n    x,\n    2)\n")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tk := range toks {
		if tk.Kind == token.INT && tk.Literal == "2" {
			found = true
			if tk.Pos.Line != 3 || tk.Pos.Col != 5 {
				t.Errorf("`2` at %d:%d, want 3:5", tk.Pos.Line, tk.Pos.Col)
			}
		}
	}
	if !found {
		t.Fatal("no INT token for `2`")
	}
}

// A tab is only rejected where it would be indentation. Inside a joined line
// there is no indentation to get wrong.
func TestTabIsAllowedInsideParens(t *testing.T) {
	if _, err := Lex("Using: (x) -> min(\n\tx,\n\t2)\n"); err != nil {
		t.Errorf("a tab aligning a continuation line should lex: %v", err)
	}
}

// An unclosed parenthesis swallows the rest of the file, so it is reported
// where it was opened rather than wherever the confusion surfaces.
func TestUnclosedParenReportsItsOpeningPosition(t *testing.T) {
	_, err := Lex("Cursed Energy: x\nCursed Technique: Map Each\n    Using: (x) -> min(x\nReveal: stdout\n")
	if err == nil {
		t.Fatal("an unclosed '(' should be an error")
	}
	var le *Error
	if !asLexError(err, &le) {
		t.Fatalf("error is %T, want *lexer.Error", err)
	}
	if le.Pos.Line != 3 || le.Pos.Col != 22 {
		t.Errorf("reported at %d:%d, want 3:22 (the `(` of `min(`)", le.Pos.Line, le.Pos.Col)
	}
	if !le.Incomplete {
		t.Error("an unclosed '(' is incomplete input, so the REPL can wait for the rest")
	}
	if !strings.Contains(le.Msg, "unclosed") {
		t.Errorf("message = %q, want it to say the parenthesis is unclosed", le.Msg)
	}
}

// A ')' with nothing to close is the parser's error to describe, not the
// lexer's: it must not leave the paren stack negative or swallow later lines.
func TestStrayCloseParenClosesNothing(t *testing.T) {
	toks, err := Lex("Cursed Energy: x)\nReveal: stdout\n")
	if err != nil {
		t.Fatalf("a stray ')' is not a lexical error: %v", err)
	}
	var newlines int
	for _, tk := range toks {
		if tk.Kind == token.NEWLINE {
			newlines++
		}
	}
	if newlines != 2 {
		t.Errorf("NEWLINE count = %d, want 2: the stray ')' must not join the lines", newlines)
	}
}

func asLexError(err error, out **Error) bool {
	le, ok := err.(*Error)
	if ok {
		*out = le
	}
	return ok
}
