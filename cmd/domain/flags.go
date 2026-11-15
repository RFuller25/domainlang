package main

import (
	"fmt"
	"strings"
	"time"

	"domain/runner"
)

// flagSet is the one argument loop the subcommands share. Each command
// registers its flags, then calls parse; what to do with the result (required
// paths, flags that exclude each other, defaults) stays in the command.
//
// Arguments are taken in order and the first error wins. A valued flag takes
// the next argument whatever it looks like, so `--input -x` reads "-x".
type flagSet struct {
	unknown string // format for an unknown flag; %q is the argument
	isFlag  func(string) bool
	pos     func(string) error // a non-flag argument
	bools   map[string]func()
	vals    map[string]valFlag // `--name value`
	eqs     map[string]valFlag // `--name=value`
}

type valFlag struct {
	miss string // format for a missing value; %s is the name as typed
	fn   func(string) error
}

// newFlagSet starts a flag table. unknown may be "" for the common
// `unknown flag %q`.
func newFlagSet(unknown string, pos func(string) error) *flagSet {
	if unknown == "" {
		unknown = "unknown flag %q"
	}
	return &flagSet{unknown, func(a string) bool { return strings.HasPrefix(a, "-") }, pos,
		map[string]func(){}, map[string]valFlag{}, map[string]valFlag{}}
}

// bool registers boolean flags, space-separated, that store v in *p.
func (f *flagSet) bool(names string, p *bool, v bool) {
	for _, n := range strings.Fields(names) {
		f.bools[n] = func() { *p = v }
	}
}

// val registers a valued flag. A name spelled "--x" takes its value from the
// next argument; one spelled "--x=" takes `--x=value`. Say both to accept both.
func (f *flagSet) val(names, miss string, fn func(string) error) {
	for _, n := range strings.Fields(names) {
		if n, ok := strings.CutSuffix(n, "="); ok {
			f.eqs[n] = valFlag{miss, fn}
		} else {
			f.vals[n] = valFlag{miss, fn}
		}
	}
}

// str registers a valued flag that stores its value in *p.
func (f *flagSet) str(names, miss string, p *string) {
	f.val(names, miss, func(s string) error { *p = s; return nil })
}

// onePath is a positional handler that keeps the first argument in *p and
// refuses a second one with the given format.
func onePath(p *string, extra string) func(string) error {
	return func(a string) error {
		if *p != "" {
			return fmt.Errorf(extra, a)
		}
		*p = a
		return nil
	}
}

func (f *flagSet) parse(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if fn, ok := f.bools[a]; ok {
			fn()
			continue
		}
		var err error
		if v, ok := f.vals[a]; ok {
			if i+1 >= len(args) {
				return fmt.Errorf(v.miss, a)
			}
			i++
			err = v.fn(args[i])
		} else if name, value, ok := strings.Cut(a, "="); ok && f.eqs[name].fn != nil {
			err = f.eqs[name].fn(value)
		} else if f.isFlag(a) {
			return fmt.Errorf(f.unknown, a)
		} else {
			err = f.pos(a)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// gridFlags registers the flags bench and oracle share: the cells they run, how
// long each may take, and what the program reads and prints.
func (f *flagSet) gridFlags(text *string, timeout *time.Duration, cells *[]runner.Config, release, json, plain *bool) {
	f.str("--input-text --input-text=", "%s needs a value", text)
	f.val("--timeout --timeout=", "%s needs a value", parseInto(time.ParseDuration, timeout))
	f.val("--cells --cells=", "%s needs a value", parseInto(parseCells, cells))
	f.bool("--release", release, true)
	f.bool("--json", json, true)
	f.bool("--plain", plain, true)
}

// parseInto adapts a parser such as strconv.Atoi into a flag callback. Like
// `x, err = parse(s)`, it stores the result even when parse failed.
func parseInto[T any](parse func(string) (T, error), p *T) func(string) error {
	return func(s string) (err error) {
		*p, err = parse(s)
		return err
	}
}
