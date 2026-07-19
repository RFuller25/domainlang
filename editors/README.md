# Editor support

Syntax highlighting for `.domain` files, plus full language features
(diagnostics with suggestions, primitive-documentation hover, completion,
go-to-Shikigami, quick fixes) via the built-in language server (`domain
lsp`). Wiring is in [docs/tooling.md](../docs/tooling.md). Both grammars
highlight the same surface: themed pipeline keywords, the **primitives**
(operation phrases like `Sum Each Group`, `BFS`, `Quicksort`) in their own
colour, indented argument keys (`Using:`, `Mode:`, …), loop drivers
(`Repeat`/`While`/`Iterate Until Fixed Point`), connector words
(`by`/`from`/`to`/`with`), strings with escapes and `{typed:holes}`, lambda
arrows, operators, `and`/`or`/`not`/`if`/`then`/`else`, numbers, the full set
of expression builtins (in call position), `Mode:` values,
`Ascending`/`Descending`, and the value-kind type names. Both also configure
`#` comments and spaces-only indentation — tabs are a lex error in Domain.

## VS Code (`editors/vscode/`)

A full extension: the TextMate grammar **and** a language client that
launches `domain lsp`, so hover, completion, diagnostics, go-to-Shikigami,
and quick fixes work out of the box.

Its one runtime dependency is `vscode-languageclient`, so install
dependencies once, then package or symlink the folder:

```sh
cd editors/vscode
npm install                       # pulls vscode-languageclient

# Option A — package and install:
npx @vscode/vsce package          # produces domain-language-0.3.0.vsix
code --install-extension domain-language-0.3.0.vsix

# Option B — symlink for local development (then reload VS Code):
ln -s "$PWD" ~/.vscode/extensions/domain-lang.domain-language-0.3.0
```

The extension finds `domain` on your `PATH`; if the binary lives elsewhere,
set **`domain.server.path`** in your settings. After upgrading the binary,
run **Domain: Restart Language Server** from the command palette. Set
`domain.trace.server` to `verbose` to inspect the JSON-RPC traffic in the
"Domain Language Server" output channel. The extension also defaults
`.domain` buffers to 4-space indentation with `insertSpaces` on.

> The language server itself is stdlib-only Go; only the thin VS Code client
> needs Node dependencies. `node_modules/` and any built `.vsix` are
> git-ignored.

## Neovim / Vim (`editors/nvim/`)

A classic runtime plugin (`ftdetect` + `ftplugin` + `syntax`) — no
dependencies, works in Neovim and Vim 8+.

**lazy.nvim:**

```lua
{ dir = "/path/to/domain/editors/nvim", name = "domain-lang" }
```

**Native packages (Neovim):**

```sh
mkdir -p ~/.local/share/nvim/site/pack/domain/start
cp -r editors/nvim ~/.local/share/nvim/site/pack/domain/start/domain-lang
```

**Nix / Home Manager** — the flake exports the plugin as a package:

```nix
programs.neovim.plugins = [
  inputs.domain.packages.${pkgs.system}.domain-nvim
];
```

The ftplugin sets `expandtab`, 4-space indentation, and `commentstring` for
`#`, so commenting plugins and auto-indent behave.
