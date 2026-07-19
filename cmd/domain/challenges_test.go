package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChallenges mirrors TestExamples over challenges/ — the thirteen classic
// programs are golden-tested in both optimizer modes, so the suite doubles as
// an end-to-end regression net (the compiled-backend counterpart lives in
// codegen/challenges_test.go).
func TestChallenges(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "challenges"))
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
				t.Fatalf("every challenge needs a sibling .expected file: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, base+".input")); err != nil {
				t.Fatalf("every challenge needs a sibling .input file: %v", err)
			}
			want := strings.TrimRight(string(expected), "\n")

			for _, opt := range []bool{true, false} {
				var out, errBuf bytes.Buffer
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
	if ran < 13 {
		t.Fatalf("expected at least 13 challenges, found %d", ran)
	}
}
