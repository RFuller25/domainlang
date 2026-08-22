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
| `[]T{}` / an empty accumulator | expr `emptylist(v)` — `v` is a type witness, like `emptyset`/`emptymap` |
| `xor` the whole column (2021 D3-ish, 2018 puzzle-input checksums) | expr `bxorall(xs)`; `bandall`/`borall` for the other two |
| `a ^ b` on Bool | expr `xor(a, b)` — there is no infix spelling |
| `fields(line)` | prim `Cursed Technique: Split Fields` — Text or per-line |
| `sscanf(line, fmt)` | prim `Cursed Technique: Match Pattern` + `Using:` template — typed holes, static output type |
| a label then a variable-length run (`Time: 7 15 30`) | a repeating hole: `Using: "{label:word}: {ns:int+ sep=\" \"}"` → `List<Int>` field |
| a file whose lines are not all one shape | `Match Pattern` `Mode: Try` — one pass per shape, each keeping its own lines |
| a line with an optional trailing clause (`fwft (72) -> a, b`) | an optional group: `"{name:word} ({w:int})[? -> {kids:word+ sep=\", \"}]"` — absent leaves `kids` empty |
| a repeating pair on one line (`3 blue, 4 red`) | a repeated group: `"{draws:( {n:int} {c:word} )+ sep=\", \"}"` → `List<Record>` |
| "was this optional part there at all?" | `{?name}` inside the group → a `Bool` field |
| a pattern buried in noise (`mul(2,4)` amid junk) | `Match Pattern` `Mode: Scan` — every occurrence inside each line |
| one file, several verbs, **in order** (`turn on` / `toggle`) | `Case: <tag> "<template>"` lines → a `kind` field naming the match |
| column-aligned input (`Register A:   12`) | `{~}` — a run of whitespace, owning no field |
| a hex literal (`#70c710`) | `{c:hex}` → an `Int`, parsed base 16 |
| digits whose leading zeros matter | `{d:digits}` → `Text`, not `Int` |
| exactly one character | `{c:char}` — `{word}` is a whole run |

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
| `Transpose(rows)` | prim `Transpose` — a `Grid<T>` or a `List<List<T>>`, so no `Convert To Grid` detour |
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
| iterate a map | pass it straight to `Map Each`/`Filter`/`Sort By`/… — the list-shaped primitives read a Map as its entries |
| transform values | prim `Cursed Technique: Map Values` |
| filter entries | prim `Cursed Technique: Filter Entries` |
| `[]Pair` from a map | prim `Channeled Energy: Convert To Entries` (and `Convert To Map` back) |
| "most common element" | `Count By` → `Maximum Technique: Max By` on `item(e, 1)` — no `Convert To Entries` detour |

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
primitives below wearing a disguise — or, when the graph is not a grid, a
`Graph<K>` (if the edges are in the input) or `Domain Expansion: Explore` (if
they are generated as you go).

## Graph search

All searches are `Domain Expansion`s: named algorithm requests the
optimizer is allowed to substitute.

| Classic helper | In Domain |
|---|---|
| `BFS(start, isWalkable)` | `Domain Expansion: BFS from R C` + `Using:` walkable predicate → `Grid<Int>` step distances (−1 unreachable) |
| `Dijkstra(start, cost)` | `Domain Expansion: Dijkstra from R C` over a `Grid<Int>` of cell entry costs → `Grid<Int>` min total costs |
| `Dijkstra` over an **explicit** graph | `Channeled Energy: Convert To Graph` then `Domain Expansion: Dijkstra` `Start:` → `Map<K, Int>` |
| `Dijkstra` over an **implicit** state space | `Domain Expansion: Explore` `Mode: Cheapest` + `Cost:` — states, not cells |
| cheapest cost to every state | `Domain Expansion: Explore` `Mode: Costs` → `Map<S, Int>` |
| weighted edges rather than node weights | a 2-parameter `Cost: (s, t) -> Int` |
| `FloodFill(grid, start, match)` | `Domain Expansion: Flood Fill from R C` + `Using:` region predicate → `Grid<Int>` 0/1 mask |
| counting regions | `Domain Expansion: Connected Components` + `Using:` predicate → `Int` |
| 8-connectivity (diagonals) | `Mode: 8` on any grid search; `Mode: 4` is the default |
| BFS over an **explicit** graph | `Channeled Energy: Convert To Graph` then `Domain Expansion: BFS` `Start:` → `Map<K, Int>` (unreachable absent) |
| BFS over an **implicit** state space | `Domain Expansion: Explore` + `Using:` successor lambda — states, not cells |
| the shortest path itself, not its length | `Domain Expansion: Shortest Path` `Start:`/`Goal:` → `List<K>` |
| counting pieces of an explicit graph | `Domain Expansion: Connected Components` over a `Graph<K>` — no predicate |
| fewest moves to a goal | `Domain Expansion: Explore` `Mode: Steps` + `Until:` |
| reachable-state count | `Domain Expansion: Explore` `Mode: Count` |
| shortest distances to every state | `Domain Expansion: Explore` `Mode: Distances` → `Map<S, Int>` |
| `TopoSort(deps)` | `Domain Expansion: Topological Sort` — takes a `Graph<K>`, an adjacency Map, or an edge list |
| an adjacency list you keep and re-query | `Graph<K>` — built once with `Convert To Graph`, then `neighbors`/`weight`/`degree` in any lambda |
| the root of a parsed `parent -> child` listing | expr `root(g)` — the one node with no arc in; an error when a forest or a cycle means there is not exactly one |
| how much a node's arcs weigh in total | expr `weightof(g, k)` — `degree` with the weights counted; `weightof(flipedges(g), k)` asks it of the arcs coming in |
| in-degree, without reversing the graph to get it | expr `indegree(g, k)`; `roots(g)` / `leaves(g)` for the nodes with none either way |
| `Reachable(from)` / a transitive closure | expr `reachable(g, k)` — breadth-first, including the start |
| `HasCycle(g)` | expr `hascycle(g)` — what `Topological Sort` refuses, asked without the refusal |
| removing a node and everything attached to it | expr `delnode(g, k)`; `mergegraphs(a, b)` and `undirected(g)` for the other whole-graph edits |
| `Kruskal` / `Prim` (minimum spanning tree) | `Domain Expansion: Minimum Spanning Tree` over a `Graph<K>` — a forest when the graph is in pieces |
| `Tarjan` / `Kosaraju` (strongly connected components) | `Domain Expansion: Strongly Connected Components` → `List<List<K>>`, in a topological order of the groups |
| an adjacency map to hand to something else | `Channeled Energy: Convert To Adjacency` → `Map<K, List<K>>`; `Convert To Edges` when the weights matter |

## Memoization

| Classic helper | In Domain |
|---|---|
| `Memo[K, V].Get(key, compute)` | runtime `ir.Memo` — compute-once keyed caching for primitive implementers (codegen uses it to intern generated struct declarations). |
| memoized recursion / DP | `Domain Expansion: Explore` — Domain has no recursion, and a search over keyable states is what the visited set memoizes. |
| "how many ways" over a DAG | `Domain Expansion: Explore` `Mode: Tally` + `Value:`/`Combine:` — each state folded once however many paths reach it |
| linear DP over a sorted list | `Maximum Technique: Fold` with a `Map` accumulator (`insert`/`getor`) — linear, not quadratic, since [linear accumulators](optimizer.md#linear-accumulators--the-pass-that-runs-last) |

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
| `a < b` (lexicographic) | the `<` `<=` `>` `>=` operators over two Texts — the same ordering `Sort` uses |

## List operations inside a lambda

Each of these is a primitive *and* an expression builtin, answering the same
way. The expression spelling is what a `Fold` needs, since a pipeline body
cannot stand in for its 2-parameter lambda.

| Classic helper | In Domain |
|---|---|
| `sort(xs)` | expr `sort(xs)`, or prim `Domain Expansion: Sort` |
| `unique(xs)` | expr `unique(xs)`, or prim `Cursed Technique: Unique` |
| `flatten(xss)` | expr `flatten(xss)`, or prim `Cursed Technique: Flatten` |
| `product(xs)` | expr `product(xs)`, or prim `Maximum Technique: Product` |
| `zip(a, b)` | expr `zip(a, b)`, or prim `Maximum Technique: Zip` (channels) |
| `enumerate(xs)` | expr `enumerate(xs)`, or prim `Cursed Technique: Enumerate` |
| `chunk(xs, n)` | expr `chunk(xs, n)`, or prim `Cursed Technique: Chunk n` — keeps a short final block |
| `windows(xs, n)` | expr `windows(xs, n)`, or prim `Cursed Technique: Window n` — drops a partial one |
| `transpose(xss)` | expr `transpose(xss)`, or prim `Cursed Technique: Transpose` |

## Cycle detection

| Classic helper | In Domain |
|---|---|
| "find the loop, then do the arithmetic" | prim `Maximum Technique: Find Cycle` over an `Iterate` trajectory → `(start, period)` |
