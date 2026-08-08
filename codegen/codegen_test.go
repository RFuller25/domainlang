package codegen_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// frontEnd runs the shared front end (lex → parse → resolve → optionally
// optimize) over program source.
// resolveMu serializes the front end. prims.Resolve keeps its binding and
// ambient scopes at package level and says so — "prims.Resolve / interp.Run
// are never called concurrently within one process" (prims/ambient.go) — while
// the oracle tests below run their subtests in parallel. Most programs never
// notice, because most have no Consider binding and no For loop to leak; one
// that does resolves against another test's scope and fails with an unknown
// identifier, intermittently and only under the full suite.
//
// The lock belongs here rather than in prims: serializing the resolver for
// every caller would be a change to its threading model, and the tests are the
// only thing that ever asked for concurrency.
var resolveMu sync.Mutex

func frontEnd(src string, optimize bool) (*ir.Pipeline, error) {
	resolveMu.Lock()
	defer resolveMu.Unlock()
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, fmt.Errorf("lex: %w", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		return nil, fmt.Errorf("resolve: %w", err)
	}
	optimizer.Optimize(pipe, optimize)
	return pipe, nil
}

// compilePipeline is frontEnd with test-fatal error handling.
func compilePipeline(t *testing.T, src string, optimize bool) *ir.Pipeline {
	t.Helper()
	pipe, err := frontEnd(src, optimize)
	if err != nil {
		t.Fatal(err)
	}
	return pipe
}

// runInterpreter produces the oracle stdout for a pipeline.
func runInterpreter(t *testing.T, pipe *ir.Pipeline, input []byte) string {
	t.Helper()
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &out}
	if _, err := interp.Run(pipe, ctx); err != nil {
		t.Fatalf("interpreter: %v", err)
	}
	return out.String()
}

// buildAndRun compiles the pipeline to Go, builds a binary, and runs it with
// the given stdin from an empty working directory (so Read Source falls back
// to stdin, exactly like the interpreter tests).
func buildAndRun(t *testing.T, pipe *ir.Pipeline, input []byte, opts codegen.Options) string {
	t.Helper()
	goSrc, err := codegen.EmitProgram(pipe, opts)
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatalf("BuildBinary: %v\n--- generated source ---\n%s", err, goSrc)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("compiled binary: %v\nstderr: %s\n--- generated source ---\n%s", err, errb.String(), goSrc)
	}
	return out.String()
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
}

// The anchor programs from testdata, compiled and diffed against the
// interpreter oracle in both optimizer modes.
func TestCompiledAnchorsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	td := "../testdata"
	anchors := []struct {
		name    string
		program string
		input   string
	}{
		{"day1", "day1.domain", "day1_input.txt"},
		{"day1_shikigami", "day1_shikigami.domain", "day1_input.txt"},
		{"day4", "day4.domain", "day4_input.txt"},
		{"day5", "day5.domain", "day5_input.txt"},
		{"day5_full", "day5_full.domain", "day5_input.txt"},
		{"day8", "day8.domain", "day8_input.txt"},
		{"day8_full", "day8_full.domain", "day8_input.txt"},
		{"aoc2020_day1", "aoc2020_day1.domain", "aoc2020_day1_input.txt"},
		{"aoc2020_day1_part2", "aoc2020_day1_part2.domain", "aoc2020_day1_input.txt"},
	}
	for _, a := range anchors {
		src, err := os.ReadFile(filepath.Join(td, a.program))
		if err != nil {
			t.Fatalf("reading %s: %v", a.program, err)
		}
		input, err := os.ReadFile(filepath.Join(td, a.input))
		if err != nil {
			t.Fatalf("reading %s: %v", a.input, err)
		}
		for _, mode := range []struct {
			name     string
			optimize bool
		}{{"optimized", true}, {"naive", false}} {
			// Resolution and interpretation stay on this goroutine: prims'
			// ambient For-loop stacks are package-level, and prims/ambient.go
			// documents that Resolve and Run are never called concurrently.
			// Only the Go build and the binary's run — where the time actually
			// goes — are parallel.
			pipe := compilePipeline(t, string(src), mode.optimize)
			want := runInterpreter(t, pipe, input)
			t.Run(a.name+"/"+mode.name, func(t *testing.T) {
				t.Parallel()
				got := buildAndRun(t, pipe, input, codegen.Options{})
				if got != want {
					t.Errorf("compiled output diverges from interpreter\n got: %q\nwant: %q", got, want)
				}
			})
		}
	}
}

// Inline programs covering surfaces the anchors miss: the regex-fallback
// Match Pattern path (word holes) and list rendering at the Reveal sink.
func TestCompiledInlineProgramsMatchInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	progs := []struct {
		name  string
		src   string
		input string
	}{
		// Part blocks. The interpreter carries the label on ir.Context and
		// branches at runtime; the compiler bakes it in as a literal. These pin
		// the two to byte-identical output over every shape the label rule
		// distinguishes — scalar, multi-line text, composite, and grid.
		{
			name: "part blocks with scalar answers",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Part "1":
    Maximum Technique: Max
    Reveal: stdout
Part "2":
    Maximum Technique: Sum
    Reveal: stdout
`,
			input: "3\n1\n4\n1\n5",
		},
		{
			name: "part revealing a multi-line text",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Part "joined":
    Maximum Technique: Join with "\n"
    Reveal: stdout
`,
			input: "a,b,c",
		},
		{
			name: "part revealing a grid picture",
			src: `Cursed Energy: stdin
Shikigami: Lines
Part "picture":
    Channeled Energy: Convert To Grid
    Reveal: stdout
`,
			input: "ab\ncd",
		},
		{
			name: "part revealing a list and a float",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Part "list":
    Domain Expansion: Quicksort
    Reveal: stdout
Part "mean-ish":
    Channeled Energy: Convert List to Floats
    Maximum Technique: Sum
    Reveal: stdout
`,
			input: "5\n1\n9",
		},
		{
			name: "part passthrough with a following top-level reveal",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Part "1":
    Maximum Technique: Sum
    Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "3\n1\n4",
		},
		{
			name: "part with no reveal computes nothing observable",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Part "quiet":
    Maximum Technique: Sum
Maximum Technique: Count
Reveal: stdout
`,
			input: "3\n1\n4",
		},
		{
			name: "part consuming a channel defined above it",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Channel "total":
    Maximum Technique: Sum
Part "1":
    Maximum Technique: Combine
        From: total
        Using: (t) -> t * 2
    Reveal: stdout
`,
			input: "3\n1\n4",
		},
		{
			name: "part containing a loop and a vow",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Part "doubled":
    Simple Domain: Repeat 3
        Cursed Technique: Map Each
            Using: (x) -> x * 2
    Binding Vow: All Values > 0
    Reveal: stdout
`,
			input: "1\n2\n3",
		},
		// Shikigami signatures and richer parameters are resolve-time only —
		// the body is still inlined, so the compiler sees plain primitives.
		// These pin that: a lambda parameter must compile to the same inlined
		// expression a written-out lambda would.
		{
			name: "higher-order shikigami with a lambda parameter",
			src: `Shikigami "Count Where" (p: (Int) -> Bool) : List<Int> -> Int
    Maximum Technique: Count Matching
        Using: p
Cursed Energy: stdin
Shikigami: Ints
Shikigami: Count Where
    p: (x) -> x > 3
Reveal: stdout
`,
			input: "1\n5\n2\n9",
		},
		{
			name: "shikigami with float and bool parameters",
			src: `Shikigami "Scale If" (f: Float, on: Bool) : List<Int> -> List<Float>
    Cursed Technique: Map Each
        Using: (x) -> if on then x * f else x * 1.0
Cursed Energy: stdin
Shikigami: Ints
Shikigami: Scale If
    f: 2.5
    on: true
Reveal: stdout
`,
			input: "2\n4",
		},
		{
			name: "signed shikigami still fuses into a quickselect",
			src: `Shikigami "Top Two" : List<Int> -> Int
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top 2, Sum
Cursed Energy: stdin
Shikigami: Ints
Shikigami: Top Two
Reveal: stdout
`,
			input: "5\n1\n9\n7",
		},
		{
			name: "regex fallback word holes",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{w:word} {n:int}"
Cursed Technique: Map Each
    Using: (r) -> r.n
Maximum Technique: Sum
Reveal: stdout
`,
			input: "ab 1\ncd 2\nef 39",
		},
		{
			name: "reveal a list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3
Reveal: stdout
`,
			input: "5\n9\n1\n7\n3",
		},
		{
			name: "filter unique reverse vow",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Using: (x) -> x > 2
Cursed Technique: Unique
Reverse Cursed Technique: Reverse
Binding Vow: All Values > 2
Reveal: stdout
`,
			input: "5\n3\n5\n1\n9\n3",
		},
		{
			name: "fold with seed",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: 100
    Using: (acc, x) -> acc * 2 + x
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			name: "repeat loop",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Sum
Simple Domain: Repeat 3
    Cursed Technique: Apply
        Using: (v) -> v * 2
Reveal: stdout
`,
			input: "3\n4",
		},
		{
			name: "while loop with division",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Sum
Simple Domain: While
    Using: (n) -> n > 1
    Cursed Technique: Apply
        Using: (n) -> n / 2
Reveal: stdout
`,
			input: "60\n40",
		},
		{
			name: "fixed point over a list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Simple Domain: Iterate Until Fixed Point
    Cursed Technique: Filter
        Using: (x) -> x < 10
Reveal: stdout
`,
			input: "12\n3\n15\n7",
		},
		{
			// Regression: eqExpr's scalar fast path used to omit ir.KFloat, so
			// structural equality on any Float-containing composite fell to
			// eqFunc's composite-only switch and failed with "no equality for
			// type Float". A Fixed Point body that isn't a single bare Map
			// Each forces the general convergence-test path (g.eqExpr on the
			// whole List<Float>), which is exactly where this broke.
			name: "fixed point over a list of floats",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Floats
Simple Domain: Iterate Until Fixed Point
    Cursed Technique: Filter
        Using: (x) -> x < 10.0
Reveal: stdout
`,
			input: "12.5\n3.25\n15.0\n7.5",
		},
		{
			name: "group by renders a map",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Group By
    Using: (n) -> n / 3
Reveal: stdout
`,
			input: "1\n2\n3\n5\n8\n2",
		},
		{
			name: "intersect renders a set",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Maximum Technique: Intersect
Reveal: stdout
`,
			input: "abcd\ncbe\nfcb",
		},
		{
			name: "union then count",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Maximum Technique: Union
Maximum Technique: Count
Reveal: stdout
`,
			input: "abc\ncde\nefa",
		},
		{
			name: "difference of channels",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"

Channel "one":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by ","

Channel "two":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by ","

Maximum Technique: Difference
    From: one, two

Reveal: stdout
`,
			input: "a,b,c\nb,c,d",
		},
		{
			name: "tuple match pattern",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{int}x{word}"
Reveal: stdout
`,
			input: "5xabc\n-7xq",
		},
		{
			name: "list builtins in map each",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Cursed Technique: Map Each
    Using: (g) -> item(g, 0) * 1000 + first(g) + last(g) + sum(take(g, 2)) + max(g) - min(g) + length(drop(g, 1))
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1\n2\n3\n\n4\n5",
		},
		{
			name: "contains reverse concat builtins",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ","
Channeled Energy: Convert Each List to Integers
Cursed Technique: Filter
    Using: (g) -> contains(g, 5)
Cursed Technique: Map Each
    Using: (g) -> sum(concat(reverse(g), take(g, 1)))
Reveal: stdout
`,
			input: "1,5,2\n3,4\n5,5",
		},
		{
			name: "grid at builtin",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Cursed Technique: Apply
    Using: (g) -> at(g, 1, 2) * 10 + at(g, 0, 0)
Reveal: stdout
`,
			input: "123\n456",
		},
		{
			name: "map get builtin",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Group By
    Using: (n) -> n / 3
Cursed Technique: Apply
    Using: (m) -> sum(get(m, 1))
Reveal: stdout
`,
			input: "1\n3\n4\n5\n8\n2",
		},
		{
			name: "composite equality in combine",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"

Channel "one":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by ","
    Channeled Energy: Convert List to Integers

Channel "two":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by ","
    Channeled Energy: Convert List to Integers

Maximum Technique: Combine
    From: one, two
    Using: (a, b) -> a = b

Reveal: stdout
`,
			input: "1,2,3\n1,2,3",
		},
		// The v0.4 optimizer passes: in optimized mode these compile the
		// rewritten nodes (QuickselectItem, HashSetTripleScan,
		// HashSetDiffScan, LinearMapExtremum, fused/elided shapes); in naive
		// mode the original primitives. Both must match the interpreter.
		{
			name: "quickselect item",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort, Descending
Cursed Technique: Take Item 1
Reveal: stdout
`,
			input: "5\n9\n1\n7\n3",
		},
		{
			name: "triple sum count",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Combinations 3
    Mode: Count
    Using: (a, b, c) -> a + b + c = 10
Reveal: stdout
`,
			input: "1\n2\n7\n3\n5\n2\n4",
		},
		{
			name: "triple sum first",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Combinations 3
    Mode: First
    Using: (a, b, c) -> a + b + c = 12
Reveal: stdout
`,
			input: "1\n2\n7\n3\n5\n2",
		},
		{
			name: "pair diff first",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: All Pairs
    Mode: First
    Using: (a, b) -> a - b = 3
Reveal: stdout
`,
			input: "9\n4\n6\n1",
		},
		{
			name: "pair diff count flipped",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: All Pairs
    Mode: Count
    Using: (a, b) -> b - a = 2
Reveal: stdout
`,
			input: "1\n3\n5\n3\n1\n7",
		},
		{
			name: "linear map extremum",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> 3 - 2 * x
Maximum Technique: Max
Reveal: stdout
`,
			input: "5\n-2\n7\n0",
		},
		{
			name: "fusion cascade with constant predicate",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> x * 2
Cursed Technique: Map Each
    Using: (y) -> y + 1
Cursed Technique: Filter
    Using: (x) -> 1 = 1 and x > 0
Cursed Technique: Filter
    Using: (y) -> y < 15
Maximum Technique: Count
Reveal: stdout
`,
			input: "3\n-4\n1\n9\n0",
		},
		{
			name: "reorder elision cascade",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort
Reverse Cursed Technique: Reverse
Reverse Cursed Technique: Reverse
Cursed Technique: Unique
Maximum Technique: Max
Reveal: stdout
`,
			input: "4\n1\n4\n9\n2",
		},
		{
			name: "math builtins in map each",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> abs(n) * sign(n) + gcd(n, 12) + lcm(n, 4) + modpow(n, 3, 97) + modinv(3, 11)
Maximum Technique: Sum
Reveal: stdout
`,
			input: "-6\n0\n9\n15",
		},
		{
			name: "text builtins toint occurrences repeats",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count Matching
    Using: (s) -> repeats(s) or occurrences(s, "z") > 1 or toint("3") = 3 and s = "three"
Reveal: stdout
`,
			input: "abab\nzz top z\nthree\nplain",
		},
		// The M22 optimizer passes: in optimized mode these compile the
		// rewritten nodes (WindowedReduce, DivisorPairScan); in naive mode the
		// original Window + Map Each / All Pairs. Both must match.
		{
			name: "windowed sum",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Window 3
Cursed Technique: Map Each
    Using: (w) -> sum(w)
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5",
		},
		{
			name: "windowed max with step",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Window 2 2
Cursed Technique: Map Each
    Using: (w) -> max(w)
Reveal: stdout
`,
			input: "5\n1\n4\n4\n7\n-2",
		},
		{
			name: "windowed min",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Window 4
Cursed Technique: Map Each
    Using: (w) -> min(w)
Reveal: stdout
`,
			input: "3\n-1\n8\n0\n2\n9",
		},
		{
			name: "pair product count with zeros",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: All Pairs
    Mode: Count
    Using: (a, b) -> a * b = 0
Reveal: stdout
`,
			input: "0\n3\n0\n4\n-2",
		},
		{
			name: "pair product first",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: All Pairs
    Mode: First
    Using: (a, b) -> b * a = 12
Reveal: stdout
`,
			input: "2\n8\n6\n3",
		},
		// Composite Map/Set keys (M25): tuples lower to comparable Tup
		// structs, so dmMap/dmSet key on them directly. Both backends must
		// agree on values and insertion-ordered rendering.
		{
			name: "unique over points",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> point(x, x)
Cursed Technique: Unique
Reveal: stdout
`,
			input: "3\n3\n4\n3",
		},
		{
			name: "set of points and contains",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (x) -> point(x, 0)
Channeled Energy: Convert To Set
Cursed Technique: Apply
    Using: (s) -> contains(s, point(3, 0))
Reveal: stdout
`,
			input: "1\n3\n5",
		},
		{
			name: "count by point key",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Count By
    Using: (x) -> point(x / 2, 0)
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5",
		},
		{
			name: "group by point key and get",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Group By
    Using: (x) -> point(x / 10, 0)
Cursed Technique: Apply
    Using: (m) -> length(get(m, point(1, 0)))
Reveal: stdout
`,
			input: "5\n12\n17\n23",
		},
		// The M26 early-exit search fusion: optimized mode compiles the
		// SearchTarget node, naive mode the full search + at().
		{
			name: "bfs at target",
			src: `Cursed Energy: stdin
Shikigami: Digit Grid
Domain Expansion: BFS from 0 0
    Using: (c) -> c < 5
Cursed Technique: Apply
    Using: (g) -> at(g, 2, 2)
Reveal: stdout
`,
			input: "123\n145\n110",
		},
		{
			name: "bfs at unreachable target",
			src: `Cursed Energy: stdin
Shikigami: Digit Grid
Domain Expansion: BFS from 0 0
    Using: (c) -> c < 5
Cursed Technique: Apply
    Using: (g) -> at(g, 2, 2)
Reveal: stdout
`,
			input: "119\n911\n111",
		},
		{
			name: "dijkstra at target",
			src: `Cursed Energy: stdin
Shikigami: Digit Grid
Domain Expansion: Dijkstra from 0 0
Cursed Technique: Apply
    Using: (g) -> at(g, 1, 2)
Reveal: stdout
`,
			input: "123\n145\n110",
		},
		{
			name: "inbounds and set contains in combine",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"

Channel "grid":
    Channeled Energy: Convert To Grid

Channel "seen":
    Cursed Technique: Split Each by ""
    Maximum Technique: Union

Maximum Technique: Combine
    From: grid, seen
    Using: (g, s) -> inbounds(g, 1, 2) and contains(s, "a") and inbounds(g, 9, 0) = (1 = 2)
Reveal: stdout
`,
			input: "abc\ndef",
		},
		{
			name: "float pipeline with builtins and mixed promotion",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Floats
Cursed Technique: Map Each
    Using: (x) -> sqrt(x * x) + x / 4 + tofloat(round(x)) * 2 + abs(0.0 - x)
Domain Expansion: Sort, Descending
Maximum Technique: Sum
Reveal: stdout
`,
			input: "2.5\n0.75\n3\n1.125",
		},
		{
			name: "float min max product reveal list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Floats
Cursed Technique: Map Each
    Using: (x) -> if x > 1.5 then x * 0.5 else x + 0.25
Domain Expansion: Sort
Reveal: stdout
`,
			input: "2.5\n0.75\n4.125\n1.5",
		},
		{
			// Regression: sum of a runtime-empty List<Float> must be
			// float64(0), not int64(0). The Filter empties the list, so the
			// interpreter's sum builtin can no longer sniff Float from the
			// elements — the static element type has to decide, exactly as
			// it does in the compiled binary. The division makes a wrong
			// int64 zero observable: 7 / (0 + 2) is 3 over Int but 3.5 over
			// Float.
			name: "sum of runtime-empty float list stays float",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Floats
Cursed Technique: Filter
    Using: (x) -> x > 100.0
Cursed Technique: Apply
    Using: (xs) -> 7 / (sum(xs) + 2)
Reveal: stdout
`,
			input: "2.5\n0.75",
		},

		// Measured arguments: an Int argument written as a lambda over the
		// current value instead of a literal. The size is only known at
		// runtime, so the compiler emits a computed operand where it used to
		// emit a constant — these pin the two backends over every lowering
		// that grew one.
		{
			name: "window measured at half the list",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Window
    Size: (xs) -> length(xs) / 2
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5\n6",
		},
		{
			// The measured size feeds a reduce, which is the shape the
			// Window+Map Each fusion matches on. The pass must stand down and
			// both backends must still agree.
			name: "measured window feeding a reduce",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Window
    Size: (xs) -> max(1, length(xs) / 3)
    Step: (xs) -> 2
Cursed Technique: Map Each
    Using: (w) -> sum(w)
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5\n6\n7\n8\n9",
		},
		{
			name: "chunk measured from the list length",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Chunk
    Size: (xs) -> length(xs) / 3
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5\n6\n7\n8",
		},
		{
			// Sort + Select Top K is the quickselect rewrite's pair; a measured
			// count must keep the honest sort rather than fusing to Top 0.
			name: "select top measured after a sort",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top
    Count: (xs) -> length(xs) / 2
Reveal: stdout
`,
			input: "3\n1\n4\n1\n5\n9\n2\n6",
		},
		{
			name: "measured select top then sum",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top, Sum
    Count: (xs) -> length(xs) / 4
Reveal: stdout
`,
			input: "3\n1\n4\n1\n5\n9\n2\n6",
		},
		{
			// The compiler's fusion rules key on the separator's *value*, and
			// three of them fire on the empty string. A measured separator has
			// no literal, so this pins that they stand down rather than firing
			// on a fabricated "" — the digit-grid fast path would otherwise
			// take a program it never saw the separator of.
			name: "measured empty separator does not take the fused fast path",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each
    By: (xs) -> ""
Channeled Energy: Convert To Integers
Channeled Energy: Convert To Grid
Reveal: stdout
`,
			input: "12\n34",
		},
		{
			// A measured argument reaching a slot through a Shikigami's lambda
			// parameter: the body is inlined, so the compiler sees the measured
			// slot exactly as if it had been written inline.
			name: "measured argument through a Shikigami parameter",
			src: `Shikigami "Sized Windows" (size: (List<Int>) -> Int)
    Cursed Technique: Window
        Size: size

Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Shikigami: Sized Windows
    size: (xs) -> length(xs) / 2
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5\n6",
		},
		{
			// The grid family: a crop, a border and a search start all sized
			// from the grid itself. The search also exercises the fused
			// lines-to-search path standing down — that fusion exists to avoid
			// materializing the grid a measured start needs.
			name: "measured grid crop, pad and search start",
			src: `Cursed Energy: stdin
Shikigami: Lines
Channeled Energy: Convert To Grid
Cursed Technique: Pad Grid
    Thickness: (g) -> 1
    Fill: (g) -> "."
Cursed Technique: Subgrid
    Row: (g) -> 1
    Col: (g) -> 1
    Height: (g) -> rows(g) - 2
    Width: (g) -> cols(g) - 2
Domain Expansion: BFS
    Row: (g) -> rows(g) - 1
    Col: (g) -> cols(g) - 1
    Using: (c) -> c = "."
Reveal: stdout
`,
			input: "...\n.#.\n...",
		},
		{
			// The accumulator's Go type comes from the seed, so a measured one
			// compiles to a struct rather than an int64 — the widest reach a
			// measured argument has into the backend.
			name: "measured fold seed with a tuple accumulator",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Fold
    Seed: (xs) -> tuple(0, 0)
    Using: (acc, x) -> tuple(prow(acc) + x, pcol(acc) + 1)
Reveal: stdout
`,
			input: "3\n1\n4\n1\n5",
		},
		{
			name: "measured fold and scan seeds from the data",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Scan
    Seed: (xs) -> length(xs)
    Using: (acc, x) -> acc + x
Maximum Technique: Fold
    Seed: (xs) -> first(xs)
    Using: (acc, x) -> max(acc, x)
Reveal: stdout
`,
			input: "3\n1\n4\n1\n5",
		},
		{
			name: "measured fill and sparse default",
			src: `Cursed Energy: stdin
Shikigami: Lines
Channeled Energy: Convert To Grid
Cursed Technique: Pad Grid 1
    Fill: (g) -> at(g, 0, 0)
Channeled Energy: Convert To Sparse Grid
    Default: (g) -> at(g, 0, 0)
Channeled Energy: Convert To Grid
Reveal: stdout
`,
			input: "ab\ncd",
		},
		{
			name: "measured sparse default and mark over points",
			src: `Cursed Energy: stdin
Cursed Technique: Extract Integers
Cursed Technique: Chunk 2
Channeled Energy: Convert To Sparse Grid
    Default: (ps) -> "."
    Mark: (ps) -> "#"
Channeled Energy: Convert To Grid
Reveal: stdout
`,
			input: "0 0\n1 1\n0 2",
		},
		{
			name: "measured separators, split and join",
			src: `Cursed Energy: stdin
Cursed Technique: Split
    By: (t) -> if indexof(t, "|") >= 0 then "|" else ","
Maximum Technique: Join
    With: (xs) -> if length(xs) > 2 then " | " else "-"
Reveal: stdout
`,
			input: "a|b|c",
		},
		{
			name: "measured sliding reduce",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: Sliding Reduce
    Size: (xs) -> length(xs) / 2
    Mode: Max
Reveal: stdout
`,
			input: "5\n1\n9\n2\n7\n3",
		},
		{
			name: "measured take item, iterate and repeat",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Simple Domain: Repeat
    Times: (xs) -> length(xs)
    Cursed Technique: Map Each
        Using: (n) -> n + 1
Cursed Technique: Take Item
    Index: (xs) -> length(xs) - 1
Cursed Technique: Iterate
    Times: (n) -> 3
    Using: (n) -> n * 2
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// Range discards its input, so a measured bound is the only way to
			// size it from the data.
			name: "measured range bounds",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Range
    Low: (xs) -> first(xs)
    High: (xs) -> last(xs)
Binding Vow: Count Equals
    Count: (xs) -> 4
Reveal: stdout
`,
			input: "2\n6",
		},
		{
			// Inside a For loop the measuring lambda takes the lap's binding as
			// a trailing parameter, exactly like a Using: lambda there.
			name: "measured window inside a for loop",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Simple Domain: For k in range(2)
    Cursed Technique: Window
        Size: (xs, k) -> k + 1
    Cursed Technique: Map Each
        Using: (w, k) -> sum(w)
Reveal: stdout
`,
			input: "1\n2\n3\n4",
		},

		// A Using: written as an indented pipeline body. The compiler lowers
		// the body to a top-level function and calls it where the lambda
		// expression would have gone; the interpreter runs the same node list
		// per invocation. These pin the two together over the shapes that
		// differ structurally — a per-element search, a reducing body, a
		// nested body, an ambient loop variable reaching into one (which the
		// emitted function takes as an extra parameter), a Set input, and the
		// spread of primitives the form reaches beyond Map Each.
		{
			name: "using body: per-row pair search",
			src: `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> (ikke b = 0 and a % b = 0) or (ikke a = 0 and b % a = 0)
    Maximum Technique: Reduce
        Using: (x, y) -> max(x, y) / min(x, y)
Maximum Technique: Sum
Reveal: stdout
`,
			input: "5 9 2 8\n9 4 7 3\n3 8 6 5",
		},
		{
			// The sum-to-constant rewrite fires inside the body in optimized
			// mode and not in naive mode; both must agree with the interpreter.
			name: "using body: optimizable pair sum per row",
			src: `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> a + b = 100
    Maximum Technique: Product
Reveal: stdout
`,
			input: "1 5 99 3\n50 50 2",
		},
		{
			name: "using body: reducing body",
			src: `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Map Each
    Cursed Technique: Extract Integers
    Maximum Technique: Sum
Reveal: stdout
`,
			input: "1 2 3\n40 50",
		},
		{
			name: "using body: nested bodies",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Cursed Technique: Map Each
    Cursed Technique: Map Each
        Cursed Technique: Extract Integers
        Maximum Technique: Sum
    Maximum Technique: Max
Reveal: stdout
`,
			input: "1 2 3\n4 5 6\n\n7 8\n9 10",
		},
		{
			name: "using body: ambient For variable inside the body",
			src: `Cursed Energy: stdin
Shikigami: Ints
Channel "deltas":
    Cursed Technique: Apply
        Using: (xs) -> list(10, 100)
Cursed Technique: Map Each
    Using: (n) -> list(n, n)
Simple Domain: For d in deltas
    Cursed Technique: Map Each
        Cursed Technique: Map Each
            Using: (n, d) -> n + d
Reveal: stdout
`,
			input: "1\n2",
		},
		{
			name: "using body: over a set",
			src: `Cursed Energy: stdin
Shikigami: Ints
Channeled Energy: Convert To Set
Cursed Technique: Map Each
    Cursed Technique: Apply
        Using: (n) -> n * 2
Reveal: stdout
`,
			input: "3\n1\n3\n2",
		},
		{
			// Beyond Map Each: a predicate body, a sort-key body, a grouping
			// body and a whole-value body, in one program so the emitted
			// functions also have to coexist.
			name: "using body: across primitives",
			src: `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Extract Integers
Cursed Technique: Filter
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (s) -> s > 15
Domain Expansion: Sort By
    Maximum Technique: Max
Part "grouped":
    Maximum Technique: Group By
        Maximum Technique: Count
    Reveal: stdout
Part "total":
    Maximum Technique: Sum By
        Maximum Technique: Max
    Reveal: stdout
Part "size":
    Cursed Technique: Apply
        Maximum Technique: Count
    Reveal: stdout
`,
			input: "5 9 2 8\n9 4 7 3\n1 3 5",
		},
		{
			// A Reveal inside a body prints per invocation — the compiled
			// backend bakes the Part label in, the interpreter carries it on
			// the Context, and both have to agree on the interleaving.
			name: "using body: reveal from inside the body",
			src: `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Map Each
    Cursed Technique: Extract Integers
    Maximum Technique: Sum
    Reveal: stdout
Maximum Technique: Sum
Reveal: stdout
`,
			input: "1 2\n3 4",
		},
		// `Consider` bindings. A constant and a function binding are gone by
		// the time the backend sees the program — folded into the lambda and
		// inlined at their call sites — so what these pin is the third kind:
		// a Consider node, its value compiled once per scope, read by name
		// from inside the lambdas the scope covers.
		{
			name: "consider: an Of binding read by a lambda",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Consider mean Of (xs) -> sum(xs) / length(xs)
    Using: (x) -> x > mean
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5",
		},
		{
			name: "consider: an Of binding written as an operation",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Consider total Of Sum
    Using: (x) -> x + total
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			name: "consider: an Of binding written as a sub-pipeline",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Consider scaled Of
        Maximum Technique: Sum
        Cursed Technique: Apply
            Using: (s) -> s * 10
    Using: (x) -> x + scaled
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			name: "consider: mixed kinds, and a binding of a binding",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Domain Expansion: All Pairs
    Mode: Count
    Consider accum As 3
    Consider double As (x) -> x * 2
    Consider total Of Sum
    Consider bar As total - accum
    Using: (a, b) -> double(a) + b + accum > bar
Reveal: stdout
`,
			input: "1\n2\n3\n4\n5",
		},
		{
			name: "consider: a value with no literal form",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Consider ds As list(10, 20, 30)
    Using: (x) -> x + item(ds, 1)
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// The binding is a local of main; the body compiles to a top-level
			// function, so it has to travel there as a parameter.
			name: "consider: a binding read from inside a Using: body",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Consider bump Of Sum
    Cursed Technique: Apply
        Using: (x) -> x * 100 + bump
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			// Both rails at once: a For loop's ambient variable is positional,
			// a binding is by name, and the lambda reads one of each.
			name: "consider: a binding beside a For loop's ambient variable",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Channel "steps":
    Cursed Technique: Apply
        Using: (xs) -> list(1, 2)
Simple Domain: For k in steps
    Consider total Of Sum
    Cursed Technique: Map Each
        Using: (x, k) -> x + k * total
Reveal: stdout
`,
			input: "1\n2\n3",
		},
		{
			name: "consider: a binding inside a Shikigami body",
			src: `Shikigami "Bumped" (k: Int) : List<Int> -> List<Int>
    Consider bump Of Sum
    Cursed Technique: Map Each
        Using: (x) -> x + bump + k

Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Shikigami: Bumped
    k: 100
Reveal: stdout
`,
			input: "1\n2\n3",
		},
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			// Sequential resolve + interpret, parallel compile — see the note
			// in TestCompiledAnchorsMatchInterpreter.
			pipe := compilePipeline(t, p.src, optimize)
			want := runInterpreter(t, pipe, []byte(p.input))
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				got := buildAndRun(t, pipe, []byte(p.input), codegen.Options{})
				if got != want {
					t.Errorf("compiled output diverges from interpreter\n got: %q\nwant: %q", got, want)
				}
			})
		}
	}
}

// The day4 template is all-int with safe literals, so it must take the
// hand-rolled scanner path (no regexp at runtime); a word hole must fall back.
func TestMatchPatternPathSelection(t *testing.T) {
	day4 := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{a:int}-{b:int},{c:int}-{d:int}"
Maximum Technique: Count Matching
    Using: (r) -> r.a <= r.b
Reveal: stdout
`
	pipe := compilePipeline(t, day4, true)
	src, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	if strings.Contains(src, "regexp") {
		t.Errorf("all-int template should compile to a hand-rolled scanner, but the generated source uses regexp:\n%s", src)
	}

	// A word hole delimited by a whitespace literal compiles to a hand-rolled
	// scanner (the greedy \S+ run stops at the whitespace with no backtracking).
	word := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{w:word} {n:int}"
Cursed Technique: Map Each
    Using: (r) -> r.n
Maximum Technique: Sum
Reveal: stdout
`
	pipe = compilePipeline(t, word, true)
	src, err = codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	if strings.Contains(src, "regexp") {
		t.Errorf("whitespace-delimited word-hole template should compile to a scanner, but uses regexp:\n%s", src)
	}

	// A text hole (.*) still needs the regex engine.
	text := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{s:text}:{n:int}"
Cursed Technique: Map Each
    Using: (r) -> r.n
Maximum Technique: Sum
Reveal: stdout
`
	pipe = compilePipeline(t, text, true)
	src, err = codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	if !strings.Contains(src, "regexp.MustCompile") {
		t.Errorf("text-hole template should fall back to regexp, generated source:\n%s", src)
	}

	// A repeated hole matches a run of unknown length, which a left-to-right
	// scan over fixed literals cannot bound — so it takes the regexp path even
	// though every element is an int.
	repeat := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{ns:int+ sep=\",\"}"
Cursed Technique: Map Each
    Using: (r) -> sum(r.ns)
Maximum Technique: Sum
Reveal: stdout
`
	pipe = compilePipeline(t, repeat, true)
	src, err = codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	if !strings.Contains(src, "regexp.MustCompile") {
		t.Errorf("repeated-hole template should fall back to regexp, generated source:\n%s", src)
	}
}

// Fusion is the other path selection Match Pattern makes, and Mode: Try opts
// out of it: every fused loop assumes each line parses and fails the program
// when one does not, which is the behavior Try exists to replace. The
// observable is the `[]string` — under fusion the Split never materializes one,
// and under Try it must.
func TestModeTryStandsFusionDown(t *testing.T) {
	prog := func(mode string) string {
		return `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: ` + mode + `
    Using: "{a:int}-{b:int}"
Maximum Technique: Count Matching
    Using: (r) -> r.a <= r.b
Reveal: stdout
`
	}
	emit := func(mode string) string {
		src, err := codegen.EmitProgram(compilePipeline(t, prog(mode), true), codegen.Options{})
		if err != nil {
			t.Fatalf("Mode: %s: EmitProgram: %v", mode, err)
		}
		return src
	}
	if got := emit("Each"); strings.Contains(got, "strings.Split(") {
		t.Errorf("Split + Match Pattern (Each) + Count Matching should fuse into one loop, "+
			"but the generated source still splits:\n%s", got)
	}
	if got := emit("Try"); !strings.Contains(got, "strings.Split(") {
		t.Errorf("Mode: Try must not fuse — the fused loop cannot drop a line — "+
			"but the generated source fused anyway:\n%s", got)
	}
}

func TestEmitIsDeterministic(t *testing.T) {
	src, err := os.ReadFile("../testdata/day5.domain")
	if err != nil {
		t.Fatal(err)
	}
	a, err := codegen.EmitProgram(compilePipeline(t, string(src), true), codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	b, err := codegen.EmitProgram(compilePipeline(t, string(src), true), codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	if a != b {
		t.Error("EmitProgram is not deterministic for the same program")
	}
}

// Every real v0.2 primitive now compiles, so the unsupported path is
// exercised with a synthetic node — the guard a future primitive hits if it
// ships without a codegen case.
func TestUnsupportedPrimitiveErrors(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{{
		Prim: "Frobnicate",
		In:   nil,
		Out:  ir.Int(),
	}}}
	_, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err == nil {
		t.Fatal("expected an unsupported-primitive error, got none")
	}
	if !strings.Contains(err.Error(), "Frobnicate") || !strings.Contains(err.Error(), "domain run") {
		t.Errorf("error should name the primitive and point at 'domain run': %v", err)
	}
}

// For loops used to be interpreter-only — the one advertised exception to
// "every primitive works in both backends". They compile now; what remains is
// that a malformed node still fails cleanly rather than emitting broken Go.
func TestForLoopWithoutMetadataErrors(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{{
		Prim: "Simple Domain (For)",
		In:   ir.List(ir.Int()),
		Out:  ir.List(ir.Int()),
	}}}
	_, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err == nil {
		t.Fatal("expected an error for a For node with no body metadata, got none")
	}
	if !strings.Contains(err.Error(), "Simple Domain (For)") {
		t.Errorf("error should name the primitive: %v", err)
	}
}

// TestCompiledToolboxMatchesInterpreter drives every AoC-toolbox primitive
// and point/tuple builtin through both backends (B.f5) — including the five
// examples/11–15 program shapes — and requires byte-identical stdout.
func TestCompiledToolboxMatchesInterpreter(t *testing.T) {
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
			name: "extract integers and merge ranges (int pairs)",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Maximum Technique: Merge Ranges
Reveal: stdout
`,
			input: "range 1 to 4\nrange 3 to 7\nrange 10 to 12\nrange 6 to 8\nrange 20 to 20",
		},
		{
			name: "extract integers signed corner cases",
			src: `Cursed Energy: stdin
Cursed Technique: Extract Integers
Reveal: stdout
`,
			input: "move 12 from -3 to 5, x=-7 and 36-92 --4",
		},
		{
			name: "merge ranges over records",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{lo:int}..{hi:int}"
Maximum Technique: Merge Ranges
Reveal: stdout
`,
			input: "1..2\n7..9\n3..3",
		},
		{
			name: "split fields both shapes",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Fields
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Reveal: stdout
`,
			input: "1 2 3\n 4\t 5 \n",
		},
		{
			name: "convert to set renders and counts",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Set
Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "a,b,a,c,b",
		},
		{
			name: "find cells feeds manhattan",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Find Cells
    Using: (c) -> c = "X"
Reveal: stdout
Cursed Technique: Apply
    Using: (ps) -> manhattan(first(ps), last(ps))
Reveal: stdout
`,
			input: "X.O\n.XX",
		},
		{
			name: "point rotations and padd in map each",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Cursed Technique: Map Each
    Using: (n) -> padd(rotr(point(n, 0)), rotl(point(0, n)))
Reveal: stdout
`,
			input: "1\n-2\n3",
		},
		{
			name: "dirs4 neighbors and solve2x2 in apply",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Apply
    Using: (g) -> length(neighbors4(g, 0, 0)) * 1000 + length(neighbors8(g, 1, 1)) * 100 + length(dirs4()) * 10 + prow(solve2x2(94, 22, 8400, 34, 67, 5400)) - pcol(solve2x2(94, 22, 8400, 34, 67, 5400))
Reveal: stdout
`,
			input: "abc\ndef\nghi",
		},
		{
			name: "permutations order and count",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Domain Expansion: Permutations
Reveal: stdout
Maximum Technique: Count
Reveal: stdout
`,
			input: "a,b,c",
		},
		{
			name: "subsets filtered by length and contains",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by ", "
Domain Expansion: Subsets
Reveal: stdout
Maximum Technique: Count Matching
    Using: (team) -> length(team) = 2 and contains(team, "ana")
Reveal: stdout
`,
			input: "ana, bo, cy",
		},
		{
			name: "bfs distances grid",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: BFS from 0 0
    Using: (c) -> c = "."
Reveal: stdout
Cursed Technique: Apply
    Using: (g) -> at(g, 4, 4)
Reveal: stdout
`,
			input: ".#...\n.#.#.\n.#.#.\n.#.#.\n...#.",
		},
		{
			name: "dijkstra risk map",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Domain Expansion: Dijkstra from 0 0
Reveal: stdout
`,
			input: "116\n138\n213",
		},
		{
			name: "flood fill mask then count",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Flood Fill from 0 0
    Using: (c) -> c = "#"
Reveal: stdout
Maximum Technique: Count Cells
    Using: (m) -> m = 1
Reveal: stdout
`,
			input: "##.\n.#.\n..#",
		},
		{
			name: "connected components",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Connected Components
    Using: (c) -> c = "#"
Reveal: stdout
`,
			input: "##..#\n#..##\n..#..\n#..##",
		},
		{
			// The else arm is partial (first of a possibly-empty list); the
			// empty middle line proves the arms stay lazy in both backends.
			name: "conditional with lazy partial arm",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Fields
Channeled Energy: Convert Each List to Integers
Cursed Technique: Map Each
    Using: (xs) -> if length(xs) = 0 then -1 else first(xs) + last(xs)
Reveal: stdout
`,
			input: "1 2 3\n\n5",
		},
		{
			name: "set list row col rows cols builtins",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Cursed Technique: Apply
    Using: (g) -> concat(set(row(g, 0), 0, 99), list(rows(g), cols(g), sum(col(g, 1))))
Reveal: stdout
`,
			input: "123\n456",
		},
		{
			name: "positional map cells and join",
			src: `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Map Cells
    Using: (g, r, c) -> if r = c then "\\" else at(g, r, c)
Reveal: stdout
Cursed Technique: Apply
    Using: (g) -> concat(row(g, 0), row(g, 1))
Maximum Technique: Join with "-"
Reveal: stdout
`,
			input: "ab\ncd",
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

// TestMergeRangesMaxInt64Adjacency guards against a signed-overflow bug in
// the generated adjacency check for Merge Ranges: comparing
// "merged[k-1][1]+1" against the next range's low end wraps to MinInt64 when
// the stored upper bound is math.MaxInt64, silently corrupting the merge
// decision (a range ending at MaxInt64 would fail to absorb a subsequent
// overlapping or adjacent range).
//
// The interpreter (prims/toolbox.go) has the exact same latent overflow bug,
// so it cannot serve as the oracle here — diffing against it would either
// mask the bug (if both are wrong the same way) or spuriously fail once only
// the compiler is fixed. Instead this checks the compiled binary's output
// directly against the hand-computed correct answer: a range ending at
// MaxInt64 followed by a single point exactly at MaxInt64 must merge into
// one range.
func TestMergeRangesMaxInt64Adjacency(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{lo:int}..{hi:int}"
Maximum Technique: Merge Ranges
Reveal: stdout
`
	input := "0..9223372036854775807\n9223372036854775807..9223372036854775807"
	const want = "[{lo: 0, hi: 9223372036854775807}]\n"
	for _, optimize := range []bool{true, false} {
		mode := "naive"
		if optimize {
			mode = "optimized"
		}
		optimize := optimize
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			pipe := compilePipeline(t, src, optimize)
			got := buildAndRun(t, pipe, []byte(input), codegen.Options{})
			if got != want {
				t.Errorf("compiled output = %q, want %q (an overflowing adjacency check would wrongly keep these as two separate ranges)", got, want)
			}
		})
	}
}

// TestReleaseModeStripsVows: a failing vow aborts the debug binary but is
// compiled out entirely in release mode, matching the release-mode
// interpreter.
func TestReleaseModeStripsVows(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Binding Vow: All Values > 100
Maximum Technique: Sum
Reveal: stdout
`
	input := []byte("1\n2\n3")
	pipe := compilePipeline(t, src, true)

	debugSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram(debug): %v", err)
	}
	if !strings.Contains(debugSrc, "vow violated") {
		t.Error("debug build should emit the vow check")
	}
	relSrc, err := codegen.EmitProgram(pipe, codegen.Options{Release: true})
	if err != nil {
		t.Fatalf("EmitProgram(release): %v", err)
	}
	if strings.Contains(relSrc, "vow violated") {
		t.Errorf("release build should compile the vow out, generated:\n%s", relSrc)
	}

	if testing.Short() {
		return
	}
	requireGo(t)

	// Release binary and release interpreter agree on the clean result.
	var want bytes.Buffer
	ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &want, Release: true}
	if _, err := interp.Run(pipe, ctx); err != nil {
		t.Fatalf("release interpreter: %v", err)
	}
	got := buildAndRun(t, pipe, input, codegen.Options{Release: true})
	if got != want.String() {
		t.Errorf("release binary diverges from release interpreter\n got: %q\nwant: %q", got, want.String())
	}
}
