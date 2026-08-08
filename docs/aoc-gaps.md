# What Domain is still missing for Advent of Code

[aoc-toolbox.md](aoc-toolbox.md) maps the classic helper library onto Domain and
finds an entry for nearly every row. This page is the other half of that
audit: the survey went looking for AoC problems Domain **cannot** express, or
can only express at a speed that makes the answer unreachable, and found five
of them — plus a tail of sharp edges that cost a workaround rather than a
solve.

Everything below was reproduced against the interpreter and, where the claim
is about speed, against a compiled binary too. Each item says what it blocks,
what the workaround costs today, and the smallest change that would close it.

## Summary

| # | Gap | Severity | Blocks |
|---|---|---|---|
| 1 | ~~Functional collection update is O(size)~~ | **Closed** | — |
| 2 | ~~No weighted search over a state space~~ | **Closed** | — |
| 3 | ~~No counting or aggregating DP~~ | **Closed** | — |
| 4 | ~~Nested iteration cannot use a primitive on the inside~~ | **Closed** | — |
| 5 | No recursive data | **Blocker** | nested packets, snailfish numbers, expression trees |
| 6 | ~~No way to name the current value~~ | **Closed** | — |
| 7 | ~~Loop bodies cannot consume channels~~ | **Closed** | — |
| 8 | ~~`<` `>` `<=` `>=` reject `Text`~~ | **Closed** | — |
| 9 | ~~`Min By`/`Max By` require an `Int` key~~ | **Closed** | — |
| 10 | ~~`Map` is second-class in the pipeline layer~~ | **Closed** | — |
| 11 | ~~`Transpose` is `Grid`-only~~ | **Closed** | — |
| 12 | ~~`Match Pattern` has no repetition, alternation, or optional match~~ | **Closed** | — |
| 13 | ~~No first-order list builtins in the expression layer~~ | **Closed** | — |

---

## 1. Functional collection update is O(size)

**Closed** — see
[aoc-gaps-plan.md](aoc-gaps-plan.md#phase-1--linear-accumulators--done) for
what shipped. `insert`, `put` and `setat` still copy wherever the copy can be
observed; where a `Fold`'s accumulator is provably dead they write through, and
the two measurements below are now **0.05 s** and **0.03 s** interpreted
(0.007 s and 0.03 s compiled). `--no-optimize` still takes 23.7 s, which is
what makes it the oracle.

`insert`, `del`, `put`, `setat`, `with` and `set` each copied their whole
argument. That is the right *semantics* — [expressions.md](expressions.md)
explains why a lambda applied twice must not see its own work — but it is
implemented as a literal copy, so building a collection one element at a time
is **quadratic in the size of the collection**, not in the number of writes.

Measured, on this repo at `dd48530`:

| Program | Writes | Structure | `domain run` | compiled |
|---|---|---|---|---|
| linear DP, `Fold` + `insert` into a `Map<Int,Int>` | 20,000 | map grows to 20,001 | **30.2 s** | **12.4 s** |
| `Fold` + `setat` on a 300×300 `Grid<Text>` | 20,000 | 90,000 cells, constant | **43.6 s** | — |

Both are ordinary AoC shapes at ordinary AoC sizes, and both should be
milliseconds. The second is worse than the first: the grid never grows, so
every one of the 20,000 writes copies all 90,000 cells — 1.8 billion element
copies to change 20,000 of them.

What this blocks, in practice: falling sand, rope simulation, lights-out
grids, any "walk the input and mark cells" pass, any frequency table or
visited set built by hand rather than by `Count By`/`Group By`, and every
DP whose table is a `Map`. `docs/expressions.md` already flags the cost
("a fold over *n* elements is O(n·size)") and points at `Count By`/`Group By`
instead — but those build one *specific* collection from one pass, and the
problems above are exactly the ones that need a different one.

**How it was closed.** The first of the three options below, as written:

- **Uniqueness-driven mutation.** The accumulator of a `Fold` is dead the
  instant the lambda returns, so the copy is never observed. Recognizing
  `Fold` (and `Repeat`/`While` bodies) whose lambda threads the accumulator
  straight into an `insert`/`put`/`setat`/`set`/`with` — and lowering that one
  shape to an in-place write — removes the copy with no user-visible change
  and no type-system work. This covers most of the damage.
- **Runtime one-reference check.** Give `MapValue`/`GridValue`/`SparseValue` a
  cheap "this is the only handle" bit and mutate when it holds. Broader than
  the pattern match, still invisible to the user, but needs care in `codegen`
  where values are unboxed Go structs and slices.
- **Persistent structures** (HAMT for `Map`/`Set`, RRB for `List`). O(log n)
  rather than O(1), but correct by construction and needs no analysis. It
  would cost the compiled backend's "no interfaces, no boxing" property,
  which is a real trade against [compiler.md](compiler.md)'s claims.

The first option was the one to do, and it is what shipped: local to the
optimizer, testable against the existing naive oracle, and the difference
between a 30-second program and a fast one. The other two remain available if
the analysis ever needs to reach further than a fold's accumulator.

## 2. No weighted search over a state space

**Closed** — `Explore` gained `Mode: Cheapest` and `Mode: Costs`, driven by a
`Cost:` lambda in either the node-weight or the edge-weight arity. See
[primitives.md](primitives.md#cost--the-weighted-search).

`Domain Expansion: Dijkstra` took a `Grid<Int>` and nothing else.
`Domain Expansion: Explore` takes an arbitrary keyable state and a successor
lambda — but it is breadth-first, so every edge costs one.

Between them there is no way to ask for the cheapest path through a graph
whose nodes are not grid cells: state = (position, facing, run length), state =
(valve set, minute), state = (room configuration). That is a whole recurring
AoC family, and the runtime already has the missing piece — `ir.PQ`, a
stable min-heap, is sitting there driving grid Dijkstra.

**How it was closed**, as sketched:

```domain ignore
Domain Expansion: Explore
    Mode: Cheapest
    Cost: (s, t) -> weight(s, t)     # or Cost: (t) -> entry cost of t
    Until: (s) -> s = goal
    Using: (s) -> successors(s)
```

`Mode: Cheapest` returns the cost, `Mode: Costs` returns `Map<S, Int>` the way
`Mode: Distances` does today. The visited set becomes a settled set and the
queue becomes the `PQ`; the rest of `runExplore` is unchanged. An optional
`Heuristic:` turns the same primitive into A* — and it is exactly the kind of
substitution `Domain Expansion` is supposed to be free to make.

## 3. No counting or aggregating DP

**Closed** — `Explore` gained `Mode: Tally`, which folds the reachable DAG
with `Value:` and `Combine:` instead of walking it. See
[primitives.md](primitives.md#tally--the-counting-dp).

`Explore` memoized only *whether* a state was reached. It has no mode that
memoizes a *value* per state, so "how many ways" has no spelling at all.
Neither does memoized recursion, since a Shikigami is inlined and a
self-referential one is refused by design.

A linear DP does work, and works well — this is a correct, fast AoC 2020
Day 10 Part 2 modulo gap #1:

```domain
Maximum Technique: Fold
    Seed: (xs) -> insert(emptymap(0, 0), 0, 1)
    Using: (acc, j) -> insert(acc, j, getor(acc, j - 1, 0) + getor(acc, j - 2, 0) + getor(acc, j - 3, 0))
```

What has no spelling is a DP whose subproblems form a tree or a DAG rather
than a line: arrangement counting, dice-roll universe splitting, molecule
expansion, the "count paths to the goal" half of a search.

**How it was closed**, as sketched:

```domain ignore
Domain Expansion: Explore
    Mode: Tally
    Value: (s) -> 1                      # a leaf's contribution
    Combine: (a, b) -> a + b             # how a state's successors fold up
    Using: (s) -> successors(s)
```

This is a fold over the reachable DAG, which is what a memo table is. It
needs the successor graph to be acyclic (or a topological order), and
`Domain Expansion: Topological Sort` already exists to check that and to
name the offending node when it does not — the same error shape.

## 4. Nested iteration cannot use a primitive on the inside

**Closed** — a stage that takes an n-parameter lambda now takes a body too,
with the parameters named on the stage:

```domain ignore
Maximum Technique: Fold
    Seed: (xs) -> 0
    Params: acc, row                    # the body's current value is `row`;
    Domain Expansion: Sort              # `acc` is in scope like a binding
    Cursed Technique: Apply
        Using: (s) -> acc + first(s)
```

The body still computes one value from one value — the extra parameters arrive
as bindings, which is machinery the `Consider` implementation already had. Every
declared name is a real binding, the last one included, so it obeys the rules a
`Consider` name obeys (no shadowing a builtin, no duplicates). See
[aoc-gaps-plan.md](aoc-gaps-plan.md#phase-4--scope--done).

The original report follows.

A `Using:` written as an indented pipeline is the escape hatch for
"this per-element job needs a primitive", and it is a good one. But it stands
in only for a **1-parameter** lambda, and the refusal is explicit:

```
Fold takes a 2-parameter Using: lambda, and a nested body computes one value
from one value — write the lambda, or move the body to a stage that takes a
1-parameter lambda
```

`Fold`, `Reduce`, `Scan`, `All Pairs` and `Combinations k` are therefore
closed to primitives. Combined with the expression layer having no
higher-order builtins (gap 13), the rule is: *inside a fold you get flat
expression builtins and nothing else*. A fold whose step needs to sort, group,
search, or run a second fold cannot be written — which is what a
two-dimensional DP is.

**Closing it.** Let a body stand in for an n-parameter lambda by naming the
parameters on the stage:

```domain ignore
Maximum Technique: Fold
    Seed: (xs) -> list()
    Params: acc, row                    # the body's current value is `row`;
    Domain Expansion: Sort By           # `acc` is in scope like a binding
        Using: (c) -> c
    ...
```

The body still computes one value from one value — the extra parameters
arrive as bindings, which is machinery the `Consider` implementation already
has. That reuses the whole existing body path rather than adding a second one.

## 5. No recursive data

`ir.Type` has no recursive constructor, so a value whose depth is not known
statically cannot be typed. Nested packet lists, snailfish pairs, an
expression tree parsed out of a line, anything JSON-shaped: not slow, not
awkward — unrepresentable.

This is the most expensive item here and the least AoC-frequent (roughly one
day a year), so it is the one to defer knowingly rather than the one to leave
undocumented. If it is ever taken on, the cheap version is a single built-in
`Nested<T>` (a value is either a `T` or a list of `Nested<T>`) with a parse
primitive and a fold primitive, rather than user-declared recursive types —
that covers the AoC cases without putting sum types into a language that has
no other use for them.

---

## Sharp edges

These all have workarounds. Each one costs a few lines and a reread.

### 6. No way to name the current value

**Closed** — `Consider line Of Itself`, and `domain expansion: optimize`
rewrites the old spelling into it. The stage-versus-element rule below is
now stated outright in [expressions.md](expressions.md#scope), which is the
other half of what made this confusing.

`Consider … Of` on a `Map Each` binds what the operation makes of the
**stage's** input — the whole list — not of the element. Inside a body, the
element is bindable only by attaching the `Consider` to a nested statement
and going through an identity `Apply`:

```domain ignore
Cursed Technique: Map Each
    Cursed Technique: Apply
        Consider line Of Apply
            Using: (l) -> l              # the identity dance
        Consider dig As (i) -> … charat(line, i) …
        Cursed Technique: Apply
            Using: (l) -> range(0, length(l))
        Cursed Technique: Map Each
            Using: (i) -> dig(i)
```

That is AoC 2023 Day 1 Part 2, and the identity `Apply` is pure ceremony.
`Consider line Of Itself` (or bare `Consider line Of`, with no operation)
would delete it. The stage-versus-element rule is also worth stating outright
in [expressions.md](expressions.md#scope) — the current wording ("once per
pass through the stage") is accurate but reads the other way for `Map Each`.

### 7. Loop bodies cannot consume channels

**Closed for loops**, which is where the complaint was. A Shikigami and a
`Using:` body still refuse one and now say why: the first is inlined at call
sites that need not share a scope, the second compiles to a function where a
channel's local is not in scope.

One honest caveat. The shape that benefits is `Fold From:`, which folds a
channel's list into the state each lap. `Combine` and the other consumers
*replace* the current value, so they rarely satisfy a loop's type-preservation
rule — meaning a loop lambda still cannot simply **name** a channel. Closing
that would want a `Consider … From:`, which is not in the language.

`From:` was refused inside a loop or Shikigami body, so a loop had no way to
see a read-only value parsed above it. Everything it needs has to be smuggled
through the state, and since a loop body must preserve its value type, the
smuggled part rides along forever. AoC 2023 Day 8 becomes a 4-tuple whose
last two slots never change:

```domain ignore
Maximum Technique: Combine
    From: instr, net
    Using: (i, n) -> tuple("AAA", 0, i, n)
Simple Domain: While
    Using: (s) -> ikke item(s, 0) = "ZZZ"
    Cursed Technique: Apply
        Using: (s) -> consider ins as item(s, 2) in consider node as get(item(s, 3), item(s, 0)) in
            tuple(if item(ins, item(s, 1) % length(ins)) = "L" then item(node, 0) else item(node, 1),
                  item(s, 1) + 1, ins, item(s, 3))
```

The real state is `(node, steps)`; the other half is plumbing. A channel is
fully computed before the loop starts and is immutable, so there is no
ordering hazard in letting a loop body read one — the restriction looks
inherited from "channels cannot nest" rather than motivated on its own.

### 8. `<` `>` `<=` `>=` reject `Text`

**Closed** — the operators now reach exactly as far as `ir.Ordered`.

```
comparison needs Int or Float operands, got Text and Text
```

…while `Domain Expansion: Sort` ordered `List<Text>` happily, and `Sort By`
accepted a `Text` key. The ordering existed in the runtime and was simply not
reachable from a lambda: no lexicographic guard, no `min` over names, no text
tiebreak in a compound predicate.

`<` `<=` `>` `>=` now compare any two values of one ordered type — Int, Float,
Text, or a Tuple of those — over `ir.Compare`, the same ordering the sorting
primitives use. Anything else (Bool, Record, List, Map, Set, Grid, Sparse) is
a resolve error naming the rule. See
[expressions.md](expressions.md#grammar).

### 9. `Min By`/`Max By` require an `Int` key

**Closed** — both take any ordered key, exactly as `Sort By` does.

```
Max By key lambda must return Int, got Text
```

`Sort By` took `Text` and tuple keys; `Min By`/`Max By` took `Int` only. Two
reductions over the same notion of "key" disagreeing about what a key may be
is the kind of inconsistency that costs a lookup every time — and it is now
a pinned property rather than a coincidence: whatever `Min By` picks, a
`Sort By` on the same key puts first.

### 10. `Map` is second-class in the pipeline layer

**Closed** — a `Map` reads as its entries wherever a List is accepted, and
`Count` over one now typechecks, so the `aoc-toolbox.md` row that always
claimed it is finally true.

`Set` flowed into the list-shaped primitives. `Map` did not:

```
Map Each expects a List input, got Map<Text, Int>
Sum expects input of type List<Int>, but the pipeline produced Map<Text, Int>
Count expects a List or Set input, got Map<Int, Int>
```

Since `Count By` and `Group By` are among the most-reached-for reductions,
almost every program that uses one immediately spends a
`Channeled Energy: Convert To Entries` to get back to a shape the rest of the
language accepts. Accepting a `Map` wherever a `List<(K,V)>` is accepted —
iterating entries in insertion order, which is already the rendering order —
would remove that step.

(The `aoc-toolbox.md` line that offered `Maximum Technique: Count` for
`len(m)` — which does not typecheck — is corrected; it now says `size(m)`
and names the restriction. Making the original claim *true* is the rest of
this gap.)

### 11. `Transpose` is `Grid`-only

**Closed** — `Transpose` takes a `List<List<T>>` too.

`Extract Integers`, `Split Fields`, `Split Each` and positional `Match Pattern`
all produce `List<List<T>>`, and column-wise work on that shape needed a
`Convert To Grid` round-trip — which additionally requires the rows to be
same-typed, a constraint transposition does not need. A ragged list of rows is
a runtime error naming the row and both lengths, the same one
`Convert To Grid` raises.

### 12. `Match Pattern` has no repetition, alternation, or optional match

**Closed for the two cases that mattered** — a repeating hole and `Mode: Try`.
See [match-pattern.md](match-pattern.md#repetition) and
[aoc-gaps-plan.md](aoc-gaps-plan.md#phase-5--parsing--done).

```domain ignore
Cursed Technique: Match Pattern
    Mode: Try
    Using: "{id:word}: {vals:int+ sep=\",\"}"
```

**Fully closed as of phase 6.** What phase 5 left — a repeated *hole* is not a
repeated *group*, and a `{text}` sponge is not an optional one — is what
[phase 6](aoc-gaps-plan.md#phase-6--template-groups--done) added:

```domain ignore
Using: "{name:word} ({w:int})[? -> {kids:word+ sep=\", \"}]"
Using: "Game {id:int}: {draws:( {n:int} {color:word} )+ sep=\", \"}"
```

AoC 2017 D7 and 2023 D2, one template each. An absent group leaves its holes at
their type's zero, and `{?name}` inside it adds a `Bool` for when the zero and
a captured zero have to be told apart.

Alternation followed in
[phase 7](aoc-gaps-plan.md#phase-7--reach--done) as `Case:` — several templates
on one stage, first match wins, a `kind` field naming it — together with
`Mode: Scan` for a template that describes a *fragment* rather than a line.

**Still absent, on purpose:** nested groups (one level only), and alternation
*inside* a template, which would need a sum type for its result to land in.

The original report follows.

[match-pattern.md](match-pattern.md) lists these as deliberate omissions, and
the reasoning (stay readable, fall back to `Extract Integers`) holds for most
inputs. Two cases where it does not:

- **A repeated group.** `Game 1: 3 blue, 4 red; 1 red, 2 green` needs
  splitting three ways before any template applies. A repetition hole —
  `{cubes:int+ sep=", "}` capturing a `List<Int>` — would keep one template.
- **Two shapes in one file.** A non-match is a hard runtime error, so a file
  mixing `turn on 0,0 through 9,9` and `toggle 0,0 through 9,9` cannot be
  matched with one pass or tried with two. A `Mode: Try` yielding
  `List<Record>` of the lines that matched (or an `Otherwise:` arm) would.

### 13. No first-order list builtins in the expression layer

**Closed** — `sort`, `unique`, `flatten`, `product`, `zip`, `enumerate`,
`chunk`, `windows` and `transpose` are in the table, in all three layers. See
[expressions.md](expressions.md#first-order-list-operations).

Already flagged in
[expressions.md](expressions.md#what-the-expression-layer-does-not-have-yet):
`sort`, `unique`, `flatten`, `product`, `zip`, `enumerate`, `chunk`,
`windows`, `transpose` take no function argument, so they do not violate the
"no higher-order builtins" rule — they are simply absent. Each one absent
forces a nested body where an expression would have done, and inside a `Fold`
(gap 4) a nested body is not available at all, so their absence there is
absolute rather than merely verbose. This is the cheapest item on the page:
the operations all exist as primitives, and each is one entry in three tables.

---

## Where to start

Ranked by (problems unblocked) ÷ (work):

1. ~~**Gap 1**, via the `Fold`-accumulator rewrite.~~ Done — see
   [aoc-gaps-plan.md](aoc-gaps-plan.md#phase-1--linear-accumulators--done).
2. ~~**Gap 13**, the first-order list builtins.~~ Done, with gap 10 — see
   [aoc-gaps-plan.md](aoc-gaps-plan.md#phase-3--sequences--done).
3. ~~**Gaps 8 and 9**, `Text` ordering.~~ Done, along with gap 11 — see
   [aoc-gaps-plan.md](aoc-gaps-plan.md#phase-0--make-the-ordering-agree-with-itself--done).
4. ~~**Gap 2**, `Explore` with a `Cost:`.~~ Done, with gap 3 — see
   [aoc-gaps-plan.md](aoc-gaps-plan.md#phase-2--explore-with-a-cost-and-a-tally--done).
5. ~~**Gaps 6 and 7**, the two friction items that show up in almost every
   nontrivial program.~~ Done, with gap 4 — see
   [aoc-gaps-plan.md](aoc-gaps-plan.md#phase-4--scope--done).
6. ~~**Gap 12**, repetition and `Mode: Try`.~~ Done — see
   [aoc-gaps-plan.md](aoc-gaps-plan.md#phase-5--parsing--done), and its
   residue with [phase 6](aoc-gaps-plan.md#phase-6--template-groups--done).
7. **Gap 5** is what is left, and it is the one deliberately deferred: the
   most expensive item here and the least AoC-frequent. (Gap 3 shipped
   alongside gap 2, since both are modes on one primitive.)
