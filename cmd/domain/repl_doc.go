// `:doc` — the primitive catalog, from inside the session.
//
// The binary already carries documentation for every primitive: the same
// catalog the language server hovers with (prims.Catalog), a signature and a
// sentence each, pinned by a test to exactly the registered set. The REPL is
// where that is most wanted — "what does Fold take again" is a question asked
// mid-pipeline, and answering it should not cost a trip to a browser.
//
// `:doc <name>` prints one entry. Bare `:doc` opens the catalog as a filtered
// list: type to narrow, Enter to put that primitive's statement on the prompt
// ready to be finished.
//
// When `:docs` is serving the documentation site, each entry's name is also a
// hyperlink into the full reference page for it — the catalog's DocAnchor is
// exactly that link.
package main

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"domain/prims"
)

// docBrowser is the :doc overlay: a filter over the catalog and the entry it
// currently has selected.
type docBrowser struct {
	query   string
	matches []prims.PrimDoc
	cursor  int
	keys    docKeyMap
}

type docKeyMap struct {
	Up, Down, Accept, Cancel key.Binding
}

func defaultDocKeys() docKeyMap {
	return docKeyMap{
		Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
		Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
		Accept: key.NewBinding(key.WithKeys("enter", "tab")),
		Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
	}
}

// newDocBrowser opens the catalog, unfiltered.
func newDocBrowser() *docBrowser {
	b := &docBrowser{keys: defaultDocKeys()}
	b.filter("")
	return b
}

// filter re-runs the search and puts the cursor back at the best match.
//
// Matching is substring, ranked: the primitives whose *name* starts with the
// query, then those whose name contains it, then those whose summary mentions
// it. That is deliberately not fuzzy matching — bubbles/list brings that, and
// with it a module this repository would have to vendor and pin a new Nix hash
// for, to search a hundred short names that substring already finds.
func (b *docBrowser) filter(query string) {
	b.query, b.cursor = query, 0
	needle := strings.ToLower(strings.TrimSpace(query))
	var prefix, contains, summary []prims.PrimDoc
	for _, doc := range sortedCatalog() {
		name := strings.ToLower(doc.Keyword + ": " + doc.ID)
		id := strings.ToLower(doc.ID)
		switch {
		case needle == "":
			prefix = append(prefix, doc)
		case strings.HasPrefix(id, needle):
			prefix = append(prefix, doc)
		case strings.Contains(name, needle):
			contains = append(contains, doc)
		case strings.Contains(strings.ToLower(doc.Summary), needle):
			summary = append(summary, doc)
		}
	}
	b.matches = append(append(prefix, contains...), summary...)
}

// selected is the entry on show, and whether there is one.
func (b *docBrowser) selected() (prims.PrimDoc, bool) {
	if b.cursor >= len(b.matches) {
		return prims.PrimDoc{}, false
	}
	return b.matches[b.cursor], true
}

// statementFor is the line a primitive is written as, ready to be finished.
func statementFor(doc prims.PrimDoc) string { return doc.Keyword + ": " + doc.ID }

// update handles one keystroke. It reports whether the browser is still open
// and, when it is not, the statement to put on the prompt.
func (b *docBrowser) update(msg tea.KeyPressMsg) (open bool, statement string) {
	switch {
	case key.Matches(msg, b.keys.Cancel):
		return false, ""
	case key.Matches(msg, b.keys.Accept):
		if doc, ok := b.selected(); ok {
			return false, statementFor(doc)
		}
		return false, ""
	case key.Matches(msg, b.keys.Up):
		if b.cursor > 0 {
			b.cursor--
		}
		return true, ""
	case key.Matches(msg, b.keys.Down):
		if b.cursor < len(b.matches)-1 {
			b.cursor++
		}
		return true, ""
	case msg.String() == "backspace":
		if b.query != "" {
			b.filter(b.query[:len(b.query)-1])
		}
		return true, ""
	}
	if msg.Text != "" {
		b.filter(b.query + msg.Text)
	}
	return true, ""
}

// view draws the catalog: the filter, the matches, and the selected entry's
// summary in full underneath.
func (b *docBrowser) view(width, height int) string {
	var out strings.Builder
	out.WriteString(styTitle.Render("primitives") + styDim.Render(fmt.Sprintf("  %d of %d", len(b.matches), len(prims.Catalog))) + "\n")
	out.WriteString(styHeading.Render("filter: ") + b.query + "\n\n")

	rows := max(height-8, 3)
	start := max(min(b.cursor-rows/2, len(b.matches)-rows), 0)
	end := min(start+rows, len(b.matches))
	nameWidth := max(min(width-30, 44), 20)
	for i := start; i < end; i++ {
		doc := b.matches[i]
		name := pad(truncateVis(statementFor(doc), nameWidth), nameWidth)
		row := "  " + name + " " + styDim.Render(truncateVis(doc.Signature, max(width-nameWidth-6, 10)))
		if i == b.cursor {
			row = styCursor.Render("› "+name) + " " + styType.Render(truncateVis(doc.Signature, max(width-nameWidth-6, 10)))
		}
		out.WriteString(row + "\n")
	}

	if doc, ok := b.selected(); ok {
		out.WriteString("\n" + truncateVis(doc.Summary, max(width-2, 20)) + "\n")
		if base := docsBaseURL(); base != "" {
			out.WriteString(styDim.Render(docsLink(base+"#/primitives#"+doc.DocAnchor, "primitives", true)) + "\n")
		}
	}
	out.WriteString(styDim.Render("type to filter · ↑/↓ move · enter puts the statement on the prompt · esc closes"))
	return out.String()
}

// sortedCatalog orders the catalog by keyword, then by name — the order the
// documentation itself is in, rather than a map's.
func sortedCatalog() []prims.PrimDoc {
	docs := make([]prims.PrimDoc, 0, len(prims.Catalog))
	for _, d := range prims.Catalog {
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Keyword != docs[j].Keyword {
			return docs[i].Keyword < docs[j].Keyword
		}
		return docs[i].ID < docs[j].ID
	})
	return docs
}

// docLookup answers `:doc <name>` from the catalog: an exact match on the
// primitive's name, else every entry whose name or summary mentions it.
func docLookup(query string) []prims.PrimDoc {
	needle := strings.ToLower(strings.TrimSpace(query))
	var partial []prims.PrimDoc
	for _, doc := range sortedCatalog() {
		name := strings.ToLower(doc.ID)
		if name == needle {
			return []prims.PrimDoc{doc}
		}
		if strings.Contains(name, needle) || strings.Contains(strings.ToLower(doc.Summary), needle) {
			partial = append(partial, doc)
		}
	}
	return partial
}

// doc prints one primitive's documentation, or the shortlist a vague query
// matched. Bare `:doc` is handled by the editor, which opens the browser.
func (r *repl) doc(query string) {
	if strings.TrimSpace(query) == "" {
		fmt.Fprintln(r.out, "usage: :doc <primitive> (bare :doc opens the catalog on a terminal)")
		return
	}
	found := docLookup(query)
	switch len(found) {
	case 0:
		fmt.Fprintf(r.out, "no primitive matches %q (:doc with no argument lists them all)\n", query)
	case 1:
		fmt.Fprint(r.out, renderDoc(found[0], r.color))
	default:
		fmt.Fprintf(r.out, "%d primitives match %q:\n", len(found), query)
		for _, doc := range found {
			fmt.Fprintf(r.out, "  %s %s\n",
				pad(paintIf(r.color, styKeyword, doc.Keyword+": "+doc.ID), 44),
				paintIf(r.color, styDim, truncateVis(doc.Summary, 60)))
		}
	}
}

// renderDoc is one catalog entry, in full.
func renderDoc(doc prims.PrimDoc, color bool) string {
	var b strings.Builder
	name := doc.Keyword + ": " + doc.ID
	if color {
		name = docsLink(styKeyword.Render(name), "primitives", color)
	}
	fmt.Fprintf(&b, "%s\n", name)
	fmt.Fprintf(&b, "  %s\n", paintIf(color, styType, doc.Signature))
	fmt.Fprintf(&b, "  %s\n", doc.Summary)
	if base := docsBaseURL(); base != "" {
		fmt.Fprintf(&b, "  %s\n", paintIf(color, styDim, base+"#/primitives#"+doc.DocAnchor))
	} else {
		fmt.Fprintf(&b, "  %s\n", paintIf(color, styDim, "primitives.md#"+doc.DocAnchor+" — `:docs` serves it locally"))
	}
	return b.String()
}
