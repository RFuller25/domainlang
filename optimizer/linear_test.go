package optimizer

import (
	"math/rand"
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
)

// The negative cases come first on purpose. Marking too few sites costs
// speed; marking one too many is a wrong answer, so the tests that matter are
// the ones pinning what the pass refuses.

// inPlaceSites counts the annotated updates in every lambda of a resolved,
// optimized pipeline.
func inPlaceSites(t *testing.T, src string) int {
	t.Helper()
	pipe, _ := resolveProgram(t, src, true)
	n := 0
	for _, list := range nodeLists(pipe) {
		for _, node := range list {
			lam, _ := node.Meta["lambda"].(*ast.Lambda)
			if lam == nil {
				continue
			}
			n += countInPlace(lam.Body)
		}
	}
	return n
}

func countInPlace(e ast.Expr) int {
	n := 0
	switch x := e.(type) {
	case *ast.CallExpr:
		if x.InPlace {
			n++
		}
		for _, a := range x.Args {
			n += countInPlace(a)
		}
	case *ast.BinaryExpr:
		n += countInPlace(x.Left) + countInPlace(x.Right)
	case *ast.UnaryExpr:
		n += countInPlace(x.X)
	case *ast.FieldAccess:
		n += countInPlace(x.Target)
	case *ast.CondExpr:
		n += countInPlace(x.Cond) + countInPlace(x.Then) + countInPlace(x.Else)
	case *ast.LetExpr:
		n += countInPlace(x.Value) + countInPlace(x.Body)
	case *ast.AssignExpr:
		n += countInPlace(x.Value)
	case *ast.AlsoExpr:
		n += countInPlace(x.Body)
		for _, c := range x.Clauses {
			n += countInPlace(c)
		}
	}
	return n
}

// foldSrc wraps a Fold body in a program over List<Int>.
func foldSrc(seed, body string) string {
	return "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Maximum Technique: Fold\n" +
		"    Seed: (xs) -> " + seed + "\n" +
		"    Using: " + body + "\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (m) -> size(m)\n" +
		"Reveal: stdout\n"
}

const emptyIntMap = "emptymap(0, 0)"

func TestLinearRefusesWhenTheAccumulatorIsReadAfterTheUpdate(t *testing.T) {
	for _, tc := range []struct{ name, seed, body string }{
		{
			// The canonical unsafe shape: the pre-update value is still
			// wanted, so the copy is the whole point of the call.
			name: "read in a later argument",
			seed: emptyIntMap,
			body: "(acc, x) -> tomap(concat(entries(insert(acc, x, 1)), entries(acc)))",
		},
		{
			// Nested deeper, and the read is two arguments over rather than
			// one: `size(acc)` would see the update if it went through.
			name: "read in a later argument, nested",
			seed: emptyIntMap,
			body: "(acc, x) -> tomap(list(tuple(size(insert(acc, x, 1)), 0), tuple(size(acc), 1)))",
		},
		{
			// A `consider` whose body reads the accumulator after binding an
			// updated copy of it.
			name: "read in a let body",
			seed: emptyIntMap,
			body: "(acc, x) -> consider m as insert(acc, x, 1) in if size(acc) > 0 then m else acc",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inPlaceSites(t, foldSrc(tc.seed, tc.body)); got != 0 {
				t.Errorf("marked %d site(s); the accumulator is read after the update", got)
			}
		})
	}
}

// A body that writes a binding stands every rewrite down, this one included:
// the pass reasons about evaluation order, and a write is what makes the order
// observable in a way the tree alone does not show.
func TestLinearRefusesAnUpdatingBody(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Maximum Technique: Fold\n" +
		"    Consider seen As 0\n" +
		"    Seed: (xs) -> emptymap(0, 0)\n" +
		"    Using: (acc, x) -> insert(acc, x, seen) also seen := seen + 1\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (m) -> size(m)\n" +
		"Reveal: stdout\n"
	if got := inPlaceSites(t, src); got != 0 {
		t.Errorf("marked %d site(s) in a body that writes a binding", got)
	}
}

// Scan and Iterate keep every intermediate accumulator in their output, so the
// value the update copied from is still live when the next step begins.
func TestLinearRefusesAccumulatorKeepingPrimitives(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Cursed Technique: Scan\n" +
		"    Seed: (xs) -> emptymap(0, 0)\n" +
		"    Using: (acc, x) -> insert(acc, x, 1)\n" +
		"Maximum Technique: Count\n" +
		"Reveal: stdout\n"
	if got := inPlaceSites(t, src); got != 0 {
		t.Errorf("marked %d site(s) on a Scan", got)
	}
}

// A receiver that is not the accumulator is a copy of something else, and the
// pass has proved nothing about it.
func TestLinearRefusesAnUnrootedReceiver(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Maximum Technique: Fold\n" +
		"    Seed: (xs) -> emptymap(0, emptyset(0))\n" +
		"    Using: (acc, x) -> insert(acc, x % 3, insert(getor(acc, x % 3, emptyset(0)), x))\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (m) -> size(m)\n" +
		"Reveal: stdout\n"
	// The outer insert is rooted at acc and nothing reads acc after it; the
	// inner one is rooted at a getor, so it must still copy.
	if got := inPlaceSites(t, src); got != 1 {
		t.Errorf("marked %d site(s), want exactly the outer one", got)
	}
}

func TestLinearMarksTheSafeShapes(t *testing.T) {
	for _, tc := range []struct {
		name, seed, body string
		want             int
	}{
		{
			// The frequency map. Reads of the accumulator are all arguments,
			// so they happen before the update.
			name: "read then write in one call",
			seed: emptyIntMap, want: 1,
			body: "(acc, x) -> insert(acc, x, getor(acc, x, 0) + 1)",
		},
		{
			// The conditional record — the shape a positional last-use rule
			// refuses, because `else acc` comes after.
			name: "conditional record",
			seed: emptyIntMap, want: 1,
			body: "(acc, x) -> if x > 0 then insert(acc, x, 1) else acc",
		},
		{
			// Both arms update: the arms are mutually exclusive, so a use in
			// one is not a use after a site in the other.
			name: "both arms update",
			seed: emptyIntMap, want: 2,
			body: "(acc, x) -> if x > 0 then insert(acc, x, 1) else insert(acc, 0 - x, 2)",
		},
		{
			// A chain: the outer is rooted at the inner, so neither copies.
			name: "chained inserts",
			seed: emptyIntMap, want: 2,
			body: "(acc, x) -> insert(insert(acc, x, 1), x + 1, 2)",
		},
		{
			name: "let binding the updated value",
			seed: emptyIntMap, want: 1,
			body: "(acc, x) -> consider m as insert(acc, x, 1) in m",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inPlaceSites(t, foldSrc(tc.seed, tc.body)); got != tc.want {
				t.Errorf("marked %d site(s), want %d", got, tc.want)
			}
		})
	}
}

// A `consider` that rebinds the accumulator's name hides it: no site inside
// the body is rooted at the parameter, and no read inside it is a read of it.
func TestLinearRespectsShadowing(t *testing.T) {
	src := foldSrc(emptyIntMap,
		"(acc, x) -> consider acc as emptymap(0, 0) in insert(acc, x, 1)")
	if got := inPlaceSites(t, src); got != 0 {
		t.Errorf("marked %d site(s) on a shadowed name", got)
	}
}

// --no-optimize is the copying semantics, so nothing may be marked there —
// that is what makes the naive pipeline an oracle for this pass at all.
func TestLinearMarksNothingWithoutTheOptimizer(t *testing.T) {
	src := foldSrc(emptyIntMap, "(acc, x) -> insert(acc, x, getor(acc, x, 0) + 1)")
	pipe, _ := resolveProgram(t, src, false)
	n := 0
	for _, list := range nodeLists(pipe) {
		for _, node := range list {
			if lam, _ := node.Meta["lambda"].(*ast.Lambda); lam != nil {
				n += countInPlace(lam.Body)
			}
		}
	}
	if n != 0 {
		t.Errorf("marked %d site(s) with the optimizer off", n)
	}
}

// The annotation has to survive the rewrite cascade, which rebuilds CallExpr
// nodes in two places. The pass runs last so nothing rebuilds after it, but
// the rebuilds carry the flag anyway — this pins that they do, so the ordering
// stays a policy rather than the only thing keeping it correct.
func TestInPlaceSurvivesACallExprRebuild(t *testing.T) {
	call := &ast.CallExpr{
		Fn:      &ast.Ident{Name: "insert"},
		Args:    []ast.Expr{&ast.Ident{Name: "acc"}, &ast.IntLit{Value: 1}, &ast.IntLit{Value: 2}},
		InPlace: true,
	}
	s := &simplifier{}
	if got, ok := s.simplify(call).(*ast.CallExpr); !ok || !got.InPlace {
		t.Error("expression simplification dropped the InPlace annotation")
	}
	if got, ok := substIdent(call, "nothing", &ast.IntLit{Value: 0}).(*ast.CallExpr); !ok || !got.InPlace {
		t.Error("the optimizer's expression substitution dropped the InPlace annotation")
	}
}

// --explain has to say what happened, like every other rewrite.
func TestLinearIsExplained(t *testing.T) {
	src := foldSrc(emptyIntMap, "(acc, x) -> insert(acc, x, getor(acc, x, 0) + 1)")
	_, rewrites := resolveProgram(t, src, true)
	var msgs []string
	for _, r := range rewrites {
		msgs = append(msgs, r.Message)
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "write in place") {
		t.Errorf("no rewrite mentions the in-place update:\n%s", joined)
	}
}

// Only the collection accumulators are marked: a List is a Go slice whose
// subslices alias, and a Record copy was never the cost.
func TestLinearOnlyMarksCollectionAccumulators(t *testing.T) {
	for _, t2 := range []*ir.Type{ir.Int(), ir.Text(), ir.List(ir.Int()), ir.Record()} {
		if mutableAcc(t2) {
			t.Errorf("%s should not be an in-place accumulator", t2)
		}
	}
	for _, t2 := range []*ir.Type{
		ir.Map(ir.Int(), ir.Int()), ir.Set(ir.Int()), ir.Grid(ir.Int()), ir.Sparse(ir.Int()),
	} {
		if !mutableAcc(t2) {
			t.Errorf("%s should be an in-place accumulator", t2)
		}
	}
}

// The analysis proves nothing inside the lambda reads the copied-from value
// after an update. It proves nothing about who else holds the *seed*, and a
// Part or a Channel branches from one upstream value — so whichever primitive
// drives the fold clones the accumulator once on entry.
//
// These are the tests that fail if that clone is ever removed. Each folds over
// a value a sibling Part is still going to read, and the sibling has to see
// the value it was given.
func TestTheAccumulatorCloneProtectsASharedValue(t *testing.T) {
	for _, tc := range []struct{ name, src, input, want string }{
		{
			// FoldOver: the seed *is* the current pipeline value, so this is
			// the case with no seed expression to hide behind.
			name: "FoldOver over a grid a sibling Part reads",
			src: "Cursed Energy: stdin\n" +
				"Cursed Technique: Split Text by \"\\n\"\n" +
				"Channeled Energy: Convert To Grid\n" +
				"Channel \"idx\":\n" +
				"    Cursed Technique: Apply\n        Using: (g) -> range(0, 2)\n" +
				"Part \"folded\":\n" +
				"    Maximum Technique: Fold\n        From: idx\n" +
				"        Using: (g, i) -> setat(g, i, i, \"Z\")\n" +
				"    Reveal: stdout\n" +
				"Part \"untouched\":\n    Reveal: stdout\n",
			input: "ab\ncd",
			want:  "Part folded:\nZb\ncZ\nPart untouched:\nab\ncd\n",
		},
		{
			// Reduce is seedless: the accumulator starts as an element of the
			// input list, which the pipeline still holds.
			name: "Reduce over sets the pipeline still holds",
			src: "Cursed Energy: stdin\n" +
				"Cursed Technique: Split Text by \"\\n\"\n" +
				"Channeled Energy: Convert List to Integers\n" +
				"Cursed Technique: Map Each\n    Using: (x) -> toset(list(x))\n" +
				"Part \"reduced\":\n" +
				"    Maximum Technique: Reduce\n" +
				"        Using: (a, b) -> insert(a, first(tolist(b)))\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> size(s)\n" +
				"    Reveal: stdout\n" +
				"Part \"first set\":\n" +
				"    Cursed Technique: Take Item 0\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> size(s)\n" +
				"    Reveal: stdout\n",
			input: "1\n2\n3",
			want:  "Part reduced: 3\nPart first set: 1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, optimize := range []bool{true, false} {
				pipe, _ := resolveProgram(t, tc.src, optimize)
				got, err := interpret(pipe, tc.input)
				if err != nil {
					t.Fatalf("optimize=%v: %v", optimize, err)
				}
				if got != tc.want {
					t.Errorf("optimize=%v:\n got %q\nwant %q", optimize, got, tc.want)
				}
			}
		})
	}
}

// A property test over random Fold bodies: whatever the pass decides, the
// answer has to be the one the copying pipeline gives. This is the check that
// scales past the shapes anyone thought to write down.
func TestLinearAccumulatorsMatchTheCopyingOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(1729))
	// Bodies chosen to mix the shapes the analysis reasons about: reads before
	// the write, reads after it, conditionals with one or both arms updating,
	// chains, and a shadowing consider.
	bodies := []struct{ seed, using, read string }{
		{"emptymap(0, 0)", "(acc, x) -> insert(acc, x % 7, getor(acc, x % 7, 0) + 1)",
			"(m) -> size(m) * 1000 + sum(values(m))"},
		{"emptymap(0, 0)", "(acc, x) -> if x % 3 = 0 then insert(acc, x, 1) else insert(acc, 0 - x, 2)",
			"(m) -> size(m) * 1000 + sum(keys(m))"},
		{"emptymap(0, 0)", "(acc, x) -> tomap(concat(entries(insert(acc, x % 5, 1)), entries(acc)))",
			"(m) -> size(m) * 1000 + sum(values(m))"},
		{"emptymap(0, 0)", "(acc, x) -> consider m as insert(acc, x % 4, x) in if size(acc) > 2 then acc else m",
			"(m) -> size(m) * 1000 + sum(values(m))"},
		{"emptyset(0)", "(acc, x) -> insert(insert(acc, x % 6), 0 - (x % 6))",
			"(s) -> size(s) * 1000 + sum(tolist(s))"},
		{"emptyset(0)", "(acc, x) -> if x > 0 then insert(acc, x % 5) else acc",
			"(s) -> size(s) * 1000 + sum(tolist(s))"},
		{"sparse(0)", "(acc, x) -> put(acc, x % 4, x % 3, x)",
			"(g) -> cells(g) * 1000 + at(g, 0, 0) + at(g, 9, 9)"},
	}
	for i, b := range bodies {
		src := listHeader +
			"Maximum Technique: Fold\n" +
			"    Seed: (xs) -> " + b.seed + "\n" +
			"    Using: " + b.using + "\n" +
			"Cursed Technique: Apply\n    Using: " + b.read + "\n" +
			"Reveal: stdout\n"
		naive, _ := resolveProgram(t, src, false)
		opt, _ := resolveProgram(t, src, true)
		inputs := []string{"", "0", "5", "3\n3\n3"}
		for range 80 {
			inputs = append(inputs, intsInput(randInts(rng, 14, 11)))
		}
		for _, input := range inputs {
			wantOut, wantErr := interpret(naive, input)
			gotOut, gotErr := interpret(opt, input)
			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("body %d, input %q: error divergence\noptimized: %v\nnaive: %v",
					i, input, gotErr, wantErr)
			}
			if wantErr == nil && gotOut != wantOut {
				t.Fatalf("body %d, input %q: output divergence\noptimized: %q\nnaive: %q",
					i, input, gotOut, wantOut)
			}
		}
	}
}
