package parser

import (
	"strings"
	"testing"

	"domain/lexer"
)

// A Part may carry a role word, an argument, or both. The parser records what
// was written and judges none of it: which roles exist belongs to the
// program's Innate Domain, which the parser does not know about.
func TestParsePartRoleShapes(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantRole  string
		wantName  string
		wantInt   int64
		wantIsInt bool
	}{
		{"unroled label", "Part \"1\":\n    Reveal: stdout\n", "", "1", 0, false},
		{"role alone", "Part World:\n    Reveal: stdout\n", "World", "", 0, false},
		{"role and label", "Part Entity \"Creep\":\n    Reveal: stdout\n", "Entity", "Creep", 0, false},
		{"role and number", "Part Every 120:\n    Reveal: stdout\n", "Every", "", 120, true},
		{"unknown role parses", "Part Gamma:\n    Reveal: stdout\n", "Gamma", "", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := parse(t, c.src)
			if len(prog.Statements) != 1 {
				t.Fatalf("got %d statements, want 1", len(prog.Statements))
			}
			stmt := prog.Statements[0]
			if stmt.Keyword != "Part" {
				t.Fatalf("keyword = %q, want Part", stmt.Keyword)
			}
			if stmt.PartRole != c.wantRole {
				t.Errorf("role = %q, want %q", stmt.PartRole, c.wantRole)
			}
			// PartName keeps carrying the label, so every existing reader of
			// it is unaffected by roles existing.
			if stmt.PartName != c.wantName {
				t.Errorf("name = %q, want %q", stmt.PartName, c.wantName)
			}
			switch {
			case c.wantIsInt:
				if stmt.PartArg == nil || !stmt.PartArg.IsInt || stmt.PartArg.Int != c.wantInt {
					t.Errorf("arg = %+v, want int %d", stmt.PartArg, c.wantInt)
				}
			case c.wantName != "":
				if stmt.PartArg == nil || stmt.PartArg.IsInt || stmt.PartArg.Text != c.wantName {
					t.Errorf("arg = %+v, want text %q", stmt.PartArg, c.wantName)
				}
			default:
				if stmt.PartArg != nil {
					t.Errorf("arg = %+v, want none", stmt.PartArg)
				}
			}
		})
	}
}

func TestParsePartRefusals(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"no colon", "Part World\n    Reveal: stdout\n", "expected"},
		{"no body", "Part World:\n", "indented sub-pipeline"},
		{"no body, named in the message", "Part Every 120:\n", "Part Every 120"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toks, err := lexer.Lex(c.src)
			if err != nil {
				t.Fatalf("lex: %v", err)
			}
			_, err = Parse(c.src, toks)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

// `Innate Domain:` names the kind of program this is and is hoisted like a
// Shikigami definition, so where it sits in the file does not matter.
func TestParseScopeDeclaration(t *testing.T) {
	prog := parse(t, "Cursed Energy: stdin\nInnate Domain: Game Dev\nReveal: stdout\n")
	if prog.Scope == nil {
		t.Fatalf("no scope declared")
	}
	if prog.Scope.Name != "Game Dev" {
		t.Errorf("scope = %q, want %q", prog.Scope.Name, "Game Dev")
	}
	if len(prog.Statements) != 2 {
		t.Errorf("got %d statements, want 2 — the declaration is hoisted", len(prog.Statements))
	}
}

// A program has one kind. A second declaration names where the first was
// rather than letting the last line quietly win.
func TestParseSecondScopeIsAnError(t *testing.T) {
	src := "Innate Domain: Game Dev\nInnate Domain: Advent of Code\nReveal: stdout\n"
	toks, err := lexer.Lex(src)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	if _, err = Parse(src, toks); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "already declared") {
		t.Errorf("error = %v, want it to say the scope is already declared", err)
	}
}

// A library is imported with its own keyword now, and the error for a bare
// one names that keyword rather than the scope declaration.
func TestParseInheritedTechnique(t *testing.T) {
	prog := parse(t, "Inherited Technique: aoc\nCursed Energy: stdin\nReveal: stdout\n")
	if len(prog.Imports) != 1 || prog.Imports[0].Target != "aoc" {
		t.Fatalf("imports = %+v, want one targeting aoc", prog.Imports)
	}
	if prog.Scope != nil {
		t.Errorf("scope = %+v, want none — an import is not a scope", prog.Scope)
	}

	src := "Inherited Technique:\nReveal: stdout\n"
	toks, _ := lexer.Lex(src)
	if _, err := Parse(src, toks); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "Inherited Technique needs a library name") {
		t.Errorf("error = %v, want it to name the keyword", err)
	}
}

// A bare `Part:` is not a Part at all as far as the parser is concerned: it
// is an ordinary keyword statement with no operation, which is what it was
// before roles existed and what resolution still reports it as. Pinned here
// because parsePart's dispatch is what guarantees it, and widening that
// dispatch carelessly would change an existing error message.
func TestBarePartStaysAKeywordStatement(t *testing.T) {
	prog := parse(t, "Part:\n")
	if len(prog.Statements) != 1 {
		t.Fatalf("got %d statements, want 1", len(prog.Statements))
	}
	stmt := prog.Statements[0]
	if stmt.Keyword != "Part" || stmt.PartRole != "" || stmt.PartArg != nil {
		t.Errorf("statement = %+v, want a bare Part keyword with no role or argument", stmt)
	}
}
