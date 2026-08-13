package docs_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"domain/prims"
)

// The site needs two things the Markdown pages cannot give it: the runnable
// programs (examples/ and challenges/, which live outside docs/ and so cannot
// be embedded from here) and the primitive catalog (which lives in Go). Both
// are generated into JSON that ships alongside the Markdown.
//
// They are checked in rather than built at serve time so the site works
// identically in all three places it runs: the binary's embedded copy, a
// static file server, and GitHub Pages. Checked-in generated data drifts, so
// the tests below regenerate it and compare — `go test ./docs -update`
// rewrites both files, and every other run fails if they are stale. That is
// the same discipline the rest of docs_test.go applies to the prose: the
// source of truth is the code, and the test is what stops the copy diverging.

var update = flag.Bool("update", false, "rewrite the generated docs data (gallery.json, primitives.json)")

// galleryProgram is one runnable program as the site consumes it.
type galleryProgram struct {
	ID          string `json:"id"`          // "01_top_calories"
	Group       string `json:"group"`       // "examples" | "challenges"
	Title       string `json:"title"`       // first line of the leading comment block
	Description string `json:"description"` // the rest of that block, minus the shell recipe
	Source      string `json:"source"`      // the program itself, comments and all
	Input       string `json:"input"`       // its .input file
	Expected    string `json:"expected"`    // its .expected file
	// Libs are the Shikigami libraries the program imports, keyed by the
	// target as written (`Innate Domain: lib/shapes` → "lib/shapes"). The
	// playground has no filesystem to find them on, so they travel with the
	// program and are handed to the resolver as a virtual one.
	Libs map[string]string `json:"libs,omitempty"`
}

// primitiveEntry is one row of the primitive index.
type primitiveEntry struct {
	ID        string `json:"id"`
	Keyword   string `json:"keyword"`
	Signature string `json:"signature"`
	Summary   string `json:"summary"`
	Anchor    string `json:"anchor"`
	Page      string `json:"page"`
}

// buildGallery reads examples/ and challenges/ into the site's shape. Title
// and description come from each program's own leading comment block, so they
// stay accurate without a second copy to maintain: the block is what a reader
// opening the file sees first, and every program has one.
func buildGallery(t *testing.T) []galleryProgram {
	t.Helper()
	var out []galleryProgram
	for _, group := range []string{"examples", "challenges"} {
		paths, err := filepath.Glob(filepath.Join("..", group, "*.domain"))
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) == 0 {
			t.Fatalf("no programs found in %s/", group)
		}
		sort.Strings(paths)
		for _, p := range paths {
			id := strings.TrimSuffix(filepath.Base(p), ".domain")
			source := readOrFail(t, p)
			title, desc := leadingComment(source)
			if title == "" {
				t.Errorf("%s: no leading comment block to take a title from", p)
			}
			out = append(out, galleryProgram{
				ID:          id,
				Group:       group,
				Title:       title,
				Description: desc,
				Source:      source,
				Input:       readOrFail(t, strings.TrimSuffix(p, ".domain")+".input"),
				Expected:    readOrFail(t, strings.TrimSuffix(p, ".domain")+".expected"),
				Libs:        collectLibs(t, filepath.Join("..", group), source, map[string]string{}),
			})
		}
	}
	return out
}

func readOrFail(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// leadingComment splits a program's opening `#` block into its first sentence
// (the title) and the prose under it. The block conventionally ends with the
// shell lines showing how to run the program — those are dropped, since the
// gallery has a Run button where the recipe would go.
func leadingComment(src string) (title, description string) {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		lines = append(lines, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#")))
	}
	if len(lines) == 0 {
		return "", ""
	}
	title = strings.TrimSuffix(lines[0], ".")
	var body []string
	for _, l := range lines[1:] {
		// The recipe: indented `domain ...` invocations and their expected
		// output, which the gallery shows for real instead.
		if strings.HasPrefix(l, "domain ") || strings.HasPrefix(l, "./domain") ||
			strings.HasPrefix(l, "Expected output:") {
			continue
		}
		body = append(body, l)
	}
	return title, strings.TrimSpace(strings.Join(body, "\n"))
}

// importTarget matches an `Innate Domain: lib/shapes` statement.
var importTarget = regexp.MustCompile(`(?m)^\s*Innate Domain:\s*(\S+)\s*$`)

// collectLibs gathers, transitively, every library a program imports, so the
// playground can resolve them without a filesystem. Returns nil when there are
// none, which keeps the generated JSON free of empty objects.
func collectLibs(t *testing.T, baseDir, source string, into map[string]string) map[string]string {
	t.Helper()
	for _, m := range importTarget.FindAllStringSubmatch(source, -1) {
		target := m[1]
		if _, done := into[target]; done {
			continue
		}
		b, err := os.ReadFile(filepath.Join(baseDir, target+".domain"))
		if err != nil {
			t.Errorf("import %q does not resolve under %s: %v", target, baseDir, err)
			continue
		}
		into[target] = string(b)
		collectLibs(t, baseDir, string(b), into) // a library may import too
	}
	if len(into) == 0 {
		return nil
	}
	return into
}

// countCodeLines counts the lines of a program that are neither blank nor a
// comment — what is left once the leading block is stripped.
func countCodeLines(src string) int {
	n := 0
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			n++
		}
	}
	return n
}

// buildPrimitiveIndex flattens prims.Catalog, which catalog_test.go already
// pins to exactly the registered primitives — so the index cannot list a
// primitive that does not exist, or miss one that does.
func buildPrimitiveIndex() []primitiveEntry {
	out := make([]primitiveEntry, 0, len(prims.Catalog))
	for _, d := range prims.Catalog {
		out = append(out, primitiveEntry{
			ID: d.ID, Keyword: d.Keyword, Signature: d.Signature,
			Summary: d.Summary, Anchor: d.DocAnchor, Page: d.DocPage(),
		})
	}
	// Keyword order follows the pipeline's own shape (source, transform,
	// reduction, ...) rather than the alphabet, then ID within it.
	rank := map[string]int{
		"Cursed Energy": 0, "Cursed Technique": 1, "Channeled Energy": 2,
		"Maximum Technique": 3, "Domain Expansion": 4, "Binding Vow": 5,
		"Simple Domain": 6, "Shikigami": 7, "Reveal": 8,
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rank[out[i].Keyword], rank[out[j].Keyword]
		if ri != rj {
			return ri < rj
		}
		if out[i].Keyword != out[j].Keyword {
			return out[i].Keyword < out[j].Keyword
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// marshal writes the indented JSON the two generated files hold, with a
// trailing newline so they behave like every other text file in the tree.
func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

// checkGenerated compares a generated file against what the code produces now,
// rewriting it under -update. Failure means the repository changed and the
// site's copy did not.
func checkGenerated(t *testing.T, name string, want []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(name, want, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", name, len(want))
		return
	}
	got, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v\nrun: go test ./docs -update", name, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s is stale — the repository changed and this file did not.\nrun: go test ./docs -update", name)
	}
}

// The gallery is the 32 runnable programs the site shows. It must list every
// one of them: a program that ships without appearing is invisible to a
// reader browsing the site, which is the gap this whole file exists to close.
func TestGalleryDataIsCurrent(t *testing.T) {
	gallery := buildGallery(t)
	checkGenerated(t, "gallery.json", marshal(t, gallery))

	var examples, challenges int
	for _, p := range gallery {
		switch p.Group {
		case "examples":
			examples++
		case "challenges":
			challenges++
		}
		if p.Expected == "" {
			t.Errorf("%s: no expected output", p.ID)
		}
		// The program is what gets run; an empty one would render a blank
		// card. Checking for a themed keyword would be wrong — 16_no_prefixes
		// is the example that leaves them all out — so this only asks that
		// something other than the comment block is there.
		if countCodeLines(p.Source) == 0 {
			t.Errorf("%s: source is all comments", p.ID)
		}
	}
	// The prose in docs/README.md promises these counts; docs_test.go checks
	// the prose, and this checks the data behind it.
	if examples == 0 || challenges == 0 {
		t.Fatalf("gallery has %d examples and %d challenges", examples, challenges)
	}
	t.Logf("gallery: %d examples, %d challenges", examples, challenges)
}

// The primitive index is generated from the catalog, so it inherits
// catalog_test.go's guarantee that the catalog matches the registry.
// pageAnchors is every anchor one page defines, including the short alias the
// renderer emits for a heading with a signature — the part before the em dash,
// which is what the catalog stores so a link survives an edit to the signature.
func pageAnchors(t *testing.T, page string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	fence := false
	for _, line := range strings.Split(docFile(t, page), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		trimmed := strings.TrimLeft(line, "#")
		n := len(line) - len(trimmed)
		if n < 1 || n > 6 || !strings.HasPrefix(trimmed, " ") {
			continue
		}
		raw := strings.TrimSpace(trimmed)
		out[slugify(raw)] = true
		if i := strings.Index(raw, "—"); i >= 0 {
			out[slugify(raw[:i])] = true
		}
	}
	return out
}

func TestPrimitiveIndexIsCurrent(t *testing.T) {
	index := buildPrimitiveIndex()
	checkGenerated(t, "primitives.json", marshal(t, index))

	if len(index) != len(prims.Catalog) {
		t.Errorf("index has %d entries, catalog has %d", len(index), len(prims.Catalog))
	}
	for _, e := range index {
		if e.Signature == "" || e.Summary == "" {
			t.Errorf("%s: incomplete catalog entry", e.ID)
		}
		// Every row links into the reference; a dead anchor sends the reader
		// to the top of the page instead of the primitive they clicked.
		if e.Anchor == "" {
			t.Errorf("%s: no doc anchor", e.ID)
			continue
		}
		if e.Page == "" {
			t.Errorf("%s: no doc page — prims.keywordPages has no entry for %q", e.ID, e.Keyword)
			continue
		}
		// The anchor has to be on the page the entry names, or the site's
		// primitive index sends the reader to a page that does not document it.
		if !pageAnchors(t, e.Page)[e.Anchor] {
			t.Errorf("%s: anchor %q is not a heading of %s", e.ID, e.Anchor, e.Page)
		}
	}
}
