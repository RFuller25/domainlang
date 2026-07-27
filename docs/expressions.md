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
| `sqrt(x)` | `Int \| Float -> Float` | **Error** on negative input. |

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

**Positions count runes, not bytes**, everywhere — `length`, `charat`, `slice`
and `indexof` agree with each other and with `Split Text by ""`, so an index
means the same thing in both layers on non-ASCII input.

### Bit operations

| Builtin | Type | Behavior |
|---|---|---|
| `band(a, b)` / `bor(a, b)` / `bxor(a, b)` | `Int × Int -> Int` | Bitwise and / or / xor. |
| `shl(a, n)` / `shr(a, n)` | `Int × Int -> Int` | Left / arithmetic right shift. **Error** on a negative shift count. |
| `frombin(s)` | `Text -> Int` | Parse a binary string (whitespace-tolerant) — the 2021 D3 diagnostic parse. **Error** if not binary. |

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

- **Record construction.** `Match Pattern` is still the only source of a
  `Record`, so a fold whose accumulator is a *named-field* struct cannot be
  written. `tuple` covers the positional case, and a measured `Seed:` (see
  [primitives.md](primitives.md#measured-arguments)) is what lets a fold
  actually start from one — `Seed: (xs) -> tuple(0, 0)`. A named-field
  constructor still needs a new argument form in the expression grammar and
  per-shape struct interning in codegen.
- **User-defined functions.** Shikigami operate at the pipeline layer instead,
  and are not recursive — see `Domain Expansion: Explore` in
  [primitives.md](primitives.md) for the search that replaces recursion.

(Historical gaps since closed: conditional expressions (`if/then/else`
above) and index-aware grid iteration — the positional `(g, r, c)` lambda
form of `Map Cells`/`Count Cells` with the `row`/`col`/`rows`/`cols`
builtins; floats; number-to-text conversion (`totext`); modulo and the
integer-math group; text access beyond parsing; the Map/Set escape hatches;
heterogeneous tuples; boolean negation (`ikke`); and local bindings
(`consider`).)
