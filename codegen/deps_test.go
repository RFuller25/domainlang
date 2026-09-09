package codegen

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The generated dependency manifest, and the rule that keeps it honest.
//
// codegen/deps_data.go says which modules an emitted program may ask for. It
// is generated from the repository's own go.mod and go.sum, and this is what
// makes "generated" true rather than aspirational: the moment somebody adds a
// dependency, bumps a version or runs `go mod tidy`, this test fails and
// `go test ./codegen -update` rewrites the file.
//
// It matters because the alternative is silent. A hand-maintained list drifts
// one version at a time, and the symptom is a user's `domain build` resolving
// a Bubble Tea this compiler's generated runtime was never checked against —
// which shows up as a game that paints slightly wrong, months later.

// depsData renders codegen/deps_data.go from the repository's module files.
func depsData(gomod, gosum string) (string, error) {
	version, requires, err := readGoMod(gomod)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`// Code generated from the repository's go.mod and go.sum by
// ` + "`go test ./codegen -update`" + `. DO NOT EDIT.
//
// See codegen/deps.go for why an emitted program may only name a module this
// compiler was itself built against.

package codegen

// depsGoVersion is the language version an emitted module declares. It is the
// repository's own, so a program this compiler emits is built under the rules
// this compiler was built under.
const depsGoVersion = `)
	fmt.Fprintf(&b, "%q\n\n", version)
	b.WriteString(`// depsRequire is every module in the repository's build list, one
// "path version" per line. The whole list rather than the handful an emitted
// program imports today: the requirements have to cover the transitive graph,
// and a subset would send ` + "`go build`" + ` to the network for the rest.
const depsRequire = ` + "`" + strings.Join(requires, "\n") + "`\n\n")
	b.WriteString(`// depsSum is the repository's go.sum, verbatim. It travels with the
// generated go.mod so that a build verifies the same hashes this repository
// does, from the local module cache, with nothing fetched.
const depsSum = ` + "`" + strings.TrimRight(gosum, "\n") + "\n`\n")
	return b.String(), nil
}

// readGoMod reads the language version and the require lines out of a go.mod.
// It is deliberately a small reader rather than golang.org/x/mod/modfile: this
// repository depends on nothing it does not need, and the two constructs it
// has to understand are a `go` line and a require block.
func readGoMod(text string) (version string, requires []string, err error) {
	inBlock := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			requires = append(requires, line)
		case line == "require (":
			inBlock = true
		case strings.HasPrefix(line, "require "):
			requires = append(requires, strings.TrimSpace(strings.TrimPrefix(line, "require ")))
		case strings.HasPrefix(line, "go "):
			version = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	if version == "" {
		return "", nil, fmt.Errorf("go.mod has no `go` line")
	}
	if len(requires) == 0 {
		return "", nil, fmt.Errorf("go.mod requires nothing, so an emitted program could not import anything")
	}
	return version, requires, nil
}

// updating reports whether the run was asked to rewrite generated files.
//
// The -update flag is declared once for this package, by the golden snapshot
// test in the external codegen_test package; both halves link into one test
// binary, so this reads the flag rather than declaring a second one that
// would panic at registration.
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

func TestModuleDepsMatchTheRepository(t *testing.T) {
	gomod, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("reading the repository's go.mod: %v", err)
	}
	gosum, err := os.ReadFile("../go.sum")
	if err != nil {
		t.Fatalf("reading the repository's go.sum: %v", err)
	}
	want, err := depsData(string(gomod), string(gosum))
	if err != nil {
		t.Fatal(err)
	}
	const path = "deps_data.go"
	if updating() {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v\nrun `go test ./codegen -update` to generate it", err)
	}
	if string(got) != want {
		t.Errorf("codegen/deps_data.go is out of step with go.mod/go.sum.\n" +
			"An emitted program would ask for versions this compiler was not built against.\n" +
			"Run `go test ./codegen -update` to regenerate it.")
	}
}

// TestGeneratedDepsAreUsable is the other half: the constants are not merely
// current, they parse back into a module file a build can use.
func TestGeneratedDepsAreUsable(t *testing.T) {
	if depsGoVersion == "" {
		t.Fatal("depsGoVersion is empty")
	}
	mods := requiredModules()
	if len(mods) < 2 {
		t.Fatalf("expected the repository's modules, got %v", mods)
	}
	for i := 1; i < len(mods); i++ {
		if len(mods[i]) > len(mods[i-1]) {
			t.Fatalf("requiredModules is not longest-first: %q before %q", mods[i-1], mods[i])
		}
	}
	for _, line := range requireLines() {
		path, version, ok := strings.Cut(line, " ")
		if !ok || path == "" || !strings.HasPrefix(version, "v") {
			t.Errorf("require line %q is not `path version`", line)
		}
		if !strings.Contains(depsSum, path+" "+version+" h1:") {
			t.Errorf("go.sum has no hash for %s %s, so a build could not verify it", path, version)
		}
	}
}
