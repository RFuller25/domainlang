# `domain expansion: development` — the plan

> **Built.** All seven phases are done; the editor is documented in
> [development.md](development.md). This page is kept as the record of what was
> planned and why, including the decisions taken and the two places the plan
> was wrong — noted under [how it went](#how-it-went).

A terminal editor for Domain, in the same binary as everything else: write a
program with the language's own knowledge of it on screen — types, errors,
completions, documentation — then pick an input, run it, and step through the
run without leaving the editor.

This page is the implementation plan for its first version: what is in it, what
is deliberately not, where the code goes, what has to change outside it, and
the order the work happens in. The feasibility research behind it is summarized
under [what the spike settled](#what-the-spike-settled).

## What v1 is

**A single-file editor for `.domain` programs that never needs a second
window.** Open a program (or start a new one), edit it with syntax
highlighting, see the type flowing out of every statement and the errors as
they appear, complete keywords and primitives, read a primitive's
documentation, choose or create an input file, run it, and hand the run to the
stepper. Save it and quit.

It is not a general-purpose text editor and does not try to be. It edits one
Domain program at a time, and every feature it has exists because the language
already knows something the editor can show you.

## The features

### The editing surface

| | Feature | Notes |
|---|---|---|
| 1 | Line buffer with a byte-offset cursor | Byte offsets because the lexer, `diag` and the language server all speak them |
| 2 | Per-line syntax highlighting | Faces resolved per byte, painted last; a broken line paints only itself |
| 3 | Scrolling viewport with clipping | Not optional — see [the measurements](#what-the-spike-settled) |
| 4 | Line-number gutter, with room for diagnostic markers | The gutter width is computed from the line count, so a program does not shift its own text as it grows |
| 5 | Undo and redo | Coalesced by run of typing, not per keystroke |
| 6 | Selection, cut, copy, paste | System clipboard via `atotto/clipboard`, already a dependency |
| 7 | Indent and dedent a line or selection | Tab/Shift-Tab, four spaces, matching `domain fmt` |
| 8 | Enter carries the leading indentation | The one editing affordance an indentation-sensitive language cannot do without |
| 9 | Word motions, line and document ends, page motion, go-to-line | |
| 10 | Search within the buffer | Incremental, wrapping, with match highlighting |
| 11 | Open, save, save-as | Reuses the REPL's picker, which already handles naming a file that does not exist |
| 12 | Unsaved-work guard on quit | The REPL guards its quit the same way, through a message filter rather than a check in `Update` |

### What the language knows

| | Feature | Source |
|---|---|---|
| 13 | Type hints at end of line — the type flowing out of each statement | `lsp/inlay.go` |
| 14 | Inline diagnostics: gutter markers, the message on the current line, a count in the status bar | `diag.Analyze` |
| 15 | Autocomplete for keywords, primitives, argument labels, Modes and file paths | `lsp.CompletionItems` plus `repl_complete.go`'s path source |
| 16 | Inspect the primitive under the cursor — keyword, signature, summary, concrete type step | The server's `hover`, lifted |
| 17 | Go to a Shikigami definition | The server's `definition`, lifted; `prims.DefSite` carries prelude and imported names too |
| 18 | Browse the documentation catalog, and insert the selected primitive's statement | `repl_doc.go`'s browser, near enough unchanged |
| 19 | Format the buffer | `format.Format` |

### Running and debugging

| | Feature | Notes |
|---|---|---|
| 20 | Choose or create the input file, and bind it to the program's `Cursed Energy:` target | The picker, plus an edit to the source line |
| 21 | Run, with output in a pane and Ctrl+C actually stopping it | `While` is unbounded by design; the run goes on a `tea.Cmd` with an `ir.Interrupter`, exactly as the REPL does it |
| 22 | Typecheck on idle, without running | The same debounce the REPL uses for its live type preview |
| 23 | Open the stepper over the current buffer, and come back to where you were | `repl_visualize.go` is the working precedent for the overlay handoff |

### Reading the input file

| | Feature | Notes |
|---|---|---|
| 24 | Analyze the selected input and rank candidate opening preludes | A new `shape` package: pure, and testable against a corpus that already exists |
| 25 | Insert the chosen opening at the top of the program | One prelude line, or a `Match Pattern` statement with an inferred template |

### The shell around it

| | Feature | Notes |
|---|---|---|
| 26 | `domain expansion: development [file]` | With `--input FILE` to preselect an input |
| 27 | A key-binding overlay | The stepper already gives a whole screen to its key list; this follows it |
| 28 | Light/dark theme detection, shared with the REPL and the stepper | `tea.RequestBackgroundColor` at startup, then `useTheme` |
| 29 | Status line: file, dirty marker, cursor position, current type, diagnostic count | |

### Deliberately not in v1

Multiple files or tabs; split panes; mouse support; rename and other
refactorings; macros; git integration; syntax-aware (structural) selection;
editing anything that is not a `.domain` program. Each is a reasonable thing to
want. None of them is what makes an editor for *this* language worth having,
and every one of them is easier to add to a working editor than to a plan.

## Where the code goes

In `cmd/domain`, as `dev_*.go` files in package `main` — beside `repl_*.go` and
`visualize_*.go`, which is where every other Domain-aware terminal UI in this
binary already lives.

The alternative, a separate `editor` package, reads better on paper and costs
more than it returns: the shared colour scheme (`visualize_style.go`), the file
picker, the documentation browser and the stepper are all package `main`
today, so extracting the editor means extracting those first. That is a large
refactor in service of a boundary nothing is currently pushing on.

| File | Holds |
|---|---|
| `dev.go` | The command: argument parsing, terminal detection, the piped-stdin refusal |
| `dev_buffer.go` | The line buffer — edits, motions, undo, selection |
| `dev_render.go` | Viewport clipping, gutter, painting, the status line |
| `dev_model.go` | The Bubble Tea model and the overlay stack |
| `dev_keys.go` | The key map and the help overlay |
| `dev_intel.go` | Type hints, diagnostics, completion, inspect, go-to-definition |
| `dev_run.go` | Running, interrupting, the output pane, the stepper handoff |
| `dev_suggest.go` | Presenting what `shape` found |

One new package, `shape`, holds the input analysis. It goes outside
`cmd/domain` because it is pure — an input file in, ranked candidate openings
out — with no terminal in it at all, and because its correctness is a corpus
question rather than a UI one.

## What has to change outside the editor

Five things, all small, and three of them improve what they touch.

**`lsp` gains an exported surface.** Today only `Serve`, `Server` and
`CompletionItems` are exported; `hover` and `definition` are methods on
`*Server` reachable only over the wire. A new `lsp/api.go` exposes the pure
analysis — resolve a document, then ask for hints, hover text or a definition
site — and the existing server methods become thin JSON wrappers over it. This
is the change that lets the editor use the language server's knowledge without
running a subprocess against itself, and it makes those paths directly testable
for the first time.

**`traceView` learns an in-memory source.** `visualize.go` reads the program
back off disk for its source pane. The REPL sidesteps this by passing
`path: "repl"` and letting the read fail. An editor with unsaved changes needs
better: an optional lines field that `source()` prefers, so the pane shows the
buffer you are actually looking at.

**`repl_highlight.go` gives up its role decision.** The spike copied
`styleFor`/`inKeywordPhrase` to compute faces instead of styles. That
duplication should not survive: one function decides a token's role, and the
REPL's string-returning highlighter and the editor's face-returning one both
call it.

**`expansion.go` gains `{"development"}`** in `expansionCommands`, plus its
help text — a one-line change the dispatcher already accommodates.

**The documentation gains a page.** `development.md`, linked from
[cli.md](cli.md) and [tooling.md](tooling.md), and indexed in
[README.md](README.md). `docs/embed.go` globs `*.md`, so it ships in the binary
with no further work.

## The phases

Each phase ends somewhere usable. The order is by risk first and by what
unblocks the most second.

### Phase 0 — promote the spike

Move `spike/` into `cmd/domain` as `dev_buffer.go` and the face half of the
highlighter, fold its duplicated role decision back into `repl_highlight.go`,
and carry its tests across. Delete `spike/`.

*Ends with:* the buffer, its highlighting and its property tests living where
they belong, and `go test ./...` still green.

### Phase 1 — an editor you can open a file in

The command, the model, the viewport with clipping, the gutter, the status
line, open/save/save-as through the picker, the quit guard, the key overlay,
theme detection.

*Ends with:* `domain expansion: development day7.domain` opens a real program,
highlighted and scrollable, and saves it back unchanged. This is the phase to
stop and use for an afternoon before going further.

### Phase 2 — an editor you would choose to type in

Undo and redo, selection, clipboard, search, word and page motions,
indent/dedent, go-to-line.

*Ends with:* nothing about the editing gets in the way. Everything after this
adds knowledge rather than mechanics.

### Phase 3 — the language on screen

The `lsp/api.go` lift first, since the rest depends on it. Then type hints on
idle, diagnostics in the gutter and status line, the completion popup, inspect,
go-to-definition, the documentation browser, format.

*Ends with:* the reason the editor exists. A program's types and errors are
visible while it is being written, without a save, a second window or a
language-server round trip.

### Phase 4 — run it and watch it

The input picker bound to `Cursed Energy:`, the run command with its
interrupter and output pane, the stepper overlay and the `traceView` source
change it needs.

*Ends with:* write, run, step through, fix, run again — the whole loop inside
one screen.

### Phase 5 — read the input

The `shape` package and its oracle test, then the suggestion overlay and the
insertion.

*Ends with:* choosing an input file offers you the opening that reads it.

### Phase 6 — finish it

`development.md`, the `cli.md` and `tooling.md` entries, the README index, a
pass over the key bindings for consistency with the REPL and the stepper, and a
walkthrough in `walkthroughs.md`.

*Ends with:* a feature someone else could find without being told it exists.

## How it is tested

The pattern is already established by `repl_tty_test.go` and
`visualize_test.go`: drive the model with injected messages and run the
commands it hands back, rather than driving a terminal.

- **Buffer and highlighting** — property tests over every program in
  `examples/` and `challenges/`: painting preserves text and width, every line
  lexes alone, the cursor never disturbs what is under it. These exist and pass.
- **The model** — injected `KeyPressMsg` sequences; a typed program round-trips.
- **Layout** — golden renders at a fixed terminal size, ANSI stripped.
- **`shape`** — an oracle test over the 33 `(.input, .domain)` pairs in the
  repository: the suggester's ranked candidates must contain the opening the
  real program actually uses. This is the part of the plan with the strongest
  test story, which is not where anyone expected it.
- **The `lsp` lift** — the existing server tests keep passing, and the newly
  exported functions get direct tests they could not have had before.

## What the spike settled

A working prototype of the editing buffer is on the branch. It answered the
three questions the plan hangs on:

- **Every line of every program in the repository lexes on its own** — all 610
  non-blank lines across 33 programs, indented continuation lines included.
  Domain is indentation-sensitive, so this was not a safe assumption; it is what
  makes per-line highlighting possible, and it means a half-typed string literal
  un-paints one line rather than the file.
- **Painting is width-preserving**, with the cursor at every column. That
  property is what the gutter, the end-of-line type hints and the diagnostic
  markers all stand on, and it is why the cursor can sit inside a token without
  any ANSI string ever being cut.
- **Painting costs more than lexing, by eleven to one.** Lexing a line is 3.9µs
  of a 45µs repaint; each Lip Gloss `Render` is 3.6µs and a line has about a
  dozen face-runs. A 40-line screen repaints in 0.61ms — comfortably inside a
  frame — but 200 unclipped lines take 9ms. So: clip to the viewport, and do not
  bother caching tokens.

## How it went

Seven phases, all landed, with the feature list intact. Three things are worth
recording against the plan rather than quietly absorbing.

**The spike's design paid off twice.** Faces — a syntactic role per byte,
turned into escape codes only at the very end — were built to let a cursor sit
inside a token without cutting an ANSI string. When Phase 2 needed selections
and search highlighting, they were the same kind of thing: a byte range that
overrides what is underneath. Nothing needed inventing, and nothing has to
understand ANSI.

**The `traceView` source change was load-bearing, not cosmetic.** The plan
listed it as a small change so the stepper's source pane would show an unsaved
buffer. It is the reason the stepper is honest at all: without it the pane
reads whatever is on disk under that name and shows a program that is not the
one being stepped through.

**Two things the plan got wrong.** It suggested caching the last successful
token list; the measurements said painting outweighs lexing eleven to one, so
the cache would have recovered 9% and clipping to the viewport was the actual
lever. And the key bindings needed a pass they were not planned to need: `ctrl+w`
for copy collided with nano's search and emacs' cut, and `ctrl+d` for the
catalog collided with the REPL's quit. Both moved.

**The suggester ended up with the strongest test story in the project**, as the
plan predicted for the least obvious reason: 31 of the 33 `(.input, .domain)`
pairs in the repository have their real opening in the ranked suggestions. The
two that do not are both a single integer — a seed, not a shape — and are named
in the test rather than tolerated.

## Decisions taken

- **A bare `domain expansion: development` opens the picker.** Not a scratch
  buffer: unlike `domain repl`, which starts empty because a session *is* the
  program being built, this edits a file, and the first question it has is which
  one. `:save` on an unnamed buffer would only ask the same question later.
- **Undo breaks after a pause.** A run of typing coalesces into one step, and
  the run ends when typing stops — so undo follows the rhythm of writing rather
  than the count of keystrokes.
- **The suggester runs on choosing an input file,** offering its ranking
  immediately, with a key to reopen it later.
- **`Reveal: stdout` output appears after the run.** A raw-mode terminal cannot
  take interleaved writes, which is the same reason `:visualize` captures it.
