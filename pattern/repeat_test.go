package pattern

import (
	"strings"
	"testing"

	"domain/ir"
)

// Repetition holes: `{ns:int+ sep=", "}` captures one or more values and
// yields a List. The separator is required rather than defaulted, because a
// default would be right about half the time and a template that silently
// matches the wrong thing is worse than one that asks.

func TestRepetitionHoleTypesAndMatches(t *testing.T) {
	tmpl, err := ParseTemplate(`{id:word}: {vals:int+ sep=","}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := ir.Record(
		ir.Field{Name: "id", Type: ir.Text()},
		ir.Field{Name: "vals", Type: ir.List(ir.Int())},
	)
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Errorf("output type: got %s, want %s", got, want)
	}
	re, err := tmpl.CompileRegex()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := re.FindStringSubmatch("target: 1,2,3")
	if m == nil {
		t.Fatal("the template did not match")
	}
	if m[2] != "1,2,3" {
		t.Errorf("repeated capture: got %q, want the whole run %q", m[2], "1,2,3")
	}
	// One element is still a match: `+` is one-or-more.
	if re.FindStringSubmatch("solo: 7") == nil {
		t.Error("a single element should still match a repeated hole")
	}
}

// A word element is `\S+`, which would eat a separator containing no space —
// so the element pattern excludes the separator's first byte. Without that,
// `a,b,c` captures as one element rather than three.
func TestRepeatedWordsStopAtTheSeparator(t *testing.T) {
	tmpl, err := ParseTemplate(`{ws:word+ sep=","}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	re, _ := tmpl.CompileRegex()
	m := re.FindStringSubmatch("a,b,c")
	if m == nil {
		t.Fatal("no match")
	}
	if got := tmpl.Holes[0].Split(m[1]); len(got) != 3 {
		t.Errorf("split gave %d elements (%v), want 3", len(got), got)
	}
}

// A space separator, which is the shape AoC 2023 day 6 arrives in and the one
// worked case in the reference that no test reached. It is worth its own case
// because an int element and a space separator are the pair most likely to run
// together: the element pattern has to stop at the separator here too, and the
// template matches the single spaces literally — a run of them is what
// `Split Fields` is for, and match-pattern.md says so.
func TestRepeatedIntsWithASpaceSeparator(t *testing.T) {
	tmpl, err := ParseTemplate(`{label:word}: {ns:int+ sep=" "}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := ir.Record(
		ir.Field{Name: "label", Type: ir.Text()},
		ir.Field{Name: "ns", Type: ir.List(ir.Int())},
	)
	if got := tmpl.OutputType(); !got.Equal(want) {
		t.Errorf("output type: got %s, want %s", got, want)
	}
	re, err := tmpl.CompileRegex()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := re.FindStringSubmatch("Time: 7 15 30")
	if m == nil {
		t.Fatal("the template did not match `Time: 7 15 30`")
	}
	if m[1] != "Time" {
		t.Errorf("label: got %q, want %q", m[1], "Time")
	}
	got := tmpl.Holes[1].Split(m[2])
	if len(got) != 3 || got[0] != "7" || got[1] != "15" || got[2] != "30" {
		t.Errorf("split gave %v, want [7 15 30]", got)
	}
	// The separator is matched literally, so the column-aligned spelling of the
	// same line is not this template's job.
	if re.FindStringSubmatch("Time:      7  15   30") != nil {
		t.Error("padded columns matched a literal single-space separator")
	}
}

// A positional template's homogeneity has to count repetition: a repeated hole
// and a plain one of the same scalar type are List<Int> and Int, which are
// different types and so a Tuple rather than a List.
func TestRepetitionAffectsPositionalHomogeneity(t *testing.T) {
	tmpl, err := ParseTemplate(`{int} {int+ sep=","}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := tmpl.OutputType()
	if got.Kind != ir.KTuple {
		t.Fatalf("output type: got %s, want a Tuple", got)
	}
	want := ir.Tuple(ir.Int(), ir.List(ir.Int()))
	if !got.Equal(want) {
		t.Errorf("output type: got %s, want %s", got, want)
	}
}

func TestRepetitionRefusals(t *testing.T) {
	for _, tc := range []struct{ tmpl, want string }{
		{`{ns:int+}`, "names no separator"},
		{`{ns:int+ sep=,}`, "not a quoted string"},
		{`{ns:int+ sep=""}`, "empty separator"},
		// A text hole is greedy to the next literal, so a repeated one would
		// swallow its own separators and capture the run as element zero.
		{`{ts:text+ sep=","}`, "greedy to the next literal"},
	} {
		if _, err := ParseTemplate(tc.tmpl); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected an error containing %q, got %v", tc.tmpl, tc.want, err)
		}
	}
}

// The compiler builds the unnamed form of the same pattern. Two lowerings of
// one template is how a compiled program could parse differently from the
// interpreted one, so there is only the one — this pins that the two spellings
// differ in nothing but the group names.
func TestRegexSourceNamedAndUnnamedAgree(t *testing.T) {
	for _, src := range []string{
		`{a:int}-{b:int}`,
		`{id:word}: {vals:int+ sep=", "}`,
		`{int}x{int}x{int}`,
		`{w:word} {n:int}`,
	} {
		tmpl, err := ParseTemplate(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		named, plain := tmpl.RegexSource(true), tmpl.RegexSource(false)
		if strings.Count(named, "(") != strings.Count(plain, "(") {
			t.Errorf("%s: group counts differ\nnamed: %s\nplain: %s", src, named, plain)
		}
		// Stripping the name prefixes turns one into the other exactly.
		stripped := named
		for _, h := range tmpl.Holes {
			stripped = strings.Replace(stripped, "(?P<"+h.Name+">", "(", 1)
		}
		if stripped != plain {
			t.Errorf("%s: the two lowerings differ\nnamed→plain: %s\nplain:       %s",
				src, stripped, plain)
		}
	}
}
