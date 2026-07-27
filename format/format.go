// Package format implements canonical whitespace formatting for Domain
// source, in the spirit of gofmt: fixed operator/punctuation spacing and
// 4-space indentation per nesting level. Comments and blank-line paragraph
// breaks are carried through from the original source (collapsing runs of
// blank lines to at most one), since the lexer itself discards both and
// they can only be recovered from the raw byte gaps between tokens.
package format

import (
	"strings"

	"domain/lexer"
	"domain/parser"
	"domain/token"
)

const indentUnit = "    "

// Format returns the canonical form of Domain source src. On a lex or parse
// error, src is returned unchanged alongside the error, so a caller can
// never lose or corrupt input it failed to understand.
func Format(src string) (string, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return src, err
	}
	if _, err := parser.Parse(src, toks); err != nil {
		return src, err
	}

	var out strings.Builder
	depth := 0
	atLineStart := true
	var prev token.Token
	havePrev := false
	prevEnd := 0

	for _, t := range toks {
		switch t.Kind {
		case token.EOF:
			out.WriteString(interLineGap(src[prevEnd:t.Pos.Offset], depth, false))
		case token.NEWLINE:
			out.WriteString(danglingComment(src[prevEnd:t.Pos.Offset]))
			out.WriteByte('\n')
			atLineStart = true
			havePrev = false
		case token.INDENT:
			out.WriteString(interLineGap(src[prevEnd:t.Pos.Offset], depth, true))
			depth++
		case token.DEDENT:
			out.WriteString(interLineGap(src[prevEnd:t.Pos.Offset], depth, true))
			depth--
		default:
			out.WriteString(interLineGap(src[prevEnd:t.Pos.Offset], depth, true))
			if atLineStart {
				out.WriteString(strings.Repeat(indentUnit, depth))
				atLineStart = false
			} else if havePrev && needsSpace(prev, t) {
				out.WriteByte(' ')
			}
			out.WriteString(tokenText(t))
			prev, havePrev = t, true
		}
		prevEnd = t.End
	}

	return out.String(), nil
}

// noSpaceAfter/noSpaceBefore keep call syntax "f(x)" and access chains
// "a.b" tight, and structural markers ("label:", "a, b") glued to what
// precedes them; every other adjacent pair gets a single separating space.
var noSpaceAfter = map[token.Kind]bool{
	token.LPAREN: true,
	token.DOT:    true,
}

var noSpaceBefore = map[token.Kind]bool{
	token.RPAREN: true,
	token.COMMA:  true,
	token.COLON:  true,
	token.DOT:    true,
}

func needsSpace(prev, cur token.Token) bool {
	if noSpaceAfter[prev.Kind] || noSpaceBefore[cur.Kind] {
		return false
	}
	return true
}

// interLineGap renders the comment-only and blank lines found in the raw
// source between two tokens that span a line break, reindented to depth.
// Runs of blank lines collapse to one. When dropLast is set, the final
// fragment (the next content line's leading indentation, already handled
// by the caller) is discarded rather than emitted as a line of its own.
func interLineGap(gap string, depth int, dropLast bool) string {
	if gap == "" {
		return ""
	}
	lines := strings.Split(gap, "\n")
	if dropLast {
		lines = lines[:len(lines)-1]
	}

	var b strings.Builder
	blank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blank = true
			continue
		}
		if blank {
			b.WriteByte('\n')
			blank = false
		}
		b.WriteString(strings.Repeat(indentUnit, depth))
		b.WriteString(trimmed)
		b.WriteByte('\n')
	}
	return b.String()
}

// danglingComment extracts a same-line trailing comment (everything after
// the last real token up to the newline that ends it), if any.
func danglingComment(gap string) string {
	trimmed := strings.TrimSpace(gap)
	if trimmed == "" {
		return ""
	}
	return " " + trimmed
}

func tokenText(t token.Token) string {
	switch t.Kind {
	case token.STRING:
		return quoteString(t.Literal)
	case token.COLON:
		return ":"
	case token.COMMA:
		return ","
	case token.DOT:
		return "."
	case token.LPAREN:
		return "("
	case token.RPAREN:
		return ")"
	case token.ARROW:
		return "->"
	case token.PLUS:
		return "+"
	case token.MINUS:
		return "-"
	case token.STAR:
		return "*"
	case token.SLASH:
		return "/"
	case token.PERCENT:
		return "%"
	case token.EQ:
		return "="
	case token.LT:
		return "<"
	case token.GT:
		return ">"
	case token.LE:
		return "<="
	case token.GE:
		return ">="
	default: // IDENT, INT, FLOAT: source text is already canonical
		return t.Literal
	}
}

// quoteString re-escapes a STRING token's decoded Literal for output, since
// the lexer stores the unescaped value rather than the source spelling.
func quoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case 0:
			b.WriteString(`\0`)
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}
