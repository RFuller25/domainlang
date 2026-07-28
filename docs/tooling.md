# Tooling — the REPL, the language server, and the visualizer

All of them live in the one `domain` binary, alongside the CLI and the
[diagnostics engine](diagnostics.md). The step-by-step run visualizer
(`domain expansion: visualize`) is documented with the other commands in
[cli.md](cli.md#domain-expansion-visualize).

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

The themed keyword is optional here as it is in a file, which makes the REPL
considerably faster to type into:

```
domain> input.txt
=> "1000\n2000\n\n3000" : Text
domain> Split Text by "\n\n"
=> ["1000\n2000", "3000"] : List<Text>
domain> Sum Each Group
```

Tab completion at the head of a line offers both spellings: the themed
keywords first, then every primitive as a bare statement. See
[optional keywords](language.md#optional-keywords).

**Execution model — replay.** Each accepted statement re-runs the whole
accumulated program from the top. That keeps every feature exactly
file-semantics (Channels, `From:` consumers, Shikigami, loops, vows) with no
incremental special cases; AoC-scale pipelines replay in microseconds.
Two visible consequences:

- `Cursed Energy:` file targets are re-read on every statement, resolved
  against the working directory. There is no program stdin — the interactive
  terminal cannot double as one — so a file target that does not exist is a
  reported error rather than an empty read.
- `Reveal:` output is suppressed during replays — the REPL already prints the
  current value after every statement.

**Blocks.** A statement that needs an indented block (`Using:` lambdas,
`Channel "x":` bodies, `Shikigami` definitions) puts the REPL into
continuation mode (`   ...>`): keep typing indented lines, and finish with a
blank line or the next top-level statement. Statements that are already
complete evaluate immediately. Which errors mean "keep typing" is a property
of the error itself, not of its wording — the front end marks them, so
rephrasing a message can never change what the prompt does.

**Errors never end the session.** A statement that fails is reported through
the [diagnostics engine](diagnostics.md) — the same block `domain check`
prints, with the offending line, carets under the exact span, a "did you
mean", and the repaired line when the repair is unambiguous — and dropped;
the program stays at its last good state.

```
domain> Cursed Tecnique: Split
error[name]: unknown keyword "Cursed Tecnique"
  --> repl:2:1
   2 | Cursed Tecnique: Split
     | ^^^^^^^^^^^^^^^
  help: did you mean "Cursed Technique"?
  fix: Cursed Technique: Split
```

**Interactive terminals** get a full editor. Left/right arrows move within the
line and up/down walk the history — which persists across sessions (in
`$XDG_STATE_HOME/domain/repl_history`, else `~/.local/state/…`), skips an
immediate repeat, and parks the line you were typing so walking back down
returns it. A statement that needs an indented body has its continuation line
pre-seeded with a 4-space indent; press enter on that seeded-but-otherwise-empty
line to end the block. Ctrl+enter (on terminals whose keyboard protocol
supports it — kitty, WezTerm, iTerm2, ghostty, foot, …) or alt+enter (works
everywhere) forces continuation mode on a statement the parser wouldn't
otherwise ask for a block on; on a `:command`, which has no block, it is
ignored. Submitted lines are echoed into the scrollback with syntax
highlighting, and so is `:list`.

**Completion.** Tab completes keywords, primitives, argument labels/Mode
values (reusing the language server's own completion logic), REPL
`:commands`, and file paths for `Cursed Energy:`/`:load`/`:save` targets.
Repeated Tab cycles through the matches in place and shows the cycle under
the prompt; the top candidate also appears as dimmed ghost text ahead of the
cursor as you type.

**Live types.** Pause while typing and the statement you are writing is
resolved in the background; the type it would produce appears at the end of
the line (`: List<Int>`) before you submit it. The current value's type is
also kept in the terminal's title bar. Previews pause while the window does
not have focus — nobody is reading them.

**The session fits its terminal.** The palette follows the terminal's
background: both the REPL and the visualizer ask for it at startup and switch
to darker ink on a light one. Ctrl+L clears the screen (the scrollback keeps
the transcript). The cursor's shape says where the session is — a bar at the
top level, a block inside an unfinished body, and nothing at all while a
program runs.

**Evaluation is interruptible.** A submitted line runs off the event loop, so
the editor keeps painting and **Ctrl+C stops it** — the escape a `While` loop
that never terminates would otherwise not have, since raw mode has already turned Ctrl+C from a signal into a
keystroke. The interrupted statement is dropped and the session carries on.

While a program runs, a spinner marks it, an elapsed time appears once there
is one worth reading, and after ten seconds the hint says the thing you are
starting to wonder — an unbounded loop looks exactly like a loop that has not
finished yet. How far the replay has got is drawn beside the spinner, counted
in top-level stages, and mirrored onto the terminal's own progress indicator,
so a long replay is visible from a window that is not on top.

**Pasting a program works.** A multi-line paste is submitted line by line,
exactly as a piped script would be, instead of being flattened into one line —
and reported as one block: the statements, anything that went wrong, and the
value it ended on, rather than one intermediate value per line. `:paste` does
the same from the system clipboard.

**Output that does not fit opens a reader.** Anything taller than the window —
`:list` on a finished program, a long `:stats` profile, a hundred-row grid —
opens a full-screen pager (↑/↓, g/G, q) instead of scrolling the transcript
away. A paged profile also takes `s`, which re-orders it slowest-first from
measurements already taken.

**Ctrl+R searches the history** instead of walking it: type a fragment, Ctrl+R
again for older matches, Enter to put the match on the prompt — where it can
still be edited, because a search that submitted for you would be a search you
had to be sure about.

**Ctrl+O edits a whole block.** Continuation mode types a body one line at a
time, and a line already submitted is gone. Ctrl+O opens the block being built
in a proper multi-line editor — real up and down movement, the statement head
above it for context — and Ctrl+D submits the body as one statement.

**Ctrl+C and Ctrl+D.** Ctrl+C clears the line, then discards a half-typed
block, and only quits when there is nothing left to abandon. Ctrl+D quits on
an empty line, and refuses while a block is open. Either way, a session with
statements you have not saved asks once before discarding them (`:save` to
keep, `:quit!` to insist).

Piped input (`domain repl < script.domain`) always gets the plain
line-at-a-time reader described above, since there's no terminal to edit in.

Commands:

| Command | Effect |
|---|---|
| `:help` | list commands |
| `:list` | show the program built so far, highlighted |
| `:type` | the current value's type |
| `:stats` | replay under the profiler and chart where the time goes |
| `:undo` | drop the last statement (and show the restored value) |
| `:reset` | drop everything |
| `:load <file>` | replace the session with a file's statements (validated first; a broken file leaves the session untouched) |
| `:save <file>` | write the session's program to a file (`:save!` overwrites an existing one) |
| `:edit` | open the program in `$EDITOR`, and adopt whatever comes back (terminal only) |
| `:copy` | copy the program to the system clipboard (terminal only) |
| `:keys` | show the editor's key bindings — also Ctrl+G (terminal only) |
| `:visualize` | step through a recording of the session's own program (a text trace when piped) |
| `:replay` | run the program again as it stands |
| `:watch <file>` | replay whenever that file changes; bare `:watch` stops (terminal only) |
| `:doc [name]` | a primitive's signature and summary; bare `:doc` browses the catalog (terminal only) |
| `:docs [port]` | serve the documentation site for the rest of the session |
| `:paste` | load a program from the system clipboard (terminal only) |
| `:quit` | leave the domain (`:quit!` discards unsaved statements) |

A command's argument is the rest of the line, so a path with spaces in it is
one path; `~` expands to the home directory. A bare `:load` or `:save` opens a
file browser instead of guessing — `:load day7.domain` still just loads it,
since someone who typed the name knows the name.

### `:visualize` — the stepper, over the session

`domain expansion: visualize <file>` records a run and opens a stepper over the
recording ([cli.md](cli.md#domain-expansion-visualize)). A session's program is
not a file, so `:visualize` records it in place and hands the recording to that
same stepper as an overlay: step through it, press `q`, and the prompt is where
it was. The recording is of the program **as the session runs it** —
unoptimized — because that is the program being built; the rewrites the
optimizer would apply are collected separately for the explain pane.

Piped, `:visualize` prints the text trace instead, which is what `--plain`
prints and for the same reason.

### `:doc` — the primitive catalog

`:doc Fold` prints one primitive's keyword, signature and summary, from the
same catalog the language server hovers with. Bare `:doc` opens the catalog:
type to filter, Enter puts that primitive's statement on the prompt ready to be
finished.

`:docs` serves the embedded documentation website for the rest of the session
(the same site `domain expansion: documentation` serves) and, once it is
running, every diagnostic's error code becomes a terminal hyperlink into the
page explaining that class of mistake.

### `:watch` — replay when the input changes

`:watch input.txt` replays the program whenever that file changes: edit the
input in one window, watch the answer change in the other. The replay is the
one the session already does after every statement, so nothing about the
program's semantics differs — only what triggered it. Bare `:watch` stops.

### `:stats` — where the time goes

`:stats` replays the program under the same aggregating tracer `domain run
--stats` installs, and draws each stage's share as a bar colored by the
visualizer's heat ramp, so the expensive stage is the one you see first:

```
domain> :stats
[stats] 4 stage(s) · 43.9µs total · tree-walking interpreter, not the compiled binary
    1 Read Source <- nums.txt              39.2µs  89.1% █████████████████████░░░
    2 Split by "\n"                         1.8µs   4.0% █░░░░░░░░░░░░░░░░░░░░░░░
    3 Convert List to Integers              2.1µs   4.7% █░░░░░░░░░░░░░░░░░░░░░░░
    4 Sum                                    947ns   2.2% █░░░░░░░░░░░░░░░░░░░░░░░
```

A stage that opened frames (a loop, a Channel or Part body) lists its costliest
nested steps underneath it with their call counts. The numbers measure the
tree-walking interpreter, not the compiled binary — the header says so for the
same reason `--stats` does. Profiling runs the program again; it does not
change the session, and it can be interrupted like any other run.

### Comments and file round-trips

A comment typed at the prompt is not a statement: it is held and attached to
the statement it introduces, so `:undo` drops the pair and `:list` shows them
together. `:load` reads a file the same way — comments and the blank lines
around them travel with the statement below them — so `:load` followed by
`:save!` writes the file back unchanged, and the statement counts the REPL
reports are statements rather than lines.

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
| `textDocument/inlayHint` | the value **type flowing out of each statement**, at end of line — the REPL's `=> value : Type` feedback, in the editor |

Diagnostics and completion work on anything; definition needs a program that
parses. Hover documents the primitive on a line even when the program does not
yet type-check, and adds the concrete type step once it resolves. The
primitive documentation is the same catalog the `domain` binary carries, kept
in lock-step with the primitives themselves by a test.

### Inlay hints

```domain
Cursed Energy: input.txt                    : Text
Cursed Technique: Split Text by "\n\n"      : List<Text>
Channeled Energy: Convert Each List to…     : List<List<Int>>
Maximum Technique: Sum Each Group           : List<Int>
```

Per-construct rules: a `Shikigami` call shows the **call's** result type (its
inlined nodes carry positions from the definition — possibly in the prelude or
a library — so the resolver tags the group with the call site); a `Channel`
shows its **body's** result, which is what a `From:` consumer will see; a
`Part` and a `Binding Vow` show nothing, since neither changes the value.

**A program that does not resolve still gets hints.** Resolution stops at the
first error but hands back the nodes it built, so every line above a mistake
keeps its type — the REPL's incremental feel in a file you are still writing.

Hints come from an **unoptimized** resolve. The optimizer replaces, fuses and
deletes nodes, so an optimized node list cannot be mapped back to source lines;
the server never optimizes, so this is both correct and free.

Also on hover: a `Shikigami` definition shows its parameters and its declared
signature (`(k: Int) : List<Int> -> Int`) when it has one.

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
