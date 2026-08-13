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

**[walkthroughs.md](walkthroughs.md)** — the same features as whole programs
rather than as definitions: what flows between the stages, one working program
per idea, every output taken from a real run.

Runnable material: twenty annotated programs in
[../examples/](../examples/README.md) and thirteen classic challenges
(FizzBuzz → Game of Life) in [../challenges/](../challenges/README.md),
all golden-tested in both backends.

Reading this on the documentation site (`domain expansion: documentation`)?
Every one of those programs is on it with a **Run** button, and so is a
playground — the language compiled to WebAssembly, running in your own tab.
**Explain** shows the optimizer answering a named-algorithm request with a
different algorithm, which is the one claim in these documents worth watching
happen rather than taking on trust. The playground is a separate build step
(`./docs/wasm/build.sh`); without it the site works exactly as before, minus
the buttons.

## Reference

| Document | Covers |
|---|---|
| [walkthroughs.md](walkthroughs.md) | Whole programs, start to finish: bindings, all four loops, Channels, Parts, pipeline bodies, Shikigami, the optimizer |
| [language.md](language.md) | Source structure: the two layers, keywords, statements, arguments, Channels, Shikigami, loops, vows |
| [primitives.md](primitives.md) | Every pipeline primitive: signature, arguments, errors, two runnable examples each — an index over seven pages, one per keyword class |
| [expressions.md](expressions.md) | The expression layer: operators, lambdas, conditionals, `consider`, `:=`, stage bindings. The 161 builtin functions are on six pages linked from it |
| [aoc-toolbox.md](aoc-toolbox.md) | The classic AoC helper library (parsing, grids, searches, math, ranges, combinatorics) mapped onto Domain, item by item |
| [data-model.md](data-model.md) | The value and type model (Int, Float, Text, Bool, List, Tuple, Record, Map, Set, Grid, Sparse): representation, construction, equality, rendering |
| [match-pattern.md](match-pattern.md) | The `Match Pattern` typed-hole template language: syntax, output shapes, lowering, and failure modes |
| [cli.md](cli.md) | The `domain` binary: implicit modes, subcommands, every flag, exit codes |
| [diagnostics.md](diagnostics.md) | The error engine: rich diagnostics, "did you mean" suggestions, auto-fix, the linter, and the `domain expansion:` command family |
| [tooling.md](tooling.md) | The REPL (`domain repl`) and the language server (`domain lsp`), with editor wiring |
| [development.md](development.md) | `domain expansion: development`, the terminal editor: types and errors on screen, completion, running, the stepper, and the opening it offers for an input file |
| [optimizer.md](optimizer.md) | The 31-pass catalog (algorithm substitution, dead code, fusion, expression simplification, linear accumulators), `--explain`, and the oracle-testing discipline |
| [compiler.md](compiler.md) | The Go compiler backend: what it emits, its guarantees, and its documented deltas |
| [aoc-gaps.md](aoc-gaps.md) | The other side of the toolbox: the AoC problems Domain still cannot express (or cannot express fast enough), each with a measurement and the smallest change that would close it |

**Every example in the reference runs.** A block marked ```domain run carries
the input it is given and the output it must print; both backends execute it
in both optimizer modes and the printed answer is diffed, so nothing on these
pages can drift from what the language does. 210 of them at present, at least
two for every primitive, language construct and builtin group.

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
