package lsp

import (
	"strings"
	"testing"
)

// One program with every shape that introduces a name: a global and the
// `Cursed Tool` that writes it, a stage binding, a function binding, a lambda
// parameter, and a Shikigami with a typed parameter of its own.
const symProgram = `Cursed Object: target As 2020
Cursed Energy: input.txt
Shikigami: Ints
Maximum Technique: Count Matching
    Consider tolerance As 5
    Consider spread As (a, b) -> a - b
    Using: (x) -> spread(x, target) > tolerance
Cursed Tool: target As target + 1
Reveal: stdout
`

// col is the byte offset of word in the given 1-based line of src, which is
// how a test names a position without counting characters by hand. The match
// is on the whole word — `n` is the parameter, not the letter inside
// "Technique".
func col(t *testing.T, src string, line int, word string) int {
	t.Helper()
	text := strings.Split(src, "\n")[line-1]
	for i := 0; i+len(word) <= len(text); i++ {
		if text[i:i+len(word)] != word {
			continue
		}
		if i > 0 && isNameByte(text[i-1]) {
			continue
		}
		if i+len(word) < len(text) && isNameByte(text[i+len(word)]) {
			continue
		}
		return i
	}
	t.Fatalf("line %d (%q) does not contain the word %q", line, text, word)
	return 0
}

func TestSymbolsIndexesEveryDeclaration(t *testing.T) {
	a := Analyze("prog.domain", symProgram)
	got := map[string]Symbol{}
	for _, s := range a.Symbols() {
		got[s.Name] = s
	}

	for _, want := range []struct {
		name, kind, typ string
		line            int
	}{
		{"target", KindGlobal, "Int", 1},
		{"tolerance", KindBinding, "Int", 5},
		{"spread", KindFunc, "", 6},
		{"a", KindLambda, "", 6},
		{"x", KindLambda, "", 7},
	} {
		s, ok := got[want.name]
		if !ok {
			t.Errorf("%s is declared by the program and is not in the index", want.name)
			continue
		}
		if s.Kind != want.kind || s.Type != want.typ || s.Pos.Line != want.line {
			t.Errorf("%s = {%s %q line %d}, want {%s %q line %d}",
				want.name, s.Kind, s.Type, s.Pos.Line, want.kind, want.typ, want.line)
		}
	}

	// A `Cursed Tool` writes a global; it does not declare one. Reporting the
	// write as the declaration would send a reader past the line that says
	// what the name is.
	if w := got["target"].Writes; len(w) != 1 || w[0] != 8 {
		t.Errorf("target's writes = %v, want [8]", w)
	}
	if got["target"].Decl != "Cursed Object: target As 2020" {
		t.Errorf("target's declaring line = %q", got["target"].Decl)
	}
}

func TestSymbolAtResolvesTheNameUnderTheCursor(t *testing.T) {
	a := Analyze("prog.domain", symProgram)
	cases := []struct {
		line int
		word string
		kind string
		decl int
	}{
		{7, "spread", KindFunc, 6},   // a read, inside a lambda
		{7, "target", KindGlobal, 1}, // a global read from a stage
		{7, "tolerance", KindBinding, 5},
		{8, "target", KindGlobal, 1}, // the write side names the declaration
		{1, "target", KindGlobal, 1}, // the declaration itself
		{7, "x", KindLambda, 7},
	}
	for _, c := range cases {
		sym, ok := a.SymbolAt(c.line, col(t, symProgram, c.line, c.word))
		if !ok {
			t.Errorf("%d:%s resolved to nothing", c.line, c.word)
			continue
		}
		if sym.Name != c.word || sym.Kind != c.kind || sym.Pos.Line != c.decl {
			t.Errorf("%d:%s = {%s %s line %d}, want {%s %s line %d}",
				c.line, c.word, sym.Name, sym.Kind, sym.Pos.Line, c.word, c.kind, c.decl)
		}
	}
}

// A word that is not a name the program declares gets no answer: a keyword, an
// operation phrase, a comment, and the inside of a string literal are all
// things a reader can point at, and none of them is a variable.
func TestSymbolAtIgnoresWhatIsNotAName(t *testing.T) {
	const src = `Cursed Object: total As 0
Cursed Energy: input.txt
technically total is a comment word here
Cursed Technique: Map Each
    Using: (n) -> n + total
Reveal: "total"
`
	a := Analyze("prog.domain", src)
	for _, c := range []struct {
		line int
		word string
	}{
		{1, "Cursed"}, // a keyword
		{4, "Map"},    // an operation phrase
		{3, "total"},  // inside a comment
		{6, "total"},  // inside a string literal
	} {
		if sym, ok := a.SymbolAt(c.line, col(t, src, c.line, c.word)); ok {
			t.Errorf("%d:%s resolved to %s %q, want nothing", c.line, c.word, sym.Kind, sym.Name)
		}
	}
	// …while the read in the lambda on line 5 does resolve, which is what makes
	// the four above a real distinction rather than a broken lookup.
	if _, ok := a.SymbolAt(5, col(t, src, 5, "total")); !ok {
		t.Error("the global read on line 5 resolved to nothing")
	}
}

// A binding is visible to its own statement and the statements nested beneath
// it, and nowhere else. Two stages that bind the same name are two names.
func TestSymbolAtRespectsScope(t *testing.T) {
	const src = `Cursed Energy: input.txt
Maximum Technique: Count Matching
    Consider limit As 5
    Using: (x) -> x > limit
Cursed Technique: Map Each
    Consider limit As 99
    Using: (x) -> x + limit
Reveal: stdout
`
	a := Analyze("prog.domain", src)
	for _, c := range []struct{ read, decl int }{{4, 3}, {7, 6}} {
		sym, ok := a.SymbolAt(c.read, col(t, src, c.read, "limit"))
		if !ok {
			t.Fatalf("the read on line %d resolved to nothing", c.read)
		}
		if sym.Pos.Line != c.decl {
			t.Errorf("the read on line %d resolved to the binding on line %d, want line %d",
				c.read, sym.Pos.Line, c.decl)
		}
	}
}

// A Shikigami's parameter is a name too, and the definition header is where it
// comes from.
func TestSymbolAtFindsShikigamiParameters(t *testing.T) {
	const src = `Shikigami "Top N" (n: Int)
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top n
Cursed Energy: input.txt
Shikigami: Ints
Shikigami: Top N
Reveal: stdout
`
	a := Analyze("prog.domain", src)
	sym, ok := a.SymbolAt(3, col(t, src, 3, "n"))
	if !ok {
		t.Fatal("the parameter read on line 3 resolved to nothing")
	}
	if sym.Kind != KindParam || sym.Type != "Int" || sym.Pos.Line != 1 {
		t.Errorf("n = {%s %q line %d}, want {%s %q line 1}", sym.Kind, sym.Type, sym.Pos.Line, KindParam, "Int")
	}
}

// Go-to-definition answers for a name first and for the line's Shikigami call
// second, so both keys land somewhere useful on a `Shikigami:` line.
func TestDefinitionAtPosPrefersTheNameUnderTheCursor(t *testing.T) {
	a := Analyze("prog.domain", symProgram)
	loc, ok := a.DefinitionAtPos(7, col(t, symProgram, 7, "target"))
	if !ok || loc.Pos.Line != 1 {
		t.Errorf("the global read jumped to %+v (ok=%v), want line 1", loc, ok)
	}
	// A position on no name at all falls back to the line's own question.
	if _, ok := a.DefinitionAtPos(9, 0); ok {
		t.Error("a Reveal line offered a definition")
	}
}

// Hovering says what a reader asked: the type, where it came from, and — for a
// global — whether anything changes it.
func TestSymbolDescribe(t *testing.T) {
	a := Analyze("prog.domain", symProgram)
	sym, ok := a.SymbolAt(7, col(t, symProgram, 7, "target"))
	if !ok {
		t.Fatal("target resolved to nothing")
	}
	got := sym.Describe()
	for _, want := range []string{"**target**", "`Int`", "global, declared on line 1",
		"Cursed Object: target As 2020", "written on line 8"} {
		if !strings.Contains(got, want) {
			t.Errorf("hover text is missing %q:\n%s", want, got)
		}
	}

	const unwritten = `Cursed Object: k As 3
Cursed Energy: input.txt
Reveal: stdout
`
	b := Analyze("prog.domain", unwritten)
	sym, ok = b.SymbolAt(1, col(t, unwritten, 1, "k"))
	if !ok {
		t.Fatal("k resolved to nothing")
	}
	if !strings.Contains(sym.Describe(), "never written") {
		t.Errorf("a global nothing writes did not say so:\n%s", sym.Describe())
	}
}

// The index is built from the AST, so a program that does not type-check still
// says where its names come from — which is the state a program is in while it
// is being written.
func TestSymbolsSurviveABrokenProgram(t *testing.T) {
	const broken = `Cursed Object: total As 0
Cursed Energy: input.txt
Maximum Technique: Nonsense Operation
    Using: (n) -> n + total
`
	a := Analyze("prog.domain", broken)
	sym, ok := a.SymbolAt(4, col(t, broken, 4, "total"))
	if !ok {
		t.Fatal("a broken program forgot its own globals")
	}
	if sym.Kind != KindGlobal || sym.Pos.Line != 1 {
		t.Errorf("total = {%s line %d}, want {%s line 1}", sym.Kind, sym.Pos.Line, KindGlobal)
	}
}

// A cursor just past a name is on it: that is where it sits after typing one.
func TestWordAtAcceptsTheCursorAfterAName(t *testing.T) {
	a := Analyze("prog.domain", symProgram)
	line := 5 // "    Consider tolerance As 5"
	end := col(t, symProgram, line, "tolerance") + len("tolerance")
	if w, ok := a.WordAt(line, end); !ok || w != "tolerance" {
		t.Errorf("WordAt(%d, %d) = %q, %v; want tolerance", line, end, w, ok)
	}
}
