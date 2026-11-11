// `domain doc <name>` — the reference, one entry at a time, from the shell.
//
// The binary already carries all of it: the primitive catalog the language
// server hovers with, and the documentation site's pages, embedded so
// `domain expansion: documentation` can serve them. What it lacked was a way to
// ask one question without starting a session (`:doc` in the REPL) or a browser
// (the site). This is that: a primitive's signature, its sentence, and the
// first worked example its reference page gives; or an expression builtin's row
// of the builtin tables.
//
// Nothing here is a second copy of the documentation. Primitives come from
// prims.Catalog and everything else is read out of the embedded pages at the
// moment it is asked for, so an entry cannot disagree with the site.
package main

import (
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"sync"

	"domain/docs"
	"domain/typecheck"
)

// docEntry is one thing `domain doc` can describe.
type docEntry struct {
	Kind      string `json:"kind"` // "primitive" or "builtin"
	Name      string `json:"name"` // "Split", "length"
	Title     string `json:"title"`
	Signature string `json:"signature"`
	Summary   string `json:"summary"`
	Page      string `json:"page,omitempty"` // the reference page it is documented on
	Example   string `json:"example,omitempty"`
	Input     string `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
}

var (
	docIndexOnce sync.Once
	docIndex     []docEntry
)

// docEntries is every entry, primitives first in catalog order and then the
// builtins in the order their pages list them.
func docEntries() []docEntry {
	docIndexOnce.Do(func() {
		pages := docPages()
		examples := primitiveExamples(pages)
		for _, d := range sortedCatalog() {
			e := docEntry{
				Kind:      "primitive",
				Name:      d.ID,
				Title:     d.Keyword + ": " + d.ID,
				Signature: d.Signature,
				Summary:   d.Summary,
			}
			if ex, ok := examples[strings.ToLower(d.ID)]; ok {
				e.Page, e.Example, e.Input, e.Output = ex.page, ex.program, ex.input, ex.output
			}
			docIndex = append(docIndex, e)
		}
		docIndex = append(docIndex, builtinEntries(pages)...)
	})
	return docIndex
}

type docPage struct {
	name  string
	lines []string
}

// docPages reads the embedded reference pages, in name order so the index is
// the same on every run.
func docPages() []docPage {
	names, _ := fs.Glob(docs.FS, "*.md")
	slices.Sort(names)
	var pages []docPage
	for _, name := range names {
		body, err := fs.ReadFile(docs.FS, name)
		if err != nil {
			continue
		}
		pages = append(pages, docPage{name, strings.Split(string(body), "\n")})
	}
	return pages
}

type docExample struct {
	page, program, input, output string
}

// primitiveExamples maps a primitive's name to the first runnable example under
// its heading. A heading documents one primitive (`### Split — …`) or a family
// (`### Max / Min / Product — …`), and each name in it gets the section.
func primitiveExamples(pages []docPage) map[string]docExample {
	out := map[string]docExample{}
	for _, page := range pages {
		if !strings.HasPrefix(page.name, "ref-") {
			continue
		}
		for i, line := range page.lines {
			if !strings.HasPrefix(line, "### ") {
				continue
			}
			title := strings.TrimPrefix(line, "### ")
			if cut := strings.Index(title, " — "); cut >= 0 {
				title = title[:cut]
			}
			// A runnable example — one with its input and output beside it —
			// is worth more than a fragment, so it is preferred wherever the
			// section has one.
			ex := sectionExample(page.lines[i+1:], true)
			if ex.program == "" {
				ex = sectionExample(page.lines[i+1:], false)
			}
			ex.page = page.name
			for _, name := range strings.Split(title, " / ") {
				key := strings.ToLower(strings.TrimSpace(strings.Trim(name, "`")))
				if _, dup := out[key]; !dup && key != "" {
					out[key] = ex
				}
			}
		}
	}
	return out
}

// sectionExample takes the first ```domain block before the next heading —
// only a ```domain run one when runnable is set — and the ```input and
// ```output blocks that follow it directly.
func sectionExample(lines []string, runnable bool) docExample {
	var ex docExample
	fence := "" // the info string of the block being read, "" outside one
	var body []string
	for _, line := range lines {
		if fence == "" {
			if strings.HasPrefix(line, "#") {
				break
			}
			if !strings.HasPrefix(line, "```") {
				continue
			}
			info := strings.TrimSpace(strings.TrimPrefix(line, "```"))
			switch {
			case ex.program == "" && (info == "domain run" || (!runnable && strings.HasPrefix(info, "domain"))):
				fence = "domain"
			case info == "input" && ex.program != "" && ex.input == "" && ex.output == "":
				fence = "input"
			case info == "output" && ex.program != "" && ex.output == "":
				fence = "output"
			case ex.program != "":
				// Another block of some other kind: the example is over.
				return ex
			default:
				fence = "skip"
			}
			body = body[:0]
			continue
		}
		if !strings.HasPrefix(line, "```") {
			body = append(body, line)
			continue
		}
		text := strings.Join(body, "\n")
		switch fence {
		case "domain":
			ex.program = text
		case "input":
			ex.input = text
		case "output":
			ex.output = text
			return ex
		}
		fence = ""
	}
	return ex
}

var (
	builtinCall = regexp.MustCompile("`([a-z][a-z0-9_]*)\\(")
	mdEmphasis  = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdLink      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
)

// builtinEntries reads the builtin tables: rows of
// "| `name(args)` | `type` | behaviour |". A row that documents several
// spellings (`padleft(…)` / `padright(…)`) yields an entry for each name.
func builtinEntries(pages []docPage) []docEntry {
	var out []docEntry
	seen := map[string]bool{}
	for _, page := range pages {
		if !strings.HasPrefix(page.name, "ref-builtins-") {
			continue
		}
		for _, line := range page.lines {
			if !strings.HasPrefix(line, "| `") {
				continue
			}
			cells := splitTableRow(line)
			if len(cells) < 3 {
				continue
			}
			title := strings.ReplaceAll(cells[0], "`", "")
			for _, m := range builtinCall.FindAllStringSubmatch(cells[0], -1) {
				name := m[1]
				if seen[name] {
					continue
				}
				seen[name] = true
				out = append(out, docEntry{
					Kind:      "builtin",
					Name:      name,
					Title:     title,
					Signature: strings.ReplaceAll(cells[1], "`", ""),
					Summary:   plainMarkdown(strings.Join(cells[2:], " | ")),
					Page:      page.name,
				})
			}
		}
	}
	// A few builtins are documented in prose rather than in a table row —
	// `style(v, spec)` opens a paragraph, `pending` has a heading. Their entry
	// is that paragraph, so no builtin the type checker knows goes unanswered.
	for _, name := range typecheck.Builtins {
		if seen[name] {
			continue
		}
		if e, ok := proseBuiltin(pages, name); ok {
			seen[name] = true
			out = append(out, e)
		}
	}
	return out
}

// proseBuiltin finds the paragraph that introduces a builtin: one opening with
// a call to it, or the first under a heading that is its name.
func proseBuiltin(pages []docPage, name string) (docEntry, bool) {
	for _, page := range pages {
		if !strings.HasPrefix(page.name, "ref-builtins-") {
			continue
		}
		for i, line := range page.lines {
			title := ""
			start := i
			switch {
			case strings.HasPrefix(line, "`"+name+"("):
				title = line[1 : 1+strings.IndexByte(line[1:], '`')]
			case strings.HasPrefix(line, "#") && strings.TrimSpace(strings.TrimLeft(line, "#")) == "`"+name+"`":
				title = name
				for start = i + 1; start < len(page.lines) && strings.TrimSpace(page.lines[start]) == ""; start++ {
				}
			default:
				continue
			}
			var para []string
			for _, l := range page.lines[start:] {
				if strings.TrimSpace(l) == "" {
					break
				}
				para = append(para, strings.TrimSpace(l))
			}
			return docEntry{
				Kind:    "builtin",
				Name:    name,
				Title:   title,
				Summary: plainMarkdown(strings.Join(para, " ")),
				Page:    page.name,
			}, true
		}
	}
	return docEntry{}, false
}

// splitTableRow splits a Markdown table row on its unescaped pipes.
func splitTableRow(line string) []string {
	const escaped = "\x00"
	line = strings.ReplaceAll(strings.TrimSpace(line), `\|`, escaped)
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(strings.ReplaceAll(cells[i], escaped, "|"))
	}
	return cells
}

// plainMarkdown drops the inline markup a terminal would print literally.
func plainMarkdown(s string) string {
	s = mdEmphasis.ReplaceAllString(s, "$1")
	s = mdLink.ReplaceAllString(s, "$1")
	return strings.ReplaceAll(s, "`", "")
}

// docSearch answers a query: every entry whose name is exactly it, else the
// partial matches ranked name-prefix, name-contains, then mentioned-in-summary.
// A query written as a whole statement head (`Cursed Technique: Split`) or a
// call (`length(xs)`) is reduced to the name first.
func docSearch(query string) (exact, partial []docEntry) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if i := strings.Index(needle, ": "); i >= 0 {
		needle = strings.TrimSpace(needle[i+2:])
	}
	if i := strings.IndexByte(needle, '('); i > 0 {
		needle = needle[:i]
	}
	if needle == "" {
		return nil, nil
	}
	var prefix, contains, summary []docEntry
	for _, e := range docEntries() {
		name := strings.ToLower(e.Name)
		switch {
		case name == needle:
			exact = append(exact, e)
		case strings.HasPrefix(name, needle):
			prefix = append(prefix, e)
		case strings.Contains(name, needle):
			contains = append(contains, e)
		case strings.Contains(strings.ToLower(e.Summary), needle):
			summary = append(summary, e)
		}
	}
	return exact, append(append(prefix, contains...), summary...)
}

// docNearest is the "did you mean" for a query nothing contains: the names
// within a small edit distance, nearest first.
func docNearest(query string) []string {
	needle := strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		name string
		d    int
	}
	var near []scored
	seen := map[string]bool{}
	for _, e := range docEntries() {
		if seen[e.Name] {
			continue
		}
		seen[e.Name] = true
		if d := editDistance(needle, strings.ToLower(e.Name)); d <= max(2, len(needle)/3) {
			near = append(near, scored{e.Name, d})
		}
	}
	slices.SortStableFunc(near, func(a, b scored) int { return a.d - b.d })
	var out []string
	for i := 0; i < len(near) && i < 5; i++ {
		out = append(out, near[i].name)
	}
	return out
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur := make([]int, len(br)+1)
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(br)]
}

type docOptions struct {
	List bool
	JSON bool
}

func parseDocArgs(args []string) (string, docOptions, error) {
	var opts docOptions
	var words []string
	for _, a := range args {
		switch {
		case a == "--list" || a == "-l":
			opts.List = true
		case a == "--json":
			opts.JSON = true
		case strings.HasPrefix(a, "--"):
			return "", opts, fmt.Errorf("unknown flag %q", a)
		default:
			words = append(words, a)
		}
	}
	// The name is the rest of the line, so `domain doc Split Each` needs no
	// quoting — most primitives are more than one word.
	query := strings.Join(words, " ")
	if query == "" && !opts.List {
		return "", opts, fmt.Errorf("doc needs a name — a primitive (`domain doc Split Each`) or a builtin (`domain doc length`); --list shows every one")
	}
	return query, opts, nil
}

// Doc is `domain doc`. It exits 0 when it showed something and 1 when nothing
// matched.
func Doc(query string, opts docOptions, stdout, stderr io.Writer) int {
	color := isColorTerminal(stdout)
	var found []docEntry
	full := false // print whole entries rather than one line each
	if query == "" {
		found = docEntries()
	} else {
		exact, partial := docSearch(query)
		switch {
		case len(exact) > 0:
			found, full = exact, true
		case len(partial) == 1:
			found, full = partial, true
		default:
			found = partial
		}
	}
	if opts.List {
		full = false
	}

	if len(found) == 0 {
		fmt.Fprintf(stderr, "domain: nothing in the reference matches %q\n", query)
		if near := docNearest(query); len(near) > 0 {
			fmt.Fprintf(stderr, "  did you mean: %s?\n", strings.Join(near, ", "))
		}
		fmt.Fprintln(stderr, "  `domain doc --list` shows every primitive and builtin")
		return 1
	}
	if opts.JSON {
		if err := writeJSON(stdout, found); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		return 0
	}
	if full {
		for i := range found {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			fmt.Fprint(stdout, renderDocEntry(found[i], color))
		}
		return 0
	}
	if query != "" {
		fmt.Fprintf(stdout, "%d entries match %q:\n", len(found), query)
	}
	for _, e := range found {
		name := e.Title
		if e.Kind == "builtin" {
			name = e.Name
		}
		fmt.Fprintf(stdout, "  %s %s\n",
			pad(paintIf(color, styKeyword, truncateVis(name, 40)), 40),
			paintIf(color, styDim, truncateVis(e.Summary, 70)))
	}
	if query != "" {
		fmt.Fprintln(stdout, "name one exactly for its full entry")
	}
	return 0
}

// renderDocEntry is one entry, in full.
func renderDocEntry(e docEntry, color bool) string {
	var b strings.Builder
	kind := "primitive"
	if e.Kind == "builtin" {
		kind = "expression builtin"
	}
	fmt.Fprintf(&b, "%s  %s\n", paintIf(color, styKeyword, e.Title), paintIf(color, styDim, "("+kind+")"))
	if e.Signature != "" {
		fmt.Fprintf(&b, "  %s\n", paintIf(color, styType, e.Signature))
	}
	fmt.Fprintf(&b, "  %s\n", e.Summary)
	block := func(label, text string) {
		if text == "" {
			return
		}
		fmt.Fprintf(&b, "\n  %s\n", paintIf(color, styDim, label))
		for _, line := range strings.Split(text, "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	block("example", highlightSource(e.Example, color && e.Example != ""))
	block("input", e.Input)
	block("output", e.Output)
	if e.Page != "" {
		fmt.Fprintf(&b, "\n  %s\n", paintIf(color, styDim,
			"more: docs/"+e.Page+" — `domain expansion: documentation` serves it"))
	}
	return b.String()
}
