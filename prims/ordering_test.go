package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// Min By and Max By take any ir.Ordered key, exactly as Sort By does. The two
// used to disagree — Sort By took Text and tuple keys while these took Int
// only — which made "the alphabetically first X" unwritable for no reason the
// user could see, since it is the same notion of key over the same ordering.

func TestMinByMaxByOverATextKey(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Maximum Technique: %s\n" +
		"    Using: (w) -> w\n"
	for _, tc := range []struct{ prim, want string }{
		{"Max By", "cherry"},
		{"Min By", "apple"},
	} {
		v, _ := runPipeline(t, strings.Replace(src, "%s", tc.prim, 1), "banana,apple,cherry")
		if v != tc.want {
			t.Errorf("%s over a text key: got %v, want %v", tc.prim, v, tc.want)
		}
	}
}

func TestMinByMaxByOverATupleKey(t *testing.T) {
	// The tiebreak shape: order by the first component, then the second.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Match Pattern\n" +
		"    Using: \"{w:word} {n:int}\"\n" +
		"Maximum Technique: %s\n" +
		"    Using: (r) -> tuple(r.w, r.n)\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (r) -> r.w + \":\" + totext(r.n)\n"
	input := "b 1\na 9\nb 0\na 2"
	for _, tc := range []struct{ prim, want string }{
		{"Max By", "b:1"},
		{"Min By", "a:2"},
	} {
		v, _ := runPipeline(t, strings.Replace(src, "%s", tc.prim, 1), input)
		if v != tc.want {
			t.Errorf("%s over a tuple key: got %v, want %v", tc.prim, v, tc.want)
		}
	}
}

// The first element wins a tie, which is what makes the result the same one
// Sort By would put at that end.
func TestMinByMaxByKeepFirstOnATie(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Maximum Technique: %s\n" +
		"    Using: (w) -> charat(w, 0)\n"
	for _, prim := range []string{"Max By", "Min By"} {
		v, _ := runPipeline(t, strings.Replace(src, "%s", prim, 1), "ax,ay,az")
		if v != "ax" {
			t.Errorf("%s on a tie: got %v, want ax", prim, v)
		}
	}
}

// An unordered key is refused, and the message names the rule rather than
// naming Int — a Record is keyable but has no ordering, and that distinction
// is the thing a user has to learn here.
func TestMinByMaxByRefuseAnUnorderedKey(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Maximum Technique: Max By\n" +
		"    Using: (w) -> chars(w)\n"
	_, err := runErr(t, src, "ab,cd")
	if err == nil || !strings.Contains(err.Error(), "must return an ordered type") {
		t.Fatalf("expected an ordered-key error, got %v", err)
	}
}

// Whatever Min By and Max By pick, a Sort By over the same key puts at that
// end. One ordering, reached two ways — the property that was false before.
func TestKeyedExtremaAgreeWithSortBy(t *testing.T) {
	const input = "pear,fig,apple,fig,quince,date"
	head := "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\n"
	key := "    Using: (w) -> tuple(length(w), w)\n"

	sorted, _ := runPipeline(t, head+"Domain Expansion: Sort By\n"+key, input)
	xs, err := ir.AsList(sorted)
	if err != nil || len(xs) == 0 {
		t.Fatalf("Sort By did not produce a list: %v (%v)", sorted, err)
	}
	minV, _ := runPipeline(t, head+"Maximum Technique: Min By\n"+key, input)
	maxV, _ := runPipeline(t, head+"Maximum Technique: Max By\n"+key, input)
	if minV != xs[0] {
		t.Errorf("Min By = %v, but Sort By starts with %v", minV, xs[0])
	}
	if maxV != xs[len(xs)-1] {
		t.Errorf("Max By = %v, but Sort By ends with %v", maxV, xs[len(xs)-1])
	}
}
