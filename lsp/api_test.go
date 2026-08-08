package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These paths existed before, reachable only by speaking JSON-RPC at them.
// Lifting them out of the protocol is what makes this file possible at all.

const apiProgram = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Reveal: stdout
`

func TestAnalyzeTypeHintsFollowThePipeline(t *testing.T) {
	a := Analyze("prog.domain", apiProgram)

	byLine := map[int]string{}
	for _, h := range a.TypeHints() {
		byLine[h.Line] = h.Label
	}
	want := map[int]string{
		1: ": Text",
		2: ": List<Text>",
		3: ": List<List<Text>>",
		4: ": List<List<Int>>",
		5: ": List<Int>",
	}
	for line, label := range want {
		if got := byLine[line]; got != label {
			t.Errorf("line %d: hint %q, want %q", line, got, label)
		}
	}
}

// A program that does not resolve still answers for the prefix that did. This
// is the state a program spends almost all of its life in, and the reason the
// hints are useful while it is being written rather than only once it is done.
func TestAnalyzeIsFailureTolerant(t *testing.T) {
	const broken = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Maximum Technique: Nonsense Operation
`
	a := Analyze("prog.domain", broken)
	hints := a.TypeHints()
	if len(hints) == 0 {
		t.Fatal("a broken program gave up entirely")
	}
	byLine := map[int]string{}
	for _, h := range hints {
		byLine[h.Line] = h.Label
	}
	if byLine[1] != ": Text" || byLine[2] != ": List<Text>" {
		t.Errorf("the resolved prefix lost its hints: %v", byLine)
	}
}

// A Binding Vow never changes the value, so a hint on it would repeat the line
// above.
func TestTypeHintsSkipVows(t *testing.T) {
	const src = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Binding Vow: Count Equals 3
`
	for _, h := range Analyze("p.domain", src).TypeHints() {
		if h.Line == 3 {
			t.Errorf("a vow got a hint: %q", h.Label)
		}
	}
}

func TestInspectLineDescribesAPrimitive(t *testing.T) {
	a := Analyze("prog.domain", apiProgram)
	ins, ok := a.InspectLine(2)
	if !ok {
		t.Fatal("nothing to inspect on the Split line")
	}
	if !strings.Contains(ins.Title, "Split") {
		t.Errorf("title is %q", ins.Title)
	}
	if ins.Signature == "" || ins.Summary == "" || ins.DocAnchor == "" {
		t.Errorf("incomplete catalog entry: %+v", ins)
	}
	// The concrete step, which the declared signature's type variables hide.
	if ins.TypeStep != "Text → List<Text>" {
		t.Errorf("type step is %q", ins.TypeStep)
	}
}

// Inspecting works even when the program does not type-check — the moment you
// most want to know what an operation does is while its line is still wrong.
func TestInspectLineWorksOnABrokenProgram(t *testing.T) {
	const broken = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Maximum Technique: Nonsense Operation
`
	if _, ok := Analyze("p.domain", broken).InspectLine(2); !ok {
		t.Error("a good line lost its documentation because a later line is wrong")
	}
}

func TestInspectLineDescribesAShikigamiDefinition(t *testing.T) {
	const src = `Shikigami "Double"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
Cursed Energy: input.txt
Channeled Energy: Convert To Integers
Shikigami: Double
`
	a := Analyze("p.domain", src)
	ins, ok := a.InspectLine(1)
	if !ok {
		t.Fatal("the definition header described nothing")
	}
	if !strings.Contains(ins.Title, "Double") {
		t.Errorf("title is %q", ins.Title)
	}
}

func TestDefinitionAtFindsALocalShikigami(t *testing.T) {
	const src = `Shikigami "Double"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
Cursed Energy: input.txt
Channeled Energy: Convert To Integers
Shikigami: Double
Reveal: stdout
`
	a := Analyze("p.domain", src)
	loc, ok := a.DefinitionAt(6)
	if !ok {
		t.Fatal("the call did not resolve to a definition")
	}
	if loc.Name != "Double" || loc.Pos.Line != 1 || loc.Origin != "local" {
		t.Errorf("got %+v", loc)
	}
}

// A prelude name is real and has nowhere to jump to. Reporting that is
// different from reporting nothing, which reads as "no such definition".
func TestDefinitionAtReportsPreludeNames(t *testing.T) {
	const src = `Cursed Energy: input.txt
Shikigami: Ints
Reveal: stdout
`
	a := Analyze("p.domain", src)
	loc, ok := a.DefinitionAt(2)
	if !ok {
		t.Fatal("a prelude Shikigami should be reported, not silently missing")
	}
	if loc.Origin != "prelude" || loc.Name != "Ints" {
		t.Errorf("got %+v", loc)
	}
	if loc.Path != "" {
		t.Errorf("a prelude definition has no file, got %q", loc.Path)
	}
}

func TestDefinitionAtIgnoresLinesThatAreNotCalls(t *testing.T) {
	a := Analyze("p.domain", apiProgram)
	if _, ok := a.DefinitionAt(2); ok {
		t.Error("a Cursed Technique line is not a Shikigami call")
	}
}

// An imported name follows into the library file the resolver actually loaded.
func TestDefinitionAtFollowsAnImport(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "helpers.domain")
	if err := os.WriteFile(lib, []byte("Shikigami \"Triple\"\n    Cursed Technique: Map Each\n        Using: (x) -> x * 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "main.domain")
	src := "Innate Domain: helpers\nCursed Energy: input.txt\nChanneled Energy: Convert To Integers\nShikigami: Triple\nReveal: stdout\n"

	a := Analyze(prog, src)
	loc, ok := a.DefinitionAt(4)
	if !ok {
		t.Fatal("the imported call did not resolve")
	}
	if loc.Origin != "import" {
		t.Errorf("origin is %q, want import", loc.Origin)
	}
	if loc.Path != lib {
		t.Errorf("path is %q, want %q", loc.Path, lib)
	}
	if loc.Pos.Line != 1 {
		t.Errorf("line is %d, want 1", loc.Pos.Line)
	}
}

// Analyze must tolerate an empty or unnamed buffer — the state an editor is in
// before anything has been typed or saved.
func TestAnalyzeToleratesAnEmptyBuffer(t *testing.T) {
	a := Analyze("", "")
	if a == nil {
		t.Fatal("nil analysis")
	}
	if hints := a.TypeHints(); len(hints) != 0 {
		t.Errorf("an empty buffer produced hints: %v", hints)
	}
	if _, ok := a.InspectLine(1); ok {
		t.Error("an empty buffer described something")
	}
	if _, ok := a.DefinitionAt(1); ok {
		t.Error("an empty buffer defined something")
	}
}
