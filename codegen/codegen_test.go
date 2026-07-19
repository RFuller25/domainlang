package codegen_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
func frontEnd(src string, optimize bool) (*ir.Pipeline, error) {
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
			t.Run(a.name+"/"+mode.name, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, string(src), mode.optimize)
				want := runInterpreter(t, pipe, input)
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
	}
	for _, p := range progs {
		for _, optimize := range []bool{true, false} {
			mode := "naive"
			if optimize {
				mode = "optimized"
			}
			t.Run(p.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				pipe := compilePipeline(t, p.src, optimize)
				want := runInterpreter(t, pipe, []byte(p.input))
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
