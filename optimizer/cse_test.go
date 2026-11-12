package optimizer

import (
	"math/rand"
	"testing"
)

// Differential tests for the expression-layer rules that share work inside a
// lambda body: conditional arms that differ in one argument, and repeated
// subexpressions. Same oracle as TestLongWayMatchesNaiveOracle.

func runDiffCases(t *testing.T, cases []diffCase, seed int64) {
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

func sharedArmCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	mapEach := func(lam string) string { return "Cursed Technique: Map Each\n    Using: " + lam + "\n" }
	return []diffCase{
		{name: "tuple arms differing in one field",
			src:     p(mapEach("(x) -> if x > 3 then tuple(x, 1, 2) else tuple(x, 0, 2)")),
			explain: "shared conditional arms"},
		{name: "record arms differing in one field",
			src: p(mapEach("(x) -> if x % 2 = 0 then record(\"a\", x, \"n\", 1) else record(\"a\", x, \"n\", 0)") +
				mapEach("(r) -> r.n + r.a")),
			explain: "shared conditional arms"},
		{name: "arms differing in the first argument",
			src: p(mapEach("(x) -> if x > 3 then tuple(x + 1, 7) else tuple(x, 7)") +
				mapEach("(t) -> item(t, 0) + item(t, 1)")),
			explain: "shared conditional arms"},
		{name: "a condition that can fail still fails first",
			src:         p(mapEach("(x) -> if 10 / x > 1 then tuple(x, 1) else tuple(x, 2)")),
			explain:     "shared conditional arms",
			extraInputs: []string{"0", "3\n0\n5", "5\n0"}},
		{name: "a partial argument before the difference keeps its place",
			src:           p(mapEach("(x) -> if 10 / x > 1 then tuple(100 / (x - 2), 1) else tuple(100 / (x - 2), 2)")),
			explainAbsent: "shared conditional arms",
			extraInputs:   []string{"0", "2", "3\n0\n5", "2\n0"}},
		{name: "a partial argument after the difference is fine",
			src:         p(mapEach("(x) -> if 10 / x > 1 then tuple(1, 100 / (x - 2)) else tuple(2, 100 / (x - 2))")),
			explain:     "shared conditional arms",
			extraInputs: []string{"0", "2", "3\n0\n5", "2\n0"}},
		{name: "a partial differing argument is evaluated only on its own arm",
			src:         p(mapEach("(x) -> if x > 3 then tuple(x, 10 / x) else tuple(x, 0)")),
			explain:     "shared conditional arms",
			extraInputs: []string{"0", "0\n5"}},
		{name: "two arguments differ",
			src:           p(mapEach("(x) -> if x > 3 then tuple(x, 1, 2) else tuple(x, 0, 3)")),
			explainAbsent: "shared conditional arms"},
		{name: "different builders",
			src:           p(mapEach("(x) -> if x > 3 then list(x, 1) else concat(list(x), list(0))")),
			explainAbsent: "shared conditional arms"},
		{name: "a builtin that is not a builder",
			src:           p(mapEach("(x) -> if x > 3 then max(list(x, 1)) else max(list(x, 0))")),
			explainAbsent: "shared conditional arms"},
	}
}

func TestSharedArmsMatchNaiveOracle(t *testing.T) {
	runDiffCases(t, sharedArmCases(), 21)
}

func commonSubexpressionCases() []diffCase {
	p := func(body string) string { return listHeader + body + "Reveal: stdout\n" }
	apply := func(lam string) string { return "Cursed Technique: Apply\n    Using: " + lam + "\n" }
	const short = "1"
	return []diffCase{
		{name: "a partial expression bound twice",
			src:         p(apply("(xs) -> consider i as 2 in consider a as item(xs, i) in consider b as item(xs, i) in a + b")),
			explain:     "common subexpression",
			extraInputs: []string{short, "1\n2", "4\n5\n6"}},
		{name: "a partial expression repeated inside a binding's body",
			src:         p(apply("(xs) -> consider i as length(xs) / 2 in consider a as item(xs, i) in a * 10 + item(xs, i)")),
			explain:     "common subexpression",
			extraInputs: []string{short, "1\n2", "4\n5\n6"}},
		{name: "a rebound name is not the same expression",
			src: p(apply("(xs) -> consider a as indexof(xs, 3) in consider xs as take(xs, 2) in " +
				"consider b as indexof(xs, 3) in a * 10 + b")),
			explainAbsent: "common subexpression",
			extraInputs:   []string{"3\n1\n2", "1\n2\n3\n3"}},
		{name: "a rebound argument is not the same expression",
			src: p(apply("(xs) -> consider k as 1 in consider a as indexof(xs, k) in consider k as 2 in " +
				"consider b as indexof(xs, k) in a * 10 + b")),
			explainAbsent: "common subexpression",
			extraInputs:   []string{"1\n2", "2\n1", "5"}},

		{name: "maximum, its index, and the item at it",
			src: p(apply("(xs) -> consider m as max(xs) in consider i as indexof(xs, m) in " +
				"consider v as item(xs, i) in tuple(m, i, v)")),
			explain:     "cheaper equivalent builtin",
			extraInputs: []string{short, "3\n9\n9\n1"}},
		{name: "minimum and item, written inline",
			src:         p(apply("(xs) -> item(xs, indexof(xs, min(xs))) + 1")),
			explain:     "cheaper equivalent builtin",
			extraInputs: []string{short, "3\n9\n9\n1"}},
		{name: "item at the index of something that may be absent",
			src:           p(apply("(xs) -> item(xs, indexof(xs, 5))")),
			explainAbsent: "cheaper equivalent builtin",
			extraInputs:   []string{"5", "1\n2\n5"}},
		{name: "item at the index of a non-extremum binding",
			src:           p(apply("(xs) -> consider m as length(xs) in consider i as indexof(xs, m) in item(xs, i)")),
			explainAbsent: "cheaper equivalent builtin",
			extraInputs:   []string{"1", "2\n1", "3\n3\n3"}},
		{name: "item at the index of a rebound maximum",
			src:           p(apply("(xs) -> consider m as max(xs) in consider m as 99 in item(xs, indexof(xs, m))")),
			explainAbsent: "cheaper equivalent builtin",
			extraInputs:   []string{"99", "1\n99", "1\n2"}},
		{name: "item at the index of a maximum of another list",
			src:           p(apply("(xs) -> consider m as max(xs) in item(take(xs, 1), indexof(take(xs, 1), m))")),
			explainAbsent: "cheaper equivalent builtin",
			extraInputs:   []string{"1\n2", "2\n1"}},

		{name: "a total costly call written twice",
			src:     p(apply("(xs) -> length(unique(xs)) + length(unique(xs)) * 2")),
			explain: "common subexpression"},
		{name: "one occurrence per arm is not shared",
			src:           p(apply("(xs) -> if length(xs) > 3 then length(unique(xs)) else length(unique(xs)) + 1")),
			explainAbsent: "common subexpression"},
		{name: "twice in one arm is shared inside the arm",
			src:     p(apply("(xs) -> if length(xs) > 3 then length(unique(xs)) * length(unique(xs)) else 0")),
			explain: "common subexpression"},
		{name: "only the right of an and is shared inside it",
			src:     p(apply("(xs) -> length(xs) > 2 and length(unique(xs)) + length(unique(xs)) > 4")),
			explain: "common subexpression"},
		{name: "a partial costly call is not moved",
			src:           p(apply("(xs) -> max(xs) + max(xs)")),
			explainAbsent: "common subexpression",
			extraInputs:   []string{short}},
		{name: "an expression over a name bound inside is shared inside",
			src:     p(apply("(xs) -> consider k as length(xs) / 2 in sum(take(xs, k)) + sum(take(xs, k))")),
			explain: "common subexpression"},
	}
}

func TestCommonSubexpressionsMatchNaiveOracle(t *testing.T) {
	runDiffCases(t, commonSubexpressionCases(), 22)
}
