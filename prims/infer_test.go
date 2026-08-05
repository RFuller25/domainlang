package prims

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/token"
	"domain/typecheck"
)

func parseSrc(t *testing.T, src string) *ast.Program {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

// inferLine runs one prefix-free line through inference and returns the
// keyword it was given. The line is placed second so the source-stage rule
// does not apply; source: true puts it first instead.
func inferLine(t *testing.T, line string, source bool) (string, error) {
	t.Helper()
	src := line + "\n"
	if !source {
		src = "Cursed Energy: stdin\n" + line + "\n"
	}
	prog := parseSrc(t, src)
	err := Infer(prog)
	return prog.Statements[len(prog.Statements)-1].Keyword, err
}

// TestInferKeywords pins the keyword recovered for a prefix-free line of each
// shape — the whole point of the feature.
func TestInferKeywords(t *testing.T) {
	cases := []struct {
		line, want string
	}{
		{`Split Text by "\n\n"`, "Cursed Technique"},
		{`Split Each by "\n"`, "Cursed Technique"},
		{`Split Fields`, "Cursed Technique"},
		{`Map Each`, "Cursed Technique"},
		{`Take Item 0`, "Cursed Technique"},
		{`Convert To Integers`, "Channeled Energy"},
		{`Convert Each List to Integers`, "Channeled Energy"},
		{`Convert To Sparse Grid`, "Channeled Energy"},
		{`Sum`, "Maximum Technique"},
		{`Sum Each Group`, "Maximum Technique"},
		{`Select Top 3, Sum`, "Maximum Technique"},
		{`Max By`, "Maximum Technique"},
		{`Quicksort, Descending`, "Domain Expansion"},
		{`Sort By`, "Domain Expansion"},
		{`BFS from 0 0`, "Domain Expansion"},
		{`Reverse`, "Reverse Cursed Technique"},
		{`Repeat 3`, "Simple Domain"},
		{`While`, "Simple Domain"},
		{`Iterate Until Fixed Point`, "Simple Domain"},
		{`Count Equals 3`, "Binding Vow"},
		{`All Values > 0`, "Binding Vow"},
		{`stdout`, "Reveal"},
		{`Top K Sum`, "Shikigami"}, // a prelude definition
	}
	for _, c := range cases {
		got, err := inferLine(t, c.line, false)
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: inferred %q, want %q", c.line, got, c.want)
		}
	}
}

// TestInferSource: the first statement of a program is the one place an
// unrecognized phrase reads as an input to read rather than an error.
func TestInferSource(t *testing.T) {
	for _, line := range []string{"stdin", "input.txt", "data/day1.txt", "16_no_prefixes.input"} {
		got, err := inferLine(t, line, true)
		if err != nil {
			t.Errorf("%s: %v", line, err)
			continue
		}
		if got != "Cursed Energy" {
			t.Errorf("%s: inferred %q, want Cursed Energy", line, got)
		}
	}
	// A path-shaped phrase is a source even when its words collide with a
	// primitive: `sum.txt` is a file, not the Sum reduction.
	got, err := inferLine(t, "sum.txt", true)
	if err != nil || got != "Cursed Energy" {
		t.Fatalf("sum.txt: inferred %q (%v), want Cursed Energy", got, err)
	}
	// Off the first line there is no source stage, so the same phrase fails.
	if _, err := inferLine(t, "day1.txt", false); err == nil {
		t.Fatal("a source target after the first stage should not infer")
	}
}

// TestInferUnknownPhrase: a phrase that names nothing is an error naming the
// phrase, not a silent misresolution.
func TestInferUnknownPhrase(t *testing.T) {
	_, err := inferLine(t, "Frobnicate Everything", false)
	if err == nil {
		t.Fatal("expected an error for an unknown phrase")
	}
	if !strings.Contains(err.Error(), "cannot infer a keyword") ||
		!strings.Contains(err.Error(), "Frobnicate Everything") {
		t.Fatalf("unhelpful message: %v", err)
	}
}

// TestInferIsIdempotentAndLeavesKeywordsAlone: a fully keyworded program must
// resolve through exactly the path it always did.
func TestInferIsIdempotentAndLeavesKeywordsAlone(t *testing.T) {
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Reveal: stdout\n"
	prog := parseSrc(t, src)
	before := []string{}
	for _, s := range prog.Statements {
		before = append(before, s.Keyword)
	}
	for i := 0; i < 2; i++ {
		if err := Infer(prog); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	for i, s := range prog.Statements {
		if s.Keyword != before[i] {
			t.Errorf("statement %d: keyword changed %q -> %q", i, before[i], s.Keyword)
		}
	}
}

// TestInferKeepsGoingAfterAFailure: one unnameable line must not cost the
// lines after it their keywords, or the linter misreads the rest of the file.
func TestInferKeepsGoingAfterAFailure(t *testing.T) {
	prog := parseSrc(t, "stdin\nFrobnicate\nSum\nstdout\n")
	if err := Infer(prog); err == nil {
		t.Fatal("expected the unnameable line to be reported")
	}
	want := []string{"Cursed Energy", "", "Maximum Technique", "Reveal"}
	for i, s := range prog.Statements {
		if s.Keyword != want[i] {
			t.Errorf("statement %d: keyword %q, want %q", i, s.Keyword, want[i])
		}
	}
}

// TestNoCrossKeywordAmbiguity walks every documented primitive phrase and
// checks it names exactly one keyword. This is the guard on registering a new
// primitive: matchers overlapping across keywords would make prefix-free lines
// ambiguous, and this test fails the day that happens.
func TestNoCrossKeywordAmbiguity(t *testing.T) {
	for _, p := range Registry {
		if catchAllKeywords[p.Keyword] {
			continue // matched by shape, not by the registry scan
		}
		// A primitive whose ID is not itself writable lists the phrases that
		// are; every one of them has to name it just as unambiguously.
		for _, phrase := range p.Spellings() {
			src := "Cursed Technique: " + phrase + "\n"
			if _, isLang := ast.ForeignLanguage(phrase); isLang {
				src += "    a block\n" // a language name does not parse without one
			}
			prog := parseSrc(t, src)
			op := prog.Statements[0].Op
			prim, err := inferPrimitive(op, op.Pos)
			if err != nil {
				t.Errorf("%s (%q): %v", p.ID, phrase, err)
				continue
			}
			if prim == nil {
				t.Errorf("%s: no primitive matches its phrase %q", p.ID, phrase)
				continue
			}
			if prim.ID != p.ID {
				t.Errorf("%s: prefix-free %q resolves to %q instead", p.ID, phrase, prim.ID)
			}
		}
	}
}

// TestAmbiguousPhraseIsAnError pins the message a future cross-keyword clash
// would produce, using two primitives registered under different keywords that
// both accept the phrase.
func TestAmbiguousPhraseIsAnError(t *testing.T) {
	clash := &Primitive{
		ID: "Clashing Sum", Keyword: "Cursed Technique",
		Match: func(op *ast.Operation) bool { return hasWord(op, "Sum") },
		Build: func(*ast.Operation, ArgSet, *ir.Type, token.Position) (*ir.Node, error) { return nil, nil },
	}
	saved := Registry
	Registry = append(append([]*Primitive{}, Registry...), clash)
	defer func() { Registry = saved }()

	_, err := inferLine(t, "Sum", false)
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{"ambiguous", "Maximum Technique: Sum", "Cursed Technique: Clashing Sum"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q should mention %q", err.Error(), want)
		}
	}
}

// ---------------------------------------------------------------------------
// Shikigami naming

func TestShikigamiMayNotBeNamedAfterABuiltin(t *testing.T) {
	bad := []string{
		"Sum",              // a primitive ID
		"sum",              // ... case-insensitively
		"Quicksort",        // a spelling its matcher accepts
		"Sort By",          // every word load-bearing
		"Convert To Grid",  // a multi-word ID
		"Cursed Technique", // a themed keyword
		"Reveal",           // ... including the single-word ones
		"gcd",              // an expression-layer builtin
		"Repeat",           // a loop kind
		"All Values",       // a vow shape
		"stdout",           // the sink
		"stdin",            // a source
	}
	for _, name := range bad {
		src := "Shikigami \"" + name + "\"\n    Maximum Technique: Sum\n"
		prog := parseSrc(t, src)
		err := Infer(prog)
		if err == nil {
			t.Errorf("%q should be rejected as a Shikigami name", name)
			continue
		}
		if !strings.Contains(err.Error(), "is named after") {
			t.Errorf("%q: unexpected error %v", name, err)
		}
	}
}

// TestShikigamiNamesThatMerelyMentionABuiltin: the rule reserves the names of
// built-ins, not every phrase containing one of their words. Primitive
// matchers look for words anywhere in a phrase, so without the load-bearing
// test these ordinary names would all be taken away.
func TestShikigamiNamesThatMerelyMentionABuiltin(t *testing.T) {
	ok := []string{"Scaled Sum", "Sort Text", "Top K Sum", "Digit Grid", "Heavy Elves", "Lines"}
	for _, name := range ok {
		src := "Shikigami \"" + name + "\"\n    Maximum Technique: Sum\n"
		prog := parseSrc(t, src)
		if err := Infer(prog); err != nil {
			t.Errorf("%q should be a legal Shikigami name: %v", name, err)
		}
	}
}

// TestPreludeNamesAreLegal: the standard library must obey its own rule.
func TestPreludeNamesAreLegal(t *testing.T) {
	defs, err := preludeDefs()
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range defs {
		if err := checkShikigamiName(def); err != nil {
			t.Errorf("prelude: %v", err)
		}
	}
}

// TestReservedNamesCoversTheVocabulary sanity-checks the list diagnostics show.
func TestReservedNamesCoversTheVocabulary(t *testing.T) {
	got := map[string]bool{}
	for _, n := range ReservedNames() {
		got[n] = true
	}
	for _, p := range Registry {
		if !got[p.ID] {
			t.Errorf("reserved names omit the primitive %q", p.ID)
		}
	}
	for _, k := range ast.Keywords {
		if !got[k] {
			t.Errorf("reserved names omit the keyword %q", k)
		}
	}
	for _, b := range typecheck.Builtins {
		if !got[b] {
			t.Errorf("reserved names omit the builtin %q", b)
		}
	}
}

// TestKeywordListCoversTheRegistry keeps ast.Keywords — which the parser uses
// to tell a forgotten colon from a prefix-free phrase — in step with the
// keywords primitives are actually registered under.
func TestKeywordListCoversTheRegistry(t *testing.T) {
	known := map[string]bool{}
	for _, k := range ast.Keywords {
		known[k] = true
	}
	for _, p := range Registry {
		if !known[p.Keyword] {
			t.Errorf("ast.Keywords is missing %q (registered by %q)", p.Keyword, p.ID)
		}
	}
}

// TestKeywordInferredFlag: the AST records which spelling the source used, so
// tools that speak back to the user (lint advice, a formatter) can match it.
func TestKeywordInferredFlag(t *testing.T) {
	prog := parseSrc(t, "Cursed Energy: stdin\nSum\n")
	if err := Infer(prog); err != nil {
		t.Fatal(err)
	}
	if prog.Statements[0].KeywordInferred {
		t.Error("a written keyword must not be marked inferred")
	}
	if !prog.Statements[1].KeywordInferred {
		t.Error("a prefix-free line must be marked inferred")
	}
}
