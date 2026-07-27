package codegen_test

import (
	"testing"

	"domain/codegen"
)

// The second v0.5 batch across backends: Range, Reverse over Text, Subgrid,
// Pad Grid, and Mode: 4 | 8 on the grid searches. The connectivity cases
// matter most — the neighbour walk is written twice, and a Mode: 8 that
// silently stayed orthogonal in one backend is exactly what parity catches.
func TestCompiledV05BatchTwoMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			name: "range drives fizzbuzz",
			src: `Cursed Energy: stdin
Cursed Technique: Range 1 21
Cursed Technique: Map Each
    Using: (n) -> if mod(n, 15) = 0 then "FizzBuzz" else (if mod(n, 3) = 0 then "Fizz" else (if mod(n, 5) = 0 then "Buzz" else totext(n)))
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "x",
		},
		{
			name: "range one-argument form",
			src: `Cursed Energy: stdin
Cursed Technique: Range 6
Maximum Technique: Sum
Reveal: stdout
`,
			input: "x",
		},
		{
			name: "reverse text by rune",
			src: `Cursed Energy: stdin
Reverse Cursed Technique: Reverse
Reveal: stdout
`,
			input: "héllo wörld",
		},
		{
			name: "reverse text in a lambda",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Filter
    Using: (s) -> reverse(s) = s
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "racecar\nabc\nlevel\nxy",
		},
		{
			name: "subgrid and pad",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Part "crop":
    Cursed Technique: Subgrid 1 1 2 2
    Reveal: stdout
Part "pad":
    Cursed Technique: Pad Grid 2
        Fill: "."
    Reveal: stdout
`,
			input: "abcd\nefgh\nijkl",
		},
		{
			// A diagonal chain: three components orthogonally, one with
			// diagonals.
			name: "connected components connectivity",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Part "four":
    Domain Expansion: Connected Components
        Mode: 4
        Using: (c) -> c = "#"
    Reveal: stdout
Part "eight":
    Domain Expansion: Connected Components
        Mode: 8
        Using: (c) -> c = "#"
    Reveal: stdout
`,
			input: "#..\n.#.\n..#",
		},
		{
			name: "bfs connectivity",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Part "four":
    Domain Expansion: BFS from 0 0
        Using: (c) -> c = "."
    Reveal: stdout
Part "eight":
    Domain Expansion: BFS from 0 0
        Mode: 8
        Using: (c) -> c = "."
    Reveal: stdout
`,
			input: "....\n....\n....",
		},
		{
			name: "flood fill connectivity",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Flood Fill from 0 0
    Mode: 8
    Using: (c) -> c = "#"
Reveal: stdout
`,
			input: "#..\n.#.\n..#",
		},
		{
			name: "dijkstra connectivity",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Domain Expansion: Dijkstra from 0 0
    Mode: 8
Reveal: stdout
`,
			input: "191\n191\n111",
		},
		{
			// stderr must not reach stdout in either backend — that is the
			// whole point of the target.
			name: "reveal stderr stays off stdout",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count
Reveal: stderr
Cursed Technique: Apply
    Using: (n) -> n * 10
Reveal: stdout
`,
			input: "a\nb\nc",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p := p
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}

// Topological Sort across backends. The tie-breaking is the delicate part:
// both implementations must scan ready nodes in first-seen order, or they
// produce different-but-valid orders and the parity contract breaks.
func TestCompiledTopologicalSortMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			name: "edge list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Domain Expansion: Topological Sort
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "a -> b\na -> c\nb -> d\nc -> d",
		},
		{
			name: "tie breaking is first-seen",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Domain Expansion: Topological Sort
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "b -> z\na -> z\nc -> z\nd -> z",
		},
		{
			name: "integer nodes",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{int} -> {int}"
Domain Expansion: Topological Sort
Cursed Technique: Map Each
    Using: (n) -> totext(n)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "3 -> 1\n3 -> 2\n1 -> 9\n2 -> 9",
		},
		{
			name: "adjacency map form",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{a:word} -> {b:word}"
Maximum Technique: Group By
    Using: (r) -> r.a
Cursed Technique: Map Values
    Using: (rs) -> list(item(rs, 0).b)
Domain Expansion: Topological Sort
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "a -> b\nb -> c",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p := p
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}

// The list primitives accept a Set, which compiles to dmSet and so has to be
// unwrapped to its .elems slice before anything can range over it.
func TestCompiledSetAsListMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		{
			name: "map and filter over a set",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Set
Cursed Technique: Map Each
    Using: (s) -> upper(s)
Cursed Technique: Filter
    Using: (s) -> ikke startswith(s, "B")
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "apple\nbee\napple\ncow",
		},
		{
			// Insertion order, the order a Set already renders in.
			name: "set order is insertion order",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Set
Cursed Technique: Enumerate
Reveal: stdout
`,
			input: "c\na\nb\na",
		},
		{
			// A transform may collapse two elements onto one value; the result
			// is a List, so the duplicate is kept.
			name: "mapping a set does not deduplicate",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Set
Cursed Technique: Map Each
    Using: (s) -> charat(s, 0)
Maximum Technique: Join with ","
Reveal: stdout
`,
			input: "ax\nay\nbz",
		},
		{
			name: "group and count over a set",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Set
Maximum Technique: Count By
    Using: (s) -> charat(s, 0)
Reveal: stdout
`,
			input: "ax\nay\nbz\nax",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			p := p
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
				}
			})
		}
	}
}

// A Channel body consuming earlier channels: the compiled backend stores each
// channel in a Go variable as it emits it, so a later body reads one already
// in scope — but only because channel bodies are emitted in declaration order.
func TestCompiledChannelCompositionMatchesInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"

Channel "firsts":
    Cursed Technique: Map Each
        Using: (s) -> charat(s, 0)

Channel "lengths":
    Cursed Technique: Map Each
        Using: (s) -> length(s)

Channel "labelled":
    Maximum Technique: Zip
        From: firsts, lengths
    Cursed Technique: Map Each
        Using: (p) -> item(p, 0) + totext(item(p, 1))

Maximum Technique: Combine
    From: labelled
    Using: (xs) -> textjoin(xs, ",")
Reveal: stdout
`
	for _, optimize := range []bool{true, false} {
		mode := "naive"
		if optimize {
			mode = "optimized"
		}
		optimize := optimize
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			pipe := compilePipeline(t, src, optimize)
			want := runInterpreter(t, pipe, []byte("apple\nant\nbee\ncow"))
			got := buildAndRun(t, pipe, []byte("apple\nant\nbee\ncow"), codegen.Options{})
			if got != want {
				t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", want, got)
			}
		})
	}
}
