# Language reference

## Source format

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
   `(a, b) -> a + b = 2020`. See [expressions.md](expressions.md).

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
| `Part "label":` | a labelled output block branching from the current value |
| `Shikigami "name" (params) : In -> Out` / `Shikigami: Name` | user-defined operation definition / call |
| `Binding Vow:` | debug-time assertion over the current value |
| `Reveal:` | terminal output sink |

The full per-primitive reference is [primitives.md](primitives.md).

## Optional keywords

Every keyword above is optional. A line written as a bare operation phrase is
resolved to the same statement — the compiler recovers the keyword from the
phrase — so these two programs are identical, down to the optimizer rewrite
they trigger:

```domain
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

### Measured arguments

An argument that a phrase takes as a literal — `Window 3`, `Chunk 4`,
`Select Top 3`, `Split Text by ","` — may instead be given as a named argument
holding a lambda over the **current value**, so it can depend on the data
rather than on the source:

```domain
Cursed Technique: Window
    Size: (xs) -> length(xs) / 2     # windows half the current list long
```

The lambda binds the whole current value (the binding `Apply` gives its
lambda), plus one trailing parameter per enclosing `For` loop, and must
return the slot's own type (`Int` for a count, `Text` for a separator, the
cell type for a fill). It runs once per execution of the statement, before the
primitive does. [primitives.md](primitives.md#measured-arguments) has the
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

## Part — two answers from one parse

Every Advent of Code day asks two questions about the same input. A
`Part "label":` statement runs an indented sub-pipeline **on the current
value** and labels what that body prints — so the parse above the Parts
happens once:

```domain
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

```
Part 1: 24000
Part 2: 45000
```

A Part is a **passthrough**, exactly like a [Channel](#channels--multi-section-inputs):
the main pipeline's current value is unchanged, so sibling Parts all branch
from the same upstream value (Part 1 sorting cannot disturb what Part 2 sees)
and a top-level `Reveal` after the Parts still prints the upstream value.

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

```domain
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
`Grid<T>`, `Sparse<T>`, `Map<K,V>`, and `(A, B)` tuples. Two limits worth
knowing:

- **No type variables.** A signature is necessarily monomorphic, so a
  genuinely polymorphic Shikigami (`List<T> -> T`) simply declares none and
  behaves exactly as it always did. That is why signatures are opt-in.
- **Record types cannot be written** (`{a:Int, b:Int}`), so a Shikigami
  operating on `Match Pattern` records leaves its signature off.

In a signature the top-level `->` always separates input from output, so
`: (Int, Int) -> Int` takes a **tuple** and returns an `Int`.

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

```domain
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

## Simple Domain — loops

The body is an indented sub-pipeline that must **preserve the value type**
(its output type equals its input type). Three kinds:

```domain
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

`While` and `Iterate Until Fixed Point` are **unbounded**. Domain used to cap
them at 1,000,000 iterations, which turned a legitimate long-running
simulation into a spurious failure; a limit must never be the reason a correct
program cannot run. The trade is real: a genuinely non-terminating loop now
spins until interrupted rather than failing loudly. The same reasoning removed
the ceilings on `Permutations`, `Subsets`, and sparse densification.

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

`<source>` is a channel name (declared via `Channel "name": ...`, holding a
`List<T>`) or an inline `range(N)` (`N` an Int literal, yielding `0..N-1`).
Nested `For` loops each add their own parameter, outermost first:
`Using: (v, a, b) -> ...` for `For a in as: For b in bs: ...`. A leading
parameter that happens to share the ambient name shadows it.

`For` compiles like every other loop kind — the earlier interpreter-only
restriction is gone, so there is no advertised gap between the backends.

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

## Reveal — output

`Reveal: stdout` prints the current value and ends the useful pipeline.
`Reveal: stderr` prints to standard error instead — a mid-pipeline Reveal that
does not disturb the program's answer, so a golden test still passes with one
in place. Every
type has a deterministic rendering (insertion order for Maps/Sets, row-major
for Grids, sorted set-cell listings for Sparse grids); see
[data-model.md](data-model.md).
