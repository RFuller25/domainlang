package prims

import (
	"fmt"
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

func TestAllPairsAnyAll(t *testing.T) {
	cases := []struct {
		name, mode, lam, input, want string
	}{
		{"any hit", "Any", "a + b = 5", "1,2,3,4", "true"},
		{"any miss", "Any", "a + b = 99", "1,2,3,4", "false"},
		{"all hold", "All", "a < b", "1,2,3,4", "true"},
		{"all fail", "All", "a + b < 6", "1,2,3,4", "false"},
		{"any, one element", "Any", "a = a", "7", "false"},
		{"all, one element", "All", "a = 99", "7", "true"},
	}
	for _, c := range cases {
		src := ints + "Domain Expansion: All Pairs\n    Mode: " + c.mode + "\n    Using: (a, b) -> " + c.lam + "\n"
		v, _ := runPipeline(t, src, c.input)
		if got := fmt.Sprint(v); got != c.want {
			t.Errorf("%s: got %v want %s", c.name, v, c.want)
		}
	}
	// Combinations k: too few elements for a combination.
	v, _ := runPipeline(t, ints+"Domain Expansion: Combinations 3\n    Mode: Any\n    Using: (a, b, c) -> a = a\n", "1,2")
	if v != false {
		t.Errorf("Combinations 3 Any over 2 elements: got %v", v)
	}
	v, _ = runPipeline(t, ints+"Domain Expansion: Combinations 3\n    Mode: All\n    Using: (a, b, c) -> a = 99\n", "1,2")
	if v != true {
		t.Errorf("Combinations 3 All over 2 elements: got %v", v)
	}
}

// Any and All stop early: a predicate that would fail on a later combination
// is never asked about it.
func TestAllPairsAnyAllShortCircuit(t *testing.T) {
	v, _ := runPipeline(t, ints+"Domain Expansion: All Pairs\n    Mode: Any\n    Using: (a, b) -> a = 1 or 10 / (a - 3) > 0\n", "1,2,3")
	if v != true {
		t.Errorf("Any: got %v", v)
	}
	v, _ = runPipeline(t, ints+"Domain Expansion: All Pairs\n    Mode: All\n    Using: (a, b) -> a = 2 and 10 / (a - 3) > 0\n", "1,2,3")
	if v != false {
		t.Errorf("All: got %v", v)
	}
}

func TestAllPairsAnyAllErrors(t *testing.T) {
	for _, mode := range []string{"Any", "All"} {
		_, err := resolveSrc(t, ints+"Domain Expansion: All Pairs\n    Mode: "+mode+"\n    Using: (a, b) -> a + b\n")
		if err == nil || !strings.Contains(err.Error(), "predicate must return Bool") {
			t.Errorf("%s with a non-Bool lambda: got %v", mode, err)
		}
	}
	_, err := resolveSrc(t, ints+"Domain Expansion: All Pairs\n    Mode: Some\n    Using: (a, b) -> a = b\n")
	if err == nil || !strings.Contains(err.Error(), "Filter, Count, First, Map, Any, or All") {
		t.Errorf("bad mode message: got %v", err)
	}
}
