package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"domain/prims"
	"domain/typecheck"
)

// Every primitive in the catalog and every builtin the type checker knows can
// be looked up by name. The builtins are read out of the reference pages'
// tables, so this is also what notices a builtin nobody wrote a row for.
func TestDocCoversTheVocabulary(t *testing.T) {
	have := map[string]bool{}
	for _, e := range docEntries() {
		have[e.Kind+":"+e.Name] = true
	}
	for id := range prims.Catalog {
		if !have["primitive:"+id] {
			t.Errorf("no entry for the primitive %q", id)
		}
	}
	for _, name := range typecheck.Builtins {
		if !have["builtin:"+name] {
			t.Errorf("no entry for the builtin %q — is it missing from the docs/ref-builtins-*.md tables?", name)
		}
	}
}

func TestDocSearch(t *testing.T) {
	cases := []struct {
		query     string
		wantExact []string // kind:name
	}{
		{"Split Each", []string{"primitive:Split Each"}},
		{"split each", []string{"primitive:Split Each"}},
		{"Cursed Technique: Split Each", []string{"primitive:Split Each"}},
		{"length(xs)", []string{"builtin:length"}},
		// One word, two things: the reduction and the expression builtin.
		{"sum", []string{"primitive:Sum", "builtin:sum"}},
	}
	for _, c := range cases {
		exact, _ := docSearch(c.query)
		var got []string
		for _, e := range exact {
			got = append(got, e.Kind+":"+e.Name)
		}
		if strings.Join(got, ",") != strings.Join(c.wantExact, ",") {
			t.Errorf("docSearch(%q) exact = %v, want %v", c.query, got, c.wantExact)
		}
	}

	if exact, partial := docSearch("grid"); len(exact) != 0 || len(partial) < 3 {
		t.Errorf("docSearch(grid): %d exact, %d partial; want a shortlist", len(exact), len(partial))
	}
	if near := docNearest("splt"); len(near) == 0 || near[0] != "Split" {
		t.Errorf("docNearest(splt) = %v, want Split first", near)
	}
}

// A primitive's entry carries the worked example from its reference page, and
// prefers the runnable one — the block with its input and output beside it.
func TestDocExampleComesFromTheReference(t *testing.T) {
	exact, _ := docSearch("Split Each")
	if len(exact) != 1 {
		t.Fatalf("want one entry, got %d", len(exact))
	}
	e := exact[0]
	if !strings.Contains(e.Example, "Split Each") || e.Input == "" || e.Output == "" {
		t.Errorf("Split Each: example %q, input %q, output %q — want a runnable example", e.Example, e.Input, e.Output)
	}
	if e.Page != "ref-transforms.md" {
		t.Errorf("Split Each is documented on %q, want ref-transforms.md", e.Page)
	}
}

func TestDocCommand(t *testing.T) {
	run := func(args ...string) (int, string, string) {
		query, opts, err := parseDocArgs(args)
		if err != nil {
			t.Fatalf("parseDocArgs(%v): %v", args, err)
		}
		var out, errOut bytes.Buffer
		code := Doc(query, opts, &out, &errOut)
		return code, out.String(), errOut.String()
	}

	// The name is the rest of the line: no quoting for a two-word primitive.
	code, out, _ := run("Split", "Each")
	if code != 0 || !strings.Contains(out, "Cursed Technique: Split Each") || !strings.Contains(out, "List<Text> → List<List<Text>>") {
		t.Errorf("doc Split Each: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "example") || !strings.Contains(out, "output") {
		t.Errorf("doc Split Each shows no example:\n%s", out)
	}

	code, out, _ = run("length")
	if code != 0 || !strings.Contains(out, "length(xs)") || !strings.Contains(out, "expression builtin") {
		t.Errorf("doc length: exit %d\n%s", code, out)
	}

	code, out, _ = run("grid")
	if code != 0 || !strings.Contains(out, `entries match "grid"`) || !strings.Contains(out, "Convert To Grid") {
		t.Errorf("doc grid: exit %d\n%s", code, out)
	}

	code, out, errOut := run("splt")
	if code != 1 || out != "" || !strings.Contains(errOut, "did you mean: Split") {
		t.Errorf("doc splt: exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	code, out, _ = run("--list")
	if code != 0 || strings.Count(out, "\n") != len(docEntries()) {
		t.Errorf("doc --list: exit %d, %d lines for %d entries", code, strings.Count(out, "\n"), len(docEntries()))
	}

	code, out, _ = run("sum", "--json")
	var entries []docEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil || code != 0 || len(entries) != 2 {
		t.Errorf("doc sum --json: exit %d, err %v, %d entries\n%s", code, err, len(entries), out)
	}

	if _, _, err := parseDocArgs(nil); err == nil {
		t.Error("doc with no name should be a usage error")
	}
	if _, _, err := parseDocArgs([]string{"--frobnicate"}); err == nil {
		t.Error("an unknown flag should be a usage error")
	}
}
