package docs_test

import (
	"strings"
	"testing"

	"domain/docs"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// Every ```domain block in the documentation is a program, and programs rot:
// a renamed primitive or a changed argument turns teaching material into
// something that no longer runs, with nothing to notice. These tests put every
// block through the real front end.
//
// Two levels, because not every block is a whole program:
//
//   - **Parsing** applies to all of them. A fragment showing one stage is still
//     valid syntax, so a lex or parse failure means the snippet is simply wrong.
//   - **Resolution** (type checking) applies to blocks that are complete
//     programs — those that begin with a source. A fragment has no input type,
//     so there is nothing to check it against.
//
// A block that is deliberately not a program — a side-by-side layout, or one
// showing an error — opts out with an `ignore` flag in the info string:
//
//	```domain ignore
//
// The site renders those exactly like any other Domain block (see the
// info-string handling in render.js); only these tests treat them differently.

// Block extraction lives in docs/blocks.go so that this file and the
// runnable-example harness in cmd/domain cannot disagree about what a
// ```domain block is; see the commentary there for the three info states.

// isProgram reports whether a block is a whole program rather than a fragment:
// whether its first meaningful line is a source stage. `Innate Domain` and
// `Shikigami` definitions may precede it, since both are declarations.
func isProgram(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(line, " ") {
			continue
		}
		switch {
		case strings.HasPrefix(t, "Innate Domain:"), strings.HasPrefix(t, "Shikigami \""):
			continue
		case strings.HasPrefix(t, "Cursed Energy:"):
			return true
		default:
			return false
		}
	}
	return false
}

func allDomainBlocks(t *testing.T) []docs.Block {
	t.Helper()
	all, err := docs.AllBlocks()
	if err != nil {
		t.Fatal(err)
	}
	var out []docs.Block
	for _, b := range all {
		if b.Lang == "domain" {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		t.Fatal("found no ```domain blocks at all — the extractor is broken")
	}
	return out
}

// Every Domain block in the docs is valid syntax.
func TestDocBlocksParse(t *testing.T) {
	for _, b := range allDomainBlocks(t) {
		if b.Ignored() {
			continue
		}
		toks, err := lexer.Lex(b.Source)
		if err != nil {
			t.Errorf("%s:%d: does not lex: %v\n%s", b.Page, b.Line, err, b.Source)
			continue
		}
		if _, err := parser.Parse(b.Source, toks); err != nil {
			t.Errorf("%s:%d: does not parse: %v\n%s", b.Page, b.Line, err, b.Source)
		}
	}
}

// Every block that is a complete program also type-checks, which is what
// catches a renamed primitive or a changed argument.
func TestDocProgramsResolve(t *testing.T) {
	checked := 0
	for _, b := range allDomainBlocks(t) {
		if b.Ignored() || !isProgram(b.Source) {
			continue
		}
		// A program with an import needs a file context to resolve against,
		// which this test has none of — it holds source, not a path. Those
		// blocks are covered by the runnable harness in cmd/domain, which
		// stages the library beside the program and then runs it, so they are
		// checked more thoroughly there rather than not at all.
		if strings.Contains(b.Source, "Innate Domain:") {
			continue
		}
		toks, err := lexer.Lex(b.Source)
		if err != nil {
			continue // reported by TestDocBlocksParse
		}
		prog, err := parser.Parse(b.Source, toks)
		if err != nil {
			continue
		}
		checked++
		if _, err := prims.Resolve(prog); err != nil {
			t.Errorf("%s:%d: does not resolve: %v\n%s", b.Page, b.Line, err, b.Source)
		}
	}
	if checked == 0 {
		t.Fatal("no complete programs found in the docs — isProgram is too strict")
	}
	t.Logf("resolved %d complete programs out of the docs", checked)
}
