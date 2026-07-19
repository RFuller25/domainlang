# Getting started with Domain

Domain is a pipeline language for Advent of Code-style problems, themed
after *Jujutsu Kaisen*, built on one thesis: **you describe *what* needs to
happen; the compiler decides *how* to do it optimally.** This guide takes
you from zero to compiling your own programs. Nothing here requires prior
knowledge of the language.

## 1. Install and run

You need either Go or Nix.

```sh
# With Go (from a clone of the repository):
go build -o domainc ./cmd/domain
./domainc --help

# With Nix, no clone needed:
nix run github:RFuller25/domain -- --help
```

The CLI picks its mode from the arguments:

```sh
domainc prog.domain < input.txt    # bare file → interpret
domainc prog.domain -o prog        # extra argument → compile to ./prog
domainc check prog.domain          # typecheck only, run nothing
domainc repl                       # interactive pipeline builder
```

## 2. Your first program

Create `hello.domain`:

```domain
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Sum
Reveal: stdout
```

Run it:

```sh
printf '3\n4\n5' | ./domainc hello.domain
# 12
```

Read it top to bottom. A Domain program is a **pipeline**: each statement
transforms a single implicit "current value" and hands it to the next line.

| Line | Current value after it |
|---|---|
| `Cursed Energy: stdin` | `Text` — the whole input |
| `Split Text by "\n"` | `List<Text>` — the lines |
| `Convert List to Integers` | `List<Int>` |
| `Sum` | `Int` |
| `Reveal: stdout` | (prints it) |

The themed keywords are not decoration — each names a **semantic role** the
tooling relies on:

| Keyword | Role |
|---|---|
| `Cursed Energy:` | data source (a file name, or `stdin`) |
| `Cursed Technique:` | 1:1 transforms (`Split`, `Map Each`, `Filter`, …) |
| `Channeled Energy:` | type coercions (`Convert To Integers/Grid/Set/…`) |
| `Maximum Technique:` | reductions (`Sum`, `Count`, `Fold`, `Group By`, …) |
| `Domain Expansion:` | a **named algorithm the optimizer may replace** |
| `Reverse Cursed Technique:` | inversions (`Reverse`) |
| `Simple Domain:` | loops (`Repeat N`, `While`, `Iterate Until Fixed Point`) |
| `Channel "name":` | a named side branch of the pipeline |
| `Shikigami:` | call a user-defined (or prelude) operation |
| `Binding Vow:` | a debug assertion over the current value |
| `Reveal:` | print the current value |

Everything is **typechecked before anything runs**: misspell a primitive or
feed `Sum` a `List<Text>` and you get a positioned error, not a crash midway
through your input. Try `./domainc check hello.domain` — it typechecks
without executing, and `domainc expansion: diagnosis` explains errors with
"did you mean" suggestions.

Two syntax rules to know: indentation is significant and **spaces only**;
`#` starts a comment.

## 3. The second layer: expressions and lambdas

Pipelines move data; **lambdas** decide the details. Any primitive that
needs per-element logic takes a `Using:` argument written in a plain
(non-themed) expression language:

```domain
Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Filter
    Using: (n) -> n > 0 and n / 2 * 2 = n
Cursed Technique: Map Each
    Using: (n) -> if n > 100 then 100 else n
Maximum Technique: Sum
Reveal: stdout
```

Things to notice:

- `Shikigami: Ints` calls the **prelude** — a tiny standard library written
  in Domain itself. `Ints` is "split lines, convert to integers". (Also
  there: `Lines`, `Blocks`, `Digit Grid`, `Top K Sum`.)
- **`=` is equality, always.** There is no assignment anywhere in the
  language. `and`/`or` short-circuit.
- `if cond then a else b` is an expression with lazy arms, so
  `if length(xs) = 0 then -1 else first(xs)` is safe.
- There are 61 builtin functions available inside lambdas — list ops
  (`length`, `item`, `take`, `sum`, …), math (`gcd`, `modpow`, …), text
  (`toint`, `totext`, …), bits (`band`, `frombin`, …), points and grids
  (`point`, `manhattan`, `at`, `neighbors4`, …), and sparse grids (`put`,
  `has`, …). The full table with types is in
  [expressions.md](expressions.md).

Lambda arity is fixed by the consuming primitive: 1 for `Map Each`/`Filter`,
2 for `Fold` (`(acc, x) -> …`), k for `Combinations k`.

## 4. Name an algorithm, get a better one

This is the language's signature move. Ask for a sort and the top three:

```domain
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
```

Run it with `--explain`:

```
[explain] Domain rewrote Quicksort (Descending) + Top 3 → Cursed Quickselect. Guaranteed hit.
```

`Domain Expansion` names are **requests, not commands**. The optimizer
recognized sort-then-top-k and substituted an O(n) quickselect that produces
the same answer. The same happens when an `All Pairs` scan has a
sum-to-constant predicate (→ hash-set scan), when `Window` feeds
`max` (→ streaming monotonic deque), when `Dijkstra` feeds `at(target)`
(→ early-exit search), and 20+ more — see [optimizer.md](optimizer.md).
`--no-optimize` runs the naive path; the two must always agree (the test
suite enforces it over thousands of random inputs).

## 5. Grids

AoC lives on grids. `Convert To Grid` builds one from lines of characters
or digits; lambdas over cells do the rest:

```domain
# How many trees (height >= 5)?
Cursed Energy: stdin
Shikigami: Digit Grid
Maximum Technique: Count Cells
    Using: (h) -> h >= 5
Reveal: stdout
```

Positions are 0-based `(row, col)`. `Find Cells` returns matching positions
as **points** — `(Int, Int)` tuples that the point builtins (`prow`,
`padd`, `manhattan`, …) consume. Cell lambdas have a positional 3-parameter
form `(g, r, c)` when the body needs to look around with `at`/`row`/`col`.

Graph algorithms over grids are one-liners under `Domain Expansion`:

```domain
Domain Expansion: BFS from 0 0        # step distances from (0,0)
    Using: (c) -> c = "."             # which cells are walkable
Domain Expansion: Dijkstra from 0 0   # weighted: cells are entry costs
Domain Expansion: Flood Fill from 0 0 # 0/1 mask of the connected region
Domain Expansion: Connected Components
    Using: (c) -> c = "#"             # count the 4-connected groups
```

## 6. Sparse grids: the infinite plane

Dense grids have edges. For point clouds, cellular automata, and plotters,
use **`Sparse<T>`** — an unbounded plane (negative coordinates included)
where every cell holds a **default value** until written:

```domain
# Plot points, then print the picture.
Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: "."
    Mark: "#"
Channeled Energy: Convert To Grid
Reveal: stdout
```

The rules that make it pleasant:

- `at(g, r, c)` is **total** — unset cells read the default, no bounds
  errors ever;
- only written cells are stored, and bounds (`minrow`/`maxrow`/…) track
  them exactly;
- `Convert To Grid` **densifies**: the bounding box becomes a dense grid
  (translated to start at `(0, 0)`, default-filled) — that's how you print
  the picture;
- `Map Cells` / `Count Cells` / `Find Cells` visit the set cells in sorted
  row-major order;
- in the expression layer, `sparse(default)` makes an empty plane and
  `put(g, r, c, v)` is a functional update — so loops can grow a grid.

The show-off piece is `challenges/11_game_of_life.domain`: a glider running
4 generations on the infinite plane, one generation per loop body, using
`Count By` over neighbor points. Also see `12_origami.domain` (fold dots,
read the letter) and `13_minesweeper.domain` (neighbor counts).

## 7. Channels, Shikigami, loops, vows

**Channels** branch the pipeline when an input has two structurally
different sections, or when you need two analyses of one value; `From:`
consumers (`Combine`, `Zip`, `Difference`, `Fold From:`) join them back:

```domain
Channel "regions":
    Domain Expansion: Connected Components
        Using: (c) -> c = "#"
Channel "span":
    Cursed Technique: Find Cells
        Using: (c) -> c = "#"
    Cursed Technique: Apply
        Using: (ps) -> manhattan(first(ps), last(ps))
Maximum Technique: Combine
    From: regions, span
    Using: (r, s) -> r * 100 + s
```

**Shikigami** are user-defined operations — a name for a composition of
primitives, with typed parameters, inlined at the call site (so the
optimizer sees through them):

```domain
Shikigami "Doubled" (k: Int)
    Cursed Technique: Map Each
        Using: (n) -> n * k

Cursed Energy: stdin
Shikigami: Ints
Shikigami: Doubled
    k: 2
Reveal: stdout
```

**Loops** thread the current value through a pipeline body:

```domain
Simple Domain: Repeat 3                      # fixed count
Simple Domain: While                         # predicate-driven (bounded)
    Using: (v) -> v > 100
Simple Domain: Iterate Until Fixed Point     # until the value stops changing
```

**Binding Vows** are debug assertions — they never change output, only
catch when reality diverges from expectation (`Binding Vow: All Values > 0`).
`--release` sheds them: skipped by `run`, compiled out entirely by `build`.

## 8. Compile it

Every program that runs also compiles to a standalone native binary — same
optimized IR, no interpreter inside:

```sh
./domainc build prog.domain -o prog     # or: ./domainc prog.domain -o prog
./prog < input.txt
./domainc build prog.domain --emit-go - # inspect the generated Go
./domainc build prog.domain --run       # compile-and-run in one step
```

The generated Go is fully typed (no `[]any`), lambdas are inlined into
their consuming loops, and the interpreter serves as the correctness
oracle — the test suite requires byte-identical stdout from both backends
for every example, challenge, and anchor program. Expect roughly 7× the
interpreter's speed. One behavioral difference to know: a compiled binary
resolves `Cursed Energy:` file paths against the *working directory* (like
any CLI tool), while the interpreter resolves them relative to the program
file; both fall back to stdin when the file is absent.

## 9. Where to go next

- **[../examples/](../examples/README.md)** — fifteen small programs, each
  showing off one feature, with inputs and expected outputs.
- **[../challenges/](../challenges/README.md)** — thirteen classics
  (FizzBuzz → Game of Life) solved in Domain; great for seeing idioms
  composed.
- **[language.md](language.md)** — the precise source-structure rules.
- **[primitives.md](primitives.md)** and
  **[expressions.md](expressions.md)** — the complete vocabulary.
- **[aoc-toolbox.md](aoc-toolbox.md)** — "how do I do X from my AoC helper
  library?" mapped item by item.
- **[tooling.md](tooling.md)** — the REPL and the language server;
  [diagnostics.md](diagnostics.md) — the `expansion:` command family
  (diagnosis, lint, fix, optimize).
