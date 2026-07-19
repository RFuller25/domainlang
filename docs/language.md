# Language reference

## Source format

- **Significant indentation, spaces only.** A tab anywhere in indentation is a
  lex error. Indented lines form a block belonging to the statement above.
- **One pipeline statement per line**, `Keyword: operation phrase`.
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
| `Cursed Energy:` | input / data source |
| `Cursed Technique:` | 1:1 transforms |
| `Channeled Energy:` | type coercions |
| `Maximum Technique:` | reductions / aggregation |
| `Domain Expansion:` | a **named algorithm the optimizer may swap** |
| `Reverse Cursed Technique:` | inversions |
| `Simple Domain:` | control flow (loops) |
| `Channel "name":` | a named sub-pipeline branching from the current value |
| `Shikigami "name" (params)` / `Shikigami: Name` | user-defined operation definition / call |
| `Binding Vow:` | debug-time assertion over the current value |
| `Reveal:` | terminal output sink |

The full per-primitive reference is [primitives.md](primitives.md).

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
- `Seed:` — the fold accumulator's initial value (Int or Text).
- `From:` — the channels a consumer reads (Combine, Difference, Fold, Zip).

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

`Combine` binds channel values to the lambda's parameters in `From:` order.
`Difference` (exactly two channels, Set-or-List of keyable elements) emits the
set difference `a - b`. `Fold` with `From:` (one channel holding a List)
folds over the channel's list with the **current pipeline value as the
seed**, and `Zip` pairs two channel lists element-wise — see
[primitives.md](primitives.md). These consumers are the only place the
otherwise linear pipeline forms a graph.

## Shikigami — user-defined operations

A Shikigami names a composition of primitives, with typed parameters
(`Int` or `Text` in v0.2) substituted into its body:

```domain
Shikigami "Top K Sum" (k: Int)
    Domain Expansion: Quicksort, Descending
    Maximum Technique: Select Top k, Sum
```

Calls use the block form; parameters are passed as named arguments:

```domain
Shikigami: Top K Sum
    k: 3
```

Shikigami are **inlined** during resolution, so optimizer rewrites fire
through them (the call above still fuses into a quickselect), and the
compiler backend sees only primitives. Recursion is guarded by an inlining
depth limit.

### The prelude

The standard library is itself written in Domain and loaded before every
program:

| Shikigami | Expands to | Type |
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

`While` and `Iterate Until Fixed Point` are bounded at 1,000,000 iterations —
a runaway program fails loudly instead of hanging.

## Binding Vows — debug assertions

A vow is a predicate over the current value. It **never changes the value**;
on violation it aborts with the vow, the stage, and the offending value.

```domain
Binding Vow: Count Equals 200      # the current List has exactly 200 items
Binding Vow: All Values > 0        # every Int in the current List<Int> passes
```

Supported comparisons in `All Values`: `>` `>=` `<` `<=` `=`.

Vows are debug-time: `--release` sheds them (`domain run --release` skips
them; `domain build --release` compiles them out of the binary entirely).

## Reveal — output

`Reveal: stdout` prints the current value and ends the useful pipeline. Every
type has a deterministic rendering (insertion order for Maps/Sets, row-major
for Grids, sorted set-cell listings for Sparse grids); see
[data-model.md](data-model.md).
