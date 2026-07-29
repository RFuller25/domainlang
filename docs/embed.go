// Package docs embeds the browsable documentation website (index.html plus
// every reference page) directly into the `domain` binary. This is what lets
// `domain expansion: documentation` serve the docs from a standalone install
// — including the NixOS binary, where there is no source tree on disk to read
// the Markdown files from.
package docs

import "embed"

// FS holds the documentation site: index.html, the Markdown renderer it
// loads (render.js, split out so it can be unit-tested — see render_test.go),
// and all the Markdown pages it renders. Paths are flat (e.g. "index.html",
// "README.md"), which is exactly what index.html fetches at runtime.
//
//go:embed index.html render.js *.md
var FS embed.FS
