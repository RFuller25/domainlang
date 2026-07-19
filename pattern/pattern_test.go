package pattern

import (
	"strings"
	"testing"

	"domain/ir"
)

func mustParse(t *testing.T, s string) *Template {
	t.Helper()
	tmpl, err := ParseTemplate(s)
	if err != nil {
		t.Fatalf("ParseTemplate(%q): %v", s, err)
	}
	return tmpl
}

func TestNamedHolesProduceRecord(t *testing.T) {
	tmpl := mustParse(t, "{a:int}-{b:int}")
	if !tmpl.Named {
		t.Fatal("expected named template")
	}
	want := ir.Record(ir.Field{Name: "a", Type: ir.Int()}, ir.Field{Name: "b", Type: ir.Int()})
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Fatalf("output type: got %s want %s", got, want)
	}
}

func TestPositionalHomogeneousProducesList(t *testing.T) {
	tmpl := mustParse(t, "{int}-{int}")
	if tmpl.Named {
		t.Fatal("expected positional template")
	}
	want := ir.List(ir.Int())
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Fatalf("output type: got %s want %s", got, want)
	}
}

func TestPositionalMixedProducesTuple(t *testing.T) {
	tmpl := mustParse(t, "{word} {int}")
	want := ir.Tuple(ir.Text(), ir.Int())
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Fatalf("output type: got %s want %s", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"{a:int}-{int}",   // mixes named and positional
		"{a:float}",       // unknown hole type
		"{int",            // unterminated hole
		"abc",             // no holes
		"{:int}",          // empty name
		"a}b",             // stray close brace
		"{a:int}-{a:int}", // duplicate named hole
		"{my name:int}",   // invalid identifier (space)
		"{a-b:int}",       // invalid identifier (hyphen)
	}
	for _, c := range cases {
		if _, err := ParseTemplate(c); err == nil {
			t.Fatalf("expected error parsing %q", c)
		}
	}
}

// TestDuplicateNamedHoleRejected is a regression test: ParseTemplate used to
// accept a repeated hole name (e.g. "{a:int}-{a:int}"), producing a Record
// type with a duplicate field. Because ir.RecordValue.Set does last-write-wins
// and Go's regexp package (unlike most regex engines) permits duplicate named
// capture groups, this silently discarded the first capture at runtime with
// no error anywhere in the pipeline.
func TestDuplicateNamedHoleRejected(t *testing.T) {
	_, err := ParseTemplate("{a:int}-{a:int}")
	if err == nil {
		t.Fatal("expected an error for a template with a duplicate named hole")
	}
}

// TestInvalidHoleNameRejected is a regression test: a hole name that isn't a
// legal identifier (e.g. containing a space or hyphen) used to pass
// ParseTemplate and only fail later, at CompileRegex time, with a raw Go
// regexp-internals error ("error parsing regexp: invalid named capture: ...")
// because the name is interpolated directly into a "(?P<name>...)" capture
// group. parseHole now validates the name up front and reports a clean,
// domain-specific error that names the offending hole.
func TestInvalidHoleNameRejected(t *testing.T) {
	cases := []string{"{my name:int}", "{a-b:int}"}
	for _, c := range cases {
		_, err := ParseTemplate(c)
		if err == nil {
			t.Fatalf("expected an error for template %q with an invalid hole name", c)
		}
		if strings.Contains(err.Error(), "error parsing regexp") {
			t.Fatalf("ParseTemplate(%q) leaked a raw regexp error instead of a clean diagnostic: %v", c, err)
		}
	}
}

// TestRegexLoweringMatchesAnchorCases validates that the regex lowering matches
// the exact inputs the design note promises (docs/match-pattern.md).
func TestRegexLoweringMatchesAnchorCases(t *testing.T) {
	cases := []struct {
		template string
		input    string
		groups   []string // expected captures (group 1..n)
	}{
		{"{a:int}-{b:int},{c:int}-{d:int}", "2-4,6-8", []string{"2", "4", "6", "8"}},
		{"{dir:word} {n:int}", "forward 5", []string{"forward", "5"}},
		{"move {n:int} from {src:int} to {dst:int}", "move 1 from 2 to 1", []string{"1", "2", "1"}},
		{"{lo:int}-{hi:int} {ch:word}: {pw:text}", "1-3 a: abcde", []string{"1", "3", "a", "abcde"}},
		{"{int}x{int}x{int}", "2x3x4", []string{"2", "3", "4"}},
	}
	for _, c := range cases {
		tmpl := mustParse(t, c.template)
		re, err := tmpl.CompileRegex()
		if err != nil {
			t.Fatalf("CompileRegex(%q): %v", c.template, err)
		}
		m := re.FindStringSubmatch(c.input)
		if m == nil {
			t.Fatalf("template %q did not match input %q (regex %q)", c.template, c.input, re.String())
		}
		got := m[1:]
		if len(got) != len(c.groups) {
			t.Fatalf("template %q: got %d captures, want %d", c.template, len(got), len(c.groups))
		}
		for i := range c.groups {
			if got[i] != c.groups[i] {
				t.Fatalf("template %q capture %d: got %q want %q", c.template, i, got[i], c.groups[i])
			}
		}
		// Capture count must equal the hole count.
		if len(tmpl.Holes) != len(c.groups) {
			t.Fatalf("template %q: %d holes but %d expected captures", c.template, len(tmpl.Holes), len(c.groups))
		}
	}
}

// TestRegexRejectsNonMatch confirms anchoring: a line that doesn't fit fails.
func TestRegexRejectsNonMatch(t *testing.T) {
	tmpl := mustParse(t, "{a:int}-{b:int}")
	re, err := tmpl.CompileRegex()
	if err != nil {
		t.Fatal(err)
	}
	if re.FindStringSubmatch("2-4 extra") != nil {
		t.Fatal("anchored template should not match trailing garbage")
	}
	if re.FindStringSubmatch("x-4") != nil {
		t.Fatal("{int} should not match non-numeric text")
	}
}
