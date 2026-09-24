package main

import (
	"fmt"
	"io"
	"os"

	"domain/diag"
)

// printOpinions writes the compiler's performance warnings for a program that
// is about to run or be built: the costs it found and could not remove itself
// (see diag/opinions.go). Everything it could remove it already has, silently;
// --explain lists those.
//
// A program with nothing to say prints nothing. The warnings go to stderr, so
// the program's own output on stdout is untouched, and they never stop it
// running — they are advice, and the run is what was asked for.
func printOpinions(path string, stderr io.Writer) {
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	r := diag.Analyze(path, string(src))
	color := isColorTerminal(stderr)
	shown := 0
	for i := range r.Diags {
		d := &r.Diags[i]
		// Warnings only. The perf hints lint also carries — a stage stood
		// down for touching a global, an update that keeps its copy — are
		// prices for a choice the program made on purpose, and a program
		// built on globals has dozens: printed on every run they would bury
		// the advice that is actually new.
		if d.Code != "perf" || d.Severity != diag.Warning {
			continue
		}
		fmt.Fprint(stderr, diag.Render(d, path, color))
		fmt.Fprintln(stderr)
		shown++
	}
	if shown > 0 {
		fmt.Fprintf(stderr, "%d performance note(s) — the program runs either way; --quiet hides them\n\n", shown)
	}
}
