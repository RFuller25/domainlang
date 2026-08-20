# `domain expansion: development` — the editor

Write a Domain program with the language's own knowledge of it on screen, pick
an input, run it, and step through the run — without leaving the terminal or
saving a file first.

```sh
domain expansion: development day7.domain              # open a program
domain expansion: development                          # pick one
domain expansion: development day7.domain --input day7.txt
```

The other `expansion:` commands analyze a program you already have. This is the
one you write it in.

| Flag | Effect |
|---|---|
| `--input FILE` / `-i FILE` | bind the program's input before opening |

A file that does not exist yet is not an error — it is a new program under that
name, and nothing is written until you save. A bare invocation opens the file
browser rather than an empty buffer: unlike [the REPL](tooling.md), where the
session *is* the program being built, this edits a file and the first question
it has is which one.

The command needs a terminal and says so rather than failing obscurely. Every
other command in the family has something to say without a screen —
`visualize` prints its trace, the REPL reads a script — and an editor does not.

## What is on screen

```
 1 │ Cursed Energy: day7.input                    : Text
 2 │ Shikigami: Lines                             : List<Text>
 3 ✗ Cursed Tecnique: Split Fields
 4 │ Maximum Technique: Sum
 5 │ Reveal: stdout
   │
 day7.domain*        unknown keyword "Cursed Tecnique"      3:1  1✗ 0!  ctrl+g keys
```

**Types at the end of each line.** The value type flowing out of every
statement, from the same resolver `domain check` uses. This is the REPL's
`=> value : Type` feedback, in a file you are still writing.

**Errors in the gutter.** A mark beside the line, the message for the line the
cursor is on in the status bar, and counts on the right. The mark stands where
the separator does, so a program's text never moves sideways because a line
acquired an error.

Both are computed from the **buffer**, not from a file on disk, and both
tolerate failure: a program that does not yet resolve still shows the types of
the prefix that did — which is the state a program spends almost all of its
life in. Analysis runs when typing pauses, not on every keystroke: a type that
flickers on every character is noise, and one that appears when you pause is an
answer.

## Keys

The scheme is a full-screen editor's rather than the REPL's, because that is
the muscle memory people bring to one; `micro` is the closest reference. It
agrees with the REPL where it matters — `ctrl+g` is the key list in both.

The tables below are the keys worth learning, not all of them: the ordinary
motions a full-screen editor binds (`home`/`end`, `pgup`/`pgdn`, `ctrl+←` and
`ctrl+→` by word, `ctrl+home`/`ctrl+end` for the whole program) are there and
behave as you expect. `ctrl+g` lists every binding, including those.

`ctrl+c` copies and does **not** leave. It interrupts a run while one is going,
and is the clipboard key the rest of the time. Leaving is `ctrl+q` alone, so a
buffer cannot be abandoned by the reflex that copies.

### Files

| Key | |
|---|---|
| `ctrl+o` | open a program |
| `ctrl+s` | save |
| `alt+s` | save as |
| `ctrl+q` | leave — again to discard unsaved changes |

### Editing

| Key | |
|---|---|
| `enter` | split the line, carrying its indentation |
| `tab` | complete, or indent where there is nothing to complete |
| `shift+tab` | dedent |
| `ctrl+z` / `ctrl+y` | undo / redo |
| `ctrl+c` / `ctrl+x` / `ctrl+v` | copy / cut / paste |
| `ctrl+a` | select the whole program |
| `shift`+motion | extend the selection |
| `ctrl+f` | find, incremental and wrapping |
| `ctrl+l` | go to a line number |

Undo breaks after a pause. A run of typing coalesces into one step and the run
ends when the typing does, so undo follows the rhythm of writing rather than
the count of keystrokes. A paste, a cut, an indent or a line break is its own
step whatever the timing.

### What the language knows

| Key | |
|---|---|
| `tab` | complete a keyword, primitive, argument label or path |
| `alt+k` | inspect the name under the cursor, or what is on this line |
| `alt+a` / `alt+A` | apply the fix for this line / every confident fix |
| `ctrl+]` / `ctrl+[` | go to the definition of the name under the cursor, or to a Shikigami definition, following imports / come back |
| `alt+z` / `alt+Z` | fold or unfold the block here / unfold everything |
| `alt+d` | browse the primitive catalog |
| `alt+f` | format the program |

**Inspecting a name.** `alt+k` on a word answers about the word: what kind of
name it is, its type, and the line that declares it — a `Cursed Object` global
(with the `Cursed Tool` lines that write it, or a note that nothing does), a
`Consider` binding, a Shikigami parameter, a lambda parameter. Anywhere else on
the line it answers about the statement, as it always has. `ctrl+]` follows the
same name to where it is declared. Both are the language server's own answers
([tooling.md](tooling.md#hovering-a-name)), so the editor and an LSP client
cannot disagree about where a name comes from.

**Fixes.** A diagnostic that carries a confident repair can apply it: `alt+a`
on the line, `alt+A` for every one in the program — the same repairs
`domain expansion: fix` makes, and it refuses when the program has been typed
since it was checked, because a fix is a byte range and a byte range against
different text edits the wrong characters.

**Errors and advice are both shown.** The status line counts errors, warnings
and hints separately: `domain expansion: lint` is the checker plus the linter,
and so is this.

**Following a definition** across an `Innate Domain:` import opens the library
file; `ctrl+[` comes back to where you were. Unsaved work is asked about rather
than discarded.

**Folding** takes its extent from the parse, not from the indentation — which
matters for the one case indentation gets wrong, a blank line inside a body.
Pressing `enter` after `Channel`, `Simple Domain`, `Part` or `Shikigami "X"`
indents into the body, because those four always take one. It does not guess at
which operations want a `Using:` line: nothing in the vocabulary declares that,
and guessing would be wrong more often than it was right.

Completion is the language server's own, so the editor and `domain lsp` cannot
offer different vocabularies. `ctrl+]` follows an imported name into its
library file; a prelude name is reported rather than silently doing nothing,
since it is real and simply has nowhere on disk to jump to.

### Running

| Key | |
|---|---|
| `ctrl+e` | choose the input file — then it offers an opening |
| `alt+i` | offer an opening again |
| `ctrl+r` | run — the monitor takes the screen and watches it |
| `ctrl+c` | stop a run that will not end |
| `alt+m` | reopen the last run's monitor |
| `ctrl+t` | open the stepper over the last run |
| `alt+↑` / `alt+↓` | walk the recorded stages, watching the value change |
| `alt+e` | what the optimizer did to the last run |

## Running a program

`ctrl+r` resolves the buffer and runs it, optimized, exactly as `domain run`
would — and hands the screen to the **run monitor** for as long as it takes.

The program's `Reveal:` output is captured rather than printed: a raw-mode
terminal cannot take interleaved writes from a program that thinks it owns
stdout, which is why [`:visualize`](tooling.md) captures it too. The monitor
tails what has been captured as it arrives, so capturing it no longer costs you
the *timing* of the output — only its place on the terminal. The whole of it is
in a scrollable pane when the run is over.

`ctrl+c` stops a run. `Simple Domain: While` runs to a billion iterations
before it gives up, which is no help to someone waiting, so this is not a
nicety — the run is deliberately handed to a background command so that
the editor stays able to hear the key. A run that was stopped is reported as
stopped rather than as failed.

## The run monitor

```
domain expansion: development  run monitor                            day7.domain
────────────────────────────────────────────────────────────────────────────────
⣾ running  3.412s  ·  2,481,003 steps  ·  727K steps/s        stage 4/7 ██████░░

  heap in use  38.2 MB                                           peak 51.0 MB
  ▁▂▃▅▇█▇▆▅▄▃▃▄▅▆▇█▇▅▃▂▁▁▂▃▄▅▆▇█
  cpu, share of one core  102%              process, this editor included
  ▇███▇███████▇████████▇███████▇
  0s                                                                    3.412s

  4.6 GB allocated in total  ·  39 GC cycles  ·  26.18ms paused for collection
  last run  5.900s ↓42%  ·  peak 71.0 MB ↓28%  ·  3,100,000 steps

  where it is  Repeat 3 iter 2/3  ·  depth 1
   3 │ Maximum Technique: Sum
 → 4 │ Simple Domain: Repeat 3
   5 │     Cursed Technique: Apply

 => [1000, 2000, 3000] : List<Int>

 output   as it is printed
  48

  ctrl+c stops the run
```

Five questions, answered while the answer can still change what you do:

- **Is it going, and how far has it got.** Elapsed time, the steps the run has
  taken, and which top-level stage it is on — the same depth-0 attribution
  `--stats` uses, so a loop of four hundred laps is one stage that takes a
  while rather than four hundred units of progress.
- **What it is spending.** Live heap and CPU, over the whole run. The history
  halves when it is full and keeps the heavier reading of each pair, so the
  chart always covers the run from its start and the peak survives being
  squeezed.
- **Where it is.** The line, the enclosing frame (`Repeat 3 iter 2/3`) and the
  nesting depth, from the trace hook every node evaluation already passes
  through rather than from a guess made outside the run.
- **What it has got.** The last reported step's value and type — the REPL's own
  `=> value : Type`, against a program that has not finished.
- **What it has said.** The `Reveal:` output, tailed as it is printed.

**The numbers are the process's, this editor included**, and the screen says
so. There is no way to ask the Go runtime what one goroutine's heap is, and a
number that pretended otherwise would be worse than no number.

Memory is read with `runtime.ReadMemStats` ten times a second — ~52µs a call
against `runtime/metrics`' 825ns, paid because the cheap reading only advances
at a GC, which turns a memory curve into a sawtooth of collection points. CPU
comes from `runtime/metrics`, which is the only one of the two that accounts
for it; those counters *do* only advance at a GC, so a reading that has learnt
nothing new repeats the last one rather than drawing a zero the process never
spent. Rendering the current value is not done per step: the screen raises a
flag and the next step to report renders one value, which is ten renderings a
second rather than two million.

**While it is running**, `ctrl+c` is the only key that means anything — there is
nothing else to do to a running program, and a keystroke that dismissed the
screen would hide what it was opened to show. A stop lands at the next node
boundary, so a single long primitive has to finish first; when that is what is
happening, the screen says so rather than looking like it ignored the key.

**When it has finished** the screen stays, as the report on the run: the
outcome, what it cost, the lines it spent itself on, and how it compares with
the run before it — which is the comparison worth having while making a program
faster, since it is the edit that is on trial rather than the program.

| Key | |
|---|---|
| `ctrl+c` | stop the run (while it is running) |
| `ctrl+t` | open the stepper over this run |
| `ctrl+r` | run it again |
| any other key | back to the program |

`alt+m` opens the last run's monitor again — the screen closes on any key,
which makes it easy to lose to a keystroke that was meant for the program, and
nothing about a finished run stops being true.

### What a run leaves behind

Every run records. The recorder is on the hot path of an instrumented run, but
an AoC-scale program is microseconds either way, and one recording answers
three questions at once — so running without recording would mean running twice
to see any of them.

A **value bar** above the status line shows what the line under the cursor
produced:

```
 => [1000, 2000, 3000] : List<Int>  3  39% of the run
```

That is the REPL's `=> value : Type` against a file, and it is the difference
between knowing a stage produced `List<List<Text>>` and knowing whether that is
the `List<List<Text>>` you wanted.

**Line numbers are tinted** by their share of the run, on the same ramp the
stepper's heat pane uses, so the expensive stage is the one that catches the
eye. `alt+↑` and `alt+↓` walk the recorded stages — the stepper's gesture,
against the program you are editing.

`alt+e` shows what the optimizer did: the `--explain` output, which is where
the language's central claim (a named algorithm is a *request*) becomes visible
in the place you are writing it.

`ctrl+t` opens the same stepper
[`domain expansion: visualize`](cli.md#domain-expansion-visualize) does, over
the program on screen. The recording carries the buffer's own source, so the
stepper's source pane shows the program you are looking at rather than whatever
is on disk under that name.


`ctrl+e` opens a browser over every file beside the program — an input is
whatever the puzzle gave you, so it is not filtered to `.domain` — and points
the program's `Cursed Energy:` stage at what you choose, adding one if there is
not one yet.

The binding is written **into the program** rather than held beside it in the
editor. The input file is part of what the program is; a binding that lived
only here would make the program behave differently under `domain run`, which
is the one thing an editor for a language must not do.

### The opening it offers

Choosing an input reads its shape and offers the statements that would take it
in, ranked, with the evidence for each:

```
opening for day7.input
what would read this input — the order is a guess, the choice is not

› Shikigami: Digit Grid
    every line is the same width and all digits

  Shikigami: Ints
    every line parses as an integer
```

The suggestions come from package `shape`, which reads the file and maps it
onto the vocabulary in [aoc-toolbox.md](aoc-toolbox.md#parsing--input): the
prelude's `Lines`, `Blocks`, `Ints` and `Digit Grid`, plus `Convert To Grid`,
`Split Fields`, `Extract Integers`, and a `Match Pattern` template inferred
from the lines themselves.

It **ranks rather than decides**, because some inputs are genuinely ambiguous.
A rectangle of digits is a grid or a column of numbers and nothing in the file
says which — the two most-cited cases in this repository, `10_shortest_path`
and `02_pair_sum`, are the same shape with different intents. Width orders
them and both are always offered.

Accepted statements land **after** the source stage, since the opening reads
the value the source produced. `esc` declines and leaves the program untouched;
`alt+i` asks again.

## What it does not do

No multiple files or tabs, no split panes, no mouse, no rename or other
refactoring, no macros, and it edits `.domain` programs only. Each is a
reasonable thing to want; none of them is what makes an editor for *this*
language worth having.

There is also no soft wrap, by design — Domain programs are line-oriented, and
a wrapped pipeline line reads worse than a scrolled one. A line wider than the
window scrolls sideways under a fixed gutter instead, so the cursor is always
somewhere you can see it.
