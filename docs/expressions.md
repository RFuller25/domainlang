# Expression layer reference

The expression layer is the plain (non-themed) language inside `Using:`
lambdas. It is deliberately small, statically typed at resolve time, and
compiled to bare Go expressions by the compiler backend — a lambda costs no
closure, no boxing, no dispatch in a built binary.

## Lambdas

```
(a, b) -> a + b = 2020
(r) -> (r.a <= r.c and r.b >= r.d) or (r.c <= r.a and r.d >= r.b)
(g) -> sum(take(reverse(g), 2))
```

A lambda's arity is fixed by the consuming primitive (1 for `Map Each` /
`Filter` / `Apply` / `Group By` / cell predicates; 2 for `Fold`; k for
`Combinations k`; one per channel for `Combine`). Parameter types come from
the pipeline's current type, and the body's result type is inferred — a
predicate position requires `Bool`, `Map Each` produces `List<body type>`,
and so on.

One parameter, in a `Map Each`; the body's type decides the result's element
type:

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

Two parameters, because the consuming primitive fixes the arity — the lambda
never declares it:

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

## Writing an expression across lines

An expression that has outgrown its line breaks in one of two places, and both
mean the same thing: the expression is not finished yet.

**Inside a parenthesis**, a newline is whitespace. The reader can already see
the expression is unfinished, so nothing else has to say so, and the
indentation of the continued lines is alignment rather than layout — it is
yours to arrange, and `domain fmt` leaves it alone (shifting it only when the
line that opened the parenthesis moves):

```domain
Cursed Technique: Map Each
    Using: (p) -> min(list(
        manhattan(p, point(0, 0)),
        manhattan(p, point(9, 9))
    ))
```

**Indented under the argument**, the rest of the expression continues on the
lines below it. This is the form for the outermost level, which has no
parentheses to break inside — a `consider … in if … then … else …` is one
unbracketed expression however long it gets:

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

Everything indented past the argument's own line is part of its value, however
deep — the `then` and `else` arms above are indented again purely for reading.
The block ends where the indentation returns, so the statements after it are
unaffected, and the value may start on the line below the argument name
(`Using:` alone, then the lambda) when that reads better.

Both forms are just line breaks: they change nothing about how the expression
is parsed, typed, evaluated or compiled. In the REPL they behave like every
other indented block — keep typing, finish with a blank line.

## Pipeline bodies — a `Using:` that needs a primitive

A body stands in wherever a one-parameter `Using:` lambda is accepted, which
is how a per-element job reaches a primitive — the expression layer cannot
iterate, so there is no lambda that searches an element which is itself a
list:

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

It is not a `Map Each` feature — a `Filter` predicate takes one too, so long
as the body ends in a `Bool`:

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


The expression layer has no higher-order builtins: nothing here maps, filters,
or searches. So once a lambda's parameter is itself a list, a job that needs a
*primitive* has no expression spelling at all — `All Pairs` over each row of a
`List<List<Int>>` cannot be written as `(row) -> ...` because there is no
expression that searches `row`.

**Indent a pipeline where the lambda would go** and it runs in the lambda's
place, with the value the parameter would have bound as its current value:

```domain
Cursed Technique: Map Each          # List<List<Int>> -> List<Int>
    Domain Expansion: All Pairs
        Mode: First
        Using: (a, b) -> a + b = 2020
    Maximum Technique: Product
```

This is not a `Map Each` feature. A lambda `(x) -> e` and a sub-pipeline both
turn one value into one value, so a body stands in **wherever a 1-parameter
`Using:` lambda is accepted** — `Filter` and the other predicates, `Sort By`,
`Group By`, `Count By`, `Sum By`, `Min By`/`Max By`, `Any`/`All`, `Find`,
`Partition`, `Take While`/`Drop While`, `Map Values`, `Apply`, `Iterate`,
`Explore`, the grid searches, and `Map Each`:

```domain
Cursed Technique: Filter            # keep the rows summing over 15
    Maximum Technique: Sum
    Cursed Technique: Apply
        Using: (s) -> s > 15

Domain Expansion: Sort By           # order rows by their total
    Maximum Technique: Sum
```

The body's result type is the lambda's result type, so the usual rule applies
unchanged: a predicate position needs a body ending in `Bool`, `Map Each`
produces `List<body type>`.

**More than one parameter: `Params:`.** A body still computes one value from
one value, so a lambda of two or more parameters has exactly one it can be the
body *of* — and `Params:` says what the rest are called:

```domain ignore
Maximum Technique: Fold            # a fold whose step needs a primitive
    Seed: (xs) -> 0
    Params: acc, row
    Domain Expansion: Sort
    Cursed Technique: Apply
        Using: (r) -> acc + first(r)
```

The **last** name is what the body is over. For a fold that reads the way the
lambda does: the body is a pipeline over the element, producing the new
accumulator, and `acc` is the value carried in rather than the one being
transformed.

**Every** name is also readable, the last one included — it is the body's
current value *and* in scope by name, so a `Params:` name never turns out to
be decoration. They arrive through the same machinery
[`Consider`](#stage-bindings--consider--as--consider--of) uses, so an outer
binding and a body parameter sit side by side and shadow in the usual order,
and a `Params:` name obeys the rules a binding name does: it may not shadow an
expression builtin, and two parameters may not share a spelling.

Without this, a `Fold` whose step needed to sort, group or search could not be
written at all: the expression layer has no lambda-taking builtins, so there
was nothing to reach for inside the lambda either.

**What it cannot do.** A stage with no `Using:` lambda at all refuses a body
rather than ignoring it. Supplying both a lambda and a body is an error, and so
is a `Params:` beside a written lambda — that lambda already names its own
parameters, and silently ignoring the line would let a program say one thing
and do another.

A body is a nested scope like a loop's: bodies nest, an enclosing `For` loop's
ambient variable is in scope inside one, and `Channel` definitions and `From:`
consumers are refused. The optimizer treats it like any other sub-pipeline —
in-place rewrites, algorithm substitution included, fire inside it (see
[optimizer.md](optimizer.md)).

## Grammar

Primary expressions: integer literals, double-quoted string literals,
identifiers (lambda parameters), parenthesized expressions, **record literals**
(`{a: 1, b: x}`), field access (`expr.name`), and builtin calls
(`name(args...)`). Unary minus negates an Int.

A record literal is written

```
record := '{' IDENT ':' expr (',' IDENT ':' expr)* '}'
```

and is exactly the `record(...)` call it parses to — `{a: 1}` *is*
`record("a", 1)`, so the two spellings share one set of rules and `domain fmt`
gives each back as written. Field names are bare identifiers rather than
expressions because the record's type depends on them. See
[ref-builtins-records.md](ref-builtins-records.md).

Braces are only record literals. `{1, 2}` and `{"a": 1}` — the set and map
spellings the values themselves print as — are refused by name, not by a
generic syntax error, so the syntax stays available; build those with `toset`
and `tomap`.

Binary operators, loosest-binding first (left-associative unless noted):

| Precedence | Operators | Operands | Result |
|---|---|---|---|
| 0 (loosest) | `also` (postfix) | any | the body's type |
| 0.5 | `:=` (**right**-associative) | the name's type | the value written |
| 1 | `or` | Bool | Bool |
| 2 | `and` | Bool | Bool |
| 2.5 | `ikke` (prefix) | Bool | Bool |
| 3 | `=` `<` `>` `<=` `>=` | see below | Bool |
| 4 | `+` `-` | Int, Float; `+` also Text | Int, Float, Text |
| 5 (tightest) | `*` `/` `%` | Int, Float (`%` Int only) | Int, Float |

Notes:

- **`=` is equality, always.** Assignment is spelled `:=`, and it writes to a
  name that is already bound rather than introducing one.
- **`ikke` is negation** (Norwegian for "not"). It binds looser than a
  comparison and tighter than `and`, so `ikke a = b` reads as `ikke (a = b)`
  and `ikke a and b` as `(ikke a) and b`.
- **`+` over two Texts concatenates.** It is the one non-numeric operator
  overload; everything else stays arithmetic.
- **`%` is Euclidean modulo**, not Go's truncated remainder: the result is
  non-negative for a positive modulus whatever the sign of the left operand,
  so `(0 - 1) % 5` is `4`. Wrap-around indexing is the dominant use and
  truncated remainder gets it wrong at exactly the interesting boundary. It
  binds like `*` and `/`, so `0 - 1 % 5` is `0 - (1 % 5)`. Modulo by zero is a
  clean runtime error. `mod(a, b)` is the same operation as a builtin.
- `=` compares any two values of the same static type — scalars by value,
  composites (List/Record/Tuple/Grid/Map/Set) structurally.
- `<` `<=` `>` `>=` compare any two values of the same **ordered** type: Int,
  Float, Text, or a Tuple of those (plus mixed Int/Float, which compares
  through the numeric tower's promotion). That is exactly the reach of
  `Sort`/`Sort By`/`Min By`/`Max By`, over the same ordering — Text
  lexicographically (byte-wise, which agrees with rune order), a Tuple by its
  first differing element. Anything else — Bool, Record, List, Map, Set, Grid,
  Sparse — is a resolve error saying so. Ordered is deliberately narrower than
  keyable: a Record is a legal Map key but has no ordering, because its fields
  have names rather than positions.
- `and` / `or` short-circuit, so guard idioms are safe:
  `n = 0 or 10 / n = 5` never divides by zero.
- `/` is integer division truncating toward zero; division by zero is a
  clean runtime error in both backends.
- Field access requires a Record (from a named-hole `Match Pattern`):
  `m.n`, `r.a`. Unknown fields are resolve-time errors.

## Conditional expressions

```
if cond then a else b
if length(xs) = 0 then -1 else first(xs)
if n < 0 then "neg" else if n = 0 then "zero" else "pos"
```

The condition must be `Bool` and both arms must share one type (the
result type). **Arms are lazy** in both backends: only the selected arm is
evaluated, so the guard idiom above never trips `first` on an empty list —
the compiler lowers the conditional to an inlined Go `if`, not an eager
helper call. `if`/`then`/`else` are contextual keywords inside expressions;
arms extend as far right as possible (`if c then a else b + 1` puts `b + 1`
in the else arm — parenthesize to override).

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

Both arms must have the same type, since the expression has one type whichever
way it goes — and they chain, which is how a multi-way choice is written:

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


## Local bindings — `consider`

```
consider d as manhattan(a, b) in if d < 3 then d else 0
consider lo as min(a, b) in consider hi as max(a, b) in hi - lo
```

`consider NAME as VALUE in BODY` names a subexpression. The value is evaluated
**exactly once** and `NAME` is in scope only inside `BODY`.

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

They nest, which is how more than one name is introduced:

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

Without it a repeated subexpression has to be written — and computed — twice,
since lambda-body CSE is a candidate optimizer pass rather than an implemented
one.

Like `if`/`then`/`else`, the three words are contextual: they stay usable as
ordinary identifiers everywhere else. Bindings nest, the body extends as far
right as possible, and an inner binding shadows an outer one (and a lambda
parameter) for its body only. The compiler lowers it to a Go local, so it
costs nothing at runtime.

## Updating a local — `:=`

`NAME := VALUE` writes to a name already in scope and **yields the value it
wrote**, so the update can sit in the middle of the expression that needs it:

```
consider t as x * 2 in (t := t + 1) * 10
```

Two kinds of name can be written to, and the difference is how long the write
lives:

| Target | The write lives |
|---|---|
| a `consider` local | until the expression ends — it cannot escape one evaluation of the lambda |
| a `Consider … As/Of` **stage binding** | for the whole stage: the next element sees it, and so does the next lap of a loop |

Everything else is refused where it is written, because in each case there is
nothing to write *to*: a **lambda parameter** is bound afresh by whatever
applies the lambda, a **function binding** (`Consider f As (x) -> …`) is
inlined at its call sites, and a **Shikigami parameter** is substituted into
the body as a literal. Each says so, and says what to write instead.

The type may not change. A binding's type is fixed when its scope opens and is
what every other expression in that scope was typed against, so `n := 1.5` on
an `Int` binding is an error rather than a widening — widen at the binding.

**Order is defined and it matters.** Operands and arguments are evaluated left
to right, so `n + (n := x) + n` reads the old `n`, writes, and reads the new
one. Both backends agree on this; the compiler emits explicit sequencing to
guarantee it, since Go's own evaluation order does not.

Writing to a **stage binding** is the kind that carries: it is the one value
that survives from one element to the next without a `Fold`.

> **Known bug — writing to a `consider` local does not work.** The form below
> is the intended one and is what this section describes, but every spelling of
> it currently fails at run time with `"t" cannot be updated here`:
>
> ```domain ignore
> Using: (x) -> consider t as x * 2 in (t := t + 1) * 10
> ```
>
> The cause is that two different questions share one answer.
> `ast.UpdatedNames` deliberately drops a `consider` local's own name, since a
> write there is a write to the local rather than to any stage binding outside
> it — correct for the question it was written for. But `programUpdates` in
> `prims/prims.go` reuses it to decide whether to call `eval.EnableUpdates()`
> at all, so a program whose only `:=` targets a local never turns updates on,
> the local is stored as a bare value instead of a `*Cell`, and `assignTo`
> reaches the branch its own comment calls unreachable. Use a stage binding
> until it is fixed.

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

A stage binding's write is visible to the next element, which is what makes
the running total above accumulate rather than reset:

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


**A write that is not reached does not happen.** `and`/`or` short-circuit and
`if` arms are lazy in both backends, so the write in `x > 100 and (n := 1) > 0`
never runs when the left side is false. That is the same laziness the guard
idiom depends on; it is worth knowing when the right-hand side is doing work
rather than answering a question.

### What it gives up

A stage whose lambdas write to a binding is no longer a pure function of its
input, and the optimizer stands its rewrites down for that stage: no algorithm
substitution, no fusion with a neighbour, no expression simplification (see
[optimizer.md](optimizer.md)). The binding itself also stops being folded into
the lambdas that read it — a constant substituted as a literal has nowhere to
put a new value — which is the same trade a `Consider … Of` binding has always
made. Stages that do not write are untouched.

`domain expansion: visualize --expressions` replays an application to show what
each subexpression came to, so it declines a lambda that writes: re-running the
body would do the writing a second time.

## Running an expression for its effect — `also`

`BODY also C1, C2, …` evaluates the body, then each clause in written order,
**discards the clauses' values**, and yields the body's. Its type is the body's
type; the clauses may be any type but still have to typecheck, and one that
fails at runtime fails the expression.

```domain
Cursed Technique: Map Each          # each element, and how many came before it
    Consider seen As 0
    Using: (x) -> x + seen also seen := seen + 1
```

The body's value is taken *before* the clauses run, which is the whole point of
the ordering: a clause updates what the **next** reader of the name sees, not
what this expression already yielded. Written inside a parenthesis, that next
reader can be the rest of the same expression:

```
(x also n := n + 1) + n      # the old x, plus the new n
```

`also` binds looser than everything, and its clause list runs to the end of the
expression. That leaves two places it cannot be read without guessing, and both
are refused rather than guessed:

- **inside a call's arguments**, where the clause commas and the argument
  commas are the same character — write `f((a also b), c)`;
- **twice at one level** — write `(a also b) also c`.

So a bare `also` list is written where something else already ends it: at the
end of a lambda body, a `Consider … As` value, or a parenthesis.

`also` carries no update of its own — a clause that writes nothing is evaluated
and discarded, which is legal and does nothing. It is the place to put the
writes whose *value* is not what the expression is for.

A `Using:` written as an [indented
pipeline](#pipeline-bodies--a-using-that-needs-a-primitive) writes through like
any other lambda. It compiles to a Go function of its own, and a binding it
updates reaches that function as a pointer rather than a copy, so the write
lands on the binding in both backends — including from a body nested inside
another.

## Stage bindings — `Consider … As` / `Consider … Of`

`Of` applies an operation to the value entering the stage, so a whole-list
statistic is available to a per-element lambda:

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

`As` never sees the pipeline value; it names a constant or a function, and a
function binding is inlined at its call sites:

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


`consider` names a subexpression *inside* one expression. A **stage binding**
names a value for a whole pipeline stage — every lambda on it, and every
statement nested beneath it — and it is written on the pipeline layer, in the
stage's indented block beside `Mode:` and `Using:`:

```domain
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Domain Expansion: All Pairs
    Mode: Count
    Consider accum As 3
    Consider double As (x) -> x * 2
    Consider total Of Sum
    Using: (a, b) -> double(a) + b + accum > total
Reveal: stdout
```

**The preposition says where the value comes from, and it has to, because a
1-parameter lambda already means two different things in Domain depending on
the slot it is written in**: a `Using:` lambda is applied per element, while a
measured argument's lambda (`Size: (xs) -> length(xs) / 2`) is applied once to
the current pipeline value. A binding has no slot to disambiguate it.

| Written | Binds | Computed |
|---|---|---|
| `Consider accum As 3` | a constant | at compile time |
| `Consider n As 2 * (k + 1)` | an expression over earlier bindings | at compile time when it folds, otherwise once per pass |
| `Consider len As (x) -> length(x)` | **a function** — call it as `len(xs)` | at each call site |
| `Consider total Of Sum` | an operation applied to the current value | once per pass through the stage |
| `Consider total Of (xs) -> sum(xs)` | the same, written as a lambda | once per pass through the stage |
| `Consider total Of` + an indented pipeline | a whole sub-pipeline over that value | once per pass through the stage |
| `Consider line Of Itself` | the value entering the scope, unchanged | once per pass through the stage |

**`As` never sees the pipeline value; `Of` always does.** That is the whole
rule. `Of` accepts an operation phrase, a lambda over the current value, or an
indented sub-pipeline — but not a bare expression, so `Of Sum` is
unambiguously the primitive and never an identifier:

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

A binding is in scope for **every lambda-valued argument of its statement** —
`Using:`, `By:`, `While:`, `Until:`, a measured `Times:` — and for every
statement nested beneath it, including a `Using:` written as an indented
pipeline. It goes out of scope with the statement.

Bindings are read in written order and each sees the ones above it, so
`Consider half As total / 2` is legal and a cycle cannot be written at all.
An inner block rebinding a name shadows the outer one; a lambda parameter of
that name shadows the binding, exactly as it shadows an outer `consider`. A
binding written at the top of a `Shikigami` body scopes over the whole body and
may use the definition's parameters.

An `Of` binding is computed once when its scope opens, from the value entering
**the statement it is written on** — which for a `Map Each` is the whole list,
not one element. That is worth stating outright, because it reads the other way
round for exactly the stage people reach for most: to name the *element*, put
the `Consider` on a statement inside the body, where the element is what
arrives.

At the head of a loop, "the stage" is the whole loop rather than one lap.

`Of Itself` is the value itself, unchanged. It exists because naming the
current value otherwise took an identity Apply —

```domain ignore
Consider line Of Apply          # the long way
    Using: (l) -> l

Consider line Of Itself         # the same binding
```

— which is pure ceremony, and exactly what a pipeline body needs to name its
own element. `domain expansion: optimize` rewrites the first into the second.
(It is a word rather than a bare `Consider line Of`, because that spelling is
how the REPL knows an indented sub-pipeline is still coming.)

### What a binding cannot be

A binding may not take an expression builtin's name: `Consider length As …` is
refused rather than allowed to change what `length(xs)` means for every
expression in scope.

A function binding must be **called**. Domain has no function values — there is
no type to give one — so `(a + b) * len` where `len` is a lambda is an error
that says so, and `len(list(a, b))` is what it is asking for. The same rule
from the other side: a value binding cannot be called.

### What it costs

Nothing, for the first two kinds. A constant is substituted into the lambdas
that read it as a literal, and a function is inlined at each call site (its
arguments bound with `consider`, so each is evaluated exactly once however many
times the body names it). Both are gone before either backend sees the program.

That is also why a constant is folded rather than bound: the optimizer matches
the *shape* of a lambda body, so `Consider target As 2020` + `(a, b) -> a + b =
target` still becomes the hash-set scan, while an `Of` binding — a value that
is not known until data arrives — stands the rewrite down the way a measured
argument does (see [optimizer.md](optimizer.md)).

A binding that something [updates with `:=`](#updating-a-local--) is the third
case, and it costs what the second one does: it is computed when its scope
opens rather than folded, because a literal substituted into a body has nowhere
to put a new value.

## Builtin functions

The fixed builtin table (`typecheck.Builtins`). Every builtin is implemented
in the interpreter **and** the compiler with identical behavior — the
*partial* ones fail with the same message wording in both, and the
point/tuple group compiles through the interned tuple structs (see
[compiler.md](compiler.md)).

The table is split by what a builtin operates on:

| Page | Covers |
|---|---|
| [ref-builtins-list.md](ref-builtins-list.md) | Lists, tuples and grids: indexing, slicing, and the first-order list operations |
| [ref-builtins-collections.md](ref-builtins-collections.md) | Maps and Sets: reading them, and building or updating a collection |
| [ref-builtins-math.md](ref-builtins-math.md) | Integer maths, number theory and floats |
| [ref-builtins-text.md](ref-builtins-text.md) | Text |
| [ref-builtins-bits.md](ref-builtins-bits.md) | Bit operations, logic and number theory |
| [ref-builtins-records.md](ref-builtins-records.md) | Records, points and grid geometry, sparse grids |

Each page carries two worked examples for its group, executed against their
printed output in both backends.

### Design rules for extending the table

Keep it small and total where possible; every new builtin lands in **three**
places (typecheck, eval, codegen) with unit tests in the first two and an
interpreter-vs-binary oracle program in the third. A builtin implemented in
eval but not codegen must fail compilation with a positioned error — never
produce differing output. Partial builtins must use identical failure
wording in both backends.

## What the expression layer does not have (yet)

- **Higher-order builtins.** Nothing here maps, filters or searches with a
  function you supply. The pure list transforms that need *no* function
  argument are no longer in that sentence — `sort`, `unique`, `flatten`,
  `product`, `zip`, `enumerate`, `chunk`, `windows` and `transpose` all have
  an expression spelling now ([above](ref-builtins-list.md#first-order-list-operations)). What
  still has none is anything taking a lambda: indent a pipeline where the
  lambda goes and the primitives do the work instead.
- **User-defined functions.** Shikigami operate at the pipeline layer instead,
  and are not recursive — see `Domain Expansion: Explore` in
  [primitives.md](primitives.md) for the search that replaces recursion.

(Historical gaps since closed: conditional expressions (`if/then/else`
above) and index-aware grid iteration — the positional `(g, r, c)` lambda
form of `Map Cells`/`Count Cells` with the `row`/`col`/`rows`/`cols`
builtins; floats; number-to-text conversion (`totext`); modulo and the
integer-math group; text access beyond parsing; the Map/Set escape hatches;
heterogeneous tuples; boolean negation (`ikke`); local bindings
(`consider`); **collection construction and update** — a Map, Set or dense
Grid could be read but never built, so a fold could not accumulate one —
along with list generation, text splitting and code points, the float tower
past `sqrt`, named-field records, and the base/bit/number-theory group; and,
most recently, **updating a name** ([`:=`](#updating-a-local--) and
[`also`](#running-an-expression-for-its-effect--also)), which is how a value
carried between elements stops having to be threaded through a `Fold`
accumulator.)
