package prims

import (
	"os"
	"path/filepath"
	"testing"

	"domain/lexer"
	"domain/parser"
)

// What the front end costs per program.
//
// Resolution runs once per CLI run, where it is lost in the noise of actually
// running the program — but on *every keystroke* in the language server and
// the terminal editor, which is what makes it worth a benchmark. It also runs
// over the whole embedded prelude every time, so a cost that looks per-program
// is really per-definition-the-prelude-carries.
//
// This exists because there was no such benchmark, and adding two entries to
// ast.Keywords turned out to add an allocation per statement per keyword —
// KeywordPrefix re-split the whole list on every call. Nothing would have
// noticed.
func benchResolveFile(b *testing.B, path string) {
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
		prog, err := parser.Parse(string(src), toks)
		if err != nil {
			b.Fatalf("parse: %v", err)
		}
		if _, err := Resolve(prog); err != nil {
			b.Fatalf("resolve: %v", err)
		}
	}
}

func BenchmarkResolveDay1(b *testing.B) {
	benchResolveFile(b, filepath.Join("..", "testdata", "day1.domain"))
}

func BenchmarkResolveGridBFS(b *testing.B) {
	benchResolveFile(b, filepath.Join("..", "bench", "testdata", "grid_bfs.domain"))
}

func BenchmarkResolveExploreStates(b *testing.B) {
	benchResolveFile(b, filepath.Join("..", "bench", "testdata", "explore_states.domain"))
}

func BenchmarkResolveShikigamiCalls(b *testing.B) {
	benchResolveFile(b, filepath.Join("..", "bench", "testdata", "shikigami_calls.domain"))
}
