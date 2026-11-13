# Language reference

This page describes how a Domain program is put together: the source format,
the keywords, and each construct a program is built from — statements and
their arguments, Channels, Parts, Shikigami, loops, globals, vows and output.

If you have not written a Domain program yet, read
[getting-started.md](getting-started.md) first; it introduces everything here
in order, one small program at a time. The operations themselves (`Split`,
`Sum`, `Sort`, …) are in [primitives.md](primitives.md), and what goes inside
a `Using:` lambda is in [expressions.md](expressions.md).

## Source format

- **One statement per line**, written `Keyword: operation phrase` — or just
  `operation phrase`, because the keyword is optional (see
  [Optional keywords](#optional-keywords)).
- **Indentation is significant, and spaces only.** Indented lines form a block
  belonging to the statement above them. A tab anywhere in indentation is an
  error.
- **A comment starts at `#`, or at the word `technically`**, and runs to the
  end of the line ([below](#comments)).
- **Strings are double-quoted** and understand the escapes `\n`, `\t`, `\\`
  and `\"`.
- **Blank lines mean nothing.**

One construct is exempt from all of this: the body of a foreign block
(`Domain Expansion: Python`, and its siblings for other languages) is source
in that language, taken verbatim — its own comment character, its own
brackets, tabs if it wants them. See
[Foreign Block](ref-expansions.md#foreign-block--t---text-or-a-declared-in---out).

## Comments

A comment runs to the end of the line and starts either of two ways:

```domain
# the classic
Cursed Energy: input.txt   # a trailing note

technically the input is one number per line
Shikigami: Ints            technically that splits the lines and converts them
Maximum Technique: Sum
Reveal: stdout
```

`technically` is a comment marker because the language is written in sentences
and its asides can be too: "technically the grid is ragged" reads as the
remark it is.

It is a **word**, and it only starts a comment where a word can start and
where no word continues past it:

| Written | Read as |
|---|---|
| `technically fine` | a comment |
| `Technically, fine` | a comment — case does not matter |
| `technicallyOK` | an identifier |
| `technically_true` | an identifier |
| `Reveal: "technically"` | a string, exactly as `"#"` is |

There is no block comment, and neither marker means anything inside a foreign
block.

## The two layers

A Domain program is written in two small languages, one inside the other.

1. **The pipeline layer** uses the themed keywords. A program is an ordered
   list of statements, each of which transforms a single implicit *current
   value* and hands the result to the next. Input and output happen only
   here.
2. **The expression layer** is plain and unthemed. It is what you write inside
   a `Using:` lambda to decide a per-element detail:
   `(a, b) -> a + b = 2020`. See [expressions.md](expressions.md).

The whole program is type-checked **before** anything runs. Each statement is
matched to an operation, its input type is checked against what the previous
statement produced, and its output type is computed — lambda result types
included. A mistyped pipeline fails with a line and column, never halfway
through your input.

## Keywords

Each themed keyword names a role a statement can play.

| Keyword | Role |
|---|---|
| `Innate Domain:` | declares what kind of program this is — see [scopes.md](scopes.md). Must be written with its keyword |
| `Inherited Technique:` | imports a library of Shikigami. Must be written with its keyword |
| `Cursed Energy:` | the input source |
| `Cursed Technique:` | transforms |
| `Channeled Energy:` | type conversions |
| `Maximum Technique:` | reductions and aggregation |
| `Domain Expansion:` | a **named algorithm, which the optimizer may replace** with a faster one that gives the same result |
| `Reverse Cursed Technique:` | inversions |
| `Simple Domain:` | loops |
| `Channel "name":` | a named side branch computed from the current value |
| `Part "label":` | a labelled answer computed from the current value |
| `Cursed Object:` / `Cursed Tool:` | declares a global / changes one ([below](#cursed-object--globals)) |
| `Shikigami "name" (params) : In -> Out` / `Shikigami: Name` | defines a user operation / calls one |
| `Binding Vow:` | an assertion about the current value |
| `Reveal:` | prints the current value |

Two more things appear inside a statement's indented block rather than as
statements of their own:

| Written | Meaning |
|---|---|
| `Consider name As …` / `Consider name Of …` | a local binding for that statement's expressions ([below](#local-bindings)) |
| an indented pipeline where a `Using:` lambda would go | a sub-pipeline standing in for the lambda ([expressions.md](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)) |

Every operation available under each keyword is listed in
[primitives.md](primitives.md).

## Optional keywords

The keyword may be left off. A line written as a bare operation phrase means
the same statement, so these two programs are identical:

```domain ignore
Cursed Energy: input.txt                    input.txt
Cursed Technique: Split Text by "\n\n"      Split Text by "\n\n"
Channeled Energy: Convert To Integers       Convert To Integers
Domain Expansion: Quicksort, Descending     Quicksort, Descending
Maximum Technique: Select Top 3, Sum        Select Top 3, Sum
Reveal: stdout                              stdout
```

The two spellings mix freely, line by line — write the keyword where it helps
a reader and leave it out where it is noise. The type checker, the optimizer
and every tool see the same program either way.

**How a bare phrase is recognised.** The compiler tries these in order and
takes the first that fits:

1. **a Shikigami name** — a call is just the name (`Top K Sum`)
2. **a statement with a `From:` argument** — it draws on channels (`Combine`)
3. **a loop** — `Repeat N`, `While`, `Iterate Until Fixed Point`. (The
   operations `Take While`, `Drop While` and `Iterate n` share words with
   loops but are not loops, and are not matched here.)
4. **a vow** — `Count Equals N`, `All Values <cmp> N`
5. **the output** — `stdout`
6. **an input source**, on the first line only — `stdin`, or something shaped
   like a path (`input.txt`, `data/day1.txt`). Paths are recognised before
   operation names, so a file called `sum.txt` is a file.
7. **an operation** — any primitive whose phrase matches
8. **an input source**, on the first line only — anything left over

**Ambiguity is an error, never a guess.** If a phrase could be an operation
under two different keywords, Domain says so and asks you to write the
keyword. A phrase that matches nothing is an error with a suggestion:

```
error[name]: cannot infer a keyword for "Splt Each by": no operation matches this phrase
  help: did you mean "Split Each" (Cursed Technique)?
```

**Where the keyword is required:**

- `Innate Domain:` and `Inherited Technique:` are always written out. A bare
  library name would look exactly like an input file or a misspelled
  operation.
- If you write a keyword, write its colon. `Reveal stdout` is a syntax error
  (with an automatic fix), not a bare phrase.

## Statements and arguments

A statement is a keyword, an operation phrase, and optionally an indented
block of **named arguments**:

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{a:int}-{b:int},{c:int}-{d:int}"
```

An argument's value is one of:

| Kind | Example |
|---|---|
| a string | `Using: "{a:int}-{b:int}"` |
| an integer | `k: 3`, `Seed: 0` |
| a bare word | `Mode: First` |
| a list of names | `From: moves, rows` |
| a lambda | `Using: (a, b) -> a + b` |

Which arguments an operation accepts is listed with it in
[primitives.md](primitives.md). The ones you will meet most:

- `Using:` — a lambda, or a `Match Pattern` template string.
- `Mode:` — selects a variant (`One`/`Each` for `Match Pattern`;
  `Filter`/`Count`/`First`/`Map` for `All Pairs` and `Combinations`).
- `Seed:` — a fold's starting value.
- `From:` — the channels a statement reads (`Combine`, `Difference`, `Fold`,
  `Zip`).

An argument the operation does not read is ignored, and the linter reports it
(see [diagnostics.md](diagnostics.md#the-linter)).

**A long value may continue on the following lines.** Inside a parenthesis a
newline is just whitespace, and lines indented under the argument continue its
value:

```domain
Cursed Technique: Apply
    Using: (v) ->
        consider d as abs(v - 10)
        in if d > 3
            then d * 2
            else d
```

Only the line breaks change; see
[expressions.md](expressions.md#writing-an-expression-across-lines).

### Local bindings

A statement's block may also hold `Consider` lines. Each names a value that
the statement's expressions can use:

- **`Consider NAME Of …`** computes the value from the pipeline value arriving
  at the statement. What follows `Of` is an operation, a lambda, or an
  indented sub-pipeline.
- **`Consider NAME As …`** never sees the pipeline value. It names a constant,
  an expression, or a helper function.

`Of` is how a per-element lambda gets at something computed from the whole
list:

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

`As` is where a constant or a helper function goes:

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

A binding is in scope for every lambda on its statement and for the statements
nested under it, and it goes out of scope when the statement ends. Those
lambdas may also [update it with `:=`](expressions.md#updating-a-local--),
which lets a value carry from one element to the next.

`Consider` always needs its keyword. An argument's name is fixed vocabulary
(`Mode:`, `Using:`) while a binding's name is yours, so keeping the two apart
means a misspelled `Usng:` is reported as a misspelled argument rather than
quietly becoming a binding nobody reads.

The full rules are in
[expressions.md](expressions.md#stage-bindings--consider--as--consider--of).

### Measured arguments

Many operations take a literal in their phrase — `Window 3`, `Chunk 4`,
`Select Top 3`, `Split Text by ","`. Any of these may instead be written as a
named argument holding a lambda over the **current value**, so the number or
separator is computed from the data rather than fixed in the program. This is
a *measured argument*.

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

Here the separator is chosen by looking at the text:

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

The rules:

- The lambda receives the whole current value — plus one trailing parameter
  per enclosing `For` loop.
- It must return the type the literal would have had: `Int` for a count,
  `Text` for a separator, the cell type for a fill.
- It runs once each time the statement runs, before the operation does.

The arguments that can be measured are `Size:`, `Step:`, `Count:`, `Index:`,
`Times:`, `Low:`, `High:`, `By:`, `With:`, `Fill:`, `Default:`, `Mark:`,
`Seed:`, `Row:`, `Col:`, `Height:`, `Width:` and `Thickness:`.
[The primitive reference](ref-transforms.md#measured-arguments) lists which
operations take which.

**What cannot be measured.** An argument can be measured when it is *data*.
Three kinds are not, and must stay literal:

1. **Arguments that decide a type.** `Combinations k` fixes how many
   parameters the `Using:` lambda takes; a `Match Pattern` template fixes the
   output record's fields; a `Mode:` picks the result's shape. Types are
   settled before the program runs.
2. **Arguments read before there is a current value.** The input source and
   an `Inherited Technique:` target are needed before any data exists.
3. **Arguments that name part of the program.** `From:` names channels;
   `Channel`, `Part` and `Shikigami` name declarations.

## Channels — multi-section inputs

A `Channel "name":` statement runs an indented sub-pipeline **on the current
value** and stores the result under the name. The main pipeline's current
value is left unchanged, so several channels in a row all start from the same
value. A later statement reads them with a `From:` argument.

That makes channels the tool for an input with more than one section: split
it, give each section its own channel, and join the results.

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

**The statements that read channels:**

| Statement | Reads | Produces |
|---|---|---|
| `Combine` | any number of channels | whatever its lambda returns; the channels are bound to the lambda's parameters in `From:` order |
| `Difference` | exactly two, each a Set or List | the elements of the first that are not in the second |
| `Zip` | two lists | their elements paired up |
| `Fold` with `From:` | one list | a fold over the channel's list, starting from the **current value** |

Each is described in full in [primitives.md](primitives.md).

**A channel may read the channels declared above it**, so a value derived from
other channels can be named once instead of recomputed wherever it is needed:

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

**Rules:**

- A channel can only read channels declared *above* it. Reading itself, or one
  declared later, is an unknown-channel error.
- Channels cannot be nested inside one another.
- A loop body may read a channel (see [loops](#simple-domain--loops)). A
  Shikigami body and a `Using:` sub-pipeline may not.

## Part — two answers from one parse

An Advent of Code day asks two questions about the same input. A
`Part "label":` statement runs an indented sub-pipeline **on the current
value** and labels whatever that sub-pipeline prints — so everything above the
Parts runs once and feeds both.

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

**A Part does not change the current value.** Like a
[Channel](#channels--multi-section-inputs), it branches off and leaves the
main pipeline as it was. Each Part therefore starts from the same value, and
nothing one Part does is visible to the next — or to a `Reveal` after them:

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

**A Part prints only what its body `Reveal`s.** There is no implicit print. A
Part that never reveals anything is pointless, and the linter warns about it.

**The label** is free text, not just a number — `Part "totals":` is fine. It
is printed in front of the output. A value that spans several lines starts on
the line *after* its label, so a grid stays aligned:

```
Part picture:
###..
..###
```

**Rules:**

- Parts are top-level only. A Part inside a Channel, a loop, a Shikigami or
  another Part is an error.
- A Part may read channels declared above it with `From:`, but may not declare
  channels of its own.
- Two Parts with the same label is a lint warning.

## Shikigami — user-defined operations

A Shikigami gives a name to a sequence of statements, so it can be used like a
built-in operation. Define it with the name in quotes, an optional parameter
list, and an indented body; call it with `Shikigami: Name`, passing any
parameters as named arguments.

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

A definition is a declaration, not a step, so it may sit above or below the
pipeline that uses it. One definition can be called any number of times, with
different arguments each time:

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

The `Shikigami:` keyword on a call is optional like any other, so a call can
be written as just the name:

```domain ignore
Shikigami: Top K Sum        Top K Sum
    k: 3                        k: 3
```

**A call is replaced by the definition's body** before the program is
optimized or run. So a Shikigami costs nothing, and the optimizer sees
straight through it — `Top K Sum` above is rewritten into a quickselect
exactly as if its two statements had been written in place.

### Parameters

A parameter is a value written at the call site. It is either a simple value
or a lambda:

| Declared | Passed as | Where the body can use it |
|---|---|---|
| `k: Int` | `k: 3` | in operation phrases (`Select Top k`) and in lambdas |
| `s: Text` | `s: "note"` | in operation phrases and in lambdas |
| `f: Float` | `f: 1.5` | in lambdas only; an Int argument is widened |
| `b: Bool` | `b: true` | in lambdas only; written as bare `true` / `false` |
| `p: (Int) -> Bool` | `p: (x) -> x > 100` | wherever a lambda is expected |

A simple parameter is substituted into the body wherever its name appears:

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

A lambda parameter lets the caller supply part of the operation — here, the
test to count by:

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

The lambda is checked against its declared type at the call, so the wrong
number of parameters or the wrong result type is reported there. A lambda
parameter may be used in more than one place in the body.

A lambda parameter is also how a
[measured argument](#measured-arguments) is passed in. Declare the parameter
as a function and hand it to the argument:

```domain
Shikigami "Sized Windows" (size: (List<Int>) -> Int)
    Cursed Technique: Window
        Size: size
```

(Declaring it `size: Int` and passing a lambda is an error: a simple parameter
is substituted as a literal value.)

**Lists, grids and other composite values cannot be parameters**, because
there is no way to write one at a call site. They reach a Shikigami the way
they reach any operation: as the current value.

### Declared signatures

`: In -> Out` after the parameter list states what type the Shikigami takes
from the pipeline and what type it leaves there. It is optional. When written,
it is checked at both ends:

- the **input** against the pipeline's type at each call, so calling it on the
  wrong thing is reported at the call — `Shikigami "Top K Sum" expects input
  of type List<Int>, but the pipeline produced Text` — rather than somewhere
  inside the body;
- the **output** against what the body actually produces.

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

**Writing types.** `Int`, `Float`, `Text`, `Bool`, `List<T>`, `Set<T>`,
`Grid<T>`, `Sparse<T>`, `Map<K,V>`, tuples as `(A, B)`, and records as
`{a: Int, b: Text}`. This is the same spelling type errors use. The order of a
record's fields does not matter: `{b: Text, a: Int}` matches
`{a: Int, b: Text}`. The outermost `->` separates input from output, so
`: (Int, Int) -> Int` takes a tuple and returns an `Int`.

**A signature names exact types** — there are no type variables. To write an
operation that works on more than one type, leave the signature off:

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

### Naming: a Shikigami may not be named after a built-in

A call is just the name, so a Shikigami called `Sum` would leave no way to
write the built-in `Sum`. These names are refused:

- an operation's name, or any phrase that spells one (`Sum`,
  `Convert To Grid`, `Quicksort`, `Sort By`)
- a themed keyword (`Cursed Technique`, `Reveal`)
- an [expression builtin](expressions.md) (`length`, `gcd`, `sum`)
- a loop kind (`Repeat`), a vow shape (`All Values`), `stdout`, or `stdin`

Only names that *are* a built-in are reserved, not every name that contains
one: `Scaled Sum`, `Sort Text` and `Top K Sum` are all fine. The check happens
at the definition, so a clash is reported there.

### Recursion is refused

A Shikigami cannot call itself, directly or through another. A call is
replaced by the body, and a recursive definition would never finish expanding.
The error names the cycle:

```
error: Shikigami "Ping" is recursive (Ping -> Pong -> Ping): a Shikigami is
inlined at its call site, so it has no finite expansion — use
`Domain Expansion: Explore` for a search over states
```

Shikigami that call other Shikigami without a cycle are fine, to any depth.

For problems that seem to need recursion — reachability, fewest moves,
counting arrangements — use
[`Domain Expansion: Explore`](ref-expansions.md), which searches a space of
states and stops when it has seen them all.

### Inherited Technique — importing a library

`Inherited Technique: <library>` loads a file of Shikigami definitions, so its
operations can be called by name:

```domain ignore
Inherited Technique: aoc
Inherited Technique: grids/hex

Cursed Energy: input.txt
Shikigami: Lines
Shikigami: All Ints          # defined in aoc.domain
```

The library is named **without** its `.domain` extension, and
`Inherited Technique:` must be written with its keyword.

Here is a library beside a program, and the program calling into it:

```domain run
Inherited Technique: aoc

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

**A library holds Shikigami definitions and nothing else.** A pipeline
statement in an imported file is an error. A library may import other
libraries.

**An import is a declaration, not a step.** Where it appears in the file does
not matter — by convention, at the top — as this program with its import at
the bottom shows:

```domain run
Cursed Energy: stdin
Shikigami: Doubled
Reveal: stdout

Inherited Technique: helpers
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

**Where libraries are looked for.** The first match wins:

1. the directory of the file doing the importing;
2. each directory in `$DOMAIN_PATH` (colon-separated);
3. `~/.config/domain/lib`.

A name with a slash (`grids/hex`) is looked up the same way under each of
those. If nothing is found, the error lists every directory searched.

**When two definitions share a name**, the nearer one wins:

```
prelude  <  imports, in the order written  <  the program's own Shikigami
```

A library's own definitions win over the ones it imported. A library reached
by two routes is loaded once, and libraries that import each other in a circle
are an error naming the circle. The
[naming rule](#naming-a-shikigami-may-not-be-named-after-a-built-in) applies
to imported definitions too.

**Libraries are needed only when the program is built.** Imported operations
are substituted into the program like local ones, so they are optimized the
same way, and a compiled binary never looks for the library again.

### The prelude

The prelude is a small standard library of Shikigami, available in every
program with no import:

| Shikigami | What it does | Type |
|---|---|---|
| `Lines` | splits on `"\n"` | `Text -> List<Text>` |
| `Blocks` | splits on `"\n\n"`, then each block on `"\n"` | `Text -> List<List<Text>>` |
| `Ints` | splits on `"\n"` and converts to integers | `Text -> List<Int>` |
| `Digit Grid` | splits into lines, then characters, converts to integers, builds a grid | `Text -> Grid<Int>` |
| `Top K Sum` (k: Int) | sorts descending, takes the top k, sums them | `List<Int> -> Int` |

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

`Blocks` parses paragraph-separated input, and `Top K Sum` takes a parameter:

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

A Shikigami of your own with the same name replaces the prelude's.

## Simple Domain — loops

A loop runs an indented sub-pipeline — its *body* — over the current value,
repeatedly. There are five kinds:

| Loop | Runs the body |
|---|---|
| `Repeat N` | exactly N times |
| `While` | as long as its `Using:` test is true of the current value |
| `Iterate Until Fixed Point` | until the value stops changing |
| `For x in <list>` | once per element of a fixed list |
| `For Each x In` | once per element of a list computed from the current value |

**The body must give back the type it was given.** Whatever goes into a lap
comes out of it as the same type; that is what makes running it again
meaningful. To get something of a different type *out* of a loop — a count of
laps, say — use a [global](#cursed-object--globals).

```domain ignore
Simple Domain: Repeat 3
    <body>

Simple Domain: While
    Using: (v) -> v > 1
    <body>

Simple Domain: Iterate Until Fixed Point
    <body>
```

`Repeat N` runs the body a fixed number of times:

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

`While` tests the current value before each lap and stops as soon as the test
is false:

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

`Iterate Until Fixed Point` stops when a lap leaves the value equal to what it
was before the lap.

`While` and `Iterate Until Fixed Point` give up with an error after
**1,000,000,000** laps. A loop that gets there is not slow but stuck, and an
error is more useful than a program that never returns.

### For

`For x in <list>` walks a list that is fixed before the loop starts. The list
is either a channel holding a `List<T>` or an inline `range(N)`, which counts
`0` to `N-1` (`N` must be a literal number).

Inside the body, the loop variable is added as an **extra trailing parameter**
to every `Using:` lambda:

```domain
Channel "deltas":
    Cursed Technique: Apply
        Using: (xs) -> xs

Simple Domain: For x in deltas
    Cursed Technique: Filter
        Using: (v, x) -> v > x
```

`Filter` normally takes a one-parameter lambda; inside the loop it takes
`(v, x)`, where `x` is the current element of `deltas`. Nested `For` loops
each add their own parameter, outermost first: `(v, a, b)` inside
`For a in …` containing `For b in …`.

### For Each

`For Each x In` walks a list **computed from the current value** by a
`Using:` lambda. Use it when the list to walk is part of the value being
looped over.

```domain ignore
Simple Domain: For Each creep In
    Using: (w) -> w.creeps
    <body>
```

The body's lambdas gain **two** trailing parameters: the element and its index
in the list. The body does not write the element back automatically — to
change the list, say so, usually with `set(xs, i, v)`:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Simple Domain: For Each n In
    Using: (xs) -> xs
    Cursed Technique: Apply
        Using: (xs, n, i) -> set(xs, i, n * 2)
Reveal: stdout
```
```input
1,2,3
```
```output
[2, 4, 6]
```

The `Using:` lambda is evaluated again each time the loop statement itself
runs. So when a `For Each` sits inside another loop, it walks the list as it
is on that lap — which is what a game needs when its list of entities grows
and shrinks from tick to tick.

### Loops and channels

A loop body may read a channel with `From:`. A channel is computed before the
loop starts and never changes, so this is how a loop gets at read-only data
without carrying it around in its own value. The useful form is `Fold` with
`From:`, which folds the channel's list into the loop's value each lap.

A Shikigami body and a `Using:` sub-pipeline cannot read a channel, so a loop
written inside one of those cannot either.

## Cursed Object — globals

A `Consider` binding lasts for one statement. **`Cursed Object`** declares a
name that lasts for the rest of the program, and **`Cursed Tool`** changes its
value.

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

The reason to want one is usually a loop. A loop body must hand back the type
it was given, so a loop cannot simply return "the result, and also how many
laps it took". A global is somewhere to put the second thing:

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

Inside a lambda, `laps := laps + 1` writes to the global, and `also` runs that
write after computing the lambda's result (see
[expressions.md](expressions.md#updating-a-local--)). `Cursed Tool: laps As
laps + 1` is the same write as a statement of its own.

### Declaring: `As` and `Of`

The two words mean what they mean on a `Consider`: **`As` never sees the
pipeline value; `Of` always does.**

| Written | The global holds |
|---|---|
| `Cursed Object: n As 0` | a value |
| `Cursed Object: n As k + 1` | an expression over globals already declared |
| `Cursed Object: n Of (xs) -> length(xs)` | something computed from the pipeline value at this line |
| `Cursed Object: n Of Sum` | the same, written as an operation |
| `Cursed Object: n Of` + an indented pipeline | the same, written as a sub-pipeline |
| `Cursed Object: n Of Itself` | the pipeline value at this line, unchanged |

In every case the value is computed when the pipeline reaches the line.

Several declarations can share one keyword, and each sees the ones above it:

```domain
Cursed Object:
    a As 2
    b As a * 5
    c As a + b
```

A global cannot hold a function. `Cursed Object: f As (x) -> …` is refused;
use a `Consider … As` binding for a helper function.

### Scope

A global is in scope **from its own line to the end of the program**, whatever
block it was declared in. Reading one above its declaration is an error.

A nearer name of the same spelling — a lambda parameter, a `consider` local, a
`Consider` binding — hides the global for as long as that name is in scope.

A global's type is fixed by its declaration, and every later write is checked
against it. A global may not be given a name the language already uses: an
operation, a keyword, a loop kind, a vow, `stdout`/`stdin`, an expression
builtin, or a Channel.

### A declaration inside a loop runs every lap

`Cursed Object` is a statement, so inside a loop body it runs again on every
lap — and a counter declared there is reset every time. Declare it above the
loop and change it with `Cursed Tool`:

```domain ignore
Simple Domain: Repeat 10
    Cursed Object: n As 0        # 0 again every lap — almost certainly a mistake
    Cursed Tool:   n As n + 1    # what was meant, with n declared above the loop
```

The linter warns about the first form. A declaration written with `Of` is
exempt, because reading the value arriving on each lap is the point of it.

### Where globals cannot reach

- **Parts are isolated.** Each Part starts from the same value *and* the same
  globals. Anything a Part writes to a global is discarded when the Part ends.
- **Channel bodies cannot read or write globals.** A channel's result must not
  depend on when it happened to be computed.
- **Prelude and imported Shikigami cannot read or write globals.** Their
  authors never saw your program's names. A Shikigami defined in your own file
  can.

### What a global costs

A global that nothing writes to after its declaration is a constant, and costs
nothing — `Cursed Object: target As 2020` leaves every optimization available.

A global that *is* written to is different. A statement that reads it no
longer depends only on its input, so the optimizer leaves that statement
exactly as written (see [optimizer.md](optimizer.md)). `domain expansion:
lint` names each statement this applies to.

## Binding Vows — debug assertions

A vow states something that should be true of the current value. It never
changes the value. If it is false, the program stops and reports the vow, the
line, and the value that broke it.

```domain
Binding Vow: Count Equals 200      # the current list has exactly 200 items
Binding Vow: All Values > 0        # every number in the current List<Int> passes

Binding Vow: Holds                 # any test, over any type
    Using: (g) -> rows(g) = cols(g)
```

`Count Equals N` and `All Values <cmp> N` are shorthand for the two commonest
checks on a list of integers; `All Values` accepts `>`, `>=`, `<`, `<=` and
`=`. `Holds` is the general form and takes any lambda returning `Bool`.

A vow that holds is invisible — the value passes through untouched:

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

`Holds` works on any type, here a grid:

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

Vows are for development. `--release` removes them: `domain run --release`
skips them, and `domain build --release` leaves them out of the binary.

## Reveal — output

`Reveal: stdout` prints the current value.

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

`Reveal: stderr` prints to standard error instead. It is the way to look at a
value in the middle of a pipeline without changing what the program prints as
its answer — standard output below still carries only the total:

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

`Reveal` does not change the current value, so the pipeline may continue after
one. Every type prints in a fixed, predictable form — Maps and Sets in the
order their entries were added, Grids row by row; see
[data-model.md](data-model.md).
