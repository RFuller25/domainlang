package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domain/docs"
)

// The documentation's examples, executed.
//
// docs/examples_test.go already parses every ```domain block and resolves the
// ones that are whole programs, which catches a renamed primitive. What it
// cannot catch is a wrong *answer*: every output printed in the reference was
// prose, typed by hand, and nothing ever compared it to what the program does.
// A block that opts in with ```domain run carries its input and its expected
// stdout beside it, and this runs it.
//
// It lives in cmd/domain rather than docs because Execute and Build are in
// package main, which cannot be imported. The block extraction is shared
// (docs.Examples), so the two packages cannot disagree about what a `run`
// block is.

// runnableExamples collects every declared example across the embedded site.
func runnableExamples(t *testing.T) []docs.Example {
	t.Helper()
	pages, err := docs.Pages()
	if err != nil {
		t.Fatal(err)
	}
	var out []docs.Example
	for _, p := range pages {
		src, err := docs.FS.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		exs, problem := docs.Examples(p, string(src))
		if problem != "" {
			t.Error(problem)
			continue
		}
		out = append(out, exs...)
	}
	return out
}

// stage writes an example into its own directory: the program, and its input
// under whatever name the program's Cursed Energy stage asks for.
func stage(t *testing.T, ex docs.Example) (prog, dir string) {
	t.Helper()
	dir = t.TempDir()
	prog = filepath.Join(dir, "example.domain")
	if err := os.WriteFile(prog, []byte(ex.Block.Source), 0o644); err != nil {
		t.Fatal(err)
	}
	// Imported libraries, so an `Innate Domain:` example has something to
	// resolve against.
	for name, src := range ex.Libs {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A program that names a file gets one; one that reads stdin gets it on
	// stdin. Writing the file in both cases would be harmless but misleading,
	// since `Cursed Energy: <file>` falls back to stdin when the file is absent
	// and a test should not depend on which path it took.
	if file, stdin := ex.Source(); !stdin {
		path := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(ex.Input), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return prog, dir
}

// stdinFor returns the input to pipe: the declared input for a stdin-reading
// program, nothing for one that reads a file it has already been given.
func stdinFor(ex docs.Example) string {
	if _, stdin := ex.Source(); stdin {
		return ex.Input
	}
	return ""
}

// Every ```domain run block in the docs produces the output printed beneath
// it, in both optimizer modes — the naive run is the oracle that says the
// optimizer did not change the answer, exactly as TestExamples does for
// examples/.
func TestDocExamplesRun(t *testing.T) {
	examples := runnableExamples(t)
	for _, ex := range examples {
		t.Run(ex.Name, func(t *testing.T) {
			prog, _ := stage(t, ex)
			for _, opt := range []bool{true, false} {
				var out, errBuf bytes.Buffer
				err := Execute(prog, Options{Optimize: opt},
					strings.NewReader(stdinFor(ex)), &out, &errBuf)
				if err != nil {
					t.Fatalf("optimize=%v: %v\n%s", opt, err, ex.Block.Source)
				}
				if got := strings.TrimRight(out.String(), "\n"); got != ex.Output {
					t.Errorf("optimize=%v: the ```output block is wrong\n got: %q\nwant: %q\n%s",
						opt, got, ex.Output, ex.Block.Source)
				}
			}
		})
	}
	t.Logf("ran %d documented examples", len(examples))
}

// The same examples, compiled. compiler.md claims byte-identical stdout
// between the two backends; the documentation is a corpus that claim should
// hold over too, and it is the corpus a reader actually copies from.
func TestDocExamplesCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a binary per example; skipped in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	for _, ex := range runnableExamples(t) {
		t.Run(ex.Name, func(t *testing.T) {
			prog, dir := stage(t, ex)
			// A compiled binary resolves a `Cursed Energy:` path against the
			// working directory rather than the program's own directory — a
			// documented delta (see compiler.md). Running from the staged
			// directory is what makes an `input.txt` example testable in both
			// backends rather than only in the interpreter.
			t.Chdir(dir)
			var out, errBuf bytes.Buffer
			opts := BuildOptions{Optimize: true, Run: true}
			if err := Build(prog, opts, strings.NewReader(stdinFor(ex)), &out, &errBuf); err != nil {
				t.Fatalf("build: %v\n%s\n%s", err, errBuf.String(), ex.Block.Source)
			}
			if got := strings.TrimRight(out.String(), "\n"); got != ex.Output {
				t.Errorf("compiled output differs from the ```output block\n got: %q\nwant: %q\n%s",
					got, ex.Output, ex.Block.Source)
			}
		})
	}
}
