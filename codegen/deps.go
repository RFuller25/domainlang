// The modules an emitted program may need.
//
// Everything this backend emitted for the first two years depended only on the
// standard library, and a build was therefore a bare `go.mod` and nothing
// else. A `Game Dev` binary breaks that: it paints a terminal, and the
// terminal half of this repository is Bubble Tea. So an emitted program can
// now name a module, and the moment it can, two things have to be true.
//
// The first is that it may only name a module **this binary was itself built
// against**. A compiler that asked for a version it had never seen would be
// resolving dependencies at its user's build time, against whatever the proxy
// answered that morning — and the generated runtime is transliterated from
// this repository's, so a different version is a different program. deps_data.go
// is therefore generated from the repository's own go.mod and go.sum, and
// deps_test.go fails if the two drift apart.
//
// The second is that a program which names no module is built **exactly as it
// was before**: same bare go.mod, same flags, no go.sum, no network. That is
// not politeness towards the old path, it is the published contract — bench/
// numbers, the reproducibility claim in docs/compiler.md, and every golden
// build in this repository rest on it. moduleFiles decides by looking at the
// generated source's own import block, so the old path is not merely
// preserved, it is unreachable from a program that does not need the new one.
package codegen

import (
	"fmt"
	goparser "go/parser"
	gotok "go/token"
	"strconv"
	"strings"
)

// modulePath is what an emitted program calls itself. It is not published
// anywhere and nothing imports it; it exists because a module needs a name.
const modulePath = "domainprog"

// bareGoMod is the module file a standard-library-only program is built with,
// unchanged since the backend's first commit. Every byte of it is load
// bearing: see TestStdlibBuildInputsAreUnchanged.
const bareGoMod = "module " + modulePath + "\n\ngo 1.22\n"

// moduleFiles is the go.mod and go.sum an emitted program should be built
// with. An empty go.sum means "write no go.sum, and build as this backend
// always has".
func moduleFiles(goSrc string) (gomod, gosum string, err error) {
	ext, err := externalImports(goSrc)
	if err != nil {
		return "", "", err
	}
	if len(ext) == 0 {
		return bareGoMod, "", nil
	}
	if err := checkRequired(ext); err != nil {
		return "", "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo %s\n\nrequire (\n", modulePath, depsGoVersion)
	for _, line := range requireLines() {
		fmt.Fprintf(&b, "\t%s\n", line)
	}
	b.WriteString(")\n")
	return b.String(), depsSum, nil
}

// requireLines is the generated require block, one "path version" per line.
func requireLines() []string {
	var out []string
	for _, line := range strings.Split(depsRequire, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// requiredModules is the module path of each requirement, longest first, so
// that a package path is attributed to the most specific module that covers
// it — charm.land/bubbletea/v2 before charm.land/bubbletea, were both there.
func requiredModules() []string {
	lines := requireLines()
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		path, _, _ := strings.Cut(line, " ")
		out = append(out, path)
	}
	// Sorted longest-first rather than by name: the check below is a prefix
	// test, and a shorter module path is a prefix of a longer one.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && len(out[j]) > len(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// checkRequired refuses an import no requirement covers.
//
// It can only fire if this package grew an emitter that imports something the
// repository does not itself depend on, which is a mistake in the compiler
// rather than in the program being compiled — so it says so in those terms.
// The alternative is a `go build` that reaches for the network and either
// resolves a version nobody chose or fails with a proxy error.
func checkRequired(imports []string) error {
	mods := requiredModules()
	for _, imp := range imports {
		covered := false
		for _, m := range mods {
			if imp == m || strings.HasPrefix(imp, m+"/") {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("internal: the generated program imports %q, "+
				"which no module this compiler was built against provides; "+
				"add it to the repository's go.mod and regenerate codegen/deps_data.go "+
				"with `go test ./codegen -update`", imp)
		}
	}
	return nil
}

// externalImports is the generated program's non-standard-library imports.
//
// Parsed rather than pattern-matched. The import block is the one place the
// answer actually lives, and a textual scan would have to keep up with
// gofmt's grouping, aliases and blank imports — for a question `go/parser`
// answers exactly, in imports-only mode, without reading the rest of the file.
func externalImports(goSrc string) ([]string, error) {
	fset := gotok.NewFileSet()
	f, err := goparser.ParseFile(fset, "main.go", goSrc, goparser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("internal: generated Go does not parse: %v", err)
	}
	var out []string
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || stdlibPath(path) {
			continue
		}
		out = append(out, path)
	}
	return out, nil
}

// stdlibPath reports whether an import path is a standard-library one. The
// test is the toolchain's own: a first path element containing a dot is a
// domain name, and the standard library has none.
func stdlibPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// WithModuleHeader is the generated source as `--emit-go` should write it:
// unchanged for a program that depends only on the standard library, and
// prefixed with the module file for one that does not.
//
// It exists because `--emit-go` hands somebody a file, and a file that will
// not build is a worse answer than no file. `domain build` writes the module
// into a throwaway directory the caller never sees, so without this the one
// thing a reader would need next — which versions this source was generated
// against — is the one thing the source does not say.
//
// The header goes after the generated-by line rather than above it, so that
// line stays first and the tools that look for it still find it.
func WithModuleHeader(goSrc string) (string, error) {
	gomod, gosum, err := moduleFiles(goSrc)
	if err != nil {
		return "", err
	}
	if gosum == "" {
		return goSrc, nil
	}
	var b strings.Builder
	b.WriteString("// This program is not standalone Go: it needs the modules below, which are\n" +
		"// the versions the compiler that emitted it was built against. `domain build`\n" +
		"// writes them into a throwaway module of its own; to build this source by\n" +
		"// hand, put this go.mod beside it, and this repository's go.sum with it.\n//\n")
	for _, line := range strings.Split(strings.TrimRight(gomod, "\n"), "\n") {
		if line == "" {
			b.WriteString("//\n")
			continue
		}
		b.WriteString("//\t" + line + "\n")
	}
	// After the generated-by line, which stays first.
	head, rest, found := strings.Cut(goSrc, "\n")
	if !found {
		return b.String() + goSrc, nil
	}
	return head + "\n\n" + b.String() + rest, nil
}
