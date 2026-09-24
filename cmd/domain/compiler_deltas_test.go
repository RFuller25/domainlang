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

// compiler.md's "documented semantic deltas" are the two places the two
// backends are allowed to differ. Everything else about them is promised
// identical, so a delta that quietly closes — or quietly widens — is a claim
// on that page going wrong with nothing to say so.
//
// One of them had already closed. The page said an int capture overflowing
// int64 "is reported by the interpreter as an invalid capture and by the
// binary as a non-matching line"; the binary reports the invalid capture too,
// and has in every Match Pattern mode. What is left between them is the
// position and the primitive name, which is what the bullet now says.

// `Mode: One` reads a whole Text; the other three read a list of lines.
func overflowProgram(mode string) (src, input string) {
	const stage = `Cursed Technique: Match Pattern
    Mode: ` + "%s" + `
    Using: "n={v:int}"
Reveal: stdout
`
	if mode == "One" {
		return "Cursed Energy: stdin\n" + strings.Replace(stage, "%s", mode, 1),
			"n=99999999999999999999999"
	}
	return "Cursed Energy: stdin\nCursed Technique: Split Text by \"\\n\"\n" +
			strings.Replace(stage, "%s", mode, 1),
		"n=1\nn=99999999999999999999999\n"
}

func TestOverflowingCaptureReadsTheSameInBothBackends(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a binary per mode; skipped in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	const diagnosis = `captured "99999999999999999999999" is not a valid integer`

	for _, mode := range []string{"One", "Each", "Try", "Scan"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			prog := filepath.Join(dir, "p.domain")
			src, input := overflowProgram(mode)
			if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}

			var out, errBuf bytes.Buffer
			err := Execute(prog, Options{Optimize: true}, strings.NewReader(input), &out, &errBuf)
			if err == nil {
				t.Fatalf("interpreting did not fail; it printed %q", out.String())
			}
			interp := err.Error()
			if !strings.Contains(interp, diagnosis) {
				t.Errorf("interpreted: %q\ndoes not carry the diagnosis %q", interp, diagnosis)
			}
			// The page says the interpreter's is the one with a position and a
			// primitive name on it.
			if !strings.Contains(interp, "(in Match Pattern)") {
				t.Errorf("interpreted error no longer names the primitive: %q", interp)
			}

			t.Chdir(dir)
			var bout, berr bytes.Buffer
			buildErr := Build(prog, BuildOptions{Optimize: true, Run: true},
				strings.NewReader(input), &bout, &berr)
			if buildErr == nil {
				t.Fatalf("the compiled binary did not fail; it printed %q", bout.String())
			}
			compiled := buildErr.Error() + berr.String()
			if !strings.Contains(compiled, diagnosis) {
				t.Errorf("compiled: %q\ndoes not carry the same diagnosis %q", compiled, diagnosis)
			}
			if strings.Contains(compiled, "(in Match Pattern)") {
				t.Errorf("the compiled binary now names the primitive too — compiler.md says it does not: %q", compiled)
			}
		})
	}

	// And the page still describes it this way.
	page, err := docs.FS.ReadFile("compiler.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"The *diagnosis* is the same one",
		"`captured \"…\" is not a valid integer`",
	} {
		if !strings.Contains(string(page), want) {
			t.Errorf("compiler.md no longer says %q — update this test with the page", want)
		}
	}
}

// Delta one: a relative `Cursed Energy:` target resolves against the program's
// own directory when interpreted and against the working directory when
// compiled, and both fall back to stdin when the file is not there. Two files
// of the same name, one beside each, is the only arrangement that tells the
// two apart — and the only one that would notice either half changing.
func TestInputPathResolutionDiffersAsDocumented(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a binary; skipped in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	const program = `Cursed Energy: data.txt
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
`
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(sub, "p.domain")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(prog, program)
	write(filepath.Join(sub, "data.txt"), "1,2,3")    // beside the program
	write(filepath.Join(dir, "data.txt"), "10,20,30") // beside the caller
	t.Chdir(dir)

	run := func(name string, fn func(out, errBuf *bytes.Buffer) error) string {
		t.Helper()
		var out, errBuf bytes.Buffer
		if err := fn(&out, &errBuf); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, errBuf.String())
		}
		return strings.TrimSpace(out.String())
	}

	interpreted := run("interpreting", func(out, errBuf *bytes.Buffer) error {
		return Execute(prog, Options{Optimize: true}, strings.NewReader(""), out, errBuf)
	})
	if interpreted != "6" {
		t.Errorf("interpreted read %s, want 6 — the file beside the program", interpreted)
	}

	compiled := run("building", func(out, errBuf *bytes.Buffer) error {
		return Build(prog, BuildOptions{Optimize: true, Run: true}, strings.NewReader(""), out, errBuf)
	})
	if compiled != "60" {
		t.Errorf("the compiled binary read %s, want 60 — the file beside the caller", compiled)
	}

	// With neither file present both fall back to stdin, which is the half of
	// the bullet that makes the delta survivable.
	for _, p := range []string{filepath.Join(sub, "data.txt"), filepath.Join(dir, "data.txt")} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	viaStdin := run("interpreting from stdin", func(out, errBuf *bytes.Buffer) error {
		return Execute(prog, Options{Optimize: true}, strings.NewReader("7,8"), out, errBuf)
	})
	viaStdinCompiled := run("running the binary from stdin", func(out, errBuf *bytes.Buffer) error {
		return Build(prog, BuildOptions{Optimize: true, Run: true}, strings.NewReader("7,8"), out, errBuf)
	})
	if viaStdin != "15" || viaStdinCompiled != "15" {
		t.Errorf("with the file missing: interpreted %s, compiled %s, want 15 from both",
			viaStdin, viaStdinCompiled)
	}
}
