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
			explain:     "early-exit search",
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
		// --- idioms: the long way round to a named reduction ---
		{name: "fold that counts becomes count",
			src:         p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + 1\n"),
			explain:     "counting by hand) → Count",
			extraInputs: []string{"", "7"}},
		{name: "fold that counts, literal first",
			src:         p("Maximum Technique: Fold\n    Seed: 0\n    Using: (n, x) -> 1 + n\n"),
			explain:     "counting by hand) → Count",
			extraInputs: []string{""}},
		{name: "fold summing a mapped value becomes sum by",
			src:         p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + x * x\n"),
			explain:     "a running sum of a mapped value) → Sum By",
			extraInputs: []string{"", "-3\n4"}},
		{name: "fold counting conditionally becomes count matching",
			src:         p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> if x % 2 = 0 then acc + 1 else acc\n"),
			explain:     "a conditional count) → Count Matching",
			extraInputs: []string{"", "1\n3", "2\n-4\n5"}},
		{name: "fold summing conditionally becomes a guarded sum by",
			src:         p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> if x > 2 then x * 3 + acc else acc\n"),
			explain:     "a conditional running sum) → Sum By",
			extraInputs: []string{"", "1\n2", "3\n-4\n5"}},
		{name: "fold with a nonzero seed is left alone",
			src:           p("Maximum Technique: Fold\n    Seed: 5\n    Using: (acc, x) -> acc + 1\n"),
			explainAbsent: "→ Count",
			extraInputs:   []string{"", "1\n2"}},
		{name: "fold whose term can fail is left alone",
			src:           p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + 10 / x\n"),
			explainAbsent: "→ Sum By",
			extraInputs:   []string{"5\n0\n2", "5\n1"}},
		{name: "fold whose term reads the accumulator is left alone",
			src:           p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + acc * x\n"),
			explainAbsent: "→ Sum By",
			extraInputs:   []string{"5\n3\n2"}},
		{name: "fold whose else arm is not the accumulator is left alone",
			src:           p("Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> if x > 2 then acc + 1 else 0\n"),
			explainAbsent: "→ Count Matching",
			extraInputs:   []string{"5\n1\n7\n8"}},
		{name: "fold idiom inside a Part body is rewritten in place",
			src: listHeader + "Part \"1\":\n    Maximum Technique: Fold\n        Seed: 0\n" +
				"        Using: (acc, x) -> acc + x * 2\n    Reveal: stdout\n",
			explain:     "→ Sum By",
			extraInputs: []string{"", "4\n5"}},
		{name: "map then a hand-written counting fold collapses to count",
			src: p("Cursed Technique: Map Each\n    Using: (x) -> x * 3\n" +
				"Maximum Technique: Fold\n    Seed: 0\n    Using: (acc, x) -> acc + 1\n"),
			explain:     "mapping preserves length",
			extraInputs: []string{""}},
		// --- a reordering that never reaches the result ---
		{name: "sort dies across a map into a sum",
			src: p("Domain Expansion: Quicksort\nCursed Technique: Map Each\n    Using: (x) -> x * x\n" +
				"Maximum Technique: Sum\n"),
			explain:     "its ordering never reaches the result",
			extraInputs: []string{"", "3\n-1\n2"}},
		{name: "reverse dies across a filter and a map into a max",
			src: p("Reverse Cursed Technique: Reverse\nCursed Technique: Filter\n    Using: (x) -> x > 0\n" +
				"Cursed Technique: Map Each\n    Using: (x) -> 100 - x\nMaximum Technique: Max\n"),
			explain:     "its ordering never reaches the result",
			extraInputs: []string{"", "-1\n-2", "3\n-1\n2"}}, // empty after the filter: both must error
		{name: "sort dies before count matching",
			src:         p("Domain Expansion: Quicksort, Descending\nMaximum Technique: Count Matching\n    Using: (x) -> x > 2\n"),
			explain:     "its ordering never reaches the result",
			extraInputs: []string{"", "3\n1\n5"}},
		{name: "sort dies before any",
			src:         p("Domain Expansion: Quicksort\nMaximum Technique: Any\n    Using: (x) -> x > 2\n"),
			explain:     "its ordering never reaches the result",
			extraInputs: []string{"", "3\n1\n5", "1\n0"}},
		{name: "sort survives a stage that can fail",
			// Which element fails first is exactly what the sort decides.
			src: p("Domain Expansion: Quicksort\nCursed Technique: Map Each\n    Using: (x) -> 10 / x\n" +
				"Maximum Technique: Sum\n"),
			explainAbsent: "its ordering never reaches the result",
			extraInputs:   []string{"5\n0\n2", "5\n1"}},
		{name: "sort survives before an order-sensitive stage",
			src: p("Domain Expansion: Quicksort\nCursed Technique: Map Each\n    Using: (x) -> x * 2\n" +
				"Cursed Technique: Take Item 0\n"),
			explainAbsent: "its ordering never reaches the result",
			extraInputs:   []string{"5\n0\n2", ""}},
		{name: "filter moves ahead of a sort",
			src:         p("Domain Expansion: Quicksort, Descending\nCursed Technique: Filter\n    Using: (x) -> x % 2 = 0\n"),
			explain:     "filter first, sort only the survivors",
			extraInputs: []string{"", "3\n4\n2\n8\n1", "1\n3"}},
		{name: "a filter that can fail stays behind the sort",
			src:           p("Domain Expansion: Quicksort\nCursed Technique: Filter\n    Using: (x) -> 10 / x > 1\n"),
			explainAbsent: "filter first",
			extraInputs:   []string{"5\n0\n2", "5\n1"}},
		{name: "sort then filter then first is a selection of the survivors",
			src: p("Domain Expansion: Quicksort\nCursed Technique: Filter\n    Using: (x) -> x > 2\n" +
				"Cursed Technique: Take Item 0\n"),
			explain:     "kth order statistic",
			extraInputs: []string{"", "1\n2", "9\n4\n3\n1"}},
		// --- a list built to be measured ---
		{name: "unique then count is a distinct count",
			src:         p("Cursed Technique: Unique\nMaximum Technique: Count\n"),
			explain:     "→ Count Distinct",
			extraInputs: []string{"", "1\n1\n1", "3\n1\n3\n2"}},
		{name: "sort unique count cascades to a distinct count",
			src:         p("Domain Expansion: Quicksort\nCursed Technique: Unique\nMaximum Technique: Count\n"),
			explain:     "→ Count Distinct",
			extraInputs: []string{"", "3\n1\n3\n2"}},
		{name: "a count compared with zero becomes any",
			src: p("Maximum Technique: Count Matching\n    Using: (x) -> x > 4\n" +
				"Cursed Technique: Apply\n    Using: (c) -> c > 0\n"),
			explain:     "(count > 0) → Any",
			extraInputs: []string{"", "1\n2", "1\n9\n2"}},
		{name: "filter count at least one becomes any",
			src: p("Cursed Technique: Filter\n    Using: (x) -> x > 4\nMaximum Technique: Count\n" +
				"Cursed Technique: Apply\n    Using: (c) -> 1 <= c\n"),
			explain:     "(count > 0) → Any",
			extraInputs: []string{"", "1\n2", "1\n9\n2"}},
		{name: "a count equal to zero becomes all-not",
			src: p("Maximum Technique: Count Matching\n    Using: (x) -> x > 4\n" +
				"Cursed Technique: Apply\n    Using: (c) -> c = 0\n"),
			explain:     "(count = 0) → All",
			extraInputs: []string{"", "1\n2", "1\n9\n2"}},
		{name: "a filtered list tested for emptiness becomes any",
			src: p("Cursed Technique: Filter\n    Using: (x) -> x > 4\n" +
				"Cursed Technique: Apply\n    Using: (xs) -> length(xs) > 0\n"),
			explain:     "Filter + Apply (count > 0) → Any",
			extraInputs: []string{"", "1\n2", "1\n9\n2"}},
		{name: "a filtered list tested for no survivors becomes all-not",
			src: p("Cursed Technique: Filter\n    Using: (x) -> x > 4\n" +
				"Cursed Technique: Apply\n    Using: (xs) -> 0 = length(xs)\n"),
			explain:     "Filter + Apply (count = 0) → All",
			extraInputs: []string{"", "1\n2", "1\n9\n2"}},
		{name: "a count compared with another number keeps counting",
			src: p("Maximum Technique: Count Matching\n    Using: (x) -> x > 4\n" +
				"Cursed Technique: Apply\n    Using: (c) -> c > 1\n"),
			explainAbsent: "→ Any",
			extraInputs:   []string{"", "9\n8", "1\n9\n2"}},
		{name: "a count whose predicate can fail keeps counting",
			// Stopping at the first match would skip the 0 that the count fails on.
			src: p("Maximum Technique: Count Matching\n    Using: (x) -> 10 / x > 1\n" +
				"Cursed Technique: Apply\n    Using: (c) -> c > 0\n"),
			explainAbsent: "→ Any",
			extraInputs:   []string{"5\n0\n2", "5\n1"}},
		// --- a whole list mapped to read one element ---
		{name: "take item moves ahead of a map",
			src:         p("Cursed Technique: Map Each\n    Using: (x) -> x * x + 1\nCursed Technique: Take Item 1\n"),
			explain:     "the lambda runs once",
			extraInputs: []string{"", "4", "4\n-5\n6"}}, // out of range: both must error alike
		{name: "take item stays behind a map that can fail",
			src:           p("Cursed Technique: Map Each\n    Using: (x) -> 10 / x\nCursed Technique: Take Item 0\n"),
			explainAbsent: "the lambda runs once",
			extraInputs:   []string{"5\n0\n2", "5\n1"}},
		{name: "membership in a sorted copy is membership",
			src:         p("Cursed Technique: Apply\n    Using: (xs) -> contains(sort(xs), 3) and contains(unique(xs), 1)\n"),
			explain:     "cheaper equivalent builtin",
			extraInputs: []string{"", "3\n1", "1\n2"}},
		// --- typed expression rules: an element type makes order unobservable ---
		{name: "sum of a sorted list in a lambda",
			src:         p("Cursed Technique: Apply\n    Using: (xs) -> sum(sort(xs)) + max(reverse(xs)) - min(sort(xs))\n"),
			explain:     "cheaper equivalent builtin",
			extraInputs: []string{"7", "3\n-1\n4", ""}}, // empty: max/min must fail in the same words
		{name: "first of a sorted list is a guarded minimum",
			src:         p("Cursed Technique: Apply\n    Using: (xs) -> first(sort(xs)) * 100 + last(sort(xs))\n"),
			explain:     "cheaper equivalent builtin",
			extraInputs: []string{"5", "3\n-1\n4", ""}}, // empty: first must fail in its own words
		{name: "first of a sorted list over floats is left alone",
			src: listHeader + "Channeled Energy: Convert List to Floats\n" +
				"Cursed Technique: Apply\n    Using: (xs) -> first(sort(xs))\nReveal: stdout\n",
			explainAbsent: "cheaper equivalent builtin",
			extraInputs:   []string{"3\n1"}},
		{name: "item zero of a sorted list and the ends of a reversed one",
			src:         p("Cursed Technique: Apply\n    Using: (xs) -> item(sort(xs), 0) * 10000 + first(reverse(xs)) * 100 + last(reverse(xs))\n"),
			explain:     "cheaper equivalent builtin",
			extraInputs: []string{"5", "3\n9\n4", ""}},
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
		{name: "length-changing fusion fires inside a Part body",
			src: listHeader + "Part \"1\":\n    Domain Expansion: Quicksort, Descending\n" +
				"    Maximum Technique: Select Top 2, Sum\n    Reveal: stdout\n",
			// A Part reads its body from Meta at run time, so a fusion that
			// shortens the list is what both backends run.
			explain:     "Cursed Quickselect",
			extraInputs: []string{"1", "9\n4\n6\n1\n7"}},
		{name: "length-changing passes fire in both Parts and in a loop body",
			src: listHeader + "Part \"1\":\n    Cursed Technique: Map Each\n        Using: (x) -> x * 2\n" +
				"    Maximum Technique: Fold\n        Seed: 0\n        Using: (acc, x) -> acc + 1\n    Reveal: stdout\n" +
				"Part \"2\":\n    Domain Expansion: Quicksort\n    Cursed Technique: Filter\n        Using: (x) -> x > 2\n    Reveal: stdout\n" +
				"Simple Domain: Repeat 2\n    Domain Expansion: Quicksort\n    Reverse Cursed Technique: Reverse\n",
			explain:     "filter first",
			extraInputs: []string{"", "5\n1\n9\n3"}},

		// --- linear accumulators ---
		// The naive pipeline copies on every update, so it is the oracle for
		// the whole pass: same answers, or the analysis proved something false.
		{name: "frequency map fold updates in place",
			src: listHeader + "Maximum Technique: Fold\n" +
				"    Seed: (xs) -> emptymap(0, 0)\n" +
				"    Using: (acc, x) -> insert(acc, x % 5, getor(acc, x % 5, 0) + 1)\n" +
				"Channeled Energy: Convert To Entries\n" +
				"Cursed Technique: Map Each\n    Using: (e) -> item(e, 0) * 1000 + item(e, 1)\n" +
				"Reveal: stdout\n",
			explain: "write in place"},
		{name: "conditional record updates in place",
			src: listHeader + "Maximum Technique: Fold\n" +
				"    Seed: (xs) -> emptyset(0)\n" +
				"    Using: (acc, x) -> if x % 2 = 0 then insert(acc, x) else acc\n" +
				"Cursed Technique: Apply\n    Using: (s) -> tolist(s)\n" +
				"Reveal: stdout\n",
			explain: "write in place"},
		{name: "a fold that reads the accumulator afterwards still copies",
			src: listHeader + "Maximum Technique: Fold\n" +
				"    Seed: (xs) -> emptymap(0, 0)\n" +
				"    Using: (acc, x) -> tomap(list(tuple(size(insert(acc, x, 1)), 0), tuple(size(acc), 1)))\n" +
				"Cursed Technique: Apply\n    Using: (m) -> size(m) * 100 + getor(m, 0, 0)\n" +
				"Reveal: stdout\n",
			explainAbsent: "write in place"},
		{name: "sparse fold updates in place",
			src: listHeader + "Maximum Technique: Fold\n" +
				"    Seed: (xs) -> sparse(0)\n" +
				"    Using: (acc, x) -> put(acc, x % 4, x % 3, x)\n" +
				"Cursed Technique: Apply\n    Using: (g) -> cells(g) * 100 + at(g, 0, 0)\n" +
				"Reveal: stdout\n",
			explain: "write in place"},
		{name: "chained inserts update in place",
			src: listHeader + "Maximum Technique: Fold\n" +
				"    Seed: (xs) -> emptyset(0)\n" +
				"    Using: (acc, x) -> insert(insert(acc, x), 0 - x)\n" +
				"Cursed Technique: Apply\n    Using: (s) -> size(s)\n" +
				"Reveal: stdout\n",
			explain: "write in place"},
		{name: "group sums become a running sum per key",
			src: p("Maximum Technique: Group By\n    Using: (n) -> n % 4\n" +
				"Cursed Technique: Map Values\n    Using: (b) -> sum(b)\n"),
			explain: "running sum per key"},
		{name: "group lengths become a count per key",
			src: p("Maximum Technique: Group By\n    Using: (n) -> n % 4\n" +
				"Cursed Technique: Map Values\n    Using: (b) -> length(b)\n"),
			explain: "running count per key"},
		{name: "a failing group key still fails",
			src: p("Maximum Technique: Group By\n    Using: (n) -> 12 / n\n" +
				"Cursed Technique: Map Values\n    Using: (b) -> sum(b)\n"),
			explain: "running sum per key"},
		{name: "any other bucket reduction keeps its groups",
			src: p("Maximum Technique: Group By\n    Using: (n) -> n % 4\n" +
				"Cursed Technique: Map Values\n    Using: (b) -> max(b)\n"),
			explainAbsent: "per key"},
		// --- Text built by a fold ---
		{name: "a text fold appends to one buffer",
			src:     p("Maximum Technique: Fold\n    Seed: \"\"\n    Using: (acc, x) -> acc + totext(x) + \",\"\n"),
			explain: "one buffer"},
		{name: "a text fold keeps its seed",
			src:     p("Maximum Technique: Fold\n    Seed: \"[\"\n    Using: (acc, x) -> acc + (totext(x * 2) + \";\")\n"),
			explain: "one buffer"},
		{name: "a part that reads the accumulator stays a fold",
			src:           p("Maximum Technique: Fold\n    Seed: \"\"\n    Using: (acc, x) -> acc + totext(length(acc))\n"),
			explainAbsent: "one buffer"},
		{name: "a failing part fails on the same element",
			src:         p("Maximum Technique: Fold\n    Seed: \"\"\n    Using: (acc, x) -> acc + totext(12 / x)\n"),
			explain:     "one buffer",
			extraInputs: []string{"3\n0\n4"}},
		// --- cheaper equivalent builtins ---
		{name: "a map's key list searched becomes a key probe",
			src: p("Cursed Technique: Apply\n" +
				"    Using: (xs) -> tuple(contains(keys(tomap(zip(xs, xs))), 3), contains(tolist(toset(xs)), 4))\n"),
			explain: "cheaper equivalent builtin"},
		{name: "a list built only to be measured is not built",
			src: p("Cursed Technique: Apply\n" +
				"    Using: (xs) -> length(keys(tomap(zip(xs, xs)))) + length(sort(xs)) + length(reverse(xs)) + length(tolist(toset(xs)))\n"),
			explain: "cheaper equivalent builtin"},
		{name: "idempotent transforms applied twice run once",
			src: p("Cursed Technique: Apply\n" +
				"    Using: (xs) -> concat(sort(sort(xs)), concat(unique(unique(xs)), reverse(reverse(xs))))\n"),
			explain: "cheaper equivalent builtin"},
		// --- loop-invariant code motion ---
		{name: "an invariant list membership becomes a set",
			src: p("Cursed Technique: Filter\n" +
				"    Consider wanted Of (xs) -> take(xs, 3)\n" +
				"    Using: (x) -> contains(wanted, x)\n"),
			explain: "a set once"},
		{name: "invariant work moves out of the lambda",
			src: p("Cursed Technique: Map Each\n" +
				"    Consider all Of Itself\n" +
				"    Using: (x) -> x * 100 + sum(sort(all))\n"),
			explain: "hoisted `sum(sort(all))`"},
		{name: "invariant work that can fail stays per element",
			src: p("Cursed Technique: Map Each\n" +
				"    Consider all Of Itself\n" +
				"    Using: (x) -> x + max(all)\n"),
			explainAbsent: "hoisted"},
		{name: "a name some lambda writes is not invariant",
			src: p("Cursed Technique: Map Each\n" +
				"    Consider seen As 0\n" +
				"    Using: (x) -> seen := seen + x\n" +
				"Cursed Technique: Filter\n" +
				"    Consider limit Of Itself\n" +
				"    Using: (x) -> x > sum(unique(limit))\n"),
			explain: "hoisted `sum(unique(limit))`"},
		{name: "work that reads the element stays",
			src: p("Cursed Technique: Filter\n" +
				"    Consider all Of Itself\n" +
				"    Using: (x) -> contains(sort(all), x + 1)\n"),
			// The sort is dropped first (membership does not need it), so the
			// set is built from the list itself.
			explain: "made `all` a set"},
		{name: "Scan keeps every accumulator, so it still copies",
			src: listHeader + "Cursed Technique: Scan\n" +
				"    Seed: (xs) -> emptyset(0)\n" +
				"    Using: (acc, x) -> insert(acc, x)\n" +
				"Cursed Technique: Map Each\n    Using: (s) -> size(s)\n" +
				"Reveal: stdout\n",
			explainAbsent: "write in place"},
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
			for range 60 {
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
