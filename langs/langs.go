// Package langs is the one place Domain records how to run a program written
// in another language.
//
// It serves three callers that used to each keep their own copy of the table:
//
//   - the lexer, through ast.ForeignLanguages, which has to recognize
//     `Domain Expansion: Python` as a block opener *before* anything with an
//     opinion about semantics has seen the line — from the next line onward the
//     source is not Domain and cannot be tokenized as Domain;
//   - the interpreter (prims/foreign.go), which runs a block as a subprocess;
//   - the compiler (codegen/foreigngen.go), which emits Go that does the same.
//
// Keeping one table is not tidiness. The three were held in sync by hand, and
// a language present in two of them and missing from the third is a program
// that lexes, interprets, and then fails to compile — the worst place to find
// out.
//
// The set is closed on purpose. A name here changes how the lexer reads the
// lines beneath it, so it has to be small, known, and unable to grow by
// accident.
package langs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Spec is everything needed to run one language's program.
type Spec struct {
	// Name is the canonical spelling, as a foreign block writes it.
	Name string

	// File is what the program is written to. Extensions matter: a Go file
	// must end in .go to compile, and the others are named for the benefit of
	// whoever is reading a stack trace.
	File string

	// Env names an environment variable that overrides the runtime, and may
	// hold a command with arguments ("uv run python"). This is what makes the
	// feature usable on a machine where the binary is not on PATH under its
	// usual name, and what lets the tests run against whatever is installed.
	Env string

	// Candidates are the PATH lookups tried in order when Env is unset.
	Candidates []string

	// Args sit between the binary and the program path — `run` in
	// `weave run program.weave`. Go puts its whole invocation here (`run .`)
	// and appends no program, because it runs the throwaway module rather than
	// a file.
	Args []string

	// AppendProg appends the program's path after Args.
	AppendProg bool

	// Extra are sidecar files the runtime needs beside the program.
	Extra map[string]string

	// Exts are the filename extensions that identify this language, so
	// `domain expansion: battle prog.domain rival.py` can infer --lang.
	Exts []string

	// Home is where to get the runtime, for the message a missing one earns.
	// Empty for the languages a reader can be assumed to already know how to
	// install.
	Home string
}

// specs is the table. Order is the order `domain help` and the docs list them.
var specs = []Spec{
	{
		Name: "Python", File: "program.py", Env: "DOMAIN_PYTHON",
		Candidates: []string{"python3", "python"},
		AppendProg: true,
		Exts:       []string{".py"},
	},
	{
		Name: "Go", File: "main.go", Env: "DOMAIN_GO",
		Candidates: []string{"go"},
		// `go run .` inside the throwaway module: the block is a whole
		// `package main`, exactly as a hand-written Go program would be, and
		// the build cache keeps the repeat cost down.
		Args:  []string{"run", "."},
		Extra: map[string]string{"go.mod": "module domainforeign\n\ngo 1.22\n"},
		Exts:  []string{".go"},
	},
	{
		Name: "rask", File: "program.rask", Env: "DOMAIN_RASK",
		Candidates: []string{"rask"},
		AppendProg: true,
		Exts:       []string{".rask"},
	},
	{
		Name: "cRust", File: "program.crust", Env: "DOMAIN_CRUST",
		Candidates: []string{"crust"},
		AppendProg: true,
		Exts:       []string{".crust"},
		Home:       "github.com/sintfoap/crust",
	},
	{
		// Weave's own CLI documents `weave run file.weave` as "compile and
		// run, feeding stdin to Source", and a program's final bare expression
		// is what it prints. That is exactly the wire format's contract — a
		// value in on stdin, a value out on stdout — with no adapter needed.
		Name: "Weave", File: "program.weave", Env: "DOMAIN_WEAVE",
		Candidates: []string{"weave"},
		Args:       []string{"run"},
		AppendProg: true,
		Exts:       []string{".weave", ".wv"},
		Home:       "github.com/malleum/weavelang",
	},
}

// All returns the table.
func All() []Spec { return append([]Spec(nil), specs...) }

// Names is every canonical language name, in table order.
func Names() []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = s.Name
	}
	return out
}

// Lookup finds a language by name, case-insensitively, returning its canonical
// spelling in Spec.Name.
//
// Matching ignores case because Domain's operation phrases are matched
// case-insensitively everywhere else, and a lexical rule that cared about case
// would be the only one that did.
func Lookup(name string) (Spec, bool) {
	for _, s := range specs {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return Spec{}, false
}

// ByExt identifies a language from a filename, so a command that takes a
// program in another language need not be told which language it is.
func ByExt(path string) (Spec, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return Spec{}, false
	}
	for _, s := range specs {
		for _, e := range s.Exts {
			if e == ext {
				return s, true
			}
		}
	}
	return Spec{}, false
}

// Binary resolves the runtime: the environment override when set, else the
// first candidate found on PATH.
func (s Spec) Binary() ([]string, error) {
	if override := strings.TrimSpace(os.Getenv(s.Env)); override != "" {
		return strings.Fields(override), nil
	}
	for _, c := range s.Candidates {
		if path, err := exec.LookPath(c); err == nil {
			return []string{path}, nil
		}
	}
	return nil, &NotInstalledError{Spec: s}
}

// Command is the full argv for running a program written to dir, plus the
// sidecar files that must exist beside it.
func (s Spec) Command(dir string) ([]string, map[string]string, error) {
	bin, err := s.Binary()
	if err != nil {
		return nil, nil, err
	}
	argv := append(append([]string(nil), bin...), s.Args...)
	if s.AppendProg {
		argv = append(argv, filepath.Join(dir, s.File))
	}
	extra := map[string]string{}
	for k, v := range s.Extra {
		extra[k] = v
	}
	return argv, extra, nil
}

// CommandFor is Command for a program that already exists at path, rather than
// one written into a throwaway directory. It is what battle uses: the file the
// user named is run where it lies.
func (s Spec) CommandFor(path string) ([]string, error) {
	bin, err := s.Binary()
	if err != nil {
		return nil, err
	}
	argv := append(append([]string(nil), bin...), s.Args...)
	if s.AppendProg {
		argv = append(argv, path)
	}
	return argv, nil
}

// NotInstalledError is a missing runtime. It is a distinct type because a
// missing toolchain is not a failure of the user's program and should not be
// reported as one — the caller says where to get it and stops.
type NotInstalledError struct{ Spec Spec }

func (e *NotInstalledError) Error() string {
	msg := fmt.Sprintf("%s not found on PATH (tried %s); set %s to name it differently",
		e.Spec.Name, strings.Join(e.Spec.Candidates, " or "), e.Spec.Env)
	if e.Spec.Home != "" {
		msg += fmt.Sprintf(", or install it from %s", e.Spec.Home)
	}
	return msg
}
