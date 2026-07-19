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

```domain
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
into individual characters (runes).

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

### Flatten — `List<List<T>> -> List<T>`

Concatenates the groups in order.

### Enumerate — `List<T> -> List<(Int, T)>`

Pairs every element with its 0-based index. Over `List<Int>` the pairs are
points, so `prow`/`pcol` read index/value in a following lambda.

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

---

## Channeled Energy — coercions

### Convert To Integers

Two forms, chosen by input type:

- `List<Text> -> List<Int>` — `Convert List to Integers`
- `List<List<Text>> -> List<List<Int>>` — `Convert Each List to Integers`

Whitespace around each number is tolerated; a non-integer is a runtime error
naming the offending item.

### Convert To Floats

Four forms, chosen by input type:

- `List<Text> -> List<Float>` and `List<Int> -> List<Float>` —
  `Convert List to Floats`
- `List<List<Text>> -> List<List<Float>>` and
  `List<List<Int>> -> List<List<Float>>` — `Convert Each List to Floats`

Text parses with tolerant whitespace (a non-number is a runtime error naming
the item); Int widens exactly. `Sum`, `Max`, `Min`, `Product`, and `Sort`
all accept `List<Float>` afterward. Floats are not keyable — `Unique`,
`Group By`, Sets, and Map keys reject them.

### Convert To Grid

Three forms, chosen by input type:

- `List<List<T>> -> Grid<T>` — each inner list is a row; rows must be equal
  length (a ragged grid is a runtime error).
- `List<Text> -> Grid<Text>` — each character (rune) becomes a cell.
- `Sparse<T> -> Grid<T>` — **densify**: materialize the bounding box,
  translated so `(minrow, mincol)` lands at `(0, 0)`, unset cells filled
  with the default. The empty sparse grid becomes the 0×0 grid. Guarded at
  4,000,000 cells (`ir.MaxSparseDense`) — a clear runtime error instead of
  an OOM when two far-apart cells imply a huge box. This is how a sparse
  plot becomes a printable picture.

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

### Convert To Set — `List<T> -> Set<T>` (T keyable)

```domain
Channeled Energy: Convert To Set
```

Deduplicates a list into a Set, preserving first-seen order. Sets support
`Count`, the Channel consumers (`Difference`), and the `contains(s, v)`
expression builtin.

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

### Fold — `List<T> × Seed × (Acc, T -> Acc) -> Acc`

```domain
Maximum Technique: Fold
    Seed: 100
    Using: (acc, x) -> acc * 2 + x
```

`Seed:` is an Int or Text literal and fixes the accumulator type; the lambda
must return that same type.

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

### Intersect / Union — `List<List<T>> -> Set<T>` (T keyable)

Set reduction over the groups, seeded with the first group. `Intersect`
keeps the accumulator's element order; `Union` appends left-to-right,
deduplicated. The empty input produces the empty set.

### Merge Ranges — coalesce inclusive integer intervals

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

---

## Domain Expansion — swappable algorithms

These are the optimizer's targets: you name an algorithm, the compiler owes
you its *result*.

### Sort / Quicksort — `List<Int> -> List<Int>`

```domain
Domain Expansion: Quicksort, Descending
```

Ascending by default; the `Descending` modifier flips it. Followed
immediately by `Select Top K`, the pair is rewritten to a partial selection
that never fully sorts.

### Sort By — `List<T> × (T -> Int) -> List<T>`

```domain
Domain Expansion: Sort By, Descending
    Using: (r) -> r.score
```

Stable sort by the lambda's Int key, ascending by default.

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

`All Pairs` with a sum-to-constant predicate (`Mode: First` or `Count`) is
rewritten to an O(n) hash-set scan — see [optimizer.md](optimizer.md).

### Permutations — `List<T> -> List<List<T>>`

```domain
Domain Expansion: Permutations
```

Every ordering of the list, in lexicographic index order. Bounded: more
than 9 elements is a runtime error (n! explodes; state the search
differently or wait for an optimizer pass that prunes it).

### Subsets — `List<T> -> List<List<T>>`

```domain
Domain Expansion: Subsets
```

The power set. Subset k includes element i iff bit i of k is set, so the
empty set comes first and the full list last; each subset preserves element
order. Bounded: more than 16 elements is a runtime error (2^n explodes).

### BFS — `Grid<T> × (T -> Bool) -> Grid<Int>`

```domain
Domain Expansion: BFS from 0 0
    Using: (c) -> c = "."
```

Breadth-first search from the `from ROW COL` start over the cells the
`Using:` predicate marks walkable, 4-connectivity. Produces a grid of step
distances from the start; unreachable (and unwalkable) cells hold `-1`.
Read results with `at`, or reduce with `Count Cells`. A start that is out
of bounds or not walkable is a runtime error.

### Dijkstra — `Grid<Int> -> Grid<Int>`

```domain
Domain Expansion: Dijkstra from 0 0
```

Minimum total cost from the start to every cell, where stepping **into** a
cell costs that cell's value (the AoC risk-map convention — the start's own
value is not paid), 4-connectivity, min-heap under the hood. Negative cell
costs are a runtime error.

### Flood Fill — `Grid<T> × (T -> Bool) -> Grid<Int>`

```domain
Domain Expansion: Flood Fill from 0 0
    Using: (c) -> c = "#"
```

Marks the start's 4-connected region: `1` for every cell reachable from the
start through cells satisfying the predicate, `0` elsewhere. A start that
is out of bounds or fails the predicate is a runtime error.

### Connected Components — `Grid<T> × (T -> Bool) -> Int`

```domain
Domain Expansion: Connected Components
    Using: (c) -> c = "#"
```

How many 4-connected regions of matching cells the grid contains (union-find
under the hood). `0` for a grid with no matching cells.

---

## Reverse Cursed Technique — inversions

### Reverse — `List<T> -> List<T>`

Reverses element order.

---

## Simple Domain, Channel, Shikigami, Binding Vow, Reveal

Control flow (`Repeat N` / `While` / `Iterate Until Fixed Point`), Channels
and their consumers, Shikigami definition/calls, vows, and the output sink
are described in [language.md](language.md). All are fully supported by both
backends, including vow stripping under `--release`.
