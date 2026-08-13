package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The runner drives the domain binary for interpreted configurations, so the
// tests need one. It is built once per test process.
var (
	domainBinOnce sync.Once
	domainBinPath string
	domainBinErr  error
)

func domainBinary(t *testing.T) string {
	t.Helper()
	requireGo(t)
	domainBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "domain-runner-test-*")
		if err != nil {
			domainBinErr = err
			return
		}
		bin := filepath.Join(dir, "domain")
		cmd := exec.Command("go", "build", "-o", bin, "domain/cmd/domain")
		if out, err := cmd.CombinedOutput(); err != nil {
			domainBinErr = err
			t.Logf("building domain: %s", out)
			return
		}
		domainBinPath = bin
	})
	if domainBinErr != nil {
		t.Fatalf("building the domain binary: %v", domainBinErr)
	}
	return domainBinPath
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
}

// writeProgram puts a program and (optionally) a sibling input in a temp dir,
// the layout a real Domain project has.
func writeProgram(t *testing.T, src string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.domain")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// sumStdin reads its input as lines of integers and prints the total.
const sumStdin = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`

// sumNamed is the same program reading a file it names, which is what makes
// input substitution interesting.
const sumNamed = `Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`

func TestRunInterpretedReadsStdin(t *testing.T) {
	prog := writeProgram(t, sumStdin, nil)
	res, err := Once(prog, Config{Optimize: true}, Input{Bytes: []byte("1\n2\n3\n")},
		Options{DomainBin: domainBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != nil {
		t.Fatalf("run failed: %v\nstderr: %s", res.Err, res.Stderr)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "6" {
		t.Errorf("got %q want %q (stderr: %s)", got, "6", res.Stderr)
	}
}

// The case the mirroring exists for: the program names input.txt, a real
// input.txt sits beside it, and the caller supplies different bytes. The
// caller's input must win — if the sibling shadowed it, every shrink candidate
// and every fuzz input would silently be a no-op.
func TestRunNamedSourceIsSubstituted(t *testing.T) {
	prog := writeProgram(t, sumNamed, map[string]string{"input.txt": "100\n200\n"})
	res, err := Once(prog, Config{Optimize: true}, Input{Bytes: []byte("1\n2\n3\n")},
		Options{DomainBin: domainBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != nil {
		t.Fatalf("run failed: %v\nstderr: %s", res.Err, res.Stderr)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "6" {
		t.Errorf("the sibling input.txt shadowed the supplied input: got %q want 6", got)
	}
	// And the user's own file is untouched.
	data, err := os.ReadFile(filepath.Join(filepath.Dir(prog), "input.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "100\n200\n" {
		t.Errorf("the program's own input.txt was modified: %q", data)
	}
}

// All four configurations must agree on the answer. This is the property
// bench reports and fuzz differentials against, and a disagreement here is a
// compiler bug rather than a test failure.
func TestFourConfigurationsAgree(t *testing.T) {
	requireGo(t)
	defer Cleanup()
	prog := writeProgram(t, sumStdin, nil)
	results, err := Race(prog, Four, Input{Bytes: []byte("4\n5\n6\n")},
		Options{Runs: 1, DomainBin: domainBinary(t), KeepStdout: true})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Config.Label(), r.Err)
		}
		if r.Failed() {
			t.Fatalf("%s failed: exit %d, stderr %s", r.Config.Label(), r.ExitCode, r.Stderr)
		}
		got := strings.TrimSpace(string(r.Stdout))
		if got != "15" {
			t.Errorf("%s: got %q want 15", r.Config.Label(), got)
		}
		if i > 0 {
			want := strings.TrimSpace(string(results[0].Stdout))
			if got != want {
				t.Errorf("%s disagrees with %s: %q vs %q",
					r.Config.Label(), results[0].Config.Label(), got, want)
			}
		}
		if r.Wall <= 0 {
			t.Errorf("%s: no wall time recorded", r.Config.Label())
		}
	}
	// Only the compiled configurations pay a build.
	for _, r := range results {
		if r.Config.Compiled && r.Build <= 0 {
			t.Errorf("%s: compiled config reported no build time", r.Config.Label())
		}
		if !r.Config.Compiled && r.Build != 0 {
			t.Errorf("%s: interpreted config reported build time %s", r.Config.Label(), r.Build)
		}
	}
}

// A program that fails is not a harness failure: it comes back with its exit
// code and its stderr, which is exactly what shrink's oracle reads.
func TestProgramFailureIsNotHarnessFailure(t *testing.T) {
	prog := writeProgram(t, sumStdin, nil)
	res, err := Once(prog, Config{Optimize: true}, Input{Bytes: []byte("1\nnot-a-number\n")},
		Options{DomainBin: domainBinary(t)})
	if err != nil {
		t.Fatalf("Once returned a harness error for a program failure: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("program failure surfaced as a harness error: %v", res.Err)
	}
	if !res.Failed() {
		t.Fatalf("a program that could not parse its input reported success")
	}
	if !strings.Contains(string(res.Stderr), "not-a-number") {
		t.Errorf("stderr did not carry the program's own message: %s", res.Stderr)
	}
}

// Non-termination is a finding, not a hang of the tool.
func TestTimeoutIsReportedNotHung(t *testing.T) {
	// A loop with a predicate that never goes false.
	prog := writeProgram(t, `Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> toint(s)
Simple Domain: While
    Using: (n) -> n > 0
    Cursed Technique: Apply
        Using: (n) -> n + 1
Reveal: stdout
`, nil)
	start := time.Now()
	res, err := Once(prog, Config{Optimize: true}, Input{Bytes: []byte("1\n")},
		Options{DomainBin: domainBinary(t), Timeout: 900 * time.Millisecond})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Timeout {
		t.Fatalf("expected a timeout, got exit %d (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if !res.Failed() {
		t.Error("a timed-out run should count as failed")
	}
	if elapsed > 10*time.Second {
		t.Errorf("timeout took %s to take effect", elapsed)
	}
}

// The allocation protocol, end to end: an interpreted run reports figures
// through the same file a compiled one would.
func TestAllocReportingInterpreted(t *testing.T) {
	prog := writeProgram(t, sumStdin, nil)
	res, err := Run(prog, Config{Optimize: true}, Input{Bytes: []byte("1\n2\n3\n")},
		Options{Runs: 1, Alloc: true, DomainBin: domainBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !res.Alloc.Reported {
		t.Fatal("interpreted run did not report allocation figures")
	}
	if res.Alloc.TotalAlloc == 0 || res.Alloc.Mallocs == 0 {
		t.Errorf("implausible allocation figures: %+v", res.Alloc)
	}
}

func TestAllocReportingCompiled(t *testing.T) {
	requireGo(t)
	defer Cleanup()
	prog := writeProgram(t, sumStdin, nil)
	res, err := Run(prog, Config{Compiled: true, Optimize: true}, Input{Bytes: []byte("1\n2\n3\n")},
		Options{Runs: 1, Alloc: true, DomainBin: domainBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !res.Alloc.Reported {
		t.Fatal("compiled run did not report allocation figures; the codegen hook is not firing")
	}
	if res.Alloc.TotalAlloc == 0 {
		t.Errorf("implausible allocation figures: %+v", res.Alloc)
	}
}

// An ordinary run must not write a report file or otherwise notice the
// protocol exists.
func TestNoAllocReportWithoutTheEnvVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "should-not-exist")
	t.Setenv(EnvAllocReport, "")
	WriteReport()
	if _, err := os.Stat(path); err == nil {
		t.Error("a report was written with no environment variable set")
	}
}

func TestWriteAndParseReportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alloc")
	t.Setenv(EnvAllocReport, path)
	WriteReport()

	got, ok := parseReport(path)
	if !ok {
		t.Fatal("the report this package wrote could not be read back")
	}
	if !got.Reported || got.TotalAlloc == 0 {
		t.Errorf("round trip lost the figures: %+v", got)
	}
}

// The build cache is what keeps a fuzz campaign from recompiling per
// candidate: the second request for the same configuration must not build.
func TestCompiledBuildIsCached(t *testing.T) {
	requireGo(t)
	defer Cleanup()
	prog := writeProgram(t, sumStdin, nil)
	cfg := Config{Compiled: true, Optimize: true}

	first, err := buildCommand(prog, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildCommand(prog, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("the same program and configuration built twice")
	}
	// A different configuration is a different binary.
	third, err := buildCommand(prog, Config{Compiled: true, Optimize: false}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Error("naive and optimized configurations shared a binary")
	}
}

func TestConfigLabel(t *testing.T) {
	cases := []struct {
		c    Config
		want string
	}{
		{Config{}, "interpret / naive"},
		{Config{Optimize: true}, "interpret / optimized"},
		{Config{Compiled: true, Optimize: true}, "compile / optimized"},
		{Config{Compiled: true, Optimize: true, Release: true}, "compile / optimized / release"},
	}
	for _, tc := range cases {
		if got := tc.c.Label(); got != tc.want {
			t.Errorf("Label() = %q want %q", got, tc.want)
		}
	}
}
