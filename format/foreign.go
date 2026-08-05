package format

import (
	"strings"

	"domain/token"
)

// A foreign block (`Domain Expansion: Python` and its indented body) is the one
// region the formatter must not touch at all. Its `#` is another language's
// comment, its tabs are that language's indentation, and its trailing
// whitespace may be inside a string the formatter cannot see — every rule in
// this package is wrong there.
//
// It cannot simply be left as written either. The block belongs to its opener
// *by indentation*: it is the block only while it is indented deeper than the
// line above it. So when the formatter moves the opener, the block moves with
// it, by exactly the same amount and with every byte after the leading
// whitespace preserved. That is the same relationship `shiftIndent` maintains
// for the lines continuing an open parenthesis, held to a stricter promise
// about the interior.

// rawBlock is one foreign block's extent in source lines, and the line of the
// statement that opened it.
type rawBlock struct {
	opener, first, last int
}

// rawBlocks locates every foreign block, returning the blocks and a map from
// each of their lines to the block it belongs to.
func rawBlocks(src string, toks []token.Token) ([]rawBlock, map[int]int) {
	var blocks []rawBlock
	byLine := map[int]int{}
	for _, t := range toks {
		if t.Kind != token.RAW {
			continue
		}
		if t.Pos.Offset < 0 || t.End > len(src) || t.Pos.Offset > t.End {
			continue // defensive; a malformed span is left to the ordinary path
		}
		region := src[t.Pos.Offset:t.End]
		last := t.Pos.Line + strings.Count(region, "\n")
		if strings.HasSuffix(region, "\n") {
			last--
		}
		// The lexer emits a RAW starting on the line after its opener.
		b := rawBlock{opener: t.Pos.Line - 1, first: t.Pos.Line, last: last}
		for l := b.first; l <= b.last; l++ {
			byLine[l] = len(blocks)
		}
		blocks = append(blocks, b)
	}
	return blocks, byLine
}

// shiftRaw re-indents a whole foreign block by delta columns, preserving every
// byte after the leading whitespace of each line.
//
// A negative shift is clamped to what the least-indented line can give up, so
// the block's own internal structure — the thing its language cares about —
// survives a move that would otherwise have eaten into it. Only spaces are
// removed; a tab is indivisible and is never split into columns.
func shiftRaw(lines []string, delta int) []string {
	if delta < 0 {
		room := -delta
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			room = min(room, len(line)-len(strings.TrimLeft(line, " ")))
		}
		delta = -room
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case strings.TrimSpace(line) == "":
			out[i] = ""
		case delta > 0:
			out[i] = strings.Repeat(" ", delta) + line
		default:
			out[i] = line[-delta:]
		}
	}
	return out
}
