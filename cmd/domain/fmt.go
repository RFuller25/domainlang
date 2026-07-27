// The `domain fmt` subcommand: canonical whitespace for Domain source.
//
// Conventions follow gofmt, because that is what anyone reaching for a
// formatter expects: with no flags the formatted result goes to stdout, `-w`
// rewrites files in place, `-l` lists the files that would change, and
// `--check` is `-l` plus a nonzero exit so CI can fail on unformatted source.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"domain/format"
)

// fmtOptions are the parsed `domain fmt` flags.
type fmtOptions struct {
	Write bool // -w: rewrite each file in place
	List  bool // -l: print the name of each file that is not formatted
	Check bool // --check: like -l, but exit 1 when any file differs
}

// parseFmtArgs parses `domain fmt`'s arguments: any number of paths (or "-"
// for stdin) plus the flags above.
func parseFmtArgs(args []string) (paths []string, opts fmtOptions, err error) {
	for _, a := range args {
		switch a {
		case "-w", "--write":
			opts.Write = true
		case "-l", "--list":
			opts.List = true
		case "--check":
			opts.Check = true
		case "-":
			paths = append(paths, a)
		default:
			if strings.HasPrefix(a, "-") {
				return nil, opts, fmt.Errorf("unknown flag %q for fmt", a)
			}
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		return nil, opts, fmt.Errorf("fmt needs at least one file (or - for stdin)")
	}
	if opts.Write {
		for _, p := range paths {
			if p == "-" {
				return nil, opts, fmt.Errorf("cannot use -w with stdin")
			}
		}
	}
	return paths, opts, nil
}

// Fmt formats each path, returning the process exit code: 0 when everything is
// already formatted (or was written successfully), 1 when a file could not be
// read or does not parse, or when --check found an unformatted file.
func Fmt(paths []string, opts fmtOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	code := 0
	for _, path := range paths {
		src, err := readSource(path, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			code = 1
			continue
		}

		formatted, err := format.Format(src)
		if err != nil {
			// Format returns the source untouched on failure, so nothing is
			// lost; report where it gave up and move on to the next file.
			fmt.Fprintf(stderr, "domain: %s: %v\n", name(path), err)
			code = 1
			continue
		}

		changed := formatted != src
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
				continue // never touch mtime for a file that is already clean
			}
			if err := os.WriteFile(path, []byte(formatted), 0o644); err != nil {
				fmt.Fprintf(stderr, "domain: %v\n", err)
				code = 1
			}
		default:
			fmt.Fprint(stdout, formatted)
		}
	}
	return code
}

// readSource reads a path, or stdin when the path is "-".
func readSource(path string, stdin io.Reader) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// name renders a path for messages, spelling stdin rather than "-".
func name(path string) string {
	if path == "-" {
		return "<stdin>"
	}
	return path
}
