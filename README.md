# Domain

A programming language for Advent of Code where **you describe *what* needs to
happen, and the compiler decides *how* to do it optimally.** You name an
algorithm; the compiler treats that as a *request*, not a command, and is free
to substitute a faster implementation that produces the same result.

Domain is deliberately verbose and styled after *Jujutsu Kaisen*: functions are
"Domain Expansions," operations are "Cursed Techniques," assertions are "Binding
Vows." The theme is load-bearing — it maps JJK's power hierarchy onto compiler
stages.

This repository is a **tree-walking interpreter** plus a **Go compiler
backend**, both consuming the same optimized IR. The vocabulary is complete
across both: parsing (`Match Pattern`), dense and sparse grids,
pairs/combinations, sets/maps, higher-order lambda operations (with a `Using:`
that may be an [indented pipeline](docs/expressions.md) rather than an
expression), named dataflow `Channel`s, loops, measured arguments,
user-defined `Shikigami` + a prelude, and a **31-pass optimizer** (algorithm
substitution, fusion, dead-code elimination, expression simplification).
`domain build` compiles that same IR into a standalone, aggressively typed Go
binary — every primitive and all 176 expression builtins have a codegen case,
each pinned by an interpreter-vs-binary oracle test.

**New here? Start with [docs/getting-started.md](docs/getting-started.md)** —
a ground-up tutorial from FizzBuzz to a Game of Life glider, then
[docs/walkthroughs.md](docs/walkthroughs.md), which takes the features one at a
time as whole working programs. The full reference (language, primitives,
expression builtins, CLI, optimizer, compiler backend) lives in
[`docs/`](docs/README.md).

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

Twenty-one ready-to-run programs with inputs and expected outputs live in
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
domain expansion: visualize day1.domain          # step through the run, watching the data change shape
domain expansion: visualize day1.domain --expressions   # …down to what each Using: expression computed
domain expansion: development day1.domain        # write it in a terminal editor that knows the language
domain expansion: documentation                  # serve the docs as a local website (port 4444)
domain expansion: vscode                         # install the VS Code extension carried in the binary
```

The documentation site includes a browser playground compiled to WebAssembly.
`go build ./cmd/domain` alone won't have it — build with `make build` instead
(runs `docs/wasm/build.sh` first); see [docs/wasm/README.md](docs/wasm/README.md).

There is also a terminal editor (`domain expansion: development` — types at the
end of every line and errors in the gutter as you type, completion, a monitored
run you can watch and interrupt, and the step-through visualizer over the
buffer you are editing; see
[docs/development.md](docs/development.md)), an interactive REPL (`domain repl`
— build a pipeline line by line, seeing the value and type after each
statement) and a language server (`domain lsp` — live diagnostics, inlay type
hints after every statement, hover types, go-to-Shikigami (across imported
libraries), quick fixes in any LSP editor); see
[docs/tooling.md](docs/tooling.md).

Choosing an input file in the editor reads its shape and offers the opening
that would take it in — `Shikigami: Digit Grid` for a rectangle of digits, a
`Match Pattern` template inferred from the lines, and so on — ranked, with the
evidence for each.

`domain run --stats` reports per-stage element counts and timings (with
`--verbose` for the steps inside loops and Parts) — the interpreter's numbers,
not the compiled binary's, and the header says so.

`domain fmt` is the formatter — indentation is significant in Domain, so it
fixes the part that actually bites, and `--check` makes it a CI gate. It
never adds or removes a themed keyword (that choice is yours, line by line)
and never rewrites an operation phrase's interior; see
[docs/cli.md](docs/cli.md#domain-fmt).

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
in [`editors/`](editors/README.md). Both grammars are **generated from the
language itself** — the primitives from the registry, all 176 expression
builtins, the keywords — and a test fails if they fall behind it.

The binary carries the VS Code extension and installs it for you:

```sh
domain expansion: vscode                 # into the first editor found
domain expansion: vscode --list-targets  # VS Code, Insiders, Codium, Cursor, remote/WSL, …
```
 Note for maintainers:
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

**The keywords are optional.** They say what *kind* of step a line is, and the
compiler can work that out from the step itself, so the same program can be
written without a single prefix — it resolves to the identical pipeline, down
to the quickselect rewrite:

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

Both spellings mix freely, line by line. Where a bare phrase could mean two
different things Domain refuses to guess and asks for the keyword; and because
a Shikigami is called by its bare name, a Shikigami may no longer be named
after a built-in operation. See
[docs/language.md](docs/language.md#optional-keywords).

## The two layers

1. **The pipeline layer** (themed). An ordered sequence of statements, each
   transforming a single implicit "current value." Statements use themed
   keywords like `Cursed Technique:`.
2. **The expression layer** (plain, *not* themed). Inside lambdas and vows you
   write ordinary expressions: `(a, b) -> a + b = 2020`.

## Keyword taxonomy

Every keyword below is optional except where the row says otherwise — see
[optional keywords](docs/language.md#optional-keywords).

| Keyword | Semantic role | Examples |
|---|---|---|
| `Innate Domain:` | import a Shikigami library | `Innate Domain: aoc` |
| `Cursed Energy:` | input / data source | Read Source |
| `Cursed Technique:` | 1:1 transforms | Split, Map Each, Filter, Match Pattern, Take Item, Transpose, Map Cells, Apply, Unique, Scan, Pairs, Chunk, Take/Drop While, Partition, Iterate, Unfold |
| `Channeled Energy:` | type coercion | Convert To Integers, Convert To Grid |
| `Maximum Technique:` | reductions / aggregation | Sum, Max, Min, Count, Fold, Reduce, Any/All, Find, Sum By, Group By, Select Top K, Intersect/Union, Combine, Zip |
| `Domain Expansion:` | a named algorithm the optimizer may swap — or a foreign block, the one it may not | Quicksort, All Pairs, Combinations, Sliding Reduce, Python |
| `Reverse Cursed Technique:` | inversions | Reverse |
| `Simple Domain:` | control flow | Repeat N, Iterate Until Fixed Point, While |
| `Channel "name":` | named sub-pipeline (dataflow branch) | + `From:` consumers |
| `Part "label":` | labelled output block (two answers, one parse) | + `Reveal:` inside |
| `Shikigami "name" (p: T) : In -> Out` | user-defined operation (inlined) | the prelude is written this way |
| `Consider x As …` / `Consider x Of …` (required) | a local variable for one stage's expressions — `As` a constant or a function, `Of` the current value put through an operation | `Consider total Of Sum` |
| `Cursed Object:` / `Cursed Tool:` | declare a global / change one — a name whose scope is the rest of the program, not one stage | `Cursed Object: total As 0` |
| `Binding Vow:` | debug-time assertion over the current value | Count Equals N, All Values > N |
| `Reveal:` | terminal output sink | stdout |

## What it can do

- **Parsing** via `Match Pattern` typed-hole templates (`"{a:int}-{b:int}"`):
  named holes → Records, positional → tuples/lists.
- **Data model**: Int, Float, Text, Bool, List, Tuple, Record, Map, Set,
  Grid, and Sparse (the unbounded default-valued plane).
- **Higher-order operations** driven by `Using:` lambdas — `Map Each`, `Filter`,
  `Fold`, `Group By`, `Count Matching`, `All Pairs`/`Combinations` — and, where
  a lambda cannot reach, **a `Using:` written as an indented pipeline**: it
  stands in for the lambda at every stage that takes a 1-parameter one, so a
  per-element job can use primitives (a pair search per row of a
  `List<List<Int>>`, a sort key that is itself a reduction).
- **The functional layer around Fold** — `Reduce` (seedless fold, so the
  accumulator can be any type), `Scan` (the running fold), `Unfold` (Fold's
  dual: grow a value into a list) and `Iterate n` (keep the trajectory a
  `Repeat` loop throws away).
- **List shaping** — `Pairs` (each element tupled with the next), `Chunk n`
  (blocks, keeping a short final one), `Take While`/`Drop While` (split at a
  boundary instead of filtering), `Partition` (one pass, both halves).
- **Early-exit reductions** — `Any`/`All` stop at the element that decides the
  answer; `Find`/`Find Index` stop at the first match; `Sum By`/`Product By`
  fold a key lambda without building the mapped list. The optimizer rewrites
  the naive spellings (`Filter` + `Take Item 0`, `Map Each` + `Sum`) into
  them.
- **Expression-layer builtins** inside lambdas — `length`, `item`, `take`,
  `drop`, `reverse`, `concat`, `first`, `last`, `sum`, `min`, `max`,
  `contains`, `get(m, k)`, `at(grid, r, c)` — e.g.
  `(g) -> sum(take(g, 2)) > max(drop(g, 2))`.
- **Local bindings on any stage** — `Consider accum As 3`, `Consider len As (x)
  -> length(x)`, `Consider total Of Sum`. `As` binds a constant or a function
  and never sees the pipeline value; `Of` binds what an operation, a lambda, or
  a whole sub-pipeline makes *of* it. The preposition has to carry that,
  because a 1-parameter lambda already means two things depending on the slot
  it sits in — per element in a `Using:`, once over the current value in a
  measured argument — and a binding has no slot. A constant folds into the
  lambdas that read it and a function is inlined at its call sites, so both are
  gone before either backend runs, and the optimizer still sees the body shapes
  it rewrites.
- **Updating a binding** — `:=` writes to a `consider` local or a stage binding
  and yields what it wrote, and `also` runs expressions after a lambda body for
  their updates alone, discarding their values:
  `(x) -> x + seen also seen := seen + 1` numbers the elements as they go by. A
  write to a stage binding is the one value that carries from one element (or
  one lap of a loop) to the next without a `Fold` accumulator to thread it
  through. The stage that writes gives up its optimizer rewrites in exchange —
  a lambda that updates is not a function of its arguments, and every pass here
  assumes it is.
- **Grids**: build from chars or digits, `Transpose`, `Map Cells`, `Count Cells`,
  neighbor walks.
- **Sparse grids** (`Sparse<T>`): an infinite plane with a default value —
  plot point clouds (`Convert To Sparse Grid`), read anywhere totally
  (`at`), grow in loops (`put`), and densify back to a printable picture
  (`Convert To Grid`). Powers the Game of Life / origami / Minesweeper
  challenges.
- **`Channel`s** for inputs with multiple structurally-different sections, with
  `From:` consumers (`Combine` via lambda, `Difference`).
- **`Part` blocks** — the two-answers-per-input shape. A Part branches from the
  current value like a Channel and labels what its body `Reveal`s, so the parse
  above the Parts happens once and each Part sees the same upstream value.
- **Loops** (`Repeat`/`Iterate Until Fixed Point`/`While`), bounded against
  runaway.
- **Foreign blocks** — `Domain Expansion: Python` (or `Go`, `rask`, `cRust`)
  followed by an indented block of *that language's* source, run as a
  subprocess with the current value on its stdin and its stdout as the next
  stage's value. The block is captured verbatim — its own comment character,
  its own braces, tabs if it wants them — and a declared `: List<Int> -> Int`
  says what crosses the wire. It is the one Domain Expansion the optimizer
  never touches: it names an implementation, not a result. See
  [docs/ref-expansions.md](docs/ref-expansions.md#foreign-block--t---text-or-a-declared-in---out).
- **`Innate Domain`** — import a library of Shikigami (`Innate Domain: aoc`),
  searched beside the program, then `$DOMAIN_PATH`, then `~/.config/domain/lib`.
  Libraries are free: a Shikigami is inlined, so an imported operation gets
  every optimizer rewrite a local one would, and the binary needs the library
  only at build time.
- **`Shikigami`** — name a composition of primitives; the prelude (`Lines`,
  `Blocks`, `Ints`, `Digit Grid`, `Top K Sum`) is itself written in Domain.
  A Shikigami is called by its name alone, so the name may not be one a
  built-in already answers to. Parameters may be `Int`, `Text`, `Float`,
  `Bool`, or a **lambda** (`p: (Int) -> Bool`), which makes a Shikigami
  higher-order; an optional declared signature (`: List<Int> -> Int`) is
  checked at every call site *and* against the body, without stopping the
  inlining that lets optimizer rewrites fire through it.
- **The AoC toolbox** — the classic helper library, natively:
  `Extract Integers` / `Split Fields` / `Merge Ranges` / `Convert To Set` /
  `Find Cells`; grid searches as Domain Expansions (`BFS`, `Dijkstra`,
  `Flood Fill`, `Connected Components`); `Permutations` / `Subsets`; and
  math, point-geometry, and string builtins (`gcd`/`lcm`/`modpow`/`modinv`/
  `solve2x2`, `manhattan`/`neighbors4`/`rotr`, `occurrences`/`repeats`).
  The full map from the canonical Go helper library lives in
  [`docs/aoc-toolbox.md`](docs/aoc-toolbox.md).
- **A 31-pass optimizer** that fires even through Shikigami abstraction (below).

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
  **once**; `Filter` + `Take Item 0` becomes a `Find` that stops at the
  first match instead of collecting every one.
- **Reordering dead code** — double sorts, `Reverse + Reverse`, reorderings
  feeding order-insensitive reductions, redundant `Unique`s.
- **Fusion** — adjacent `Map Each`s and `Filter`s collapse into one pass;
  `Filter + Count` becomes `Count Matching`; a running-sum `Fold` becomes
  `Sum`; `Map Each + Sum`/`Product` becomes `Sum By`/`Product By`, folding
  as it maps; `Zip + Map Each` becomes one pass with no tuple list;
  `Window n + Map Each` becomes the O(n) streaming `Sliding Reduce`.
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
optimizer/  31 rewrite passes: algorithm substitution, fusion, dead code, expression simplification, linear accumulators
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
interpreter; binaries are self-contained (~1.5 MB, stdlib only) — unless the
program contains a [foreign block](docs/ref-expansions.md#foreign-block--t---text-or-a-declared-in---out), which
embeds another language's source but not the runtime that runs it.

The benchmark that matters is against Go rather than against the
interpreter: [`bench/`](bench/README.md) pairs each Domain program in it
with a hand-written Go counterpart answering the same question about the
same input, and requires byte-identical output from both. Every case is
inside
2× of the hand-written Go and five are faster than it — a quickselect the
Go program never asked for, a read sized before the first byte arrives, a
`Match Pattern` scanner with the predicate inlined into the loop.

**Every primitive compiles.** Map/Set values lower to insertion-ordered
generic runtime types so rendered output matches the interpreter exactly;
`Simple Domain` loops thread one mutable variable through their emitted
bodies; Fixed Point convergence and composite `=` share generated structural
equality functions; tuple-shaped `Match Pattern` emits positional structs.
A future primitive that ships without a codegen case fails `domain build`
with a positioned error and keeps working under `domain run`. Nothing is in
that state today: the whole surface — parsing/range/set primitives, the
grid searches, the sparse grid type, and all 176 expression builtins
including the point group — compiles, with oracle tests pinning
interpreter/binary parity (see [`docs/compiler.md`](docs/compiler.md)).
A foreign block compiles too, with its source embedded as a constant and the
same subprocess run at run time; what it costs is the sentence above about
self-contained binaries.

## Design decisions

- Significant indentation, **spaces only** (tabs in indentation are an error) —
  except inside a foreign block, which is another language's source and is
  copied byte for byte, tabs included.
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

There is no recursion: a Shikigami is inlined at its call site, so a
self-referential one is refused (naming the cycle). `Domain Expansion:
Explore` is the iterative search that covers the problems which look
recursive — see [docs/primitives.md](docs/primitives.md).

The combinatorial generators are **unbounded**: Domain used to cap
`Permutations` at 9 elements and `Subsets` at 16, and those ceilings refused
correct programs, so they are gone. Loops (`While`, `Iterate Until Fixed
Point`, `Unfold`) stop at **a billion** iterations, raised from the million
that failed a 40,000,000-step generator — high enough that reaching it means
stuck rather than slow, and identical in the interpreter and a compiled
binary.

## License

[MIT](LICENSE)
