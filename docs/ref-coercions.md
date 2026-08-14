# Coercions — `Channeled Energy`

One class of the [primitive reference](primitives.md).

## Channeled Energy — coercions

### Convert To Integers — `List<Text> -> List<Int>` | `List<List<Text>> -> List<List<Int>>`

Two forms, chosen by input type:

- `List<Text> -> List<Int>` — `Convert List to Integers`
- `List<List<Text>> -> List<List<Int>>` — `Convert Each List to Integers`

Whitespace around each number is tolerated; a non-integer is a runtime error
naming the offending item.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
```
```input
 10
20
 30
```
```output
60
```

The nested form is the one a grid of digits needs, and the keyword says which
you meant rather than the shape being inferred silently:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Reveal: stdout
```
```input
1 2 3
10 20
```
```output
[6, 30]
```

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Floats
Maximum Technique: Sum
Reveal: stdout
```
```input
1.5,2.25,0.25
```
```output
4
```

Widening from `Int` is exact, which is how an integer pipeline reaches the
float builtins without a reparse:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Channeled Energy: Convert To Floats
Cursed Technique: Map Each
    Using: (x) -> sqrt(x)
Reveal: stdout
```
```input
1,4,9
```
```output
[1, 2, 3]
```

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

From `List<Text>`, each character becomes a cell — the char-grid form almost
every grid puzzle opens with:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Maximum Technique: Count Cells
    Using: (c) -> c = "#"
Reveal: stdout
```
```input
.#.
###
```
```output
4
```

From `List<List<T>>` each inner list is a row, which is how a digit grid keeps
its cells as `Int` rather than as text:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Cursed Technique: Map Cells
    Using: (n) -> n * 2
Reveal: stdout
```
```input
12
34
```
```output
2 4
6 8
```

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

From a list of points, `Mark:` says what to write at each — the shape a
coordinate-list puzzle parses straight into:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Channeled Energy: Convert To Sparse Grid
    Default: "."
    Mark: "#"
Reveal: stdout
```
```input
0,1
1,0
```
```output
{[0, 1]: #, [1, 0]: #}
```

From a `Grid<T>` the cells differing from the default become the set cells,
which is how a dense picture becomes a sparse one that can then grow past its
original bounds:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Channeled Energy: Convert To Sparse Grid
    Default: "."
Reveal: stdout
```
```input
.#
#.
```
```output
{[0, 1]: #, [1, 0]: #}
```

Only set cells are stored, so `cells` counts them while the plane itself stays
unbounded — see [data-model.md](data-model.md).

### Convert To Rows — `Grid<T> -> List<List<T>>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Channeled Energy: Convert To Rows
Reveal: stdout
```
```input
ab
cd
```
```output
[[a, b], [c, d]]
```

It is the way back out of the grid layer, so the list primitives become
available again:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Transpose
Channeled Energy: Convert To Rows
Cursed Technique: Map Each
    Using: (r) -> textjoin(r, "")
Reveal: stdout
```
```input
ab
cd
```
```output
[ac, bd]
```


The inverse of `Convert To Grid`, which was otherwise a one-way door: anything
the grid primitives do not cover can be reached by dropping back to lists.

### Convert To Graph — `List<(K,K)> | List<(K,K,Int)> | Map<K,List<K>> -> Graph<K>`

The pipeline half of the `graph` builtin: a parse that has just produced an
edge list becomes a graph without detouring through an `Apply`.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Channeled Energy: Convert To Graph
Reveal: stdout
```
```input
a -> b
a -> c
b -> c
```
```output
{a: [(b, 1), (c, 1)], b: [(c, 1)], c: []}
```

`Mode: Undirected` inserts both arcs. There is no second kind of value: an
undirected graph *is* one with the arcs both ways, which is what keeps one set
of algorithms working over both.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Channeled Energy: Convert To Graph
    Mode: Undirected
Reveal: stdout
```
```input
a -> b
b -> c
```
```output
{a: [(b, 1)], b: [(a, 1), (c, 1)], c: [(b, 1)]}
```


Four input shapes, all of them shapes a parse actually lands on:

| Input | Weights |
|---|---|
| `List<(K, K)>` | all 1 |
| `List<(K, K, Int)>` | as given |
| `List<List<K>>` — what a **positional** `Match Pattern` produces | all 1 |
| `Map<K, List<K>>` — an adjacency map | all 1 |

The adjacency form is the only one that can name an **isolated** node, since
an edge list mentions a node only by connecting it. Nodes must be keyable
(`Int`, `Text`, or a tuple/record of them).

### Convert To Edges — `Graph<K> -> List<(K, K, Int)>`

The way back out, so anything the graph vocabulary does not cover is reachable
by dropping to lists — the same role `Convert To Rows` plays for a `Grid`.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Channeled Energy: Convert To Graph
Channeled Energy: Convert To Edges
Reveal: stdout
```
```input
a -> b
b -> c
```
```output
[[a, b, 1], [b, c, 1]]
```

Weights survive the round trip, which is what makes the pair a way to *edit* a
graph with the list vocabulary and put it back:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> list(tuple("a", "b", 4), tuple("b", "c", 6))
Channeled Energy: Convert To Graph
Channeled Energy: Convert To Edges
Reveal: stdout
```
```input
```
```output
[[a, b, 4], [b, c, 6]]
```


Arcs come out in node insertion order, and each node's arcs in theirs — the
same order the graph renders in.


### Convert To Entries — `Map<K,V> -> List<(K, V)>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Maximum Technique: Count By
    Using: (c) -> c
Channeled Energy: Convert To Entries
Reveal: stdout
```
```input
aabc
```
```output
[[a, 2], [b, 1], [c, 1]]
```

Entries are what puts a `Map` back into the list layer, where it can be
sorted — a `Map` has insertion order, not a sorted one:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Maximum Technique: Count By
    Using: (c) -> c
Channeled Energy: Convert To Entries
Domain Expansion: Sort By, Descending
    Using: (e) -> item(e, 1)
Cursed Technique: Take Item 0
Reveal: stdout
```
```input
abracadabra
```
```output
[a, 5]
```


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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by "="
Cursed Technique: Map Each
    Using: (p) -> tuple(first(p), toint(last(p)))
Channeled Energy: Convert To Map
Reveal: stdout
```
```input
a=1
b=2
```
```output
{a: 1, b: 2}
```

Last write wins on a duplicate key, which is what makes it usable on input
that restates a setting:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by "="
Cursed Technique: Map Each
    Using: (p) -> tuple(first(p), toint(last(p)))
Channeled Energy: Convert To Map
Reveal: stdout
```
```input
a=1
a=9
```
```output
{a: 9}
```


### Convert To Set — `List<T> -> Set<T>` (T keyable)

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Set
Reveal: stdout
```
```input
a,b,a,c
```
```output
{a, b, c}
```

Insertion order is preserved, and `Count` over the set is the distinct count:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Channeled Energy: Convert To Set
Maximum Technique: Count
Reveal: stdout
```
```input
3,1,3,1,2
```
```output
3
```


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
