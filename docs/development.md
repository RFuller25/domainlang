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
| `alt+k` | inspect what is on this line |
| `alt+a` / `alt+A` | apply the fix for this line / every confident fix |
| `ctrl+]` / `ctrl+[` | go to a Shikigami definition, following imports / come back |
| `alt+z` / `alt+Z` | fold or unfold the block here / unfold everything |
| `alt+d` | browse the primitive catalog |
| `alt+f` | format the program |

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
| `ctrl+r` | run, and record |
| `ctrl+c` | stop a run that will not end |
| `ctrl+t` | open the stepper over the last run |
| `alt+↑` / `alt+↓` | walk the recorded stages, watching the value change |
| `alt+e` | what the optimizer did to the last run |

## Running a program

`ctrl+r` resolves the buffer and runs it, optimized, exactly as `domain run`
would. The program's `Reveal:` output appears in a pane when the run is over
rather than as it happens: a raw-mode terminal cannot take interleaved writes
from a program that thinks it owns stdout, which is why
[`:visualize`](tooling.md) captures it too.

`ctrl+c` stops a run. `Simple Domain: While` is unbounded by design, so this is
not a nicety — the run is deliberately handed to a background command so that
the editor stays able to hear the key. A run that was stopped is reported as
stopped rather than as failed.

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

## The input file

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
