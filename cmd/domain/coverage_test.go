package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/prims"
)

func TestParseCoverageArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(*testing.T, string, coverageOptions)
	}{
		{
			name: "folder alone",
			args: []string{"examples"},
			check: func(t *testing.T, path string, o coverageOptions) {
				if path != "examples" {
					t.Errorf("path = %q", path)
				}
				if o.Dynamic || o.Used {
					t.Error("defaults should be static and missing-oriented")
				}
			},
		},
		{
			name: "every flag",
			args: []string{"--dynamic", "--used", "--only=prims", "--exclude=*_wip.domain", "--min=50", "examples"},
			check: func(t *testing.T, _ string, o coverageOptions) {
				if !o.Dynamic || !o.Used {
					t.Error("flags not recorded")
				}
				if o.Only != "prims" || o.Exclude != "*_wip.domain" || o.Min != 50 {
					t.Errorf("options = %+v", o)
				}
			},
		},
		{name: "no folder", args: []string{"--dynamic"}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope", "examples"}, wantErr: true},
		{name: "bad --only", args: []string{"--only=widgets", "examples"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, opts, err := parseCoverageArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, path, opts)
			}
		})
	}
}

// The totals are asserted against the catalog's own size rather than pinned,
// so a new primitive does not churn this test — only the denominator moves.
func TestCoverageOverChallenges(t *testing.T) {
	root := filepath.Join("..", "..", "challenges")
	_, opts, err := parseCoverageArgs([]string{root, "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Coverage(root, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, errb.String())
	}
	got := out.String()
	for _, want := range []string{
		"primitives", "builtins", "keywords",
		"counted from the source",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
	// A primitive the challenges plainly use must not be listed as missing,
	// and one they plainly do not must be.
	if strings.Contains(got, "\n    Split                 ") {
		t.Error("Split is used by the challenges but was reported unexercised")
	}
	if !strings.Contains(got, "Sliding Reduce") {
		t.Error("Sliding Reduce is unused by the challenges but was not reported")
	}
}

func TestCoverageJSONShape(t *testing.T) {
	root := filepath.Join("..", "..", "challenges")
	_, opts, err := parseCoverageArgs([]string{root, "--json"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Coverage(root, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, errb.String())
	}
	var doc coverageJSON
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if doc.Prims.Total != len(prims.AllPrims()) {
		t.Errorf("primitive total = %d want %d", doc.Prims.Total, len(prims.AllPrims()))
	}
	if doc.Builtins.Total != len(prims.AllBuiltins()) {
		t.Errorf("builtin total = %d want %d", doc.Builtins.Total, len(prims.AllBuiltins()))
	}
	if doc.Keywords.Total != len(prims.AllKeywords()) {
		t.Errorf("keyword total = %d want %d", doc.Keywords.Total, len(prims.AllKeywords()))
	}
	// used + missing must exhaust the catalog, or the report has lost entries.
	if doc.Prims.Used+len(doc.Prims.Missing) != doc.Prims.Total {
		t.Errorf("used (%d) + missing (%d) != total (%d)",
			doc.Prims.Used, len(doc.Prims.Missing), doc.Prims.Total)
	}
	if doc.Programs != 13 {
		t.Errorf("programs = %d, want the 13 challenge programs", doc.Programs)
	}
}

// --dynamic must distinguish "written" from "actually ran". A Part that the
// input never reaches is written but never evaluated, and saying so is the
// whole reason the flag exists.
func TestCoverageDynamicFindsUnreachedCode(t *testing.T) {
	dir := t.TempDir()
	// The Part is guarded by a condition this input never satisfies.
	prog := `Cursed Energy: prog.input
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`
	if err := os.WriteFile(filepath.Join(dir, "prog.domain"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prog.input"), []byte("1\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, opts, err := parseCoverageArgs([]string{dir, "--dynamic", "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Coverage(dir, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "were also run") {
		t.Errorf("--dynamic did not report running anything:\n%s", got)
	}
	// Everything this program writes does run, so there must be no
	// "written but never evaluated" section.
	if strings.Contains(got, "written but never evaluated") {
		t.Errorf("a fully-exercised program was reported as having dead stages:\n%s", got)
	}
}

// A program with no input cannot be run, and --dynamic must say so rather
// than counting it as fully covered.
func TestCoverageDynamicSkipsInputlessPrograms(t *testing.T) {
	dir := t.TempDir()
	prog := `Cursed Energy: missing.input
Cursed Technique: Split Text by "\n"
Maximum Technique: Count
Reveal: stdout
`
	if err := os.WriteFile(filepath.Join(dir, "prog.domain"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	_, opts, err := parseCoverageArgs([]string{dir, "--dynamic", "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	Coverage(dir, opts, &out, &errb)
	got := out.String()
	if !strings.Contains(got, "no input file") {
		t.Errorf("an unrunnable program was not reported as skipped:\n%s", got)
	}
}

// A program that does not resolve is listed, not silently dropped: a coverage
// number computed over a folder half of which failed to parse would be a lie
// with no warning attached.
func TestCoverageReportsBrokenPrograms(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.domain"), []byte(benchProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.domain"), []byte("Cursed Nonsense: wat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, opts, err := parseCoverageArgs([]string{dir, "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	Coverage(dir, opts, &out, &errb)
	got := out.String()
	if !strings.Contains(got, "skipped") || !strings.Contains(got, "broken.domain") {
		t.Errorf("a broken program was not reported as skipped:\n%s", got)
	}
}

// --min makes the command a CI gate, so it has to actually fail.
func TestCoverageMinGate(t *testing.T) {
	root := filepath.Join("..", "..", "challenges")
	_, opts, err := parseCoverageArgs([]string{root, "--plain", "--min=99"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Coverage(root, opts, &out, &errb); code == 0 {
		t.Error("--min=99 over a folder covering a quarter of the catalog should fail")
	}
	_, opts, err = parseCoverageArgs([]string{root, "--plain", "--min=1"})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Coverage(root, opts, &out, &errb); code != 0 {
		t.Errorf("--min=1 should pass, got exit %d", code)
	}
}

func TestCoverageRejectsAFile(t *testing.T) {
	path := writeProgram(t, "p.domain", benchProgram)
	_, opts, err := parseCoverageArgs([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Coverage(path, opts, &out, &errb); code != 2 {
		t.Errorf("a file rather than a folder should be a usage error, got %d", code)
	}
}
