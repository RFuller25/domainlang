package docs_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"domain/docs"
	"domain/prims"
	"domain/typecheck"
)

// Guards against documentation drift — the failure mode where the reference
// keeps describing a version of the language that no longer exists.
//
// Every check here is against the *code*, not against another document: the
// registry says which primitives exist, typecheck says which builtins exist,
// the filesystem says how many examples there are. An audit once turned up a
// page promising that out-of-bounds grid reads return a sentinel (they are a
// clean error), bounds on Permutations and Subsets that had been removed, and
// a count of examples four short — none of which a human reviewer is likely to
// re-verify while editing prose nearby.
//
// What these tests deliberately do NOT check is whether the prose is any good;
// they check that the surface it describes is the surface that exists.

// docFile reads one page out of the embedded site, which is what actually
// ships in the binary — testing the files on disk would let an embed mistake
// through.
func docFile(t *testing.T, name string) string {
	t.Helper()
	b, err := docs.FS.ReadFile(name)
	if err != nil {
		t.Fatalf("reading embedded %s: %v", name, err)
	}
	return string(b)
}

// referencePages are the pages the primitive reference is split across. The
// split follows the keyword classes, and prims.PrimDoc.DocPage names the same
// set from the Go side — TestCatalogPagesExist holds the two together.
func referencePages(t *testing.T) []string {
	t.Helper()
	pages, err := fs.Glob(docs.FS, "ref-*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("no ref-*.md pages found — the reference split is missing")
	}
	return pages
}

// referenceText is every reference page concatenated, for the checks that ask
// whether the reference documents something at all rather than where.
func referenceText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, p := range referencePages(t) {
		b.WriteString(docFile(t, p))
		b.WriteString("\n")
	}
	return b.String()
}

// expressionRefText is expressions.md plus the builtin pages it was split
// into, for the check that asks whether a builtin is documented at all.
func expressionRefText(t *testing.T) string {
	t.Helper()
	pages, err := fs.Glob(docs.FS, "ref-builtins-*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("no ref-builtins-*.md pages found — the builtin split is missing")
	}
	var b strings.Builder
	b.WriteString(docFile(t, "expressions.md"))
	for _, p := range pages {
		b.WriteString("\n")
		b.WriteString(docFile(t, p))
	}
	return b.String()
}

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(b)
}

// Every primitive the registry exposes is described in the reference. A
// primitive that ships without documentation is invisible to users.
func TestEveryPrimitiveIsDocumented(t *testing.T) {
	ref := referenceText(t)
	var missing []string
	for _, p := range prims.Registry {
		// "id" is the placeholder ID of the generated one-per-variant
		// primitives (Any/All, Take While/Drop While, …); each real variant is
		// named in its own Match phrase, and the reference documents those.
		if p.ID == "" || p.ID == "id" {
			continue
		}
		if !strings.Contains(ref, p.ID) {
			missing = append(missing, p.ID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("primitives missing from the ref-*.md pages: %s", strings.Join(missing, ", "))
	}
}

// Every expression builtin is documented, and the count the prose quotes is
// the real one.
func TestEveryBuiltinIsDocumented(t *testing.T) {
	ref := expressionRefText(t)
	var missing []string
	for _, b := range typecheck.Builtins {
		// Documented in the builtin tables as `name(args)`.
		if !strings.Contains(ref, "`"+b+"(") {
			missing = append(missing, b)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("builtins missing from the expression reference: %s", strings.Join(missing, ", "))
	}

	// The count appears in several places as a selling point; pin it to the
	// table it counts.
	want := fmt.Sprintf("%d builtin", len(typecheck.Builtins))
	for _, page := range []string{"README.md", "expressions.md", "compiler.md", "getting-started.md"} {
		src := docFile(t, page)
		if !strings.Contains(src, fmt.Sprintf("%d builtin", len(typecheck.Builtins))) &&
			regexp.MustCompile(`\b\d+ builtin`).MatchString(src) {
			t.Errorf("docs/%s quotes a builtin count that is not %q", page, want)
		}
	}
}

// The counts of runnable programs quoted in prose match what is on disk. These
// are written as number words, which is exactly why they rot silently: nothing
// about editing a sentence forces anyone to recount the directory.
func TestRunnableProgramCountsAreCurrent(t *testing.T) {
	words := map[int]string{
		10: "ten", 11: "eleven", 12: "twelve", 13: "thirteen", 14: "fourteen",
		15: "fifteen", 16: "sixteen", 17: "seventeen", 18: "eighteen",
		19: "nineteen", 20: "twenty", 21: "twenty-one", 22: "twenty-two",
	}
	// Longest alternative first: Go's regexp is leftmost-*first*, so with
	// `twenty` ahead of `twenty-one` the shorter one wins and "twenty-one"
	// can never be recognized — the words table above has always had the
	// entry, and the twenty-first example is what finally reached it.
	anyWord := regexp.MustCompile(`\b(twenty-one|twenty-two|twenty|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen)\b`)
	// Flatten to one line and drop dots, so a window can span the line wrap and
	// the "../examples/" inside a link target without the scan stopping early.
	normalize := func(s string) string {
		s = strings.ToLower(s)
		s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
		return strings.ReplaceAll(s, ".", "")
	}
	count := func(dir string) int {
		hits, err := filepath.Glob(filepath.Join("..", dir, "*.domain"))
		if err != nil {
			t.Fatal(err)
		}
		return len(hits)
	}

	// For each mention of the noun, the nearest number word *before* it is the
	// one claiming its count. Checking the nearest rather than any word in a
	// window is what lets one sentence quote both counts ("nineteen programs …
	// and thirteen challenges") without either check firing on the other.
	check := func(t *testing.T, where, src, noun string, want int) {
		t.Helper()
		norm := normalize(src)
		for _, loc := range regexp.MustCompile(`\b`+noun+`\b`).FindAllStringIndex(norm, -1) {
			from := loc[0] - 60
			if from < 0 {
				from = 0
			}
			near := anyWord.FindAllString(norm[from:loc[0]], -1)
			if len(near) == 0 {
				continue
			}
			got := near[len(near)-1]
			if got != words[want] {
				t.Errorf("%s: %q claims %q, but there are %d (%q)",
					where, noun, got, want, words[want])
			}
		}
	}

	for _, n := range []int{count("examples"), count("challenges")} {
		if _, ok := words[n]; !ok {
			t.Fatalf("%d programs — extend the number-word table", n)
		}
	}
	for _, page := range []string{"README.md", "getting-started.md", "walkthroughs.md"} {
		src := docFile(t, page)
		check(t, "docs/"+page, src, "examples", count("examples"))
		check(t, "docs/"+page, src, "challenges", count("challenges"))
		check(t, "docs/"+page, src, "programs", count("examples"))
	}
	for _, f := range []string{"README.md", "examples/README.md"} {
		check(t, f, repoFile(t, f), "programs", count("examples"))
	}
	check(t, "README.md", repoFile(t, "README.md"), "challenges", count("challenges"))
	check(t, "challenges/README.md", repoFile(t, "challenges/README.md"), "challenges", count("challenges"))
}

// Every CLI flag the binary accepts is documented. A flag nobody can discover
// may as well not exist.
func TestEveryCLIFlagIsDocumented(t *testing.T) {
	ref := docFile(t, "cli.md")
	sources, err := filepath.Glob(filepath.Join("..", "cmd", "domain", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	flagRe := regexp.MustCompile(`"(--[a-z][a-z-]+)"`)
	seen := map[string]bool{}
	for _, s := range sources {
		if strings.HasSuffix(s, "_test.go") {
			continue
		}
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range flagRe.FindAllStringSubmatch(string(b), -1) {
			seen[m[1]] = true
		}
	}
	var missing []string
	for f := range seen {
		if !strings.Contains(ref, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("CLI flags missing from docs/cli.md: %s", strings.Join(missing, ", "))
	}
}

// slugify mirrors the heading-anchor algorithm in docs/index.html, which is
// GitHub's: strip formatting marks, drop anything that is not a word
// character, space or hyphen, then map each remaining space to a hyphen. Runs
// of hyphens are not collapsed and the ends are not trimmed — the two used to
// disagree, which silently broke every anchor into a heading containing
// punctuation (most of the primitive reference) on the rendered site while
// leaving it working on GitHub.
var (
	slugStrip  = regexp.MustCompile("[`*_~]")
	slugDrop   = regexp.MustCompile(`[^\p{L}\p{N}_\s-]`)
	slugSpaces = regexp.MustCompile(`\s`)
)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugStrip.ReplaceAllString(s, "")
	s = slugDrop.ReplaceAllString(s, "")
	return slugSpaces.ReplaceAllString(s, "-")
}

func headings(src string) map[string]bool {
	out := map[string]bool{}
	fence := false
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		trimmed := strings.TrimLeft(line, "#")
		if n := len(line) - len(trimmed); n >= 1 && n <= 6 && strings.HasPrefix(trimmed, " ") {
			out[slugify(strings.TrimSpace(trimmed))] = true
		}
	}
	return out
}

// Every cross-reference between documentation pages resolves — both the file
// and, when given, the heading anchor. A broken anchor is invisible to the
// author (the link still renders) and lands the reader at the top of a
// thousand-line page.
func TestDocLinksResolve(t *testing.T) {
	pages, err := filepath.Glob("*.md")
	if err != nil {
		t.Fatal(err)
	}
	heads := map[string]map[string]bool{}
	load := func(path string) map[string]bool {
		abs, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		if h, ok := heads[abs]; ok {
			return h
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		h := headings(string(b))
		heads[abs] = h
		return h
	}
	for _, p := range pages {
		load(p)
	}

	linkRe := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	for _, page := range pages {
		src := docFile(t, page)
		for _, m := range linkRe.FindAllStringSubmatch(src, -1) {
			target := strings.TrimSpace(m[1])
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			path, anchor, _ := strings.Cut(target, "#")
			if path == "" {
				if anchor != "" && !load(page)[anchor] {
					t.Errorf("%s: link to #%s, but this page has no such heading", page, anchor)
				}
				continue
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s: link to %s, which does not exist", page, path)
				continue
			}
			if anchor == "" || !strings.HasSuffix(path, ".md") {
				continue
			}
			if h := load(path); h != nil && !h[anchor] {
				t.Errorf("%s: link to %s#%s, but that page has no such heading", page, path, anchor)
			}
		}
	}
}

// docHeading is one `###`-level entry of the primitive reference: the names it
// documents (a heading may cover several, "Max / Min / Product") and the type
// signature after the em dash.
type docHeading struct {
	names []string
	sig   string
}

// referenceHeadings parses primitives.md into the anchors it defines and the
// signature it gives each primitive name.
func referenceHeadings(t *testing.T) (anchors map[string]bool, byName map[string][]docHeading) {
	t.Helper()
	anchors, byName = map[string]bool{}, map[string][]docHeading{}
	for _, line := range strings.Split(referenceText(t), "\n") {
		trimmed := strings.TrimLeft(line, "#")
		level := len(line) - len(trimmed)
		if level < 1 || level > 6 || !strings.HasPrefix(trimmed, " ") {
			continue
		}
		raw := strings.TrimSpace(trimmed)
		anchors[slugify(raw)] = true
		names, sig := raw, ""
		if i := strings.Index(raw, "—"); i >= 0 {
			names, sig = raw[:i], strings.TrimSpace(raw[i+len("—"):])
			// The renderer also emits the short alias — the part before the
			// em dash — so a link survives an edit to the signature.
			if short := slugify(names); short != "" {
				anchors[short] = true
			}
		}
		if level != 3 {
			continue
		}
		h := docHeading{sig: sig}
		for _, n := range strings.Split(names, "/") {
			h.names = append(h.names, strings.TrimSpace(n))
		}
		for _, n := range h.names {
			byName[n] = append(byName[n], h)
		}
	}
	return anchors, byName
}

// normSig flattens the cosmetic differences between how the catalog and the
// reference spell a signature (arrow glyph, backticks, spacing) so a comparison
// is about the types rather than the typography.
func normSig(s string) string {
	r := strings.NewReplacer("→", "->", "×", "x", "`", "", " or ", " | ")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

// The hover catalog and the primitive reference describe the same primitives,
// and the editor's link into the reference has to land somewhere.
//
// Both halves of this caught real bugs. Every DocAnchor but two was written as
// a bare name ("window") while the headings carry their signature, so every
// hover pointed at an anchor that did not exist; the renderer now emits the
// short alias, which is the spelling worth keeping because editing a signature
// must not break every link into that heading. And five catalog signatures were
// simply wrong — Sort claimed List<Int> when it orders any ordered type, Find
// Cells and Count Cells omitted Sparse, Sort By's key was pinned to Int, and
// Topological Sort omitted its edge-list form — so the editor was showing a
// narrower type than the primitive accepts.
func TestCatalogMatchesTheReference(t *testing.T) {
	anchors, byName := referenceHeadings(t)

	// Documented in language.md rather than the primitive reference: these are
	// statements, and their catalog anchors point at the section that covers
	// them there.
	elsewhere := map[string]bool{"Binding Vow": true, "Emit": true}

	for id, d := range prims.Catalog {
		if !anchors[d.DocAnchor] {
			t.Errorf("catalog %q: DocAnchor %q is not a heading in primitives.md", id, d.DocAnchor)
		}
		if elsewhere[id] {
			continue
		}
		// A heading may name a primitive with a trailing word ("Combinations k").
		var heads []docHeading
		for name, hs := range byName {
			if name == id || strings.HasPrefix(name, id+" ") {
				heads = append(heads, hs...)
			}
		}
		if len(heads) == 0 {
			t.Errorf("catalog %q: no ### heading in primitives.md documents it", id)
			continue
		}
		if d.Signature == "" {
			continue
		}
		matched := false
		for _, h := range heads {
			if h.sig != "" && strings.Contains(normSig(h.sig), normSig(d.Signature)) {
				matched = true
				break
			}
		}
		if !matched {
			var sigs []string
			for _, h := range heads {
				sigs = append(sigs, h.sig)
			}
			t.Errorf("catalog %q signature %q is not in its reference heading(s) %q",
				id, d.Signature, strings.Join(sigs, " | "))
		}
	}
}
