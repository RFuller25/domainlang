# Swappable algorithms — `Domain Expansion`

One class of the [primitive reference](primitives.md).

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

Naming the algorithm is a *request*, not a command — the optimizer may answer
it with a different one, and the result is the same either way:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Quicksort
Reveal: stdout
```
```input
5,1,9,3
```
```output
[1, 3, 5, 9]
```

`List<Text>` sorts alphabetically, because ordered means Int, Float, Text, or
a tuple of those — no comparison lambda is needed for the common case:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Domain Expansion: Sort, Descending
Reveal: stdout
```
```input
pear,apple,fig
```
```output
[pear, fig, apple]
```

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Domain Expansion: Sort By
    Using: (w) -> length(w)
Reveal: stdout
```
```input
banana
fig
apple
```
```output
[fig, apple, banana]
```

A tuple key is the whole tiebreak mechanism — sort on the first component,
then the second, in one pass:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Domain Expansion: Sort By
    Using: (w) -> tuple(length(w), w)
Reveal: stdout
```
```input
pear
fig
cat
apple
```
```output
[cat, fig, pear, apple]
```

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
[`Map Each` block](ref-transforms.md#map-each--listt--t---u---listu), whose body runs once per
element.

`All Pairs` with a sum-to-constant predicate (`Mode: First` or `Count`) is
rewritten to an O(n) hash-set scan — see [optimizer.md](optimizer.md).

The 2020 Day 1 opening, and the shape the optimizer rewrites into a hash-set
scan:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: All Pairs
    Mode: First
    Using: (a, b) -> a + b = 2020
Maximum Technique: Product
Reveal: stdout
```
```input
1721
979
366
299
675
1456
```
```output
514579
```

`Mode: Count` asks how many rather than which, and `Combinations k` widens the
lambda to k parameters:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Combinations 3
    Mode: Count
    Using: (a, b, c) -> a + b + c = 6
Reveal: stdout
```
```input
1,2,3,4
```
```output
1
```

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Sliding Reduce 3
    Mode: Sum
Reveal: stdout
```
```input
1,2,3,4,5
```
```output
[6, 9, 12]
```

`Max` is the monotonic-deque one, still O(n) however wide the window is:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Sliding Reduce 3
    Mode: Max
Reveal: stdout
```
```input
1,9,2,3,8
```
```output
[9, 9, 8]
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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Domain Expansion: Permutations
Reveal: stdout
```
```input
a,b,c
```
```output
[[a, b, c], [a, c, b], [b, a, c], [b, c, a], [c, a, b], [c, b, a]]
```

`n!` of them, which is the number a following reduction is really iterating —
usually to find the best ordering rather than to look at the list:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Permutations
Maximum Technique: Count
Reveal: stdout
```
```input
1,2,3,4
```
```output
24
```

### Subsets — `List<T> -> List<List<T>>`

```domain
Domain Expansion: Subsets
```

The power set. Subset k includes element i iff bit i of k is set, so the
empty set comes first and the full list last; each subset preserves element
order. **Unbounded**, for the same reason as `Permutations` (`2^n` still
explodes; `prims.MaxSubsetInput` restores a ceiling if you want one).

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Domain Expansion: Subsets
Reveal: stdout
```
```input
a,b
```
```output
[[], [a], [b], [a, b]]
```

The empty subset is included, so there are `2^n` of them:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Domain Expansion: Subsets
Maximum Technique: Count
Reveal: stdout
```
```input
a,b,c,d
```
```output
16
```

### Explore — `S × (S -> List<S>) -> List<S> | Int | Map<S,Int> | V` (S keyable)

```domain
Domain Expansion: Explore
    Mode: Steps                        # see the table below
    Until: (s) -> s = target
    Using: (s) -> successors(s)
```

Search over the **implicit** graph the successor lambda describes. The seed is
the current pipeline value, and the visited set both bounds the search over a
cyclic space and answers "how many distinct configurations".

| Mode | Result | Extra arguments |
|---|---|---|
| `Collect` (default) | `List<S>` — every reachable state, in BFS order, seed first | |
| `Count` | `Int` — how many distinct states | |
| `Distances` | `Map<S, Int>` — shortest step count from the seed to each | |
| `Steps` | `Int` — steps to the first state satisfying `Until:`, or `-1` | `Until:` |
| `Cheapest` | `Int` — cheapest total `Cost:` to the first `Until:` hit, or `-1` | `Cost:`, `Until:` |
| `Costs` | `Map<S, Int>` — cheapest total `Cost:` to each state | `Cost:` |
| `Tally` | `V` — the reachable states folded to one value | `Value:`, `Combine:` |

`Until:` is required by `Steps` and `Cheapest`, and optional elsewhere, where
it prunes: a satisfying state is recorded but never expanded.

**The step-counting modes are breadth-first**, which is what makes their step
counts the *shortest* ones.

Collatz, as an implicit graph — the seed is the current pipeline value and the
successor lambda draws the edges:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Domain Expansion: Explore
    Mode: Steps
    Until: (s) -> s = 1
    Using: (s) -> if mod(s, 2) = 0 then list(s / 2) else list(3 * s + 1)
Reveal: stdout
```
```input
6
```
```output
8
```

`Mode: Count` asks how many distinct states are reachable instead, which is
what the visited set was already tracking:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Domain Expansion: Explore
    Mode: Count
    Using: (s) -> if s >= 10 then emptylist(s) else list(s + 1, s + 2)
Reveal: stdout
```
```input
7
```
```output
5
```

#### `Cost:` — the weighted search

`Cheapest` and `Costs` are Dijkstra over the same implicit graph: the frontier
is a min-heap keyed by cost so far, and a state is **settled** the first time
it is popped rather than the first time it is seen.

```domain ignore
Domain Expansion: Explore
    Mode: Cheapest
    Cost: (t) -> weight(t)             # entering t costs this
    Until: (s) -> s = goal
    Using: (s) -> successors(s)
```

`Cost:` comes in two arities, because both questions get asked:

- **`(t) -> Int`** is the cost of *entering* a state — the convention grid
  [`Dijkstra`](#dijkstra--gridint---gridint--graphk--start---mapkint) already follows, where the start's
  own value is not paid.
- **`(s, t) -> Int`** is the cost of the *edge*, which a graph with weighted
  edges needs and a node weight cannot express.

A **negative** cost is a runtime error rather than a wrong answer: Dijkstra
settles a state the first time it pops it, which a negative edge can
invalidate after the fact. Grid `Dijkstra` refuses negative cells for the same
reason.

This is the half of graph search that had no spelling: `Dijkstra` takes a
`Grid<Int>` and nothing else, so the cheapest path through a graph whose nodes
are *not* grid cells — `(position, facing, run length)`, `(valve set, minute)`,
a whole room configuration — could not be asked for at all.

#### `Tally` — the counting DP

`Tally` **folds** the reachable states instead of walking them: a state with no
successors contributes `Value:`, and every other state is its successors'
values folded with `Combine:`. That is what a memo table is, which is why this
is the mode that answers "how many ways".

```domain ignore
Domain Expansion: Explore
    Mode: Tally
    Until: (s) -> s = goal             # a satisfying state is a leaf
    Value: (s) -> 1                    # what a leaf contributes
    Combine: (a, b) -> a + b           # how successors fold together
    Using: (s) -> successors(s)
```

`Value:` may produce any type; `Combine:` folds two of them into one, and its
result is the primitive's output type.

Each state is folded **once** however many paths reach it, which is the whole
point: a lattice with 61 states and four trillion paths through it is 61 folds.
`Until:` marks a leaf here — a satisfying state is never expanded, so "count
the paths that reach the goal" is the natural spelling.

The search must be **acyclic**: a cycle has no finite fold, and the error names
a state on it rather than only reporting that one exists — the same reason
[`Topological Sort`](#topological-sort--graphk--mapk-listk--listk-k---listk-k-keyable)
names a blocked node.

**This is what Domain has instead of recursion.** A Shikigami is inlined at
its call site, so a self-referential one has no finite expansion and is
refused (see [language.md](language.md)); the problems that look recursive —
reachability, fewest-moves, "how many configurations" — are searches, and this
states them directly. It is also the non-grid half of graph search: the four
primitives below all take a `Grid`, while Explore takes a *state*, so the
graph can be nodes named in a text file or tuples of position and facing.

The state must be keyable, which is what makes termination possible; build a
compound one with `tuple(...)`.

### Topological Sort — `Graph<K> | Map<K, List<K>> | List<(K, K)> -> List<K>` (K keyable)

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(first(p), last(p))
Domain Expansion: Topological Sort
Reveal: stdout
```
```input
a b
b c
```
```output
[a, b, c]
```

Given edge pairs it infers the node set from the edges themselves, so nothing
has to declare the nodes separately.

A `Graph<K>` is the third input shape, and the one the other two were standing
in for — an adjacency map and an edge list are both descriptions of a graph
that had nowhere to live:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Topological Sort
Maximum Technique: Join with ","
Reveal: stdout
```
```input
a b
b c
d a
```
```output
d,a,b,c
```

All three shapes go through the same adjacency map internally, so a graph and
the edge list it was built from sort **identically** — the tie-breaking is part
of the answer, not an accident of the input form.

A diamond has more than one valid order; the sort is deterministic, so the
answer is reproducible rather than merely correct:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(first(p), last(p))
Domain Expansion: Topological Sort
Maximum Technique: Count
Reveal: stdout
```
```input
a b
a c
b d
c d
```
```output
4
```

### BFS — `Grid<T> × (T -> Bool) -> Grid<Int> | Graph<K> × Start: -> Map<K,Int>`

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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: BFS from 0 0
    Using: (c) -> c = "."
Reveal: stdout
```
```input
...
.#.
...
```
```output
0 1 2
1 -1 3
2 3 4
```

Unwalkable and unreachable cells both hold `-1`, so counting the reachable
ones is a `Count Cells` away:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: BFS from 0 0
    Using: (c) -> c = "."
Maximum Technique: Count Cells
    Using: (d) -> d >= 0
Reveal: stdout
```
```input
...
.#.
...
```
```output
8
```


Given a **`Graph<K>`** it answers the same question over an explicit graph:
`Start:` names the node to search from, and the result maps each *reachable*
node to its hop count. Unreachable nodes are **absent** rather than `-1` — a
graph has no "every position" obligation the way a dense grid does, so there is
no cell to put a sentinel in.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: BFS
    Start: "a"
Reveal: stdout
```
```input
a b
b d
a c
c d
e f
```
```output
{a: 0, b: 1, c: 1, d: 2}
```

`e` and `f` are in the graph but not reachable from `a`, so they are not in the
answer. `Start:` may also be a lambda over the graph, which is how the start is
computed rather than written: `Start: (g) -> first(nodes(g))`.

### Dijkstra — `Grid<Int> -> Grid<Int> | Graph<K> × Start: -> Map<K,Int>`

```domain
Domain Expansion: Dijkstra from 0 0
```

Minimum total cost from the start to every cell, where stepping **into** a
cell costs that cell's value (the AoC risk-map convention — the start's own
value is not paid), min-heap under the hood. `Mode: 4` (default) or `Mode: 8`. Negative cell
costs are a runtime error.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Domain Expansion: Dijkstra from 0 0
Reveal: stdout
```
```input
19
11
```
```output
0 9
1 2
```

The start's own value is never paid, which is why the top-left cell is `0`
however large its digit is:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Channeled Energy: Convert Each List to Integers
Channeled Energy: Convert To Grid
Domain Expansion: Dijkstra from 0 0
Reveal: stdout
```
```input
91
11
```
```output
0 1
1 2
```


Over a **`Graph<K>`** it is the weighted twin of `BFS`: `Start:` names the
node, and the result maps each reachable node to the cheapest total weight of
reaching it. The arc weights are the cost, so this and `BFS` disagree exactly
when the cheap route is not the short one.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(item(p, 0), item(p, 1), toint(item(p, 2)))
Channeled Energy: Convert To Graph
Domain Expansion: Dijkstra
    Start: "a"
Reveal: stdout
```
```input
a b 1
b d 10
a c 2
c d 3
```
```output
{a: 0, b: 1, c: 2, d: 5}
```

`d` is two hops away either way, but 5 through `c` and 11 through `b`. A
negative weight is a runtime error rather than a wrong answer: the
settled-once invariant a priority queue rests on does not hold for them.

### Flood Fill — `Grid<T> × (T -> Bool) -> Grid<Int>`

```domain
Domain Expansion: Flood Fill from 0 0
    Using: (c) -> c = "#"
```

Marks the start's connected region: `1` for every cell reachable from the
start through cells satisfying the predicate, `0` elsewhere. `Mode: 4`
(default) or `Mode: 8`. A start that
is out of bounds or fails the predicate is a runtime error.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Flood Fill from 0 0
    Using: (c) -> c = "#"
Reveal: stdout
```
```input
##.
#..
..#
```
```output
1 1 0
1 0 0
0 0 0
```

The far `#` is not in the region, so summing the marks is how the region's
size is read off:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Flood Fill from 0 0
    Using: (c) -> c = "#"
Maximum Technique: Count Cells
    Using: (m) -> m = 1
Reveal: stdout
```
```input
##.
#..
..#
```
```output
3
```

### Shortest Path — `Graph<K> × Start: × Goal: -> List<K>`

`Dijkstra` answers "how far is everything"; this answers "which way do I go".
The result is the cheapest path as a list of nodes, **including both
endpoints**, so a path from a node to itself is that one node.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (p) -> tuple(item(p, 0), item(p, 1), toint(item(p, 2)))
Channeled Energy: Convert To Graph
    Mode: Undirected
Domain Expansion: Shortest Path
    Start: "a"
    Goal: "d"
Maximum Technique: Join with "->"
Reveal: stdout
```
```input
a b 1
b d 10
a c 2
c d 3
```
```output
a->c->d
```

An unreachable goal is the **empty list**, not an error — "there is no path" is
an answer, and `Count` on it is 0:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Shortest Path
    Start: "a"
    Goal: "f"
Maximum Technique: Count
Reveal: stdout
```
```input
a b
b c
e f
```
```output
0
```


`Start:` and `Goal:` are each a node literal or a lambda over the graph. A node
that is not in the graph is a runtime error naming it — unreachable and absent
are different mistakes and get different messages. Weights must be
non-negative, as for `Dijkstra`.

### Connected Components — `Grid<T> × (T -> Bool) -> Int | Graph<K> -> Int`

```domain
Domain Expansion: Connected Components
    Using: (c) -> c = "#"
```

How many connected regions of matching cells the grid contains (union-find
under the hood), with `Mode: 4` (default) or `Mode: 8`. `0` for a grid with no
matching cells.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Connected Components
    Using: (c) -> c = "#"
Reveal: stdout
```
```input
#..
.#.
..#
```
```output
3
```

Connectivity is a per-call choice rather than a property of the grid, and on
this input it is the whole answer — `Mode: 8` joins the diagonal that
`Mode: 4` keeps apart:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Domain Expansion: Connected Components
    Mode: 8
    Using: (c) -> c = "#"
Reveal: stdout
```
```input
#..
.#.
..#
```
```output
1
```


Over a **`Graph<K>`** it takes no predicate — a graph's nodes are all there is
— and counts **weakly** connected components: the arcs are read as undirected,
which is what "how many separate pieces is this" almost always means.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Connected Components
Reveal: stdout
```
```input
a b
b c
e f
```
```output
2
```

### Root — `Graph<K> -> K`

```domain
Domain Expansion: Root
```

The one node with no incoming arc — the top of a `parent -> child` listing,
without a lambda to ask for it. The stage form of the `root` builtin, and it
says the same things.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} -> {word}"
Channeled Energy: Convert To Graph
Domain Expansion: Root
Reveal: stdout
```
```input
a -> b
a -> c
b -> d
```
```output
a
```

The order the arcs were read in does not matter — the root is the node nothing
points at, wherever it turns up in the input:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word}){word}"
Channeled Energy: Convert To Graph
Domain Expansion: Root
Reveal: stdout
```
```input
B)C
COM)B
C)D
```
```output
COM
```

**Error** unless there is exactly one: a forest has several and a graph whose
cycle reaches every node has none, and each failure says which it is. A
self-loop counts as an arc in. Where "not exactly one" is the expected answer
rather than a fault, `roots(g)` in a lambda hands back however many there are.

### Accumulate Up — `Graph<K> × (K -> Int) -> Map<K,Int>`

```domain
Domain Expansion: Accumulate Up
    Using: (n) -> getor(own, n, 0)
```

What everything under a node comes to. `Using:` gives a node its **own** value,
and the stage folds that together with the totals of everything the node's arcs
reach — children first, so one pass finishes it and nothing is recomputed.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} {word}"
Channeled Energy: Convert To Graph
Domain Expansion: Accumulate Up
    Using: (n) -> 1
Reveal: stdout
```
```input
a b
a c
b d
```
```output
{a: 4, b: 2, c: 1, d: 1}
```

That is the size of each subtree, counting the node itself — the shape of "how
big is this directory", "how many orbits is this", "what does this tower weigh".
The `Using:` lambda is where the node's weight comes from, so a `Map<K,Int>` of
weights read with `getor` is the usual spelling.

This is the question the search vocabulary cannot answer. `Dijkstra` and
`Shortest Path` weigh a *path*; folding a node's weight onto its in-arcs turns a
node-weighted graph into one they read (see [`Map Weights`](ref-transforms.md)).
But "what is under here" is not a path — every node under this one counts once
per parent that reaches it — and no re-weighting expresses it.

**`Combine:` replaces the fold**, which is `+` by default — here the widest
name anywhere under each node, rather than the total:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} {word}"
Channeled Energy: Convert To Graph
Domain Expansion: Accumulate Up
    Using: (n) -> length(n)
    Combine: (a, b) -> max(a, b)
Reveal: stdout
```
```input
a bb
a ccc
bb dddd
```
```output
{a: 4, bb: 4, ccc: 3, dddd: 4}
```

Each node's children are folded in **adjacency order**, left to right, starting
from its own value — so a `Combine:` that is not commutative still gives one
answer, and both backends give the same one.

A **cycle is a runtime error** naming the node it blocked: there is no
"children first" to fold in, which is the same reason `Topological Sort`
refuses one. On a graph that is not a tree, a node under two parents is folded
into each of them — it is genuinely under both.

### Minimum Spanning Tree — `Graph<K> -> Graph<K>`

```domain
Domain Expansion: Minimum Spanning Tree
```

The cheapest set of arcs that keeps everything connected — Kruskal under the
hood, over the arcs read as **undirected**, since a spanning tree is a question
about connection rather than direction.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} {word} {int}"
Channeled Energy: Convert To Graph
    Mode: Undirected
Domain Expansion: Minimum Spanning Tree
Cursed Technique: Apply
    Using: (g) -> weightsum(g)
Reveal: stdout
```
```input
a b 1
b c 5
a c 3
c d 2
```
```output
6
```

Arcs are taken cheapest first, ties broken by the order they were read, so the
tree is the same one on every run and in both backends. The chosen arcs keep
the direction they had; `undirected(g)` in a lambda is there for a caller who
wants both ways.

A graph in several pieces gives a spanning **forest** rather than an error —
one tree per piece, which is the same answer to the same question:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{word} {word} {int}"
Channeled Energy: Convert To Graph
    Mode: Undirected
Domain Expansion: Minimum Spanning Tree
Channeled Energy: Convert To Edges
Reveal: stdout
```
```input
a b 4
a c 1
x y 7
```
```output
[[a, c, 1], [a, b, 4], [x, y, 7]]
```

### Strongly Connected Components — `Graph<K> -> List<List<K>>`

```domain
Domain Expansion: Strongly Connected Components
```

The groups of nodes that can all reach each other, following the arcs the way
they point. This is the question `Connected Components` does *not* ask: that
one reads the arcs as undirected and answers how many pieces there are.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Strongly Connected Components
Reveal: stdout
```
```input
a b
b c
c a
b d
d e
e d
```
```output
[[a, b, c], [d, e]]
```

A node in no cycle is a component of one, so every node appears exactly once
and an acyclic graph has as many components as it has nodes:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert To Graph
Domain Expansion: Strongly Connected Components
Maximum Technique: Count
Reveal: stdout
```
```input
a b
b c
a c
```
```output
3
```

It answers the components rather than a count, unlike `Connected Components`:
`Count` is one stage away from a list, as above, and the members are not
recoverable from a number. The components come out in a topological order of
the groups — one that can reach another comes first — and each component's
nodes in the graph's insertion order.

### Foreign Block — `T -> Text`, or a declared `In -> Out`

```domain
Cursed Energy: input.txt
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: Python : List<Int> -> Int
    import sys
    print(sum(int(x) for x in sys.stdin))
Reveal: stdout
```

A stage written in another language. The indented block is **not Domain** —
it is source in `Python`, `Go`, `rask`, `cRust`, or `Weave`, captured verbatim — and it
runs as a subprocess with the current value on its stdin. What it prints
becomes the next stage's value.

The block is dedented to column zero, so it is written at whatever indentation
reads well, with its own comment character, its own braces, and tabs if that
is what the language wants. It ends where the indentation returns to the
statement that opened it.

**The wire format.** A foreign stage exchanges lines of text, so only the
types that have one obvious spelling as lines can cross it:

| Type | On the wire |
|---|---|
| `Text` | the text itself |
| `Int` / `Float` / `Bool` | its ordinary rendering, the one `Reveal` prints |
| `List<T>` (T scalar) | one element per line |
| `Grid<T>` (T scalar) | one row per line — going in only |

with a single closing newline on a non-empty value. Anything else — a `Map`,
a `Set`, a `Record`, a `Sparse` plane — is refused when the program is
resolved, by name, rather than failing later as a decode error. A `Grid` can
be shown to a foreign program but not received from one, because its rows have
no cell separator to split on; `Convert To Grid` is one stage away.

**Types.** Without a declared signature the block takes whatever is flowing
and produces `Text` — the shell-pipe reading, and the common case, since the
existing vocabulary can reshape text from there. A declared `: In -> Out`
replaces both halves and is checked against the pipeline exactly as a
Shikigami's signature is. It is the only way to get a non-`Text` value back,
because nothing else can know what the foreign program meant by its output.

**Running it.** Each language is started by the first of its usual binaries
found on `PATH` — `python3`/`python`, `go`, `rask`, `crust`, `weave` — and each can be
pointed somewhere else with an environment variable, which may name a command
with arguments:

| Language | Variable | How it runs |
|---|---|---|
| `Python` | `DOMAIN_PYTHON` | `python3 program.py` |
| `Go` | `DOMAIN_GO` | `go run .` over a throwaway module; the block is a whole `package main` |
| `rask` | `DOMAIN_RASK` | `rask program.rask` — the block reads `input`/`lines`/`blocks` |
| `cRust` | `DOMAIN_CRUST` | `crust program.crust` — the block reads `unbox`/`lines`, prints with `deliver` |
| `Weave` | `DOMAIN_WEAVE` | `weave run program.weave` — the block reads `Source`, and its final bare expression is what it prints |

A block that fails takes the program down with it, reporting the runtime's own
words: a Python traceback, a Go compile error. Line numbers in that report are
line numbers in the dedented block, so they count from the statement that
opened it. A block that succeeds has its stderr passed through to the
program's own, so print-debugging inside one reaches the terminal without
disturbing the value in the pipeline.

**What it costs.** Three things, all of them deliberate:

- **The optimizer stops here.** `Domain Expansion` elsewhere names a result the
  compiler may reach any way it likes; this one names an implementation and is
  honored literally. Nothing fuses through a foreign block, reorders around
  it, or substitutes for it.
- **A compiled binary stops being self-contained.** `domain build` embeds the
  block, not the runtime that runs it, so the binary needs `python3` (or the
  toolchain, or `rask`, `crust` or `weave`) wherever it runs — and, for `Go`,
  at run time rather than build time.
- **It is a subprocess per evaluation.** A foreign block inside a `Map Each`
  body starts one process per element. Put it where the whole pipeline value
  passes through it once.

The tooling treats it as one opaque stage, which is what it is. `--stats` and
`expansion: visualize` show it as a single row with its own cost — usually most
of the run, since starting a process dwarfs anything the rest of the pipeline
does — and the profile attributes that cost to the statement that opened the
block, not to the lines of foreign source beneath it. A block that fails shows
up as a failed step carrying its runtime's whole report, so the visualizer
stays a debugger for the parts of the program written in another language.

Opaque is not the same as invisible. Pressing `x` on a foreign stage — the key
that breaks a `Using:` expression into its parts everywhere else — shows what
this stage has instead: the program it ran, the runtime that ran it, and the
bytes that crossed in each direction. That last part is where a wire-format
mistake is actually visible, and it is
[documented with the other panes](cli.md#inside-a-foreign-block).

The documentation playground cannot run foreign blocks at all: it is compiled
to WebAssembly, where there are no subprocesses.

---

### Request — `W -> W` *(Game Dev)*

Asks a server something, without waiting for the answer.

A game cannot block on a round trip: a hundred milliseconds is six frames, and
a frozen picture every time the program asks a question is not a game. So a
Request **fires and returns the world unchanged**, and the answer arrives later
at a [`Part Reply`](scopes.md#game-dev) — an event like any other.

A replayed run answers from its script rather than from a server, so the whole
exchange is testable with nothing running:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {marks: "", note: ""}

Part On "key enter":
    Domain Expansion: Request
        Url: (w) -> "https://example.test/guess"
        As: "guess"
        Into: {marks: List<Text>}

Part Reply "guess":
    Cursed Technique: Apply
        Using: (w) -> if reply.ok
            then with(w, "marks", textjoin(reply.value.marks, ""))
            else with(w, "note", reply.error)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(if pending("guess") then "asking…" else w.marks + w.note)
```
```input
key enter
frame
reply guess {"marks": ["G","Y"]}
frame
```
```output
asking…

GY
```

The first frame is drawn while the question is still out — the world carried
on, which is the whole point.

| Argument | Meaning |
|---|---|
| `Url:` | a lambda over the world producing the address. Required. |
| `As:` | the tag a `Part Reply` answers to. Required. |
| `Into:` | the shape the answer is decoded into. Required. |
| `Method:` | `GET` (default), `POST`, `PUT`, `PATCH` or `DELETE`. |
| `Body:` | a lambda over the world producing the request body as Text. |
| `Timeout:` | milliseconds; 5000 by default. |

**The tag is checked both ways at resolve time.** A Request nothing answers,
and a `Part Reply` nothing fires, are each refused by name — otherwise both
show up as a game that silently does nothing, which is the hardest kind of bug
to find in a program whose output is a picture.

**The reply always has the same shape**: `{ok: Bool, error: Text, value: T}`.
Every failure lands in it — a server that is not there, a status that is not
2xx, a timeout, a body that does not decode — so a game has one thing to branch
on, and "the server is down" becomes a state it can draw rather than a crash
mid-frame. When `ok` is false, `value` is the zero of what was asked for.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {marks: "", note: ""}

Part On "key enter":
    Domain Expansion: Request
        Url: (w) -> "https://example.test/guess"
        As: "guess"
        Into: {marks: List<Text>}

Part Reply "guess":
    Cursed Technique: Apply
        Using: (w) -> if reply.ok
            then with(w, "marks", textjoin(reply.value.marks, ""))
            else with(w, "note", "offline: " + reply.error)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.marks + w.note)
```
```input
key enter
fail guess the server is not there
frame
```
```output
offline: the server is not there
```

**One request is in flight per tag**, and firing again supersedes the one that
was out. That is a correctness rule before it is a policy: HTTP replies can
arrive out of order, and a client polling positions against a slow server will
otherwise merge an answer from three requests ago *after* a newer one — which
looks like other players teleporting backwards and reads as a game bug rather
than a networking one. Superseding rather than dropping the new one is right
because a body is normally derived from the current world: it sends where the
player *is*, not where they *were*.

The one thing that costs — a dropped reply — is visible.
[`pending("tag")`](ref-builtins-chance.md) says whether one is out, so a program
that must not lose one guards first:

```domain ignore
Part On "key enter":
    Cursed Technique: Apply
        Using: (w) -> if pending("guess") then w else submit(w)
```

**A replayed run never touches the network.** The script says what came back —
`reply guess {…}` or `fail guess connection refused` — which is what makes a
multiplayer game testable with nothing running, and why a request the script
never answers simply never arrives. That is itself a state worth testing: it is
what a server going away mid-game looks like.

### Load — `W -> T` *(Game Dev)*

Reads persisted state back in — a high score, a level unlocked — from a file
rather than a server.

Unlike Request, a local file answers in the same tick it was asked, so there
is no round trip and no `Part Reply` to write: `Load` produces the loaded
value directly, in place. That is also why it fits inside a
[`Consider … Of`](expressions.md#stage-bindings--consider--as--consider--of)
body — the shape a search already uses to run a whole sub-pipeline over the
world and keep it — since a `Domain Expansion` that changes the *type* in
flight cannot sit in the middle of a body whose value stays the world:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Consider save Of
            Domain Expansion: Load
                Path: (w) -> "save.json"
                Into: {highScore: Int}
                Default: (w) -> {highScore: 0}
        Using: (w) -> {score: 0, highScore: save.highScore}

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "score", w.score + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text("best " + totext(w.highScore))
```
```input
frame
```
```output
best 0
```

| Argument | Meaning |
|---|---|
| `Path:` | a lambda over the world producing the file's path. Required. |
| `Into:` | the shape a save is decoded into. Required. |
| `Default:` | a lambda over the world producing the `Into:` shape a fresh game starts with. Required. |

**`Default:` covers every way of getting nothing**, on purpose: no file at
that path, a file that isn't valid JSON, one whose shape doesn't fit `Into:`.
A save that doesn't parse is exactly what a missing one looks like — the game
starts over rather than crashing on a corrupt file — so there is only the one
fallback to write, not a `{ok, error, value}` to branch on the way `Request`
needs.

**A replayed run never opens a real file.** A `load <json>` [script
line](scopes.md#game-dev) stands in for one, on the same terms `reply`
stands in for a server — read by the next `Load` the script reaches:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {highScore: -1}

Part On "key r":
    Cursed Technique: Apply
        Consider save Of
            Domain Expansion: Load
                Path: (w) -> "save.json"
                Into: {highScore: Int}
                Default: (w) -> {highScore: 0}
        Using: (w) -> with(w, "highScore", save.highScore)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text("best " + totext(w.highScore))
```
```input
load {"highScore": 42}
key r
frame
```
```output
best 42
```

**A line only reaches a `Load` fired after it.** `Part World:` and
`Part Start:` run before the script is read at all — the same limitation
`seed` already has, for the same reason — so a `load` line can change what a
handler or a timer reads, but never what the world opens with. Load one from
`Part Start:` instead when a test needs to cover the loaded-at-startup case;
its `Default:` is what every such run gets.

### Save — `W -> W` *(Game Dev)*

Writes state that outlives the run.

A passthrough, on `Beep`'s and `Quit`'s shape: the value it was given is the
value it hands back. There is no `Part Reply` for it, because nothing needs
to come back — a fire-and-forget write is simpler than a question in exactly
the way `Save` is simpler than `Request`.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {highScore: 0}

Part On "key a":
    Cursed Technique: Apply
        Using: (w) -> with(w, "highScore", w.highScore + 1)
    Domain Expansion: Save
        Path: (w) -> "save.json"
        Value: (w) -> {highScore: w.highScore}

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text("best " + totext(w.highScore))
```
```input
frame
key a
frame
```
```output
best 0

best 1
```

| Argument | Meaning |
|---|---|
| `Path:` | a lambda over the world producing the file's path. Required. |
| `Value:` | a lambda over the world producing what to write — any encodable shape, the same rule `tojson` follows. Required. |

**A replayed run never writes a real file.** The two frames above look
identical to a version of this program with no `Save` in it at all — that is
the point: writing a save file must never change what a golden test sees, or
the same script would stop giving the same answer on a machine that happened
to have one lying around. A played session is where `Save` does anything,
which is also where [`--record`](cli.md#--record-a-played-session-becomes-a-golden-test)
watches it happen.

A failed write — a read-only disk, a path that doesn't exist — has nowhere
to report to, unlike a failed `Request`: there is no `Part Reply` to carry
`{ok: false, …}` back to the program. It is noted on stderr and otherwise
swallowed, so a game that cannot save keeps playing rather than crashing over
it.

`Value:` may produce anything JSON can hold — the same rule `tojson` follows,
right down to refusing a `View` — not only a Record built to mirror `Load`'s
`Into:`:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {lives: 3}

Part On "key x":
    Cursed Technique: Apply
        Using: (w) -> with(w, "lives", w.lives - 1)
    Domain Expansion: Save
        Path: (w) -> "lives.json"
        Value: (w) -> w.lives

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text("lives " + totext(w.lives))
```
```input
key x
frame
```
```output
lives 2
```
