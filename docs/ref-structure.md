# Inversions and statements

One class of the [primitive reference](primitives.md).

## Reverse Cursed Technique — inversions

### Reverse — `List<T> -> List<T>` or `Text -> Text`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Reverse Cursed Technique: Reverse
Reveal: stdout
```
```input
a,b,c
```
```output
[c, b, a]
```

It reverses text as well as lists, choosing the form by the current type:

```domain run
Cursed Energy: stdin
Reverse Cursed Technique: Reverse
Reveal: stdout
```
```input
hello
```
```output
olleh
```


Reverses element order — or, over `Text`, the runes. A palindrome check used
to have to round-trip through `Split Text by ""`.

---

### Quit — `T -> T` *(Game Dev)*

Ends the program. A game has no natural end — nothing runs out, and the loop
is driven from outside — so this is how one says it is finished: the snake hit
itself, the player pressed q, the last word was guessed.

It is a **passthrough**. The world it was given is the world it hands back, so
the Part it sits in still meets its contract and the value that reaches
`Part Ending:` is the one the game ended on: a game ends *on* a state, it does
not discard one.

A Game Dev program reads its [replay script](scopes.md#game-dev) from stdin,
so these run like any other example — the `input` block is the script, and the
output is the frames and then whatever `Part Ending:` revealed:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part On "key q":
    Simple Domain: Quit

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.n)

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> "stopped at " + totext(w.n)
    Reveal: stdout
```
```input
tick
tick
key q
tick
frame
```
```output
stopped at 2
```

The third `tick` never happens and the frame is never drawn: `quit` ends the
run where it is reached, and the rest of the script is not read.

With a `Using:` predicate it stops only when the answer is true, which saves
wrapping a whole body in a conditional that has to produce the world either
way:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)
    Simple Domain: Quit
        Using: (w) -> w.n = 3

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.n)

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> "stopped at " + totext(w.n)
    Reveal: stdout
```
```input
tick 1000
```
```output
stopped at 3
```

The clock was asked for a thousand milliseconds, which is ten ticks. The game
stopped at three, and the world it stopped on is the one `Part Ending:` saw.

Only the `Game Dev` [Innate Domain](scopes.md) has it; an `Advent of Code`
program ends when its pipeline does.

---

### Beep — `T -> T` *(Game Dev)*

Rings the terminal bell. A frame is a picture, and a hit landing, a piece
coming to rest, a wrong guess are moments rather than pictures — `style()`
already covers what a frame can say, and this covers what it cannot.

It is a **passthrough**, on the same shape as `Quit`: the value it was given
is the value it hands back, so it drops in wherever a body is already
threading a value through — not only where that value is the world.

**It waits for a frame.** Unlike `Quit`, which the host checks after every
body, a beep rung mid-tick does not sound until the next `Part Draw:` runs —
a frame is the unit a replayed game is diffed at, so the bell rings exactly
where it would land in that diff: as the very first byte written for the
next frame, ahead of the picture (invisible below, since a bell has no glyph
— but it is really there, and the golden this page is checked against is
byte-for-byte). Two beeps between frames still ring once; a frame drawn with
none pending stays silent.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part On "key a":
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)
    Simple Domain: Beep

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.n))
```
```input
frame
key a
frame
```
```output
0

1
```

`examples/games/snake.domain` rings it on every bite this way — guarded so a
step that both eats and collides does not sound one for a game that is
already over.

With a `Using:` predicate it rings only when the answer is true, the same
condition `Quit` supports for the same reason: it saves wrapping the whole
body in a conditional that has to produce the value either way. A predicate
that stays false the whole run stays silent the whole run — the value still
passes through untouched, since ringing or not is the only thing the
predicate decides:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)
    Simple Domain: Beep
        Using: (w) -> w.n > 100

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.n))
```
```input
tick
tick
frame
```
```output
2
```

Only the `Game Dev` [Innate Domain](scopes.md) has it.

## Simple Domain, Channel, Shikigami, Binding Vow, Reveal

`Reveal: stderr` sends the value to standard error instead of stdout, so a
mid-pipeline Reveal becomes a debugging tool that does not disturb the
program's answer — or its golden test. A nil sink discards, so a host that
captures only stdout never sees stderr output mixed in.

Control flow (`Repeat N` / `While` / `Iterate Until Fixed Point`), Channels
and their consumers, Shikigami definition/calls, vows, and the output sink
are described in [language.md](language.md). All are fully supported by both
backends, including vow stripping under `--release`.
