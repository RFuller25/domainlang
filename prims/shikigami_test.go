package prims

import (
	"strings"
	"testing"
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
			"type error inside body",
			// Top K Sum expects List<Int>, but Text is supplied.
			"Cursed Energy: stdin\nShikigami: Top K Sum\n    k: 3\n",
			"in Shikigami \"Top K Sum\"",
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

	// Calling the prelude's Top K Sum over Text fails inside the embedded
	// prelude source — the trace must say so.
	preludeCall := "Cursed Energy: stdin\nShikigami: Top K Sum\n    k: 3\n"
	_, err = resolveSrc(t, preludeCall)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	msg = err.Error()
	if !strings.HasPrefix(msg, "2:") {
		t.Fatalf("outer position should be the call site (line 2), got: %q", msg)
	}
	if !strings.Contains(msg, `in Shikigami "Top K Sum" (prelude source `) {
		t.Fatalf("prelude body errors should be labeled, got: %q", msg)
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
