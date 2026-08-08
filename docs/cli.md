# The `domain` CLI

One binary contains both backends. The mode is picked from the arguments:

```
domain <file.domain>                 interpret (bare file, no other args)
domain <file.domain> <args...>       compile (any extra argument selects the compiler)
domain run <file.domain> [flags]     interpret, explicitly (accepts the shared flags)
domain build <file.domain> [flags]   compile, explicitly
domain check <file.domain>           typecheck only: report the first error, run nothing
domain fmt <file.domain>... [flags]  canonical whitespace (see below)
domain repl                          interactive pipeline builder (docs/tooling.md)
domain lsp                           language server over stdio (docs/tooling.md)
domain help | -h | --help            print the full usage text
```

Plus the diagnostics command family (full reference in
[diagnostics.md](diagnostics.md)):

```
domain expansion: diagnosis <file>        error list with fix suggestions (read-only)
domain expansion: lint <file>             errors + style warnings + perf hints (read-only)
domain expansion: fix <file>              apply unambiguous fixes in place (original → .bak)
domain expansion: optimize <file>         optimization report; rewrites the source where possible (.bak)
domain expansion: maximum compile <file>  fix, lint, optimize, then compile and run with stdin
domain expansion: visualize <file>        step through a run in a terminal UI (see below)
domain expansion: development [file]      write a program in a terminal editor (see below)
domain expansion: documentation [-p PORT] serve this documentation as a local website (default port 4444)
domain expansion: vscode [--dir PATH]     install the VS Code extension carried in this binary
```

`documentation` and `vscode` are the two expansion commands that take no
program file — they hand you something the binary carries rather than
analyzing something you wrote. `development` is the one whose file is
*optional*: with none, it asks which program to open.

`documentation` serves the browsable documentation site (this reference,
rendered with search and cross-links) at `http://localhost:4444/` and opens it
in your browser. The optional `-p`/`--port` picks a different port. The whole
site is embedded in the `domain` binary, so it works from any install —
including the NixOS package — with no source checkout present. Press Ctrl+C to
stop the server.

### `domain expansion: development`

The editor. Full reference in [development.md](development.md).

```sh
domain expansion: development day7.domain              # open a program
domain expansion: development                          # pick one
domain expansion: development day7.domain --input day7.txt
```

| Flag | Effect |
|---|---|
| `--input FILE` / `-i FILE` | bind the program's input before opening |

Types at the end of every line and errors in the gutter, both from the buffer
rather than a saved file; completion, inspect and go-to-definition from the
same engines `domain lsp` serves; run with `ctrl+r` and interrupt with
`ctrl+c`; and `ctrl+t` opens the same stepper `visualize` does, over the
program on screen. Choosing an input file offers the opening that would read
it.

A file that does not exist is a new program under that name. Unlike every other
command here it needs a terminal, and says so rather than failing later: an
editor has nothing to do without a screen.

### `domain expansion: vscode`

Installs the VS Code extension — syntax highlighting plus the language-server
client for `domain lsp` — into your editor's extensions directory:

```sh
domain expansion: vscode                 # into the first editor found
domain expansion: vscode --list-targets  # show the candidates, install nothing
domain expansion: vscode --insiders      # into VS Code Insiders
domain expansion: vscode --dir PATH      # into a directory you name
domain expansion: vscode --force         # reinstall over the same version
```

| Flag | Effect |
|---|---|
| `--list-targets` | list every extensions directory the installer knows, marking the ones that exist, and stop |
| `--insiders` | install into the VS Code Insiders layout |
| `--dir PATH` | install into the extensions directory you name (mutually exclusive with `--insiders`) |
| `--force` | reinstall when the same version is already there |

The extension is embedded in the binary and installed as an **unpacked
folder**, which is a first-class way for VS Code to load one — so there is no
`.vsix`, no marketplace, and no network involved. Candidate directories, in
order: VS Code, VS Code Insiders, a remote/WSL `~/.vscode-server`, VS Codium,
Cursor, Windsurf. The first that exists wins; if none does, VS Code's own is
created, since the editor reads that directory at startup whether or not it is
there now.

Running it again after upgrading the binary **upgrades in place**. Running it
against the version already installed reports that and changes nothing, so a
local edit is never silently discarded; `--force` is the way through.

Two things it deliberately does not do. It does not put the `domain` binary on
your `PATH` — the extension runs `domain lsp` for diagnostics and types, so
either `domain` is findable or `domain.server.path` names it, and the installer
prints the path of the binary you ran. And it does not fetch the language
client's npm dependency (`vscode-languageclient`), which needs npm: highlighting
works without it, the server features wait for `npm install --omit=dev` in the
installed folder. The installer prints that command with the path filled in.

The rule of thumb: **a bare program file runs it; anything more builds it.**
The explicit `run` subcommand exists because of that rule — it is the only
way to interpret *with* flags (`domain run prog.domain --explain`), since a
subcommand-less `domain prog.domain --explain` counts as "extra args" and
selects the compiler.

## Flags

Shared by `run` and `build`:

| Flag | Effect |
|---|---|
| `--explain` | print the optimizer's algorithm substitutions to stderr (or `no optimizations applied`) |
| `--no-optimize` | skip the optimizer; run/compile the naive pipeline (the correctness oracle) |
| `--release` | shed Binding Vows: `run` skips them, `build` compiles them out of the binary |

`run` only:

| Flag | Effect |
|---|---|
| `--stats` | per-stage counts and timings on stderr, after the program's output |
| `--verbose` | with `--stats`, also list the nested steps inside loop, Channel and Part bodies |

```
$ domain run day1.domain --stats
45000
[stats] interpreter, 7 stages, 42.9µs total (tree-walking evaluator, not the compiled binary)
    #  stage                                  out type            size      time      %
    1  Read Source <- input.txt               Text                  54    15.8µs  36.9
    2  Split by "\n\n"                        List<Text>             5     1.0µs   2.4
    …
    6  Repeat 4 (4 frames, 28 steps)          Sparse<Int>            5   614.9µs  91.8
```

"size" is the value's element count — a list's length, a grid's cells, a text's
bytes — and an em dash where a size is not a meaningful question (a scalar).
Work inside a loop, Channel or Part body is attributed to the stage that
encloses it, with the frame and step counts alongside; `--verbose` breaks that
down per node.

Two honest limits. `--stats` measures the **tree-walking interpreter**, which
is why the header says so — do not benchmark the language with it. And it is
`run` only: instrumenting a compiled binary would need generated
instrumentation and its own oracle tests, so `build` does not accept it.

`build` only:

| Flag | Effect |
|---|---|
| `-o <path>`, `--output <path>` | where to write the compiled binary. Default: the source name minus `.domain`, in the current directory (an extensionless source gets `.bin` so the build can never overwrite it) |
| `--emit-go <path>` | also write the generated Go source; `-` writes it to stdout |
| `--run` | run the binary immediately with the current stdin, propagating its exit code. Without `-o` the binary goes to a temp path and is cleaned up afterwards — a one-shot compile-and-run; with `-o` the binary is kept |

`check` takes no flags: it runs the static front end — lex, parse,
resolve (which is where Domain typechecks, per the "typecheck at resolve
time" rule) — and prints `<file>: ok` or the positioned errors. The parser
recovers at top-level statement boundaries, so one run reports every
independently broken line (capped at 10), one per line; resolve errors
still stop at the first, since later types depend on earlier ones. It
never reads program input or executes anything, so it is safe on programs
whose vows or data would fail at runtime; exit codes are 0 / 1 / 2 as
below. Errors inside Shikigami bodies carry an inlining trace
(`in Shikigami "X" (body at L:C): …`), and errors inside the embedded
prelude are labeled `prelude source L:C` so they are never mistaken for
positions in your file.

## `domain fmt`

Canonical whitespace, with gofmt's conventions:

```sh
domain fmt prog.domain            # print the formatted result to stdout
domain fmt -w prog.domain         # rewrite in place (only if it would change)
domain fmt -l *.domain            # list the files that are not formatted
domain fmt --check *.domain       # same, but exit 1 — the CI form
domain fmt -                      # read stdin, write stdout
```

| Flag | Effect |
|---|---|
| `-w`, `--write` | rewrite each file in place; a file that is already formatted is not touched (its mtime is preserved) |
| `-l`, `--list` | print the name of each file that would change, exit 0 |
| `--check` | print the name of each file that would change, exit 1 if any did |

What it normalizes: indentation to four spaces per level (taken from the
lexer's own layout, so it can never disagree with the program's structure),
one space after a keyword's colon, canonical spacing inside argument lines
(`Using:`, `Seed:`, `Mode:`, `From:`), runs of blank lines collapsed to one,
trailing whitespace, and a single final newline.

Two things it deliberately does **not** do:

- **It never adds or removes a themed keyword.** Every keyword is optional
  and mixing the two spellings line by line is a language feature, not a
  style violation ([optional keywords](language.md#optional-keywords)), so
  `fmt` leaves that choice to you.
- **It never rewrites the interior of an operation phrase.** A phrase's raw
  source text is load-bearing — it is read verbatim as a Shikigami call name
  and as a `Read Source` file target — so `data/day1.txt` can never become
  `data / day1.txt`. Argument lines, which are parsed structurally, are
  canonicalized in full.

A file that does not lex or parse is left **exactly** as it was, with the
error reported: `fmt` never rewrites a file it cannot fully understand, so it
cannot make a broken program worse. Tab indentation is a lex error, which is
why `domain expansion: fix` — not `fmt` — is what repairs it; `fix`'s output
is itself `fmt`-clean.

## `domain expansion: visualize`

Step through a run and watch the data change shape:

```sh
domain expansion: visualize day1.domain                     # the program names its own input
domain expansion: visualize day1.domain --input input.txt    # or say where it is
domain expansion: visualize day1.domain --input-text "1,2,3" # or give it inline
domain expansion: visualize day1.domain --watch              # re-record on every save
domain expansion: visualize day1.domain --plain              # print the trace as text
domain expansion: visualize life.domain --max-steps 0        # record the whole run
domain expansion: visualize day1.domain --go                 # open on the emitted Go
domain expansion: visualize day1.domain --expressions        # open on the expression pane
```

| Flag | Effect |
|---|---|
| `--input <file>`, `-i` | what the program reads |
| `--input-text <text>` | the same, given inline (a trailing newline is added) |
| `--max-steps N` | how many steps to keep (default 250,000; `0` keeps the whole run) |
| `--watch`, `-w` | re-record whenever the program or its input changes on disk |
| `--plain` | print the trace as text instead of opening the UI |
| `--depth N` | with `--plain`, how deep to nest before summarizing |
| `--json` | print the recording as data (see [below](#--json)) |
| `--go` | open on the emitted Go (see [below](#the-code-screen-c)); text under `--plain` |
| `--expressions`, `--exprs` | open on the expression pane (see [below](#inside-an-expression-x)) |
| `--expand-loops` | start with every lap of every loop open |
| `--no-optimize` | record the naive pipeline |

The program is resolved, optimized and run **once** under a recording tracer;
the UI then navigates what was recorded. Four consequences worth knowing:

- **A failing run is still explorable.** The trace shows every step that ran
  and the error on the one that failed, which is what makes this a debugger
  rather than a demo.
- **The whole program runs before anything is shown.** That is what makes the
  recording walkable in both directions, and it means a slow program is a wait.
  Past about half a second the command says so on stderr — `recording 84,000
  steps · 1.2s elapsed` — and erases the line before the UI takes the screen.
- **A long run is recorded in full by default.** The step cap is a memory bound
  and nothing else; at 250,000 steps a real solution over its real input fits.
  Where it does not, `--max-steps 0` removes the bound, and the header says
  `capped` whenever it was reached rather than quietly showing you a prefix.
- **The program's own `Reveal` output is captured**, not interleaved — a
  raw-mode terminal cannot take writes from underneath the UI. It is shown in
  the plain output under `revealed:`.

### A failure is always in the recording

A run that dies 400,000 steps in used to record its opening stretch, report
`capped`, and leave `!` with nothing to jump to — the tool saying "this run
failed" and "there is no failure here" in the same breath. A step that failed is
now recorded **however far past the cap it happened**, and the frames around it
are kept to hold it, so the failing row is reachable in every recording that has
one.

Loops, `Channel` bodies, `Part` bodies and nested `Using:` bodies are **frames
you can step into**: `Repeat 4` collapses to one row and opens into `Repeat 4
iter 2/4`, each holding that iteration's steps, and a `Map Each` with an
indented body opens into `Map Each body 2/4` — one frame per element, since a
body runs once per element. An inlined `Shikigami` has no frame, because there
is no runtime construct — its steps appear where the call was.

A collapsed row says what it is hiding — `Repeat 500 (500 iterations, 2000
steps)` — so the row that is most of the run explains itself before it is
opened.

### What a block produced

A `Channel` and a `Part` are **passthroughs**: the pipeline carries on with the
value that entered them, and what the code inside them computed is not the
value they hand on. Their row reports the **body's** result — its type, its
size, and the value itself — because that is the answer someone opening a block
in a debugger is after. The detail pane shows both, named:

```
Channel
type   Int
size   —
…
in     ["gojo\nnanami\nitadori\nnobara", "75\n120\n95"]

result
  what the body produced, after every step in it
  4

passes on
  the value the next stage receives, unchanged
  ["gojo\nnanami\nitadori\nnobara", "75\n120\n95"]
```

One lap of a loop reports its result the same way, so a loop can be read
without opening every iteration.

### Folded repetition

Anything that runs the same steps over and over — a loop's laps, a `Using:`
body applied to each element — is gathered behind **one row that opens onto all
of them**:

```
▾ Map Each                               List<Bool>    4   4.21ms   61.2%   1.1%
  ▸ 500 iterations (1500 steps)          Bool          —   4.16ms   60.1%
```

Nothing is summarized away — opening the fold gives every repetition, each
still holding its own steps — but the stages *around* it stay on screen, which
is what a trace with fifteen hundred nearly identical rows costs you. In the
text output the first one is printed and the rest are one line saying what is
behind it; `--expand-loops` prints them all.

### Keys

`?` shows this list in the UI — the footer carries only where you are, since a
legend wide enough to hold every key is the loudest thing on the screen.

| | |
|---|---|
| `j`/`k`, arrows | move; `ctrl+d`/`ctrl+u` by half a page; `g`/`G` to the ends |
| `l`/`h` | open and close a frame; `enter` steps in |
| `tab` | move the keys to the right-hand pane, to scroll it (see [below](#scrolling-a-pane-tab)) |
| `H` | jump to the hottest row — the one with the most self time |
| `!` | jump to the next failing step, wrapping |
| `:` or `#` | go to a step by its number — the one `--json` prints |
| `/` | search: narrows the tree live as you type, `enter` accepts, `esc` clears |
| `n`/`N` | next and previous match, with the tree left whole |
| `<`/`>` | give the tree less or more of the width |
| `x` | the row's `Using:` expression, one parenthesis at a time |
| `d` | what the stage changed — the value in against the value out |
| `t` | the timing profile — call sites ranked by self time |
| `s` | the program source, with each line's share of the run |
| `e` | the optimizer's rewrites |
| `c` | the emitted Go, full screen (see [below](#the-code-screen-c)) |
| `r` | record the program again, keeping this view (see [below](#recording-again-r-and---watch)) |
| `w` | write the recording beside the program as JSON |
| `y` | copy the selected value to the system clipboard |
| `o` | open the program at this stage's line in `$EDITOR` |
| `?` | the key list |
| `q` | quit; `esc` backs out of the pane focus, then a filter, then a pane, then the program |

`H`, `!` and `:` open whatever frames stand between the cursor and their
target: on a recording deep enough to need them, the row you want is usually
inside a collapsed loop, which is exactly why hunting for it by hand does not
work.

`o` uses `$VISUAL`, then `$EDITOR`, then `vi`, and tells it which line to open:
`+N` for the vi-family and `--goto file:line` for VS Code. An editor that
understands neither still opens the file.

### Scrolling a pane (`tab`)

The tree owned the movement keys, which made every other pane a poster: the
recorder keeps up to 64 KiB of a value and the pane showed the dozen lines that
fit, with no way to reach the thirteenth. `tab` moves the keys to the right-hand
pane; `j`/`k`, `ctrl+d`/`ctrl+u` and `g`/`G` scroll it, and `tab` or `esc`
brings them back. The footer says which half they are driving.

Two panes do more than scroll under focus. In the **profile** (`t`) and the
**source** (`s`), `enter` takes the tree to the row at the top of the window —
the call site you are looking at, or the first step recorded on that line. That
is what turns the two panes that answer *what should I fix* into a way to get
there.

### Where the time went

Every row — steps *and* frames — carries its share of the run, so a slow stage
is visible without reading a single duration:

```
step                                     out type                 size      time       %   self%
Read Source <- in.txt                    Text                       15    11.3µs   37.0%
Split by ","                             List<Text>                  8     1.1µs    3.6%
Repeat 3                                 List<Int>                   8    16.1µs   52.7%   35.6%
  Repeat 3 iter 1/3                                                        3.1µs   10.2%
    Map Each                             List<Int>                   8     3.1µs   10.2%
```

Two things make those numbers mean what they look like:

- **The denominator is the run, counted once.** A step's recorded duration
  already includes everything nested inside it, so summing every row would
  count the same nanoseconds once per level of nesting. The total is the sum of
  the **top-level** rows only — the same total [`--stats`](#flags) reports —
  which is what makes the top-level percentages add up to 100.
- **`self%` is the row's own work**, with its frames' subtracted. This is the
  column that keeps a `Repeat 500` row at 98% from reading as a slow loop
  primitive: it is 500 iterations of whatever is inside it, and `self%` says
  so. It is shown only where it differs from `%` — on a leaf step the two are
  the same number, and on a frame the self time is zero by construction.

In the UI the tree pane carries the share and a bar whose solid head is the
row's self time and whose light run is the rest; the detail pane spells both
out in full, and selecting a frame reports what that one iteration cost.

Percentages are shares of what was **recorded**: past `--max-steps` the run
continues but the recording does not, and the header says `capped` when that
happened. These are the tree-walking interpreter's timings, not a compiled
binary's.

### What the stage changed (`d`)

Every other pane describes a value. This one describes the *difference*, which
is what "watch the data change shape" was always supposed to mean: for a stage
that maps two hundred elements to two hundred elements, the answer is not what
came out, it is which of the two hundred moved.

```
what changed
the value in, against the value out

  8 in · 8 out · comparing elements

    3 items unchanged
  - 41
  + 42
    4 items unchanged
```

Both sides are broken into items the same way — lines for `Text`, rows for a
`Grid`, elements for a list, set or map — and compared as a diff, so an
*inserted* element reads as one insertion rather than as every element after it
having changed. Runs of untouched items collapse to a count, since a diff that
prints the hundred and eighty that stayed the same has buried its own answer. A
scalar just says what it was and what it became.

One caveat the pane states on itself when it applies. The recording keeps only
a **short** rendering of each step's input, because in a pipeline the input is
the previous step's output and keeping both would double every recording. So
the full input comes from the step before, checked against what this step says
it received. Where that check fails — the first stage, a branch, a body
boundary — the pane says so and compares what it has.

### Values in the shape they are

The value pane renders by type rather than as a wall of text. A `Grid` gets row
numbers and, when its cells are single characters, a column ruler — because a
coordinate is the whole reason to open a grid in a debugger:

```
   0123456789
 0 ..#.......
 1 ...#......
 2 .###......
```

A list, set or map gets one element per line with its index, so the two
hundredth element is somewhere you can scroll to rather than somewhere off the
right edge. `Text` keeps its line structure and shows its trailing whitespace as
`·`, which is the difference that most often explains why the next stage could
not parse it.

### Recording again (`r`, and `--watch`)

`r` runs the program again without leaving the stepper, and `--watch` does it
whenever the program or its input file changes on disk. What makes them worth
having is what happens around the re-run.

**The view survives it.** A new recording is a new tree, so the open frames and
the cursor are carried over by *path* rather than by pointer: the row that was
`Repeat 3 / iter 2/3 / Map Each` is that row again in the new recording. The
pane you were reading, the filter you had typed and the width you had set all
stay. When a row genuinely no longer exists — you deleted the stage — the cursor
falls back to its nearest surviving ancestor and says so.

**The difference is on screen.** Rows whose output changed are marked `●` in the
tree, and the footer says what moved:

```
re-recorded +7 steps · -18% time · 3 rows changed · revealed 41 → 42
```

A program that no longer resolves — the normal state of a file halfway through
being edited — leaves the recording you were reading on screen and puts the
error in the footer, rather than tearing the trace down over a missing bracket.

### The profile (`t`)

The tree answers *what happened*; the profile answers *what to fix*. They are
different questions, and on a recording with 400 loop iterations the tree
cannot answer the second one — 400 rows of 2µs each are individually invisible
and collectively the whole run. The profile rolls every call site's iterations
into one line and ranks them by self time:

```
where the time went
call sites by self time, worst first

Map Each ×400                   4.21ms  61.2%
Repeat 400                      1.10ms  16.0%
Split by ","                     956ns   1.8%
```

`H` jumps the tree cursor to the row behind the top entry.

### Inside an expression (`x`)

Every other pane describes a *stage*: `Map Each` took 200 numbers and gave back
200 numbers. The arithmetic in between — the `Using:` expression — is where the
wrong number is actually made, and `x` opens it up. Every parenthesis is its own
row, with the value it came to, nested the way the source nests:

```
expression
every parenthesis, and what it came to

  s = 4, r = 13

  consider t as s - 1 - min(list(abs(s * s - r), …           2
    s - 1 - min(list(abs(s * s - r), abs(s * s - s …         2
      s - 1                                                  3
      min(list(abs(s * s - r), abs(s * s - s - r), …         1
        list(abs(s * s - r), abs(s * s - s - r), a…          [3, 1, 5, 9]
          abs(s * s - r)                                     3
            s * s - r                                        3
              s * s                                          16
    if r = s * s then s - 1 else t                           2
      r = s * s                                              false
```

Three things about what is and is not there:

- **The rows are what ran, not what was written.** An `if` shows only the arm
  it took and a short-circuited `and`/`or` only the operand it needed — which
  is frequently the answer on its own.
- **Literals and bare names get no row.** `4` coming to `4` is noise, and what
  a parameter is bound to is the line above the rows.
- **One application is shown, and on a failure it is the one that failed.** A
  lambda applied to every element of a list ran hundreds of times. The pane
  replays one of them and says which: the first on a stage that succeeded, and
  on a stage that failed the application that raised the error — `application
  900 of 900 — the one that failed`. Showing element 1's tidy arithmetic beside
  a row marked ✗ at element 900 was the pane's most confident wrong answer, at
  the exact moment it was most likely to be read. A `Using:` written as an
  indented pipeline has no expression to break down, and says so — its stages
  are already rows of the tree.

The values are **recomputed on demand**, not recorded: the expression layer is
pure, so the recording keeps only the arguments of each step's first
application (`interp.Application`) and replays it when asked
(`eval.TraceLambda`). That is what makes this detail free in a run nobody opens
the pane for.

`--expressions` prints the same breakdown for every stage that ran one, without
opening the UI, and adds an `expressions` array to `--json`.

#### Inside a foreign block

A [foreign block](primitives.md#foreign-block--t---text-or-a-declared-in---out)
has no expression — its inside is another language's program — but `x` asks the
same question of it, and answers with what there is:

```
python block
the program, and the bytes that crossed to and from it

ran    /usr/local/bin/python3 program.py
took   12.7ms

source
  import sys
  for line in sys.stdin:
      print(int(line) + 1)

stdin · 6 bytes
  1
  2
  3

stdout · 6 bytes
  2
  3
  4
```

The bytes are the ones that actually crossed, not a rendering of the Domain
value on either side, because a foreign stage is the one place a value stops
being a value and becomes bytes — and that is where its mistakes are. A
trailing newline that was or was not there, a grid whose rows were not what the
block expected, an empty list that arrived as no input at all: none of those are
visible from the values, and all of them are visible here. Trailing whitespace
is shown as `·` and a stream with no final newline says so, for the same reason.

A block that failed keeps its `stderr` too — the traceback or compile error is
in the step's own error, and the input that provoked it is here.

**One run per stage is kept, and a failing one displaces the first.** A block
inside a `Map Each` body runs once per element, and a recording holding every
one of them would be the input again several times over. The first is the
representative sample — until one fails, at which point that is the run shown
(`run 41 of 200 — the one that failed`) and its `stderr` and its input are the
ones kept. A block that works for forty elements and dies on the forty-first is
the shape of the bug; its fortieth tidy `stdout` is not the answer.

Unlike an expression breakdown this is **recorded, not replayed**: a subprocess
is not a pure expression, so re-running one to recover its detail later is not
free of consequence and is not done. The capture is bounded — 4 KiB per stream,
1 MiB across a recording — and a stream past the budget says it was dropped
rather than reading as empty.

`--expressions` prints the same under a `foreign blocks:` heading, and `--json`
carries it as a `foreign` array (always, since it cannot be recovered later).

### The source pane (`s`)

The same profile, projected back onto the text you wrote — which is where a fix
has to happen. Each line's gutter carries the self time spent in the steps on
it, and the line of the selected step is highlighted, so the pane tracks the
tree as you move:

```
source
self time by line

 39.1%    1 Cursed Energy: in.txt
 16.2%    2 Cursed Technique: Split Text by ","
  1.3%    3 Channeled Energy: Convert List to Integers
 26.4%    4 Simple Domain: Repeat 3
  8.8%    5     Cursed Technique: Map Each
```

A step **inlined from the prelude or a library** reports that source rather
than a line number: inlining copies a `Shikigami`'s body into the caller with
the *definition's* positions, and `token.Position` carries no file, so a line
number would confidently point at the wrong line of your program. Those nodes
are marked at resolve time (`ir.MetaForeign`) and left out of the per-line
profile.

### The code screen (`c`)

What the stage under the cursor *is*, once compiled. A Domain stage is one
line; the loop, the allocation and the scan it becomes are twenty lines of Go.
`c` opens the emitted program across the whole terminal, at that stage's code —
its lines lit and marked in the gutter — and scrolls anywhere from there,
because the code around a stage is most of what makes it legible:

```
emitted go day1.domain · 155 lines
Split by "," → lines 133–141

 131 func main() {
 132     v1 := dmReadSource("day1.input")
▌133     v2 := make([]int64, 0, len(v1)/2+1)
▌134     start3 := 0
▌135     for i4 := 0; i4 < len(v1); i4++ {
```

| | |
|---|---|
| `j`/`k`, arrows | scroll a line |
| `ctrl+d`/`ctrl+u`, `pgdn`/`pgup`, space | scroll half a screen |
| `g`/`G` | the top and the end of the program |
| `z` | back to the selected row's code, after scrolling away |
| `esc`, `q`, `c` | back to the tree |

The code is the compiler backend's real output — the same source
[`domain build --emit-go`](compiler.md) writes, byte for byte. It is compiled
on demand, the first time the screen is opened, from the *recorded* program, so
a fused stage's rewrite and the Go it became are one keystroke apart.

Two rows have no code of their own and say so: a **frame**, which is a label
around a sub-pipeline rather than a stage, and a step the backend **fused into
its neighbour**. A program the backend cannot compile yet reports that instead
of the code — the interpreter ran it perfectly well, and the rest of the
recording is unaffected. `--go` prints the same source without opening the UI.

### `--json`

The recording as data, for a reader that is not a terminal — a CI job asserting
a stage stayed under its share of the run, or a tool that wants the trace
without parsing a table:

```sh
domain expansion: visualize day1.domain --json | jq '.hotspots[0]'
{
  "name": "Map Each", "prim": "Map Each", "line": 5,
  "calls": 400, "self": "4.21ms", "self_ns": 4210593, "self_pct": 61.2
}
```

The document carries the program, the run's failure if it had one, the
optimizer's messages, the program's `Reveal` output, the whole tree (nested,
with each row's `time`/`pct`/`self_pct` and source `line`), and the ranked
`hotspots`. A block's row also carries what its body produced, as `result`,
`result_type` and `result_size` — separate fields from `out`, which is the
value it passed on — and a folded run of loop laps is a row marked `folded`
holding those laps unabridged. With `--go` the emitted Go is in the `go`
field. Percentages are rounded to the tenth the UI displays, so a report
and a test of that report cannot disagree about what a step cost. `--json`
wins over `--plain` when both are given.

**Input and the terminal.** An interactive terminal cannot double as the
program's stdin — the same constraint [the REPL](tooling.md#the-repl-domain-repl)
documents — so the input comes from `--input`, from piped stdin (read in full
before the UI starts), or from the program's own `Cursed Energy:` file target.
Given none of those, the command says so rather than hanging.

**Bounded capture.** A program can run a million loop iterations over a
million-element list, so the recorder keeps at most `--max-steps` steps and
spends a fixed byte budget on full value renderings. Past either limit it keeps
running but stops storing, and says `capped` — a visualizer that quietly showed
a truncated run would be worse than one that admits it.

Without a terminal (piped, redirected, or `--plain`) the trace is printed as
indented text instead, which is also how it is tested.

## Input

`Cursed Energy: <target>` reads the named file, falling back to stdin when
the file does not exist — so both of these work:

```sh
domain run prog.domain              # prog reads ./input.txt if present
domain run prog.domain < input.txt  # …or whatever is piped in
```

When interpreting, relative targets resolve against the *program file's*
directory; a compiled binary resolves them against the *working directory*
(see [compiler.md](compiler.md) for this and the other documented delta).

## Libraries

`Innate Domain: <library>` imports a file of Shikigami definitions (see
[language.md](language.md#innate-domain--importing-a-library)). The target is
written without its `.domain` extension and is looked for in this order, first
hit winning:

1. the importing file's own directory;
2. each colon-separated entry of `$DOMAIN_PATH`;
3. `~/.config/domain/lib` — the place for a personal helper library.

A miss lists every directory searched. Imports are resolved at build time, so a
compiled binary never needs its libraries again.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | program error: unreadable program file, lex/parse/resolve failure, runtime error, vow violation, or a failed `go build` |
| 2 | usage error: no arguments at all, unknown flag, missing file argument, or a flag missing its value |

Compiled binaries follow the same convention: 0 on success, 1 with a
`domain: ...` message on stderr for runtime failures (including vow
violations in a debug build).

## The build toolchain

`domain build` writes a throwaway Go module to a temp directory and shells
out to `go build -trimpath -ldflags "-s -w"` with `CGO_ENABLED=0`, producing
a self-contained static binary (~1.5 MB). The Go toolchain must be on
`PATH`; the Nix package wraps the binary so this is always true (see the
[repository README](../README.md#install-with-nix)). Cross-compiling works
the usual Go way: set `GOOS`/`GOARCH` before `domain build`.

## Examples

```sh
domain day1.domain < input.txt                 # interpret
domain day1.domain -o day1                     # compile → ./day1
./day1 < input.txt                             # run the binary
domain run day1.domain --explain < input.txt   # see optimizer rewrites
domain run day1.domain --no-optimize           # the naive oracle path
domain build day1.domain --release -o day1     # vow-free release binary
domain build day1.domain --emit-go -           # inspect the generated Go
domain build day1.domain --run < input.txt     # one-shot compile-and-run
```
