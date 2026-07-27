package prims

import (
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"domain/ast"
	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/token"
)

func TestRepeat(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Repeat 3\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n * 2\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1\n2\n3") // [1,2,3] -> *2 thrice -> [8,16,24]
	if v.(int64) != 48 {
		t.Fatalf("repeat: got %v want 48", v)
	}
}

func TestWhile(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
		"Simple Domain: While\n    Using: (n) -> n > 1\n" +
		"    Cursed Technique: Apply\n        Using: (n) -> n / 2\n"
	v, _ := runPipeline(t, src, "60\n40") // sum 100 -> halve until <=1 -> 1
	if v.(int64) != 1 {
		t.Fatalf("while: got %v want 1", v)
	}
}

func TestFixedPoint(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Iterate Until Fixed Point\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n / 2\n"
	v, _ := runPipeline(t, src, "4\n8") // converges to [0,0]
	xs := v.([]ir.Value)
	if len(xs) != 2 || xs[0].(int64) != 0 || xs[1].(int64) != 0 {
		t.Fatalf("fixed point: got %v want [0 0]", v)
	}
}

func TestReverse(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nReverse Cursed Technique: Reverse\n"
	v, _ := runPipeline(t, src, "1\n2\n3")
	xs := v.([]ir.Value)
	if len(xs) != 3 || xs[0].(int64) != 3 || xs[2].(int64) != 1 {
		t.Fatalf("reverse: got %v want [3 2 1]", v)
	}
}

func TestApplyScalar(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
		"Cursed Technique: Apply\n    Using: (n) -> n * n\n"
	v, _ := runPipeline(t, src, "1\n2\n3") // sum 6 -> 36
	if v.(int64) != 36 {
		t.Fatalf("apply: got %v want 36", v)
	}
}

func TestVowInLoopBodyPasses(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Repeat 2\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n + 1\n" +
		"    Binding Vow: All Values > 0\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1\n2\n3") // +1 twice -> [3,4,5], vow holds -> 12
	if v.(int64) != 12 {
		t.Fatalf("vow-in-loop: got %v want 12", v)
	}
}

func TestVowInLoopBodyFails(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\n" +
		"Simple Domain: Repeat 5\n" +
		"    Cursed Technique: Map Each\n        Using: (n) -> n - 1\n" +
		"    Binding Vow: All Values > 0\n"
	_, err := runErr(t, src, "1\n2") // first iter -> [0,1], vow fails
	if err == nil || !strings.Contains(err.Error(), "vow violated") {
		t.Fatalf("expected vow violation in loop, got %v", err)
	}
}

func TestLoopResolveErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"body changes type",
			"Cursed Energy: stdin\nShikigami: Ints\nSimple Domain: Repeat 2\n    Maximum Technique: Sum\n",
			"must preserve the value type",
		},
		{
			"while predicate not bool",
			"Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
				"Simple Domain: While\n    Using: (n) -> n + 1\n    Cursed Technique: Apply\n        Using: (n) -> n - 1\n",
			"must return Bool",
		},
		{
			"repeat missing count",
			"Cursed Energy: stdin\nShikigami: Ints\nSimple Domain: Repeat\n    Cursed Technique: Map Each\n        Using: (n) -> n\n",
			"Repeat needs a count",
		},
		{
			"repeat negative count",
			"Cursed Energy: stdin\nShikigami: Ints\nSimple Domain: Repeat -1\n    Cursed Technique: Map Each\n        Using: (n) -> n\n",
			"Repeat count must be >= 0",
		},
	}
	for _, c := range cases {
		_, err := resolveSrc(t, c.src)
		if err == nil {
			t.Fatalf("%s: expected resolve error", c.name)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error %q does not contain %q", c.name, err.Error(), c.want)
		}
	}
}

// TestReverseTwiceIsIdentity is a property test: Reverse∘Reverse ==
// identity, over many random lists.
func TestReverseTwiceIsIdentity(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for iter := 0; iter < 200; iter++ {
		n := rng.Intn(11) + 1 // empty input is covered separately (TestReverseEmptyList)
		nums := make([]string, n)
		want := make([]ir.Value, n)
		for i := range nums {
			v := int64(rng.Intn(1000) - 500)
			nums[i] = strconv.FormatInt(v, 10)
			want[i] = v
		}
		src := "Cursed Energy: stdin\nShikigami: Ints\n" +
			"Reverse Cursed Technique: Reverse\nReverse Cursed Technique: Reverse\n"
		v, _ := runPipeline(t, src, strings.Join(nums, "\n"))
		got, _ := v.([]ir.Value)
		if len(got) != len(want) {
			t.Fatalf("iter %d: length %d want %d", iter, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("iter %d: element %d got %v want %v", iter, i, got[i], want[i])
			}
		}
	}
}

func TestReverseEmptyList(t *testing.T) {
	pos := tokenPos()
	node, err := reverse.Build(opWords("Reverse"), ArgSet{}, ir.List(ir.Int()), pos)
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, node, []ir.Value{}).([]ir.Value)
	if len(out) != 0 {
		t.Fatalf("reverse of empty list: got %v", out)
	}
}

func TestWhileIterationCap(t *testing.T) {
	old := maxLoopIterations
	maxLoopIterations = 100
	defer func() { maxLoopIterations = old }()

	src := "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n" +
		"Simple Domain: While\n    Using: (n) -> n > 0\n" +
		"    Cursed Technique: Apply\n        Using: (n) -> n + 1\n" // never terminates
	_, err := runErr(t, src, "1")
	if err == nil || !strings.Contains(err.Error(), "exceeded 100 iterations") {
		t.Fatalf("expected iteration-cap error, got %v", err)
	}
}

func TestForLoopOverChannel(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "deltas":
    Cursed Technique: Apply
        Using: (xs) -> xs

Cursed Technique: Apply
    Using: (xs) -> take(xs, 0)
Simple Domain: For x in deltas
    Cursed Technique: Apply
        Using: (acc, x) -> concat(acc, list(x))
Cursed Technique: Apply
    Using: (acc) -> length(acc)
`
	got := runForLoopProgram(t, src, "1\n2\n3")
	if got != int64(3) {
		t.Fatalf("got %v, want 3", got)
	}
}

func TestForLoopOverRange(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints
Maximum Technique: Sum
Simple Domain: For x in range(3)
    Cursed Technique: Apply
        Using: (v, x) -> v + x
`
	got := runForLoopProgram(t, src, "10")
	if got != int64(10+0+1+2) {
		t.Fatalf("got %v, want %d", got, 10+0+1+2)
	}
}

func TestForLoopWrongAmbientArityErrors(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints
Simple Domain: For x in range(2)
    Cursed Technique: Apply
        Using: (v) -> v
`
	_, err := resolveForLoopTestProgram(t, src)
	if err == nil || !strings.Contains(err.Error(), "must take 2 parameter(s)") {
		t.Fatalf("expected an ambient-arity error, got %v", err)
	}
}

func TestForLoopFilterUsesAmbient(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(1)

Cursed Technique: Apply
    Using: (v) -> v
Simple Domain: For x in one
    Cursed Technique: Filter
        Using: (v, x) -> v > x
Maximum Technique: Sum
`
	got := runForLoopProgram(t, src, "3\n-1\n5")
	if got != int64(8) { // 3 + 5, both > 1
		t.Fatalf("got %v, want 8", got)
	}
}

func TestForLoopMapEachUsesAmbient(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(1)

Cursed Technique: Apply
    Using: (v) -> v
Simple Domain: For x in one
    Cursed Technique: Map Each
        Using: (v, x) -> v + x
Maximum Technique: Sum
`
	got := runForLoopProgram(t, src, "3\n-1\n5")
	if got != int64(3+1+(-1+1)+(5+1)) {
		t.Fatalf("got %v, want %d", got, 3+1+(-1+1)+(5+1))
	}
}

func TestForLoopFoldUsesAmbient(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(1)

Simple Domain: For x in one
    Maximum Technique: Fold
        Seed: 0
        Using: (acc, v, x) -> acc + v + x
    Cursed Technique: Apply
        Using: (n, x) -> list(n)
Maximum Technique: Sum
`
	got := runForLoopProgram(t, src, "3\n-1\n5")
	if got != int64(0+3+1-1+1+5+1) {
		t.Fatalf("got %v, want %d", got, 0+3+1-1+1+5+1)
	}
}

func TestForLoopGroupByUsesAmbient(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(1)

Cursed Technique: Apply
    Using: (v) -> v
Simple Domain: For x in one
    Maximum Technique: Group By
        Using: (v, x) -> sign(v - x)
    Cursed Technique: Apply
        Using: (m, x) -> get(m, 1)
Cursed Technique: Apply
    Using: (v) -> length(v)
`
	got := runForLoopProgram(t, src, "3\n-1\n5")
	if got != int64(2) { // 3 and 5 have sign(v-1) == 1
		t.Fatalf("got %v, want 2", got)
	}
}

func TestForLoopCountByUsesAmbient(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(1)

Cursed Technique: Apply
    Using: (v) -> v
Simple Domain: For x in one
    Maximum Technique: Count By
        Using: (v, x) -> sign(v - x)
    Cursed Technique: Apply
        Using: (m, x) -> list(get(m, 1))
Cursed Technique: Apply
    Using: (v) -> item(v, 0)
`
	got := runForLoopProgram(t, src, "3\n-1\n5")
	if got != int64(2) { // 3 and 5 have sign(v-1) == 1
		t.Fatalf("got %v, want 2", got)
	}
}

func TestForLoopMinByMaxBySortByUseAmbient(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(1)

Cursed Technique: Apply
    Using: (v) -> v
Simple Domain: For x in one
    Domain Expansion: Sort By
        Using: (v, x) -> x - v
Cursed Technique: Take Item 0
`
	got := runForLoopProgram(t, src, "3\n-1\n5")
	// key = x - v = 1 - v, ascending => largest v sorts first; asymmetric in
	// v/x (unlike 0-v-x) so a reversed v/x binding would flip the winner.
	if got != int64(5) {
		t.Fatalf("got %v, want 5", got)
	}
}

func TestForLoopMapCellsGridUsesAmbient(t *testing.T) {
	src := `Cursed Energy: grid.txt
Shikigami: Digit Grid

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(1)

Simple Domain: For x in one
    Cursed Technique: Map Cells
        Using: (c, x) -> c - x
`
	t.Chdir(t.TempDir())
	if err := os.WriteFile("grid.txt", []byte("12\n34"), 0o644); err != nil {
		t.Fatal(err)
	}
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	v, err := interp.Run(pipe, &ir.Context{BaseDir: "."})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	g, ok := v.(*ir.GridValue)
	if !ok {
		t.Fatalf("result is not a Grid: %s", ir.DescribeValue(v))
	}
	want := []int64{0, 1, 2, 3}
	for i, c := range g.Cells {
		if c != want[i] {
			t.Errorf("cell %d = %v, want %d", i, c, want[i])
		}
	}
}

func TestForLoopMapCellsSparseUsesAmbient(t *testing.T) {
	src := `Cursed Energy: pts.txt
Shikigami: Lines
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: 0
    Mark: 1

Channel "one":
    Cursed Technique: Apply
        Using: (sp) -> list(1)

Simple Domain: For x in one
    Cursed Technique: Map Cells
        Using: (c, x) -> c - x
`
	t.Chdir(t.TempDir())
	if err := os.WriteFile("pts.txt", []byte("0,0\n1,1"), 0o644); err != nil {
		t.Fatal(err)
	}
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	v, err := interp.Run(pipe, &ir.Context{BaseDir: "."})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	sp, ok := v.(*ir.SparseValue)
	if !ok {
		t.Fatalf("result is not a Sparse: %s", ir.DescribeValue(v))
	}
	if sp.Def != int64(-1) { // default 0 - x(1)
		t.Errorf("default = %v, want -1", sp.Def)
	}
	if got := sp.At(0, 0); got != int64(0) { // mark 1 - x(1)
		t.Errorf("(0,0) = %v, want 0", got)
	}
}

// buildFoldOver and buildCombine (prims/channel.go) can never be exercised
// through a parsed Domain program: they're only reachable via
// resolveSequence's hasFrom(stmt) branch (prims/prims.go), which requires
// allowChannels — and every loop kind's body (For included) resolves with
// allowChannels=false unconditionally (prims/control.go's resolveLoop/
// resolveForLoop both call r.resolveSequence(stmt.Block, cur, false)), with
// no workaround via nesting (a Channel body also passes false). So these two
// call buildFoldOver/buildCombine directly, priming the ambient stack by
// hand exactly like ambient_test.go does for requireLambda.
func TestBuildFoldOverUsesAmbient(t *testing.T) {
	pushAmbient("x", ir.Int())
	defer popAmbient()

	lam := &ast.Lambda{Params: []string{"acc", "item", "x"}, Body: &ast.Ident{Name: "x"}}
	args := ArgSet{[]*ast.Arg{{Name: "Using", Value: ast.LambdaArg{Lambda: lam}}}}

	node, err := buildFoldOver(args, []string{"vals"}, []*ir.Type{ir.List(ir.Int())}, ir.Int(), token.Position{})
	if err != nil {
		t.Fatalf("buildFoldOver: %v", err)
	}

	pushAmbientValue(int64(9), ir.Int())
	defer popAmbientValue()

	ctx := &ir.Context{}
	ctx.SetChannel("vals", []ir.Value{int64(1)})
	got, err := node.Eval(ctx, int64(0))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got != int64(9) {
		t.Errorf("got %v, want 9 (the lambda body is just `x`, the pushed ambient value)", got)
	}
}

func TestBuildCombineUsesAmbient(t *testing.T) {
	pushAmbient("x", ir.Int())
	defer popAmbient()

	lam := &ast.Lambda{Params: []string{"s", "x"}, Body: &ast.Ident{Name: "x"}}
	args := ArgSet{[]*ast.Arg{{Name: "Using", Value: ast.LambdaArg{Lambda: lam}}}}

	node, err := buildCombine(args, []string{"sum"}, []*ir.Type{ir.Int()}, ir.Int(), token.Position{})
	if err != nil {
		t.Fatalf("buildCombine: %v", err)
	}

	pushAmbientValue(int64(9), ir.Int())
	defer popAmbientValue()

	ctx := &ir.Context{}
	ctx.SetChannel("sum", int64(5))
	got, err := node.Eval(ctx, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got != int64(9) {
		t.Errorf("got %v, want 9 (the lambda body is just `x`, the pushed ambient value)", got)
	}
}

func TestForLoopAllPairsUsesAmbient(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "one":
    Cursed Technique: Apply
        Using: (xs) -> list(100)

Cursed Technique: Apply
    Using: (v) -> v
Simple Domain: For x in one
    Domain Expansion: All Pairs
        Mode: Count
        Using: (a, b, x) -> a * 10 + b + x = 113
    Cursed Technique: Apply
        Using: (n, x) -> list(n)
Cursed Technique: Apply
    Using: (v) -> item(v, 0)
`
	got := runForLoopProgram(t, src, "1\n2\n3")
	// pairs (a,b): (1,2)->10+2+100=112, (1,3)->10+3+100=113 match, (2,3)->20+3+100=123
	if got != int64(1) {
		t.Fatalf("got %v, want 1", got)
	}
}

// runForLoopProgram parses+resolves+runs src against a nums.txt input file
// of body, returning the final Int value.
func runForLoopProgram(t *testing.T, src, body string) int64 {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := Resolve(prog)
	if err != nil {
		t.Fatal(err)
	}
	v, err := interp.Run(pipe, &ir.Context{BaseDir: "."})
	if err != nil {
		t.Fatal(err)
	}
	n, ok := v.(int64)
	if !ok {
		t.Fatalf("result is not an Int: %s", ir.DescribeValue(v))
	}
	return n
}

// resolveForLoopTestProgram parses+resolves src (no run), for tests that
// only care about a resolve-time error.
func resolveForLoopTestProgram(t *testing.T, src string) (*ir.Pipeline, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	return Resolve(prog)
}

func TestForLoopNestedOutermostFirst(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints

Channel "as":
    Cursed Technique: Apply
        Using: (xs) -> list(10, 20)
Channel "bs":
    Cursed Technique: Apply
        Using: (xs) -> list(1, 2)

Cursed Technique: Apply
    Using: (v) -> take(v, 0)
Simple Domain: For a in as
    Simple Domain: For b in bs
        Cursed Technique: Apply
            Using: (acc, a, b) -> concat(acc, list(a * 10 + b))
Cursed Technique: Apply
    Using: (acc) -> sum(acc)
`
	got := runForLoopProgram(t, src, "0")
	// a*10+b is position-weighted (not commutative like a+b), so a bug that
	// swapped outer/inner binding order would sum to a different, wrong
	// total instead of coincidentally matching:
	// (10*10+1)+(10*10+2)+(20*10+1)+(20*10+2) = 101+102+201+202 = 606
	if got != int64(606) {
		t.Fatalf("got %v, want 606", got)
	}
}

// TestForLoopSumOfEmptyFloatListStaysFloat is the regression test for the
// final review's finding: a lambda lexically inside a For body has an
// ambient trailing parameter, so its written arity (own + ambient) only
// matched EvalLambdaTyped's paramTypes slice once ambientTypes() could
// answer correctly at Eval time too (prims/ambient.go). Before that fix,
// paramTypes was too short inside any For body, so EvalLambdaTyped's static
// types.Env went nil and sum() of a runtime-empty list fell back to dynamic
// sniffing, which cannot distinguish List<Float> from List<Int> when both
// are empty — reopening the exact divergence commit 3195c2a closed for the
// non-loop case.
func TestForLoopSumOfEmptyFloatListStaysFloat(t *testing.T) {
	src := `Cursed Energy: nums.txt
Shikigami: Ints
Channeled Energy: Convert To Floats
Cursed Technique: Filter
    Using: (v) -> 1 = 0
Simple Domain: For x in range(1)
    Cursed Technique: Apply
        Using: (v, x) -> list(sum(v))
Cursed Technique: Apply
    Using: (v) -> first(v)
`
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := Resolve(prog)
	if err != nil {
		t.Fatal(err)
	}
	v, err := interp.Run(pipe, &ir.Context{BaseDir: "."})
	if err != nil {
		t.Fatal(err)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("result is %s (%T), want Float 0 (sum of a runtime-empty List<Float> must stay Float)", ir.DescribeValue(v), v)
	}
	if f != 0.0 {
		t.Fatalf("got %v, want 0.0", f)
	}
}
