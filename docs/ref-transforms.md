# List and text transforms — `Cursed Technique`

One class of the [primitive reference](primitives.md).

## Cursed Technique — transforms

### Split — `Text -> List<Text>`

```domain
Cursed Technique: Split Text by "\n\n"
```

Splits on the required separator string. An empty separator (`""`) splits
into individual characters (runes). The separator is a
[measured argument](#measured-arguments) (`By:`), so a program that has to
look at the text before it knows how to split it can:

```domain
Cursed Technique: Split
    By: (t) -> if indexof(t, "\t") >= 0 then "\t" else ","
```

Splitting on a literal separator, and counting what came back:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Maximum Technique: Count
Reveal: stdout
```
```input
alpha,beta,gamma
```
```output
3
```

The empty separator is the one worth remembering, because it is how a line
becomes its characters without leaving the pipeline layer:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ""
Reveal: stdout
```
```input
hello
```
```output
[h, e, l, l, o]
```

### Split Each — `List<Text> -> List<List<Text>>`

```domain
Cursed Technique: Split Each by "\n"
```

`Split`, applied to every element.

The usual AoC opening — paragraphs, then lines within each paragraph:

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

10
20
30
```
```output
[3, 60]
```

It splits every element with the same separator, so a ragged input stays
ragged rather than erroring:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Map Each
    Using: (parts) -> length(parts)
Reveal: stdout
```
```input
a b c
d
e f
```
```output
[3, 1, 2]
```

### Map Each — `List<T> × (T -> U) -> List<U>`

```domain
Cursed Technique: Map Each
    Using: (m) -> m.n
```

**The `Using:` may be written as an indented pipeline** instead of a lambda,
which is how a per-element job reaches a *primitive* — the expression layer
cannot iterate, so there is no lambda that searches an element which is itself
a list:

```domain
Cursed Technique: Map Each          # List<List<Int>> -> List<Int>
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> a + b = 2020
    Maximum Technique: Product
```

This is not a `Map Each` feature: a body stands in wherever a 1-parameter
`Using:` lambda is accepted. See
[pipeline bodies](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)
for the rule, the primitives it reaches, and its limits.

One value in, one value out, per element — the lambda's return type becomes
the list's element type, so a `Map Each` may change it:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (line) -> length(line)
Reveal: stdout
```
```input
a
abc
ab
```
```output
[1, 3, 2]
```

And the pipeline-body form, where the per-element job needs a primitive the
expression layer cannot spell:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Channeled Energy: Convert Each List to Integers
Cursed Technique: Map Each
    Domain Expansion: Sort
    Cursed Technique: Take Item 0
Reveal: stdout
```
```input
3 1 2
9 7 8
```
```output
[1, 7]
```

### Filter — `List<T> × (T -> Bool) -> List<T>`

```domain
Cursed Technique: Filter
    Using: (x) -> x > 2
```

The lambda must be a predicate (return `Bool`).

Keeping what matches, in the order it arrived:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Using: (n) -> mod(n, 2) = 0
Reveal: stdout
```
```input
1
2
3
4
5
6
```
```output
[2, 4, 6]
```

Matching nothing is the empty list, not an error — which is what lets a
reduction sit downstream without a guard:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Using: (n) -> n > 1000
Maximum Technique: Count
Reveal: stdout
```
```input
1
2
3
```
```output
0
```

### Unique — `List<T> -> List<T>` (T keyable)

Order-preserving deduplication (first occurrence wins).

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Unique
Reveal: stdout
```
```input
b,a,b,c,a
```
```output
[b, a, c]
```

First occurrence wins, so `Unique` after a `Sort` and `Unique` before one are
different programs — the second keeps input order:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Unique
Maximum Technique: Count
Reveal: stdout
```
```input
5,3,5,3,1
```
```output
3
```

### Match Pattern — `Text -> V` or `List<Text> -> List<V>`

A typed template with named holes, producing a record per line:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{a:int}-{b:int}"
Reveal: stdout
```
```input
1-2
30-40
```
```output
[{a: 1, b: 2}, {a: 30, b: 40}]
```

The holes are typed, so the fields arrive as `Int` rather than as text to be
converted, and `.field` reads them downstream:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{a:int}-{b:int}"
Maximum Technique: Sum By
    Using: (r) -> r.b - r.a
Reveal: stdout
```
```input
1-2
30-40
```
```output
11
```


```domain
Cursed Technique: Match Pattern
    Mode: Each                       # One | Each | Try | Scan; One/Each inferred
    Using: "{a:int}-{b:int},{c:int}-{d:int}"
```

Parses each line against a typed-hole template. Named holes produce a Record
(`V = {a:Int, b:Int, ...}`); positional holes produce a `List<T>` when all
holes share one type, otherwise a Tuple. Hole types: `int`, `hex` (base-16,
captured as `Int`), `digits` (`Text`, leading zeros kept), `word` (non-space
run), `char` (exactly one), `text` (rest of field). `{~}` matches a run of
whitespace and owns no field, for column-aligned input. Named and positional
holes cannot mix. A non-matching input line is a runtime error naming the line and the
template.

An `int` or `word` hole may **repeat**: `{ns:int+ sep=", "}` captures one or
more elements and yields a `List` of them, which is how `Time: 7 15 30` parses
in one stage. The separator is required (a default would be right about half
the time), `+` is one-or-more, and `text` cannot repeat.

A run of *structure* is a **group**. `[? … ]` makes one optional — its holes
take their type's zero when it is absent, and `{?name}` inside adds a `Bool`
saying whether it matched. `( … )+` repeats one, yielding a `List` of the inner
template's own type:

```domain
Cursed Technique: Match Pattern
    Using: "{name:word} ({w:int})[? -> {kids:word+ sep=\", \"}]"
```
```domain
Cursed Technique: Match Pattern
    Using: "Game {id:int}: {draws:( {n:int} {color:word} )+ sep=\", \"}"
```

Only `[?` opens a group — a bare `[` is a literal — and groups do not nest.

`Mode: Try` keeps the lines that match and drops the rest — one pass per line
shape over a file that mixes them. It is never inferred, and it only swallows a
*shape* mismatch: a capture that fails to convert still stops the program.

`Mode: Scan` reads the template as a *fragment* rather than a whole line and
takes every occurrence inside each one, concatenated — `mul({a:int},{b:int})`
over a line of noise. A line holding none contributes nothing. Also never
inferred.

`Case:` carries several templates on one stage instead of `Using:`, tried in
the order written, with a `kind` field naming the one that matched:

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Case: on     "turn on {a:int},{b:int} through {c:int},{d:int}"
    Case: toggle "toggle {a:int},{b:int} through {c:int},{d:int}"
```

Every case must produce the same fields — that is what keeps the output one
type — and they must use named holes, so `kind` has somewhere to live. This is
the ordered one-pass answer to what `Mode: Try` can only do as one pass per
shape, concatenating by shape and losing the input's own order.

Full template grammar: [match-pattern.md](match-pattern.md).

### Split Fields — `Text -> List<Text>` or `List<Text> -> List<List<Text>>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Fields
Reveal: stdout
```
```input
  alpha   beta  gamma
```
```output
[alpha, beta, gamma]
```

It splits on runs of whitespace and drops the empties, which is the
difference from `Split Text by " "` — and given a list it does that per line:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Fields
Cursed Technique: Map Each
    Using: (parts) -> length(parts)
Reveal: stdout
```
```input
a  b   c
d e
```
```output
[3, 2]
```


```domain
Cursed Technique: Split Fields
```

Splits on runs of whitespace (spaces and tabs), discarding empty fields —
the classic `fields()` helper. The form is chosen by the input type: bare
`Text` gives one field list; `List<Text>` splits every line.

### Extract Integers — `Text -> List<Int>` or `List<Text> -> List<List<Int>>`

```domain
Cursed Technique: Extract Integers
```

Mines every integer out of messy text — the AoC "parse ints off the line"
workhorse: `"move 12 from -3 to 5"` yields `[12, -3, 5]`. A leading `-`
negates **unless** it directly follows a digit, so `"36-92"` yields
`[36, 92]` (the `-` separates) while `"x=-5"` yields `[-5]`. The form is
chosen by the input type, like `Split Fields`.

Everything between the numbers is discarded, which is what makes it a parser
for lines nobody wants to write a pattern for:

```domain run
Cursed Energy: stdin
Cursed Technique: Extract Integers
Reveal: stdout
```
```input
move 12 from -3 to 5
```
```output
[12, -3, 5]
```

Given a list it gives a list per line, so a whole input parses in two stages:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Maximum Technique: Sum Each Group
Reveal: stdout
```
```input
target x=36-92
target y=-5..-1
```
```output
[128, -6]
```

### Ragged Columns — `List<Text> -> List<List<Text>>`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Ragged Columns
Reveal: stdout
```
```input
ab
cde
f
```
```output
[[a, c, f], [b, d], [e]]
```

Unlike `Transpose` it does not require the rows to be equal length — short
rows simply stop contributing, which is what "ragged" means here.

An equal-length input makes it agree with `Transpose` exactly, which is the
easiest way to see what it generalizes:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Ragged Columns
Reveal: stdout
```
```input
ab
cd
```
```output
[[a, c], [b, d]]
```


```domain
Cursed Technique: Ragged Columns
```

The character columns of a block of lines, top to bottom, tolerating
unpadded (ragged) line lengths by skipping the cells short lines don't have
— the classic move for fixed-column drawings like AoC 2022 Day 5's crate
stacks, whose crates for stack k live in column `4(k-1)+1`. See
`testdata/day5_full.domain` for the full worked parse.

### Window — `List<T> -> List<List<T>>`

```domain
Cursed Technique: Window 3      # sliding windows of 3, step 1
Cursed Technique: Window 2 2    # non-overlapping pairs (step 2)
```

Fully-contained sliding windows (a list shorter than the window yields
none). Size and step must be ≥ 1. The 2021 D1 idiom is `Window 2` +
`Count Matching (w) -> last(w) > first(w)`.

Size and step are **measured arguments**: either the literal above, or
`Size:`/`Step:` holding a lambda over the current list (see
[below](#measured-arguments)).

```domain
Cursed Technique: Window
    Size: (xs) -> length(xs) / 2     # windows half the list long
```

The 2021 D1 idiom in full — how many readings are larger than the one before:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Window 2
Maximum Technique: Count Matching
    Using: (w) -> last(w) > first(w)
Reveal: stdout
```
```input
199
200
208
210
200
```
```output
3
```

A step equal to the size makes the windows non-overlapping, and a trailing
partial window is dropped rather than kept short:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Window 2 2
Maximum Technique: Sum Each Group
Reveal: stdout
```
```input
1,2,3,4,5
```
```output
[3, 7]
```

### Measured arguments

An argument a phrase takes as a literal may instead be written as an indented
named argument holding a **lambda over the current value**, so it can depend on
the data flowing through the pipe rather than on the source:

| Primitive | phrase form | measured form |
|---|---|---|
| `Window` | `Window SIZE [STEP]` | `Size:`, `Step:` |
| `Chunk` | `Chunk SIZE` | `Size:` |
| `Sliding Reduce` | `Sliding Reduce SIZE [STEP]` | `Size:`, `Step:` |
| `Select Top K` | `Select Top K` | `Count:` |
| `Take Item` | `Take Item I` | `Index:` |
| `Iterate` | `Iterate N` | `Times:` |
| `Repeat` (`Simple Domain`) | `Repeat N` | `Times:` |
| `Range` | `Range [LO] HI` | `Low:`, `High:` |
| `Binding Vow: Count Equals` | `Count Equals N` | `Count:` |
| `Split` / `Split Each` | `Split Text by "SEP"` | `By:` (Text) |
| `Join` | `Join "SEP"` | `With:` (Text) |
| `Pad Grid` | — | `Fill:` (the cell type) |
| `Convert To Sparse Grid` | — | `Default:`, `Mark:` (the cell type) |
| `Fold` / `Scan` | — | `Seed:` (the accumulator type) |
| `Subgrid` | `Subgrid R C H W` | `Row:`, `Col:`, `Height:`, `Width:` |
| `Pad Grid` | `Pad Grid N` | `Thickness:` |
| `BFS` / `Dijkstra` / `Flood Fill` | `… from R C` | `Row:`, `Col:` |

The rules, which are the same for every measured argument:

- the lambda takes **one parameter, the whole current value** — the binding
  `Apply` gives its lambda — plus one trailing parameter per enclosing `For`
  loop, exactly as a `Using:` lambda does there. It must return the slot's own
  type — `Int` for a count, `Text` for a separator, the cell type for a fill or
  a default (which is checked against the value it has to match, exactly as a
  literal's type is);
- the same named slot also accepts a plain literal (`Size: 3`). A slot written
  **both** ways — `Window 3` with a `Size:` under it — is a resolve error
  rather than a silent win for either spelling;
- it is evaluated **once per execution of the statement**, before the
  primitive runs. Inside a loop that means once per lap, which is the point: a
  `Window` over a list that shrinks each lap re-measures each lap;
- an argument with no bound of its own is checked where it always was: a
  measured `Take Item` index is range-checked against the list (`index 99 out
  of range (length 3)`), and a measured `Range` pair against each other;
- bounds move with the value. `Window 0` is a resolve error as it always was;
  a `Size:` that *measures* 0 can only fail once it has been measured, so it
  is a runtime error naming what it measured and from what. It is an error and
  not a clamp — a window silently widened to 1 is a wrong answer that looks
  right. The guard is writable: `Size: (xs) -> max(1, length(xs) / 2)`;
- a measured argument is invisible to the optimizer's constant folding. The
  two rewrites whose fused nodes take the value as data (`Window` + a reduce,
  `Sort` + `Select Top K`) carry it through and still fire; the rewrites that
  are valid *because* of what the literal is stand down — see
  [optimizer.md](optimizer.md#measured-arguments-and-the-passes-that-fold-literals).

Arguments that *type* the program stay literal and always will: the `k` of
`Combinations k` (it fixes the `Using:` lambda's arity) and a `Match Pattern`
template (it fixes the output `Record`'s fields) are decisions the resolver
makes, not data.

A measured argument reaches a `Shikigami` through a lambda parameter — declare
it as the function the slot takes and hand it over (see
[language.md](language.md#parameters)).

### Flatten — `List<List<T>> -> List<T>`

Concatenates the groups in order.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Split Each by " "
Cursed Technique: Flatten
Reveal: stdout
```
```input
a b
c
d e
```
```output
[a, b, c, d, e]
```

One level only, and order is preserved, so a flatten after a `Split Each`
undoes exactly the grouping the split introduced:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Flatten
Maximum Technique: Sum
Reveal: stdout
```
```input
1 2
3 4 5
```
```output
15
```

### Enumerate — `List<T> -> List<(Int, T)>`

Pairs every element with its 0-based index. Over `List<Int>` the pairs are
points, so `prow`/`pcol` read index/value in a following lambda.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Enumerate
Reveal: stdout
```
```input
a,b,c
```
```output
[[0, a], [1, b], [2, c]]
```

A tuple is `[]Value` at run time, so it renders like a list — the parentheses
in `(Int, Text)` are the *type*'s notation, not the value's.

The index is what a positional puzzle needs — here, the elements whose value
matches their own position:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Enumerate
Maximum Technique: Count Matching
    Using: (p) -> prow(p) = pcol(p)
Reveal: stdout
```
```input
0,5,2,9
```
```output
2
```

### Pairs — `List<T> -> List<(T, T)>`

```domain
Cursed Technique: Pairs
```

Every element tupled with the one after it (`zip xs (tail xs)`): `n` elements
give `n-1` pairs, and a list shorter than 2 gives none. Over `List<Int>` the
pairs are points, so a following lambda reads the two sides with
`prow`/`pcol` — the 2021 D1 "count the increases" idiom without a Window:

```domain
Cursed Technique: Pairs
Maximum Technique: Count Matching
    Using: (p) -> pcol(p) > prow(p)
```

`Window 2` covers the same ground with `List<List<T>>` elements; reach for
`Pairs` when you want a tuple (points, `Map`/`Group By` keys — tuples are
keyable, lists are not).

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Pairs
Reveal: stdout
```
```input
1,2,3,4
```
```output
[[1, 2], [2, 3], [3, 4]]
```

`n` elements give `n-1` pairs, so the 2021 D1 count comes out without a
`Window`:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Pairs
Maximum Technique: Count Matching
    Using: (p) -> pcol(p) > prow(p)
Reveal: stdout
```
```input
199
200
208
210
200
```
```output
3
```

### Chunk — `List<T> -> List<List<T>>`

```domain
Cursed Technique: Chunk 3
```

Consecutive non-overlapping blocks of the given size, **keeping a short final
block**. That is the difference from `Window 3 3`, which drops a trailing
partial window — usually a bug rather than the intent. Size must be ≥ 1; a
list shorter than the size yields one block holding all of it. Size is a
[measured argument](#measured-arguments): `Size: (xs) -> length(xs) / 3`.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Chunk 3
Reveal: stdout
```
```input
a,b,c,d,e,f,g
```
```output
[[a, b, c], [d, e, f], [g]]
```

The short final block is the difference from `Window 3 3`, and it is usually
what a program wants — nothing is silently dropped:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Chunk 2
Maximum Technique: Sum Each Group
Reveal: stdout
```
```input
1,2,3,4,5
```
```output
[3, 7, 5]
```

### Take While / Drop While — `List<T> × (T -> Bool) -> List<T>`

```domain
Cursed Technique: Take While
    Using: (x) -> x < 4        # [1, 2, 9, 3] -> [1, 2]
Cursed Technique: Drop While
    Using: (x) -> x < 4        # [1, 2, 9, 3] -> [9, 3]
```

The longest leading run all of whose elements satisfy the predicate, and
everything from the first failure onward. **They are not `Filter`**: both stop
testing at the boundary, so the `3` after the `9` above is neither taken nor
tested — a `Filter` would have kept it.

Together they split the list at one point: `Take While p` ++ `Drop While p` is
always the original.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Take While
    Using: (x) -> x < 4
Reveal: stdout
```
```input
1,2,9,3
```
```output
[1, 2]
```

`Drop While` is the other half, and the `3` shows why neither is a `Filter`:
testing stops at the `9`, so the `3` after it is kept without being tested.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Drop While
    Using: (x) -> x < 4
Reveal: stdout
```
```input
1,2,9,3
```
```output
[9, 3]
```

### Partition — `List<T> × (T -> Bool) -> List<List<T>>`

```domain
Cursed Technique: Partition
    Using: (x) -> x > 2
Cursed Technique: Take Item 0   # the matches
```

A two-element list, `[matching, non-matching]`, each in the input's order. One
pass and one predicate evaluation per element, where a `Filter` and its
negation cost two of each.

The halves are reached the way input sections already are — `Take Item 0` /
`Take Item 1` in the pipeline, `first(p)` / `last(p)` inside a lambda.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Partition
    Using: (x) -> x > 2
Reveal: stdout
```
```input
1,5,2,9
```
```output
[[5, 9], [1, 2]]
```

Both halves keep the input's order, and taking one of them is a stage rather
than a second predicate:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Partition
    Using: (x) -> mod(x, 2) = 0
Cursed Technique: Take Item 1
Maximum Technique: Sum
Reveal: stdout
```
```input
1,2,3,4
```
```output
4
```

### Iterate — `T × (T -> T) -> List<T>`

```domain
Cursed Technique: Iterate 5
    Using: (x) -> x * 2        # 1 -> [2, 4, 8, 16, 32]
```

The value after each of `n` applications of the step. A `Simple Domain:
Repeat n` loop threads a value through a body and keeps only where it ended
up; `Iterate` keeps the whole trajectory, which is what "had I been here
before?" needs. As with `Scan`, the starting value is not re-emitted, so the
result has exactly `n` elements and its last one is where the equivalent
`Repeat` would have finished.

The step must return its own input type — it has to be applicable again — but
that type can be anything, including a whole list or grid.

Not to be confused with `Simple Domain: Iterate Until Fixed Point`, the loop.

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Technique: Iterate 5
    Using: (x) -> x * 2
Reveal: stdout
```
```input
1
```
```output
[2, 4, 8, 16, 32]
```

The starting value is not re-emitted, so the result has exactly `n` elements
and the last is where a `Repeat n` would have finished — which is what makes
the trajectory searchable for a repeat:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Technique: Iterate 8
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

### Unfold — `T × (T -> Bool) × (T -> T) -> List<T>`

The dual of `Fold`: where a fold consumes a list into a value, an unfold grows
a value into a list.

```domain
Cursed Technique: Unfold
    While: (x) -> x > 1
    Using: (x) -> x / 2        # 20 -> [20, 10, 5, 2]
```

The current value is emitted while the `While:` predicate holds and advanced
by the `Using:` step, so a predicate that is false at the start gives the
empty list. Like the `Simple Domain` loops it is bounded at **1,000,000,000**
elements, in the interpreter and a compiled binary alike: a step that never
falsifies the predicate fails loudly instead of eating memory until it hangs.

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Technique: Unfold
    While: (x) -> x > 1
    Using: (x) -> x / 2
Reveal: stdout
```
```input
20
```
```output
[20, 10, 5, 2]
```

The value is emitted *before* the predicate advances it, so a predicate that
is already false yields nothing at all rather than one element:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Cursed Technique: Unfold
    While: (x) -> x > 100
    Using: (x) -> x / 2
Maximum Technique: Count
Reveal: stdout
```
```input
20
```
```output
0
```

### Scan — `List<T> × Seed? × (Acc, T -> Acc) -> List<Acc>`

The running fold: `Fold` keeps only the final accumulator, `Scan` keeps them
all.

```domain
Cursed Technique: Scan            # seedless: Acc is the element type
    Using: (a, b) -> a + b        # [1,2,3,4] -> [1, 3, 6, 10]

Cursed Technique: Scan            # seeded: Seed: fixes Acc, as in Fold
    Seed: 100
    Using: (acc, x) -> acc + x    # [1,2,3] -> [101, 103, 106]
```

There is exactly **one result per input element** — the accumulator *after*
folding that element in — so the result stays index-aligned with the list it
scanned, and the seed is not re-emitted. Index `i` of the output is the fold
of the first `i+1` inputs, and the last element equals what `Fold` with the
same seed and lambda would have returned.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Scan
    Using: (a, b) -> a + b
Reveal: stdout
```
```input
1,2,3,4
```
```output
[1, 3, 6, 10]
```

Seeded, the accumulator type comes from `Seed:` rather than the elements, and
the seed itself is not emitted — so the output stays index-aligned with the
input:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Scan
    Seed: 100
    Using: (acc, x) -> acc + x
Reveal: stdout
```
```input
1,2,3
```
```output
[101, 103, 106]
```

Without `Seed:` the first element starts the accumulator (so `Acc` is the
element type, and any type works — not just the Int and Text a `Seed:`
literal can spell). Scanning the empty list gives the empty list either way.

### Take Item — `List<T> -> T`

```domain
Cursed Technique: Take Item 0
```

0-based; out of range is a runtime error. Typically picks an input section
after a `Split` (see Channels in [language.md](language.md)).

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Take Item 1
Reveal: stdout
```
```input
first section

second section
```
```output
second section
```

It leaves the list layer entirely — the current value afterwards is one
element, not a one-element list:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Domain Expansion: Sort
Cursed Technique: Take Item 0
Reveal: stdout
```
```input
5,3,9,1
```
```output
1
```

### Apply — `T × (T -> U) -> U`

```domain
Cursed Technique: Apply
    Using: (v) -> v * 2
```

The scalar analogue of Map Each: transform the whole current value. Useful
on its own and as a loop body.

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> upper(trim(t))
Reveal: stdout
```
```input
  hello
```
```output
HELLO
```

Because it transforms the value rather than its elements, it is also how a
whole list is reshaped in one step:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> concat(drop(xs, 2), take(xs, 2))
Reveal: stdout
```
```input
1,2,3,4,5
```
```output
[3, 4, 5, 1, 2]
```


### Range — `-> List<Int>`

```domain run
Cursed Energy: stdin
Cursed Technique: Range 0 to 5
Reveal: stdout
```
```input
```
```output
[0, 1, 2, 3, 4]
```

It ignores the current value rather than transforming it, which is what makes
it a source in the middle of a pipeline:

```domain run
Cursed Energy: stdin
Cursed Technique: Range 1 to 6
Maximum Technique: Product
Reveal: stdout
```
```input
```
```output
120
```


```domain
Cursed Technique: Range 5        # [0, 1, 2, 3, 4]
Cursed Technique: Range 1 16     # [1, …, 15]
```

The half-open integer range `[lo, hi)`, replacing the current value (like
`Combine` and `Zip`, which also ignore it). The bounds are
[measured arguments](#measured-arguments) — `Low:` and `High:` — and this is
where that matters most: `Range` discards its input, so
`High: (xs) -> length(xs)` is a range sized from the data that no literal
spelling can express. Half-open **deliberately**:
`range(N)` in a `For` header already means `0..N-1`, and two meanings of
"range" in one language would be worse than the occasional `Range 1 16`. It
also matches `slice`, `take` and `drop`. An inverted range is a resolve error.
