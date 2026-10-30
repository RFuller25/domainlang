# Domain vs hand-written Go

Every program in [`testdata/`](testdata) exists twice: `<case>.domain` and
`<case>.go`, answering the same question about the same bytes. The harness
compiles the Domain program through the real front end and `codegen`, builds
the hand-written Go with the **same** toolchain flags, checks the two agree
byte for byte, and times them against each other.

The question these numbers answer: **does `domain build` output stay within
2× of what a competent Go programmer writes by hand?** On all thirty-seven
cases it does, twenty-four of them faster than the hand-written Go.

## Running them

```sh
go test ./bench                          # parity: the two programs must agree
go test ./bench -bench . -run XXX        # the head-to-head benchmarks
DOMAIN_BENCH=1 go test ./bench -run TestSpeedRatio -v   # the 2× gate, with a table
```

`TestParity` is the precondition for every number below and runs in a normal
`go test ./...` (at 1/1000th the input size). `TestSpeedRatio` is opt-in: it
runs every program at full scale, five times each, alternating the two sides
so drift lands on both.

## What each case measures

| Case | The Domain feature under test |
|---|---|
| `read_length` | the floor — read stdin, count characters, print |
| `pairs_increase` | `Pairs`, each element tupled with the next (v0.5 list shaping) |
| `scan_mod` | `Scan` (the running fold) and the `%` Euclidean modulo operator |
| `sliding_max` | **measured arguments** — a `Size:` lambda over the current list, streamed by `Sliding Reduce` |
| `pipeline_body` | **pipeline bodies** — a `Map Each` whose `Using:` is an indented pipeline, over `Sum By` |
| `text_builtins` | the v0.5 text builtins — `startswith`/`indexof`/`slice`/`upper` inside a lambda |
| `fold_map_dp` | **linear accumulators** — a `Fold` that builds a Map with `insert`/`getor`, one write at a time |
| `fold_grid_writes` | **linear accumulators** — `Fold From:` a channel, one `setat` per step, cloned once on entry |
| `count_by_entries` | the Map vocabulary — `Count By`, `Convert To Entries`, tuple `item()` |
| `partition_parts` | `Partition`, the early-exit `Find Index`, and `Part` blocks |
| `topk_sum` | algorithm substitution — a requested `Quicksort` + `Select Top K` becomes a quickselect |
| `dijkstra_grid` | a grid search as a named algorithm — `Dijkstra` over a digit grid |
| `match_pattern` | all-int `Match Pattern` templates, compiled to a hand-rolled scanner |
| `toposort_words` | `Topological Sort` over an edge list parsed by **word holes** |
| `combinations3` | algorithm substitution — `Combinations 3` summing to a constant, O(n³) → O(n²) |
| `sort_by_key` | `Sort By` with a key lambda, fused with `Take Item 0` into a selection |
| `merge_ranges` | `Merge Ranges` over the tuple list a positional `Match Pattern` produces |
| `group_map_values` | `Group By` + `Map Values` — the two-line grouped aggregation |
| `set_intersect` | the set vocabulary — `Split Each by ""` feeding `Intersect` |
| `connected_components` | `Connected Components` over a dense grid (union-find) |
| `grid_bfs` | `BFS` over a grid, then `Count Cells` over the distances it produced |
| `sparse_life` | the **Sparse plane** — eight generations of Life over `Find Cells` + `Count By` |
| `explore_states` | `Explore` — breadth-first search over an implicit graph |
| `loop_repeat` | a `Simple Domain` loop: five million laps threading one `Int` |
| `join_output` | the output path — a per-line transform and a `Join` rendering megabytes |
| `channels_zip` | **Channels** and a channel consumer — two branches from one value, zipped |
| `shikigami_calls` | **Shikigami** inlining, including a higher-order one taking a lambda |
| `vows_hot` | **Binding Vows** left in — what a debug build costs over `--release` |
| `grid_transform` | grid geometry — `Transpose`, `Rotate`, `Flip`, positional `Map Cells` |
| `float_sum` | the **Float** path — parsing, a non-Int accumulation, Float rendering |
| `fold_tuple` | a **measured `Seed:`** widening `Fold` to a composite accumulator |
| `iterate_unfold` | the generators — `Iterate` keeps the trajectory, `Unfold` grows one back |
| `while_halve` | a `While` loop whose predicate is a reduction over the value it guards |
| `fixed_point` | `Iterate Until Fixed Point` — structural equality over the whole value |
| `list_shaping` | `Chunk`, `Take While`, `Unique` and `Reverse`, one after another |
| `for_loop` | a `For` loop binding an ambient parameter into the body's lambdas |
| `math_builtins` | the number-theory builtins — `gcd`, `isqrt`, `modpow` in a hot lambda |

## Results

Best of five alternating runs each, Go 1.25, linux/amd64. Lower is better;
**ratio is Domain ÷ Go**.

| Case | Domain | Go | Ratio |
|---|---:|---:|---:|
| `read_length` | 10.5 ms | 73.6 ms | **0.14×** |
| `pairs_increase` | 49.7 ms | 58.4 ms | **0.85×** |
| `scan_mod` | 65.4 ms | 64.7 ms | 1.01× |
| `sliding_max` | 73.1 ms | 246.5 ms | **0.30×** |
| `pipeline_body` | 63.5 ms | 76.7 ms | **0.83×** |
| `text_builtins` | 66.0 ms | 65.5 ms | 1.01× |
| `fold_map_dp` | 112.3 ms | 103.6 ms | 1.08× |
| `fold_grid_writes` | 44.4 ms | 42.9 ms | 1.03× |
| `count_by_entries` | 66.9 ms | 65.0 ms | 1.03× |
| `partition_parts` | 62.1 ms | 61.7 ms | 1.01× |
| `topk_sum` | 70.6 ms | 426.0 ms | **0.17×** |
| `dijkstra_grid` | 82.9 ms | 117.0 ms | **0.71×** |
| `match_pattern` | 41.9 ms | 88.0 ms | **0.48×** |
| `toposort_words` | 188.3 ms | 200.4 ms | **0.94×** |
| `combinations3` | 899.2 ms | 892.2 ms | 1.01× |
| `sort_by_key` | 264.7 ms | 651.1 ms | **0.41×** |
| `merge_ranges` | 442.0 ms | 393.5 ms | 1.12× |
| `group_map_values` | 65.7 ms | 81.8 ms | **0.80×** |
| `set_intersect` | 16.5 ms | 139.8 ms | **0.12×** |
| `connected_components` | 23.4 ms | 24.5 ms | **0.95×** |
| `grid_bfs` | 73.2 ms | 70.4 ms | 1.04× |
| `sparse_life` | 149.1 ms | 85.9 ms | 1.73× |
| `explore_states` | 567.5 ms | 612.1 ms | **0.93×** |
| `loop_repeat` | 25.4 ms | 25.0 ms | 1.02× |
| `join_output` | 81.2 ms | 88.2 ms | **0.92×** |
| `channels_zip` | 94.6 ms | 317.2 ms | **0.30×** |
| `shikigami_calls` | 53.0 ms | 63.8 ms | **0.83×** |
| `vows_hot` | 53.1 ms | 193.2 ms | **0.27×** |
| `grid_transform` | 79.2 ms | 80.8 ms | **0.98×** |
| `float_sum` | 103.6 ms | 111.5 ms | **0.93×** |
| `fold_tuple` | 48.2 ms | 63.2 ms | **0.76×** |
| `iterate_unfold` | 26.8 ms | 23.5 ms | 1.14× |
| `while_halve` | 114.5 ms | 145.5 ms | **0.79×** |
| `fixed_point` | 96.5 ms | 162.1 ms | **0.59×** |
| `list_shaping` | 69.0 ms | 235.3 ms | **0.29×** |
| `for_loop` | 117.1 ms | 233.8 ms | **0.50×** |
| `math_builtins` | 90.5 ms | 85.8 ms | 1.06× |

All thirty-seven are inside the 2× target and twenty-four are faster than
the hand-written Go. The worst is `sparse_life` at 1.73×; the history of how it
got there from 4.50× is [below](#the-case-that-was-over-target-sparse_life).

Where Domain wins it is usually not a codegen trick but a decision the
compiler is in a position to make and a hand-written program usually is not:

- **`topk_sum`** — the program asks for a full descending sort and Domain
  substitutes a quickselect, so it never sorts the tail it is about to throw
  away. The Go side does what the source says, with `slices.Sort` (pdqsort,
  the fastest sort in the standard library).
- **`sliding_max`, `read_length`** — the generated reader sizes its
  allocation from the input's own length before reading a byte, so the parse
  fills a list that never grows. The idiomatic Go (`var xs []int64` + append,
  `io.ReadAll` + `string(data)`) reallocates its way up and pays the GC for
  it: on `sliding_max`, timing the Go program with the deque removed shows
  that most of its run is the append growth, not the sliding window.
- **`match_pattern`** — an all-int template compiles to a hand-rolled scanner
  with the predicate inlined into the counting loop.

## Methodology

- **Both sides are subprocesses**, so both pay process startup, and both read
  the input as a **redirected regular file** (`./prog < input.txt`) — the way
  these programs are actually run. This matters more than it looks: over a
  pipe neither side can size its read up front, which costs a whole-input
  reader like Domain's ~3× on the read alone (`pairs_increase`: 348 ms piped
  vs 139 ms redirected, at the time of measuring). Timing the redirect keeps
  the comparison about the compute.
- **Same algorithm on both sides**, except where the case is *about* the
  algorithm (`topk_sum` — and there the Domain source asks for the sort too).
  The point is to measure code generation, not to let Domain win a race the
  Go program was never entered in.
- **Best of N**, not the mean: scheduler noise and page-cache misses only
  ever add time, so the fastest observed run is closest to the cost of the
  work itself.
- **Same build flags** on both sides (`-trimpath -ldflags "-s -w"`,
  `CGO_ENABLED=0`) — the Go reference is built by `codegen.BuildBinary`, the
  same function that builds a Domain binary.
- **Deterministic inputs**: the generators in `inputs_test.go` produce the
  same bytes for a given size, so a number measured today is comparable with
  one measured next month, and parity checks the very shape the benchmark
  times.

## What the first measurement found, and what fixed it

The first run had six cases over the 2× target, the worst at 6.5×. Three
causes, established by hand-editing the emitted Go one change at a time and
re-timing (ratios below are against the same hand-written Go):

**1. Reading over a pipe defeated the sized read.** `dmReadSource` asks
`os.Stdin.Stat()` for a size and `Grow`s once; a pipe has no size, so it grew
a 12 MB buffer the hard way. This was the harness's fault, not the
compiler's — fixed by redirecting a file, as a user would. It is still worth
knowing that `cat input | ./prog` costs a Domain binary far more than
`./prog < input`.

**2. Intermediates built by `append` from an empty slice.** `Pairs` emitted
`v := []Tup1{}` and grew it two million times; `Partition` did the same for
both halves. Sizing them up front is a one-line change per emitter and was
the single biggest win in the set:

| | as emitted | preallocated | hand-written Go |
|---|---:|---:|---:|
| `pairs_increase` | 2.27× | **1.01×** | 1.00× |
| `partition_parts` | 1.97× | **1.20×** | 1.00× |

Both are now in `codegen` (`emitPairs`, `emitPartition`). `Partition`
reserves the full length for *both* halves: a cap that guesses the split pays
a full reallocation the moment the data is lopsided, which is the normal case
for a predicate worth writing.

**3. Rune-indexed text builtins allocated per call.** `slice(s, 0, 3)` inside
a lambda ran `[]rune(s)` and then `string(rs[l:h])` — two allocations per
line, a million lines. Since text positions are defined in runes, the fix is
an ASCII check (one pass, no allocation) that turns the slice into a
substring of the original:

| | as emitted | ASCII fast path | + fusion (below) |
|---|---:|---:|---:|
| `count_by_entries` | 2.05× | **1.39×** | 1.06× |
| `text_builtins` | 2.01× | **1.53×** | 1.19× |

Now in `codegen/runtime.go` (`dmASCII`, used by `dmSliceText` and
`dmCharAt`).

A fourth candidate, **raising `GOGC` in generated `main`**, looked like a
1.3–1.6× win in the first round and evaporated once the read was sized: the
allocation it was hiding was the unsized read buffer, not the program's own
data. Not worth doing.

## Round two: the searches, the plane, and the templates

The second batch of cases — graph and grid searches, the Sparse plane, the
set and Map vocabulary, the loop and output paths — put four more programs
over the target. The same method (hand-edit the emitted Go, one change at a
time, re-time) found five allocation bugs and one missed fast path. All six
are fixed:

**The direction table was rebuilt for every cell a search visited.**
`dmSearchDirs` returned a fresh `[][2]int64` composite literal, which escapes,
so `BFS`, `Flood Fill` and `Dijkstra` each allocated 128 bytes per dequeued
cell. It is now a package-level array the function slices. Worth **2.06× →
1.58×** on `grid_bfs` on its own.

**BFS queues were resliced from the front.** `queue = queue[1:]` walks the
backing array forward, so every append past the original capacity reallocates
and copies. Popping by index instead (and sizing the queue from the mask) is
the same fix the hand-written Go already had.

**The grid cell mask reserved nothing.** `make([]bool, 0, rows*cols)` was
emitted before `cols` was known — it was still `0` — so the "hint" reserved
zero and a 2.25M-cell mask grew an element at a time. `dmCellHint` computes
it from the first row's length, which the rectangularity check guarantees.

**Union-find carried two `[]int` the size of the grid.** On a 1500×1500 grid
that is 36 MB of working set; `[]int32` halves it, and a grid with more than
2³¹ cells could not be read in the first place.

**The sparse plane's iteration order used `sort.Slice`.** Its reflective
swapper costs more than the comparison it is ordering; `slices.SortFunc` is a
drop-in with identical results. `Find Cells` also grew its point list from
empty — it is now sized from the cell count.

**`Count By` probed the map three times per element** (`put(k, vals[k]+1)` is
a read, a membership test and a store) where **one** does: a compound
assignment on a missing key starts from the zero value, and the map's length
before and after says whether the key was new. Counting is the hottest thing
anyone does to a Map, and the `dmBump` helper it now goes through took
`count_by_entries` from 1.26× to 1.06× and `sparse_life` from 3.53× to 2.78×.

**Positional word templates fell back to the regexp engine.** The hand-rolled
scanner already handled `{word}` holes — the rule that makes it provably
equivalent to the regex is that the literal after the hole starts with
whitespace — but only for *named* templates, where each hole lands in a
record field. A positional all-word template like `"{word} -> {word}"` is a
`List<Text>`, which is just as well-typed at an index, so it takes the
scanner now too: **3.08× → 2.02×** on `toposort_words`.

**Topological Sort indexed its working structures with maps.** It numbers
every node, and then kept `adj` and `indeg` as `map[int][]int` and
`map[int]int` — hashing an integer that is already a dense index. Numbering
all nodes in a first pass (successors need not be keys) lets both be slices;
with the adjacency build also down to two map probes per edge, the case
finishes at **1.76×**.

Together these took the four failing cases to 1.76×, 1.77×, 1.53× and
3.53×.

### The case that was over target: `sparse_life`

Eight generations of Life on the Sparse plane was the one case outside the 2×
target through rounds one to three (4.50×, then 2.60×), and carried an explicit
budget in the case table for it. Round four closed it, and the reason it had
been over is worth keeping because it was a stack of small things rather than
one big one.

A pipeline stage produces a *value*, so each lap of the loop rebuilds every
collection it touches: `Find Cells` materializes the live points, the fused
`Map Each`+`Flatten`+`Count By` builds an insertion-ordered score Map,
`Convert To Sparse Grid` copies that Map into a plane, `Find Cells` walks the
plane back out into points, and `Convert To Sparse Grid` builds the next
plane from those. The Go program keeps two plain maps and swaps them.

Three changes, measured one at a time (ratio against the same hand-written Go):

| | ratio |
|---|---:|
| as left by round three | 2.60–3.20× |
| `dmMap` keeps its values in a slice beside the keys, so walking a map does not hash | 2.86× |
| the plane keys cells by a packed `int64` instead of a two-field struct | 2.63× |
| `Count By` → plane → `Find Cells` → marked plane fused into one walk of the map | 1.7× |

The fusion is the one the table above predicted (2.28× by hand-editing); it was
worth more than predicted because the two earlier changes had already made each
remaining map operation cheaper. It is deliberately narrow — the predicate must
be pure and total, because the fused walk visits cells in the map's insertion
order where the unfused one visits them sorted, and a predicate that could fail
would report a different cell first — and the sort it removes feeds a set,
which has no order to lose.

The remaining 1.7× is the data model: a `Map` that preserves insertion order
carries a keys slice beside its index, and a `Sparse` plane tracks its bounding
box on every `put`. Both are promises the language makes — rendering order and
`Convert To Grid` — and neither is free.

## Round three: what the toolchain could not give us

A survey of alternative Go compilers and build flags came back almost empty,
which is itself worth recording so nobody repeats it. **gccgo** cannot
compile the generated code at all — its front end still has no generics, so
every program with a Map, Set or Grid is a syntax error, and where it does
compile it is 1.0–2.35× slower. **TinyGo** compiles all of them correctly and
LLVM's codegen is genuinely better on tight loops (−32% on `pipeline_body`
with its GC removed), but its own GC and maps give it all back — 1.06–3.07×
slower overall — and it takes 3.8 s per build against 60 ms warm for `gc`.
**gollvm** is unmaintained. On flags: PGO is noise on a flat single-file
`main` with nothing to devirtualize, `-l=4` is noise, `GOEXPERIMENT` has
nothing left to turn on, newer toolchains are within ±3%, and `GOMAXPROCS=1`
is a 12–23% *regression* because the GC loses its parallel mark workers.
`GOAMD64=v3` (1–5%) and `-gcflags=all=-B` (2–9%, and negative on one case)
both work but change the portability and safety contract of a build artifact,
so neither is a default.

The wins were all in the emitted source. Two more landed here:

**Parsing an already-split field re-trimmed it.** `Convert To Integers` calls
`dmParseInt`, which starts with `strings.TrimSpace` — pure overhead on a
field that a `Split` just produced. `dmParseIntSeg` is the same function with
the trim moved into its fallback path, so substituting it is unconditionally
safe (a segment that *does* need trimming still parses identically) and
**6.4% faster on `pipeline_body`**, A/B'd against the previous compiler.

**The segment walk kept a bounds check.** `for i := 0; i <= len(s); i++ { if
i == len(s) || s[i] == sep }` reads `s[i]` under a condition the prover
cannot combine with the loop bound; `-d=ssa/check_bce/debug=1` reports an
`IsInBounds` on that line. Keeping the index strictly inside the string and
emitting the final segment after the loop removes it. Honest result: **the
elimination is real and the speedup is not** — ±1%, inside the noise, because
the loop is memory-bound and the removed compare predicts perfectly. It is
kept because it also collapsed four copy-pasted loop emitters into one
helper, not because it made anything faster.

## Round four: the twelve paths nothing had exercised

Channels, Shikigami inlining, Binding Vows, grid geometry, Floats, a
composite `Fold` seed, `Iterate`/`Unfold`, `While`, `Iterate Until Fixed
Point`, `For`, the list-shaping battery and the number-theory builtins had no
coverage at all. Eleven of the twelve came in inside the target on the first
measurement — including two the language could plausibly have been bad at:

- **`vows_hot` (0.23×)** — a debug build with `All Values` and `Holds` vows
  left in still beats hand-written Go, because the vow compiles to the same
  loop the assertion would have been written as. Leaving assertions on costs
  a pass over the list, not a data-structure change.
- **`grid_transform` (0.92×)** and **`fold_tuple` (0.65×)** — the grid
  geometry family and a tuple accumulator both lower to flat `int64` work
  with no boxing, which is what the compiler backend claims and this is the
  first measurement of it.

The twelfth, `shikigami_calls`, came in at **2.25×** — and the cause was not
the Shikigami. Inlining worked exactly as advertised: two `Map Each` calls
through two separate Shikigami definitions fused into a single expression,
`((((e*3)+1)*2)+1)`. What cost the 2× was the `Filter` after them, which
built its output by appending to an empty slice.

**Go grows a large slice by a quarter at a time, not by doubling.** From
empty to two million elements that is ~65 reallocations and roughly five
times the final length in copying — which is why this one line was the whole
gap:

| `shikigami_calls` | ratio |
|---|---:|
| as emitted, `Filter` from an empty slice | 2.14× |
| `Filter` output preallocated | **1.20×** |
| `Filter` fused into `Sum` (no filtered list) | 0.98× |
| + `Map Each` fused in too (nothing materialized) | 0.83× |
| hand-written Go | 1.00× |

`Filter` and `Unique` now size their output to the input, like `Partition`'s
halves already did; `Flatten` sizes its output exactly, by summing the group
lengths in one cheap pass over the outer list; and `Find Cells` over a dense
grid sizes to the cell count. `Group By` also still built its buckets with
`put(k, append(vals[k], e))` — three map probes per element — and now uses
the same two-probe `dmAppend` the `Topological Sort` adjacency build got:
`group_map_values` 2.03× → 1.68×.

The remaining `Filter`+`Sum` fusion is the same story as every other case
above 1×, and is folded into the list below.

## What is still on the table: fusion

Every remaining case above 1× is above it for one reason — the generated
program **materializes a list that the hand-written program never builds**.
`Split Text by "\n"` produces a real `[]string` of a million headers before
the consumer sees an element; `Split Each` + `Convert Each List to Integers`
builds a whole `[][]string` that exists only to be parsed into `[][]int64`.

Hand-editing that materialization out of the emitted Go (nothing else
changed) says what a streaming/fusion pass would be worth:

| Case | today | with the fusion | hand-written Go |
|---|---:|---:|---:|
| `pipeline_body` | 1.32× | **0.96×** (no `[][]string`) → **0.54×** (rows never materialized) | 1.00× |
| `count_by_entries` | 1.32× | **0.96×** | 1.00× |
| `pairs_increase` | 0.96× | **0.80×** (`Pairs` fused into `Count Matching`) → **0.65×** (split fused too) | 1.00× |
| `partition_parts` | 1.20× | **0.97×** (`Partition` + `Take Item 0`) | 1.00× |
| `text_builtins` | 1.35× | **1.18×** | 1.00× |

Three passes, in the order their payoff justifies:

1. **Fuse `Split Each` + `Convert Each List to …`** — the `[][]string` is
   pure waste; parse each field straight out of the line. Worth ~30% on
   `pipeline_body` on its own, and it needs no new IR concept.
2. **Stream a `Split` into the element-wise stage that consumes it** —
   `Map Each`, `Filter`, `Count Matching`, `Count By`, `Fold`, a pipeline
   body: all of them want one element at a time, and the line list only
   exists because the stages are compiled independently. This is the
   general form of (1) and the biggest lever in the set.
3. **Fuse the shaping primitives into their consumer** — `Pairs` +
   `Count Matching`, `Partition` + `Take Item k`. Cheap and local: the tuple
   or the two halves need never exist.

None of these change what a program means; they change what the compiler
materializes on the way. The same theme runs through the Sparse plane case
above: the gap that is left, everywhere it is left, is a value the generated
program builds and the hand-written one never does.

## The tight-loop residual: measured, and not where it was expected

The gaps above are all *materialization* — a value the generated program builds
and the hand-written one never does. A tight loop over a state tuple has no such
value, and it is still slower than hand-written Go, so it was worth asking where
that goes. Two plausible answers turn out to be wrong, which is worth writing
down so they are not re-derived.

The program is the array-walk shape: a `While` threading `(List, Int, Int, Int)`,
every field bound to a name before the write so the in-place rewrite applies,
3,000,000 steps over 1.5M cells. Measured one process at a time on an idle box,
`min` of 7.

| | | |
|---|---:|---|
| Domain, as emitted | 73.8 ms | 1.40× |
| Domain, guarded index helpers replaced by `xs[i]` | 64.6 ms | 1.23× |
| hand-written Go | 52.6 ms | 1.00× |

**It is not per-lap tuple allocation.** The obvious hypothesis — the loop rebuilds
a 4-tuple every lap, so keep the fields in locals and rebuild only on exit — is
optimizing something that already costs nothing. `go build -gcflags=-m` on the
emitted source reports no `Tup` escaping to the heap at all: Go's escape analysis
already keeps the whole state in registers and stack. A scalar-replacement pass
would have nothing to collect.

**It is not the `consider` lowering either.** Each `consider` becomes a nested
`func() T { … }()` capturing the state, and the array walk nests five of them per
lap, which reads like an obvious per-lap cost. Hand-flattening them into
sequential `var` declarations in one body — nothing else changed — measured
75.4 ms against 73.8 ms, i.e. no difference outside noise. Go inlines them.
(Flattening is still worth doing for the reason in the ergonomics notes: a large
`consider` chain can exhaust the Go compiler. It is not a speed argument.)

**What does cost is the guarded index helpers.** `dmItem` and `dmSetAtIn` are
calls carrying their own `i < 0 || i >= len(xs)` test so the failure is Domain's
message rather than a Go panic, and that explicit test is also what stops Go
eliminating its own bounds check. Replacing the two calls with direct indexing —
which changes the diagnostic on an out-of-range index, so it is a measurement
rather than a patch — is worth 1.14× on this loop and closes roughly 40% of the
distance to hand-written Go.

So the lever on tight loops is bounds-check elimination, not allocation.

### What was done about it, and what was not

Rewriting the check as one unsigned comparison — `uint64(i) >= uint64(len(xs))`,
where a negative index converts to a very large unsigned value and fails the same
test — is sound, local, and landed. It is worth about 1.6%, which is the useful
half of the finding: **the cost is having a branch at all, not how it is
written.** Both remaining routes to removing it were rejected on their merits.

*Proving the index in range* is the honest version, and on the shape above it
means proving that the loop's length field still equals the length of the loop's
list field, on every lap, across a state tuple both are threaded through. That is
an invariant analysis, and the failure mode of getting it wrong is not a slow
program or a wrong answer but an out-of-bounds write. Thirteen percent on
indexing-bound loops does not buy that risk, and the fusion work above is a
larger lever on more programs.

*Letting Go's own bounds check panic and translating it at the top level* costs
nothing in the fast path and was rejected for a different reason: a recovered
runtime panic cannot say which builtin failed, so `item: index 99 out of range
(length 3)` becomes a generic Go message, and interpreter/binary parity on error
output is a rule this repo enforces with differential tests.

The measurements are recorded here so the next attempt starts from them rather
than from the tuple-allocation hypothesis.

## Round four: storage, fusion and what a mode actually needs

Seven cases were above 1.1× after round three. Each change below was measured
alone against the table, and the emitted Go of every program in `bench/`,
`challenges/` and `examples/` was diffed before and after the one change that
was meant to touch no output (the builtin registry), as a check that a refactor
changed nothing a binary does.

| Case | before | after | what changed |
|---|---:|---:|---|
| `sparse_life` | 3.20× | 1.7× | map storage, packed plane keys, the filter fusion (above) |
| `pipeline_body` | 1.46× | 0.88× | `Split` + `Split Each` + `Convert To Integers` parse each field straight out of its line: no `[]string` of lines, no `[][]string` of fields |
| `pairs_increase` | 1.26× | 0.84× | `Pairs` + `Count Matching` tests each pair as it is formed; the list of tuples (twice its source's size) is never built |
| `join_output` | 1.62× | 1.05× | the fused `Join` reserved exactly the input's length, so a transform that appends one byte per line overflowed it and the builder reallocated and copied the whole answer; it now reserves a quarter more, and a large `Text` is written to stdout without `fmt`'s doubling buffer |
| `explore_states` | 1.31× | 0.93× | `Mode: Count` is the size of the visited set, so the visit-order list is only built for the mode that returns it; depth per queued state only when `Distances` or `Until:` reads it; the visited set is tested and set with one map assignment |
| `partition_parts` | 1.06× | 1.01× | `Partition` + `Take Item 0/1` collects only the half that is read; the other half is never built |
| `math_builtins` | 1.12× | 1.06× | `isqrt` corrects its float estimate by multiplying (safe up to `floor(sqrt(MaxInt64))`) instead of dividing twice |
| `group_map_values`, `toposort_words`, `count_by_entries` | 1.68×, 1.85×, 1.07× | 0.79×, 1.02×, 1.09× | the map storage change; the first two had also been helped by earlier rounds, so the table above had drifted |

**Three notes.** `dmMap`'s new layout (values in a slice,
an index map beside it) was expected to help everything that builds a map and
helped the ones that walk it; building is one probe either way.
**Bounds checks** (the remaining idea from the tight-loop section) were
implemented and then taken out again, which is worth recording because the
reason is not the one the section above expected. The sound, narrow form is
guard-dominated indexing: when an expression has just proved the index in range
(`j >= 0 and j < length(all) and item(all, j) > x`, or the same in an `if`), the
`item` is emitted as a plain index. It worked — the checked helper disappeared
from the emitted Go and the Go compiler reported no bounds check left in the
loop — and it made the program **slower**: 60 ms became 74 ms on a count of
elements exceeding a looked-up value, and the same 20–25% on four other
data-dependent predicates. Disassembly shows why: with the redundant check
present Go compiled the final comparison to a branchless `SETG`; without it,
to a conditional jump on a value that is unpredictable by construction. The
check was not the cost, and removing it changed the code layout for the
worse. Pairing the plain index with a branchless counter recovered the loss
and gained about 1.5%, which does not pay for the machinery, and a
dependent-load microbenchmark showed the guarded helper indistinguishable from
raw indexing (1.93 ms against 1.95 ms for 1M steps). So the dominated check
stays: it is free, and in at least one common shape it is load-bearing.

The general question — could *any* bounds-check work pay — has a measurement
that bounds it, and it is kept as a test: `DOMAIN_BENCH_BCE=1 go test ./bench
-run TestBoundsCheckUpperBound -v` builds every case's emitted Go twice, as
shipped and with `-gcflags=-B` (every check deleted, safety included), and
times both. Across all thirty-seven the mean difference is **+0.2%**; no case
is above its noise except as an artefact. The apparent outlier,
`fold_grid_writes`, swings between −8% and +34% between *rebuilds of the same
source* (28 ms, 30 ms, 42 ms for identical code), which is binary layout rather
than anything the checks do. The array-walk program from the tight-loop section
(`bench/mahoraga/i05_jumps`) measures 9.6 ms checked and 9.8 ms unchecked. The
reason is the one the dependent-load microbenchmark showed: `dmItem` is
inlined, its test makes Go's own check redundant, and the one remaining branch
is perfectly predicted. There is nothing here for a bounds-check pass to take.
Re-run the test after a toolchain upgrade; the README's earlier 1.14× on that
loop was measured with an older one.
`join_output`'s `fmt`
buffer was worth about 5% on its own; the builder reallocation was the rest.

## Verifying the in-place widening against the AoC 2017 suite

The 25-program suite that motivated the accumulator work lives on a branch that
was never merged, so it is not in this tree. It is still the only check that
matters for a pass whose whole job is to remove a copy, and running the compiler
against it found two things no unit test in this repo could have.

Method: every program built with the compiler before and after the change, run
one process at a time on an idle box, `min` of 5 with the warmup discarded, every
answer checked against the suite's `expected/`.

**It found a regression the change itself introduced.** Day 6 came out 1.8×
*slower*. Its outer search carries a map that grows to twelve thousand entries
and its inner redistribution loop writes only the sixteen-element list beside it,
and owning the whole state on entry to the inner loop cloned that map once per lap
of the outer one — the quadratic the pass exists to remove, one level up. The
copy is now narrowed to the fields a marked update actually writes.

**It found a restriction nobody had written down.** The pass only looked at a
loop body of *one* stage. Day 6's outer body is an `Apply` plus a nested loop, so
its map insert — the entire reason that program is slow — was never considered at
all. Nothing in the source shows which side of that line a program falls on.

With both fixed:

| | before | after | |
|---|---:|---:|---|
| day 6, Memory Reallocation | 54.5 ms | 3.8 ms | 14.5× |
| day 23, Coprocessor Conflagration | 22.6 ms | 5.6 ms | 4.0× |
| day 7, Recursive Circus | 25.8 ms | 16.8 ms | 1.5× |

The remaining 22 programs are within noise, which is the expected shape: the pass
only pays where a collection is threaded through a loop.

Days 22 and 25 do not compile at all with the earlier compiler — both call
`fill(n, 0)` and store the result where its element type has to agree with the
rest of the program, which is the generic-instantiation bug fixed on this branch.
That is two of twenty-five programs that a unit test found and no benchmark
would have.

### What the natural spelling costs now

Days 22 and 25 were both written *around* the old limitation — a flat List with
hand-computed indices instead of the Map the other language columns use — and
their headers say so. Rewritten the natural way, both now run where they
previously could not, and both are slower than the workaround:

| | hand-indexed List | natural Map | |
|---|---:|---:|---|
| day 22, Sporifica Virus | 411.9 ms | 1156.3 ms | 2.81× |
| day 25, The Halting Problem | ~1.3–1.8 s | ~2.2 s | ~1.3× |

That is the right answer, not a disappointment: indexing beats hashing, and it
did before the pass existed. What changed is that the natural spelling is now a
*performance choice* rather than a program that never finishes, and the cost of
choosing it is a number rather than a cliff.
