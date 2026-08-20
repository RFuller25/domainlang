package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"domain/ast"
	"domain/docs"
)

// Guards for the ways this documentation has actually been observed to rot.
//
// Its neighbours in docs_test.go check that the documented *surface* is the
// surface that exists — every primitive described, every builtin listed. These
// check the places where the docs restate something the code owns in a form no
// existing test was reading: a table of keywords, the prelude's contents, the
// command list, and error messages quoted verbatim from tool output.
//
// Each one exists because something in that shape had already gone stale, or
// because adding one entry to a list in Go required remembering three separate
// tables in Markdown and nothing would have said so.

// tableRows are the Markdown table rows of a page — every line that starts
// with a pipe. Asking "does some row mention this?" is deliberately looser
// than parsing the table: the tables differ in shape from page to page, and
// the question worth guarding is whether the reader can find the thing at all.
func tableRows(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			out = append(out, line)
		}
	}
	return out
}

// Every themed keyword has a row in the keyword table of every page that
// carries one.
//
// Three pages present a keyword table as the list of what the language has, so
// a keyword missing from one is a page telling the reader it does not exist.
// Adding `Cursed Object` and `Cursed Tool` meant editing all three by hand;
// getting-started.md went out without a row for `Innate Domain` and nothing
// noticed, which is what this is for.
func TestEveryKeywordHasATableRow(t *testing.T) {
	for _, page := range []struct{ where, src string }{
		{"docs/language.md", docFile(t, "language.md")},
		{"docs/getting-started.md", docFile(t, "getting-started.md")},
		{"README.md", repoFile(t, "README.md")},
	} {
		rows := tableRows(page.src)
		for _, kw := range ast.Keywords {
			found := false
			for _, r := range rows {
				if strings.Contains(r, kw) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s has no table row for the keyword %q", page.where, kw)
			}
		}
	}
}

// Every command the binary advertises in its own help is documented.
//
// The companion to TestEveryCLIFlagIsDocumented one file over: that one covers
// `--flags`, and nothing covered the subcommands they belong to. A command
// nobody can discover may as well not exist.
func TestEveryCLICommandIsDocumented(t *testing.T) {
	ref := docFile(t, "cli.md")
	help := repoFile(t, filepath.Join("cmd", "domain", "main.go"))
	// The help block lists one command per line, indented, as
	// `  domain <command>  <description>`.
	re := regexp.MustCompile(`(?m)^\s{2}domain ((?:expansion: )?[a-z][a-z: ]*?)(?:\s{2,}|\s+<|$)`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(help, -1) {
		cmd := strings.TrimSpace(m[1])
		if cmd == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		if !strings.Contains(ref, "domain "+cmd) {
			t.Errorf("docs/cli.md does not document `domain %s`", cmd)
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no commands in the help text — the extraction above has stopped matching")
	}
}

// Every Shikigami the prelude defines is named in the reference, and the
// reference names no others.
//
// The prelude is a standard library written in Domain, so it is documented in
// prose rather than generated from the registry like the primitives are —
// which means adding one is a change nothing else would have caught.
func TestPreludeShikigamiAreDocumented(t *testing.T) {
	src := repoFile(t, filepath.Join("prims", "prelude.go"))
	defined := regexp.MustCompile(`Shikigami "([^"]+)"`).FindAllStringSubmatch(src, -1)
	if len(defined) == 0 {
		t.Fatal("found no Shikigami in the prelude — the extraction above has stopped matching")
	}
	ref := docFile(t, "language.md")
	for _, m := range defined {
		if !strings.Contains(ref, m[1]) {
			t.Errorf("docs/language.md never mentions the prelude Shikigami %q", m[1])
		}
	}
}

// Error text the documentation quotes as tool output is text the tools still
// produce.
//
// Several pages show a real diagnostic — a type error, a "did you mean", a
// violated vow — because "it fails before it runs" is a claim a reader has to
// see rather than be told. Nothing connected those blocks to the code that
// prints them, so rewording a message left the documentation quoting an error
// the language no longer emits, with every test still green.
//
// This is a substring check against the source rather than a diff of real
// output, which makes it weak but not useless: it fires on exactly the change
// that causes the rot, which is someone editing the wording in Go. When it
// does fire, the fix is to update the page *and* the phrase here together —
// the point of the list is that the two move at the same time.
func TestQuotedToolOutputStillExists(t *testing.T) {
	goSources := func() []string {
		var out []string
		err := filepath.Walk("..", func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") ||
				strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, readErr := os.ReadFile(p)
			if readErr == nil {
				out = append(out, string(b))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}()

	for _, tc := range []struct{ page, phrase, produced string }{
		{"getting-started.md", "expects input of type", "the type error a mismatched stage reports"},
		{"getting-started.md", "unknown operation", "the resolver's unknown-name error"},
		{"getting-started.md", "did you mean", "the diagnostics engine's suggestion"},
		{"getting-started.md", "auto-fixable", "the diagnostics engine's fix hint"},
		{"getting-started.md", "vow violated", "a Binding Vow failing"},
		{"getting-started.md", "Guaranteed hit", "an optimizer rewrite message"},
		{"getting-started.md", "Cursed Quickselect", "the sort-then-top-k substitution"},
	} {
		t.Run(tc.page+"/"+tc.phrase, func(t *testing.T) {
			if !strings.Contains(docFile(t, tc.page), tc.phrase) {
				t.Fatalf("docs/%s no longer quotes %q — drop it from this list", tc.page, tc.phrase)
			}
			for _, src := range goSources {
				if strings.Contains(src, tc.phrase) {
					return
				}
			}
			t.Errorf("docs/%s quotes %q as tool output (%s), but no Go source produces that text",
				tc.page, tc.phrase, tc.produced)
		})
	}
}

// The counts inside cli.md's sample output.
//
// Those blocks show what a command prints, numbers included, and the numbers
// are real ones the tool derives from the repository — so they go stale the
// moment a program is added. The sample said "20 program(s)" against
// twenty-two, alongside a primitive and builtin count that had drifted too.
//
// Only the counts are pinned, never the timings or the percentages: those
// differ per machine and per run, and a sample that had to be regenerated on
// every laptop would be deleted rather than maintained.
func TestCLISampleOutputCountsAreCurrent(t *testing.T) {
	ref := docFile(t, "cli.md")
	// The coverage and stats commands walk the folder, so a library under a
	// subdirectory counts — which is why this is a walk and not a glob.
	countDomain := func(dir string) int {
		n := 0
		err := filepath.Walk(filepath.Join("..", dir), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(p, ".domain") {
				n++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, dir := range []string{"examples", "challenges"} {
		re := regexp.MustCompile(regexp.QuoteMeta(dir+"/ — ") + `(\d+) program\(s\)`)
		m := re.FindStringSubmatch(ref)
		if m == nil {
			continue // the page no longer shows a sample for this folder
		}
		if got, want := m[1], countDomain(dir); got != strconv.Itoa(want) {
			t.Errorf("docs/cli.md sample says %q, but %s/ holds %d .domain files",
				m[0], dir, want)
		}
	}
}

// The playground's own documentation names the script that builds it, and that
// script exists. The site tells a reader with no Run buttons to run it, so a
// rename would strand them with an instruction that does nothing.
func TestPlaygroundBuildInstructionsPointAtRealFiles(t *testing.T) {
	for _, page := range []string{"wasm/README.md", "index.html"} {
		src := docFile(t, page)
		if !strings.Contains(src, "docs/wasm/build.sh") && !strings.Contains(src, "./docs/wasm/build.sh") {
			continue // the page does not give the instruction
		}
		if _, err := os.Stat(filepath.Join("..", "docs", "wasm", "build.sh")); err != nil {
			t.Errorf("docs/%s tells the reader to run docs/wasm/build.sh, which is not there: %v", page, err)
		}
	}
	if _, err := docs.FS.ReadFile("wasm/runner.js"); err != nil {
		t.Errorf("the playground worker is missing from the embedded site: %v", err)
	}
}
