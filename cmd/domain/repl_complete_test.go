package main

import (
	"os"
	"testing"
)

func TestCompleteTokenKeyword(t *testing.T) {
	candidates, tokenStart := completeToken("Cursed T", 8, ".")
	if tokenStart != 0 {
		t.Errorf("tokenStart = %d, want 0", tokenStart)
	}
	if len(candidates) != 1 || candidates[0] != "Cursed Technique: " {
		t.Errorf(`candidates = %v, want ["Cursed Technique: "]`, candidates)
	}
}

func TestCompleteTokenPrimitive(t *testing.T) {
	line := "Domain Expansion: BF"
	candidates, tokenStart := completeToken(line, len(line), ".")
	want := len("Domain Expansion: ")
	if tokenStart != want {
		t.Errorf("tokenStart = %d, want %d", tokenStart, want)
	}
	if len(candidates) != 1 || candidates[0] != "BFS" {
		t.Errorf(`candidates = %v, want ["BFS"]`, candidates)
	}
}

func TestCompleteTokenMultiWordPrimitiveNotTruncated(t *testing.T) {
	// A word-boundary rule based on trailing spaces would wrongly narrow
	// this to just "By" and lose "Sort" — the primitive names themselves
	// contain spaces ("Sort By", "Flood Fill", ...).
	line := "Domain Expansion: Sort B"
	candidates, tokenStart := completeToken(line, len(line), ".")
	want := len("Domain Expansion: ")
	if tokenStart != want {
		t.Errorf("tokenStart = %d, want %d", tokenStart, want)
	}
	if len(candidates) != 1 || candidates[0] != "Sort By" {
		t.Errorf(`candidates = %v, want ["Sort By"]`, candidates)
	}
}

func TestCompleteTokenReplCommand(t *testing.T) {
	candidates, tokenStart := completeToken(":lo", 3, ".")
	// tokenStart is 0, not 1: the candidate already includes the leading
	// ':' (it replaces the whole command), so splicing it in at
	// line[:tokenStart] + candidate + line[cursor:] must not also keep
	// line's own leading ':' — that would double it up into "::load".
	if tokenStart != 0 {
		t.Errorf("tokenStart = %d, want 0", tokenStart)
	}
	if len(candidates) != 1 || candidates[0] != ":load" {
		t.Errorf(`candidates = %v, want [":load"]`, candidates)
	}
}

func TestCompleteTokenFilePath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	line := "Cursed Energy: nu"
	candidates, tokenStart := completeToken(line, len(line), ".")
	want := len("Cursed Energy: ")
	if tokenStart != want {
		t.Errorf("tokenStart = %d, want %d", tokenStart, want)
	}
	found := false
	for _, c := range candidates {
		if c == "nums.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("candidates = %v, missing nums.txt", candidates)
	}
}

func TestCompleteTokenNoMatch(t *testing.T) {
	candidates, _ := completeToken("zzz", 3, ".")
	if len(candidates) != 0 {
		t.Errorf("candidates = %v, want none", candidates)
	}
}
