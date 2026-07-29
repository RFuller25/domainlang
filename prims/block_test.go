package prims

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
)

// A `Using:` written as an indented pipeline body runs that sub-pipeline in
// place of the lambda, with the value the lambda's parameter would have bound
// as its current value. It works wherever a one-parameter `Using:` lambda
// does, because that is the seam it is built on (prims/block.go).

// The shape that motivated the feature — AoC 2017 day 2 part 2: find the
// evenly-divisible pair in each row of a List<List<Int>>.
func TestBlockPerRowPairSearch(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> (ikke b = 0 and a % b = 0) or (ikke a = 0 and b % a = 0)
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "5 9 2 8\n9 4 7 3\n3 8 6 5")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Combinations come in lexicographic index order, so each row reports the
	// first divisible pair it reaches: (2,8), (9,3), (3,6).
	if want := "[[2, 8], [9, 3], [3, 6]]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A body resolves to an ordinary node with an ordinary Using: lambda — the
// lambda's *body* is the sub-pipeline. That is what makes the form generic:
// nothing downstream (the primitives, the optimizer's fusions, the compiler's
// emitters) has a block case to grow, because there is no block node for them
// to recognize.
func TestBlockResolvesToAnOrdinaryLambdaNode(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Map Each
    Cursed Technique: Extract Integers
    Maximum Technique: Sum
Reveal: stdout
`
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var node *ir.Node
	for _, n := range pipe.Nodes {
		if n.Prim == "Map Each" {
			node = n
		}
	}
	if node == nil {
		t.Fatal("no Map Each node in the pipeline")
	}
	lam, _ := node.Meta["lambda"].(*ast.Lambda)
	if lam == nil {
		t.Fatal("the block form should still hand the primitive a Using: lambda")
	}
	bb, ok := lam.Body.(*ast.BlockBody)
	if !ok {
		t.Fatalf("lambda body is %T, want *ast.BlockBody", lam.Body)
	}
	if got := len(bb.Pipe.BlockNodes()); got != 2 {
		t.Fatalf("resolved body has %d nodes, want 2", got)
	}
	if want := ir.List(ir.Int()); !node.Out.Equal(want) {
		t.Fatalf("output type is %s, want %s", node.Out, want)
	}
	got, err := runProgramWithInput(t, src, "1 2 3\n40 50")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[6, 90]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Every primitive taking a one-parameter Using: lambda accepts a body, whatever
// the lambda is for and whatever type it binds — a predicate, a sort key, a
// grouping key, a projection. None of these primitives knows the form exists.
func TestBlockWorksAcrossPrimitives(t *testing.T) {
	const rows = "5 9 2 8\n9 4 7 3\n1 3 5"
	const header = `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Extract Integers
`
	cases := []struct {
		name, stage, input, want string
	}{
		{
			name: "Filter (predicate body)",
			stage: `Cursed Technique: Filter
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (s) -> s > 15
`,
			input: rows,
			want:  "[[5, 9, 2, 8], [9, 4, 7, 3]]\n",
		},
		{
			name: "Sort By (key body)",
			stage: `Domain Expansion: Sort By
    Maximum Technique: Sum
`,
			input: rows,
			want:  "[[1, 3, 5], [9, 4, 7, 3], [5, 9, 2, 8]]\n",
		},
		{
			name: "Sum By (projection body)",
			stage: `Maximum Technique: Sum By
    Maximum Technique: Max
`,
			input: rows,
			want:  "23\n",
		},
		{
			name: "Group By (key body)",
			stage: `Maximum Technique: Group By
    Maximum Technique: Count
`,
			input: rows,
			want:  "{4: [[5, 9, 2, 8], [9, 4, 7, 3]], 3: [[1, 3, 5]]}\n",
		},
		{
			name: "Max By (ordering body)",
			stage: `Maximum Technique: Max By
    Maximum Technique: Sum
`,
			input: rows,
			want:  "[5, 9, 2, 8]\n",
		},
		{
			name: "Any (predicate body)",
			stage: `Maximum Technique: Any
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (s) -> s = 9
`,
			input: rows,
			want:  "true\n",
		},
		{
			name: "Count Matching (predicate body)",
			stage: `Maximum Technique: Count Matching
    Maximum Technique: Count
    Cursed Technique: Apply
        Using: (n) -> n = 4
`,
			input: rows,
			want:  "2\n",
		},
		{
			name: "Partition (predicate body)",
			stage: `Cursed Technique: Partition
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (s) -> s > 15
`,
			input: rows,
			want:  "[[[5, 9, 2, 8], [9, 4, 7, 3]], [[1, 3, 5]]]\n",
		},
		{
			name: "Apply (whole-value body)",
			stage: `Cursed Technique: Apply
    Maximum Technique: Count
`,
			input: rows,
			want:  "3\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := runProgramWithInput(t, header+c.stage+"Reveal: stdout\n", c.input)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Bodies nest: the inner body's input is the outer body's element.
func TestBlockNests(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Cursed Technique: Map Each
    Cursed Technique: Map Each
        Cursed Technique: Extract Integers
        Maximum Technique: Sum
    Maximum Technique: Max
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "1 2 3\n4 5 6\n\n7 8\n9 10")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[15, 19]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// An enclosing For loop's ambient variable is in scope inside a body: the body
// resolves and runs within the same ambient stack, so its own lambdas pick the
// values up without the body threading anything.
func TestBlockSeesAmbientForVariable(t *testing.T) {
	src := `Cursed Energy: stdin
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
`
	got, err := runProgramWithInput(t, src, "1\n2")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[[111, 111], [112, 112]]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A Reveal inside a body prints per invocation. It is the reason the evaluator
// records the running Context: a lambda body has no Context parameter.
func TestBlockCanRevealFromInsideTheBody(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Map Each
    Cursed Technique: Extract Integers
    Maximum Technique: Sum
    Reveal: stdout
Maximum Technique: Sum
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "1 2\n3 4")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "3\n7\n10\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A Set reads as a sequence wherever a List does, the block form included.
func TestBlockOverASet(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Ints
Channeled Energy: Convert To Set
Cursed Technique: Map Each
    Cursed Technique: Apply
        Using: (n) -> n * 2
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "3\n1\n3\n2")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[6, 2, 4]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Keywords are optional everywhere, the block form included: inference fills
// in the keyword before the resolver ever sees the body.
func TestBlockWithInferredKeywords(t *testing.T) {
	src := `Cursed Energy: stdin
Lines
Map Each
    Extract Integers
    Sum
Reveal: stdout
`
	got, err := runProgramWithInput(t, src, "1 2 3\n40 50")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[6, 90]\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The two spellings are alternatives, not layers: each would define the same
// result.
func TestBlockRejectsUsingLambdaToo(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Map Each
    Using: (s) -> s
    Maximum Technique: Count
Reveal: stdout
`
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "either a Using: lambda or an indented pipeline body") {
		t.Fatalf("expected the both-forms error, got %v", err)
	}
}

// A body turns one value into one value, so it can only stand in for a
// one-parameter lambda. The refusal names the arity rather than the primitive,
// because the reason is the shape of the lambda, not the primitive's identity.
func TestBlockRefusedForMultiParameterLambdas(t *testing.T) {
	for _, c := range []struct{ name, stage string }{
		{"Reduce", "Maximum Technique: Reduce\n    Maximum Technique: Sum\n"},
		{"Fold", "Maximum Technique: Fold\n    Seed: 0\n    Maximum Technique: Sum\n"},
		{"All Pairs", "Domain Expansion: All Pairs\n    Mode: First\n    Maximum Technique: Sum\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "Cursed Energy: stdin\nShikigami: Ints\n" + c.stage + "Reveal: stdout\n"
			_, err := resolveSrc(t, src)
			if err == nil || !strings.Contains(err.Error(), "-parameter Using: lambda") {
				t.Fatalf("expected an arity refusal, got %v", err)
			}
			if !strings.Contains(err.Error(), "one value from one value") {
				t.Fatalf("the refusal should say why, got %v", err)
			}
		})
	}
}

// A primitive with no Using: lambda at all has nothing for a body to stand in
// for, and the body is refused rather than silently dropped.
func TestBlockRefusedWhereThereIsNoLambda(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Ints
Maximum Technique: Sum
    Maximum Technique: Count
Reveal: stdout
`
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "Sum does not take an indented pipeline body") {
		t.Fatalf("expected a refusal naming Sum, got %v", err)
	}
}

// A type error inside a body is reported at the offending body line, with the
// primitive named so the position is not orphaned.
func TestBlockReportsBodyTypeErrors(t *testing.T) {
	src := `Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Map Each
    Maximum Technique: Sum
Reveal: stdout
`
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "in the body") {
		t.Fatalf("expected a body error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Sum expects input of type List<Int>") {
		t.Fatalf("the inner error should survive, got %v", err)
	}
}

// A body is a nested scope like a loop's: channels are top-level things and a
// From: consumer would read a channel that is not per-invocation.
func TestBlockRefusesChannelsAndConsumers(t *testing.T) {
	channelSrc := `Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Map Each
    Channel "inner":
        Maximum Technique: Count
    Cursed Technique: Apply
        Using: (n) -> n
Reveal: stdout
`
	_, err := resolveSrc(t, channelSrc)
	if err == nil || !strings.Contains(err.Error(), "Channels cannot be nested inside a loop, Shikigami, or Using: body") {
		t.Fatalf("expected a channel scope error, got %v", err)
	}

	consumerSrc := `Cursed Energy: stdin
Shikigami: Ints
Channel "c":
    Maximum Technique: Count
Cursed Technique: Map Each
    Maximum Technique: Combine
        From: c
        Using: (x) -> x
Reveal: stdout
`
	_, err = resolveSrc(t, consumerSrc)
	if err == nil || !strings.Contains(err.Error(), "From: consumers are not allowed inside a loop, Shikigami, or Using: body") {
		t.Fatalf("expected a consumer scope error, got %v", err)
	}
}

// The primitive's own input rule still applies: Map Each needs a list whether
// its per-element function is a lambda or a body.
func TestBlockDoesNotBypassTheInputTypeCheck(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Map Each
    Maximum Technique: Count
Reveal: stdout
`
	_, err := resolveSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "Map Each expects a List input, got Text") {
		t.Fatalf("expected a list-input error, got %v", err)
	}
}
