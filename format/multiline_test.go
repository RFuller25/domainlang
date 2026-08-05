package format

import "testing"

// A `Using:` written across lines survives formatting: the block that continues
// it is re-indented like any other block, and the lines inside an open
// parenthesis keep the alignment the author gave them.
func TestMultiLineExpressionKeepsItsShape(t *testing.T) {
	src := "Cursed Energy: x\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (s, r) ->\n" +
		"        consider t as min(list(\n" +
		"            abs(s - r),\n" +
		"            abs(s + r)\n" +
		"        ))\n" +
		"        in if r = s\n" +
		"            then s - 1\n" +
		"            else t\n" +
		"Reveal: stdout\n"
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("already-canonical source was rewritten:\ngot:\n%s\nwant:\n%s", got, src)
	}
}

// The lines inside a parenthesis carry no layout tokens, so nothing can tell
// the formatter their depth. They move with the line that opened them instead,
// which keeps a call's hand-alignment intact while still normalizing the block.
func TestParenContinuationMovesWithItsOpeningLine(t *testing.T) {
	src := "Cursed Energy: x\n" +
		"Maximum Technique: Fold\n" +
		"  Seed: 0\n" +
		"  Using: (a, b) -> a + max(list(\n" +
		"        b,\n" +
		"        0 - b\n" +
		"      ))\n" +
		"Reveal: stdout\n"
	want := "Cursed Energy: x\n" +
		"Maximum Technique: Fold\n" +
		"    Seed: 0\n" +
		"    Using: (a, b) -> a + max(list(\n" +
		"          b,\n" +
		"          0 - b\n" +
		"        ))\n" +
		"Reveal: stdout\n"
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	twice, err := Format(got)
	if err != nil {
		t.Fatal(err)
	}
	if twice != got {
		t.Errorf("not idempotent:\n%s", twice)
	}
}

// A comment between the arguments of a broken-up call is inside the
// parenthesis too, so it travels with them rather than being pulled back to
// the enclosing block's indentation.
func TestCommentInsideAMultiLineCallStaysPut(t *testing.T) {
	src := "Cursed Energy: x\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (v) -> min(list(\n" +
		"        v,\n" +
		"        # the wrapped-around case\n" +
		"        0 - v\n" +
		"    ))\n" +
		"Reveal: stdout\n"
	got, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != src {
		t.Errorf("got:\n%s\nwant:\n%s", got, src)
	}
}
