package prims

import (
	"strings"
	"testing"
)

const ints = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \",\"\n" +
	"Channeled Energy: Convert List to Integers\n"

func TestAllPairsFirstThenProduct(t *testing.T) {
	src := ints +
		"Domain Expansion: All Pairs\n    Mode: First\n    Using: (a, b) -> a + b = 2020\n" +
		"Maximum Technique: Product\n"
	v, _ := runPipeline(t, src, "1721,979,366,299,675,1456")
	if v.(int64) != 514579 { // 1721 * 299
		t.Fatalf("all pairs first product: got %v want 514579", v)
	}
}

func TestAllPairsCount(t *testing.T) {
	src := ints + "Domain Expansion: All Pairs\n    Mode: Count\n    Using: (a, b) -> a + b = 5\n"
	v, _ := runPipeline(t, src, "1,2,3,4") // (1,4) and (2,3)
	if v.(int64) != 2 {
		t.Fatalf("all pairs count: got %v want 2", v)
	}
}

func TestAllPairsMap(t *testing.T) {
	src := ints +
		"Domain Expansion: All Pairs\n    Mode: Map\n    Using: (a, b) -> a + b\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1,2,3") // pair sums 3,4,5
	if v.(int64) != 12 {
		t.Fatalf("all pairs map then sum: got %v want 12", v)
	}
}

func TestCombinationsThreeFirst(t *testing.T) {
	src := ints +
		"Domain Expansion: Combinations 3\n    Mode: First\n    Using: (a, b, c) -> a + b + c = 6\n" +
		"Maximum Technique: Product\n"
	v, _ := runPipeline(t, src, "1,2,3,4") // first triple summing to 6 is 1+2+3
	if v.(int64) != 6 {
		t.Fatalf("combinations 3 product: got %v want 6", v)
	}
}

func TestAllPairsNoMatchIsRuntimeError(t *testing.T) {
	src := ints + "Domain Expansion: All Pairs\n    Mode: First\n    Using: (a, b) -> a + b = 999\n"
	_, err := runErr(t, src, "1,2,3")
	if err == nil || !strings.Contains(err.Error(), "no combination satisfied") {
		t.Fatalf("expected no-match error, got %v", err)
	}
}

func TestPairsResolveErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			"invalid mode",
			ints + "Domain Expansion: All Pairs\n    Mode: Frobnicate\n    Using: (a, b) -> a + b = 1\n",
			"Mode must be",
		},
		{
			"filter predicate not bool",
			ints + "Domain Expansion: All Pairs\n    Mode: Filter\n    Using: (a, b) -> a + b\n",
			"predicate must return Bool",
		},
		{
			"combinations missing size",
			ints + "Domain Expansion: Combinations\n    Mode: First\n    Using: (a) -> a = 1\n",
			"requires a size",
		},
		{
			"wrong lambda arity",
			ints + "Domain Expansion: All Pairs\n    Mode: First\n    Using: (a) -> a = 1\n",
			"must take 2 parameter",
		},
	}
	for _, c := range cases {
		_, err := resolveSrc(t, c.src)
		if err == nil {
			t.Fatalf("%s: expected resolve error", c.name)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error %q does not contain %q", c.name, err.Error(), c.want)
		}
	}
}
