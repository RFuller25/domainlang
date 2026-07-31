#!/usr/bin/env bash
# Build the documentation site's playground: the language front end compiled to
# WebAssembly, plus the JavaScript shim Go needs to start it.
#
# The output is deliberately NOT committed. It is a 5 MB build artifact that
# would have to be rebuilt on every language change to stay honest, and a stale
# one would run subtly different code from the docs describing it. The site
# treats it as optional: without it the Run buttons simply do not appear and
# the playground page explains this script.
#
# Once built, `go build ./cmd/domain` embeds it (see docs/embed.go), so the
# playground ships inside `domain expansion: documentation` too.
#
# Usage: ./docs/wasm/build.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

command -v go >/dev/null || { echo "build.sh: needs a Go toolchain on PATH" >&2; exit 1; }

# wasm_exec.js is part of the toolchain and must match the compiler that built
# the module, so it is copied from GOROOT rather than vendored. Go moved it in
# 1.24; check both homes.
goroot="$(go env GOROOT)"
shim=""
for candidate in "$goroot/lib/wasm/wasm_exec.js" "$goroot/misc/wasm/wasm_exec.js"; do
  [ -f "$candidate" ] && { shim="$candidate"; break; }
done
[ -n "$shim" ] || { echo "build.sh: cannot find wasm_exec.js under $goroot" >&2; exit 1; }

echo "building the playground (GOOS=js GOARCH=wasm)…"
# -s -w drop the symbol table and DWARF: nothing here is debugged in the
# browser, and they are a third of the payload.
GOOS=js GOARCH=wasm go build -C "$repo" -ldflags="-s -w" -o "$here/domain.wasm" ./cmd/domain-wasm
cp "$shim" "$here/wasm_exec.js"

size=$(( $(wc -c < "$here/domain.wasm") / 1024 / 1024 ))
echo "wrote docs/wasm/domain.wasm (${size} MB) and docs/wasm/wasm_exec.js"
echo
echo "the site picks it up on reload; rebuild the binary to ship it:"
echo "    go build -o domain ./cmd/domain"
