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

// listFoldSrc is foldSrc for a List accumulator: the same shape with a tail
// that typechecks over a list rather than over a Map.
func listFoldSrc(seed, body string) string {
	return "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert To Integers\n" +
		"Maximum Technique: Fold\n" +
		"    Seed: (xs) -> " + seed + "\n" +
		"    Using: " + body + "\n" +
		"Cursed Technique: Apply\n" +
		"    Using: (m) -> length(m)\n" +
		"Reveal: stdout\n"
}

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
		{
			// A graph accumulated in a fold — the shape the whole Graph type
			// exists for, and the difference between linear and quadratic in
			// its writes.
			name: "graph edge added in a fold",
			seed: "emptygraph(0)", want: 1,
			body: "(acc, x) -> addedge(acc, x, x + 1)",
		},
		{
			name: "graph node and edge chained",
			seed: "emptygraph(0)", want: 2,
			body: "(acc, x) -> addedge(addnode(acc, x), x, x + 1, 2)",
		},
		{
			// deledge is deliberately not in the table: removing an arc shifts
			// the indices behind it, exactly as del shifts a key order.
			name: "deledge is not marked",
			seed: "emptygraph(0)", want: 0,
			body: "(acc, x) -> deledge(acc, x, x + 1)",
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

// Only the collection accumulators are marked. A List is one of them now, and
// only behind aliasSafe — the guard, not the type, is what makes it safe. A
// Record copy was never the cost and a scalar has nothing to copy.
func TestLinearOnlyMarksCollectionAccumulators(t *testing.T) {
	for _, t2 := range []*ir.Type{ir.Int(), ir.Text(), ir.Record()} {
		if mutableAcc(t2) {
			t.Errorf("%s should not be an in-place accumulator", t2)
		}
	}
	if !mutableAcc(ir.List(ir.Int())) {
		t.Error("List should be an in-place accumulator behind the alias guard")
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

// A List accumulator, and the guard that lets it be one.
//
// The negative case is the one that matters and it comes first: `take`, `drop`
// and `slice` hand out a subslice of the accumulator's own backing array in
// both backends, so a body that calls one of them can see a write the pass
// would otherwise call unobservable. The write is then not unobservable, and
// the pass has to refuse the whole lambda rather than that one site — the
// subslice may be taken before or after, and either way it aliases.
func TestLinearRefusesAListWhoseStorageEscapes(t *testing.T) {
	const seed = "range(0, 10)"
	cases := []struct {
		name string
		body string
		want int
	}{
		{"plain next-pointer write", "(acc, x) -> set(acc, x, item(acc, 0))", 1},
		// Both, not one: a chain of updates rooted at the accumulator writes
		// through at every link, which is what rootedAtAcc is for and what
		// `insert(insert(acc, …), …)` has always done.
		{"two writes, chained", "(acc, x) -> set(set(acc, x, 1), 0, x)", 2},
		{"take of the accumulator", "(acc, x) -> set(acc, x, first(take(acc, 3)))", 0},
		{"drop of the accumulator", "(acc, x) -> set(acc, x, first(drop(acc, 3)))", 0},
		{"slice of the accumulator", "(acc, x) -> set(acc, x, first(slice(acc, 1, 3)))", 0},
		// The subslice does not have to be near the write, or used for
		// anything. The guard is about the storage, not about the dataflow.
		{"take somewhere else entirely", "(acc, x) -> set(acc, x, length(take(acc, 2)) + x)", 0},
		// concat allocates, so it is not an alias source and costs nothing.
		{"concat is not an alias", "(acc, x) -> set(acc, x, length(concat(acc, acc)))", 1},
		// The accumulator read after the update is the original refusal, and
		// it still applies to a List.
		{"read after the write", "(acc, x) -> concat(set(acc, x, 1), acc)", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inPlaceSites(t, listFoldSrc(seed, tc.body))
			if got != tc.want {
				t.Errorf("marked %d sites, want %d — for %s", got, tc.want, tc.body)
			}
		})
	}
}

// The guard is asked of every accumulator kind, not only of List. A Map hands
// out no interior storage, so no Map program should be affected by it — this
// pins that the guard did not quietly narrow what already worked.
func TestTheAliasGuardLeavesMapAccumulatorsAlone(t *testing.T) {
	src := foldSrc(emptyIntMap, "(acc, x) -> insert(acc, x, getor(acc, x, 0) + 1)")
	if got := inPlaceSites(t, src); got != 1 {
		t.Errorf("a Map fold marked %d sites, want 1", got)
	}
}

// A loop that writes into the list inside its state tuple — the shape a
// simulation takes when the loop threads one value and the program needs
// somewhere to keep its other variables.
//
// The first case is load-bearing in a way the differential tests are not.
// Those compare the optimized build against the naive one, so they pass
// *vacuously* when the pass marks nothing at all — which is exactly what
// happened while this analysis read a tuple's element types out of Fields
// (the record's) instead of Elems, and reported clean for an afternoon. An
// assertion that the mark fires is the only thing that catches that.
func TestLinearMarksALoopStateWrite(t *testing.T) {
	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (x) -> tuple(x, 0, length(x), 0)
`
	const tail = `Cursed Technique: Apply
    Using: (s) -> sum(item(s, 0))
Reveal: stdout
`
	cases := []struct {
		name string
		body string
		want int
	}{
		// Every other field is read into a binding *before* the write, which
		// is what leaves nothing reading the state after it. This is how
		// bench/mahoraga/i05_jumps happens to be written, and the case below
		// shows what it costs to write it the other way.
		{"while over a state tuple", `Simple Domain: While
    Using: (s) -> item(s, 1) < item(s, 2)
    Cursed Technique: Apply
        Using: (s) -> consider i as item(s, 1) in
                      consider len as item(s, 2) in
                      consider c as item(s, 3) in
                      consider n as set(item(s, 0), i, item(item(s, 0), i) + 1) in
                      tuple(n, i + 1, len, c)
`, 1},
		{"repeat over a state tuple", `Simple Domain: Repeat 3
    Cursed Technique: Apply
        Using: (s) -> consider a as item(s, 1) in
                      consider b as item(s, 2) in
                      consider c as item(s, 3) in
                      consider n as set(item(s, 0), 0, 5) in
                      tuple(n, a, b, c)
`, 1},
		// The same program with the other fields read *after* the write, which
		// the pass used to refuse: the rule was "no read of the state after the
		// site" and did not distinguish a read of field 2 from a read of the
		// list in field 0, so the rewrite depended on the order the `consider`
		// lines happened to be written in. Reads are matched against the field
		// each one names now, and a read of field 2 cannot observe a write to
		// field 0.
		{"other fields read after the write", `Simple Domain: While
    Using: (s) -> item(s, 1) < item(s, 2)
    Cursed Technique: Apply
        Using: (s) -> consider i as item(s, 1) in
                      consider n as set(item(s, 0), i, item(item(s, 0), i) + 1) in
                      tuple(n, i + 1, item(s, 2), item(s, 3))
`, 1},
		// The same shape written with no bindings at all, which is what the
		// ordering trap made unwritable.
		{"every other field read inline after the write", `Simple Domain: While
    Using: (s) -> item(s, 1) < item(s, 2)
    Cursed Technique: Apply
        Using: (s) -> tuple(set(item(s, 0), item(s, 1), 7), item(s, 1) + 1, item(s, 2), item(s, 3))
`, 1},
		// The written field itself, read after the write. This is the read the
		// rule exists for, and it is still refused.
		{"the written field read after the write", `Simple Domain: While
    Using: (s) -> item(s, 1) < item(s, 2)
    Cursed Technique: Apply
        Using: (s) -> consider i as item(s, 1) in
                      consider n as set(item(s, 0), i, 7) in
                      tuple(n, i + sum(item(s, 0)), item(s, 2), item(s, 3))
`, 0},
		// A variable index names a different element on different laps, so the
		// projection cannot be resolved to a field and the conservative reading
		// stands: this reads the whole state as far as the pass is concerned.
		{"a variable index keeps the conservative reading", `Simple Domain: While
    Using: (s) -> item(s, 1) < item(s, 2)
    Cursed Technique: Apply
        Using: (s) -> consider i as item(s, 1) in
                      consider n as set(item(s, 0), i, 7) in
                      tuple(n, i + item(item(s, 0), i), item(s, 2), item(s, 3))
`, 0},
		// The alias guard applies to a loop state exactly as it does to a fold
		// accumulator.
		{"subslice of the state's list", `Simple Domain: While
    Using: (s) -> item(s, 1) < item(s, 2)
    Cursed Technique: Apply
        Using: (s) -> consider i as item(s, 1) in
                      consider n as set(item(s, 0), i, sum(take(item(s, 0), 2))) in
                      tuple(n, i + 1, item(s, 2), item(s, 3))
`, 0},
		// Iterate keeps every intermediate state in its output, so no lap's
		// value is ever dead. It is excluded by construction, and this pins it.
		{"iterate keeps every state", `Cursed Technique: Iterate 3
    Cursed Technique: Apply
        Using: (s) -> consider n as set(item(s, 0), 0, 5) in
                      tuple(n, item(s, 1), item(s, 2), item(s, 3))
Cursed Technique: Apply
    Using: (ss) -> first(ss)
`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inPlaceSites(t, header+tc.body+tail); got != tc.want {
				t.Errorf("marked %d sites, want %d", got, tc.want)
			}
		})
	}
}

// The projection the receiver may be followed through is a *constant tuple
// field*, and nothing else. A variable index names a different list on
// different laps, and a projection through a list reaches storage the clone on
// entry does not own — so both are refused rather than reasoned about.
func TestLinearRefusesAProjectionItCannotOwn(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (x) -> tuple(list(x, x), 0, length(x), 0)
Simple Domain: While
    Using: (s) -> item(s, 1) < item(s, 2)
    Cursed Technique: Apply
        Using: (s) -> consider i as item(s, 1) in
                      consider inner as item(item(s, 0), 0) in
                      consider n as set(inner, 0, 5) in
                      tuple(item(s, 0), i + 1, item(s, 2), item(s, 3))
Cursed Technique: Apply
    Using: (s) -> sum(first(item(s, 0)))
Reveal: stdout
`
	if got := inPlaceSites(t, src); got != 0 {
		t.Errorf("marked %d sites through a list-of-lists projection, want 0", got)
	}
}

// A Map or Set held in a loop's state tuple is the shape every non-trivial loop
// reaches for, because a loop threads one value and a program needs somewhere
// to put the rest of its variables. Following the receiver to it was gated on
// the field being a *List*, so these were cloned on every lap — a memory
// reallocation search over ~12k laps spent all of its time copying a map that
// grew to ~12k entries.
//
// Widening the gate needs no new aliasing argument. The one recorded above
// inPlaceUpdates already covers Map and Set, and it is *weaker* than the one
// accepted for List: no builtin hands out a map's or set's interior — keys,
// values and tolist all copy — while take/drop/slice hand out a list's backing
// array, which is why List alone is behind aliasSafe.
func TestLinearMarksACollectionInLoopState(t *testing.T) {
	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (x) -> tuple(x, emptymap(0, 0), emptyset(0), 0)
`
	const tail = `Cursed Technique: Apply
    Using: (s) -> size(item(s, 1)) + size(item(s, 2))
Reveal: stdout
`
	cases := []struct {
		name string
		body string
		want int
	}{
		// Every other field bound before the write, so nothing reads the state
		// after it — the same discipline the List cases above need.
		{"insert into a map in state", `Simple Domain: While
    Using: (s) -> item(s, 3) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider st as item(s, 2) in
                      consider n as item(s, 3) in
                      consider m as insert(item(s, 1), n, n * 2) in
                      tuple(xs, m, st, n + 1)
`, 1},
		{"insert into a set in state", `Simple Domain: While
    Using: (s) -> item(s, 3) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider m as item(s, 1) in
                      consider n as item(s, 3) in
                      consider st as insert(item(s, 2), n * 3) in
                      tuple(xs, m, st, n + 1)
`, 1},
		// Both collections written on the same lap, and only *one* mark comes
		// out of it. The set's write reads the state — `item(s, 2)` — and it
		// sits after the map's write, so the read-after-write rule refuses the
		// map and takes the set. The rule does not distinguish a read of field
		// 2 from a read of the map in field 1, which is the cost recorded
		// against per-field tracking; two writes to two different collections
		// on one lap cannot both be marked today, however they are ordered.
		{"map and set both written, only the last is marked", `Simple Domain: While
    Using: (s) -> item(s, 3) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider n as item(s, 3) in
                      consider m as insert(item(s, 1), n, n * 2) in
                      consider st as insert(item(s, 2), n * 3) in
                      tuple(xs, m, st, n + 1)
`, 1},
		// Other fields read after the map write. Each read names a field the
		// write cannot reach, so the rewrite stands — this is the shape that
		// used to depend on binding every field to a name beforehand.
		{"other fields read after the map write", `Simple Domain: While
    Using: (s) -> item(s, 3) < 10
    Cursed Technique: Apply
        Using: (s) -> consider m as insert(item(s, 1), item(s, 3), 1) in
                      tuple(item(s, 0), m, item(s, 2), item(s, 3) + 1)
`, 1},
		// The map itself read after the write — the case the rule exists for.
		{"map read after the map write", `Simple Domain: While
    Using: (s) -> item(s, 3) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider st as item(s, 2) in
                      consider n as item(s, 3) in
                      consider m as insert(item(s, 1), n, n * 2) in
                      tuple(xs, m, st, n + size(item(s, 1)))
`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inPlaceSites(t, header+tc.body+tail); got != tc.want {
				t.Errorf("marked %d sites, want %d", got, tc.want)
			}
		})
	}
}

// The other half of the widened pass, and the half that would be a wrong answer
// rather than a slow one. The analysis lets a mark be rooted at a Map held in a
// loop's state tuple; ownLoopState is what gives that map storage of its own
// before the loop writes through it. A field copied by reference would have the
// loop mutating a map its caller still holds — and here the caller is a sibling
// Part, which reads the same value and must still see it untouched.
//
// Both optimizer modes are run: the in-place answer has to be the copying one.
func TestTheLoopStateCloneProtectsASharedCollection(t *testing.T) {
	for _, tc := range []struct{ name, src, input, want string }{
		{
			name: "a Map in loop state a sibling Part reads",
			src: "Cursed Energy: stdin\n" +
				"Cursed Technique: Split Text by \"\\n\"\n" +
				"Channeled Energy: Convert List to Integers\n" +
				"Cursed Technique: Apply\n" +
				"    Using: (xs) -> tuple(xs, tomap(list(tuple(0, 0))), 0)\n" +
				"Part \"looped\":\n" +
				"    Simple Domain: While\n" +
				"        Using: (s) -> item(s, 2) < 5\n" +
				"        Cursed Technique: Apply\n" +
				"            Using: (s) -> consider xs as item(s, 0) in\n" +
				"                          consider n as item(s, 2) in\n" +
				"                          consider m as insert(item(s, 1), n + 1, n) in\n" +
				"                          tuple(xs, m, n + 1)\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> size(item(s, 1))\n" +
				"    Reveal: stdout\n" +
				"Part \"untouched\":\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> size(item(s, 1))\n" +
				"    Reveal: stdout\n",
			input: "1\n2\n3",
			want:  "Part looped: 6\nPart untouched: 1\n",
		},
		{
			name: "a Set in loop state a sibling Part reads",
			src: "Cursed Energy: stdin\n" +
				"Cursed Technique: Split Text by \"\\n\"\n" +
				"Channeled Energy: Convert List to Integers\n" +
				"Cursed Technique: Apply\n" +
				"    Using: (xs) -> tuple(xs, toset(list(0)), 0)\n" +
				"Part \"looped\":\n" +
				"    Simple Domain: While\n" +
				"        Using: (s) -> item(s, 2) < 5\n" +
				"        Cursed Technique: Apply\n" +
				"            Using: (s) -> consider xs as item(s, 0) in\n" +
				"                          consider n as item(s, 2) in\n" +
				"                          consider st as insert(item(s, 1), n + 1) in\n" +
				"                          tuple(xs, st, n + 1)\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> size(item(s, 1))\n" +
				"    Reveal: stdout\n" +
				"Part \"untouched\":\n" +
				"    Cursed Technique: Apply\n        Using: (s) -> size(item(s, 1))\n" +
				"    Reveal: stdout\n",
			input: "1\n2\n3",
			want:  "Part looped: 6\nPart untouched: 1\n",
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

// A receiver bound through `consider` used to defeat the pass silently. These
// two programs mean the same thing:
//
//	insert(item(s, 1), k, v)
//	consider tape as item(s, 1) in insert(tape, k, v)
//
// and only the first was rewritten, because the binding hid the projection the
// receiver walk follows. Nothing in the program looked different, which is the
// worst property an optimization can have — on the day-25 shape it was the
// difference between about a second and never finishing.
func TestLinearFollowsAConsiderBoundReceiver(t *testing.T) {
	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (x) -> tuple(x, emptymap(0, 0), 0, 0)
`
	const tail = `Cursed Technique: Apply
    Using: (s) -> size(item(s, 1)) + item(s, 3)
Reveal: stdout
`
	cases := []struct {
		name string
		body string
		want int
	}{
		// The receiver is a bound name for the map in field 1. Every other
		// field is bound too, and those reads name *other* fields, so they
		// cannot observe the write and do not stand it down.
		{"bound receiver, other fields bound too", `Simple Domain: While
    Using: (s) -> item(s, 2) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider tape as item(s, 1) in
                      consider n as item(s, 2) in
                      consider hits as item(s, 3) in
                      consider m as insert(tape, n, n * 2) in
                      tuple(xs, m, n + 1, hits)
`, 1},
		// A second name for the *written* field, read after the write. This is
		// the shape the alias tracking exists to refuse: with the reads half
		// left out, the pass marks the insert and the program answers 5|500
		// where it should answer 5|0 — a wrong answer that no reader could see
		// coming, which is why `reads` counts a name whose field overlaps a
		// write.
		{"a second name for the written field, read after", `Simple Domain: While
    Using: (s) -> item(s, 2) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider a as item(s, 1) in
                      consider b as item(s, 1) in
                      consider n as item(s, 2) in
                      consider hits as item(s, 3) in
                      consider m as insert(a, n, 100) in
                      tuple(xs, m, n + 1, hits + getor(b, n, 0))
`, 0},
		// The same alias, read *before* the write. Nothing observes the update,
		// so the rewrite stands.
		{"a second name for the written field, read before", `Simple Domain: While
    Using: (s) -> item(s, 2) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider a as item(s, 1) in
                      consider b as item(s, 1) in
                      consider n as item(s, 2) in
                      consider hits as item(s, 3) in
                      consider seen as hits + getor(b, n, 0) in
                      consider m as insert(a, n, 100) in
                      tuple(xs, m, n + 1, seen)
`, 1},
		// A name for the written field is enough on its own — the alias does not
		// have to be the receiver for a read through it to matter.
		{"alias to the field written through the projection", `Simple Domain: While
    Using: (s) -> item(s, 2) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider b as item(s, 1) in
                      consider n as item(s, 2) in
                      consider hits as item(s, 3) in
                      consider m as insert(item(s, 1), n, 100) in
                      tuple(xs, m, n + 1, hits + getor(b, n, 0))
`, 0},
		// A `consider` that shadows an alias stops it aliasing: `tape` inside
		// the inner binding is an Int, and a read of it is a read of nothing.
		{"a shadowing binding stops the alias", `Simple Domain: While
    Using: (s) -> item(s, 2) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider tape as item(s, 1) in
                      consider n as item(s, 2) in
                      consider hits as item(s, 3) in
                      consider m as insert(tape, n, n * 2) in
                      consider tape as n * 7 in
                      tuple(xs, m, n + 1, hits + tape)
`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inPlaceSites(t, header+tc.body+tail); got != tc.want {
				t.Errorf("marked %d sites, want %d", got, tc.want)
			}
		})
	}
}

// A loop body of several stages. The pass used to look at a body of exactly one
// stage, on the reasoning that with two the first stage's output is the second's
// input and that value, not the loop's, is what a write would alias. But the
// second stage's input *is* the first stage's return value — the state the loop
// threads — so each stage asks the same question about its own lambda.
//
// The restriction cost day 6 of the AoC suite its entire win: an outer search
// whose body is an Apply and a nested redistribution loop, whose map insert was
// never considered. Removing it took that program from 54.5 ms to 3.8 ms.
func TestLinearMarksEveryStageOfALoopBody(t *testing.T) {
	const header = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (x) -> tuple(x, emptymap(0, 0), 0, 0)
`
	const tail = `Cursed Technique: Apply
    Using: (s) -> size(item(s, 1)) + item(s, 3)
Reveal: stdout
`
	cases := []struct {
		name string
		body string
		want int
	}{
		// Two Apply stages, each writing a different field of the state.
		{"two stages both marked", `Simple Domain: While
    Using: (s) -> item(s, 2) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider n as item(s, 2) in
                      consider c as item(s, 3) in
                      consider m as insert(item(s, 1), n, n) in
                      tuple(xs, m, n + 1, c)
    Cursed Technique: Apply
        Using: (s) -> consider m as item(s, 1) in
                      consider n as item(s, 2) in
                      consider c as item(s, 3) in
                      consider xs as set(item(s, 0), 0, n) in
                      tuple(xs, m, n, c + 1)
`, 2},
		// A nested loop stage stops the chain, and the Apply before it is still
		// analysed — which is day 6's shape.
		{"apply then a nested loop", `Simple Domain: While
    Using: (s) -> item(s, 2) < 10
    Cursed Technique: Apply
        Using: (s) -> consider xs as item(s, 0) in
                      consider n as item(s, 2) in
                      consider c as item(s, 3) in
                      consider m as insert(item(s, 1), n, n) in
                      tuple(xs, m, n + 1, c)
    Simple Domain: While
        Using: (s) -> item(s, 3) > 0
        Cursed Technique: Apply
            Using: (s) -> consider xs as item(s, 0) in
                          consider m as item(s, 1) in
                          consider n as item(s, 2) in
                          consider c as item(s, 3) in
                          tuple(xs, m, n, c - 1)
`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inPlaceSites(t, header+tc.body+tail); got != tc.want {
				t.Errorf("marked %d sites, want %d", got, tc.want)
			}
		})
	}
}

// A node's Meta["lambda"] does not mean the same thing for every primitive: a
// loop stores its *predicate* there, which takes the state and returns a Bool.
// Treating one as a body stage would analyse the predicate as though its result
// were the next state, and a mark inside it writes through a value the loop
// re-reads on the very next lap.
//
// The shape is a loop whose entire body is another loop, so the inner loop is
// stage zero and there is no Apply to stop at.
func TestLinearDoesNotAnalyseALoopPredicateAsABodyStage(t *testing.T) {
	const src = `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (x) -> tuple(x, emptymap(0, 0), 0)
Simple Domain: While
    Using: (s) -> item(s, 2) < 3
    Simple Domain: While
        Using: (s) -> size(insert(item(s, 1), item(s, 2), 0)) > 99
        Cursed Technique: Apply
            Using: (s) -> consider xs as item(s, 0) in
                          consider m as item(s, 1) in
                          tuple(xs, m, item(s, 2) + 1)
Cursed Technique: Apply
    Using: (s) -> size(item(s, 1)) + item(s, 2)
Reveal: stdout
`
	// The insert lives in the inner loop's predicate. Whatever else the pass
	// does here, it must not mark it.
	pipe, _ := resolveProgram(t, src, true)
	for _, list := range nodeLists(pipe) {
		for _, node := range list {
			lam, _ := node.Meta["lambda"].(*ast.Lambda)
			if lam == nil || node.Prim != "Simple Domain (While)" {
				continue
			}
			if countInPlace(lam.Body) != 0 {
				t.Errorf("%s: marked an update inside a loop predicate", node.Prim)
			}
		}
	}
}
