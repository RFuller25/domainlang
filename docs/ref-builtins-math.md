# Numeric builtins

Part of the [expression layer reference](expressions.md).

### Math / number theory

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> gcd(item(xs, 0), item(xs, 1)) + lcm(item(xs, 0), item(xs, 1))
Reveal: stdout
```
```input
12,18
```
```output
42
```

`mod` is Euclidean, so it answers with the sign of the divisor rather than of
the dividend — which is what wrapping a coordinate needs:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Using: (n) -> mod(n, 3)
Reveal: stdout
```
```input
-4,-1,5
```
```output
[2, 2, 2]
```


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

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Floats
Cursed Technique: Map Each
    Using: (f) -> floor(f)
Reveal: stdout
```
```input
1.9,2.1,3.5
```
```output
[1, 2, 3]
```

`round` is half away from zero, which is where it parts company with `floor`
and `trunc`:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Floats
Cursed Technique: Map Each
    Using: (f) -> round(f)
Reveal: stdout
```
```input
1.5,-1.5,2.4
```
```output
[2, -2, 2]
```


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
An `Int × Int` call still returns an `Int`, so the integral case is unaffected.

**There is no infinity and no NaN.** Neither can be written and neither prints
usefully, so a computation that leaves the reals is an **error where it
happens** rather than a poison value that surfaces three stages later:
`log(0)`, `exp(1000)` and `tan` at a pole all fail with a positioned message.
