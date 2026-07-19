// Suggestion machinery: edit-distance "did you mean" lookups against the live
// primitive registry (never a hardcoded list — new primitives are suggestable
// the day they are registered), plus a table of type-conversion advice for
// type mismatches.
package diag

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/prims"
)

// structuralKeywords are statement forms that are not primitives but are
// valid at the start of a line.
var structuralKeywords = []string{"Channel", "Shikigami", "Simple Domain"}

// knownKeywords returns every keyword that may start a statement, in a stable
// order, derived from the primitive registry plus the structural forms.
func knownKeywords() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range prims.Registry {
		if !seen[p.Keyword] {
			seen[p.Keyword] = true
			out = append(out, p.Keyword)
		}
	}
	for _, k := range structuralKeywords {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// opsUnder returns the primitive IDs registered under a keyword.
func opsUnder(keyword string) []string {
	var out []string
	for _, p := range prims.Registry {
		if p.Keyword == keyword {
			out = append(out, p.ID)
		}
	}
	return out
}

// levenshtein is the classic edit distance, case-insensitive, two rolling rows.
func levenshtein(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// closest finds the candidate nearest to got, or "" when nothing is close
// enough to suggest with a straight face. The budget scales with the length
// of the word: short words tolerate 1–2 edits, long phrases a bit more.
func closest(got string, candidates []string) (string, int) {
	best, bestD := "", 1<<30
	for _, c := range candidates {
		if d := levenshtein(got, c); d < bestD {
			best, bestD = c, d
		}
	}
	budget := 2
	if n := len(got); n >= 8 {
		budget = n / 4
		if budget > 5 {
			budget = 5
		}
	}
	if best == "" || bestD > budget {
		return "", 0
	}
	return best, bestD
}

// suggestKeyword proposes a correction for an unknown statement keyword.
// A pure case mismatch ("cursed technique") is a certain match; otherwise
// edit distance decides.
func suggestKeyword(got string) (string, bool) {
	for _, k := range knownKeywords() {
		if strings.EqualFold(got, k) {
			return k, true
		}
	}
	s, d := closest(got, knownKeywords())
	if s == "" {
		return "", false
	}
	return s, d <= 2
}

// opSuggestion is the outcome of analysing an unknown operation phrase.
type opSuggestion struct {
	Keyword   string // the keyword the operation actually lives under
	Op        string // the primitive ID to write
	Confident bool
}

// suggestOperation proposes a correction for an operation phrase the resolver
// did not recognize under its keyword. Two intelligence layers:
//
//  1. The operation exists verbatim — under a different keyword. The user
//     wrote `Cursed Technique: Sum`; Sum is a Maximum Technique. Certain.
//  2. The leading words of the phrase are a near-miss of a primitive ID under
//     the same keyword ("Splitt Text by ..." → "Split"). Edit distance on the
//     matching word count decides, and short distances are auto-fixable.
func suggestOperation(keyword string, op *ast.Operation) *opSuggestion {
	if op == nil || len(op.Words) == 0 {
		return nil
	}

	// Layer 1: exact ID (case-insensitive) under any keyword.
	for _, p := range prims.Registry {
		if phraseStartsWith(op.Words, idWords(p.ID)) && p.Keyword != keyword {
			return &opSuggestion{Keyword: p.Keyword, Op: p.ID, Confident: true}
		}
	}

	// Layer 2: fuzzy against IDs under the same keyword, then everywhere.
	if s := fuzzyOp(op.Words, opsUnder(keyword)); s != nil {
		s.Keyword = keyword
		return s
	}
	for _, k := range knownKeywords() {
		if k == keyword {
			continue
		}
		if s := fuzzyOp(op.Words, opsUnder(k)); s != nil {
			s.Keyword = k
			s.Confident = false // moving keyword on a fuzzy match is a suggestion, not a repair
			return s
		}
	}
	return nil
}

// fuzzyOp compares the leading words of the phrase against each ID.
func fuzzyOp(words []string, ids []string) *opSuggestion {
	best, bestD := "", 1<<30
	for _, id := range ids {
		iw := idWords(id)
		if len(words) < len(iw) {
			continue
		}
		got := strings.Join(words[:len(iw)], " ")
		if d := levenshtein(got, strings.Join(iw, " ")); d < bestD {
			best, bestD = id, d
		}
	}
	if best == "" || bestD == 0 || bestD > 2 {
		return nil
	}
	return &opSuggestion{Op: best, Confident: bestD <= 2}
}

// idWords splits a primitive ID into its literal words, dropping single
// uppercase-letter placeholders ("Select Top K" matches "Select Top 3").
func idWords(id string) []string {
	var out []string
	for _, w := range strings.Fields(id) {
		if len(w) == 1 && w[0] >= 'A' && w[0] <= 'Z' {
			continue
		}
		out = append(out, w)
	}
	return out
}

// phraseStartsWith reports whether words begins with all of prefix,
// case-insensitively.
func phraseStartsWith(words, prefix []string) bool {
	if len(prefix) == 0 || len(words) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if !strings.EqualFold(words[i], p) {
			return false
		}
	}
	return true
}

// conversionAdvice maps a (produced → expected) type pair to the pipeline
// line that bridges it. Keys are the exact ir.Type String() forms.
var conversionAdvice = map[[2]string]string{
	{"List<Text>", "List<Int>"}:             `Channeled Energy: Convert To Integers`,
	{"List<List<Text>>", "List<List<Int>>"}: `Channeled Energy: Convert Each List to Integers`,
	{"Text", "List<Int>"}:                   `Cursed Technique: Extract Integers`,
	{"Text", "List<Text>"}:                  `Cursed Technique: Split Text by "\n"`,
	{"List<List<Int>>", "List<Int>"}:        `Cursed Technique: Flatten`,
	{"List<List<Text>>", "List<Text>"}:      `Cursed Technique: Flatten`,
	{"List<Text>", "List<Float>"}:           `Channeled Energy: Convert To Floats`,
	{"List<Int>", "List<Float>"}:            `Channeled Energy: Convert To Floats`,
}

// adviseConversion suggests the pipeline line that turns got into want, or ""
// when the gap has no single-step bridge.
func adviseConversion(got, want string) string {
	if line, ok := conversionAdvice[[2]string{got, want}]; ok {
		return fmt.Sprintf("insert `%s` before this line to convert %s into %s", line, got, want)
	}
	if strings.HasPrefix(got, "Grid") && strings.HasPrefix(want, "List") {
		return "grids and lists are different shapes; `Cursed Technique: Find Cells` or `Map Cells` extract values from a Grid"
	}
	if got == "Text" && strings.HasPrefix(want, "Grid") {
		return "insert `Cursed Technique: Split Text by \"\\n\"` then `Channeled Energy: Convert To Grid` to build a Grid from raw text"
	}
	return ""
}
