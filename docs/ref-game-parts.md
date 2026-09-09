# Part roles — `Innate Domain: Game Dev`

The [`Game Dev`](game-dev.md) scope's `Part` roles: what each one is, when it
runs, what it is handed and what it must produce. A role belongs to a scope, so
`Part Draw:` is a resolve error in a puzzle solver and `Part "1":` is one in a
game.

| Role | When | Body | Puts in scope |
|---|---|---|---|
| `Part World:` | first | `{}` → the world. Must be a Record | |
| `Part Start:` | once, before the first frame | world → world | |
| `Part On "…":` | when an input arrives | world → world | `key`, or `width`/`height` |
| `Part Every N:` | every N milliseconds | world → world | |
| `Part Reply "…":` | when a request completes | world → world | `reply` |
| `Part Draw:` | each frame | world → [`View`](ref-builtins-view.md) | |
| `Part Ending:` | last | world → anything; its `Reveal` is the output | |
| `Part Entity "…":` | never — it is a definition | world → anything, callable by name | |

**Cardinality.** Exactly one `Part World:` and one `Part Draw:`; at most one
`Part Start:` and one `Part Ending:`; any number of the rest. Two
`Part Every 120:` blocks are the same timer twice and refused; a `Part Every
120:` and a `Part Every 250:` are two timers.

**Order does not matter.** The world is resolved first wherever it sits, so
every other role has a type to be checked against, and an `Entity` is callable
before the line that defines it.

## The world, and the roles that change it

`Part World:` opens with the **empty world** — a Record with no fields — and
its body says what the world is. It must produce a Record: the other roles read
and rewrite it by name, and a bare Int gives them nothing to name.

`Part Start:` is the one-shot with the world in hand, before the first frame.
It is where anything that has to happen once but needs the finished world goes.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {board: fill(3, "..."), turn: 0}

Part Start:
    Cursed Technique: Apply
        Using: (w) -> with(w, "board", list("...", ".#.", "..."))

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "turn", w.turn + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> beside(draw(w.board), text(" turn " + totext(w.turn)))
```
```input
frame
tick
frame
```
```output
... turn 0
.#.
...

... turn 1
.#.
...
```

A body is an ordinary pipeline, so its **intermediate stages may be any shape**
— only the last one has to satisfy the role's contract. That is what keeps a
step from being one expression nobody can read:

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {at: 0, hits: 0}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> {w: w, next: mod(w.at + 3, 5)}
    Cursed Technique: Apply
        Using: (s) -> {w: s.w, next: s.next, wall: s.next = 0}
    Cursed Technique: Apply
        Using: (s) ->
            with(with(s.w, "at", s.next), "hits", s.w.hits + (if s.wall then 1 else 0))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.at) + " / " + totext(w.hits))
```
```input
tick 500
frame
```
```output
0 / 1
```

## What may happen

`Part On "…":` takes a spec from a closed table. It is a table rather than a
pattern language on purpose: a game reacting to input is answering "which of
these few things happened?", and a spec language would turn a typo into a
handler that silently never fires — the quietest possible bug in a program
whose output is a picture. An unknown spec is refused at resolve time, by name.

| Spec | Means |
|---|---|
| `"key <name>"` | that key: `up`, `down`, `left`, `right`, `enter`, `esc`, `space`, `backspace`, `tab`, a letter, `ctrl+c`, … |
| `"key"` | any key press |
| `"text"` | a printable keystroke — what the player typed |
| `"resize"` | the terminal changed size |

When several specs answer to one event they all run, **specific first**:
`"key q"` and `"key"` are two different questions about one keystroke, and a
program that writes both means both.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {log: ""}

Part On "key q":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + "Q")

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + ".")

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.log)
```
```input
key q
key z
frame
```
```output
Q..
```

Ctrl+C always leaves, unless the program says what it means by it — a game with
no quit handler is the first game anybody writes, and an unkillable one is a
bad first impression.

`"text"` is the other question: what the keystroke *wrote*, which is what a
game asking the player to type reads. A key that writes nothing never reaches
it.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {typed: "", presses: 0}

Part On "text":
    Cursed Technique: Apply
        Using: (w) -> with(w, "typed", w.typed + upper(key))

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "presses", w.presses + 1)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.typed + " after " + totext(w.presses) + " presses")
```
```input
text a
text b
key enter
key esc
frame
```
```output
AB after 2 presses
```

## What a role puts in scope

A role may put values in scope for its body. They are **named bindings**, on
the same footing as a `Consider` written on the Part, and deliberately not
extra lambda parameters: an ambient would change every lambda's arity in the
body, including inside any `Shikigami` inlined there, so one definition could
not be called from a handler that has `key` and a timer that does not.

| Role | Binding | Type |
|---|---|---|
| `Part On "key …"`, `"key"`, `"text"` | `key` | `Text` — the key's name, or what was typed |
| `Part On "resize"` | `width`, `height` | `Int` |
| `Part Reply "<tag>"` | `reply` | `{ok: Bool, error: Text, value: T}` |

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {size: "", last: ""}

Part On "resize":
    Cursed Technique: Apply
        Using: (w) -> with(w, "size", totext(width) + "x" + totext(height))

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "last", key)

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.size + " " + w.last)
```
```input
size 30 10
key left
frame
```
```output
30x10 left
```

`reply` is the same mechanism with a bigger payload: one shape for success and
for every way the exchange can fail, so a body branches once.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {said: ""}

Part On "key enter":
    Domain Expansion: Request
        Url: (w) -> "https://example.test/who"
        As: "who"
        Into: {name: Text}

Part Reply "who":
    Cursed Technique: Apply
        Using: (w) ->
            with(w, "said", if reply.ok then "hello " + reply.value.name else "(" + reply.error + ")")

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.said)
```
```input
key enter
reply who {"name": "ada"}
frame
key enter
fail who timed out
frame
```
```output
hello ada

(timed out)
```

## Requests and replies

`Domain Expansion: Request` fires and returns the world unchanged; the answer
arrives at the `Part Reply "<tag>":` written for its tag. The tag is a literal,
which is what lets the two halves be checked against each other at resolve
time: a request nothing answers, and a reply nothing fires, are each refused.

**One request is in flight per tag, and firing again supersedes the one that
was out.** That is a correctness rule before it is a policy. HTTP replies can
arrive out of order, and a client polling positions at 5 Hz against a server
that sometimes takes half a second will, under any unbounded scheme, receive an
answer from three requests ago *after* a newer one and merge it — which reads
as other players teleporting backwards, looks like a game bug rather than a
networking one, and costs a day to find. It supersedes rather than dropping the
new one because a body is normally derived from the current world: superseding
sends where the player *is*, keeping the old one sends where they *were*.

What that costs is a dropped reply, and the cost is visible:
[`pending(tag)`](ref-builtins-chance.md) says whether one is out, so a program
that must not lose one guards first.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {log: ""}

Part On "key enter":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + (if pending("ask") then "!" else ">"))
    Domain Expansion: Request
        Url: (w) -> "https://example.test/ask"
        As: "ask"
        Into: {n: Int}

Part Reply "ask":
    Cursed Technique: Apply
        Using: (w) -> with(w, "log", w.log + (if reply.ok then totext(reply.value.n) else "x"))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.log)
```
```input
key enter
key enter
reply ask {"n": 7}
key enter
fail ask down
frame
```
```output
>!7>x
```

The second `enter` found a request already out and said so; the answer to it
arrived and was appended; the third went out and failed.

A request the script never answers simply never arrives, which is itself a
state worth testing: it is what a server going away mid-game looks like, and
there is no other way to write a test for it.

```domain run
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {waited: 0, got: "nothing yet"}

Part On "key enter":
    Domain Expansion: Request
        Url: (w) -> "https://example.test/slow"
        As: "slow"
        Into: {n: Int}
        Timeout: 250

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> if pending("slow") then with(w, "waited", w.waited + 1) else w

Part Reply "slow":
    Cursed Technique: Apply
        Using: (w) -> with(w, "got", "answered")

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(w.got + " after " + totext(w.waited))
```
```input
key enter
tick 300
frame
```
```output
nothing yet after 3
```

## Entities

`Part Entity "Name":` is a definition, not a lifecycle role: it contributes no
stage to any run. Its body becomes an ordinary
[Shikigami](language.md#shikigami--user-defined-operations) — the label is the
name — and from there it is called by name, inlined at the call site, and
optimized through, with no new call machinery and no new reserved-name rule.

```domain run
Innate Domain: Game Dev

Part Entity "Wrap":
    Cursed Technique: Apply
        Using: (w) -> with(w, "at", mod(w.at, 4))

Part World:
    Cursed Technique: Apply
        Using: (w) -> {at: 0}

Part On "key right":
    Cursed Technique: Apply
        Using: (w) -> with(w, "at", w.at + 3)
    Shikigami: Wrap

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "at", w.at + 1)
    Shikigami: Wrap

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(totext(w.at))
```
```input
key right
tick
tick
frame
```
```output
1
```

An `Entity` is world → **anything**, so it is as useful for asking a question
about the world as for changing it — the caller decides what to do with what
comes back.

```domain run
Innate Domain: Game Dev

Part Entity "Score":
    # world -> a number, not a world
    Cursed Technique: Apply
        Using: (w) -> length(w.hits) * 10

Part World:
    Cursed Technique: Apply
        Using: (w) -> {hits: emptylist(0), shown: 0}

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "hits", concat(w.hits, list(1)))

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> w

Part Draw:
    Cursed Technique: Apply
        Consider points Of
            Shikigami: Score
        Using: (w) -> text("hits " + totext(length(w.hits)) + " worth " + totext(points))

Part Ending:
    Shikigami: Score
    Reveal: stdout
```
```input
key a
key b
frame
```
```output
hits 2 worth 20
20
```

Two ways to reach it, both in that program. `Part Ending:` may produce
anything, so it calls the Entity as an ordinary stage. `Part Draw:` must
produce a `View`, so it calls it inside a
[`Consider … Of`](expressions.md#stage-bindings--consider--as--consider--of) —
which takes a **pipeline body**, not only a lambda, and so can run a whole
sub-pipeline over the world and still leave the world in hand for `Using:`.
That form is what puts every search in the language within reach of a game;
`examples/games/towerdef.domain` routes its creeps with it.

## Globals

`Cursed Object` values may be written in `Part World:` and `Part Start:`, and
are **read-only everywhere else**. That is stricter than `Advent of Code`, and
deliberately: it leaves exactly one mutable thing — the world — so a frame is a
function of the world alone, and the same world always draws the same picture.
Globals are for what genuinely does not change: the board size, the word list,
the palette, a level loaded once.

A game's declarations run **before** the world is built, so a world may be made
out of one.

```domain run
Innate Domain: Game Dev

Cursed Object: width As 5
Cursed Object: fillWith As "-"

Part World:
    Cursed Technique: Apply
        Using: (w) -> {bar: repeat(fillWith, width), n: 0}

Part Every 100:
    Cursed Technique: Apply
        Using: (w) -> with(w, "n", mod(w.n + 1, width))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(slice(w.bar, 0, w.n + 1))
```
```input
tick
tick
frame
```
```output
---
```

`Part Start:` may write one too, which is where anything that has to be
computed once rather than typed goes.

```domain run
Innate Domain: Game Dev

Cursed Object: alphabet As "abcdefghijklmnopqrstuvwxyz"
Cursed Object: chosen As ""

Part World:
    Cursed Technique: Apply
        Using: (w) -> {at: 0}

Part Start:
    Cursed Tool: chosen As slice(alphabet, 0, 5)

Part On "key":
    Cursed Technique: Apply
        Using: (w) -> with(w, "at", mod(w.at + 1, length(chosen)))

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> text(chosen + " -> " + charat(chosen, w.at))
```
```input
key a
key b
frame
```
```output
abcde -> c
```

A `Cursed Tool` write from any other role is a resolve error, and it names the
role it was written in.

## Shape

Two whole-program rules, checked once every statement has resolved:

- **A request needs an answer.** A `Request` tagged `"scores"` with no
  `Part Reply "scores":` is refused, and so is a reply nothing fires.
- **Something must change the world.** A program with no `Part On`, no
  `Part Every` and no `Part Reply` draws one frame and never moves.

Neither can be answered by looking at a single line, which is why they are
whole-program checks rather than statement ones.
