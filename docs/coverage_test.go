package docs_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"domain/docs"
)

// Every documented feature carries at least two examples that actually run.
//
// The neighbouring tests in docs_test.go check that the documented *surface*
// is the surface that exists. This one checks the teaching: a signature and a
// paragraph tell a reader what a primitive is for, and two worked examples
// tell them what it does — the second one usually carrying the edge the prose
// only names (the empty case, the clamping, the alternate argument form).
//
// It is deliberately a hard failure rather than a report. "Two examples per
// feature" is a promise the documentation makes to a reader, and a promise
// nothing enforces is a promise that decays one hurried edit at a time.

// exemptSections are the headings under which two runnable programs would be
// worse documentation, not better. Each needs a reason, and the reason has to
// be about the section rather than about the effort.
var exemptSections = map[string]string{
	// The source stage cannot be demonstrated apart from a program, and every
	// other example on the page already opens with one. Its own block is two
	// mutually exclusive lines, which is the clearest form it has.
	"Read Source": "demonstrated by every other example on the page",

	// Not a primitive: a shared argument convention, illustrated in place by
	// the primitives that accept one.
	"Measured arguments": "an argument convention, not a primitive",

	// Runs an external interpreter. An example here would fail on any machine
	// without python3 installed, and the harness has no per-example toolchain
	// guard — the other Foreign Block tests guard with exec.LookPath, which a
	// documentation example cannot do.
	"Foreign Block": "shells out to an external interpreter",
}

// sectionExamples counts the runnable examples under each heading of the given
// levels. primitives.md is asked for level 3 alone, since its level-2 headings
// are keyword-class group headers rather than features.
func sectionExamples(t *testing.T, page string, levels ...int) map[string]int {
	t.Helper()
	src := docFile(t, page)
	blocks := docs.Blocks(page, src)

	// Heading line numbers, so a block can be attributed to the section it
	// falls under. Both come from the same source, so the two agree.
	type heading struct {
		line  int
		level int
		name  string
	}
	// Both levels: primitives.md puts every feature under `###`, while
	// language.md and expressions.md use `##` for the major constructs and
	// `###` for what sits inside them. A block belongs to the nearest heading
	// above it of either level.
	var heads []heading
	for i, line := range strings.Split(src, "\n") {
		level := 3
		name, ok := strings.CutPrefix(line, "### ")
		if !ok {
			level = 2
			name, ok = strings.CutPrefix(line, "## ")
		}
		if !ok {
			continue
		}
		// The heading text up to the signature em dash is the feature name.
		if j := strings.Index(name, "—"); j >= 0 {
			name = name[:j]
		}
		heads = append(heads, heading{line: i + 1, level: level, name: strings.TrimSpace(name)})
	}

	want := map[int]bool{}
	for _, l := range levels {
		want[l] = true
	}
	counts := map[string]int{}
	for _, h := range heads {
		if want[h.level] {
			counts[h.name] = 0
		}
	}
	for _, b := range blocks {
		if b.Lang != "domain" || !b.Runnable() {
			continue
		}
		// The last heading at or above the block's line owns it.
		owner := ""
		for _, h := range heads {
			if h.line < b.Line && want[h.level] {
				owner = h.name
			}
		}
		if owner != "" {
			counts[owner]++
		}
	}
	return counts
}

// TestEveryPrimitiveHasTwoRunnableExamples is the worklist as much as the
// guard: while the reference is being filled in it names exactly what is
// still missing, and once it is full it stops anything from thinning out.
func TestEveryPrimitiveHasTwoRunnableExamples(t *testing.T) {
	const want = 2
	counts := map[string]int{}
	for _, page := range referencePages(t) {
		for name, n := range sectionExamples(t, page, 3) {
			counts[name] += n
		}
	}

	var short []string
	for name, got := range counts {
		if _, ok := exemptSections[name]; ok {
			continue
		}
		if got < want {
			short = append(short, fmt.Sprintf("%s (%d)", name, got))
		}
	}
	if len(short) > 0 {
		sort.Strings(short)
		t.Errorf("%d of %d sections in the ref-*.md pages have fewer than %d runnable examples:\n  %s",
			len(short), len(counts)-len(exemptSections), want, strings.Join(short, "\n  "))
	}
}

// An exemption that names a section which no longer exists is a stale excuse,
// and would silently stop covering a feature that got renamed into it.
func TestExemptionsNameRealSections(t *testing.T) {
	counts := map[string]int{}
	for _, page := range referencePages(t) {
		for name, n := range sectionExamples(t, page, 3) {
			counts[name] += n
		}
	}
	for name := range exemptSections {
		if _, ok := counts[name]; !ok {
			t.Errorf("exemptSections names %q, which is not a section of any ref-*.md page", name)
		}
	}
}

// The other two reference pages are covered by name rather than by walking
// every heading, because both mix features with commentary — "The two layers"
// and "Design rules for extending the table" are prose about the language, and
// a runnable example under them would be filler. Listing what must be covered
// keeps the judgement visible and reviewable instead of hiding it in a
// heading-shaped heuristic.
var coveredSections = map[string][]string{
	// The language constructs: everything a program is built out of that is
	// not a pipeline primitive.
	"language.md": {
		"Local bindings", "Measured arguments", "Channels", "Part",
		"Shikigami", "Parameters", "Declared signatures", "Innate Domain",
		"The prelude", "Simple Domain", "Binding Vows", "Reveal",
	},
	// The expression layer: its own constructs, then one entry per builtin
	// category — per the reference's own grouping, since two examples per
	// category is what makes a table of 161 rows navigable without turning
	// each row into a section.
	"expressions.md": {
		"Lambdas", "Pipeline bodies", "Conditional expressions",
		"Local bindings", "Updating a local", "Stage bindings",
	},
	"ref-builtins-list.md":        {"Lists, maps, grids", "First-order list operations"},
	"ref-builtins-collections.md": {"Maps and Sets", "Building and updating a collection"},
	"ref-builtins-math.md":        {"Math / number theory", "Floats"},
	"ref-builtins-text.md":        {"Text"},
	"ref-builtins-bits.md":        {"Bit operations", "Logic", "Number theory"},
	"ref-builtins-records.md":     {"Records", "Points and grid geometry", "Sparse grids"},
}

func TestEveryConstructAndBuiltinGroupHasTwoRunnableExamples(t *testing.T) {
	const want = 2
	for page, sections := range coveredSections {
		counts := sectionExamples(t, page, 2, 3)
		var short []string
		for _, name := range sections {
			got, ok := counts[name]
			if !ok {
				t.Errorf("%s: coveredSections names %q, which is not a section of that page", page, name)
				continue
			}
			if got < want {
				short = append(short, fmt.Sprintf("%s (%d)", name, got))
			}
		}
		if len(short) > 0 {
			sort.Strings(short)
			t.Errorf("%d of %d covered sections in %s have fewer than %d runnable examples:\n  %s",
				len(short), len(sections), page, want, strings.Join(short, "\n  "))
		}
	}
}
