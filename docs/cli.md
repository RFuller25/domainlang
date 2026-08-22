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

Plus the expansion commands. The first five are the diagnostics family (full
reference in [diagnostics.md](diagnostics.md)); the next five run your program
to answer their question ([below](#measurement-and-exploration)); the last four
hand you something the binary carries.

```
domain expansion: diagnosis <file>        error list with fix suggestions (read-only)
domain expansion: lint <file>             errors + style warnings + perf hints (read-only)
domain expansion: fix <file>              apply unambiguous fixes in place (original → .bak)
domain expansion: optimize <file>         optimization report; rewrites the source where possible (.bak)
domain expansion: maximum compile <file>  fix, lint, optimize, then compile and run with stdin

domain expansion: bench <file>            time all four backend × optimizer cells
domain expansion: coverage <folder>       which builtins and primitives a folder never exercises
domain expansion: stats <folder>          per-program runtime, LOC and optimizer passes, ranked
domain expansion: battle <a> [--lang L] <b>  race a Domain program against one in another language
domain expansion: mahoraga <f> <in> <exp>    adapt one program to one input; writes a binary and a recipe

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

The site's browser playground (the Run/Explain buttons) is a WebAssembly build
of the language, itself a build artifact — see
[docs/wasm/README.md](wasm/README.md). Building the `domain` binary with
`make build` at the repo root picks it up automatically; a plain
`go build ./cmd/domain` does not.

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
same engines `domain lsp` serves; `ctrl+r` runs the program on a monitor screen
that charts its memory and CPU, says which stage it is on and what value it is
carrying, and stops on `ctrl+c`; and `ctrl+t` opens the same stepper
`visualize` does, over the program on screen. Choosing an input file offers the
opening that would read it.

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
| `f` | the selected value, full screen and scrolling (see [below](#the-value-screen-f)) |
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

A **`Graph`** is drawn. Nodes are drawn once, at the first place a walk reaches
them, and the arcs between them as branches — starting from the roots, which is
where a `parent -> child` listing parsed out of text wants to be read from:

```
6 nodes · 5 arcs · 2 roots · weighted · acyclic

a
└─[1]─ b
       ├─[2]─ d
       │      └─[4]─ f
       └─[3]─ e

c
└─[1]─ e ↩
```

That is a spanning tree of the graph with the arcs that could not fit into a
tree **marked rather than dropped**: `↩` means "this node is drawn above", which
is what a second parent, a cross arc and a cycle all look like from here. A
drawing that dropped those arcs would be a different graph, and one that
followed them would not terminate. The weights ride on the arcs where there are
any, and are left off entirely where every arc weighs 1. A graph whose cycle
reaches every node has no root to start from and draws anyway, from wherever the
walk has not been; an isolated node is a piece of its own.

Past a few hundred arcs a drawing is mostly `↩` markers, so it gives way to a
listing — one node per line with its arcs — which stays readable at any size.
The line above the drawing is worth reading on its own: `has a cycle` is the
answer to the question `Topological Sort` refuses to be asked twice.

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

A [foreign block](ref-expansions.md#foreign-block--t---text-or-a-declared-in---out)
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

### The value screen (`f`)

The pane is half a screen wide and shares its height with everything else the
row has to say — the type, the timings, the input line, the headings. For a
number that is plenty. For the values this command exists to show it is not: a
40-column pane cuts a grid into a column of stumps and leaves a graph's drawing
folded at the width where its branches stop lining up.

`f` gives the selected row's value the whole terminal, and scrolls:

```
  value Convert To Graph
  Graph<Text> · 6 elements · 12 lines
  6 nodes · 6 arcs · 2 roots · weighted · has a cycle
```

| | |
|---|---|
| `j`/`k`, arrows | scroll a line |
| `ctrl+d`/`ctrl+u`, `pgdn`/`pgup`, space | scroll half a screen |
| `g`/`G` | the top and the end of the value |
| `z` | fold long lines into the width instead of cutting them |
| `y` | copy the value to the clipboard |
| `esc`, `q`, `f` | back to the tree |

It is the same renderers the pane uses, at the width they were written for, so
every type arrives in the shape it already had: a grid with its ruler across
the full width, a list with its index, `Text` with its whitespace visible, a
graph drawn. `z` is the other half of seeing all of it — a line wider than the
terminal has no width left to give, so it folds rather than being cut.

The screen shows the row's **output** — a block's `result`, where the row is a
`Channel` or a `Part`. The input is not offered: the recorder keeps only a
short rendering of it, since in a pipeline it is the previous step's output and
keeping both would double every recording. `d` is where the two are compared,
and it recovers the input honestly.

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

## Measurement and exploration

The five commands below answer their question by *running* a program rather
than reading it. All five share one execution layer (`runner`), so they agree
about what "how long does this take" means: every timed run is a subprocess,
the input is a redirected regular file rather than a pipe, and the reported
figure is the best of N runs interleaved between configurations so drift lands
on all of them equally. This is the methodology
[bench/README.md](../bench/README.md) established for the Domain-vs-Go suite,
applied to your own programs.

| Command | Asks |
|---|---|
| [`bench`](#domain-expansion-bench) | how long does this program take, in each of the four backend × optimizer cells? |
| [`coverage`](#domain-expansion-coverage) | what does this folder of programs never exercise? |
| [`stats`](#domain-expansion-stats) | how do a folder's programs compare, ranked? |
| [`battle`](#domain-expansion-battle) | is this faster than the same answer written in another language? |
| [`mahoraga`](#domain-expansion-mahoraga) | what schedule of passes suits *this* program on *this* input? |

### `domain expansion: bench`

Four cells — both backends, each with the optimizer on and off:

```sh
domain expansion: bench day15.domain                     # finds the input beside it
domain expansion: bench day15.domain --input input.txt   # or say where it is
domain expansion: bench day15.domain --runs 9            # more repetitions
domain expansion: bench day15.domain --cells interpret/optimized,compile/optimized
```

```
day15.domain · input: input.txt · best of 5

  cell                         time     peak RSS    allocated      build
  ──────────────────────────────────────────────────────────────────────
  interpret / naive      did not finish            —            —          —
  interpret / optimized  did not finish            —            —          —
  compile / naive        did not finish            —            —   226.56ms
  compile / optimized        1.415s       254 MB       460 MB   221.01ms

  2 optimizer pass(es) fired: fuseUnfoldStream, fuseFilterCount
```

**The cross-check is the point.** Every cell that produces output must produce
the *same* output: the naive/optimized pair is the optimizer's own correctness
oracle and the interpret/compile pair is the backends'. A disagreement is a
compiler bug, and the report says so in those words, shows the first differing
line, and exits 1. When only one cell survives — common on a heavy program,
where the naive pipeline cannot finish — the report says the cross-check was
skipped rather than quietly omitting the tick.

A cell that hits the timeout reads `did not finish` rather than showing a
number, and a ratio is only printed between two cells that both produced a
time.

Allocation comes in two forms. Peak RSS is from the kernel and always
available. Bytes allocated needs the measured process to report, which both
the interpreter and a compiled binary do — a dash means the figure was not
offered, never that it was zero. Allocation is measured in a separate,
untimed run, because reading the runtime's memory stats stops the world.

| Flag | Meaning |
|---|---|
| `--input F` / `--input-text T` | what the program reads (default: the sibling input) |
| `--runs N` | repetitions per cell (default 5) |
| `--timeout D` | per-run deadline (default 60s) |
| `--cells LIST` | a subset, e.g. `compile/optimized`; interpreting the naive pipeline is often the cell nobody wants to wait for |
| `--release` | measure with Binding Vows shed |
| `--json`, `--markdown`, `--plain` | machine-readable, pasteable, or unstyled |

### `domain expansion: coverage`

What a folder of programs has never exercised, against the catalog:

```sh
domain expansion: coverage examples/
domain expansion: coverage aoc2024/ --dynamic   # also run each one
domain expansion: coverage aoc2024/ --used      # invert: what you do use
domain expansion: coverage examples/ --min 40   # a CI gate
```

```
examples/ — 22 program(s)

  primitives   35 /  92  (38%)
  builtins     15 / 187  (8%)
  keywords      7 /   8  (88%)

  Channeled Energy — 7 not exercised
    Convert To Edges       Graph<K> → List<(K, K, Int)>  ref-coercions.md#convert-to-edges
    Convert To Entries     Map<K,V> → List<(K, V)>       ref-coercions.md#convert-to-entries
    …
```

Two things it is careful about, because a coverage number is easy to make
meaningless:

- **It measures the unoptimized pipeline.** `fuseMapMap` turns two `Map Each`
  nodes into one and `elideRedundantSort` deletes a `Sort` outright, so
  counting the optimized IR would report that a program which visibly uses
  `Sort` does not.
- **Builtins are counted from the source, primitives can also be traced.** A
  builtin is not a node — `gcd` is evaluated inside the expression layer and
  never reaches the trace hook — so it is counted where it is written, and a
  builtin inside a branch that never runs still counts. `--dynamic` runs each
  program against its input and additionally reports primitives that were
  *written but never evaluated*, which is the stronger finding. The header
  always says which mode produced the numbers.

A program that does not resolve, or has no input to run against, is listed as
skipped with the reason rather than silently dropped.

| Flag | Meaning |
|---|---|
| `--dynamic` | also run each program against its input, and report what was written but never evaluated |
| `--used` | invert the report: what the folder does use, most first |
| `--only prims\|builtins\|keywords` | one section rather than all three |
| `--exclude GLOB` | skip programs by basename |
| `--min PCT` | exit 1 when primitive coverage is below this, for CI |
| `--json`, `--plain` | machine-readable, or unstyled |

### `domain expansion: stats`

A whole folder at a glance — the portfolio command:

```sh
domain expansion: stats aoc2024/
domain expansion: stats aoc2024/ --sort time --top 10
domain expansion: stats aoc2024/ --markdown        # paste into a README
```

```
challenges/ — 13 program(s), compile / optimized, best of 3

  program                  LOC  stages    runtime  passes fired                 ✓
  ───────────────────────────────────────────────────────────────────────────────
  01_fizzbuzz               10       6     2.74ms  —                            ✓
  02_two_sum                 6       5     2.85ms  ×1 fuseAllPairsSum           ✓
  …
  ───────────────────────────────────────────────────────────────────────────────
  13 programs              139      94    34.16ms  3 rewrites                   13/13

  slowest         05_window_max (3.06ms) · 04_collatz (3.02ms)
  most rewritten  02_two_sum (1) · 05_window_max (1)

  vocabulary      23 / 92 primitives · 18 / 187 builtins
```

`bench` is for one program studied properly; `stats` runs one configuration
(compiled and optimized, unless `--interpret`) over a folder and ranks the
results. **LOC** is non-blank, non-comment lines — what a reader would count.
**stages** is top-level nodes in the *unoptimized* pipeline, so the column
measures what was written rather than what survived. **✓** compares the output
against the `.expected` sibling; a folder without them drops the column rather
than filling it with dashes. A program that fails keeps its row, with the
reason in place of a time, and makes the command exit nonzero — dropping it
would quietly shrink the folder and make every total wrong.

| Flag | Meaning |
|---|---|
| `--sort name\|time\|loc\|passes` | ranking (default `name`, which is day order for AoC naming) |
| `--top N` | keep the first N rows |
| `--runs N` | repetitions per program (default 3) |
| `--interpret` | measure the interpreter instead of a compiled binary |
| `--exclude GLOB` | skip programs by basename |
| `--json`, `--markdown`, `--plain` | machine-readable, pasteable, or unstyled |

### `domain expansion: battle`

Two programs, one input, one required answer:

```sh
domain expansion: battle day15.domain day15.py            # language inferred from .py
domain expansion: battle day15.domain --lang weave rival  # or name it
domain expansion: battle sum.domain sum.weave --runs 9
```

```
input: sum.input · best of 3

  sum.domain (Domain, compiled)
    run     8.37ms
    build   132.48ms
    first   140.85ms  (build + run — what you wait for the first time)
    peak    16.3 MB

  sum.weave (Weave)
    run     116.93ms
    peak    94.9 MB

  output ✓ identical (149966320234)

  SUM.DOMAIN (DOMAIN, COMPILED) WINS — 14.0× faster on the run
  sum.weave (Weave) wins to first answer — 1.2× (the build is the difference)
```

**Correctness gates the race.** Both programs run once and their output is
compared before any timing is reported. If they disagree the command prints
`NO CONTEST`, shows the first differing line, declares no winner, and exits 1 —
a faster program that prints the wrong answer has not come out on top. It
reports no timings at all in that case, because the race never ran and printing
the check's single run under a "best of N" header would be inventing a
measurement.

**Both clocks are reported**, because they can disagree and showing only one
would be taking a position. `run` is the compute. `first answer` adds the
build, which the compiled Domain side pays and an interpreted challenger does
not — which is exactly how the example above splits.

The verdict ends with the rules the numbers rest on, so the result can be
argued with rather than merely believed: both sides are subprocesses reading
the input as a redirected regular file, best of N alternating so drift lands on
both, the Domain side compiled and optimized (`--interpret` races the
interpreter instead, and the report says so), and the challenger run as its own
runtime runs it with no flags the user did not ask for.

The challenger's language comes from the [table](#foreign-languages) below.
A missing runtime is a setup problem rather than a failed race: the command
names it, says where to get it, and exits 2 without racing anything.

| Flag | Meaning |
|---|---|
| `--lang L` | the challenger's language; inferred from its extension when omitted |
| `--input F` / `--input-text T` | what both programs read (default: the sibling input) |
| `--runs N` | repetitions per side (default 5) |
| `--timeout D` | per-run deadline |
| `--interpret` | race `domain run` rather than a compiled binary |
| `--challenger-args A` | extra arguments for the challenger, shown in the verdict |
| `--json`, `--plain` | machine-readable, or unstyled |

#### Foreign languages

The same table backs `battle` and the `Domain Expansion: <language>` foreign
block, so a language works in both or neither:

| Language | Runs as | Override | Extensions |
|---|---|---|---|
| Python | `python3 program.py` | `DOMAIN_PYTHON` | `.py` |
| Go | `go run .` | `DOMAIN_GO` | `.go` |
| rask | `rask program.rask` | `DOMAIN_RASK` | `.rask` |
| cRust | `crust program.crust` | `DOMAIN_CRUST` | `.crust` |
| Weave | `weave run program.weave` | `DOMAIN_WEAVE` | `.weave`, `.wv` |

Each override may name a command with arguments (`DOMAIN_PYTHON="uv run
python"`), which is what makes these usable where a runtime is not on PATH
under its usual name.

[Weave](https://github.com/malleum/weavelang) fits the wire format without an
adapter: `weave run file.weave` feeds stdin to `Source`, and a program's final
bare expression is what it prints — a value in, a value out, which is the
contract every foreign block is built on.

### `domain expansion: mahoraga`

Adapt one program to one input:

```sh
domain expansion: mahoraga day11.domain input.txt expected.txt
domain expansion: mahoraga day11.domain in.txt want.txt --turns 5 --runs 20
domain expansion: mahoraga --replay day11.mahoraga.json      # rebuild from the recipe
domain expansion: mahoraga --verify day11.mahoraga.json in   # can I still use this binary?
```

Where `optimize` asks what is true of all programs, mahoraga asks what is true
of *this run*, and is allowed to exploit anything it can measure — switching
off an optimizer pass that pessimises this program, rebuilding against a
profile of the actual work, and (in later turns) cutting stages this input
never reaches. Those are answers to a question the general optimizer is not
permitted to ask, which is why they live in a separate command and never leak
into the pass list.

It writes two things: a binary (`<stem>-adapted`) and a **recipe**
(`<stem>.mahoraga.json`) recording every adaptation, what it measured, and the
ones rejected with why. The recipe is the durable half — reviewable in a diff,
replayable, and designed to be committed beside the program. A binary that is
faster for unexplained reasons is a liability.

**The eight turns.** Turn 1 is reconnaissance: a baseline of N runs whose mean
is what everything is compared against and whose spread sets the noise floor,
plus a CPU profile, two further untimed runs — one reporting the heap, one a
probe build reporting what the program's own bindings and list accumulators
held — and the Go the compiler actually emitted. Turns 2–8 each adapt to what
has been measured: dead code for this input, how the program is compiled, pass
ablation, pass ordering, templated codegen edits, guarded specialisation,
pinned specialisation. Turns not yet built are reported as such, so the report
distinguishes "found nothing" from "did not look".

**Turn 3 asks how `go build` should be invoked** — the one question the Domain
compiler never gets to ask. A rebuild against a profile of this program doing
this work; a larger inlining budget, which a Domain binary is unusually
sensitive to because `consider … in` lowers to nested closures and every
builtin is a call; and this machine's instruction set (`GOAMD64=v3`) where the
CPU has it. All general tier: a toolchain flag cannot change what a program
computes. Each is recorded in the recipe, since a binary must never end up
faster for a reason nobody wrote down.

**Turn 2 watches the program run**, interpreted, once, and looks for stages
that did nothing to the value that passed through them — a `Filter` that kept
every one of two million elements still evaluates its predicate two million
times and still copies the list. Only primitives where an unchanged length
means an unchanged value are eligible (`Filter`, `Filter Entries`, `Unique`,
`Merge Ranges`); a `Sort` preserves length and reorders, a `Map Each` preserves
length and replaces every element, and neither may ever be removed on the
strength of one. Removing a stage is pinned tier.

**The catalogue** (turns 6, 7 and 8) is a closed table of templated edits, each
one a measured fact about the input plus a place in the emitted Go where that
fact is worth something. An entry checks both: the facts say the input permits
it, the emitted source says the program has anywhere to apply it.

| Entry | Turn | Precondition | What it does | Tier |
|---|---|---|---|---|
| exact list capacity | 6 | the split-and-parse loop reserved `len/2+1`, and the input's segments were counted | reserves the measured count instead — for five-digit numbers the guess over-reserves 2.5× | guarded |
| one scheduler thread | 6 | the machine has more than one core and the baseline collected at all | `GOMAXPROCS(1)`: a Domain binary is a straight line of loops on one goroutine, and the Ps beyond the first exist for the collector's mark workers | general |
| collector off for one run | 6 | the baseline actually ran collections, and its heap fits under a limit | `SetGCPercent(-1)` with `SetMemoryLimit` as the backstop: a program that lives 50ms and exits gains nothing by tidying up | guarded |
| collector four times lazier | 6 | the baseline ran four collections or more | `SetGCPercent(400)` under a wider limit — the answer for a program that allocates far more than it keeps, where switching off is not an option | guarded |
| measured list capacity | 6 | the probe watched an accumulator the generator reserves *nothing* for grow past a few thousand elements | reserves the length it reached: a list that grows to five million is reallocated and copied twenty-two times on the way there | guarded |
| ASCII fast path | 7 | the program decodes runes | compiles the byte-indexed loop *and* the decode, choosing per line — correct on any input, faster on plain ones | guarded |
| no UTF-8 decoding | 8 | every byte of the input is one rune, and the program decodes runes | the same fast path with the check and the fallback removed | pinned |
| pinned constant | 8 | the probe watched a `Consider` binding hold one value for the whole run, and no stage writes it | emits the value at every site that reads it, so `% l` becomes `% 16` — a mask rather than a hardware division | pinned |

**Two of those rest on a probe build** rather than on the input file: a build
that reports what its own bindings held and how long its own lists grew, run
once, untimed, and thrown away. It is the only way to answer either question —
how many elements a filtered stream produces is a property of the data, and a
binding hoisted out of the loop it was written in is evaluated once and read a
million times. A binding that held a *different* value at any point is dropped
where it is read, so a loop variable can never be pinned; a capacity, being a
hint that `append` overrides, keeps the largest length it saw.

The turns divide by tier and shape together, so an entry runs in exactly one of
them: turn 6 takes the parameter edits, turn 7 the specialisations that compile
a fallback beside the fast path, turn 8 everything that gives the fallback up.
The last two rows are the same transformation at two tiers, which is what makes
`--tier` a concrete choice rather than a procedural one.

The measured facts and the resulting tuning are both recorded in the recipe, so
a reader can check the reasoning rather than take it, and a replay rebuilds the
same binary. The catalogue is closed on purpose: no entry can express "print
the answer", which is what makes that outcome unreachable rather than merely
rejected.

**The wheel.** On a terminal the search runs under an animated display: eight
handles at the compass points around a hub, one per turn, with a sweep rotating
through them at a speed set by how fast candidates are finishing. A handle is
hollow (`◌`) for a turn the catalogue has not reached, `·` for one not reached
yet, `◈` for the turn in flight, `○` for a turn that ran and kept nothing, and
`◆` — crimson through gold by how much it won — for one that adapted. An
adaptation flashes the whole wheel, because it is the search as a whole getting
faster.

| Key | While the wheel turns |
|---|---|
| `space` | hold the animation; the search keeps running |
| `s` | abandon the turn in flight and move to the next one |
| `a` | every candidate tried, kept or not, with why |
| `r` | what a recipe written right now would say |
| `p` | which optimizer passes the champion is built with |
| `?` | the key list |
| `q` | stop looking, but still re-measure and write both artifacts |
| `ctrl+c` | abort — nothing is written |

The wheel draws on the alternate screen, so the verdict is printed to the
terminal underneath once it exits — the animation is not the record. `--plain`,
`--json` and any non-terminal (a pipe, CI) get a line per event from the same
search instead; the search emits events and never learns which is watching.

**Measurement discipline** is the difference between a tuner and a random
number generator with good typography:

- The first run of any measurement is discarded as warmup — cold page cache, a
  freshly written binary, an unscaled CPU.
- The slowest quarter of what remains is discarded too. Interference on a
  shared machine is one-sided — a neighbour process can only make a run
  slower — so the upper tail is other people's work, not the program's spread.
  Leaving it in cost one search a real 16% win: minima 44.2ms against 52.3ms,
  means polluted to 51.4 and 62.4 with enough spread that the two could not be
  told apart.
- Every candidate is raced against the champion **interleaved**, alternating
  run by run, and the two figures compared are from the same minute. Timing a
  candidate alone against a champion measured earlier puts all the machine's
  drift on the candidate, which is enough to read a 15% win as "slower by 10".
- The **baseline runs in every race too**, as a third contestant. That makes
  the champion's standing against it a measurement rather than a product of
  ratios accumulated across turns, and it means a machine that drifts costs the
  search time rather than correctness — every comparison is internal to the
  race making it. How far the box moved is reported (`drifted_races`) and
  rejects nothing.
- The figure reported as "best" is the baseline scaled by that measured ratio,
  never the champion's own mean. Those two numbers come from different minutes;
  only a ratio within one race is drift-free.
- The noise floor is the **standard error** of the baseline mean, not its
  standard deviation. The two differ by a factor of √N, and using the
  deviation put the floor near 20% on a 20ms program, rejecting every real
  improvement.
- A candidate is accepted only when it is distinguishable from the champion
  given how precisely each is known, *and* clears the minimum effect worth
  recording. Too few samples to have a spread means not distinguishable.
- The champion is **re-measured against the baseline** after the search, fresh
  and interleaved, because a champion picked across dozens of measurements is
  partly picked for favourable noise. If that measurement does not confirm it,
  the baseline binary is written instead and the recipe records that the search
  was overturned.
- A search that found nothing says `BASELINE UNBEATEN`. A search that adapted
  nothing can never report a speedup: its champion *is* the baseline.
- A candidate that measured faster and still could not be distinguished is
  counted separately and reported. That is a question the measurement budget
  could not answer, not a failed candidate — on a noisy machine a real 17%
  win against a baseline known to ±9% is genuinely undecided, and the remedy
  (`--runs`) is one the user can act on.

**Tiers.** `general` adaptations hold for any input. `guarded` ones keep a
fallback — a capacity hint that is wrong still appends, a disabled collector
still has its memory limit — so they stay *correct* on any input and only stop
being optimal. `pinned` ones are valid only for the input's contract, and carry
**no runtime check**, because the verification happened while adapting. That
is deliberate: paying a startup check on every run forever, to re-derive a
fact settled once during the search, is the waste this command exists to
remove. The cost is stated rather than buried — a pinned binary is bound to
its input and will not notice a different one. `--verify` and `--replay` are
where that contract is re-checked, at build time, and `--verify` distinguishes
the two: a guarded recipe on a different input is worth mentioning, a pinned
one is refused.

The final report says which of these you actually got, from the adaptations
that were *kept* rather than from the tier the search was allowed to reach: a
run at the default `pinned` tier that kept only general and guarded adaptations
has produced a program that is correct on any input, and says so.

**The contract.** A pinned adaptation records the assumption it rests on, not
the file it was measured from. "Every byte is one rune" is satisfied by any
number of inputs, so `--verify` re-checks the assumption against a candidate
input and accepts it if it holds:

```sh
domain expansion: mahoraga --verify day11.mahoraga.json other-input.txt
```

Some assumptions cannot be re-established by looking at a file — whether a
`Filter` would keep every element of it is a property of running the program —
and those are recorded as unverifiable. A recipe carrying one is bound to the
input it was adapted to, and the refusal names the clause that binds it.

| Flag | Meaning |
|---|---|
| `-o PATH` | the adapted binary (default `<stem>-adapted`) |
| `--recipe PATH` | the recipe (default `<stem>.mahoraga.json`) |
| `--replay RECIPE` | rebuild from a recipe instead of searching, re-verifying the output |
| `--verify RECIPE [input]` | check a recipe's contract against an input, without building |
| `--turns N` | stop after N turns of the wheel |
| `--runs N` | measurement runs for the baseline and confirmations (default 10) |
| `--screen-runs N` | cheaper first measurement per candidate (default 3) |
| `--min-effect F` | the improvement worth recording, as a fraction (default 0.02) |
| `--tier general\|guarded\|pinned` | how far an adaptation may commit (default pinned) |
| `--seed N` | make the search reproducible |
| `-q`, `--quiet` | the verdict only, with no per-candidate progress |
| `--plain` | one line per candidate, unstyled (no wheel) |
| `--json` | write the recipe to stdout instead of a report |

`--runs` is the one to raise on a machine that is not quiet. It sets both the
baseline's run count and every confirmation's, so it is what the search's
ability to tell a real effect from noise is bought with — the report says how
many candidates it could not decide, and that number is what `--runs` spends
against.

Three levels of output, on one axis: the wheel when both ends are a terminal,
`--plain` for a line per candidate, `--quiet` for the verdict alone. `--quiet`
never suppresses the verdict itself; a flag that did would leave only an exit
code to interpret.

A recipe of general-tier adaptations can also be applied by the compiler
directly:

```sh
domain build day11.domain --recipe day11.mahoraga.json -o day11
```

`domain build --recipe` **refuses** a recipe carrying a guarded or pinned
adaptation. Those were verified against a particular input and `domain build`
has none to check them against; applying one there would produce a binary bound
to a contract nobody checked. `mahoraga --replay` is the path that has the
input and does the checking.

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
