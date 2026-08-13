package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"domain/codegen"
)

// The profile hook exists to feed `go build -pgo`, so the test that matters is
// not "a file appeared" but "the Go toolchain accepted it as a profile".
//
// This is the whole of turn 3 of `domain expansion: mahoraga` proved in one
// test: run the compiled program with DOMAIN_CPU_PROFILE set, then rebuild it
// against the profile it just produced.
func TestCPUProfileFeedsPGO(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	src := `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (x) -> (x * 48271) % 2147483647
Maximum Technique: Sum
Reveal: stdout
`
	pipe := compilePipeline(t, src, true)
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatal(err)
	}

	// Enough work that the sampling profiler has something to sample.
	var input strings.Builder
	for i := range 200000 {
		input.WriteString(strconv.Itoa(i))
		input.WriteByte('\n')
	}
	inPath := filepath.Join(dir, "input")
	if err := os.WriteFile(inPath, []byte(input.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := filepath.Join(dir, "cpu.pprof")
	run := func(env ...string) {
		t.Helper()
		f, err := os.Open(inPath)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		cmd := exec.Command(bin)
		cmd.Stdin = f
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("running the program: %v\n%s", err, out)
		}
	}

	// Without the variable set, nothing is written — an ordinary run must not
	// notice the hook exists.
	run()
	if _, err := os.Stat(profile); err == nil {
		t.Fatal("a profile was written with no environment variable set")
	}

	run("DOMAIN_CPU_PROFILE=" + profile)
	info, err := os.Stat(profile)
	if err != nil {
		t.Fatalf("no profile written: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("the profile is empty")
	}

	// The claim under test: the toolchain accepts it for PGO.
	pgoBin := filepath.Join(dir, "prog-pgo")
	if err := codegen.BuildBinaryWith(goSrc, pgoBin, codegen.BuildConfig{
		Flags: []string{"-pgo=" + profile},
	}); err != nil {
		t.Fatalf("go build -pgo rejected the emitted profile: %v", err)
	}
	if _, err := os.Stat(pgoBin); err != nil {
		t.Fatalf("the PGO build produced no binary: %v", err)
	}
}

// BuildConfig must reach the toolchain, and a bad flag must surface rather
// than being silently dropped — a measurement taken against a flag that never
// applied would be worse than no measurement.
func TestBuildConfigReachesTheToolchain(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	pipe := compilePipeline(t, "Cursed Energy: stdin\nReveal: stdout\n", true)
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	if err := codegen.BuildBinaryWith(goSrc, filepath.Join(dir, "a"), codegen.BuildConfig{
		Env: []string{"GOAMD64=v3"},
	}); err != nil {
		// Not fatal on non-amd64, where the variable is simply ignored.
		t.Logf("GOAMD64=v3 build: %v", err)
	}

	err = codegen.BuildBinaryWith(goSrc, filepath.Join(dir, "b"), codegen.BuildConfig{
		Flags: []string{"-this-is-not-a-flag"},
	})
	if err == nil {
		t.Error("an unknown build flag was accepted silently")
	}
}
