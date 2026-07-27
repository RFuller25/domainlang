package prims

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
)

// interpretPipeline runs a resolved pipeline against stdin and returns stdout.
func interpretPipeline(t *testing.T, pipe *ir.Pipeline, stdin string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	ctx := &ir.Context{Stdin: strings.NewReader(stdin), Stdout: &out}
	if _, err := interp.Run(pipe, ctx); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// lib writes a library file into dir and returns dir.
func lib(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name+".domain")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// resolveIn resolves src as if it were a program file sitting in dir.
func resolveIn(t *testing.T, dir, src string, search ...string) error {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		return err
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return err
	}
	_, err = ResolveWith(prog, ResolveOptions{BaseDir: dir, Search: search})
	return err
}

// runIn resolves and interprets src as a program file in dir, returning stdout.
func runIn(t *testing.T, dir, src, stdin string, search ...string) (string, error) {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		return "", err
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return "", err
	}
	pipe, err := ResolveWith(prog, ResolveOptions{BaseDir: dir, Search: search})
	if err != nil {
		return "", err
	}
	return interpretPipeline(t, pipe, stdin)
}

const shapesLib = `Shikigami "Doubled"
    Cursed Technique: Map Each
        Using: (x) -> x * 2

Shikigami "Scaled Sum" (by: Int)
    Cursed Technique: Map Each
        Using: (x) -> x * by
    Maximum Technique: Sum
`

func TestImportFromSiblingDirectory(t *testing.T) {
	dir := lib(t, t.TempDir(), "shapes", shapesLib)
	src := `Innate Domain: shapes
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Doubled
Reveal: stdout
`
	got, err := runIn(t, dir, src, "1,2,3")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[2, 4, 6]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// An imported Shikigami is callable by its bare name, like any other — which
// means keyword inference has to know the imported names exist.
func TestImportedShikigamiCallableWithoutKeyword(t *testing.T) {
	dir := lib(t, t.TempDir(), "shapes", shapesLib)
	src := `Innate Domain: shapes
stdin
Split Text by ","
Convert List to Integers
Doubled
stdout
`
	got, err := runIn(t, dir, src, "5,6")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[10, 12]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestImportedShikigamiTakesParameters(t *testing.T) {
	dir := lib(t, t.TempDir(), "shapes", shapesLib)
	src := `Innate Domain: shapes
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Scaled Sum
    by: 10
Reveal: stdout
`
	got, err := runIn(t, dir, src, "1,2,3")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "60\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// An import is a declaration, so its position in the file does not matter.
func TestImportIsHoisted(t *testing.T) {
	dir := lib(t, t.TempDir(), "shapes", shapesLib)
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Doubled
Innate Domain: shapes
Reveal: stdout
`
	got, err := runIn(t, dir, src, "4")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[8]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestImportFromSearchPath(t *testing.T) {
	progDir := t.TempDir()
	libDir := lib(t, t.TempDir(), "shapes", shapesLib)
	src := `Innate Domain: shapes
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Doubled
Reveal: stdout
`
	if _, err := runIn(t, progDir, src, "1", libDir); err != nil {
		t.Fatalf("search path import failed: %v", err)
	}
	// Without the search entry it must fail, naming where it looked.
	err := resolveIn(t, progDir, src)
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	if !strings.Contains(err.Error(), "cannot find the library") || !strings.Contains(err.Error(), "looked in") {
		t.Errorf("error = %q, want a not-found error listing the directories searched", err.Error())
	}
}

// The importing file's own directory wins over the search path.
func TestImportPrefersSiblingOverSearchPath(t *testing.T) {
	progDir := lib(t, t.TempDir(), "shapes", `Shikigami "Doubled"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
`)
	other := lib(t, t.TempDir(), "shapes", `Shikigami "Doubled"
    Cursed Technique: Map Each
        Using: (x) -> x * 100
`)
	src := `Innate Domain: shapes
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Doubled
Reveal: stdout
`
	got, err := runIn(t, progDir, src, "3", other)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[6]\n"; got != want {
		t.Errorf("output = %q, want %q (the sibling library must win)", got, want)
	}
}

func TestImportSubdirectoryTarget(t *testing.T) {
	dir := t.TempDir()
	lib(t, filepath.Join(dir, "grids"), "hex", `Shikigami "Doubled"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
`)
	src := `Innate Domain: grids/hex
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Doubled
Reveal: stdout
`
	if _, err := runIn(t, dir, src, "1"); err != nil {
		t.Fatalf("subdirectory import failed: %v", err)
	}
}

func TestTransitiveImport(t *testing.T) {
	dir := t.TempDir()
	lib(t, dir, "base", `Shikigami "Doubled"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
`)
	lib(t, dir, "mid", `Innate Domain: base
Shikigami "Quadrupled"
    Shikigami: Doubled
    Shikigami: Doubled
`)
	src := `Innate Domain: mid
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Quadrupled
Reveal: stdout
`
	got, err := runIn(t, dir, src, "1,2")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[4, 8]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// A diamond loads each library once; loading twice would let a dependency
// shadow the dependent that pulled it in.
func TestDiamondImportLoadsOnce(t *testing.T) {
	dir := t.TempDir()
	lib(t, dir, "base", `Shikigami "Doubled"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
`)
	lib(t, dir, "left", "Innate Domain: base\n"+`Shikigami "Left"
    Shikigami: Doubled
`)
	lib(t, dir, "right", "Innate Domain: base\n"+`Shikigami "Right"
    Shikigami: Doubled
`)
	src := `Innate Domain: left
Innate Domain: right
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Left
Shikigami: Right
Reveal: stdout
`
	got, err := runIn(t, dir, src, "1")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[4]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestImportCycleReportsTheChain(t *testing.T) {
	dir := t.TempDir()
	lib(t, dir, "a", "Innate Domain: b\n"+`Shikigami "A"
    Maximum Technique: Sum
`)
	lib(t, dir, "b", "Innate Domain: a\n"+`Shikigami "B"
    Maximum Technique: Sum
`)
	err := resolveIn(t, dir, "Innate Domain: a\nCursed Energy: stdin\nReveal: stdout\n")
	if err == nil {
		t.Fatal("expected a cycle error")
	}
	if !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("error = %q, want an import cycle", err.Error())
	}
	for _, want := range []string{"a.domain", "b.domain"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("cycle error should name %s: %q", want, err.Error())
		}
	}
}

// A library that imports itself is the degenerate cycle.
func TestSelfImportIsACycle(t *testing.T) {
	dir := t.TempDir()
	lib(t, dir, "solo", "Innate Domain: solo\n"+`Shikigami "S"
    Maximum Technique: Sum
`)
	err := resolveIn(t, dir, "Innate Domain: solo\nCursed Energy: stdin\nReveal: stdout\n")
	if err == nil || !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("error = %v, want an import cycle", err)
	}
}

func TestLibraryMayNotContainStatements(t *testing.T) {
	dir := lib(t, t.TempDir(), "bad", `Shikigami "Fine"
    Maximum Technique: Sum
Cursed Energy: stdin
Reveal: stdout
`)
	err := resolveIn(t, dir, "Innate Domain: bad\nCursed Energy: stdin\nReveal: stdout\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "only Shikigami definitions") {
		t.Errorf("error = %q, want the library-shape error", err.Error())
	}
}

// Shadowing order: prelude < import < the program's own definition.
func TestShadowingPrecedence(t *testing.T) {
	dir := lib(t, t.TempDir(), "over", `Shikigami "Lines"
    Cursed Technique: Split Text by ";"
`)
	// The import shadows the prelude's Lines (which splits on "\n").
	src := `Innate Domain: over
Cursed Energy: stdin
Shikigami: Lines
Reveal: stdout
`
	got, err := runIn(t, dir, src, "a;b")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[a, b]\n"; got != want {
		t.Errorf("import should shadow the prelude: got %q, want %q", got, want)
	}

	// A local definition shadows the import in turn.
	local := `Innate Domain: over
Shikigami "Lines"
    Cursed Technique: Split Text by "|"
Cursed Energy: stdin
Shikigami: Lines
Reveal: stdout
`
	got, err = runIn(t, dir, local, "a|b")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[a, b]\n"; got != want {
		t.Errorf("local definition should shadow the import: got %q, want %q", got, want)
	}
}

// The reserved-name rule applies to imported definitions too, reported at the
// import (a position the user's file actually has) and naming the library.
func TestImportedReservedNameIsRejected(t *testing.T) {
	dir := lib(t, t.TempDir(), "bad", `Shikigami "Sum"
    Maximum Technique: Count
`)
	err := resolveIn(t, dir, "Innate Domain: bad\nCursed Energy: stdin\nReveal: stdout\n")
	if err == nil {
		t.Fatal("expected a reserved-name error")
	}
	for _, want := range []string{"bad.domain", "named after"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
	if !strings.HasPrefix(err.Error(), "1:1") {
		t.Errorf("error should be positioned at the import statement, got %q", err.Error())
	}
}

// An error inside an imported body names the library, so its line and column are
// never mistaken for coordinates in the user's own file.
func TestErrorInsideImportedBodyNamesTheLibrary(t *testing.T) {
	dir := lib(t, t.TempDir(), "shapes", `Shikigami "Broken"
    Maximum Technique: Sum
`)
	// Sum needs List<Int>; the pipeline hands it Text.
	src := `Innate Domain: shapes
Cursed Energy: stdin
Shikigami: Broken
Reveal: stdout
`
	err := resolveIn(t, dir, src)
	if err == nil {
		t.Fatal("expected a type error")
	}
	if !strings.Contains(err.Error(), "shapes.domain:2:") {
		t.Errorf("error = %q, want it to locate the failure in shapes.domain", err.Error())
	}
}

// Plain Resolve has no file context, so it must say so rather than silently
// ignoring an import.
func TestResolveWithoutFileContextRejectsImports(t *testing.T) {
	src := "Innate Domain: shapes\nCursed Energy: stdin\nReveal: stdout\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(prog); err == nil {
		t.Fatal("expected an error without file context")
	} else if !strings.Contains(err.Error(), "file context") {
		t.Errorf("error = %q, want it to mention the missing file context", err.Error())
	}
}

func TestImportParseErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"no target", "Innate Domain:\nCursed Energy: stdin\n", "needs a library name"},
		{"with a block", "Innate Domain: x\n    Using: (a) -> a\n", "takes no arguments"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, err := lexer.Lex(c.src)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parser.Parse(c.src, toks); err == nil {
				t.Fatalf("expected a parse error containing %q", c.want)
			} else if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.want)
			}
		})
	}
}

// The loader reads through ResolveOptions.ReadFile, so a host with unsaved
// buffers (the language server) can resolve imports without touching disk.
func TestImportsUseTheInjectedReader(t *testing.T) {
	files := map[string]string{
		filepath.Join("/virtual", "shapes.domain"): `Shikigami "Doubled"
    Cursed Technique: Map Each
        Using: (x) -> x * 2
`,
	}
	src := `Innate Domain: shapes
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Doubled
Reveal: stdout
`
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) ([]byte, error) {
		if content, ok := files[path]; ok {
			return []byte(content), nil
		}
		return nil, os.ErrNotExist
	}
	pipe, err := ResolveWith(prog, ResolveOptions{BaseDir: "/virtual", ReadFile: read})
	if err != nil {
		t.Fatalf("resolve with injected reader: %v", err)
	}
	got, err := interpretPipeline(t, pipe, "2,3")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "[4, 6]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// Sites is how the language server learns which file answered an import.
func TestSitesReportsDefinitionOrigins(t *testing.T) {
	dir := lib(t, t.TempDir(), "shapes", shapesLib)
	src := `Innate Domain: shapes
Shikigami "Mine"
    Maximum Technique: Sum
Cursed Energy: stdin
Reveal: stdout
`
	toks, _ := lexer.Lex(src)
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	sites := map[string]DefSite{}
	if _, err := ResolveWith(prog, ResolveOptions{BaseDir: dir, Sites: sites}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := sites["Doubled"]; got.Origin != "import" || !strings.HasSuffix(got.Path, "shapes.domain") {
		t.Errorf("Doubled site = %+v, want an import of shapes.domain", got)
	}
	if got := sites["Mine"]; got.Origin != "local" {
		t.Errorf("Mine site = %+v, want local", got)
	}
	if got := sites["Lines"]; got.Origin != "prelude" {
		t.Errorf("Lines site = %+v, want prelude", got)
	}
}

// Imports are inlined before the optimizer runs, so an imported Shikigami gets
// every rewrite a local one would — the property that makes libraries free.
func TestOptimizerFiresThroughImportedShikigami(t *testing.T) {
	dir := lib(t, t.TempDir(), "aoc", `Shikigami "Top Two Sum"
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top 2, Sum
`)
	src := `Innate Domain: aoc
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Top Two Sum
Reveal: stdout
`
	toks, _ := lexer.Lex(src)
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := ResolveWith(prog, ResolveOptions{BaseDir: dir})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// The inlined body is plain nodes at the top level, so the length-changing
	// quickselect fusion applies.
	if !hasPrims(pipe.Nodes, "Sort", "SelectTopK") {
		t.Fatalf("expected the naive pair before optimization, got %v", primNames(pipe.Nodes))
	}
	rewrites := optimizer.Optimize(pipe, true)
	if !hasPrims(pipe.Nodes, "PartialSelect") {
		t.Errorf("quickselect did not fire through the import: %v (rewrites %v)",
			primNames(pipe.Nodes), rewrites)
	}
}

func primNames(nodes []*ir.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Prim
	}
	return out
}

func hasPrims(nodes []*ir.Node, want ...string) bool {
	have := map[string]bool{}
	for _, n := range nodes {
		have[n.Prim] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

// An inlined body's nodes carry positions from the *definition's* file, and
// token.Position holds no file to disambiguate them. Nodes from the prelude or
// a library are therefore marked, so tooling that maps a node back to a source
// line — the visualizer's source pane is the first — knows not to point at a
// line of the user's program that has nothing to do with the step.
func TestInlinedForeignNodesAreMarked(t *testing.T) {
	dir := lib(t, t.TempDir(), "aoc", `Shikigami "Halve"
    Cursed Technique: Map Each
        Using: (x) -> x / 2
`)
	src := `Innate Domain: aoc
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Halve
Reveal: stdout
`
	toks, _ := lexer.Lex(src)
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := ResolveWith(prog, ResolveOptions{BaseDir: dir})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	var marked, unmarked []string
	for _, n := range pipe.Nodes {
		if where, foreign := n.Foreign(); foreign {
			marked = append(marked, n.Prim)
			if where != "aoc.domain" {
				t.Errorf("%s is marked %q, want the library file it came from", n.Prim, where)
			}
			continue
		}
		unmarked = append(unmarked, n.Prim)
	}
	if len(marked) == 0 {
		t.Fatalf("the imported body's nodes should be marked, got %v", unmarked)
	}
	// Only the imported body: everything the user wrote keeps real coordinates.
	for _, prim := range unmarked {
		if prim == "Map" {
			t.Errorf("the imported Map Each should have been marked")
		}
	}
	if len(unmarked) == 0 {
		t.Error("the user's own statements should not be marked foreign")
	}
}

// A Shikigami the user defined in their own file is inlined too, but its
// positions *are* real coordinates in the file they are reading — so marking
// them would send tooling to the wrong answer just as surely.
func TestInlinedLocalNodesAreNotMarked(t *testing.T) {
	src := `Shikigami "Halve"
    Cursed Technique: Map Each
        Using: (x) -> x / 2
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert List to Integers
Shikigami: Halve
Reveal: stdout
`
	toks, _ := lexer.Lex(src)
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, n := range pipe.Nodes {
		if where, foreign := n.Foreign(); foreign {
			t.Errorf("%s came from the user's own file but is marked %q", n.Prim, where)
		}
	}
}
