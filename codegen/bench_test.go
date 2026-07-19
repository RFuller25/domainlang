package codegen_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
)

// B.f3 — benchmark guards. These turn the "compiled binaries are several
// times faster than the interpreter" claim into numbers `go test -bench .`
// reports on every run: one split-heavy program (the Day 1 shape) and one
// Match Pattern-heavy program (the Day 4 shape), interpreter vs binary over
// the same generated large input. The compiled benchmarks include process
// startup, which is what a user actually pays.

const splitHeavySrc = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
`

const matchHeavySrc = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Using: "{a:int}-{b:int},{c:int}-{d:int}"
Maximum Technique: Count Matching
    Using: (r) -> (r.a <= r.c and r.b >= r.d) or (r.c <= r.a and r.d >= r.b)
Reveal: stdout
`

// splitHeavyInput builds ~n groups of 1-4 numbers each.
func splitHeavyInput(n int) []byte {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		for j := 0; j <= i%4; j++ {
			fmt.Fprintf(&sb, "%d\n", (i*7919+j*13)%10000)
		}
		sb.WriteByte('\n')
	}
	return []byte(strings.TrimRight(sb.String(), "\n"))
}

// matchHeavyInput builds n range-pair lines.
func matchHeavyInput(n int) []byte {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		a, b := i%50, i%50+i%13
		c, d := i%60, i%60+i%7
		fmt.Fprintf(&sb, "%d-%d,%d-%d\n", a, b, c, d)
	}
	return []byte(strings.TrimRight(sb.String(), "\n"))
}

// benchInterpreter runs the resolved pipeline once per iteration.
func benchInterpreter(b *testing.B, src string, input []byte) {
	b.Helper()
	pipe := compileBenchPipeline(b, src)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &out}
		if _, err := interp.Run(pipe, ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// benchCompiled builds the binary once, then runs it once per iteration.
func benchCompiled(b *testing.B, src string, input []byte) {
	b.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		b.Skip("go toolchain not available")
	}
	pipe := compileBenchPipeline(b, src)
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := exec.Command(bin)
		cmd.Stdin = bytes.NewReader(input)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

// compileBenchPipeline mirrors compilePipeline for benchmarks (optimized).
func compileBenchPipeline(b *testing.B, src string) *ir.Pipeline {
	b.Helper()
	pipe, err := frontEnd(src, true)
	if err != nil {
		b.Fatal(err)
	}
	return pipe
}

func BenchmarkSplitHeavyInterpreter(b *testing.B) {
	benchInterpreter(b, splitHeavySrc, splitHeavyInput(50000))
}

func BenchmarkSplitHeavyCompiled(b *testing.B) {
	benchCompiled(b, splitHeavySrc, splitHeavyInput(50000))
}

func BenchmarkMatchPatternInterpreter(b *testing.B) {
	benchInterpreter(b, matchHeavySrc, matchHeavyInput(100000))
}

func BenchmarkMatchPatternCompiled(b *testing.B) {
	benchCompiled(b, matchHeavySrc, matchHeavyInput(100000))
}
