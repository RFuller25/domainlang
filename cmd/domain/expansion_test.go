package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const day1Clean = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`

// day1Broken is day1Clean with a keyword typo, a wrong keyword for an
// operation, and a missing colon — all auto-fixable.
var day1Broken = strings.NewReplacer(
	"Cursed Technique: Split Text", "Cursed Tecnique: Split Text",
	"Maximum Technique: Select Top 3, Sum", "Cursed Technique: Select Top 3, Sum",
	"Reveal: stdout", "Reveal stdout",
).Replace(day1Clean)

func writeProgram(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExpansionInvocationParsing(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantCmd string
		wantOK  bool
	}{
		{"split form", []string{"expansion:", "lint", "x.domain"}, "lint", true},
		{"two-word command", []string{"expansion:", "maximum", "compile", "x.domain"}, "maximum compile", true},
		{"quoted form", []string{"expansion: maximum compile", "x.domain"}, "maximum compile", true},
		{"documentation with port", []string{"expansion:", "documentation", "-p", "8080"}, "documentation", true},
		{"no colon", []string{"expansion", "fix", "x.domain"}, "fix", true},
		{"case-insensitive", []string{"Expansion:", "Diagnosis", "x.domain"}, "diagnosis", true},
		{"unknown command", []string{"expansion:", "summon", "x.domain"}, "", true},
		{"not expansion at all", []string{"run", "x.domain"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, rest, ok := expansionInvocation(c.args)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if got := strings.Join(cmd, " "); got != c.wantCmd {
				t.Fatalf("cmd = %q, want %q", got, c.wantCmd)
			}
			if ok && c.wantCmd != "" && len(rest) == 0 {
				t.Fatal("expected the file argument in rest")
			}
		})
	}
}

func TestExpansionUsageErrors(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := Expansion([]string{"lint"}, nil, nil, &out, &errBuf); code != 2 {
		t.Errorf("missing file: code = %d, want 2", code)
	}
	if code := Expansion([]string{"lint"}, []string{"--wat", "x.domain"}, nil, &out, &errBuf); code != 2 {
		t.Errorf("unknown flag: code = %d, want 2", code)
	}
	if code := Expansion(nil, []string{"summon"}, nil, &out, &errBuf); code != 2 {
		t.Errorf("unknown command: code = %d, want 2", code)
	}
}

func TestExpansionDiagnosisReportsAndLeavesFileAlone(t *testing.T) {
	path := writeProgram(t, "broken.domain", day1Broken)
	var out, errBuf bytes.Buffer
	code := Expansion([]string{"diagnosis"}, []string{path}, nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr: %s", code, errBuf.String())
	}
	report := out.String()
	for _, want := range []string{
		`unknown keyword "Cursed Tecnique"`,
		`did you mean "Cursed Technique"?`,
		"Maximum Technique operation",
		"add a ':' after",
		"auto-fixable",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("diagnosis missing %q:\n%s", want, report)
		}
	}
	src, _ := os.ReadFile(path)
	if string(src) != day1Broken {
		t.Error("diagnosis must not modify the file")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Error("diagnosis must not create a backup")
	}
}

func TestExpansionDiagnosisCleanProgram(t *testing.T) {
	path := writeProgram(t, "clean.domain", day1Clean)
	var out, errBuf bytes.Buffer
	if code := Expansion([]string{"diagnosis"}, []string{path}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "no errors found") {
		t.Errorf("missing clean verdict:\n%s", out.String())
	}
}

func TestExpansionLintExitCodes(t *testing.T) {
	clean := writeProgram(t, "clean.domain", day1Clean)
	var out, errBuf bytes.Buffer
	if code := Expansion([]string{"lint"}, []string{clean}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("clean lint code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "clean") {
		t.Errorf("missing clean summary:\n%s", out.String())
	}

	warned := writeProgram(t, "warned.domain", strings.Replace(day1Clean,
		"Domain Expansion: Quicksort, Descending",
		"Channel \"orphan\":\n    Maximum Technique: Sum\nDomain Expansion: Quicksort, Descending", 1))
	out.Reset()
	if code := Expansion([]string{"lint"}, []string{warned}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("warnings-only lint code = %d, want 0 (warnings do not fail)", code)
	}
	if !strings.Contains(out.String(), `Channel "orphan" is defined but never consumed`) {
		t.Errorf("missing unused-channel warning:\n%s", out.String())
	}
}

func TestExpansionFixRepairsInPlaceWithBackup(t *testing.T) {
	path := writeProgram(t, "broken.domain", day1Broken)
	var out, errBuf bytes.Buffer
	code := Expansion([]string{"fix"}, []string{path}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("code = %d, want 0; out: %s stderr: %s", code, out.String(), errBuf.String())
	}
	fixed, _ := os.ReadFile(path)
	if string(fixed) != day1Clean {
		t.Errorf("fixed file:\n%s\nwant:\n%s", fixed, day1Clean)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(bak) != day1Broken {
		t.Error("backup must hold the original source")
	}

	// A second run has nothing left to do and must not touch the backup.
	out.Reset()
	if code := Expansion([]string{"fix"}, []string{path}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("second fix code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "nothing to fix") {
		t.Errorf("expected 'nothing to fix':\n%s", out.String())
	}
	bak2, _ := os.ReadFile(path + ".bak")
	if string(bak2) != day1Broken {
		t.Error("no-op fix must not rewrite the backup")
	}
}

func TestExpansionFixReportsUnfixable(t *testing.T) {
	src := strings.Replace(day1Clean, "Maximum Technique: Sum Each Group",
		"Maximum Technique: Frobnicate Wildly", 1)
	path := writeProgram(t, "hopeless.domain", src)
	var out, errBuf bytes.Buffer
	if code := Expansion([]string{"fix"}, []string{path}, nil, &out, &errBuf); code != 1 {
		t.Fatalf("code = %d, want 1; out: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Frobnicate") {
		t.Errorf("unfixable error not reported:\n%s", out.String())
	}
}

func TestExpansionOptimizeRewritesSource(t *testing.T) {
	src := strings.Replace(day1Clean,
		"Domain Expansion: Quicksort, Descending",
		"Domain Expansion: Quicksort\nReverse Cursed Technique: Reverse", 1)
	path := writeProgram(t, "opt.domain", src)
	var out, errBuf bytes.Buffer
	code := Expansion([]string{"optimize"}, []string{path}, nil, &out, &errBuf)
	if code != 0 {
		t.Fatalf("code = %d, want 0; out: %s stderr: %s", code, out.String(), errBuf.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != day1Clean {
		t.Errorf("optimized source:\n%s\nwant:\n%s", got, day1Clean)
	}
	report := out.String()
	if !strings.Contains(report, "fused Sort + Reverse") {
		t.Errorf("missing source rewrite report:\n%s", report)
	}
	if !strings.Contains(report, "Cursed Quickselect") {
		t.Errorf("missing IR optimization report:\n%s", report)
	}
}

func TestExpansionOptimizeRefusesBrokenProgram(t *testing.T) {
	path := writeProgram(t, "broken.domain", day1Broken)
	var out, errBuf bytes.Buffer
	if code := Expansion([]string{"optimize"}, []string{path}, nil, &out, &errBuf); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "cannot optimize a broken program") {
		t.Errorf("missing refusal:\n%s", out.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != day1Broken {
		t.Error("a refused optimize must not modify the file")
	}
}

func TestExpansionMaximumCompile(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	input, err := os.ReadFile(filepath.Join(repoTestdata(t), "day1_input.txt"))
	if err != nil {
		t.Fatal(err)
	}
	path := writeProgram(t, "day1.domain", day1Broken)
	var out, errBuf bytes.Buffer
	code := Expansion([]string{"maximum", "compile"}, []string{path},
		bytes.NewReader(input), &out, &errBuf)
	if code != 0 {
		t.Fatalf("code = %d, want 0; out: %s stderr: %s", code, out.String(), errBuf.String())
	}
	report := out.String()
	if !strings.Contains(report, "[fix] applied 3 fix(es)") {
		t.Errorf("missing fix stage report:\n%s", report)
	}
	if !strings.Contains(errBuf.String(), "Cursed Quickselect") {
		t.Errorf("missing optimizer explain on stderr:\n%s", errBuf.String())
	}
	if !strings.HasSuffix(strings.TrimSpace(report), "45000") {
		t.Errorf("program output missing; got:\n%s", report)
	}
	fixed, _ := os.ReadFile(path)
	if string(fixed) != day1Clean {
		t.Errorf("maximum compile left the source as:\n%s", fixed)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("maximum compile must back up before fixing")
	}
}
