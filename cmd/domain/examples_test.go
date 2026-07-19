package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExamples runs every program in examples/ against its .input file and
// compares stdout to its .expected file, in both optimizer modes — the
// examples in the repo can never silently rot.
func TestExamples(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "examples"))
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
			expected, err := os.ReadFile(filepath.Join(dir, base+".expected"))
			if err != nil {
				t.Fatalf("every example needs a sibling .expected file: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, base+".input")); err != nil {
				t.Fatalf("every example needs a sibling .input file: %v", err)
			}
			want := strings.TrimRight(string(expected), "\n")

			for _, opt := range []bool{true, false} {
				var out, errBuf bytes.Buffer
				// The program names its own input file; Execute resolves it
				// relative to the program's directory, so stdin stays empty.
				err := Execute(filepath.Join(dir, e.Name()), Options{Optimize: opt},
					strings.NewReader(""), &out, &errBuf)
				if err != nil {
					t.Fatalf("optimize=%v: %v", opt, err)
				}
				if got := strings.TrimRight(out.String(), "\n"); got != want {
					t.Errorf("optimize=%v: got %q want %q", opt, got, want)
				}
			}
		})
	}
	if ran < 10 {
		t.Fatalf("expected at least 10 examples, found %d", ran)
	}
}
