package aoc

import (
	"strings"
	"testing"
)

// blocksOf renders one article and returns its blocks, which is what every
// test in this file is about.
func blocksOf(t *testing.T, page string) []Block {
	t.Helper()
	p := parsePuzzle(2023, 7, page)
	if p.Unlocked() == 0 {
		t.Fatal("no parts came out of the page")
	}
	return p.Part(1)
}

func TestTheDescriptionBecomesBlocks(t *testing.T) {
	blocks := blocksOf(t, pageOnePart)

	var kinds []BlockKind
	for _, b := range blocks {
		kinds = append(kinds, b.Kind)
	}
	want := []BlockKind{Heading, Para, Para, Code, Para, Item, Item}
	if len(kinds) != len(want) {
		t.Fatalf("got %d blocks %v, want %d %v", len(kinds), kinds, len(want), want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("block %d is kind %d, want %d (%q)", i, kinds[i], want[i], blocks[i].Text)
		}
	}
}

// Emphasis and inline code are the site's own markup, and a terminal cannot
// show them — but the words inside them are half the puzzle, so they survive.
func TestInlineMarkupKeepsItsWords(t *testing.T) {
	blocks := blocksOf(t, pageOnePart)
	joined := ""
	for _, b := range blocks {
		joined += b.Text + "\n"
	}
	for _, word := range []string{"ride", "Camel Cards", "total winnings"} {
		if !strings.Contains(joined, word) {
			t.Errorf("the description lost %q, which was inside an <em> or a <code>", word)
		}
	}
	if strings.Contains(joined, "<em>") || strings.Contains(joined, "</code>") {
		t.Errorf("a tag survived into the text:\n%s", joined)
	}
}

// Entities are decoded, because a puzzle that reads "&gt; 0 &amp; &lt; 100" is
// a puzzle nobody can read.
func TestEntitiesAreDecoded(t *testing.T) {
	blocks := blocksOf(t, pageOnePart)
	for _, b := range blocks {
		if strings.Contains(b.Text, "&gt;") || strings.Contains(b.Text, "&amp;") {
			t.Errorf("an entity survived: %q", b.Text)
		}
	}
	found := false
	for _, b := range blocks {
		if strings.Contains(b.Text, "> 0 & < 100") {
			found = true
		}
	}
	if !found {
		t.Error("the decoded text is not there at all")
	}
}

// A listing is the worked example and is the one place whitespace means
// something. It comes through exactly as it was written.
func TestAListingKeepsItsShape(t *testing.T) {
	page := `<article><p>like this:</p><pre><code>  indented
....#
#....
</code></pre></article>`
	blocks := blocksOf(t, page)
	var code string
	for _, b := range blocks {
		if b.Kind == Code {
			code = b.Text
		}
	}
	if code != "  indented\n....#\n#...." {
		t.Errorf("the listing was reshaped:\n%q", code)
	}
}

// The example is the first listing with more than one line: descriptions open
// by quoting a single line or a single number often enough that "first" alone
// picks the wrong one.
func TestTheExampleIsTheFirstMultiLineListing(t *testing.T) {
	page := `<article><p>A line looks like <code>x</code>:</p>
<pre><code>32T3K 765</code></pre>
<p>For example:</p>
<pre><code>32T3K 765
T55J5 684
KK677 28</code></pre></article>`
	p := parsePuzzle(2023, 7, page)
	if !strings.Contains(p.Example, "T55J5") {
		t.Errorf("example = %q, want the three-line listing", p.Example)
	}
}

// With nothing but one-line listings, the first one is still better than
// nothing — the screen shows what was chosen, so a wrong guess is visible.
func TestASingleLineListingIsStillOffered(t *testing.T) {
	page := `<article><pre><code>only-line</code></pre></article>`
	if got := parsePuzzle(2023, 7, page).Example; got != "only-line" {
		t.Errorf("example = %q, want the only listing there is", got)
	}
}

func TestASolvedDayCarriesItsAnswers(t *testing.T) {
	p := parsePuzzle(2023, 7, pageTwoParts)
	if p.Unlocked() != 2 {
		t.Fatalf("unlocked = %d, want 2", p.Unlocked())
	}
	if p.Solved() != 1 {
		t.Fatalf("solved = %d, want 1", p.Solved())
	}
	if got, ok := p.Answer(1); !ok || got != "250120186" {
		t.Errorf("answer(1) = %q (%v)", got, ok)
	}
	if _, ok := p.Answer(2); ok {
		t.Error("answer(2) exists, and part two has not been solved")
	}
	if p.Working() != 2 {
		t.Errorf("working part = %d, want 2 — part one is done", p.Working())
	}
}

// The part to be looking at is the first one without an answer, and the last
// one when the day is finished.
func TestWorkingPart(t *testing.T) {
	cases := []struct {
		parts, answers, want int
	}{
		{1, 0, 1},
		{2, 1, 2},
		{2, 2, 2},
		{0, 0, 1},
	}
	for _, c := range cases {
		p := &Puzzle{}
		for range c.parts {
			p.Parts = append(p.Parts, nil)
		}
		for range c.answers {
			p.Answers = append(p.Answers, "x")
		}
		if got := p.Working(); got != c.want {
			t.Errorf("%d parts, %d answers: working = %d, want %d", c.parts, c.answers, got, c.want)
		}
	}
}

func TestDayNamePadsTheDay(t *testing.T) {
	if got := DayName(2023, 7); got != "2023-07" {
		t.Errorf("DayName(2023, 7) = %q, want %q", got, "2023-07")
	}
	if got := DayName(2023, 25); got != "2023-25" {
		t.Errorf("DayName(2023, 25) = %q, want %q", got, "2023-25")
	}
}

// A stray '<' in the prose is not a tag, and must not swallow the rest of the
// description. AoC descriptions do contain them.
func TestAStrayAngleBracketIsText(t *testing.T) {
	page := `<article><p>while x < y, keep going</p><p>then stop</p></article>`
	blocks := blocksOf(t, page)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2: %v", len(blocks), blocks)
	}
	if !strings.Contains(blocks[0].Text, "x < y") {
		t.Errorf("the comparison was eaten: %q", blocks[0].Text)
	}
}

// A <br> is a line break somebody asked for, unlike the newlines in the
// source, which are just how the page was typed.
func TestLineBreaksAreKeptAndSourceNewlinesAreNot(t *testing.T) {
	page := "<article><p>one\n   two</p><p>above<br>below</p></article>"
	blocks := blocksOf(t, page)
	if blocks[0].Text != "one two" {
		t.Errorf("paragraph = %q, want the source newline squeezed", blocks[0].Text)
	}
	if blocks[1].Text != "above\nbelow" {
		t.Errorf("paragraph = %q, want the <br> kept", blocks[1].Text)
	}
}
