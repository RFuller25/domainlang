package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The suggester's correctness lives in package shape, tested against the
// repository's own programs. These are about the offer: that it appears when
// an input is chosen, that declining leaves the program alone, and that
// accepting puts the statements somewhere they will resolve.

// withInput writes an input beside the program and returns a model editing it.
func withInput(t *testing.T, prog, input string) (devModel, string) {
	t.Helper()
	dir := t.TempDir()
	inPath := filepath.Join(dir, "day7.txt")
	if err := os.WriteFile(inPath, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestDevModel(prog)
	m.path = filepath.Join(dir, "p.domain")
	return m, inPath
}

// Choosing an input is when its shape is worth reading, so the offer comes
// then rather than waiting to be asked.
func TestDevChoosingAnInputOffersAnOpening(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "1000\n2000\n\n3000\n")

	p := newPicker(":load", filepath.Dir(inPath))
	p.anyFile = true
	m.picker, m.pickingInput = p, true
	next, _ := m.pickerKey(devKeyMsg("esc")) // cancelled: no offer
	if next.(devModel).suggest != nil {
		t.Error("cancelling the browser still offered an opening")
	}

	m.picker, m.pickingInput = p, true
	m = m.acceptPick(t, inPath)
	if m.suggest == nil {
		t.Fatal("choosing an input offered nothing")
	}
	if !strings.Contains(m.suggest.candidates[0].First(), `Split Text by "\n\n"`) {
		t.Errorf("first suggestion is %q", m.suggest.candidates[0].First())
	}
}

// acceptPick drives the picker to a chosen path, standing in for the keystrokes
// that would have walked to it.
func (m devModel) acceptPick(t *testing.T, path string) devModel {
	t.Helper()
	saving, forInput := m.picker.saving(), m.pickingInput
	m.picker, m.pickingInput = nil, false
	if saving || !forInput {
		t.Fatal("this helper is for the input browser")
	}
	m, _ = m.bindInput(path)
	if b, err := os.ReadFile(path); err == nil {
		if sg, ok := suggestFor(filepath.Base(path), string(b)); ok {
			m.suggest = sg
		}
	}
	return m
}

// Declining leaves the program exactly as it was — inserting code uninvited is
// a bad way to be right.
func TestDevDecliningASuggestionLeavesTheProgramAlone(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "1000\n2000\n\n3000\n")
	m.picker, m.pickingInput = newPicker(":load", filepath.Dir(inPath)), true
	m = m.acceptPick(t, inPath)
	before := m.buf.text()

	m = devKey(m, "esc")
	if m.suggest != nil {
		t.Error("esc did not close the offer")
	}
	if m.buf.text() != before {
		t.Errorf("declining changed the program:\n%s", m.buf.text())
	}
}

// The statements go after the source stage, because the opening reads the
// value the source produced. Putting them first would write a program that
// does not resolve.
func TestDevAcceptedSuggestionLandsAfterTheSourceStage(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "1000\n2000\n\n3000\n")
	m.picker, m.pickingInput = newPicker(":load", filepath.Dir(inPath)), true
	m = m.acceptPick(t, inPath)

	m = devKey(m, "enter")
	if m.suggest != nil {
		t.Error("enter did not close the offer")
	}
	lines := m.buf.lines
	if !strings.HasPrefix(lines[0], "Cursed Energy:") {
		t.Fatalf("the source stage moved: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Split Text") {
		t.Errorf("the opening did not land after the source stage:\n%s", m.buf.text())
	}
	if !strings.Contains(m.buf.text(), "Reveal: stdout") {
		t.Error("the rest of the program was lost")
	}
	if !m.dirty {
		t.Error("inserting did not mark the buffer dirty")
	}
}

// An inserted opening resolves — which is the whole reason it goes where it
// goes. This runs the real front end over the result.
func TestDevAcceptedSuggestionProducesAProgramThatResolves(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "1000\n2000\n\n3000\n")
	m.picker, m.pickingInput = newPicker(":load", filepath.Dir(inPath)), true
	m = m.acceptPick(t, inPath)
	m = devKey(m, "enter")

	if _, err := devResolve(m.buf.text(), m.path); err != nil {
		t.Errorf("the suggested program does not resolve: %v\n%s", err, m.buf.text())
	}
}

// Every ranked opening is offered, and moving through them changes what would
// be inserted.
func TestDevSuggestionsCanBeChosenBetween(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "1163751742\n1381373672\n2136511328\n")
	m.picker, m.pickingInput = newPicker(":load", filepath.Dir(inPath)), true
	m = m.acceptPick(t, inPath)

	if len(m.suggest.candidates) < 2 {
		t.Fatalf("only %d suggestions for an ambiguous input", len(m.suggest.candidates))
	}
	first := m.suggest.candidates[0].First()
	m = devKey(m, "down")
	second := m.suggest.candidates[m.suggest.cursor].First()
	if first == second {
		t.Error("moving did not change the highlighted suggestion")
	}
	m = devKey(m, "enter")
	if !strings.Contains(m.buf.text(), second) {
		t.Errorf("the second suggestion was not the one inserted:\n%s", m.buf.text())
	}
}

// The overlay shows the evidence, not just the code: a reason is what makes
// the choice informed rather than trusted.
func TestDevSuggestionViewShowsTheEvidence(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "..#..\n#...#\n#####\n")
	m.picker, m.pickingInput = newPicker(":load", filepath.Dir(inPath)), true
	m = m.acceptPick(t, inPath)

	view := ansi.Strip(m.suggestView())
	if !strings.Contains(view, "Shikigami: Lines") {
		t.Errorf("the opening is missing:\n%s", view)
	}
	if !strings.Contains(view, "same width") {
		t.Errorf("the evidence is missing:\n%s", view)
	}
	// The whole of a multi-line opening is shown, so it is not a surprise once
	// it lands in the program.
	if !strings.Contains(view, "Convert To Grid") {
		t.Errorf("the rest of the statements are hidden:\n%s", view)
	}
}

// Accepting is one undo step.
func TestDevSuggestionIsOneUndoStep(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "1000\n2000\n\n3000\n")
	m.picker, m.pickingInput = newPicker(":load", filepath.Dir(inPath)), true
	m = m.acceptPick(t, inPath)
	before := m.buf.text()

	m = devKey(m, "enter")
	m = devKey(m, "ctrl+z")
	if m.buf.text() != before {
		t.Errorf("undo gave:\n%s\nwant:\n%s", m.buf.text(), before)
	}
}

// Asking again without an input says what to do rather than doing nothing.
func TestDevSuggestWithoutAnInputExplainsItself(t *testing.T) {
	m := newTestDevModel("Reveal: stdout")
	m = devKey(m, "alt+i")
	if m.suggest != nil {
		t.Error("something was suggested with no input chosen")
	}
	if !strings.Contains(m.status, "choose an input") {
		t.Errorf("status is %q", m.status)
	}
}

// And the key reopens the offer for an input already chosen.
func TestDevSuggestKeyReopensTheOffer(t *testing.T) {
	m, inPath := withInput(t, "Cursed Energy: old.txt\nReveal: stdout", "1000\n2000\n\n3000\n")
	m.picker, m.pickingInput = newPicker(":load", filepath.Dir(inPath)), true
	m = m.acceptPick(t, inPath)
	m = devKey(m, "esc")

	m = devKey(m, "alt+i")
	if m.suggest == nil {
		t.Error("alt+i did not reopen the offer")
	}
}
