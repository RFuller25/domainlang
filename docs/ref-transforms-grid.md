# Grid, Sparse and Map transforms — `Cursed Technique`

One class of the [primitive reference](primitives.md).

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Map Cells
    Using: (c) -> upper(c)
Reveal: stdout
```
```input
ab
cd
```
```output
AB
CD
```

The 3-parameter form binds `(grid, row, col)` instead, so the body can read
other cells while it rewrites this one:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Map Cells
    Using: (g, r, c) -> if r = c then "X" else at(g, r, c)
Reveal: stdout
```
```input
ab
cd
```
```output
Xb
cX
```

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Find Cells
    Using: (c) -> c = "#"
Reveal: stdout
```
```input
.#.
..#
```
```output
[[0, 1], [1, 2]]
```

The positions come back as points, so the point builtins read them directly:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Find Cells
    Using: (c) -> c = "#"
Maximum Technique: Sum By
    Using: (p) -> manhattan(p, point(0, 0))
Reveal: stdout
```
```input
.#.
..#
```
```output
4
```

### Transpose — `Grid<T> -> Grid<T>` | `List<List<T>> -> List<List<T>>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Transpose
Reveal: stdout
```
```input
a b c
d e f
```
```output
[[a, d], [b, e], [c, f]]
```

Over a `Grid<T>` it gives back a grid, so rows become columns without leaving
the grid layer:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Transpose
Reveal: stdout
```
```input
ab
cd
```
```output
ac
bd
```


Swaps rows and columns.

Two input shapes. A `Grid<T>` transposes to a `Grid<T>`; a **list of rows**
transposes to a list of columns, which is the shape `Extract Integers`,
`Split Fields`, `Split Each` and a positional `Match Pattern` all produce.
Without the second, a column-wise question had to detour through
`Convert To Grid` — which additionally demands one element type across the
whole thing, a constraint transposition does not need.

```domain ignore
Cursed Technique: Extract Integers   # List<List<Int>>
Cursed Technique: Transpose          # the columns, as rows
Cursed Technique: Map Each
    Using: (col) -> sum(col)
```

A **ragged** list of rows is a runtime error naming the row and both lengths,
the same one `Convert To Grid` raises — transposing it would have to invent
or drop cells. Rows with no columns transpose to the empty list, so that
round trip is the one place `Transpose` twice is not the identity: a matrix
with no columns has no rows to remember.

### Subgrid — `Grid<T> -> Grid<T>`

```domain
Cursed Technique: Subgrid 1 1 2 2      # ROW COL HEIGHT WIDTH
```

A rectangular crop. A crop that does not fit is a **runtime error, not a
clamp** — silently returning fewer rows than asked for would be a wrong answer
that looks right.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Subgrid 1 1 2 2
Reveal: stdout
```
```input
abcd
efgh
ijkl
```
```output
fg
jk
```

The crop is `ROW COL HEIGHT WIDTH`, so a 1×1 window is how a single cell is
lifted out as a grid rather than as a value:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Subgrid 0 0 1 3
Reveal: stdout
```
```input
abcd
efgh
```
```output
abc
```

### Pad Grid — `Grid<T> -> Grid<T>`

```domain
Cursed Technique: Pad Grid 1
    Fill: "."
```

Adds a border `n` cells wide (default 1). `Fill:` is an Int or Text literal and
must match the grid's element type. This is the standard move before a flood
fill: a one-cell border lets the fill reach every outside cell without
special-casing the edges.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Pad Grid 1
    Fill: "."
Reveal: stdout
```
```input
ab
cd
```
```output
....
.ab.
.cd.
....
```

A wider border costs nothing extra to write, and the dimensions grow by twice
the width in each direction:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Pad Grid 2
    Fill: "#"
Maximum Technique: Count Cells
    Using: (c) -> c = "#"
Reveal: stdout
```
```input
ab
```
```output
28
```

(A 1×2 grid padded by 2 is 5×6; 30 cells less the 2 original ones is 28.)

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Rotate Grid
    Mode: Right
Reveal: stdout
```
```input
ab
cd
```
```output
ca
db
```

A quarter turn swaps the dimensions, which a non-square grid shows plainly:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Rotate Grid
    Mode: Left
Reveal: stdout
```
```input
abc
def
```
```output
cf
be
ad
```

### Flip Grid — `Grid<T> -> Grid<T>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Flip Grid
    Mode: Horizontal
Reveal: stdout
```
```input
abc
def
```
```output
cba
fed
```

`Vertical` mirrors top-to-bottom instead, and neither changes the dimensions
the way a quarter turn does:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Flip Grid
    Mode: Vertical
Reveal: stdout
```
```input
abc
def
```
```output
def
abc
```


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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Group By
    Using: (w) -> charat(w, 0)
Cursed Technique: Map Values
    Using: (g) -> length(g)
Reveal: stdout
```
```input
apple
avocado
banana
```
```output
{a: 2, b: 1}
```

Keys and their order survive the mapping, so the value type may change freely:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Group By
    Using: (w) -> length(w)
Cursed Technique: Map Values
    Using: (g) -> textjoin(g, "+")
Reveal: stdout
```
```input
ab
cd
xyz
```
```output
{2: ab+cd, 3: xyz}
```

### Filter Entries — `Map<K,V> × ((K, V) -> Bool) -> Map<K,V>`

```domain
Cursed Technique: Filter Entries
    Using: (k, n) -> n > 1
```

The lambda takes **two** parameters, key then value.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Maximum Technique: Count By
    Using: (c) -> c
Cursed Technique: Filter Entries
    Using: (k, n) -> n > 1
Reveal: stdout
```
```input
abracadabra
```
```output
{a: 5, b: 2, r: 2}
```

Both halves of the entry are in scope, so a filter may ask about the key
instead of the count:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Maximum Technique: Count By
    Using: (c) -> c
Cursed Technique: Filter Entries
    Using: (k, n) -> k = "a"
Reveal: stdout
```
```input
abracadabra
```
```output
{a: 5}
```

---
