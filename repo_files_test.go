package main_test

// Every source file this repository needs must actually be in it.
//
// This exists because one was not, for weeks, and nothing noticed. `.gitignore`
// carried `coverage.*` for Go's coverage profiles; that pattern is unanchored,
// so it also matched `cmd/domain/coverage.go` — the implementation of
// `domain expansion: coverage`. `git add` skipped the file in silence, every
// working tree that had created it kept building, and the break surfaced only
// when someone built from a source tarball, as a Nix build failing with
// `undefined: parseCoverageArgs`.
//
// The shape of that bug is what the test is for. A working tree cannot detect
// it: the file is right there. Only the difference between "what is on disk"
// and "what is committed" shows it, so that is what this checks.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoSourceFileIsGitIgnored fails if any file git would refuse to track
// looks like source rather than build output.
//
// The list of extensions is deliberately about *authorship*: a .go, .domain,
// .md or .nix file is something a person wrote and a build needs. Anything a
// tool emits — a coverage profile, a compiled binary, a wasm bundle — is
// expected to be ignored and is not the subject here.
func TestNoSourceFileIsGitIgnored(t *testing.T) {
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root,
		"ls-files", "--others", "--ignored", "--exclude-standard").Output()
	if err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Extensions whose files are written by people, not by builds.
	source := map[string]bool{
		".go": true, ".domain": true, ".nix": true, ".sh": true,
		".mod": true, ".sum": true, ".ts": true, ".tmLanguage": true,
	}
	var bad []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f == "" {
			continue
		}
		// node_modules and vendored trees are ignored on purpose and are full
		// of files with source extensions.
		if strings.Contains(f, "node_modules/") || strings.HasPrefix(f, "vendor/") {
			continue
		}
		if source[filepath.Ext(f)] {
			bad = append(bad, f)
		}
	}
	if len(bad) > 0 {
		t.Errorf("these source files are gitignored, so a clone or a source tarball "+
			"will not have them and will not build:\n  %s\n"+
			"Check .gitignore for an unanchored pattern — `coverage.*` once matched "+
			"cmd/domain/coverage.go this way.", strings.Join(bad, "\n  "))
	}
}

// TestEveryGoFileIsTracked is the same question asked the other way round, and
// catches a file that is merely untracked rather than ignored — one somebody
// created and forgot to add. It is the cheaper half of the check and the one
// that would have caught this on the very first commit.
func TestEveryGoFileIsTracked(t *testing.T) {
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root,
		"ls-files", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	var untracked []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f == "" || filepath.Ext(f) != ".go" {
			continue
		}
		if strings.Contains(f, "node_modules/") {
			continue
		}
		untracked = append(untracked, f)
	}
	if len(untracked) > 0 {
		t.Errorf("these Go files exist but are not tracked, so they are not in a "+
			"clone:\n  %s", strings.Join(untracked, "\n  "))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	return strings.TrimSpace(string(out))
}
