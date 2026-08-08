package codegen_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domain/codegen"
	"domain/interp"
	"domain/ir"
)

// Every oracle test in this package compares stdout, and both helpers fail the
// test when a backend exits non-zero — so a program that is *supposed* to fail
// cannot be compared at all. That is how Mode: Try came to drop a bad capture
// in the binary while the interpreter halted on it: the two disagreed only in
// the failure, which nothing looked at.
//
// These helpers return the failure instead of fataling on it, so refusal is
// oracle-testable the same way output is.

func interpreterOutcome(t *testing.T, pipe *ir.Pipeline, input []byte) (out string, failed bool, msg string) {
	t.Helper()
	var b bytes.Buffer
	ctx := &ir.Context{Stdin: bytes.NewReader(input), Stdout: &b}
	if _, err := interp.Run(pipe, ctx); err != nil {
		return b.String(), true, err.Error()
	}
	return b.String(), false, ""
}

func binaryOutcome(t *testing.T, pipe *ir.Pipeline, input []byte) (out string, failed bool, msg string) {
	t.Helper()
	goSrc, err := codegen.EmitProgram(pipe, codegen.Options{})
	if err != nil {
		t.Fatalf("EmitProgram: %v", err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog")
	if err := codegen.BuildBinary(goSrc, bin); err != nil {
		t.Fatalf("BuildBinary: %v\n--- generated source ---\n%s", err, goSrc)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), true, stderr.String()
	}
	return stdout.String(), false, ""
}

// A capture that fits the template's shape and then fails to convert is a
// broken line, not a different kind of line — so it stops the program in every
// mode, Try included. Skipping it would turn a corrupt input into a quietly
// short answer, and for a while that is exactly what the compiled binary did:
// the generated parse function reported a conversion failure with the same
// `false` it uses for a shape mismatch, which Try then dropped.
func TestBothBackendsRefuseTheSameBadCaptures(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles binaries; skipped in -short mode")
	}
	requireGo(t)

	const lines = `Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
`
	// Both templates below are all-int with safe literals, so they take the
	// compiler's hand-rolled scanner; the {w:word} ones take the regex path.
	// The overflow has to be exercised on both, since each converts its own way.
	cases := []struct {
		name, src, input string
	}{
		{"Try, scanner path", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "n={v:int}"
Reveal: stdout
`, "n=1\nn=99999999999999999999\nn=3\n"},

		{"Each, scanner path", lines + `Cursed Technique: Match Pattern
    Mode: Each
    Using: "n={v:int}"
Reveal: stdout
`, "n=1\nn=99999999999999999999\n"},

		{"Try, regex path", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "{w:word} {v:int}"
Reveal: stdout
`, "a 1\nb 99999999999999999999\nc 3\n"},

		{"Try, repeated hole", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "{vs:int+ sep=\",\"}"
Reveal: stdout
`, "1,2\n3,99999999999999999999\n4\n"},

		// The other half of the contract: a *shape* mismatch is the one thing
		// Try may drop, and both backends must drop it rather than fail.
		{"Try drops a shape mismatch in both", lines + `Cursed Technique: Match Pattern
    Mode: Try
    Using: "n={v:int}"
Reveal: stdout
`, "n=1\nnot a number at all\nn=3\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			input := []byte(c.input)
			var pipe *ir.Pipeline
			var wantOut string
			var wantFail bool
			var wantMsg string
			func() {
				frontEndMu.Lock()
				defer frontEndMu.Unlock()
				pipe = compilePipeline(t, c.src, true)
				wantOut, wantFail, wantMsg = interpreterOutcome(t, pipe, input)
			}()
			gotOut, gotFail, gotMsg := binaryOutcome(t, pipe, input)

			if gotFail != wantFail {
				t.Fatalf("the backends disagree about whether this program fails\n"+
					"interpreter: failed=%v %s\nbinary:      failed=%v %s\n\n%s",
					wantFail, wantMsg, gotFail, gotMsg, c.src)
			}
			if !wantFail && gotOut != wantOut {
				t.Errorf("stdout mismatch\ninterpreter: %q\nbinary:      %q", wantOut, gotOut)
			}
			// Not the whole message — the compiled one carries no source
			// position — but the part that says what went wrong must agree, or
			// the two are refusing for different reasons.
			if wantFail {
				const gist = "is not a valid integer"
				if strings.Contains(wantMsg, gist) != strings.Contains(gotMsg, gist) {
					t.Errorf("the backends refuse for different reasons\n"+
						"interpreter: %s\nbinary:      %s", wantMsg, strings.TrimSpace(gotMsg))
				}
			}
		})
	}
}
