// Paging the output that does not fit.
//
// Most of what the session prints is a line or two, and belongs in the
// scrollback where the rest of the transcript is. Some of it is not: `:list`
// on a finished program, a `:stats` profile of a pipeline with a dozen stages,
// `:help`. Printing those into the scrollback pushes the transcript off the
// screen to show something the user is about to stop looking at.
//
// So output taller than the window opens a pager instead — full screen, arrow
// keys, q to close — and the scrollback is left as it was. The rule is on the
// *height of the output*, not on which command produced it, so a value that
// happens to be a hundred-row grid pages too.
package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// pager is the full-screen reader for one block of output.
type pager struct {
	vp    viewport.Model
	title string
	keys  pagerKeyMap

	// sortable is set for content that can be re-ordered in place — a `:stats`
	// profile, which is worth reading both in program order and worst-first.
	// It renders the same block sorted the other way.
	sortable func(bySelf bool) string
	bySelf   bool
}

type pagerKeyMap struct {
	Close key.Binding
	Sort  key.Binding
	Top   key.Binding
	End   key.Binding
}

func defaultPagerKeys() pagerKeyMap {
	return pagerKeyMap{
		Close: key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "close")),
		Sort:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Top:   key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		End:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "end")),
	}
}

// newPager builds a pager over content, sized to the window.
func newPager(title, content string, width, height int) *pager {
	vp := viewport.New()
	p := &pager{vp: vp, title: title, keys: defaultPagerKeys()}
	p.resize(width, height)
	p.vp.SetContent(content)
	return p
}

// resize fits the pager to the window, leaving room for its own two lines of
// chrome.
func (p *pager) resize(width, height int) {
	if width <= 0 {
		width = 80
	}
	if height <= 3 {
		height = 24
	}
	p.vp.SetWidth(width)
	p.vp.SetHeight(height - 2)
}

// update handles one message; it reports false once the pager has been closed.
func (p *pager) update(msg tea.Msg) (open bool, cmd tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case matches(key, p.keys.Close):
			return false, nil
		case matches(key, p.keys.Top):
			p.vp.SetYOffset(0)
			return true, nil
		case matches(key, p.keys.End):
			p.vp.SetYOffset(p.vp.TotalLineCount())
			return true, nil
		case p.sortable != nil && matches(key, p.keys.Sort):
			p.bySelf = !p.bySelf
			p.vp.SetContent(p.sortable(p.bySelf))
			p.vp.SetYOffset(0)
			return true, nil
		}
	}
	p.vp, cmd = p.vp.Update(msg)
	return true, cmd
}

// view draws the pager: a title, the content, and the keys that work here.
func (p *pager) view() string {
	var b strings.Builder
	b.WriteString(styTitle.Render(p.title))
	b.WriteString("\n")
	b.WriteString(p.vp.View())
	b.WriteString("\n")

	hints := []string{"↑/↓ scroll", "g/G ends", "q close"}
	if p.sortable != nil {
		order := "program order"
		if p.bySelf {
			order = "slowest first"
		}
		hints = append([]string{"s sort (" + order + ")"}, hints...)
	}
	if !p.vp.AtBottom() {
		hints = append(hints, fmt.Sprintf("%d%%", int(p.vp.ScrollPercent()*100)))
	}
	b.WriteString(styDim.Render(strings.Join(hints, " · ")))
	return b.String()
}

// matches is key.Matches for a single binding, kept short because the overlay
// key handling is nothing but these.
func matches(msg tea.KeyPressMsg, b key.Binding) bool { return key.Matches(msg, b) }

// tooTallToPrint reports whether a block of output should open a pager rather
// than be printed into the scrollback. Height 0 (a terminal that never
// reported its size) prints, since a pager cannot be sized either.
func tooTallToPrint(content string, height int) bool {
	if height <= 0 || content == "" {
		return false
	}
	// The prompt line, the line the output is announced on, and a little room
	// to see what came before it.
	return countLines(content) > height-3
}

func countLines(s string) int {
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

// pagerTitle names a pager after the command that filled it, falling back to
// the first line of the content.
func pagerTitle(line, content string) string {
	name, _ := splitCommand(strings.TrimSpace(line))
	if strings.HasPrefix(name, ":") {
		return "domain " + name
	}
	first, _, _ := strings.Cut(ansi.Strip(content), "\n")
	return truncateVis(first, 60)
}
