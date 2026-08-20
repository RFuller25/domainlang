package docs_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The documentation site's Markdown renderer is hand-written, which is why it
// is worth pinning: a bug in it is invisible from the Markdown side. The one
// that prompted these tests was in slugify — it collapsed runs of hyphens, so
// every cross-reference into a heading containing punctuation (most of the
// primitive reference) resolved on GitHub and silently failed on the rendered
// site. Nothing in the Markdown looked wrong, and the link still rendered.
//
// render.js has no DOM access and no dependencies, so it runs unchanged under
// node. Tests skip when node is absent, the same way the compiler tests skip
// without a Go toolchain.

// docIndex is the manifest render.js consults when deciding whether a link to
// a sibling page becomes an in-app route. The real page carries the whole
// manifest; the tests only need the ids.
var docIndex = map[string]map[string]string{
	"README": {"id": "README"}, "getting-started": {"id": "getting-started"},
	"walkthroughs": {"id": "walkthroughs"}, "language": {"id": "language"},
	"primitives":  {"id": "primitives"},
	"expressions": {"id": "expressions"}, "aoc-toolbox": {"id": "aoc-toolbox"},
	"data-model": {"id": "data-model"}, "match-pattern": {"id": "match-pattern"},
	"cli": {"id": "cli"}, "diagnostics": {"id": "diagnostics"},
	"tooling": {"id": "tooling"}, "optimizer": {"id": "optimizer"},
	"compiler": {"id": "compiler"},
}

func requireNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping the Markdown renderer tests")
	}
	return node
}

// renderJS calls one exported function of render.js with the given JSON
// arguments and returns its result as JSON.
func renderJS(t *testing.T, fn string, args ...any) json.RawMessage {
	t.Helper()
	node := requireNode(t)
	abs, err := filepath.Abs("render.js")
	if err != nil {
		t.Fatal(err)
	}
	argJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	// Arguments go through a file rather than argv: a search index built from
	// the real pages is megabytes of JSON, well past the argv limit.
	argPath := filepath.Join(t.TempDir(), "args.json")
	if err := os.WriteFile(argPath, argJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	// Each call is its own node process, so the document index has to be
	// installed inside the same invocation as the function under test.
	script := `const r = require(process.argv[1]);
r.setDocIndex(JSON.parse(process.argv[4]));
const args = JSON.parse(require("fs").readFileSync(process.argv[2], "utf8"));
const out = r[process.argv[3]](...args);
process.stdout.write(JSON.stringify(out === undefined ? null : out));`
	idxJSON, err := json.Marshal(docIndex)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", script, abs, argPath, fn, string(idxJSON))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("node running %s: %v\n%s", fn, err, errb.String())
	}
	return out.Bytes()
}

func renderString(t *testing.T, fn string, args ...any) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(renderJS(t, fn, args...), &s); err != nil {
		t.Fatalf("decoding %s result: %v", fn, err)
	}
	return s
}

func renderMarkdown(t *testing.T, md string) string {
	t.Helper()
	var res struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(renderJS(t, "renderMarkdown", md), &res); err != nil {
		t.Fatalf("decoding renderMarkdown result: %v", err)
	}
	return res.HTML
}

// slugify must match GitHub's algorithm exactly: the same link has to work in
// the rendered site and in the Markdown read on GitHub. The cases that matter
// are the ones with punctuation, where the two implementations diverged.
func TestSlugifyMatchesGitHub(t *testing.T) {
	for _, c := range []struct{ heading, want string }{
		{"Window", "window"},
		{"Map Each", "map-each"},
		// An em dash is dropped, leaving the spaces on either side of it — so
		// two hyphens. Collapsing them was the original bug.
		{"Reduce — `List<T> × (T, T -> T) -> T`", "reduce--listt--t-t---t---t"},
		{"Map Each — `List<T> × (T -> U) -> List<U>`", "map-each--listt--t---u---listu"},
		// Leading hyphens survive: `--json` is a real heading in cli.md.
		{"`--json`", "--json"},
		{"Channels — multi-section inputs", "channels--multi-section-inputs"},
		{"All Pairs / Combinations k", "all-pairs--combinations-k"},
		{"Sum Each Group", "sum-each-group"},
	} {
		if got := renderString(t, "slugify", c.heading); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.heading, got, c.want)
		}
	}
}

// The short alias is the stable half of an anchor: editing a signature must not
// break every link into that heading.
func TestShortSlug(t *testing.T) {
	for _, c := range []struct{ heading, want string }{
		{"Window — `List<T> -> List<List<T>>`", "window"},
		{"Map Each — `List<T> × (T -> U) -> List<U>`", "map-each"},
		{"All Pairs / Combinations k — `List<T> × Mode × lambda -> …`", "all-pairs--combinations-k"},
		// No em dash: the full slug is already short, so there is no alias.
		{"Measured arguments", ""},
		{"Convert To Grid", ""},
	} {
		if got := renderString(t, "shortSlug", c.heading); got != c.want {
			t.Errorf("shortSlug(%q) = %q, want %q", c.heading, got, c.want)
		}
	}
}

// A heading emits the full slug as its id and the short one as an alias
// immediately above, so both spellings of the link land on it.
func TestHeadingEmitsBothAnchors(t *testing.T) {
	html := renderMarkdown(t, "### Window — `List<T> -> List<List<T>>`\n")
	for _, want := range []string{
		`<a class="heading-alias" id="window"></a>`,
		`<h3 id="window--listt---listlistt">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered heading missing %q:\n%s", want, html)
		}
	}
}

// Inline rendering: the escaping matters more than the styling, since doc text
// is full of type signatures containing angle brackets.
func TestRenderInline(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"plain text", "plain text"},
		{"a `List<T>` value", "a <code>List&lt;T&gt;</code> value"},
		{"**bold**", "<strong>bold</strong>"},
		{"*italic*", "<em>italic</em>"},
		{"~~gone~~", "<del>gone</del>"},
		// Angle brackets outside code must not become tags.
		{"List<T> bare", "List&lt;T&gt; bare"},
		// An ampersand is escaped once, not twice.
		{"a & b", "a &amp; b"},
	} {
		if got := renderString(t, "renderInline", c.in); got != c.want {
			t.Errorf("renderInline(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A link to a sibling page becomes an in-app route; anything else is left
// alone. Getting this wrong sends the reader out of the site (or nowhere).
func TestMakeLinkRouting(t *testing.T) {
	for _, c := range []struct{ url, want string }{
		{"primitives.md", `href="#/primitives" data-route`},
		{"primitives.md#window", `href="#/primitives#window" data-route`},
		{"#local-anchor", `href="#local-anchor"`},
		{"https://example.com", `target="_blank"`},
		// The two directories the site renders itself become in-app routes.
		{"../examples/README.md", `href="#/examples" data-route`},
		{"../challenges/README.md", `href="#/challenges" data-route`},
		{"../examples/", `href="#/examples" data-route`},
		// Anything else outside docs/ goes to GitHub absolutely: served with
		// docs/ as the root, a relative "../" cannot climb out and silently
		// lands back inside the site.
		{"../README.md", `href="https://github.com/RFuller25/domain/blob/main/README.md"`},
		{"../README.md#install-with-nix", `blob/main/README.md#install-with-nix`},
		{"../editors/README.md", `blob/main/editors/README.md`},
		// A .md page that is not in the manifest stays a plain relative link.
		{"nonesuch.md", `href="nonesuch.md"`},
	} {
		got := renderString(t, "makeLink", "label", c.url)
		if !strings.Contains(got, c.want) {
			t.Errorf("makeLink(%q) = %q, want it to contain %q", c.url, got, c.want)
		}
	}
}

// Every link in the real pages that points outside docs/ must be rewritten
// into something the site can actually reach. These were all dead: the site is
// served with docs/ as the root — from the binary's embedded copy there is
// nothing above it — so eight of the nine 404'd and "../README.md" resolved
// back onto the documentation index, quietly showing the wrong page.
func TestOutboundLinksAreReachable(t *testing.T) {
	requireNode(t)
	pages, err := filepath.Glob("*.md")
	if err != nil {
		t.Fatal(err)
	}
	outbound := regexp.MustCompile(`\]\((\.\./[^)\s]+)\)`)
	seen := 0
	for _, page := range pages {
		for _, m := range outbound.FindAllStringSubmatch(docFile(t, page), -1) {
			seen++
			got := renderString(t, "makeLink", "label", m[1])
			switch {
			case strings.Contains(got, "data-route"): // an in-app gallery route
			case strings.Contains(got, "https://github.com/"): // read on GitHub
			default:
				t.Errorf("%s: link to %q renders as %s, which does not resolve when docs/ is the root",
					page, m[1], got)
			}
		}
	}
	if seen == 0 {
		t.Error("found no outbound links to check — has the pattern changed?")
	}
	t.Logf("checked %d outbound links", seen)
}

// Code fences carry a language tag and a copy button, and Domain blocks are
// highlighted. Highlighting runs on already-escaped text, so a mis-tokenization
// can only lose a colour — never produce broken markup.
func TestCodeFenceRendering(t *testing.T) {
	html := renderMarkdown(t, "```domain\nCursed Technique: Window 3    # note\n    Size: (xs) -> length(xs)\n```\n")
	for _, want := range []string{
		`<span class="lang-tag">domain</span>`,
		`<button class="copy-btn"`,
		`<span class="tok-kw">Cursed Technique</span>`,
		`<span class="tok-arg">Size</span>`,
		`<span class="tok-num">3</span>`,
		`<span class="tok-comment"># note</span>`,
		`<span class="tok-op">-&gt;</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("domain fence missing %q:\n%s", want, html)
		}
	}

	// A non-Domain fence is escaped but not tokenized.
	plain := renderMarkdown(t, "```sh\ndomain run x.domain\n```\n")
	if strings.Contains(plain, "tok-") {
		t.Errorf("non-domain fence should not be highlighted:\n%s", plain)
	}

	// An info string may carry flags; only the first word is the language.
	flagged := renderMarkdown(t, "```domain ignore\nCursed Energy: a.txt\n```\n")
	if !strings.Contains(flagged, `<span class="lang-tag">domain</span>`) {
		t.Errorf("flagged fence should still be tagged as domain:\n%s", flagged)
	}

	// Content inside a fence is never interpreted as Markdown.
	raw := renderMarkdown(t, "```\n# not a heading\n**not bold**\n```\n")
	if strings.Contains(raw, "<h1") || strings.Contains(raw, "<strong>") {
		t.Errorf("fence contents must stay literal:\n%s", raw)
	}
}

// Tables are the reference's densest format — several pages are mostly table.
func TestTableRendering(t *testing.T) {
	html := renderMarkdown(t, "| Builtin | Type |\n|---|---|\n| `abs(n)` | `Int -> Int` |\n")
	for _, want := range []string{"<table>", "<th>Builtin</th>", "<code>abs(n)</code>", "<td>"} {
		if !strings.Contains(html, want) {
			t.Errorf("table missing %q:\n%s", want, html)
		}
	}
}

// Lists, blockquotes and rules round out the constructs the docs actually use.
func TestBlockConstructs(t *testing.T) {
	for _, c := range []struct{ md, want string }{
		{"- one\n- two\n", "<ul>"},
		{"1. one\n2. two\n", "<ol>"},
		{"> quoted\n", "<blockquote>"},
		{"---\n", "<hr>"},
		{"a paragraph\n", "<p>a paragraph</p>"},
	} {
		if got := renderMarkdown(t, c.md); !strings.Contains(got, c.want) {
			t.Errorf("rendering %q gave %q, want it to contain %q", c.md, got, c.want)
		}
	}
}

// Every heading in the real pages renders with an id, and every id is unique —
// a collision would silently send one of two links to the wrong place.
func TestRealPagesHaveUniqueHeadingIDs(t *testing.T) {
	pages, err := filepath.Glob("*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		html := renderMarkdown(t, docFile(t, page))
		seen := map[string]bool{}
		for _, m := range idAttr.FindAllStringSubmatch(html, -1) {
			if seen[m[1]] {
				t.Errorf("%s: duplicate heading id %q", page, m[1])
			}
			seen[m[1]] = true
		}
		if len(seen) == 0 {
			t.Errorf("%s: rendered with no heading ids at all", page)
		}
	}
}

// idAttr matches the id of a heading or its alias anchor in rendered HTML.
var idAttr = regexp.MustCompile(`id="([^"]+)"`)

// ============================================================
//  Search
// ============================================================
// Search was silently dead once before: the index lived in render.js while the
// two functions that used it stayed in index.html, so indexing threw for every
// page — inside a catch that ignored it — and every keystroke threw too. The
// panel just never left "Start typing to search". Nothing about either file
// looked wrong on its own, which is exactly the shape of bug these tests are
// for. The pieces are pure now, so they can be driven end to end from here.

// searchEntry mirrors one entry of the search index.
type searchEntry struct {
	DocID       string `json:"docId"`
	DocTitle    string `json:"docTitle"`
	Heading     string `json:"heading"`
	HeadingSlug string `json:"headingSlug"`
	Text        string `json:"text"`
	Lower       string `json:"lower"`
}

// buildEntries indexes one page and hands the entries back as JSON, ready to
// be passed straight into searchEntries in a second call.
func buildEntries(t *testing.T, docID, docTitle, md string) (json.RawMessage, []searchEntry) {
	t.Helper()
	raw := renderJS(t, "buildSearchEntries", docID, docTitle, md)
	var entries []searchEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decoding buildSearchEntries result: %v", err)
	}
	return raw, entries
}

// searchIn runs a query against already-built entries.
func searchIn(t *testing.T, entries json.RawMessage, query string) []struct {
	Entry searchEntry `json:"entry"`
	Score float64     `json:"score"`
} {
	t.Helper()
	var results []struct {
		Entry searchEntry `json:"entry"`
		Score float64     `json:"score"`
	}
	// entries is already JSON, so it goes in as a raw message rather than being
	// round-tripped through a Go struct that might drop a field.
	if err := json.Unmarshal(renderJS(t, "searchEntries", entries, query), &results); err != nil {
		t.Fatalf("decoding searchEntries result: %v", err)
	}
	return results
}

// A page is split into one entry per heading, each carrying the slug that
// links to it — the same slug renderMarkdown gives the heading, or the link
// lands nowhere.
func TestBuildSearchEntries(t *testing.T) {
	md := "# Primitives\n\nIntro prose.\n\n" +
		"## Window — `List<T> -> List<List<T>>`\n\nSlides a window over a list.\n\n" +
		"```domain\nCursed Technique: Window 3\n```\n"
	_, entries := buildEntries(t, "primitives", "Primitives", md)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want one per heading:\n%+v", len(entries), entries)
	}
	if entries[0].Heading != "Primitives" || entries[0].HeadingSlug != "primitives" {
		t.Errorf("first entry = %q/%q, want Primitives/primitives", entries[0].Heading, entries[0].HeadingSlug)
	}
	// The slug is the full one, matching the heading's rendered id.
	if want := "window--listt---listlistt"; entries[1].HeadingSlug != want {
		t.Errorf("second entry slug = %q, want %q", entries[1].HeadingSlug, want)
	}
	// Fenced code is indexed: searching a primitive name should find the
	// example that uses it.
	if !strings.Contains(entries[1].Text, "Cursed Technique: Window 3") {
		t.Errorf("code fence not indexed:\n%q", entries[1].Text)
	}
	// Backticks and heading marks are stripped from the displayed heading.
	if strings.ContainsAny(entries[1].Heading, "`#") {
		t.Errorf("heading kept markdown noise: %q", entries[1].Heading)
	}
	if entries[1].Lower != strings.ToLower(entries[1].Text) {
		t.Errorf("lower is not the lowercased text: %q", entries[1].Lower)
	}
}

// The end-to-end path: index the real pages, then run the queries a reader
// actually types. Each must come back with the right page on top.
func TestSearchFindsRealPages(t *testing.T) {
	requireNode(t)
	// One combined index, built page by page the way the site builds it.
	var all []json.RawMessage
	for _, d := range []struct{ id, title, file string }{
		{"README", "Overview", "README.md"},
		{"ref-transforms", "Transforms", "ref-transforms.md"},
		{"cli", "CLI", "cli.md"},
		{"optimizer", "Optimizer", "optimizer.md"},
	} {
		raw, entries := buildEntries(t, d.id, d.title, docFile(t, d.file))
		if len(entries) == 0 {
			t.Fatalf("%s indexed to nothing", d.file)
		}
		all = append(all, raw)
	}
	combined := mergeJSONArrays(t, all)

	for _, c := range []struct{ query, wantDoc string }{
		{"window", "ref-transforms"},
		{"--json", "cli"},
		{"algorithm substitutions", "optimizer"},
		// Two terms: both must be present in the same entry.
		{"exit codes", "cli"},
	} {
		results := searchIn(t, combined, c.query)
		if len(results) == 0 {
			t.Errorf("search(%q) found nothing", c.query)
			continue
		}
		if results[0].Entry.DocID != c.wantDoc {
			t.Errorf("search(%q) top hit is in %q, want %q (heading %q)",
				c.query, results[0].Entry.DocID, c.wantDoc, results[0].Entry.Heading)
		}
		if results[0].Score <= 0 {
			t.Errorf("search(%q) top hit scored %v, want > 0", c.query, results[0].Score)
		}
	}

	// A query matching nothing returns nothing rather than erroring, and an
	// empty query returns nothing rather than everything.
	for _, q := range []string{"zzzznotathing", "   ", ""} {
		if results := searchIn(t, combined, q); len(results) != 0 {
			t.Errorf("search(%q) returned %d results, want none", q, len(results))
		}
	}
}

// Every term has to appear, and a heading match outranks a body match — the
// two scoring rules a reader would notice immediately if they broke.
func TestSearchRankingAndConjunction(t *testing.T) {
	md := "# Sorting\n\nThe optimizer substitutes quickselect here.\n\n" +
		"## Quicksort\n\nA named algorithm request.\n\n" +
		"## Unrelated\n\nNothing to see.\n"
	raw, _ := buildEntries(t, "optimizer", "Optimizer", md)

	results := searchIn(t, raw, "quicksort")
	if len(results) == 0 {
		t.Fatal("search found nothing")
	}
	if results[0].Entry.Heading != "Quicksort" {
		t.Errorf("top hit heading = %q, want the heading match to win", results[0].Entry.Heading)
	}

	// Both terms must land in the same entry: "quicksort" is only under its own
	// heading, "unrelated" only under another, so together they match nothing.
	if r := searchIn(t, raw, "quicksort unrelated"); len(r) != 0 {
		t.Errorf("search(%q) returned %d results, want none — terms are ANDed", "quicksort unrelated", len(r))
	}
}

// Snippets are HTML: they must escape the doc text (type signatures are full
// of angle brackets) and mark the terms, without the marks being escaped.
func TestMakeSnippet(t *testing.T) {
	_, entries := buildEntries(t, "primitives", "Primitives", "# Window\n\nTurns a List<T> into a List<List<T>> of windows.\n")
	if len(entries) == 0 {
		t.Fatal("indexed to nothing")
	}
	entryJSON, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	snippet := renderString(t, "makeSnippet", json.RawMessage(entryJSON), []string{"windows"})
	if strings.Contains(snippet, "<List") || !strings.Contains(snippet, "&lt;") {
		t.Errorf("snippet did not escape the doc text:\n%s", snippet)
	}
	if !strings.Contains(snippet, "<mark>windows</mark>") {
		t.Errorf("snippet did not mark the term:\n%s", snippet)
	}
}

// The unit tests above drive render.js directly, which is exactly what the
// original search bug slipped through: index.html referenced `searchIndex`, a
// name that lived inside render.js's closure and was never exported. Each file
// was fine on its own. So this boots the page's own scripts against a DOM stub
// (testdata/page_harness.js) and types a query into the search box, which
// catches a ReferenceError at the page's top level or anywhere in the search
// path — the failure that otherwise shows up only as a panel that never
// responds.
func TestPageSearchWorksEndToEnd(t *testing.T) {
	node := requireNode(t)
	harness, err := filepath.Abs(filepath.Join("testdata", "page_harness.js"))
	if err != nil {
		t.Fatal(err)
	}
	docsDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	run := func(query string) pageRun {
		t.Helper()
		var out, errb bytes.Buffer
		cmd := exec.Command(node, harness, docsDir, query)
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("booting the docs page: %v\n%s", err, errb.String())
		}
		var res pageRun
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("decoding harness output: %v\n%s", err, out.String())
		}
		return res
	}

	res := run("window")
	if len(res.Errors) > 0 {
		t.Fatalf("the page raised errors while searching:\n%s", strings.Join(res.Errors, "\n"))
	}
	if !res.Indexed {
		t.Fatal("the search index never finished building")
	}
	if res.ResultCount == 0 {
		t.Fatalf("searching for %q returned no results; the panel rendered:\n%s", "window", res.HTML)
	}
	if len(res.DocIDs) == 0 || res.DocIDs[0] != "ref-transforms" {
		t.Errorf("top hit for %q is in %v, want ref-transforms first", "window", res.DocIDs)
	}
	// Results must link at an anchor that the renderer actually emits, or the
	// hit opens the right page at the wrong place.
	if !strings.Contains(res.HTML, "#/ref-transforms#window--listt---listlistt") {
		t.Errorf("top hit does not link to the Window heading's anchor:\n%s", res.HTML)
	}
	// Snippets are escaped and the term is marked.
	if !strings.Contains(res.HTML, "<mark>Window</mark>") {
		t.Errorf("search terms are not highlighted in the snippet:\n%s", res.HTML)
	}
	if strings.Contains(res.HTML, "<List<T>") {
		t.Errorf("snippet did not escape a type signature:\n%s", res.HTML)
	}

	// A query with no matches says so instead of erroring or hanging.
	none := run("zzzznotathing")
	if len(none.Errors) > 0 {
		t.Errorf("a query with no matches raised errors:\n%s", strings.Join(none.Errors, "\n"))
	}
	if none.ResultCount != 0 || !strings.Contains(none.HTML, "No matches") {
		t.Errorf("a query with no matches rendered:\n%s", none.HTML)
	}
}

// A gallery card carries everything needed to understand a program without
// leaving the page: the source highlighted, the input it runs against, and the
// exact output the golden tests pin.
func TestRenderGallery(t *testing.T) {
	programs := []map[string]string{{
		"id": "01_fizzbuzz", "group": "challenges", "title": "FizzBuzz",
		"description": "The classic.", "source": "Cursed Energy: 01_fizzbuzz.input\nReveal: stdout",
		"input": "15", "expected": "1\n2\nFizz\n",
	}}
	var res struct {
		HTML     string `json:"html"`
		Headings []struct {
			Text string `json:"text"`
			Slug string `json:"slug"`
		} `json:"headings"`
	}
	if err := json.Unmarshal(renderJS(t, "renderGallery", programs, map[string]bool{"runnable": false}), &res); err != nil {
		t.Fatalf("decoding renderGallery result: %v", err)
	}
	for _, want := range []string{
		`<section class="program" id="01-fizzbuzz">`,
		`FizzBuzz`,
		`challenges/01_fizzbuzz.domain`,
		`<span class="tok-kw">Cursed Energy</span>`, // the source is highlighted
		`<details class="program-io">`,              // the input is there but folded away
		`data-expected="01_fizzbuzz"`,
		`<button class="copy-btn"`,
	} {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("gallery card missing %q:\n%s", want, res.HTML)
		}
	}
	// No Run button unless the playground was built.
	if strings.Contains(res.HTML, "data-run-program") {
		t.Errorf("gallery offered Run with runnable:false:\n%s", res.HTML)
	}
	if len(res.Headings) != 1 || res.Headings[0].Slug != "01-fizzbuzz" {
		t.Errorf("headings = %+v, want one entry slugged 01-fizzbuzz", res.Headings)
	}

	// With the playground available, every card gets a Run button.
	var runnable struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(renderJS(t, "renderGallery", programs, map[string]bool{"runnable": true}), &runnable); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runnable.HTML, `data-run-program="01_fizzbuzz"`) {
		t.Errorf("gallery did not offer Run with runnable:true:\n%s", runnable.HTML)
	}
}

// The primitive index groups by keyword, links each row into the reference,
// and filters across name, type and description at once.
func TestRenderPrimitiveIndex(t *testing.T) {
	entries := []map[string]string{
		{"id": "Window", "keyword": "Cursed Technique", "signature": "List<T> → List<List<T>>",
			"summary": "Sliding windows.", "anchor": "window", "page": "ref-transforms.md"},
		{"id": "Sum", "keyword": "Maximum Technique", "signature": "List<Int> → Int",
			"summary": "Adds them up.", "anchor": "sum", "page": "ref-reductions.md"},
	}
	decode := func(filter string) (html string, shown int) {
		t.Helper()
		var res struct {
			HTML  string `json:"html"`
			Shown int    `json:"shown"`
		}
		if err := json.Unmarshal(renderJS(t, "renderPrimitiveIndex", entries, filter), &res); err != nil {
			t.Fatalf("decoding renderPrimitiveIndex result: %v", err)
		}
		return res.HTML, res.Shown
	}

	html, shown := decode("")
	if shown != 2 {
		t.Errorf("unfiltered index shows %d, want 2", shown)
	}
	for _, want := range []string{
		`<h2 id="cursed-technique">`, `<h2 id="maximum-technique">`,
		// The row must link at the page the entry names, not at a hardcoded
		// one: the reference is split by keyword class, and these two rows are
		// deliberately on different pages.
		`href="#/ref-transforms#window" data-route`,
		`href="#/ref-reductions#sum" data-route`,
		`<code>Window</code>`,
		`List&lt;T&gt; → List&lt;List&lt;T&gt;&gt;`, // signatures are escaped
	} {
		if !strings.Contains(html, want) {
			t.Errorf("primitive index missing %q:\n%s", want, html)
		}
	}

	// Filtering matches the name...
	if html, shown = decode("window"); shown != 1 || !strings.Contains(html, "<code>Window</code>") {
		t.Errorf("filter \"window\" showed %d:\n%s", shown, html)
	}
	// ...the type step...
	if _, shown = decode("List<Int>"); shown != 1 {
		t.Errorf("filter by type step showed %d, want 1", shown)
	}
	// ...and the summary.
	if _, shown = decode("adds"); shown != 1 {
		t.Errorf("filter by summary showed %d, want 1", shown)
	}
	if html, shown = decode("zzz"); shown != 0 || !strings.Contains(html, "No primitive matches") {
		t.Errorf("filter with no matches rendered:\n%s", html)
	}
}

// A program that uses a primitive must not outrank the primitive's own
// reference. Before entries carried a weight, "window" put the challenge
// "Sliding window maximum" above the Window primitive.
func TestReferenceOutranksPrograms(t *testing.T) {
	refJSON, _ := buildEntries(t, "primitives", "Primitives",
		"## Window — `List<T> -> List<List<T>>`\n\nSliding windows over a list. Window is the primitive.\n")
	var ref []map[string]any
	if err := json.Unmarshal(refJSON, &ref); err != nil {
		t.Fatal(err)
	}
	programJSON := renderJS(t, "buildGalleryEntries", "challenges", "Challenges", []map[string]string{{
		"id": "05_window_max", "group": "challenges", "title": "Sliding window maximum",
		"description": "Window over a list.", "source": "Cursed Technique: Window 3", "expected": "9",
	}})
	var program []map[string]any
	if err := json.Unmarshal(programJSON, &program); err != nil {
		t.Fatal(err)
	}
	if program[0]["weight"] == nil {
		t.Fatal("gallery entries carry no weight, so ranking is decided by manifest order")
	}
	combined, err := json.Marshal(append(program, ref...)) // program first, so order alone would favour it
	if err != nil {
		t.Fatal(err)
	}
	results := searchIn(t, combined, "window")
	if len(results) == 0 {
		t.Fatal("search found nothing")
	}
	if results[0].Entry.DocID != "primitives" {
		t.Errorf("top hit for \"window\" is %q, want the reference to win over the program",
			results[0].Entry.DocID)
	}
}

// The search dialog is the site's main navigation aid and was, until now,
// unreachable by anyone not using a mouse and a working pair of eyes: no
// dialog semantics, no focus management, and arrow-key selection that moved a
// CSS class a screen reader has no way to observe. These are the attributes
// that carry that behaviour, checked against the markup because the harness's
// DOM stub cannot meaningfully assert accessibility.
func TestSearchDialogIsAccessible(t *testing.T) {
	page := docFile(t, "index.html")
	for _, c := range []struct{ want, why string }{
		{`role="dialog"`, "the overlay must announce itself as a dialog"},
		{`aria-modal="true"`, "the page behind the dialog must be inert to assistive tech"},
		{`aria-labelledby="searchLabel"`, "the dialog needs an accessible name"},
		{`role="combobox"`, "the input drives a list of results"},
		{`aria-controls="searchResults"`, "the input must point at the list it drives"},
		{`role="listbox"`, "the results are a list of options"},
		{`role="option"`, "each result is an option"},
		{`aria-activedescendant`, "arrow-key selection must be announced, not just styled"},
		{`aria-selected`, "the selected option must be marked as such"},
		{`aria-live="polite"`, "the result count must be announced when it changes"},
		{`aria-pressed`, "the scope toggle must expose its state"},
		{`focusBeforeSearch`, "closing the dialog must return focus where it came from"},
		{`e.key !== "Tab"`, "a modal dialog must trap Tab"},
	} {
		if !strings.Contains(page, c.want) {
			t.Errorf("index.html is missing %s — %s", c.want, c.why)
		}
	}

	// The live region has to be present but invisible; a visible one would
	// print a running result count into the middle of the dialog.
	if !strings.Contains(page, "visually-hidden") || !strings.Contains(page, "clip: rect(0 0 0 0)") {
		t.Error("index.html has no visually-hidden style for the live region")
	}
}

// pageRun is what the harness reports after booting the site: what search
// found, and how every page in the manifest rendered.
type pageRun struct {
	Errors      []string `json:"errors"`
	Indexed     bool     `json:"indexed"`
	ResultCount int      `json:"resultCount"`
	DocIDs      []string `json:"docIds"`
	HTML        string   `json:"html"`
	Pages       map[string]struct {
		Length   int    `json:"length"`
		HasError bool   `json:"hasError"`
		HTML     string `json:"html"`
	} `json:"pages"`
}

// Every page in the manifest must render. Four of them are not Markdown at all
// — the two galleries and the primitive index are built from generated JSON,
// the playground from the WebAssembly build — so nothing else in this file
// covers them, and a broken route shows up as an empty page rather than a
// crash.
func TestEveryPageRenders(t *testing.T) {
	node := requireNode(t)
	harness, err := filepath.Abs(filepath.Join("testdata", "page_harness.js"))
	if err != nil {
		t.Fatal(err)
	}
	docsDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	cmd := exec.Command(node, harness, docsDir, "window")
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("booting the docs page: %v\n%s", err, errb.String())
	}
	var res pageRun
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decoding harness output: %v\n%s", err, out.String())
	}
	if len(res.Errors) > 0 {
		t.Fatalf("the page raised errors while rendering:\n%s", strings.Join(res.Errors, "\n"))
	}
	if len(res.Pages) < 15 {
		t.Fatalf("only %d pages were visited; the manifest should have more", len(res.Pages))
	}
	for _, id := range []string{"examples", "challenges", "primitive-index", "playground"} {
		if _, ok := res.Pages[id]; !ok {
			t.Errorf("%s is not in the manifest", id)
		}
	}
	for id, p := range res.Pages {
		if p.HasError {
			t.Errorf("%s rendered an error box:\n%s", id, p.HTML)
		}
		// The playground is deliberately short when the wasm artifact has not
		// been built — it is one explanatory note. Everything else is a page.
		min := 1500
		if id == "playground" {
			min = 300
		}
		if p.Length < min {
			t.Errorf("%s rendered only %d bytes, which is too little to be the page:\n%s", id, p.Length, p.HTML)
		}
	}
}

// The galleries are the 32 runnable programs. They must actually carry the
// programs — source, input and the exact expected output — since that is the
// whole reason they exist.
func TestGalleryPagesShowPrograms(t *testing.T) {
	node := requireNode(t)
	harness, _ := filepath.Abs(filepath.Join("testdata", "page_harness.js"))
	docsDir, _ := filepath.Abs(".")
	var out bytes.Buffer
	cmd := exec.Command(node, harness, docsDir, "fizzbuzz")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("booting the docs page: %v", err)
	}
	var res pageRun
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	// Searching for a program by name finds its gallery.
	if len(res.DocIDs) == 0 || res.DocIDs[0] != "challenges" {
		t.Errorf("searching \"fizzbuzz\" put %v on top, want challenges — the gallery is not indexed", res.DocIDs)
	}
	// The gallery pages carry real weight: 19 and 13 programs with their
	// sources inline.
	if got := res.Pages["examples"].Length; got < 20000 {
		t.Errorf("the examples gallery rendered %d bytes, too little for 19 programs", got)
	}
	if got := res.Pages["challenges"].Length; got < 15000 {
		t.Errorf("the challenges gallery rendered %d bytes, too little for 13 programs", got)
	}
}

// mergeJSONArrays concatenates several JSON arrays into one.
func mergeJSONArrays(t *testing.T, arrays []json.RawMessage) json.RawMessage {
	t.Helper()
	var all []json.RawMessage
	for _, a := range arrays {
		var part []json.RawMessage
		if err := json.Unmarshal(a, &part); err != nil {
			t.Fatal(err)
		}
		all = append(all, part...)
	}
	out, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The renderer carries a code block's info-string flags through to the DOM, as
// `data-flags` on the <pre>.
//
// The site needs them to decide where a Run button belongs. It used to guess
// from the block's text — looking for themed keywords — and the guess was
// wrong in both directions: a `domain ignore` fragment listing several
// `Domain Expansion:` lines got a button that could only fail, and the program
// written *without* its optional keywords, which is the example teaching that
// they are optional, got none. It also needs them to find a ```lib block's
// path, since that rides in the info string too.
func TestCodeBlockFlagsReachTheDOM(t *testing.T) {
	for _, tc := range []struct{ name, md, wantAttr string }{
		{"run", "```domain run\nCursed Energy: stdin\n```\n", `data-flags="run"`},
		{"ignore", "```domain ignore\nDomain Expansion: BFS from 0 0\n```\n", `data-flags="ignore"`},
		{"lib path", "```lib aoc.domain\nShikigami \"X\"\n```\n", `data-flags="aoc.domain"`},
		{"no flags", "```domain\nCursed Energy: stdin\n```\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := renderMarkdown(t, tc.md)
			if tc.wantAttr == "" {
				if strings.Contains(html, "data-flags") {
					t.Errorf("a block with no flags got one:\n%s", html)
				}
				return
			}
			if !strings.Contains(html, tc.wantAttr) {
				t.Errorf("want %s in:\n%s", tc.wantAttr, html)
			}
		})
	}
}
