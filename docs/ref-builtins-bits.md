# Bit, logic and number-theory builtins

Part of the [expression layer reference](expressions.md).

### Bit operations

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> band(item(xs, 0), item(xs, 1)) + bor(item(xs, 0), item(xs, 1))
Reveal: stdout
```
```input
12,10
```
```output
22
```

`bxorall` is the "xor the whole column" one-liner, and each of the three
reducers returns its operator's identity on the empty list:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> bxorall(xs)
Reveal: stdout
```
```input
1,2,3
```
```output
0
```


| Builtin | Type | Behavior |
|---|---|---|
| `band(a, b)` / `bor(a, b)` / `bxor(a, b)` | `Int × Int -> Int` | Bitwise and / or / xor. |
| `bnot(n)` | `Int -> Int` | Bitwise complement. |
| `shl(a, n)` / `shr(a, n)` | `Int × Int -> Int` | Left / arithmetic right shift. **Error** on a negative shift count. |
| `popcount(n)` | `Int -> Int` | Set bits in the two's-complement representation. |
| `bandall(xs)` / `borall(xs)` / `bxorall(xs)` | `List<Int> -> Int` | Reduce a list with the operator, the way `sum` and `product` do. The empty list gives the operator's **identity**, so a later fold is unchanged by it: `0` for `or`/`xor` and **`-1`** for `and` (all bits set). `bxorall` is the "xor the whole column" one-liner. |
| `testbit(n, i)` | `Int × Int -> Bool` | Whether bit `i` is set. **Error** outside `0`–`63`. |
| `frombin(s)` | `Text -> Int` | Parse a binary string (whitespace-tolerant) — the 2021 D3 diagnostic parse. **Error** if not binary. |
| `frombase(s, b)` | `Text × Int -> Int` | Parse in base `b` (2–36), sign allowed. **Error** on a bad base or an unparseable string. |
| `fromhex(s)` | `Text -> Int` | Base 16, tolerating a `0x` prefix. |
| `tobase(n, b)` | `Int × Int -> Text` | Render in base `b` (2–36). |
| `tohex(n)` / `tobin(n)` | `Int -> Text` | Base 16 / base 2. |

### Logic

The named forms, beside the infix `and`/`or` — `xor` has no infix spelling at
all:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Count Matching
    Using: (n) -> xor(n > 1, n < 3)
Reveal: stdout
```
```input
1,2,5
```
```output
2
```

`and` and `or` short-circuit, which is what pairs `inbounds` with `at`:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Maximum Technique: Count Cells
    Using: (g, r, c) -> inbounds(g, r + 1, c) and at(g, r + 1, c) = "#"
Reveal: stdout
```
```input
..
##
```
```output
2
```


The infix connectives are `and`, `or` and `ikke` (prefix negation). These are
their **function** spellings, plus the `xor` that has no infix form at all.

| Builtin | Type | Behavior |
|---|---|---|
| `and(a, b)` / `or(a, b)` | `Bool × Bool -> Bool` | Conjunction / disjunction. |
| `xor(a, b)` | `Bool × Bool -> Bool` | Exactly one of the two. No infix spelling exists. |
| `not(a)` | `Bool -> Bool` | Negation — `ikke` as a function. |

**The function forms do not short-circuit.** Every builtin evaluates all of its
arguments before it runs, and these are builtins; the infix operators are
syntax and keep their short-circuit. The difference is observable:

```domain ignore
x > 99 and item(xs, 5) > 0      # false — the right operand never runs
and(x > 99, item(xs, 5) > 0)    # error: index 5 out of range
```

Prefer the infix operators when either operand can fail or is expensive. The
function forms are for `xor`, and for reading a chain of conditions as a
call — never for guarding one operand with another.

`and` and `or` are recognized as operators only in **infix** position and as
ordinary names elsewhere, so both spellings coexist:
`a > 0 and and(b > 0, not(c < 0))` is one expression. The one ambiguous
position — an infix `or` immediately followed by `(` — is a parse error rather
than a silent reinterpretation.

### Number theory

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Count Matching
    Using: (n) -> isprime(n)
Reveal: stdout
```
```input
2,4,7,9,11
```
```output
3
```

`digits` and `fromdigits` are inverses, which is how a numeric puzzle
manipulates a number's decimal form without going through text:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> fromdigits(reverse(digits(toint(t))))
Reveal: stdout
```
```input
1234
```
```output
4321
```


| Builtin | Type | Behavior |
|---|---|---|
| `isprime(n)` | `Int -> Bool` | **Exact**, not probabilistic: deterministic Miller-Rabin over a witness set that settles every `Int`. O(log³ n), because a 19-digit Int is legal to write and trial division would be three billion divisions. |
| `divisors(n)` | `Int -> List<Int>` | Every positive divisor, ascending. **Error** on a non-positive input (zero has infinitely many). One pass to √n, with no sort. |
| `digits(n)` | `Int -> List<Int>` | The decimal digits of `\|n\|`, most significant first; `0` is `[0]`. |
| `fromdigits(ds)` | `List<Int> -> Int` | The number those digits spell — the inverse of `digits`. **Error** on an element outside 0–9, or on overflow (a silently wrapped number is a wrong answer that looks right). |
| `crt(rs, ms)` | `List<Int> × List<Int> -> Int` | The smallest non-negative `x` with `x ≡ rs[i] (mod ms[i])` for every `i`. The moduli need **not** be coprime: each pair is checked for agreement modulo their gcd and merged on their lcm, so a system read out of a puzzle works rather than only one constructed to be coprime. **Error** on an inconsistent system. |
