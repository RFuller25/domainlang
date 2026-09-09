package interp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every whole program is run through its scope's host.
//
// This is the rule the whole scope mechanism rests on, and it is the one that
// fails silently when it is broken. interp.Run threads a value through a
// linear chain of nodes; a `Game Dev` pipeline is not one, so running it that
// way walks its Parts in source order as passthroughs and prints nothing. Not
// an error, not a crash — a program that produces no output and no complaint.
//
// There were nine callers when scopes arrived and all nine were converted. The
// tenth is the risk: somebody adds a tool, reaches for the function whose name
// says "run", and the failure shows up as a blank screen in whichever corner
// of the toolbelt they were not thinking about. So the rule is enforced over
// the source rather than remembered.
//
// interp's own files are exempt: Run is defined there, and HostFor's default
// is a reference to it.
func TestNothingRunsAPipelineOutsideItsHost(t *testing.T) {
	var offenders []string
	scopedDirs := map[string]bool{}  // packages that run a whole program
	importsGame := map[string]bool{} // packages that ask for the hosts

	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "interp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		for i, line := range strings.Split(string(src), "\n") {
			// The call, not the word: a comment naming interp.Run — and this
			// package's documentation names it often — is not a caller.
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "interp.Run(") {
				offenders = append(offenders, fmt.Sprintf("%s:%d", filepath.ToSlash(path), i+1))
			}
			if strings.Contains(code, "interp.RunScoped(") {
				scopedDirs[dir] = true
			}
			if strings.Contains(code, `"domain/game"`) {
				importsGame[dir] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("these call interp.Run directly on a whole program:\n  %s\n\n"+
			"Use interp.RunScoped, which runs it under the host its Innate Domain asks for. "+
			"interp.Run walks a linear chain of nodes, and a program whose scope says it is "+
			"not one produces no output and no error that way.",
			strings.Join(offenders, "\n  "))
	}

	// RunScoped only reaches a host that something linked in. A package that
	// runs whole programs and does not import the hosts gets interp.Run back
	// by the other door — HostFor's fallback — with the same silent result.
	var unregistered []string
	for dir := range scopedDirs {
		if !importsGame[dir] {
			unregistered = append(unregistered, dir)
		}
	}
	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Errorf("these run whole programs but do not import the hosts:\n  %s\n\n"+
			"Add `_ \"domain/game\"`. A host registers itself from an init, so RunScoped in a "+
			"package that never linked one falls back to interp.Run and a game produces nothing.",
			strings.Join(unregistered, "\n  "))
	}
	if len(scopedDirs) == 0 {
		t.Error("no package calls interp.RunScoped, so this test is checking nothing")
	}
}

// TestHostForFallsBackToRun: a scope that registers nothing runs the way every
// program always has. Registering a host is something only a scope that needs
// one does.
func TestHostForFallsBackToRun(t *testing.T) {
	if HasHost("Advent of Code") {
		t.Error("the default scope should not register a host of its own")
	}
	if HasHost("") {
		t.Error("a pipeline with no scope should not have a host of its own")
	}
}
