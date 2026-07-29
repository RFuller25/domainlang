package docs_test

import (
	"bytes"
	"encoding/json"
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
	"language": {"id": "language"}, "primitives": {"id": "primitives"},
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
	// Each call is its own node process, so the document index has to be
	// installed inside the same invocation as the function under test.
	script := `const r = require(process.argv[1]);
r.setDocIndex(JSON.parse(process.argv[4]));
const args = JSON.parse(process.argv[2]);
const out = r[process.argv[3]](...args);
process.stdout.write(JSON.stringify(out === undefined ? null : out));`
	idxJSON, err := json.Marshal(docIndex)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", script, abs, string(argJSON), fn, string(idxJSON))
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
		{"../examples/README.md", `href="../examples/README.md"`},
		// A .md page that is not in the manifest stays a plain relative link.
		{"nonesuch.md", `href="nonesuch.md"`},
	} {
		got := renderString(t, "makeLink", "label", c.url)
		if !strings.Contains(got, c.want) {
			t.Errorf("makeLink(%q) = %q, want it to contain %q", c.url, got, c.want)
		}
	}
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
