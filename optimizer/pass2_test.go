package optimizer

import (
	"math/rand"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// runDiff runs cases against the naive oracle like TestLongWayMatchesNaiveOracle.
func runDiff(t *testing.T, seed int64, cases []diffCase) {
	rng := rand.New(rand.NewSource(seed))
	for _, c := range cases {
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
			inputs = append(inputs, "", "0", "3\n3\n3", "1\n2\n3\n4\n5\n6\n7")
			for range 80 {
				inputs = append(inputs, intsInput(randInts(rng, 12, 9)))
			}
			for _, input := range inputs {
				wantOut, wantErr := interpret(naive, input)
				gotOut, gotErr := interpret(opt, input)
				if (gotErr != nil) != (wantErr != nil) {
					t.Fatalf("input %q: error divergence\noptimized err: %v\nnaive err: %v", input, gotErr, wantErr)
				}
				if wantErr != nil && gotErr.Error() != wantErr.Error() {
					t.Fatalf("input %q: the failure changed its words\noptimized: %v\nnaive:     %v", input, gotErr, wantErr)
				}
				if wantErr == nil && gotOut != wantOut {
					t.Fatalf("input %q: output divergence\noptimized: %q\nnaive:     %q", input, gotOut, wantOut)
				}
			}
		})
	}
}

func considerCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	return []diffCase{
		{name: "map behind a binding fuses with the next map",
			src: p("Cursed Technique: Map Each\n    Consider n Of (xs) -> length(xs)\n    Using: (x) -> x + n\n" +
				"Cursed Technique: Map Each\n    Using: (y) -> y * 2\n"),
			explain: "fused Map Each"},
		{name: "filter behind a binding fuses with the next filter",
			src: p("Cursed Technique: Filter\n    Consider top Of (xs) -> sum(xs)\n    Using: (x) -> x * 3 < top\n" +
				"Cursed Technique: Filter\n    Using: (y) -> y > 2\n"),
			explain: "fused Filter"},
		{name: "binding that can fail keeps failing first",
			src: p("Cursed Technique: Map Each\n    Consider f Of (xs) -> first(xs)\n    Using: (x) -> x + f\n" +
				"Cursed Technique: Map Each\n    Using: (y) -> 10 / y\n"),
			explain:     "fused Map Each",
			extraInputs: []string{"0", "5\n0"}},
		{name: "control: plain maps, second can fail",
			src: p("Cursed Technique: Map Each\n    Using: (x) -> x + 1\n" +
				"Cursed Technique: Map Each\n    Using: (y) -> 10 / y\n"),
			explain:     "fused Map Each",
			extraInputs: []string{"-1"}},
		{name: "stage after a binding is not pulled into it",
			src: p("Cursed Technique: Map Each\n    Using: (x) -> x + 1\n" +
				"Cursed Technique: Map Each\n    Consider n Of (xs) -> length(xs)\n    Using: (y) -> y * n\n"),
			explainAbsent: "fused Map Each"},
		{name: "filter then count behind a binding",
			src:     p("Cursed Technique: Filter\n    Consider n Of (xs) -> length(xs)\n    Using: (x) -> x < n\nMaximum Technique: Count\n"),
			explain: "Count Matching"},
	}
}

func TestPairsSeeThroughConsider(t *testing.T) { runDiff(t, 21, considerCases()) }

func mergeCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	const chunk = "Cursed Technique: Chunk 4\n"
	const aSort = "Cursed Technique: Map Each\n    Domain Expansion: Quicksort\n    Reverse Cursed Technique: Reverse\n"
	const flags = "Cursed Technique: Map Each\n    Domain Expansion: All Pairs\n        Mode: Map\n" +
		"        Using: (a, b) -> if a = b then 0 else 1\n" +
		"Maximum Technique: Sum By\n    Maximum Technique: Any\n        Using: (x) -> x = 0\n" +
		"    Cursed Technique: Apply\n        Using: (f) -> if f then 0 else 1\n"
	return []diffCase{
		{name: "pairs mapped to flags in one body, flags read in the next",
			src: p(chunk + flags), explain: "joined bodies"},
		{name: "merged body exposes the duplicate check",
			src: p(chunk + flags), explain: "not every pair"},
		{name: "body then a fallible expression is left alone: map with a lambda that can fail",
			src:           p(chunk + aSort + "Cursed Technique: Map Each\n    Using: (ys) -> 10 / (first(ys) - 3)\n"),
			explainAbsent: "joined bodies",
			extraInputs:   []string{"3\n1\n2\n5", "1\n2\n3\n4\n5\n6\n7\n8\n3\n9\n9\n9"}},
		{name: "body then a fallible expression is left alone: sum by with a lambda that can fail",
			src:           p(chunk + aSort + "Maximum Technique: Sum By\n    Using: (ys) -> 10 / (first(ys) - 3)\n"),
			explainAbsent: "joined bodies",
			extraInputs:   []string{"3\n1\n2\n5", "1\n2\n3\n4\n5\n6\n7\n8\n3\n9\n9\n9"}},
		{name: "body then count matching",
			src:     p(chunk + aSort + "Maximum Technique: Count Matching\n    Using: (ys) -> sum(ys) > 3\n"),
			explain: "joined bodies"},
		{name: "body then a fallible expression is left alone: any stops early over a failing lambda",
			src:           p(chunk + aSort + "Maximum Technique: Any\n    Using: (ys) -> 10 / (first(ys) - 6) > 100 or first(ys) > 7\n"),
			explainAbsent: "joined bodies",
			extraInputs:   []string{"9\n9\n9\n9\n1\n1\n6\n6", "1\n2\n3\n4\n5\n6\n7\n8"}},
		{name: "body then any stops early",
			src:     p(chunk + aSort + "Maximum Technique: Any\n    Using: (ys) -> sum(ys) > 7\n"),
			explain: "joined bodies"},
		{name: "body then all",
			src:     p(chunk + aSort + "Maximum Technique: All\n    Using: (ys) -> sum(ys) > 1\n"),
			explain: "joined bodies"},
		{name: "expression map then a body",
			src: p(chunk + "Cursed Technique: Map Each\n    Using: (c) -> reverse(c)\n" +
				"Maximum Technique: Sum By\n    Maximum Technique: Sum\n"),
			explain: "joined bodies"},
		{name: "three stages join pairwise",
			src: p(chunk + aSort + "Cursed Technique: Map Each\n    Reverse Cursed Technique: Reverse\n" +
				"Maximum Technique: Sum By\n    Using: (ys) -> sum(ys)\n"),
			explain: "joined bodies"},
		{name: "a body that can fail is not merged: take item",
			src: p(chunk + "Cursed Technique: Map Each\n    Cursed Technique: Take Item 5\n" +
				"Cursed Technique: Map Each\n    Using: (x) -> 10 / (x - 3)\n"),
			explainAbsent: "joined bodies",
			extraInputs:   []string{"3\n1\n2\n5\n0\n0\n0\n0\n0"}},
		{name: "a body that can fail is not merged: max",
			src: p(chunk + "Cursed Technique: Map Each\n    Maximum Technique: Max\n" +
				"Maximum Technique: Sum By\n    Using: (x) -> 10 / (x - 3)\n"),
			explainAbsent: "joined bodies",
			extraInputs:   []string{"3\n1\n2\n3"}},
		{name: "a lambda inside the first body that can fail is not merged",
			src: p(chunk + "Cursed Technique: Map Each\n    Cursed Technique: Map Each\n        Using: (x) -> 10 / (x - 2)\n" +
				"Maximum Technique: Sum By\n    Using: (ys) -> length(ys)\n"),
			explainAbsent: "joined bodies",
			extraInputs:   []string{"2\n1\n1\n1", "1\n2\n3\n4\n5\n2\n0\n9"}},
		{name: "two expressions are left to map fusion",
			src:           p(chunk + "Cursed Technique: Map Each\n    Using: (c) -> reverse(c)\nMaximum Technique: Sum By\n    Using: (ys) -> sum(ys)\n"),
			explainAbsent: "joined bodies"},
		{name: "a body that prints is not merged",
			src: p(chunk + "Cursed Technique: Map Each\n    Reverse Cursed Technique: Reverse\n    Reveal: stderr\n" +
				"Maximum Technique: Sum By\n    Using: (ys) -> sum(ys)\n"),
			explainAbsent: "joined bodies"},
	}
}

func TestMergeEachBodies(t *testing.T) { runDiff(t, 22, mergeCases()) }

func normalizeCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	ap := func(e string) string { return "Cursed Technique: Apply\n    Using: (x) -> " + e + "\n" }
	return []diffCase{
		{name: "apply sort then take item", src: p(ap("sort(x)") + "Cursed Technique: Take Item 1\n"),
			explain: "Apply (sort) → Sort", extraInputs: []string{"5"}},
		{name: "apply sort cascades into quickselect", src: p(ap("sort(x)") + "Cursed Technique: Take Item 0\n"),
			explain: "Cursed Quickselect"},
		{name: "apply reverse twice cancels", src: p(ap("reverse(x)") + ap("reverse(x)")),
			explain: "double inversion"},
		{name: "apply unique then count", src: p(ap("unique(x)") + "Maximum Technique: Count\n"),
			explain: "Count Distinct"},
		{name: "apply sum", src: p(ap("sum(x)")), explain: "Apply (sum) → Sum"},
		{name: "apply length", src: p(ap("length(x)")), explain: "Apply (length) → Count"},
		{name: "apply sort then sum drops the sort", src: p(ap("sort(x)") + ap("sum(x)")),
			explain: "order-insensitive"},
		{name: "apply max stays", src: p(ap("max(x)")), explainAbsent: "→ Max", extraInputs: []string{""}},
		{name: "apply first stays", src: p(ap("first(x)")), explainAbsent: "→ Take Item", extraInputs: []string{""}},
		{name: "apply take stays", src: p(ap("take(x, 2)")), explainAbsent: "Apply (take)"},
		{name: "sort of something else stays",
			src:           p("Cursed Technique: Map Each\n    Using: (v) -> totext(v)\n" + ap("sort(x)")),
			explainAbsent: "Apply (sort)"},
		{name: "apply of a bigger expression stays", src: p(ap("sort(x) = x")),
			explainAbsent: "Apply (sort)"},
	}
}

func TestNormalizeListBuiltins(t *testing.T) { runDiff(t, 23, normalizeCases()) }

func takeCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	take := func(n string) string { return "Cursed Technique: Apply\n    Using: (x) -> take(x, " + n + ")\n" }
	mp := func(e string) string { return "Cursed Technique: Map Each\n    Using: (x) -> " + e + "\n" }
	win := func(spec string) string { return "Cursed Technique: Window " + spec + "\n" }
	return []diffCase{
		{name: "map then take", src: p(mp("x * x + 1") + take("3")), explain: "Apply (take 3) + Map Each"},
		{name: "map then take more than there is", src: p(mp("x * 2") + take("100")), explain: "Apply (take 100) + Map Each"},
		{name: "map then take none", src: p(mp("x * 2") + take("0")), explain: "Apply (take 0) + Map Each"},
		{name: "map then take a negative count", src: p(mp("x * 2") + take("-4")), explain: "Apply (take -4) + Map Each"},
		{name: "a failing map is not reordered", src: p(mp("10 / x") + take("2")),
			explainAbsent: "+ Map Each", extraInputs: []string{"1\n2\n0", "0\n1\n2"}},
		{name: "a map reading a failing partial builtin is not reordered", src: p(mp("item(sort(chars(totext(x))), 1)") + take("2")),
			explainAbsent: "+ Map Each", extraInputs: []string{"5\n12\n7"}},
		{name: "window then take", src: p(win("3") + take("2")), explain: "Window + Apply (take 2) → Apply (take 4)"},
		{name: "window with a step then take", src: p(win("3 2") + take("3")), explain: "→ Apply (take 7)"},
		{name: "window then take more than there is", src: p(win("2") + take("50")), explain: "Window + Apply"},
		{name: "window then take none", src: p(win("2") + take("0")), explain: "→ Apply (take 0)"},
		{name: "window then take a negative count", src: p(win("2") + take("-1")), explain: "→ Apply (take 0)"},
		{name: "window then take one over a short list", src: p(win("5") + take("1")), explain: "→ Apply (take 5)"},
		{name: "a measured window size is left alone",
			src:           p("Cursed Technique: Window\n    Size: (xs) -> length(xs) / 3\n" + take("2")),
			explainAbsent: "+ Window", extraInputs: []string{"1\n2\n3\n4\n5\n6", "1\n2"}},
		{name: "map then take then map", src: p(mp("x + 1") + take("4") + mp("x * 3")), explain: "Apply (take 4) + Map Each"},
		{name: "a take of something else stays", src: p(mp("x + 1") + "Cursed Technique: Apply\n    Using: (x) -> drop(x, 1)\n"),
			explainAbsent: "Apply (take"},
	}
}

func TestPushTakeBack(t *testing.T) { runDiff(t, 24, takeCases()) }

func TestNodeMentionsGuardsCapture(t *testing.T) {
	lam := &ast.Lambda{Params: []string{"y"}, Body: &ast.BinaryExpr{Op: token.PLUS,
		Left: &ast.Ident{Name: "y"}, Right: &ast.Ident{Name: "n"}}}
	n := &ir.Node{Prim: "Map Each", Meta: map[string]any{"lambda": lam}}
	if !nodeMentions(n, []string{"n"}) {
		t.Fatal("a lambda reading the binding name must count as a mention")
	}
	if nodeMentions(n, []string{"m"}) {
		t.Fatal("an unrelated name is not a mention")
	}
	odd := &ir.Node{Prim: "X", Meta: map[string]any{"k": struct{}{}}}
	if !nodeMentions(odd, []string{"m"}) {
		t.Fatal("an unreadable Meta value must count as a mention")
	}
}
