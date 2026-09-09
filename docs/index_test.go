package docs_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"domain/docs"
)

// Every page ships to the site, and the site's own list is the only thing
// that decides.
//
// docs/index.html holds the navigation, and a page missing from it is
// unreachable there — not a broken link, which somebody would notice, but a
// page nobody can find. `scopes.md`, `ref-builtins-view.md` and
// `ref-builtins-chance.md` were each written, linked from `README.md`, and
// left off the site for a whole milestone, which is exactly how quietly this
// fails.
//
// The three pages that are deliberately not in the navigation are named here,
// with the reason, so the exception is a decision rather than an oversight.
var notOnSite = map[string]string{
	"README.md":       "the site's own overview page, rendered as the landing view",
	"wasm/README.md":  "build instructions for the playground bundle, not a page",
	"aoc-gaps.md":     "a working document about what the language cannot do yet",
	"development.md":  "reached from the tooling page rather than listed twice",
	"tooling.md":      "listed under Tooling; see the navigation entry",
	"walkthroughs.md": "listed under Introduction; see the navigation entry",
}

var navFile = regexp.MustCompile(`file:\s*"([^"]+\.md)"`)

func TestEveryDocPageIsOnTheSite(t *testing.T) {
	index := docFile(t, "index.html")
	listed := map[string]bool{}
	for _, m := range navFile.FindAllStringSubmatch(index, -1) {
		listed[m[1]] = true
	}
	if len(listed) < 10 {
		t.Fatalf("only %d pages found in the navigation; the list's shape must have changed", len(listed))
	}

	pages, err := fs.Glob(docs.FS, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, p := range pages {
		if listed[p] {
			continue
		}
		if _, ok := notOnSite[p]; ok {
			continue
		}
		missing = append(missing, p)
	}
	if len(missing) > 0 {
		t.Errorf("these pages are in docs/ but not in the site navigation in docs/index.html:\n  %s\n\n"+
			"Add an entry to DOCS, or name the page in notOnSite with the reason it is not there.",
			strings.Join(missing, "\n  "))
	}

	// And the other direction: a navigation entry naming a page that is not
	// there is a dead link on every load of the site.
	for name := range listed {
		if _, err := fs.Stat(docs.FS, name); err != nil {
			t.Errorf("the navigation lists %q, which is not in docs/", name)
		}
	}
	// An excuse for a page that no longer exists is a stale excuse.
	for name := range notOnSite {
		if _, err := fs.Stat(docs.FS, name); err != nil {
			t.Errorf("notOnSite names %q, which is not in docs/", name)
		}
	}
}

// docLink matches a Markdown link to another page in docs/.
var docLink = regexp.MustCompile(`\(([a-z0-9-]+\.md)(?:#[^)]*)?\)`)

// The other index is the README, which is what a reader on GitHub sees. The
// property that matters there is not "listed in the table" — the reference
// pages are deliberately reached through primitives.md and expressions.md,
// which is what makes those two hubs rather than a flat list of twenty — but
// **reachable**: somebody who opens docs/README.md can get to every page by
// following links.
func TestEveryDocPageIsReachableFromTheReadme(t *testing.T) {
	seen := map[string]bool{"README.md": true}
	queue := []string{"README.md"}
	for len(queue) > 0 {
		page := queue[0]
		queue = queue[1:]
		for _, m := range docLink.FindAllStringSubmatch(docFile(t, page), -1) {
			if target := m[1]; !seen[target] {
				if _, err := fs.Stat(docs.FS, target); err != nil {
					continue // a link to somewhere else in the repository
				}
				seen[target] = true
				queue = append(queue, target)
			}
		}
	}

	pages, err := fs.Glob(docs.FS, "*.md")
	if err != nil {
		t.Fatal(err)
	}
	var unreachable []string
	for _, p := range pages {
		if !seen[p] {
			unreachable = append(unreachable, p)
		}
	}
	if len(unreachable) > 0 {
		t.Errorf("these pages cannot be reached by following links from docs/README.md:\n  %s\n\n"+
			"Link them from the page they belong under — the reference pages hang off primitives.md "+
			"and expressions.md rather than off the README's own table.",
			strings.Join(unreachable, "\n  "))
	}
}
