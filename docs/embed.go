// Package docs embeds the browsable documentation website (index.html plus
// every reference page) directly into the `domain` binary. This is what lets
// `domain expansion: documentation` serve the docs from a standalone install
// — including the NixOS binary, where there is no source tree on disk to read
// the Markdown files from.
package docs

import "embed"

// FS holds the documentation site: index.html, the Markdown renderer it
// loads (render.js, split out so it can be unit-tested — see render_test.go),
// all the Markdown pages it renders, and the two generated data files.
//
// gallery.json carries the 32 runnable programs from examples/ and
// challenges/, which live outside this package and so cannot be embedded
// directly; primitives.json carries the primitive catalog, which lives in Go.
// Both are generated and kept honest by gen_test.go (`go test ./docs -update`).
//
// wasm/ holds the playground: the language compiled to WebAssembly, plus its
// worker. The module itself is a build artifact and is not committed (see
// docs/wasm/README.md), so this embeds whatever is there — the site probes for
// it at boot and hides the Run buttons when it is absent. Build it first and
// the playground ships inside the binary too.
//
// Paths are flat apart from wasm/ (e.g. "index.html", "README.md"), which is
// exactly what index.html fetches at runtime.
//
//go:embed index.html render.js *.md *.json wasm
var FS embed.FS
