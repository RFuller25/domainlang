package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/docs"
)

// Every `[explain]` line quoted in optimizer.md, produced by a program.
//
// docs/optimizer.md is the page making the language's central claim — that a
// `Domain Expansion` names a *result*, not a method — and the way it makes it
// is by printing what `--explain` says. Nothing connected those lines to the
// optimizer. The neighbouring guard in docs/drift_test.go greps the Go sources
// for a phrase, which catches a rewording but not a rewrite that stopped
// firing, a pattern that narrowed, or a message assembled from parts that no
// longer combine that way.
//
// So each line has a program here that triggers it, and the test insists the
// page and the run agree in both directions: every line below is on the page,
// and every `[explain]` line on the page is claimed by one of these programs.
// A new pass documented with a new line fails this until it is given one.

// explainCase is one rewrite: the smallest program that triggers it, the input
// it runs on, and the answer it must still produce — the point of a rewrite
// being that it does not change that.
type explainCase struct {
	name    string
	program string
	input   string
	output  string
	explain string
	// slow marks a case whose *naive* run is the expensive one — the pass
	// exists precisely because the unoptimized shape is costly — so the oracle
	// comparison is skipped in -short mode.
	slow bool
}

var explainCases = []explainCase{
	{
		name: "sort then top k",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`,
		input:   "1000\n2000\n\n4000\n\n5000\n6000\n\n7000",
		output:  "22000",
		explain: "Domain rewrote Quicksort (Descending) + Top 3 → Cursed Quickselect. Guaranteed hit.",
	},
	{
		name: "three summing to a constant",
		program: `Cursed Energy: stdin
Shikigami: Ints
Domain Expansion: Combinations 3
    Mode: First
    Using: (a, b, c) -> a + b + c = 2020
Maximum Technique: Product
Reveal: stdout
`,
		input:   "1721\n979\n366\n299\n675\n1456",
		output:  "241861950",
		explain: "Domain rewrote Combinations 3 (sum = 2020) → Cursed Hash-Set Triple Scan. Guaranteed hit.",
	},
	{
		name: "a pair a fixed distance apart",
		program: `Cursed Energy: stdin
Shikigami: Ints
Domain Expansion: All Pairs
    Mode: Count
    Using: (a, b) -> a - b = 3
Reveal: stdout
`,
		input:   "4\n1\n7\n2",
		output:  "1",
		explain: "Domain rewrote All Pairs (difference = 3) → Cursed Hash-Set Scan. Guaranteed hit.",
	},
	{
		name: "the kth largest",
		program: `Cursed Energy: stdin
Shikigami: Ints
Domain Expansion: Quicksort, Descending
Cursed Technique: Take Item 1
Reveal: stdout
`,
		input:   "5\n3\n9\n1",
		output:  "5",
		explain: "Domain rewrote Quicksort (Descending) + Take Item 1 → Cursed Quickselect (kth order statistic). Guaranteed hit.",
	},
	{
		name: "a linear map before an extremum",
		program: `Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Map Each
    Using: (x) -> 0 - 2 * x + 1
Maximum Technique: Max
Reveal: stdout
`,
		input:   "5\n3\n9\n1",
		output:  "-1",
		explain: "Domain rewrote Map Each (linear) + Max → input Min + one application (monotone maps commute with extrema). Guaranteed hit.",
	},
	{
		name: "a pair with a fixed product",
		program: `Cursed Energy: stdin
Shikigami: Ints
Domain Expansion: All Pairs
    Mode: Count
    Using: (a, b) -> a * b = 12
Reveal: stdout
`,
		input:   "3\n4\n6\n2",
		output:  "2",
		explain: "Domain rewrote All Pairs (product = 12) → Cursed Divisor Scan. Guaranteed hit.",
	},
	{
		name: "summing every window",
		program: `Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Window 3
Cursed Technique: Map Each
    Using: (w) -> sum(w)
Reveal: stdout
`,
		input:   "1\n2\n3\n4\n5",
		output:  "[6, 9, 12]",
		explain: "Domain rewrote Window 3 + Map Each (sum) → Cursed Sliding-Window Sum (one pass, no window lists). Guaranteed hit.",
	},
	{
		name: "a search read at one cell",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: BFS from 0 0
    Using: (c) -> c = "."
Cursed Technique: Apply
    Using: (g) -> at(g, 2, 2)
Reveal: stdout
`,
		input:   "...\n...\n...",
		output:  "4",
		explain: "Domain rewrote BFS + at(2, 2) → early-exit search (stops when the target settles). Guaranteed hit.",
	},
	{
		name: "the first element passing a test",
		program: `Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Filter
    Using: (x) -> x > 5
Cursed Technique: Take Item 0
Reveal: stdout
`,
		input:   "1\n7\n3\n9",
		output:  "7",
		explain: "Domain rewrote Filter + Take Item 0 → Cursed First Match (stops at the first hit). Guaranteed hit.",
	},
	{
		name: "a bounded generator, terminated by take",
		program: `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> 1
Cursed Technique: Unfold
    While: (x) -> x < 5000000
    Using: (x) -> x + 1
Cursed Technique: Map Each
    Using: (x) -> x * 2
Cursed Technique: Filter
    Using: (x) -> x % 3 = 0
Cursed Technique: Apply
    Using: (x) -> take(x, 5000000)
Maximum Technique: Count
Reveal: stdout
`,
		input:   "x",
		output:  "1666666",
		explain: "Domain rewrote Unfold + Map Each + Filter + Apply (take 5000000) → Cursed Stream (early exit). Guaranteed hit.",
		slow:    true,
	},
	{
		name: "a predicate that folds",
		program: `Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Filter
    Using: (x) -> (1 = 1) and (x > 1)
Reveal: stdout
`,
		input:   "1\n2\n3",
		output:  "[2, 3]",
		explain: "Domain simplified the Using: lambda of Filter (boolean short-circuit, constant folding). Guaranteed hit.",
	},
	{
		name: "a map built one write at a time",
		program: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Maximum Technique: Fold
    Seed: (xs) -> emptymap("", 0)
    Using: (acc, w) -> insert(acc, w, getor(acc, w, 0) + 1)
Reveal: stdout
`,
		input:   "a,b,a",
		output:  "{a: 2, b: 1}",
		explain: "Domain made 1 accumulator update(s) in Fold write in place — the copy was never read.",
	},
	{
		name: "a loop state written through",
		program: `Cursed Energy: stdin
Shikigami: Ints
Simple Domain: While
    Using: (xs) -> item(xs, 0) < 5
    Cursed Technique: Apply
        Using: (xs) -> set(xs, 0, item(xs, 0) + 1)
Reveal: stdout
`,
		input:   "1\n2\n3",
		output:  "[5, 2, 3]",
		explain: "Domain made 1 state update(s) in While write in place — the copy was never read.",
	},
}

func TestOptimizerExplainLinesAreProduced(t *testing.T) {
	page, err := docs.FS.ReadFile("optimizer.md")
	if err != nil {
		t.Fatal(err)
	}
	quoted := map[string]bool{}
	for line := range strings.SplitSeq(string(page), "\n") {
		if rest, ok := strings.CutPrefix(line, "[explain] "); ok {
			quoted[rest] = true
		}
	}
	if len(quoted) == 0 {
		t.Fatal("optimizer.md quotes no [explain] output — the extraction has stopped matching")
	}

	claimed := map[string]bool{}
	for _, c := range explainCases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			prog := filepath.Join(dir, "p.domain")
			if err := os.WriteFile(prog, []byte(c.program), 0o644); err != nil {
				t.Fatal(err)
			}
			var out, errBuf bytes.Buffer
			if err := Execute(prog, Options{Optimize: true, Explain: true},
				strings.NewReader(c.input), &out, &errBuf); err != nil {
				t.Fatalf("running: %v\n%s", err, errBuf.String())
			}
			if got := strings.TrimRight(out.String(), "\n"); got != c.output {
				t.Errorf("output = %q, want %q", got, c.output)
			}
			if want := "[explain] " + c.explain; !strings.Contains(errBuf.String(), want) {
				t.Errorf("--explain said:\n%s\nwant a line reading:\n%s", errBuf.String(), want)
			}
			// The rewrite must not have changed the answer, which is the whole
			// promise it makes; the naive run is the oracle that says so.
			if c.slow && testing.Short() {
				return
			}
			var naive, naiveErr bytes.Buffer
			if err := Execute(prog, Options{Optimize: false},
				strings.NewReader(c.input), &naive, &naiveErr); err != nil {
				t.Fatalf("running unoptimized: %v\n%s", err, naiveErr.String())
			}
			if got := strings.TrimRight(naive.String(), "\n"); got != c.output {
				t.Errorf("the rewrite changed the answer: naive gives %q, optimized %q", got, c.output)
			}
		})
		// The page prints these lines without the "Guaranteed hit." suffix in
		// the two linear-accumulator blocks, so match on the prefix each time.
		for q := range quoted {
			if strings.HasPrefix(q, c.explain) || strings.HasPrefix(c.explain, q) {
				claimed[q] = true
			}
		}
	}

	for q := range quoted {
		if !claimed[q] {
			t.Errorf("optimizer.md prints this, and no program here produces it:\n[explain] %s", q)
		}
	}
}
