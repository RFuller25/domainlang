package lexer

import (
	"strings"

	"domain/ast"
	"domain/token"
)

// Foreign blocks: the indented body of `Domain Expansion: Python` (and its
// siblings) is source in another language, and the lexer must not tokenize it.
// Python's `{`, Rust's `&`, rask's `|` are not Domain characters; a `#` inside
// the block is not a Domain comment; a tab in its indentation is not an error.
// So the whole region is captured verbatim as one token.RAW and handed on as
// text.
//
// The trigger has to be recognized here rather than in the parser, because by
// the time the parser sees a statement the lines beneath it have already been
// tokenized — and tokenizing them is the thing that cannot happen. Two rules
// keep that from being dangerous:
//
//  1. The opening line must be a *language name and nothing else*, either
//     bare or behind a themed keyword: `Domain Expansion: Python`, `Python`.
//     An argument line (`Using: Python`) does not qualify, because the words
//     before its colon are not a themed keyword.
//  2. A block only opens if the next line with content is indented deeper
//     than the opener. A language name with no block beneath it lexes exactly
//     as it always did, and the parser reports the missing block.
//
// Everything else about the region is left alone. The one transformation is
// dedenting: the block's common leading whitespace is removed so the foreign
// source starts at column zero, which is what its own compiler expects.

// rawBlock captures the foreign block beneath the logical line that just
// ended, emitting it as a single token.RAW. The cursor is at the start of the
// following line; it stays at a line start either way, so a line that opens no
// block is left for the ordinary loop exactly as it found it.
//
// No INDENT is pushed and none is emitted: the block is not a Domain block, and
// the indentation stack must come out of it believing nothing happened, so the
// next Domain line reconciles against the opener's own level.
func (l *lexer) rawBlock() {
	if !foreignOpener(l.toks[l.lineTok : len(l.toks)-1]) { // less the NEWLINE just emitted
		return
	}
	openerIndent := l.indents[len(l.indents)-1]
	end, ok := rawBlockExtent(l.src, l.pos, openerIndent)
	if !ok {
		return
	}
	start := l.curPos()
	region := l.src[l.pos:end]
	// Byte by byte, so line and column keep counting through the block and
	// every position after it still points where it should.
	for l.pos < end {
		l.advance()
	}
	l.emit(token.RAW, rawBlockText(region), start)
}

// foreignOpener reports whether the logical line whose tokens are lineToks
// (excluding the closing NEWLINE) opens a foreign block. The shape rule is
// ast.ForeignOpener, shared with the parser; all this adds is stripping the
// layout tokens, which are emitted ahead of a line's first content token and so
// arrive in front of an indented opener.
func foreignOpener(lineToks []token.Token) bool {
	for len(lineToks) > 0 &&
		(lineToks[0].Kind == token.INDENT || lineToks[0].Kind == token.DEDENT) {
		lineToks = lineToks[1:]
	}
	_, _, ok := ast.ForeignOpener(lineToks)
	return ok
}

// rawBlockExtent finds the byte range of the foreign block beginning at off (a
// line start), given the indentation width of the line that opened it. It
// returns the offset just past the block's last content line.
//
// Blank lines are part of the block when content follows them and are left
// outside it when none does, so a blank line separating the block from the next
// statement stays an ordinary blank line — the formatter's business, not the
// foreign compiler's.
//
// ok is false when no line qualifies, which is how "a language name with no
// block beneath it" stays an ordinary statement (rule 2 above).
func rawBlockExtent(src string, off, openerIndent int) (end int, ok bool) {
	for i := off; i < len(src); {
		j := strings.IndexByte(src[i:], '\n')
		lineEnd := len(src)
		next := len(src)
		if j >= 0 {
			lineEnd = i + j
			next = lineEnd + 1
		}
		line := src[i:lineEnd]
		if blankLine(line) {
			i = next
			continue
		}
		if indentWidthOf(line) <= openerIndent {
			break
		}
		end = next
		i = next
	}
	return end, end > off
}

// rawBlockText renders the captured region as the foreign source it stands for:
// every line stripped of the block's common leading whitespace, blank lines
// emptied, and a single closing newline.
//
// The common prefix is compared as literal bytes rather than as a width, so a
// tab-indented Go block and a space-indented Python one are each dedented by
// exactly what they were indented with, and any interior structure the author
// wrote is preserved byte for byte. A block whose lines disagree about their
// leading whitespace has no common prefix to remove, and keeps the indentation
// as written — a mixed-indentation block is the foreign compiler's complaint to
// make, and it can only make it about text it actually received.
func rawBlockText(region string) string {
	lines := strings.Split(strings.TrimSuffix(region, "\n"), "\n")

	prefix := ""
	first := true
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if blankLine(line) {
			continue
		}
		ws := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if first {
			prefix, first = ws, false
			continue
		}
		prefix = commonPrefix(prefix, ws)
	}

	out := make([]string, len(lines))
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if blankLine(line) {
			out[i] = ""
			continue
		}
		out[i] = strings.TrimPrefix(line, prefix)
	}
	return strings.Join(out, "\n") + "\n"
}

// blankLine reports whether a line holds no content. A `#` does not make a line
// blank here: inside a foreign block it is that language's comment (or its
// preprocessor, or nothing at all), and either way it is content.
func blankLine(line string) bool {
	return strings.TrimLeft(line, " \t\r") == ""
}

// indentWidthOf measures a line's leading whitespace in columns, expanding a
// tab to the next multiple of 8. Domain's own indentation is spaces only, so
// this rule exists solely to decide whether a *foreign* line is inside the
// block — and foreign code is routinely indented with tabs.
func indentWidthOf(line string) int {
	w := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			w++
		case '\t':
			w += 8 - w%8
		default:
			return w
		}
	}
	return w
}

func commonPrefix(a, b string) string {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}
