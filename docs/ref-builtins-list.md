# List builtins

Part of the [expression layer reference](expressions.md).

### Lists, maps, grids

The read side: indexing, slicing and the questions that guard them.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> item(xs, 0) + last(xs) + length(xs)
Reveal: stdout
```
```input
10,20,30
```
```output
43
```

`take`, `drop` and `slice` are total — they clamp rather than erroring, which
is what lets them be used without a length check first:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Apply
    Using: (xs) -> concat(take(xs, 99), slice(xs, 1, 2))
Reveal: stdout
```
```input
a,b,c
```
```output
[a, b, c, b]
```


| Builtin | Type | Behavior |
|---|---|---|
| `length(xs)` | `List<T> -> Int` | Number of elements. |
| `item(xs, i)` | `List<T> × Int -> T` | 0-based element. **Error** if `i` is out of range. |
| `take(xs, n)` | `List<T> × Int -> List<T>` | First `n` elements. Total: `n` clamps to `[0, length]`. |
| `drop(xs, n)` | `List<T> × Int -> List<T>` | All but the first `n`. Clamps like `take`. |
| `reverse(xs)` | `List<T> -> List<T>` | Reversed copy. |
| `concat(a, b)` | `List<T> × List<T> -> List<T>` | `a` then `b`. Both lists must share one type. |
| `first(xs)` | `List<T> -> T` | **Error** on the empty list. |
| `last(xs)` | `List<T> -> T` | **Error** on the empty list. |
| `sum(xs)` | `List<N> -> N` (N = Int or Float) | Total; `0` for the empty list. |
| `min(xs)` | `List<N> -> N` (N = Int or Float) | **Error** on the empty list. |
| `max(xs)` | `List<N> -> N` (N = Int or Float) | **Error** on the empty list. |
| `contains(xs, v)` | `List<T> \| Set<T> × T -> Bool` (T keyable) | Membership over a list or a set. Element type must be keyable (Int, Text, or Tuples/Records of them) — so a set of points works. |
| `get(m, k)` | `Map<K,V> × K -> V` | Lookup. **Error** if the key is absent. |
| `at(g, r, c)` | `Grid<T> \| Sparse<T> × Int × Int -> T` | 0-based `(row, col)` cell. Dense: **error** out of bounds. Sparse: total — unset cells read the default. |
| `inbounds(g, r, c)` | `Grid<T> × Int × Int -> Bool` | Whether `(r, c)` is a legal cell. Pairs with `at` under short-circuit `and`. |
| `list(a, b, …)` | `T × … -> List<T>` (≥ 1 arg) | Construct a list; all elements must share one type. |
| `emptylist(v)` | `T -> List<T>` | The empty list. `v` is a **type witness**, never stored — the same trick `emptyset`/`emptymap` play, and the reason `list()` is not the spelling: with no arguments there is nothing to read the element type from, and every expression's type is fixed at resolve time. |
| `set(xs, i, v)` | `List<T> × Int × T -> List<T>` | Copy of `xs` with element `i` replaced (functional update). **Error** if `i` is out of range. |
| `row(g, r)` | `Grid<T> × Int -> List<T>` | Row `r` as a list. **Error** out of range. |
| `col(g, c)` | `Grid<T> × Int -> List<T>` | Column `c` as a list. **Error** out of range. |
| `rows(g)` / `cols(g)` | `Grid<T> -> Int` | The grid's dimensions. |
| `slice(xs, lo, hi)` | `List<T> × Int × Int -> List<T>` | Half-open `[lo, hi)`. Total: bounds clamp like `take`/`drop`, and an inverted range gives the empty list. |
| `indexof(xs, v)` | `List<T> × T -> Int` (T keyable) | Position of the first equal element, or `-1` — the sentinel `Find Index` uses. |
| `tuple(a, b, …)` | `T1 × T2 × … -> (T1, T2, …)` (≥ 2 args) | Build a **heterogeneous** tuple. Unlike `list`, the elements need not share a type — this is how a mixed `Group By` key or a `Sort By` tiebreak is written. |
| `range(lo, hi)` | `Int × Int -> List<Int>` | The half-open `[lo, hi)`, matching the `Range` primitive. Empty when `hi <= lo`. |
| `fill(n, v)` | `Int × T -> List<T>` | `n` copies of `v`. Total: a negative count is the empty list, like `take`. |
| `item(t, i)` | `(T1, …) × Int -> Ti` | Tuple element. The index must be a **literal**: the elements have different types, so the result type is only knowable when the position is. Compiles to a direct struct field. |
| `length(t)` | `(T1, …) -> Int` | A tuple's arity. |

### First-order list operations

The list primitives, reachable from inside a lambda — which is what lets a
per-element job sort or deduplicate without a nested body:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Using: (xs) -> first(sort(xs))
Reveal: stdout
```
```input
3 1 2
9 7 8
```
```output
[1, 7]
```

None of them takes a function argument, which is the rule they respect — they
are ordinary values in, ordinary values out:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Apply
    Using: (xs) -> length(unique(xs))
Reveal: stdout
```
```input
a,b,a,c
```
```output
3
```


None of these takes a function argument, so none of them is a higher-order
builtin — they were simply absent, and each absence forced a nested pipeline
body where an expression would have done. Inside a `Fold`, where a body cannot
stand in for a 2-parameter lambda at all, the absence was total.

Each mirrors the primitive of the same job exactly, because the two spellings
have to answer the same question the same way.

| Builtin | Type | Behavior |
|---|---|---|
| `sort(xs)` | `List<T> -> List<T>` (T ordered) | Ascending, stable, over the ordering [`Sort`](ref-expansions.md#sort--quicksort--listt---listt-t-ordered) and `<` share. |
| `unique(xs)` | `List<T> -> List<T>` (T keyable) | Deduplicated, keeping first-seen order — `Unique`, inside a lambda. |
| `flatten(xss)` | `List<List<T>> -> List<T>` | One level, left to right. |
| `product(xs)` | `List<N> -> N` (N = Int or Float) | The product; `1` for the empty list, as `sum` is `0`. |
| `zip(a, b)` | `List<A> × List<B> -> List<(A, B)>` | Element-wise pairs, truncated to the shorter — the `Zip` consumer without the channels. |
| `enumerate(xs)` | `List<T> -> List<(Int, T)>` | Each element tupled with its 0-based index. |
| `chunk(xs, n)` | `List<T> × Int -> List<List<T>>` | Non-overlapping blocks, **keeping a short final one**. **Error** if `n < 1`. |
| `windows(xs, n)` | `List<T> × Int -> List<List<T>>` | Sliding windows of exactly `n`, so a trailing partial one is **dropped** — the difference from `chunk`. **Error** if `n < 1`. |
| `transpose(xss)` | `List<List<T>> -> List<List<T>>` | Rows become columns. **Error** on a ragged input, naming the row. |

A `Set` or a `Map` reaches these through `tolist` and `entries`, which is the
same bridge the rest of the table uses.
