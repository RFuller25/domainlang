# The optimizer

The optimizer is the language's thesis made concrete: a `Domain Expansion`
names an algorithm, and the pipeline owes you its **result**, not its
method. After resolution produces the typed IR, `optimizer.Optimize` runs
rewrite passes over the node list; each applied rewrite is recorded for
`--explain`. Because Shikigami are inlined before optimization, rewrites
fire straight through user-defined operations — calling the prelude's
`Top K Sum` still fuses.

Both backends consume the pipeline **after** optimization, so a rewrite
speeds up the interpreter and the compiled binary alike.

Passes run in rounds until a full round applies nothing, so rewrites
**cascade**: `Quicksort + Reverse` first flips into one descending
`Quicksort`, which can then fuse with a following `Select Top K` into a
quickselect. `--explain` prints every step of the chain.

## The pass catalog

Twenty-six passes in four families. "Cost" is what the rewrite saves.

### Algorithm substitutions

The showpieces: the named algorithm is swapped for a faster one with a
provably identical result.

| # | Pattern | Rewrite | Cost |
|---|---------|---------|------|
| 1 | `Sort` + `Select Top K` | `PartialSelect` (quickselect, sorts only the k selected) | O(n log n) → O(n + k log k) |
| 2 | `All Pairs` with `(a, b) -> a + b = K`, Mode First/Count | `HashSetPairScan` (complement multiset) | O(n²) → O(n) |
| 3 | `All Pairs` with `(a, b) -> a - b = K` or `b - a = K`, Mode First/Count | `HashSetDiffScan` (complement multiset) | O(n²) → O(n) |
| 4 | `Combinations 3` with `(a, b, c) -> a + b + c = K`, Mode First/Count | `HashSetTripleScan` (prefix pair-sum multiset) | O(n³) → O(n²) |
| 5 | `Sort` + `Take Item k` | `QuickselectItem` (kth order statistic) | O(n log n) → O(n) |
| 6 | `Map Each` (linear body `a*x + b`, `a ≠ 0`) + `Max`/`Min` | `LinearMapExtremum`: reduce the input first, apply the lambda **once** (a decreasing map flips Max↔Min) | n lambda applications → 1, no mapped list |
| 7 | `All Pairs` with `(a, b) -> a * b = K`, Mode First/Count | `DivisorPairScan` (each element's only partner is K÷element; zeros counted separately, since a zero pairs with *everything* exactly when K = 0) | O(n²) → O(n) |
| 8 | `Window size [step]` + `Map Each ((w) -> sum(w)/max(w)/min(w))` | `WindowedReduce`: prefix sums for sum, a monotonic deque for max/min — one streaming pass, no window lists materialized | O(n·size) time and space → O(n) |
| 9 | `BFS`/`Dijkstra` + `Apply ((g) -> at(g, R, C))` | `SearchTarget`: early-exit search that stops the moment the target settles (BFS labels at enqueue, Dijkstra at pop — the value is already final) | whole-grid exploration → only cells at distance/cost ≤ the target's |

For 2–4 and 7 the lambda is recognized in any operand order or association,
with the literal on either side of `=`, and the parameters must be distinct
names. `First` modes return the values of the lexicographically-first index
combination, exactly matching the naive scan. Pass 9 reproduces every naive
validation with identical wording — predicate errors, start checks,
Dijkstra's cost check, and the `at()` bounds error the naive pipeline would
only hit after the full search — and returns -1 for an unreachable or
unwalkable target, exactly like reading the full distance grid. Passes 6–8 assume Domain's
usual numeric model (values stay within int64); pass 6 deliberately excludes
division from the linear form. Pass 8 is safe for the partial `max`/`min`
builtins because windows always hold ≥ 1 elements, so the empty-list error
they guard against cannot occur.

```
[explain] Domain rewrote Quicksort (Descending) + Top 3 → Cursed Quickselect. Guaranteed hit.
[explain] Domain rewrote Combinations 3 (sum = 2020) → Cursed Hash-Set Triple Scan. Guaranteed hit.
[explain] Domain rewrote All Pairs (difference = 3) → Cursed Hash-Set Scan. Guaranteed hit.
[explain] Domain rewrote Quicksort (Descending) + Take Item 1 → Cursed Quickselect (kth order statistic). Guaranteed hit.
[explain] Domain rewrote Map Each (linear) + Max → input Min + one application (monotone maps commute with extrema). Guaranteed hit.
[explain] Domain rewrote All Pairs (product = 12) → Cursed Divisor Scan. Guaranteed hit.
[explain] Domain rewrote Window 3 + Map Each (sum) → Cursed Sliding-Window Sum (one pass, no window lists). Guaranteed hit.
[explain] Domain rewrote BFS + at(2, 2) → early-exit search (stops when the target settles). Guaranteed hit.
```

### Reordering dead code

Reorderings whose effect is provably invisible are cancelled or hoisted.

| # | Pattern | Rewrite |
|---|---------|---------|
| 10 | `Sort` + `Sort` | keep only the second (the first ordering is dead) |
| 11 | `Reverse` + `Reverse` | drop both (an involution applied twice) |
| 12 | `Sort` + `Reverse` | one `Sort` with the opposite order |
| 13 | `Sort`/`Reverse` + `Sum`/`Count`/`Max`/`Min`/`Product` | drop the reordering (the reduction is order-insensitive) |
| 14 | `Unique` + `Unique` | one `Unique` (idempotent) |
| 15 | `Unique` + `Max`/`Min` | drop `Unique` (duplicates cannot move an extremum) |
| 16 | `Sort` + `Unique` | swap to `Unique` + `Sort` (dedupe first, sort d ≤ n elements) |

Pass 13 deliberately excludes `Count Matching`: its *result* is
order-insensitive but its per-element error positions are not.

### Map/Filter dead code and fusion

| # | Pattern | Rewrite |
|---|---------|---------|
| 17 | `Map Each ((x) -> x)` | drop the node (often the residue of pass 24) |
| 18 | `Map Each` (total lambda) + `Count` | drop the map (mapping preserves length) |
| 19 | `Map Each` + `Map Each` (first lambda total) | one fused `Map Each` running the composed lambda — one pass, no intermediate list |
| 20 | `Filter` + `Filter` | one fused `Filter` with the conjoined predicate |
| 21 | `Filter` + `Count` | `Count Matching` (count without materializing the list) |
| 22 | `Fold` with `Seed: 0` and `(acc, x) -> acc + x` | `Sum` |
| 23 | constant predicates (after folding): always-true `Filter` disappears; always-false `Filter` returns `[]` without scanning; always-true `Count Matching` becomes `Count`; always-false becomes `0` | |

### Expression-layer simplification

These rewrite `Using:` lambda bodies in place (interpreter and compiler
share the lambda, so both see it), and they feed the structural passes —
folding `1 = 2` to `false` is what arms pass 23.

| # | Pattern | Examples |
|---|---------|----------|
| 24 | algebraic identities | `x + 0 → x` · `x * 1 → x` · `x / 1 → x` · `x * 0 → 0` · `x - x → 0` · `x = x → true` |
| 25 | constant folding | `2 + 3 → 5` · `7 / 2 → 3` · `2 < 3 → true` · `"a" = "b" → false` · `-(4) → -4` |
| 26 | boolean short-circuit | `true and p → p` · `false and p → false` · `p or false → p` |

```
[explain] Domain simplified the Using: lambda of Filter (boolean short-circuit, constant folding). Guaranteed hit.
```

## The safety rules every pass obeys

1. **Types are preserved.** A rewritten node keeps the pipeline's In/Out
   signature; typecheck already ran and stays valid.
2. **Errors are never swallowed.** A pass may only *discard* work that is
   total (cannot fail). `x * 0` folds to `0` only when `x`'s expression is
   error-free; `Map Each ((x) -> 10 / x)` is never elided before `Count`,
   never fused into a following map, and `7 / 0` never folds — the naive
   pipeline's division error must survive. The `isTotal` analysis in
   `optimizer/walk.go` is the gatekeeper.
3. **Both backends see the same rewrite.** Rewritten nodes carry their
   arguments in `Meta`, and the fused lambdas both backends run are the
   *same* object — the codegen switch has a case for every rewritten prim
   (`PartialSelect`, `HashSetPairScan`, `HashSetDiffScan`,
   `HashSetTripleScan`, `QuickselectItem`, `LinearMapExtremum`,
   `DivisorPairScan`, `WindowedReduce`, `SearchTarget`).
4. **Sub-pipelines are respected.** Passes that rewrite a node in place
   (the scans, `Fold → Sum`, lambda simplification) also fire inside
   `Channel` bodies and `Simple Domain` loop bodies. Passes that change the
   node list's length run only at the top level, because nested lists are
   captured by their parents' closures.

Two documented near-misses show where the line is: `Unique` is *not*
elided before `Sum`/`Count` (it changes them), and `Sort + Unique` swaps
rather than drops (both are needed).

## Flags

- `--explain` prints each applied rewrite to stderr, or
  `[explain] no optimizations applied.`
- `--no-optimize` skips every pass. This is not just a debugging aid: the
  naive pipeline is the **correctness oracle** — property tests run each
  rewrite against it over thousands of random inputs, and the golden/oracle
  suites run every anchor program in both modes and require identical
  output.

## How the passes are tested

Three layers, in `optimizer/` and `codegen/`:

1. **Algorithm property tests** (`scans_test.go`, `optimizer_test.go`,
   `pairsum_test.go`): the fast implementations (quickselect, the hash
   scans) against naive Go implementations over thousands of random inputs.
2. **Differential program tests** (`e2e_test.go`): every pass has a real
   Domain program run interpreted with and without optimization over
   randomized inputs — outputs must match, an optimized run may never turn
   an error into a success, and the expected `--explain` line must actually
   appear (with negative cases asserting guarded rewrites do *not* fire).
3. **Compiled oracle tests** (`codegen/codegen_test.go`): programs
   exercising each rewritten node are compiled in both optimizer modes and
   diffed byte-for-byte against the interpreter.

## Adding a pass (the discipline)

1. Implement the naive primitive first; it is the oracle.
2. Pattern-match on `Node.Prim` + `Node.Meta` in a new pass listed in
   `optimizer.passes`; swap or fuse nodes **keeping the type signature**;
   append a `Rewrite{Message}`. Mark swappable source primitives with
   `Swappable: true`.
3. Never discard a computation unless `isTotal` proves it cannot fail.
4. Property-test the rewritten node against the naive path over random
   inputs, add a differential case to `e2e_test.go` (assert your
   `--explain` message fires), and add a compiled oracle program.
5. If the pass renames `Node.Prim` or the new node needs arguments, record
   them in `Meta` and add the matching codegen case — otherwise
   `domain build` loses the rewrite (this is how `HashSetPairScan` carries
   its `target`).

Candidate future passes:

- `Fold → Product` (blocked: `Product` seeds from the first element and
  errors on empty, but `Fold Seed: 1` returns 1 — semantics differ on empty
  input).
- Common-subexpression elimination inside lambda bodies.
