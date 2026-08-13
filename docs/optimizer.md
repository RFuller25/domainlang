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

Thirty passes in four families, plus one that runs after the rest. "Cost" is
what the rewrite saves.

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
| 10 | `Filter` (total predicate) + `Take Item 0` | `Find`: stop at the first match instead of testing every element and building the list of all of them, and report `Take Item`'s own message when there is no match | O(n) tests + a match list → tests up to the first hit, no list |
| 11 | Bounded `Unfold` + zero or more total `Map Each`/`Filter`, optionally ending in `Apply ((x) -> take(x, N))` | `Stream`: one generate/map/filter/collect loop instead of a materialized list per stage; terminated by `take`, it stops the instant N elements have survived instead of running the full `While:` bound | one allocation instead of one per stage; terminated by `take`, raw generation itself stops early (AoC 2017 day 15's dueling generators: ~4×N and ~8×N draws instead of the full bound) |

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
[explain] Domain rewrote Filter + Take Item 0 → Cursed First Match (stops at the first hit). Guaranteed hit.
[explain] Domain rewrote Unfold + Map Each + Filter + Apply (take 5000000) → Cursed Stream (early exit). Guaranteed hit.
```

### Reordering dead code

Reorderings whose effect is provably invisible are cancelled or hoisted.

| # | Pattern | Rewrite |
|---|---------|---------|
| 11 | `Sort` + `Sort` | keep only the second (the first ordering is dead) |
| 12 | `Reverse` + `Reverse` | drop both (an involution applied twice) |
| 13 | `Sort` + `Reverse` | one `Sort` with the opposite order |
| 14 | `Sort`/`Reverse` + `Sum`/`Count`/`Max`/`Min`/`Product` | drop the reordering (the reduction is order-insensitive) |
| 15 | `Unique` + `Unique` | one `Unique` (idempotent) |
| 16 | `Unique` + `Max`/`Min` | drop `Unique` (duplicates cannot move an extremum) |
| 17 | `Sort` + `Unique` | swap to `Unique` + `Sort` (dedupe first, sort d ≤ n elements) |

Pass 14 deliberately excludes `Count Matching`: its *result* is
order-insensitive but its per-element error positions are not.

### Map/Filter dead code and fusion

| # | Pattern | Rewrite |
|---|---------|---------|
| 18 | `Map Each ((x) -> x)` | drop the node (often the residue of pass 28) |
| 19 | `Map Each` (total lambda) + `Count` | drop the map (mapping preserves length) |
| 20 | `Map Each` + `Map Each` (first lambda total) | one fused `Map Each` running the composed lambda — one pass, no intermediate list |
| 21 | `Filter` + `Filter` | one fused `Filter` with the conjoined predicate |
| 22 | `Filter` + `Count` | `Count Matching` (count without materializing the list) |
| 23 | `Fold` with `Seed: 0` and `(acc, x) -> acc + x` | `Sum` |
| 24 | constant predicates (after folding): always-true `Filter` disappears; always-false `Filter` returns `[]` without scanning; always-true `Count Matching` becomes `Count`; always-false becomes `0` | |
| 25 | `Map Each` + `Sum` / `Product` | `Sum By` / `Product By` — folds each mapped value as it is produced, so no mapped list is built |
| 26 | `Zip` + `Map Each` | one fused pass that builds each pair as a loop-local; the compiled form has no `[]tuple` in it |
| 27 | constant predicates on the early-exit primitives: always-true `Take While` (and always-false `Drop While`) disappear, their opposites return `[]` unscanned; `Any`/`All` become a constant or an emptiness test |  |

Passes 25 and 26 are unconditional: neither reorders lambda calls nor
substitutes one body into another, so every evaluation the naive pipeline
performed still happens, in the same order, and a lambda that fails still
fails on the same element.

### Expression-layer simplification

These rewrite `Using:` lambda bodies in place (interpreter and compiler
share the lambda, so both see it), and they feed the structural passes —
folding `1 = 2` to `false` is what arms passes 24 and 27.

| # | Pattern | Examples |
|---|---------|----------|
| 28 | algebraic identities | `x + 0 → x` · `x * 1 → x` · `x / 1 → x` · `x * 0 → 0` · `x - x → 0` · `x = x → true` |
| 29 | constant folding | `2 + 3 → 5` · `7 / 2 → 3` · `2 < 3 → true` · `"a" = "b" → false` · `-(4) → -4` |
| 30 | boolean short-circuit | `true and p → p` · `false and p → false` · `p or false → p` |

```
[explain] Domain simplified the Using: lambda of Filter (boolean short-circuit, constant folding). Guaranteed hit.
```

### Linear accumulators — the pass that runs last

`insert`, `put` and `setat` are **functional**: each returns a new collection
and leaves its argument untouched, which is what makes a lambda safe to apply
twice (see [expressions.md](ref-builtins-collections.md#building-and-updating-a-collection)).
They were implemented by copying, so a `Fold` that built a collection one
write at a time was quadratic in the *collection*, not linear in the writes —
20,000 inserts into a Map took 30 s interpreted, and 20,000 `setat`s on a
300×300 grid took 44 s, because each one copied all 90,000 cells.

The semantics do not change. What the pass observes is that a fold's
accumulator is dead the moment the lambda returns, so where nothing can read
the copied-from value after an update, the copy is unobservable. Those sites
are marked, and both backends write through instead of copying.

| # | Pattern | Cost |
|---|---------|------|
| 31 | an update rooted at a `Fold`/`Reduce`/`Fold From:` accumulator, with no read of it afterwards | O(size) per write → O(1) |

```
[explain] Domain made 1 accumulator update(s) in Fold write in place — the copy was never read. Guaranteed hit.
```

Three things make it safe rather than merely plausible:

- **The last-use test is path-sensitive.** `if wanted(x) then insert(acc, k, x)
  else acc` is the ordinary shape of a conditional record, and a positional
  "is this the textually last mention" rule refuses it, because the `else acc`
  comes after. Conditional arms are mutually exclusive, so a use in one is not
  a use after a site in the other.
- **The accumulator is cloned once on entry.** The analysis proves nothing
  *inside* the lambda reads the copied-from value; it proves nothing about who
  else holds the seed, and a `Part` or a `Channel` branches from one value —
  `Fold From:`'s accumulator *is* the current pipeline value, and `Reduce`'s
  is an element of the input list. One copy, amortized over every write.
- **It runs after the cascade has settled**, so no later pass can duplicate an
  annotated call. Constant folding applies a lambda twice, which is exactly
  what an in-place update must not have done to it.

`Scan`, `Iterate` and `Iterate Until Fixed Point` are excluded by
construction: the first two keep every intermediate accumulator in their
output, and the third compares the previous value against the new one to
detect convergence. So are `Repeat`, `While` and `For`: the pass drives off a
primitive whose lambda threads an accumulator, and a loop body is a
sub-pipeline rather than a lambda, so there is no accumulator parameter to
follow. A simulation written as `Repeat N` over a state value still copies.

Three builtins that *look* like they belong are also excluded, each for its
own reason:

- **`del`** — removing a key shifts the key order, which a list taken from the
  accumulator earlier *would* see, unlike an append.
- **`set` (List)** — a `List` is a Go slice at run time, and `take`, `drop` and
  `slice` hand out a subslice of the *same backing array* in both backends
  (`xs[:n]`, in `eval/eval.go` and in the generated runtime's `dmTake`). An
  in-place write is therefore visible through any subslice taken earlier, so
  "is the accumulator dead?" stops being a question about the accumulator
  alone. `concat` is not an alias source — it allocates.
- **`with` (Record)** — excluded on cost, not safety. A record copy is
  O(fields), which was never what made anything quadratic.

The `set` exclusion has a measured price, recorded in
[aoc-gaps.md](aoc-gaps.md#14-set-on-a-list-accumulator-is-still-osize). See
`optimizer/linear.go`.

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
   `DivisorPairScan`, `WindowedReduce`, `SearchTarget`, `Stream`).
4. **Sub-pipelines are respected.** Passes that rewrite a node in place
   (the scans, `Fold → Sum`, lambda simplification) also fire inside
   `Channel` bodies, `Simple Domain` loop bodies, and a `Using:` written as
   an [indented pipeline](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)
   — which is where a `List<List<Int>>` puts its pair scans. Passes that
   change the node list's length run only at the top level, with one
   exception: `fuseUnfoldStream` also runs inside `Channel` bodies (see
   "Length-changing passes inside sub-pipelines" below). `Part` and loop
   bodies stay out of reach for every length-changing pass — their node
   lists are still captured by their parents' closures.

Two documented near-misses show where the line is: `Unique` is *not*
elided before `Sum`/`Count` (it changes them), and `Sort + Unique` swaps
rather than drops (both are needed).

## Measured arguments and the passes that fold literals

A primitive's Int argument may be *measured* — a lambda over the current
value rather than a literal (see
[primitives.md](ref-transforms.md#measured-arguments)). A measured argument has
no value at optimize time: it lands in `Meta` under its own `…Expr` key, and
the literal key is **absent**.

That absence is the hazard. Every pass reads its literal with a type
assertion whose zero value is a perfectly plausible number, so a measured
argument does not make a pass fail to fire — it makes the pass fire with a
fabricated constant. `Select Top (measured)` folded as `Top 0` returns the
empty list, silently, and only when optimized.

So the rule is opt-in, and there are two ways to opt in.

**Carry it.** A pass whose fused node takes the argument as *data* reads it
through `readArg` and moves it onto the fused node with `writeMeta`, never
touching `Meta` directly. The value is resolved at run time through the
primitive's own resolver — bound check included — so the rewrite cannot turn
an error into a success (safety rule 2). Two passes do this:

| Pass | Argument | Why it can carry |
|---|---|---|
| `fuseWindowReduce` | `size`, `step` | `ir.WindowedSums` / `ir.WindowedExtrema` take them as runtime arguments already |
| Sort + `Select Top K` → quickselect | `k` | `TopK` (and the compiled `dmTopK`) take `k` as a value |
| `fuseSearchTarget` | `row`, `col` | the early-exit search takes the start as data |

`--explain` says `Top (measured)` rather than inventing a number.

**Stand down.** Every other pass consults `hasMeasuredArg` before reading any
literal, because its rewrite is valid *because of what the literal is*:

| Pass | Reads | Why it cannot carry |
|---|---|---|
| `Filter` + `Take Item 0` → `Find` | `index` | The early exit is valid only for index 0 |
| `Sort` + `Take Item` → extremum | `index` | Same |
| `Fold` (seed 0) → `Sum` | `seed` | The identity depends on the seed being 0 |
| the pair/triple scans | `k` | Not applicable — `Combinations k` fixes a lambda arity and stays literal by rule |

The guard is the default, so a measured argument added later to a primitive a
pass has never heard of is refused rather than mis-folded, and enabling a pass
is a deliberate change at one call site.

## Lambdas that update a binding

A lambda containing a `:=` (see
[expressions.md](expressions.md#updating-a-local--)) is not a function of its
arguments: applying it writes to a binding that outlives the application. Every
pass here assumes the opposite, and in ways a write would notice — fusion turns
"all of `f`, then all of `g`" into "`f` then `g`, per element", an algorithm
substitution applies the lambda to different elements a different number of
times, and the expression rules drop and duplicate subexpressions.

So **every** rewrite stands down on such a lambda, and it is one guard rather
than twenty: `nodeLambda` reports an updating lambda as *absent*, and a pass
that cannot see a lambda does not fire. The expression simplifier, which reads
`Meta` directly because it wants lambdas no other pass may have, repeats the
check itself. `isTotal` also reports a write as non-total — not because it can
fail, but because its callers use that answer to decide what may be discarded,
and discarding a write loses it.

The cost lands on the stage that writes, not on the program: a pipeline where
one `Map Each` updates a counter still gets every rewrite its other stages
earn.

## Local bindings and node lists

A `Consider … Of` binding (see
[expressions.md](expressions.md#stage-bindings--consider--as--consider--of))
puts the statements of its scope inside a **Consider node**, the way a loop
holds its body. That has the same consequence a loop body has: passes that
rewrite nodes *in place* recurse into the list and fire normally, while passes
that change a list's **length** — the fusions — stay on the top-level chain,
so a stage carrying an `Of` binding is not fused with its neighbours. It is
the measured-argument bargain in a different shape: a value that is not known
until data arrives stands the length-changing rewrites down.

The `As` bindings never reach the optimizer at all, and one of them is folded
rather than bound precisely so that they do not cost a rewrite. The passes
below match the *shape* of a lambda body — `(a, b) -> a + b = 2020` is the
pair-sum scan's whole trigger — so a constant left behind a name would have
hidden the literal the pass reads. Substituting it at resolve time means
`Consider target As 2020` keeps the rewrite, and a test pins that.

The compiler's *lowerings* have no such distinction: `gen.measuredOperand`
emits the literal as a constant when there is one and a computed `int64`
otherwise, so a measured argument costs one variable and the bounds check the
interpreter runs at the same moment — in a fused lowering exactly as in an
unfused one.

The compiler's own **fusions** do, though, and for a sharper reason. The
adjacency rules in `codegen/matchgen.go` are keyed on a `Split`'s separator,
and three of them fire on `sep == ""` — the character-split fast paths. The
empty string is a meaningful separator there, not a "missing" one, so a
measured separator read through a type assertion would fire those three
*wrongly* rather than merely failing to fire. `tryFuse` therefore checks
`hasMeasured(nodes[0], "sep")` before any rule runs. It is the same rule as
`hasMeasuredArg`, on the other side of the backend, and the reason both exist:
a zero value is only safe to read as "absent" when it is not also a legal
answer.

One compiler fusion stands down for a different reason. `gridSearchFusable`
builds a search's mask straight from input lines so the grid is never
materialized — and a measured start is a lambda *over that grid*. There would
be nothing to measure from, so the fusion refuses a measured start and the
ordinary path, which does build the grid, handles it.

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
- **Common prefix hoisting across `Part` blocks.** Two Parts that start with
  the same operations could share one computation. This is CSE over
  sub-pipelines and wants its own design — the naive version would have to
  prove the shared prefix total, since discarding a failing computation would
  swallow an error the naive program reports.
- **Part-local dead code.** A `Part` whose body never `Reveal`s computes
  nothing observable and could be dropped entirely under `--release`. Today
  it is a lint warning, which is the right first step.
- **Length-changing passes inside sub-pipelines.** Rule 4 above confines most
  of them to the top level, so `Sort` + `Select Top K` written inside a
  `Part` or loop body is not fused. `Channel` bodies are the one exception:
  `fuseUnfoldStream` (pass 11) runs there too, which is what day 15's dueling
  generators needs. It works because `Channel`'s own `Eval` reads its node
  list from `Meta["nodes"]` at call time rather than closing over a captured
  slice (`prims/channel.go`) — the "real refactor" this note used to call
  for, now done for `Channel` specifically. `Part` and loop bodies still
  capture their node lists by closure and remain out of reach.
