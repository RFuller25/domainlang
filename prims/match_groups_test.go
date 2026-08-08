package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Template groups end to end. Phase 5 closed repetition for *holes* and wrote
// down what that left: a repeated hole is not a repeated group, and a {text}
// sponge is not an optional one. These are the two shapes that residue named.

// AoC 2017 D7. Before optional groups the only single-pass spelling was a
// trailing {rest:text} plus a hand-written re-parse in the expression layer,
// because {text} is `.*` and so matches empty — a de-facto optional, but an
// untyped one that hands back raw text.
func TestOptionalGroupParsesBothLineShapes(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"{name:word} ({w:int})[? -> {kids:word+ sep=\\\", \\\"}]\"\n"
	v, _ := runPipeline(t, src, "pbga (66)\nfwft (72) -> ktlj, cntj, xhth")
	want := "[{name: pbga, w: 66, kids: []}, {name: fwft, w: 72, kids: [ktlj, cntj, xhth]}]"
	if got := ir.FormatValue(v); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// An absent group leaves each hole at its type's zero — which is the whole of
// "absent" in a language with no sum types, and the reason {?flag} exists.
func TestAbsentHolesTakeTheirZero(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"{v:int}[?/{d:int}][?:{s:word}]\"\n"
	v, _ := runPipeline(t, src, "1/2:x\n3")
	want := "[{v: 1, d: 2, s: x}, {v: 3, d: 0, s: }]"
	if got := ir.FormatValue(v); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// The flag is the answer to "the zero and a real zero look the same".
func TestPresenceFlagDistinguishesZeroFromAbsent(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"{v:int}[? (was {n:int}){?changed}]\"\n"
	v, _ := runPipeline(t, src, "7 (was 0)\n7")
	want := "[{v: 7, n: 0, changed: true}, {v: 7, n: 0, changed: false}]"
	if got := ir.FormatValue(v); got != want {
		t.Fatalf("the two lines differ only in the flag; got %s\nwant %s", got, want)
	}
}

// AoC 2023 D2: a repeating {int} {word} *pair*. The element type is the inner
// template's own output type, so a named group gives List<Record>.
func TestRepeatedGroupYieldsAListOfRecords(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"Game {id:int}: {draws:( {n:int} {color:word} )+ sep=\\\", \\\"}\"\n"
	v, _ := runPipeline(t, src, "Game 1: 3 blue, 4 red\nGame 2: 1 red")
	want := "[{id: 1, draws: [{n: 3, color: blue}, {n: 4, color: red}]}, " +
		"{id: 2, draws: [{n: 1, color: red}]}]"
	if got := ir.FormatValue(v); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// The inner template's named-ness is its own: a positional one gives a tuple
// per element under the same homogeneity rule a top-level template uses, while
// the outer hole here is named and so the line is still a Record.
func TestAnInnerTemplateIsNamedOrPositionalOnItsOwn(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"{ps:( {int},{int} )+ sep=\\\" \\\"}\"\n"
	v, _ := runPipeline(t, src, "1,2 3,4\n5,6")
	if got := ir.FormatValue(v); got != "[{ps: [[1, 2], [3, 4]]}, {ps: [[5, 6]]}]" {
		t.Fatalf("got %s", got)
	}
}

// A bare `[` is an ordinary character. AoC input is full of them, and a
// template that matched "[1,2]" before groups existed has to keep doing so —
// which is why only `[?` opens a group.
func TestABareBracketIsStillALiteral(t *testing.T) {
	src := linesHead +
		"Cursed Technique: Match Pattern\n" +
		"    Mode: Each\n" +
		"    Using: \"[{a:int},{b:int}]\"\n"
	v, _ := runPipeline(t, src, "[1,2]\n[3,4]")
	if got := ir.FormatValue(v); got != "[{a: 1, b: 2}, {a: 3, b: 4}]" {
		t.Fatalf("got %s", got)
	}
}

// A group's own refusals reach the user as resolve errors with a position,
// because the template is a literal and is checked before the program runs.
func TestGroupRefusalsAreResolveErrors(t *testing.T) {
	for _, tc := range []struct{ tmpl, want string }{
		{`{a:( {b:int} )}`, "groups without repeating it"},
		{`{a:( {b:int} )+}`, "names no separator"},
		{`{?loose} {a:int}`, "only means something inside an optional group"},
		{`{a:int}[? plain text]`, "records whether it matched"},
		{`{a:int}[? -> {b:int}`, "unterminated optional group"},
	} {
		src := linesHead +
			"Cursed Technique: Match Pattern\n    Mode: Each\n" +
			"    Using: \"" + strings.ReplaceAll(tc.tmpl, `"`, `\"`) + "\"\nReveal: stdout\n"
		_, err := runErr(t, src, "x")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected an error containing %q, got %v", tc.tmpl, tc.want, err)
		}
	}
}
