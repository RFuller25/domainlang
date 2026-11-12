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

Fifty passes in five families, plus one that runs after the rest. "Cost" is
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
| 8 | `Window size [step]` + `Map Each ((w) -> sum(w)/max(w)/min(w))`, or + `Sum Each` | `WindowedReduce`: prefix sums for sum, a monotonic deque for max/min — one streaming pass, no window lists materialized | O(n·size) time and space → O(n) |
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
| 12 | `Sort` + `Sort` | keep only the second (the first ordering is dead) |
| 13 | `Reverse` + `Reverse` | drop both (an involution applied twice) |
| 14 | `Sort` + `Reverse` | one `Sort` with the opposite order |
| 15 | `Sort`/`Reverse` + `Sum`/`Count`/`Max`/`Min`/`Product` | drop the reordering (the reduction is order-insensitive) |
| 16 | `Unique` + `Unique` | one `Unique` (idempotent) |
| 17 | `Unique` + `Max`/`Min` | drop `Unique` (duplicates cannot move an extremum) |
| 18 | `Sort` + `Unique` | swap to `Unique` + `Sort` (dedupe first, sort d ≤ n elements) |

Pass 15 deliberately excludes `Count Matching`: its *result* is
order-insensitive but its per-element error positions are not.

### Map/Filter dead code and fusion

| # | Pattern | Rewrite |
|---|---------|---------|
| 19 | `Map Each ((x) -> x)` | drop the node (often the residue of pass 32) |
| 20 | `Map Each` (total lambda) + `Count` | drop the map (mapping preserves length) |
| 21 | `Map Each` + `Map Each` (first lambda total) | one fused `Map Each` running the composed lambda — one pass, no intermediate list. The first body is substituted for the second's parameter when that is free (arithmetic) and bound once with `consider` when it calls something and is read more than once — `(c) -> sum(c)` into `(y) -> y * y + y` sums once, not three times |
| 22 | `Filter` + `Filter` | one fused `Filter` with the conjoined predicate |
| 23 | `Filter` + `Count` | `Count Matching` (count without materializing the list) |
| 24 | `Fold` with `Seed: 0` and `(acc, x) -> acc + x` | `Sum` |
| 25 | constant predicates (after folding): always-true `Filter` disappears; always-false `Filter` returns `[]` without scanning; always-true `Count Matching` becomes `Count`; always-false becomes `0` | |
| 26 | `Map Each` + `Sum` / `Product` | `Sum By` / `Product By` — folds each mapped value as it is produced, so no mapped list is built |
| 27 | `Group By` + `Map Values ((b) -> sum(b))` (Int) or `((b) -> length(b))` | one running sum (or count) per key, in the keys' first-seen order — no bucket list is built |
| 28 | `Zip` + `Map Each` | one fused pass that builds each pair as a loop-local; the compiled form has no `[]tuple` in it |
| 29 | constant predicates on the early-exit primitives: always-true `Take While` (and always-false `Drop While`) disappear, their opposites return `[]` unscanned; `Any`/`All` become a constant or an emptiness test |  |
| 30 | `Fold` with a Text `Seed:` and `(acc, x) -> acc + e1 + e2 …`, no `eᵢ` reading `acc` | one buffer: the seed, then each element's parts appended — appending to a Text copies all of it, so the fold was quadratic in its output (80k short lines: 30 s interpreted, 19 s compiled → under 0.1 s) |
| 31 | a stage lambda doing work that does not depend on its element — `sort(all)`, `keys(m)`, `sum(xs)` over a binding or a global — and `contains(xs, v)` over such a list | the work is computed **once** when the stage starts, by a synthesized `Consider` around the stage, and the lambda reads the binding; a list on the left of `contains` is bound as a set, so each lookup is one probe instead of a scan. Runs once after the cascade settles. Only a total, deterministic expression reading nothing a `:=` writes moves — anything else is left in place and reported as a warning |

Passes 26 and 27 are unconditional: neither reorders lambda calls nor
substitutes one body into another, so every evaluation the naive pipeline
performed still happens, in the same order, and a lambda that fails still
fails on the same element.

### Expression-layer simplification

These rewrite `Using:` lambda bodies in place (interpreter and compiler
share the lambda, so both see it), and they feed the structural passes —
folding `1 = 2` to `false` is what arms passes 25 and 28.

| # | Pattern | Examples |
|---|---------|----------|
| 32 | algebraic identities | `x + 0 → x` · `x * 1 → x` · `x / 1 → x` · `x * 0 → 0` · `x - x → 0` · `x = x → true` · **common subexpressions**: `consider a as E in … E …` reads `a` for the second `E` (even a partial `E` — the binding already evaluated it); a total, costly `E` written twice with no binding is bound once, at the start of the narrowest scope (body, `consider` body, `if` arm, right of `and`/`or`) that evaluates it on every path |
| 33 | constant folding | `2 + 3 → 5` · `7 / 2 → 3` · `2 < 3 → true` · `"a" = "b" → false` · `-(4) → -4` |
| 34 | boolean short-circuit, and a Bool's round trip through a number | `true and p → p` · `false and p → false` · `p or false → p` · `(if c then 0 else 1) = 0 → c` · `(if c then 1 else 0) > 0 → c` · `(if c then 0 else 1) = 1 → ikke c` (any comparison of a two-literal conditional with a literal that the two arms answer differently) · **conditional arms that differ in one argument**: `if c then tuple(A, n + 1, k) else tuple(A, n, k) → tuple(A, if c then n + 1 else n, k)` (also `record`, `list`, `insert`, `put`, `set`, `setat`, `concat`; the arguments *before* the differing one must be total, since they now run ahead of `c`) |
| 35 | cheaper equivalent builtins | `contains(keys(m), k) → haskey(m, k)` · `contains(tolist(s), x) → contains(s, x)` · `length(keys(m))` (or `values`, `entries`, `tolist`) `→ size(m)` · `contains(sort(xs), v)` (or `unique`) `→ contains(xs, v)` — which pass 31 can then turn into a set probe · `length(sort(xs))` (or `reverse`, `chars`) `→ length(xs)` · `sort(sort(xs)) → sort(xs)` · `unique(unique(xs)) → unique(xs)` · `reverse(reverse(xs)) → xs` · `item(xs, indexof(xs, m)) → m` when `xs` is a `List<Int>` and `m` is `max(xs)`/`min(xs)` (written inline, or bound by an enclosing `consider` that has not been shadowed) — the extremum is an element, and `max` has already failed on an empty list; over Floats a NaN makes `indexof` answer -1, so the rule is Int-only |

```
[explain] Domain simplified the Using: lambda of Filter (boolean short-circuit, constant folding). Guaranteed hit.
```

### Idioms — the long way round

The families above match shapes written *with* the vocabulary. These match
what gets written *instead of* it, by someone who knew one tool and used it for
everything: a `Fold` that counts, a list sorted on its way into a sum three
stages later, a count compared with zero to ask "is there one". Each is a
correct program whose cost is in how it was said — and the language's promise
is that how it was said is not what it costs.

| # | Pattern | Rewrite | Cost |
|---|---------|---------|------|
| 36 | `Fold` with `Seed: 0` spelling out a named reduction: `acc + 1` · `acc + f(x)` · `if p(x) then acc + 1 else acc` · `if p(x) then acc + f(x) else acc` (either operand order; `f`, `p` total and not reading `acc`) | `Count` · `Sum By f` · `Count Matching p` · `Sum By (if p then f else 0)` | `Count` is the list's length — no loop at all. The others are sinks the compiler [streams into](compiler.md#what-the-generated-code-looks-like); a `Fold` is not, so a `Map Each`/`Filter` in front of a hand-written one built a list the named form never does |
| 37 | `Sort`/`Reverse` + a run of pure, total `Map Each`/`Filter` + an order-insensitive reduction (`Sum`, `Count`, `Max`, `Min`, `Product`, and — with a pure, total lambda — `Sum By`, `Product By`, `Count Matching`, `Any`, `All`) | drop the reordering | O(n log n) and a copy of the list → nothing. Pass 15 is the adjacent case; this is the same fact seen through elementwise stages |
| 38 | `Sort` + `Filter` (pure, total predicate) over `List<Int>` | swap to `Filter` + `Sort` | sorts only the survivors; and `Sort` + `Filter` + `Take Item 0` then cascades into `Filter` + a selection (pass 5) instead of a full sort |
| 39 | `Unique` + `Count` | `CountDistinct`: the size of the set `Unique` builds | the deduplicated list (up to the size of the input) is never built |
| 40 | `Count Matching p` + `Apply ((c) -> c > 0)` (or `>= 1`, `= 0`, `< 1`, `<= 0`, literal on either side), and `Filter p` + `Apply ((xs) -> length(xs) > 0)` likewise; `p` pure and total | `Any p` — or `All (not p)` for the "none" spellings | every element tested → stops at the first match; `Filter` + `Count` + a comparison reaches here through pass 23 |
| 41 | `Map Each` (pure, total) + `Take Item k` | `Take Item k` + `Apply` | n lambda applications and a mapped list → one application |

| 42 | `Window 2` + `Map Each`/`Count Matching`/`Sum By`/`Any`/`All` whose lambda reads each window only at its ends — `first(w)`, `last(w)`, `item(w, 0)`, `item(w, 1)` | `Pairs` + the same stage over a tuple | a list allocated per window → a pair, which the compiler keeps as a loop-local (and for a count never builds). A tuple's projections are total, so the totality-gated passes now reach the stage |
| 43 | `Sort` + `Window 2` + `Any ((w) -> first(w) = last(w))`, or `All Pairs (Mode: Map)` + `Any` whose two lambdas compose to `a = b`, or `All Pairs (Mode: Any)` with `(a, b) -> a = b` (`Mode: All` with its negation); and both with `All` and a negated equality | `Apply ((xs) -> length(unique(xs)) < length(xs))` — or `=` for "all distinct" | O(n log n) plus a list of windows, or O(n²) → one hash pass |
| 44 | `Sum By ((x) -> if p(x) then 1 else 0)` (or `0 else 1`), `p` total | `Count Matching p` (or `ikke p`) | a Bool turned into a number to be summed is a count; `Map Each` + `Sum` reaches here through pass 26 |
| 45 | `Filter ((xs) -> length(xs) > 0)` + `Flatten` | `Flatten` | the filtered list is never built: an empty list adds nothing to a concatenation |
| 46 | `Iterate n` + zero or more pure `Map Each` + `Find Cycle` | the same stages, with an `Iterate` that stops at the first state equal to an earlier one | n laps → about as many as it takes to repeat. A count written "large enough to be sure" costs what the trajectory needs, not what was written |
| 47 | `Map Each` + `Map Each`/`Sum By`/`Count Matching`/`Any`/`All`, at least one side an indented body, the first body provably total (`totalNodes`), both pure | the same stage over one joined body — one loop, no list between, and the first body's last stage now sits beside the second's first, where other passes match (an `All Pairs` mapped to flags, then an `Any` over them, becomes pass 43) |
| 48 | `Apply ((x) -> sort(x))` / `reverse` / `unique` / `sum` / `length`, over a List (Int for `sort` and `sum`, a keyable element for `unique`) | `Sort` · `Reverse` · `Unique` · `Sum` · `Count`, so the passes keyed on the primitive see it. `first`, `max`, `min` are left alone (their failure wording differs from the primitive's) and `take` has no primitive |
| 49 | `Map Each` (pure, total) + `Apply ((x) -> take(x, N))`; `Window s t` (literal `s`, `t`) + the same | `take` first, then `Map Each` on N elements; for a window, `take` of the first `(N-1)·t + s` elements, then `Window` | n lambda applications or windows → N |
| 50 | `Window size [step]` + `Map Each`/`Count Matching`/`Sum By`/`Any`/`All` whose lambda reads each window only as `first(w)`, `last(w)`, `item(w, c)` (literal `c`) | `WindowPositions` + the same stage over tuples of just the elements read (see below) | O(n·size) time and allocation → O(n) |

```
[explain] Domain rewrote Fold (Seed: 0, a conditional count) → Count Matching. Guaranteed hit.
[explain] Domain rewrote Sort + Map Each + Sum → Map Each + Sum (every stage after the Sort works one element at a time, so its ordering never reaches the result). Guaranteed hit.
[explain] Domain rewrote Quicksort + Filter → Filter + Quicksort (filter first, sort only the survivors). Guaranteed hit.
[explain] Domain rewrote Unique + Count → Count Distinct (the set is counted, the deduplicated list never built). Guaranteed hit.
[explain] Domain rewrote Count Matching + Apply (count > 0) → Any (stops at the first match instead of counting them all). Guaranteed hit.
[explain] Domain rewrote Map Each + Take Item 2 → Take Item 2 + Apply (the lambda runs once, on the element that is kept). Guaranteed hit.
[explain] Domain rewrote Window 2 + Count Matching → Pairs + Count Matching (each window was only read at its two ends; a pair is a tuple, not a list). Guaranteed hit.
[explain] Domain rewrote Sort + Window 2 + Any (neighbours equal) → one hash pass (a duplicate check needs a set, not an ordering). Guaranteed hit.
[explain] Domain rewrote All Pairs (a = b) + Any → one hash pass (a duplicate check needs a set, not every pair). Guaranteed hit.
[explain] Domain rewrote Sum By (if … then 1 else 0) → Count Matching (a Bool was being turned into a number to be summed). Guaranteed hit.
[explain] Domain rewrote Filter (non-empty) + Flatten → Flatten (an empty list adds nothing to a concatenation). Guaranteed hit.
[explain] Domain rewrote Iterate 50000 + Find Cycle → an Iterate that stops at its first repeated state (everything after a repeat is a copy of what came before). Guaranteed hit.
[explain] Domain rewrote Map Each + Sum By → one Sum By over the joined bodies (one loop; the first body cannot fail, so no failure moves). Guaranteed hit.
[explain] Domain rewrote Apply (sort) → Sort (a whole-list builtin, spelled as the primitive so the passes that know it can see it). Guaranteed hit.
[explain] Domain rewrote Map Each + Apply (take 3) → Apply (take 3) + Map Each (the lambda runs on the elements kept, not on all of them). Guaranteed hit.
[explain] Domain rewrote Window + Apply (take 2) → Apply (take 4) + Window (only the windows that are kept get built). Guaranteed hit.
```

Every one of these drops, skips or reorders lambda evaluations, so every one
is gated on the lambda being **pure and total** (`optimizer.Elementwise`): a
stage that can fail reports the *first* element it fails on, and which element
is first is exactly what a sort decides and what an early exit skips. The
differential tests pin the stand-downs — `Sort` + `Map Each ((x) -> 10 / x)` +
`Sum` keeps its sort, and a count whose predicate divides keeps counting.
Pass 36 additionally requires the literal seed `0`, for the reason pass 24
does. Pass 36 rewrites in place and so also fires inside `Part`, loop and
pipeline bodies; 37–41 change the node list's length and run at the top level.

Passes 47–49 are the follow-ups. **47** interleaves two loops, which moves which
failure is reported first, so the first body must be *total*: `totalNodes` is an
allowlist of primitives whose evaluators have no failure path but their
lambdas', each lambda proven total with its parameter types. `Take Item`, `Max`,
`Min`, `Product`, `Convert To Integers`, `Extract Integers` and any measured
argument keep the stages apart. An expression body joins as an `Apply` that
returns the lambda's own error, so the words of a failure do not change.
**48** swaps only where builtin and primitive agree exactly, failures included.
**49** drops evaluations past N, so the map must be pure and total, and a
measured `Window` size or step stands the rewrite down; `take` clamps, and the
cut is the same builtin with the same N, so negative and oversized counts mean
what they meant. Separately, every adjacent-pair pass now sees through a
single-stage `Consider … Of` (see "Local bindings" below).

Pass 50 generalizes 42 to any window: `Window size [step]` (size and step literal or measured) followed by `Map Each`, `Count Matching`, `Sum By`, `Any` or `All` whose lambda reads each window only as `first(w)`, `last(w)` or `item(w, c)` with a literal `c` becomes `WindowPositions` + the same consumer. `WindowPositions` walks the same start indexes (same size/step resolution, bound checks and order) and builds, per window, a tuple of just the elements read (the bare element when only one position is read), so AoC 2017 day 1 part 2 (`Window` of a measured size, compared at its two ends) is O(n) instead of O(n·size). The consumer is unchanged, so a failing lambda is reported against the same window index in the same words, and `Any`/`All` stop where they did. `item(w, c)` with `c > 0` is only rewritten for a literal size with `c < size` (otherwise the naive pipeline fails inside the lambda, in words that name the window's length, which a tuple cannot reproduce); a measured size admits only `first`, `last` and `item(w, 0)`. Any other use of the window (`length(w)`, `sum(w)`, a computed index, the window passed whole) stands the rewrite down.

```
[explain] Domain rewrote Window 3 + Map Each → Window Positions + Map Each (each window is read only at position 0, position 2, so those elements are read straight from the list instead of every window being copied). Guaranteed hit.
```

Passes 42–46 came from reading solutions written by hand against real puzzles
(Advent of Code 2017, 2020 and 2021), which is a different source of patterns
from the vocabulary's own examples. Three of them are not gated on totality,
and each for a reason of its own:

- **42** drops and reorders nothing — the consumer is the same stage, run on
  the same elements in the same order, over a tuple instead of a list.
- **45** discards a predicate that cannot fail (`length` of a list).
- **46** stops early, which is what totality usually pays for, and here does
  not have to. A deterministic step that reaches a state it has been in before
  retraces everything it did since, so every lap the marked `Iterate` skips is
  a copy of a lap that already succeeded. The shorter trajectory therefore
  gives each later stage the same answer *and the same failure*: an elementwise
  `Map Each` fails on the same first element (anything it would have failed on
  later has a copy in the prefix), and `Find Cycle` reports the first repeat,
  which the prefix ends in. What it needs instead is determinism — the step
  and the maps must write nothing and draw nothing, which for an indented body
  means it is built only from stages known to (`pureStages`) — and a
  Float-free state, since `-0.0 = 0.0` equates two states that do not behave
  alike. The repeat is found by Brent's algorithm on the raw state: one
  equality test per lap, no hashing, so a state that is not *keyable* (a
  `List`, which is why such programs map it to Text before `Find Cycle`) costs
  nothing extra. On AoC 2017 day 6 as written — `Iterate 50000`, then every
  state joined into a Text key, then `Find Cycle` — the interpreter goes from
  1.7 s to 0.55 s and the compiled binary from 94 ms to 20 ms.

Pass 43's second shape is matched *after* composing the two lambdas and
simplifying, so it is found however the flag was spelled: `if a = b then 0
else 1` followed by `(x) -> x = 0` composes to `(if a = b then 0 else 1) = 0`,
which the expression layer reads as `a = b` (pass 34).

What is deliberately *not* here: `Fold` → `Max`/`Min` (a seeded fold answers
the seed on an empty list where `Max` fails).

The expression layer (pass 35) also has typed rules, for stages whose lambda
parameter types the node states (`Apply`, `Map Each`, `Filter`, `Fold`, …): over a
`List<Int>`, `sum`/`min`/`max` of a `sort`ed or `reverse`d list read the list
directly, and `first`/`last`/`item(_, 0)` of `sort(xs)` become `min`/`max` of
`xs` **behind an empty-list guard** — `if length(xs) = 0 then <original> else
min(xs)` — so an empty list still fails in the original's words. The guard
needs `xs` to be a plain name (it is evaluated twice) and the type to be Int (a
Float list's order is observable through `NaN`). `first(reverse(xs))` and
`last(reverse(xs))` swap to the other end the same way, for any element type.

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
| 51 | an update rooted at a `Fold`/`Reduce`/`Fold From:` accumulator, with no read of it afterwards | O(size) per write → O(1) |
| 51 | the same over a `List` accumulator, where no `take`/`drop`/`slice` is applied to it anywhere in the lambda | O(size) per write → O(1) |

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
detect convergence. `For` is excluded too: its body sees an ambient parameter as
well as the threaded value, which is a second reader this analysis has not
been taught about.

**`While` and `Repeat` are in**, through a second entry point rather than a row
in the table above. A loop body is a sub-pipeline, not a lambda, so the
accumulator is the value entering the body and the lambda to analyse belongs to
the body's own stage — and only when the loop has exactly one stage, since with
two it is the *first stage's* output a write would alias, which is different
reasoning.

The simulation shape this exists for keeps its list inside a state tuple, so
the receiver is followed through constant tuple projections: `set(item(state,
0), i, v)`. Constant indices and tuple fields only — a variable index names a
different list on different laps, and a projection through a list reaches
storage the clone on entry does not own.

| # | Pattern | Cost |
|---|---------|------|
| 51 | a `set` rooted at a `While`/`Repeat` state, or at a constant tuple field of one, with no read of the state afterwards | O(size) per lap → O(1) |

```
[explain] Domain made 1 state update(s) in While write in place — the copy was never read. Guaranteed hit.
```

**How the tuple is written decides whether it fires.** The rule is "no read of
the state after the site", and it does not distinguish a read of field 2 from
a read of the list in field 0 — so

```domain ignore
Using: (s) -> consider i as item(s, 1) in
              consider n as set(item(s, 0), i, item(item(s, 0), i) + 1) in
              tuple(n, i + 1, item(s, 2), item(s, 3))   # reads s after the write
```

is refused, and the same program with `item(s, 2)` and `item(s, 3)` bound
before the write is rewritten. That is conservative rather than clever, and
deliberately so: the alternative is reasoning about which field aliases which,
and the cost of being wrong is a silently different answer.

On `bench/mahoraga/i05_jumps.domain` — a jump-offset walk over a 384-element
list, which is written the second way — the compiled binary goes from **381 ms
to 7.6 ms**, and at full puzzle size from 33 s to 0.17 s.

Three builtins that *look* like they belong are also excluded, each for its
own reason:

- **`del`** — removing a key shifts the key order, which a list taken from the
  accumulator earlier *would* see, unlike an append.
- **`with` (Record)** — excluded on cost, not safety. A record copy is
  O(fields), which was never what made anything quadratic.

**`set` (List) is in, behind a guard rather than an argument.** A `List` is a
Go slice at run time, and `take`, `drop` and `slice` hand out a subslice of the
*same backing array* in both backends (`xs[:n]`, in `eval/eval.go` and in the
generated runtime's `dmTake`). An in-place write is visible through any
subslice taken earlier, so for a List — and for no other accumulator — "is the
accumulator dead?" is not a question about the accumulator alone. So the pass
asks a second question, `aliasSafe`: is any subslice-producing builtin applied
to anything rooted at the accumulator, anywhere in the lambda? If so the whole
lambda is refused, not merely that site, because the subslice may be taken
before the write as easily as after.

The guard is conservative in the direction that matters: it asks nothing about
where the subslice *goes*, so `sum(take(acc, 3))` is refused even though its
result is a number. The alternative is an escape analysis, and the cost of
being wrong is a program that answers differently for a reason no reader can
see. `concat` needs no guard — it allocates — and `item` reads one element out,
so the next-pointer fold this exists for passes untouched.

On the example [aoc-gaps.md](aoc-gaps.md#14-set-on-a-list-accumulator-is-still-osize)
recorded for it — 20,000 writes into a 100,000-element list — the interpreter
goes from **29.9 s to 23 ms**, and the compiled binary runs it in 4 ms. See
`optimizer/linear.go`.

## The safety rules every pass obeys

1. **Types are preserved.** A rewritten node keeps the pipeline's In/Out
   signature; typecheck already ran and stays valid.
2. **Errors are never swallowed.** A pass may only *discard* work that is
   total (cannot fail). `x * 0` folds to `0` only when `x`'s expression is
   error-free; `Map Each ((x) -> 10 / x)` is never elided before `Count`,
   never fused into a following map, and `7 / 0` never folds — the naive
   pipeline's division error must survive. The `isTotal` analysis in
   `optimizer/walk.go` is the gatekeeper. It is **typed** wherever the stage
   states its parameter types: a Tuple's arity is static, so `item(t, k)` with
   a literal `k` in range is total wherever it sits in the body — on the
   parameter, on a `consider` binding, on a projection of a projection. Before
   that, any stage that kept its data in a tuple (which is every stage after an
   `Enumerate`, a `Zip`, a `Pairs`, or a hand-rolled `tuple(...)`) was partial
   as far as every gated pass could tell, and got none of them.
3. **Both backends see the same rewrite.** Rewritten nodes carry their
   arguments in `Meta`, and the fused lambdas both backends run are the
   *same* object — the codegen switch has a case for every rewritten prim
   (`PartialSelect`, `HashSetPairScan`, `HashSetDiffScan`,
   `HashSetTripleScan`, `QuickselectItem`, `LinearMapExtremum`,
   `DivisorPairScan`, `WindowedReduce`, `WindowPositions`, `SearchTarget`, `Stream`,
   `CountDistinct`).
4. **Sub-pipelines are respected.** Passes that rewrite a node in place
   (the scans, `Fold → Sum`, lambda simplification) also fire inside
   `Channel` bodies, `Simple Domain` loop bodies, and a `Using:` written as
   an [indented pipeline](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)
   — which is where a `List<List<Int>>` puts its pair scans. Passes that
   change the node list's length — fusions, elisions, swaps — run at the
   top level **and inside every `Channel`, `Part` and `Simple Domain` loop
   body**. Each of those reads its node list from `Meta["nodes"]` when it runs
   (`prims.bodyOf`; the compiler reads the same key), so a rewrite that shortens
   the list replaces that key and both backends run the new one. A `Using:`
   written as an indented pipeline is reached the same way — its block pipeline
   is read by both backends each time the body runs or is emitted, and takes a
   rewritten node list (`SetBlockNodes`) — which matters more than its place in
   this list suggests: a hand-written program puts a `Map Each` body around its
   work the moment its data is a list of lists, so that body is where its
   `Sort` + `Window` + `Any` actually is. A `Consider`'s `Of` body is still
   captured by its parent's closure and stays out of reach of the
   length-changing passes.

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

## Stages that touch a global

A [global](language.md#cursed-object--globals) widens that question in two ways
a binding did not.

**A `Cursed Tool` is a statement, not a `:=`.** A lambda whose body is an
indented sub-pipeline containing one carries a write that no expression walk
finds, so `ast.HasUpdate` answers "no write here" perfectly correctly and
perfectly uselessly. Such a lambda is treated as effectful too.

**A write is visible to other stages, not only to the next element.** So the
question for a *reader* is whether anything can change what it reads:

- A global **nothing writes after it is declared** is a constant of the run.
  Every reader of it sits after its declaration — visibility is forward-only —
  so reading it is as pure as reading a literal, and those stages keep every
  rewrite. `Cursed Object: target As 2020` followed by a pair scan against
  `target` still becomes the hash-set scan.
- A global something **can** write — a `Cursed Tool`, a `:=`, or a declaration
  written somewhere that runs more than once — costs each stage that reads it
  the same rewrites a writing lambda loses.

Mutability is decided once, when the program is resolved, and travels on the
resolved read (`ast.GlobalRef.Mutable`). It is deliberately over-approximated:
a `:=` counts by spelling alone, without asking whether something nearer
shadowed the global at that point. Being wrong that way costs a stage its
rewrites; being wrong the other way is a wrong answer.

`domain expansion: lint` reports every stage that paid, and names the globals
that cost it — the failure mode here is a program that silently got slower, and
the usual fix is to stop writing the global.

## Stages that draw from the stream

`random`, `randomf` and `pick` read from the run's
[seeded stream](ref-builtins-chance.md), and `frame` and `elapsed` read its
clock, and `pending` reads the table of requests still out. All six answer
differently on two identical inputs, which is the one thing every pass in this
catalog assumes cannot happen.

So a lambda that names any of them is **effectful**, by the same seam a
writing lambda is: `ast.HasNondeterminism` feeds `optimizer/globals.go`'s
`impure`, and from there every aggressive pass sees it. The rewrites it costs
are the ones that would change how many times the lambda runs, or in what
order:

- A `Map Each` fused into the `Max` above it evaluates each element's lambda
  once instead of once per pass, which is the same answer for a pure lambda
  and a different number of draws for this one.
- A dead-code elision that drops a stage nothing reads would drop its draws
  with it, and every draw after it would come from a different place in the
  stream.
- Any reordering moves which draw lands where.

`--explain` says so plainly. `Map Each Using: (n) -> n * 2` followed by `Max`
reports the fusion; the same pair with `(n) -> n * random(1000)` reports no
optimizations applied, and the program prints the same number with and without
`--no-optimize` — which is the property that matters, since a
[replayed game](scopes.md) is only reproducible if the stream is.

This is why the cost is worth paying rather than papering over. A game's
frames are its test output, and a rewrite that quietly changed how many times
a die was rolled would change every frame after it — a diff nobody could read
against a program nobody had changed.

## Local bindings and node lists

A `Consider … Of` binding (see
[expressions.md](expressions.md#stage-bindings--consider--as--consider--of))
puts the statements of its scope inside a **Consider node**, the way a loop
holds its body. That has the same consequence a loop body has: passes that
rewrite nodes *in place* recurse into the list and fire normally, while passes
that change a list's **length** — the fusions — stay on the top-level chain,
so a stage carrying an `Of` binding is not fused with its neighbours — except
through the pair rules, below. It is
the measured-argument bargain in a different shape: a value that is not known
until data arrives stands the length-changing rewrites down.

**Adjacent-pair rules do see through a one-stage Consider**, for the shape that
is sound: the wrapped stage is the *first* of the pair and the rule answers with
one node. The binding is computed from the value entering the Consider — the
wrapped stage's input — and the replacement takes the stage's place inside the
scope, so the binding is still computed first, once, from the same value. The
scope then also covers the second stage, so that stage must not mention any
binding name (an enclosing binding of that name would be captured); anything the
check cannot read counts as a mention. A binding on the *second* stage is never
crossed — fusing the first stage into it would feed the binding the first
stage's input instead of its output — and a rule that answers with no node or
several is declined.

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

- `item(sort(xs), k)` for k > 0 in the expression layer → a selection. The
  stage form (`Sort` + `Take Item k`) is pass 5, and k = 0 is done above.
- `Fold → Product` (blocked: `Product` seeds from the first element and
  errors on empty, but `Fold Seed: 1` returns 1 — semantics differ on empty
  input).
- Common-subexpression elimination of *partial* expressions that have no dominating binding (pass-32 family shares only let-bound ones and total ones).
- **Common prefix hoisting across `Part` blocks.** Two Parts that start with
  the same operations could share one computation. This is CSE over
  sub-pipelines and wants its own design — the naive version would have to
  prove the shared prefix total, since discarding a failing computation would
  swallow an error the naive program reports.
- **Part-local dead code.** A `Part` whose body never `Reveal`s computes
  nothing observable and could be dropped entirely under `--release`. Today
  it is a lint warning, which is the right first step.
- **Length-changing passes inside `Consider` `Of` bodies.** `Channel`, `Part`,
  the loops and `Using:` bodies re-read their node list at run time, so every
  pass reaches inside them (`optimizer.runInBodies`). An `Of` body is still held
  by a closure that does not.
- **Fusing a `Map Each` body into the stage after it.** `Map Each` (body A) +
  `Sum By` (body B) is one loop over A then B, and concatenating the two bodies
  would put A's last stage beside B's first — which is where the pattern a pass
  is looking for often is (AoC 2017 day 4 part 2 builds every pair's flag in
  one body and asks `Any` about them in the next). It needs a totality analysis
  for a *body*, since interleaving the two changes which failure is reported
  first.
- **A stage carrying a `Consider … Of` binding is invisible to its
  neighbours.** It sits alone inside the Consider node, so no adjacent-pair
  pass sees it. Pass 46 looks one level down for exactly this; the general fix
  is for `rewritePairs` to.
- **`Window k` read only by position** — `first(w)`, `last(w)`, `item(w, c)` —
  for any `k`, including a measured one. Pass 42 is the `k = 2` case; the
  general one never builds a window at all and turns O(n·k) into O(n).
