# Map and Set builtins

Part of the [expression layer reference](expressions.md).

### Maps and Sets

The total lookups, which is what a frequency map needs — `get` errors on a
missing key and `getor` does not:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Maximum Technique: Count By
    Using: (c) -> c
Cursed Technique: Apply
    Using: (m) -> getor(m, "a", 0) + getor(m, "z", 0)
Reveal: stdout
```
```input
banana
```
```output
3
```

`keys`, `values` and `size` read a map without leaving the expression layer,
and `haskey` is the guard `get` never had:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Maximum Technique: Count By
    Using: (c) -> c
Cursed Technique: Apply
    Using: (m) -> if haskey(m, "n") then size(m) else -1
Reveal: stdout
```
```input
banana
```
```output
3
```


`Group By` and `Count By` are among the most reachable primitives and both
produce a `Map`, so reading one back has to be as ordinary as building it.
`get` **errors** on a missing key; `haskey` and `getor` are the two ways to ask
without one, and a frequency map is normally queried through `getor`.

| Builtin | Type | Behavior |
|---|---|---|
| `haskey(m, k)` | `Map<K,V> × K -> Bool` | Whether the key is present. The guard `get` never had. |
| `getor(m, k, d)` | `Map<K,V> × K × V -> V` | Total lookup: the value, or `d` when absent. |
| `keys(m)` | `Map<K,V> -> List<K>` | Keys in insertion order. |
| `values(m)` | `Map<K,V> -> List<V>` | Values in the same order. |
| `size(m)` | `Map<K,V> \| Set<T> \| Graph<K> -> Int` | Entry count (a `Graph`'s node count) — `Count`, without leaving the lambda. |
| `tolist(s)` | `Set<T> -> List<T>` | Elements in insertion order. Without it a `Set` is a dead end: `Map Each` has no Set case. |

### Graphs

An edge list becomes a graph, and the graph answers questions about itself:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Cursed Technique: Apply
    Using: (rs) -> graph(rs)
Cursed Technique: Apply
    Using: (g) -> textjoin(list(
        totext(size(g)),
        textjoin(nodes(g), "/"),
        textjoin(neighbors(g, "a"), "/"),
        if hasedge(g, "c", "a") then "cycle" else "no cycle"
    ), " ")
Reveal: stdout
```
```input
a -> b
a -> c
b -> c
```
```output
3 a/b/c b/c no cycle
```

Weights are `Int` and default to 1, so an unweighted graph needs no separate
spelling — and a graph renders as the adjacency listing it is:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (s) -> addnode(addedge(addedge(emptygraph(""), "a", "b"), "a", "c", 7), "q")
Reveal: stdout
```
```input
```
```output
{a: [(b, 1), (c, 7)], b: [], c: [], q: []}
```


A `Graph<K>` is a **directed**, `Int`-weighted adjacency over keyable nodes.
See [data-model.md](data-model.md) for the full contract; the short version is
four rules:

- **Directed.** An undirected graph is one with both arcs — that is what
  `Convert To Graph`'s `Mode: Undirected` inserts.
- **`Int` weights, default `1`.** An unweighted graph is one whose weights are
  all `1`, so no algorithm needs a separate unweighted path.
- **Insertion-ordered**, like `Map` and `Set`. An edge brings its endpoints
  into the graph, so an edge list is a complete description; `addnode` is for
  the isolated nodes an edge list cannot mention.
- **A repeated edge re-weights**, in place, rather than adding a parallel one —
  which is what keeps `weight(g, a, b)` a question with one answer.

Nodes must be keyable (`Int`, `Text`, or a tuple/record of them), the same rule
`Set` elements and `Explore`'s state follow: the visited set is what makes a
traversal terminate. A `Graph` is not itself keyable or ordered, so it cannot
be a `Map` key, a `Set` element, or sorted.

| Builtin | Type | Behavior |
|---|---|---|
| `graph(es)` | `List<(K,K)> \| List<(K,K,Int)> \| List<List<K>> -> Graph<K>` | Build from an edge list. Pairs weigh 1; the ragged `List<List<K>>` form is what a positional `Match Pattern` produces, and also weighs 1. |
| `emptygraph(k)` | `K -> Graph<K>` | The empty graph. The argument is a *type witness* — evaluated, then discarded — like `emptyset`'s. |
| `addnode(g, k)` | `Graph<K> × K -> Graph<K>` | A copy with `k` present. An existing node keeps its arcs. |
| `addedge(g, a, b)` / `addedge(g, a, b, w)` | `Graph<K> × K × K [× Int] -> Graph<K>` | A copy with the arc `a → b` (weight `w`, default 1). Both endpoints join the graph. A repeat re-weights. |
| `deledge(g, a, b)` | `Graph<K> × K × K -> Graph<K>` | A copy without that arc. The nodes stay. |
| `nodes(g)` | `Graph<K> -> List<K>` | Nodes in insertion order. |
| `edges(g)` | `Graph<K> -> List<(K, K, Int)>` | Every arc as `(from, to, weight)`. |
| `neighbors(g, k)` | `Graph<K> × K -> List<K>` | The destinations of `k`'s out-arcs. A node not in the graph has none — total, not an error. |
| `edgesof(g, k)` | `Graph<K> × K -> List<(K, Int)>` | `neighbors` with the weights. |
| `hasedge(g, a, b)` | `Graph<K> × K × K -> Bool` | Whether `a → b` is an arc. |
| `weight(g, a, b)` | `Graph<K> × K × K -> Int` | The arc's weight. **Error** if there is no such arc. |
| `weightor(g, a, b, d)` | `Graph<K> × K × K × Int -> Int` | `weight` with a default — the total twin, exactly as `getor` is to `get`. |
| `degree(g, k)` | `Graph<K> × K -> Int` | Out-arc count; 0 for a node not in the graph. |
| `flipedges(g)` | `Graph<K> -> Graph<K>` | Every arc reversed, node order kept. (`reverse` and `transpose` are taken.) |
| `subgraph(g, ks)` | `Graph<K> × List<K> -> Graph<K>` | Restricted to `ks`, keeping only arcs with both endpoints in it. A name not in `g` is skipped, not invented. |
| `size(g)` | `Graph<K> -> Int` | Node count. The arc count is `length(edges(g))` — a different question. |
| `contains(g, k)` | `Graph<K> × K -> Bool` | Node membership. |

Every update is **functional**, like `insert`/`put`/`setat`: it returns a new
graph and leaves its receiver untouched. The dead-receiver rewrite below
reaches `addnode` and `addedge`, so a graph accumulated in a `Fold` is linear
in its writes rather than quadratic — 12,000 edges into a 900-node graph takes
0.03 s against 2.1 s with `--no-optimize`. `deledge` is deliberately not
rewritten, for the reason `del` is not: removing an arc shifts the ones behind
it.

Equality is **order-insensitive**: two graphs built by reading the same edges
in a different order are the same graph, so `=` and `Iterate Until Fixed Point`
agree with that. Rendering still follows insertion order, and always shows
weights:

```
{a: [(b, 1), (c, 2)], b: [(c, 1)], c: []}
```

### Building and updating a collection

```domain
Maximum Technique: Fold                  # a frequency map, in one lambda
    Seed: (xs) -> emptymap("", 0)
    Using: (acc, w) -> insert(acc, w, getor(acc, w, 0) + 1)
```

Every update is functional, but it is not always a copy. Where the optimizer
can prove nothing reads the copied-from value after an update — which is the
usual case in a `Fold`, whose accumulator is dead the moment the lambda
returns — it marks the site and both backends write through instead, so the
fold above is linear in its writes rather than quadratic in the map. The
accumulator is cloned once on entry, because a `Part` or a `Channel` may be
holding the same value; see
[optimizer.md](optimizer.md#linear-accumulators--the-pass-that-runs-last) for
what has to be true, and `--no-optimize` for the copying behaviour.

**Three shapes the rewrite does not reach**, because the cost is real and
silent when it applies:

- **`set` on a `List`.** It always copies. `take`/`drop`/`slice` hand out a
  subslice of the same backing array, so an in-place write would be visible
  through one taken earlier. A fold that writes into a long list is quadratic;
  20,000 writes into a 100k list takes 26 s, against 0.1 s for the same shape
  over a `Map`. Prefer a `Map<Int, V>` accumulator when the list is large, or
  rebuild with `concat`, which allocates. See
  [aoc-gaps.md](aoc-gaps.md#14-set-on-a-list-accumulator-is-still-osize).
- **`with` on a `Record`.** Also always a copy, but O(fields) — small enough
  that it has never been what made anything slow.
- **Loop bodies.** `Repeat`, `While` and `For` take a sub-pipeline rather than
  a lambda, so there is no accumulator parameter to follow and no site to mark.
  A simulation written as `Repeat N` over a `Map` state copies every lap; the
  same work written as a `Fold` does not.

The proof can fail, and then the copy stays: a body that still reads the
accumulator *after* updating it needs the old value, so it gets it. `del` is
never done in place. And a `Count By` or `Group By` still builds one
collection in one pass, which beats any fold.

A frequency map built one write at a time — `emptymap`'s arguments are type
witnesses, and `getor` supplies the zero the first write needs:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Maximum Technique: Fold
    Seed: (xs) -> emptymap("", 0)
    Using: (acc, w) -> insert(acc, w, getor(acc, w, 0) + 1)
Reveal: stdout
```
```input
a,b,a
```
```output
{a: 2, b: 1}
```

The same shape over a `Set`, with `insert`'s two-argument form:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Maximum Technique: Fold
    Seed: (xs) -> emptyset("")
    Using: (acc, w) -> insert(acc, w)
Maximum Technique: Count
Reveal: stdout
```
```input
a,b,a,c
```
```output
3
```


Every collection has a constructor and a functional update reachable from
inside an expression, so any of them can be a `Fold` accumulator — including
through a measured `Seed:`, which is itself an expression.

Every update below is **functional**: it returns a new collection and leaves its
argument untouched. That is not a stylistic choice — a lambda may be applied to
the same value twice (the optimizer folds constants by doing exactly that), and
an in-place update would let the second application see the first one's work.

| Builtin | Type | Behavior |
|---|---|---|
| `toset(xs)` | `List<T> -> Set<T>` (T keyable) | Deduplicates, keeping first-seen order — `Convert To Set` inside a lambda. |
| `emptyset(v)` | `T -> Set<T>` | The empty set. `v` is a **type witness**, never stored: an absence cannot say what it is an absence of, which is the same reason `sparse(d)` takes a default. |
| `tomap(ps)` | `List<(K,V)> -> Map<K,V>` (K keyable) | Builds from key/value pairs; last write wins. |
| `emptymap(k, v)` | `K × V -> Map<K,V>` | The empty map; both arguments are type witnesses. |
| `entries(m)` | `Map<K,V> -> List<(K,V)>` | Pairs in insertion order — the inverse of `tomap`. |
| `insert(s, v)` | `Set<T> × T -> Set<T>` | A copy with `v` added. |
| `insert(m, k, v)` | `Map<K,V> × K × V -> Map<K,V>` | A copy with `k` bound to `v`. |
| `del(s, v)` | `Set<T> × T -> Set<T>` | A copy without `v`. Absent is not an error. |
| `del(m, k)` | `Map<K,V> × K -> Map<K,V>` | A copy without `k`. Absent is not an error. |
| `union(a, b)` | `Set<T> × Set<T> -> Set<T>` | All of `a`, then `b`'s new elements. |
| `intersect(a, b)` | `Set<T> × Set<T> -> Set<T>` | Elements in both, in `a`'s order. |
| `difference(a, b)` | `Set<T> × Set<T> -> Set<T>` | Elements of `a` not in `b`. |
| `setat(g, r, c, v)` | `Grid<T> × Int × Int × T -> Grid<T>` | A copy with cell `(r, c)` replaced. **Error** out of bounds — a dense grid is finite, so unlike `put` there is no cell to write. |
| `cellpoints(g)` | `Sparse<T> -> List<(Int, Int)>` | The set cells' coordinates in sorted row-major order. `cells` counts them; this is what walks them. |

So a fold can now carry real state:
