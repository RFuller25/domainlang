# Domain

A programming language for Advent of Code where **you describe *what* needs to
happen, and the compiler decides *how* to do it optimally.** You name an
algorithm; the compiler treats that as a *request*, not a command, and is free
to substitute a faster implementation that produces the same result.

Domain is deliberately verbose and styled after *Jujutsu Kaisen*: functions are
"Domain Expansions," operations are "Cursed Techniques," assertions are "Binding
Vows." The theme is load-bearing — it maps JJK's power hierarchy onto compiler
stages.

This repository is a **tree-walking interpreter** (v0.1 → v0.2) plus the
**v0.3 Go compiler backend (MVP)**. It has the full v0.2 vocabulary — parsing
(`Match Pattern`), dense and sparse grids, pairs/combinations, sets/maps,
higher-order lambda operations, named dataflow `Channel`s, loops,
user-defined `Shikigami` + a prelude, and a **26-pass optimizer** (algorithm
substitution, fusion, dead-code elimination, expression simplification).
`domain build` compiles the same optimized IR the interpreter runs into a
standalone, aggressively typed Go binary.

**New here? Start with [docs/getting-started.md](docs/getting-started.md)** —
a ground-up tutorial from FizzBuzz to a Game of Life glider. The full
reference (language, primitives, expression builtins, CLI, optimizer,
compiler backend) lives in [`docs/`](docs/README.md).

## Try it

```sh
go run ./cmd/domain run testdata/day1.domain < testdata/day1_input.txt
# 45000

go run ./cmd/domain run testdata/day1.domain --explain < testdata/day1_input.txt
# [explain] Domain rewrote Quicksort (Descending) + Top 3 → Cursed Quickselect. Guaranteed hit.
# 45000

# Compile to a standalone optimized binary (requires the Go toolchain):
go run ./cmd/domain build testdata/day1.domain -o day1
./day1 < testdata/day1_input.txt
# 45000

# Inspect the generated Go instead:
go run ./cmd/domain build testdata/day1.domain --emit-go -
```

Fifteen ready-to-run programs with inputs and expected outputs live in
[`examples/`](examples/README.md) — each one shows off a different piece of
the language — and thirteen classic programming challenges (FizzBuzz, Two
Sum, Kadane, Conway's Game of Life, Minesweeper, …) live in
[`challenges/`](challenges/README.md). Tests keep every one of them green
in both backends.

The CLI picks its mode from the arguments: a bare program file interprets it,
any extra argument compiles it, and `domain --help` lists everything:

```sh
domain day1.domain < input.txt   # interpreter
domain day1.domain -o day1       # compiler → ./day1
```

The diagnostics engine ships as the `expansion:` command family — rich
positioned errors with "did you mean" suggestions, auto-fix, a linter, and
source-level optimization rewrites (see
[docs/diagnostics.md](docs/diagnostics.md)):

```sh
domain expansion: diagnosis day1.domain          # every error + how to fix it
domain expansion: fix day1.domain                # apply the unambiguous fixes (.bak kept)
domain expansion: maximum compile day1.domain < input.txt   # fix → lint → optimize → compile → run
domain expansion: documentation                  # serve the docs as a local website (port 4444)
```

There is also an interactive REPL (`domain repl` — build a pipeline line by
line, seeing the value and type after each statement) and a language server
(`domain lsp` — live diagnostics, hover types, go-to-Shikigami, quick fixes
in any LSP editor); see [docs/tooling.md](docs/tooling.md).

## Install with Nix

The repo is a flake. `nix run` it directly, or add it to a system:

```sh
nix run github:RFuller25/domain -- day1.domain < input.txt   # interpret
nix run github:RFuller25/domain -- day1.domain -o day1       # compile
nix profile install github:RFuller25/domain                  # put `domain` on PATH
nix develop                                                   # hacking shell (go, gopls)
```

In a NixOS / Home Manager flake, take the package from the input:

```nix
{
  inputs.domain.url = "github:RFuller25/domain";

  # e.g. in a NixOS module:
  environment.systemPackages = [ inputs.domain.packages.${pkgs.system}.default ];
  # or via the overlay:
  nixpkgs.overlays = [ inputs.domain.overlays.default ];
}
```

The packaged binary is wrapped so the compiler's `go build` step always finds
a Go toolchain, even on machines without Go installed.

## Editor support

Syntax highlighting for VS Code (TextMate grammar) and Neovim/Vim (runtime
plugin, also exported by the flake as `packages.<system>.domain-nvim`) lives
in [`editors/`](editors/README.md). Note for maintainers:
after the first `nix build` / `nix flake lock`, commit the generated
`flake.lock` so consumers are pinned to the same nixpkgs.

The target program (AoC 2022 Day 1 Part 2 — sum the calories of the top 3 elves):

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

Read top to bottom as a pipeline: raw text in → split into groups → split each
group into lines → convert to integers → sum each group → sort → take top 3 and
sum → print.

## The two layers

1. **The pipeline layer** (themed). An ordered sequence of statements, each
   transforming a single implicit "current value." Statements use themed
   keywords like `Cursed Technique:`.
2. **The expression layer** (plain, *not* themed). Inside lambdas and vows you
   write ordinary expressions: `(a, b) -> a + b = 2020`.

## Keyword taxonomy

| Keyword | Semantic role | Examples |
|---|---|---|
| `Cursed Energy:` | input / data source | Read Source |
| `Cursed Technique:` | 1:1 transforms | Split, Map Each, Filter, Match Pattern, Take Item, Transpose, Map Cells, Apply, Unique |
| `Channeled Energy:` | type coercion | Convert To Integers, Convert To Grid |
| `Maximum Technique:` | reductions / aggregation | Sum, Max, Min, Count, Fold, Group By, Select Top K, Intersect/Union, Combine |
| `Domain Expansion:` | a named algorithm the optimizer may swap | Quicksort, All Pairs, Combinations |
| `Reverse Cursed Technique:` | inversions | Reverse |
| `Simple Domain:` | control flow | Repeat N, Iterate Until Fixed Point, While |
| `Channel "name":` | named sub-pipeline (dataflow branch) | + `From:` consumers |
| `Shikigami "name" (p: T)` | user-defined operation (inlined) | the prelude is written this way |
| `Binding Vow:` | debug-time assertion over the current value | Count Equals N, All Values > N |
| `Reveal:` | terminal output sink | stdout |

## What it can do

- **Parsing** via `Match Pattern` typed-hole templates (`"{a:int}-{b:int}"`):
  named holes → Records, positional → tuples/lists.
- **Data model**: Int, Float, Text, Bool, List, Tuple, Record, Map, Set,
  Grid, and Sparse (the unbounded default-valued plane).
- **Higher-order operations** driven by `Using:` lambdas — `Map Each`, `Filter`,
  `Fold`, `Group By`, `Count Matching`, `All Pairs`/`Combinations`.
- **Expression-layer builtins** inside lambdas — `length`, `item`, `take`,
  `drop`, `reverse`, `concat`, `first`, `last`, `sum`, `min`, `max`,
  `contains`, `get(m, k)`, `at(grid, r, c)` — e.g.
  `(g) -> sum(take(g, 2)) > max(drop(g, 2))`.
- **Grids**: build from chars or digits, `Transpose`, `Map Cells`, `Count Cells`,
  neighbor walks.
- **Sparse grids** (`Sparse<T>`): an infinite plane with a default value —
  plot point clouds (`Convert To Sparse Grid`), read anywhere totally
  (`at`), grow in loops (`put`), and densify back to a printable picture
  (`Convert To Grid`). Powers the Game of Life / origami / Minesweeper
  challenges.
- **`Channel`s** for inputs with multiple structurally-different sections, with
  `From:` consumers (`Combine` via lambda, `Difference`).
- **Loops** (`Repeat`/`Iterate Until Fixed Point`/`While`), bounded against
  runaway.
- **`Shikigami`** — name a composition of primitives; the prelude (`Lines`,
  `Blocks`, `Ints`, `Digit Grid`, `Top K Sum`) is itself written in Domain.
- **The AoC toolbox** — the classic helper library, natively:
  `Extract Integers` / `Split Fields` / `Merge Ranges` / `Convert To Set` /
  `Find Cells`; grid searches as Domain Expansions (`BFS`, `Dijkstra`,
  `Flood Fill`, `Connected Components`); `Permutations` / `Subsets`; and
  math, point-geometry, and string builtins (`gcd`/`lcm`/`modpow`/`modinv`/
  `solve2x2`, `manhattan`/`neighbors4`/`rotr`, `occurrences`/`repeats`).
  The full map from the canonical Go helper library lives in
  [`docs/aoc-toolbox.md`](docs/aoc-toolbox.md).
- **A 26-pass optimizer** that fires even through Shikigami abstraction (below).

Worked anchor programs live in [`testdata/`](testdata): AoC 2022 Days 1/4/5/8
and AoC 2020 Day 1 (parts 1 & 2).

## The signature optimization

When you write `Domain Expansion: Quicksort` followed by `Select Top 3`, the
optimizer recognizes the pattern and rewrites it into a **partial selection**
(quickselect) that never fully sorts — O(n) instead of O(n log n). The named
algorithm was a request; the compiler honored the *result*, not the literal
method. Run with `--explain` to see it; run with `--no-optimize` to use the
naive path (which is also the correctness oracle the optimizer is
property-tested against).

The other passes follow the same idea, in four families (see
[`docs/optimizer.md`](docs/optimizer.md) for the full catalog):

- **Algorithm substitution** — `All Pairs` with a sum- or
  difference-to-constant predicate drops from an O(n²) pair scan to an
  **O(n) hash-set complement scan**; `Combinations 3` summing to a constant
  (AoC 2020 Day 1 Part 2) drops from O(n³) to **O(n²)**; `Sort` +
  `Take Item k` becomes a quickselect of the kth order statistic; a linear
  `Map Each` feeding `Max`/`Min` reduces first and applies the lambda
  **once**.
- **Reordering dead code** — double sorts, `Reverse + Reverse`, reorderings
  feeding order-insensitive reductions, redundant `Unique`s.
- **Fusion** — adjacent `Map Each`s and `Filter`s collapse into one pass;
  `Filter + Count` becomes `Count Matching`; a running-sum `Fold` becomes
  `Sum`.
- **Expression simplification** — constant folding, algebraic identities,
  and boolean short-circuits inside lambda bodies, cascading into the
  structural passes (an always-false predicate eliminates its whole scan).

Rewrites cascade until a fixpoint: `Quicksort + Reverse + Select Top 3`
first flips into one descending sort, which then fuses into a quickselect.

## Binding Vows

A vow is a predicate over the current pipeline value. It **never changes the
program's output** — it only catches when reality diverges from expectation,
throwing on violation and reporting the vow, the stage, and the offending value.

```domain
Binding Vow: Count Equals 200
Binding Vow: All Values > 0
```

Vows are debug-time: pass `--release` to shed them for speed — `domain run
--release` skips them, and `domain build --release` compiles them out of the
binary entirely.

## Architecture

```
token/      token kinds + source positions
lexer/      source → tokens (significant indentation, escapes, comments)
ast/        parsed tree (pipeline layer + expression layer)
parser/     tokens → AST (line/block parser + Pratt expression parser)
pattern/    Match Pattern typed-hole templates (parse + type + regex lowering)
ir/         typed pipeline graph, value/type model, runtime collections
typecheck/  static expression typer (lambda output-type inference)
eval/       dynamic expression evaluator (lambda bodies, runtime field access)
prims/      primitive vocabulary + resolver/typechecker + Shikigami + prelude
optimizer/  26 rewrite passes: algorithm substitution, fusion, dead code, expression simplification
interp/     tree-walking evaluator
codegen/    Go compiler backend: optimized IR → typed Go source → `go build`
cmd/domain/ CLI (bare file → interpret, extra args → compile; run/build, --help)
flake.nix   Nix packaging: packages/apps/devShell/overlay, `nix run`-able
```

Pipeline: **lex → parse → lower/typecheck → optimize → interpret** (or
**→ codegen → `go build`** for `domain build`). Resolution catches type
mismatches before interpretation; the interpreter recovers from internal
panics so users only ever see positioned errors.

## The compiler backend

`domain build` consumes the pipeline **after** the optimizer, so algorithm
substitution is already done — the emitted program contains the quickselect
and the hash-set scan, not the requested quicksort or pair loop. The generated
Go is aggressively concrete:

- values are unboxed end to end (`int64`, `[]int64`, generated structs for
  Records, a tiny generic grid) — no `[]any`, no interfaces;
- `Using:` lambdas compile to plain Go expressions inlined into their
  consuming loops — no closures, no per-element evaluator walks;
- all-int `Match Pattern` templates compile to hand-rolled string scanners
  (word/text holes fall back to one precompiled regexp);
- `All Pairs`/`Combinations k` unroll into `k` nested loops at compile time.

The interpreter is the correctness oracle: `codegen`'s tests compile every
anchor program in both modes and require byte-identical stdout. On a
1M-line AoC 2022 Day 4 input the compiled binary is ~7× faster than the
interpreter; binaries are self-contained (~1.5 MB, stdlib only).

**Every v0.2 primitive compiles.** Map/Set values lower to insertion-ordered
generic runtime types so rendered output matches the interpreter exactly;
`Simple Domain` loops thread one mutable variable through their emitted
bodies; Fixed Point convergence and composite `=` share generated structural
equality functions; tuple-shaped `Match Pattern` emits positional structs.
A future primitive that ships without a codegen case fails `domain build`
with a positioned error and keeps working under `domain run`. Nothing is in
that state today: the whole surface — parsing/range/set primitives, the
grid searches, the sparse grid type, and all 61 expression builtins
including the point group — compiles, with oracle tests pinning
interpreter/binary parity (see [`docs/compiler.md`](docs/compiler.md)).

## Design decisions

- Significant indentation, **spaces only** (tabs in indentation are an error).
- `=` is equality; `and`/`or` are the boolean connectives (no assignment exists).
- `#` begins a comment to end of line.
- Double-quoted strings interpret standard escapes (`\n \t \\ \"`).
- One pipeline statement per line; indented children form sub-blocks.

## Tests

```sh
go test ./...
```

Includes property tests that compare each optimization against its naive oracle
over thousands of random inputs, plus a golden harness running every anchor
program in both optimized and `--no-optimize` modes (the two must agree).

## Known limitations

Arbitrary-precision integers are deferred — everything runs on `int64`,
which covers every anchor and challenge program (see
[docs/data-model.md](docs/data-model.md)).

## License

[MIT](LICENSE)
