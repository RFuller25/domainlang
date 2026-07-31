// Composing an indented block as a block.
//
// Continuation mode types a body one line at a time, and a line that has been
// submitted is gone: a typo on the `Using:` line of a four-line loop body means
// finishing the block, watching it fail, and typing all four again. That is
// fine for the two-line bodies most statements have, and wrong for the ones
// that made you think.
//
// Ctrl+O opens the block being built in a textarea instead — real up and down
// movement, real editing, the statement head shown above it for context — and
// submits the whole thing at once. It is the same body either way: the buffer
// is seeded from the lines already typed, and what comes back replaces them.
//
// This is the small sibling of `:edit`, which opens the *program* in $EDITOR.
// The difference is what is being fixed: a block you are in the middle of
// writing, versus a statement six lines up.
package main

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// blockEditor is the Ctrl+O overlay: the statement being continued, and a
// textarea holding its body.
type blockEditor struct {
	head string // the statement the body belongs to, for context
	ta   textarea.Model
	keys blockKeyMap
}

type blockKeyMap struct {
	Accept key.Binding
	Cancel key.Binding
}

func defaultBlockKeys() blockKeyMap {
	return blockKeyMap{
		// Enter inserts a newline here — that is the point of the mode — so
		// submitting needs a key of its own.
		Accept: key.NewBinding(key.WithKeys("ctrl+d", "ctrl+s"), key.WithHelp("ctrl+d", "submit the block")),
		Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "back to the prompt")),
	}
}

// newBlockEditor opens an editor over a pending statement: head is its first
// line, body the indented lines typed so far, and current whatever is on the
// prompt right now.
func newBlockEditor(head string, body []string, current string, width, height int) *blockEditor {
	ta := textarea.New()
	// The terminal's own cursor, as at the prompt: it blinks the way the
	// user's terminal blinks rather than on a timer of ours.
	ta.SetVirtualCursor(false)
	ta.ShowLineNumbers = false
	ta.Prompt = "  "
	ta.SetWidth(max(width-4, 20))
	ta.SetHeight(max(min(height-6, 12), 3))
	ta.CharLimit = 0

	lines := slices.Clone(body)
	if strings.TrimSpace(current) != "" {
		lines = append(lines, current)
	}
	ta.SetValue(strings.Join(lines, "\n"))
	ta.MoveToEnd()
	ta.Focus()
	return &blockEditor{head: head, ta: ta, keys: defaultBlockKeys()}
}

// update handles one message. It reports whether the editor is still open and,
// when it is not, the body lines to submit (nil when cancelled).
func (b *blockEditor) update(msg tea.Msg) (open bool, body []string, cmd tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(k, b.keys.Cancel):
			return false, nil, nil
		case key.Matches(k, b.keys.Accept):
			return false, b.lines(), nil
		}
	}
	b.ta, cmd = b.ta.Update(msg)
	return true, nil, cmd
}

// lines are the body as statement lines: indented the way the REPL indents,
// with the blank lines a text editor collects along the way dropped.
func (b *blockEditor) lines() []string {
	var out []string
	for _, line := range strings.Split(b.ta.Value(), "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			line = "    " + line // an unindented line still belongs to the block
		}
		out = append(out, strings.ReplaceAll(line, "\t", "    "))
	}
	return out
}

func (b *blockEditor) view(width int) string {
	var out strings.Builder
	out.WriteString(styTitle.Render("block") + styDim.Render("  editing the body of:") + "\n")
	out.WriteString(promptTop + highlightSource(b.head, true) + "\n\n")
	out.WriteString(b.ta.View() + "\n\n")
	out.WriteString(styDim.Render(truncateVis(
		"ctrl+d submits the block · enter is a newline here · esc returns to the prompt", max(width, 20))))
	return out.String()
}

// blockHeaderLines is how far down the view the textarea starts: the title,
// the statement it belongs to, and the blank line between them.
const blockHeaderLines = 3

// cursor is the terminal cursor's place inside the block editor.
func (b *blockEditor) cursor() *tea.Cursor {
	c := b.ta.Cursor()
	if c == nil {
		return nil
	}
	c.Y += blockHeaderLines
	c.Shape = tea.CursorBar
	return c
}

func (b *blockEditor) resize(width, height int) {
	b.ta.SetWidth(max(width-4, 20))
	b.ta.SetHeight(max(min(height-6, 12), 3))
}
