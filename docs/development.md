# `domain expansion: development` — the editor

Write a Domain program with the language's own knowledge of it on screen, pick
an input, run it, and step through the run — without leaving the terminal or
saving a file first.

```sh
domain expansion: development day7.domain              # open a program
domain expansion: development                          # pick one
domain expansion: development day7.domain --input day7.txt
domain expansion: development day7.domain --aoc 2023/7 # open on a puzzle
```

The other `expansion:` commands analyze a program you already have. This is the
one you write it in.

| Flag | Effect |
|---|---|
| `--input FILE` / `-i FILE` | bind the program's input before opening |
| `--aoc YEAR/DAY` | fetch that Advent of Code day as the editor opens ([below](#advent-of-code)) |

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
| `ctrl+s` | tidy and save ([below](#tidying-on-save)) |
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
| `alt+a` / `alt+A` | apply the fix for this line, or go to the writes a perf hint is about / every confident fix |
| `ctrl+]` / `ctrl+[` | go to the definition of the name under the cursor, or to a Shikigami definition, following imports / come back |
| `alt+z` / `alt+Z` | fold or unfold the block here / unfold everything |
| `alt+d` | browse the primitive catalog |
| `alt+f` | format the program |
| `alt+o` | tidy without saving |
| `alt+r` | refactor: the rewrites that need a name from you ([below](#refactoring)) |
| `f2` | rename the name under the cursor |
| `alt+v` | offer Binding Vows — for the parse, or against a failure ([below](#binding-vows)) |

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

**Following a definition** across an `Inherited Technique:` import opens the library
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
| `alt+t` | switch between the example and your input |
| `ctrl+r` | run — the monitor takes the screen and watches it |
| `ctrl+c` | stop a run that will not end |
| `alt+m` | reopen the last run's monitor |
| `ctrl+t` | open the stepper over the last run |
| `alt+↑` / `alt+↓` | walk the recorded stages, watching the value change |
| `alt+e` | what the optimizer did to the last run |

### Puzzles

| Key | |
|---|---|
| `alt+c` | fetch an [Advent of Code](#advent-of-code) day — the puzzle, your input and the example |
| `alt+x` | check the last run's answer for the part you are on |

## Tidying on save

`ctrl+s` tidies the program and then saves it. Tidying is a fixed set of
rewrites that bring a working program to the shape the language's own advice
describes, without changing what it computes. `alt+o` applies the same
rewrites without saving, to look at what they do.

**A save never depends on it.** A program that does not check yet — a
misspelled operation, a type error, a missing import — is saved exactly as
typed, unformatted. So is one with nothing to tidy.

**It is one undo step.** `ctrl+z` puts the program back as you wrote it, and
saving that text again saves it as written: undoing the tidy is how you decline
it, and the editor does not rewrite a program it has just been told to leave
alone. Edit the program, or press `alt+o`, and it is offered again.

**Nothing is kept unless it still checks.** Each rewrite edits the source text
— comments stay where they were — and afterwards the program is resolved and
type-checked again, and the type of everything it prints is compared with what
it was. A rewrite that fails any of that is dropped and the rest carry on.

### What it rewrites

**Tuple parameters are destructured.** A lambda that only ever reads its
parameter through `item(p, k)` takes a pattern instead:

```domain ignore
Using: (st) ->                                Using: ((js, i, steps)) ->
    consider js as item(st, 0) in                 consider j as item(js, i) in
    consider i as item(st, 1) in                  tuple(set(js, i, j + 1), i + j, steps + 1)
    consider steps as item(st, 2) in
    consider j as item(js, i) in
    tuple(set(js, i, j + 1), i + j, steps + 1)
```

The field names are always yours. They come from a `consider NAME as
item(p, k) in` that opens the lambda's value — which is removed along with the
indexing — or
are borrowed from another lambda that already named the same field of the same
tuple type. (A pair's names are only borrowed within one top-level statement:
`(Int, Int)` is a point in one place and a range in the next.) A field nothing
has named is not given an invented one; the lambda is left alone and the status
line says how many are waiting. Name the field once, anywhere, and the others
follow on the next save. A parameter that is also used whole — returned,
passed on — keeps its name.

**Work every Part repeats moves above them.** The leading statements all the
Parts spell identically are written once, above the first:

```domain ignore
Part "1":                                   Shikigami: Lines
    Shikigami: Lines                        Channeled Energy: Convert To Integers
    Channeled Energy: Convert To Integers
    Maximum Technique: Sum                  Part "1":
    Reveal: stdout                              Maximum Technique: Sum
Part "2":                                       Reveal: stdout
    Shikigami: Lines                        Part "2":
    Channeled Energy: Convert To Integers       Maximum Technique: Max
    Maximum Technique: Max                      Reveal: stdout
    Reveal: stdout
```

It stops at the first statement that differs, and before anything that prints,
draws from the random stream, runs a foreign block, or calls a Shikigami from
an imported library (whose body it cannot see). It does nothing if the copies
it would delete from the later Parts carry comments, and says so. It does nothing when a
statement other than a Part follows the first Part, since that statement would
start from a different value.

**`Case:` lines that differ in one word become one template.** When each
case's tag is its differing word, `Case: inc "{r:word} inc {n:int}"` and
`Case: dec "{r:word} dec {n:int}"` become `Using: "{r:word} {kind:word}
{n:int}"`. The hole is called `kind` because that is the field the cases were
already producing, so nothing downstream changes. A `{kind:word}` hole accepts
any word where the cases accepted two, so a line with a third verb now parses
instead of stopping the program — which is why `Mode: Try`, where such lines
were being dropped on purpose, is left alone.

**A global that is set once, late, becomes a constant.** `Cursed Object: n As
0` followed later by a single top-level `Cursed Tool: n Of Count`, with
nothing reading `n` in between, becomes the one line `Cursed Object: n Of
Count` — and every stage reading `n` gets its optimizer rewrites back.

**One spelling per file.** Where a file mixes `Cursed Technique: Apply` with
bare `Apply`, the minority follows the majority, and a tie goes to writing the
keyword. `Cursed Energy:` is left as it is either way — the source line is the
one people keep the keyword on. The same goes for the case of phrase words:
`while` beside `While` becomes whichever the file uses more.

**The formatter runs last**, as `alt+f` would: indentation, spacing, trailing
whitespace, the final newline.

### What it does not, and why

Some of the same advice cannot be followed without something the program does
not contain — almost always a name. Those are not attempted on save; they are
[asked for](#refactoring) instead. The status line mentions them when it sees
them: a tuple whose fields nobody has named, a global that is still written
after it is declared.

One has no tool at all. Replacing a hand-written `Split Each by " "` and
`item(x, 0)` with a `Match Pattern` template needs to know that every line of
the input really has that shape, which is a question about the file rather
than the program. The [opening the editor offers](#the-opening-it-offers) for
an input file is the place that question is answered.

### Binding Vows

A vow after the parse is the cheapest check a program can carry, and
`--release` sheds it. But a vow is a claim about the *input*, and only you know
which claims are meant — a list of 1000 lines is `Count Equals 1000` for the
puzzle input and wrong for the example. So vows are offered, never applied.

`alt+v` runs the program as far as the end of the parse and lists what held of
the value there, each with its evidence:

```
vows after Shikigami: Ints
tried on this input and on day5.example — the ones that hold on both are facts about the puzzle

› Binding Vow: All Values > 0
    every number is positive (the smallest is 3) · holds on day5.example too

  Binding Vow: Count Equals 1000
    the list has 1000 items on this input · does not hold on day5.example
```

`enter` inserts the highlighted one, `a` inserts them all, `esc` leaves the
program alone. With the cursor on a top-level statement of the pipeline, the
vows are about that statement's value; anywhere else they are about the last
statement above the first Part, or the end of the opening run of splitting and
converting in a program with no Parts. The run is given five seconds.

**Two inputs are better evidence than one.** When the program's input has a
partner — the example beside the real input, [found as `alt+t` finds
it](#the-example-and-the-real-input) — every vow is tried on both. The ones
that hold on both are listed first, and `a` inserts only those: a vow that is
true of one file and false of the other is a fact about a file.

**After a run that died, the question changes.** A stage that fails is usually
failing on a value that was already wrong when it arrived: a blank last line,
Windows line endings, a ragged row. So when the last run failed and the
program has not been edited since, `alt+v` runs as far as the value that
*entered* the failing statement and offers the vows that value breaks — each
false on this run, each of which would have stopped it one statement earlier
with a message about the data rather than about the stage:

```
vows after Cursed Technique: Split Text by "\n"
what was wrong with the value the failed stage was given — each of these is false on this run, and would have stopped it here

› Binding Vow: Holds
      Using: (xs) -> ikke contains(xs, "")
    the last item is empty — a blank line at the end of the input
```

The output pane of a failed run says the offer is there. If nothing is unsound
about the value it says that instead, which is also an answer: the stage, not
its input, is what to look at.

## Refactoring

Saving tidies what can be tidied without asking. What it leaves is left for
one reason each time: the rewrite needs a name the program does not contain.
`alt+r` lists the rewrites available — for the name under the cursor first,
then for the program as a whole — and each asks for what it needs on the
bottom line before anything changes.

```
refactor
each is one undo step, and is kept only if the program still checks

› rename steps
    the lambda parameter under the cursor — 3 places

  name the fields of st (line 28)
    (Int, Map<(Int, Int), Int>, Int, …) — 1 field is read through item() with no name; …

  extract what the Parts share into a Shikigami
    Part "1" and Part "2" run the same 2 statements from line 7, except for one ((List<Int>, Int, Int)) -> (List<Int>, Int, Int)

  carry laps in the loop on line 12
    Simple Domain: While writes it every lap; as part of the loop's own value it becomes a constant afterwards, …
```

Each is checked exactly as the save-time rewrites are — the result must
resolve, type-check, and print values of the types it printed before — and a
refusal says why and changes nothing.

**Rename** (`f2`, or the first entry of `alt+r`). The name under the cursor is
one of a few things, each with a scope the parser already knows: a lambda's
parameter or `consider` local, in its lambda; a `Consider` binding, on its
statement; a Shikigami parameter, in the definition and at every call that
passes it by name; a `Cursed Object` global, or a Shikigami's own name, in the
whole file. Rename rewrites that scope and nothing else: a lambda parameter
that happens to share a global's spelling is left alone when the global is
renamed. The new name must not already be written anywhere in the scope, even
where it would turn out harmless — a rename that silently changes which `x` an
`x` is, is the one failure a rename is not forgiven for. A Shikigami from the
prelude or an imported library is not this file's to rename.

**Name a tuple's fields.** For each tuple that
[destructuring](#what-it-rewrites) is waiting on, one question per field that
is read and has no name: the field's index, how it is read, and its type. The
answers destructure every lambda over that tuple at once. An empty answer
leaves a field unnamed, and the lambdas that read it as they were.

**Extract what the Parts share.** When every Part runs the same statements
except for one thing — one number, one string, or one lambda — those
statements become a Shikigami taking that one thing as a parameter, and each
Part becomes a call. It asks for the Shikigami's name and the parameter's; the
parameter's type is read off the type checker. It needs at least two
statements to share, exactly one difference, and nothing in them that reads a
channel, which a Shikigami body may not.

**Carry a counter in its loop.** A global that a loop writes on every lap
costs every stage that reads it its optimizer rewrites. For the one shape
where the alternative is mechanical, this writes it:

```domain ignore
Cursed Object: laps As 0                  Cursed Technique: Apply
Simple Domain: While                          Using: (v) -> tuple(v, 0)
    Using: (v) -> v > 1                   Simple Domain: While
    Cursed Technique: Apply                   Using: ((v, laps)) -> v > 1
        Using: (n) -> (n / 2)                 Cursed Technique: Apply
            also laps := laps + 1                 Using: ((n, laps)) -> tuple((n / 2), laps + 1)
                                          Cursed Object: laps Of ((_, c)) -> c
                                          Cursed Technique: Apply
                                              Using: ((v, _)) -> v
```

Afterwards `laps` is declared once and never written. The shape is narrow on
purpose: a `While` or `Repeat` at the top level, whose body is nothing but
`Apply` stages, each writing the global at most once as the last thing its
lambda does, with nothing reading the global between its declaration and the
loop. A `Map Each` or a `Filter` in the body sees one element at a time and
has nowhere to carry a second value, so a loop containing one is refused, with
the line that stopped it.

**From a hint to its cause.** A stage that lost its rewrites to a written
global carries a `perf` hint, and the fix is not at the stage. `alt+a` on such
a line goes to where the global is written — and again to the next write, and
round. From there `alt+r` offers to carry it, when it can.

The save-time rewrites are also a command, [`domain tidy`](cli.md#domain-tidy),
for files outside the editor. The ones on this page are the editor's alone:
they are questions, and a command line is a poor place to be asked one.

## Running a program

`ctrl+r` resolves the buffer and runs it, optimized, exactly as `domain run`
would — and hands the screen to the **run monitor** for as long as it takes.

The program's `Reveal:` output is captured rather than printed: a raw-mode
terminal cannot take interleaved writes from a program that thinks it owns
stdout, which is why [`:visualize`](tooling.md) captures it too. The monitor
shows what has been captured as it arrives, so you still see the output in
real time. The whole of it is
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

## Advent of Code

`alt+c` asks for a year and a day, and fetches three things: the puzzle text,
your own puzzle input, and the worked example out of the description. The
description takes the screen, the two input files land beside the program, and
the example is bound as the program's input.

```
domain expansion: development  advent of code                        2023 day 7
────────────────────────────────────────────────────────────────────────────────
  Camel Cards  part 1 of 1  ☆☆                     example: aoc-2023-07.example

   --- Day 7: Camel Cards ---

  Your all-expenses-paid trip turns out to be a ride in an airship. Because
  the journey is long, you can play a game: Camel Cards. In Camel Cards, you
  get a list of hands, and your goal is to order them based on the strength of
  each hand.

  For example:

      32T3K 765
      T55J5 684
      KK677 28

    • the first hand is a pair of threes, so it is the weakest of the five
    • the second is three of a kind

 check  part 1: 6440 — the site has not seen this one
         from the only line of output, against the example

  y sends 6440 to adventofcode.com  ·  any other key leaves it unsent
```

The loop is one screen: read the puzzle, `ctrl+r` to run, `c` to check, `i` to
switch from the example to your real input, `c` again, `y` to send the answer.

| Key | On the puzzle screen |
|---|---|
| `c` | check what the last run produced |
| `s` | check, then offer to submit |
| `y` | send the offered answer (nothing else does) |
| `e` / `i` | read the example / your own input |
| `1` / `2` | part one / part two |
| `ctrl+r` | run the program without leaving the puzzle |
| `r` | re-read the page — part two appears once part one is solved |
| `d` | another day |
| `esc` | back to the program; `alt+c` returns to the same day |

**The example is bound first, not your input.** It is the input a program is
*written* against — small enough to read, walked through by the prose, and with
its answer in the text. `i` switches to the real one for the run that counts.
Both are written beside the program as `aoc-YEAR-DD.example` and
`aoc-YEAR-DD.input`, and the binding goes into the program's `Cursed Energy:`
stage like any other, so the program runs the same way under `domain run`.

Which listing in the description is the example is a **guess** — the first one
with more than one line in it. It is on screen, so a wrong guess is visible
rather than silent, and a corrected file is never overwritten by opening the
day again (`r` rewrites both).

**The answer comes from the run, not from typing.** A `Part "1":` block labels
its own output, so a [two-part program](walkthroughs.md) says which number
belongs to which part and there is nothing to guess. Without Parts the rule is
stated on screen — "the only line of output", "line 2 of 3" — rather than
applied silently.

**Checking costs no request** when the site's answer is already known: a solved
part carries "your puzzle answer was" on its page, which is what makes this
worth pressing while refactoring a day you have already finished. When the site
has not seen the answer, the only thing that knows is the site, so the answer is
*offered* and `y` sends it.

### The session cookie

Your puzzle input is yours — the site hands it out against the session cookie
your browser holds — so that cookie is the one thing to configure. Copy the
value of the cookie named `session` from a logged-in browser, then either:

```sh
export AOC_SESSION=<the value>          # or
echo <the value> > ~/.config/domain/aoc-session
```

`~/.adventofcode.session` is read too, for anyone who already keeps one there
for another tool. The cookie is a login: keep it out of anything you commit,
and expect it to expire after about a month — the site answers an expired one
with a 400, and the editor says so in those words rather than as a status code.

`AOC_CONTACT` is added to the `User-Agent` — an address to reach you at, which
is what [the site's automation guidelines][automation] ask of anything that
fetches on your behalf.

[automation]: https://www.reddit.com/r/adventofcode/wiki/faqs/automation

### What it does not ask twice

The same guidelines are the reason for the shape of the rest of it:

- **Inputs are fetched once, ever.** They cannot change, so the second fetch of
  a day makes no request at all. Everything is cached under
  `domain/aoc/YEAR/DD/` under the user cache directory (`$XDG_CACHE_HOME`, or
  `~/.cache`, on Linux; `~/Library/Caches` on macOS), written `0600` because a puzzle input is a
  personal file.
- **The puzzle page is re-read only when asked** — by `r`, or automatically
  after a correct answer, which is the one moment the page is known to have
  changed.
- **A day is not fetched before it unlocks.** Midnight EST on the day; asking
  earlier is refused here, with the time it opens, rather than at the server.
- **An answer already refused is never sent again**, and the countdown the site
  quotes after a wrong answer is waited out locally rather than walked into.

## The input file

`ctrl+e` opens a browser over every file beside the program — an input is
whatever the puzzle gave you, so it is not filtered to `.domain` — and points
the program's `Cursed Energy:` stage at what you choose, adding one if there is
not one yet.

The binding is written **into the program** rather than held beside it in the
editor. The input file is part of what the program is; a binding that lived
only here would make the program behave differently under `domain run`, which
is the one thing an editor for a language must not do.

### The example and the real input

A puzzle is worked against two files: the small example the description walks
through, and your own input. `alt+t` switches the program between them, by
rewriting the `Cursed Energy:` line as `ctrl+e` does.

With a day fetched (`alt+c`) the two files are the day's. Without one the pair
is found by name, under the conventions people use: `day5.example` beside
`day5.input`, `example.txt` beside `input.txt`. When nothing pairs with the
current input the status line says what an example would be called.

A run of a fetched day's example says, under its output, what the puzzle text
gives as the answer and whether the run agreed:

```
Part 1: 24000

example, part 1: 24000 — as the puzzle says
alt+t switches to your input
```

The expected answer is read out of the description and is a guess — the last
emphasised value in each part — so a disagreement is worth a look at the prose
before it is worth a look at the program.

The pair is also what makes [vow suggestions](#binding-vows) better: a vow is
tried on both files, and the ones true of both are the ones about the puzzle.

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
