package token

import "testing"

// The word marker is a word, and the whole of the claim is that it behaves
// like one: it opens a comment where a word can start and where none continues
// past it, and nowhere else. Every consumer in the repository asks this
// function rather than scanning for itself, so this is the one place the rule
// is pinned.
func TestCommentMarker(t *testing.T) {
	cases := []struct {
		src  string
		at   int
		want int
	}{
		{"# a comment", 0, 1},
		{"technically", 0, 11},
		{"Technically", 0, 11},
		{"TECHNICALLY, yes", 0, 11},
		{"technically fine", 0, 11},
		{"Reveal: stdout technically fine", 15, 11},

		// A word does not start inside another word, and does not end inside
		// one either.
		{"technicallyOK", 0, 0},
		{"technically_true", 0, 0},
		{"technically2", 0, 0},
		{"untechnically", 2, 0},
		{"x_technically", 2, 0},

		// Punctuation on either side is a boundary like any other.
		{"(technically)", 1, 11},
		{"technically:", 0, 11},

		// Nothing starts a comment here at all.
		{"Reveal: stdout", 0, 0},
		{"technical", 0, 0},
		{"", 0, 0},
		{"#", 1, 0},
		{"#", -1, 0},
	}
	for _, c := range cases {
		if got := CommentMarker(c.src, c.at); got != c.want {
			t.Errorf("CommentMarker(%q, %d) = %d, want %d", c.src, c.at, got, c.want)
		}
	}
}

// A marker inside a string literal is not a comment — the rule that keeps
// `Using: (c) -> c = "#"` intact, and now `Reveal: "technically"` too.
func TestCommentStartSkipsStrings(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{`Reveal: stdout`, -1},
		{`Reveal: stdout # why`, 15},
		{`Reveal: stdout technically why`, 15},
		{`Using: (c) -> c = "#"`, -1},
		{`Using: (c) -> c = "technically"`, -1},
		{`Using: (c) -> c = "#" # and a comment`, 22},
		{`Using: (c) -> c = "\"#" technically here`, 24},
		{`Match Pattern: "{a:int} # {b:int}"`, -1},
		// An unterminated literal swallows the rest of the line, which is a lex
		// error anyway; nothing after it is claimed as a comment.
		{`Reveal: "unterminated # no`, -1},
	}
	for _, c := range cases {
		if got := CommentStart(c.line); got != c.want {
			t.Errorf("CommentStart(%q) = %d, want %d", c.line, got, c.want)
		}
	}
}

func TestIsCommentLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"# a note", true},
		{"    # indented", true},
		{"technically a note", true},
		{"\tTechnically indented", true},
		{"", false},
		{"   ", false},
		{"Reveal: stdout", false},
		{"Reveal: stdout # trailing", false},
		{"technicallyOK As 1", false},
	}
	for _, c := range cases {
		if got := IsCommentLine(c.line); got != c.want {
			t.Errorf("IsCommentLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
