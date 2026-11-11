package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func explainSource(t *testing.T, src string) *explainReport {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prog.domain")
	writeFile(t, path, src)
	rep, err := buildExplain(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func stageAt(t *testing.T, rep *explainReport, line int) explainStage {
	t.Helper()
	for _, st := range rep.Stages {
		if st.Line == line {
			return st
		}
	}
	t.Fatalf("no stage on line %d", line)
	return explainStage{}
}

// Each stage carries the statement, the catalog's sentence and the types — and
// a rewrite lands on the lines it happened to.
func TestExplainPutsRewritesOnTheirLines(t *testing.T) {
	rep := explainSource(t, `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`)
	if len(rep.Stages) != 6 {
		t.Fatalf("%d stages, want 6", len(rep.Stages))
	}
	split := stageAt(t, rep, 2)
	if split.Source != `Cursed Technique: Split Text by "\n"` || split.In != "Text" || split.Out != "List<Text>" ||
		!strings.HasPrefix(split.What, "Splits text") || split.Optimized != "" {
		t.Errorf("split stage: %+v", split)
	}
	if src := stageAt(t, rep, 1); src.In != "" || src.Out != "Text" {
		t.Errorf("a source stage takes nothing in: %+v", src)
	}
	// Sort + Select Top K fuse into a quickselect: the sort's line runs as
	// something else, and the selection is gone.
	if sort := stageAt(t, rep, 4); !strings.Contains(sort.Optimized, "runs as") || !strings.Contains(sort.Optimized, "Quickselect") {
		t.Errorf("sort stage: optimized = %q", sort.Optimized)
	}
	if top := stageAt(t, rep, 5); top.Optimized != "absorbed into another stage" {
		t.Errorf("select stage: optimized = %q", top.Optimized)
	}
	if len(rep.Rewrites) != 1 || !strings.Contains(rep.Rewrites[0], "Quickselect") {
		t.Errorf("rewrites: %v", rep.Rewrites)
	}
}

// A statement's argument lines stay with it, a body's stages nest under it,
// and a structural statement gets a sentence although the catalog has none.
func TestExplainBodiesAndArguments(t *testing.T) {
	rep := explainSource(t, `Cursed Energy: stdin
Shikigami: Ints
Maximum Technique: Sum
Simple Domain: While
    Using: (v) -> v > 100
    Cursed Technique: Apply
        Using: (v) -> v - 30
Reveal: stdout
`)
	loop := stageAt(t, rep, 4)
	if loop.Depth != 0 || len(loop.Args) != 1 || loop.Args[0] != "Using: (v) -> v > 100" ||
		!strings.Contains(loop.What, "while it holds") {
		t.Errorf("loop stage: %+v", loop)
	}
	body := stageAt(t, rep, 6)
	if body.Depth != 1 || len(body.Args) != 1 || body.Args[0] != "Using: (v) -> v - 30" {
		t.Errorf("body stage: %+v", body)
	}
	if len(rep.Rewrites) != 0 {
		t.Errorf("rewrites: %v", rep.Rewrites)
	}
}

// A Shikigami call leaves no node of its own; it still gets a heading, with
// its inlined body under it — from the prelude, and from the same file.
func TestExplainShikigamiCalls(t *testing.T) {
	rep := explainSource(t, `Shikigami "Scaled Sum" (factor: Int)
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (v) -> v * factor

Cursed Energy: stdin
Shikigami: Ints
Shikigami: Scaled Sum
    factor: 7
Reveal: stdout
`)
	var got []string
	for _, st := range rep.Stages {
		got = append(got, strings.Repeat(">", st.Depth)+st.Stage)
	}
	want := []string{
		"Read Source <- stdin",
		"Shikigami: Ints",
		`>Split by "\n"`,
		">Convert List to Integers",
		"Shikigami: Scaled Sum",
		">Sum",
		">Apply",
		"Reveal -> stdout",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stages:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	ints := stageAt(t, rep, 7)
	if ints.In != "Text" || ints.Out != "List<Int>" || !strings.Contains(ints.What, "2 stage(s)") {
		t.Errorf("prelude call: %+v", ints)
	}
	// A prelude stage's line is a coordinate in another file, so it is not shown.
	if st := rep.Stages[2]; st.Line != 0 || st.From == "" {
		t.Errorf("inlined prelude stage: %+v", st)
	}
	if call := stageAt(t, rep, 8); len(call.Args) != 1 || call.Args[0] != "factor: 7" || call.Out != "Int" {
		t.Errorf("local call: %+v", call)
	}
	// The local Shikigami's stages point at its definition.
	if st := rep.Stages[5]; st.Line != 2 {
		t.Errorf("inlined local stage: %+v", st)
	}
}

func TestExplainCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prog.domain")
	writeFile(t, path, "Cursed Energy: stdin\nMaximum Technique: Count Characters\nReveal: stdout\n")

	var out, errOut bytes.Buffer
	if code := Expansion([]string{"explain"}, []string{path}, nil, &out, &errOut); code != 1 {
		t.Errorf("a program that does not resolve: exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "has to resolve before it can be explained") {
		t.Errorf("stderr: %s", errOut.String())
	}

	writeFile(t, path, "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nMaximum Technique: Count\nReveal: stdout\n")
	out.Reset()
	errOut.Reset()
	if code := Expansion([]string{"explain"}, []string{path, "--plain"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{
		"4 stage(s)",
		`   2 Cursed Technique: Split Text by ","`,
		"Text → List<Text>",
		"The optimizer runs this program as written.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output is missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if code := Expansion([]string{"explain"}, []string{path, "--json"}, nil, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep explainReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || len(rep.Stages) != 4 {
		t.Errorf("--json: %v, %d stages\n%s", err, len(rep.Stages), out.String())
	}

	if code := Expansion([]string{"explain"}, []string{}, nil, &out, &errOut); code != 2 {
		t.Errorf("no file: exit %d, want 2", code)
	}
}

// Every shipped program can be explained: none of the nesting forms the
// examples use — channels, parts, loops, block lambdas, games — breaks the walk.
func TestExplainEveryExample(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../../examples/*.domain", "../../challenges/*.domain", "../../examples/games/*.domain"} {
		m, _ := filepath.Glob(pattern)
		files = append(files, m...)
	}
	if len(files) < 30 {
		t.Fatalf("found only %d programs", len(files))
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rep, err := buildExplain(f, src)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		var out bytes.Buffer
		rep.write(&out, false)
		for _, st := range rep.Stages {
			if st.What == "" || st.Out == "" {
				t.Errorf("%s: a stage with nothing to say: %+v", f, st)
			}
		}
	}
}
