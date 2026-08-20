package token

import "strings"

// A comment starts either way.
//
//	# the classic
//	technically this is one too
//
// The word form exists because the language is written in sentences and its
// asides are written that way too: "technically the grid is ragged" is a
// remark a reader was going to make anyway, and making it a comment costs no
// punctuation. It is a *word*, so it is recognized only where a word can
// start and only when a word does not continue past it — `technicallyOK` is an
// identifier, and so is `technically_true`.
//
// Both markers run to the end of the line; neither has a block form.
const (
	// CommentHash is the punctuation marker.
	CommentHash = '#'
	// CommentWord is the word marker, matched without regard to case: a
	// sentence that opens the line writes `Technically`, and one that
	// interrupts a statement writes `technically`.
	CommentWord = "technically"
)

// CommentMarker returns the width in bytes of the comment marker beginning at
// src[i], or 0 when no comment starts there.
//
// The word form is held to word boundaries on both sides, which is what keeps
// it from swallowing a name that merely contains it. The `#` form has no such
// rule — it never could be part of a name — so it starts a comment wherever it
// is not inside a string literal, exactly as it always has.
func CommentMarker(src string, i int) int {
	if i < 0 || i >= len(src) {
		return 0
	}
	if src[i] == CommentHash {
		return 1
	}
	// The first byte is checked before anything else because the lexer asks
	// this once per token: a byte that cannot open the word costs one
	// comparison. `|0x20` lower-cases an ASCII letter, and only 't' and 'T'
	// map onto CommentWord's first byte.
	if src[i]|0x20 != CommentWord[0] {
		return 0
	}
	n := len(CommentWord)
	if i+n > len(src) || !strings.EqualFold(src[i:i+n], CommentWord) {
		return 0
	}
	if i > 0 && isWordByte(src[i-1]) {
		return 0 // the tail of a longer name
	}
	if i+n < len(src) && isWordByte(src[i+n]) {
		return 0 // the head of one
	}
	return n
}

// CommentStart returns the byte offset of the first comment marker on line, or
// -1 when it has no comment. A marker inside a string literal is not a comment,
// so quoted text — and its escapes — is skipped: `Using: (c) -> c = "#"` has
// no comment on it, and neither does `Reveal: "technically fine"`.
//
// The line is assumed to start outside a string, which is true of every line
// of Domain source: a string literal that reaches a newline is a lex error.
func CommentStart(line string) int {
	inString := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case inString && c == '\\':
			i++ // skip the escaped byte
		case inString && c == '"':
			inString = false
		case inString:
		case c == '"':
			inString = true
		default:
			if w := CommentMarker(line, i); w > 0 {
				return i
			}
		}
	}
	return -1
}

// IsCommentLine reports whether a line is nothing but a comment, leading
// whitespace aside. A blank line is not one.
func IsCommentLine(line string) bool {
	t := strings.TrimLeft(line, " \t\r")
	return t != "" && CommentMarker(t, 0) > 0
}

// isWordByte reports whether c can appear in an identifier, which is what
// decides where the word marker's boundaries are. Domain identifiers are ASCII
// letters, digits and underscores — the same set lexIdent accepts — so a rune
// outside that set is a boundary like any other punctuation.
func isWordByte(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
