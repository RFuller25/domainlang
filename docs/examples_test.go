package docs_test

import (
	"path/filepath"
	"strings"
	"testing"

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

type docBlock struct {
	page   string
	line   int // 1-based line of the opening fence
	info   string
	source string
}

// domainBlocks extracts every fenced block whose language is "domain".
func domainBlocks(t *testing.T, page, src string) []docBlock {
	t.Helper()
	var out []docBlock
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "```") {
			continue
		}
		info := strings.TrimSpace(strings.TrimPrefix(lines[i], "```"))
		start := i
		i++
		var body []string
		for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			body = append(body, lines[i])
			i++
		}
		if fields := strings.Fields(info); len(fields) > 0 && fields[0] == "domain" {
			out = append(out, docBlock{page: page, line: start + 1, info: info,
				source: strings.Join(body, "\n") + "\n"})
		}
	}
	return out
}

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

func allDomainBlocks(t *testing.T) []docBlock {
	t.Helper()
	pages, err := filepath.Glob("*.md")
	if err != nil {
		t.Fatal(err)
	}
	var out []docBlock
	for _, p := range pages {
		out = append(out, domainBlocks(t, p, docFile(t, filepath.Base(p)))...)
	}
	if len(out) == 0 {
		t.Fatal("found no ```domain blocks at all — the extractor is broken")
	}
	return out
}

// Every Domain block in the docs is valid syntax.
func TestDocBlocksParse(t *testing.T) {
	for _, b := range allDomainBlocks(t) {
		if strings.Contains(b.info, "ignore") {
			continue
		}
		toks, err := lexer.Lex(b.source)
		if err != nil {
			t.Errorf("%s:%d: does not lex: %v\n%s", b.page, b.line, err, b.source)
			continue
		}
		if _, err := parser.Parse(b.source, toks); err != nil {
			t.Errorf("%s:%d: does not parse: %v\n%s", b.page, b.line, err, b.source)
		}
	}
}

// Every block that is a complete program also type-checks, which is what
// catches a renamed primitive or a changed argument.
func TestDocProgramsResolve(t *testing.T) {
	checked := 0
	for _, b := range allDomainBlocks(t) {
		if strings.Contains(b.info, "ignore") || !isProgram(b.source) {
			continue
		}
		toks, err := lexer.Lex(b.source)
		if err != nil {
			continue // reported by TestDocBlocksParse
		}
		prog, err := parser.Parse(b.source, toks)
		if err != nil {
			continue
		}
		checked++
		if _, err := prims.Resolve(prog); err != nil {
			t.Errorf("%s:%d: does not resolve: %v\n%s", b.page, b.line, err, b.source)
		}
	}
	if checked == 0 {
		t.Fatal("no complete programs found in the docs — isProgram is too strict")
	}
	t.Logf("resolved %d complete programs out of the docs", checked)
}
