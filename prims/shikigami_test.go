package prims

import (
	"fmt"
	"strings"
	"testing"

	"domain/ir"
	"domain/optimizer"
	"domain/token"
)

// the AoC 2022 Day 1 example, grouped by elf.
const day1Example = "1000\n2000\n3000\n\n4000\n\n5000\n6000\n\n7000\n8000\n9000\n\n10000"

func TestPreludeInts(t *testing.T) {
	src := "Cursed Energy: stdin\nShikigami: Ints\nMaximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "10\n20\n30")
	if v.(int64) != 60 {
		t.Fatalf("Ints then Sum: got %v want 60", v)
	}
}

func TestPreludeTopKSum(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\\n\"\n" +
		"Cursed Technique: Split Each by \"\\n\"\n" +
		"Channeled Energy: Convert Each List to Integers\n" +
		"Maximum Technique: Sum Each Group\n" +
		"Shikigami: Top K Sum\n    k: 3\n"
	v, _ := runPipeline(t, src, day1Example)
	if v.(int64) != 45000 {
		t.Fatalf("Top K Sum: got %v want 45000", v)
	}
}

func TestUserShikigamiOpParam(t *testing.T) {
	src := "Shikigami \"Biggest\" (k: Int)\n" +
		"    Domain Expansion: Quicksort, Descending\n" +
		"    Maximum Technique: Select Top k, Sum\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Biggest\n    k: 2\n"
	v, _ := runPipeline(t, src, "1\n5\n3\n9\n2") // top 2 = 9 + 5
	if v.(int64) != 14 {
		t.Fatalf("Biggest k=2 sum: got %v want 14", v)
	}
}

func TestUserShikigamiLambdaParam(t *testing.T) {
	src := "Shikigami \"At Least\" (t: Int)\n" +
		"    Cursed Technique: Filter\n" +
		"        Using: (n) -> n >= t\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: At Least\n    t: 3\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "1\n2\n3\n4\n5") // n >= 3 -> {3,4,5}
	if v.(int64) != 3 {
		t.Fatalf("At Least t=3 count: got %v want 3", v)
	}
}

// TestShikigamiParamNameCollidesWithDispatchWord is a regression test.
// substituteOp used to replace any operation word matching a declared
// parameter's name by blind string equality, with no regard for whether the
// word actually drove primitive dispatch. A Shikigami parameter named
// "Matching" on a `Count Matching` line silently stripped "Matching" from
// the phrase, re-dispatching the statement to the unrelated `Count`
// primitive (plain cardinality, ignoring Using: entirely) with no error
// raised anywhere in the pipeline.
func TestShikigamiParamNameCollidesWithDispatchWord(t *testing.T) {
	src := "Shikigami \"Weird\" (Matching: Int)\n" +
		"    Maximum Technique: Count Matching\n" +
		"        Using: (n) -> n > 100\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Weird\n    Matching: 1\n"
	v, _ := runPipeline(t, src, "1\n2\n3\n4\n5")
	// None of the elements are > 100, so Count Matching must report 0, not
	// silently re-dispatch to plain Count (which would report 5).
	if v.(int64) != 0 {
		t.Fatalf("Count Matching with a colliding param name: got %v want 0 (must not misdispatch to plain Count)", v)
	}
}

// TestShikigamiParamNameCollidesWithSplitEach covers a second instance of the
// same class of bug: a parameter named "Each" on a `Split Each` line must not
// cause misdispatch to plain `Split`.
func TestShikigamiParamNameCollidesWithSplitEach(t *testing.T) {
	src := "Shikigami \"Weird\" (Each: Int)\n" +
		"    Cursed Technique: Split Text by \"\\n\\n\"\n" +
		"    Cursed Technique: Split Each by \"\\n\"\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Weird\n    Each: 1\n" +
		"Maximum Technique: Count\n"
	v, _ := runPipeline(t, src, "a\nb\n\nc\nd")
	// Split Text by "\n\n" -> 2 groups, then Split Each by "\n" -> List<List<Text>>
	// of length 2. A misdispatch of "Split Each" to plain "Split" would fail
	// to type-check at all (Split wants Text, not List<Text>) — this proves
	// dispatch still lands on Split Each with the colliding param present.
	if v.(int64) != 2 {
		t.Fatalf("Split Each with a colliding param name: got %v want 2 (must not misdispatch to plain Split)", v)
	}
}

// TestUserShikigamiLambdaParamShadowing confirms a lambda's own bound
// parameter shadows an outer Shikigami parameter of the same name, rather
// than being substituted away (substExpr's `shadowed` map).
func TestUserShikigamiLambdaParamShadowing(t *testing.T) {
	src := "Shikigami \"AddT\" (t: Int)\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (t) -> t + 1\n" + // the lambda's own "t" must shadow the outer "t"
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: AddT\n    t: 100\n" +
		"Maximum Technique: Sum\n"
	v, _ := runPipeline(t, src, "1\n2\n3") // (1+1)+(2+1)+(3+1) = 9, not using the outer t=100
	if v.(int64) != 9 {
		t.Fatalf("lambda param shadowing: got %v want 9", v)
	}
}

// TestUserShikigamiTextParamInLambda confirms a Text-typed Shikigami
// parameter substitutes into a lambda body as a StringLit (the other half of
// substExpr's Ident case, alongside the Int case already covered above).
func TestUserShikigamiTextParamInLambda(t *testing.T) {
	src := "Shikigami \"IsCmd\" (want: Text)\n" +
		"    Cursed Technique: Match Pattern\n" +
		"        Using: \"{cmd:word}: {rest:text}\"\n" +
		"    Cursed Technique: Apply\n" +
		"        Using: (r) -> r.cmd = want\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: IsCmd\n    want: \"note\"\n"
	v, _ := runPipeline(t, src, "note: hello")
	if v.(bool) != true {
		t.Fatalf("got %v want true (cmd should equal the substituted Text param)", v)
	}
	v2, _ := runPipeline(t, src, "todo: hello")
	if v2.(bool) != false {
		t.Fatalf("got %v want false", v2)
	}
}

func TestArgSetHas(t *testing.T) {
	args := ArgSet{}
	if args.Has("Mode") {
		t.Fatal("empty ArgSet should not have any named argument")
	}
}

func TestShikigamiResolveErrors(t *testing.T) {
	topK := "Shikigami \"Top K\" (k: Int)\n" +
		"    Domain Expansion: Quicksort, Descending\n" +
		"    Maximum Technique: Select Top k, Sum\n"

	cases := []struct {
		name, src, want string
	}{
		{
			"unknown shikigami",
			"Cursed Energy: stdin\nShikigami: Nope\n",
			"unknown Shikigami",
		},
		{
			"missing parameter",
			topK + "Cursed Energy: stdin\nShikigami: Ints\nShikigami: Top K\n",
			"requires Int parameter",
		},
		{
			"wrong parameter type",
			topK + "Cursed Energy: stdin\nShikigami: Ints\nShikigami: Top K\n    k: \"three\"\n",
			"requires Int parameter",
		},
		{
			// Top K Sum declares `: List<Int> -> Int`, so supplying Text is
			// caught at the boundary rather than as an inlining trace.
			"input type violates a declared signature",
			"Cursed Energy: stdin\nShikigami: Top K Sum\n    k: 3\n",
			`Shikigami "Top K Sum" expects input of type List<Int>, but the pipeline produced Text`,
		},
		{
			// Without a signature the failure still surfaces from inside the
			// body, with the inlining trace.
			"type error inside an unsigned body",
			"Shikigami \"Unsigned\"\n    Domain Expansion: Quicksort\n\nCursed Energy: stdin\nShikigami: Unsigned\n",
			"in Shikigami \"Unsigned\" (body at 2:",
		},
		{
			"body does not produce what the signature declares",
			"Shikigami \"Lying\" : Text -> Int\n    Cursed Technique: Split Text by \",\"\n\nCursed Energy: stdin\nShikigami: Lying\n",
			"declares it produces Int",
		},
		{
			"unknown type in a signature",
			"Shikigami \"Bad\" : Nope -> Int\n    Maximum Technique: Sum\n\nCursed Energy: stdin\nShikigami: Bad\n",
			"unknown type \"Nope\"",
		},
		{
			"unkeyable map key in a signature",
			"Shikigami \"Bad\" : Map<Float, Int> -> Int\n    Maximum Technique: Sum\n\nCursed Energy: stdin\nShikigami: Bad\n",
			"Map keys must be keyable",
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

// TestShikigamiErrorTrace pins the inlining trace: an error inside
// a user-defined body carries the call site as its position plus a "body at
// L:C" pointer into the definition, and an error inside a prelude body is
// labeled as prelude source rather than masquerading as a user-file position.
func TestShikigamiErrorTrace(t *testing.T) {
	// The bad statement is the Quicksort on line 2 of the user's file; the
	// call site is line 5.
	src := "Shikigami \"Sort Text\"\n" +
		"    Domain Expansion: Quicksort\n" +
		"\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Sort Text\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "5:") {
		t.Fatalf("outer position should be the call site (line 5), got: %q", msg)
	}
	if !strings.Contains(msg, `in Shikigami "Sort Text" (body at 2:`) {
		t.Fatalf("error should point into the body, got: %q", msg)
	}

	// Every prelude definition declares a signature, so a mistyped call to one
	// is now caught at the boundary — a better error than the inlining trace it
	// used to produce, and still positioned at the call site.
	preludeCall := "Cursed Energy: stdin\nShikigami: Top K Sum\n    k: 3\n"
	_, err = resolveSrc(t, preludeCall)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	msg = err.Error()
	if !strings.HasPrefix(msg, "2:") {
		t.Fatalf("outer position should be the call site (line 2), got: %q", msg)
	}
	if !strings.Contains(msg, `Shikigami "Top K Sum" expects input of type List<Int>`) {
		t.Fatalf("a declared signature should be checked at the call, got: %q", msg)
	}

	// A user definition shadowing a prelude name is the user's code again:
	// no prelude label, body position in the user's file.
	shadow := "Shikigami \"Ints\"\n" +
		"    Domain Expansion: Quicksort\n" +
		"\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n"
	_, err = resolveSrc(t, shadow)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	if strings.Contains(err.Error(), "prelude") {
		t.Fatalf("shadowed prelude name must not be labeled prelude, got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), `in Shikigami "Ints" (body at 2:`) {
		t.Fatalf("shadowed definition should trace into the user body, got: %q", err.Error())
	}
}

// The label helpers decide which file a position inside an inlined body belongs
// to. token.Position carries no file, so getting this wrong prints coordinates
// that look like the user's own source. The prelude branch is unreachable
// through the prelude itself now that all five definitions declare signatures
// (a mistyped call is caught at the boundary first), so it is pinned directly.
func TestBodyPositionLabels(t *testing.T) {
	r := &resolver{
		origins: map[string]DefSite{
			"Local":    {Origin: "local"},
			"Prelude":  {Origin: "prelude"},
			"Imported": {Origin: "import", Path: "/libs/aoc.domain"},
		},
		displays: map[string]string{"Imported": "aoc.domain"},
	}
	pos := token.Position{Line: 7, Col: 3}
	cases := []struct{ name, wantBody, wantDef string }{
		{"Local", "body at 7:3", "defined at 7:3"},
		{"Prelude", "prelude source 7:3", "prelude source 7:3"},
		{"Imported", "aoc.domain:7:3", "aoc.domain:7:3"},
		{"Unknown", "body at 7:3", "defined at 7:3"}, // no origin recorded
	}
	for _, c := range cases {
		if got := r.whereInBody(c.name, pos); got != c.wantBody {
			t.Errorf("whereInBody(%s) = %q, want %q", c.name, got, c.wantBody)
		}
		if got := r.whereDefined(c.name, pos); got != c.wantDef {
			t.Errorf("whereDefined(%s) = %q, want %q", c.name, got, c.wantDef)
		}
	}
}

// Float, Bool and lambda parameters. Float and Bool substitute into lambda
// bodies as literals; a lambda parameter substitutes as a whole argument, which
// is what makes a Shikigami higher-order.
func TestShikigamiFloatParam(t *testing.T) {
	src := "Shikigami \"Scale\" (f: Float) : List<Int> -> List<Float>\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x * f\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Scale\n    f: 1.5\n"
	v, _ := runPipeline(t, src, "2\n4")
	got := ir.FormatValue(v)
	if got != "[3, 6]" {
		t.Fatalf("Scale f=1.5: got %s want [3, 6]", got)
	}
}

// An Int argument widens to a Float parameter, matching the numeric tower's
// single promotion rule.
func TestShikigamiFloatParamAcceptsInt(t *testing.T) {
	src := "Shikigami \"Scale\" (f: Float) : List<Int> -> List<Float>\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x * f\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Scale\n    f: 2\n"
	v, _ := runPipeline(t, src, "3")
	if got := ir.FormatValue(v); got != "[6]" {
		t.Fatalf("got %s want [6]", got)
	}
}

func TestShikigamiBoolParam(t *testing.T) {
	src := "Shikigami \"Pick\" (high: Bool) : List<Int> -> List<Int>\n" +
		"    Cursed Technique: Filter\n" +
		"        Using: (x) -> if high then x > 2 else x <= 2\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Pick\n    high: true\n"
	v, _ := runPipeline(t, src, "1\n2\n3\n4")
	if got := ir.FormatValue(v); got != "[3, 4]" {
		t.Fatalf("high: true gave %s, want [3, 4]", got)
	}

	low := strings.Replace(src, "high: true", "high: false", 1)
	v2, _ := runPipeline(t, low, "1\n2\n3\n4")
	if got := ir.FormatValue(v2); got != "[1, 2]" {
		t.Fatalf("high: false gave %s, want [1, 2]", got)
	}
}

// A lambda parameter: the caller supplies the predicate, so one Shikigami
// abstracts over "count the elements matching anything".
func TestShikigamiLambdaValuedParam(t *testing.T) {
	src := "Shikigami \"Count Where\" (p: (Int) -> Bool) : List<Int> -> Int\n" +
		"    Maximum Technique: Count Matching\n" +
		"        Using: p\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Count Where\n    p: (x) -> x > 100\n"
	v, _ := runPipeline(t, src, "5\n200\n300\n1")
	if v.(int64) != 2 {
		t.Fatalf("Count Where: got %v want 2", v)
	}
}

// The same lambda parameter used at two sites is typed independently at each,
// which is a feature of textual substitution, not a leak.
func TestShikigamiLambdaParamUsedTwice(t *testing.T) {
	src := "Shikigami \"Both\" (p: (Int) -> Bool) : List<Int> -> Int\n" +
		"    Cursed Technique: Filter\n" +
		"        Using: p\n" +
		"    Maximum Technique: Count Matching\n" +
		"        Using: p\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Both\n    p: (x) -> x > 2\n"
	v, _ := runPipeline(t, src, "1\n3\n4")
	if v.(int64) != 2 {
		t.Fatalf("Both: got %v want 2", v)
	}
}

// A two-parameter lambda type, passed to a Fold.
func TestShikigamiBinaryLambdaParam(t *testing.T) {
	src := "Shikigami \"Combine All\" (f: (Int, Int) -> Int) : List<Int> -> Int\n" +
		"    Maximum Technique: Reduce\n" +
		"        Using: f\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Combine All\n    f: (a, b) -> a * 10 + b\n"
	v, _ := runPipeline(t, src, "1\n2\n3")
	if v.(int64) != 123 {
		t.Fatalf("Combine All: got %v want 123", v)
	}
}

func TestShikigamiParamErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"missing float argument",
			"Shikigami \"S\" (f: Float)\n    Cursed Technique: Map Each\n        Using: (x) -> x * f\n" +
				"Cursed Energy: stdin\nShikigami: Ints\nShikigami: S\n",
			"requires Float parameter",
		},
		{
			"bool argument is not true or false",
			"Shikigami \"S\" (b: Bool)\n    Cursed Technique: Filter\n        Using: (x) -> if b then x > 0 else x < 0\n" +
				"Cursed Energy: stdin\nShikigami: Ints\nShikigami: S\n    b: maybe\n",
			"takes true or false",
		},
		{
			"missing lambda argument",
			"Shikigami \"S\" (p: (Int) -> Bool)\n    Cursed Technique: Filter\n        Using: p\n" +
				"Cursed Energy: stdin\nShikigami: Ints\nShikigami: S\n",
			"requires (Int) -> Bool parameter",
		},
		{
			"lambda argument has the wrong arity",
			"Shikigami \"S\" (p: (Int) -> Bool)\n    Cursed Technique: Filter\n        Using: p\n" +
				"Cursed Energy: stdin\nShikigami: Ints\nShikigami: S\n    p: (a, b) -> a > b\n",
			"takes 1 argument(s), got 2",
		},
		{
			"lambda argument returns the wrong type",
			"Shikigami \"S\" (p: (Int) -> Bool)\n    Cursed Technique: Filter\n        Using: p\n" +
				"Cursed Energy: stdin\nShikigami: Ints\nShikigami: S\n    p: (x) -> x + 1\n",
			"but its lambda returns Int",
		},
		{
			"a parameter cannot be a composite value",
			"Shikigami \"S\" (g: Grid<Int>)\n    Cursed Technique: Transpose\n" +
				"Cursed Energy: stdin\nShikigami: Digit Grid\nShikigami: S\n    g: 1\n",
			"must be Int, Text, Float, Bool, or a lambda type",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := resolveSrc(t, c.src)
			if err == nil {
				t.Fatalf("expected an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.want)
			}
		})
	}
}

// A signature is a check, not a compilation boundary: the body is still inlined,
// so the optimizer still fuses straight through a signed Shikigami. This is the
// property the language's whole thesis rests on.
func TestSignatureDoesNotBlockInlining(t *testing.T) {
	src := "Shikigami \"Top Two\" : List<Int> -> Int\n" +
		"    Domain Expansion: Quicksort, Descending\n" +
		"    Maximum Technique: Select Top 2, Sum\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Top Two\n"
	pipe, err := resolveSrc(t, src)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !hasPrims(pipe.Nodes, "Sort", "SelectTopK") {
		t.Fatalf("body was not inlined: %v", primNames(pipe.Nodes))
	}
	optimizer.Optimize(pipe, true)
	if !hasPrims(pipe.Nodes, "PartialSelect") {
		t.Errorf("quickselect did not fire through a signed Shikigami: %v", primNames(pipe.Nodes))
	}
}

// Regression: substExpr had no CondExpr case, so a Shikigami parameter used
// anywhere inside an `if/then/else` was returned unsubstituted and then failed
// to resolve as an unknown identifier. It affected every parameter kind, not
// just the Bool parameters that surfaced it.
func TestShikigamiParamInsideConditional(t *testing.T) {
	src := "Shikigami \"Cap At\" (cap: Int)\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> if x > cap then cap else x\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Cap At\n    cap: 3\n"
	v, _ := runPipeline(t, src, "1\n5\n2\n9")
	if got := ir.FormatValue(v); got != "[1, 3, 2, 3]" {
		t.Fatalf("Cap At cap=3: got %s want [1, 3, 2, 3]", got)
	}

	// Nested conditionals, and a Text parameter in an arm.
	textSrc := "Shikigami \"Label\" (name: Text)\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> if x > 0 then (if x > 10 then name else \"small\") else \"neg\"\n" +
		"Cursed Energy: stdin\n" +
		"Shikigami: Ints\n" +
		"Shikigami: Label\n    name: \"big\"\n"
	v2, _ := runPipeline(t, textSrc, "50\n5\n-1")
	if got := ir.FormatValue(v2); got != "[big, small, neg]" {
		t.Fatalf("Label: got %s want [big, small, neg]", got)
	}
}

// Inlining terminates by cycle detection, not by a depth counter. A deeply
// composed but non-recursive chain is legal however deep it goes — the old
// fixed ceiling of 64 refused those too, for no reason.
func TestDeepNonRecursiveInliningIsAllowed(t *testing.T) {
	var src string
	const depth = 200
	for i := 0; i < depth; i++ {
		src += fmt.Sprintf("Shikigami \"Step %d\" : List<Int> -> List<Int>\n", i)
		if i == 0 {
			src += "    Cursed Technique: Map Each\n        Using: (x) -> x + 1\n"
		} else {
			src += fmt.Sprintf("    Shikigami: Step %d\n", i-1)
		}
	}
	src += "Cursed Energy: stdin\nShikigami: Ints\n" +
		fmt.Sprintf("Shikigami: Step %d\n", depth-1)
	v, _ := runPipeline(t, src, "1\n2")
	if got := ir.FormatValue(v); got != "[2, 3]" {
		t.Fatalf("deep chain: got %s", got)
	}
}

// Genuine self-reference is still refused — a Shikigami is inlined, so it has
// no finite expansion — and the message names the cycle and points at Explore
// rather than blaming an arbitrary depth.
func TestRecursiveShikigamiNamesTheCycle(t *testing.T) {
	src := "Shikigami \"Loop\" : List<Int> -> List<Int>\n" +
		"    Shikigami: Loop\n" +
		"Cursed Energy: stdin\nShikigami: Ints\nShikigami: Loop\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected a recursion error")
	}
	msg := err.Error()
	for _, want := range []string{"recursive", "Loop -> Loop", "Explore"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q, got: %s", want, msg)
		}
	}
}

// Mutual recursion is a cycle too, and the chain shows both names.
func TestMutuallyRecursiveShikigamiIsRefused(t *testing.T) {
	src := "Shikigami \"Ping\" : List<Int> -> List<Int>\n" +
		"    Shikigami: Pong\n" +
		"Shikigami \"Pong\" : List<Int> -> List<Int>\n" +
		"    Shikigami: Ping\n" +
		"Cursed Energy: stdin\nShikigami: Ints\nShikigami: Ping\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected a recursion error")
	}
	if msg := err.Error(); !strings.Contains(msg, "Ping -> Pong -> Ping") {
		t.Errorf("error should show the cycle chain, got: %s", msg)
	}
}
