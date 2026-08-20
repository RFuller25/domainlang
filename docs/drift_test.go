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
	summary := openingBlocks(t, "cli.md", 2)
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
		// …and in the two summary blocks the page opens with, which are what a
		// reader scans to learn what the binary can do. Being documented in a
		// section further down is not the same thing: `bench`, `coverage`,
		// `stats`, `battle` and `mahoraga` each had a section of their own
		// while the opening list named none of them.
		if !strings.Contains(summary, "domain "+cmd) {
			t.Errorf("the command list at the top of docs/cli.md omits `domain %s`", cmd)
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no commands in the help text — the extraction above has stopped matching")
	}
}

// openingBlocks returns the first n fenced code blocks of a page.
func openingBlocks(t *testing.T, page string, n int) string {
	t.Helper()
	fences := regexp.MustCompile("(?s)```.*?\n(.*?)```").FindAllStringSubmatch(docFile(t, page), n)
	if len(fences) < n {
		t.Fatalf("docs/%s opens with %d code blocks, expected at least %d", page, len(fences), n)
	}
	var b strings.Builder
	for _, m := range fences {
		b.WriteString(m[1])
	}
	return b.String()
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

// The optimizer's pass catalog is numbered, the prose cross-references those
// numbers ("often the residue of pass 29"), and four other places quote the
// total. Nothing held the three together: the reordering family restarted its
// numbering at 11, colliding with the Stream substitution, so the catalog ran
// 1..31 with a duplicate and thirty-two entries while five sentences called it
// a 31-pass optimizer. This reads the numbering off the table, insists it is
// contiguous, and holds every quoted total and cross-reference to it.
func TestPassCatalogNumberingIsConsistent(t *testing.T) {
	catalog := docFile(t, "optimizer.md")

	// The rows, in the order they appear. Numbers must first appear as
	// 1, 2, … N; the only repeat allowed is of N itself, because the last pass
	// is documented in two tables (a fold accumulator, and a While/Repeat
	// state reached through a second entry point). Requiring that the repeat
	// be of the *last* number is the part that matters: the numbering that
	// prompted this test read 1..11, 11, 12, … which is contiguous but names
	// two different passes 11.
	rowNum := regexp.MustCompile(`(?m)^\| (\d+) \|`)
	rows := rowNum.FindAllStringSubmatch(catalog, -1)
	if len(rows) == 0 {
		t.Fatal("found no numbered rows in the pass catalog")
	}
	var seq []int
	for _, m := range rows {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		seq = append(seq, n)
	}
	last := 0
	for _, n := range seq {
		switch {
		case n == last+1:
			last = n
		case n == last && n == seq[len(seq)-1]:
			// a further table for the final pass
		default:
			t.Fatalf("the pass catalog reads %v: %d after %d is neither the next pass nor a second table for the last one", seq, n, last)
		}
	}

	// The catalog opens by counting itself in words, which is exactly the
	// spelling nothing recounts when a pass is added.
	names := map[int]string{
		28: "Twenty-eight", 29: "Twenty-nine", 30: "Thirty", 31: "Thirty-one",
		32: "Thirty-two", 33: "Thirty-three", 34: "Thirty-four",
	}
	families, ok := names[last-1]
	if !ok {
		t.Fatalf("%d passes in the four families — extend the number-word table", last-1)
	}
	if want := families + " passes in four families, plus one that runs after the rest."; !strings.Contains(catalog, want) {
		t.Errorf("optimizer.md no longer opens the catalog with %q", want)
	}

	// Every cross-reference names a row that exists.
	ref := regexp.MustCompile(`(?i)\bpass(?:es)? (\d+)(?:[–-](\d+))?(?: and (\d+))?`)
	for _, m := range ref.FindAllStringSubmatch(catalog, -1) {
		for _, s := range m[1:] {
			if s == "" {
				continue
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				t.Fatal(err)
			}
			if n < 1 || n > last {
				t.Errorf("optimizer.md refers to %q, but the catalog stops at %d", m[0], last)
			}
		}
	}

	// And every page quoting the total quotes this one.
	total := regexp.MustCompile(`(\d+)-pass|(\d+) rewrite passes`)
	// index.html carries the site navigation, whose one-line description of
	// the optimizer page quotes the total too — and was the last place still
	// saying 30 when everything else said 31.
	for _, page := range []string{"optimizer.md", "README.md", "diagnostics.md", "index.html"} {
		for _, m := range total.FindAllStringSubmatch(docFile(t, page), -1) {
			got := m[1] + m[2]
			if got != strconv.Itoa(last) {
				t.Errorf("docs/%s says %q, but the catalog documents %d passes", page, m[0], last)
			}
		}
	}
	for _, m := range total.FindAllStringSubmatch(repoFile(t, "README.md"), -1) {
		if got := m[1] + m[2]; got != strconv.Itoa(last) {
			t.Errorf("README.md says %q, but the catalog documents %d passes", m[0], last)
		}
	}
}

// A program the walkthroughs page introduces as `examples/NN_name.domain` is
// that file, minus the comment header the file carries for a reader who opens
// it directly. Nothing held the two together: the page's copy could be edited,
// or the example could be, and the page would go on claiming the reader could
// find this program on disk.
func TestWalkthroughsQuoteTheExamplesTheyName(t *testing.T) {
	page := docFile(t, "walkthroughs.md")

	// `examples/05_two_sections.domain` — the name as the prose writes it.
	named := regexp.MustCompile("`examples/([0-9a-z_]+)\\.domain`").FindAllStringSubmatch(page, -1)
	if len(named) == 0 {
		t.Fatal("walkthroughs.md names no example programs — the pattern above has stopped matching")
	}

	// The page's programs, in order, so each name can be paired with the block
	// that follows it.
	var progs []docs.Block
	for _, b := range docs.Blocks("walkthroughs.md", page) {
		if b.Lang == "domain" && !b.Ignored() {
			progs = append(progs, b)
		}
	}

	// stripHeader drops the leading comment block an example opens with.
	stripHeader := func(src string) string {
		lines := strings.Split(src, "\n")
		for len(lines) > 0 && (strings.HasPrefix(lines[0], "#") || strings.TrimSpace(lines[0]) == "") {
			lines = lines[1:]
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}

	for _, m := range named {
		name := m[1]
		at := strings.Index(page, m[0])
		line := strings.Count(page[:at], "\n") + 1
		// The first program below the mention is the one it introduces.
		var quoted *docs.Block
		for i := range progs {
			if progs[i].Line > line {
				quoted = &progs[i]
				break
			}
		}
		if quoted == nil {
			t.Errorf("walkthroughs.md names %s but shows no program after it", m[0])
			continue
		}
		want := stripHeader(repoFile(t, filepath.Join("examples", name+".domain")))
		if got := strings.TrimSpace(quoted.Source); got != want {
			t.Errorf("walkthroughs.md:%d quotes %s, but examples/%s.domain reads differently:\n--- the file\n%s\n--- the page\n%s",
				quoted.Line, m[0], name, want, got)
		}
	}
}

// The overview quotes how many runnable examples the documentation carries,
// and that number was 210 while 252 blocks were being executed. It is exactly
// the kind of figure nobody recounts, so it is counted here instead.
func TestRunnableExampleCountIsCurrent(t *testing.T) {
	pages, err := docs.Pages()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, page := range pages {
		exs, problem := docs.Examples(page, docFile(t, page))
		if problem != "" {
			t.Fatal(problem)
		}
		total += len(exs)
	}
	if total == 0 {
		t.Fatal("found no runnable examples at all — the extraction has stopped matching")
	}
	m := regexp.MustCompile(`(\d+) runnable examples`).FindStringSubmatch(docFile(t, "README.md"))
	if m == nil {
		t.Fatal("docs/README.md no longer says how many runnable examples there are")
	}
	if m[1] != strconv.Itoa(total) {
		t.Errorf("docs/README.md says %q, but %d blocks are executed", m[0], total)
	}
}

// match-pattern.md ends with a table of worked cases and the sentence "Each of
// these is covered by a test". Nothing connected the two, so a template could
// be reworded on the page, or the test that covered it deleted, and the
// sentence would go on being printed.
func TestWorkedMatchPatternCasesAreCovered(t *testing.T) {
	page := docFile(t, "match-pattern.md")
	if !strings.Contains(page, "Each of these is covered by a test.") {
		t.Fatal("match-pattern.md no longer claims its worked cases are tested — drop this test with the claim")
	}
	at := strings.Index(page, "## Worked cases")
	if at < 0 {
		t.Fatal("match-pattern.md has no worked-cases section")
	}
	table := page[at:]
	if end := strings.Index(table, "\n## "); end > 0 {
		table = table[:end]
	}

	// The Template column of each row, when it holds a literal template rather
	// than a description of one ("three `Case:` lines").
	tmpl := regexp.MustCompile("\\| `(\"[^`]*\")` \\|")
	found := tmpl.FindAllStringSubmatch(table, -1)
	if len(found) < 5 {
		t.Fatalf("found %d templates in the worked-cases table — the pattern above has stopped matching", len(found))
	}

	var sources []string
	err := filepath.Walk("..", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") && !strings.HasSuffix(p, ".domain") {
			return nil
		}
		if b, readErr := os.ReadFile(p); readErr == nil {
			sources = append(sources, string(b))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range found {
		// The table escapes the quotes inside a template for Markdown; the
		// sources carry the template itself.
		want := strings.ReplaceAll(strings.Trim(m[1], `"`), `\"`, `"`)
		covered := false
		for _, src := range sources {
			if strings.Contains(src, want) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("match-pattern.md lists the template %s as a worked case, but no test or example program uses it", m[1])
		}
	}
}

// The hole types match-pattern.md documents are the hole types the parser
// accepts. The page listed all six in its syntax table while its closing
// omissions named `int`, `word` and `text` as the whole set — three of them
// were added and only one half of the page was updated.
func TestHoleTypesAreTheOnesTheParserAccepts(t *testing.T) {
	src := repoFile(t, filepath.Join("pattern", "pattern.go"))
	at := strings.Index(src, "func holeTypeFromString(")
	if at < 0 {
		t.Fatal("pattern.go no longer has holeTypeFromString — this test reads its cases")
	}
	body := src[at:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	types := regexp.MustCompile(`(?m)^\tcase "([a-z]+)":`).FindAllStringSubmatch(body, -1)
	if len(types) == 0 {
		t.Fatal("found no hole types in holeTypeFromString — the pattern above has stopped matching")
	}

	page := docFile(t, "match-pattern.md")
	for _, m := range types {
		if !strings.Contains(page, "| `{"+m[1]+"}` |") && !strings.Contains(page, "`{name:"+m[1]+"}`") {
			t.Errorf("the parser accepts the hole type {%s}, but match-pattern.md's table does not list it", m[1])
		}
	}
	// And the closing list names the same set, so the two halves of the page
	// cannot disagree about what a template may say.
	var spelled []string
	for _, m := range types {
		spelled = append(spelled, "`"+m[1]+"`")
	}
	// Flattened, so the list may wrap across lines as prose does.
	flat := regexp.MustCompile(`\s+`).ReplaceAllString(page, " ")
	if want := strings.Join(spelled, ", "); !strings.Contains(flat, want) {
		t.Errorf("match-pattern.md's deliberate omissions no longer name the hole types as %s", want)
	}
}

// The loop ceiling is one number written down in three places: the
// interpreter's `prims.maxLoopIterations`, the compiler's
// `dmMaxLoopIterations`, and the sentence in compiler.md that quotes it. The
// two constants are in different packages and one of them is unexported, so
// neither can reference the other — codegen's own comment says it "must keep
// mirroring" prims and nothing made it. A program that runs interpreted and
// dies compiled is the single failure a shared ceiling exists to prevent.
func TestTheLoopCeilingIsOneNumber(t *testing.T) {
	read := func(rel, name string) string {
		src := repoFile(t, rel)
		m := regexp.MustCompile(`(?m)^(?:var|const) ` + name + ` = ([0-9_]+)`).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("%s no longer defines %s as a literal — this test reads it from there", rel, name)
		}
		return strings.ReplaceAll(m[1], "_", "")
	}
	interp := read(filepath.Join("prims", "control.go"), "maxLoopIterations")
	compiled := read(filepath.Join("codegen", "loopgen.go"), "dmMaxLoopIterations")
	if interp != compiled {
		t.Errorf("the interpreter stops a loop after %s and the compiled binary after %s", interp, compiled)
	}

	// compiler.md writes it with thousands separators.
	var grouped []string
	for i, r := range interp {
		if i > 0 && (len(interp)-i)%3 == 0 {
			grouped = append(grouped, ",")
		}
		grouped = append(grouped, string(r))
	}
	want := strings.Join(grouped, "")
	// It is the bolded figure in the Limits section; the paragraph's other
	// grouped numbers are the history of the one time the three did drift.
	page := docFile(t, "compiler.md")
	if !strings.Contains(page, "**"+want+"**") {
		t.Errorf("compiler.md does not quote the loop ceiling as **%s**", want)
	}
}

// The REPL's commands are listed twice: in `replHelp`, which is what `:help`
// prints, and in tooling.md's table, which is what a reader finds first. A
// command added to one and not the other is invisible from wherever the reader
// happened to look, and neither list can tell.
func TestREPLCommandsAreDocumented(t *testing.T) {
	help := repoFile(t, filepath.Join("cmd", "domain", "repl.go"))
	at := strings.Index(help, "const replHelp = `")
	if at < 0 {
		t.Fatal("cmd/domain/repl.go no longer defines replHelp — this test reads the commands from it")
	}
	block := help[at:]
	if end := strings.Index(block, "\n`\n"); end > 0 {
		block = block[:end]
	}
	inHelp := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^  (:[a-z]+)`).FindAllStringSubmatch(block, -1) {
		inHelp[m[1]] = true
	}
	if len(inHelp) < 10 {
		t.Fatalf("found %d commands in replHelp — the pattern above has stopped matching", len(inHelp))
	}

	page := docFile(t, "tooling.md")
	inPage := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| `(:[a-z]+)").FindAllStringSubmatch(page, -1) {
		inPage[m[1]] = true
	}
	if len(inPage) == 0 {
		t.Fatal("tooling.md no longer tabulates the REPL commands")
	}

	for cmd := range inHelp {
		if !inPage[cmd] {
			t.Errorf("`%s` is in the REPL's :help and not in tooling.md's table", cmd)
		}
	}
	for cmd := range inPage {
		if !inHelp[cmd] {
			t.Errorf("tooling.md documents `%s`, which :help does not list", cmd)
		}
	}
}

// Every key development.md tabulates is a key the editor binds. The page is a
// curated selection — it says so, and points at ctrl+g for the whole list — so
// the check only runs in this direction, which is the one that strands a
// reader: a documented key that does nothing.
func TestDocumentedEditorKeysAreBound(t *testing.T) {
	src := repoFile(t, filepath.Join("cmd", "domain", "dev_keys.go"))
	at := strings.Index(src, "func devHelpBody()")
	if at < 0 {
		t.Fatal("cmd/domain/dev_keys.go no longer defines devHelpBody — this test reads the bindings from it")
	}
	body := src[at:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	bound := map[string]bool{}
	for _, m := range regexp.MustCompile(`row\("([^"]+)"`).FindAllStringSubmatch(body, -1) {
		for _, k := range strings.Split(m[1], "/") {
			bound[strings.TrimSpace(k)] = true
		}
	}
	if len(bound) < 20 {
		t.Fatalf("found %d bindings in devHelpBody — the pattern above has stopped matching", len(bound))
	}
	// The motions the page names as present-but-not-tabulated.
	for _, k := range []string{"home", "end", "pgup", "pgdn", "ctrl+home", "ctrl+end"} {
		bound[k] = true
	}
	// ctrl+g opens the list itself, which is why it is not a row in it.
	bound["ctrl+g"] = true

	page := docFile(t, "development.md")
	keyish := regexp.MustCompile(`^(?:ctrl|alt|shift)\+\S+$`)
	checked := 0
	for _, line := range strings.Split(page, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(line, -1) {
			k := m[1]
			if !keyish.MatchString(k) {
				continue
			}
			checked++
			if !bound[k] {
				t.Errorf("development.md documents %s, which the editor does not bind", k)
			}
		}
	}
	if checked < 15 {
		t.Fatalf("only found %d documented keys — the table format has changed", checked)
	}
}

// aoc-gaps.md opens by counting itself: how many gaps the survey found and how
// many are closed. Both numbers move every time one closes, and the summary
// table below them is the record — so read them off it. The page said "found
// five of them" in the present tense with twelve of fourteen already struck
// through, which reads as five open blockers to anyone who stops there.
func TestTheGapCountsAreCurrent(t *testing.T) {
	page := docFile(t, "aoc-gaps.md")
	rows := regexp.MustCompile(`(?m)^\| (\d+) \| (.*?) \| \*\*([A-Za-z]+)\*\* \|`).FindAllStringSubmatch(page, -1)
	if len(rows) < 10 {
		t.Fatalf("found %d rows in the summary table — the pattern above has stopped matching", len(rows))
	}
	closed, open := 0, []string{}
	for _, r := range rows {
		if r[3] == "Closed" {
			closed++
			continue
		}
		open = append(open, r[1])
	}

	words := map[int]string{
		9: "Nine", 10: "Ten", 11: "Eleven", 12: "Twelve", 13: "Thirteen", 14: "Fourteen",
	}
	total := map[int]string{
		12: "twelve", 13: "thirteen", 14: "fourteen", 15: "fifteen", 16: "sixteen",
	}
	cw, ok := words[closed]
	if !ok {
		t.Fatalf("%d closed — extend the number-word table", closed)
	}
	tw, ok := total[len(rows)]
	if !ok {
		t.Fatalf("%d gaps — extend the number-word table", len(rows))
	}
	if want := cw + " of the " + tw + " are now closed."; !strings.Contains(page, "**"+want+"**") {
		t.Errorf("aoc-gaps.md does not open with %q", want)
	}
	if want := tw + " items in all"; !strings.Contains(page, want) {
		t.Errorf("aoc-gaps.md does not say %q", want)
	}
	// And it names the ones still open, so the summary and the sentence cannot
	// disagree about which they are.
	for _, n := range open {
		if !strings.Contains(page, "gap "+n) {
			t.Errorf("gap %s is still open in the summary, and the opening paragraph does not name it", n)
		}
	}
}
