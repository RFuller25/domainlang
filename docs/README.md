# Domain documentation

Domain is a pipeline language for Advent of Code built on one thesis: **you
describe *what* needs to happen; the compiler decides *how* to do it
optimally.** A named algorithm (`Domain Expansion: Quicksort`) is a request,
not a command — the optimizer may substitute a faster implementation with the
same result, and the Go compiler backend turns the optimized pipeline into a
standalone native binary.

The theme is *Jujutsu Kaisen*: transformations are Cursed Techniques,
reductions are Maximum Techniques, named algorithms are Domain Expansions,
assertions are Binding Vows, user-defined operations are Shikigami. The theme
is load-bearing — each keyword class maps to a semantic role the tooling
relies on.

## Start here

**[getting-started.md](getting-started.md)** — the ground-up tutorial: install,
first program, the two layers, grids, Channels, loops, sparse grids, and
compiling to a binary. Then come back to the reference below.

Runnable material: fifteen annotated programs in
[../examples/](../examples/README.md) and thirteen classic challenges
(FizzBuzz → Game of Life) in [../challenges/](../challenges/README.md),
all golden-tested in both backends.

## Reference

| Document | Covers |
|---|---|
| [language.md](language.md) | Source structure: the two layers, keywords, statements, arguments, Channels, Shikigami, loops, vows |
| [primitives.md](primitives.md) | Every pipeline primitive: signature, arguments, errors, examples |
| [expressions.md](expressions.md) | The expression layer: operators, lambdas, conditionals, typing, and all 92 builtin functions |
| [aoc-toolbox.md](aoc-toolbox.md) | The classic AoC helper library (parsing, grids, searches, math, ranges, combinatorics) mapped onto Domain, item by item |
| [data-model.md](data-model.md) | The value/type model design notes (Int, Float, Text, Bool, List, Tuple, Record, Map, Set, Grid, Sparse) |
| [match-pattern.md](match-pattern.md) | The `Match Pattern` typed-hole template language design notes |
| [cli.md](cli.md) | The `domain` binary: implicit modes, subcommands, every flag, exit codes |
| [diagnostics.md](diagnostics.md) | The error engine: rich diagnostics, "did you mean" suggestions, auto-fix, the linter, and the `domain expansion:` command family |
| [tooling.md](tooling.md) | The REPL (`domain repl`) and the language server (`domain lsp`), with editor wiring |
| [optimizer.md](optimizer.md) | The 30-pass catalog (algorithm substitution, dead code, fusion, expression simplification), `--explain`, and the oracle-testing discipline |
| [compiler.md](compiler.md) | The Go compiler backend: what it emits, its guarantees, and its documented deltas |

## Quick orientation

A program is a top-to-bottom pipeline threading one implicit "current value":

```domain
Cursed Energy: input.txt                  # Text
Cursed Technique: Split Text by "\n\n"    # List<Text>
Cursed Technique: Split Each by "\n"      # List<List<Text>>
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group         # List<Int>
Domain Expansion: Quicksort, Descending   # a *request* —
Maximum Technique: Select Top 3, Sum      # …rewritten to quickselect
Reveal: stdout
```

Run it, or compile it to a native binary:

```sh
domain prog.domain < input.txt      # interpret
domain prog.domain -o prog          # compile (same optimized IR)
domain run prog.domain --explain    # see what the optimizer substituted
```

Installation (including Nix flake usage) is covered in the
[repository README](../README.md).
