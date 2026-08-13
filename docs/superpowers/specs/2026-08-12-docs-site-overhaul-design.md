# Docs site: wasm-by-default + JJK visual overhaul

## Part 1 — wasm playground works by default

Today `docs/wasm/domain.wasm` is a deliberately uncommitted build artifact
(`docs/wasm/README.md`, `docs/wasm/build.sh`) — `go build ./cmd/domain` alone
ships a docs site with no Run buttons unless someone has manually run
`docs/wasm/build.sh` first. Fix: automate that step at every build entrypoint,
without committing the artifact (staleness risk stays the reason it's not
committed).

- **Root `Makefile`**: `make build` runs `docs/wasm/build.sh` then
  `go build -o domain ./cmd/domain`. This becomes the documented primary build
  path.
- **`//go:generate ./wasm/build.sh`** added to `docs/embed.go`, so
  `go generate ./docs/...` also works standalone (editor/CI workflows that use
  `go generate` conventions).
- **`flake.nix`**: `buildGoModule` gets a `preBuild` phase that runs
  `docs/wasm/build.sh` before the main compile, so the Nix package (including
  NixOS) ships a working playground too. No network access needed — same Go
  toolchain already in the sandbox.
- Update `docs/wasm/README.md` and the top-level `README.md` build
  instructions to point at `make build`.

## Part 2 — lean into the JJK theme

The site already seeds a Jujutsu Kaisen motif (17 per-page "technique"
banners with kanji, color, animated icon — see `DOC_ANIM` in `index.html`).
The overhaul amplifies what's there rather than replacing the IA.

**Signature element:** the landing page (`README.md` render) currently opens
with a plain rendered markdown H1. It gets a bespoke hero: a canvas-drawn
"Domain Expansion" barrier cast — an octagon lattice that snaps open once on
load in the page's `--jjk-c` purple, settling into an ambient glow behind the
headline — with a live, auto-running mini Domain snippet directly in the
hero, so the language demonstrates itself in the first two seconds.

**Color:** extend the existing per-page `--jjk-c` technique color beyond the
banner icon. While viewing a given doc, that page's color drives: the active
sidebar link, `h2` bottom-border, blockquote left-border, and inline-code
accent. Each doc reads as visibly "its own domain."

**Type:** no new font file is embedded (avoids binary bloat + licensing
overhead for an offline-first, embedded site). Instead, a distinctive display
treatment for `h1`/brand/hero text: heavier weight, tightened tracking, and a
gradient text-fill keyed to `--jjk-c`, distinguishing display text from body
copy using the existing system-sans stack.

**Layout:** the 3-column IA (sidebar / article / page-toc) is unchanged.
Redesign effort concentrates on: the hero (landing only), the 17 banners
(upgrade from small icon-bar to fuller canvas-particle treatment at higher
fidelity, reusing each page's existing `fx` concept), the playground page
(reskin as a "cast" — glowing panel border, technique-styled Run button), and
the search overlay (amplify the Six Eyes scanning-grid effect already stubbed
via `.search-pulse`).

**Motion:** one orchestrated moment (hero cast, plays once per load) plus
amplified ambient effects on banners and search. All motion — new and
existing — stays gated behind the current `prefers-reduced-motion` block.

## Out of scope

- No changes to the Markdown content, IA/nav structure, or search/indexing
  logic itself.
- No new runtime dependencies (still a dependency-free `render.js`).
- No committed wasm artifact — Part 1 is automation, not a policy change.
