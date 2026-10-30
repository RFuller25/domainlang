package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBoundsCheckUpperBound answers "what could removing bounds checks buy?"
// with the only number that bounds the question: it builds each case's emitted
// Go twice, once as shipped and once with `-gcflags=-B` (every bounds check
// deleted, memory safety included), and times the two on the case's input.
// Whatever work on the checks themselves might achieve, it cannot beat that.
//
// The answer on the cases here is "nothing measurable" (see README.md, "Round
// four"), which is why this is a test to re-run when the toolchain or the
// generated code changes rather than a pass in the compiler. Opt in with
// DOMAIN_BENCH_BCE=1; it needs the same idle machine as the speed gate.
func TestBoundsCheckUpperBound(t *testing.T) {
	if os.Getenv("DOMAIN_BENCH_BCE") == "" {
		t.Skip("set DOMAIN_BENCH_BCE=1 to measure what removing every bounds check would buy")
	}
	requireGo(t)
	requireIdle(t)
	only := os.Getenv("DOMAIN_BENCH_CASE") // restrict to one case while investigating
	for _, c := range cases {
		if only != "" && only != c.name {
			continue
		}
		src, err := os.ReadFile(filepath.Join("testdata", c.name+".domain"))
		if err != nil {
			t.Fatal(err)
		}
		goSrc, err := emitGo(string(src))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		checked := filepath.Join(binDir, c.name+".checked")
		unchecked := filepath.Join(binDir, c.name+".unchecked")
		buildWithFlags(t, goSrc, checked, "")
		buildWithFlags(t, goSrc, unchecked, "-B")
		input := inputFile(t, c.name, c.gen(c.size))
		run(t, checked, input)
		run(t, unchecked, input)
		a, b := fastestPair(t, checked, unchecked, input, 15)
		t.Logf("%-22s checked %9s  unchecked %9s  gain %5.1f%%", c.name,
			a.Round(100_000), b.Round(100_000), 100*(float64(a)-float64(b))/float64(a))
	}
}

// buildWithFlags builds generated Go the way codegen.BuildBinary does, plus the
// given -gcflags.
func buildWithFlags(t *testing.T, src, out, gcflags string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module j\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-gcflags", gcflags, "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
}
