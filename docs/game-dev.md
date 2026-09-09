# Game Dev — writing a game in Domain

`Innate Domain: Game Dev` is the second [scope](scopes.md), and the one that
shows what a scope can change. A game is not a question with an answer, so it
is not a pipeline: it is a **world**, the **events** that change it, and a
**frame** drawn from it.

This page builds one, a step at a time. Everything on it runs — the blocks are
executed and their output diffed, like every other example in these documents.

## The smallest game

Three `Part` blocks. One says what the world is, one changes it, one draws it.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text("tick " + totext(w.n))
```
```input
frame
tick
tick
frame
```
```output
tick 0

tick 2
```

Three things are worth reading off that.

**The world is a Record, and it is the only thing that changes.** `Part World:`
opens with the empty world — a Record with no fields — and its body says what
the world actually is. Every other role is typed against whatever it produced,
so a `with(w, "n", …)` that misspells `n` is a resolve error rather than a
field that silently appears.

**A role name answers *when*.** `Every 100` is every hundred milliseconds,
`Draw` is once a frame. Nothing declares an order; the host decides, and the
program says only what each event means.

**The input is a script.** The block above is a replay script, and so is
everything this page feeds a game. A game with a terminal on both ends plays —
the keyboard moves it and the frames are painted — and anything else replays.
That is the same rule `Cursed Energy:` already uses to choose between a file
and stdin, so nothing has to pass a flag.

Writing one by hand for a new game is the one thing this rule does not help
with — `domain run yourgame.domain --record yourgame.script` does: play it
once, on a real terminal, and every key, tick and reply is written out as the
game receives it, ready to trim down into a test. See
[`--record`](cli.md#--record-a-played-session-becomes-a-golden-test).

## Input

`Part On "…":` says what an input does. The label is what happened, from a
[closed table](ref-game-parts.md#what-may-happen): a named key, any key, a
printable keystroke, a resize.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {typed: "", presses: 0}

Part On "text":
    Cursed Technique: Apply
        Using: (w) -> with(w, "typed", w.typed + key)

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "presses", w.presses + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.typed + " (" + totext(w.presses) + " presses)")
```
```input
text h
text i
key enter
frame
```
```output
hi (1 presses)
```

The handler reads `key`, which the role put in scope for it. It is an ordinary
named binding, on the same footing as a `Consider` written on the Part — not an
extra lambda parameter — which is what lets one `Shikigami` be called from a
handler that has `key` and from a timer that does not.

`"key"` and `"text"` are two different questions about one keystroke, and a
script asks them one at a time: the two `text` lines above wrote letters, and
the `key enter` line was the only press. **Played**, a printable keystroke
fires both — so a program that writes both handlers sees `h` twice, once as a
press and once as what it wrote. A key that writes nothing, like enter, is only
ever a press.

## Drawing

`Part Draw:` produces a [`View`](ref-builtins-view.md), which is a picture
rather than a string: `text`, `stack`, `beside`, `box`, `align`, `margin`,
`style`, `fit`, `draw`. Layout happens once, before anything is painted, so a
replayed frame and a played one are the same picture.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {score: 7, lives: 3}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> w

Part Draw:
    Cursed Technique: Apply
        Using: (w) ->
            box(stack(
                beside(text("score "), style(text(totext(w.score)), "bold yellow")),
                beside(text("lives "), style(text(totext(w.lives)), "bold red")),
                margin(text("press q"), 0, 1)))
```
```input
frame
```
```output
┌───────┐
│score 7│
│lives 3│
│       │
│press q│
│       │
└───────┘
```

`style` is dropped when a frame is written as plain text and painted when it is
painted, which is why the golden above shows the words and not the colour.
`margin(v, 0, 1)` is how a blank row is made: `blank()` is an empty slot and
occupies nothing at all.

A board is `draw`, which takes a `Grid`, a `Sparse` or a plain list of rows:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {rows: list(".....", ".....", ".....")}

Part On "key x":
    Cursed Technique: Apply
        Using: (w) -> with(w, "rows", list(".....", "..#..", "....."))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> box(draw(w.rows))
```
```input
frame
key x
frame
```
```output
┌─────┐
│.....│
│.....│
│.....│
└─────┘

┌─────┐
│.....│
│..#..│
│.....│
└─────┘
```

## Ending

`Simple Domain: Quit` says the program is finished. It is a passthrough — the
world it was given is the world it hands back — so the state the game stopped
on is the state `Part Ending:` sees, and whatever that `Reveal`s is the
program's output.

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
        Using: (w) -> text(totext(w.n))

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> "stopped at " + totext(w.n)
    Reveal: stdout
```
```input
tick 1000
frame
```
```output
stopped at 3
```

The clock was asked for a thousand milliseconds, which is ten ticks. The game
stopped at three, and the `frame` after it never ran — `Quit` ends the run
where it happens.

That is worth knowing when the last thing a player should see is the board that
killed them. `snake` and `tetris` in [`examples/games/`](../examples/games/README.md)
both freeze instead: a game over is a state the world is in, and a key is what
leaves.

## Naming a piece of the game

`Part Entity "Name":` is a definition rather than a lifecycle role. Its body is
world → anything, it becomes an ordinary
[Shikigami](language.md#shikigami--user-defined-operations), and from there it
is called by name, inlined at the call site and optimized through.

```domain run
Innate Domain: Game Dev

Part Entity "Bump":
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", w.n + 1)

Part World:
    Cursed Technique: Apply
        Using: (w) -> {n: 0}

Part On "key":
    Shikigami: Bump

Part Every 100:
    Shikigami: Bump

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text("n = " + totext(w.n))
```
```input
key a
tick
frame
```
```output
n = 2
```

One definition, called from a handler and from a timer. That works because a
role's payload is a *named binding* rather than a lambda parameter: `Bump`'s
lambda is exactly as written in both places, and never has to declare a `key`
it does not read.

## Talking to a server

A round trip is a hundred milliseconds on a good day and a frame is sixteen, so
a `Request` **fires and returns the world unchanged**. The answer arrives later
at `Part Reply "<tag>":`, an event like any other.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {note: "idle", score: 0}

Part On "key enter":
    Cursed Technique: Apply
        Using: (w) -> with(w, "note", "asking…")
    Domain Expansion: Request
        Url: (w) -> "https://example.test/score"
        As: "score"
        Into: {points: Int}

Part Reply "score":
    Cursed Technique: Apply
        Using: (w) ->
            if reply.ok
                then with(with(w, "score", reply.value.points), "note", "got it")
                else with(w, "note", "no answer: " + reply.error)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.note + " " + totext(w.score))
```
```input
key enter
frame
reply score {"points": 42}
frame
key enter
fail score connection refused
frame
```
```output
asking… 0

got it 42

no answer: connection refused 42
```

**A replayed run never touches the network.** The script says what came back,
so the two halves of an exchange are two lines of a file — which is what makes
a multiplayer game testable with nothing running. A `Request` the script never
answers is a server that went away mid-game, and there is no other way to write
a test for that.

Every failure lands in the same shape, `{ok, error, value}`, so a game has one
thing to branch on and "the server is down" is a state it can draw rather than
a crash over a half-painted screen. When `ok` is false, `value` is the
[zero](data-model.md) of what was asked for.

One request is in flight per tag and firing again supersedes the one that was
out — see [`ref-game-parts.md`](ref-game-parts.md#requests-and-replies) for why
that is a correctness rule rather than a policy, and what `pending` is for.

## What a game may not do

The scope takes nothing away from the vocabulary — a game can reach for
`Dijkstra` or `Match Pattern` without ceremony — but it does change the shape a
program may take.

- **No top-level pipeline.** A `Game Dev` file is Parts and declarations only.
  There is no single value flowing through one, so a stage written at the top
  level has nothing to act on.
- **The globals freeze.** `Cursed Object` values may be written in
  `Part World:` and `Part Start:`, and are read-only everywhere else. That
  leaves exactly one mutable thing — the world — so a frame is a function of
  the world alone.
- **A request needs an answer.** A `Request` tagged `"scores"` with no
  `Part Reply "scores":` is refused at resolve time, and so is a reply nothing
  fires. The two halves are written apart, so they are checked against each
  other rather than hoped about.
- **Something must change the world.** A program with no `Part On`, no
  `Part Every` and no `Part Reply` draws one frame and never moves, which is a
  picture rather than a game.

## Where to go next

- [`ref-game-parts.md`](ref-game-parts.md) — the roles, what may happen, and
  what each one puts in scope.
- [`ref-builtins-view.md`](ref-builtins-view.md) — the `View` type and its ten
  builtins.
- [`ref-builtins-chance.md`](ref-builtins-chance.md) — `random`, `pick`,
  `frame`, `elapsed`, and why a seeded stream makes a game testable.
- [`scopes.md`](scopes.md) — what a scope decides, the replay script, and which
  tools work on a game.
- [`cli.md`](cli.md#--trace-watching-a-game-one-part-at-a-time) — `--trace`,
  one line per `Part` body run as a replayed game goes, for when a frame diff
  alone doesn't say which step got it wrong.
- [`examples/games/`](../examples/games/README.md) — six whole games, each with
  its script and its exact frames.
