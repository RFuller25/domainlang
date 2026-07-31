package bench

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"domain/codegen"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// binDir holds every binary built by this test process: each program is built
// at most once, however many benchmarks and tests ask for it.
var (
	binDir   string
	buildMu  sync.Mutex
	binCache = map[string]string{}
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "domain-bench-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// requireGo skips when there is no Go toolchain to build either side with.
func requireGo(tb testing.TB) {
	tb.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		tb.Skip("go toolchain not available")
	}
}

// domainBinary compiles testdata/<program>.domain through the real front end
// (lex → parse → resolve → optimize) and codegen, and builds the emitted Go.
func domainBinary(tb testing.TB, program string) string {
	tb.Helper()
	return build(tb, program+".domain", func() (string, error) {
		src, err := os.ReadFile(filepath.Join("testdata", program+".domain"))
		if err != nil {
			return "", err
		}
		return emitGo(string(src))
	})
}

// goBinary builds the hand-written Go counterpart, with the same build flags
// codegen uses for a Domain binary so the comparison is toolchain-neutral.
func goBinary(tb testing.TB, program string) string {
	tb.Helper()
	return build(tb, program+".go", func() (string, error) {
		src, err := os.ReadFile(filepath.Join("testdata", program+".go"))
		if err != nil {
			return "", err
		}
		return string(src), nil
	})
}

// emitGo runs the front end and the compiler backend over Domain source.
func emitGo(src string) (string, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return "", fmt.Errorf("lex: %w", err)
	}
	prog, err := parser.Parse(src, toks)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	pipe, err := prims.Resolve(prog)
	if err != nil {
		return "", fmt.Errorf("resolve: %w", err)
	}
	optimizer.Optimize(pipe, true)
	return codegen.EmitProgram(pipe, codegen.Options{})
}

func build(tb testing.TB, key string, source func() (string, error)) string {
	tb.Helper()
	requireGo(tb)
	buildMu.Lock()
	defer buildMu.Unlock()
	if bin, ok := binCache[key]; ok {
		return bin
	}
	src, err := source()
	if err != nil {
		tb.Fatalf("%s: %v", key, err)
	}
	bin := filepath.Join(binDir, key)
	if err := codegen.BuildBinary(src, bin); err != nil {
		tb.Fatalf("building %s: %v", key, err)
	}
	binCache[key] = bin
	return bin
}

// inputFile materializes an input on disk, once per process, and hands back
// the path. Redirecting a regular file is how these programs are actually run
// (`./day1 < input.txt`), and it matters: over a pipe neither side can size
// its read up front, which costs the whole-input readers far more than the
// streaming ones. Timing the redirect keeps the comparison about the compute.
var (
	inputMu    sync.Mutex
	inputCache = map[string]string{}
)

func inputFile(tb testing.TB, key string, data []byte) string {
	tb.Helper()
	inputMu.Lock()
	defer inputMu.Unlock()
	if path, ok := inputCache[key]; ok {
		return path
	}
	path := filepath.Join(binDir, key+".input")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		tb.Fatal(err)
	}
	inputCache[key] = path
	return path
}

// exec runs a built binary with the input file redirected onto its stdin.
func execBin(tb testing.TB, bin, input string, stdout io.Writer) {
	tb.Helper()
	f, err := os.Open(input)
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	cmd := exec.Command(bin)
	cmd.Stdin = f
	cmd.Stdout = stdout
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		tb.Fatalf("%s: %v\nstderr: %s", filepath.Base(bin), err, errb.String())
	}
}

// run executes a built binary and returns its stdout.
func run(tb testing.TB, bin, input string) string {
	tb.Helper()
	var out bytes.Buffer
	execBin(tb, bin, input, &out)
	return out.String()
}

// timeRun is run without keeping the output, returning the wall time a user
// would pay: process start, read, compute, print, exit.
func timeRun(tb testing.TB, bin, input string) time.Duration {
	tb.Helper()
	start := time.Now()
	execBin(tb, bin, input, io.Discard)
	return time.Since(start)
}

// fastestPair times the two binaries alternately, keeping the shortest run of
// each. Running one side's reps and then the other's would let a load spike or
// a thermal step land entirely on one of them; alternating makes any drift
// common to both.
func fastestPair(tb testing.TB, a, b, input string, reps int) (time.Duration, time.Duration) {
	tb.Helper()
	best := [2]time.Duration{1<<62 - 1, 1<<62 - 1}
	for range reps {
		for j, bin := range [2]string{a, b} {
			best[j] = min(best[j], timeRun(tb, bin, input))
		}
	}
	return best[0], best[1]
}
