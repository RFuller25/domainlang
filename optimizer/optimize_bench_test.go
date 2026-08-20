package optimizer

import (
	"os"
	"path/filepath"
	"testing"

	"domain/lexer"
	"domain/parser"
	"domain/prims"
)

// What the optimizer costs on a real program.
//
// Every pass asks `effectful` of every lambda it looks at, so this is where a
// change to that question shows up — and `effectful` is exactly the place
// globals had to widen. Keep an eye on allocs/op: the passes allocate the same
// amount for the same program, so a change there is a change in what the
// optimizer is doing, where a change in ns/op on shared hardware is usually
// not.
func benchOptimizeFile(b *testing.B, path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		b.Skipf("read: %v", err)
	}
	toks, err := lexer.Lex(string(src))
	if err != nil {
		b.Fatalf("lex: %v", err)
	}
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		prog, err := parser.Parse(string(src), toks)
		if err != nil {
			b.Fatalf("parse: %v", err)
		}
		pipe, err := prims.Resolve(prog)
		if err != nil {
			b.Fatalf("resolve: %v", err)
		}
		b.StartTimer()
		Optimize(pipe, true)
	}
}

func BenchmarkOptimizeDay1(b *testing.B) {
	benchOptimizeFile(b, filepath.Join("..", "testdata", "day1.domain"))
}
func BenchmarkOptimizeGridBFS(b *testing.B) {
	benchOptimizeFile(b, filepath.Join("..", "bench", "testdata", "grid_bfs.domain"))
}
func BenchmarkOptimizeExploreStates(b *testing.B) {
	benchOptimizeFile(b, filepath.Join("..", "bench", "testdata", "explore_states.domain"))
}
func BenchmarkOptimizeFoldMapDP(b *testing.B) {
	benchOptimizeFile(b, filepath.Join("..", "bench", "testdata", "fold_map_dp.domain"))
}
