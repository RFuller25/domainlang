// Package lexer turns Domain source text into a flat token stream. It handles
// both syntactic layers (the themed pipeline surface and the plain expression
// surface) — they share one token stream and the parser decides context.
//
// Indentation is significant: the lexer emits INDENT / DEDENT tokens using the
// classic stack-of-widths approach. Indentation must be spaces; a tab in the
// leading whitespace of a line is an error (decision locked in v0.1).
//
// The one place layout is suspended is **inside parentheses**. An expression
// that has grown past a comfortable line has to break somewhere, and the only
// place a break can be unambiguous is where the reader can already see the
// expression is unfinished: an open `(`. So while a parenthesis is open a
// newline is not a line ending and the next line's leading spaces are not
// indentation — the classic implicit-continuation rule. The parser's other
// half of multi-line expressions (an indented block continuing an argument's
// value) is layout-aware and therefore lives in the parser, not here.
package lexer

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"domain/token"
)

// Error is a lexical error with a source position.
type Error struct {
	Pos token.Position
	Msg string
	// Incomplete marks the errors that mean "nothing is wrong yet, the source
	// just stops in the middle" — today, an open parenthesis at end of input.
	// A file has no more lines to offer and reports the error as it stands; the
	// REPL reads the flag and waits for them, the way it does for
	// parser.Error.NeedsBlock. It is a property of the error rather than of its
	// wording, so rephrasing the message cannot change what the REPL does.
	Incomplete bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

type lexer struct {
	src         string
	pos         int // byte offset
	line        int
	col         int
	indents     []int // stack of indentation widths, always starts with 0
	toks        []token.Token
	atLineStart bool
	// parens is the stack of open '(' positions. While it is non-empty the
	// lexer is inside an expression that cannot have ended, so newlines and
	// leading whitespace are not layout. The positions are kept (rather than a
	// bare counter) so an unclosed parenthesis can point at itself.
	parens []token.Position
	// lineTok is the index in toks where the current logical line's tokens
	// begin, so a completed line can be inspected as a whole — which is how a
	// foreign block opener is recognized (see foreign.go).
	lineTok int
}

// Lex tokenizes src into a slice ending in EOF, or returns the first error.
func Lex(src string) ([]token.Token, error) {
	l := &lexer{src: src, line: 1, col: 1, indents: []int{0}, atLineStart: true}
	return l.run()
}

func (l *lexer) errf(format string, args ...any) error {
	return &Error{Pos: l.curPos(), Msg: fmt.Sprintf(format, args...)}
}

func (l *lexer) curPos() token.Position {
	return token.Position{Line: l.line, Col: l.col, Offset: l.pos}
}

func (l *lexer) at() byte {
	if l.pos < len(l.src) {
		return l.src[l.pos]
	}
	return 0
}

func (l *lexer) advance() {
	if l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.line++
			l.col = 1
		case c&0xC0 == 0x80:
			// A UTF-8 continuation byte: the lead byte of this rune already
			// accounted for one column, so this byte (consumed one at a time
			// by the same byte-oriented call sites, e.g. lexString) must not
			// advance the column again or positions after it would drift.
		default:
			l.col++
		}
		l.pos++
	}
}

func (l *lexer) emit(k token.Kind, lit string, start token.Position) {
	l.toks = append(l.toks, token.Token{Kind: k, Literal: lit, Pos: start, End: l.pos})
}

func (l *lexer) run() ([]token.Token, error) {
	for l.pos < len(l.src) {
		if l.atLineStart {
			if err := l.lineStart(); err != nil {
				return nil, err
			}
			continue
		}
		c := l.at()
		switch {
		case c == '\n' && len(l.parens) > 0:
			// Implicit continuation: the expression is demonstrably unfinished,
			// so the break is whitespace. Leaving atLineStart false is the whole
			// mechanism — the next line's leading spaces fall through to the
			// whitespace case below instead of being measured as indentation.
			l.advance()
		case c == '\n':
			start := l.curPos()
			l.advance()
			l.emit(token.NEWLINE, "", start)
			l.atLineStart = true
			// The line is complete, so it can now be read as a whole: if it
			// opened a foreign block, everything indented beneath it is another
			// language's source and must be captured before the loop reaches it.
			l.rawBlock()
			l.lineTok = len(l.toks)
		case c == ' ' || c == '\t' || c == '\r':
			l.advance()
		case c == '#':
			for l.pos < len(l.src) && l.at() != '\n' {
				l.advance()
			}
		case isLetter(c):
			l.lexIdent()
		case isDigit(c):
			l.lexNumber()
		case c == '"':
			if err := l.lexString(); err != nil {
				return nil, err
			}
		default:
			if err := l.lexOperator(); err != nil {
				return nil, err
			}
		}
	}

	// An open parenthesis has swallowed every newline since it was written, so
	// the rest of the file is one logical line and whatever goes wrong next
	// would be reported somewhere it did not happen. Say where it started.
	if len(l.parens) > 0 {
		open := l.parens[len(l.parens)-1]
		return nil, &Error{Pos: open, Incomplete: true,
			Msg: "unclosed '(': the expression opened here never ends"}
	}

	// Emit a trailing NEWLINE if the final line had content but no newline.
	if !l.atLineStart {
		l.emit(token.NEWLINE, "", l.curPos())
	}
	// Close any open indentation blocks.
	for len(l.indents) > 1 {
		l.indents = l.indents[:len(l.indents)-1]
		l.emit(token.DEDENT, "", l.curPos())
	}
	l.emit(token.EOF, "", l.curPos())
	return l.toks, nil
}

// lineStart measures indentation at the beginning of a line and emits
// INDENT/DEDENT tokens. Blank and comment-only lines produce no layout tokens.
func (l *lexer) lineStart() error {
	width := 0
	for l.at() == ' ' {
		width++
		l.advance()
	}
	// A tab can only turn up here as the first non-space byte of the leading
	// whitespace run, which is exactly the case that is an error.
	if l.at() == '\t' {
		return l.errf("tabs are not allowed for indentation; use spaces")
	}

	// End of input after trailing whitespace: this line never had real
	// content, so it's effectively a blank line and must not cause run()
	// to emit a trailing NEWLINE. Leave atLineStart as-is (true).
	if l.pos >= len(l.src) {
		return nil
	}

	switch l.at() {
	case '\n': // blank line
		l.advance()
		return nil
	case '\r':
		l.advance()
		return nil
	case '#': // comment-only line
		for l.pos < len(l.src) && l.at() != '\n' {
			l.advance()
		}
		return nil
	}

	// A line with real content: reconcile indentation.
	top := l.indents[len(l.indents)-1]
	switch {
	case width > top:
		l.indents = append(l.indents, width)
		l.emit(token.INDENT, "", l.curPos())
	case width < top:
		for len(l.indents) > 1 && l.indents[len(l.indents)-1] > width {
			l.indents = l.indents[:len(l.indents)-1]
			l.emit(token.DEDENT, "", l.curPos())
		}
		if l.indents[len(l.indents)-1] != width {
			return l.errf("inconsistent dedent: indentation does not match any enclosing block")
		}
	}
	l.atLineStart = false
	return nil
}

func (l *lexer) lexIdent() {
	start := l.curPos()
	begin := l.pos
	for l.pos < len(l.src) && (isLetter(l.at()) || isDigit(l.at()) || l.at() == '_') {
		l.advance()
	}
	l.emit(token.IDENT, l.src[begin:l.pos], start)
}

// lexNumber reads an integer, or a float when the digits are followed by a
// '.' and at least one more digit. The trailing-digit requirement keeps
// dotted file targets like `2.txt` lexing as INT DOT IDENT, unchanged.
func (l *lexer) lexNumber() {
	start := l.curPos()
	begin := l.pos
	for l.pos < len(l.src) && isDigit(l.at()) {
		l.advance()
	}
	if l.at() == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1]) {
		l.advance() // '.'
		for l.pos < len(l.src) && isDigit(l.at()) {
			l.advance()
		}
		l.emit(token.FLOAT, l.src[begin:l.pos], start)
		return
	}
	l.emit(token.INT, l.src[begin:l.pos], start)
}

func (l *lexer) lexString() error {
	start := l.curPos()
	l.advance() // opening quote
	var sb strings.Builder
	for {
		if l.pos >= len(l.src) {
			return &Error{Pos: start, Msg: "unterminated string literal"}
		}
		c := l.at()
		if c == '"' {
			l.advance()
			break
		}
		if c == '\n' {
			return &Error{Pos: start, Msg: "unterminated string literal (newline in string)"}
		}
		if c == '\\' {
			l.advance()
			if l.pos >= len(l.src) {
				return &Error{Pos: start, Msg: "unterminated escape in string literal"}
			}
			switch l.at() {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '\\':
				sb.WriteByte('\\')
			case '"':
				sb.WriteByte('"')
			case '0':
				sb.WriteByte(0)
			default:
				return l.errf("unknown escape sequence \\%c", l.at())
			}
			l.advance()
			continue
		}
		sb.WriteByte(c)
		l.advance()
	}
	l.emit(token.STRING, sb.String(), start)
	return nil
}

func (l *lexer) lexOperator() error {
	start := l.curPos()
	c := l.at()
	two := ""
	if l.pos+1 < len(l.src) {
		two = l.src[l.pos : l.pos+2]
	}
	switch two {
	case "->":
		l.advance()
		l.advance()
		l.emit(token.ARROW, "->", start)
		return nil
	case "<=":
		l.advance()
		l.advance()
		l.emit(token.LE, "<=", start)
		return nil
	case ">=":
		l.advance()
		l.advance()
		l.emit(token.GE, ">=", start)
		return nil
	case ":=":
		// The two characters must be adjacent, so a statement's `Keyword: …`
		// colon is never mistaken for one: every keyword and argument colon is
		// followed by a space or a line break (and `domain fmt` writes it that
		// way). `Mode:=First` is the one spelling that changes meaning, and it
		// was never the spelling of anything.
		l.advance()
		l.advance()
		l.emit(token.ASSIGN, ":=", start)
		return nil
	}

	var k token.Kind
	switch c {
	case ':':
		k = token.COLON
	case ',':
		k = token.COMMA
	case '.':
		k = token.DOT
	case '(':
		k = token.LPAREN
		l.parens = append(l.parens, start)
	case ')':
		k = token.RPAREN
		// A stray ')' is not the lexer's error to report — the parser has the
		// context to say what was expected — so it simply closes nothing.
		if len(l.parens) > 0 {
			l.parens = l.parens[:len(l.parens)-1]
		}
	case '+':
		k = token.PLUS
	case '-':
		k = token.MINUS
	case '*':
		k = token.STAR
	case '/':
		k = token.SLASH
	case '%':
		k = token.PERCENT
	case '=':
		k = token.EQ
	case '<':
		k = token.LT
	case '>':
		k = token.GT
	default:
		// c may be the lead byte of a multi-byte UTF-8 rune; decode it so the
		// message shows the character that was actually in the source rather
		// than misinterpreting the raw byte value as its own code point.
		r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
		return l.errf("unexpected character %q", string(r))
	}
	l.advance()
	l.emit(k, string(c), start)
	return nil
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
