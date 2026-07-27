package main

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestReplTTYCtrlCQuits(t *testing.T) {
	m := newReplModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned a nil command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c did not return tea.Quit")
	}
}

func TestReplTTYSimpleStatementEvaluates(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("3\n1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReplModel()
	m.ti.SetValue("Cursed Energy: nums.txt")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(replModel)

	if got := m.ti.Value(); got != "" {
		t.Errorf("input not cleared after submit: %q", got)
	}
	if got := m.ti.Prompt; got != "domain> " {
		t.Errorf("prompt should stay top-level: %q", got)
	}
	if !strings.Contains(m.buf.String(), `=> "3\n1\n2" : Text`) {
		t.Errorf("missing evaluated result:\n%s", m.buf.String())
	}
}

func TestReplTTYNeedsBlockAutoIndentsAndCompletes(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1\n2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReplModel()
	submit := func(value string) {
		m.ti.SetValue(value)
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(replModel)
	}

	submit("Cursed Energy: nums.txt")
	submit(`Cursed Technique: Split Text by "\n"`)
	submit("Channeled Energy: Convert To Integers")
	submit("Cursed Technique: Map Each") // needs Using: — enters continuation mode

	if len(m.core.pending) == 0 {
		t.Fatal("expected a pending block after a statement needing Using:")
	}
	if got := m.ti.Value(); got != "    " {
		t.Errorf("next line not auto-indented: %q", got)
	}
	if got := m.ti.Prompt; got != "   ...> " {
		t.Errorf("prompt not switched to continuation: %q", got)
	}

	submit("    Using: (x) -> x * 10")
	if got := m.ti.Value(); got != "    " {
		t.Errorf("continuation line not re-seeded with the 4-space indent: %q", got)
	}

	submit("    ") // blank but for the auto-inserted indent — ends the block
	if len(m.core.pending) != 0 {
		t.Error("block did not end on the seeded-but-otherwise-empty line")
	}
	if !strings.Contains(m.buf.String(), "=> [10, 20] : List<Int>") {
		t.Errorf("block statement result missing:\n%s", m.buf.String())
	}
}

func TestReplTTYForcedContinuationViaCtrlOrAltEnter(t *testing.T) {
	for _, mod := range []tea.KeyMod{tea.ModCtrl, tea.ModAlt} {
		t.Chdir(t.TempDir())
		if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
			t.Fatal(err)
		}
		m := newReplModel()
		m.ti.SetValue("Cursed Energy: nums.txt") // a complete statement on its own

		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: mod})
		m = next.(replModel)

		if len(m.core.pending) == 0 {
			t.Fatalf("mod %v: did not force a pending block", mod)
		}
		if len(m.core.stmts) != 0 {
			t.Errorf("mod %v: should not have evaluated the statement on its own", mod)
		}
		if got := m.ti.Value(); got != "    " {
			t.Errorf("mod %v: next line not auto-indented: %q", mod, got)
		}
	}
}

func TestReplTTYCtrlEnterNoOpOnEmptyLine(t *testing.T) {
	m := newReplModel()
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	m2 := next.(replModel)
	if cmd != nil {
		t.Error("ctrl+enter on an empty line should not print anything")
	}
	if len(m2.core.pending) != 0 || len(m2.history) != 0 {
		t.Error("ctrl+enter on an empty line should be a total no-op")
	}
}

func TestReplTTYHistoryRecall(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newReplModel()
	submit := func(value string) {
		m.ti.SetValue(value)
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(replModel)
	}
	submit("Cursed Energy: nums.txt")
	submit(":type")

	press := func(code rune) {
		next, _ := m.Update(tea.KeyPressMsg{Code: code})
		m = next.(replModel)
	}

	press(tea.KeyUp)
	if got := m.ti.Value(); got != ":type" {
		t.Errorf("up did not recall the most recent line: %q", got)
	}
	press(tea.KeyUp)
	if got := m.ti.Value(); got != "Cursed Energy: nums.txt" {
		t.Errorf("second up did not recall the earlier line: %q", got)
	}
	press(tea.KeyDown)
	if got := m.ti.Value(); got != ":type" {
		t.Errorf("down did not step forward in history: %q", got)
	}
	press(tea.KeyDown)
	if got := m.ti.Value(); got != "" {
		t.Errorf("down past the end should clear the line: %q", got)
	}
}

func TestReplTTYTabCompletesUniqueMatch(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("Cursed T")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(replModel)

	if got := m.ti.Value(); got != "Cursed Technique: " {
		t.Fatalf("tab did not complete the keyword: %q", got)
	}
	if !m.completing {
		t.Error("expected completing to be true right after Tab")
	}
}

func TestReplTTYTabCyclesThroughMultipleCandidates(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("Domain Expansion: Sort")
	press := func() {
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(replModel)
	}

	press()
	first := m.ti.Value()
	if first != "Domain Expansion: Sort" && first != "Domain Expansion: Sort By" {
		t.Fatalf("unexpected first completion: %q", first)
	}

	press()
	second := m.ti.Value()
	if second == first {
		t.Fatal("second tab did not advance to a different candidate")
	}
	if second != "Domain Expansion: Sort" && second != "Domain Expansion: Sort By" {
		t.Fatalf("unexpected second completion: %q", second)
	}

	press()
	third := m.ti.Value()
	if third != first {
		t.Errorf("third tab should wrap back to the first candidate: got %q, want %q", third, first)
	}
}

func TestReplTTYTabCompletionResetsOnOtherKey(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("Cursed T")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(replModel)

	next, _ = m.Update(tea.KeyPressMsg{Text: "x"})
	m = next.(replModel)

	if m.completing {
		t.Error("typing a character should exit completion cycling")
	}
	if got := m.ti.Value(); got != "Cursed Technique: x" {
		t.Errorf("typed character should append after the accepted completion: %q", got)
	}
}

func TestReplTTYTabNoMatchIsNoOp(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("zzz")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m2 := next.(replModel)

	if cmd != nil {
		t.Error("no-match tab should not print anything")
	}
	if got := m2.ti.Value(); got != "zzz" {
		t.Errorf("no-match tab should leave the line untouched: %q", got)
	}
	if m2.completing {
		t.Error("no-match tab should not enter completing state")
	}
}

func TestReplTTYTabCompletesReplCommandWithoutDoublingColon(t *testing.T) {
	// Regression: completeToken's :command candidates already include the
	// leading ':', so a tokenStart that also preserves the line's own ':'
	// would splice in a second one ("::load").
	m := newReplModel()
	m.ti.SetValue(":lo")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(replModel)

	if got := m.ti.Value(); got != ":load" {
		t.Errorf("tab did not complete the :command cleanly: %q", got)
	}
}
