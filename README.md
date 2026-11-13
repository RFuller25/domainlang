# Domain

Domain is a small programming language for puzzles of the Advent of Code kind:
read some awkward input, reshape it, compute an answer.

A program is a pipeline, read top to bottom. There is one value in flight, and
each line transforms it. This one solves AoC 2022 Day 1 Part 2 — sum the
calories carried by the top three elves:

```domain
Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
```

Raw text in → split into groups → split each group into lines → convert to
integers → sum each group → sort → take the top three and add them → print.

Two ideas shape the language:

- **You describe *what* should happen; the compiler decides *how*.** Naming an
  algorithm (`Domain Expansion: Quicksort`) is a request for its result, not
  an order to run it. Here the optimizer sees a sort followed by "top 3" and
  runs a quickselect instead — same answer, less work. `--explain` shows every
  such substitution, and `--no-optimize` runs the program exactly as written.
- **The keywords are themed after *Jujutsu Kaisen*,** and each names the role
  a line plays: `Cursed Energy` is input, `Cursed Technique` transforms,
  `Maximum Technique` reduces, `Domain Expansion` names an algorithm, `Reveal`
  prints. They are also optional — the program above can be written with no
  keywords at all:

```domain
input.txt
Split Text by "\n\n"
Split Each by "\n"
Convert Each List to Integers
Sum Each Group
Quicksort, Descending
Select Top 3, Sum
stdout
```

**New here? Start with [docs/getting-started.md](docs/getting-started.md)**, a
ground-up tutorial, then [docs/walkthroughs.md](docs/walkthroughs.md), which
goes through the features again as whole working programs. The full reference
is in [`docs/`](docs/README.md).

## Try it

From a clone, with Go installed:

```sh
go run ./cmd/domain run testdata/day1.domain < testdata/day1_input.txt
# 45000

go run ./cmd/domain run testdata/day1.domain --explain < testdata/day1_input.txt
# [explain] Domain rewrote Quicksort (Descending) + Top 3 → Cursed Quickselect. Guaranteed hit.
# 45000

# Compile to a standalone binary:
go run ./cmd/domain build testdata/day1.domain -o day1
./day1 < testdata/day1_input.txt
# 45000

# Or play something. Arrows move; q leaves.
go run ./cmd/domain run examples/games/snake.domain
```

There is plenty to run and read:

- Twenty-one annotated programs, each showing one piece of the language, in
  [`examples/`](examples/README.md).
- Thirteen classic challenges — FizzBuzz, Two Sum, Kadane, Conway's Game of
  Life, Minesweeper — in [`challenges/`](challenges/README.md).
- Six whole terminal games — snake, tetris, an Oregon Trail, a tower defence,
  and two that talk to a server — in
  [`examples/games/`](examples/games/README.md).

## Install

```sh
go build -o domain ./cmd/domain     # the binary
make build                          # the same, plus the browser playground for the docs site
```

`domain build` needs the Go toolchain on the machine; everything else does
not.

### Install with Nix

The repo is a flake. `nix run` it directly, or add it to a system:

```sh
nix run github:RFuller25/domainlang -- day1.domain < input.txt   # interpret
nix run github:RFuller25/domainlang -- build day1.domain -o day1 # compile
nix profile install github:RFuller25/domainlang                  # put `domain` on PATH
nix develop                                                   # hacking shell (go, gopls)
```

In a NixOS / Home Manager flake, take the package from the input:

```nix
{
  inputs.domain.url = "github:RFuller25/domainlang";

  # e.g. in a NixOS module:
  environment.systemPackages = [ inputs.domain.packages.${pkgs.system}.default ];
  # or via the overlay:
  nixpkgs.overlays = [ inputs.domain.overlays.default ];
}
```

Two packages come from the flake. `default` (also `packages.<system>.domain`)
is wrapped so the compiler's `go build` step always finds a Go toolchain, even
on machines without Go installed; the price is a closure of about 254 MiB.
`packages.<system>.domain-lite` is the same binary without the wrapper, about
60 MiB: it interprets, and runs the REPL, editor, `fmt`, `doc` and the language
server, but `domain build` and anything else that shells out to `go` will not
work there. Choose `domain-lite` if you never compile.

```sh
nix run github:RFuller25/domainlang#domain-lite -- day1.domain < input.txt
```

The flake builds from a filtered source, and `.gitattributes` leaves tests,
`testdata`, `*.expected`, `selfhost/`, `bench/` and `docs/superpowers/` out of
GitHub's archive downloads — which is what `nix run github:…` and flake inputs
fetch. A download therefore builds and runs `domain` but has no test suite
and no benchmarks; clone the repository to run `go test ./...`.

## Using it

```sh
domain day1.domain < input.txt     # run a program
domain build day1.domain -o day1   # compile it to ./day1
domain check day1.domain           # type-check it without running
domain --help                      # everything else
```

The same binary carries the tools around the language:

| Command | What it is |
|---|---|
| `domain repl` | build a pipeline line by line, seeing the value and its type after each statement |
| `domain fmt` | the formatter; `--check` for CI |
| `domain doc Split Each` | look up an operation or builtin from the shell |
| `domain run --watch` | run again on every save |
| `domain run --stats` | element counts and timings for each line |
| `domain expansion: lint` / `fix` | every error with a suggested fix, plus warnings; apply the unambiguous fixes |
| `domain expansion: explain` | every line in plain English, and what the optimizer made of it |
| `domain expansion: visualize` | step through a run, watching the data change shape |
| `domain expansion: development` | a terminal editor that knows the language: types and errors as you type, completion, a stepper, and Advent of Code puzzles fetched alongside |
| `domain expansion: documentation` | serve the docs as a local website, with a playground |
| `domain lsp` | a language server: diagnostics, type hints, hover, go-to-definition, quick fixes |
| `domain expansion: vscode` | install the VS Code extension the binary carries |
| `domain expansion: oracle` / `bench` / `mahoraga` | check that interpreter and compiler agree; benchmark; tune a binary for one input |

[docs/cli.md](docs/cli.md) documents every command and flag,
[docs/diagnostics.md](docs/diagnostics.md) the error messages and fixes, and
[docs/tooling.md](docs/tooling.md) the REPL and editor setup. Syntax
highlighting for VS Code and Neovim/Vim is in [`editors/`](editors/README.md).

## The language in brief

A program has **two layers**. The pipeline layer is the themed statements
above. The expression layer is the plain language inside a lambda, where a
per-element detail is decided: `(a, b) -> a + b = 2020`.

| Keyword | Role | Examples |
|---|---|---|
| `Innate Domain:` | declares what kind of program this is (keyword required) | `Innate Domain: Game Dev` |
| `Inherited Technique:` | imports a library of Shikigami (keyword required) | `Inherited Technique: aoc` |
| `Cursed Energy:` | the input | `input.txt`, `stdin` |
| `Cursed Technique:` | transforms | Split, Map Each, Filter, Match Pattern, Take Item, Transpose, Map Cells, Apply, Unique, Chunk, Partition |
| `Channeled Energy:` | type conversions | Convert To Integers, Convert To Grid |
| `Maximum Technique:` | reductions | Sum, Max, Min, Count, Fold, Any/All, Find, Group By, Select Top K, Combine |
| `Domain Expansion:` | a named algorithm the optimizer may replace | Quicksort, All Pairs, Combinations, BFS, Dijkstra, Explore |
| `Reverse Cursed Technique:` | inversions | Reverse |
| `Simple Domain:` | loops | Repeat N, While, Iterate Until Fixed Point, For x in y, For Each x In |
| `Channel "name":` | a named side branch, read later with `From:` | two sections of one input |
| `Part "label":` | a labelled answer | Part 1 and Part 2 from one parse |
| `Shikigami "name" (p: T) : In -> Out` | a user-defined operation | the prelude: `Lines`, `Blocks`, `Ints`, `Digit Grid`, `Top K Sum` |
| `Cursed Object:` / `Cursed Tool:` | declares a program-wide value / changes it | `Cursed Object: total As 0` |
| `Binding Vow:` | an assertion about the current value | Count Equals N, All Values > N |
| `Reveal:` | prints the current value | `stdout` |

What you get:

- **Parsing** with `Match Pattern` templates — `"{a:int}-{b:int}"` turns a
  line into a typed record.
- **Types** checked before the program runs: Int, Float, Text, Bool, List,
  Tuple, Record, Map, Set, Grid, Sparse (an unbounded plane) and Graph.
- **Lambdas** with 208 builtins for lists, text, maths, bits, points and
  grids — and, where a lambda is not enough, an indented pipeline in its
  place.
- **Grid and graph searches** as one-liners: BFS, Dijkstra, flood fill,
  connected components, shortest path, and `Explore` for searching any space
  of states.
- **Structure** when one straight pipeline is not enough: Channels, Parts,
  loops, user-defined Shikigami, libraries, local bindings and globals.
- **A 51-pass optimizer** that replaces what you asked for with something
  faster that gives the same result.
- **A compiler** to a standalone native binary whose output matches the
  interpreter's byte for byte.
- **Terminal games** as a second kind of program: a world, the events that
  change it, and a frame drawn from it.
- **Foreign blocks** — a stage written in Python, Go or another language, run
  as a subprocess.

Each of these is covered in [`docs/`](docs/README.md).

## Repository layout

```
token/      token kinds + source positions
lexer/      source → tokens (significant indentation, escapes, comments)
ast/        parsed tree (pipeline layer + expression layer)
parser/     tokens → AST (line/block parser + Pratt expression parser)
pattern/    Match Pattern typed-hole templates (parse + type + regex lowering)
ir/         typed pipeline graph, value/type model, runtime collections
typecheck/  static expression typer (lambda output-type inference)
eval/       dynamic expression evaluator (lambda bodies, runtime field access)
builtins/   registry of expression builtins: typing rule, evaluator and emitter in one place
prims/      primitive vocabulary + resolver/typechecker + Shikigami + prelude
optimizer/  51 numbered passes: algorithm substitution, fusion, dead code, expression simplification, idioms, linear accumulators
mahoraga/   input-adaptive codegen tunings: measure, search a catalogue, verify/replay
interp/     tree-walking evaluator
codegen/    Go compiler backend: optimized IR → typed Go source → `go build`
cmd/domain/ CLI (bare file → interpret, any extra arg is a usage error naming run/build; run, build, check, fmt, doc, --help), REPL, visualizer, editor
```

Beside the compiler pipeline sit the packages that serve the tools around it:

```
diag/       diagnostics engine: positioned errors, fix-and-continue, lint, performance hints, auto-fixes
format/     canonical source rendering behind `domain fmt` and the editor's reformatting
lsp/        language-server knowledge (hover, completion, definitions, inlay hints) and the stdio server
shape/      reads an input file and suggests the statements that would take it in
game/       host for `Innate Domain: Game Dev` programs: play, replay, record
langs/      how to run a program written in another language (foreign blocks)
runner/     measuring a run: allocations, per-node counts, process timing
aoc/        Advent of Code client for the editor: puzzle text, your input, answer checks
docs/       this documentation, its site renderer and the drift tests that keep it honest
editors/    VS Code and Neovim support carried inside the binary
bench/      Domain vs hand-written Go head-to-head benchmarks
selfhost/   research notes: how far a Domain compiler written in Domain can go
examples/ challenges/ testdata/   programs with checked answers (golden tests)
flake.nix   Nix packaging: packages/apps/devShell/overlay, `nix run`-able
```

Pipeline: **lex → parse → lower/typecheck → optimize → interpret** (or
**→ codegen → `go build`** for `domain build`). Resolution catches type
mismatches before interpretation; the interpreter recovers from internal
panics so users only ever see positioned errors.

## How it runs

**lex → parse → type-check → optimize → interpret**, or **→ generate Go →
`go build`** for `domain build`. Both the interpreter and the compiler run the
optimized program, so a rewrite speeds up both, and the test suite requires
the two to print identical output for every program in the repository. See
[docs/optimizer.md](docs/optimizer.md) and
[docs/compiler.md](docs/compiler.md).

[`bench/`](bench/README.md) measures compiled Domain against hand-written Go:
forty-four paired cases, each required to be within 2× of the Go version.

## Tests

```sh
go test ./...
```

This runs every example, challenge and game in both the interpreter and the
compiler, with and without optimization, and requires them to agree. Each
optimizer rewrite is also tested against the unoptimized program over
thousands of random inputs. The documentation is tested too: every code block
that claims to run is run, and the counts, flags and commands the pages quote
are checked against the code (`go test ./docs`).

## Known limits

- Integers are 64-bit; there are no big integers.
- There is no recursion and no recursive data. `Domain Expansion: Explore`
  covers most problems that look recursive.
- `While`, `Iterate Until Fixed Point` and `Unfold` stop with an error after a
  billion iterations.

More in [docs/aoc-gaps.md](docs/aoc-gaps.md).

## License

[MIT](LICENSE)
