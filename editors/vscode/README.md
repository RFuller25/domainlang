# Domain for VS Code

Language support for [Domain](https://github.com/RFuller25/domain) — the
JJK-themed pipeline language for Advent of Code where you name an algorithm and
the compiler is free to substitute a faster one.

## What you get

**Syntax highlighting** for the whole surface, generated from the language
itself so it cannot fall behind it: every pipeline primitive, all 144
expression builtins, the themed keywords, `Consider … As/Of` bindings,
indented argument keys, `Match Pattern` typed holes, `Mode:` values, and
foreign-language blocks — where a `Domain Expansion: Python` block is handed to
the Python grammar rather than coloured as Domain.

**A language server** (`domain lsp`), started automatically:

- live diagnostics as you type — the same engine as `domain expansion: lint`
- inlay type hints after every statement, and on every `Consider … Of` line
- hover: what a primitive does, plus the concrete type step once it resolves
- go-to-definition for Shikigami, across imported libraries
- completion for primitives, arguments and builtins
- quick fixes for the mistakes the diagnostics engine can repair

## Requirements

The `domain` binary must be on your `PATH` (or set `domain.server.path`).
Highlighting works without it; everything else needs it.

## Installation

The binary installs this extension itself:

```sh
domain expansion: vscode
```

That writes the extension into your VS Code extensions directory and tells you
to reload the window. `--insiders`, `--dir` and `--list-targets` handle the
other layouts (Insiders, VS Codium, Cursor, a remote `~/.vscode-server`); see
`domain expansion: vscode --help`.

## Settings

| Setting | Default | Meaning |
|---|---|---|
| `domain.server.path` | `domain` | the executable used for `domain lsp` |
| `domain.trace.server` | `off` | log the LSP conversation to the output channel |

The extension also sets 4-space indentation for `.domain` files, because
indentation is significant in Domain and tabs are not part of the layout rules.

## Commands

**Domain: Restart Language Server** — after upgrading the binary.
