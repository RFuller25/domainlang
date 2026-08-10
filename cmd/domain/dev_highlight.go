// Syntax highlighting for the editor, where the REPL's approach does not
// reach.
//
// `highlightSource` (repl_highlight.go) paints a whole program and hands back a
// string. That is right for a transcript, and wrong for something being typed
// into, for two reasons.
//
// The first is the cursor. Once escape codes are in the string, putting a
// cursor at column N means cutting a styled string at a visual column — the
// operation that makes hand-rolled terminal editors fragile, and one that gets
// no easier when the cursor lands in the middle of a token. So a line is
// resolved to a **face per byte** and faces become escape codes only at the
// very end. The cursor is then one more face on one more rune, and nothing
// ever cuts an ANSI string.
//
// The second is failure. `lexer.Lex` refuses a half-typed string literal, and
// `highlightSource` answers that by returning the source unpainted — which,
// over a whole program, means one open quote un-paints the file while you are
// still typing the line that contains it. Here each line is lexed on its own,
// so the damage stops at the line being edited. That works because every line
// of a Domain program means something alone, indented continuation lines
// included — see TestEveryLineOfEveryProgramLexesAlone, which is a claim about
// an indentation-sensitive language and so is checked rather than assumed.
//
// What a token *is* is decided in repl_highlight.go (faceFor), so a program
// looks the same in a transcript and in the editor.
package main

import (
	"strings"
	"unicode/utf8"

	"domain/lexer"
	"domain/token"
)

// facesFor assigns a face to every byte of one line of source. A line that
// does not lex comes back all-plain, which is the REPL's rule — highlighting
// never alters text, only paint — applied one line at a time.
func facesFor(line string) []face {
	out := make([]face, len(line))
	toks, err := lexer.Lex(line)
	if err != nil {
		return out
	}
	for i, t := range toks {
		if t.Kind == token.EOF {
			break
		}
		start, end := t.Pos.Offset, t.End
		// Layout tokens (NEWLINE, INDENT, DEDENT) stand for text they do not
		// span; RAW is a foreign block and has no Domain syntax to paint.
		if end <= start || start < 0 || end > len(line) || t.Kind == token.RAW {
			continue
		}
		f := faceFor(toks, i)
		for j := start; j < end; j++ {
			out[j] = f
		}
	}
	markComment(out, line)
	return out
}

// markComment paints a trailing comment. The lexer drops comments entirely, so
// they are found in the gaps rather than among the tokens — and a '#' inside a
// string literal is not a comment, which is why this runs *after* the tokens
// have claimed their bytes.
func markComment(out []face, line string) {
	for i := range len(line) {
		if line[i] != '#' || out[i] != facePlain {
			continue // a '#' a token already owns is inside a string literal
		}
		for j := i; j < len(line); j++ {
			out[j] = faceComment
		}
		return
	}
}

// lineDecor is what the editor wants drawn over a line's syntax: the cursor,
// the selection, and any search hits. All three are the same kind of thing —
// a byte range that overrides the face underneath — which is the payoff of
// deciding faces before painting rather than after. Nothing here has to
// understand ANSI, and nothing has to cut a styled string.
//
// Ranges are half-open byte offsets. Later entries win, so the order below is
// the precedence: a cursor sitting on a search hit inside a selection still
// reads as the cursor.
type lineDecor struct {
	cursor     int // byte offset, or negative for no cursor on this line
	selStart   int // selection range, both -1 when there is none
	selEnd     int
	matches    []devMatch
	currentHit int // index into matches that is the search's current one, or -1
}

// noDecor is a line with nothing drawn over it.
func noDecor() lineDecor { return lineDecor{cursor: -1, selStart: -1, selEnd: -1, currentHit: -1} }

// renderLine paints one line with only a cursor on it. A cursor at or past the
// end of the line is drawn as a trailing block, which is where it sits while
// you type at the end.
func renderLine(line string, cursor int) string {
	d := noDecor()
	d.cursor = cursor
	return paintLine(line, d)
}

// paintLine paints one line's syntax with its decorations over the top.
//
// Runs of equal face are coalesced into one styled chunk. That is not a
// micro-optimization: painting is eleven times the cost of lexing here, and it
// is paid per chunk, so a line rendered a character at a time would cost forty
// times what this does.
func paintLine(line string, d lineDecor) string {
	faces := facesFor(line)

	fill := func(start, end int, f face) {
		for j := max(start, 0); j < min(end, len(faces)); j++ {
			faces[j] = f
		}
	}

	if d.selStart >= 0 && d.selEnd > d.selStart {
		fill(d.selStart, d.selEnd, faceSelect)
	}
	for i, m := range d.matches {
		f := faceMatch
		if i == d.currentHit {
			f = faceMatchCurrent
		}
		fill(m.start, m.end, f)
	}
	if d.cursor >= 0 && d.cursor < len(faces) {
		// The cursor covers a whole rune, not a byte: painting one byte of a
		// multi-byte rune would split it across two styled chunks and print two
		// replacement characters instead of the character.
		_, w := utf8.DecodeRuneInString(line[d.cursor:])
		fill(d.cursor, d.cursor+w, faceCursor)
	}

	var b strings.Builder
	for i := 0; i < len(line); {
		j := i
		for j < len(line) && faces[j] == faces[i] {
			j++
		}
		b.WriteString(faceStyle(faces[i]).Render(line[i:j]))
		i = j
	}
	if d.cursor >= len(line) {
		b.WriteString(faceStyle(faceCursor).Render(" "))
	} else if d.selEnd > len(line) {
		// A selection that runs past the end of a line covers its newline, and
		// showing that as one highlighted cell is what makes a multi-line
		// selection read as continuous rather than as a stack of fragments.
		b.WriteString(faceStyle(faceSelect).Render(" "))
	}
	return b.String()
}
