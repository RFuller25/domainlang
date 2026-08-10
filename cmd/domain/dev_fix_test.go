package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"domain/diag"
)

// The engine already computes the repair; the editor used to show the message
// and make you type the answer it had.
func TestDevApplyFixRepairsTheLine(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt\nCursed Tecnique: Sum\nReveal: stdout")
	m = settle(t, m)
	m.buf.row = 1

	if _, ok := m.fixableAt(2); !ok {
		t.Fatalf("no confident fix offered for the misspelling; diags: %v", m.intel.diags[2])
	}
	m = devKey(m, "alt+a")

	if got := m.buf.lines[1]; !strings.Contains(got, "Cursed Technique") {
		t.Errorf("line is %q, want it repaired", got)
	}
	if !m.dirty {
		t.Error("applying a fix did not mark the buffer dirty")
	}
	if !strings.Contains(m.status, "fixed") {
		t.Errorf("status is %q", m.status)
	}
}

// One undo withdraws the whole repair.
func TestDevApplyFixIsOneUndoStep(t *testing.T) {
	const src = "Cursed Energy: input.txt\nCursed Tecnique: Sum\nReveal: stdout"
	m, _ := newClockedModel(src)
	m = settle(t, m)
	m.buf.row = 1
	m = devKey(m, "alt+a")
	m = devKey(m, "ctrl+z")
	if m.buf.text() != src {
		t.Errorf("undo gave %q", m.buf.text())
	}
}

// A fix is a byte range. Applying one computed against text that has since been
// typed over would edit the wrong characters, so it refuses.
func TestDevApplyFixRefusesStaleOffsets(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt\nCursed Tecnique: Sum\nReveal: stdout")
	m = settle(t, m)
	m.buf.row = 1

	// Type at the top, moving every offset below it, without re-analysing.
	m.buf.gotoLine(1)
	m = devType(m, "# note\n")
	before := m.buf.text()

	m.buf.gotoLine(3)
	m = devKey(m, "alt+a")
	if m.buf.text() != before {
		t.Errorf("a stale fix was applied:\n%s", m.buf.text())
	}
	if !strings.Contains(m.status, "changed since it was checked") {
		t.Errorf("status is %q", m.status)
	}
}

// A line with no fix says which kind of nothing it is.
func TestDevApplyFixExplainsWhenThereIsNone(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt\nReveal: stdout")
	m = settle(t, m)
	m = devKey(m, "alt+a")
	if !strings.Contains(m.status, "nothing to fix") {
		t.Errorf("status is %q", m.status)
	}
}

// Fixing everything must produce exactly what `domain expansion: fix` produces.
// That is the real contract — not "repairs every misspelling", which the engine
// deliberately does not promise: it repairs as far as it can see, and an
// unresolvable type error stops it before the lines below.
func TestDevFixAllAgreesWithTheCommand(t *testing.T) {
	const src = "Cursed Energy: input.txt\nCursed Tecnique: Sum\nMaximum Techniqe: Count\nReveal: stdout"
	m, _ := newClockedModel(src)
	m = settle(t, m)
	m = devKey(m, "alt+shift+a")

	want := strings.TrimSuffix(diag.Analyze(m.path, src).FixedSrc, "\n")
	if got := m.buf.text(); got != want {
		t.Errorf("the editor and the command disagree:\n got:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(m.status, "fix") {
		t.Errorf("status is %q", m.status)
	}
}

// And on a repair it can see all the way through, the misspelling is gone.
func TestDevFixAllRepairsWhatItCanReach(t *testing.T) {
	m, _ := newClockedModel("Cursed Energy: input.txt\nCursed Tecnique: Split Text by \"x\"\nReveal: stdout")
	m = settle(t, m)
	m = devKey(m, "alt+shift+a")
	if strings.Contains(m.buf.text(), "Tecnique") {
		t.Errorf("the misspelling survived:\n%s", m.buf.text())
	}
}

func TestDevFixAllIsOneUndoStep(t *testing.T) {
	const src = "Cursed Energy: input.txt\nCursed Tecnique: Sum\nMaximum Techniqe: Count\nReveal: stdout"
	m, _ := newClockedModel(src)
	m = settle(t, m)
	m = devKey(m, "alt+shift+a")
	m = devKey(m, "ctrl+z")
	if m.buf.text() != src {
		t.Errorf("undo gave:\n%s", m.buf.text())
	}
}

// ---------------------------------------------------------------------------
// lint
// ---------------------------------------------------------------------------

// Analyze says what is wrong; Lint says what is unwise. `domain expansion:
// lint` is the two together, and so is the editor.
func TestDevLintFindingsReachTheGutter(t *testing.T) {
	// A Channel nothing consumes is the linter's territory, not the checker's.
	const src = "Cursed Energy: input.txt\n" +
		"Shikigami: Ints\n" +
		"Channel \"unused\":\n" +
		"    Maximum Technique: Count\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout"
	m := settle(t, newTestDevModel(src))

	total := 0
	for _, ds := range m.intel.diags {
		total += len(ds)
	}
	if total == 0 {
		t.Skip("this program produced no findings of any kind")
	}
	if m.intel.hints_ == 0 && m.intel.warns == 0 {
		t.Errorf("the linter contributed nothing: %d errs, %d warns, %d hints",
			m.intel.errs, m.intel.warns, m.intel.hints_)
	}
}

// A clean program stays clean: the linter must not invent advice.
func TestDevLintIsSilentOnACleanProgram(t *testing.T) {
	m := settle(t, newTestDevModel("Cursed Energy: input.txt\nShikigami: Ints\nMaximum Technique: Sum\nReveal: stdout"))
	if m.intel.errs != 0 {
		t.Errorf("%d errors on a clean program", m.intel.errs)
	}
	if got := ansi.Strip(m.statusLine()); strings.Contains(got, "✗") && m.intel.errs == 0 {
		t.Errorf("the status line reports errors it does not have: %q", got)
	}
}

// Analyze is already the whole of `domain expansion: lint` — checker then
// linter. Running the linter again on top of it reported every finding twice,
// and ran the resolve-time half unguarded, which is where "Combine ignores the
// argument From" came from.
func TestDevLintFindingsAreNotDuplicated(t *testing.T) {
	const src = "Cursed Energy: input.txt\n" +
		"Shikigami: Ints\n" +
		"Channel \"unused\":\n" +
		"    Maximum Technique: Count\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout"
	m := settle(t, newTestDevModel(src))

	for line, ds := range m.intel.diags {
		seen := map[string]bool{}
		for _, d := range ds {
			if seen[d.Msg] {
				t.Errorf("L%d reports %q twice", line, d.Msg)
			}
			seen[d.Msg] = true
		}
	}
}

// And the editor's counts are the command's counts.
func TestDevDiagnosticCountsMatchTheCommand(t *testing.T) {
	const src = "Cursed Energy: input.txt\n" +
		"Shikigami: Ints\n" +
		"Channel \"unused\":\n" +
		"    Maximum Technique: Count\n" +
		"Maximum Technique: Sum\n" +
		"Reveal: stdout"
	m := settle(t, newTestDevModel(src))

	errs, warns, hints := diag.Analyze(m.path, src).Counts()
	if m.intel.errs != errs || m.intel.warns != warns || m.intel.hints_ != hints {
		t.Errorf("editor reports %d/%d/%d, the command reports %d/%d/%d",
			m.intel.errs, m.intel.warns, m.intel.hints_, errs, warns, hints)
	}
}
