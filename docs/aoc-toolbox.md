# The AoC toolbox — where each classic helper lives in Domain

Every Advent of Code veteran carries a personal helper library: parsing
one-liners, a grid type with neighbor walks, a queue and a heap for searches,
union-find, gcd/lcm, interval merging, combinatorics. This page maps that
canonical toolbox onto Domain, item by item, so you can find the equivalent
without guessing. Detailed semantics live in
[primitives.md](primitives.md) and [expressions.md](expressions.md).

Legend: **prim** = pipeline primitive, **expr** = expression-layer builtin,
**prelude** = Shikigami loaded before every program, **runtime** = a Go
type in `ir/` that powers primitives (not directly user-visible). The whole
toolbox is supported by both backends — `domain run` and compiled binaries —
with oracle tests pinning identical output ([compiler.md](compiler.md)).

## Parsing / input

| Classic helper | In Domain |
|---|---|
| `parseLines(raw)` | prelude `Shikigami: Lines`, or `Cursed Technique: Split Text by "\n"` |
| `parseBlocks(raw)` | prelude `Shikigami: Blocks` (split on `"\n\n"`, then each on `"\n"`) |
| `parseInts(line)` | prim `Cursed Technique: Extract Integers` — Text or per-line |
| `parseGrid(raw)` | `Channeled Energy: Convert To Grid` (chars) or prelude `Digit Grid` |
| `splitOn(line, sep)` | `Cursed Technique: Split Text by "sep"` / `Split Each by "sep"` |
| `atoi(s)` | expr `toint(s)`; whole lists via `Channeled Energy: Convert To Integers` |
| `fields(line)` | prim `Cursed Technique: Split Fields` — Text or per-line |

## Grid / 2D

Points are `(Int, Int)` tuples of `(row, col)` — the grid coordinate system.

| Classic helper | In Domain |
|---|---|
| `Point{X, Y}` | expr `point(r, c)`, accessors `prow(p)` / `pcol(p)` |
| `Grid.At(p)` | expr `at(g, r, c)` (error out of bounds); guard with `inbounds` first |
| `Grid.InBounds(p)` | expr `inbounds(g, r, c)` |
| `Grid.Neighbors4(p)` | expr `neighbors4(g, r, c)` — in-bounds only |
| `Grid.SubGrid(r, c, h, w)` | prim `Cursed Technique: Subgrid r c h w` |
| pad a border before a fill | prim `Cursed Technique: Pad Grid n` + `Fill:` |
| `Grid.Rotate/Flip` | prim `Rotate Grid` / `Flip Grid` |
| `Grid.Neighbors8(p)` | expr `neighbors8(g, r, c)` |
| `Grid.Find(target)` | prim `Cursed Technique: Find Cells` + `Using:` predicate |
| `Point.Add(o)` | expr `padd(p, q)` |
| `Point.Manhattan(o)` | expr `manhattan(p, q)` |
| `Dirs4` | expr `dirs4()` — up, down, left, right unit vectors |
| `Dirs8` | expr `dirs8()` — the eight, diagonals included |
| `Point.Sub(o)` | expr `psub(p, q)` |
| `Point.Scale(n)` | expr `pscale(p, n)` — step n along a direction |
| `Point.Chebyshev(o)` | expr `chebyshev(p, q)` |
| `Neighbors(p)` without a grid | expr `around4(p)` / `around8(p)` — no bounds check, for `Sparse<T>` |
| `Point.RotateRight()` | expr `rotr(p)` — `rotr(up)` is right |
| `Point.RotateLeft()` | expr `rotl(p)` |

Grid transforms (`Transpose`, `Map Cells`, `Count Cells`) are primitives of
their own — see [primitives.md](primitives.md).

For the *sparse* half of the classic library — `map[Point]T` with a default,
unknown extent, negative coordinates — use `Sparse<T>`:

| Classic helper | In Domain |
|---|---|
| `map[Point]T` + default | `Channeled Energy: Convert To Sparse Grid` (`Default:`, `Mark:`) |
| `m[p]` with zero-value read | expr `at(g, r, c)` — total, unset reads the default |
| `m[p] = v` | expr `put(g, r, c, v)` (functional) |
| `_, ok := m[p]` | expr `has(g, r, c)` |
| `len(m)` | expr `cells(g)` |
| bounding box scan | expr `minrow`/`maxrow`/`mincol`/`maxcol`, or densify |
| "print the board" | `Channeled Energy: Convert To Grid` (translates to (0,0), default-fills) |
| neighbor walk on a sparse plane | expr `around4`/`around8` — `neighbors4`/`neighbors8` need a dense Grid |

See the Sparse section of [data-model.md](data-model.md) and the Game of
Life / origami / Minesweeper programs in
[../challenges/](../challenges/README.md).

## Set

| Classic helper | In Domain |
|---|---|
| `Set[T]` | the `Set<T>` type (Int/Text elements, insertion-ordered) |
| `Set.Add(v)` | build with `Channeled Energy: Convert To Set`, `Union`, or `Intersect`; values are immutable, so "add" is set union |
| `Set.Has(v)` | expr `contains(s, v)` |
| `Set.Len()` | `Maximum Technique: Count` |
| difference | `Maximum Technique: Difference` (Channel consumer) |
| iterate a set | pass it straight to `Map Each`/`Filter`/… — the list-shaped primitives accept a Set — or expr `tolist(s)` inside a lambda |

## Map / frequency table

| Classic helper | In Domain |
|---|---|
| `map[K]V` from a fold | `Maximum Technique: Group By` / `Count By` |
| `m[k]` | expr `get(m, k)` (errors when absent) or `getor(m, k, d)` (total) |
| `_, ok := m[k]` | expr `haskey(m, k)` |
| `len(m)` | expr `size(m)`, or `Maximum Technique: Count` |
| iterate keys / values | expr `keys(m)` / `values(m)` |
| transform values | prim `Cursed Technique: Map Values` |
| filter entries | prim `Cursed Technique: Filter Entries` |
| `[]Pair` from a map | prim `Channeled Energy: Convert To Entries` (and `Convert To Map` back) |
| "most common element" | `Count By` → `Convert To Entries` → `Sort By, Descending` → `Take Item 0` |

## Queue / Stack / Priority queue

Domain has no user-visible mutable containers — pipelines transform values.
The traversal structures exist as runtime types powering the search
primitives, each with its own unit tests:

| Classic helper | In Domain |
|---|---|
| `Queue[T]` (push back / pop front) | runtime `ir.Queue` — drives **BFS** |
| `Stack[T]` | runtime `ir.Stack` — drives **Flood Fill** |
| `PQ[T]` (push with priority / pop min) | runtime `ir.PQ` (min-heap, stable ties) — drives **Dijkstra** |

If a solve seems to need an explicit queue, it is usually one of the search
primitives below wearing a disguise — or, when the graph is not a grid,
`Domain Expansion: Explore`.

## Graph search

All searches are `Domain Expansion`s: named algorithm requests the
optimizer is allowed to substitute.

| Classic helper | In Domain |
|---|---|
| `BFS(start, isWalkable)` | `Domain Expansion: BFS from R C` + `Using:` walkable predicate → `Grid<Int>` step distances (−1 unreachable) |
| `Dijkstra(start, cost)` | `Domain Expansion: Dijkstra from R C` over a `Grid<Int>` of cell entry costs → `Grid<Int>` min total costs |
| `FloodFill(grid, start, match)` | `Domain Expansion: Flood Fill from R C` + `Using:` region predicate → `Grid<Int>` 0/1 mask |
| counting regions | `Domain Expansion: Connected Components` + `Using:` predicate → `Int` |
| 8-connectivity (diagonals) | `Mode: 8` on any grid search; `Mode: 4` is the default |
| BFS over a **non-grid** graph | `Domain Expansion: Explore` + `Using:` successor lambda — states, not cells |
| fewest moves to a goal | `Domain Expansion: Explore` `Mode: Steps` + `Until:` |
| reachable-state count | `Domain Expansion: Explore` `Mode: Count` |
| shortest distances to every state | `Domain Expansion: Explore` `Mode: Distances` → `Map<S, Int>` |
| `TopoSort(deps)` | `Domain Expansion: Topological Sort` — takes an adjacency Map or an edge list |

## Memoization

| Classic helper | In Domain |
|---|---|
| `Memo[K, V].Get(key, compute)` | runtime `ir.Memo` — compute-once keyed caching for primitive implementers (codegen uses it to intern generated struct declarations). |
| memoized recursion / DP | `Domain Expansion: Explore` — Domain has no recursion, and a search over keyable states is what the visited set memoizes. |

## Union-Find

| Classic helper | In Domain |
|---|---|
| `NewUnionFind(n)` / `Find` / `Union` / `Connected` | runtime `ir.UnionFind` (path compression + union by size) — drives **Connected Components** |

## Math / number theory

All expression builtins; all compile.

| Classic helper | In Domain |
|---|---|
| `GCD(a, b)` | expr `gcd(a, b)` |
| `Mod(a, b)` (Euclidean) | expr `mod(a, b)` or the `%` operator — never negative for a positive modulus |
| `DivMod(a, b)` | expr `divmod(a, b)` |
| `Pow(b, e)` | expr `pow(b, e)` |
| `IntSqrt(n)` | expr `isqrt(n)` — exact at perfect squares |
| `Clamp(v, lo, hi)` | expr `clamp(v, lo, hi)` |
| `Factorial(n)` / `Choose(n, k)` | expr `factorial(n)` / `choose(n, k)` |
| `Min(a, b)` / `Max(a, b)` | expr `min(a, b)` / `max(a, b)` (two-argument form) |
| `LCM(a, b)` | expr `lcm(a, b)` |
| `Abs(x)` | expr `abs(x)` |
| `Sign(x)` | expr `sign(x)` |
| `ModPow(base, exp, mod)` | expr `modpow(b, e, m)` |
| `ModInverse(a, mod)` | expr `modinv(a, m)` |
| `SolveLinear2x2(...)` | expr `solve2x2(a, b, c, d, e, f)` — exact integer arithmetic; errors instead of returning floats |

## Interval / Range

| Classic helper | In Domain |
|---|---|
| `for i := lo; i < hi; i++` | prim `Cursed Technique: Range lo hi` — half-open, like `range(N)` in a For header |
| `Range{Lo, Hi}` | a two-Int-field record (`Match Pattern: "{lo:int}-{hi:int}"`) or an `(Int, Int)` pair |
| `Range.Contains(x)` | lambda: `(r) -> r.lo <= x and x <= r.hi` |
| `Range.Overlaps(o)` | lambda: `(a, b) -> a.lo <= b.hi and b.lo <= a.hi` |
| `MergeRanges(rs)` | prim `Maximum Technique: Merge Ranges` — sorts and coalesces overlapping/adjacent inclusive ranges |

## Combinatorics

| Classic helper | In Domain |
|---|---|
| `Permutations(items)` | `Domain Expansion: Permutations` — bounded at 9 elements |
| `Combinations(items, k)` | `Domain Expansion: Combinations k` (+ `Mode:`/`Using:` to filter/count/map in place) |
| `Subsets(items)` | `Domain Expansion: Subsets` — the power set, bounded at 16 elements |

## String helpers

| Classic helper | In Domain |
|---|---|
| `IsRepeatedPattern(s)` | expr `repeats(s)` |
| `CountOccurrences(s, sub)` | expr `occurrences(s, sub)` |
| `len(s)` | expr `length(s)` — runes |
| `s[i]` | expr `charat(s, i)` |
| `s[a:b]` | expr `slice(s, a, b)` — clamped |
| `strings.Index` | expr `indexof(s, sub)` — `-1` when absent |
| `strings.HasPrefix` / `HasSuffix` | expr `startswith` / `endswith` |
| `strings.ReplaceAll` | expr `replace(s, old, new)` |
| `strings.TrimSpace` | expr `trim(s)` |
| `strings.ToUpper` / `ToLower` | expr `upper(s)` / `lower(s)` |
| `strings.Split(s, "")` | expr `chars(s)` |
| `strings.Join` | expr `textjoin(xs, sep)` |
| reverse a string | prim `Reverse Cursed Technique: Reverse` over Text, or expr `reverse(s)` |
| `a + b` | the `+` operator over two Texts |

## Cycle detection

| Classic helper | In Domain |
|---|---|
| "find the loop, then do the arithmetic" | prim `Maximum Technique: Find Cycle` over an `Iterate` trajectory → `(start, period)` |
