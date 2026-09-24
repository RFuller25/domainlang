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

// The word marker is the other way to write a comment, and it is dimmed the
// same way. A name that merely contains the word is not one, and neither is
// the word inside a string.
func TestHighlightTechnicallyComments(t *testing.T) {
	got := highlightSource("technically a note\nMaximum Technique: Sum", true)
	if !strings.Contains(got, styComment.Render("technically a note")) {
		t.Errorf("a technically comment was not dimmed: %q", got)
	}

	got = highlightSource("Reveal: stdout technically it prints", true)
	if !strings.Contains(got, styComment.Render("technically it prints")) {
		t.Errorf("a trailing technically comment was not dimmed: %q", got)
	}

	got = highlightSource(`Cursed Technique: Split Text by "technically"`, true)
	if strings.Contains(got, styComment.Render(`"technically"`)) {
		t.Errorf("the word inside a string was treated as a comment: %q", got)
	}

	src := "Maximum Technique: Count Matching\n    Consider technicallyOK As 1\n    Using: (n) -> n = technicallyOK"
	if plain := ansi.Strip(highlightSource(src, true)); plain != src {
		t.Errorf("a name containing the word was eaten as a comment:\n got: %q\nwant: %q", plain, src)
	}
}

// A declared name is painted as one, and so is the `:=` that writes it. What
// this pins is the *distinction*: the same word read rather than declared
// keeps the ordinary label colour, which is the whole point of the rule.
func TestHighlightDeclaredNames(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"a global", "Cursed Object: total As 0", "total"},
		{"a global being changed", "Cursed Tool: total As total + 1", "total"},
		{"a block-form declaration", "Cursed Object:\n    bump As 10", "bump"},
		{"a stage binding", "Maximum Technique: Count\n    Consider mean Of Sum", "mean"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := highlightSource(c.src, true)
			if !strings.Contains(got, styDeclName.Render(c.want)) {
				t.Errorf("%q was not painted as a declared name: %q", c.want, got)
			}
		})
	}

	// `Cursed Tool: total As total + 1` declares the first `total` and reads
	// the second. Only one of them is a declaration.
	got := highlightSource("Cursed Tool: total As total + 1", true)
	if !strings.Contains(got, styLabel.Render("total")) {
		t.Errorf("the read of the global lost its ordinary colour: %q", got)
	}

	// An operation phrase of the same shape is not a declaration: `Subsets of
	// 3` is an operation, and only the `As` spelling opens a block-form
	// declaration line.
	got = highlightSource("Cursed Technique: Explore\n    Subsets of 3", true)
	if strings.Contains(got, styDeclName.Render("Subsets")) {
		t.Errorf("an operation phrase was read as a declaration: %q", got)
	}
}

func TestHighlightWalrus(t *testing.T) {
	got := highlightSource("Cursed Technique: Map Each\n    Using: (n) -> n := n + 1", true)
	if !strings.Contains(got, styAssign.Render(":=")) {
		t.Errorf(":= was not painted as the write it is: %q", got)
	}
	if !strings.Contains(got, styDeclName.Render("n")) {
		t.Errorf("the walrus target was not painted as one: %q", got)
	}
}
