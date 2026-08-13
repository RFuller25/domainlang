package prims

import (
	"os"
	"path/filepath"
	"testing"

	"domain/lexer"
	"domain/parser"
)

func usageOf(t *testing.T, src string) *Usage {
	t.Helper()
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pipe, err := Resolve(prog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return Used(pipe)
}

func TestUsedCountsPrimsBuiltinsAndKeywords(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (x) -> gcd(x, 12) + abs(x)
Maximum Technique: Sum
Reveal: stdout
`
	u := usageOf(t, src)

	for _, want := range []string{"Read Source", "Split", "Convert To Integers", "Map Each", "Sum"} {
		if u.Prims[want] == 0 {
			t.Errorf("primitive %q not counted; got %v", want, u.Prims)
		}
	}
	// Both builtins in the one lambda body, each once.
	if got := u.Builtins["gcd"]; got != 1 {
		t.Errorf("gcd counted %d times, want 1", got)
	}
	if got := u.Builtins["abs"]; got != 1 {
		t.Errorf("abs counted %d times, want 1", got)
	}
	// A builtin nobody called must not appear.
	if _, ok := u.Builtins["modpow"]; ok {
		t.Errorf("modpow was never called but is counted")
	}
	for _, want := range []string{"Cursed Energy", "Cursed Technique", "Maximum Technique"} {
		if u.Keywords[want] == 0 {
			t.Errorf("keyword %q not counted; got %v", want, u.Keywords)
		}
	}
}

// A primitive inside a loop body is written, so it is used. The walk has to
// descend into sub-pipelines or a coverage report would credit a program with
// less vocabulary than it spent.
func TestUsedDescendsIntoNestedPipelines(t *testing.T) {
	src := `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Simple Domain: Repeat 3
    Domain Expansion: Sort Ascending
    Reverse Cursed Technique: Reverse
Maximum Technique: Sum
Reveal: stdout
`
	u := usageOf(t, src)
	if u.Prims["Sort"] == 0 {
		t.Errorf("Sort inside a loop body not counted; got %v", u.Prims)
	}
	if u.Prims["Reverse"] == 0 {
		t.Errorf("Reverse inside a loop body not counted; got %v", u.Prims)
	}
}

// A Shikigami call is a CallExpr like a builtin call, and is not a builtin.
// Crediting it would inflate coverage with a name the catalog never had.
func TestUsedIgnoresNonBuiltinCalls(t *testing.T) {
	src := `Shikigami "Scaled" (factor: Int)
    Cursed Technique: Map Each
        Using: (x) -> x * factor

Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Shikigami: Scaled
    factor: 7
Maximum Technique: Sum
Reveal: stdout
`
	u := usageOf(t, src)
	if _, ok := u.Builtins["Scaled"]; ok {
		t.Errorf("a Shikigami call was counted as a builtin: %v", u.Builtins)
	}
	// The inlined body's own primitive is still counted.
	if u.Prims["Map Each"] == 0 {
		t.Errorf("the Shikigami body's Map Each not counted; got %v", u.Prims)
	}
}

// The catalog sizes are what a coverage report divides by, so they are worth
// an assertion that they are populated and mutually consistent rather than a
// pinned number that churns on every new primitive.
func TestCatalogAccessors(t *testing.T) {
	if len(AllPrims()) != len(Registry) {
		t.Errorf("AllPrims: got %d want %d", len(AllPrims()), len(Registry))
	}
	if len(AllBuiltins()) == 0 {
		t.Error("AllBuiltins is empty")
	}
	if len(AllKeywords()) == 0 {
		t.Error("AllKeywords is empty")
	}
	// AllBuiltins must hand back a copy; a caller sorting it must not
	// reorder the typecheck table every other package reads.
	a := AllBuiltins()
	if len(a) > 0 {
		a[0] = "clobbered"
		if AllBuiltins()[0] == "clobbered" {
			t.Error("AllBuiltins exposes the underlying table")
		}
	}
}

func TestUsageMerge(t *testing.T) {
	a := newUsage()
	a.Prims["Sort"] = 2
	a.Builtins["gcd"] = 1
	b := newUsage()
	b.Prims["Sort"] = 3
	b.Prims["Sum"] = 1
	a.Merge(b)
	if a.Prims["Sort"] != 5 {
		t.Errorf("Sort: got %d want 5", a.Prims["Sort"])
	}
	if a.Prims["Sum"] != 1 {
		t.Errorf("Sum: got %d want 1", a.Prims["Sum"])
	}
	if a.Builtins["gcd"] != 1 {
		t.Errorf("gcd: got %d want 1", a.Builtins["gcd"])
	}
	a.Merge(nil) // must not panic
}

// Every node Prim the repo's own programs produce is either a catalog entry,
// an alias of one, or a listed structural statement.
//
// This is the guard on the one way coverage can quietly lie. A primitive whose
// Build names its node something other than its Primitive.ID — Fold builds
// "FoldOver", Select Top K builds "SelectTopK" — is reported as never
// exercised however often it runs, and nothing else in the system notices.
// Running the whole corpus through Used is what turns that from a silent
// wrong number into a failing test.
func TestEveryObservedNodePrimIsAccountedFor(t *testing.T) {
	dirs := []string{
		filepath.Join("..", "challenges"),
		filepath.Join("..", "examples"),
		filepath.Join("..", "testdata"),
		filepath.Join("..", "bench", "testdata"),
	}
	seen := map[string]string{} // node prim -> a program that produced it
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.domain"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			toks, err := lexer.Lex(string(src))
			if err != nil {
				continue
			}
			prog, err := parser.Parse(string(src), toks)
			if err != nil {
				continue
			}
			pipe, err := ResolveWith(prog, FileOptions(path))
			if err != nil {
				continue
			}
			for id := range Used(pipe).Prims {
				if _, ok := seen[id]; !ok {
					seen[id] = path
				}
			}
		}
	}
	if len(seen) < 20 {
		t.Fatalf("only %d distinct primitives across the corpus; the scan is not working", len(seen))
	}
	for id, where := range seen {
		if _, ok := Catalog[id]; ok {
			continue
		}
		if StructuralPrims[id] {
			continue
		}
		t.Errorf("node Prim %q (from %s) is neither a catalog entry nor a listed structural statement.\n"+
			"If it is a primitive whose Build names its node differently, add it to nodePrimAlias; "+
			"if it is a language statement, add it to StructuralPrims. Until then coverage reports it as never exercised.",
			id, where)
	}
}

// The three known renames resolve, and an unrelated name passes through.
func TestCatalogIDResolvesAliases(t *testing.T) {
	for node, want := range map[string]string{
		"FoldOver":       "Fold",
		"SelectTopK":     "Select Top K",
		"WindowedReduce": "Sliding Reduce",
		"Split":          "Split",
		"Channel":        "Channel",
	} {
		if got := CatalogID(node); got != want {
			t.Errorf("CatalogID(%q) = %q want %q", node, got, want)
		}
	}
	// And each alias target really is in the catalog, or the map is pointing
	// at a name that does not exist.
	for node, id := range nodePrimAlias {
		if _, ok := Catalog[id]; !ok {
			t.Errorf("nodePrimAlias[%q] = %q, which is not a catalog entry", node, id)
		}
	}
}
