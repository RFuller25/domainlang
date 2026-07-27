// The interactive line editor for `domain repl` on a real terminal: arrow-key
// cursor movement and history recall (via bubbles/textinput), plus
// auto-indented continuation lines for Using:/Channel/Shikigami blocks. Piped
// input and tests use the plain reader in repl.go instead — see Repl.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// replTTY runs the interactive loop over a real terminal.
func replTTY(stdin *os.File, stdout io.Writer) int {
	p := tea.NewProgram(newReplModel(), tea.WithInput(stdin), tea.WithOutput(stdout))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(stdout, "error: %v\n", err)
		return 1
	}
	return 0
}

// replModel is the bubbletea Model driving one interactive session. It wraps
// the same repl core repl.go's plain reader uses; core.out is a buffer this
// model reads from after every line so the REPL's own writes (results,
// errors, :command output) can be re-emitted through tea.Println instead of
// being written straight to the terminal, which raw mode does not allow.
type replModel struct {
	ti      textinput.Model
	core    *repl
	buf     *strings.Builder
	seen    int // bytes of buf already echoed via tea.Println
	history []string
	histIdx int

	completing bool
	candidates []string
	candIdx    int
	tokenStart int
}

func newReplModel() replModel {
	ti := textinput.New()
	ti.Prompt = "domain> "
	ti.Focus()
	buf := &strings.Builder{}
	return replModel{
		ti:   ti,
		buf:  buf,
		core: &repl{out: buf, baseDir: "."},
	}
}

func (m replModel) Init() tea.Cmd {
	return tea.Println("Domain REPL — an interactive domain expansion. :help lists commands, :quit leaves.")
}

func (m replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() != "tab" {
			m.completing = false
			m.candidates = nil
		}
		switch msg.String() {
		case "tab":
			return m.completeTab()
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+d":
			if m.ti.Value() == "" {
				return m, tea.Quit
			}
		case "up":
			if m.histIdx > 0 {
				m.histIdx--
				m.ti.SetValue(m.history[m.histIdx])
				m.ti.CursorEnd()
			}
			return m, nil
		case "down":
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.ti.SetValue(m.history[m.histIdx])
				m.ti.CursorEnd()
			} else {
				m.histIdx = len(m.history)
				m.ti.SetValue("")
			}
			return m, nil
		case "enter", "ctrl+enter", "alt+enter":
			line := strings.TrimRight(m.ti.Value(), " \t\r")
			force := msg.String() != "enter" && len(m.core.pending) == 0
			if force && line == "" {
				return m, nil
			}
			var quit bool
			if force {
				m.core.pending = []string{line}
			} else {
				quit = m.core.handleLine(line)
			}
			cmd := m.submitLine(line)
			if quit {
				return m, tea.Sequence(cmd, tea.Quit)
			}
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

// submitLine records line in history, echoes it (plus whatever the repl core
// wrote for it) to permanent scrollback, and resets the editor for the next
// line — pre-seeded with a tab while a block is still pending.
func (m *replModel) submitLine(line string) tea.Cmd {
	m.history = append(m.history, line)
	m.histIdx = len(m.history)
	echoed := m.ti.Prompt + line

	out := strings.TrimSuffix(m.buf.String()[m.seen:], "\n")
	m.seen = m.buf.Len()

	// textinput sanitizes literal tabs down to a single space (it's a
	// single-line widget), so seed with repl.go's own 4-space indent
	// convention instead of a tab — it survives SetValue untouched.
	seed := ""
	if len(m.core.pending) > 0 {
		seed = "    "
	}
	m.ti.SetValue(seed)
	m.ti.SetCursor(len(seed))
	m.ti.Prompt = m.prompt()

	if out == "" {
		return tea.Println(echoed)
	}
	return tea.Println(echoed + "\n" + out)
}

func (m replModel) prompt() string {
	if len(m.core.pending) > 0 {
		return "   ...> "
	}
	return "domain> "
}

// completeTab starts or advances a Tab-completion cycle: the first Tab on a
// token computes candidates via completeToken and shows the first one;
// each subsequent Tab (while still completing) advances to the next,
// wrapping around. Any other key (handled in Update, above) resets the
// cycle before its own handling runs.
func (m replModel) completeTab() (tea.Model, tea.Cmd) {
	value := m.ti.Value()
	cursor := m.ti.Position() // assumed ASCII up to the cursor, like the rest of the REPL

	if m.completing {
		m.candIdx = (m.candIdx + 1) % len(m.candidates)
	} else {
		candidates, tokenStart := completeToken(value, cursor, m.core.baseDir)
		if len(candidates) == 0 {
			return m, nil
		}
		m.candidates = candidates
		m.tokenStart = tokenStart
		m.candIdx = 0
		m.completing = true
	}

	candidate := m.candidates[m.candIdx]
	newValue := value[:m.tokenStart] + candidate + value[cursor:]
	m.ti.SetValue(newValue)
	m.ti.SetCursor(m.tokenStart + len(candidate))
	return m, nil
}

func (m replModel) View() tea.View {
	return tea.NewView(m.ti.View())
}
