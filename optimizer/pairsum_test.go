package optimizer

import (
	"math/rand"
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/token"
)

// bruteFirstPair is the naive oracle: the values of the lexicographically-first
// index pair i<j summing to target.
func bruteFirstPair(xs []int64, target int64) ([]int64, bool) {
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			if xs[i]+xs[j] == target {
				return []int64{xs[i], xs[j]}, true
			}
		}
	}
	return nil, false
}

func bruteCountPairs(xs []int64, target int64) int64 {
	var c int64
	for i := 0; i < len(xs); i++ {
		for j := i + 1; j < len(xs); j++ {
			if xs[i]+xs[j] == target {
				c++
			}
		}
	}
	return c
}

// TestPairSumMatchesOracle proves the hash-set scans match the naive O(n²)
// oracle across many random inputs (the v0.1 oracle methodology).
func TestPairSumMatchesOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 3000; iter++ {
		n := rng.Intn(20)
		xs := make([]int64, n)
		for i := range xs {
			xs[i] = int64(rng.Intn(13) - 6) // duplicates, negatives
		}
		target := int64(rng.Intn(13) - 6)

		gotPair, gotOK := FindPairSum(xs, target)
		wantPair, wantOK := bruteFirstPair(xs, target)
		if gotOK != wantOK {
			t.Fatalf("iter %d: FindPairSum ok=%v want %v (xs=%v target=%d)", iter, gotOK, wantOK, xs, target)
		}
		if gotOK && (gotPair[0] != wantPair[0] || gotPair[1] != wantPair[1]) {
			t.Fatalf("iter %d: FindPairSum=%v want %v (xs=%v target=%d)", iter, gotPair, wantPair, xs, target)
		}
		if got, want := CountPairSum(xs, target), bruteCountPairs(xs, target); got != want {
			t.Fatalf("iter %d: CountPairSum=%d want %d (xs=%v target=%d)", iter, got, want, xs, target)
		}
	}
}

func lambdaFrom(t *testing.T, src string) *ast.Lambda {
	t.Helper()
	full := "X:\n    Using: " + src + "\n"
	toks, err := lexer.Lex(full)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(full, toks)
	if err != nil {
		t.Fatal(err)
	}
	return prog.Statements[0].Args[0].Value.(ast.LambdaArg).Lambda
}

func TestMatchSumPair(t *testing.T) {
	good := map[string]int64{
		"(a, b) -> a + b = 2020": 2020,
		"(a, b) -> 2020 = a + b": 2020,
		"(a, b) -> b + a = 99":   99,
	}
	for src, want := range good {
		k, ok := matchSumPair(lambdaFrom(t, src))
		if !ok || k != want {
			t.Fatalf("%s: got (%d,%v) want (%d,true)", src, k, ok, want)
		}
	}
	bad := []string{
		"(a, b) -> a * b = 2020", // not a sum
		"(a, b) -> a + b > 2020", // not equality
		"(a, b, c) -> a + b = 1", // wrong arity
		"(a, b) -> a + a = 2020", // not both params
	}
	for _, src := range bad {
		if _, ok := matchSumPair(lambdaFrom(t, src)); ok {
			t.Fatalf("%s: expected no match", src)
		}
	}
}

// dupParamLambda builds the AST for `(a, a) -> a + a = 6` directly, bypassing
// the parser. The parser now rejects a repeated lambda parameter name at
// parse time (see parser.TestDuplicateLambdaParamRejected), so this shape can
// no longer be produced by lambdaFrom/parser.Parse; it is built by hand here
// to exercise matchSumPair's own belt-and-suspenders defense below.
func dupParamLambda() *ast.Lambda {
	return &ast.Lambda{
		Params: []string{"a", "a"},
		Body: &ast.BinaryExpr{
			Op: token.EQ,
			Left: &ast.BinaryExpr{
				Op:    token.PLUS,
				Left:  &ast.Ident{Name: "a"},
				Right: &ast.Ident{Name: "a"},
			},
			Right: &ast.IntLit{Value: 6},
		},
	}
}

// TestMatchSumPairRejectsDuplicateParamNames is a regression test. A lambda
// like `(a, a) -> a + a = K` used to be legal Domain (nothing rejected a
// repeated parameter name), and eval.EvalLambda's map-based Env made the
// second binding shadow the first, so the naive evaluator only ever saw one
// element of the pair (doubled) — it never actually summed two distinct list
// elements. matchSumPair used to accept this shape anyway (isSumOf only
// checks that the two operand names are {p0, p1} as a set), which meant the
// optimizer would install a real two-distinct-element hash-set scan in place
// of a naive path that computes something else entirely — an optimization
// that silently changes the program's output.
//
// The parser now rejects duplicate lambda parameter names outright, so this
// input can no longer reach matchSumPair through normal parsing; the AST is
// constructed directly here to confirm matchSumPair's own check still holds
// as defense in depth.
func TestMatchSumPairRejectsDuplicateParamNames(t *testing.T) {
	if _, ok := matchSumPair(dupParamLambda()); ok {
		t.Fatal("matchSumPair must reject lambdas with duplicate parameter names")
	}
}

// TestFuseAllPairsSkipsDuplicateParamLambda proves the fix end-to-end: with a
// duplicate-param lambda, Optimize must leave the All Pairs node untouched
// (no rewrite), so the naive Eval is what actually runs. As above, such a
// lambda can no longer come from the parser, so the AST is built by hand.
func TestFuseAllPairsSkipsDuplicateParamLambda(t *testing.T) {
	pipe := &ir.Pipeline{Nodes: []*ir.Node{{
		Prim: "All Pairs",
		In:   ir.List(ir.Int()),
		Out:  ir.List(ir.Int()),
		Meta: map[string]any{
			"k":      2,
			"mode":   "First",
			"lambda": dupParamLambda(),
		},
	}}}
	rewrites := Optimize(pipe, true)
	if len(rewrites) != 0 {
		t.Fatalf("expected no rewrites for a duplicate-param lambda, got %d", len(rewrites))
	}
	if pipe.Nodes[0].Prim != "All Pairs" {
		t.Fatalf("duplicate-param lambda must not be rewritten to HashSetPairScan, got %q", pipe.Nodes[0].Prim)
	}
}

func TestFuseAllPairsFiresAndDisabled(t *testing.T) {
	mkPipe := func() *ir.Pipeline {
		return &ir.Pipeline{Nodes: []*ir.Node{{
			Prim: "All Pairs",
			In:   ir.List(ir.Int()),
			Out:  ir.List(ir.Int()),
			Meta: map[string]any{
				"k":      2,
				"mode":   "First",
				"lambda": lambdaFrom(t, "(a, b) -> a + b = 2020"),
			},
		}}}
	}

	pipe := mkPipe()
	rewrites := Optimize(pipe, true)
	if len(rewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %d", len(rewrites))
	}
	if pipe.Nodes[0].Prim != "HashSetPairScan" {
		t.Fatalf("expected HashSetPairScan, got %q", pipe.Nodes[0].Prim)
	}
	// Output type is preserved.
	if !pipe.Nodes[0].Out.Equal(ir.List(ir.Int())) {
		t.Fatalf("output type changed: %s", pipe.Nodes[0].Out)
	}
	// The rewritten eval finds the pair.
	v, err := pipe.Nodes[0].Eval(&ir.Context{}, ir.IntsToValue([]int64{1721, 979, 366, 299}))
	if err != nil {
		t.Fatal(err)
	}
	pair := v.([]ir.Value)
	if pair[0].(int64) != 1721 || pair[1].(int64) != 299 {
		t.Fatalf("hash-set pair: got %v want [1721 299]", pair)
	}

	// Disabled: untouched.
	pipe2 := mkPipe()
	if r := Optimize(pipe2, false); len(r) != 0 {
		t.Fatalf("expected no rewrites when disabled, got %d", len(r))
	}
	if pipe2.Nodes[0].Prim != "All Pairs" {
		t.Fatalf("disabled optimizer must not rewrite the node")
	}
}

// The sum-to-constant rewrite reaches nested node lists, not just the top
// level: an All Pairs inside a per-element Map Each body — the shape a
// List<List<Int>> forces — is the same O(n²) request and gets the same
// substitution. Its siblings in scans.go and product.go already walked
// nodeLists; this one did not, so a Channel or loop body missed it too.
func TestPairSumRewriteReachesNestedBodies(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> a + b = 100
    Maximum Technique: Product
Reveal: stdout
`
	const input = "1 5 99 3\n50 50 2"

	pipe, rewrites := resolveProgram(t, src, true)
	if len(rewrites) != 1 || !strings.Contains(rewrites[0].Message, "Hash-Set Scan") {
		t.Fatalf("expected the pair-sum rewrite to fire inside the body, got %v", rewrites)
	}
	var scans int
	for _, list := range nodeLists(pipe) {
		for _, n := range list {
			if n.Prim == "HashSetPairScan" {
				scans++
			}
		}
	}
	if scans != 1 {
		t.Fatalf("expected one rewritten node in the body, found %d", scans)
	}

	// The naive pipeline is the oracle, as everywhere else in the optimizer.
	got, err := interpret(pipe, input)
	if err != nil {
		t.Fatalf("optimized run: %v", err)
	}
	naive, _ := resolveProgram(t, src, false)
	want, err := interpret(naive, input)
	if err != nil {
		t.Fatalf("naive run: %v", err)
	}
	if got != want {
		t.Fatalf("optimized output %q diverges from naive %q", got, want)
	}
}
