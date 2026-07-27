# v0.5 — vocabulary: the expression layer, Maps, and the searches that aren't grids

> **Status: implemented, bar the items in [§Still open](#still-open).** What
> shipped, what changed on contact with the code, and what remains are
> recorded in [§Outcome](#outcome) at the end. The body below is the original proposal, kept as written so the
> reasoning that drove the work stays legible.

This was the
"separate proposal" the v0.4 design deferred to
([2026-07-26-v04-ergonomics-design.md](2026-07-26-v04-ergonomics-design.md),
§Non-goals: *"No new expression-layer builtins (`mod`, `let`, text functions
and the Map/Set escape hatches are a separate proposal)"*).

v0.4 was about the parts of writing Domain that aren't the vocabulary. This is
the vocabulary. Every item below is a gap found by reading the current docs
against the current code and asking what a solve has to do instead — where a
program in `challenges/` or `examples/` already works around the absence, that
program is cited as the evidence.

The items are grouped by how much they cost, because that is the honest way to
prioritize them:

| Tier | What it takes | Items |
|---|---|---|
| [1](#tier-1--the-expression-layer-is-too-small) | A builtin: typecheck + eval + codegen + oracle test (`expressions.md` §Design rules) | 1–6 |
| [2](#tier-2--primitives-that-close-a-known-shape) | A primitive: catalog entry, resolve, Eval, codegen case, docs | 7–13 |
| [3](#tier-3--abilities-that-need-their-own-design) | New IR, or a change to how bodies are held | 14–20 |

Tier 1 is where the leverage is. Six of them are pure table entries and no new
type, and between them they remove every workaround listed in this document's
evidence column.

---

## Tier 1 — the expression layer is too small

The pipeline layer's catalog runs to seventy-odd entries. The expression layer
has 61 builtins, but almost none are about `Text`, `Map`, or building a
composite — so the
moment a solve needs to look *inside* a value rather than reshape a list, it
falls off a cliff and has to climb back to the pipeline layer.

### 1. `mod` — and a `%` operator

```
mod(a, b)        Int × Int -> Int      # Euclidean: result has b's sign, or always ≥ 0
a % b                                  # same, as an operator at precedence 5
divmod(a, b)     Int × Int -> (Int, Int)
```

**The gap is not subtle.** `challenges/01_fizzbuzz.domain` spells divisibility
three times as `n / 15 * 15 = n`, and its header comment says so. Modular
arithmetic is *the* AoC recurrence: wrap-around grids, cycle lengths, the
`modpow`/`modinv`/CRT family that Domain already ships builtins for but gives
no way to reduce into.

Pick **Euclidean** (`mod(-7, 3) = 2`), not Go's truncated `%`, and say so
loudly in the table — wrap-around indexing (`mod(i - 1, length(xs))`) is the
dominant use and truncated remainder gets it wrong at exactly the interesting
boundary. `divmod` earns its place separately: base conversion and
digit-splitting want both halves and shouldn't compute the division twice.

*Optimizer note:* `mod(x, 2^k)` → `band(x, 2^k - 1)` is a one-line strength
reduction for the expression-simplification pass, and `n / k * k = n` →
`mod(n, k) = 0` is a natural lint/`expansion: optimize` rewrite that would fire
on FizzBuzz today.

### 2. Text is opaque inside a lambda

`length` is list-only (`typecheck.go:168` calls `needList`). There is no
substring, no character access, no case folding, no prefix test. Inside a
`Using:` lambda, a `Text` value supports `toint`, `occurrences`, `repeats`,
`frombin`, and equality — and nothing else. Any other question about a string
requires climbing back to the pipeline layer and `Split Text by ""`.

```
length(s)                Text -> Int                 # overload the existing name
slice(s, lo, hi)         Text × Int × Int -> Text     # half-open, clamped (total, like take/drop)
charat(s, i)             Text × Int -> Text           # 1-rune Text; error out of range
chars(s)                 Text -> List<Text>           # the Split "" of the expression layer
indexof(s, sub)          Text × Text -> Int           # -1 when absent, matching Find Index
startswith / endswith    Text × Text -> Bool
replace(s, old, new)     Text × Text × Text -> Text
trim(s)                  Text -> Text
upper(s) / lower(s)      Text -> Text
textjoin(xs, sep)        List<Text> × Text -> Text    # the expression-layer Join
```

Keep them **total** where the list equivalents are total — `slice` clamps the
way `take`/`drop` clamp, `indexof` returns `-1` the way `Find Index` does — so
the two layers answer "absent" the same way. `charat` is the exception and
errors like `item`, its exact analogue.

Runes, not bytes, at every index: `Split Text by ""` already splits into runes,
and having the two layers disagree about what position 3 means would be worse
than having no `charat` at all.

### 3. A `Map` you cannot read

`Group By` and `Count By` are two of the most reachable primitives in the
language, and they both produce a `Map`. The expression layer offers exactly
one Map builtin: `get(m, k)`, which **errors when the key is absent**. There is
no `haskey`. So a frequency map cannot be safely queried at all — the guard
idiom that works for lists (`length(xs) = 0 or first(xs) > 2`) has no Map
spelling, because there is nothing to guard with.

```
haskey(m, k)      Map<K,V> × K -> Bool
getor(m, k, d)    Map<K,V> × K × V -> V      # total lookup; the workhorse
keys(m)           Map<K,V> -> List<K>
values(m)         Map<K,V> -> List<V>
size(m)           Map<K,V> | Set<T> -> Int   # Count, without leaving the lambda
```

`getor` is the one that changes what programs can be written; the other four
are the escape hatches that let a Map flow back into the list vocabulary the
rest of the language is built on.

Sets have the mirror-image problem and only need one entry: **a `Set<T>` is a
dead end today.** `Map Each` has no Set case (nothing in `prims/functional.go`
handles `ir.KSet`), so after `Convert To Set` the only moves left are `Count`,
`contains`, and `Difference`. Either add `tolist(s)` as a builtin or let the
list primitives accept a Set — §10 argues for the latter.

### 4. `not`

The precedence table (`expressions.md`) has `or`, `and`, and the comparisons.
There is no negation. `Filter` with an inverted predicate means rewriting the
predicate by hand, De Morgan and all, and a lambda parameter of type
`(Int) -> Bool` cannot be negated at its use site at all — the caller has to
pass a different lambda.

```
not e            Bool -> Bool     # precedence 2.5: binds tighter than `and`, looser than `=`
```

One token, one AST node, one eval case, one codegen case. It is the cheapest
item in this document.

### 5. You cannot build a composite

`list(a, b, …)` requires all elements to share one type. `point(r, c)` is
`Int × Int` only. That is the complete set of constructors — so:

- a heterogeneous pair (`(Text, Int)`) **cannot be written**, which means
  `Group By` cannot key on a mixed compound and `Count By`'s output cannot be
  re-keyed;
- a `Fold` whose accumulator is a small struct **cannot be written**, because
  `Seed:` takes an Int or Text literal and there is no record constructor. The
  Day 5 fold in `primitives.md` gets away with a `List` accumulator precisely
  because its state is homogeneous;
- `Match Pattern` is the *only* source of a `Record` in the entire language.

```
tuple(a, b, …)               T1 × T2 × … -> (T1, T2, …)     # heterogeneous, ≥ 2 args
record(name: e, …)           -> {name: T, …}                # ordered, field names literal
with(r, name: e, …)          {…} × … -> {…}                 # functional field update
```

`tuple` is nearly free — tuples are already `[]Value` at runtime and comparable
generated structs in codegen, which is exactly what `point` compiles through.
`record`/`with` cost more (codegen interns a struct per record shape via
`ir.Memo`, and a literal-keyed constructor needs its own argument form in the
expression grammar), and they are what unlock stateful folds. If only one
ships, ship `tuple`.

*Depends on:* nothing. *Unlocks:* §5 is the precondition for §14's memoized
recursion carrying anything but a scalar.

### 6. `let` — local bindings

```
let d = manhattan(a, b) in if d < 3 then d else 0
```

There is no way to name a subexpression. The workaround is to write it twice
and let the optimizer notice — except "common-subexpression elimination inside
lambda bodies" is a *candidate* pass in `optimizer.md`, not an implemented one,
so today it really is computed twice. Long AoC predicates get genuinely
unreadable without this.

Two spellings are plausible and the choice matters:

- **`let x = e in body`**, an expression form. Composes anywhere, nests, and
  the compiler lowers it to a Go local — no closure, consistent with how
  `if/then/else` already lowers to an inlined `if`.
- **A `Where:` argument** beside `Using:`, so bindings are a pipeline-layer
  concept. Reads better for a wide lambda but does not nest and cannot appear
  inside a conditional arm.

Recommend the expression form. It is the one that composes, and it makes the
deferred CSE pass an optimization rather than a load-bearing requirement.

---

## Tier 2 — primitives that close a known shape

### 7. `Sort` cannot sort text

`Sort` is `List<Int> -> List<Int>` and `Sort By` takes an `Int` key
(`prims/catalog.go:147`). So **sorting a `List<Text>` alphabetically is
impossible**, and so is sorting by a Text field, or by a tuple key, or by two
keys with a tiebreak. This is a surprising hole in a language that ships
quickselect fusion.

```
Domain Expansion: Sort              # accept List<Text> and List<Float>; total order per type
Domain Expansion: Sort By           # accept any ordered key: Int, Float, Text, or a tuple of them
    Using: (r) -> tuple(r.group, -r.score)
```

Tuple keys are lexicographic, which is how a tiebreak gets written without a
second pass — and they depend on §5's `tuple`. Float sorting inherits the
existing "float pipelines are exempt from int-specialized rewrites"
(`data-model.md` §Floats) rule; Text sorting is stable and can fuse into
quickselect exactly as Int sorting does.

### 8. Map pipeline operations

A `Map` can be produced (`Group By`, `Count By`) and rendered, and that is
close to all. The single most common AoC follow-up — *"which key occurred
most?"* — has no spelling at all.

```
Cursed Technique: Map Values        # Map<K,V> × (V -> W) -> Map<K,W>   (Group By, then reduce each bucket)
Cursed Technique: Filter Entries    # Map<K,V> × ((K,V) -> Bool) -> Map<K,V>
Channeled Energy: Convert To Entries    # Map<K,V> -> List<(K,V)>
Channeled Energy: Convert To Map        # List<(K,V)> -> Map<K,V>  (last write wins, documented)
```

`Convert To Entries` is the important one: it drops a Map back into the list
vocabulary, where `Sort By` (§7, keyed on `pcol`-style tuple access) and
`Select Top K` already live. `Count By` → `Convert To Entries` → `Sort By
Descending` → `Select Top 1` is the whole "most common element" idiom, and the
existing quickselect fusion fires on it for free.

`Map Values` deserves its own node rather than being expressed through
entries — `Group By` then `Map Values (b) -> sum(b)` is a two-line spelling of
an aggregation the optimizer could later fuse into a single grouped pass.

### 9. A general `Binding Vow`

`prims/vow.go:102` says it plainly: *"unsupported Binding Vow (v0.1 supports
'Count Equals N' and 'All Values <cmp> N')"*. Two shapes, both about lists of
ints, both bounded by literal integers. Meanwhile the expression layer can
express any predicate at all.

```
Binding Vow: Holds
    Using: (v) -> rows(v) = cols(v)
```

The vow machinery — passthrough node, abort with the vow text and the offending
value, stripped under `--release` — already exists and is type-agnostic. This is
a resolve case and a one-line Eval, and it makes vows useful on grids, maps,
records, and sparse planes instead of only `List<Int>`.

### 10. Grid geometry beyond `Transpose`

`Transpose` is the only structural grid transform. Rotation, reflection, and
cropping are all standard AoC moves (tile-matching, folding, region extraction)
and all currently require densify-round-trips through lists.

```
Cursed Technique: Rotate Grid        Mode: Right | Left | Half
Cursed Technique: Flip Grid          Mode: Horizontal | Vertical
Cursed Technique: Subgrid r c h w    # crop; out-of-bounds is an error
Cursed Technique: Pad Grid n         Fill: "."
Channeled Energy: Convert To Rows    # Grid<T> -> List<List<T>>, the inverse of Convert To Grid
```

`Convert To Rows` is the quiet one: `Convert To Grid` is a one-way door today,
so anything a grid needs that the grid primitives don't have cannot be reached
by dropping back to lists. Note `Rotate Right` = `Transpose` + `Flip
Horizontal`, so the optimizer gets a free algebraic identity for the pair.

While here: the list vocabulary should accept a `Set<T>` wherever it accepts a
`List<T>` for a pure read (`Map Each`, `Filter`, `Count Matching`, `Sort`) —
insertion order is already the documented iteration order, so the semantics are
unambiguous, and it closes §3's dead end without a new builtin.

### 11. Points that don't need a grid

`neighbors4`/`neighbors8` require `args[0].Kind == ir.KGrid`
(`typecheck.go:389`). **A `Sparse<T>` cannot ask for its neighbors** — which is
the one type whose whole purpose is unbounded cellular automata. The Game of
Life challenge works around it, and `dirs4()` + `padd` is the manual spelling
every sparse program has to re-derive.

```
psub(p, q)          (Int,Int) × (Int,Int) -> (Int,Int)
pscale(p, n)        (Int,Int) × Int -> (Int,Int)       # step n times along a direction
chebyshev(p, q)     (Int,Int) × (Int,Int) -> Int       # 8-connectivity distance
dirs8()             -> List<(Int,Int)>                 # dirs4 already exists; diagonals don't
around4(p) / around8(p)   (Int,Int) -> List<(Int,Int)> # neighbors with no grid, no bounds
```

`around8` is what makes a sparse automaton readable, and `pscale` + `psub` are
what make direction vectors compose (`padd` alone can only step by one).

### 12. `Reveal` has one target and one format

```
Reveal: stderr                      # debugging output that doesn't pollute the answer
Reveal: stdout
    As: Lines | Grid | Compact      # explicit rendering, instead of the type's default
```

Minor, but `stderr` in particular makes a mid-pipeline `Reveal` a debugging
tool rather than a thing that breaks the golden test. Rendering stays
deterministic per `data-model.md`; `As:` only selects among renderings that
already exist.

### 13. `Find Cycle`

```
Cursed Technique: Find Cycle        List<T> -> (Int, Int)   # (first index of the repeat, period)
```

`Iterate n` produces a trajectory precisely so a program can ask *"have I been
here before?"* — `primitives.md` says exactly that. But the asking has no
primitive: `Find Index` needs a predicate over one element, not a
seen-set-over-the-prefix. And the answer is what turns "run this 1,000,000,000
times" into arithmetic, which is a whole genre of AoC part 2.

Requires `T` keyable, errors (or returns `(-1, 0)`) when the trajectory doesn't
repeat, and pairs naturally with a `Cycle Skip` helper in the prelude.

---

## Tier 3 — abilities that need their own design

These are not table entries. Each one wants a spec of its own; they are listed
here because the shape of the gap is clear even though the fix isn't.

### 14. There is no recursion, so there is no DP

Shikigami are **inlined**, with a depth limit as the recursion guard
(`language.md` §Shikigami). So a Shikigami cannot call itself, the expression
layer has no user-defined functions, and the loop constructs all thread a
single value. Any AoC problem whose part 2 is *"now memoize it"* — counting
paths, splitting stones, arrangement-counting — is currently inexpressible, not
merely awkward.

The runtime already has the missing half: `ir.Memo`, described in
`aoc-toolbox.md` as *"compute-once keyed caching for primitive implementers…
Pipelines themselves are pure, so user programs don't memoize explicitly."*
That last sentence is the limitation, stated as a design choice.

Two directions worth specifying:

- **A recursive Shikigami with a mandatory signature.** A declared signature is
  currently a check, explicitly *not* a compilation boundary. Making it one —
  when and only when the body is self-referential — gives codegen a real Go
  function to emit and a natural place to hang an `ir.Memo` keyed on the
  (keyable) input. The cost is that a recursive Shikigami stops receiving
  optimizer rewrites through inlining, which is a defensible trade and needs to
  be documented as one.
- **`Domain Expansion: Explore` — a worklist over states.** Seed state, an
  expansion lambda `(s) -> List<S>`, and a mode (`Count`/`Collect`/`Reach`),
  with memoization on the keyable state. This stays declarative, fits the
  "named algorithm the optimizer may substitute" contract, and is the shape
  most AoC DP actually has. It generalizes §15 too — BFS over an implicit graph
  *is* this with a distance accumulator.

Recommend `Explore`. It keeps the "no user-visible recursion" property that
makes the compiler backend simple, and it is one node rather than a change to
what a Shikigami is.

### 15. Every search is a grid search

`BFS`, `Dijkstra`, `Flood Fill` and `Connected Components` all take a
`Grid<T>`, all use 4-connectivity, and all return a `Grid<Int>`. The toolbox
page claims *"if a solve seems to need an explicit queue, it is usually one of
the search primitives wearing a disguise"* — true for grids, and not true at
all for the graph half of AoC, where nodes are names in a text file.

Four separable gaps:

```
Domain Expansion: BFS               From: graph        # Map<K, List<K>> -> Map<K, Int>
    Start: <the current value>
Domain Expansion: Dijkstra          Using: (a, b) -> …  # weight lambda, not cell-entry cost
Domain Expansion: Shortest Path from R C to R2 C2       # -> List<(Int,Int)>, the path itself
Domain Expansion: Topological Sort                      # Map<K, List<K>> -> List<K>; error on a cycle
Mode: 4 | 8                                             # on every grid search
```

The path-returning variant matters more than it looks: today a program can
learn the *distance* to a cell but not *how it got there*, so any "describe the
route" question is out of reach. `Mode: 8` is nearly free — the neighbor walk
is already parameterized in `ir/`.

### 16. Channels cannot nest or see each other

A `Channel` is the only naming mechanism in the language, and `language.md`
states the two limits: channels cannot nest, and a channel body cannot consume
another channel with `From:`. So a value derived from two channels cannot
itself be named — it must be recomputed at each consumer, or the pipeline has to
be restructured around it.

Allowing a channel body to use `From:` over channels declared **above** it is
the smaller, obviously-terminating half of this (declaration order gives the
DAG for free, and a cycle is a resolve error naming the chain, exactly like the
import cycle check). Nesting is the larger half and can wait.

### 17. Signatures are monomorphic

Documented in `language.md`: no type variables, so a genuinely polymorphic
Shikigami (`List<T> -> T`) declares nothing and gets no call-site checking.
Since the v0.4 outcome notes that annotating the prelude *"improved the most
common error in the language"*, the polymorphic definitions are exactly the
ones still missing that improvement.

Rank-1 prefix-quantified variables (`: List<T> -> T`, unified at each call
site, no inference beyond matching the argument) would cover every case the
prelude and a user library actually need, and it does not change inlining —
the check happens before substitution, which is already where signature
checking lives.

### 18. `For` loops don't compile

`language.md` says it: *"Interpreter only for now — `domain build` reports the
loop kind as unsupported."* The repo's stated discipline is that every
primitive works in both backends with oracle-pinned identical output, and this
is the one advertised exception. Closing it is a codegen case for the ambient
parameter binding, not a design question — but it should be closed before more
surface is added on top of it.

### 19. Optimizer passes these unlock

Recorded here so `optimizer.md`'s candidate list can absorb them:

- `mod(x, 2^k)` → `band(x, 2^k - 1)`; `n / k * k = n` → `mod(n, k) = 0`.
- `Range` (§20) + `Map Each` → a counted loop that materializes nothing.
- `Group By` + `Map Values` (§8) → one grouped aggregation pass.
- `Rotate Right` ≡ `Transpose` + `Flip Horizontal` (§10) — pick the cheaper.
- `Convert To Entries` + `Sort By` + `Select Top K` (§8) → the existing
  quickselect fusion, reached from a Map for the first time.
- `let` (§6) makes the deferred lambda-CSE pass an optimization rather than a
  correctness-adjacent requirement.

### 20. Smaller things, listed for completeness

- **A range generator.** `challenges/01_fizzbuzz.domain` builds `1..15` with a
  `Repeat 14` loop appending `length(xs) + 1`, and its comment says *"Domain
  has no range generator"*. `For x in range(N)` already parses inline, so the
  concept exists in the grammar and just isn't reachable as a value:
  `Cursed Energy: range(1, 15)` or `Cursed Technique: Range 1 15`.
- **`Sum`/`Min`/`Max` over `Map` values** — falls out of §8 for free.
- **`min(a, b)` / `max(a, b)` on two scalars.** Both names are taken by the
  list forms; the two-argument overload is unambiguous and constantly wanted.
- **`pow(b, e)` and `isqrt(n)`.** `modpow` exists but unmodular exponentiation
  does not, and `sqrt` returns a `Float` that has to be rounded and re-checked.
- **`Reverse` for `Text`.** `reverse` is list-only; palindrome checks currently
  round-trip through `Split Text by ""`.
- **Big integers.** Deferred by an explicit decision (`README.md` §Known
  limitations, `data-model.md` §Out of scope). Left deferred — noted only so
  this document's absence of it is deliberate.

---

## What to do first

If exactly one thing ships: **§1 `mod`**. It is one builtin, it deletes a
workaround from a shipped challenge program, and it brings the existing
`modpow`/`modinv` builtins into reach.

If one *group* ships, take Tier 1 whole. Items 1–6 are six builtins and one
operator, they share the same three-layer implementation path
(`expressions.md` §Design rules), they need no new `ir.Type` kind and no
optimizer changes, and together they close the gap that makes the expression
layer the limiting factor: today a lambda can reshape a list but cannot look
inside a `Text`, safely read a `Map`, build a pair, negate a predicate, or name
an intermediate.

Tier 2 is best sequenced §8 → §7 → §11, because Map entries and text-keyed
sorting compose into the "most common element" idiom that every other item
keeps gesturing at.

Tier 3 items are independent of each other and of everything above, except that
§14's `Explore` should be specified after §5's `tuple` lands — its state values
want to be composite, and today they cannot be written.

---

## Outcome

Most of the document shipped. The expression layer went from 61 builtins to
92; nine primitives were added; three language-level limits were removed. Every
addition lands in all three layers (typecheck, eval, codegen) with unit tests
and an interpreter-vs-binary oracle program in both optimizer modes, per
`expressions.md`'s design rules.

### Shipped

| § | Item | Notes |
|---|---|---|
| 1 | `mod`, `%`, `divmod` | Euclidean, as argued |
| 2 | Text access | `length`/`slice`/`charat`/`chars`/`indexof`/`startswith`/`endswith`/`replace`/`trim`/`upper`/`lower`/`textjoin`, plus `+` as concatenation. Rune-indexed |
| 3 | Map/Set escape hatches | `haskey`, `getor`, `keys`, `values`, `size`, `tolist` |
| 4 | Negation | Spelled **`ikke`**, not `not` |
| 5 | `tuple` | Shipped; `record`/`with` deferred |
| 6 | Local bindings | Spelled **`consider n as v in body`**, not `let` |
| 7 | Ordered `Sort` / `Sort By` | Via new `ir.Ordered`/`ir.Compare`; tuple keys are tiebreaks |
| 8 | Map pipeline ops | `Map Values`, `Filter Entries`, `Convert To Entries`, `Convert To Map` |
| 9 | General vow | `Binding Vow: Holds` with a `Using:` predicate |
| 10 | Grid geometry | `Rotate Grid`, `Flip Grid`, `Convert To Rows`, `Subgrid`, `Pad Grid` |
| 10 | Sets as lists | The `listElem` family accepts a `Set`; the result is a List |
| 11 | Grid-free points | `psub`, `pscale`, `chebyshev`, `dirs8`, `around4`, `around8` |
| 12 | `Reveal: stderr` | Shipped; the `As:` rendering selector is not |
| 13 | `Find Cycle` | `(start, period)`, `(-1, 0)` when there is no repeat |
| 14 | `Explore` | The recommended `Explore`, not recursive Shikigami |
| 15 | `Mode: 4 \| 8` | On BFS, Dijkstra, Flood Fill and Connected Components |
| 15 | `Topological Sort` | Adjacency map *or* edge list, deterministic ties |
| 16 | Channel composition | A Channel body may consume channels declared above it |
| 18 | `For`-loop codegen | Closed — no advertised backend gap remains |
| 20 | `Range` generator | Half-open, matching `range(N)` in a For header |
| 20 | `Reverse` over `Text` | Both the primitive and the `reverse` builtin |
| 19 | Optimizer | `isTotal` learned `%` and the new total builtins; passes unchanged otherwise |

### Where the proposal was wrong, or the request overrode it

1. **The names.** `not` became `ikke` and `let` became `consider … as … in …`,
   at the author's direction. Both are contextual identifiers, like
   `if`/`then`/`else`, so neither reserves a word.

2. **Removing limits is not free.** §14 argued only about the inlining depth,
   but the instruction was to remove hard-coded limits everywhere. Deleting the
   4,000,000-cell sparse densify ceiling outright turned a clean error into a
   `makeslice` panic — strictly worse. The guard is now about
   *representability*, not policy: a box Go cannot allocate on any machine
   still fails with a message. Refusing work that could succeed was the
   problem; crashing on work that cannot is not an improvement.

3. **Recursion needed a termination argument, not just a bigger number.**
   Removing the depth counter without replacing it would make a self-recursive
   Shikigami loop forever at resolve time. Cycle detection over the inlining
   chain removes the arbitrary constant *and* keeps termination, and it reports
   the cycle by name (`Ping -> Pong -> Ping`) instead of blaming depth.

4. **`Topological Sort`'s useful input shape was not the proposed one.** §15
   assumed `Map<K, List<K>>`, but `Group By` over a parsed edge file produces
   `Map<K, List<Record>>` and the expression layer has no way to map a list
   inside a lambda — so the adjacency form is nearly unreachable from a parse.
   It accepts an **edge list** too, which is exactly what a positional
   `Match Pattern` produces, following `Merge Ranges`' precedent of taking
   several shapes.

5. **`Sort` needed a stability change in codegen.** Over a tuple element the
   comparison is not a strict total order on ties, so `sort.Slice` (unstable)
   would permute equal elements differently from the interpreter.
   `emitSort` now uses `SliceStable`.

6. **The ambient-parameter problem had a cheaper solution than expected.**
   Compiling `For` bodies looked like it needed a parameter threaded through 48
   lambda-compilation sites. It did not: the trailing parameters are *always*
   exactly the ambient ones, so their count equals the ambient depth and no
   primitive's own arity matters. One choke point (`nodeLambda`) plus one
   fallback in `compileExpr`'s `Ident` case covers everything.

### Bugs found while testing

- **A lambda that ignores its parameter did not compile.** `Apply Using: (s) -> 5`
  is legal Domain, but left the upstream Go variable declared and never read.
  `emitSequence` now blanks a previous variable the emitted node never
  mentioned, checked textually against exactly that node's output.
- **A bare integer literal as the pipeline value did not compile.** It is an
  untyped Go constant, so `:=` made it `int`, which then failed to assign where
  an `int64` was expected. `emitApply` declares the variable with its type.
- **`prims`' ambient stack is not concurrency-safe**, as `prims/ambient.go`
  documents. Parallel subtests that resolve `For` bodies interleave their
  pushes and mis-count lambda arity; the `For` parity suite runs serially. This
  affects tests only — a build or a run is one resolve per process.

### A third bug found while testing

- **`Reveal: stderr` first leaked into stdout.** A nil `Context.Stderr`
  originally fell back to `Stdout` so a caller supplying one writer "still saw
  everything". That broke backend parity immediately: a harness capturing only
  stdout got the stderr line from the interpreter and not from the binary. Nil
  now discards, exactly as a nil `Stdout` already does — never cross the
  streams, or the two backends disagree about what a program printed.

### Still open

Each of these wants design work rather than more of the same:

- **§5 records** — `record(...)` / `with(...)`. `tuple` covers the positional
  case; named fields need a new argument form in the expression grammar and
  per-shape struct interning in codegen.
- **§12** the `As:` rendering selector on `Reveal`.
- **§15 the rest of graph search** — a weight lambda for Dijkstra and a
  path-returning shortest path. `Explore` covers non-grid reachability,
  `Topological Sort` the dependency-order question, and `Mode: 4 | 8` has
  shipped.
- **§16 nesting** — a Channel still cannot contain another Channel. The
  smaller half (consuming earlier channels) has shipped.
- **§17 polymorphic signatures** — rank-1 type variables in a Shikigami's
  declared type.
- **§20** `Sum`/`Min`/`Max` directly over a Map's values (`Convert To Entries`
  reaches them today), and big integers, which stay deliberately deferred.
