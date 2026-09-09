package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The games in examples/games/, run against their scripts.
//
// They need a harness of their own for one reason: a game reads its **script
// from stdin**, where a puzzle solver names its input file with
// `Cursed Energy:`. Everything else is the same discipline — both optimizer
// modes, an exact `.expected`, no program allowed to rot quietly — and the
// compiled half is in codegen, where the toolchain already lives.
//
// A golden here is a picture. That is the whole point of the replay host: a
// frame diff is to a game what a stdout diff is to an answer, and without one
// there is no way to test a game at all.
func TestGameExamples(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "examples", "games"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".domain") {
			continue
		}
		ran++
		base := strings.TrimSuffix(e.Name(), ".domain")
		t.Run(base, func(t *testing.T) {
			script, err := os.ReadFile(filepath.Join(dir, base+".script"))
			if err != nil {
				t.Fatalf("every game needs a sibling .script: %v", err)
			}
			expected, err := os.ReadFile(filepath.Join(dir, base+".expected"))
			if err != nil {
				t.Fatalf("every game needs a sibling .expected: %v", err)
			}
			want := strings.TrimRight(string(expected), "\n")

			for _, opt := range []bool{true, false} {
				var out, errBuf bytes.Buffer
				err := Execute(filepath.Join(dir, e.Name()), Options{Optimize: opt},
					bytes.NewReader(script), &out, &errBuf)
				if err != nil {
					t.Fatalf("optimize=%v: %v\n%s", opt, err, errBuf.String())
				}
				if got := strings.TrimRight(out.String(), "\n"); got != want {
					t.Errorf("optimize=%v: frames differ\n got:\n%s\n\nwant:\n%s", opt, got, want)
				}
			}
		})
	}
	if ran == 0 {
		t.Fatal("no games in examples/games/")
	}
}
