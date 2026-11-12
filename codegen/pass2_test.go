package codegen_test

import (
	"strings"
	"testing"

	"domain/codegen"
	"domain/optimizer"
)

// The passes that see through a Consider, join a Map Each body to the stage
// after it, normalize whole-list builtins into primitives and push take
// backwards each hand the compiler nodes it had not seen. The binary must print
// what the interpreter prints, optimized and naive.
func TestCompiledPass2MatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	const head = "Cursed Energy: stdin\nCursed Technique: Extract Integers\n"
	cases := []struct {
		name, body, rewrite string
	}{
		{"map behind a binding fuses with the next", `Cursed Technique: Map Each
    Consider n Of (xs) -> length(xs)
    Using: (x) -> x + n
Cursed Technique: Map Each
    Using: (y) -> y * 2
`, "fused Map Each"},
		{"pairs mapped to flags, flags read in the next body", `Cursed Technique: Chunk 4
Cursed Technique: Map Each
    Domain Expansion: All Pairs
        Mode: Map
        Using: (a, b) -> if a = b then 0 else 1
Maximum Technique: Sum By
    Maximum Technique: Any
        Using: (x) -> x = 0
    Cursed Technique: Apply
        Using: (f) -> if f then 0 else 1
`, "not every pair"},
		{"body then map", `Cursed Technique: Chunk 3
Cursed Technique: Map Each
    Domain Expansion: Quicksort
    Reverse Cursed Technique: Reverse
Cursed Technique: Map Each
    Using: (ys) -> sum(ys) + 1
`, "joined bodies"},
		{"body then any", `Cursed Technique: Chunk 3
Cursed Technique: Map Each
    Domain Expansion: Quicksort
Maximum Technique: Any
    Using: (ys) -> sum(ys) > 7
`, "joined bodies"},
		{"body then count matching", `Cursed Technique: Chunk 3
Cursed Technique: Map Each
    Reverse Cursed Technique: Reverse
Maximum Technique: Count Matching
    Using: (ys) -> sum(ys) > 3
`, "joined bodies"},
		{"apply of builtins", `Cursed Technique: Apply
    Using: (xs) -> sort(xs)
Cursed Technique: Apply
    Using: (xs) -> reverse(xs)
Cursed Technique: Apply
    Using: (xs) -> unique(xs)
`, "→ Sort"},
		{"take pushed before a map", `Cursed Technique: Map Each
    Using: (x) -> x * x + 1
Cursed Technique: Apply
    Using: (xs) -> take(xs, 3)
`, "take"},
		{"take pushed before a window", `Cursed Technique: Window 2
Cursed Technique: Apply
    Using: (xs) -> take(xs, 3)
`, "take"},
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
