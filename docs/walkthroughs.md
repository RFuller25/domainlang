# Walkthroughs

The reference pages define each piece of Domain on its own. This page shows the
pieces working together: whole programs, start to finish, with what actually
flows between the stages.

Every program here runs exactly as printed, and every output on this page came
from running it. Where a table shows types and sizes, that is `domain run
prog.domain --stats` — the stage list is the pipeline, so it is also the fastest
way to see what a program is really doing.

| Walkthrough | What it shows |
|---|---|
| [Reading a pipeline](#reading-a-pipeline) | the shape of every Domain program, one stage at a time |
| [Values a stage can name](#values-a-stage-can-name) | `Consider … As` / `Consider … Of` |
| [Loops](#loops) | all four `Simple Domain` drivers, one program each |
| [Two sections](#two-sections) | `Channel` + `Combine` |
| [Two answers](#two-answers) | `Part` blocks |
| [When a lambda cannot do the job](#when-a-lambda-cannot-do-the-job) | a `Using:` written as a pipeline |
| [Naming a composition](#naming-a-composition) | `Shikigami` |
| [Watching the optimizer](#watching-the-optimizer) | `--explain`, and what a "request" means |
| [Putting it together](#putting-it-together) | bindings inside a loop inside a Part |

---

## Reading a pipeline

The canonical program — AoC 2022 day 1 part 2, sum the calories of the top
three elves:

```domain
Cursed Energy: input.txt
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
```

There is no state but one implicit **current value**, and each line transforms
it. `--stats` prints exactly that:

```
    #  stage                                  out type                size
    1  Read Source <- input.txt               Text                      54
    2  Split by "\n\n"                        List<Text>                 5
    3  Split Each by "\n"                     List<List<Text>>           5
    4  Convert Each List to Integers          List<List<Int>>            5
    5  Sum Each Group                         List<Int>                  5
    6  Cursed Quickselect: Top 3, Sum         Int                        —
    7  Reveal -> stdout                       Int                        —
```

Read the `out type` column downward and you have the whole program: text, split
into groups, each group into lines, lines into numbers, each group summed, then
one number out.

**Stage 6 is not what was written.** The program asked for a sort followed by a
top-3 sum; the optimizer answered with a quickselect, which produces the same
answer without ordering the rest. That is the language's central claim, and
[`--explain`](#watching-the-optimizer) makes it say so out loud.

The keywords are optional — the same program without a single one resolves to
the identical pipeline (see [examples/16_no_prefixes](../examples/README.md)).
They are there to say what *kind* of step a line is.

---

## Values a stage can name

A lambda sees one element at a time. So a per-element test that depends on the
whole list — anything measured against a total, a mean, a maximum — has nothing
to reach for.

**`Consider` binds a value for a whole stage.** Here is "how many readings beat
the average by more than a margin", which needs both a constant and a value
derived from the list:

```domain run
Cursed Energy: readings.input
Shikigami: Lines
Channeled Energy: Convert List to Integers
Maximum Technique: Count Matching
    Consider tolerance As 5
    Consider mean Of (xs) -> sum(xs) / length(xs)
    Using: (x) -> x - mean > tolerance
Reveal: stdout
```
```input
10
12
30
8
40
5
22
3
```
```output
3
```

The mean of those eight readings is 16, so the test is `x > 21`, and three of
them clear it.

The two prepositions are the whole distinction, and there has to be one,
because a 1-parameter lambda already means two different things in Domain
depending on where it is written — per element in a `Using:`, once over the
current value in a [measured argument](language.md#measured-arguments):

| Form | Binds | Sees the pipeline value? |
|---|---|---|
| `Consider tolerance As 5` | a constant | no |
| `Consider double As (x) -> x * 2` | **a function**, called as `double(n)` | no |
| `Consider mean Of Sum` | an operation applied to the current value | yes |
| `Consider mean Of (xs) -> …` | a lambda applied to it | yes |
| `Consider mean Of` + an indented pipeline | a whole sub-pipeline over it | yes |

`Of` also takes a pipeline, which is what to reach for when the value needs a
primitive to compute:

```domain run
Cursed Energy: readings.input
Shikigami: Lines
Channeled Energy: Convert List to Integers
Cursed Technique: Filter
    Consider mean Of
        Maximum Technique: Sum
        Cursed Technique: Apply
            Using: (s) -> s / 8
    Using: (x) -> x > mean
Reveal: stdout
```
```input
10
12
30
8
40
5
22
3
```
```output
[30, 40, 22]
```

The same readings, above the same mean of 16 — reached this time by a stage of
its own rather than by a lambda.

A binding is in scope for every lambda on its statement and for everything
nested beneath it; bindings read in written order, so one may be written in
terms of another. The full rules — shadowing, what a binding may not be named,
and what each kind costs — are in
[expressions.md](expressions.md#stage-bindings--consider--as--consider--of).

**What it costs: nothing, twice.** A constant is substituted into the lambdas
that read it as a literal, and a function is inlined at each call site, so
neither survives to run time. Only an `Of` binding becomes a real stage — the
`Consider mean` row `--stats` shows — and it is computed once per pass, not
once per element.

---

## Loops

`Simple Domain` has four drivers. They differ in one question — *what decides
when to stop?* — and nothing else: each takes an indented body that must give
back the same type it was handed, so the value can go round again.

All four programs below read the same five numbers, `3 1 4 1 5`.

### `Repeat n` — a count decides

```domain run
Cursed Energy: nums.input
Shikigami: Lines
Channeled Energy: Convert List to Integers
Simple Domain: Repeat 3
    Cursed Technique: Map Each
        Using: (x) -> x * 2
Reveal: stdout
```
```input
3
1
4
1
5
```
```output
[24, 8, 32, 8, 40]
```

Three doublings, ×8.

The count can itself be measured from the data
(`Times: (xs) -> length(xs)`), which is how a loop runs as many times as there
are elements.

### `While` — a predicate decides

```domain run
Cursed Energy: nums.input
Shikigami: Lines
Channeled Energy: Convert List to Integers
Simple Domain: While
    Using: (xs) -> sum(xs) < 100
    Cursed Technique: Map Each
        Using: (x) -> x * 2
Reveal: stdout
```
```input
3
1
4
1
5
```
```output
[24, 8, 32, 8, 40]
```

The same place, reached by a different rule: the sum
runs 14 → 28 → 56 → 112, and the fourth check stops it. The predicate is
checked *before* each lap, so a list that already fails it is returned untouched.

### `Iterate Until Fixed Point` — the data decides

```domain run
Cursed Energy: big.input
Shikigami: Lines
Channeled Energy: Convert List to Integers
Simple Domain: Iterate Until Fixed Point
    Cursed Technique: Map Each
        Using: (x) -> if x > 9 then x / 10 else x
Reveal: stdout
```
```input
3452
17
8
90000
```
```output
[3, 1, 8, 9]
```

Every number down to one digit, with no count and no predicate: it stops when a
lap changes nothing. `--stats --verbose` shows how many that took:

```
    4  Iterate Until Fixed Point (5 frames, …  List<Int>     4
       ↳ Map Each                              List<Int>     4    ×5
```

Five laps — a number nothing in the program mentions, because the input is what
determines it.

### `For x in <channel>` — a list decides

```domain run
Cursed Energy: nums.input
Shikigami: Lines
Channeled Energy: Convert List to Integers
Channel "shifts":
    Cursed Technique: Apply
        Using: (xs) -> list(1, 10, 100)
Simple Domain: For s in shifts
    Cursed Technique: Map Each
        Using: (x, s) -> x + s
Reveal: stdout
```
```input
3
1
4
1
5
```
```output
[114, 112, 115, 112, 116]
```

One lap per element of the channel — +1, then +10, then +100 — with that
element bound to `s`.

This is the one driver that brings a *name* into the body, and it does it
positionally: every `Using:` lambda inside a `For` body takes one extra trailing
parameter per enclosing loop. That is why the inner lambda is `(x, s)` rather
than `(x)` — `x` is Map Each's element, `s` is the loop's.

| Driver | Stops when | Body sees |
|---|---|---|
| `Repeat n` | the count runs out | the value |
| `While` | the predicate is false | the value |
| `Iterate Until Fixed Point` | a lap changes nothing | the value |
| `For x in c` | the channel is exhausted | the value, plus `x` |

---

## Two sections

Real inputs are rarely one shape. A `Channel` runs a sub-pipeline **on the
current value** and stores the result under a name, leaving the current value
untouched — so sibling channels all branch from the same place — and a `From:`
consumer brings them back together.

`examples/05_two_sections.domain`, over a roster and a score list separated by
a blank line:

```domain run
Cursed Energy: 05_two_sections.input
Cursed Technique: Split Text by "\n\n"

Channel "squad":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by "\n"
    Maximum Technique: Count

Channel "score":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by "\n"
    Channeled Energy: Convert List to Integers
    Maximum Technique: Sum

Maximum Technique: Combine
    From: squad, score
    Using: (squad, score) -> squad * 1000 + score

Reveal: stdout
```
```input
gojo
nanami
itadori
nobara

75
120
95
```
```output
4290
```

Four names and 290 points, combined into one number.

The split happens **once**, above both channels; each takes the section it
wants. `Combine`'s lambda takes one parameter per channel named in `From:`, in
that order, and its result becomes the new current value.

---

## Two answers

Advent of Code asks two questions about one input, and the parse is the same
for both. A `Part` branches from the current value like a Channel, but labels
what its body reveals instead of storing it — so the work above the Parts
happens once and each Part sees the same value.

`examples/17_two_parts.domain`:

```domain run
Cursed Energy: 17_two_parts.input
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group

Part "1":
    Maximum Technique: Max
    Reveal: stdout

Part "2":
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top 3, Sum
    Reveal: stdout
```
```input
1000
2000
3000

4000

5000
6000

7000
8000
9000

10000
```
```output
Part 1: 24000
Part 2: 45000
```

Each Part reveals under its own label. The label is a literal in the compiled
binary — the compiler knows every one statically — so two answers cost no
branch at run time.

---

## When a lambda cannot do the job

The expression layer has no higher-order builtins: nothing in it maps, filters
or searches. So once a lambda's parameter is itself a list, a job that needs a
*primitive* has no expression spelling at all.

**Indent a pipeline where the lambda would go.** It runs in the lambda's place,
once per element, with that element as its current value.
`examples/19_row_pairs.domain` finds the one evenly-divisible pair in each row
and divides it:

```domain run
Cursed Energy: 19_row_pairs.input
Shikigami: Lines
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> (a % b = 0) or (b % a = 0)
    Maximum Technique: Reduce
        Using: (x, y) -> max(x, y) / min(x, y)
Maximum Technique: Sum
Reveal: stdout
```
```input
5 9 2 8
9 4 7 3
3 8 6 5
```
```output
9
```

One evenly-divisible pair per row, each divided, and the three quotients
summed.

This is not a `Map Each` feature. A lambda `(x) -> e` and a sub-pipeline both
turn one value into one value, so a body stands in **wherever a 1-parameter
`Using:` is accepted** — `Filter`, `Sort By`, `Group By`, `Any`/`All`, the grid
searches, and the rest. What it cannot do is stand in for a lambda that takes
two parameters, and the stages that need one say so with the arity named.

---

## Naming a composition

A `Shikigami` is a name for a composition of primitives, with parameters.
`examples/10_shikigami.domain`:

```domain run
Shikigami "Scaled Sum" (factor: Int)
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (v) -> v * factor

Cursed Energy: 10_shikigami.input
Shikigami: Ints
Cursed Technique: Filter
    Using: (x) -> x > 0
Shikigami: Scaled Sum
    factor: 7
Reveal: stdout
```
```input
5
-2
11
0
17
```
```output
231
```

Two calls there, and one of them is not in the file: `Ints` comes from the
**prelude**, which is written in Domain and loaded before every program.
`Scaled Sum` is the program's own. Both are **inlined** at the call site with
the arguments substituted, which is why a Shikigami costs nothing and why the
optimizer sees through one — a rewrite that would fire on the primitives
written out fires just the same through the name. The same is true across an
`Innate Domain:` import.

A Shikigami may declare its pipeline type (`: List<Int> -> Int`), and then the
mismatch is reported against the *call* rather than surfacing from somewhere
inside the inlined body. It may not be recursive: an inlined body has no finite
expansion, and `Domain Expansion: Explore` is the answer for a search.

---

## Watching the optimizer

`Domain Expansion:` names an algorithm, and the compiler treats that as a
request. `examples/02_pair_sum.domain` asks for the first pair summing to 100:

```domain run
Cursed Energy: 02_pair_sum.input
Shikigami: Ints
Domain Expansion: All Pairs
    Mode: First
    Using: (a, b) -> a + b = 100
Maximum Technique: Product
Reveal: stdout
```
```input
12
41
87
23
59
61
39
```
```output
2419
```

`--explain` says how it got there:

```
$ domain run 02_pair_sum.domain --explain
[explain] Domain rewrote All Pairs (sum = 100) → Cursed Hash-Set Scan. Guaranteed hit.
2419
```

The program asked for every pair — O(n²). It got a single hash-set pass — O(n)
— because the optimizer matched the *shape of the lambda body*: `a + b =
<constant>` is a pattern it knows a better algorithm for.

Two things follow from "it matches the body's shape". A value the optimizer
cannot see stands the rewrite down, which is why a
[measured argument](optimizer.md#measured-arguments-and-the-passes-that-fold-literals)
does; and it is why a constant `Consider` binding is folded into the lambda as
a literal rather than bound at run time — `Consider target As 100` keeps the
rewrite, and a test pins that.

`--no-optimize` runs the naive pipeline instead. Every optimizer pass is
oracle-tested against it, so the two agree by construction; when you want to
check that for yourself, run both.

---

## Putting it together

Stock levels, one per line, and two questions about them. This uses bindings
inside a loop inside a Part:

```domain run
Cursed Energy: stock.input
Shikigami: Lines
Channeled Energy: Convert List to Integers

Part "understocked":
    Maximum Technique: Count Matching
        Consider average Of (xs) -> sum(xs) / length(xs)
        Using: (x) -> x < average
    Reveal: stdout

Part "after restocking":
    Simple Domain: While
        Consider opening_average Of (xs) -> sum(xs) / length(xs)
        Using: (xs) -> min(xs) < opening_average
        Cursed Technique: Map Each
            Consider average Of (xs) -> sum(xs) / length(xs)
            Consider crate As 2
            Using: (x) -> if x < average then x + crate else x
    Maximum Technique: Sum
    Reveal: stdout
```
```input
4
11
2
9
7
1
```
```output
Part understocked: 3
Part after restocking: 44
```

The two bindings are named differently on purpose, and the names are the
lesson. **An `Of` binding is computed once when its scope opens**, and the two
scopes here are not the same:

- `opening_average` belongs to the `While` statement, whose scope is *the whole
  loop*. It is computed once, before the first lap, from the list going in — so
  the loop's condition is measured against where it started.
- `average` belongs to the `Map Each` inside the body, whose scope is one lap.
  It is recomputed every time round, against that lap's list.

Both are correct; they are simply different questions, and the binding names
are where a reader can see which one a stage is asking. If you want a value
recomputed per lap, bind it inside the body; if you want the loop measured
against its starting point, bind it on the loop.

---

## Writing one of these, start to finish

Every program above is shown finished. This is the loop that produces one, in
the editor that carries the rest of this documentation's engines:

```sh
domain expansion: development day7.domain --input day7.input
```

The input is read first, and its shape decides the opening. A file of
`6,10` / `0,14` lines offers a `Match Pattern` template inferred from the lines
themselves; a rectangle of digits offers `Shikigami: Digit Grid` *and*
`Shikigami: Ints`, because that shape is genuinely two readings and the file
cannot say which was meant. Accepting one puts it after the source stage, where
it can see the value the source produced.

From there the pipeline is written a statement at a time, and the thing that
makes it quick is that the answers are already on screen: the type flowing out
of each line at the end of it, so a stage that produced `List<List<Text>>` when
you wanted `List<Int>` is visible immediately rather than at the next run; the
error for the line the cursor is on in the status bar; `tab` for the primitive
whose name you half-remember; `alt+k` for what it does to the type.

`ctrl+r` runs it, on a screen that charts what the run costs and says where in
the program it has got to — and stays there afterwards as the report on it.
`ctrl+t` records a run and opens the stepper from
[cli.md](cli.md#domain-expansion-visualize) over the program on screen — the
same panes, over a buffer that has never been saved. When a stage produced the
wrong shape, the tree is where you find out which one, and the fix is two keys
away rather than in another window.

The whole of that is the same engines this page's programs were checked with:
one resolver, one diagnostics engine, one recorder. See
[development.md](development.md).

---

## Where to go next

- [getting-started.md](getting-started.md) — the ground-up tutorial, if you
  arrived here first.
- [primitives.md](primitives.md) — every stage these programs used, with its
  signature, arguments and failure modes.
- [expressions.md](expressions.md) — everything legal inside a `Using:`.
- [../examples/](../examples/README.md) — twenty-one runnable programs, each
  showing one piece of the language, all golden-tested in both backends.
- [development.md](development.md) — the editor the section above uses, in
  full: every key, what it analyzes, and what it deliberately does not do.
