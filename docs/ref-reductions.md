# Reductions — `Maximum Technique`

One class of the [primitive reference](primitives.md).

## Maximum Technique — reductions

### Sum — `List<Int> -> Int`

Sum of all elements; `0` for the empty list.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
```
```input
3
4
1
5
```
```output
13
```

Nothing to add is `0` rather than an error, which is what lets a `Sum` sit
downstream of a `Filter` that may match nothing:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Using: (n) -> n > 100
Maximum Technique: Sum
Reveal: stdout
```
```input
3
4
1
5
```
```output
0
```

### Sum Each Group — `List<List<Int>> -> List<Int>`

Per-group sums, preserving order.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Reveal: stdout
```
```input
1
2
3

10
20
```
```output
[6, 30]
```

It reduces one level rather than all of them, so the result is still a list —
which is what a following `Max` or `Select Top K` consumes:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Maximum Technique: Max
Reveal: stdout
```
```input
1
2

100
```
```output
100
```

### Max / Min / Product — `List<Int> -> Int`

Seeded with the first element; the empty list is a runtime error
(`"Max of an empty list is undefined"`).

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Max
Reveal: stdout
```
```input
3,9,2,7
```
```output
9
```

`Product` is the same reduction with a different operator, and unlike `Sum`
none of the three has an answer for the empty list — there is no element to
seed with:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Product
Reveal: stdout
```
```input
2,3,4
```
```output
24
```

### Count — `List<T> | Set<T> -> Int`

Cardinality.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count
Reveal: stdout
```
```input
one
two
three
```
```output
3
```

Over a `Set<T>` it counts the distinct elements, which is the shortest way to
ask "how many different ones are there":

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Set
Maximum Technique: Count
Reveal: stdout
```
```input
a,b,a,c,b
```
```output
3
```

### Count Matching — `List<T> × (T -> Bool) -> Int`

How many elements satisfy the predicate.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Maximum Technique: Count Matching
    Using: (n) -> n > 2
Reveal: stdout
```
```input
1
3
2
5
```
```output
2
```

It is `Filter` then `Count` in one stage, and the optimizer fuses that pair
into this — so writing it directly costs nothing and reads as the question
being asked:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count Matching
    Using: (line) -> startswith(line, "a")
Reveal: stdout
```
```input
apple
banana
avocado
```
```output
2
```

### Count Cells — `Grid<T> | Sparse<T> × (T -> Bool) -> Int`

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

The 3-parameter form binds `(grid, row, col)` instead of the cell value, which
is what a predicate needs when it has to look around the cell it is judging:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Maximum Technique: Count Cells
    Using: (g, r, c) -> at(g, r, c) = "#" and r = 0
Reveal: stdout
```
```input
.#.
###
```
```output
1
```


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

The count is a [measured argument](ref-transforms.md#measured-arguments) (`Count:`), and the
quickselect survives it: `TopK` takes the count as data, so the fused node
measures it at run time like any other argument.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Sort, Descending
Maximum Technique: Select Top 3
Reveal: stdout
```
```input
5,1,9,3,7
```
```output
[9, 7, 5]
```

The `, Sum` modifier turns the list into the one number the puzzle usually
wants, and this is the pair the optimizer rewrites into a quickselect:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Sort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
```
```input
5,1,9,3,7
```
```output
21
```

### Fold — `List<T> × Seed × (Acc, T -> Acc) -> Acc`

```domain
Maximum Technique: Fold
    Seed: 100
    Using: (acc, x) -> acc * 2 + x
```

`Seed:` fixes the accumulator type and the lambda must return that same type.
Written as a literal it is an Int or Text — the two a named argument can
spell — but it is a [measured argument](ref-transforms.md#measured-arguments), and that is the
one place measuring *widens* a primitive rather than only moving where its
value comes from: a measured seed takes its type from the lambda body, so the
accumulator can be a composite.

```domain
Maximum Technique: Fold
    Seed: (xs) -> tuple(0, 0)                                  # (sum, count)
    Using: (acc, x) -> tuple(prow(acc) + x, pcol(acc) + 1)
```

Two variations live nearby: [Reduce](#reduce--listt--t-t---t---t) is the same left fold seeded by
the first element instead, and [Scan](ref-transforms.md#scan--listt--seed--acc-t---acc---listacc) keeps every intermediate
accumulator rather than only the last (its `Seed:` measures the same way).

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Fold
    Seed: 100
    Using: (acc, x) -> acc + x
Reveal: stdout
```
```input
1,2,3
```
```output
106
```

A measured `Seed:` is how the accumulator becomes a composite — here a running
`(sum, count)` pair that a literal seed could not have spelled:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Fold
    Seed: (xs) -> tuple(0, 0)
    Using: (acc, x) -> tuple(prow(acc) + x, pcol(acc) + 1)
Reveal: stdout
```
```input
4,5,6
```
```output
[15, 3]
```

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

### Reduce — `List<T> × (T, T -> T) -> T`

The seedless fold: the first element *is* the starting accumulator, so no
`Seed:` is needed and none is accepted.

```domain
Maximum Technique: Reduce
    Using: (a, b) -> a * 10 + b   # [1,2,3,4] -> 1234
```

It folds left, like `Fold`, and a one-element list is its own answer (the
lambda never runs). Because the accumulator is an element, the lambda must
be `T × T -> T` — and `T` can be anything the pipeline carries, including
composites a `Seed:` literal cannot spell:

```domain
Maximum Technique: Reduce
    Using: (a, b) -> padd(a, b)   # sum a list of points
```

**`Reduce` of an empty list is a runtime error** — there is no accumulator to
start from. Use `Fold` with a `Seed:` when the empty case needs an answer.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Reduce
    Using: (a, b) -> a * 10 + b
Reveal: stdout
```
```input
1,2,3,4
```
```output
1234
```

Because the accumulator *is* an element, the lambda may work on a type no
`Seed:` literal could name — a point, for instance:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Using: (xs) -> point(first(xs), last(xs))
Maximum Technique: Reduce
    Using: (a, b) -> padd(a, b)
Reveal: stdout
```
```input
1 2
10 20
```
```output
[11, 22]
```

### Any / All — `List<T> × (T -> Bool) -> Bool`

```domain
Maximum Technique: Any
    Using: (x) -> x > 4
Maximum Technique: All
    Using: (x) -> x > 0
```

Whether some — or every — element satisfies the predicate. **Both stop at the
element that decides the answer**: `Any` at the first true, `All` at the first
false. `Count Matching … > 0` computes the same thing but always visits the
whole list.

On the empty list each takes the identity of its connective: `Any` is `false`
(nothing satisfies it), `All` is `true` (nothing violates it).

Note that `All Pairs` is the [combination generator](ref-expansions.md#all-pairs--combinations-k--listt--mode--lambda---)
and `All Values > n` is a [Binding Vow](ref-structure.md#simple-domain-channel-shikigami-binding-vow-reveal);
neither is this reduction.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Any
    Using: (x) -> x > 4
Reveal: stdout
```
```input
1,2,9,3
```
```output
true
```

`All` is the other connective, and the empty-list answers follow from that
rather than from a special case — nothing violates a predicate:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Using: (x) -> x > 100
Maximum Technique: All
    Using: (x) -> x > 0
Reveal: stdout
```
```input
1,2,3
```
```output
true
```

### Find / Find Index — `List<T> × (T -> Bool) -> T` | `List<T> × (T -> Bool) -> Int`

```domain
Maximum Technique: Find
    Using: (r) -> r.n > 3
Maximum Technique: Find Index
    Using: (x) -> x > 3        # 0-based, or -1 when absent
```

The first element satisfying the predicate, or its position. Both stop there —
the rest of the list is never touched.

`Find` on no match is a runtime error (there is no element to hand back);
`Find Index` answers `-1`, the sentinel the expression layer already uses for
"not there". `Filter` + `Take Item 0` is the long way round, and the optimizer
[rewrites it into a `Find`](optimizer.md) when the predicate cannot fail.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Find
    Using: (x) -> x > 3
Reveal: stdout
```
```input
1,5,2,9
```
```output
5
```

`Find Index` answers with the position instead, and `-1` rather than an error
when nothing matches — so it can be asked speculatively:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Find Index
    Using: (x) -> x > 100
Reveal: stdout
```
```input
1,5,2,9
```
```output
-1
```

### Find Cycle — `List<T> -> (Int, Int)` (T keyable)

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Technique: Iterate 10
    Using: (x) -> mod(x * 3, 7)
Maximum Technique: Find Cycle
Reveal: stdout
```
```input
1
```
```output
[0, 6]
```

A trajectory that never repeats answers `(-1, 0)` rather than erroring, since
"no cycle" is a legitimate result:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Technique: Iterate 5
    Using: (x) -> x + 1
Maximum Technique: Find Cycle
Reveal: stdout
```
```input
1
```
```output
[-1, 0]
```


```domain
Cursed Technique: Iterate 200
    Using: (s) -> step(s)
Maximum Technique: Find Cycle      # -> (first index of the repeat, period)
```

Where a trajectory first repeats, and its period. `Iterate` produces the
trajectory precisely so a program can ask "have I been here before?", but the
asking had no primitive — `Find Index` needs a predicate over one element, not
a seen-set over the prefix. The answer is what turns "run this a billion
times" into arithmetic.

A trajectory with no repeat gives `(-1, 0)` — the sentinel `Find Index`
already uses — rather than an error, since that is a legitimate result.

### Sum By / Product By — `List<T> × (T -> Int) -> Int`

```domain
Maximum Technique: Sum By
    Using: (x) -> x * x
```

Folds the lambda's Int key over the list without building the mapped list
first, completing the key-lambda family that already has `Count By`, `Min By`
and `Max By`. `Map Each` + `Sum` is the two-pass spelling, and the optimizer
fuses it into exactly this. The empty list gives the identity: `0` for `Sum
By`, `1` for `Product By`.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Sum By
    Using: (x) -> x * x
Reveal: stdout
```
```input
1,2,3
```
```output
14
```

The key lambda is where the work goes, so a reduction over text needs no
mapping stage in front of it:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Sum By
    Using: (line) -> length(line)
Reveal: stdout
```
```input
a
abc
ab
```
```output
6
```

### Join — `List<Text> -> Text`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Maximum Technique: Join
Reveal: stdout
```
```input
a,b,c
```
```output
abc
```

The separator is an argument, so joining back with one is a single stage —
`Join` and `Split` are inverses when they agree on it:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Join with ", "
Reveal: stdout
```
```input
one
two
three
```
```output
one, two, three
```


```domain
Maximum Technique: Join
Maximum Technique: Join with ", "
```

Concatenates the elements, with an optional separator.

### Group By — `List<T> × (T -> K) -> Map<K, List<T>>` (K keyable)

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Group By
    Using: (w) -> charat(w, 0)
Reveal: stdout
```
```input
apple
avocado
banana
```
```output
{a: [apple, avocado], b: [banana]}
```

Every element is kept, which is the difference from `Count By` — reach for
this when the members matter and for `Count By` when only the tally does.

The key may be any keyable expression, including a tuple — which is how a
grouping on two fields at once is spelled:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Group By
    Using: (w) -> length(w)
Reveal: stdout
```
```input
ab
cd
xyz
```
```output
{2: [ab, cd], 3: [xyz]}
```


```domain
Maximum Technique: Group By
    Using: (n) -> n / 3
```

Buckets preserve element order; keys appear in first-seen order.

### Count By — `List<T> × (T -> K) -> Map<K, Int>` (K keyable)

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Maximum Technique: Count By
    Using: (c) -> c
Reveal: stdout
```
```input
abracadabra
```
```output
{a: 5, b: 2, r: 2, c: 1, d: 1}
```

The frequency table in one stage, keys in first-seen order.

It is `Group By` followed by mapping each group to its length, fused — and
the usual way to find the most common thing:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count By
    Using: (w) -> charat(w, 0)
Channeled Energy: Convert To Entries
Maximum Technique: Max By
    Using: (e) -> item(e, 1)
Reveal: stdout
```
```input
apple
avocado
banana
```
```output
[a, 2]
```

(`item(e, 1)` rather than `last(e)`: an entry is a `(Text, Int)` tuple, and
the list builtins do not read tuples — the index has to be a literal so the
result type is knowable.)


```domain
Maximum Technique: Count By
    Using: (n) -> n / 10
```

Frequency map of the lambda's key, keys in first-seen order.

### Min By / Max By — `List<T> × (T -> K) -> T` (K ordered)

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Max By
    Using: (w) -> length(w)
Reveal: stdout
```
```input
apple
kiwi
banana
```
```output
banana
```

It answers with the *element*, not the key — which is why it exists beside
`Max`, which would only have given back the length.

`Min By` is the same reduction the other way, and ties go to the first
element seen:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Min By
    Using: (w) -> length(w)
Reveal: stdout
```
```input
apple
kiwi
fig
cat
```
```output
fig
```


```domain
Maximum Technique: Max By
    Using: (r) -> r.n
```

The element whose key is smallest/largest (the first wins ties; the empty
list is a runtime error).

`K` is any **ordered** type — Int, Float, Text, or a Tuple of them — exactly
as [`Sort By`](ref-expansions.md#sort-by--listt--t---k---listt-k-ordered)'s is, and over the
same ordering: whatever `Min By` picks, a `Sort By` on the same key puts
first. A tuple key tiebreaks, so "the earliest of the shortest" is one pass:

```domain
Maximum Technique: Min By
    Using: (w) -> tuple(length(w), w)
```

### Intersect / Union / Difference — `List<List<T>> -> Set<T>` (T keyable)

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Maximum Technique: Intersect
Reveal: stdout
```
```input
abc
bcd
```
```output
{b, c}
```

`Union` is the other side of the same reduction over a list of lists:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Maximum Technique: Union
Maximum Technique: Count
Reveal: stdout
```
```input
abc
bcd
```
```output
4
```


Set reduction over the groups, seeded with the first group. `Intersect`
keeps the accumulator's element order; `Union` appends left-to-right,
deduplicated; `Difference` keeps the elements of the first group that appear
in no later group. The empty input produces the empty set.

`Difference` is also a two-channel consumer — see
[below](#difference--channel-consumer---sett-t-keyable) for that form.

### Merge Ranges — `List<(Int, Int)> -> List<(Int, Int)>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Using: (xs) -> point(first(xs), last(xs))
Maximum Technique: Merge Ranges
Reveal: stdout
```
```input
1-3
2-5
8-10
```
```output
[[1, 5], [8, 10]]
```

Overlapping and touching ranges collapse; disjoint ones survive, so the length
of the result is how many separate spans the input really covered.

Counting the surviving spans is the usual question, and it needs no
post-processing:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Using: (xs) -> point(first(xs), last(xs))
Maximum Technique: Merge Ranges
Maximum Technique: Count
Reveal: stdout
```
```input
1-3
2-5
8-10
20-21
```
```output
3
```


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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"

Channel "left":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by "\n"
    Channeled Energy: Convert To Integers
    Maximum Technique: Sum

Channel "right":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by "\n"
    Channeled Energy: Convert To Integers
    Maximum Technique: Sum

Maximum Technique: Combine
    From: left, right
    Using: (l, r) -> l * r
Reveal: stdout
```
```input
1
2

10
20
```
```output
90
```

Because the main pipeline's value is ignored, `Combine` is where a program
that branched comes back together — the two channels above both read the same
upstream split.

Any number of channels may be named, and the lambda takes one parameter each,
in `From:` order:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"

Channel "n":
    Maximum Technique: Count

Channel "total":
    Channeled Energy: Convert To Integers
    Maximum Technique: Sum

Maximum Technique: Combine
    From: n, total
    Using: (n, total) -> total / n
Reveal: stdout
```
```input
2
4
6
```
```output
4
```


### Difference — channel consumer, `-> Set<T>` (T keyable)

```domain
Maximum Technique: Difference
    From: one, two
```

Exactly two channels, each a Set or List with matching element types; emits
the elements of the first not present in the second, in the first's order.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"

Channel "one":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by ","

Channel "two":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by ","

Maximum Technique: Difference
    From: one, two
Reveal: stdout
```
```input
a,b,c

b
```
```output
{a, c}
```


**Standalone form** (no `From:`): `List<List<T>> -> Set<T>` — the first
group's elements not present in any later group, in the first group's
order; the empty input produces the empty set.

Without `From:` it reduces a list of lists instead, which needs no channels
at all:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by ""
Maximum Technique: Difference
Reveal: stdout
```
```input
abcd
bd
```
```output
{a, c}
```

### Zip — channel consumer, `-> List<(A, B)>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"

Channel "names":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by "\n"

Channel "scores":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by "\n"
    Channeled Energy: Convert To Integers

Maximum Technique: Zip
    From: names, scores
Reveal: stdout
```
```input
ann
bob

10
20
```
```output
[[ann, 10], [bob, 20]]
```

It truncates to the shorter of the two, so a mismatched pair loses its tail
rather than erroring:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"

Channel "a":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by "\n"

Channel "b":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by "\n"

Maximum Technique: Zip
    From: a, b
Maximum Technique: Count
Reveal: stdout
```
```input
x
y
z

1
2
```
```output
2
```


```domain
Maximum Technique: Zip
    From: xs, ys
```

Pairs two channel lists element-wise, truncated to the shorter one. Over
two Int lists the pairs are points (`prow`/`pcol` read them). The main
pipeline's current value is ignored, like Combine.

**Zip With.** Adding a two-parameter `Using:` lambda combines the channels
directly instead of handing back tuples for a following `Map Each` to take
apart — one pass, and no intermediate tuple list:

```domain
Maximum Technique: Zip
    From: xs, ys
    Using: (x, y) -> x * y      # -> List<Int>, not List<(Int, Int)>
```

The lambda must take one parameter per channel. Writing the naive `Zip` +
`Map Each` pair is fine too: the optimizer fuses it into the same single
pass.

---
