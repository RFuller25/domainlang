package prims

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"domain/ir"
)

// runPipeline resolves src and threads a value through the pipeline, returning
// the final value and anything written to stdout.
func runPipeline(t *testing.T, src, stdin string) (ir.Value, string) {
	t.Helper()
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var out strings.Builder
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &out}
	var cur ir.Value
	for _, n := range pipe.Nodes {
		v, err := n.Eval(ctx, cur)
		if err != nil {
			t.Fatalf("eval %s: %v", n.Prim, err)
		}
		cur = v
	}
	return cur, out.String()
}

const intsPrelude = "Cursed Energy: stdin\n" +
	"Cursed Technique: Split Text by \",\"\n" +
	"Channeled Energy: Convert List to Integers\n"

func TestFilterThenCountEndToEnd(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Filter\n" +
		"    Using: (n) -> n > 2\n" +
		"Maximum Technique: Count\n" +
		"Reveal: stdout\n"
	v, out := runPipeline(t, src, "1,2,3,4,5")
	if v.(int64) != 3 {
		t.Fatalf("filter>2 count: got %v want 3", v)
	}
	if strings.TrimSpace(out) != "3" {
		t.Fatalf("stdout: %q", out)
	}
}

func TestMapEachThenSum(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Map Each\n" +
		"    Using: (n) -> n * n\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1,2,3")
	if v.(int64) != 14 { // 1 + 4 + 9
		t.Fatalf("map square then sum: got %v want 14", v)
	}
}

func TestUnique(t *testing.T) {
	src := intsPrelude +
		"Cursed Technique: Unique\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "1,1,2,3,3,3")
	if v.(int64) != 3 {
		t.Fatalf("unique count: got %v want 3", v)
	}
}

func TestCountMatching(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Count Matching\n" +
		"    Using: (n) -> n > 2\n"
	v, _ := runPipeline(t, src, "1,2,3,4,5")
	if v.(int64) != 3 {
		t.Fatalf("count matching >2: got %v want 3", v)
	}
}

func TestMaxMinProduct(t *testing.T) {
	for _, c := range []struct {
		op   string
		want int64
	}{
		{"Maximum Technique: Max", 3},
		{"Maximum Technique: Min", 1},
		{"Maximum Technique: Product", 6},
	} {
		src := intsPrelude + c.op + "\n"
		v, _ := runPipeline(t, src, "3,1,2")
		if v.(int64) != c.want {
			t.Fatalf("%s: got %v want %d", c.op, v, c.want)
		}
	}
}

func TestFold(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Fold\n" +
		"    Seed: 0\n" +
		"    Using: (acc, n) -> acc + n\n"
	v, _ := runPipeline(t, src, "1,2,3,4")
	if v.(int64) != 10 {
		t.Fatalf("fold sum: got %v want 10", v)
	}
}

func TestGroupBy(t *testing.T) {
	src := intsPrelude +
		"Maximum Technique: Group By\n" +
		"    Using: (n) -> n\n"
	v, _ := runPipeline(t, src, "1,1,2,3,3,3")
	m, ok := v.(*ir.MapValue)
	if !ok {
		t.Fatalf("expected MapValue, got %T", v)
	}
	if m.Len() != 3 {
		t.Fatalf("group count: got %d want 3", m.Len())
	}
	bucket, _ := m.Get(int64(3))
	if len(bucket.([]ir.Value)) != 3 {
		t.Fatalf("bucket for key 3: got %v", bucket)
	}
}

// TestGroupByPartitionsCoverAllElements is a property test: Group By
// partitions cover all elements — every input element appears in exactly
// one bucket, and the buckets' total size equals the input size, over many
// random inputs.
func TestGroupByPartitionsCoverAllElements(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	for iter := 0; iter < 200; iter++ {
		n := rng.Intn(30) + 1
		nums := make([]string, n)
		vals := make([]int64, n)
		for i := range nums {
			v := int64(rng.Intn(11) - 5) // small range -> guaranteed collisions
			vals[i] = v
			nums[i] = strconv.FormatInt(v, 10)
		}
		src := intsPrelude + "Maximum Technique: Group By\n    Using: (n) -> n / 3\n"
		v, _ := runPipeline(t, src, strings.Join(nums, ","))
		m, ok := v.(*ir.MapValue)
		if !ok {
			t.Fatalf("iter %d: expected MapValue, got %T", iter, v)
		}

		total := 0
		seen := make([]bool, n)
		for _, k := range m.Keys() {
			bucketVal, _ := m.Get(k)
			bucket := bucketVal.([]ir.Value)
			total += len(bucket)
			for _, bv := range bucket {
				matched := false
				for i, want := range vals {
					if !seen[i] && bv.(int64) == want {
						seen[i] = true
						matched = true
						break
					}
				}
				if !matched {
					t.Fatalf("iter %d: bucket element %v not found among ungrouped inputs", iter, bv)
				}
			}
		}
		if total != n {
			t.Fatalf("iter %d: buckets cover %d elements, want %d (input %v)", iter, total, n, vals)
		}
	}
}

func TestIntersectAndUnionOverGroups(t *testing.T) {
	groups := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Cursed Technique: Split Each by \",\"\n"
	input := "a,b,c\nb,c,d"

	inter, _ := runPipeline(t, groups+"Maximum Technique: Intersect\nMaximum Technique: Count\n", input)
	if inter.(int64) != 2 { // {b, c}
		t.Fatalf("intersect count: got %v want 2", inter)
	}
	union, _ := runPipeline(t, groups+"Maximum Technique: Union\nMaximum Technique: Count\n", input)
	if union.(int64) != 4 { // {a, b, c, d}
		t.Fatalf("union count: got %v want 4", union)
	}
}

// Type/usage errors must be caught at resolve time.
func TestHigherOrderResolveErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"filter non-bool predicate",
			intsPrelude + "Cursed Technique: Filter\n    Using: (n) -> n + 1\n",
			"predicate must return Bool",
		},
		{
			"fold missing seed",
			intsPrelude + "Maximum Technique: Fold\n    Using: (acc, n) -> acc + n\n",
			"Fold requires a Seed",
		},
		{
			"map each on non-list",
			"Cursed Energy: stdin\nCursed Technique: Map Each\n    Using: (n) -> n\n",
			"expects a List input",
		},
		{
			"filter without lambda",
			intsPrelude + "Cursed Technique: Filter\n",
			"requires a Using: lambda",
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
