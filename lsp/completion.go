package lsp

import (
	"encoding/json"
	"strings"
	"unicode/utf16"

	"domain/prims"
)

// LSP CompletionItemKind values we use.
const (
	kindFunction = 3
	kindField    = 5
	kindKeyword  = 14
	kindValue    = 12
	kindProperty = 10
)

// keywordDoc documents a themed statement keyword — the head of a pipeline
// line. Kept here (surface syntax) rather than in package prims (semantics).
type keywordDoc struct {
	Keyword string
	Summary string
}

// statementKeywords are the themed heads a line can start with, in a sensible
// completion order (pipeline stages first, then structure).
var statementKeywords = []keywordDoc{
	{"Cursed Energy", "Source stage: read input (must be first)."},
	{"Cursed Technique", "Transform stage: reshape the current value (Split, Map Each, Filter, …)."},
	{"Channeled Energy", "Coercion stage: convert between value kinds (Convert To Integers/Grid/Set, …)."},
	{"Maximum Technique", "Reduction stage: collapse a collection (Sum, Count, Group By, Sort-adjacent, …)."},
	{"Domain Expansion", "Named algorithm — a request the optimizer may satisfy differently (Sort, BFS, Dijkstra, …)."},
	{"Reverse Cursed Technique", "Inversion stage (Reverse)."},
	{"Simple Domain", "Control flow: Repeat N / While / Iterate Until Fixed Point over a loop body."},
	{"Channel", "Branch a named sub-pipeline from the current value; recombine it later with From:."},
	{"Shikigami", "Define or call a user operation composed from primitives."},
	{"Binding Vow", "Assert an invariant about the current value (stripped under --release)."},
	{"Reveal", "Output sink: write the current value (e.g. Reveal: stdout)."},
}

// keywordsWithPrimitives is the subset that take an operation phrase, so the
// completion knows when to offer primitives after the colon.
var keywordsWithPrimitives = map[string]bool{
	"Cursed Energy": true, "Cursed Technique": true, "Channeled Energy": true,
	"Maximum Technique": true, "Domain Expansion": true, "Reverse Cursed Technique": true,
	"Reveal": true, "Binding Vow": true,
}

// argLabel documents a named argument (the indented `Key:` continuations).
type argLabel struct {
	Label   string
	Summary string
}

// argLabels documents the arguments worth explaining. It is a subset of
// prims.ArgNames() on purpose — a dimension like `Row:` or `Width:` reads as
// itself and a one-line gloss adds nothing — and TestArgLabelsAreRealArguments
// pins that every entry here is an argument the vocabulary actually reads.
var argLabels = []argLabel{
	{"Using", "The lambda for this operation, e.g. `Using: (x) -> x > 2`."},
	{"Mode", "Variant selector (One/Each/Try/Scan for Match Pattern; Filter/Count/First/Map for combinatorics; the search modes for Explore)."},
	{"Seed", "The accumulator's initial value and type for Fold."},
	{"Params", "Names the parameters an indented body stands in for, e.g. `Params: acc, row` on a Fold."},
	{"Case", "One tagged alternative of a Match Pattern, e.g. `Case: toggle \"toggle {a:int},{b:int}\"`. Repeat it; order is priority order."},
	{"From", "Names the channel(s) a consumer draws from (Combine, Zip, Difference, Fold)."},
	{"Cost", "The per-step weight for Explore's Cheapest and Costs modes."},
	{"Until", "The stopping condition for Explore and the loop drivers."},
	{"Value", "The per-state value Explore's Tally folds."},
	{"Combine", "How Explore's Tally folds two values reaching one state."},
	{"While", "Unfold's predicate: it decides when the generated sequence stops."},
	{"Default", "The background value (and element type) of a sparse plane."},
	{"Mark", "The value written at each supplied point when building a sparse plane."},
	{"Fill", "The value written into the border Pad Grid adds."},
}

// modeValues are the identifiers that follow `Mode:`, across every primitive
// that takes one — the completion has no way to know which stage the cursor is
// under, so it offers the union and lets the resolver reject a wrong one.
//
// Hand-maintained: each primitive validates its own values in its Build (a
// switch, a map, an if), so there is no list to derive this from. Grouped by
// the primitive that reads them, so a new mode has an obvious home.
var modeValues = []string{
	"One", "Each", "Try", "Scan", // Match Pattern
	"Filter", "Count", "First", "Map", // All Pairs, Combinations
	"Collect", "Distances", "Steps", "Cheapest", "Costs", "Tally", // Explore
	"Sum", "Max", "Min", "Product", // Sliding Reduce
	"Right", "Left", "Half", // Rotate Grid
	"Horizontal", "Vertical", // Flip Grid
}

// completion returns context-aware completion items for the cursor position.
func (s *Server) completion(params json.RawMessage) any {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	doc, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return nil
	}
	prefix := linePrefix(doc.text, p.Position.Line, p.Position.Character)
	items := CompletionItems(prefix)
	return map[string]any{"isIncomplete": false, "items": items}
}

// linePrefix returns the text of the given 0-based line up to the character
// column (clamped), which is all the context the completion needs. Per the LSP
// spec, char is a UTF-16 code-unit offset into the line, so it is converted to
// a byte offset before slicing the UTF-8 string.
func linePrefix(text string, line, char int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	l := lines[line]
	return l[:utf16OffsetToBytes(l, char)]
}

// utf16OffsetToBytes converts an LSP UTF-16 code-unit offset into a byte
// offset into the UTF-8 string s, clamped to [0, len(s)] and never landing
// inside a multi-byte rune. Runes outside the BMP count as two UTF-16 units
// (a surrogate pair); an offset falling between the two halves resolves to
// the end of that rune, keeping the slice on a rune boundary.
func utf16OffsetToBytes(s string, units int) int {
	for i, r := range s {
		if units <= 0 {
			return i
		}
		units -= utf16.RuneLen(r)
	}
	return len(s)
}

// CompletionItems is the pure decision function (tested directly, and
// reused directly by the REPL's tab completion — see
// cmd/domain/repl_complete.go): given the text before the cursor on a
// line, decide what to offer.
func CompletionItems(prefix string) []map[string]any {
	indented := len(prefix) > 0 && (prefix[0] == ' ' || prefix[0] == '\t')
	trimmed := strings.TrimSpace(prefix)

	// Indented continuation line → named arguments (and, after Mode:, its values).
	if indented {
		if key, ok := splitKeyword(trimmed); ok && strings.EqualFold(key, "Mode") {
			out := make([]map[string]any, 0, len(modeValues))
			for _, v := range modeValues {
				out = append(out, map[string]any{"label": v, "kind": kindValue,
					"detail": "Mode value", "insertText": v})
			}
			return out
		}
		return argItems()
	}

	// Non-indented line with a keyword and a colon → offer that keyword's
	// primitives (the operation phrase).
	if key, ok := splitKeyword(trimmed); ok {
		if canon, matched := canonicalKeyword(key); matched && keywordsWithPrimitives[canon] {
			return primitiveItems(canon)
		}
	}

	// Otherwise the cursor is at the head of a line. Both spellings are valid
	// there — the themed keyword, or the bare operation phrase that infers it —
	// so offer the keywords first and then every primitive.
	return append(keywordItems(), bareOperationItems()...)
}

// splitKeyword returns the text of a trimmed line before its first ':'. ok is
// false when there is no colon yet.
func splitKeyword(trimmed string) (key string, ok bool) {
	i := strings.IndexByte(trimmed, ':')
	if i < 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[:i]), true
}

// canonicalKeyword resolves a (possibly differently-cased) keyword to its
// canonical spelling.
func canonicalKeyword(key string) (string, bool) {
	for _, kw := range statementKeywords {
		if strings.EqualFold(kw.Keyword, key) {
			return kw.Keyword, true
		}
	}
	return "", false
}

func keywordItems() []map[string]any {
	out := make([]map[string]any, 0, len(statementKeywords))
	for i, kw := range statementKeywords {
		out = append(out, map[string]any{
			"label":         kw.Keyword,
			"kind":          kindKeyword,
			"detail":        "keyword",
			"documentation": md(kw.Summary),
			"insertText":    kw.Keyword + ": ",
			"sortText":      sortKey(i),
		})
	}
	return out
}

func argItems() []map[string]any {
	out := make([]map[string]any, 0, len(argLabels))
	for i, a := range argLabels {
		out = append(out, map[string]any{
			"label":         a.Label + ":",
			"kind":          kindProperty,
			"detail":        "argument",
			"documentation": md(a.Summary),
			"insertText":    a.Label + ": ",
			"sortText":      sortKey(i),
		})
	}
	return out
}

// bareOperationItems lists every primitive as a prefix-free statement head,
// sorted after the keywords so that a user who wants the themed spelling still
// meets it first. The detail line names the keyword each one infers.
func bareOperationItems() []map[string]any {
	out := make([]map[string]any, 0, len(prims.Registry))
	i := 0
	for _, prim := range prims.Registry {
		doc, _ := prims.Doc(prim.ID)
		// A primitive is offered under each of its writable spellings, which is
		// its ID unless it says otherwise: completing a foreign block has to
		// insert `Python`, since `Foreign Block` is a name for the construct
		// and not a phrase anyone can write.
		for _, phrase := range prim.Spellings() {
			out = append(out, map[string]any{
				"label":         phrase,
				"kind":          kindFunction,
				"detail":        prim.Keyword + " — " + doc.Signature,
				"documentation": md(doc.Summary + "\n\nThe `" + prim.Keyword + ":` keyword is optional.\n\n_See primitives.md#" + doc.DocAnchor + "_"),
				"insertText":    phrase,
				"sortText":      sortKey(len(statementKeywords) + i),
			})
			i++
		}
	}
	return out
}

// primitiveItems lists the primitives registered under a keyword, richest
// first, drawn from the shared documentation catalog.
func primitiveItems(keyword string) []map[string]any {
	var out []map[string]any
	i := 0
	for _, prim := range prims.Registry {
		if prim.Keyword != keyword {
			continue
		}
		doc, _ := prims.Doc(prim.ID)
		for _, phrase := range prim.Spellings() { // see bareOperationItems
			out = append(out, map[string]any{
				"label":         phrase,
				"kind":          kindFunction,
				"detail":        doc.Signature,
				"documentation": md(doc.Summary + "\n\n_See primitives.md#" + doc.DocAnchor + "_"),
				"insertText":    phrase,
				"sortText":      sortKey(i),
			})
			i++
		}
	}
	return out
}

// md wraps a string as an LSP MarkupContent value.
func md(value string) map[string]any {
	return map[string]any{"kind": "markdown", "value": value}
}

// sortKey preserves the curated order by prefixing with a zero-padded index.
func sortKey(i int) string {
	const digits = "0123456789"
	return string([]byte{digits[(i/10)%10], digits[i%10]})
}
