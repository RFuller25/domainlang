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

**What it cannot do.** A body computes one value from one value, so it cannot
stand in for a lambda that takes two or more parameters — `Fold`, `Reduce`,
`Scan` and `All Pairs`/`Combinations k` need the lambda written out, and say
so with the arity named. A stage with no `Using:` lambda at all refuses a body
rather than ignoring it. Supplying both a lambda and a body is an error.

A body is a nested scope like a loop's: bodies nest, an enclosing `For` loop's
ambient variable is in scope inside one, and `Channel` definitions and `From:`
consumers are refused. The optimizer treats it like any other sub-pipeline —
in-place rewrites, algorithm substitution included, fire inside it (see
[optimizer.md](optimizer.md)).

## Grammar

Primary expressions: integer literals, double-quoted string literals,
identifiers (lambda parameters), parenthesized expressions, field access
(`expr.name`), and builtin calls (`name(args...)`). Unary minus negates an
Int.

Binary operators, loosest-binding first (all left-associative):

| Precedence | Operators | Operands | Result |
|---|---|---|---|
| 1 (loosest) | `or` | Bool | Bool |
| 2 | `and` | Bool | Bool |
| 2.5 | `ikke` (prefix) | Bool | Bool |
| 3 | `=` `<` `>` `<=` `>=` | see below | Bool |
| 4 | `+` `-` | Int, Float; `+` also Text | Int, Float, Text |
| 5 (tightest) | `*` `/` `%` | Int, Float (`%` Int only) | Int, Float |

Notes:

- **There is no assignment.** `=` is equality, always.
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
  composites (List/Record/Tuple/Grid/Map/Set) structurally. `<` `<=` `>` `>=`
  are Int-only.
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

## Local bindings — `consider`

```
consider d as manhattan(a, b) in if d < 3 then d else 0
consider lo as min(a, b) in consider hi as max(a, b) in hi - lo
```

`consider NAME as VALUE in BODY` names a subexpression. The value is evaluated
**exactly once** and `NAME` is in scope only inside `BODY`; without it a
repeated subexpression has to be written — and computed — twice, since
lambda-body CSE is a candidate optimizer pass rather than an implemented one.

Like `if`/`then`/`else`, the three words are contextual: they stay usable as
ordinary identifiers everywhere else. Bindings nest, the body extends as far
right as possible, and an inner binding shadows an outer one (and a lambda
parameter) for its body only. The compiler lowers it to a Go local, so it
costs nothing at runtime.

## Stage bindings — `Consider … As` / `Consider … Of`

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
it — including at the head of a loop, where "the stage" is the whole loop
rather than one lap.

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

## Builtin functions

The fixed builtin table (`typecheck.Builtins`). Every builtin is implemented
in the interpreter **and** the compiler with identical behavior — the
*partial* ones fail with the same message wording in both, and the
point/tuple group compiles through the interned tuple structs (see
[compiler.md](compiler.md)).

### Lists, maps, grids

| Builtin | Type | Behavior |
|---|---|---|
| `length(xs)` | `List<T> -> Int` | Number of elements. |
| `item(xs, i)` | `List<T> × Int -> T` | 0-based element. **Error** if `i` is out of range. |
| `take(xs, n)` | `List<T> × Int -> List<T>` | First `n` elements. Total: `n` clamps to `[0, length]`. |
| `drop(xs, n)` | `List<T> × Int -> List<T>` | All but the first `n`. Clamps like `take`. |
| `reverse(xs)` | `List<T> -> List<T>` | Reversed copy. |
| `concat(a, b)` | `List<T> × List<T> -> List<T>` | `a` then `b`. Both lists must share one type. |
| `first(xs)` | `List<T> -> T` | **Error** on the empty list. |
| `last(xs)` | `List<T> -> T` | **Error** on the empty list. |
| `sum(xs)` | `List<N> -> N` (N = Int or Float) | Total; `0` for the empty list. |
| `min(xs)` | `List<N> -> N` (N = Int or Float) | **Error** on the empty list. |
| `max(xs)` | `List<N> -> N` (N = Int or Float) | **Error** on the empty list. |
| `contains(xs, v)` | `List<T> \| Set<T> × T -> Bool` (T keyable) | Membership over a list or a set. Element type must be keyable (Int, Text, or Tuples/Records of them) — so a set of points works. |
| `get(m, k)` | `Map<K,V> × K -> V` | Lookup. **Error** if the key is absent. |
| `at(g, r, c)` | `Grid<T> \| Sparse<T> × Int × Int -> T` | 0-based `(row, col)` cell. Dense: **error** out of bounds. Sparse: total — unset cells read the default. |
| `inbounds(g, r, c)` | `Grid<T> × Int × Int -> Bool` | Whether `(r, c)` is a legal cell. Pairs with `at` under short-circuit `and`. |
| `list(a, b, …)` | `T × … -> List<T>` (≥ 1 arg) | Construct a list; all elements must share one type. |
| `set(xs, i, v)` | `List<T> × Int × T -> List<T>` | Copy of `xs` with element `i` replaced (functional update). **Error** if `i` is out of range. |
| `row(g, r)` | `Grid<T> × Int -> List<T>` | Row `r` as a list. **Error** out of range. |
| `col(g, c)` | `Grid<T> × Int -> List<T>` | Column `c` as a list. **Error** out of range. |
| `rows(g)` / `cols(g)` | `Grid<T> -> Int` | The grid's dimensions. |
| `slice(xs, lo, hi)` | `List<T> × Int × Int -> List<T>` | Half-open `[lo, hi)`. Total: bounds clamp like `take`/`drop`, and an inverted range gives the empty list. |
| `indexof(xs, v)` | `List<T> × T -> Int` (T keyable) | Position of the first equal element, or `-1` — the sentinel `Find Index` uses. |
| `tuple(a, b, …)` | `T1 × T2 × … -> (T1, T2, …)` (≥ 2 args) | Build a **heterogeneous** tuple. Unlike `list`, the elements need not share a type — this is how a mixed `Group By` key or a `Sort By` tiebreak is written. |
| `range(lo, hi)` | `Int × Int -> List<Int>` | The half-open `[lo, hi)`, matching the `Range` primitive. Empty when `hi <= lo`. |
| `fill(n, v)` | `Int × T -> List<T>` | `n` copies of `v`. Total: a negative count is the empty list, like `take`. |
| `item(t, i)` | `(T1, …) × Int -> Ti` | Tuple element. The index must be a **literal**: the elements have different types, so the result type is only knowable when the position is. Compiles to a direct struct field. |
| `length(t)` | `(T1, …) -> Int` | A tuple's arity. |

### Maps and Sets

`Group By` and `Count By` are among the most reachable primitives and both
produce a `Map`. Until v0.5 the only Map builtin was `get`, which **errors** on
a missing key with no way to guard it — so a frequency map could not safely be
queried at all.

| Builtin | Type | Behavior |
|---|---|---|
| `haskey(m, k)` | `Map<K,V> × K -> Bool` | Whether the key is present. The guard `get` never had. |
| `getor(m, k, d)` | `Map<K,V> × K × V -> V` | Total lookup: the value, or `d` when absent. |
| `keys(m)` | `Map<K,V> -> List<K>` | Keys in insertion order. |
| `values(m)` | `Map<K,V> -> List<V>` | Values in the same order. |
| `size(m)` | `Map<K,V> \| Set<T> -> Int` | Entry count — `Count`, without leaving the lambda. |
| `tolist(s)` | `Set<T> -> List<T>` | Elements in insertion order. Without it a `Set` is a dead end: `Map Each` has no Set case. |

### Building and updating a collection

Until v0.6 every collection but `Sparse` was **read-only** from inside an
expression. That is why a sparse automaton was writable as a `Fold` and a
frequency map was not: `sparse(d)` and `put` were the only constructor and
functional update in the table, so an accumulator that was a `Map`, a `Set` or a
dense `Grid` could not be built at all — not even through a measured `Seed:`,
which is itself an expression.

Every update below is **functional**: it returns a new collection and leaves its
argument untouched. That is not a stylistic choice — a lambda may be applied to
the same value twice (the optimizer folds constants by doing exactly that), and
an in-place update would let the second application see the first one's work.

| Builtin | Type | Behavior |
|---|---|---|
| `toset(xs)` | `List<T> -> Set<T>` (T keyable) | Deduplicates, keeping first-seen order — `Convert To Set` inside a lambda. |
| `emptyset(v)` | `T -> Set<T>` | The empty set. `v` is a **type witness**, never stored: an absence cannot say what it is an absence of, which is the same reason `sparse(d)` takes a default. |
| `tomap(ps)` | `List<(K,V)> -> Map<K,V>` (K keyable) | Builds from key/value pairs; last write wins. |
| `emptymap(k, v)` | `K × V -> Map<K,V>` | The empty map; both arguments are type witnesses. |
| `entries(m)` | `Map<K,V> -> List<(K,V)>` | Pairs in insertion order — the inverse of `tomap`. |
| `insert(s, v)` | `Set<T> × T -> Set<T>` | A copy with `v` added. |
| `insert(m, k, v)` | `Map<K,V> × K × V -> Map<K,V>` | A copy with `k` bound to `v`. |
| `del(s, v)` | `Set<T> × T -> Set<T>` | A copy without `v`. Absent is not an error. |
| `del(m, k)` | `Map<K,V> × K -> Map<K,V>` | A copy without `k`. Absent is not an error. |
| `union(a, b)` | `Set<T> × Set<T> -> Set<T>` | All of `a`, then `b`'s new elements. |
| `intersect(a, b)` | `Set<T> × Set<T> -> Set<T>` | Elements in both, in `a`'s order. |
| `difference(a, b)` | `Set<T> × Set<T> -> Set<T>` | Elements of `a` not in `b`. |
| `setat(g, r, c, v)` | `Grid<T> × Int × Int × T -> Grid<T>` | A copy with cell `(r, c)` replaced. **Error** out of bounds — a dense grid is finite, so unlike `put` there is no cell to write. |
| `cellpoints(g)` | `Sparse<T> -> List<(Int, Int)>` | The set cells' coordinates in sorted row-major order. `cells` counts them; this is what walks them. |

So a fold can now carry real state:

```domain
Maximum Technique: Fold                  # a frequency map, in one lambda
    Seed: (xs) -> emptymap("", 0)
    Using: (acc, w) -> insert(acc, w, getor(acc, w, 0) + 1)
```

Each update copies, so a fold over *n* elements is O(n·size). That is the price
of a value-semantics accumulator and it is the right shape for the sizes these
programs work at; a genuinely large aggregation still belongs in `Count By` or
`Group By`, which build one collection in place.

### Math / number theory

| Builtin | Type | Behavior |
|---|---|---|
| `abs(n)` | `Int -> Int` | Absolute value. |
| `sign(n)` | `Int -> Int` | `-1`, `0`, or `1`. |
| `gcd(a, b)` | `Int × Int -> Int` | Non-negative greatest common divisor; `gcd(0, 0) = 0`. |
| `lcm(a, b)` | `Int × Int -> Int` | Non-negative least common multiple; `lcm(a, 0) = 0`. |
| `modpow(b, e, m)` | `Int × Int × Int -> Int` | `b^e mod m` by binary exponentiation, result in `[0, m)`. **Error** if `e < 0` or `m <= 0`. |
| `modinv(a, m)` | `Int × Int -> Int` | Multiplicative inverse of `a` mod `m`, in `[0, m)`. **Error** if `m <= 0` or `a` and `m` are not coprime. |
| `solve2x2(a, b, c, d, e, f)` | `Int × … -> (Int, Int)` | Solves `a·x + b·y = c`, `d·x + e·y = f` (Cramer). **Error** when the determinant is zero or the solution is not integral. |
| `mod(a, b)` | `Int × Int -> Int` | Euclidean modulo — the `%` operator as a function. Non-negative for a positive modulus whatever the sign of `a`. **Error** on a zero modulus. |
| `divmod(a, b)` | `Int × Int -> (Int, Int)` | Quotient and remainder together, matching `mod`: `q*b + r = a` holds for negative `a` too. |
| `pow(b, e)` | `Int × Int -> Int` | Exponentiation by squaring. **Error** on a negative exponent (there are no rationals to answer with). |
| `isqrt(n)` | `Int -> Int` | Integer square root: the largest `k` with `k*k <= n`. Exact at a perfect square, where `sqrt` rounds. **Error** on negative input. |
| `clamp(v, lo, hi)` | polymorphic over Int/Float | `v` confined to `[lo, hi]`. **Error** when `lo > hi`. |
| `factorial(n)` | `Int -> Int` | **Error** past `20!`, which overflows Int — a wrapped factorial is a wrong answer that looks right. |
| `choose(n, k)` | `Int × Int -> Int` | Binomial coefficient, computed multiplicatively so it stays in range far past where `factorial` overflows. `0` when `k` is out of range. |
| `min(a, b)` / `max(a, b)` | `N × N -> N` | The two-argument scalar form, beside the one-argument list reductions above. |

### Floats

Arithmetic, comparisons, and `=` accept any mix of `Int` and `Float`; a mixed
expression computes in `Float` (the numeric tower's single promotion rule).
Division by zero is a clean error for both. `abs` is polymorphic.

| Builtin | Type | Behavior |
|---|---|---|
| `tofloat(x)` | `Int \| Float \| Text -> Float` | Widen or parse. **Error** if the text is not a number. |
| `floor(f)` | `Float -> Int` | Largest integer ≤ f. |
| `ceil(f)` | `Float -> Int` | Smallest integer ≥ f. |
| `round(f)` | `Float -> Int` | Half away from zero. |
| `trunc(x)` | `Int \| Float -> Int` | Toward zero. Identity on an Int. |
| `sqrt(x)` | `Int \| Float -> Float` | **Error** on negative input. |
| `log(x)` | `Int \| Float -> Float` | Natural logarithm. **Error** on a non-positive input. |
| `log2(x)` / `log10(x)` | `Int \| Float -> Float` | Base 2 / base 10, same rules. |
| `exp(x)` | `Int \| Float -> Float` | e^x. |
| `sin(x)` / `cos(x)` / `tan(x)` | `Int \| Float -> Float` | Radians. |
| `atan2(y, x)` | `Int \| Float × … -> Float` | The angle of `(x, y)`, quadrant-aware. |
| `hypot(a, b)` | `Int \| Float × … -> Float` | `sqrt(a² + b²)` without the intermediate overflow. |

`pow` follows the operators' promotion rule rather than staying integral:
`pow(2, 10)` is the `Int` 1024, `pow(x, 0.5)` is the square root it looks like.
`Int × Int` was the whole of it before v0.6, so nothing that used to typecheck
changed meaning.

**There is no infinity and no NaN.** Neither can be written and neither prints
usefully, so a computation that leaves the reals is an **error where it
happens** rather than a poison value that surfaces three stages later:
`log(0)`, `exp(1000)` and `tan` at a pole all fail with a positioned message.

### Text

| Builtin | Type | Behavior |
|---|---|---|
| `toint(s)` | `Text -> Int` | Parse (whitespace-tolerant). **Error** if not an integer. |
| `totext(n)` | `Int \| Float -> Text` | Render a number exactly as `Reveal` would (shortest round-trip form for floats). |
| `occurrences(s, sub)` | `Text × Text -> Int` | Non-overlapping occurrences of `sub` in `s` (Go `strings.Count` semantics, including the empty-substring corner: `len+1`). |
| `repeats(s)` | `Text -> Bool` | Whether `s` is a shorter pattern repeated ≥ 2 times (`"abab"`, `"aaa"`). |
| `length(s)` | `Text -> Int` | Number of **runes**. |
| `slice(s, lo, hi)` | `Text × Int × Int -> Text` | Half-open substring, clamped like the list form. |
| `charat(s, i)` | `Text × Int -> Text` | The rune at `i`, as a 1-character Text. **Error** out of range, like `item`. |
| `chars(s)` | `Text -> List<Text>` | The runes — the expression layer's `Split Text by ""`. |
| `indexof(s, sub)` | `Text × Text -> Int` | Rune position of the first occurrence, or `-1`. |
| `startswith(s, p)` / `endswith(s, p)` | `Text × Text -> Bool` | Prefix / suffix test. |
| `replace(s, old, new)` | `Text × Text × Text -> Text` | Every occurrence. |
| `trim(s)` | `Text -> Text` | Leading and trailing whitespace removed. |
| `upper(s)` / `lower(s)` | `Text -> Text` | Case folding. |
| `textjoin(xs, sep)` | `List<Text> × Text -> Text` | The expression layer's `Join`. |
| `split(s, sep)` | `Text × Text -> List<Text>` | The expression layer's `Split Text by`. An empty separator splits into runes, like `chars`. Line splitting is `split(s, "\n")` — the pipeline layer's `Lines` Shikigami is the same operation. |
| `words(s)` | `Text -> List<Text>` | Split on runs of whitespace, dropping empties. |
| `contains(s, sub)` | `Text × Text -> Bool` | Substring test. `indexof(s, sub) >= 0` said the same thing, but a membership question should read the same whatever it is asked of. |
| `ord(s)` | `Text -> Int` | The first rune's code point. **Error** on the empty text. `ord(c) - ord("a")` is the a–z index that used to need an `indexof` over a literal alphabet. |
| `chr(n)` | `Int -> Text` | The character with code point `n`. **Error** outside a valid code point. |
| `repeat(s, n)` | `Text × Int -> Text` | `n` copies. Total: a non-positive count is `""`. Not to be confused with `repeats(s)`, which asks whether `s` *is* a repetition. |
| `padleft(s, n, p)` / `padright(s, n, p)` | `Text × Int × Text -> Text` | Widen to `n` **runes** by repeating `p` on one side, truncating the last copy. Text already that wide is returned untouched. |
| `trimprefix(s, p)` / `trimsuffix(s, p)` | `Text × Text -> Text` | Remove `p` if present — the counterparts to `startswith`/`endswith`. |
| `isdigit(s)` | `Text -> Bool` | Every rune is a decimal digit. The empty text is **false**: "every rune is a digit" is vacuously true of it, which is never what a guard means. |
| `isalpha(s)` | `Text -> Bool` | Every rune is a letter, same empty rule. |
| `isupper(s)` / `islower(s)` | `Text -> Bool` | No rune of the opposite case, and at least one cased rune — so `"AB1"` is upper and `"1"` is neither. |

**Positions count runes, not bytes**, everywhere — `length`, `charat`, `slice`
and `indexof` agree with each other and with `Split Text by ""`, so an index
means the same thing in both layers on non-ASCII input.

### Bit operations

| Builtin | Type | Behavior |
|---|---|---|
| `band(a, b)` / `bor(a, b)` / `bxor(a, b)` | `Int × Int -> Int` | Bitwise and / or / xor. |
| `bnot(n)` | `Int -> Int` | Bitwise complement. |
| `shl(a, n)` / `shr(a, n)` | `Int × Int -> Int` | Left / arithmetic right shift. **Error** on a negative shift count. |
| `popcount(n)` | `Int -> Int` | Set bits in the two's-complement representation. |
| `testbit(n, i)` | `Int × Int -> Bool` | Whether bit `i` is set. **Error** outside `0`–`63`. |
| `frombin(s)` | `Text -> Int` | Parse a binary string (whitespace-tolerant) — the 2021 D3 diagnostic parse. **Error** if not binary. |
| `frombase(s, b)` | `Text × Int -> Int` | Parse in base `b` (2–36), sign allowed. **Error** on a bad base or an unparseable string. |
| `fromhex(s)` | `Text -> Int` | Base 16, tolerating a `0x` prefix. |
| `tobase(n, b)` | `Int × Int -> Text` | Render in base `b` (2–36). |
| `tohex(n)` / `tobin(n)` | `Int -> Text` | Base 16 / base 2. |

### Number theory

| Builtin | Type | Behavior |
|---|---|---|
| `isprime(n)` | `Int -> Bool` | **Exact**, not probabilistic: deterministic Miller-Rabin over a witness set that settles every `Int`. O(log³ n), because a 19-digit Int is legal to write and trial division would be three billion divisions. |
| `divisors(n)` | `Int -> List<Int>` | Every positive divisor, ascending. **Error** on a non-positive input (zero has infinitely many). One pass to √n, with no sort. |
| `digits(n)` | `Int -> List<Int>` | The decimal digits of `\|n\|`, most significant first; `0` is `[0]`. |
| `fromdigits(ds)` | `List<Int> -> Int` | The number those digits spell — the inverse of `digits`. **Error** on an element outside 0–9, or on overflow (a silently wrapped number is a wrong answer that looks right). |
| `crt(rs, ms)` | `List<Int> × List<Int> -> Int` | The smallest non-negative `x` with `x ≡ rs[i] (mod ms[i])` for every `i`. The moduli need **not** be coprime: each pair is checked for agreement modulo their gcd and merged on their lcm, so a system read out of a puzzle works rather than only one constructed to be coprime. **Error** on an inconsistent system. |

### Records

A `Record` is named fields. `Match Pattern` produces one; `record` builds one,
which is what lets a fold carry a **named** accumulator instead of a positional
tuple whose `item(acc, 2)` nobody can read.

| Builtin | Type | Behavior |
|---|---|---|
| `record("a", x, "b", y, …)` | `Text × T1 × … -> {a:T1, b:T2, …}` | Build a record from name/value pairs. Field names must be **literals** and the argument count must be even. |
| `with(r, "a", v)` | `{a:T, …} × Text × T -> {a:T, …}` | A copy with field `a` replaced. The name is a literal and must already exist; the type is unchanged. |

The names are literals for the same reason `item(t, 0)` over a tuple needs a
literal index: the result type is only knowable when they are. That also means
`record` needs **no new syntax** — no braces, no new argument form in the
grammar — and both it and `with` compile to a plain Go struct literal and a
struct assignment, so a named accumulator costs nothing a tuple did not.

```domain
Maximum Technique: Fold
    Seed: (xs) -> record("lo", 0, "hi", 0)
    Using: (acc, n) -> with(with(acc, "lo", min(acc.lo, n)), "hi", max(acc.hi, n))
```

### Points and grid geometry

A **point** is an `(Int, Int)` tuple of `(row, col)` — the same coordinate
system grids use, and exactly what `Find Cells` and these builtins produce.

| Builtin | Type | Behavior |
|---|---|---|
| `point(r, c)` | `Int × Int -> (Int, Int)` | Construct a point. |
| `prow(p)` / `pcol(p)` | `(Int, Int) -> Int` | The row / column component. |
| `padd(p, q)` | `(Int, Int) × (Int, Int) -> (Int, Int)` | Component-wise sum (move by a direction vector). |
| `manhattan(p, q)` | `(Int, Int) × (Int, Int) -> Int` | `abs(Δrow) + abs(Δcol)`. |
| `rotl(p)` / `rotr(p)` | `(Int, Int) -> (Int, Int)` | Rotate a direction vector 90° left/right in grid coordinates: `rotr((-1, 0))` (up) is `(0, 1)` (right). |
| `psub(p, q)` | `(Int, Int) × (Int, Int) -> (Int, Int)` | Component-wise difference. |
| `pscale(p, n)` | `(Int, Int) × Int -> (Int, Int)` | Step `n` times along a direction. `padd` alone can only step by one. |
| `chebyshev(p, q)` | `(Int, Int) × (Int, Int) -> Int` | `max(\|Δrow\|, \|Δcol\|)` — 8-connectivity distance. |
| `dirs4()` | `-> List<(Int, Int)>` | The four orthogonal unit vectors: up, down, left, right. |
| `dirs8()` | `-> List<(Int, Int)>` | All eight, diagonals included. |
| `around4(p)` / `around8(p)` | `(Int, Int) -> List<(Int, Int)>` | Neighbours of a **point**, with no grid and no bounds check. `neighbors4`/`neighbors8` require a dense `Grid`, so these are what a `Sparse<T>` automaton needs. |
| `neighbors4(g, r, c)` | `Grid<T> × Int × Int -> List<(Int, Int)>` | In-bounds orthogonal neighbor coordinates of `(r, c)`. |
| `neighbors8(g, r, c)` | `Grid<T> × Int × Int -> List<(Int, Int)>` | In-bounds neighbors including diagonals. |

### Sparse grids

`Sparse<T>` is the unbounded default-valued plane — see
[data-model.md](data-model.md) for the full contract. `at` (above) is
*total* over a sparse grid.

| Builtin | Type | Behavior |
|---|---|---|
| `sparse(d)` | `T -> Sparse<T>` | An empty plane whose default is `d`. |
| `put(g, r, c, v)` | `Sparse<T> × Int × Int × T -> Sparse<T>` | Functional update: a copy with cell `(r, c)` set to `v` (the original is untouched). O(cells) per call — fine for building small grids in folds and loops; use `Convert To Sparse Grid` for bulk construction. |
| `has(g, r, c)` | `Sparse<T> × Int × Int -> Bool` | Whether `(r, c)` was explicitly set (a cell set to the default is still set). |
| `cells(g)` | `Sparse<T> -> Int` | The number of set cells. |
| `minrow(g)` / `maxrow(g)` | `Sparse<T> -> Int` | Bounds over set cells. **Error** on an empty sparse grid. |
| `mincol(g)` / `maxcol(g)` | `Sparse<T> -> Int` | Column bounds, same rules. |

Builtins compose freely with each other and with operators:

```
(g) -> item(g, 0) * 1000 + sum(take(g, 2)) + length(drop(g, 1))
(m) -> sum(get(m, 1))
(grid) -> at(grid, 1, 2) * 10 + at(grid, 0, 0)
(grid) -> inbounds(grid, 5, 0) and at(grid, 5, 0) = "#"
(ps) -> manhattan(first(ps), last(ps))
(n) -> lcm(gcd(n, 12), 8) + modpow(n, 10, 97)
```

Interval idioms need no dedicated Range type — a range is a two-field record
(from `Match Pattern: "{lo:int}-{hi:int}"`) and the operators express
containment and overlap directly (see `Merge Ranges` in
[primitives.md](primitives.md) for coalescing):

```
(r) -> r.lo <= 42 and 42 <= r.hi                          # Contains
(a, b) -> a.lo <= b.hi and b.lo <= a.hi                   # Overlaps
```

Typing errors (unknown function, wrong arity, wrong argument types) are
positioned resolve-time errors; the message for an unknown name lists the
whole builtin table.

### Design rules for extending the table

Keep it small and total where possible; every new builtin lands in **three**
places (typecheck, eval, codegen) with unit tests in the first two and an
interpreter-vs-binary oracle program in the third. A builtin implemented in
eval but not codegen must fail compilation with a positioned error — never
produce differing output. Partial builtins must use identical failure
wording in both backends.

## What the expression layer does not have (yet)

- **Higher-order builtins.** Nothing here maps, filters, sorts or searches, so
  the pure list transforms that need no function argument — `sort`, `unique`,
  `flatten`, `product`, `zip`, `enumerate`, `chunk`, `windows`, `transpose` —
  have no expression spelling either, even though they would not violate the
  rule. Indent a pipeline where the lambda goes (above) and the primitives do
  the work instead.
- **User-defined functions.** Shikigami operate at the pipeline layer instead,
  and are not recursive — see `Domain Expansion: Explore` in
  [primitives.md](primitives.md) for the search that replaces recursion.

(Historical gaps since closed: conditional expressions (`if/then/else`
above) and index-aware grid iteration — the positional `(g, r, c)` lambda
form of `Map Cells`/`Count Cells` with the `row`/`col`/`rows`/`cols`
builtins; floats; number-to-text conversion (`totext`); modulo and the
integer-math group; text access beyond parsing; the Map/Set escape hatches;
heterogeneous tuples; boolean negation (`ikke`); local bindings
(`consider`); and, in v0.6, **collection construction and update** — a Map,
Set or dense Grid could be read but never built, so a fold could not
accumulate one — along with list generation, text splitting and code points,
the float tower past `sqrt`, named-field records, and the base/bit/number-theory
group.)
