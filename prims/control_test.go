package prims

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"domain/ir"
)

func TestRepeat(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Repeat 3\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n * 2\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1\n2\n3") // [1,2,3] -> *2 thrice -> [8,16,24]
	if v.(int64) != 48 {
		t.Fatalf("repeat: got %v want 48", v)
	}
}

func TestWhile(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
		"Simple Domain: While\n    Using: (n) -> n > 1\n" +
		"    Cursed Technique: Apply\n        Using: (n) -> n / 2\n"
	v, _ := runPipeline(t, src, "60\n40") // sum 100 -> halve until <=1 -> 1
	if v.(int64) != 1 {
		t.Fatalf("while: got %v want 1", v)
	}
}

func TestFixedPoint(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Iterate Until Fixed Point\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n / 2\n"
	v, _ := runPipeline(t, src, "4\n8") // converges to [0,0]
	xs := v.([]ir.Value)
	if len(xs) != 2 || xs[0].(int64) != 0 || xs[1].(int64) != 0 {
		t.Fatalf("fixed point: got %v want [0 0]", v)
	}
}

func TestReverse(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nReverse Cursed Technique: Reverse\n"
	v, _ := runPipeline(t, src, "1\n2\n3")
	xs := v.([]ir.Value)
	if len(xs) != 3 || xs[0].(int64) != 3 || xs[2].(int64) != 1 {
		t.Fatalf("reverse: got %v want [3 2 1]", v)
	}
}

func TestApplyScalar(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
		"Cursed Technique: Apply\n    Using: (n) -> n * n\n"
	v, _ := runPipeline(t, src, "1\n2\n3") // sum 6 -> 36
	if v.(int64) != 36 {
		t.Fatalf("apply: got %v want 36", v)
	}
}

func TestVowInLoopBodyPasses(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Repeat 2\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n + 1\n" +
		"    Binding Vow: All Values > 0\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1\n2\n3") // +1 twice -> [3,4,5], vow holds -> 12
	if v.(int64) != 12 {
		t.Fatalf("vow-in-loop: got %v want 12", v)
	}
}

func TestVowInLoopBodyFails(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Repeat 5\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n - 1\n" +
		"    Binding Vow: All Values > 0\n"
	_, err := runErr(t, src, "1\n2") // first iter -> [0,1], vow fails
	if err == nil || !strings.Contains(err.Error(), "vow violated") {
		t.Fatalf("expected vow violation in loop, got %v", err)
	}
}

func TestLoopResolveErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"body changes type",
			"Cursed Energy: stdin\nShikigami: Ints\nSimple Domain: Repeat 2\n    Maximum Technique: Sum\n",
			"must preserve the value type",
		},
		{
			"while predicate not bool",
			"Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
				"Simple Domain: While\n    Using: (n) -> n + 1\n    Cursed Technique: Apply\n        Using: (n) -> n - 1\n",
			"must return Bool",
		},
		{
			"repeat missing count",
			"Cursed Energy: stdin\nShikigami: Ints\nSimple Domain: Repeat\n    Cursed Technique: Map Each\n        Using: (n) -> n\n",
			"Repeat needs a count",
		},
		{
			"repeat negative count",
			"Cursed Energy: stdin\nShikigami: Ints\nSimple Domain: Repeat -1\n    Cursed Technique: Map Each\n        Using: (n) -> n\n",
			"Repeat count must be >= 0",
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

// TestReverseTwiceIsIdentity is a property test: Reverse∘Reverse ==
// identity, over many random lists.
func TestReverseTwiceIsIdentity(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for iter := 0; iter < 200; iter++ {
		n := rng.Intn(11) + 1 // empty input is covered separately (TestReverseEmptyList)
		nums := make([]string, n)
		want := make([]ir.Value, n)
		for i := range nums {
			v := int64(rng.Intn(1000) - 500)
			nums[i] = strconv.FormatInt(v, 10)
			want[i] = v
		}
		src := "Cursed Energy: stdin\nShikigami: Ints\n" +
			"Reverse Cursed Technique: Reverse\nReverse Cursed Technique: Reverse\n"
		v, _ := runPipeline(t, src, strings.Join(nums, "\n"))
		got, _ := v.([]ir.Value)
		if len(got) != len(want) {
			t.Fatalf("iter %d: length %d want %d", iter, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("iter %d: element %d got %v want %v", iter, i, got[i], want[i])
			}
		}
	}
}

func TestReverseEmptyList(t *testing.T) {
	pos := tokenPos()
	node, err := reverse.Build(opWords("Reverse"), ArgSet{}, ir.List(ir.Int()), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, node, []ir.Value{}).([]ir.Value)
	if len(out) != 0 {
		t.Fatalf("reverse of empty list: got %v", out)
	}
}

func TestWhileIterationCap(t *testing.T) {
	old := maxLoopIterations
	maxLoopIterations = 100
	defer func() { maxLoopIterations = old }()

	src := "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
		"Simple Domain: While\n    Using: (n) -> n > 0\n" +
		"    Cursed Technique: Apply\n        Using: (n) -> n + 1\n" // never terminates
	_, err := runErr(t, src, "1")
	if err == nil || !strings.Contains(err.Error(), "exceeded 100 iterations") {
		t.Fatalf("expected iteration-cap error, got %v", err)
	}
}
