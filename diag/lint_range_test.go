package diag

import "testing"

// `Simple Domain: For k in range(4)` is the loop's own source syntax, and the
// primitive reads that call rather than discarding it — so the phrase-
// expression warning must not fire on it. It did, on every line of the first
// program that used the form, which is how this was found.
func TestForRangeIsNotAPhraseExpression(t *testing.T) {
	src := `Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {xs: list(1, 2, 3, 4), n: 0}

Part Every 100:
    Simple Domain: For k in range(4)
        Cursed Technique: Apply
            Using: (w, k) -> with(w, "n", w.n + item(w.xs, k))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.n))
`
	r := analyze(t, src)
	if d := diagWith(r, Warning, "looks like an expression"); d != nil {
		t.Errorf("the For loop's own syntax was warned about: %s", d.Msg)
	}
}

// And the mistake the rule exists for still fires: a call written into a
// phrase that does discard it.
func TestPhraseExpressionStillWarns(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Windows length(xs) / 2
Reveal: stdout
`
	r := analyze(t, src)
	if diagWith(r, Warning, "looks like an expression") == nil {
		t.Error("a call written into an ordinary phrase should still warn")
	}
}
