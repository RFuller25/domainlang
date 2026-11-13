# The data model

Every value in a Domain program has one of the types on this page. For each
type this page says what it is, how you get one, how you read it, and how it
prints.

For the operations that work on these types see
[primitives.md](primitives.md); for the functions that read them inside a
lambda see [expressions.md](expressions.md).

## The types at a glance

| Type | Written | What it is | Prints as |
|---|---|---|---|
| Int | `Int` | a 64-bit whole number | `42` |
| Float | `Float` | a 64-bit floating-point number | `3.25` |
| Text | `Text` | a string | `hello` |
| Bool | `Bool` | true or false | `true` |
| List | `List<T>` | any number of values of one type, in order | `[1, 2, 3]` |
| Tuple | `(A, B)` | a fixed number of values, each with its own type | `[1, x]` |
| Record | `{a: Int, b: Text}` | a fixed set of named fields | `{a: 1, b: x}` |
| Map | `Map<K,V>` | keys, each with a value | `{k1: v1, k2: v2}` |
| Set | `Set<T>` | distinct values | `{v1, v2}` |
| Grid | `Grid<T>` | a rectangle of cells | its rows, one per line |
| Sparse | `Sparse<T>` | an unbounded plane of cells | `{[0, 0]: #, [1, 2]: #}` |
| Graph | `Graph<K>` | nodes joined by weighted, directed edges | `{a: [(b, 1)], b: []}` |
| View | `View` | a picture to draw (games) | the picture |

The "Written" column is how a type appears in a
[Shikigami signature](language.md#declared-signatures) and in error messages.

You never write a type on a value or a lambda. Types are worked out from the
input and the operations applied to it, and a program whose types do not fit
is rejected before it runs.

## Three properties types differ in

**Equality.** `=` works on any two values of the same type, and compares
contents: two lists are equal when they hold equal elements in the same order.
The one exception is `View`, which cannot be compared at all.

**Order.** `<`, `>`, sorting, and `Min By` / `Max By` work on types that have
an order: `Int`, `Float`, `Text`, and tuples of those. Text orders
alphabetically by character code; a tuple orders by its first differing field.

**Keyable.** A Map's keys, a Set's elements, a Graph's nodes, and the states
of an `Explore` search must be *keyable*: `Int`, `Text`, or a tuple, record or
list built from keyable types. `Float` is not keyable, and neither are Grid,
Sparse, Graph or View. The same rule applies to `Unique` and `Group By`.

## Numbers

`Int` is a 64-bit signed integer. There are no big integers; operations that
could overflow say so instead (`factorial` is an error past `20!`).

`Float` is a 64-bit floating-point number, written with a dot and at least one
digit after it: `3.25`.

- **Mixing Int and Float gives a Float.** This applies to arithmetic,
  comparison and `=`.
- **Going back to Int is explicit**: `floor`, `ceil` or `round`.
- `tofloat` converts an Int or a Text. `Convert To Floats` converts a whole
  list.
- `Sum`, `Max`, `Min`, `Product` and `Sort` accept a list of floats; `abs`,
  `sum`, `min` and `max` work on either kind of number.
- A Float prints in the shortest form that reads back as the same number.
- Float literals appear in expressions and named arguments. A number written
  in an operation phrase (`Window 3`) is always an Int.

## Lists and tuples

A **List** holds any number of values of one type. Most pipelines are a list
being reshaped: `Split` makes one, `Map Each` and `Filter` transform one,
`Sum` and `Count` reduce one. Inside a lambda, `list(a, b, c)` builds one and
`item(xs, i)`, `first`, `last` and `length` read one; indexes start at 0.

A **Tuple** holds a fixed number of values that may differ in type — a pair
of a name and a count, say. `tuple(a, b)` builds one, `item(t, 0)` reads a
field, and a lambda parameter can
[unpack one directly](expressions.md#destructuring-a-tuple-parameter):
`(acc, (name, qty)) -> …`. `Enumerate`, `Zip` and `Pairs` produce tuples.

A point on a grid is a tuple `(Int, Int)` of row and column.

A tuple **prints like a list**: `[3, x]`. The parenthesized spelling
`(Int, Text)` is how the *type* is written, never the value.

## Records

A record is a fixed set of named fields.

- **You get one from** `Match Pattern` with named holes (see
  [match-pattern.md](match-pattern.md)), or by writing a record literal in a
  lambda: `{a: 1, b: x}`.
- **You read a field** with `r.field`. A field the record does not have is an
  error before the program runs.
- **Two records have the same type** when they have the same field names with
  the same types. The order the fields were written in does not matter.
- **It prints** as `{a: 1, b: x}`, with fields in the order the type declares
  them.

See [ref-builtins-records.md](ref-builtins-records.md).

## Maps and Sets

A **Map** holds keys, each with a value. A **Set** holds distinct values.
Both remember the order things were added, and print in that order.

**Getting one:**

| You want | Use |
|---|---|
| elements grouped by something | `Group By` → `Map<K, List<V>>` |
| how many of each | `Count By` → `Map<K, Int>` |
| a map from a list of pairs | `tomap` in a lambda |
| a set from a list | `Convert To Set`, or `toset` in a lambda |
| to build one up piece by piece | `emptymap` / `emptyset`, then `insert` in a `Fold` |

**Reading one:** `get`, `getor`, `haskey`, `keys`, `values` for a Map;
`contains` for a Set; `size` for either. `tolist` turns a Set back into a
List, and `Convert To Entries` turns a Map into a list of `(key, value)`
tuples. The Set operations `Intersect`, `Union` and `Difference` are reductions
over channels, and `intersect`, `union` and `difference` are builtins.

**A Map can be used where a List is expected.** It is read as its entries —
`(key, value)` tuples — in insertion order, so `Map Each`, `Filter` and
`Count` work on one directly.

There is no literal syntax for a Map or Set. Braces in a lambda always build
a record.

See [ref-builtins-collections.md](ref-builtins-collections.md).

## Grids

A `Grid<T>` is a rectangle of cells.

- **You get one from** `Convert To Grid`: a `List<Text>` becomes a grid of
  single characters, and a `List<List<T>>` becomes a `Grid<T>`. The prelude's
  `Digit Grid` goes straight from text to a `Grid<Int>`.
- **Positions are `(row, col)`, starting at 0.**
- **Reading outside the grid is an error**:
  `at: position (5, 5) out of range (grid 2x2)`. Check first with
  `inbounds(g, r, c)`, or use `neighbors4` / `neighbors8`, which only ever
  return positions inside the grid.
- **Four neighbours or eight** is decided by which function you call:
  `neighbors4` / `neighbors8` for a position in a grid, `around4` / `around8`
  for a bare point.
- **It prints** as its rows, one per line.

The operations on grids are `Map Cells`, `Find Cells`, `Count Cells`,
`Transpose`, `Subgrid`, `Pad Grid`, `Rotate Grid` and `Flip Grid`
([grid transforms](ref-transforms-grid.md)), and the searches `BFS`,
`Dijkstra`, `Flood Fill` and `Connected Components`
([expansions](ref-expansions.md)). Inside a lambda, `at`, `inbounds`, `row`,
`col`, `rows` and `cols` read one.

## Sparse grids

A `Sparse<T>` is an **unbounded plane**. Every position — negative
coordinates included — holds a *default value* until something is written
there, and only written cells are stored. Use it when you do not know the
extent in advance: scattered points, cellular automata, anything that draws.

**The rules:**

- **Reading never fails.** `at(g, r, c)` gives the stored value, or the
  default.
- **A written cell stays written**, even if the value written is the default.
  `has` reports it, and it counts toward the bounds.
- **Cells are visited in row order**, then column order — not the order they
  were written. `Find Cells`, `Map Cells`, `Count Cells` and printing all use
  this order.
- **`Map Cells` transforms the default too**, since the default is the value
  of every unwritten cell. The `(grid, row, col)` form of its lambda is for
  dense grids only.

**Getting one** — `Convert To Sparse Grid` with a `Default:` value, from:

- a `Grid<T>`: every cell that differs from the default becomes a written
  cell;
- a `Map<(Int, Int), V>`: every entry becomes a written cell;
- a list of points, with a `Mark:` value: every point is written with the
  mark.

Inside a lambda, `sparse(default)` makes an empty plane and
`put(g, r, c, v)` gives back the plane with one more cell written.

**Reading one:** `at`, `has`, `cells` (how many are written), and `minrow` /
`maxrow` / `mincol` / `maxcol` for the extent of the written cells.

**Printing one.** A sparse grid prints as a listing of its written cells,
`{[0, 0]: #, [1, 2]: #}`. To print the *picture*, convert it with
`Convert To Grid`: that produces the smallest dense grid containing every
written cell, with the default everywhere else. There is no size limit on the
conversion, so two cells very far apart make a very large grid.

The challenges `11_game_of_life`, `12_origami` and `13_minesweeper` in
[../challenges/](../challenges/README.md) are built on sparse grids.

## Graphs

A `Graph<K>` is a set of nodes of type `K` joined by edges.

- **You get one from** `Convert To Graph` — given a list of edges or a map
  from each node to its neighbours — or from the `graph` and `emptygraph`
  builtins.
- **Edges are directed.** For an undirected graph, `Mode: Undirected` adds
  each edge in both directions.
- **Every edge has an `Int` weight**, 1 unless you say otherwise.
- **There is one edge at most from one node to another.** Adding it again
  replaces its weight.
- **Nodes must be [keyable](#three-properties-types-differ-in).**
- **Two graphs are equal** when they have the same nodes and edges,
  whatever order they were added in.
- **It prints** as `{a: [(b, 1), (c, 2)], b: [], c: []}` — each node with its
  outgoing edges as `(to, weight)`, nodes in the order they were added.
  Weights are always shown.

The graph operations — `BFS`, `Dijkstra`, `Shortest Path`,
`Topological Sort`, `Connected Components` and others — are in
[ref-expansions.md](ref-expansions.md).

## Views

A `View` is a description of a picture, used by
[games](game-dev.md). It is built by the
[view builtins](ref-builtins-view.md) and drawn by `Reveal`.

A View is the one type you cannot inspect: it has no equality, no order, and
cannot be a key. Comparing two views, or two values that contain views, is an
error. Its width is measured in characters, the same unit `length` uses for
Text.

## Rendering

How `Reveal` prints each type, in one place:

| Type | Prints as |
|---|---|
| Int, Float, Text, Bool | the value itself: `42`, `3.25`, `hello`, `true` |
| List | `[v1, v2, …]` |
| Tuple | `[v1, v2, …]` — the same as a List. `(T1, T2)` is how the *type* is written; no value ever renders that way |
| Record | `{a: v1, b: v2}`, fields in the order the type declares them |
| Map | `{k1: v1, k2: v2}`, in the order entries were added |
| Set | `{v1, v2, …}`, in the order elements were added |
| Grid | rows joined by newlines |
| Sparse | `{[r, c]: v, …}` — the written cells, by row then column |
| Graph | `{a: [(b, 1), (c, 2)], b: [], c: []}` — each edge as `(to, weight)` |
| View | the picture |

Text inside a collection is printed without quotes: `[a, b]`.

## What the model does not have

- **Literals for Maps and Sets.** Build them from data with `Group By`,
  `Count By`, `Convert To Set`, `tomap` or `toset`.
- **Big integers.** `Int` is 64 bits.
- **Types that contain themselves**, such as a tree. See
  [known limits](aoc-gaps.md#no-recursive-data).
