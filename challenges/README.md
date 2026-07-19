# Challenges

Thirteen classic programming challenges — the kind every language gets
measured against — solved in Domain. Each program ships with its input
(`*.input`) and exact expected output (`*.expected`); where a challenge has a
famous published answer (the AoC samples, Kadane on the textbook array,
Collatz of 27), the expected file **is** that answer.

```sh
go build -o domainc ./cmd/domain
./domainc challenges/11_game_of_life.domain
```

`TestChallenges` (in `cmd/domain`) runs every program in both optimizer
modes against its `.expected` file, and `TestCompiledChallengesMatchInterpreter`
(in `codegen`) additionally compiles each one to a binary and requires
byte-identical stdout — so the suite doubles as an end-to-end regression net
over both backends.

| Challenge | Classic source | Output | Exercises |
|---|---|---|---|
| `01_fizzbuzz` | the interview screen | 1..15, Fizz/Buzz'd | Repeat loop as a range generator, nested conditionals, `totext`, `Join` |
| `02_two_sum` | LeetCode #1 / AoC 2020 D1 | `[1721, 299]` | `All Pairs Mode: First` → **hash-set scan rewrite** (`--explain`) |
| `03_max_subarray` | Kadane's algorithm | `6` | `Fold From:` a Channel, list-valued fold state |
| `04_collatz` | the 3n+1 problem | `111` | `While` loop threading `[value, steps]` |
| `05_window_max` | sliding window maximum | `[3, 3, 5, 5, 6, 7]` | `Window` + `max` → **O(n) WindowedReduce rewrite** |
| `06_binary_diagnostics` | AoC 2021 D3 | `198` | `Transpose`, `row`/`sum`, `frombin`, bit complement |
| `07_rotate_image` | LeetCode #48 | rotated matrix | `Transpose` + positional `Map Cells` (mirror columns) |
| `08_flood_fill` | LeetCode #733 | `4` | `Flood Fill`, `Count Cells` |
| `09_islands` | LeetCode #200 | `3` | `Connected Components` (union-find) |
| `10_shortest_path` | AoC 2021 D15 | `40` | `Dijkstra` + `at(target)` → **early-exit search rewrite** |
| `11_game_of_life` | Conway's Life (glider) | the glider, back again | **Sparse grid** on an infinite plane: `Find Cells`, point math, `Count By` weighted-neighbor trick, a whole generation per `Repeat` body |
| `12_origami` | AoC 2021 D13 | a `#` square | **Sparse grid** as plotter: per-point fold reflections, densify to a picture |
| `13_minesweeper` | Minesweeper | the numbers board | **Sparse grid** neighbor counts, `Map Cells` over the plane, `totext` |

The three sparse-grid programs (11–13) are the acceptance tests for the
dedicated `Sparse<T>` type: unbounded coordinates (negative included), the
set-cells-only iteration model, and `Convert To Grid` densification down to
the ASCII picture.

Conventions here match `examples/`: every program names its own input file,
so the interpreter finds the sibling `.input` automatically; compiled
binaries resolve the path against the working directory (or read stdin).
