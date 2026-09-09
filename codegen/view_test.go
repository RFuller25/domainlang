package codegen_test

import (
	"testing"

	"domain/codegen"
)

// The View type has two independent implementations — ir/view.go for the
// interpreter, codegen/viewgen.go for the emitted program — because codegen
// generates its own runtime rather than importing this repository's. That is
// the house pattern (see sparsegen), and these are what keep the two honest.
//
// Geometry is the thing to watch. If the two layouts ever disagree, a column
// lands one place over, every subsequent line differs, and the diff says
// nothing about which side is wrong.
func TestViewMatchesInterpreter(t *testing.T) {
	cases := []struct {
		name  string
		expr  string
		input string
	}{
		{"text of a number", `text(sum(xs))`, "1,2,3"},
		{"text of text", `text("hello")`, "1"},
		{"blank", `blank()`, "1"},
		{"stack", `stack(text("one"), text("two"), text(3))`, "1"},
		{"beside", `beside(text("ab"), text("|"), text("cd"))`, "1"},
		{"beside ragged", `beside(stack(text("a"), text("bbbb")), text("|"), text("X"))`, "1"},
		{"box", `box(text("hi"))`, "1"},
		{"nested boxes", `box(box(box(text(sum(xs)))))`, "4,5"},
		{"margin", `box(margin(text("x"), 2, 1))`, "1"},
		{"align left", `beside(align(text("ab"), 6, 1, "left"), text("|"))`, "1"},
		{"align center", `beside(align(text("ab"), 6, 1, "center"), text("|"))`, "1"},
		{"align right", `beside(align(text("ab"), 6, 1, "right"), text("|"))`, "1"},
		{"align never clips", `align(text("abcdef"), 2, 1, "center")`, "1"},
		{"fit", `fit(text("abc"), 2, 1)`, "1"},
		{"style does not move anything", `beside(style(text("ab"), "bold red on blue"), text("|"))`, "1"},
		{"nested style", `style(stack(text("a"), style(text("b"), "green")), "red bold")`, "1"},
		{"multi-line text", `box(text("ab\ncdef\ng"))`, "1"},
		{"empty stack", `box(stack())`, "1"},
		{"empty beside", `box(beside())`, "1"},
		{"a panel of everything", `box(margin(stack(
			style(text("TITLE"), "bold underline"),
			beside(text("left "), box(text(sum(xs))), text(" right")),
			align(text("end"), 20, 1, "center")), 1, 1))`, "7,8,9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "Cursed Energy: stdin\n" +
				"Cursed Technique: Split Text by \",\"\n" +
				"Channeled Energy: Convert To Integers\n" +
				"Cursed Technique: Apply\n" +
				"    Using: (xs) -> " + c.expr + "\n" +
				"Reveal: stdout\n"
			for _, opt := range []bool{true, false} {
				pipe := compilePipeline(t, src, opt)
				want := runInterpreter(t, pipe, []byte(c.input))
				got := buildAndRun(t, pipe, []byte(c.input), codegen.Options{})
				if got != want {
					t.Errorf("optimize=%v: binary and interpreter disagree\n--- binary ---\n%s\n--- interpreter ---\n%s",
						opt, got, want)
				}
			}
		})
	}
}

// draw() takes the three shapes a board arrives in, and each one has its own
// emitted traversal.
func TestDrawMatchesInterpreter(t *testing.T) {
	cases := []struct{ name, src, input string }{
		{"a Grid of text", "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \"\\n\"\n" +
			"Channeled Energy: Convert To Grid\n" +
			"Cursed Technique: Map Cells\n" +
			"    Using: (c) -> if c = \"#\" then \"@\" else \".\"\n" +
			"Cursed Technique: Apply\n" +
			"    Using: (g) -> box(draw(g))\n" +
			"Reveal: stdout\n", "#..#\n.##.\n#..#"},
		{"a Grid of ints", "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \"\\n\"\n" +
			"Channeled Energy: Convert To Grid\n" +
			"Cursed Technique: Map Cells\n" +
			"    Using: (c) -> toint(c)\n" +
			"Cursed Technique: Apply\n" +
			"    Using: (g) -> box(draw(g))\n" +
			"Reveal: stdout\n", "123\n456"},
		{"a List of rows", "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \"\\n\"\n" +
			"Cursed Technique: Split Each by \"\"\n" +
			"Cursed Technique: Apply\n" +
			"    Using: (rows) -> box(draw(rows))\n" +
			"Reveal: stdout\n", "ab\ncd"},
		{"a List of lines", "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \"\\n\"\n" +
			"Cursed Technique: Apply\n" +
			"    Using: (lines) -> box(draw(lines))\n" +
			"Reveal: stdout\n", "one\ntwo"},
		{"a Sparse plane", "Cursed Energy: stdin\n" +
			"Cursed Technique: Split Text by \"\\n\"\n" +
			"Channeled Energy: Convert To Grid\n" +
			"Channeled Energy: Convert To Sparse Grid\n" +
			"    Default: \".\"\n" +
			"Cursed Technique: Apply\n" +
			"    Using: (s) -> box(draw(s))\n" +
			"Reveal: stdout\n", "ab\ncd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, opt := range []bool{true, false} {
				pipe := compilePipeline(t, c.src, opt)
				want := runInterpreter(t, pipe, []byte(c.input))
				got := buildAndRun(t, pipe, []byte(c.input), codegen.Options{})
				if got != want {
					t.Errorf("optimize=%v: binary and interpreter disagree\n--- binary ---\n%s\n--- interpreter ---\n%s",
						opt, got, want)
				}
			}
		})
	}
}
