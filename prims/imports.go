package prims

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"domain/ast"
	"domain/lexer"
	"domain/parser"
	"domain/token"
)

// `Innate Domain: <library>` — Shikigami libraries.
//
// A library is a file of Shikigami definitions and nothing else: no pipeline
// statements, because a library is not a program. Its definitions are loaded
// before the importing program's own, so they compose exactly like the prelude
// does — and because Shikigami are inlined at their call sites, an imported
// operation gets every optimizer rewrite for free, with no work here.
//
// Imports resolve at *build* time. A compiled binary contains the inlined
// bodies and never looks for the library again.

// domainExt is the extension an import target gets. Targets are written
// without it, matching how a `Cursed Energy:` path is written bare.
const domainExt = ".domain"

// maxImportDepth bounds the transitive import graph. Cycles are detected
// exactly (by absolute path), so this only catches pathologically deep chains.
const maxImportDepth = 32

// DefSite records where a Shikigami definition came from, so tooling can point
// at it — the language server's go-to-definition follows an imported name into
// its library file.
type DefSite struct {
	Origin string // "local", "prelude", or "import"
	Path   string // absolute file path, when Origin == "import"
}

// ResolveOptions carries the file context an `Innate Domain` import needs.
// The zero value has no context, which is what plain Resolve passes: a program
// with imports then fails with a positioned error rather than silently ignoring
// them.
type ResolveOptions struct {
	// BaseDir is the importing file's directory, searched first.
	BaseDir string
	// Search is the fallback search path, in order — normally $DOMAIN_PATH
	// followed by the user library directory. See SearchPath.
	Search []string
	// ReadFile reads a library file. nil means os.ReadFile; the language server
	// supplies its own so imports resolve against unsaved editor buffers.
	ReadFile func(path string) ([]byte, error)
	// Sites, when non-nil, is filled in with each Shikigami's definition site.
	// An out-parameter rather than a return value, so Resolve's signature — and
	// its five call sites — stay as they are.
	Sites map[string]DefSite
}

// FileOptions returns the resolve options for a program stored at path: its own
// directory is searched first, then the default search path. This is what every
// caller with a file in hand wants.
func FileOptions(path string) ResolveOptions {
	dir := filepath.Dir(path)
	if path == "" {
		dir = ""
	}
	return ResolveOptions{BaseDir: dir, Search: SearchPath()}
}

// SearchPath returns the default library search path: every colon-separated
// entry of $DOMAIN_PATH, then the user library directory
// (~/.config/domain/lib). The importing file's own directory is searched before
// either and is not part of this list.
func SearchPath() []string {
	var out []string
	if env := os.Getenv("DOMAIN_PATH"); env != "" {
		for _, dir := range filepath.SplitList(env) {
			if dir != "" {
				out = append(out, dir)
			}
		}
	}
	if home, err := os.UserConfigDir(); err == nil {
		out = append(out, filepath.Join(home, "domain", "lib"))
	}
	return out
}

// importLoader loads a program's transitive imports into the resolver.
type importLoader struct {
	opts   ResolveOptions
	loaded map[string]bool // absolute paths already loaded (diamonds load once)
	stack  []string        // absolute paths on the current chain, for cycles
	defs   []*loadedDef    // definitions in load order: dependencies first
}

// loadedDef is one imported definition plus where it came from. impPos is the
// position of the `Innate Domain` statement in the *user's* file that pulled it
// in, so an error about the definition can be reported at a position the user's
// source actually has — the same discipline wrapShikigamiErr follows.
type loadedDef struct {
	def     *ast.ShikigamiDef
	absPath string
	display string // as the user would recognize it: "aoc.domain"
	impPos  token.Position
}

func (l *importLoader) read(path string) ([]byte, error) {
	if l.opts.ReadFile != nil {
		return l.opts.ReadFile(path)
	}
	return os.ReadFile(path)
}

// resolvePath finds the file an import target names, searching the importing
// file's directory first and then the search path. An absolute or explicitly
// relative target (./lib) is taken as given.
func (l *importLoader) resolvePath(baseDir, target string, pos token.Position) (string, error) {
	rel := target + domainExt
	var tried []string

	candidates := []string{}
	if filepath.IsAbs(rel) || strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") {
		candidates = append(candidates, rel)
	} else {
		if baseDir != "" {
			candidates = append(candidates, filepath.Join(baseDir, rel))
		}
		for _, dir := range l.opts.Search {
			candidates = append(candidates, filepath.Join(dir, rel))
		}
	}

	for _, c := range candidates {
		if _, err := l.read(c); err == nil {
			abs, err := filepath.Abs(c)
			if err != nil {
				return c, nil // unlikely; a relative path still works
			}
			return abs, nil
		}
		tried = append(tried, c)
	}

	if len(tried) == 0 {
		return "", &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"cannot find the library %q: imports need a file context (run a program file rather than piped source)", target)}
	}
	return "", &ResolveError{Pos: pos, Msg: fmt.Sprintf(
		"cannot find the library %q; looked in:\n    %s", target, strings.Join(tried, "\n    "))}
}

// load reads one import and, depth-first, everything it imports. Dependencies
// are appended to defs before their dependents, so a directly-imported library
// shadows what it itself imported. rootPos is the position in the user's file
// that every definition found down this chain is attributed to.
func (l *importLoader) load(baseDir string, imp *ast.Import, rootPos token.Position) error {
	if len(l.stack) >= maxImportDepth {
		return &ResolveError{Pos: imp.Pos, Msg: fmt.Sprintf(
			"import chain deeper than %d files", maxImportDepth)}
	}

	abs, err := l.resolvePath(baseDir, imp.Target, imp.Pos)
	if err != nil {
		return err
	}

	if i := slices.Index(l.stack, abs); i >= 0 {
		chain := append(slices.Clone(l.stack[i:]), abs)
		return &ResolveError{Pos: imp.Pos, Msg: fmt.Sprintf(
			"import cycle: %s", strings.Join(displayChain(chain), " → "))}
	}
	if l.loaded[abs] {
		return nil // a diamond: already loaded, and loading twice would shadow
	}

	src, err := l.read(abs)
	if err != nil {
		return &ResolveError{Pos: imp.Pos, Msg: fmt.Sprintf("cannot read %s: %v", filepath.Base(abs), err)}
	}
	toks, lexErr := lexer.Lex(string(src))
	if lexErr != nil {
		return &ResolveError{Pos: imp.Pos, Msg: fmt.Sprintf(
			"in library %s: %v", filepath.Base(abs), lexErr)}
	}
	libProg, parseErr := parser.Parse(string(src), toks)
	if parseErr != nil {
		return &ResolveError{Pos: imp.Pos, Msg: fmt.Sprintf(
			"in library %s: %v", filepath.Base(abs), parseErr)}
	}

	// A library is a bag of Shikigami, not a program.
	if len(libProg.Statements) > 0 {
		return &ResolveError{Pos: imp.Pos, Msg: fmt.Sprintf(
			"library %s must contain only Shikigami definitions (found a pipeline statement at %s)",
			filepath.Base(abs), libProg.Statements[0].Pos)}
	}

	l.loaded[abs] = true
	l.stack = append(l.stack, abs)
	defer func() { l.stack = l.stack[:len(l.stack)-1] }()

	// Depth-first: what this library imports is loaded (and so shadowed) first.
	libDir := filepath.Dir(abs)
	for _, nested := range libProg.Imports {
		if err := l.load(libDir, nested, rootPos); err != nil {
			return err
		}
	}
	for _, d := range libProg.Shikigamis {
		l.defs = append(l.defs, &loadedDef{
			def: d, absPath: abs, display: filepath.Base(abs), impPos: rootPos,
		})
	}
	return nil
}

// displayChain renders an import cycle using base names, which is what the user
// wrote, rather than long absolute paths.
func displayChain(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

// loadImports resolves prog's imports and registers their definitions on the
// resolver, beneath the program's own definitions.
func (r *resolver) loadImports(prog *ast.Program, opts ResolveOptions) error {
	if len(prog.Imports) == 0 {
		return nil
	}
	l := &importLoader{opts: opts, loaded: map[string]bool{}}
	for _, imp := range prog.Imports {
		if err := l.load(opts.BaseDir, imp, imp.Pos); err != nil {
			return err
		}
	}
	for _, ld := range l.defs {
		// The reserved-name rule applies to imported definitions too, but the
		// offending position lives in the library, which the user's source
		// cannot render — so report it at the import that pulled it in and name
		// the library and the inner position in the message.
		if err := checkShikigamiName(ld.def); err != nil {
			var re *ResolveError
			if errors.As(err, &re) {
				return &ResolveError{Pos: ld.impPos, Msg: fmt.Sprintf(
					"in library %s (%s): %s", ld.display, re.Pos, re.Msg)}
			}
			return err
		}
		r.shikigamis[ld.def.Name] = ld.def
		r.origins[ld.def.Name] = DefSite{Origin: "import", Path: ld.absPath}
		r.displays[ld.def.Name] = ld.display
	}
	return nil
}
