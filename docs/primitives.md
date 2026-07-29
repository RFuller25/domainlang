# Primitive reference

Every operation in Domain is either a primitive (implemented in Go, listed
here) or a Shikigami composed from primitives. Types are checked at resolve
time: each primitive states the input type it requires and the output type it
produces, and a mismatch is a positioned error before anything runs.

Notation: `T`, `U`, `K` are type variables; "keyable" means `Int`, `Text`,
or a Tuple/Record built from keyable types (so points key Maps and Sets)
(the legal Map key / Set element types). Lambda-consuming primitives infer
their output types from the lambda body via the typechecker.

Every primitive below is supported by **both** backends (interpreter and
compiled binary) with identical success output — the AoC-toolbox additions
included, each pinned by an interpreter-vs-binary oracle test. See
[compiler.md](compiler.md).

---

## Cursed Energy — sources

### Read Source — `(nothing) -> Text`

```domain ignore
Cursed Energy: input.txt
Cursed Energy: stdin
```

Must be the first stage. Reads the named file; if the file does not exist,
falls back to stdin (so `domain prog.domain < input.txt` works without the
file present). An empty target or `stdin` reads stdin directly. A trailing
run of `\r`/`\n` is trimmed (typical AoC inputs end with one newline).
Relative paths resolve against the program file's directory when
interpreting, and against the working directory in a compiled binary (a
documented delta — see [compiler.md](compiler.md)).

---

## Cursed Technique — transforms

### Split — `Text -> List<Text>`

```domain
Cursed Technique: Split Text by "\n\n"
```

Splits on the required separator string. An empty separator (`""`) splits
into individual characters (runes). The separator is a
[measured argument](#measured-arguments) (`By:`), so a program that has to
look at the text before it knows how to split it can:

```domain
Cursed Technique: Split
    By: (t) -> if indexof(t, "\t") >= 0 then "\t" else ","
```

### Split Each — `List<Text> -> List<List<Text>>`

```domain
Cursed Technique: Split Each by "\n"
```

`Split`, applied to every element.

### Map Each — `List<T> × (T -> U) -> List<U>`

```domain
Cursed Technique: Map Each
    Using: (m) -> m.n
```

**The `Using:` may be written as an indented pipeline** instead of a lambda,
which is how a per-element job reaches a *primitive* — the expression layer
cannot iterate, so there is no lambda that searches an element which is itself
a list:

```domain
Cursed Technique: Map Each          # List<List<Int>> -> List<Int>
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> a + b = 2020
    Maximum Technique: Product
```

This is not a `Map Each` feature: a body stands in wherever a 1-parameter
`Using:` lambda is accepted. See
[pipeline bodies](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)
for the rule, the primitives it reaches, and its limits.

### Filter — `List<T> × (T -> Bool) -> List<T>`

```domain
Cursed Technique: Filter
    Using: (x) -> x > 2
```

The lambda must be a predicate (return `Bool`).

### Unique — `List<T> -> List<T>` (T keyable)

Order-preserving deduplication (first occurrence wins).

### Match Pattern — `Text -> V` or `List<Text> -> List<V>`

```domain
Cursed Technique: Match Pattern
    Mode: Each                       # or One; inferred from input if omitted
    Using: "{a:int}-{b:int},{c:int}-{d:int}"
```

Parses each line against a typed-hole template. Named holes produce a Record
(`V = {a:Int, b:Int, ...}`); positional holes produce a `List<T>` when all
holes share one type, otherwise a Tuple. Hole types: `int`, `word`
(non-space run), `text` (rest of field). Named and positional holes cannot
mix. A non-matching input line is a runtime error naming the line and the
template. Full template grammar: [match-pattern.md](match-pattern.md).

### Split Fields — `Text -> List<Text>` or `List<Text> -> List<List<Text>>`

```domain
Cursed Technique: Split Fields
```

Splits on runs of whitespace (spaces and tabs), discarding empty fields —
the classic `fields()` helper. The form is chosen by the input type: bare
`Text` gives one field list; `List<Text>` splits every line.

### Extract Integers — `Text -> List<Int>` or `List<Text> -> List<List<Int>>`

```domain
Cursed Technique: Extract Integers
```

Mines every integer out of messy text — the AoC "parse ints off the line"
workhorse: `"move 12 from -3 to 5"` yields `[12, -3, 5]`. A leading `-`
negates **unless** it directly follows a digit, so `"36-92"` yields
`[36, 92]` (the `-` separates) while `"x=-5"` yields `[-5]`. The form is
chosen by the input type, like `Split Fields`.

### Find Cells — `Grid<T> | Sparse<T> × (T -> Bool) -> List<(Int, Int)>`

```domain
Cursed Technique: Find Cells
    Using: (c) -> c = "X"
```

The `(row, col)` positions of every cell satisfying the predicate, in
row-major order. The positions are points — `(Int, Int)` tuples — ready for
the point builtins (`prow`/`pcol`/`manhattan`/…, see
[expressions.md](expressions.md)). Over a `Sparse<T>` only the *set* cells
are visited (sorted row-major); the default never matches.

### Ragged Columns — `List<Text> -> List<List<Text>>`

```domain
Cursed Technique: Ragged Columns
```

The character columns of a block of lines, top to bottom, tolerating
unpadded (ragged) line lengths by skipping the cells short lines don't have
— the classic move for fixed-column drawings like AoC 2022 Day 5's crate
stacks, whose crates for stack k live in column `4(k-1)+1`. See
`testdata/day5_full.domain` for the full worked parse.

### Window — `List<T> -> List<List<T>>`

```domain
Cursed Technique: Window 3      # sliding windows of 3, step 1
Cursed Technique: Window 2 2    # non-overlapping pairs (step 2)
```

Fully-contained sliding windows (a list shorter than the window yields
none). Size and step must be ≥ 1. The 2021 D1 idiom is `Window 2` +
`Count Matching (w) -> last(w) > first(w)`.

Size and step are **measured arguments**: either the literal above, or
`Size:`/`Step:` holding a lambda over the current list (see
[below](#measured-arguments)).

```domain
Cursed Technique: Window
    Size: (xs) -> length(xs) / 2     # windows half the list long
```

### Measured arguments

An argument a phrase takes as a literal may instead be written as an indented
named argument holding a **lambda over the current value**, so it can depend on
the data flowing through the pipe rather than on the source:

| Primitive | phrase form | measured form |
|---|---|---|
| `Window` | `Window SIZE [STEP]` | `Size:`, `Step:` |
| `Chunk` | `Chunk SIZE` | `Size:` |
| `Sliding Reduce` | `Sliding Reduce SIZE [STEP]` | `Size:`, `Step:` |
| `Select Top K` | `Select Top K` | `Count:` |
| `Take Item` | `Take Item I` | `Index:` |
| `Iterate` | `Iterate N` | `Times:` |
| `Repeat` (`Simple Domain`) | `Repeat N` | `Times:` |
| `Range` | `Range [LO] HI` | `Low:`, `High:` |
| `Binding Vow: Count Equals` | `Count Equals N` | `Count:` |
| `Split` / `Split Each` | `Split Text by "SEP"` | `By:` (Text) |
| `Join` | `Join "SEP"` | `With:` (Text) |
| `Pad Grid` | — | `Fill:` (the cell type) |
| `Convert To Sparse Grid` | — | `Default:`, `Mark:` (the cell type) |
| `Fold` / `Scan` | — | `Seed:` (the accumulator type) |
| `Subgrid` | `Subgrid R C H W` | `Row:`, `Col:`, `Height:`, `Width:` |
| `Pad Grid` | `Pad Grid N` | `Thickness:` |
| `BFS` / `Dijkstra` / `Flood Fill` | `… from R C` | `Row:`, `Col:` |

The rules, which are the same for every measured argument:

- the lambda takes **one parameter, the whole current value** — the binding
  `Apply` gives its lambda — plus one trailing parameter per enclosing `For`
  loop, exactly as a `Using:` lambda does there. It must return the slot's own
  type — `Int` for a count, `Text` for a separator, the cell type for a fill or
  a default (which is checked against the value it has to match, exactly as a
  literal's type is);
- the same named slot also accepts a plain literal (`Size: 3`). A slot written
  **both** ways — `Window 3` with a `Size:` under it — is a resolve error
  rather than a silent win for either spelling;
- it is evaluated **once per execution of the statement**, before the
  primitive runs. Inside a loop that means once per lap, which is the point: a
  `Window` over a list that shrinks each lap re-measures each lap;
- an argument with no bound of its own is checked where it always was: a
  measured `Take Item` index is range-checked against the list (`index 99 out
  of range (length 3)`), and a measured `Range` pair against each other;
- bounds move with the value. `Window 0` is a resolve error as it always was;
  a `Size:` that *measures* 0 can only fail once it has been measured, so it
  is a runtime error naming what it measured and from what. It is an error and
  not a clamp — a window silently widened to 1 is a wrong answer that looks
  right. The guard is writable: `Size: (xs) -> max(1, length(xs) / 2)`;
- a measured argument is invisible to the optimizer's constant folding. The
  two rewrites whose fused nodes take the value as data (`Window` + a reduce,
  `Sort` + `Select Top K`) carry it through and still fire; the rewrites that
  are valid *because* of what the literal is stand down — see
  [optimizer.md](optimizer.md#measured-arguments-and-the-passes-that-fold-literals).

Arguments that *type* the program stay literal and always will: the `k` of
`Combinations k` (it fixes the `Using:` lambda's arity) and a `Match Pattern`
template (it fixes the output `Record`'s fields) are decisions the resolver
makes, not data.

A measured argument reaches a `Shikigami` through a lambda parameter — declare
it as the function the slot takes and hand it over (see
[language.md](language.md#parameters)).

### Flatten — `List<List<T>> -> List<T>`

Concatenates the groups in order.

### Enumerate — `List<T> -> List<(Int, T)>`

Pairs every element with its 0-based index. Over `List<Int>` the pairs are
points, so `prow`/`pcol` read index/value in a following lambda.

### Pairs — `List<T> -> List<(T, T)>`

```domain
Cursed Technique: Pairs
```

Every element tupled with the one after it (`zip xs (tail xs)`): `n` elements
give `n-1` pairs, and a list shorter than 2 gives none. Over `List<Int>` the
pairs are points, so a following lambda reads the two sides with
`prow`/`pcol` — the 2021 D1 "count the increases" idiom without a Window:

```domain
Cursed Technique: Pairs
Maximum Technique: Count Matching
    Using: (p) -> pcol(p) > prow(p)
```

`Window 2` covers the same ground with `List<List<T>>` elements; reach for
`Pairs` when you want a tuple (points, `Map`/`Group By` keys — tuples are
keyable, lists are not).

### Chunk — `List<T> -> List<List<T>>`

```domain
Cursed Technique: Chunk 3
```

Consecutive non-overlapping blocks of the given size, **keeping a short final
block**. That is the difference from `Window 3 3`, which drops a trailing
partial window — usually a bug rather than the intent. Size must be ≥ 1; a
list shorter than the size yields one block holding all of it. Size is a
[measured argument](#measured-arguments): `Size: (xs) -> length(xs) / 3`.

### Take While / Drop While — `List<T> × (T -> Bool) -> List<T>`

```domain
Cursed Technique: Take While
    Using: (x) -> x < 4        # [1, 2, 9, 3] -> [1, 2]
Cursed Technique: Drop While
    Using: (x) -> x < 4        # [1, 2, 9, 3] -> [9, 3]
```

The longest leading run all of whose elements satisfy the predicate, and
everything from the first failure onward. **They are not `Filter`**: both stop
testing at the boundary, so the `3` after the `9` above is neither taken nor
tested — a `Filter` would have kept it.

Together they split the list at one point: `Take While p` ++ `Drop While p` is
always the original.

### Partition — `List<T> × (T -> Bool) -> List<List<T>>`

```domain
Cursed Technique: Partition
    Using: (x) -> x > 2
Cursed Technique: Take Item 0   # the matches
```

A two-element list, `[matching, non-matching]`, each in the input's order. One
pass and one predicate evaluation per element, where a `Filter` and its
negation cost two of each.

The halves are reached the way input sections already are — `Take Item 0` /
`Take Item 1` in the pipeline, `first(p)` / `last(p)` inside a lambda.

### Iterate — `T × (T -> T) -> List<T>`

```domain
Cursed Technique: Iterate 5
    Using: (x) -> x * 2        # 1 -> [2, 4, 8, 16, 32]
```

The value after each of `n` applications of the step. A `Simple Domain:
Repeat n` loop threads a value through a body and keeps only where it ended
up; `Iterate` keeps the whole trajectory, which is what "had I been here
before?" needs. As with `Scan`, the starting value is not re-emitted, so the
result has exactly `n` elements and its last one is where the equivalent
`Repeat` would have finished.

The step must return its own input type — it has to be applicable again — but
that type can be anything, including a whole list or grid.

Not to be confused with `Simple Domain: Iterate Until Fixed Point`, the loop.

### Unfold — `T × (T -> Bool) × (T -> T) -> List<T>`

The dual of `Fold`: where a fold consumes a list into a value, an unfold grows
a value into a list.

```domain
Cursed Technique: Unfold
    While: (x) -> x > 1
    Using: (x) -> x / 2        # 20 -> [20, 10, 5, 2]
```

The current value is emitted while the `While:` predicate holds and advanced
by the `Using:` step, so a predicate that is false at the start gives the
empty list. Like the `Simple Domain` loops it is bounded: a step that never
falsifies the predicate fails loudly instead of hanging.

### Scan — `List<T> × Seed? × (Acc, T -> Acc) -> List<Acc>`

The running fold: `Fold` keeps only the final accumulator, `Scan` keeps them
all.

```domain
Cursed Technique: Scan            # seedless: Acc is the element type
    Using: (a, b) -> a + b        # [1,2,3,4] -> [1, 3, 6, 10]

Cursed Technique: Scan            # seeded: Seed: fixes Acc, as in Fold
    Seed: 100
    Using: (acc, x) -> acc + x    # [1,2,3] -> [101, 103, 106]
```

There is exactly **one result per input element** — the accumulator *after*
folding that element in — so the result stays index-aligned with the list it
scanned, and the seed is not re-emitted. Index `i` of the output is the fold
of the first `i+1` inputs, and the last element equals what `Fold` with the
same seed and lambda would have returned.

Without `Seed:` the first element starts the accumulator (so `Acc` is the
element type, and any type works — not just the Int and Text a `Seed:`
literal can spell). Scanning the empty list gives the empty list either way.

### Take Item — `List<T> -> T`

```domain
Cursed Technique: Take Item 0
```

0-based; out of range is a runtime error. Typically picks an input section
after a `Split` (see Channels in [language.md](language.md)).

### Apply — `T × (T -> U) -> U`

```domain
Cursed Technique: Apply
    Using: (v) -> v * 2
```

The scalar analogue of Map Each: transform the whole current value. Useful
on its own and as a loop body.

### Map Cells — `Grid<T> × (T -> U) -> Grid<U>` (or Sparse)

The lambda transforms every cell; dimensions are preserved. Two lambda
forms, chosen by arity: 1 parameter binds the cell value; 3 parameters bind
`(grid, row, col)` — the positional form — so the body can look around with
`at`/`row`/`col`/`inbounds`:

```domain
Cursed Technique: Map Cells
    Using: (g, r, c) -> if r = c then "\\" else at(g, r, c)
```

Over a `Sparse<T>` the 1-parameter form maps the set cells **and the
default** (the whole infinite plane is transformed), producing `Sparse<U>`;
the positional form is dense-only — densify first if the body needs
coordinates.

### Transpose — `Grid<T> -> Grid<T>`

Swaps rows and columns.

### Range — `-> List<Int>`

```domain
Cursed Technique: Range 5        # [0, 1, 2, 3, 4]
Cursed Technique: Range 1 16     # [1, …, 15]
```

The half-open integer range `[lo, hi)`, replacing the current value (like
`Combine` and `Zip`, which also ignore it). The bounds are
[measured arguments](#measured-arguments) — `Low:` and `High:` — and this is
where that matters most: `Range` discards its input, so
`High: (xs) -> length(xs)` is a range sized from the data that no literal
spelling can express. Half-open **deliberately**:
`range(N)` in a `For` header already means `0..N-1`, and two meanings of
"range" in one language would be worse than the occasional `Range 1 16`. It
also matches `slice`, `take` and `drop`. An inverted range is a resolve error.

### Subgrid — `Grid<T> -> Grid<T>`

```domain
Cursed Technique: Subgrid 1 1 2 2      # ROW COL HEIGHT WIDTH
```

A rectangular crop. A crop that does not fit is a **runtime error, not a
clamp** — silently returning fewer rows than asked for would be a wrong answer
that looks right.

### Pad Grid — `Grid<T> -> Grid<T>`

```domain
Cursed Technique: Pad Grid 1
    Fill: "."
```

Adds a border `n` cells wide (default 1). `Fill:` is an Int or Text literal and
must match the grid's element type. This is the standard move before a flood
fill: a one-cell border lets the fill reach every outside cell without
special-casing the edges.

### Rotate Grid — `Grid<T> -> Grid<T>`

```domain
Cursed Technique: Rotate Grid
    Mode: Right          # Right (default) | Left | Half
```

A quarter or half turn in grid coordinates: `Right` sends `(r, c)` to
`(c, rows-1-r)`, the clockwise turn you get by reading the first column
bottom-to-top as the new first row. A quarter turn swaps the dimensions.
`Rotate Right` equals `Transpose` + `Flip Grid Horizontal`, and four right
turns are the identity — both pinned by tests.

### Flip Grid — `Grid<T> -> Grid<T>`

```domain
Cursed Technique: Flip Grid
    Mode: Horizontal     # Horizontal (default, mirrors left-right) | Vertical
```

### Map Values — `Map<K,V> × (V -> W) -> Map<K,W>`

```domain
Cursed Technique: Map Values
    Using: (b) -> sum(b)
```

Transforms every value; keys and their order are unchanged. `Group By` then
`Map Values` is the two-line spelling of a grouped aggregation.

### Filter Entries — `Map<K,V> × ((K, V) -> Bool) -> Map<K,V>`

```domain
Cursed Technique: Filter Entries
    Using: (k, n) -> n > 1
```

The lambda takes **two** parameters, key then value.

---

## Channeled Energy — coercions

### Convert To Integers — `List<Text> -> List<Int>` | `List<List<Text>> -> List<List<Int>>`

Two forms, chosen by input type:

- `List<Text> -> List<Int>` — `Convert List to Integers`
- `List<List<Text>> -> List<List<Int>>` — `Convert Each List to Integers`

Whitespace around each number is tolerated; a non-integer is a runtime error
naming the offending item.

### Convert To Floats — `List<Text|Int> -> List<Float>` (or nested)

Four forms, chosen by input type:

- `List<Text> -> List<Float>` and `List<Int> -> List<Float>` —
  `Convert List to Floats`
- `List<List<Text>> -> List<List<Float>>` and
  `List<List<Int>> -> List<List<Float>>` — `Convert Each List to Floats`

Text parses with tolerant whitespace (a non-number is a runtime error naming
the item); Int widens exactly. `Sum`, `Max`, `Min`, `Product`, and `Sort`
all accept `List<Float>` afterward. Floats are not keyable — `Unique`,
`Group By`, Sets, and Map keys reject them.

### Convert To Grid — `List<List<T>> | List<Text> | Sparse<T> -> Grid<T>`

Three forms, chosen by input type:

- `List<List<T>> -> Grid<T>` — each inner list is a row; rows must be equal
  length (a ragged grid is a runtime error).
- `List<Text> -> Grid<Text>` — each character (rune) becomes a cell.
- `Sparse<T> -> Grid<T>` — **densify**: materialize the bounding box,
  translated so `(minrow, mincol)` lands at `(0, 0)`, unset cells filled
  with the default. The empty sparse grid becomes the 0×0 grid. This is how
  a sparse plot becomes a printable picture. **Unbounded** — the old
  4,000,000-cell ceiling refused plots a machine had room for, so two
  far-apart cells now cost memory rather than earning a clean refusal
  (`ir.MaxSparseDense` is a var and restores one). A box beyond what any
  machine could allocate is still a clean error, not a panic.

### Convert To Sparse Grid — `… -> Sparse<T>`

```domain
Channeled Energy: Convert To Sparse Grid
    Default: "."
    Mark: "#"
```

Builds the unbounded default-valued plane (see
[data-model.md](data-model.md) for the Sparse contract). `Default:` is an
Int or Text literal and fixes the element type. Three sources, chosen by
input type:

- `Grid<T>` — cells *different from the default* become set cells
  (`Default:` must match the grid's element type; no `Mark:`).
- `Map<(Int, Int), V>` — every entry becomes a set cell (`Default:` must
  match `V`; no `Mark:`). The natural continuation of `Count By` over
  points.
- `List<(Int, Int)>` or `List<List<Int>>` (two ints per row — the shape
  `Match Pattern "{int},{int}" Mode: Each` produces) — every point is set
  to the required `Mark:` literal (same type as `Default:`). Duplicate
  points collapse. A row that is not exactly two integers is a runtime
  error.

### Convert To Rows — `Grid<T> -> List<List<T>>`

The inverse of `Convert To Grid`, which was otherwise a one-way door: anything
the grid primitives do not cover can be reached by dropping back to lists.

### Convert To Entries — `Map<K,V> -> List<(K, V)>`

```domain
Channeled Energy: Convert To Entries
```

Drops a Map back into the list vocabulary, in insertion order (the order a Map
already renders in), where `Sort By` and `Select Top K` already live. This is
how the "which key occurred most?" idiom is written:

```domain
Maximum Technique: Count By
    Using: (s) -> charat(s, 0)
Channeled Energy: Convert To Entries
Domain Expansion: Sort By, Descending
    Using: (e) -> item(e, 1)
Cursed Technique: Take Item 0
```

### Convert To Map — `List<(K, V)> -> Map<K,V>` (K keyable)

The inverse. Duplicate keys: last write wins.

### Convert To Set — `List<T> -> Set<T>` (T keyable)

```domain
Channeled Energy: Convert To Set
```

Deduplicates a list into a Set, preserving first-seen order.

**A Set is accepted wherever a list-shaped primitive takes a List** — `Map
Each`, `Filter`, `Count Matching`, `Count By`, `Group By`, `Fold`, `Reduce`,
`Scan`, `Unique`, `Enumerate`, `Pairs`, `Window`, `Chunk`, `Partition`, `Take
Item`, `Sort By`, `Permutations`, `Subsets`, `Merge Ranges`, `Find Cycle` — and
is read in insertion order, the order it already renders and iterates in. The
**result is a List**: a transform may map two distinct elements onto the same
value, and silently deduplicating would lose data the program asked for.

Primitives that check their input type directly rather than by shape (`Join`,
`Sum`, `Sort`) still require a List. Sets also support `Count`, the Channel
consumers (`Difference`), and the `contains`/`size`/`tolist` expression
builtins.

---

## Maximum Technique — reductions

### Sum — `List<Int> -> Int`

Sum of all elements; `0` for the empty list.

### Sum Each Group — `List<List<Int>> -> List<Int>`

Per-group sums, preserving order.

### Max / Min / Product — `List<Int> -> Int`

Seeded with the first element; the empty list is a runtime error
(`"Max of an empty list is undefined"`).

### Count — `List<T> | Set<T> -> Int`

Cardinality.

### Count Matching — `List<T> × (T -> Bool) -> Int`

How many elements satisfy the predicate.

### Count Cells — `Grid<T> | Sparse<T> × (T -> Bool) -> Int`

How many cells satisfy the predicate. Like `Map Cells`, a 3-parameter
lambda binds `(grid, row, col)` instead of the cell value — the form the
full Day 8 visibility solve uses for line-of-sight reductions
(`testdata/day8_full.domain`). Over a `Sparse<T>` only the *set* cells are
counted (1-parameter form only).

### Select Top K — `List<Int> -> List<Int>` (or `-> Int` with `, Sum`)

```domain
Maximum Technique: Select Top 3, Sum
```

Takes the first K elements of the (already ordered) list, clamped to its
length; the `, Sum` modifier sums them. Directly after a `Sort`, the
optimizer fuses the pair into a quickselect — see
[optimizer.md](optimizer.md).

The count is a [measured argument](#measured-arguments) (`Count:`), and the
quickselect survives it: `TopK` takes the count as data, so the fused node
measures it at run time like any other argument.

### Fold — `List<T> × Seed × (Acc, T -> Acc) -> Acc`

```domain
Maximum Technique: Fold
    Seed: 100
    Using: (acc, x) -> acc * 2 + x
```

`Seed:` fixes the accumulator type and the lambda must return that same type.
Written as a literal it is an Int or Text — the two a named argument can
spell — but it is a [measured argument](#measured-arguments), and that is the
one place measuring *widens* a primitive rather than only moving where its
value comes from: a measured seed takes its type from the lambda body, so the
accumulator can be a composite.

```domain
Maximum Technique: Fold
    Seed: (xs) -> tuple(0, 0)                                  # (sum, count)
    Using: (acc, x) -> tuple(prow(acc) + x, pcol(acc) + 1)
```

Two variations live nearby: [Reduce](#reduce--listt--t-t---t---t) is the same left fold seeded by
the first element instead, and [Scan](#scan--listt--seed--acc-t---acc---listacc) keeps every intermediate
accumulator rather than only the last (its `Seed:` measures the same way).

**Fold as a channel consumer.** With `From:` naming one channel, Fold runs
over the *channel's* list and the **current pipeline value is the seed** —
how a composite state built upstream threads through a list parsed
elsewhere (the Day 5 crate simulation folds the stacks through the moves
channel):

```domain
Maximum Technique: Fold
    From: moves
    Using: (stacks, m) -> set(stacks, m.f - 1, drop(item(stacks, m.f - 1), m.n))
```

### Reduce — `List<T> × (T, T -> T) -> T`

The seedless fold: the first element *is* the starting accumulator, so no
`Seed:` is needed and none is accepted.

```domain
Maximum Technique: Reduce
    Using: (a, b) -> a * 10 + b   # [1,2,3,4] -> 1234
```

It folds left, like `Fold`, and a one-element list is its own answer (the
lambda never runs). Because the accumulator is an element, the lambda must
be `T × T -> T` — and `T` can be anything the pipeline carries, including
composites a `Seed:` literal cannot spell:

```domain
Maximum Technique: Reduce
    Using: (a, b) -> padd(a, b)   # sum a list of points
```

**`Reduce` of an empty list is a runtime error** — there is no accumulator to
start from. Use `Fold` with a `Seed:` when the empty case needs an answer.

### Any / All — `List<T> × (T -> Bool) -> Bool`

```domain
Maximum Technique: Any
    Using: (x) -> x > 4
Maximum Technique: All
    Using: (x) -> x > 0
```

Whether some — or every — element satisfies the predicate. **Both stop at the
element that decides the answer**: `Any` at the first true, `All` at the first
false. `Count Matching … > 0` computes the same thing but always visits the
whole list.

On the empty list each takes the identity of its connective: `Any` is `false`
(nothing satisfies it), `All` is `true` (nothing violates it).

Note that `All Pairs` is the [combination generator](#all-pairs--combinations-k--listt--mode--lambda---)
and `All Values > n` is a [Binding Vow](#simple-domain-channel-shikigami-binding-vow-reveal);
neither is this reduction.

### Find / Find Index — `List<T> × (T -> Bool) -> T` | `List<T> × (T -> Bool) -> Int`

```domain
Maximum Technique: Find
    Using: (r) -> r.n > 3
Maximum Technique: Find Index
    Using: (x) -> x > 3        # 0-based, or -1 when absent
```

The first element satisfying the predicate, or its position. Both stop there —
the rest of the list is never touched.

`Find` on no match is a runtime error (there is no element to hand back);
`Find Index` answers `-1`, the sentinel the expression layer already uses for
"not there". `Filter` + `Take Item 0` is the long way round, and the optimizer
[rewrites it into a `Find`](optimizer.md) when the predicate cannot fail.

### Find Cycle — `List<T> -> (Int, Int)` (T keyable)

```domain
Cursed Technique: Iterate 200
    Using: (s) -> step(s)
Maximum Technique: Find Cycle      # -> (first index of the repeat, period)
```

Where a trajectory first repeats, and its period. `Iterate` produces the
trajectory precisely so a program can ask "have I been here before?", but the
asking had no primitive — `Find Index` needs a predicate over one element, not
a seen-set over the prefix. The answer is what turns "run this a billion
times" into arithmetic.

A trajectory with no repeat gives `(-1, 0)` — the sentinel `Find Index`
already uses — rather than an error, since that is a legitimate result.

### Sum By / Product By — `List<T> × (T -> Int) -> Int`

```domain
Maximum Technique: Sum By
    Using: (x) -> x * x
```

Folds the lambda's Int key over the list without building the mapped list
first, completing the key-lambda family that already has `Count By`, `Min By`
and `Max By`. `Map Each` + `Sum` is the two-pass spelling, and the optimizer
fuses it into exactly this. The empty list gives the identity: `0` for `Sum
By`, `1` for `Product By`.

### Join — `List<Text> -> Text`

```domain
Maximum Technique: Join
Maximum Technique: Join with ", "
```

Concatenates the elements, with an optional separator.

### Group By — `List<T> × (T -> K) -> Map<K, List<T>>` (K keyable)

```domain
Maximum Technique: Group By
    Using: (n) -> n / 3
```

Buckets preserve element order; keys appear in first-seen order.

### Count By — `List<T> × (T -> K) -> Map<K, Int>` (K keyable)

```domain
Maximum Technique: Count By
    Using: (n) -> n / 10
```

Frequency map of the lambda's key, keys in first-seen order.

### Min By / Max By — `List<T> × (T -> Int) -> T`

```domain
Maximum Technique: Max By
    Using: (r) -> r.n
```

The element whose Int key is smallest/largest (the first wins ties; the
empty list is a runtime error).

### Intersect / Union / Difference — `List<List<T>> -> Set<T>` (T keyable)

Set reduction over the groups, seeded with the first group. `Intersect`
keeps the accumulator's element order; `Union` appends left-to-right,
deduplicated; `Difference` keeps the elements of the first group that appear
in no later group. The empty input produces the empty set.

`Difference` is also a two-channel consumer — see
[below](#difference--channel-consumer---sett-t-keyable) for that form.

### Merge Ranges — `List<(Int, Int)> -> List<(Int, Int)>`

```domain
Maximum Technique: Merge Ranges
```

Accepts `List<(Int, Int)>`, `List<List<Int>>` (two ints per row — the shape
a positional `"{int}-{int}"` Match Pattern produces), or a list of records
with exactly two Int fields (the **first declared field** is the low end).
Emits the same shape back: sorted by low end, with overlapping **or
adjacent** ranges merged (`[1,4]` and `[5,7]` coalesce to `[1,7]` —
inclusive integer ranges). An inverted range (`lo > hi`) is a runtime error.
For `Contains`/`Overlaps` predicates, use plain comparisons in a lambda —
see the interval idioms in [expressions.md](expressions.md).

### Combine — channel consumer, `-> U`

```domain
Maximum Technique: Combine
    From: moves, rows
    Using: (moves, rows) -> moves + rows
```

Binds each named channel's value to the lambda's parameters (in `From:`
order) and emits the lambda's result. The main pipeline's current value is
ignored.

### Difference — channel consumer, `-> Set<T>` (T keyable)

```domain
Maximum Technique: Difference
    From: one, two
```

Exactly two channels, each a Set or List with matching element types; emits
the elements of the first not present in the second, in the first's order.

**Standalone form** (no `From:`): `List<List<T>> -> Set<T>` — the first
group's elements not present in any later group, in the first group's
order; the empty input produces the empty set.

### Zip — channel consumer, `-> List<(A, B)>`

```domain
Maximum Technique: Zip
    From: xs, ys
```

Pairs two channel lists element-wise, truncated to the shorter one. Over
two Int lists the pairs are points (`prow`/`pcol` read them). The main
pipeline's current value is ignored, like Combine.

**Zip With.** Adding a two-parameter `Using:` lambda combines the channels
directly instead of handing back tuples for a following `Map Each` to take
apart — one pass, and no intermediate tuple list:

```domain
Maximum Technique: Zip
    From: xs, ys
    Using: (x, y) -> x * y      # -> List<Int>, not List<(Int, Int)>
```

The lambda must take one parameter per channel. Writing the naive `Zip` +
`Map Each` pair is fine too: the optimizer fuses it into the same single
pass.

---

## Domain Expansion — swappable algorithms

These are the optimizer's targets: you name an algorithm, the compiler owes
you its *result*.

### Sort / Quicksort — `List<T> -> List<T>` (T ordered)

```domain
Domain Expansion: Quicksort, Descending
```

Ascending by default; the `Descending` modifier flips it. An **ordered** type
is `Int`, `Float`, `Text`, or a Tuple built from ordered types — so a
`List<Text>` sorts alphabetically and a list of tuples lexicographically.
Ordered is deliberately narrower than keyable at both ends: `Float` is ordered
but not keyable, and a `Record` is keyable but not ordered, its fields having
names rather than positions. Followed
immediately by `Select Top K`, the pair is rewritten to a partial selection
that never fully sorts.

### Sort By — `List<T> × (T -> K) -> List<T>` (K ordered)

```domain
Domain Expansion: Sort By, Descending
    Using: (r) -> r.score

Domain Expansion: Sort By                  # a two-level sort in one pass
    Using: (r) -> tuple(r.group, r.score)
```

Stable sort by the lambda's key, ascending by default. The key may be any
ordered type; a **tuple key is how a tiebreak is written**, comparing
lexicographically.

### All Pairs / Combinations k — `List<T> × Mode × lambda -> …`

```domain
Domain Expansion: All Pairs          # k = 2
    Mode: First                      # Filter | Count | First | Map
    Using: (a, b) -> a + b = 2020

Domain Expansion: Combinations 3
    Mode: First
    Using: (a, b, c) -> a + b + c = 2020
```

Visits every k-combination (indices strictly increasing, lexicographic
order). The lambda takes k parameters. Modes:

| Mode | Lambda must return | Result |
|---|---|---|
| `Filter` (default) | Bool | `List<List<T>>` — the satisfying combos |
| `Count` | Bool | `Int` — how many satisfy |
| `First` | Bool | `List<T>` — the first satisfying combo; **error if none** |
| `Map` | U | `List<U>` — the lambda applied to every combo |

The lambda's parameters are elements of the **current value**. Over a
`List<List<T>>` that means whole inner lists, not the values inside them —
`(a, b) -> a % b = 0` there is a type error naming `List<Int>` operands. To
search each inner list instead, put the `All Pairs` in a
[`Map Each` block](#map-each--listt--t---u---listu), whose body runs once per
element.

`All Pairs` with a sum-to-constant predicate (`Mode: First` or `Count`) is
rewritten to an O(n) hash-set scan — see [optimizer.md](optimizer.md).

### Sliding Reduce — `List<Int> -> List<Int>`

```domain
Domain Expansion: Sliding Reduce 3
    Mode: Sum                    # Sum (default) | Max | Min | Product
Domain Expansion: Sliding Reduce 2 2   # size 2, step 2
```

The reduction of every fully-contained window, in one streaming pass. `Sum`
uses prefix sums and `Max`/`Min` a monotonic deque, so both are **O(n) in the
list length no matter how wide the window is** — the naive spelling is
O(n·size) and materializes every window besides. `Product` has no such trick
(a single zero destroys the prefix), so it is the honest per-window scan, but
it still never builds the windows.

This is the same node the optimizer already reaches from below: `Window n`
feeding a `Map Each` that reduces each window fuses into it. Naming it
directly says what you meant, and gets the streaming form whether or not the
optimizer is running:

```domain
Cursed Technique: Window 3           # these two
Cursed Technique: Map Each
    Using: (w) -> max(w)

Domain Expansion: Sliding Reduce 3   # are the same program
    Mode: Max
```

Size and step must be ≥ 1; a list shorter than the window yields no windows.

### Permutations — `List<T> -> List<List<T>>`

```domain
Domain Expansion: Permutations
```

Every ordering of the list, in lexicographic index order. **Unbounded** —
Domain used to refuse more than 9 elements, which turned a 10-element input
(3.6M orderings, comfortable on any machine) into a spurious failure. `n!`
still explodes, so a large input is slow or memory-hungry rather than
cleanly refused; `prims.MaxPermutationInput` is a var, so a caller that wants
a ceiling back can set one, and both backends read the same value.

### Subsets — `List<T> -> List<List<T>>`

```domain
Domain Expansion: Subsets
```

The power set. Subset k includes element i iff bit i of k is set, so the
empty set comes first and the full list last; each subset preserves element
order. **Unbounded**, for the same reason as `Permutations` (`2^n` still
explodes; `prims.MaxSubsetInput` restores a ceiling if you want one).

### Explore — `S × (S -> List<S>) -> List<S> | Int | Map<S,Int>` (S keyable)

```domain
Domain Expansion: Explore
    Mode: Steps                        # Collect (default) | Count | Distances | Steps
    Until: (s) -> s = target
    Using: (s) -> successors(s)
```

Breadth-first search over the **implicit** graph the successor lambda
describes. The seed is the current pipeline value, and the visited set both
bounds the search over a cyclic space and answers "how many distinct
configurations". BFS order rather than depth-first is what makes the step
counts the *shortest* ones.

| Mode | Result |
|---|---|
| `Collect` (default) | `List<S>` — every reachable state, in BFS order, seed first |
| `Count` | `Int` — how many distinct states |
| `Distances` | `Map<S, Int>` — shortest step count from the seed to each |
| `Steps` | `Int` — steps to the first state satisfying `Until:`, or `-1` |

`Until:` is required by `Steps` and optional elsewhere, where it prunes: a
satisfying state is recorded but never expanded.

**This is what Domain has instead of recursion.** A Shikigami is inlined at
its call site, so a self-referential one has no finite expansion and is
refused (see [language.md](language.md)); the problems that look recursive —
reachability, fewest-moves, "how many configurations" — are searches, and this
states them directly. It is also the non-grid half of graph search: the four
primitives below all take a `Grid`, while Explore takes a *state*, so the
graph can be nodes named in a text file or tuples of position and facing.

The state must be keyable, which is what makes termination possible; build a
compound one with `tuple(...)`.

### Topological Sort — `Map<K, List<K>> | List<(K, K)> -> List<K>` (K keyable)

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Domain Expansion: Topological Sort
```

A dependency order over an *explicit* graph — the other standard question
about one, beside Explore's reachability.

Two input shapes, like `Merge Ranges`: an adjacency `Map<K, List<K>>` (a node
mapped to its successors), or an **edge list** — `List<(K, K)>` or a
two-element `List<List<K>>`, which is exactly what a positional `Match
Pattern` produces, so a parsed dependency file needs no reshaping.

Ties break by **first-seen order** (keys in the map's insertion order, then
successors in list order), so the result is deterministic rather than merely
valid: two runs of the same program agree, and so do the two backends. A node
that appears only as a target is still ordered.

A cycle is a runtime error that **names a blocked node** — "there is a cycle"
alone leaves you to find it by hand in a large input.

### BFS — `Grid<T> × (T -> Bool) -> Grid<Int>`

```domain
Domain Expansion: BFS from 0 0
    Using: (c) -> c = "."
```

Breadth-first search from the `from ROW COL` start over the cells the
`Using:` predicate marks walkable. Connectivity is `Mode: 4` (default) or
`Mode: 8`, a per-call choice rather than a property of the grid — the same way
`neighbors4`/`neighbors8` work. Produces a grid of step
distances from the start; unreachable (and unwalkable) cells hold `-1`.
Read results with `at`, or reduce with `Count Cells`. A start that is out
of bounds or not walkable is a runtime error.

### Dijkstra — `Grid<Int> -> Grid<Int>`

```domain
Domain Expansion: Dijkstra from 0 0
```

Minimum total cost from the start to every cell, where stepping **into** a
cell costs that cell's value (the AoC risk-map convention — the start's own
value is not paid), min-heap under the hood. `Mode: 4` (default) or `Mode: 8`. Negative cell
costs are a runtime error.

### Flood Fill — `Grid<T> × (T -> Bool) -> Grid<Int>`

```domain
Domain Expansion: Flood Fill from 0 0
    Using: (c) -> c = "#"
```

Marks the start's connected region: `1` for every cell reachable from the
start through cells satisfying the predicate, `0` elsewhere. `Mode: 4`
(default) or `Mode: 8`. A start that
is out of bounds or fails the predicate is a runtime error.

### Connected Components — `Grid<T> × (T -> Bool) -> Int`

```domain
Domain Expansion: Connected Components
    Using: (c) -> c = "#"
```

How many connected regions of matching cells the grid contains (union-find
under the hood), with `Mode: 4` (default) or `Mode: 8`. `0` for a grid with no
matching cells.

---

## Reverse Cursed Technique — inversions

### Reverse — `List<T> -> List<T>` or `Text -> Text`

Reverses element order — or, over `Text`, the runes. A palindrome check used
to have to round-trip through `Split Text by ""`.

---

## Simple Domain, Channel, Shikigami, Binding Vow, Reveal

`Reveal: stderr` sends the value to standard error instead of stdout, so a
mid-pipeline Reveal becomes a debugging tool that does not disturb the
program's answer — or its golden test. A nil sink discards, so a host that
captures only stdout never sees stderr output mixed in.

Control flow (`Repeat N` / `While` / `Iterate Until Fixed Point`), Channels
and their consumers, Shikigami definition/calls, vows, and the output sink
are described in [language.md](language.md). All are fully supported by both
backends, including vow stripping under `--release`.
