# Domain documentation

Domain is a small programming language for puzzles of the Advent of Code
kind: read some awkward input, reshape it, compute an answer.

A program is a **pipeline**. It is read top to bottom, there is one value in
flight, and each line transforms it:

```domain
Cursed Energy: input.txt                  # Text — the whole file
Cursed Technique: Split Text by "\n\n"    # List<Text> — the blank-line-separated groups
Cursed Technique: Split Each by "\n"      # List<List<Text>> — each group's lines
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group         # List<Int> — one total per group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum      # Int — the three largest, added up
Reveal: stdout
```

Two things make it unusual.

**You say what you want, and the compiler decides how.** The program above
asks for a sort and then the top three. Domain notices that sorting everything
to keep three is wasted work and runs a faster selection instead. The answer
is the same; `--explain` tells you what it did.

**The keywords are themed after *Jujutsu Kaisen*.** Each keyword names the
role a line plays — `Cursed Energy` is input, `Cursed Technique` transforms,
`Maximum Technique` reduces to an answer, `Domain Expansion` names an
algorithm, `Reveal` prints. You do not need to know the series, and the
keywords are optional once you know the operations.

```sh
domain prog.domain < input.txt      # run it
domain build prog.domain -o prog    # compile it to a native binary
domain run prog.domain --explain    # run it, and show what the optimizer changed
```

Installation is covered in the [repository README](../README.md).

## Where to start

1. **[getting-started.md](getting-started.md)** — the tutorial. Install, a
   first program, and each feature of the language in turn. Start here.
2. **[walkthroughs.md](walkthroughs.md)** — the same features again as whole
   programs, showing what flows from line to line.
3. **The runnable programs** — twenty-one annotated programs in
   [../examples/](../examples/README.md) and thirteen classic challenges
   (FizzBuzz → Game of Life) in [../challenges/](../challenges/README.md).

Then use the reference below to look things up.

On the documentation site (`domain expansion: documentation`) every example
has a **Run** button, and there is a playground that runs Domain in your
browser. The playground is built separately (`./docs/wasm/build.sh`); without
it the site works, minus the buttons.

## Reference

**The language**

| Page | Covers |
|---|---|
| [language.md](language.md) | How a program is put together: source format, keywords, statements and arguments, Channels, Parts, Shikigami, loops, globals, vows |
| [expressions.md](expressions.md) | What goes inside a lambda: operators, conditionals, `consider`, `:=`, stage bindings, pipeline bodies. The 208 builtin functions are on pages linked from it |
| [primitives.md](primitives.md) | Every operation: what it takes, what it gives, its arguments and errors, with two runnable examples each — an index over one page per keyword |
| [data-model.md](data-model.md) | The types (Int, Float, Text, Bool, List, Tuple, Record, Map, Set, Grid, Sparse, Graph): how to get one, read one, and how each prints |
| [match-pattern.md](match-pattern.md) | `Match Pattern`, the template language for parsing lines of input |
| [aoc-toolbox.md](aoc-toolbox.md) | "How do I do the thing my AoC helper library does?" — parsing, grids, searches, maths, ranges, combinatorics, item by item |
| [aoc-gaps.md](aoc-gaps.md) | Known limits: what Domain cannot express, and what to do instead |
| [scopes.md](scopes.md) | `Innate Domain`: declaring what kind of program this is, and the two kinds there are |
| [game-dev.md](game-dev.md) | Writing a terminal game, built up a step at a time. Its building blocks are in [ref-game-parts.md](ref-game-parts.md) |

**The tools**

| Page | Covers |
|---|---|
| [cli.md](cli.md) | The `domain` command: every subcommand and flag, exit codes |
| [diagnostics.md](diagnostics.md) | Error messages, "did you mean" suggestions, automatic fixes, the linter |
| [tooling.md](tooling.md) | The REPL (`domain repl`) and editor support (`domain lsp`) |
| [development.md](development.md) | `domain expansion: development`, a terminal editor with types and errors on screen, completion, and a stepper |

**Under the hood** — not needed to write programs

| Page | Covers |
|---|---|
| [optimizer.md](optimizer.md) | The 51-pass catalog of rewrites, the conditions on each, and how they are tested |
| [compiler.md](compiler.md) | How `domain build` turns a program into Go, and what it guarantees |
| [mahoraga.md](mahoraga.md) | `domain expansion: mahoraga`: tuning one program for one input |
| [../bench/README.md](../bench/README.md) | Compiled Domain measured against hand-written Go |

**Every example in these pages runs.** A block marked ```domain run carries
the input it is given and the output it must print, and the test suite runs
each one — interpreted and compiled — and compares the output. There are
346 runnable examples.
