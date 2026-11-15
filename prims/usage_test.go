package prims

import (
	"os"
	"path/filepath"
	"testing"

	"domain/ir"
	"domain/lexer"
	"domain/parser"
)

// Every node Prim the repo's own programs produce is either a catalog entry,
// an alias of one, or a listed structural statement.
//
// This is the guard on the one way coverage can quietly lie. A primitive whose
// Build names its node something other than its Primitive.ID — Fold builds
// "FoldOver", Select Top K builds "SelectTopK" — is reported as never
// exercised however often it runs, and nothing else in the system notices.
// Running the whole corpus through WalkNodes is what turns that from a silent
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
			WalkNodes(pipe, func(n *ir.Node) {
				id := CatalogID(n.Prim)
				if _, ok := seen[id]; !ok {
					seen[id] = path
				}
			})
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
