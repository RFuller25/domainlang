# Chance and the clock

Part of the [expression layer reference](expressions.md).

Five builtins, and they are the only ones in the language whose answer is not
decided by their arguments. Everything else is a function of what it was
handed — which is the assumption almost every [optimizer](optimizer.md) pass
rests on, and why these five are treated specially by all of them.

| Builtin | Type | Meaning |
|---|---|---|
| `random(n)` | `Int -> Int` | A value in `[0, n)`. |
| `randomf()` | `-> Float` | A value in `[0, 1)`. |
| `pick(xs)` | `List<T> \| Set<T> -> T` | One element, drawn from the stream. |
| `frame()` | `-> Int` | How many frames have been drawn. |
| `elapsed()` | `-> Int` | Milliseconds the run has been going. |

## The stream is seeded

A language this careful about determinism has randomness because the sequence
is a **pure function of a seed**:

- **Playing** a game, the seed comes from the clock, so it is a different game
  every time somebody sits down to it.
- **Replaying** one, and in every other program, the seed is fixed — a
  [replay script](scopes.md#game-dev)'s `seed` line, or a constant. So a test
  gives the same answer every time it runs, and a run that went wrong can be
  reproduced exactly.

That is why the example below prints the same numbers on your machine as it
does here:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> list(random(100), random(100), random(100))
Reveal: stdout
```
```input
```
```output
[65, 19, 90]
```

The generator is splitmix64. It is mirrored exactly in the compiled backend,
so an interpreted run and a compiled one draw the same sequence — which is
what lets a program that rolls dice still be checked against the
[oracle](compiler.md).

`pick` draws one element, and a `Set` reads as its elements the way it does
everywhere else:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Cursed Technique: Apply
    Using: (xs) -> pick(xs) + pick(xs) + pick(xs)
Reveal: stdout
```
```input
a,b,c,d,e
```
```output
aea
```

`random(n)` maps the stream onto a range by rejection rather than by a modulo.
A modulo is one instruction and is *slightly* biased when `n` does not divide
2⁶⁴ — and a board that never places anything in its last column is a bug
somebody spends an evening on.

## What they cost

A stage whose lambda calls one of these **gives up its optimizer rewrites**,
exactly as a stage that writes to a binding does, and for the same reason: the
passes are aggressive in ways that a call with its own answer would notice.
Fusion turns "all of f, then all of g" into "f then g, per element"; algorithm
substitution applies a lambda a different number of times; constant folding
applies one twice to see what it does. All of that is sound for a function of
its arguments and none of it is sound for a die roll.

So this fuses:

```domain
Cursed Technique: Map Each
    Using: (n) -> n * 2
Maximum Technique: Max
```

and this does not, and prints the same answer with `--no-optimize` as without,
because the number of draws is part of what the program means:

```domain
Cursed Technique: Map Each
    Using: (n) -> n * random(1000)
Maximum Technique: Max
```

Nor is `random(3)` ever folded to a literal while the program is being
lowered. Folding it would draw once, then bake the result in, and every run of
that program would play the same game.

## The clock

`frame()` and `elapsed()` are **counts a host keeps**, not readings it takes.
A replayed run's clock advances because its script said `tick`, not because
the machine was slow, so `elapsed()` gives the same answer every time the same
script runs.

Outside a game there is no host keeping them, and both report zero:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> frame() + elapsed()
Reveal: stdout
```
```input
```
```output
0
```

## `pending`

`pending("tag")` reports whether a
[`Request`](ref-expansions.md#request--w---w-game-dev) with that tag is still
out. It belongs on this page for the same reason the rest do: its answer is not
a function of its argument — the same call answers differently as the exchange
goes on.

One request is in flight per tag, and firing again supersedes the one that was
out, so a reply can be lost. That is the right trade for a position poll and
the wrong one for a move nobody may drop, which is why the loss is made
visible: a program that must not lose one asks first.

```domain ignore
Part On "key enter":
    Cursed Technique: Apply
        Using: (w) -> if pending("guess") then w else submit(w)
```

Outside a game, or before anything has been fired, nothing is pending:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> pending("anything")
Reveal: stdout
```
```input
```
```output
false
```
