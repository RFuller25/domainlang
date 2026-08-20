# Editor support

Syntax highlighting for `.domain` files, plus full language features
(diagnostics with suggestions, primitive-documentation hover, completion,
go-to-Shikigami, quick fixes) via the built-in language server (`domain
lsp`). Wiring is in [docs/tooling.md](../docs/tooling.md).

**Both grammars are generated from the language itself** — the primitives from
`prims.Registry`, all 144 builtins from `typecheck.Builtins`, the keywords from
`ast.Keywords`, the prelude's Shikigami from `prims.PreludeNames()`. `go test
./editors -update` rewrites them and every other run fails if they are stale,
because a hand-maintained copy of a 144-entry list is a copy that falls behind
— as both of these had, by 41 primitives and 83 builtins. A second test reads
every operation phrase out of every program in the repository and fails on any
word no rule colours, which is what catches the spellings the registry does not
name (`Quicksort` resolves to the `Sort` primitive).

Both grammars highlight the same surface: themed pipeline keywords, the **primitives**
(operation phrases like `Sum Each Group`, `BFS`, `Quicksort`) in their own
colour, indented argument keys (`Using:`, `Mode:`, …), local bindings
(`Consider x As …` / `Consider x Of …`, with the bound name in the colour
its readers get), loop drivers
(`Repeat`/`While`/`Iterate Until Fixed Point`), connector words
(`by`/`from`/`to`/`with`), strings with escapes and `{typed:holes}`, lambda
arrows, operators, `and`/`or`/`not`/`if`/`then`/`else`, numbers, the full set
of expression builtins (in call position), `Mode:` values,
`Ascending`/`Descending`, and the value-kind type names. Both also configure
`#` comments and spaces-only indentation — tabs are a lex error in Domain.

The exception both grammars carve out is a
[foreign block](../docs/primitives.md#foreign-block--t---text-or-a-declared-in---out):
`Domain Expansion: Python` (or `Go`/`rask`/`cRust`) and everything indented
beneath it is another language's source, so neither grammar highlights it as
Domain. The region ends where the indentation returns to the statement that
opened it, which is the rule the lexer applies. VS Code embeds the language's
own grammar where one is installed, so a Python block reads as Python; the vim
syntax leaves the body plain, having no portable way to embed four grammars
that may or may not be present.

## VS Code (`editors/vscode/`)

A full extension: the TextMate grammar **and** a language client that
launches `domain lsp`, so hover, completion, diagnostics, go-to-Shikigami,
and quick fixes work out of the box.

**The binary installs it.** The extension is embedded in `domain`, so no
checkout, marketplace or `.vsix` is involved:

```sh
domain expansion: vscode                 # into the first editor found
domain expansion: vscode --list-targets  # VS Code, Insiders, Codium, Cursor, remote/WSL, …
domain expansion: vscode --dir PATH      # into a directory you name
```

Reload the window afterwards. Run it again after upgrading the binary and it
upgrades in place; run it against the version already installed and it says so
and changes nothing (`--force` reinstalls). Full flag reference in
[docs/cli.md](../docs/cli.md#domain-expansion-vscode).

Its one runtime dependency is `vscode-languageclient` — an npm package, which
is the one thing the installer cannot fetch. Highlighting needs nothing;
the language-server features need this once, in the installed folder (the
installer prints the path):

```sh
npm install --omit=dev
```

For development on the extension itself, work in the source folder instead:

```sh
cd editors/vscode
npm install                       # pulls vscode-languageclient

# Package it:
npx @vscode/vsce package          # produces domain-language-0.5.0.vsix
code --install-extension domain-language-0.5.0.vsix

# …or symlink it for a live edit loop (then reload VS Code):
ln -s "$PWD" ~/.vscode/extensions/domain-lang.domain-language-0.5.0
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
