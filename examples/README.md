# Examples

Twenty small, self-contained programs, each with its input (`*.input`) and
its exact expected output (`*.expected`). (For classic programming
challenges — FizzBuzz, Two Sum, Game of Life — see
[../challenges/](../challenges/README.md); for a guided tour, start at
[../docs/getting-started.md](../docs/getting-started.md).) Every example names its own input
file, so from the repository root this is all it takes:

```sh
go build -o domainc ./cmd/domain      # or: nix build / nix run
./domainc examples/01_top_calories.domain
```

(Bare file → interpreter. The interpreter resolves the input path relative
to the program file, so it finds the sibling `.input` automatically.)

To compile an example instead, add an output path — then run the binary
*from the `examples/` directory* or pipe the input in, since compiled
binaries resolve input paths against the working directory:

```sh
./domainc examples/01_top_calories.domain -o top3
./top3 < examples/01_top_calories.input
```

`TestExamples` (in `cmd/domain`) runs every example in both optimizer modes
and checks it against its `.expected` file, so these programs can't silently
rot.

Every example also compiles with `domain build` — including `11`–`15`,
whose AoC-toolbox primitives (searches, ranges, points) lower to
self-contained Go helpers (see [docs/compiler.md](../docs/compiler.md)).

| Example | Output | Shows off |
|---|---|---|
| `01_top_calories` | `32000` | groups → sums → **Quicksort + Top 3 fused to quickselect** (run with `--explain`) |
| `02_pair_sum` | `2419` | **All Pairs → hash-set scan** rewrite; the `Ints` prelude Shikigami |
| `03_overlapping_shifts` | `4` | `Match Pattern` named int holes → Records; `and` predicate |
| `04_grade_points` | `552` | a `word` hole (regex fallback path); field access + arithmetic in a lambda |
| `05_two_sections` | `4290` | **Channels** branching one input into two sub-pipelines + `Combine` |
| `06_tall_trees` | `9` | the `Digit Grid` prelude Shikigami; `Count Cells` over a grid |
| `07_loops` | `100` | `Repeat` and `While` loops; a Binding Vow (try `--release`) |
| `08_builtins` | `[122, 1100]` | expression builtins: `first`/`last`/`max`/`min`/`sum`/`take`; list rendering |
| `09_common_letters` | `{d, o, m, a, n}` | `Split Each by ""` → **Intersect**; set rendering in first-seen order |
| `10_shikigami` | `231` | defining a **parameterized Shikigami**; the parameter substitutes into a lambda |
| `11_merged_ranges` | `[[1, 8], [10, 12], [20, 20]]` | `Extract Integers` mining messy lines; `Merge Ranges` coalescing intervals |
| `12_maze_bfs` | `16` | **`BFS from 0 0`** flooding step distances over walkable cells; `at` reads the exit |
| `13_gear_ratios` | `22` | `Split Fields` (whitespace); `lcm`/`gcd` number-theory builtins |
| `14_regions` | `507` | **`Connected Components`** + `Find Cells` → points → `manhattan`, branched via Channels |
| `15_team_picks` | `3` | **`Subsets`** (the power set) filtered with `length`/`contains` |
| `16_no_prefixes` | `15000` | **prefix inference** — example 01 with the themed keywords left out |
| `17_two_parts` | `Part 1: 24000` / `Part 2: 45000` | **`Part` blocks** — two answers from one parse |
| `18_innate_domain` | `28` | **`Innate Domain`** — a Shikigami library, inlined and still fused |
| `19_row_pairs` | `9` | **a `Using:` written as a pipeline** — a whole sub-pipeline where a lambda would go |
| `20_stage_locals` | `3` | **`Consider … As` / `… Of`** — stage-local values, including one a lambda could not reach |

Things to try with any of them:

```sh
./domainc run examples/02_pair_sum.domain --explain     # see the rewrite fire
./domainc run examples/07_loops.domain --no-optimize    # the naive oracle path
./domainc examples/06_tall_trees.domain -o trees        # a ~1.5 MB static binary
./domainc examples/03_overlapping_shifts.domain --emit-go -   # read the generated Go
```

Break an input on purpose (a non-integer line, a ragged grid row, a negative
number for `07`'s vow) to see the positioned error reporting.
