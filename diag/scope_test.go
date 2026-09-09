package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The migration this change owes its users: every program written before
// scopes existed says `Innate Domain: aoc` and means "import a library". The
// diagnostic has to recognize that and offer the keyword that now means it —
// being told "unknown Innate Domain" and nothing else is true and useless.
func TestOldImportFormIsDiagnosedAndFixed(t *testing.T) {
	dir := t.TempDir()
	lib := "Shikigami \"Total\"\n" +
		"    Cursed Technique: Split Text by \",\"\n" +
		"    Channeled Energy: Convert To Integers\n" +
		"    Maximum Technique: Sum\n"
	if err := os.WriteFile(filepath.Join(dir, "aoc.domain"), []byte(lib), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "p.domain")
	src := "Innate Domain: aoc\n\nCursed Energy: stdin\nShikigami: Total\nReveal: stdout\n"
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// Analyze applies confident fixes and re-analyzes, so the end state is
	// what a user sees: the program repairs itself and the report is clean.
	r := Analyze(prog, src)
	if r.Applied == 0 {
		t.Fatalf("no fix was applied; diagnostics: %+v", r.Diags)
	}
	if !strings.HasPrefix(r.FixedSrc, "Inherited Technique: aoc\n") {
		t.Errorf("fixed source starts %q, want the Inherited Technique keyword",
			strings.SplitN(r.FixedSrc, "\n", 2)[0])
	}
	// The repaired source must actually resolve — the point of the fix is a
	// working program, not a different error.
	if diags, _, _ := frontEnd(prog, r.FixedSrc); len(diags) != 0 {
		t.Errorf("the repaired program still fails: %+v", diags)
	}
}

// The diagnostic itself, before any fix is applied: it names the library, the
// keyword that now imports one, and carries a confident repair.
func TestOldImportFormDiagnosticNamesTheNewKeyword(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "aoc.domain"),
		[]byte("Shikigami \"Total\"\n    Maximum Technique: Sum\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "Innate Domain: aoc\n\nCursed Energy: stdin\nReveal: stdout\n"
	prog := filepath.Join(dir, "p.domain")
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	diags, _, _ := frontEnd(prog, src)
	var found *Diagnostic
	for i := range diags {
		if strings.Contains(diags[i].Msg, "unknown Innate Domain") {
			found = &diags[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no diagnostic about the Innate Domain; got %+v", diags)
	}
	if !strings.Contains(found.Help, "Inherited Technique: aoc") {
		t.Errorf("help = %q, want it to offer the Inherited Technique keyword", found.Help)
	}
	if !found.HasConfidentFix() {
		t.Fatal("the fix should be confident: the target is a library that exists")
	}
	fixed := src[:found.Fix.Start] + found.Fix.Replacement + src[found.Fix.End:]
	if !strings.HasPrefix(fixed, "Inherited Technique: aoc\n") {
		t.Errorf("applying the fix gave %q", strings.SplitN(fixed, "\n", 2)[0])
	}
}

// A target that is not a library gets the other message: this build's scopes,
// and a note about the keyword that imports.
func TestUnknownScopeThatIsNotALibrary(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "p.domain")
	src := "Innate Domain: Cursed Womb\nCursed Energy: stdin\nReveal: stdout\n"
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	diags, _, _ := frontEnd(prog, src)
	var found *Diagnostic
	for i := range diags {
		if strings.Contains(diags[i].Msg, "unknown Innate Domain") {
			found = &diags[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no diagnostic about the Innate Domain; got %+v", diags)
	}
	if !strings.Contains(found.Help, "Advent of Code") {
		t.Errorf("help = %q, want it to list the scopes this build has", found.Help)
	}
	if found.HasConfidentFix() {
		t.Error("there is no confident fix for a name that is neither a scope nor a library")
	}
}
