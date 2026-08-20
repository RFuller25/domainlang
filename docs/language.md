# Language reference

## Source format

- **Foreign blocks are the one exception to everything below.** The body of
  `Domain Expansion: Python` (or `Go`/`rask`/`cRust`/`Weave`) is source in that
  language, captured verbatim: its own comment character, its own brackets,
  tabs if it wants them. See
  [primitives.md](ref-expansions.md#foreign-block--t---text-or-a-declared-in---out).
- **Significant indentation, spaces only.** A tab anywhere in indentation is a
  lex error. Indented lines form a block belonging to the statement above.
- **One pipeline statement per line**, `Keyword: operation phrase` — or just
  `operation phrase`: the themed keyword is optional and the compiler infers
  it (see [Optional keywords](#optional-keywords)).
- `#` starts a comment running to end of line.
- Double-quoted strings interpret the standard escapes `\n`, `\t`, `\\`, `\"`.
- Blank lines are insignificant.

## The two layers

1. **The pipeline layer** (themed). An ordered sequence of statements, each
   transforming a single implicit *current value*. Statements use the themed
   keywords below and are the only place side effects (input, output) happen.
2. **The expression layer** (plain, *not* themed). Inside `Using:` lambdas you
   write ordinary expressions over the current element(s):
   `(a, b) -> a + b = 2020`. Expressions are values, with two exceptions that
   are not: `:=` updates a name in scope and yields what it wrote, and `also`
   runs expressions after the body for their updates and discards their values.
   See [expressions.md](expressions.md).

The pipeline is statically typed **before** anything runs: resolution matches
each statement to a primitive, checks the incoming type, and computes the
outgoing type (lambda result types are inferred by the typechecker). A
mistyped pipeline fails with a positioned error, never mid-run.

## Keyword taxonomy

| Keyword | Semantic role |
|---|---|
| `Innate Domain:` | import a Shikigami library (keyword required) |
| `Cursed Energy:` | input / data source |
| `Cursed Technique:` | 1:1 transforms |
| `Channeled Energy:` | type coercions |
| `Maximum Technique:` | reductions / aggregation |
| `Domain Expansion:` | a **named algorithm the optimizer may swap** |
| `Reverse Cursed Technique:` | inversions |
| `Simple Domain:` | control flow (loops) |
| `Channel "name":` | a named sub-pipeline branching from the current value |
| an indented body under a `Using:`-taking stage | a sub-pipeline standing in for the lambda ([expressions.md](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)) |
| `Consider name As …` / `Consider name Of …` (keyword required) | a local binding for the stage's expressions ([expressions.md](expressions.md#stage-bindings--consider--as--consider--of)) |
| `Part "label":` | a labelled output block branching from the current value |
| `Cursed Object:` / `Cursed Tool:` | declare a global / change one ([below](#cursed-object--globals)) |
| `Shikigami "name" (params) : In -> Out` / `Shikigami: Name` | user-defined operation definition / call |
| `Binding Vow:` | debug-time assertion over the current value |
| `Reveal:` | terminal output sink |

The full per-primitive reference is [primitives.md](primitives.md).

## Optional keywords

Every keyword above is optional. A line written as a bare operation phrase is
resolved to the same statement — the compiler recovers the keyword from the
phrase — so these two programs are identical, down to the optimizer rewrite
they trigger:

```domain ignore
Cursed Energy: input.txt                    input.txt
Cursed Technique: Split Text by "\n\n"      Split Text by "\n\n"
Channeled Energy: Convert To Integers       Convert To Integers
Domain Expansion: Quicksort, Descending     Quicksort, Descending
Maximum Technique: Select Top 3, Sum        Select Top 3, Sum
Reveal: stdout                              stdout
```

The two spellings mix freely, line by line — write the keyword where it reads
better and leave it out where it is noise. Inference runs before resolution,
so everything downstream (type checking, the optimizer, the linter, `--explain`)
sees one fully-keyworded program either way.

The keyword is recovered from the phrase in this order:

1. **a Shikigami name** — a call is just the name (`Top K Sum`)
2. **a `From:` consumer** — the statement draws on channels (`Combine`)
3. **a loop kind** — `Repeat N`, `While`, `Iterate Until Fixed Point`. The
   primitives that borrow these words are excluded here so this step cannot
   swallow them: `Take While` / `Drop While` are prefix transforms, and
   `Iterate n` is the generator (the loop always spells out `Until Fixed
   Point`).
4. **a vow** — `Count Equals N`, `All Values <cmp> N`
5. **the sink** — `stdout`
6. **a source**, on the first line only — `stdin`, or a path (`input.txt`,
   `data/day1.txt`). Path-shaped phrases are read as sources before the
   vocabulary is consulted, so a file called `sum.txt` is still a file.
7. **a primitive** — every primitive whose matcher accepts the phrase
8. **a source**, on the first line only — anything left over is the input

Two rules keep this honest:

- **Ambiguity is an error, never a guess.** Primitives registered under one
  keyword are ordered specific-first (`Split Each` before `Split`), so the
  most specific wins. A phrase matching primitives under *different* keywords
  cannot be settled that way, and Domain says so and asks for the keyword
  rather than picking one.
- **A Shikigami may not be named after a built-in** (below).

A phrase that names nothing at all is an error with a suggestion:

```
error[name]: cannot infer a keyword for "Splt Each by": no operation matches this phrase
  help: did you mean "Split Each" (Cursed Technique)?
```

Two places need their keyword written. A forgotten colon after one you *did*
write — `Reveal stdout` — stays a syntax error (with an auto-fix) rather than
being re-read as a phrase, because it names no operation and the mistake is
worth pointing at. And `Innate Domain:` is always spelled out: a bare library
name would be indistinguishable from a source target or an unknown operation
(see [Innate Domain](#innate-domain--importing-a-library)).

## Statements and arguments

A statement is a keyword, an operation phrase, and optionally an indented
block of **named arguments**:

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{a:int}-{b:int},{c:int}-{d:int}"
```

Argument values may be a string (`Using: "..."`), an integer (`k: 3`, `Seed:
0`), a bare identifier (`Mode: First`), an identifier list (`From: moves,
rows`), or a lambda (`Using: (a, b) -> a + b`). Which arguments a primitive
accepts is listed in [primitives.md](primitives.md); common ones:

- `Using:` — a lambda or a Match Pattern template string.
- `Mode:` — a variant selector (`One`/`Each` for Match Pattern;
  `Filter`/`Count`/`First`/`Map` for All Pairs/Combinations).
- `Seed:` — the fold accumulator's initial value (an Int or Text literal, or
  a measured lambda, which is how the accumulator becomes a composite).
- `From:` — the channels a consumer reads (Combine, Difference, Fold, Zip).
- `Size:`, `Step:`, `Count:`, `Index:`, `Times:`, `Low:`, `High:`, `By:`,
  `With:`, `Fill:`, `Default:`, `Mark:`, `Seed:`, `Row:`, `Col:`, `Height:`,
  `Width:`, `Thickness:` — a **measured argument**: the literal
  a phrase or an argument takes, written instead as a lambda over the current
  value (below).

An argument no primitive on that line reads is silently ignored, so the
linter reports it (see [diagnostics.md](diagnostics.md#the-linter)).

### Local bindings

The same block may hold `Consider NAME As …` and `Consider NAME Of …` lines,
which name a value the stage's expressions can use instead of repeating it:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Consider mean Of (xs) -> sum(xs) / length(xs)
    Using: (x) -> x > mean
Reveal: stdout
```
```input
1
2
3
10
```
```output
[10]
```

They are their own line kind rather than arguments, because an argument's name
is vocabulary (`Mode:`, `Using:`) and a binding's name is yours — keeping them
apart is what lets a misspelled `Usng:` still be reported as a misspelled
argument instead of quietly becoming a local nobody reads.

`As` binds an expression or a function and never sees the pipeline value; `Of`
binds the result of applying an operation, a lambda, or a whole sub-pipeline to
it. A binding is in scope for every lambda on its statement and for the
statements nested under it, and those lambdas may
[update it with `:=`](expressions.md#updating-a-local--) — the one value in
Domain that carries from one element to the next without being threaded through
a `Fold`. The full rules are in
[expressions.md](expressions.md#stage-bindings--consider--as--consider--of).

An argument's value may run past its line. A newline inside a parenthesis is
whitespace, and lines indented under the argument continue it — which is how a
lambda body long enough to think about gets written down the page instead of
across it:

```domain
Cursed Technique: Apply
    Using: (v) ->
        consider d as abs(v - 10)
        in if d > 3
            then d * 2
            else d
```

Neither form changes anything but the line breaks; see
[expressions.md](expressions.md#writing-an-expression-across-lines).

`As` never sees the pipeline value, so it is where a constant or a helper
function goes:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider double As (n) -> n * 2
    Using: (x) -> double(x) + 1
Reveal: stdout
```
```input
1,2,3
```
```output
[3, 5, 7]
```

### Measured arguments

An argument that a phrase takes as a literal — `Window 3`, `Chunk 4`,
`Select Top 3`, `Split Text by ","` — may instead be given as a named argument
holding a lambda over the **current value**, so it can depend on the data
rather than on the program text.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Chunk
    Size: (xs) -> length(xs) / 2
Reveal: stdout
```
```input
1,2,3,4,5,6
```
```output
[[1, 2, 3], [4, 5, 6]]
```

The measured form and the literal form mean the same thing when the lambda is
constant, so reaching for it costs nothing:

```domain run
Cursed Energy: stdin
Cursed Technique: Split
    By: (t) -> if indexof(t, ";") >= 0 then ";" else ","
Maximum Technique: Count
Reveal: stdout
```
```input
a;b;c
```
```output
3
```

<!--MEASURED-->
rather than on the source:

```domain
Cursed Technique: Window
    Size: (xs) -> length(xs) / 2     # windows half the current list long
```

The lambda binds the whole current value (the binding `Apply` gives its
lambda), plus one trailing parameter per enclosing `For` loop, and must
return the slot's own type (`Int` for a count, `Text` for a separator, the
cell type for a fill). It runs once per execution of the statement, before the
primitive does. [primitives.md](ref-transforms.md#measured-arguments) has the
full rules and the list of primitives that take one.

An argument is measurable when it carries **data**. Three kinds stay literal,
and the reasons are rules rather than exceptions:

1. **It types the program.** `Combinations k` fixes the `Using:` lambda's
   arity; a `Match Pattern` template fixes the output `Record`'s fields; a
   `Mode:` picks the result's shape. Types are settled before the program
   runs.
2. **It is consumed before there is a current value.** `Cursed Energy:`'s
   source and `Innate Domain:`'s target are read at resolve time; there is
   nothing yet to measure against.
3. **It names a program element, not a value.** `From:` names channels;
   `Channel`, `Part` and `Shikigami` name declarations. A name is resolved
   against the program, not against data.

## Channels — multi-section inputs

A `Channel "name":` statement runs an indented sub-pipeline **on the current
value** and stores its result under the name; the main pipeline's current
value is unchanged, so sibling channels all branch from the same upstream
value. Channels cannot nest. A downstream statement with a `From:` argument
consumes them:

```domain
Cursed Technique: Split Text by "\n\n"

Channel "moves":
    Cursed Technique: Take Item 1
    ...
    Maximum Technique: Sum

Channel "rows":
    Cursed Technique: Take Item 0
    ...
    Maximum Technique: Count

Maximum Technique: Combine
    From: moves, rows
    Using: (moves, rows) -> moves + rows
```

**A Channel body may consume channels declared above it** with `From:`, so a
value derived from two channels can itself be named instead of being
recomputed at every consumer:

```domain
Channel "labelled":
    Maximum Technique: Zip
        From: firsts, lengths
    Cursed Technique: Map Each
        Using: (p) -> item(p, 0) + totext(item(p, 1))
```

Declaration order gives the dependency graph for free and no cycle check is
needed: a channel enters the environment only once its own body has resolved,
so a self- or forward-reference is already an unknown-channel error. Channels
still cannot *nest*, and loop and Shikigami bodies still refuse `From:`
consumers.

`Combine` binds channel values to the lambda's parameters in `From:` order.
`Difference` (exactly two channels, Set-or-List of keyable elements) emits the
set difference `a - b`. `Fold` with `From:` (one channel holding a List)
folds over the channel's list with the **current pipeline value as the
seed**, and `Zip` pairs two channel lists element-wise — see
[primitives.md](primitives.md). These consumers are the only place the
otherwise linear pipeline forms a graph.

Two channels branching from one upstream value, rejoined by `Combine`:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n\n"

Channel "rules":
    Cursed Technique: Take Item 0
    Cursed Technique: Split Text by "\n"
    Maximum Technique: Count

Channel "pages":
    Cursed Technique: Take Item 1
    Cursed Technique: Split Text by "\n"
    Maximum Technique: Count

Maximum Technique: Combine
    From: rules, pages
    Using: (r, p) -> r * 100 + p
Reveal: stdout
```
```input
a
b
c

x
y
```
```output
302
```

A channel body may consume channels declared above it, so a value derived from
two of them can be named once instead of recomputed at every consumer:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"

Channel "nums":
    Channeled Energy: Convert To Integers

Channel "total":
    Maximum Technique: Combine
        From: nums
        Using: (ns) -> sum(ns)

Maximum Technique: Combine
    From: nums, total
    Using: (ns, t) -> t / length(ns)
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

## Part — two answers from one parse

Every Advent of Code day asks two questions about the same input. A
`Part "label":` statement runs an indented sub-pipeline **on the current
value** and labels what that body prints — so the parse above the Parts
happens once:

```domain run
Cursed Energy: input.txt
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

A Part is a **passthrough**, exactly like a [Channel](#channels--multi-section-inputs):
the main pipeline's current value is unchanged, so sibling Parts all branch
from the same upstream value (Part 1 sorting cannot disturb what Part 2 sees)
and a top-level `Reveal` after the Parts still prints the upstream value.

That passthrough is worth seeing directly — the sort inside Part 1 leaves
nothing behind for Part 2, or for the `Reveal` after both:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers

Part "sorted":
    Domain Expansion: Sort
    Reveal: stdout

Part "first":
    Cursed Technique: Take Item 0
    Reveal: stdout

Reveal: stdout
```
```input
3,1,2
```
```output
Part sorted: [1, 2, 3]
Part first: 3
[3, 1, 2]
```

**Output is explicit.** A Part prints only what its body `Reveal`s — `Reveal`
stays the single output sink, and there is no implicit-print rule to
remember. A Part whose body never reveals computes nothing observable, so the
linter warns about it. The label prefixes that output, and a multi-line value
starts on the line *after* its label so a grid or a sparse picture stays
aligned:

```
Part picture:
###..
..###
```

The label is free text, not just a number (`Part "totals":` is fine), and two
Parts with the same label is a lint warning. Parts are top-level only: a Part
inside a Channel, a loop, a Shikigami, or another Part is an error. A Part
body **may** consume channels defined above it with `From:` — that is the
point, since one parse can feed both answers — but may not define channels of
its own, which cannot nest.

A Part body is a sub-pipeline, so the optimizer treats it like a Channel or
loop body: passes that rewrite a node in place (expression simplification,
algorithm substitution) fire inside it, while passes that change the length of
a node list run only at the top level (see
[optimizer.md](optimizer.md#the-safety-rules-every-pass-obeys)).

## Shikigami — user-defined operations

A Shikigami names a composition of primitives, with typed parameters
substituted into its body:

```domain
Shikigami "Top K Sum" (k: Int) : List<Int> -> Int
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top k, Sum
```

Calls use the block form; parameters are passed as named arguments, and the
`Shikigami:` keyword is optional like any other:

A definition and its call, as a whole program — the definition is a
declaration, so it may sit above or below the pipeline that uses it:

```domain run
Shikigami "Top K Sum" (k: Int) : List<Int> -> Int
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top k, Sum

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Shikigami: Top K Sum
    k: 2
Reveal: stdout
```
```input
5,1,9,3
```
```output
14
```

One definition may be called more than once with different arguments, which
is the whole reason to name a composition rather than repeat it:

```domain run
Shikigami "Top K Sum" (k: Int) : List<Int> -> Int
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top k, Sum

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers

Part "one":
    Shikigami: Top K Sum
        k: 1
    Reveal: stdout

Part "three":
    Shikigami: Top K Sum
        k: 3
    Reveal: stdout
```
```input
5,1,9,3
```
```output
Part one: 9
Part three: 17
```

```domain ignore
Shikigami: Top K Sum        Top K Sum
    k: 3                        k: 3
```

### Parameters

A parameter is a value **written at the call site**, so it is a scalar or a
lambda:

| Declared | Argument at the call | Notes |
|---|---|---|
| `k: Int` | `k: 3` | substituted into the phrase (`Select Top k`) *and* into lambda bodies |
| `s: Text` | `s: "note"` | same |
| `f: Float` | `f: 1.5` | lambda bodies only; an Int argument widens |
| `b: Bool` | `b: true` | lambda bodies only; written as bare `true`/`false` |
| `p: (Int) -> Bool` | `p: (x) -> x > 100` | a **lambda parameter** — see below |

A lambda parameter is also how a **measured argument** reaches a Shikigami:
declare it as the function the slot takes and hand it to that slot.

```domain
Shikigami "Sized Windows" (size: (List<Int>) -> Int)
    Cursed Technique: Window
        Size: size
```

Declaring it `size: Int` and passing a lambda is rejected, and the error says
this: a scalar parameter substitutes into the body as a *literal* — into lambda
bodies included — which is exactly what a function has no form for. (A body may
of course just measure directly, with no parameter at all.)

A scalar parameter substitutes into the phrase and into lambda bodies alike:

```domain run
Shikigami "Over" (limit: Int) : List<Int> -> Int
    Cursed Technique: Filter
        Using: (x) -> x > limit
    Maximum Technique: Count

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Shikigami: Over
    limit: 4
Reveal: stdout
```
```input
1,5,2,9
```
```output
2
```

A lambda parameter is the other kind, and it is how a predicate is handed in
from the call site:

```domain run
Shikigami "Count Where" (p: (Int) -> Bool) : List<Int> -> Int
    Maximum Technique: Count Matching
        Using: p

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Shikigami: Count Where
    p: (x) -> mod(x, 2) = 0
Reveal: stdout
```
```input
1,2,3,4
```
```output
2
```

Composite values (`Grid<Int>`, `List<Text>`, …) are not parameters: Domain has
no composite literals, so there would be no way to write one at a call. Those
arrive through the pipeline instead.

**Lambda parameters make a Shikigami higher-order** — the caller supplies the
operation:

```domain
Shikigami "Count Where" (p: (Int) -> Bool) : List<Int> -> Int
    Maximum Technique: Count Matching
        Using: p

Count Where
    p: (x) -> x > 100
```

The lambda is checked against its declared type at the call, so an arity or
result-type mismatch is reported there rather than surfacing from inside the
body. A lambda parameter may be used at more than one site in a body; each use
is typed independently.

### Declared signatures

`: In -> Out` after the parameter list states the Shikigami's pipeline type.
It is **optional**, and when written it is checked at both ends:

- the **input** against the pipeline's current type **at each call site** —
  which is the point. `Shikigami "Top K Sum" expects input of type List<Int>,
  but the pipeline produced Text` names the boundary that is actually wrong,
  with the usual "insert `Convert To Integers`" advice attached, instead of an
  inlining trace pointing somewhere inside the body;
- the **output** against what the body actually produces, so a body that
  drifts from its stated type is caught.

A signature is a **check, not a compilation boundary**. The body is still
inlined, so optimizer rewrites still fire straight through — a signed
Shikigami wrapping `Quicksort` + `Select Top K` still becomes a quickselect.

Types are written as `Int`, `Float`, `Text`, `Bool`, `List<T>`, `Set<T>`,
`Grid<T>`, `Sparse<T>`, `Map<K,V>`, `(A, B)` tuples, and `{a: Int, b: Text}`
records — the last written exactly as `Reveal` and every type error print one,
so a declared signature and the type it is checked against read identically.
Field order is not part of a record's identity: `{b: Text, a: Int}` matches a
pipeline producing `{a: Int, b: Text}`.

One limit worth knowing:

- **No type variables.** A signature is necessarily monomorphic, so a
  genuinely polymorphic Shikigami (`List<T> -> T`) simply declares none and
  behaves exactly as it always did. That is why signatures are opt-in.

In a signature the top-level `->` always separates input from output, so
`: (Int, Int) -> Int` takes a **tuple** and returns an `Int`.

A signature is checked at both ends, and inlining still happens through it —
the pair below is still fused into a quickselect:

```domain run
Shikigami "Top Two" : List<Int> -> Int
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top 2, Sum

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Shikigami: Top Two
Reveal: stdout
```
```input
5,1,9,3
```
```output
14
```

Leaving it off is the way to write a genuinely polymorphic operation, since
there are no type variables to declare one with:

```domain run
Shikigami "Second"
    Cursed Technique: Take Item 1

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Shikigami: Second
Reveal: stdout
```
```input
a,b,c
```
```output
b
```

The prelude declares signatures for all five of its definitions, which is
where to look for worked examples.

### Naming: a Shikigami may not be named after a built-in

Because a call site is just the name, the name is also the whole statement —
so a Shikigami called `Sum` would leave no way to write the built-in `Sum`.
These names are refused:

- a primitive: its catalog ID (`Sum`, `Convert To Grid`, `Select Top K`) or
  any phrase that spells it (`Quicksort`, `Sort By`)
- a themed keyword (`Cursed Technique`, `Reveal`)
- an [expression-layer builtin](expressions.md) (`length`, `gcd`, `sum`)
- a loop kind (`Repeat`), a vow shape (`All Values`), `stdout`, or `stdin`

Only names that *are* a built-in are reserved, not every name that mentions
one: `Scaled Sum`, `Sort Text` and `Top K Sum` are all fine, because the words
they add change what the phrase means. The check runs on every definition, so
a name that would collide is a resolve error at the definition, not a
surprise at the call.

Shikigami are **inlined** during resolution, so optimizer rewrites fire
through them (the call above still fuses into a quickselect), and the
compiler backend sees only primitives.

### Recursion is refused, and named

Because a Shikigami is inlined at its call site, a self-referential one has no
finite expansion. Domain detects that as a **cycle** rather than capping the
depth: a name may not appear twice in the chain currently being inlined, and
the error names the chain.

```
error: Shikigami "Ping" is recursive (Ping -> Pong -> Ping): a Shikigami is
inlined at its call site, so it has no finite expansion — use
`Domain Expansion: Explore` for a search over states
```

Non-recursive composition is therefore **unbounded** — a deeply nested but
finite chain is legal however deep it goes, where the old fixed ceiling of 64
refused it for no reason.

For the problems that look like they need recursion — reachability,
fewest-moves, counting configurations — reach for
[`Domain Expansion: Explore`](primitives.md), which searches a state space
iteratively and terminates on its visited set.

### Innate Domain — importing a library

`Innate Domain: <library>` loads a file of Shikigami definitions before the
program, so its operations are callable by name exactly like the prelude's:

```domain ignore
Innate Domain: aoc
Innate Domain: grids/hex

Cursed Energy: input.txt
Shikigami: Lines
Shikigami: All Ints          # defined in aoc.domain
```

The target is written **without** the `.domain` extension, the way a
`Cursed Energy:` path is written bare. `Innate Domain` is one of the few
keywords that is *not* optional — a bare `aoc` line is a source or an unknown
operation, never an import.

**A library holds Shikigami definitions and nothing else.** A pipeline
statement in an imported file is an error naming the offending line: a library
is not a program. Libraries may themselves import.

**Imports are declarations, not steps.** Like Shikigami definitions they are
hoisted, so their position in the file does not matter; by convention they go
at the top.

**Where libraries are looked for**, first hit wins:

1. the importing file's own directory — so a library beside a program just
   works, and the pair stays relocatable;
2. each entry of `$DOMAIN_PATH` (colon-separated);
3. `~/.config/domain/lib`.

A target with a separator (`grids/hex`) is resolved the same way, relative to
each root in turn.

A library beside the program, and the program calling into it:

```domain run
Innate Domain: aoc

Cursed Energy: stdin
Shikigami: Total Lengths
Reveal: stdout
```
```lib aoc.domain
Shikigami "Total Lengths"
    Cursed Technique: Split Text by "\n"
    Maximum Technique: Sum By
        Using: (line) -> length(line)
```
```input
ab
cde
```
```output
5
```

Imports are declarations rather than steps, so they are hoisted and their
position does not matter — and a library may import another:

```domain run
Cursed Energy: stdin
Shikigami: Doubled
Reveal: stdout

Innate Domain: helpers
```
```lib helpers.domain
Shikigami "Doubled"
    Cursed Technique: Split Text by ","
    Channeled Energy: Convert To Integers
    Cursed Technique: Map Each
        Using: (n) -> n * 2
```
```input
1,2,3
```
```output
[2, 4, 6]
```

each candidate directory. A miss lists every directory searched.

**Shadowing** runs weakest to strongest — prelude, then imports in import
order, then the importing file's own definitions:

```
prelude  <  imports  <  the program's own Shikigami
```

A library shadows what it itself imported, so the file you name directly wins
over its transitive dependencies. The import graph is deduped by real file
path, so a diamond loads once and a cycle is an error naming the chain. The
[reserved-name rule](#naming-a-shikigami-may-not-be-named-after-a-built-in)
applies to imported definitions too, reported at the `Innate Domain` line that
pulled the library in and naming the library and the position inside it.

**Libraries cost nothing at runtime.** A Shikigami is inlined at its call
site, so an imported operation gets every optimizer rewrite a local one would
— an imported `Quicksort` + `Select Top K` still fuses into a quickselect.
Imports are resolved at *build* time: a compiled binary contains the inlined
bodies and never looks for the library again.

### The prelude

The standard library is itself written in Domain and loaded before every
program:

Every prelude definition declares its signature, which is also the feature's
dogfood case — the standard library gets the same check a user's Shikigami does.

| Shikigami | Expands to | Declared type |
|---|---|---|
| `Lines` | Split by `"\n"` | `Text -> List<Text>` |
| `Blocks` | Split by `"\n\n"`, Split Each by `"\n"` | `Text -> List<List<Text>>` |
| `Ints` | Split by `"\n"`, Convert To Integers | `Text -> List<Int>` |
| `Digit Grid` | Split lines, split chars, convert, To Grid | `Text -> Grid<Int>` |
| `Top K Sum` (k: Int) | Quicksort Descending, Select Top k, Sum | `List<Int> -> Int` |

User definitions with the same name shadow the prelude.

The prelude's operations are callable with no import at all:

```domain run
Cursed Energy: stdin
Shikigami: Ints
Maximum Technique: Sum
Reveal: stdout
```
```input
1
2
3
```
```output
6
```

`Blocks` is the paragraph parser, and `Top K Sum` takes the parameter its
declaration names:

```domain run
Cursed Energy: stdin
Shikigami: Blocks
Channeled Energy: Convert Each List to Integers
Maximum Technique: Sum Each Group
Shikigami: Top K Sum
    k: 2
Reveal: stdout
```
```input
1
2

10

100
```
```output
110
```

## Simple Domain — loops

The body is an indented sub-pipeline that must **preserve the value type**
(its output type equals its input type). Three kinds:

```domain ignore
Simple Domain: Repeat 3
    <body>

Simple Domain: While
    Using: (v) -> v > 1
    <body>

Simple Domain: Iterate Until Fixed Point
    <body>
```

- `Repeat N` runs the body N times.
- `While` re-evaluates the predicate on the current value before each
  iteration; the body only runs while it is true.
- `Iterate Until Fixed Point` runs the body until the value stops changing
  (structural equality).

`While` and `Iterate Until Fixed Point` stop after **1,000,000,000
iterations**, and `Unfold` after a billion elements. Domain used to cap them at
1,000,000, which turned a legitimate long-running simulation into a spurious
failure; a limit must never be the reason a correct program cannot run, and a
40,000,000-step generator is an ordinary puzzle, not an abuse. A billion is
past anything that finishes while you wait, so a loop that reaches it is not
slow but stuck, and saying so beats spinning until interrupted. Both backends
use the same number — a program that runs interpreted runs compiled.

The ceilings on `Permutations`, `Subsets`, and sparse densification were
removed outright rather than raised, and stay that way: those are bounded by
their input, so a ceiling there refuses a correct program without catching a
runaway one.

**For** loops iterate a named list, binding each element as an ambient
extra parameter on every `Using:` lambda in the body:

```domain
Channel "deltas":
    Cursed Technique: Apply
        Using: (xs) -> xs

Simple Domain: For x in deltas
    Cursed Technique: Filter
        Using: (v, x) -> v > x
```

A loop body may also **consume a Channel** with `From:`. A channel is fully
computed before the loop starts and its value never changes, so there is no
ordering hazard — and without it a simulation has to smuggle its read-only
environment through the loop state, which (because a body must preserve its
value type) it then carries for every lap. The shape that benefits is
`Fold From:`, which folds a channel's list into the state each lap; `Combine`
and friends replace the value outright and so rarely satisfy a loop's type
rule.

A **Shikigami** and a **`Using:` body** still refuse one, and the reason is
structural rather than conservative: a Shikigami is inlined at call sites that
need not share a scope, and a body compiles to a top-level function where a
channel's local is not in scope. Either would be a promise the compiler could
not keep.

`<source>` is a channel name (declared via `Channel "name": ...`, holding a
`List<T>`) or an inline `range(N)` (`N` an Int literal, yielding `0..N-1`).
Nested `For` loops each add their own parameter, outermost first:
`Using: (v, a, b) -> ...` for `For a in as: For b in bs: ...`. A leading
parameter that happens to share the ambient name shadows it.

`For` compiles like every other loop kind — the earlier interpreter-only
restriction is gone, so there is no advertised gap between the backends.

`Repeat N` runs the body a fixed number of times, and the body must give back
the type it was given:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Simple Domain: Repeat 3
    Cursed Technique: Apply
        Using: (n) -> n * 2
Reveal: stdout
```
```input
1
```
```output
8
```

`While` re-tests the current value before each lap, and `Iterate Until Fixed
Point` runs until the value stops changing:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
Simple Domain: While
    Using: (v) -> v > 1
    Cursed Technique: Apply
        Using: (n) -> n / 2
Reveal: stdout
```
```input
20
```
```output
1
```

## Cursed Object — globals

`Consider` names a value for one statement. **`Cursed Object`** names one for
the rest of the program, and **`Cursed Tool`** changes it:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Object: bump As 10
Cursed Technique: Map Each
    Using: (x) -> x + bump
Reveal: stdout
```
```input
1,2,3
```
```output
[11, 12, 13]
```

The difference from a `Consider` is scope, and it is the whole point. A
binding's writes already survive from one element to the next and across a
loop's laps — but the *name* dies with the statement, so a loop that counts
something has to smuggle the count out through the loop's own value. Because a
loop body must preserve its value type, that means every lap carries a tuple
built out of scope rather than out of shape. A global does not:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> toint(t)
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

### The two prepositions

`As` and `Of` mean exactly what they mean on a `Consider`, and for the same
reason: a 1-parameter lambda already means two different things in Domain
depending on the slot it is written in, and a declaration has no slot to
disambiguate it.

| Written | Binds | Computed |
|---|---|---|
| `Cursed Object: n As 0` | a value | where the line is written |
| `Cursed Object: n As k + 1` | an expression over globals already declared | where the line is written |
| `Cursed Object: n Of (xs) -> length(xs)` | a value from the pipeline value arriving here | where the line is written |
| `Cursed Object: n Of Sum` | the same, written as an operation | where the line is written |
| `Cursed Object: n Of` + an indented pipeline | the same, written as a sub-pipeline | where the line is written |
| `Cursed Object: n Of Itself` | the value arriving here, unchanged | where the line is written |

**`As` never sees the pipeline value; `Of` always does.** A global may not be a
function — Domain has no function values to hold one — so `Cursed Object: f As
(x) -> …` is refused and points at `Consider`, which is inlined at its call
sites.

Several declarations can share one keyword, and each sees the ones above it:

```domain
Cursed Object:
    a As 2
    b As a * 5
    c As a + b
```

### Scope

A global is in scope **from its own line to the end of the program**, and it
outlives whatever block it was written in — which is what a `Consider` cannot
do. Reading one above its declaration is an error that says so rather than
leaving "unknown identifier" to stand on its own.

A name nearer wins, exactly as everywhere else: a lambda parameter, a
`consider` local, or a `Consider … As/Of` stage binding of the same spelling
shadows the global for its extent.

A global may not take a name the language already means — a primitive, a themed
keyword, a loop kind, a vow predicate, a `Reveal` sink, an input source, an
expression builtin, or a Channel. Its type is fixed by its declaration, and
`Cursed Tool` and `:=` are checked against it; widen at the declaration.

### A declaration is a statement, so it re-runs

`Cursed Object` inside a loop body re-declares its global every lap, which
means a counter written that way never counts. `Cursed Tool` is what the
accumulating case is spelled with, and the linter warns about the trap:

```domain ignore
Simple Domain: Repeat 10
    Cursed Object: n As 0        # 0 again every lap — almost certainly a mistake
    Cursed Tool:   n As n + 1    # what was meant, with n declared above the loop
```

A declaration written `Of` something is exempt: it reads the value arriving on
that lap, so re-running it is the point.

### Where globals cannot reach

Three boundaries, each protecting a guarantee the language already makes.

- **`Part` blocks are isolated.** Sibling Parts branch from the same upstream
  value, and "Part 1 sorting cannot disturb what Part 2 sees" covers the
  globals a Part can reach as well as the value it was handed. A Part's writes
  are discarded when it ends.
- **`Channel` bodies are sealed both ways.** A channel is computed once, before
  whatever consumes it; a body that read or wrote a global would make the order
  it ran in observable, which is the hazard channels exist to avoid.
- **A Shikigami from the prelude or an `Innate Domain` import is sealed.** Its
  author never saw this program's names. A Shikigami defined in your own file
  is not: it is inlined at its call sites and reads and writes globals like any
  other stage.

### What it costs

A global read is resolved to a slot index while the program is lowered, so it
costs a bounds-checked load in the interpreter and a package-level variable in
a compiled binary — cheaper than a `Consider` binding, which is seeded by name
into the environment every lambda application builds.

The cost that is real is to the optimizer. A stage reading a global that
**something writes after it is declared** is no longer a pure function of its
input, so it keeps its lambda exactly as written: no algorithm substitution, no
fusion, no expression simplification (see [optimizer.md](optimizer.md)). A
global nothing writes is a constant of the run and costs its readers nothing —
`Cursed Object: target As 2020` leaves every rewrite intact. `domain expansion:
lint` names each stage that paid.

## Binding Vows — debug assertions

A vow is a predicate over the current value. It **never changes the value**;
on violation it aborts with the vow, the stage, and the offending value.

```domain
Binding Vow: Count Equals 200      # the current List has exactly 200 items
Binding Vow: All Values > 0        # every Int in the current List<Int> passes

Binding Vow: Holds                 # any predicate, over any type
    Using: (g) -> rows(g) = cols(g)
```

Supported comparisons in `All Values`: `>` `>=` `<` `<=` `=`.

`Holds` is the general form. The two literal shapes are both about `List<Int>`
and both bounded by an integer literal, so a vow could not otherwise reach a
Grid, Map, Record or Sparse — while the expression layer can say anything.

Vows are debug-time: `--release` sheds them (`domain run --release` skips
them; `domain build --release` compiles them out of the binary entirely).

A vow that holds is invisible — it passes the value through untouched, which
is what lets one sit in the middle of a working pipeline:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Binding Vow: All Values > 0
Maximum Technique: Sum
Reveal: stdout
```
```input
1,2,3
```
```output
6
```

`Holds` is the general form, and reaches the types the two literal shapes
cannot:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Binding Vow: Holds
    Using: (g) -> rows(g) = cols(g)
Maximum Technique: Count Cells
    Using: (c) -> c = "#"
Reveal: stdout
```
```input
#.
.#
```
```output
2
```

## Reveal — output

`Reveal: stdout` prints the current value and ends the useful pipeline.
`Reveal: stderr` prints to standard error instead — a mid-pipeline Reveal that
does not disturb the program's answer, so a golden test still passes with one
in place. Every
type has a deterministic rendering (insertion order for Maps/Sets, row-major
for Grids, sorted set-cell listings for Sparse grids); see
[data-model.md](data-model.md).

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
```
```input
1,2,3
```
```output
6
```

`Reveal: stderr` prints without disturbing the program's answer, so a golden
test still passes with one left in — stdout below carries only the total:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Reveal: stderr
Maximum Technique: Sum
Reveal: stdout
```
```input
1,2,3
```
```output
6
```
