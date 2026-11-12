package codegen_test

import (
	"fmt"
	"strings"
	"testing"

	"domain/codegen"
	"domain/optimizer"
)

// The rules in optimizer/longway.go, the typed totality analysis and the
// passes that now reach inside `Using:` bodies all hand the compiler shapes it
// had not been given before: a Pairs it did not parse, an Apply with a
// synthesized lambda, an Iterate that stops early, a body whose node list was
// replaced. These hold the binary to the interpreter — optimized and naive —
// over ordinary input, a single element and the empty list.
func TestCompiledLongWayMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct {
		name, body, rewrite string
	}{
		{"fold over tuples becomes a keyed sum", `Cursed Technique: Enumerate
Maximum Technique: Fold
    Seed: 0
    Using: (acc, p) -> acc + if item(p, 0) > 1 then item(p, 1) else 0
`, "→ Sum By"},
		{"window then Sum Each", `Cursed Technique: Window 3
Maximum Technique: Sum Each Group
`, "Sliding-Window Sum"},
		{"window 2 counted by its ends", `Cursed Technique: Window 2
Maximum Technique: Count Matching
    Using: (w) -> first(w) < last(w)
`, "Pairs + Count Matching"},
		{"window 2 mapped by its ends and summed", `Cursed Technique: Window 2
Cursed Technique: Map Each
    Using: (w) -> if item(w, 0) = item(w, 1) then item(w, 0) else 0
Maximum Technique: Sum
`, "Pairs + Map Each"},
		{"window 2, any", `Cursed Technique: Window 2
Maximum Technique: Any
    Using: (w) -> last(w) - first(w) > 5
`, "Pairs + Any"},
		{"window 3 mapped by its ends", `Cursed Technique: Window 3
Cursed Technique: Map Each
    Using: (w) -> if first(w) = last(w) then first(w) else item(w, 1)
`, "Window Positions + Map Each"},
		{"window 4 step 2 counted by one position", `Cursed Technique: Window 4 2
Maximum Technique: Count Matching
    Using: (w) -> item(w, 2) > 3
`, "Window Positions + Count Matching"},
		{"measured window by its ends", `Cursed Technique: Window
    Size: (xs) -> length(xs) / 4 + 1
Cursed Technique: Map Each
    Using: (w) -> if first(w) = last(w) then first(w) else 0
Maximum Technique: Sum
`, "Window Positions + Map Each"},
		{"window any by its ends", `Cursed Technique: Window 3
Maximum Technique: Any
    Using: (w) -> first(w) = last(w)
`, "Window Positions + Any"},
		{"an expression bound twice", `Cursed Technique: Apply
    Using: (xs) -> consider a as indexof(xs, 5) in consider b as indexof(xs, 5) in a * 10 + b
`, "common subexpression"},
		{"a total call written twice", `Cursed Technique: Apply
    Using: (xs) -> length(unique(xs)) + length(unique(xs)) * 2
`, "common subexpression"},
		{"the item at the index of the maximum", `Cursed Technique: Apply
    Using: (xs) -> if length(xs) = 0 then tuple(0, 0, 0) else consider m as max(xs) in consider i as indexof(xs, m) in consider v as item(xs, i) in tuple(m, i, v)
`, "cheaper equivalent builtin"},
		{"arms that differ in one field", `Cursed Technique: Map Each
    Using: (x) -> if x > 4 then tuple(x, 1, 2) else tuple(x, 0, 2)
`, "shared conditional arms"},
		{"sorted neighbours equal", `Domain Expansion: Quicksort
Cursed Technique: Window 2
Maximum Technique: Any
    Using: (a) -> first(a) = last(a)
`, "not an ordering"},
		{"sorted neighbours all differ", `Domain Expansion: Quicksort
Cursed Technique: Window 2
Maximum Technique: All
    Using: (a) -> ikke first(a) = last(a)
`, "not an ordering"},
		{"every pair compared", `Domain Expansion: All Pairs
    Mode: Map
    Using: (a, b) -> if a = b then 0 else 1
Maximum Technique: Any
    Using: (x) -> x = 0
`, "not every pair"},
		{"pairs any equal", `Domain Expansion: All Pairs
    Mode: Any
    Using: (a, b) -> a = b
`, "Mode: Any"},
		{"pairs all distinct", `Domain Expansion: All Pairs
    Mode: All
    Using: (a, b) -> ikke a = b
`, "Mode: All"},
		{"every pair of lists compared", `Cursed Technique: Chunk 2
Domain Expansion: All Pairs
    Mode: Map
    Using: (a, b) -> if a = b then 0 else 1
Maximum Technique: Any
    Using: (x) -> x = 0
`, "not every pair"},
		{"a bool summed", `Maximum Technique: Sum By
    Using: (x) -> if x > 4 then 0 else 1
`, "→ Count Matching (a Bool"},
		{"non-empty filter before flatten", `Cursed Technique: Chunk 2
Cursed Technique: Filter
    Using: (c) -> length(c) > 0
Cursed Technique: Flatten
`, "adds nothing"},
		{"iterate searched for a repeat", `Cursed Technique: Apply
    Using: (xs) -> sum(xs)
Cursed Technique: Iterate 500
    Using: (x) -> (x * 5 + 3) % 23
Cursed Technique: Map Each
    Using: (x) -> x % 4
Maximum Technique: Find Cycle
`, "first repeated state"},
		{"iterate over a list, in a body, behind a binding", `Cursed Technique: Iterate 300
    Consider n Of (xs) -> length(xs)
    Cursed Technique: Apply
        Using: (xs) -> if n = 0 then xs else concat(drop(xs, 1), take(xs, 1))
Cursed Technique: Map Each
    Cursed Technique: Map Each
        Using: (x) -> totext(x)
    Cursed Technique: Apply
        Using: (ts) -> textjoin(ts, "-")
Maximum Technique: Find Cycle
`, "first repeated state"},
		{"iterate that never repeats", `Cursed Technique: Apply
    Using: (xs) -> sum(xs)
Cursed Technique: Iterate 9
    Using: (x) -> x + 2
Maximum Technique: Find Cycle
`, "first repeated state"},
		{"rewrites inside a Map Each body", `Cursed Technique: Chunk 4
Cursed Technique: Map Each
    Domain Expansion: Quicksort
    Cursed Technique: Window 2
    Maximum Technique: Any
        Using: (a) -> first(a) = last(a)
Maximum Technique: Sum By
    Using: (x) -> if x then 0 else 1
`, "not an ordering"},
		{"fusion inside a body inside a body", `Cursed Technique: Chunk 4
Cursed Technique: Chunk 2
Cursed Technique: Map Each
    Cursed Technique: Map Each
        Cursed Technique: Map Each
            Using: (x) -> x * 2
        Maximum Technique: Sum
    Maximum Technique: Max
`, "→ Sum By"},
		{"map fusion that binds a called body", `Cursed Technique: Chunk 3
Cursed Technique: Map Each
    Using: (c) -> sum(c) + length(c)
Cursed Technique: Map Each
    Using: (y) -> y * y + y
`, "fused Map Each"},
	}
	for _, tc := range cases {
		for _, input := range []string{"5 1 9 5 1 7 9 2 2 8 4", "7", ""} {
			name := tc.name + "/" + map[string]string{"": "empty", "7": "one"}[input]
			if input != "" && input != "7" {
				name = tc.name + "/many"
			}
			t.Run(name, func(t *testing.T) {
				src := head + tc.body + "Reveal: stdout\n"
				pipe := compilePipeline(t, src, false)
				resolveMu.Lock()
				rewrites := optimizer.Optimize(pipe, true)
				resolveMu.Unlock()
				fired := false
				for _, r := range rewrites {
					fired = fired || strings.Contains(r.Message, tc.rewrite)
				}
				if !fired {
					t.Fatalf("expected a rewrite containing %q, got %v", tc.rewrite, rewrites)
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

// A sequence stage over a Map or a Set must go through the per-node path,
// which turns the collection into its elements and types the lambda from the
// entry tuple. The adjacent-pair fusions used to take it first when the
// optimizer was off (Map Each + Sum, unfused), typing the lambda parameter as
// the Map's value and refusing to compile.
func TestCompiledSequenceStagesOverCollectionsUnoptimized(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct{ name, body string }{
		{"count by, entries mapped, summed", `Maximum Technique: Count By
    Using: (x) -> x % 3
Cursed Technique: Map Each
    Using: (e) -> item(e, 1)
Maximum Technique: Sum
`},
		{"count by, keys mapped, summed", `Maximum Technique: Count By
    Using: (x) -> x % 3
Cursed Technique: Map Each
    Using: (e) -> item(e, 0) * item(e, 1)
Maximum Technique: Sum
`},
		{"group by, buckets measured, summed", `Maximum Technique: Group By
    Using: (x) -> x % 2
Cursed Technique: Map Each
    Using: (e) -> length(item(e, 1))
Maximum Technique: Sum
`},
		{"set mapped and summed", `Channeled Energy: Convert To Set
Cursed Technique: Map Each
    Using: (x) -> x * 2
Maximum Technique: Sum
`},
		{"count by, entries filtered and counted", `Maximum Technique: Count By
    Using: (x) -> x % 3
Cursed Technique: Filter
    Using: (e) -> item(e, 1) > 1
Maximum Technique: Count
`},
	}
	for _, tc := range cases {
		for _, input := range []string{"5 1 9 5 1 7 9 2 2 8 4", "7", ""} {
			for _, optimize := range []bool{false, true} {
				name := fmt.Sprintf("%s/%q/optimize=%v", tc.name, input, optimize)
				t.Run(name, func(t *testing.T) {
					src := head + tc.body + "Reveal: stdout\n"
					pipe := compilePipeline(t, src, optimize)
					want := runInterpreter(t, compilePipeline(t, src, false), []byte(input))
					if got := buildAndRun(t, pipe, []byte(input), codegen.Options{}); got != want {
						t.Errorf("interpreter %q, binary %q", want, got)
					}
				})
			}
		}
	}
}
