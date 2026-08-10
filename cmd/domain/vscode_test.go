package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domain/editors"
)

// installTo runs the installer into dir and returns its exit code and stdout.
func installTo(t *testing.T, dir string, extra ...string) (int, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := cmdVSCode(append([]string{"--dir", dir}, extra...), &out, &errBuf)
	if errBuf.Len() > 0 {
		t.Logf("stderr: %s", errBuf.String())
	}
	return code, out.String()
}

// The installed folder has to be something VS Code can actually load: a
// package.json it can parse, and every file that manifest points at.
func TestVSCodeInstallProducesALoadableExtension(t *testing.T) {
	root := t.TempDir()
	code, out := installTo(t, root)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}

	dest := filepath.Join(root, editors.VSCodeDirName())
	b, err := os.ReadFile(filepath.Join(dest, "package.json"))
	if err != nil {
		t.Fatalf("no manifest in the installed extension: %v", err)
	}
	var pkg struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Main        string `json:"main"`
		Contributes struct {
			Grammars []struct {
				Path string `json:"path"`
			} `json:"grammars"`
			Languages []struct {
				Configuration string `json:"configuration"`
			} `json:"languages"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatalf("installed manifest is not valid JSON: %v", err)
	}
	if pkg.Version != editors.VSCodeManifest().Version {
		t.Errorf("installed version %q, want %q", pkg.Version, editors.VSCodeManifest().Version)
	}
	for _, rel := range []string{
		pkg.Main,
		pkg.Contributes.Grammars[0].Path,
		pkg.Contributes.Languages[0].Configuration,
	} {
		p := filepath.Join(dest, strings.TrimPrefix(rel, "./"))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the manifest points at %s, which was not installed: %v", rel, err)
		}
	}
	// The grammar is what the editor parses at load time; a broken one turns
	// highlighting off silently.
	g, err := os.ReadFile(filepath.Join(dest, "syntaxes", "domain.tmLanguage.json"))
	if err != nil {
		t.Fatalf("grammar not installed: %v", err)
	}
	var grammar map[string]any
	if err := json.Unmarshal(g, &grammar); err != nil {
		t.Fatalf("installed grammar is not valid JSON: %v", err)
	}
	if out == "" || !strings.Contains(out, dest) {
		t.Errorf("the report does not say where it installed:\n%s", out)
	}
}

// Running it twice over the same version changes nothing and says so, rather
// than silently overwriting what might be a local edit. --force is the way
// through.
func TestVSCodeInstallIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if code, out := installTo(t, root); code != 0 {
		t.Fatalf("first install: exit %d\n%s", code, out)
	}
	marker := filepath.Join(root, editors.VSCodeDirName(), "local-edit.txt")
	if err := os.WriteFile(marker, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := installTo(t, root)
	if code != 0 {
		t.Fatalf("second install: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "already installed") {
		t.Errorf("a repeat install did not report the existing one:\n%s", out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("a repeat install destroyed an existing file: %v", err)
	}

	if code, out := installTo(t, root, "--force"); code != 0 {
		t.Fatalf("--force install: exit %d\n%s", code, out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("--force left the old install in place; it must replace, not merge")
	}
}

// An older install is upgraded without being asked about: running this again
// after upgrading the binary is the expected path.
func TestVSCodeInstallUpgradesAnOlderVersion(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, editors.VSCodeDirName())
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dest, "stale.js")
	if err := os.WriteFile(stale, []byte("// from an older layout"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "package.json"),
		[]byte(`{"name":"domain-language","version":"0.0.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := installTo(t, root)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if strings.Contains(out, "already installed") {
		t.Errorf("an older version was treated as current:\n%s", out)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file from the older install survived the upgrade")
	}
}

func TestVSCodeListTargetsInstallsNothing(t *testing.T) {
	root := t.TempDir()
	var out, errBuf bytes.Buffer
	if code := cmdVSCode([]string{"--list-targets"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "VS Code") || !strings.Contains(out.String(), "Cursor") {
		t.Errorf("the target list is missing editors:\n%s", out.String())
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Error("--list-targets wrote something")
	}
}

func TestVSCodeArgErrors(t *testing.T) {
	for _, c := range []struct {
		name, want string
		args       []string
	}{
		{name: "a file argument", want: "takes no file", args: []string{"prog.domain"}},
		{name: "an unknown flag", want: "unknown flag", args: []string{"--verbose"}},
		{name: "--dir with nothing after it", want: "requires a directory", args: []string{"--dir"}},
		{name: "two destinations", want: "pass one", args: []string{"--dir", "/tmp/x", "--insiders"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseVSCodeArgs(c.args); err == nil {
				t.Fatal("expected an error")
			} else if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// The chooser prefers a directory that exists over one that does not, and
// falls back to VS Code's own so a fresh machine installs somewhere the editor
// will actually read.
func TestVSCodeChooseTarget(t *testing.T) {
	targets := []vscodeTarget{
		{name: "VS Code", dir: "/home/u/.vscode/extensions"},
		{name: "VS Code Insiders", dir: "/home/u/.vscode-insiders/extensions"},
		{name: "Cursor", dir: "/home/u/.cursor/extensions", exists: true},
	}
	got, err := chooseTarget(vscodeOptions{}, targets)
	if err != nil || got.name != "Cursor" {
		t.Errorf("chose %+v (%v), want the one that exists", got, err)
	}

	got, err = chooseTarget(vscodeOptions{insiders: true}, targets)
	if err != nil || got.name != "VS Code Insiders" {
		t.Errorf("--insiders chose %+v (%v)", got, err)
	}

	none := []vscodeTarget{{name: "VS Code", dir: "/home/u/.vscode/extensions"}}
	got, err = chooseTarget(vscodeOptions{}, none)
	if err != nil || got.name != "VS Code" {
		t.Errorf("with nothing installed, chose %+v (%v), want VS Code's own", got, err)
	}
}

// `expansion: vscode` has to reach the installer through the same dispatch
// every other expansion command uses, in both spellings.
func TestVSCodeExpansionDispatch(t *testing.T) {
	for _, args := range [][]string{
		{"expansion:", "vscode", "--list-targets"},
		{"expansion: vscode", "--list-targets"},
	} {
		cmd, rest, ok := expansionInvocation(args)
		if !ok || len(cmd) != 1 || cmd[0] != "vscode" {
			t.Fatalf("%v did not parse as the vscode command (cmd=%v ok=%v)", args, cmd, ok)
		}
		var out, errBuf bytes.Buffer
		if code := Expansion(cmd, rest, strings.NewReader(""), &out, &errBuf); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errBuf.String())
		}
		if !strings.Contains(out.String(), "Extension directories") {
			t.Errorf("%v did not run the installer:\n%s", args, out.String())
		}
	}
}
