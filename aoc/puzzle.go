// A puzzle page, turned into something a terminal can show.
//
// The page is HTML written by hand for a browser, and what a reader wants out
// of it in an editor is the prose, the worked example, and the answers they
// have already had accepted. So this is a small, deliberately partial HTML
// reader: it knows the handful of tags an AoC description actually uses and
// throws the rest away, rather than pretending to be a browser.
//
// Two things it must not lose. Code blocks are the worked example and the only
// part of the page whose whitespace carries meaning, so `<pre>` is captured
// verbatim while everything else is squeezed to single spaces and re-wrapped
// to the window. And "Your puzzle answer was X" is the record of what the site
// has already accepted from you, which is what lets a checked answer be
// checked against the site's own answer without asking the site again.
package aoc

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Puzzle is one day, as far as the person asking can see it.
type Puzzle struct {
	Year, Day int
	Title     string // "Camel Cards", without the --- Day 7: --- dressing
	// Parts is the description, one entry per part that has been unlocked:
	// one before part one is solved, two after.
	Parts [][]Block
	// Example is the worked example from part one — the small input the prose
	// walks through, which is the one to get a program working on first.
	Example string
	// Answers are the answers the site has already accepted, in order. Its
	// length is how many stars the day has: 0, 1 or 2.
	Answers []string
	// Input is the personal puzzle input.
	Input string
}

// Block is one paragraph, list item, heading or code listing.
type Block struct {
	Kind BlockKind
	Text string
}

type BlockKind int

const (
	// Para is prose, to be wrapped to whatever width there is.
	Para BlockKind = iota
	// Item is a bullet: prose, but hanging off a marker.
	Item
	// Code is a listing, to be shown exactly as it is and never wrapped.
	Code
	// Heading is a section break within the description.
	Heading
)

// Unlocked is how many parts of the puzzle can be read: 1 until part one is
// solved, 2 after.
func (p *Puzzle) Unlocked() int { return len(p.Parts) }

// Solved is how many parts have an accepted answer.
func (p *Puzzle) Solved() int { return len(p.Answers) }

// Answer is the accepted answer for a part, if there is one. Parts are counted
// from one, as the site counts them.
func (p *Puzzle) Answer(part int) (string, bool) {
	if part < 1 || part > len(p.Answers) {
		return "", false
	}
	return p.Answers[part-1], true
}

// Part returns the description of one part, counted from one.
func (p *Puzzle) Part(part int) []Block {
	if part < 1 || part > len(p.Parts) {
		return nil
	}
	return p.Parts[part-1]
}

// Working is the part to be looking at: the first one with no accepted answer,
// or the last one when the day is finished.
func (p *Puzzle) Working() int {
	if p.Solved() < p.Unlocked() {
		return p.Solved() + 1
	}
	return max(p.Unlocked(), 1)
}

// Name is how a day is written down — as a file name, a status line, or a
// sentence: "2023-07".
func (p *Puzzle) Name() string { return DayName(p.Year, p.Day) }

// DayName is the same, for callers with only the numbers. The day is padded so
// that a directory of them sorts the way a calendar does.
func DayName(year, day int) string { return fmt.Sprintf("%d-%02d", year, day) }

// ---------------------------------------------------------------------------
// reading the page
// ---------------------------------------------------------------------------

var (
	articleRe = regexp.MustCompile(`(?s)<article[^>]*>(.*?)</article>`)
	answerRe  = regexp.MustCompile(`Your puzzle answer was <code>(.*?)</code>`)
	titleRe   = regexp.MustCompile(`---\s*Day\s+\d+:\s*(.*?)\s*---`)
	tagRe     = regexp.MustCompile(`<[^>]*>`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

// parsePuzzle reads a fetched page.
//
// Each part of the description is one `<article>`, which is the site's own
// markup and has been for every year — so the count of articles is the count
// of unlocked parts, and no guessing about "part two" headings is needed.
func parsePuzzle(year, day int, page string) *Puzzle {
	p := &Puzzle{Year: year, Day: day}
	for _, m := range articleRe.FindAllStringSubmatch(page, -1) {
		p.Parts = append(p.Parts, renderBlocks(m[1]))
	}
	for _, m := range answerRe.FindAllStringSubmatch(page, -1) {
		p.Answers = append(p.Answers, html.UnescapeString(m[1]))
	}
	if m := titleRe.FindStringSubmatch(page); m != nil {
		p.Title = html.UnescapeString(strings.TrimSpace(tagRe.ReplaceAllString(m[1], "")))
	}
	p.Example = exampleFrom(p.Part(1))
	return p
}

// exampleFrom picks the worked example out of part one.
//
// It is the first listing with more than one line in it. "First" alone is not
// enough: a description often opens by quoting a single line of the input, or
// a single number, before showing the example proper, and a one-line listing
// is never the example. Where the guess is wrong the example is still shown on
// screen, so it is visible as a guess rather than silently used as a fact.
func exampleFrom(blocks []Block) string {
	var first string
	for _, b := range blocks {
		if b.Kind != Code {
			continue
		}
		if first == "" {
			first = b.Text
		}
		if strings.Contains(strings.TrimRight(b.Text, "\n"), "\n") {
			return b.Text
		}
	}
	return first
}

// renderBlocks turns one article into blocks.
//
// A linear scan rather than a tree walk, because the shape being read is flat:
// paragraphs, lists and listings, one after another. Nesting exists only for
// inline emphasis, and the answer for every inline tag is the same — keep the
// text, drop the tag — so there is nothing a tree would tell us.
func renderBlocks(article string) []Block {
	var out []Block
	var cur strings.Builder
	kind := Para

	flush := func() {
		text := strings.TrimSpace(squeeze(html.UnescapeString(cur.String())))
		cur.Reset()
		k := kind
		kind = Para
		if text != "" {
			out = append(out, Block{Kind: k, Text: text})
		}
	}

	for i := 0; i < len(article); {
		end, ok := tagAt(article, i)
		if !ok {
			cur.WriteByte(article[i])
			i++
			continue
		}
		name := tagName(article[i+1 : end])
		i = end + 1

		switch name {
		case "pre":
			// A listing runs to its closing tag and is kept exactly: the
			// alignment inside one is frequently the puzzle.
			rest := article[i:]
			stop := strings.Index(rest, "</pre>")
			if stop < 0 {
				stop = len(rest)
				i = len(article)
			} else {
				i += stop + len("</pre>")
			}
			flush()
			if code := trimBlankEdges(html.UnescapeString(tagRe.ReplaceAllString(rest[:stop], ""))); code != "" {
				out = append(out, Block{Kind: Code, Text: code})
			}
		case "h2", "h3":
			flush()
			kind = Heading
		case "li":
			flush()
			kind = Item
		case "/p", "/h2", "/h3", "/li", "/ul", "/ol", "/blockquote":
			flush()
		case "br":
			// A break somebody asked for, marked rather than written: the
			// newlines in the source are only how the page was typed, and
			// squeeze is about to flatten every one of them.
			cur.WriteByte(hardBreak)
		}
		// Everything else — <em>, <code>, <a>, <span>, the opening <p> — keeps
		// its text and loses its markup, which is the whole of what a terminal
		// can do with it.
	}
	flush()
	return out
}

// tagName is the element a tag opens or closes, lowercased: "p", "/p", "br"
// from `<br/>`, "a" from `<a href="…">`.
func tagName(tag string) string {
	tag = strings.TrimSuffix(strings.TrimSpace(tag), "/")
	if i := strings.IndexAny(tag, " \t\n"); i >= 0 {
		tag = tag[:i]
	}
	return strings.ToLower(tag)
}

// hardBreak stands in for a `<br>` while the text is being collected, so that
// the one line break the page asked for survives the flattening of the ones it
// did not.
const hardBreak = '\x00'

// squeeze collapses the whitespace HTML does not care about — which is all of
// it, newlines in the source included — and puts back the breaks a `<br>`
// asked for.
func squeeze(s string) string {
	s = spaceRe.ReplaceAllString(s, " ")
	var out []string
	for _, line := range strings.Split(s, string(hardBreak)) {
		out = append(out, strings.TrimSpace(line))
	}
	return strings.Join(out, "\n")
}

// tagAt reports whether a tag starts at i and where its '>' is.
//
// The question is worth asking because "while x < y" is prose, not markup, and
// AoC descriptions are full of it. A tag opens with a letter, a slash or a
// bang, and closes before the next '<' — anything else is a character that
// happens to be a bracket, and swallowing the rest of the paragraph on one
// would be a poor trade.
func tagAt(src string, i int) (end int, ok bool) {
	if src[i] != '<' || i+1 >= len(src) {
		return 0, false
	}
	switch c := src[i+1]; {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '/', c == '!':
	default:
		return 0, false
	}
	rest := src[i+1:]
	close := strings.IndexByte(rest, '>')
	if close < 0 {
		return 0, false
	}
	if open := strings.IndexByte(rest[:close], '<'); open >= 0 {
		return 0, false
	}
	return i + 1 + close, true
}

// trimBlankEdges drops the blank lines a `<pre>` picks up from being written
// on its own line in the source, without touching the indentation inside it.
func trimBlankEdges(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
