package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"domain/runner"
)

// benchProgram is small enough that four cells finish in well under a second
// each, since the CLI tests build a binary twice over.
const benchProgram = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`

func writeBenchProgram(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.domain")
	if err := os.WriteFile(path, []byte(benchProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input.txt"), []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireGoToolchain(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
}

// The runner re-executes the domain binary for interpreted cells, and under
// `go test` the running executable is the test binary — so an interpreted cell
// would re-run the test suite with the program path as an argument, and hang
// until the timeout. Build a real one once and point the measurement commands
// at it.
var (
	measureBinOnce sync.Once
	measureBinErr  error
)

func useRealDomainBinary(t *testing.T) {
	t.Helper()
	requireGoToolchain(t)
	measureBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "domain-measure-test-*")
		if err != nil {
			measureBinErr = err
			return
		}
		bin := filepath.Join(dir, "domain")
		if out, err := exec.Command("go", "build", "-o", bin, "domain/cmd/domain").CombinedOutput(); err != nil {
			measureBinErr = fmt.Errorf("%v: %s", err, out)
			return
		}
		measureDomainBin = bin
	})
	if measureBinErr != nil {
		t.Fatalf("building the domain binary: %v", measureBinErr)
	}
}

func TestParseBenchArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(*testing.T, string, benchOptions)
	}{
		{
			name: "defaults to the full grid",
			args: []string{"p.domain"},
			check: func(t *testing.T, path string, o benchOptions) {
				if path != "p.domain" {
					t.Errorf("path = %q", path)
				}
				if len(o.Cells) != 4 {
					t.Errorf("cells = %d, want the full grid of 4", len(o.Cells))
				}
			},
		},
		{
			name: "flags in both spellings",
			args: []string{"--runs=3", "--input", "in.txt", "--timeout=2s", "p.domain"},
			check: func(t *testing.T, _ string, o benchOptions) {
				if o.Runs != 3 {
					t.Errorf("runs = %d want 3", o.Runs)
				}
				if o.Input != "in.txt" {
					t.Errorf("input = %q", o.Input)
				}
				if o.Timeout != 2*time.Second {
					t.Errorf("timeout = %s", o.Timeout)
				}
			},
		},
		{
			name: "a subset of cells",
			args: []string{"--cells", "interpret/optimized,compile/optimized", "p.domain"},
			check: func(t *testing.T, _ string, o benchOptions) {
				if len(o.Cells) != 2 {
					t.Fatalf("cells = %d want 2", len(o.Cells))
				}
				for _, c := range o.Cells {
					if !c.Optimize {
						t.Errorf("cell %s should be optimized", c.Label())
					}
				}
			},
		},
		{
			name: "--release marks every cell",
			args: []string{"--release", "p.domain"},
			check: func(t *testing.T, _ string, o benchOptions) {
				for _, c := range o.Cells {
					if !c.Release {
						t.Errorf("cell %s did not get Release", c.Label())
					}
				}
			},
		},
		{name: "no program", args: []string{"--runs=2"}, wantErr: true},
		{name: "unknown flag", args: []string{"--nope", "p.domain"}, wantErr: true},
		{name: "two programs", args: []string{"a.domain", "b.domain"}, wantErr: true},
		{name: "runs without a value", args: []string{"p.domain", "--runs"}, wantErr: true},
		{name: "both input forms", args: []string{"-i", "a", "--input-text", "b", "p.domain"}, wantErr: true},
		{name: "bad cell", args: []string{"--cells", "interpret/fast", "p.domain"}, wantErr: true},
		{name: "cell without a mode", args: []string{"--cells", "interpret", "p.domain"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, opts, err := parseBenchArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", opts)
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

// The full grid over a tiny program: every cell runs, they agree, and the
// report says so. Durations are never asserted — only the shape, the labels
// and the agreement line, which are the contract.
func TestBenchFourCellsAgree(t *testing.T) {
	useRealDomainBinary(t)
	path := writeBenchProgram(t)
	_, opts, err := parseBenchArgs([]string{path, "--runs=1", "--plain"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Bench(path, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	got := out.String()
	for _, want := range []string{
		"interpret / naive", "interpret / optimized",
		"compile / naive", "compile / optimized",
		"all 4 cells that ran agreed on the output",
		"peak RSS", "allocated", "build",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
}

// A subset keeps the build cost down and is the flag most people will use.
func TestBenchCellSubset(t *testing.T) {
	useRealDomainBinary(t)
	path := writeBenchProgram(t)
	_, opts, err := parseBenchArgs([]string{path, "--runs=1", "--plain", "--cells", "interpret/naive,interpret/optimized"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Bench(path, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, errb.String())
	}
	got := out.String()
	if strings.Contains(got, "compile /") {
		t.Errorf("a cell that was not asked for appears in the report:\n%s", got)
	}
	if !strings.Contains(got, "the optimizer buys") {
		t.Errorf("the optimizer ratio is missing:\n%s", got)
	}
	// Only one ratio is computable from two interpreted cells; the compiling
	// ratio needs a compiled one and must not be invented.
	if strings.Contains(got, "compiling buys") {
		t.Errorf("a ratio was reported between cells that did not both run:\n%s", got)
	}
}

func TestBenchJSONShape(t *testing.T) {
	useRealDomainBinary(t)
	path := writeBenchProgram(t)
	_, opts, err := parseBenchArgs([]string{path, "--runs=1", "--json", "--cells", "interpret/optimized"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Bench(path, opts, &out, &errb); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, errb.String())
	}
	var doc benchJSON
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if doc.Program != path {
		t.Errorf("program = %q want %q", doc.Program, path)
	}
	if len(doc.Cells) != 1 {
		t.Fatalf("cells = %d want 1", len(doc.Cells))
	}
	if doc.Cells[0].Cell != "interpret / optimized" {
		t.Errorf("cell = %q", doc.Cells[0].Cell)
	}
	if doc.Cells[0].WallNanos <= 0 {
		t.Error("no wall time in the JSON")
	}
	// A banner in front of a JSON document would make it unparseable, which
	// is why the dispatcher suppresses it — asserted here because the check
	// lives one layer up and is easy to lose.
	if strings.HasPrefix(out.String(), "\u2501") {
		t.Error("the banner leaked into --json output")
	}
}

// A cell disagreement is a compiler bug, and the report has to say that in
// those words and exit nonzero. A real disagreement is a bug we do not have
// on hand, so the report is driven directly with a fabricated result — which
// is the only part of the path worth pinning anyway.
func TestBenchReportsDisagreementAsCompilerBug(t *testing.T) {
	rep := &benchReport{
		Program: "p.domain", Input: "in.txt", Runs: 1,
		Results: []runner.Result{
			{Config: runner.Config{Optimize: true}, Stdout: []byte("42\n"), Wall: time.Millisecond},
			{Config: runner.Config{Compiled: true, Optimize: true}, Stdout: []byte("41\n"), Wall: time.Millisecond},
		},
	}
	rep.check()
	if rep.Disagreement == "" {
		t.Fatal("two cells with different output were not flagged")
	}
	var out bytes.Buffer
	rep.writeTable(&out)
	got := out.String()
	if !strings.Contains(got, "COMPILER BUG") {
		t.Errorf("the report does not name it a compiler bug:\n%s", got)
	}
	// And it shows where they differ.
	if !strings.Contains(got, `"42"`) || !strings.Contains(got, `"41"`) {
		t.Errorf("the report does not show the differing line:\n%s", got)
	}
	if strings.Contains(got, "agreed on the output") {
		t.Errorf("a disagreeing report still claims agreement:\n%s", got)
	}
}

// Cells that agree must not be flagged, including when one of them failed and
// so has no output to compare.
func TestBenchCheckIgnoresFailedCells(t *testing.T) {
	rep := &benchReport{
		Results: []runner.Result{
			{Config: runner.Config{Optimize: true}, Stdout: []byte("42\n")},
			{Config: runner.Config{Compiled: true}, Stdout: []byte(""), Timeout: true},
			{Config: runner.Config{Compiled: true, Optimize: true}, Stdout: []byte("42\n")},
		},
	}
	rep.check()
	if rep.Disagreement != "" {
		t.Errorf("a timed-out cell was compared as a disagreement: %s", rep.Disagreement)
	}
}

// One surviving cell means the oracle never ran, and the report must not read
// like a pass.
func TestBenchSaysWhenNothingWasCrossChecked(t *testing.T) {
	rep := &benchReport{
		Results: []runner.Result{
			{Config: runner.Config{Compiled: true, Optimize: true}, Stdout: []byte("309\n"), Wall: time.Second},
			{Config: runner.Config{Optimize: true}, Timeout: true},
		},
	}
	rep.check()
	var out bytes.Buffer
	rep.writeTable(&out)
	got := out.String()
	if !strings.Contains(got, "only one cell produced output") {
		t.Errorf("the report does not say the cross-check was skipped:\n%s", got)
	}
	if !strings.Contains(got, "did not finish") {
		t.Errorf("a timed-out cell should say so rather than show a number:\n%s", got)
	}
}

func TestSiblingInputDiscovery(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "day7.domain")
	if err := os.WriteFile(prog, []byte(benchProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := siblingInput(prog); got != "" {
		t.Errorf("found an input where there is none: %q", got)
	}
	want := filepath.Join(dir, "day7.input")
	if err := os.WriteFile(want, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := siblingInput(prog); got != want {
		t.Errorf("siblingInput = %q want %q", got, want)
	}
}
