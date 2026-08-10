// The editing surface for `domain expansion: development`.
//
// This exists because bubbles/textarea cannot be made to do it: it renders
// each wrapped line through exactly one Lip Gloss style, holds its text as
// [][]rune with every cursor and column calculation running over that, and
// offers no per-token hook. Escape codes cannot be smuggled in through the
// value without corrupting both Value() and the cursor. The upstream pull
// request that would have added a highlight callback was closed unmerged in
// May 2026, and no other Go component does it either — so the buffer is ours.
//
// Losing textarea costs less here than it would elsewhere. Domain programs are
// short and genuinely line-oriented: statements, indented argument lines, no
// soft wrap worth having. Most of what textarea provides is not wanted, and
// what is wanted — a gutter that can carry diagnostics, hints painted past the
// end of a line — it would have fought.
//
// Lines are held as strings and the cursor column is a **byte** offset into
// one, kept on a rune boundary by every motion here. Bytes rather than runes
// because everything that will read this buffer — the lexer, diag, the
// language server's inlay hints — speaks byte offsets, and one conversion at
// the edge is cheaper than a conversion at every seam.
package main

import (
	"strings"
	"unicode/utf8"
)

// devBuffer is the program being edited.
type devBuffer struct {
	lines []string
	row   int // index into lines
	col   int // byte offset into lines[row]
	// goalCol is the column vertical motion aims for, so moving down through a
	// short line and out the other side returns to where you started rather
	// than to the short line's end.
	goalCol int
	// anchor is where a selection started; nil when nothing is selected. It is
	// held here rather than in the model because an edit has to move it, and
	// the edits are here.
	anchor *pos
}

func newDevBuffer(text string) *devBuffer {
	// A trailing newline is a line terminator, not an empty last line: without
	// this, opening a well-formed file and saving it back would grow it by one
	// blank line every time.
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return &devBuffer{lines: strings.Split(text, "\n")}
}

// text returns the buffer as a program — what gets lexed, checked, run, saved.
func (b *devBuffer) text() string { return strings.Join(b.lines, "\n") }

func (b *devBuffer) line() string { return b.lines[b.row] }

// clampCol pulls the cursor back onto a rune boundary within the current line.
func (b *devBuffer) clampCol() {
	l := b.line()
	b.col = min(max(b.col, 0), len(l))
	for b.col > 0 && b.col < len(l) && !utf8.RuneStart(l[b.col]) {
		b.col--
	}
}

func (b *devBuffer) insert(s string) {
	l := b.line()
	b.lines[b.row] = l[:b.col] + s + l[b.col:]
	b.col += len(s)
	b.goalCol = b.col
}

// newline splits the current line at the cursor and carries the leading
// indentation onto the new one — the single editing affordance an
// indentation-sensitive language cannot do without. A line that opens a block
// carries one level *more*, since its body has to be indented under it.
func (b *devBuffer) newline() {
	l := b.line()
	head, tail := l[:b.col], l[b.col:]
	indent := autoIndentFor(head)
	b.lines = append(b.lines, "")
	copy(b.lines[b.row+2:], b.lines[b.row+1:])
	b.lines[b.row] = head
	b.lines[b.row+1] = indent + tail
	b.row++
	b.col = len(indent)
	b.goalCol = b.col
}

// backspace deletes the rune before the cursor, joining lines at column zero.
func (b *devBuffer) backspace() {
	if b.col > 0 {
		l := b.line()
		_, w := utf8.DecodeLastRuneInString(l[:b.col])
		b.lines[b.row] = l[:b.col-w] + l[b.col:]
		b.col -= w
		b.goalCol = b.col
		return
	}
	if b.row == 0 {
		return
	}
	prev := b.lines[b.row-1]
	b.col = len(prev)
	b.lines[b.row-1] = prev + b.line()
	b.lines = append(b.lines[:b.row], b.lines[b.row+1:]...)
	b.row--
	b.goalCol = b.col
}

func (b *devBuffer) left() {
	if b.col > 0 {
		_, w := utf8.DecodeLastRuneInString(b.line()[:b.col])
		b.col -= w
	} else if b.row > 0 {
		b.row--
		b.col = len(b.line())
	}
	b.goalCol = b.col
}

func (b *devBuffer) right() {
	if b.col < len(b.line()) {
		_, w := utf8.DecodeRuneInString(b.line()[b.col:])
		b.col += w
	} else if b.row < len(b.lines)-1 {
		b.row++
		b.col = 0
	}
	b.goalCol = b.col
}

func (b *devBuffer) up() {
	if b.row == 0 {
		return
	}
	b.row--
	b.col = b.goalCol
	b.clampCol()
}

func (b *devBuffer) down() {
	if b.row >= len(b.lines)-1 {
		return
	}
	b.row++
	b.col = b.goalCol
	b.clampCol()
}

func (b *devBuffer) home() { b.col, b.goalCol = 0, 0 }

func (b *devBuffer) end() {
	b.col = len(b.line())
	b.goalCol = b.col
}

// ---------------------------------------------------------------------------
// positions and selection
// ---------------------------------------------------------------------------

// pos is a place in the buffer: a line, and a byte offset into it.
type pos struct{ row, col int }

// before reports whether p comes earlier in the buffer than q.
func (p pos) before(q pos) bool {
	return p.row < q.row || (p.row == q.row && p.col < q.col)
}

func (b *devBuffer) cursor() pos { return pos{b.row, b.col} }

// selection returns the selected range in buffer order, or ok=false when there
// is no selection. The anchor is where the selection started and the cursor is
// where it has got to, so either may be the earlier of the two.
func (b *devBuffer) selection() (start, end pos, ok bool) {
	if b.anchor == nil {
		return pos{}, pos{}, false
	}
	a, c := *b.anchor, b.cursor()
	if a == c {
		return pos{}, pos{}, false // an anchor dropped and not moved from
	}
	if c.before(a) {
		return c, a, true
	}
	return a, c, true
}

// selectedText is the selected range as it would be copied.
func (b *devBuffer) selectedText() string {
	start, end, ok := b.selection()
	if !ok {
		return ""
	}
	if start.row == end.row {
		return b.lines[start.row][start.col:end.col]
	}
	parts := []string{b.lines[start.row][start.col:]}
	parts = append(parts, b.lines[start.row+1:end.row]...)
	return strings.Join(append(parts, b.lines[end.row][:end.col]), "\n")
}

// deleteSelection removes the selected range and leaves the cursor where it
// began. It reports whether anything was deleted.
func (b *devBuffer) deleteSelection() bool {
	start, end, ok := b.selection()
	if !ok {
		return false
	}
	head, tail := b.lines[start.row][:start.col], b.lines[end.row][end.col:]
	b.lines = append(b.lines[:start.row], append([]string{head + tail}, b.lines[end.row+1:]...)...)
	b.row, b.col = start.row, start.col
	b.goalCol = b.col
	b.anchor = nil
	return true
}

// startSelecting drops an anchor at the cursor if there is not one already,
// which is what makes a shifted motion extend rather than restart.
func (b *devBuffer) startSelecting() {
	if b.anchor == nil {
		c := b.cursor()
		b.anchor = &c
	}
}

func (b *devBuffer) clearSelection() { b.anchor = nil }

func (b *devBuffer) selectAll() {
	b.anchor = &pos{0, 0}
	b.row = len(b.lines) - 1
	b.col = len(b.line())
	b.goalCol = b.col
}

// ---------------------------------------------------------------------------
// text
// ---------------------------------------------------------------------------

// insertText inserts a run that may contain newlines — what a paste is. It
// replaces the selection first, so pasting over a selection does what it does
// everywhere else.
func (b *devBuffer) insertText(s string) {
	b.deleteSelection()
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n")
	if len(parts) == 1 {
		b.insert(parts[0])
		return
	}
	l := b.line()
	head, tail := l[:b.col], l[b.col:]
	rest := append([]string{}, parts[1:]...)
	rest[len(rest)-1] += tail
	b.lines[b.row] = head + parts[0]
	b.lines = append(b.lines[:b.row+1], append(rest, b.lines[b.row+1:]...)...)
	b.row += len(parts) - 1
	b.col = len(parts[len(parts)-1])
	b.goalCol = b.col
}

// deleteForward removes the rune under the cursor, joining the next line in
// when there is nothing left on this one.
func (b *devBuffer) deleteForward() {
	if b.deleteSelection() {
		return
	}
	l := b.line()
	if b.col < len(l) {
		_, w := utf8.DecodeRuneInString(l[b.col:])
		b.lines[b.row] = l[:b.col] + l[b.col+w:]
		return
	}
	if b.row < len(b.lines)-1 {
		b.lines[b.row] = l + b.lines[b.row+1]
		b.lines = append(b.lines[:b.row+1], b.lines[b.row+2:]...)
	}
}

// ---------------------------------------------------------------------------
// word motion
// ---------------------------------------------------------------------------

// isWordByte reports whether a byte belongs to a word for motion purposes.
// Domain identifiers are letters and digits, and a `.` in `input.txt` or `r.g`
// binds tighter than a space does — but treating it as a word byte would make
// one hop cross a field access, so it is punctuation like everything else.
func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= utf8.RuneSelf
}

// wordLeft moves to the start of the word before the cursor, crossing at most
// one line break so that a motion at column zero goes somewhere rather than
// nowhere.
func (b *devBuffer) wordLeft() {
	if b.col == 0 {
		if b.row > 0 {
			b.row--
			b.col = len(b.line())
		}
		b.goalCol = b.col
		return
	}
	l := b.line()
	i := b.col
	for i > 0 && !isWordByte(l[i-1]) {
		i--
	}
	for i > 0 && isWordByte(l[i-1]) {
		i--
	}
	b.col, b.goalCol = i, i
}

func (b *devBuffer) wordRight() {
	l := b.line()
	if b.col >= len(l) {
		if b.row < len(b.lines)-1 {
			b.row++
			b.col = 0
		}
		b.goalCol = b.col
		return
	}
	i := b.col
	for i < len(l) && isWordByte(l[i]) {
		i++
	}
	for i < len(l) && !isWordByte(l[i]) {
		i++
	}
	b.col, b.goalCol = i, i
}

// ---------------------------------------------------------------------------
// indentation
// ---------------------------------------------------------------------------

// devIndent is one level, matching `domain fmt` — four spaces, never a tab.
const devIndent = "    "

// indentRows shifts every line in [first,last] by one level. Dedent removes up
// to one level of leading whitespace and is a no-op on a line that has none,
// so holding the key down cannot eat a line's text.
func (b *devBuffer) indentRows(first, last int, out bool) {
	for r := first; r <= last && r < len(b.lines); r++ {
		l := b.lines[r]
		if out {
			trimmed := strings.TrimPrefix(l, devIndent)
			if trimmed == l {
				trimmed = strings.TrimLeft(l, " \t")
			}
			removed := len(l) - len(trimmed)
			b.lines[r] = trimmed
			if r == b.row {
				b.col = max(0, b.col-removed)
			}
			if b.anchor != nil && b.anchor.row == r {
				b.anchor.col = max(0, b.anchor.col-removed)
			}
			continue
		}
		if l == "" {
			continue // an empty line gains nothing from being indented
		}
		b.lines[r] = devIndent + l
		if r == b.row {
			b.col += len(devIndent)
		}
		if b.anchor != nil && b.anchor.row == r {
			b.anchor.col += len(devIndent)
		}
	}
	b.goalCol = b.col
}

// indentTarget is the row range an indent command applies to: the selection,
// or the cursor's line when there is not one.
func (b *devBuffer) indentTarget() (first, last int) {
	start, end, ok := b.selection()
	if !ok {
		return b.row, b.row
	}
	// A selection that ends at column zero stops *before* that line — nothing
	// of it is covered — so a line-wise command must not reach into it. Without
	// this, selecting two lines by pressing shift+down twice indents three.
	if end.col == 0 && end.row > start.row {
		end.row--
	}
	return start.row, end.row
}

// gotoLine puts the cursor at the start of a 1-based line, clamped.
func (b *devBuffer) gotoLine(n int) {
	b.row = max(0, min(n-1, len(b.lines)-1))
	b.col, b.goalCol = 0, 0
	b.anchor = nil
}
