package prims

import "testing"

// TestCatalogCoversRegistry pins the documentation catalog to exactly the set
// of registered primitives: a new primitive cannot ship without a doc entry,
// and a stale entry cannot outlive its primitive. This is what lets the LSP
// trust Catalog for hover and completion.
func TestCatalogCoversRegistry(t *testing.T) {
	inRegistry := map[string]bool{}
	for _, p := range Registry {
		inRegistry[p.ID] = true
		d, ok := Catalog[p.ID]
		if !ok {
			t.Errorf("primitive %q (%s) has no Catalog entry", p.ID, p.Keyword)
			continue
		}
		if d.Keyword != p.Keyword {
			t.Errorf("%q: catalog keyword %q != registry keyword %q", p.ID, d.Keyword, p.Keyword)
		}
		if d.ID != p.ID {
			t.Errorf("%q: catalog ID field is %q", p.ID, d.ID)
		}
		if d.Signature == "" || d.Summary == "" || d.DocAnchor == "" {
			t.Errorf("%q: incomplete catalog entry %+v", p.ID, d)
		}
	}
	for id := range Catalog {
		if !inRegistry[id] {
			t.Errorf("Catalog documents %q, which is not in Registry", id)
		}
	}
}
