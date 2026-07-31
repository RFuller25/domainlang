package bench

import (
	"fmt"
	"io"
	"os"
	"testing"
)

// A case pairs testdata/<name>.domain with testdata/<name>.go: two programs
// answering the same question about the same bytes. `feature` names the piece
// of Domain the pair exists to measure.
type benchCase struct {
	name    string
	feature string
	gen     func(int) []byte
	size    int // input scale the benchmarks use
	small   int // input scale the parity test uses

	// budget overrides the 2x target for a case that is knowingly over it.
	// Every one carries a comment saying why and what would close the gap:
	// the point is to keep the gate meaningful for the other cases and to
	// fail loudly if a known-slow case gets slower still.
	budget float64
}

var cases = []benchCase{
	{
		name:    "read_length",
		feature: "the floor: read stdin, count runes, print. What both sides pay before any work.",
		gen:     genWords, size: 400_000, small: 400,
	},
	{
		name:    "pairs_increase",
		feature: "Pairs — each element tupled with the next (v0.5 list shaping)",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "scan_mod",
		feature: "Scan (the running fold) and the % Euclidean modulo operator",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "sliding_max",
		feature: "measured arguments — a Size: lambda over the current list, streamed by Sliding Reduce",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "pipeline_body",
		feature: "pipeline bodies — Map Each whose Using: is an indented pipeline, over Sum By",
		gen:     genRows, size: 250_000, small: 250,
	},
	{
		name:    "text_builtins",
		feature: "the v0.5 text builtins — startswith/indexof/slice/upper inside a lambda",
		gen:     genWords, size: 1_000_000, small: 1_000,
	},
	{
		name:    "count_by_entries",
		feature: "the Map vocabulary — Count By, Convert To Entries, tuple item()",
		gen:     genWords, size: 1_000_000, small: 1_000,
	},
	{
		name:    "partition_parts",
		feature: "Partition, the early-exit Find Index, and Part blocks",
		gen:     genIntsNeedle, size: 2_000_000, small: 2_000,
	},
	{
		name:    "topk_sum",
		feature: "algorithm substitution — a requested Quicksort + Select Top K becomes a quickselect",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "dijkstra_grid",
		feature: "a grid search as a named algorithm — Dijkstra over a digit grid",
		gen:     genGrid, size: 700, small: 40,
	},
	{
		name:    "match_pattern",
		feature: "all-int Match Pattern templates, compiled to a hand-rolled scanner",
		gen:     genRanges, size: 1_000_000, small: 1_000,
	},
	{
		name:    "toposort_words",
		feature: "Topological Sort over an edge list, parsed by word holes (the regexp path)",
		gen:     genEdges, size: 400_000, small: 400,
	},
	{
		name:    "combinations3",
		feature: "algorithm substitution — Combinations 3 summing to a constant, O(n^3) to O(n^2)",
		gen:     genTriple, size: 20_000, small: 200,
	},
	{
		name:    "sort_by_key",
		feature: "Sort By with a key lambda, fused with Take Item 0 into a selection",
		gen:     genKeyed, size: 1_000_000, small: 1_000,
	},
	{
		name:    "merge_ranges",
		feature: "Merge Ranges over the tuple list a positional Match Pattern produces",
		gen:     genSpans, size: 1_000_000, small: 1_000,
	},
	{
		name:    "group_map_values",
		feature: "Group By + Map Values — the two-line grouped aggregation",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "set_intersect",
		feature: "the set vocabulary — Split Each by \"\" feeding Intersect",
		gen:     genLetters, size: 300_000, small: 300,
	},
	{
		name:    "connected_components",
		feature: "Connected Components over a dense grid (union-find)",
		gen:     genMaze, size: 1_500, small: 40,
	},
	{
		name:    "grid_bfs",
		feature: "BFS over a grid, then Count Cells over the distances it produced",
		gen:     genMaze, size: 1_500, small: 40,
	},
	{
		name:    "sparse_life",
		feature: "the Sparse plane — eight generations of Life over Find Cells + Count By",
		gen:     genLife, size: 20_000, small: 200,
		// Over target, knowingly. Each lap rebuilds two whole planes and an
		// insertion-ordered Map where the Go program keeps two plain maps.
		// Fusing Count By + Convert To Sparse Grid + Find Cells measures at
		// 2.28x (see README); the rest is the data model, not the codegen.
		budget: 3.2,
	},
	{
		name:    "explore_states",
		feature: "Explore — breadth-first search over an implicit graph, Mode: Count",
		gen:     genSeed, size: 1, small: 1,
	},
	{
		name:    "loop_repeat",
		feature: "a Simple Domain loop: five million laps threading one Int",
		gen:     genSeed, size: 7, small: 7,
	},
	{
		name:    "join_output",
		feature: "the output path — a per-line transform and a Join rendering megabytes",
		gen:     genLetters, size: 400_000, small: 400,
	},
	{
		name:    "channels_zip",
		feature: "Channels and a channel consumer — two branches from one value, zipped",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "shikigami_calls",
		feature: "Shikigami inlining, including a higher-order one taking a lambda",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "vows_hot",
		feature: "Binding Vows left in — what a debug-time build costs over --release",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "grid_transform",
		feature: "grid geometry — Transpose, Rotate, Flip, positional Map Cells",
		gen:     genGrid, size: 1_500, small: 40,
	},
	{
		name:    "float_sum",
		feature: "the Float path — parsing, a non-Int accumulation, and Float rendering",
		gen:     genFloats, size: 2_000_000, small: 2_000,
	},
	{
		name:    "fold_tuple",
		feature: "a measured Seed widening Fold to a composite accumulator",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "iterate_unfold",
		feature: "the generators — Iterate keeps the trajectory, Unfold grows one back",
		gen:     genSeed, size: 7, small: 7,
	},
	{
		name:    "while_halve",
		feature: "a While loop whose predicate is a reduction over the value it guards",
		gen:     genInts, size: 1_000_000, small: 1_000,
	},
	{
		name:    "fixed_point",
		feature: "Iterate Until Fixed Point — structural equality over the whole value",
		gen:     genInts, size: 1_000_000, small: 1_000,
	},
	{
		name:    "list_shaping",
		feature: "Chunk, Take While, Unique and Reverse, one after another",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "for_loop",
		feature: "a For loop binding an ambient parameter into the body's lambdas",
		gen:     genInts, size: 2_000_000, small: 2_000,
	},
	{
		name:    "math_builtins",
		feature: "the number-theory builtins — gcd, isqrt and modpow in a hot lambda",
		gen:     genInts, size: 1_000_000, small: 1_000,
	},
}

// TestParity is the precondition for every number below: the two programs must
// agree, byte for byte, or the comparison is meaningless. It runs at the small
// scale so it is affordable in a normal `go test ./...`.
func TestParity(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 2 binaries per case")
	}
	requireGo(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			input := inputFile(t, c.name+"_small", c.gen(c.small))
			got := run(t, domainBinary(t, c.name), input)
			want := run(t, goBinary(t, c.name), input)
			if got != want {
				t.Fatalf("Domain and Go disagree\n domain: %q\n     go: %q", got, want)
			}
			if got == "" {
				t.Fatal("both programs printed nothing")
			}
		})
	}
}

// BenchmarkHeadToHead times each pair on the same input. Both sides are
// subprocesses, so both pay process startup and both read the input over a
// pipe — whatever that costs, it costs them equally.
func BenchmarkHeadToHead(b *testing.B) {
	requireGo(b)
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			input := inputFile(b, c.name, c.gen(c.size))
			b.Run("domain", func(b *testing.B) { benchBinary(b, domainBinary(b, c.name), input) })
			b.Run("go", func(b *testing.B) { benchBinary(b, goBinary(b, c.name), input) })
		})
	}
}

func benchBinary(b *testing.B, bin, input string) {
	b.Helper()
	if fi, err := os.Stat(input); err == nil {
		b.SetBytes(fi.Size())
	}
	b.ResetTimer()
	for range b.N {
		execBin(b, bin, input, io.Discard)
	}
}

// TestSpeedRatio is the gate the benchmarks exist to defend: Domain's compiled
// output within 2× of hand-written Go on the same input. It is opt-in — it
// runs every program several times at full scale — so set DOMAIN_BENCH=1 to
// take the measurement.
func TestSpeedRatio(t *testing.T) {
	if os.Getenv("DOMAIN_BENCH") == "" {
		t.Skip("set DOMAIN_BENCH=1 to measure (runs every program at full scale)")
	}
	requireGo(t)
	const reps = 5
	const target = 2.0

	t.Logf("%-22s %12s %12s %8s", "case", "domain", "go", "ratio")
	for _, c := range cases {
		input := inputFile(t, c.name, c.gen(c.size))
		dbin, gbin := domainBinary(t, c.name), goBinary(t, c.name)
		// Warm both binaries' pages before either is timed.
		run(t, dbin, input)
		run(t, gbin, input)
		d, g := fastestPair(t, dbin, gbin, input, reps)
		ratio := float64(d) / float64(g)
		budget := c.budget
		note := ""
		if budget == 0 {
			budget = target
		} else {
			note = fmt.Sprintf("  (over target on purpose, budget %.1fx)", budget)
		}
		t.Logf("%-22s %12s %12s %7.2fx%s", c.name, d.Round(100_000), g.Round(100_000), ratio, note)
		if ratio > budget {
			t.Errorf("%s: Domain is %.2fx hand-written Go, over its %.1fx budget (%s vs %s)",
				c.name, ratio, budget, d, g)
		}
	}
}
