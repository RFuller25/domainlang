package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Highlighting paints; it never edits. Whatever the colors, stripping them
// must give back exactly the source that went in — otherwise a transcript
// would not be the program the session ran.
func TestHighlightPreservesTheText(t *testing.T) {
	sources := []string{
		"Cursed Energy: input.txt",
		`Cursed Technique: Split Text by "\n\n"`,
		"Channel \"squad\":\n    Maximum Technique: Count",
		"# a comment\nMaximum Technique: Sum",
		`Cursed Technique: Map Each` + "\n    Using: (x) -> x * 10",
		"Simple Domain: Repeat 3\n    Cursed Technique: Apply\n        Using: (v) -> v * 2",
	}
	for _, src := range sources {
		got := highlightSource(src, true)
		if plain := ansi.Strip(got); plain != src {
			t.Errorf("highlighting changed the text:\n got: %q\nwant: %q", plain, src)
		}
		if got == src {
			t.Errorf("nothing was highlighted in %q", src)
		}
	}
}

func TestHighlightIsOffWithoutColor(t *testing.T) {
	src := `Cursed Technique: Split Text by "\n"`
	if got := highlightSource(src, false); got != src {
		t.Errorf("color-free highlighting altered the line: %q", got)
	}
}

// Half-typed source is the normal case in a REPL, and the lexer will refuse
// most of it. That is not a reason to print nothing.
func TestHighlightLeavesUnlexableSourceAlone(t *testing.T) {
	for _, src := range []string{`Cursed Technique: Split Text by "unterminated`, "Cursed Technique: Split $"} {
		if got := highlightSource(src, true); got != src {
			t.Errorf("unlexable source was altered: %q", got)
		}
	}
}

// A keyword is colored where it is a keyword, and not where it is data. The
// lexer is what makes the difference visible.
func TestHighlightDistinguishesKeywordsFromStrings(t *testing.T) {
	got := highlightSource(`Cursed Technique: Split Text by "Cursed Technique"`, true)
	head, tail, ok := strings.Cut(got, ":")
	if !ok {
		t.Fatalf("no colon in %q", got)
	}
	if !strings.Contains(head, styKeyword.Render("Cursed")) {
		t.Errorf("the keyword was not painted as one: %q", head)
	}
	if strings.Contains(tail, styKeyword.Render("Cursed")) {
		t.Errorf("a keyword inside a string literal was painted as a keyword: %q", tail)
	}
}

// A comment is dimmed; a '#' inside a string is not a comment.
func TestHighlightComments(t *testing.T) {
	got := highlightSource("# note\nMaximum Technique: Sum", true)
	if !strings.Contains(got, styComment.Render("# note")) {
		t.Errorf("comment not dimmed: %q", got)
	}

	got = highlightSource(`Cursed Technique: Split Text by "#"`, true)
	if strings.Contains(got, styComment.Render(`"#"`)) {
		t.Errorf("a '#' inside a string was treated as a comment: %q", got)
	}
}
