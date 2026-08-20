package parser

import (
	"strings"
	"testing"

	"domain/ast"
)

// The two spellings a `Cursed Object:` statement can take — one declaration on
// the keyword's line, or an indented run of them — and that both land in
// Decls rather than in Binds. The separation is the point: nothing that walks
// a statement's Binds looking for a scoped local should find a global there.
func TestGlobalDeclShapes(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Object: matches As 0
Cursed Object:
    a As 1
    doubled Of (x) -> length(x) * 2
Reveal: stdout
`
	stmts := parseSrc(t, src).Statements

	inline := stmts[1]
	if inline.Keyword != "Cursed Object" {
		t.Errorf("keyword = %q, want Cursed Object", inline.Keyword)
	}
	if len(inline.Binds) != 0 {
		t.Errorf("a declaration must not land in Binds, got %d", len(inline.Binds))
	}
	if len(inline.Decls) != 1 {
		t.Fatalf("inline form: %d decls, want 1", len(inline.Decls))
	}
	if got := inline.Decls[0]; got.Name != "matches" || got.Of {
		t.Errorf("inline decl = %+v, want matches As", got)
	}

	block := stmts[2]
	if len(block.Decls) != 2 {
		t.Fatalf("block form: %d decls, want 2", len(block.Decls))
	}
	if got := block.Decls[0]; got.Name != "a" || got.Of || got.Value == nil {
		t.Errorf("first decl = %+v, want `a As <expr>`", got)
	}
	// `Of` keeps the meaning it has on Consider: applied to the current value,
	// so it is a Lambda rather than a function binding.
	if got := block.Decls[1]; got.Name != "doubled" || !got.Of || got.Lambda == nil {
		t.Errorf("second decl = %+v, want `doubled Of <lambda>`", got)
	}
}

// A statement's operation phrase is never consulted for these keywords: they
// carry declarations instead. Op staying nil is what keeps prims.Infer and the
// phrase matchers out of it entirely.
func TestGlobalDeclHasNoPhrase(t *testing.T) {
	src := "Cursed Energy: stdin\nCursed Tool: n As 1\nReveal: stdout\n"
	stmt := parseSrc(t, src).Statements[1]
	if stmt.Op != nil {
		t.Errorf("Op = %+v, want nil — a declaration is not an operation phrase", stmt.Op)
	}
	if stmt.Keyword != "Cursed Tool" {
		t.Errorf("keyword = %q, want Cursed Tool", stmt.Keyword)
	}
}

// `Of` accepts everything it accepts on a Consider, including an indented
// sub-pipeline, because the right-hand side parsers are shared outright.
func TestGlobalDeclOfSubPipeline(t *testing.T) {
	src := `Cursed Energy: stdin
Cursed Object: total Of
    Maximum Technique: Sum
Reveal: stdout
`
	stmt := parseSrc(t, src).Statements[1]
	if len(stmt.Decls) != 1 {
		t.Fatalf("%d decls, want 1", len(stmt.Decls))
	}
	if d := stmt.Decls[0]; !d.Of || len(d.Body) != 1 {
		t.Errorf("decl = %+v, want an Of body of one statement", d)
	}
}

// Dropping the `Consider` word is safe only because these blocks are entered
// from the keyword rather than from lookahead. A phrase whose second word
// happens to be "As" must still parse as the phrase it always was.
func TestPhraseWithAsIsNotADeclaration(t *testing.T) {
	src := "Cursed Energy: stdin\nCursed Technique: Sort As Text\nReveal: stdout\n"
	stmt := parseSrc(t, src).Statements[1]
	if len(stmt.Decls) != 0 {
		t.Fatalf("a phrase was read as %d declarations", len(stmt.Decls))
	}
	if stmt.Op == nil || stmt.Op.Words[0] != "Sort" {
		t.Errorf("Op = %+v, want the phrase `Sort As Text`", stmt.Op)
	}
}

func TestGlobalDeclErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			name: "bare keyword with no block",
			src:  "Cursed Energy: stdin\nCursed Object:\nReveal: stdout\n",
			want: "needs a declaration on its own line",
		},
		{
			name: "empty block",
			src:  "Cursed Energy: stdin\nCursed Object:\n\nReveal: stdout\n",
			want: "needs a declaration on its own line",
		},
		{
			name: "no preposition",
			src:  "Cursed Energy: stdin\nCursed Object: Sum\nReveal: stdout\n",
			want: "NAME As <expression>",
		},
		{
			name: "expression keyword as a name",
			src:  "Cursed Energy: stdin\nCursed Object: if As 1\nReveal: stdout\n",
			want: "it is an expression keyword",
		},
		{
			name: "block line missing its preposition",
			src:  "Cursed Energy: stdin\nCursed Object:\n    a As 1\n    Sum\nReveal: stdout\n",
			want: "NAME As <expression>",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := parseErr(t, tc.src)
			if err == nil {
				t.Fatalf("%s parsed, want an error", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// An error about a name points at the name, not at whatever the cursor had
// reached by the time the check ran.
func TestBoundNameErrorPointsAtTheName(t *testing.T) {
	err := parseErr(t, "Cursed Energy: stdin\nCursed Object: if As 1\nReveal: stdout\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "2:16") {
		t.Errorf("error = %q, want it positioned at the name (2:16)", err)
	}
}

// Both keywords are in the canonical list, so a forgotten colon stays on the
// keyword path and keeps its precise syntax error instead of being re-read as
// a prefix-free phrase that names no operation.
func TestGlobalKeywordsAreCanonical(t *testing.T) {
	for _, kw := range []string{"Cursed Object", "Cursed Tool"} {
		got, n, ok := ast.KeywordPrefix(append(strings.Fields(kw), "x", "As", "1"))
		if !ok || got != kw || n != 2 {
			t.Errorf("KeywordPrefix(%q) = %q, %d, %v; want %q, 2, true", kw, got, n, ok, kw)
		}
	}
}
