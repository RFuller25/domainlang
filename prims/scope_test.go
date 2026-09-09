package prims

import (
	"strings"
	"testing"

	"domain/ast"
	"domain/lexer"
	"domain/parser"
)

// The whole safety case for scopes rests on this: a program that declares no
// Innate Domain resolves against exactly the vocabulary it always did, in
// exactly the order it always did. Order matters as much as membership —
// the registry is searched specific-matcher-first, so "Split Each" preceding
// "Split" is load-bearing, and a scope that reordered Core would change what
// an existing phrase resolves to without changing what is in it.
func TestDefaultScopeRegistryMatchesCore(t *testing.T) {
	got := RegistryFor(DefaultScope)
	if len(got) != len(Core) {
		t.Fatalf("RegistryFor(DefaultScope) has %d primitives, Core has %d", len(got), len(Core))
	}
	for i := range got {
		if got[i] != Core[i] {
			t.Fatalf("position %d: got %q, Core has %q — the default scope must not reorder the vocabulary",
				i, got[i].ID, Core[i].ID)
		}
	}
}

// A nil scope is the default, which is what every caller that has not been
// taught about scopes still passes.
func TestNilScopeIsTheDefault(t *testing.T) {
	if len(RegistryFor(nil)) != len(Core) {
		t.Errorf("RegistryFor(nil) = %d primitives, want Core's %d", len(RegistryFor(nil)), len(Core))
	}
}

// AllPrimitives is for the tools that describe the language rather than
// resolve one program. With one scope that adds nothing it is Core; the
// property that matters is that it never loses one.
func TestAllPrimitivesCoversCore(t *testing.T) {
	all := AllPrimitives()
	seen := map[*Primitive]bool{}
	for _, p := range all {
		if seen[p] {
			t.Errorf("AllPrimitives lists %q twice", p.ID)
		}
		seen[p] = true
	}
	for _, p := range Core {
		if !seen[p] {
			t.Errorf("AllPrimitives is missing %q", p.ID)
		}
	}
}

func TestScopeNamedIsCaseAndSpaceInsensitive(t *testing.T) {
	for _, spelling := range []string{"Advent of Code", "advent of code", "  Advent of Code  "} {
		if sc, ok := ScopeNamed(spelling); !ok || sc != adventOfCode {
			t.Errorf("ScopeNamed(%q) = %v, %v; want the Advent of Code scope", spelling, sc, ok)
		}
	}
	if _, ok := ScopeNamed("Cursed Womb"); ok {
		t.Error("ScopeNamed found a scope this build does not register")
	}
}

// The alias mechanism works, and the default scope deliberately does not use
// it. `Innate Domain: aoc` is what every program written before scopes
// existed says, and it meant "import the aoc library": an alias matching it
// would resolve that line as a scope declaration and drop the import in
// silence. It has to stay an error, so that the diagnostic can name the
// keyword that now imports.
func TestAliasesResolveButAoCIsNotOne(t *testing.T) {
	sc := &Scope{Name: "Aliased Realm", Aliases: []string{"AR"}}
	saved := Scopes
	Scopes = append(append([]*Scope{}, Scopes...), sc)
	defer func() { Scopes = saved }()

	if got, ok := ScopeNamed("ar"); !ok || got != sc {
		t.Errorf("ScopeNamed(\"ar\") = %v, %v; want the aliased scope", got, ok)
	}
	for _, spelling := range []string{"aoc", "AoC"} {
		if got, ok := ScopeNamed(spelling); ok {
			t.Errorf("ScopeNamed(%q) resolved to %s; it must stay unknown so the "+
				"old import form is diagnosed rather than silently accepted", spelling, got.Name)
		}
	}
}

// An Innate Domain naming something this build does not have says so, at the
// declaration, and lists what it does have.
func TestUnknownScopeIsAPositionedError(t *testing.T) {
	_, err := resolveSrc(t, "Innate Domain: Cursed Womb\nCursed Energy: stdin\nReveal: stdout\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"1:1", `unknown Innate Domain "Cursed Womb"`, "Advent of Code"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

// Declaring the default explicitly is legal and changes nothing.
func TestDeclaringTheDefaultScopeIsANoOp(t *testing.T) {
	if _, err := resolveSrc(t, "Innate Domain: Advent of Code\nCursed Energy: stdin\nReveal: stdout\n"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// The options override is for the tools that resolve a fragment with no
// declaration in the text — the REPL, a language-server request.
func TestResolveOptionsScopeOverride(t *testing.T) {
	src := "Cursed Energy: stdin\nReveal: stdout\n"
	toks, _ := lexer.Lex(src)
	prog, err := parser.Parse(src, toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ResolveWith(prog, ResolveOptions{Scope: "Advent of Code"}); err != nil {
		t.Fatalf("resolve with an override: %v", err)
	}
	if _, err := ResolveWith(&ast.Program{}, ResolveOptions{Scope: "nonesuch"}); err == nil {
		t.Error("expected an error for an unknown scope override")
	}
}
