# Record, point and sparse builtins

Part of the [expression layer reference](expressions.md).

### Records

`Match Pattern` produces records, and `.field` reads them:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{lo:int}-{hi:int}"
Maximum Technique: Sum By
    Using: (r) -> r.hi - r.lo
Reveal: stdout
```
```input
1-5
10-12
```
```output
6
```

`record` builds one from name/value pairs and `with` copies it with a field
replaced — both take literal field names:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (n) -> with(record("v", n, "double", 0), "double", n * 2)
Reveal: stdout
```
```input
1,2
```
```output
[{v: 1, double: 2}, {v: 2, double: 4}]
```


A `Record` is named fields. `Match Pattern` produces one; `record` builds one,
which is what lets a fold carry a **named** accumulator instead of a positional
tuple whose `item(acc, 2)` nobody can read.

| Builtin | Type | Behavior |
|---|---|---|
| `record("a", x, "b", y, …)` | `Text × T1 × … -> {a:T1, b:T2, …}` | Build a record from name/value pairs. Field names must be **literals** and the argument count must be even. |
| `with(r, "a", v)` | `{a:T, …} × Text × T -> {a:T, …}` | A copy with field `a` replaced. The name is a literal and must already exist; the type is unchanged. |

The names are literals for the same reason `item(t, 0)` over a tuple needs a
literal index: the result type is only knowable when they are. That also means
`record` needs **no new syntax** — no braces, no new argument form in the
grammar — and both it and `with` compile to a plain Go struct literal and a
struct assignment, so a named accumulator costs nothing a tuple did not.

```domain
Maximum Technique: Fold
    Seed: (xs) -> record("lo", 0, "hi", 0)
    Using: (acc, n) -> with(with(acc, "lo", min(acc.lo, n)), "hi", max(acc.hi, n))
```

### Points and grid geometry

Points are `(Int, Int)` tuples, and the builtins read and combine them:

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
.#
#.
```
```output
2
```

`dirs4` and `padd` are how a walk steps, with no grid and no bounds check:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> point(0, 0)
Cursed Technique: Apply
    Using: (p) -> length(around8(p)) + prow(padd(p, point(3, 4))) + pcol(rotr(point(-1, 0)))
Reveal: stdout
```
```input
```
```output
12
```


A **point** is an `(Int, Int)` tuple of `(row, col)` — the same coordinate
system grids use, and exactly what `Find Cells` and these builtins produce.

| Builtin | Type | Behavior |
|---|---|---|
| `point(r, c)` | `Int × Int -> (Int, Int)` | Construct a point. |
| `prow(p)` / `pcol(p)` | `(Int, Int) -> Int` | The row / column component. |
| `padd(p, q)` | `(Int, Int) × (Int, Int) -> (Int, Int)` | Component-wise sum (move by a direction vector). |
| `manhattan(p, q)` | `(Int, Int) × (Int, Int) -> Int` | `abs(Δrow) + abs(Δcol)`. |
| `rotl(p)` / `rotr(p)` | `(Int, Int) -> (Int, Int)` | Rotate a direction vector 90° left/right in grid coordinates: `rotr((-1, 0))` (up) is `(0, 1)` (right). |
| `psub(p, q)` | `(Int, Int) × (Int, Int) -> (Int, Int)` | Component-wise difference. |
| `pscale(p, n)` | `(Int, Int) × Int -> (Int, Int)` | Step `n` times along a direction. `padd` alone can only step by one. |
| `chebyshev(p, q)` | `(Int, Int) × (Int, Int) -> Int` | `max(\|Δrow\|, \|Δcol\|)` — 8-connectivity distance. |
| `dirs4()` | `-> List<(Int, Int)>` | The four orthogonal unit vectors: up, down, left, right. |
| `dirs8()` | `-> List<(Int, Int)>` | All eight, diagonals included. |
| `around4(p)` / `around8(p)` | `(Int, Int) -> List<(Int, Int)>` | Neighbours of a **point**, with no grid and no bounds check. `neighbors4`/`neighbors8` require a dense `Grid`, so these are what a `Sparse<T>` automaton needs. |
| `neighbors4(g, r, c)` | `Grid<T> × Int × Int -> List<(Int, Int)>` | In-bounds orthogonal neighbor coordinates of `(r, c)`. |
| `neighbors8(g, r, c)` | `Grid<T> × Int × Int -> List<(Int, Int)>` | In-bounds neighbors including diagonals. |

### Sparse grids

An unbounded plane, written a cell at a time — negative coordinates included:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> put(put(sparse("."), 0, 0, "#"), -5, -5, "#")
Reveal: stdout
```
```input
```
```output
{[-5, -5]: #, [0, 0]: #}
```

`cells` counts the set ones and the bounds builtins read the extent, so the
plane never has to be densified to be measured:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> put(put(sparse("."), 0, 0, "#"), -5, 3, "#")
Cursed Technique: Apply
    Using: (g) -> cells(g) + minrow(g) + maxcol(g)
Reveal: stdout
```
```input
```
```output
0
```


`Sparse<T>` is the unbounded default-valued plane — see
[data-model.md](data-model.md) for the full contract. `at` (above) is
*total* over a sparse grid.

| Builtin | Type | Behavior |
|---|---|---|
| `sparse(d)` | `T -> Sparse<T>` | An empty plane whose default is `d`. |
| `put(g, r, c, v)` | `Sparse<T> × Int × Int × T -> Sparse<T>` | Functional update: a copy with cell `(r, c)` set to `v` (the original is untouched). O(cells) per call — fine for building small grids in folds and loops; use `Convert To Sparse Grid` for bulk construction. |
| `has(g, r, c)` | `Sparse<T> × Int × Int -> Bool` | Whether `(r, c)` was explicitly set (a cell set to the default is still set). |
| `cells(g)` | `Sparse<T> -> Int` | The number of set cells. |
| `minrow(g)` / `maxrow(g)` | `Sparse<T> -> Int` | Bounds over set cells. **Error** on an empty sparse grid. |
| `mincol(g)` / `maxcol(g)` | `Sparse<T> -> Int` | Column bounds, same rules. |

Builtins compose freely with each other and with operators:

```
(g) -> item(g, 0) * 1000 + sum(take(g, 2)) + length(drop(g, 1))
(m) -> sum(get(m, 1))
(grid) -> at(grid, 1, 2) * 10 + at(grid, 0, 0)
(grid) -> inbounds(grid, 5, 0) and at(grid, 5, 0) = "#"
(ps) -> manhattan(first(ps), last(ps))
(n) -> lcm(gcd(n, 12), 8) + modpow(n, 10, 97)
```

Interval idioms need no dedicated Range type — a range is a two-field record
(from `Match Pattern: "{lo:int}-{hi:int}"`) and the operators express
containment and overlap directly (see `Merge Ranges` in
[primitives.md](primitives.md) for coalescing):

```
(r) -> r.lo <= 42 and 42 <= r.hi                          # Contains
(a, b) -> a.lo <= b.hi and b.lo <= a.hi                   # Overlaps
```

Typing errors (unknown function, wrong arity, wrong argument types) are
positioned resolve-time errors; the message for an unknown name lists the
whole builtin table.
