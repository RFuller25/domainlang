// `domain expansion: development [file]` — the editor.
//
// Write a Domain program with the language's own knowledge of it on screen,
// pick an input, run it, and step through the run without leaving the editor.
// The other expansion commands analyze a program you already have; this is the
// one you write it in.
//
//	domain expansion: development day7.domain          # open a program
//	domain expansion: development                      # pick one
//	domain expansion: development day7.domain --input day7.txt
//
// A file that does not exist yet is not an error — it is a new program under
// that name, which is what naming one is for. A bare invocation opens the file
// browser rather than an empty buffer: unlike `domain repl`, where the session
// *is* the program being built, this edits a file, and the first question it
// has is which one.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

// devOptions are the parsed `development` arguments.
type devOptions struct {
	Input string // --input FILE: the program's input, preselected
}

// parseDevelopmentArgs reads `[file] [--input FILE]`.
func parseDevelopmentArgs(args []string) (string, devOptions, error) {
	var opts devOptions
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--input" || a == "-i":
			if i+1 >= len(args) {
				return "", opts, fmt.Errorf("%s needs a file", a)
			}
			i++
			opts.Input = args[i]
		case strings.HasPrefix(a, "--input="):
			opts.Input = strings.TrimPrefix(a, "--input=")
		case strings.HasPrefix(a, "-"):
			return "", opts, fmt.Errorf("unknown flag %q (development accepts only --input)", a)
		case path != "":
			return "", opts, fmt.Errorf("unexpected extra argument %q", a)
		default:
			path = a
		}
	}
	return path, opts, nil
}

// Development opens the editor and returns the process exit code.
func Development(path string, opts devOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	// An editor is the one command here with no piped equivalent. `visualize`
	// can print its trace and the REPL can read a script, because both have
	// something to say without a screen; an editor without a terminal has
	// nothing to do at all, so it says so rather than failing obscurely later.
	in, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(in.Fd()) {
		fmt.Fprintln(stderr, "domain: expansion: development needs a terminal (it is an editor)")
		return 2
	}

	var text string
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			text = string(b)
		case os.IsNotExist(err):
			// A new program under that name. Nothing is written until it is
			// saved, so a typo costs a keystroke rather than a file.
		default:
			fmt.Fprintf(stderr, "domain: reading %s: %v\n", path, err)
			return 1
		}
	}

	m := newDevModel(text)
	m.path = path
	m.input = opts.Input
	if path == "" {
		m.picker = newPicker(":load", ".")
	}

	p := tea.NewProgram(m,
		tea.WithInput(in), tea.WithOutput(stdout),
		tea.WithFilter(guardUnsavedDevQuit))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	return 0
}
