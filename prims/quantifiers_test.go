package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// ---------------------------------------------------------------------------
// Any / All
// ---------------------------------------------------------------------------

func TestAnyAndAll(t *testing.T) {
	cases := []struct {
		prim, pred, input string
		want              bool
	}{
		{"Any", "x > 4", "1,2,5", true},
		{"Any", "x > 9", "1,2,5", false},
		{"All", "x > 0", "1,2,5", true},
		{"All", "x > 1", "1,2,5", false},
	}
	for _, c := range cases {
		src := intsPrelude +
			"Maximum Technique: " + c.prim + "\n" +
			"    Using: (x) -> " + c.pred + "\n"
		v, _ := runPipeline(t, src, c.input)
		if v.(bool) != c.want {
			t.Fatalf("%s (%s) over %s: got %v want %v", c.prim, c.pred, c.input, v, c.want)
		}
	}
}

func TestAnyAndAllOnTheEmptyList(t *testing.T) {
	// The identities of the two connectives: nothing satisfies Any, nothing
	// violates All.
	base := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Filter\n" +
		"    Using: (x) -> x > 100\n"
	for _, c := range []struct {
		prim string
		want bool
	}{{"Any", false}, {"All", true}} {
		src := base + "Maximum Technique: " + c.prim + "\n    Using: (x) -> x > 0\n"
		v, _ := runPipeline(t, src, "1,2")
		if v.(bool) != c.want {
			t.Fatalf("%s of the empty list: got %v want %v", c.prim, v, c.want)
		}
	}
}

func TestAnyAndAllShortCircuit(t *testing.T) {
	// The predicate divides by the element, so a 0 later in the list is a
	// runtime error — reaching it proves the scan did not stop early. Any
	// stops at the first true and All at the first false, so neither gets
	// there; swapping the input so the 0 comes first makes both fail.
	anySrc := intsPrelude + "Maximum Technique: Any\n    Using: (x) -> 10 / x > 1\n"
	if v, err := runErr(t, anySrc, "5,0"); err != nil {
		t.Fatalf("Any should have stopped before the 0: %v", err)
	} else if v.(bool) != true {
		t.Fatalf("Any: got %v want true", v)
	}
	if _, err := runErr(t, anySrc, "0,5"); err == nil {
		t.Fatal("Any must still evaluate the first element")
	}

	allSrc := intsPrelude + "Maximum Technique: All\n    Using: (x) -> 10 / x > 100\n"
	if v, err := runErr(t, allSrc, "5,0"); err != nil {
		t.Fatalf("All should have stopped before the 0: %v", err)
	} else if v.(bool) != false {
		t.Fatalf("All: got %v want false", v)
	}
}

func TestAnyAgreesWithCountMatching(t *testing.T) {
	const input = "3,1,4,1,5"
	anyV, _ := runPipeline(t, intsPrelude+"Maximum Technique: Any\n    Using: (x) -> x > 4\n", input)
	cnt, _ := runPipeline(t, intsPrelude+"Maximum Technique: Count Matching\n    Using: (x) -> x > 4\n", input)
	if anyV.(bool) != (cnt.(int64) > 0) {
		t.Fatalf("Any %v disagrees with Count Matching %v", anyV, cnt)
	}
}

func TestAllPairsIsStillTheCombinationGenerator(t *testing.T) {
	// The `All` matcher must not swallow `All Pairs`.
	src := "stdin\n" +
		"Split Text by \",\"\n" +
		"Convert List to Integers\n" +
		"All Pairs\n" +
		"    Mode: Count\n" +
		"    Using: (a, b) -> a + b = 5\n"
	v, err := runErr(t, src, "1,2,3,4")
	if err != nil {
		t.Fatalf("bare All Pairs must still resolve: %v", err)
	}
	if v.(int64) != 2 {
		t.Fatalf("all pairs summing to 5: got %v want 2", v)
	}
}

func TestAllValuesIsStillABindingVow(t *testing.T) {
	// `All Values > n` is a vow shape, checked before the registry scan.
	src := "stdin\n" +
		"Split Text by \",\"\n" +
		"Convert List to Integers\n" +
		"All Values > 0\n" +
		"Sum\n"
	v, err := runErr(t, src, "1,2,3")
	if err != nil {
		t.Fatalf("bare All Values must still be a vow: %v", err)
	}
	if v.(int64) != 6 {
		t.Fatalf("vow must pass the value through: got %v want 6", v)
	}
}

// ---------------------------------------------------------------------------
// Find / Find Index
// ---------------------------------------------------------------------------

func TestFindReturnsTheFirstMatch(t *testing.T) {
	src := intsPrelude + "Maximum Technique: Find\n    Using: (x) -> x > 3\n"
	v, _ := runPipeline(t, src, "1,5,2,7")
	if v.(int64) != 5 {
		t.Fatalf("find: got %v want 5", v)
	}
}

func TestFindIndexReturnsMinusOneWhenAbsent(t *testing.T) {
	src := intsPrelude + "Maximum Technique: Find Index\n    Using: (x) -> x > 3\n"
	v, _ := runPipeline(t, src, "1,5,2,7")
	if v.(int64) != 1 {
		t.Fatalf("find index: got %v want 1", v)
	}
	v, _ = runPipeline(t, src, "1,2,3")
	if v.(int64) != -1 {
		t.Fatalf("find index (absent): got %v want -1", v)
	}
}

func TestFindWithNoMatchIsARuntimeError(t *testing.T) {
	src := intsPrelude + "Maximum Technique: Find\n    Using: (x) -> x > 100\n"
	_, err := runErr(t, src, "1,2,3")
	if err == nil || !strings.Contains(err.Error(), "no element satisfied") {
		t.Fatalf("expected a no-match error, got %v", err)
	}
}

func TestFindShortCircuits(t *testing.T) {
	// As in the Any/All case: the 0 that would fail the predicate is never
	// reached, because the match before it ends the scan.
	src := intsPrelude + "Maximum Technique: Find\n    Using: (x) -> 10 / x < 3\n"
	v, err := runErr(t, src, "5,0")
	if err != nil {
		t.Fatalf("Find should have stopped before the 0: %v", err)
	}
	if v.(int64) != 5 {
		t.Fatalf("find: got %v want 5", v)
	}
}

func TestFindIsNotFindCells(t *testing.T) {
	// Find Cells is the grid search and keeps its own matcher.
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert To Grid\n" +
		"Cursed Technique: Find Cells\n" +
		"    Using: (c) -> c = \"#\"\n"
	v, err := runErr(t, src, "#.\n.#")
	if err != nil {
		t.Fatalf("Find Cells must still resolve: %v", err)
	}
	if got := ir.FormatValue(v); got != "[[0, 0], [1, 1]]" {
		t.Fatalf("find cells: got %s", got)
	}
}

// ---------------------------------------------------------------------------
// Sum By / Product By
// ---------------------------------------------------------------------------

func TestSumByAndProductBy(t *testing.T) {
	sum, _ := runPipeline(t, intsPrelude+
		"Maximum Technique: Sum By\n    Using: (x) -> x * x\n", "1,2,3")
	if sum.(int64) != 14 {
		t.Fatalf("sum by: got %v want 14", sum)
	}
	prod, _ := runPipeline(t, intsPrelude+
		"Maximum Technique: Product By\n    Using: (x) -> x + 1\n", "1,2,3")
	if prod.(int64) != 24 {
		t.Fatalf("product by: got %v want 24", prod)
	}
}

func TestSumByAgreesWithMapThenSum(t *testing.T) {
	const input = "4,8,15,16,23,42"
	by, _ := runPipeline(t, intsPrelude+
		"Maximum Technique: Sum By\n    Using: (x) -> x * 3\n", input)
	mapped, _ := runPipeline(t, intsPrelude+
		"Cursed Technique: Map Each\n    Using: (x) -> x * 3\n"+
		"Maximum Technique: Sum\n", input)
	if by != mapped {
		t.Fatalf("Sum By %v != Map Each + Sum %v", by, mapped)
	}
}

func TestKeyedArithmeticIdentitiesOnEmpty(t *testing.T) {
	base := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Cursed Technique: Filter\n" +
		"    Using: (x) -> x > 100\n"
	for _, c := range []struct {
		prim string
		want int64
	}{{"Sum By", 0}, {"Product By", 1}} {
		v, _ := runPipeline(t, base+"Maximum Technique: "+c.prim+"\n    Using: (x) -> x\n", "1,2")
		if v.(int64) != c.want {
			t.Fatalf("%s of the empty list: got %v want %v", c.prim, v, c.want)
		}
	}
}

func TestSumByIsNotSumEachGroup(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\\n\"\n" +
		"Cursed Technique: Split Each by \"\\n\"\n" +
		"Channeled Energy: Convert Each List to Integers\n" +
		"Maximum Technique: Sum Each Group\n"
	v, err := runErr(t, src, "1\n2\n\n3")
	if err != nil {
		t.Fatalf("Sum Each Group must still resolve: %v", err)
	}
	if got := ir.FormatValue(v); got != "[3, 3]" {
		t.Fatalf("sum each group: got %s", got)
	}
}

func TestKeyedArithmeticRequiresAnIntKey(t *testing.T) {
	src := intsPrelude + "Maximum Technique: Sum By\n    Using: (x) -> x > 1\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "must return Int") {
		t.Fatalf("expected an Int-key error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Zip With
// ---------------------------------------------------------------------------

const zipChannels = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \";\"\n" +
	"\n" +
	"Channel \"a\":\n" +
	"    Cursed Technique: Take Item 0\n" +
	"    Cursed Technique: Split Text by \",\"\n" +
	"    Channeled Energy: Convert List to Integers\n" +
	"\n" +
	"Channel \"b\":\n" +
	"    Cursed Technique: Take Item 1\n" +
	"    Cursed Technique: Split Text by \",\"\n" +
	"    Channeled Energy: Convert List to Integers\n" +
	"\n"

func TestZipWithCombinesDirectly(t *testing.T) {
	src := zipChannels +
		"Maximum Technique: Zip\n" +
		"    From: a, b\n" +
		"    Using: (x, y) -> x * y\n"
	v, _ := runPipeline(t, src, "1,2,3;4,5,6")
	if got := ir.FormatValue(v); got != "[4, 10, 18]" {
		t.Fatalf("zip with: got %s want [4, 10, 18]", got)
	}
}

func TestZipWithTruncatesToTheShorterChannel(t *testing.T) {
	src := zipChannels +
		"Maximum Technique: Zip\n" +
		"    From: a, b\n" +
		"    Using: (x, y) -> x + y\n"
	v, _ := runPipeline(t, src, "1,2,3;10,20")
	if got := ir.FormatValue(v); got != "[11, 22]" {
		t.Fatalf("zip with (ragged): got %s want [11, 22]", got)
	}
}

func TestZipWithoutALambdaStillGivesTuples(t *testing.T) {
	src := zipChannels + "Maximum Technique: Zip\n    From: a, b\n"
	v, _ := runPipeline(t, src, "1,2;3,4")
	if got := ir.FormatValue(v); got != "[[1, 3], [2, 4]]" {
		t.Fatalf("plain zip: got %s", got)
	}
}

func TestZipWithAgreesWithZipThenMap(t *testing.T) {
	const input = "1,2,3;4,5,6"
	with, _ := runPipeline(t, zipChannels+
		"Maximum Technique: Zip\n    From: a, b\n    Using: (x, y) -> x * y\n", input)
	mapped, _ := runPipeline(t, zipChannels+
		"Maximum Technique: Zip\n    From: a, b\n"+
		"Cursed Technique: Map Each\n    Using: (p) -> prow(p) * pcol(p)\n", input)
	if ir.FormatValue(with) != ir.FormatValue(mapped) {
		t.Fatalf("Zip With %s != Zip + Map Each %s", ir.FormatValue(with), ir.FormatValue(mapped))
	}
}

func TestZipWithArityIsChecked(t *testing.T) {
	src := zipChannels +
		"Maximum Technique: Zip\n" +
		"    From: a, b\n" +
		"    Using: (p) -> p\n"
	if _, err := resolveSrc(t, src); err == nil ||
		!strings.Contains(err.Error(), "2 parameters") {
		t.Fatalf("expected an arity error, got %v", err)
	}
}
