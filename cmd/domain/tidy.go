// The `domain tidy` subcommand: the rewrites the editor applies on save, for
// a file — or a directory's worth of them — outside the editor.
//
// It takes fmt's flags and fmt's conventions, because it is the same kind of
// tool with a longer reach: by default the tidied program goes to stdout and
// nothing is touched; -w rewrites in place; --check is the CI form. What each
// file's rewrites were, and what was noticed but left alone, goes to stderr,
// so stdout stays a program.
//
// One difference from fmt is deliberate. fmt reports a file it cannot parse
// and moves on; tidy needs more than a parse — every rewrite is checked
// against the resolver — so a program that does not *check* is an error here,
// and is left exactly as it was.
package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"domain/tidy"
)

// tidyOptions are the parsed `domain tidy` flags.
type tidyOptions struct {
	Write bool // -w: rewrite each file in place
	List  bool // -l: print the name of each file that would change
	Check bool // --check: like -l, but exit 1 when any file would
	Quiet bool // -q: do not report what was rewritten or noticed
}

func parseTidyArgs(args []string) (paths []string, opts tidyOptions, err error) {
	for _, a := range args {
		switch a {
		case "-w", "--write":
			opts.Write = true
		case "-l", "--list":
			opts.List = true
		case "--check":
			opts.Check = true
		case "-q", "--quiet":
			opts.Quiet = true
		case "-":
			paths = append(paths, a)
		default:
			if strings.HasPrefix(a, "-") {
				return nil, opts, fmt.Errorf("unknown flag %q for tidy", a)
			}
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		return nil, opts, fmt.Errorf("tidy needs at least one file (or - for stdin)")
	}
	if opts.Write && slices.Contains(paths, "-") {
		return nil, opts, fmt.Errorf("cannot use -w with stdin")
	}
	return paths, opts, nil
}

// Tidy runs the rewrites over each path and returns the process exit code.
func Tidy(paths []string, opts tidyOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	code := 0
	for _, path := range paths {
		src, err := readSource(path, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			code = 1
			continue
		}
		// Imports are looked for beside the file; stdin has no "beside".
		file := path
		if path == "-" {
			file = ""
		}
		if err := tidy.Check(file, src); err != nil {
			fmt.Fprintf(stderr, "domain: %s: %v\n", name(path), err)
			code = 1
			continue
		}
		res := tidy.Apply(file, src, tidy.Options{Format: true})

		if !opts.Quiet {
			for _, a := range res.Applied {
				fmt.Fprintf(stderr, "%s: %s\n", name(path), a)
			}
			for _, n := range res.Notes {
				fmt.Fprintf(stderr, "%s: note: %s\n", name(path), n)
			}
		}

		changed := res.Changed() && res.Src != src
		switch {
		case opts.Check:
			if changed {
				fmt.Fprintln(stdout, name(path))
				code = 1
			}
		case opts.List:
			if changed {
				fmt.Fprintln(stdout, name(path))
			}
		case opts.Write:
			if !changed {
				continue // never touch mtime for a file that is already tidy
			}
			if err := os.WriteFile(path, []byte(res.Src), 0o644); err != nil {
				fmt.Fprintf(stderr, "domain: %v\n", err)
				code = 1
			}
		default:
			fmt.Fprint(stdout, res.Src)
		}
	}
	return code
}
