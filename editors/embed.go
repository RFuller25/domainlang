// Package editors carries the editor integrations — the VS Code extension and
// the Neovim/Vim runtime files — and embeds the VS Code one into the `domain`
// binary so `domain expansion: vscode` can install it without a source tree
// present, exactly as package docs does for the documentation site.
//
// The grammars in here are *generated* from the language itself: the
// primitives from prims.Registry, the builtins from typecheck.Builtins, and
// the themed keywords from ast.Keywords. `go test ./editors -update` rewrites
// them and every other run fails if they are stale (gen_test.go), because a
// hand-maintained copy of a 144-entry list is a copy that silently falls
// behind — as both grammars had.
package editors

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sync"
)

// vscodeFS holds the VS Code extension: the manifest, the language
// configuration, the CommonJS client for `domain lsp`, and the TextMate
// grammar. node_modules is deliberately absent — the extension declares
// `vscode-languageclient` as a dependency, and the installer says when it
// needs fetching.
//
//go:embed vscode/package.json vscode/extension.js vscode/language-configuration.json vscode/syntaxes/domain.tmLanguage.json vscode/README.md
var vscodeFS embed.FS

// VSCode is the extension's file tree, rooted at the extension directory (so
// "package.json", "syntaxes/domain.tmLanguage.json").
func VSCode() fs.FS {
	sub, err := fs.Sub(vscodeFS, "vscode")
	if err != nil {
		panic("editors: embedded VS Code extension is malformed: " + err.Error())
	}
	return sub
}

// Manifest is the part of the extension's package.json anything outside VS
// Code needs: what it is called and which version it is. It is read from the
// embedded manifest rather than restated in Go, so the installer can never
// name a version the extension does not declare.
type Manifest struct {
	Name        string `json:"name"`
	Publisher   string `json:"publisher"`
	Version     string `json:"version"`
	DisplayName string `json:"displayName"`
}

var (
	manifestOnce sync.Once
	manifest     Manifest
)

// VSCodeManifest returns the embedded extension's identity. A malformed
// manifest is a build-time mistake — editors/gen_test.go parses the same file
// — so the zero value is as far as the error handling needs to go.
func VSCodeManifest() Manifest {
	manifestOnce.Do(func() {
		b, err := fs.ReadFile(VSCode(), "package.json")
		if err != nil {
			return
		}
		_ = json.Unmarshal(b, &manifest)
	})
	return manifest
}

// VSCodeDirName is the folder the extension installs as. VS Code reads the
// identity out of package.json rather than the folder name, but the two
// conventionally match, and keeping the version in it means an upgrade
// replaces the same folder instead of leaving two side by side.
func VSCodeDirName() string {
	m := VSCodeManifest()
	return m.Publisher + "." + m.Name + "-" + m.Version
}
