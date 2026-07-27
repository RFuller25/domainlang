package optimizer

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// End-to-end differential tests: every pass is exercised through a real
// Domain program, run interpreted with and without optimization over many
// inputs. The naive pipeline is the correctness oracle: outputs must be
// byte-identical when both succeed, and a rewrite may never turn a runtime
// error into a success (or the reverse).

// resolveProgram runs the front end and optionally the optimizer.
func resolveProgram(t *testing.T, src string, optimize bool) (*ir.Pipeline, []Rewrite) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v\nprogram:\n%s", err, src)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v\nprogram:\n%s", err, src)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v\nprogram:\n%s", err, src)
	}
	rewrites := Optimize(pipe, optimize)
	return pipe, rewrites
}

func interpret(pipe *ir.Pipeline, input string) (string, error) {
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader(input), Stdout: &out}
	_, err := interp.Run(pipe, ctx)
	return out.String(), err
}

// listHeader is the shared program prefix: stdin → lines → List<Int>.
const listHeader = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
`

// intsInput renders a list as the stdin the header expects.
func intsInput(xs []int64) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%d", x)
	}
	return strings.Join(parts, "\n")
}

type diffCase struct {
	name          string
	src           string // full program source
	explain       string // substring required in some optimized rewrite message
	explainAbsent string // substring that must NOT appear (guarded rewrites)
	extraInputs   []string
}

func diffCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	return []diffCase{
		// --- reordering dead code ---
		{name: "dead sort",
			src:     p("Domain Expansion: Quicksort, Descending\nDomain Expansion: Quicksort\n"),
			explain: "single Quicksort"},
		{name: "reverse reverse cancels",
			src:     p("Reverse Cursed Technique: Reverse\nReverse Cursed Technique: Reverse\n"),
			explain: "double inversion"},
		{name: "sort then reverse flips order",
			src:     p("Domain Expansion: Quicksort\nReverse Cursed Technique: Reverse\n"),
			explain: "Quicksort (Descending)"},
		{name: "sort descending then reverse flips order",
			src:     p("Domain Expansion: Quicksort, Descending\nReverse Cursed Technique: Reverse\n"),
			explain: "Quicksort (Ascending)"},
		{name: "sort before sum elided",
			src:     p("Domain Expansion: Quicksort\nMaximum Technique: Sum\n"),
			explain: "order-insensitive"},
		{name: "reverse before max elided",
			src:     p("Reverse Cursed Technique: Reverse\nMaximum Technique: Max\n"),
			explain: "order-insensitive"},
		{name: "reverse before count elided",
			src:     p("Reverse Cursed Technique: Reverse\nMaximum Technique: Count\n"),
			explain: "order-insensitive"},
		{name: "unique unique",
			src:     p("Cursed Technique: Unique\nCursed Technique: Unique\n"),
			explain: "idempotent"},
		{name: "unique before min elided",
			src:     p("Cursed Technique: Unique\nMaximum Technique: Min\n"),
			explain: "extremum"},
		{name: "sort unique swapped to unique sort",
			src:     p("Domain Expansion: Quicksort\nCursed Technique: Unique\n"),
			explain: "dedupe first"},
		// --- map/filter dead code + fusion ---
		{name: "identity map from algebra",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> x * 1\n"),
			explain: "identity"},
		{name: "map before count elided",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> x * 2 + 1\nMaximum Technique: Count\n"),
			explain: "preserves length"},
		{name: "failable map before count NOT elided",
			src:           p("Cursed Technique: Map Each\n    Using: (x) -> 10 / x\nMaximum Technique: Count\n"),
			explainAbsent: "preserves length",
			extraInputs:   []string{"3\n0\n2"}},
		{name: "map fusion",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> x * 2\nCursed Technique: Map Each\n    Using: (y) -> y + 10\n"),
			explain: "fused Map Each"},
		{name: "failable map NOT fused",
			src:           p("Cursed Technique: Map Each\n    Using: (x) -> 10 / x\nCursed Technique: Map Each\n    Using: (y) -> y + 1\n"),
			explainAbsent: "fused Map Each",
			extraInputs:   []string{"5\n0\n2"}},
		{name: "filter fusion",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> x > 0\nCursed Technique: Filter\n    Using: (y) -> y < 5\n"),
			explain: "fused Filter"},
		{name: "map fusion with conditional body",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> x + 1\nCursed Technique: Map Each\n    Using: (y) -> if y > 0 then y else 0 - y\n"),
			explain: "fused Map Each"},
		{name: "filter fusion with conditional body",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> x > 0\nCursed Technique: Filter\n    Using: (y) -> if y > 10 then y < 20 else y > -5\n"),
			explain: "fused Filter"},
		{name: "filter count to count matching",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> x > 2\nMaximum Technique: Count\n"),
			explain: "Count Matching"},
		{name: "fold to sum",
			src:     p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x\n"),
			explain: "Fold (Seed: 0, running sum) → Sum"},
		{name: "fold to sum flipped operands",
			src:     p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> x + acc\n"),
			explain: "Fold (Seed: 0, running sum) → Sum"},
		{name: "fold with nonzero seed NOT rewritten",
			src:           p("Maximum Technique: Fold\n    Seed: 7\n    Using: (acc, x) -> acc + x\n"),
			explainAbsent: "→ Sum"},
		// --- constant predicates (via the expression passes) ---
		{name: "always-true filter dropped",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> 2 < 3\n"),
			explain: "always true"},
		{name: "always-false filter short-circuits",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> 1 = 2\n"),
			explain: "always false"},
		{name: "always-true count matching becomes count",
			src:     p("Maximum Technique: Count Matching\n    Using: (x) -> 3 >= 3\n"),
			explain: "Count Matching (always true) → Count"},
		{name: "always-false count matching returns zero",
			src:     p("Maximum Technique: Count Matching\n    Using: (x) -> 3 < 3\n"),
			explain: "Count Matching (always false)"},
		{name: "boolean short-circuit inside predicate",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> 1 = 1 and x > 2\n"),
			explain: "boolean short-circuit"},
		{name: "algebraic identity inside map",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> 2 * x + 0\n"),
			explain: "algebraic identity"},
		// --- algorithm substitutions ---
		{name: "quickselect item",
			src:         p("Domain Expansion: Quicksort, Descending\nCursed Technique: Take Item 1\n"),
			explain:     "kth order statistic",
			extraInputs: []string{"", "4"}}, // out-of-range: both must error
		{name: "quickselect item ascending",
			src:     p("Domain Expansion: Quicksort\nCursed Technique: Take Item 0\n"),
			explain: "kth order statistic"},
		{name: "triple sum count",
			src:     p("Domain Expansion: Combinations 3\n    Mode: Count\n    Using: (a, b, c) -> c + a + b = 10\n"),
			explain: "Triple Scan"},
		{name: "triple sum first",
			src:         p("Domain Expansion: Combinations 3\n    Mode: First\n    Using: (a, b, c) -> a + b + c = 10\n"),
			explain:     "Triple Scan",
			extraInputs: []string{"1\n2\n7", "1\n1\n1"}}, // hit and no-hit (both error)
		{name: "pair diff first",
			src:         p("Domain Expansion: All Pairs\n    Mode: First\n    Using: (a, b) -> a - b = 3\n"),
			explain:     "difference = 3",
			extraInputs: []string{"9\n4\n6\n1", "1\n1"}},
		{name: "pair diff count flipped",
			src:     p("Domain Expansion: All Pairs\n    Mode: Count\n    Using: (a, b) -> b - a = 2\n"),
			explain: "difference = 2"},
		{name: "pair diff literal on the left",
			src:     p("Domain Expansion: All Pairs\n    Mode: Count\n    Using: (a, b) -> 4 = a - b\n"),
			explain: "difference = 4"},
		{name: "pair product count",
			src:     p("Domain Expansion: All Pairs\n    Mode: Count\n    Using: (a, b) -> a * b = 12\n"),
			explain: "product = 12"},
		{name: "pair product count zero target",
			src:         p("Domain Expansion: All Pairs\n    Mode: Count\n    Using: (a, b) -> a * b = 0\n"),
			explain:     "product = 0",
			extraInputs: []string{"0\n3\n0\n4\n-2", "0\n0\n0"}},
		{name: "pair product first",
			src:         p("Domain Expansion: All Pairs\n    Mode: First\n    Using: (a, b) -> b * a = 6\n"),
			explain:     "product = 6",
			extraInputs: []string{"2\n4\n3", "0\n6\n1\n6", "5\n7"}}, // hit, zero-then-hit, no-hit (both error)
		{name: "pair product literal on the left",
			src:     p("Domain Expansion: All Pairs\n    Mode: Count\n    Using: (a, b) -> 8 = a * b\n"),
			explain: "product = 8"},
		{name: "pair product in filter mode NOT rewritten",
			src:           p("Domain Expansion: All Pairs\n    Mode: Filter\n    Using: (a, b) -> a * b = 12\n"),
			explainAbsent: "Divisor Scan"},
		{name: "windowed sum",
			src:         p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> sum(w)\n"),
			explain:     "Sliding-Window Sum",
			extraInputs: []string{"1\n2", "1\n2\n3\n4\n5"}},
		{name: "windowed max with step",
			src:         p("Cursed Technique: Window 2 2\nCursed Technique: Map Each\n    Using: (w) -> max(w)\n"),
			explain:     "Sliding-Window Max",
			extraInputs: []string{"5\n1\n4\n4\n7\n-2"}},
		{name: "windowed min",
			src:     p("Cursed Technique: Window 4\nCursed Technique: Map Each\n    Using: (w) -> min(w)\n"),
			explain: "Sliding-Window Min"},
		{name: "window with non-reduction lambda NOT rewritten",
			src:           p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> sum(w) + 1\n"),
			explainAbsent: "Sliding-Window"},
		{name: "window with length lambda NOT rewritten",
			src:           p("Cursed Technique: Window 3\nCursed Technique: Map Each\n    Using: (w) -> length(w)\n"),
			explainAbsent: "Sliding-Window"},
		{name: "bfs at target early exit",
			src: "Cursed Energy: stdin\nShikigami: Digit Grid\n" +
				"Domain Expansion: BFS from 0 0\n    Using: (c) -> c < 5\n" +
				"Cursed Technique: Apply\n    Using: (g) -> at(g, 2, 2)\nReveal: stdout\n",
			explain: "early-exit search",
			extraInputs: []string{
				"123\n145\n110", // reachable around the 5 wall
				"119\n911\n111", // 9s are walls (c < 5): unreachable → -1 in both
				"12\n34",        // target (2,2) out of bounds: both must error
			}},
		{name: "dijkstra at target early exit",
			src: "Cursed Energy: stdin\nShikigami: Digit Grid\n" +
				"Domain Expansion: Dijkstra from 0 0\n" +
				"Cursed Technique: Apply\n    Using: (g) -> at(g, 1, 2)\nReveal: stdout\n",
			explain: "early-exit search",
			extraInputs: []string{"123\n145\n110", "19\n21"}},
		{name: "bfs with arithmetic on at NOT rewritten",
			src: "Cursed Energy: stdin\nShikigami: Digit Grid\n" +
				"Domain Expansion: BFS from 0 0\n    Using: (c) -> c < 5\n" +
				"Cursed Technique: Apply\n    Using: (g) -> at(g, 1, 1) + 1\nReveal: stdout\n",
			explainAbsent: "early-exit",
			extraInputs:   []string{"123\n145\n110"}},
		{name: "linear map before max",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> 3 * x + 5\nMaximum Technique: Max\n"),
			explain: "monotone maps commute"},
		{name: "decreasing linear map before max",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> 3 - 2 * x\nMaximum Technique: Max\n"),
			explain: "input Min"},
		{name: "decreasing linear map before min",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> 0 - x\nMaximum Technique: Min\n"),
			explain: "input Max"},
		{name: "nonlinear map before max NOT rewritten",
			src:           p("Cursed Technique: Map Each\n    Using: (x) -> x * x\nMaximum Technique: Max\n"),
			explainAbsent: "monotone maps commute"},
		// --- cascades ---
		{name: "sort reverse topk cascades into quickselect",
			src:     p("Domain Expansion: Quicksort\nReverse Cursed Technique: Reverse\nMaximum Technique: Select Top 3, Sum\n"),
			explain: "Cursed Quickselect"},
		{name: "fused maps then count elided",
			src:     p("Cursed Technique: Map Each\n    Using: (x) -> x + 1\nCursed Technique: Map Each\n    Using: (y) -> y * 3\nMaximum Technique: Count\n"),
			explain: "preserves length"},
		// --- early-exit / single-pass rewrites ---
		{name: "map then sum becomes sum by",
			src:         p("Cursed Technique: Map Each\n    Using: (x) -> x * 3\nMaximum Technique: Sum\n"),
			explain:     "→ Sum By",
			extraInputs: []string{"", "5"}},
		{name: "map then product becomes product by",
			src:         p("Cursed Technique: Map Each\n    Using: (x) -> x + 1\nMaximum Technique: Product\n"),
			explain:     "→ Product By",
			extraInputs: []string{"", "0\n3", "4"}},
		{name: "failing map then sum still fails on the same element",
			src:         p("Cursed Technique: Map Each\n    Using: (x) -> 10 / x\nMaximum Technique: Sum\n"),
			explain:     "→ Sum By",
			extraInputs: []string{"5\n0\n2", "0", ""}},
		{name: "filter then take item 0 becomes a first match",
			src:     p("Cursed Technique: Filter\n    Using: (x) -> x > 2\nCursed Technique: Take Item 0\n"),
			explain: "Cursed First Match",
			// No match at all: the rewritten node must fail exactly as the
			// Take Item it replaced did.
			extraInputs: []string{"", "1\n2", "5\n1", "1\n5"}},
		{name: "filter then take item 1 NOT rewritten",
			src:           p("Cursed Technique: Filter\n    Using: (x) -> x > 2\nCursed Technique: Take Item 1\n"),
			explainAbsent: "Cursed First Match",
			extraInputs:   []string{"3\n4\n5", "3"}},
		{name: "failable filter then take item 0 NOT rewritten",
			src:           p("Cursed Technique: Filter\n    Using: (x) -> 10 / x > 1\nCursed Technique: Take Item 0\n"),
			explainAbsent: "Cursed First Match",
			// The 0 makes the full Filter scan fail; short-circuiting would
			// have returned the 5 instead, so the guard must hold.
			extraInputs: []string{"5\n0\n2", "5\n1"}},
		// --- constant predicates on the early-exit primitives ---
		{name: "always-true take while dropped",
			src:     p("Cursed Technique: Take While\n    Using: (x) -> 2 < 3\n"),
			explain: "Take While (constant predicate) → nothing at all"},
		{name: "always-false take while empties",
			src:     p("Cursed Technique: Take While\n    Using: (x) -> 3 < 2\n"),
			explain: "Take While (constant predicate) → the empty list"},
		{name: "always-false drop while dropped",
			src:     p("Cursed Technique: Drop While\n    Using: (x) -> 3 < 2\n"),
			explain: "Drop While (constant predicate) → nothing at all"},
		{name: "always-true drop while empties",
			src:     p("Cursed Technique: Drop While\n    Using: (x) -> 2 < 3\n"),
			explain: "Drop While (constant predicate) → the empty list"},
		{name: "always-false any is a constant",
			src:         p("Maximum Technique: Any\n    Using: (x) -> 3 < 3\n"),
			explain:     "Any (constant predicate) → a constant",
			extraInputs: []string{""}},
		{name: "always-true all is a constant",
			src:         p("Maximum Technique: All\n    Using: (x) -> 3 >= 3\n"),
			explain:     "All (constant predicate) → a constant",
			extraInputs: []string{""}},
		{name: "always-true any is an emptiness test",
			src:         p("Maximum Technique: Any\n    Using: (x) -> 3 >= 3\n"),
			explain:     "Any (constant predicate) → an emptiness test",
			extraInputs: []string{""}},
		{name: "always-false all is an emptiness test",
			src:         p("Maximum Technique: All\n    Using: (x) -> 3 < 3\n"),
			explain:     "All (constant predicate) → an emptiness test",
			extraInputs: []string{""}},
		// --- the new primitives run unrewritten through the oracle too ---
		{name: "sliding reduce matches window plus map",
			src:         p("Domain Expansion: Sliding Reduce 3\n    Mode: Max\n"),
			extraInputs: []string{"1\n2", "5\n1\n9\n3\n7"}},
		{name: "chunk keeps the short block",
			src:         p("Cursed Technique: Chunk 3\n"),
			extraInputs: []string{"", "1", "1\n2\n3\n4"}},
		{name: "partition halves",
			src:         p("Cursed Technique: Partition\n    Using: (x) -> x > 2\n"),
			extraInputs: []string{"", "1\n2", "3\n4"}},
		{name: "scan then pairs then reduce",
			src: p("Cursed Technique: Scan\n    Using: (a, b) -> a + b\n" +
				"Cursed Technique: Pairs\nCursed Technique: Map Each\n    Using: (p) -> pcol(p) - prow(p)\n"),
			extraInputs: []string{"", "1", "1\n2\n3"}},
		// --- Part bodies are sub-pipelines, so optimizer safety rule 4 applies:
		// in-place passes reach into them, length-changing ones do not.
		{name: "expression simplification inside a Part body",
			src: listHeader + "Part \"1\":\n    Cursed Technique: Filter\n" +
				"        Using: (x) -> x > 0 and 2 < 3\n    Reveal: stdout\n",
			explain:     "simplified the Using: lambda",
			extraInputs: []string{"", "1\n-2\n3"}},
		{name: "in-place simplification fires in both Part bodies",
			src: listHeader + "Part \"1\":\n    Cursed Technique: Map Each\n" +
				"        Using: (x) -> x + 0\n    Reveal: stdout\n" +
				"Part \"2\":\n    Cursed Technique: Filter\n" +
				"        Using: (y) -> y > 1 and 1 = 1\n    Reveal: stdout\n",
			explain:     "simplified the Using: lambda",
			extraInputs: []string{"", "3\n-1\n4"}},
		{name: "length-changing fusion does not fire inside a Part body",
			src: listHeader + "Part \"1\":\n    Domain Expansion: Quicksort, Descending\n" +
				"    Maximum Technique: Select Top 2, Sum\n    Reveal: stdout\n",
			// Nested node lists are captured by their parent's Eval closure, so
			// re-slicing one would diverge from what the interpreter runs. The
			// naive pair must therefore survive — and still be correct.
			explainAbsent: "Cursed Quickselect",
			extraInputs:   []string{"1", "9\n4\n6\n1\n7"}},
	}
}

func TestPassesMatchNaiveOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, c := range diffCases() {
		t.Run(c.name, func(t *testing.T) {
			naive, _ := resolveProgram(t, c.src, false)
			opt, rewrites := resolveProgram(t, c.src, true)

			if c.explain != "" && !containsMessage(rewrites, c.explain) {
				t.Fatalf("expected a rewrite containing %q, got %v", c.explain, messages(rewrites))
			}
			if c.explainAbsent != "" && containsMessage(rewrites, c.explainAbsent) {
				t.Fatalf("rewrite containing %q must not fire, got %v", c.explainAbsent, messages(rewrites))
			}

			inputs := append([]string{}, c.extraInputs...)
			inputs = append(inputs, "", "0", "3\n3\n3")
			for i := 0; i < 60; i++ {
				inputs = append(inputs, intsInput(randInts(rng, 12, 9)))
			}
			for _, input := range inputs {
				wantOut, wantErr := interpret(naive, input)
				gotOut, gotErr := interpret(opt, input)
				if (gotErr != nil) != (wantErr != nil) {
					t.Fatalf("input %q: error divergence\noptimized err: %v\nnaive err: %v", input, gotErr, wantErr)
				}
				if wantErr == nil && gotOut != wantOut {
					t.Fatalf("input %q: output divergence\noptimized: %q\nnaive:     %q", input, gotOut, wantOut)
				}
			}
		})
	}
}

func containsMessage(rs []Rewrite, sub string) bool {
	for _, r := range rs {
		if strings.Contains(r.Message, sub) {
			return true
		}
	}
	return false
}

func messages(rs []Rewrite) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Message
	}
	return out
}
