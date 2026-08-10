// `domain expansion: vscode` — install the VS Code extension that ships inside
// this binary. Like `documentation` it analyzes no program; its arguments are
// where to install and how:
//
//	domain expansion: vscode                 # install into VS Code
//	domain expansion: vscode --insiders      # …into VS Code Insiders
//	domain expansion: vscode --dir PATH      # …into an extensions directory you name
//	domain expansion: vscode --list-targets  # show what was found, install nothing
//
// It installs the extension as an **unpacked folder**, which is a first-class
// way for VS Code to load one: the editor scans its extensions directory at
// startup and reads any folder with a package.json. That means no `vsce`, no
// `.vsix`, no npm registry, and no network — which matters because the whole
// point is that a working `domain` binary is enough.
//
// The one thing it cannot do is install the language client's dependency:
// `vscode-languageclient` is an npm package, and fetching it needs npm. Without
// it the extension still highlights (the grammar is declarative and needs no
// runtime); the language server features are what wait. The installer says so
// rather than leaving a reader to discover it from a silent output channel.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"domain/editors"
)

// vscodeOptions is what the flags add up to.
type vscodeOptions struct {
	dir         string // an explicit extensions directory, or ""
	insiders    bool   // prefer the Insiders layout
	listTargets bool   // report the candidates and stop
	force       bool   // replace an existing install without asking
}

// vscodeTarget is one extensions directory the installer knows how to find.
type vscodeTarget struct {
	name   string // "VS Code", "VS Code Insiders", …
	dir    string // absolute path to the extensions directory
	exists bool   // whether it is there already
}

func parseVSCodeArgs(args []string) (vscodeOptions, error) {
	var o vscodeOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--insiders":
			o.insiders = true
		case a == "--list-targets":
			o.listTargets = true
		case a == "--force" || a == "-f":
			o.force = true
		case a == "--dir":
			i++
			if i >= len(args) {
				return o, fmt.Errorf("--dir requires a directory")
			}
			o.dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			o.dir = a[len("--dir="):]
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %q (vscode accepts --insiders, --dir, --list-targets, --force)", a)
		default:
			return o, fmt.Errorf("vscode takes no file argument (got %q)", a)
		}
	}
	if o.dir != "" && o.insiders {
		return o, fmt.Errorf("--dir and --insiders name different destinations; pass one")
	}
	return o, nil
}

// vscodeTargets lists the extensions directories worth trying, in preference
// order. The layout is the same on every platform — VS Code keeps extensions
// in a dot-directory under $HOME, not under the OS's application-data path —
// which is why this is one list rather than three.
//
// The forks are included because they load the same unpacked extensions and
// people using them are exactly the people who will not have VS Code's own
// directory. A remote/WSL install (~/.vscode-server) is included for the same
// reason: on a dev container, that is the only one that exists.
func vscodeTargets(home string) []vscodeTarget {
	candidates := []struct{ name, rel string }{
		{"VS Code", ".vscode/extensions"},
		{"VS Code Insiders", ".vscode-insiders/extensions"},
		{"VS Code (remote/WSL)", ".vscode-server/extensions"},
		{"VS Codium", ".vscode-oss/extensions"},
		{"Cursor", ".cursor/extensions"},
		{"Windsurf", ".windsurf/extensions"},
	}
	out := make([]vscodeTarget, 0, len(candidates))
	for _, c := range candidates {
		dir := filepath.Join(home, filepath.FromSlash(c.rel))
		info, err := os.Stat(dir)
		out = append(out, vscodeTarget{name: c.name, dir: dir, exists: err == nil && info.IsDir()})
	}
	return out
}

// chooseTarget picks where to install: an explicit --dir wins, then the
// Insiders directory when asked for, then the first directory that exists.
//
// When none exists it falls back to creating VS Code's own — the editor reads
// that directory at startup whether or not it happens to be there now, so a
// fresh machine installs correctly rather than being told to install VS Code
// first.
func chooseTarget(o vscodeOptions, targets []vscodeTarget) (vscodeTarget, error) {
	if o.dir != "" {
		abs, err := filepath.Abs(o.dir)
		if err != nil {
			return vscodeTarget{}, err
		}
		return vscodeTarget{name: "the directory you named", dir: abs, exists: true}, nil
	}
	if o.insiders {
		for _, t := range targets {
			if t.name == "VS Code Insiders" {
				return t, nil
			}
		}
	}
	for _, t := range targets {
		if t.exists {
			return t, nil
		}
	}
	if len(targets) == 0 { // unreachable: vscodeTargets always returns the list
		return vscodeTarget{}, fmt.Errorf("no extensions directory to install into")
	}
	return targets[0], nil
}

// cmdVSCode installs the embedded extension and reports what happened.
func cmdVSCode(args []string, stdout, stderr io.Writer) int {
	o, err := parseVSCodeArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil && o.dir == "" {
		fmt.Fprintf(stderr, "domain: cannot find your home directory: %v\n", err)
		fmt.Fprintln(stderr, "        pass --dir with the extensions directory to install into")
		return 1
	}
	targets := vscodeTargets(home)

	if o.listTargets {
		fmt.Fprintln(stdout, "Extension directories, in the order they are chosen:")
		fmt.Fprintln(stdout)
		for _, t := range targets {
			mark := "—"
			if t.exists {
				mark = "found"
			}
			fmt.Fprintf(stdout, "  %-22s %-6s %s\n", t.name, mark, t.dir)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Install into one of them with --dir, or into the first that exists by")
		fmt.Fprintln(stdout, "running `domain expansion: vscode` with no flags.")
		return 0
	}

	target, err := chooseTarget(o, targets)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 1
	}
	dest := filepath.Join(target.dir, editors.VSCodeDirName())

	// Something is already installed there. An upgrade replaces it without
	// ceremony — that is the whole point of running this again after
	// upgrading the binary — but reinstalling the *same* version over itself
	// would silently discard any local edit, so that one asks first.
	if installed, err := installedVersion(dest); err == nil {
		m := editors.VSCodeManifest()
		if installed == m.Version && !o.force {
			fmt.Fprintf(stdout, "%s %s is already installed at\n  %s\n\n", m.DisplayName, installed, dest)
			fmt.Fprintln(stdout, "Nothing to do. Reinstall over it with --force.")
			return 0
		}
		// Replaced rather than merged: a file left behind by an older layout
		// would be loaded alongside the new ones.
		if err := os.RemoveAll(dest); err != nil {
			fmt.Fprintf(stderr, "domain: replacing the existing install at %s: %v\n", dest, err)
			return 1
		}
	}
	written, err := copyFS(editors.VSCode(), dest)
	if err != nil {
		fmt.Fprintf(stderr, "domain: installing into %s: %v\n", dest, err)
		return 1
	}

	fmt.Fprintf(stdout, "Domain Expansion: VS Code\n\n")
	m := editors.VSCodeManifest()
	fmt.Fprintf(stdout, "  installed  %s %s\n", m.DisplayName, m.Version)
	fmt.Fprintf(stdout, "  into       %s\n", dest)
	fmt.Fprintf(stdout, "  files      %d\n", written)
	if !target.exists && o.dir == "" {
		fmt.Fprintf(stdout, "\n  note: %s's extensions directory did not exist and was created.\n", target.name)
		fmt.Fprintf(stdout, "        If your editor keeps extensions elsewhere, see --list-targets.\n")
	}

	fmt.Fprintln(stdout, "\nNext:")
	fmt.Fprintln(stdout, "  1. Reload the window — Command Palette → \"Developer: Reload Window\".")
	fmt.Fprintln(stdout, "  2. Open any .domain file. Highlighting works immediately.")
	if exe, err := os.Executable(); err == nil {
		fmt.Fprintf(stdout, "  3. For diagnostics, hover types and go-to-Shikigami, the extension runs\n")
		fmt.Fprintf(stdout, "     `domain lsp`. This binary is %s —\n", exe)
		fmt.Fprintf(stdout, "     put it on your PATH, or set \"domain.server.path\" to it.\n")
	} else {
		fmt.Fprintln(stdout, "  3. For the language-server features, put the `domain` binary on your PATH")
		fmt.Fprintln(stdout, "     or set \"domain.server.path\" to it.")
	}
	fmt.Fprintf(stdout, "\nThe language-server client needs its npm dependency once:\n")
	fmt.Fprintf(stdout, "  cd %s && npm install --omit=dev\n", dest)
	fmt.Fprintln(stdout, "Without it, highlighting still works and the server features stay off.")
	return 0
}

// installedVersion reads the version of an extension already installed at
// dest. A missing or unreadable manifest is "nothing usable is installed
// here", which is the only distinction the caller needs.
func installedVersion(dest string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dest, "package.json"))
	if err != nil {
		return "", err
	}
	var m editors.Manifest
	if err := json.Unmarshal(b, &m); err != nil || m.Version == "" {
		return "", fmt.Errorf("%s has no readable version", dest)
	}
	return m.Version, nil
}

// copyFS writes every file of an embedded tree to dest, returning how many.
// Directories are created as needed and files are written readable — an
// extension is data the editor reads, so nothing in it is executable.
func copyFS(src fs.FS, dest string) (int, error) {
	written := 0
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out := filepath.Join(dest, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			return err
		}
		written++
		return nil
	})
	return written, err
}
