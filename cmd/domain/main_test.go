package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoTestdata returns the absolute path to the repo-root testdata directory.
func repoTestdata(t *testing.T) string {
	t.Helper()
	// This test runs from cmd/domain; testdata lives two levels up.
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// golden is one end-to-end anchor case.
type golden struct {
	name     string
	program  string // .domain file in testdata/
	input    string // *_input.txt file in testdata/
	expected string // exact stdout (trimmed)
	explain  string // substring required in --explain output (optimized run); "" to skip
}

// goldens are the v0.2 anchor programs. Each runs both optimized and
// --no-optimize and must produce the expected output in both modes.
var goldens = []golden{
	{"AoC2022 D1 P2 (Sort+TopK→quickselect)", "day1.domain", "day1_input.txt", "45000", "Cursed Quickselect"},
	{"AoC2022 D1 via Shikigami", "day1_shikigami.domain", "day1_input.txt", "45000", "Cursed Quickselect"},
	{"AoC2022 D4 (Match Pattern + and/or)", "day4.domain", "day4_input.txt", "2", ""},
	{"AoC2022 D5 (Channels)", "day5.domain", "day5_input.txt", "11", ""},
	{"AoC2022 D5 full (crate simulation)", "day5_full.domain", "day5_input.txt", "CMZ", ""},
	{"AoC2022 D8 (Grid)", "day8.domain", "day8_input.txt", "9", ""},
	{"AoC2022 D8 full (visibility)", "day8_full.domain", "day8_input.txt", "21", ""},
	{"AoC2020 D1 P1 (All Pairs→hash-set)", "aoc2020_day1.domain", "aoc2020_day1_input.txt", "514579", "Cursed Hash-Set Scan"},
	{"AoC2020 D1 P2 (Combinations 3)", "aoc2020_day1_part2.domain", "aoc2020_day1_input.txt", "241861950", ""},
}

func TestGoldenAnchors(t *testing.T) {
	td := repoTestdata(t)
	for _, g := range goldens {
		t.Run(g.name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join(td, g.input))
			if err != nil {
				t.Fatal(err)
			}
			program := filepath.Join(td, g.program)

			run := func(opts Options) (string, string) {
				var out, errBuf bytes.Buffer
				if err := Execute(program, opts, bytes.NewReader(input), &out, &errBuf); err != nil {
					t.Fatalf("execute: %v", err)
				}
				return strings.TrimSpace(out.String()), errBuf.String()
			}

			optOut, optErr := run(Options{Optimize: true, Explain: true})
			if optOut != g.expected {
				t.Fatalf("optimized output: got %q want %q", optOut, g.expected)
			}
			if g.explain != "" && !strings.Contains(optErr, g.explain) {
				t.Fatalf("--explain should contain %q, got: %q", g.explain, optErr)
			}

			naiveOut, _ := run(Options{Optimize: false})
			if naiveOut != g.expected {
				t.Fatalf("naive output: got %q want %q", naiveOut, g.expected)
			}
			if optOut != naiveOut {
				t.Fatalf("optimized (%q) and naive (%q) disagree", optOut, naiveOut)
			}
		})
	}
}

// TestCleanErrorReporting confirms every error category surfaces a positioned
// message and never panics (no raw stack leaks to users).
func TestCleanErrorReporting(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"lex: tab indent", "Cursed Energy: x\n\tReveal: stdout\n", "tabs are not allowed"},
		{"parse: missing colon", "Cursed Energy x\n", "expected ':'"},
		{"resolve: type mismatch", "Cursed Energy: x\nDomain Expansion: Quicksort\nReveal: stdout\n", "expects input of type"},
		{"resolve: unknown op", "Cursed Energy: x\nMaximum Technique: Frobnicate\nReveal: stdout\n", "unknown operation"},
		{"runtime: vow violation", "Cursed Energy: stdin\nCursed Technique: Split Text by \",\"\nChanneled Energy: Convert List to Integers\nBinding Vow: All Values > 100\nReveal: stdout\n", "vow violated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := filepath.Join(dir, "p.domain")
			if err := os.WriteFile(prog, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			var out, errBuf bytes.Buffer
			err := Execute(prog, Options{Optimize: true}, strings.NewReader("1,2,3"), &out, &errBuf)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

// TestExplainNoOptimization confirms --explain reports when nothing fired.
func TestExplainNoOptimization(t *testing.T) {
	td := repoTestdata(t)
	dir := t.TempDir()
	prog := filepath.Join(dir, "noopt.domain")
	src := "Cursed Energy: x\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	if err := Execute(prog, Options{Optimize: true, Explain: true},
		strings.NewReader("1\n2\n3"), &out, &errBuf); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "6" {
		t.Fatalf("sum output: got %q want 6", out.String())
	}
	if !strings.Contains(errBuf.String(), "no optimizations applied") {
		t.Fatalf("expected 'no optimizations applied', got %q", errBuf.String())
	}
	_ = td
}

// TestParseRunArgs covers the flag/positional-argument grammar in isolation:
// unknown flag, missing file, duplicate path, flag ordering.
func TestParseRunArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		path    string
		explain bool
		opt     bool
	}{
		{"bare path", []string{"day1.domain"}, false, "day1.domain", false, true},
		{"path then explain", []string{"day1.domain", "--explain"}, false, "day1.domain", true, true},
		{"explain then path", []string{"--explain", "day1.domain"}, false, "day1.domain", true, true},
		{"no-optimize", []string{"day1.domain", "--no-optimize"}, false, "day1.domain", false, false},
		{"both flags, path in the middle", []string{"--explain", "day1.domain", "--no-optimize"}, false, "day1.domain", true, false},
		{"missing path", []string{"--explain"}, true, "", false, false},
		{"no args at all", []string{}, true, "", false, false},
		{"unknown flag", []string{"day1.domain", "--bogus"}, true, "", false, false},
		{"duplicate path", []string{"day1.domain", "day2.domain"}, true, "", false, false},
		{"flag that looks like a path but starts with -", []string{"-day1.domain"}, true, "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, opts, err := parseRunArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error for args %v", c.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for args %v: %v", c.args, err)
			}
			if path != c.path {
				t.Fatalf("path: got %q want %q", path, c.path)
			}
			if opts.Explain != c.explain {
				t.Fatalf("Explain: got %v want %v", opts.Explain, c.explain)
			}
			if opts.Optimize != c.opt {
				t.Fatalf("Optimize: got %v want %v", opts.Optimize, c.opt)
			}
		})
	}
}

// TestExecuteMissingFile covers the "reading <path>" error path — the file
// simply doesn't exist, distinct from Read Source's own stdin-fallback
// primitive that only applies to a program's `Cursed Energy:` target.
func TestExecuteMissingFile(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := Execute(filepath.Join(t.TempDir(), "nope.domain"), Options{Optimize: true},
		strings.NewReader(""), &out, &errBuf)
	if err == nil {
		t.Fatal("expected an error for a missing program file")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Fatalf("expected a 'reading ...' error, got: %v", err)
	}
}

// TestExecuteReadsFromNamedFileNotJustStdin proves Cursed Energy can read a
// real file on disk (not only the stdin fallback exercised by the golden
// anchors and TestCleanErrorReporting).
func TestExecuteReadsFromNamedFileNotJustStdin(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(dataPath, []byte("2\n4\n6"), 0o644); err != nil {
		t.Fatal(err)
	}
	progPath := filepath.Join(dir, "p.domain")
	src := "Cursed Energy: " + dataPath + "\n" +
		"Cursed Technique: Split Text by \"\\n\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	if err := os.WriteFile(progPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	// stdin intentionally holds different data to prove the named file, not
	// stdin, was actually read.
	if err := Execute(progPath, Options{Optimize: true}, strings.NewReader("999"), &out, &errBuf); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "12" {
		t.Fatalf("got %q want 12 (2+4+6, proving the named file was read, not stdin)", out.String())
	}
}

// TestCheckCommand pins §G's typecheck-only mode: a statically valid program
// reports ok without running (even one that would fail at runtime), and a
// type error surfaces with its position while stdin is never consumed.
func TestCheckCommand(t *testing.T) {
	dir := t.TempDir()

	// Statically fine, guaranteed to fail at runtime (vow over stdin data):
	// check must still say ok because nothing executes.
	okProg := filepath.Join(dir, "ok.domain")
	okSrc := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Binding Vow: All Values > 100\n" +
		"Reveal: stdout\n"
	if err := os.WriteFile(okProg, []byte(okSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	if err := Check(okProg, &out, &errBuf); err != nil {
		t.Fatalf("check of a valid program: %v", err)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("check should report ok, got %q", out.String())
	}

	// A type error is caught with its position, and the runtime is never
	// entered.
	badProg := filepath.Join(dir, "bad.domain")
	badSrc := "Cursed Energy: stdin\n" +
		"Domain Expansion: Quicksort\n" + // sorts Text: type error at 2:1
		"Reveal: stdout\n"
	if err := os.WriteFile(badProg, []byte(badSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := Check(badProg, &out, &errBuf)
	if err == nil {
		t.Fatal("check should fail on a type error")
	}
	if !strings.Contains(err.Error(), "2:") || !strings.Contains(err.Error(), "expects input of type") {
		t.Fatalf("check error should be positioned and typed, got: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("failed check must not print ok, got %q", out.String())
	}
}

// TestCheckReportsMultipleParseErrors: with parser recovery (§G, M27) one
// check run surfaces every broken line, not just the first.
func TestCheckReportsMultipleParseErrors(t *testing.T) {
	prog := filepath.Join(t.TempDir(), "broken.domain")
	src := "Cursed Energy stdin\n" + // line 1: missing colon
		"Maximum Technique: Sum\n" +
		"Reveal stdout\n" // line 3: missing colon
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(prog, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected parse errors")
	}
	msg := err.Error()
	if !strings.Contains(msg, "1:") || !strings.Contains(msg, "3:") {
		t.Fatalf("check should report both broken lines, got: %q", msg)
	}
}

func TestParseCheckArgs(t *testing.T) {
	if _, err := parseCheckArgs(nil); err == nil {
		t.Fatal("missing path should error")
	}
	if _, err := parseCheckArgs([]string{"a.domain", "b.domain"}); err == nil {
		t.Fatal("duplicate path should error")
	}
	if _, err := parseCheckArgs([]string{"--explain", "a.domain"}); err == nil {
		t.Fatal("check takes no flags")
	}
	path, err := parseCheckArgs([]string{"a.domain"})
	if err != nil || path != "a.domain" {
		t.Fatalf("got %q, %v", path, err)
	}
}

func TestUsageMentionsRunCommand(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)
	if !strings.Contains(buf.String(), "domain run") {
		t.Fatalf("usage output should mention 'domain run', got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "domain build") {
		t.Fatalf("usage output should mention 'domain build', got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "domain check") {
		t.Fatalf("usage output should mention 'domain check', got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "bare file") || !strings.Contains(buf.String(), "--release") {
		t.Fatalf("usage should document the implicit modes and all flags, got: %q", buf.String())
	}
}

// TestParseBuildArgs covers the build subcommand's flag grammar.
func TestParseBuildArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		path    string
		opts    BuildOptions
	}{
		{"bare path", []string{"day1.domain"}, false, "day1.domain",
			BuildOptions{Optimize: true}},
		{"output flag", []string{"day1.domain", "-o", "bin/day1"}, false, "day1.domain",
			BuildOptions{Optimize: true, Out: "bin/day1"}},
		{"long output flag before path", []string{"--output", "x", "day1.domain"}, false, "day1.domain",
			BuildOptions{Optimize: true, Out: "x"}},
		{"emit-go", []string{"day1.domain", "--emit-go", "-"}, false, "day1.domain",
			BuildOptions{Optimize: true, EmitGo: "-"}},
		{"no-optimize and explain", []string{"--explain", "day1.domain", "--no-optimize"}, false, "day1.domain",
			BuildOptions{Explain: true}},
		{"run flag", []string{"day1.domain", "--run"}, false, "day1.domain",
			BuildOptions{Optimize: true, Run: true}},
		{"run with output and explain", []string{"--run", "-o", "bin/x", "day1.domain", "--explain"}, false, "day1.domain",
			BuildOptions{Optimize: true, Run: true, Out: "bin/x", Explain: true}},
		{"missing path", []string{"-o", "x"}, true, "", BuildOptions{}},
		{"-o without value", []string{"day1.domain", "-o"}, true, "", BuildOptions{}},
		{"--emit-go without value", []string{"day1.domain", "--emit-go"}, true, "", BuildOptions{}},
		{"unknown flag", []string{"day1.domain", "--bogus"}, true, "", BuildOptions{}},
		{"duplicate path", []string{"a.domain", "b.domain"}, true, "", BuildOptions{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, opts, err := parseBuildArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error for args %v", c.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for args %v: %v", c.args, err)
			}
			if path != c.path {
				t.Fatalf("path: got %q want %q", path, c.path)
			}
			if opts != c.opts {
				t.Fatalf("opts: got %+v want %+v", opts, c.opts)
			}
		})
	}
}

func TestDefaultBinaryName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"testdata/day1.domain", "day1"},
		{"day1.domain", "day1"},
		{"prog", "prog.bin"},       // never overwrite an extensionless source
		{".domain", ".domain.bin"}, // degenerate name keeps the guard
	}
	for _, c := range cases {
		if got := defaultBinaryName(c.in); got != c.want {
			t.Errorf("defaultBinaryName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBuildProducesWorkingBinary is the CLI-level smoke test for the compiler
// backend; the exhaustive interpreter-vs-binary diffing lives in
// codegen/codegen_test.go.
func TestBuildProducesWorkingBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a binary; skipped in -short mode")
	}
	td := repoTestdata(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "day1")
	goFile := filepath.Join(dir, "day1.go")

	var stdout, stderr bytes.Buffer
	opts := BuildOptions{Optimize: true, Explain: true, Out: bin, EmitGo: goFile}
	if err := Build(filepath.Join(td, "day1.domain"), opts, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(stderr.String(), "Cursed Quickselect") {
		t.Errorf("--explain during build should report the quickselect rewrite, got %q", stderr.String())
	}
	goSrc, err := os.ReadFile(goFile)
	if err != nil {
		t.Fatalf("--emit-go file: %v", err)
	}
	if !strings.Contains(string(goSrc), "dmTopK") {
		t.Errorf("optimized build should emit the quickselect runtime (dmTopK), got:\n%s", goSrc)
	}

	input, err := os.ReadFile(filepath.Join(td, "day1_input.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running compiled binary: %v", err)
	}
	if strings.TrimSpace(string(out)) != "45000" {
		t.Fatalf("compiled day1 output: got %q want 45000", out)
	}
}

// TestBuildEmitGoWriteFailure covers the --emit-go error path: an unwritable
// destination fails the build cleanly before any binary is produced (the
// write happens right after code generation), with the OS error surfaced.
func TestBuildEmitGoWriteFailure(t *testing.T) {
	td := repoTestdata(t)
	badPath := filepath.Join(t.TempDir(), "no", "such", "dir", "out.go")
	opts := BuildOptions{Optimize: true, EmitGo: badPath}
	err := Build(filepath.Join(td, "day1.domain"), opts, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error writing --emit-go into a missing directory")
	}
	if !strings.Contains(err.Error(), "no such file or directory") && !strings.Contains(err.Error(), badPath) {
		t.Fatalf("error should surface the failed write, got: %v", err)
	}
}

// TestBuildRun pins B.f4's one-shot mode: `domain build --run` builds to a
// temp path, runs the binary with the given stdin/stdout/stderr, cleans the
// temp binary up, and propagates a failing program's exit code as *exitError.
func TestBuildRun(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	td := repoTestdata(t)
	input, err := os.ReadFile(filepath.Join(td, "day1_input.txt"))
	if err != nil {
		t.Fatal(err)
	}

	// Success: the program's output arrives on the given stdout, and no
	// binary is left next to the source or in the working directory.
	var stdout, stderr bytes.Buffer
	opts := BuildOptions{Optimize: true, Run: true}
	if err := Build(filepath.Join(td, "day1.domain"), opts, bytes.NewReader(input), &stdout, &stderr); err != nil {
		t.Fatalf("build --run: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "45000" {
		t.Fatalf("build --run output: got %q want 45000", got)
	}
	if _, err := os.Stat("day1"); !os.IsNotExist(err) {
		t.Fatalf("build --run without -o must not leave ./day1 behind (stat err: %v)", err)
	}

	// Failure: a vow violation in the child comes back as *exitError with
	// the child's code, and the child's message reached the given stderr.
	dir := t.TempDir()
	prog := filepath.Join(dir, "vowed.domain")
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Binding Vow: All Values > 100\n" +
		"Reveal: stdout\n"
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	err = Build(prog, opts, strings.NewReader("1,2,3"), &stdout, &stderr)
	var xe *exitError
	if !errors.As(err, &xe) || xe.code != 1 {
		t.Fatalf("failing program under --run should return *exitError{1}, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "vow violated") {
		t.Fatalf("child's vow message should reach the given stderr, got %q", stderr.String())
	}
}

// TestReleaseModeVows pins the required behavior: a program whose vow fails
// must error in debug mode and succeed in release mode, identically under
// `domain run` and the built binary.
func TestReleaseModeVows(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "vowed.domain")
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Binding Vow: All Values > 100\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	const input = "1,2,3"

	// Interpreter: debug errors, release succeeds.
	var out, errBuf bytes.Buffer
	err := Execute(prog, Options{Optimize: true}, strings.NewReader(input), &out, &errBuf)
	if err == nil || !strings.Contains(err.Error(), "vow violated") {
		t.Fatalf("debug run should fail the vow, got: %v", err)
	}
	out.Reset()
	if err := Execute(prog, Options{Optimize: true, Release: true}, strings.NewReader(input), &out, &errBuf); err != nil {
		t.Fatalf("release run: %v", err)
	}
	if strings.TrimSpace(out.String()) != "6" {
		t.Fatalf("release run output: got %q want 6", out.String())
	}

	if testing.Short() {
		return
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	// Compiled: the debug binary exits nonzero with the vow message, the
	// release binary prints the same clean result as the release run.
	runBinary := func(release bool) (string, string, error) {
		bin := filepath.Join(dir, "vowed-bin")
		opts := BuildOptions{Optimize: true, Release: release, Out: bin}
		if err := Build(prog, opts, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatalf("build (release=%v): %v", release, err)
		}
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(input)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	_, stderr, err := runBinary(false)
	if err == nil || !strings.Contains(stderr, "vow violated") {
		t.Fatalf("debug binary should fail the vow (err=%v, stderr=%q)", err, stderr)
	}
	stdout, _, err := runBinary(true)
	if err != nil {
		t.Fatalf("release binary: %v", err)
	}
	if strings.TrimSpace(stdout) != "6" {
		t.Fatalf("release binary output: got %q want 6", stdout)
	}
}

// TestImplicitModeSelection pins the subcommand-less dispatch rule: a bare
// program file interprets, anything more compiles.
func TestImplicitModeSelection(t *testing.T) {
	cases := []struct {
		args []string
		run  bool
	}{
		{[]string{"day1.domain"}, true},
		{[]string{"day1.domain", "-o", "day1"}, false},
		{[]string{"day1.domain", "--release"}, false},
		{[]string{"-o", "day1", "day1.domain"}, false},
		{[]string{"--explain"}, false}, // lone flag: build path reports the missing file
	}
	for _, c := range cases {
		if got := isImplicitRun(c.args); got != c.run {
			t.Errorf("isImplicitRun(%v) = %v, want %v", c.args, got, c.run)
		}
	}
}

// --stats must not disturb the program: the table goes to stderr, and stdout
// stays byte-identical to an ordinary run.
func TestStatsLeavesStdoutAlone(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "p.domain")
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Simple Domain: Repeat 2\n" +
		"    Cursed Technique: Map Each\n" +
		"        Using: (x) -> x + 1\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout\n"
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(opts Options) (string, string) {
		var out, errBuf bytes.Buffer
		if err := Execute(prog, opts, strings.NewReader("1,2,3"), &out, &errBuf); err != nil {
			t.Fatalf("run: %v", err)
		}
		return out.String(), errBuf.String()
	}

	plainOut, plainErr := run(Options{Optimize: true})
	statsOut, statsErr := run(Options{Optimize: true, Stats: true})

	if statsOut != plainOut {
		t.Errorf("stdout changed with --stats:\n got %q\nwant %q", statsOut, plainOut)
	}
	if plainErr != "" {
		t.Errorf("plain run wrote to stderr: %q", plainErr)
	}
	for _, want := range []string{"[stats]", "interpreter", "Repeat 2", "frames"} {
		if !strings.Contains(statsErr, want) {
			t.Errorf("stats report missing %q:\n%s", want, statsErr)
		}
	}

	// --verbose adds the nested detail lines.
	_, verboseErr := run(Options{Optimize: true, Stats: true, Verbose: true})
	if !strings.Contains(verboseErr, "↳") {
		t.Errorf("--verbose should list nested steps:\n%s", verboseErr)
	}
}

// A failing run still gets its table: the stage that failed is the interesting
// one, and the report shows how far the program got.
func TestStatsReportedOnFailure(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "bad.domain")
	src := "Cursed Energy: stdin\n" +
		"Cursed Technique: Split Text by \",\"\n" +
		"Channeled Energy: Convert List to Integers\n" +
		"Reveal: stdout\n"
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	err := Execute(prog, Options{Optimize: true, Stats: true}, strings.NewReader("1,nope"), &out, &errBuf)
	if err == nil {
		t.Fatal("expected a runtime error")
	}
	if !strings.Contains(errBuf.String(), "[stats]") {
		t.Errorf("stats should still be reported after a failure:\n%s", errBuf.String())
	}
}

func TestParseRunArgsStats(t *testing.T) {
	_, opts, err := parseRunArgs([]string{"p.domain", "--stats", "--verbose"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Stats || !opts.Verbose {
		t.Errorf("opts = %+v, want Stats and Verbose set", opts)
	}
}
