package codegen_test

import (
	"fmt"
	"strings"
	"testing"

	"domain/codegen"
	"domain/optimizer"
)

// The idiom passes (optimizer/idioms.go) replace a long spelling with the
// named primitive it amounts to, and the stream fuser then lowers what is left
// to one loop. These hold the compiled result to the interpreter — optimized
// and naive — over ordinary input, a single element, and the empty list, which
// is where a reduction's identity and an early exit's default are decided.
func TestCompiledIdiomsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	// Extract Integers reads empty input as the empty list.
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct {
		name, body, rewrite string
	}{
		{"fold that counts", `Maximum Technique: Fold
    Seed: 0
    Using: (acc, x) -> acc + 1
`, "→ Count"},
		{"fold summing a mapped value, behind a map", `Cursed Technique: Map Each
    Using: (x) -> x - 3
Maximum Technique: Fold
    Seed: 0
    Using: (acc, x) -> acc + x * x
`, "→ Sum By"},
		{"fold counting conditionally, behind a filter", `Cursed Technique: Filter
    Using: (x) -> x > 1
Maximum Technique: Fold
    Seed: 0
    Using: (acc, x) -> if x % 2 = 1 then acc + 1 else acc
`, "→ Count Matching"},
		{"fold summing conditionally", `Maximum Technique: Fold
    Seed: 0
    Using: (acc, x) -> if x > 4 then acc + x * 2 else acc
`, "→ Sum By"},
		{"sort dead across a map and a filter", `Domain Expansion: Quicksort, Descending
Cursed Technique: Map Each
    Using: (x) -> x * 3
Cursed Technique: Filter
    Using: (x) -> x > 9
Maximum Technique: Sum
`, "its ordering never reaches the result"},
		{"filter ahead of a sort", `Domain Expansion: Quicksort
Cursed Technique: Filter
    Using: (x) -> x % 2 = 1
`, "filter first"},
		{"distinct count", `Cursed Technique: Unique
Maximum Technique: Count
`, "→ Count Distinct"},
		{"distinct count behind a map", `Cursed Technique: Map Each
    Using: (x) -> x % 4
Cursed Technique: Unique
Maximum Technique: Count
`, "→ Count Distinct"},
		{"count compared with zero", `Cursed Technique: Filter
    Using: (x) -> x > 8
Maximum Technique: Count
Cursed Technique: Apply
    Using: (c) -> c > 0
`, "→ Any"},
		{"count equal to zero, behind a map", `Cursed Technique: Map Each
    Using: (x) -> x * 2
Maximum Technique: Count Matching
    Using: (x) -> x > 16
Cursed Technique: Apply
    Using: (c) -> c = 0
`, "→ All"},
		{"filtered list tested for emptiness", `Cursed Technique: Filter
    Using: (x) -> x = 7
Cursed Technique: Apply
    Using: (xs) -> length(xs) > 0
`, "→ Any"},
		{"fold with a tuple accumulator behind a map", `Cursed Technique: Map Each
    Using: (x) -> x * 2
Maximum Technique: Fold
    Seed: (xs) -> tuple(0, 1)
    Using: (acc, x) -> tuple(prow(acc) + x, if x > pcol(acc) then x else pcol(acc))
`, ""},
		{"fold building a map behind a filter", `Cursed Technique: Filter
    Using: (x) -> x > 1
Maximum Technique: Fold
    Seed: (xs) -> emptymap(0, 0)
    Using: (acc, x) -> insert(acc, x % 3, getor(acc, x % 3, 0) + x)
Channeled Energy: Convert To Entries
`, ""},
		{"fold that max-es with a literal seed", `Cursed Technique: Map Each
    Using: (x) -> x % 7
Maximum Technique: Fold
    Seed: 0
    Using: (acc, x) -> max(acc, x)
`, ""},
		{"fold whose lambda can fail behind a total map", `Cursed Technique: Map Each
    Using: (x) -> x + 1
Maximum Technique: Fold
    Seed: 100
    Using: (acc, x) -> acc - 100 / (x - 50)
`, ""},
		{"fold with a seed that reads the list is not streamed", `Cursed Technique: Map Each
    Using: (x) -> x + 1
Maximum Technique: Fold
    Seed: (xs) -> length(xs)
    Using: (acc, x) -> acc + x * 2 - 1
`, ""},
		{"count by a text key, streamed from the lines", `Cursed Technique: Map Each
    Using: (x) -> totext(x % 5)
Maximum Technique: Count By
    Using: (s) -> slice(s, 0, 1)
Channeled Energy: Convert To Entries
`, ""},
		{"group by then sum is one keyed accumulator behind a filter", `Cursed Technique: Filter
    Using: (x) -> x > 1
Maximum Technique: Group By
    Using: (n) -> n % 3
Cursed Technique: Map Values
    Using: (b) -> sum(b)
Channeled Energy: Convert To Entries
`, ""},
		{"group by keeps its buckets", `Maximum Technique: Group By
    Using: (n) -> n % 4
Channeled Energy: Convert To Entries
`, ""},
		{"length-changing rewrites inside Part bodies", `Part "top":
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top 2, Sum
    Reveal: stdout
Part "dead":
    Domain Expansion: Quicksort
    Cursed Technique: Map Each
        Using: (x) -> x * 2
    Maximum Technique: Sum
    Reveal: stdout
Part "count":
    Cursed Technique: Unique
    Maximum Technique: Count
    Reveal: stdout
`, "Cursed Quickselect"},
		{"length-changing rewrites inside a loop body", `Simple Domain: Repeat 3
    Domain Expansion: Quicksort
    Cursed Technique: Filter
        Using: (x) -> x > 1
    Cursed Technique: Map Each
        Using: (x) -> x + 1
Cursed Technique: Apply
    Using: (xs) -> sum(xs)
`, "filter first"},
		{"sorted lists, both orders, ints and text", `Part "up":
    Domain Expansion: Quicksort
    Reveal: stdout
Part "down":
    Domain Expansion: Quicksort, Descending
    Reveal: stdout
Part "text":
    Cursed Technique: Map Each
        Using: (x) -> totext(x % 6)
    Domain Expansion: Quicksort, Descending
    Reveal: stdout
Part "expr":
    Cursed Technique: Apply
        Using: (xs) -> sort(xs)
    Reveal: stdout
`, ""},
		{"range streamed into a filter and a sum", `Cursed Technique: Range
    High: (xs) -> length(xs) * 10
Cursed Technique: Filter
    Using: (x) -> x % 3 = 0 or x % 5 = 0
Maximum Technique: Sum
`, ""},
		{"range streamed into any", `Cursed Technique: Range 1 to 50
Cursed Technique: Map Each
    Using: (x) -> x * x
Maximum Technique: Any
    Using: (x) -> x > 2000
`, ""},
		{"range streamed and collected", `Cursed Technique: Range 3 to 12
Cursed Technique: Map Each
    Using: (x) -> x * x
`, ""},
		{"all over a mapped list", `Cursed Technique: Map Each
    Using: (x) -> x + 1
Maximum Technique: All
    Using: (x) -> x > 1
`, ""},
		{"product by behind a filter", `Cursed Technique: Filter
    Using: (x) -> x < 6
Maximum Technique: Product By
    Using: (x) -> x + 1
`, ""},
	}
	for _, tc := range cases {
		for _, input := range []string{"5 1 9 5 1 7 9", "7", ""} {
			name := tc.name + "/" + map[string]string{"": "empty", "7": "one"}[input]
			if input != "" && input != "7" {
				name = tc.name + "/many"
			}
			t.Run(name, func(t *testing.T) {
				src := head + tc.body
				if !strings.Contains(tc.body, "Part ") {
					src += "Reveal: stdout\n"
				}
				pipe := compilePipeline(t, src, false)
				resolveMu.Lock()
				rewrites := optimizer.Optimize(pipe, true)
				resolveMu.Unlock()
				if tc.rewrite != "" {
					fired := false
					for _, r := range rewrites {
						fired = fired || strings.Contains(r.Message, tc.rewrite)
					}
					if !fired {
						t.Fatalf("expected a rewrite containing %q, got %v", tc.rewrite, rewrites)
					}
				}
				want := runInterpreter(t, pipe, []byte(input))
				naive := runInterpreter(t, compilePipeline(t, src, false), []byte(input))
				if want != naive {
					t.Fatalf("optimized interpreter %q, naive %q", want, naive)
				}
				if got := buildAndRun(t, pipe, []byte(input), codegen.Options{}); got != want {
					t.Errorf("interpreter %q, binary %q", want, got)
				}
			})
		}
	}
}

// What the emitted program must not contain is the point of these rewrites: a
// list nobody reads. Each case names the allocation that has to be gone.
func TestCompiledStreamedParsersMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n"
	cases := []struct{ name, body, input string }{
		{"split each into a keyed count", "Cursed Technique: Split Each by \",\"\nCursed Technique: Map Each\n    Using: (f) -> item(f, 0)\nMaximum Technique: Count By\n    Using: (s) -> s\nChanneled Energy: Convert To Entries\n",
			"a,1\nb,2\na,3\nc,4\nb,5"},
		{"split each, rows parsed to integers, summed per row", "Cursed Technique: Split Each by \" \"\nChanneled Energy: Convert Each List to Integers\nCursed Technique: Map Each\n    Using: (r) -> sum(r)\nMaximum Technique: Max\n",
			"1 2 3\n40 5\n6"},
		{"match pattern into a filter and a count by", "Cursed Technique: Match Pattern\n    Using: \"{a:int}-{b:int} {w:word}\"\nCursed Technique: Filter\n    Using: (r) -> r.b > 3\nMaximum Technique: Count By\n    Using: (r) -> r.w\nChanneled Energy: Convert To Entries\n",
			"1-3 a\n2-5 b\n3-7 a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := head + tc.body + "Reveal: stdout\n"
			pipe := compilePipeline(t, src, true)
			want := runInterpreter(t, pipe, []byte(tc.input))
			naive := runInterpreter(t, compilePipeline(t, src, false), []byte(tc.input))
			if want != naive {
				t.Fatalf("optimized interpreter %q, naive %q", want, naive)
			}
			if got := buildAndRun(t, pipe, []byte(tc.input), codegen.Options{}); got != want {
				t.Errorf("interpreter %q, binary %q", want, got)
			}
		})
	}
}

func TestIdiomsLeaveNoIntermediateList(t *testing.T) {
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct {
		name, body string
		want       []string // substrings the emitted main must contain
		absent     []string // and must not
	}{
		{"a streamed range is a counting loop",
			"Cursed Technique: Range 1 to 1000\nCursed Technique: Filter\n    Using: (x) -> x % 3 = 0\nMaximum Technique: Sum\n",
			[]string{"< 1000;"}, []string{"make([]int64"}},
		{"a hand-written fold streams like the reduction it is",
			"Cursed Technique: Map Each\n    Using: (x) -> x + 1\nMaximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x * x\n",
			nil, []string{"make([]int64"}},
		{"a distinct count builds a set and no list",
			"Cursed Technique: Map Each\n    Using: (x) -> x % 10\nCursed Technique: Unique\nMaximum Technique: Count\n",
			[]string{"map[int64]struct{}"}, []string{"make([]int64", "append("}},
		{"any over a mapped list stops early and builds nothing",
			"Cursed Technique: Map Each\n    Using: (x) -> x * 2\nMaximum Technique: Any\n    Using: (x) -> x > 10\n",
			[]string{"break decided"}, []string{"make([]int64"}},
		{"a fold behind a map streams and builds no list",
			"Cursed Technique: Map Each\n    Using: (x) -> x + 1\nMaximum Technique: Fold\n    Seed: (xs) -> tuple(0, 0)\n    Using: (acc, x) -> tuple(prow(acc) + x, pcol(acc) + 1)\n",
			nil, []string{"make([]int64"}},
		{"an integer sort is the specialised one",
			"Domain Expansion: Quicksort, Descending\n",
			[]string{"slices.Sort(", "slices.Reverse("}, []string{"sort.SliceStable"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pipe := compilePipeline(t, head+tc.body+"Reveal: stdout\n", true)
			src, err := codegen.EmitProgram(pipe, codegen.Options{})
			if err != nil {
				t.Fatal(err)
			}
			_, main, ok := strings.Cut(src, "func main() {")
			if !ok {
				t.Fatalf("no main in emitted source:\n%s", src)
			}
			// Extract Integers itself builds the input list; everything after
			// the first statement that mentions it is the pipeline under test.
			if _, rest, found := strings.Cut(main, "ExtractInts"); found {
				_, main, _ = strings.Cut(rest, "\n")
			}
			for _, w := range tc.want {
				if !strings.Contains(main, w) {
					t.Errorf("emitted main should contain %q:\n%s", w, main)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(main, a) {
					t.Errorf("emitted main should not contain %q:\n%s", a, main)
				}
			}
		})
	}
}

// A pipe has no size to reserve, so the reader grows a buffer by doubling. This
// pushes enough through one — past the first buffer, several times over — that a
// bug in the growth (a lost chunk, a short final read) changes the sum.
func TestPipedInputBeyondTheFirstBufferIsRead(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const src = "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\nChanneled Energy: Convert List to Integers\nMaximum Technique: Sum\nReveal: stdout\n"
	var in strings.Builder
	var want int64
	for i := 0; i < 700_000; i++ { // ~5 MB: five doublings from 1 MB
		v := int64(i%9973) + 1
		want += v
		fmt.Fprintf(&in, "%d\n", v)
	}
	pipe := compilePipeline(t, src, true)
	if got := strings.TrimSpace(buildAndRun(t, pipe, []byte(in.String()), codegen.Options{})); got != fmt.Sprint(want) {
		t.Errorf("sum of piped input = %s, want %d", got, want)
	}
}

// A stream run that reaches no sink only collects, and must not take the nodes
// a dedicated lowering starts from: set_intersect fell from 0.15x to 4.8x of
// the Go the first time Split Each became a stage.
func TestCollectingRunsDoNotStealDedicatedFusions(t *testing.T) {
	const intersect = "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\nCursed Technique: Split Each by \"\"\nMaximum Technique: Intersect\nMaximum Technique: Count\nReveal: stdout\n"
	pipe, err := frontEnd(intersect, true)
	if err != nil {
		t.Skipf("program shape not accepted by this build: %v", err)
	}
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(goSrc, `, "")`) {
		t.Errorf("Split Each was lowered as a stream stage instead of the running intersection:\n%s", goSrc)
	}
}
