// The editor's key bindings, and the reference screen that lists them.
//
// Bindings are `key.Binding` like the picker's and the REPL's, so every
// spelling of a key lives in one place and the help screen is generated from
// the same values the dispatcher matches against — a binding that is added
// without being documented is not possible.
//
// The choices follow a full-screen editor's conventions rather than the REPL's,
// because that is the muscle memory people bring to one. `micro` is the closest
// reference and the scheme is largely its: ctrl+q leaves, ctrl+s saves, ctrl+o
// opens, ctrl+c/x/v are the clipboard, ctrl+z and ctrl+y undo and redo, ctrl+f
// finds, ctrl+l jumps to a line. It agrees with the REPL where it matters —
// ctrl+g is the key list in both.
//
// Three deliberate divergences from the REPL, each because the REPL's meaning
// belongs to a prompt rather than to a screen. ctrl+z is undo here, not
// suspend: suspending a full-screen editor is a rarer thing to want than
// withdrawing an edit, and every editor binds it this way. ctrl+r runs the
// program rather than searching history, since there is no history to search.
// ctrl+l jumps to a line rather than clearing the screen, which a full-screen
// editor repaints anyway.
//
// ctrl+c copies, and does *not* quit. It interrupts a run in progress — checked
// before anything else, which is the whole reason the run is on a command — and
// the rest of the time it is the clipboard key everyone expects. Leaving is
// ctrl+q alone, so a buffer cannot be abandoned by the reflex that copies.
//
// The alt cluster is the language: alt+d for the catalog, alt+f to format,
// alt+i to be offered an opening, alt+k to inspect. Grouping them keeps the
// ctrl keys for the editor and the alt keys for what Domain knows.
package main

import (
	"charm.land/bubbles/v2/key"
)

type devKeyMap struct {
	Quit   key.Binding
	Save   key.Binding
	SaveAs key.Binding
	Open   key.Binding
	Help   key.Binding

	Undo      key.Binding
	Redo      key.Binding
	Find      key.Binding
	Goto      key.Binding
	SelectAll key.Binding
	Copy      key.Binding
	Cut       key.Binding
	Paste     key.Binding

	Complete   key.Binding
	Inspect    key.Binding
	Definition key.Binding
	Format     key.Binding
	Docs       key.Binding

	Run       key.Binding
	Visualize key.Binding
	Monitor   key.Binding
	Input     key.Binding
	Suggest   key.Binding
	StageNext key.Binding
	StagePrev key.Binding
	Fix       key.Binding
	FixAll    key.Binding
	Explain   key.Binding
	Fold      key.Binding
	UnfoldAll key.Binding
	JumpBack  key.Binding
}

func defaultDevKeys() devKeyMap {
	return devKeyMap{
		Quit:   key.NewBinding(key.WithKeys("ctrl+q")),
		Save:   key.NewBinding(key.WithKeys("ctrl+s")),
		SaveAs: key.NewBinding(key.WithKeys("ctrl+shift+s", "alt+s")),
		Open:   key.NewBinding(key.WithKeys("ctrl+o")),
		Help:   key.NewBinding(key.WithKeys("ctrl+g", "f1")),

		Undo: key.NewBinding(key.WithKeys("ctrl+z")),
		Redo: key.NewBinding(key.WithKeys("ctrl+y", "ctrl+shift+z")),
		Find: key.NewBinding(key.WithKeys("ctrl+f")),
		Goto: key.NewBinding(key.WithKeys("ctrl+l")),
		// ctrl+a is select-all rather than start-of-line: this is a full-screen
		// editor, not a readline prompt, and Home is already there.
		SelectAll: key.NewBinding(key.WithKeys("ctrl+a")),
		Copy:      key.NewBinding(key.WithKeys("ctrl+c")),
		Cut:       key.NewBinding(key.WithKeys("ctrl+x")),
		Paste:     key.NewBinding(key.WithKeys("ctrl+v")),

		// Tab completes where there is something to complete and indents where
		// there is not, which is how one key serves both without a mode.
		Complete:   key.NewBinding(key.WithKeys("tab")),
		Inspect:    key.NewBinding(key.WithKeys("alt+k")),
		Definition: key.NewBinding(key.WithKeys("ctrl+]")),
		Format:     key.NewBinding(key.WithKeys("alt+f")),
		Docs:       key.NewBinding(key.WithKeys("alt+d")),

		Run:       key.NewBinding(key.WithKeys("ctrl+r")),
		Visualize: key.NewBinding(key.WithKeys("ctrl+t")),
		// The monitor opens itself on ctrl+r and closes on any key; alt+m is
		// for the run whose screen was dismissed by one that was meant for the
		// program.
		Monitor: key.NewBinding(key.WithKeys("alt+m")),
		Input:   key.NewBinding(key.WithKeys("ctrl+e")),
		Suggest: key.NewBinding(key.WithKeys("alt+i")),
		// Walking the recorded stages: the stepper's gesture, against the
		// program you are editing rather than a tree beside it.
		StageNext: key.NewBinding(key.WithKeys("alt+down")),
		StagePrev: key.NewBinding(key.WithKeys("alt+up")),
		Fix:       key.NewBinding(key.WithKeys("alt+a")),
		FixAll:    key.NewBinding(key.WithKeys("alt+shift+a")),
		Explain:   key.NewBinding(key.WithKeys("alt+e")),
		Fold:      key.NewBinding(key.WithKeys("alt+z")),
		UnfoldAll: key.NewBinding(key.WithKeys("alt+shift+z")),
		JumpBack:  key.NewBinding(key.WithKeys("ctrl+[")),
	}
}

// devHelpBody is the key list, as lines. It is a function rather than a
// constant so the styles it uses are the ones installed at the time it is
// drawn, after the terminal has reported its background.
func devHelpBody() []string {
	var out []string
	section := func(title string) {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, styHeading.Render(title))
	}
	row := func(keys, what string) {
		out = append(out, "  "+styKey.Render(pad(keys, 16))+styDim.Render(what))
	}

	section("Files")
	row("ctrl+o", "open a program")
	row("ctrl+s", "save")
	row("alt+s", "save as")
	row("ctrl+q", "leave (again to discard unsaved changes)")
	row("ctrl+c", "copy — it does not leave, and it stops a run")

	section("What the language knows")
	row("(idle)", "types appear at the end of each line; errors mark the gutter")
	row("tab", "complete a keyword, primitive, label or path")
	row("↑ ↓ enter", "choose a completion; esc dismisses")
	row("alt+k", "inspect what is on this line")
	row("ctrl+]", "go to a Shikigami definition, following imports")
	row("ctrl+[", "come back from one")
	row("alt+d", "browse the primitive catalog")
	row("alt+a", "apply the fix for this line")
	row("alt+A", "apply every confident fix")
	row("alt+f", "format the program")

	section("Running")
	row("ctrl+e", "choose the input file — then it offers an opening")
	row("alt+i", "offer an opening again")
	row("ctrl+r", "run — the monitor takes the screen and watches it")
	row("ctrl+c", "stop a run that will not end (on the monitor)")
	row("alt+m", "reopen the last run's monitor")
	row("ctrl+t", "open the stepper over the last run")
	row("alt+↑ / alt+↓", "walk the recorded stages, watching the value change")
	row("alt+e", "what the optimizer did to the last run")
	row("↑ ↓", "scroll the output; any other key closes it")

	section("Selecting and the clipboard")
	row("shift+motion", "extend the selection")
	row("ctrl+a", "select the whole program")
	row("ctrl+c / ctrl+x", "copy / cut (the line, with nothing selected)")
	row("ctrl+v", "paste")

	section("Undoing")
	row("ctrl+z", "undo — a run of typing is one step")
	row("ctrl+y", "redo")

	section("Finding")
	row("ctrl+f", "find, incremental and wrapping")
	row("↑ ↓", "previous / next match")
	row("enter", "stay on the match; esc goes back")
	row("ctrl+l", "go to a line number")

	section("Structure")
	row("alt+z", "fold or unfold the block here")
	row("alt+Z", "unfold everything")
	row("enter", "after a block opener, indents into its body")

	section("Moving")
	row("← → ↑ ↓", "by character and line")
	row("home / end", "start and end of the line")
	row("pgup / pgdn", "by a screenful")
	row("ctrl+← / ctrl+→", "by word")
	row("ctrl+home / end", "start and end of the program")

	section("Editing")
	row("enter", "split the line, carrying its indentation")
	row("tab", "indent four spaces, or the selection by one level")
	row("shift+tab", "dedent")
	row("delete", "delete forward")
	row("backspace", "delete back, joining lines at column zero")

	section("This screen")
	row("ctrl+g / f1", "open it")
	row("↑ ↓ pgup pgdn", "scroll it")
	row("any other key", "close it")

	return out
}
