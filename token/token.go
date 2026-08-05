// Package token defines the lexical tokens of the Domain language and their
// source positions. It is the lowest layer and depends on nothing else.
package token

import "fmt"

// Kind enumerates the kinds of tokens produced by the lexer.
type Kind int

const (
	ILLEGAL Kind = iota
	EOF

	// Layout
	NEWLINE // end of a logical line
	INDENT  // increase in indentation
	DEDENT  // decrease in indentation

	// Literals / names
	IDENT  // bare word: input, Split, Descending, stdout
	INT    // integer literal: 2020
	FLOAT  // decimal literal: 3.25 (digits '.' digits)
	STRING // double-quoted string with escapes interpreted

	// RAW is an indented block of foreign-language source captured verbatim —
	// the body of `Domain Expansion: Python` and its siblings. It is the one
	// token that spans several lines, and the one whose text the lexer does not
	// interpret at all: no keywords, no comments, no escapes, no layout. Its
	// Literal is the block dedented to column zero (see lexer.rawBlock).
	RAW

	// Punctuation / operators
	COLON  // :
	COMMA  // ,
	DOT    // .
	LPAREN // (
	RPAREN // )
	ARROW  // ->

	PLUS    // +
	MINUS   // -
	STAR    // *
	SLASH   // /
	PERCENT // %   (Euclidean modulo; see the `mod` builtin)
	EQ      // =   (equality; there is no assignment in Domain)
	LT      // <
	GT      // >
	LE      // <=
	GE      // >=

	// Logical connectives. The lexer emits these as IDENT ("and"/"or"); the
	// expression parser recognizes them in infix position and rewrites to
	// these kinds. They are not reserved words outside infix position.
	AND // and
	OR  // or

	// NOT is the prefix negation, spelled `ikke`. Like AND/OR it is lexed as
	// an IDENT and rewritten by the expression parser in prefix position, so
	// it stays usable as an ordinary name elsewhere.
	NOT // ikke
)

var kindNames = [...]string{
	ILLEGAL: "ILLEGAL",
	EOF:     "EOF",
	NEWLINE: "NEWLINE",
	INDENT:  "INDENT",
	DEDENT:  "DEDENT",
	IDENT:   "IDENT",
	INT:     "INT",
	FLOAT:   "FLOAT",
	STRING:  "STRING",
	RAW:     "RAW",
	COLON:   "COLON",
	COMMA:   "COMMA",
	DOT:     "DOT",
	LPAREN:  "LPAREN",
	RPAREN:  "RPAREN",
	ARROW:   "ARROW",
	PLUS:    "PLUS",
	MINUS:   "MINUS",
	STAR:    "STAR",
	SLASH:   "SLASH",
	PERCENT: "PERCENT",
	EQ:      "EQ",
	LT:      "LT",
	GT:      "GT",
	LE:      "LE",
	GE:      "GE",
	AND:     "AND",
	OR:      "OR",
	NOT:     "NOT",
}

// String returns the symbolic name of a token kind, for diagnostics.
func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Position records where a token begins in the source. Offset is a byte
// offset into the source string, which the parser uses to recover the exact
// original text of an operation phrase.
type Position struct {
	Line   int
	Col    int
	Offset int
}

func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

// Token is a single lexical token.
type Token struct {
	Kind    Kind
	Literal string   // for STRING this is the unescaped value
	Pos     Position // start position (Offset = byte start)
	End     int      // byte offset just past the token in the source
}

func (t Token) String() string {
	switch t.Kind {
	case IDENT, INT, FLOAT, STRING:
		return fmt.Sprintf("%s(%q)", t.Kind, t.Literal)
	default:
		return t.Kind.String()
	}
}
