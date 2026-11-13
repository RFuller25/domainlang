# Expressions

The expression layer is the small, plain language written inside a `Using:`
lambda. The pipeline moves data from statement to statement; an expression
decides one detail about one element — which to keep, what to turn it into,
how to combine two of them.

This page covers lambdas, operators, conditionals, naming and updating values,
and the two things that sit between expressions and the pipeline: stage
bindings and pipeline bodies. The 208 builtin functions are on
[their own pages](#builtin-functions).

Expressions are type-checked with the rest of the program, before anything
runs.

## Lambdas

A lambda is a parameter list, an arrow, and an expression:

```
(a, b) -> a + b = 2020
(r) -> (r.a <= r.c and r.b >= r.d) or (r.c <= r.a and r.d >= r.b)
(g) -> sum(take(reverse(g), 2))
```

**The operation decides how many parameters the lambda takes.** `Map Each`,
`Filter`, `Apply` and `Group By` take one; `Fold` takes two; `Combinations k`
takes k; `Combine` takes one per channel. You never declare parameter types:
they come from the type of the value flowing through the pipeline, and the
result type is worked out from the body. Where an operation needs a particular
result — `Filter` needs a `Bool` — a body of the wrong type is an error.

One parameter, in a `Map Each`. The body's type becomes the element type of
the result:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (n) -> n * n
Reveal: stdout
```
```input
1,2,3
```
```output
[1, 4, 9]
```

Two parameters, because `Fold` hands its lambda the running total and the next
element:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Fold
    Seed: 0
    Using: (acc, n) -> acc * 10 + n
Reveal: stdout
```
```input
1,2,3
```
```output
123
```

### Destructuring a tuple parameter

When a parameter is a tuple, you can name its fields directly by writing a
parenthesized list of names where the parameter would go:

```
((n, map, x)) -> …              one tuple parameter, three fields
(acc, (name, qty)) -> …         beside an ordinary parameter
(acc, (key, (lo, hi))) -> …     patterns nest
((_, _, count)) -> count        `_` ignores a field
```

```domain run
Cursed Energy: stdin
Cursed Technique: Extract Integers
Cursed Technique: Enumerate
Maximum Technique: Fold
    Seed: 0
    Using: (acc, (i, x)) -> acc + i * x
Reveal: stdout
```
```input
5 1 9
```
```output
19
```

A pattern has at least two fields and must name at least one of them. It is
shorthand for unpacking with `item(p, 0)`, `item(p, 1)`, … so the names behave
like any other parameter. If the parameter is not a tuple, or has a different
number of fields, that is a type error. `_` is a placeholder only inside a
pattern; anywhere else it is an ordinary name.

## Values and operators

**Literals and names**

| Written | Is |
|---|---|
| `42`, `-7` | an `Int` |
| `3.25` | a `Float` — digits, a dot, and at least one digit after it |
| `"text"` | a `Text` |
| `{a: 1, b: x}` | a record (see below) |
| `x` | a lambda parameter, or any other name in scope |
| `r.name` | a field of a record |
| `name(args…)` | a call to a builtin function |
| `(expr)` | grouping |

**There is no `true` or `false`.** A `Bool` comes from a comparison, from
`and` / `or` / `ikke`, or from a builtin that answers a question (`contains`,
`isprime`, …). When you need a constant one, write `1 = 1` or `1 = 2`. (The
words do appear in one place: as the argument for a
[`Bool` Shikigami parameter](language.md#parameters).)

**Operators**, loosest-binding first:

| Operators | Works on | Gives |
|---|---|---|
| `also` | anything | the value on its left ([below](#running-an-expression-for-its-effect--also)) |
| `:=` | a name and a value of its type | the value written ([below](#updating-a-local--)) |
| `or` | Bool | Bool |
| `and` | Bool | Bool |
| `ikke` (prefix) | Bool | Bool |
| `=` `<` `>` `<=` `>=` | see below | Bool |
| `+` `-` | Int, Float; `+` also joins two Texts | Int, Float, Text |
| `*` `/` `%` | Int, Float (`%` Int only) | Int, Float |

Operators on one row bind equally and group left to right, except `:=`, which
groups right to left. A minus sign in front of a number negates it.

Things worth knowing:

- **`=` is equality, always.** Writing to a name is spelled `:=`.
- **`ikke` means "not"** (it is the Norwegian word). It binds looser than a
  comparison and tighter than `and`: `ikke a = b` is `ikke (a = b)`, and
  `ikke a and b` is `(ikke a) and b`.
- **`and` and `or` stop early.** The right side is not evaluated when the left
  side already decides the answer, so `n = 0 or 10 / n = 5` never divides by
  zero.
- **`/` on integers drops the remainder**, rounding toward zero. Dividing by
  zero stops the program with an error.
- **`%` never gives a negative result** for a positive right-hand side:
  `(0 - 1) % 5` is `4`. That is the behaviour wrap-around indexing wants. It
  binds like `*`, so `0 - 1 % 5` is `0 - (1 % 5)`. `mod(a, b)` is the same
  operation as a function.
- **`=` compares any two values of the same type**, however large — two lists,
  records or grids are equal when their contents are.
- **`<` `<=` `>` `>=` compare values that have an order**: Int, Float, Text,
  or a tuple of those. An Int and a Float can be compared with each other.
  Text compares alphabetically by character code; a tuple compares by its
  first differing field. This is the same ordering `Sort` uses. Other types —
  Bool, records, lists, maps, sets, grids — have no order, and comparing them
  this way is an error.

**Records.** `{a: 1, b: x}` builds a record with fields `a` and `b`, and
`r.a` reads a field. Field names are plain words, not expressions. Reading a
field the record does not have is an error before the program runs. The
function form `record("a", 1, "b", x)` means the same thing. Records also come
out of `Match Pattern`, one field per named hole. See
[ref-builtins-records.md](ref-builtins-records.md).

Braces build records only. To build a set or a map, use `toset` and `tomap`.

## Conditional expressions

```
if cond then a else b
if length(xs) = 0 then -1 else first(xs)
if n < 0 then "neg" else if n = 0 then "zero" else "pos"
```

The condition must be a `Bool`, and both arms must have the same type — that
is the type of the whole expression. There is always an `else`.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (n) -> if mod(n, 2) = 0 then "even" else "odd"
Reveal: stdout
```
```input
1,2,3
```
```output
[odd, even, odd]
```

**Only the chosen arm is evaluated.** So `if length(xs) = 0 then -1 else
first(xs)` is safe: `first` is never called on an empty list.

Conditionals chain, which is how a choice between more than two things is
written:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (n) -> if n < 0 then -1 else if n = 0 then 0 else 1
Reveal: stdout
```
```input
-5,0,7
```
```output
[-1, 0, 1]
```

An arm extends as far to the right as it can: `if c then a else b + 1` puts
`b + 1` in the else arm. Use parentheses for `(if c then a else b) + 1`.

`if`, `then` and `else` are special only in this construct; they remain usable
as ordinary names elsewhere.

## Local bindings — `consider`

`consider NAME as VALUE in BODY` gives a name to a value for use inside
`BODY`. The value is computed once.

```
consider d as manhattan(a, b) in if d < 3 then d else 0
```

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (n) -> consider d as abs(n - 10) in if d > 3 then d * 2 else d
Reveal: stdout
```
```input
1,9,20
```
```output
[18, 1, 20]
```

To introduce more than one name, nest them:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Using: (xs) -> consider lo as min(xs) in consider hi as max(xs) in hi - lo
Reveal: stdout
```
```input
3 1 9
10 20
```
```output
[8, 10]
```

Use it whenever the same subexpression would otherwise be written twice — it
is then also computed only once.

The name exists only inside `BODY`, which extends as far right as it can. An
inner `consider` of the same name hides an outer one, and hides a lambda
parameter, for its own body only. Like `if`, the words `consider`, `as` and
`in` are special only here.

## Writing an expression across lines

A long expression can be broken across lines in two ways. Neither changes what
it means.

**Inside parentheses, a line break is just a space.** Indent the continued
lines however reads best; `domain fmt` leaves that alignment alone.

```domain
Cursed Technique: Map Each
    Using: (p) -> min(list(
        manhattan(p, point(0, 0)),
        manhattan(p, point(9, 9))
    ))
```

**Lines indented under an argument continue its value.** This is the form for
an expression with no parentheses to break inside, such as a
`consider … in if … then … else …`:

```domain
Maximum Technique: Combine
    From: square, real
    Using: (s, r) ->
        consider t as s - 1 - min(list(
            abs((s * s) - r),
            abs((s * s) - s - r)
        ))
        in if r = (s * s)
            then s - 1
            else t
```

Everything indented deeper than the argument's own line belongs to its value;
the value ends where the indentation returns. The value may also start on the
line below the argument name — `Using:` alone, then the lambda.

In the REPL these behave like any other indented block: keep typing, and
finish with a blank line.

## Pipeline bodies — a `Using:` that needs a primitive

Expressions have no way to loop. There is no builtin that takes a function, so
nothing in an expression can map, filter or search. That matters when the
value a lambda receives is itself a list — a row of a list of rows, say — and
the job you want done to it is a pipeline operation.

**Indent a pipeline where the lambda would go.** It runs in the lambda's
place, once per element, with the element as its current value. Its final
value is the lambda's result.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Map Each
    Domain Expansion: Sort, Descending
    Cursed Technique: Take Item 0
Reveal: stdout
```
```input
3 1 2
9 7 8
```
```output
[3, 9]
```

This is not special to `Map Each`. A body can stand in **wherever a
one-parameter `Using:` lambda is accepted** — `Filter`, `Sort By`, `Group By`,
`Count By`, `Sum By`, `Min By` / `Max By`, `Any` / `All`, `Find`, `Partition`,
`Take While` / `Drop While`, `Map Values`, `Apply`, `Iterate`, `Explore`, the
grid searches. The usual rule about result types still applies: under a
`Filter`, the body must end in a `Bool`.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Extract Integers
Cursed Technique: Filter
    Maximum Technique: Any
        Using: (n) -> n > 8
Cursed Technique: Map Each
    Using: (xs) -> length(xs)
Reveal: stdout
```
```input
3 1 2
9 7 8
```
```output
[3]
```

Two more shapes this makes possible — filtering rows by their sum, and sorting
rows by their total:

```domain
Cursed Technique: Filter            # keep the rows summing over 15
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (s) -> s > 15

Domain Expansion: Sort By           # order rows by their total
    Maximum Technique: Sum
```

**More than one parameter: `Params:`.** A body turns one value into one value,
so for a lambda of two or more parameters you must say which parameter the
body works on and what the others are called. `Params:` lists the names; the
**last** one is the body's current value, and all of them can be read by name
inside the body.

```domain ignore
Maximum Technique: Fold            # a fold whose step needs a primitive
    Seed: (xs) -> 0
    Params: acc, line
    Domain Expansion: Sort
    Cursed Technique: Apply
        Using: (r) -> acc + first(r)
```

Here the body is a pipeline over `line`, the element; `acc` is the running
value, readable by name; and the body's result becomes the new `acc`. A
`Params:` name follows the rules for a
[stage binding](#stage-bindings--consider--as--consider--of): it may not be
the name of a builtin function, and two parameters may not share a name.

**Rules:**

- An operation that takes no `Using:` lambda does not accept a body.
- A lambda and a body together is an error. So is `Params:` beside a written
  lambda.
- Bodies nest.
- A `For` loop's variable is in scope inside a body written within the loop.
- A body may not declare a `Channel` or read one with `From:`.

## Stage bindings — `Consider … As` / `Consider … Of`

A `consider` names a value inside one expression. A **stage binding** names a
value for a whole pipeline statement: every lambda on it, and every statement
nested under it. It is written in the statement's indented block, alongside
`Mode:` and `Using:`.

There are two forms, and the difference is where the value comes from:

- **`Of`** computes it from the value arriving at the statement.
- **`As`** never sees that value. It names a constant, an expression, or a
  function.

`Of` is how a lambda that looks at one element gets at a fact about the whole
list:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Consider biggest Of Max
    Using: (x) -> x = biggest
Reveal: stdout
```
```input
3,9,2,9
```
```output
[9, 9]
```

`As` names a constant or a helper function:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider limit As 4
    Consider clampTo As (n) -> min(list(n, limit))
    Using: (x) -> clampTo(x)
Reveal: stdout
```
```input
1,5,9
```
```output
[1, 4, 4]
```

Every form:

| Written | The name holds |
|---|---|
| `Consider accum As 3` | a constant |
| `Consider n As 2 * (k + 1)` | an expression over the bindings above it |
| `Consider len As (x) -> length(x)` | **a function** — call it as `len(xs)` |
| `Consider total Of Sum` | the result of an operation applied to the arriving value |
| `Consider total Of (xs) -> sum(xs)` | the same, written as a lambda |
| `Consider total Of` + an indented pipeline | the result of a sub-pipeline over the arriving value |
| `Consider line Of Itself` | the arriving value, unchanged |

What follows `Of` is an operation phrase, a lambda, or an indented pipeline —
never a bare expression, so `Of Sum` always means the operation `Sum`. An `Of`
with a sub-pipeline looks like this:

```domain
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Filter
    Consider mean Of
        Maximum Technique: Sum
        Cursed Technique: Apply
            Using: (s) -> s / 5
    Using: (x) -> x > mean
Reveal: stdout
```

### Scope

A binding is in scope for **every lambda-valued argument on its statement** —
`Using:`, `By:`, `While:`, `Until:`, a measured `Times:` — and for every
statement nested under it. It goes out of scope when the statement ends.

Bindings are read top to bottom, and each can use the ones above it, so
`Consider half As total / 2` is fine after `Consider total Of Sum`.

A nearer name hides a farther one: a binding in an inner block hides an outer
binding of the same name, and a lambda parameter hides a binding.

A binding at the top of a Shikigami body covers the whole body and may use the
Shikigami's parameters.

**An `Of` binding sees what the *statement* receives.** On a `Map Each`, that
is the whole list, not one element — even though the lambda beside it sees one
element at a time. To name the element, put the `Consider` on a statement
*inside* the `Map Each`'s body, where the element is what arrives.
`Consider line Of Itself` is the direct way to do that.

On a loop, the binding is computed once for the whole loop, not once per lap.

### What a binding cannot be

- **It may not take a builtin function's name.** `Consider length As …` is
  refused, because it would change what `length(xs)` means everywhere in
  scope.
- **A function binding must be called.** Functions are not values in Domain,
  so `len` on its own is an error where `len(xs)` is wanted. Likewise a value
  binding cannot be called.

### What a binding costs

A constant and a function cost nothing. The constant is substituted into the
lambdas that use it, and a function is expanded where it is called, before the
program is optimized. So `Consider target As 2020` with
`(a, b) -> a + b = target` is optimized exactly as if `2020` had been written
in the lambda.

An `Of` binding is not known until the data arrives. It is computed once when
the statement starts, and an optimization that depends on knowing the value in
advance does not apply (see [optimizer.md](optimizer.md)). The same goes for
any binding that is [updated with `:=`](#updating-a-local--).

## Updating a local — `:=`

`NAME := VALUE` writes a new value to a name that is already in scope. It is
itself an expression, and its value is the value written — so an update can
sit in the middle of a larger expression:

```
consider t as x * 2 in (t := t + 1) * 10
```

For `x = 3` that reads `t` as 6, writes 7, and gives 70.

What can be written to, and how long the write lasts:

| The name is | The write lasts |
|---|---|
| a `consider` local | until that expression finishes |
| a `Consider … As/Of` **stage binding** | for the rest of the statement: the next element sees it, and so does the next lap of a loop |
| a `Cursed Object` **global** | for the rest of the program ([language.md](language.md#cursed-object--globals)) |

Writing to a stage binding is what lets a value carry from one element to the
next. Here it keeps a running total:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider running As 0
    Using: (x) -> running := running + x
Reveal: stdout
```
```input
1,2,3
```
```output
[1, 3, 6]
```

And here it counts the elements as they go by:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider seen As 0
    Using: (x) -> (seen := seen + 1) * x
Reveal: stdout
```
```input
5,5,5
```
```output
[5, 10, 15]
```

**Rules:**

- **The type cannot change.** `n := 1.5` on an `Int` binding is an error;
  declare the binding as a Float instead.
- **Three kinds of name cannot be written to**: a lambda parameter, a function
  binding (`Consider f As (x) -> …`), and a Shikigami parameter. Each error
  says what to write instead.
- **Evaluation is left to right.** `n + (n := x) + n` reads the old `n`,
  writes, then reads the new one.
- **A write that is not reached does not happen.** `and` and `or` stop early
  and only one arm of an `if` runs, so in `x > 100 and (n := 1) > 0` the write
  is skipped when `x` is not over 100.
- A write to a `consider` local stays inside that local, even if a stage
  binding outside has the same name.
- A write made from inside a [pipeline body](#pipeline-bodies--a-using-that-needs-a-primitive)
  reaches the binding like any other.

### What a write costs

A statement whose lambdas write to a binding no longer depends only on its
input, so the optimizer leaves that statement exactly as written (see
[optimizer.md](optimizer.md)). Statements that do not write are unaffected.

`domain expansion: visualize --expressions` shows what each subexpression
evaluated to by running the lambda again; it declines to do that for a lambda
that writes.

## Running an expression for its effect — `also`

`BODY also C1, C2, …` evaluates `BODY`, then each clause in order, throws the
clauses' values away, and gives `BODY`'s value. It is where you put a write
whose value is not the point:

```domain
Cursed Technique: Map Each          # each element, plus how many came before it
    Consider seen As 0
    Using: (x) -> x + seen also seen := seen + 1
```

**The body's value is taken before the clauses run.** A clause changes what
the *next* reader of the name sees, not what this expression has already
produced. Inside parentheses, the next reader can be the rest of the same
expression:

```
(x also n := n + 1) + n      # the old x, plus the new n
```

`also` binds more loosely than anything else, and its clause list runs to the
end of the expression. Two places would be ambiguous, and need parentheses:

- **inside a function call's arguments**, where the clause commas and the
  argument commas would be confused — write `f((a also b), c)`;
- **two at the same level** — write `(a also b) also c`.

So a bare `also` goes where something else already ends the expression: the
end of a lambda body, the end of a `Consider … As` value, or a closing
parenthesis.

The clauses are type-checked like any expression, and one that fails at run
time fails the whole expression.

## Builtin functions

There are 208 builtin functions. They behave identically whether the program
is interpreted or compiled, down to the wording of their error messages.

| Page | Covers |
|---|---|
| [ref-builtins-list.md](ref-builtins-list.md) | Lists, tuples and grids: indexing, slicing, sorting, zipping, chunking |
| [ref-builtins-collections.md](ref-builtins-collections.md) | Maps and Sets: reading them, building and updating them |
| [ref-builtins-math.md](ref-builtins-math.md) | Integer maths and floats |
| [ref-builtins-text.md](ref-builtins-text.md) | Text |
| [ref-builtins-bits.md](ref-builtins-bits.md) | Bit operations, logic and number theory |
| [ref-builtins-records.md](ref-builtins-records.md) | Records, points and grid geometry, sparse grids |
| [ref-builtins-view.md](ref-builtins-view.md) | Building, combining and styling a picture (for games) |
| [ref-builtins-chance.md](ref-builtins-chance.md) | Random numbers and the clock |

Each page has worked examples for its group.

## What the expression layer does not have

- **Functions that take functions.** No builtin maps, filters or searches with
  a lambda you supply. Operations that need no lambda do exist as builtins —
  `sort`, `unique`, `flatten`, `product`, `zip`, `enumerate`, `chunk`,
  `windows`, `transpose`
  ([list builtins](ref-builtins-list.md#first-order-list-operations)). For
  anything that needs one, use a
  [pipeline body](#pipeline-bodies--a-using-that-needs-a-primitive).
- **User-defined functions.** A `Consider f As (x) -> …` binding gives you a
  helper for one statement. For anything larger, define a
  [Shikigami](language.md#shikigami--user-defined-operations), which works on
  the pipeline.
- **Recursion.** Use `Domain Expansion: Explore`
  ([reference](ref-expansions.md)) for searching and counting over a space of
  states.
