# Getting started with Domain

Domain is a language for the kind of problem Advent of Code sets: read some
awkward input, reshape it, compute an answer. It is built on one idea —

> **You describe *what* needs to happen. The compiler decides *how*.**

Everything else follows from that. This guide starts from nothing and ends
with you compiling your own programs. Every complete program on this page is
run by the test suite and diffed against the output shown beneath it, so what
you read is what the program actually printed — not what someone remembered it
printing.

## 1. The idea, in one program

Here is a program that asks for a sort and then the top three. You are not
meant to be able to read it yet — §3 explains every line — so just watch what
happens to the request:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"
Cursed Technique: Split Each by "\n"
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Domain Expansion: Quicksort, Descending
Maximum Technique: Select Top 3, Sum
Reveal: stdout
```
```input
1000
2000

4000

5000
6000

7000
```
```output
22000
```

Run it with `--explain` and Domain tells you it did not sort:

```
[explain] Domain rewrote Quicksort (Descending) + Top 3 → Cursed Quickselect. Guaranteed hit.
```

`Domain Expansion:` names an algorithm, and the name is a **request, not a
command**. Sorting to take three of them is O(n log n) to answer an O(n)
question, so the optimizer recognized the pair and substituted a quickselect.
The answer is identical; you never asked for the faster one.

`--no-optimize` runs the literal program instead. The two must always agree,
and the test suite holds them to it over thousands of generated inputs — so
when you are unsure whether a rewrite changed your answer, you can check
rather than wonder.

That is the whole thesis. The rest of this guide is the vocabulary you need to
make requests worth answering.

## 2. Install

You need Go or Nix.

```sh
# From a clone of the repository:
go build -o domain ./cmd/domain
./domain --help

# Or with Nix, no clone needed:
nix run github:RFuller25/domain -- --help
```

The CLI picks its mode from the arguments:

```sh
domain prog.domain < input.txt    # a bare file → interpret it
domain prog.domain -o prog        # an extra argument → compile to ./prog
domain check prog.domain          # typecheck only, run nothing
domain repl                       # build a pipeline line by line
```

## 3. One value, flowing down

A Domain program is a **pipeline**. There is exactly one value in flight, each
line transforms it, and the next line receives the result.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert List to Integers
Maximum Technique: Sum
Reveal: stdout
```
```input
3
4
5
```
```output
12
```

Read it top to bottom and track the value's type:

| Line | The value after it |
|---|---|
| `Cursed Energy: stdin` | `Text` — the whole input |
| `Split Text by "\n"` | `List<Text>` — the lines |
| `Convert List to Integers` | `List<Int>` |
| `Sum` | `Int` |
| `Reveal: stdout` | (prints it) |

There are no variables here, and nothing is named. That is the point: a
pipeline reads as a sentence because there is only ever one thing to talk
about.

### The keywords name roles

The themed keywords are not decoration. Each one names a **semantic role**,
and the tooling relies on them:

| Keyword | Role |
|---|---|
| `Innate Domain:` | import a library of Shikigami (this keyword is required) |
| `Cursed Energy:` | where data comes from (a file name, or `stdin`) |
| `Cursed Technique:` | 1:1 transforms — `Split`, `Map Each`, `Filter`, … |
| `Channeled Energy:` | type coercions — `Convert To Integers`, `To Grid`, `To Set`, … |
| `Maximum Technique:` | reductions — `Sum`, `Count`, `Fold`, `Group By`, … |
| `Domain Expansion:` | **a named algorithm the optimizer may replace** |
| `Reverse Cursed Technique:` | inversions — `Reverse` |
| `Simple Domain:` | loops — `Repeat N`, `While`, `Iterate Until Fixed Point`, `For` |
| `Cursed Object:` / `Cursed Tool:` | declare a program-wide value / change it |
| `Channel "name":` | a named side branch |
| `Part "label":` | a labelled answer, for two-part puzzles |
| `Shikigami:` | call a user-defined or prelude operation |
| `Binding Vow:` | a debug assertion over the current value |
| `Reveal:` | print the current value |

They are also **optional**. Most operations only make sense in one role, so
the compiler works it out. This is the same program:

```domain run
stdin
Split Text by "\n"
Convert List to Integers
Sum
stdout
```
```input
3
4
5
```
```output
12
```

Write the keyword where it clarifies and drop it where it does not — the two
mix line by line. This guide keeps them, because they are the fastest way to
learn the roles. See [optional keywords](language.md#optional-keywords) for
the one thing dropping them costs you.

Two syntax rules: indentation is significant and **spaces only**, and `#`
starts a comment.

## 4. It fails before it runs

Every program is fully typechecked first. Feed `Sum` a list of text and you
get a position, not a crash halfway through your input:

```
3:1: Sum expects input of type List<Int>, but the pipeline produced List<Text>
```

`domain check prog.domain` does that without running anything. When the
problem is a name rather than a type, `domain expansion: diagnosis` goes
further:

```
error[name]: unknown operation "Sumb" under "Maximum Technique"
  --> prog.domain:4:1
   4 | Maximum Technique: Sumb
     | ^^^^^^^
  help: did you mean "Sum"?
  fix: auto-fixable — run `domain expansion: fix`
```

That family — `diagnosis`, `lint`, `fix`, `optimize` — is worth knowing early;
see [diagnostics.md](diagnostics.md).

## 5. The second layer: lambdas

Pipelines move data. **Lambdas** decide details. Any primitive needing
per-element logic takes a `Using:` argument, written in a plain, unthemed
expression language:

```domain run
Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Filter
    Using: (n) -> n > 0 and n % 2 = 0
Cursed Technique: Map Each
    Using: (n) -> if n > 100 then 100 else n
Maximum Technique: Sum
Reveal: stdout
```
```input
4
7
-2
900
6
```
```output
110
```

Four things worth noticing:

- `Shikigami: Ints` calls the **prelude**, a small standard library written in
  Domain itself. `Ints` is "split into lines, convert to integers". Also
  there: `Lines`, `Blocks`, `Digit Grid`, `Top K Sum`.
- **`=` is equality, always.** Domain has no assignment operator, so there is
  nothing for `==` to disambiguate from.
- `if … then … else …` is an expression with lazy arms, so
  `if length(xs) = 0 then -1 else first(xs)` is safe. `and` and `or`
  short-circuit.
- There are 176 builtins available inside lambdas — lists, math, text, bits,
  points, grids, sparse planes. The full table is in
  [expressions.md](expressions.md).

Lambda arity is fixed by whatever consumes it: one parameter for `Map Each`
and `Filter`, two for `Fold` (`(acc, x) -> …`), *k* for `Combinations k`.

### When a lambda cannot reach

The expression layer has no `map` or `filter` of its own. So once the value a
lambda binds is *itself a list*, some jobs have no expression spelling at all.

**Indent a pipeline where the lambda would go.** It runs in the lambda's
place, once per value, with that value as its current value:

```domain run
Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Maximum Technique: Sum
Reveal: stdout
```
```input
1 2 3
10 20
```
```output
[6, 30]
```

The body's result is the lambda's result. This works anywhere a
one-parameter `Using:` is taken, so `Filter` can test a row by reducing it and
`Sort By` can key on one.

## 6. Naming a value

Sometimes one value in flight is not enough — you need a number from the whole
list while looking at one element. There are two ways, and the difference is
how long the name lives.

**`Consider … Of`** names a value for one statement. It is computed from the
value entering that stage:

```domain run
Cursed Energy: stdin
Shikigami: Ints
Cursed Technique: Filter
    Consider biggest Of Max
    Using: (x) -> x = biggest
Reveal: stdout
```
```input
3
9
2
9
```
```output
[9, 9]
```

**`Cursed Object`** names a value for the rest of the program. That matters
when a loop computes something the stages after it need — a `Consider` would
go out of scope when the loop ends, so the count would have to ride out inside
the loop's own value:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(trim(t))
Cursed Object: laps As 0
Simple Domain: While
    Using: (v) -> v > 1
    Cursed Technique: Apply
        Using: (n) -> (n / 2) also laps := laps + 1
Cursed Technique: Apply
    Using: (v) -> laps
Reveal: stdout
```
```input
20
```
```output
4
```

`:=` writes to a name already in scope, and `also` runs a clause for its
effect and discards the result. `Cursed Tool:` is the statement form of the
same write. See [globals](language.md#cursed-object--globals) for the scope
rules and what a global costs the optimizer.

## 7. Parsing awkward input

Real input is rarely a column of integers. `Match Pattern` takes a template
with typed holes and gives back records:

```domain run
Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Match Pattern
    Using: "{name:text} has {n:int}"
    Mode: Each
Cursed Technique: Map Each
    Using: (r) -> r.name + ": " + totext(r.n * 2)
Reveal: stdout
```
```input
ana has 3
bo has 4
```
```output
[ana: 6, bo: 8]
```

A named hole becomes a record field; an unnamed one (`{int}`) becomes a
positional tuple. Holes can repeat, alternate between line shapes, and scan
for matches anywhere in a line — see [match-pattern.md](match-pattern.md).

## 8. Grids

Advent of Code lives on grids. `Digit Grid` and `Convert To Grid` build one
from lines; lambdas over cells do the rest:

```domain run
Cursed Energy: stdin
Shikigami: Digit Grid
Maximum Technique: Count Cells
    Using: (h) -> h >= 5
Reveal: stdout
```
```input
3743
2551
6532
```
```output
5
```

Positions are 0-based `(row, col)`. `Find Cells` returns matching positions as
**points** — `(Int, Int)` tuples that the point builtins (`prow`, `padd`,
`manhattan`, …) consume. Cell lambdas take a positional `(g, r, c)` form when
the body needs to look around with `at`, `row`, or `col`.

Graph searches over grids are one-liners, and each is a request the optimizer
may answer differently:

```domain ignore
Domain Expansion: BFS from 0 0          # step distances from (0,0)
    Using: (c) -> c = "."               # which cells are walkable
Domain Expansion: Dijkstra from 0 0     # weighted: cells are entry costs
Domain Expansion: Flood Fill from 0 0   # 0/1 mask of the connected region
Domain Expansion: Connected Components
    Using: (c) -> c = "#"               # count the 4-connected groups
```

### The infinite plane

Dense grids have edges. For point clouds, cellular automata, and anything that
plots, use **`Sparse<T>`** — an unbounded plane, negative coordinates
included, where every cell holds a default until written:

```domain run
Cursed Energy: stdin
Shikigami: Lines
Cursed Technique: Match Pattern
    Using: "{int},{int}"
    Mode: Each
Channeled Energy: Convert To Sparse Grid
    Default: "."
    Mark: "#"
Channeled Energy: Convert To Grid
Reveal: stdout
```
```input
0,0
1,2
2,1
```
```output
#..
..#
.#.
```

Three rules make it pleasant: `at(g, r, c)` is **total**, so unset cells read
the default and there are no bounds errors ever; only written cells are
stored, and `minrow`/`maxrow`/… track them exactly; and `Convert To Grid`
**densifies** the bounding box into a dense grid, which is how you print the
picture.

## 9. Structure

Four constructs shape a program once one straight pipeline stops being enough.

**Channels** branch the pipeline and `Combine` joins the branches back, for
when you need two analyses of one value:

```domain run
Cursed Energy: stdin
Shikigami: Ints

Channel "total":
    Maximum Technique: Sum

Channel "biggest":
    Maximum Technique: Max

Maximum Technique: Combine
    From: total, biggest
    Using: (t, b) -> t - b
Reveal: stdout
```
```input
3
9
2
```
```output
5
```

**Parts** answer both halves of a puzzle from one parse. Each branches from
the same value, and each labels its own output:

```domain run
Cursed Energy: stdin
Shikigami: Ints

Part "1":
    Maximum Technique: Sum
    Reveal: stdout

Part "2":
    Maximum Technique: Max
    Reveal: stdout
```
```input
3
9
2
```
```output
Part 1: 14
Part 2: 9
```

**Shikigami** are user-defined operations: a name for a composition of
primitives, with typed parameters, inlined at the call site so the optimizer
still sees through them:

```domain run
Shikigami "Scaled" (k: Int)
    Cursed Technique: Map Each
        Using: (n) -> n * k

Cursed Energy: stdin
Shikigami: Ints
Shikigami: Scaled
    k: 3
Reveal: stdout
```
```input
1
2
3
```
```output
[3, 6, 9]
```

**Loops** run a pipeline body over the current value, and come in four
drivers: `Repeat N` for a fixed count, `While` for a predicate,
`Iterate Until Fixed Point` for "until nothing changes", and `For x in …` to
walk a channel's list, binding each element as an extra lambda parameter.

```domain run
Cursed Energy: stdin
Shikigami: Ints
Simple Domain: Iterate Until Fixed Point
    Cursed Technique: Map Each
        Using: (n) -> if n > 0 then n - 1 else 0
Reveal: stdout
```
```input
3
1
```
```output
[0, 0]
```

A loop body must give back the type it was handed — that is what makes
"run it again" meaningful. When the thing you want to carry out of the loop is
*not* that type, that is what §6's `Cursed Object` is for.

**Binding Vows** are assertions that never change the answer — they only catch
the moment reality stops matching what you assumed:

```
3:1: vow violated [All Values > 0]: element 1 (-9) violates value > 0; actual value: [3, -9, 2] (in Binding Vow)
```

`--release` sheds them: skipped by `run`, compiled out entirely by `build`.

## 10. Compile it

Every program that runs also compiles to a standalone native binary, from the
same optimized IR, with no interpreter inside:

```sh
domain build prog.domain -o prog       # or: domain prog.domain -o prog
./prog < input.txt

domain build prog.domain --emit-go -   # inspect the generated Go
domain build prog.domain --run         # compile and run in one step
```

The generated Go is fully typed — no `[]any` — and lambdas are inlined into
the loops that consume them. The interpreter is the correctness oracle: the
test suite requires byte-identical stdout from both backends for every
example, challenge, and documentation program on this page. Expect roughly
seven times the interpreter's speed.

One behavioural difference to know: a compiled binary resolves
`Cursed Energy:` file paths against the *working directory*, like any CLI
tool, while the interpreter resolves them relative to the program file. Both
fall back to stdin when the file is missing.

## 11. Where to go next

Start with whichever matches how you like to learn.

**By reading whole programs**
- [walkthroughs.md](walkthroughs.md) — these features again, as complete
  working programs rather than fragments.
- [../examples/](../examples/README.md) — twenty-one small programs, one
  feature each, with inputs and expected outputs.
- [../challenges/](../challenges/README.md) — thirteen classics, FizzBuzz
  through Game of Life, for seeing idioms composed.

**By looking things up**
- [primitives.md](primitives.md) and [expressions.md](expressions.md) — the
  complete vocabulary, both layers.
- [language.md](language.md) — the precise rules for how source is structured.
- [aoc-toolbox.md](aoc-toolbox.md) — "how do I do the thing my AoC helper
  library does?", mapped item by item.

**By understanding the machine**
- [optimizer.md](optimizer.md) — every rewrite, and the safety rules each one
  obeys.
- [compiler.md](compiler.md) — what the generated Go looks like.
- [data-model.md](data-model.md) — the type model underneath it all.
- [tooling.md](tooling.md) — the REPL and the language server;
  [diagnostics.md](diagnostics.md) — the `expansion:` command family.
