# Tooling — the REPL and the language server

Both live in the one `domain` binary, alongside the CLI and the
[diagnostics engine](diagnostics.md).

## The REPL (`domain repl`)

An interactive pipeline builder. Every line is an ordinary Domain statement;
the current value threads top to bottom exactly as it does in a file, and
after each accepted statement the REPL prints the new value and its type:

```
$ domain repl
Domain REPL — an interactive domain expansion. :help lists commands, :quit leaves.
domain> Cursed Energy: input.txt
=> "1000\n2000\n\n3000" : Text
domain> Cursed Technique: Split Text by "\n\n"
=> ["1000\n2000", "3000"] : List<Text>
domain> Cursed Technique: Split Each by "\n"
=> [["1000", "2000"], ["3000"]] : List<List<Text>>
```

**Execution model — replay.** Each accepted statement re-runs the whole
accumulated program from the top. That keeps every feature exactly
file-semantics (Channels, `From:` consumers, Shikigami, loops, vows) with no
incremental special cases; AoC-scale pipelines replay in microseconds.
Two visible consequences:

- `Cursed Energy:` file targets are re-read on every statement, resolved
  against the working directory. (The interactive terminal cannot double as
  program stdin, so use file targets in the REPL.)
- `Reveal:` output is suppressed during replays — the REPL already prints the
  current value after every statement.

**Blocks.** A statement that needs an indented block (`Using:` lambdas,
`Channel "x":` bodies, `Shikigami` definitions) puts the REPL into
continuation mode (`   ...>`): keep typing indented lines, and finish with a
blank line or the next top-level statement. Statements that are already
complete evaluate immediately.

**Errors never end the session.** A statement that fails to resolve or fails
at runtime is reported and dropped; the program stays at its last good state.

Commands:

| Command | Effect |
|---|---|
| `:help` | list commands |
| `:list` | show the program built so far |
| `:type` | the current value's type |
| `:undo` | drop the last statement (and show the restored value) |
| `:reset` | drop everything |
| `:load <file>` | replace the session with a file's statements (validated first; a broken file leaves the session untouched) |
| `:save <file>` | write the session's program to a file |
| `:quit` | leave the domain |

## The language server (`domain lsp`)

Speaks LSP over stdio (JSON-RPC 2.0, `Content-Length` framing), stdlib only.
It fronts the same engines as the CLI, so an editor shows exactly what
`domain expansion: lint` prints.

| Capability | What it does |
|---|---|
| `publishDiagnostics` | on open/change: the full [diagnostics engine](diagnostics.md) output — errors with "did you mean" suggestions appended, lint warnings, performance hints — with precise ranges |
| `textDocument/hover` | on a primitive: its **documentation** — keyword, signature, and a one-line definition drawn from [primitives.md](primitives.md) — plus the concrete type step (`List<Int> → Int`) when the program resolves; on a `Shikigami` definition: its signature |
| `textDocument/completion` | context-aware suggestions: themed keywords at the head of a line; the primitives registered under a keyword after its `:` (each with its signature and summary); the argument labels (`Using:`, `Mode:`, `Seed:`, …) on indented lines; and the `Mode:` values |
| `textDocument/definition` | on a `Shikigami: Name` call: jumps to the definition in the file (prelude names return nothing rather than a bogus location) |
| `textDocument/codeAction` | one quickfix — "apply N automatic fix(es)" — that applies every confident repair as a single document edit, the same set `domain expansion: fix` would write |

Diagnostics and completion work on anything; definition needs a program that
parses. Hover documents the primitive on a line even when the program does not
yet type-check, and adds the concrete type step once it resolves. The
primitive documentation is the same catalog the `domain` binary carries, kept
in lock-step with the primitives themselves by a test.

### Editor wiring

**Neovim** (v0.11+ native LSP config; the `editors/nvim` runtime files
already provide filetype detection and syntax):

```lua
vim.lsp.config['domain'] = {
  cmd = { 'domain', 'lsp' },
  filetypes = { 'domain' },
}
vim.lsp.enable('domain')
```

For nvim-lspconfig or older setups, register the same `cmd`/`filetypes` pair
manually.

**VS Code**: the `editors/vscode` extension is a full language client — it
ships the grammar **and** launches `domain lsp` itself, so diagnostics, hover,
completion, go-to-Shikigami, and quick fixes work as soon as it is installed
(`npm install` once to pull the client library — see
[editors/README.md](../editors/README.md)). It finds the binary on your `PATH`
by default; set `domain.server.path` if it lives elsewhere, and use the
**Domain: Restart Language Server** command after upgrading the binary.

**Anything else**: it is plain stdio LSP — configure `domain lsp` as the
server command for files matching `*.domain`.

## Exit codes

`domain repl` exits 0 (`:quit` or EOF). `domain lsp` exits 0 when the client
disconnects cleanly, 1 on a protocol-stream error.
