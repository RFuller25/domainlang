// Syntax highlighting for the REPL's own output.
//
// What gets highlighted is the *record*: the line just submitted as it lands
// in the scrollback, and `:list`. Not the line being edited — a single-line
// textinput styles its whole value at once, so live coloring would mean
// reimplementing the editor, and the payoff is on the lines you read back
// rather than the one under the cursor.
//
// The colors are the CLI's own (visualize_style.go), so a REPL transcript and
// the visualizer look like the same program. Highlighting is driven by the
// real lexer rather than regular expressions: a `#` inside a string literal is
// not a comment, and `"Cursed Technique"` inside one is not a keyword, and the
// lexer is the only thing that already knows the difference. Source that does
// not lex — which, mid-session, is most of what a user types — is returned
// untouched.
package main

import (
	"strings"

	"charm.land/lipgloss/v2"

	"domain/ast"
	"domain/lexer"
	"domain/token"
)

// paintIf applies a style only when the session is in color, so one call site
// serves both a terminal and a pipe.
func paintIf(color bool, style lipgloss.Style, s string) string {
	if !color {
		return s
	}
	return style.Render(s)
}

// highlightSource colors a program (or one statement of one). With color off,
// or on source that does not lex, it returns src unchanged — highlighting is
// never allowed to alter what the user sees, only how it is painted.
func highlightSource(src string, color bool) string {
	if !color || src == "" {
		return src
	}
	toks, err := lexer.Lex(src)
	if err != nil {
		return src
	}

	var b strings.Builder
	prev := 0
	for i, t := range toks {
		if t.Kind == token.EOF {
			break
		}
		start, end := t.Pos.Offset, t.End
		// Layout tokens (NEWLINE, INDENT, DEDENT) carry no source extent of
		// their own; the text they stand for is copied as a gap.
		if end <= start || start < prev || end > len(src) {
			continue
		}
		b.WriteString(gapText(src[prev:start]))
		b.WriteString(styleFor(toks, i).Render(src[start:end]))
		prev = end
	}
	b.WriteString(gapText(src[prev:]))
	return b.String()
}

// gapText paints the source between two tokens: whitespace, and the comments
// the lexer dropped. It works a line at a time because a style applies to a
// line: handing a trailing newline to a renderer pads what follows it, which
// would move the program's own text.
func gapText(s string) string {
	if !strings.Contains(s, "#") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if j := strings.IndexByte(line, '#'); j >= 0 {
			lines[i] = line[:j] + styComment.Render(line[j:])
		}
	}
	return strings.Join(lines, "\n")
}

// styleFor picks a token's color from its kind and its place in the line.
func styleFor(toks []token.Token, i int) lipgloss.Style {
	switch toks[i].Kind {
	case token.STRING:
		return styString
	case token.INT, token.FLOAT:
		return styNumber
	case token.IDENT:
		switch {
		case inKeywordPhrase(toks, i):
			return styKeyword
		case toks[i+1].Kind == token.COLON && startsLine(toks, i):
			return styArgName // an indented `Using:` / `From:` / `Seed:` label
		}
		return styLabel
	default:
		return styPunct
	}
}

// startsLine reports whether the token at i is the first one on its line.
func startsLine(toks []token.Token, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch toks[j].Kind {
		case token.NEWLINE, token.INDENT, token.DEDENT:
			continue
		default:
			return toks[j].Pos.Line != toks[i].Pos.Line
		}
	}
	return true
}

// inKeywordPhrase reports whether the IDENT at i is part of the themed keyword
// that opens its line. The check runs over the whole run of leading
// identifiers, so the second word of "Cursed Technique" is colored as keyword
// while the "Technique" in an operation phrase is not.
func inKeywordPhrase(toks []token.Token, i int) bool {
	start := i
	for start > 0 && toks[start-1].Kind == token.IDENT && toks[start-1].Pos.Line == toks[i].Pos.Line {
		start--
	}
	if !startsLine(toks, start) {
		return false
	}
	var words []string
	for j := start; j < len(toks) && toks[j].Kind == token.IDENT && toks[j].Pos.Line == toks[i].Pos.Line; j++ {
		words = append(words, toks[j].Literal)
	}
	_, n, ok := ast.KeywordPrefix(words)
	return ok && i-start < n
}
