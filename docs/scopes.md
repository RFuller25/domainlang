# Innate Domain — the kind of program this is

Every Domain program has a **scope**: the vocabulary in reach, the shapes a
program may take, and how it is run. `Innate Domain:` declares it.

```domain ignore
Innate Domain: Advent of Code
```

Almost no program writes that line, because it is the default. A file with no
`Innate Domain:` is an `Advent of Code` program — puzzle input in, answer out
— which is every program in [`examples/`](../examples/README.md) and
[`challenges/`](../challenges/README.md).

## What a scope decides

| It owns | Which means |
|---|---|
| the vocabulary | which primitives a phrase can name |
| the `Part` roles | what kinds of block a program is made of |
| the program shape | whether a top-level pipeline is even legal |
| the prelude | a standard library, written in Domain, loaded before your file |
| the host | how the resolved program is run, and whether it compiles |

A scope only ever **adds** to the vocabulary. Nothing an `Innate Domain` does
can make an operation stop resolving in a program that resolves today, and
nothing it adds is reserved anywhere it is not available — so a `Shikigami`
you may name today, you may name tomorrow.

## The ones that exist

| Scope | Also spelled | For |
|---|---|---|
| `Advent of Code` | — | Puzzle input in, answer out: the pipeline Domain was designed around. |
| `Game Dev` | `Game` | A terminal game: a world, events that change it, and a frame drawn from it. |

`Advent of Code` has no short spelling on purpose. `Innate Domain: aoc` is what
every program written before scopes existed says, and it meant *import the aoc
library* — so it has to stay an error, and get you
[the keyword that now imports](#the-rules) rather than quietly becoming a
scope declaration.

Declaring it explicitly changes nothing at all:

```domain run
Innate Domain: Advent of Code

Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
```
```input
1,2,3,4
```
```output
10
```

The same program without the line is the same program:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Maximum Technique: Sum
Reveal: stdout
```
```input
1,2,3,4
```
```output
10
```

## The rules

**One per program.** A second `Innate Domain:` is an error naming where the
first one was, rather than the last line quietly winning.

**Hoisted, like a Shikigami definition.** Where the line sits in the file does
not matter. By convention it goes first, because it is the thing a reader most
needs to know before reading anything else.

**The keyword is required.** A bare `Advent of Code` line is an operation
phrase, not a declaration — the same rule `Inherited Technique:` lives under,
and for the same reason.

**Not a file path.** An `Innate Domain` names a scope this build has. To load
a library of your own Shikigami, use
[`Inherited Technique:`](language.md#inherited-technique--importing-a-library):

```domain ignore
Innate Domain: Advent of Code   # what kind of program this is
Inherited Technique: aoc        # a library of Shikigami to load
```

An `Innate Domain:` naming something this build does not have is an error at
that line, listing what it does have. If the name is a library on the search
path, the error says so and names the keyword you meant.

## Game Dev

The second scope, and the one that shows what a scope can change. A
`Game Dev` program is **Parts and declarations only** — there is no top-level
pipeline, because there is no single value flowing through one:

```domain ignore
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (w) -> {score: 0, dir: "right"}

Part On "key left":
    Cursed Technique: Apply
        Using: (w) -> with(w, "dir", "left")

Part Every 120:
    Shikigami: Step

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> box(text(w.score))

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> w.score
    Reveal: stdout
```

Every lifecycle role name answers **when**:

| Role | When it runs | Body |
|---|---|---|
| `Part World:` | first | the empty world → the world. Must be a Record |
| `Part Start:` | once, before the first frame | world → world |
| `Part On "…":` | when an input arrives | world → world |
| `Part Every N:` | every N milliseconds | world → world |
| `Part Reply "…":` | when a request completes | world → world |
| `Part Draw:` | each frame | world → [`View`](ref-builtins-view.md) |
| `Part Ending:` | last | world → anything; what it `Reveal`s is the output |
| `Part Entity "…":` | never — it is a definition | world → anything, callable by its name |

`Part Entity` is the exception, and the only role not in the lifecycle. It
lowers to an ordinary [Shikigami](language.md#shikigami--user-defined-operations): its label becomes the
name, its body becomes the definition, and from there it is called by name and
inlined like any other.

**Order does not matter.** Parts, `Cursed Object` declarations and Shikigami
definitions are all declarations of a sort, and none of them runs where it is
written — the world is resolved first wherever it sits, so that every other
role has a type to be checked against, and an Entity is callable before the
line that defines it.

**Playing and replaying.** A game with a terminal on both ends plays: events
come from the keyboard and frames are painted. Anything else — a pipe, a test,
the browser playground — **replays**: events come from a script on stdin and
frames are written as plain text. The mode is chosen the way `Cursed Energy`
already chooses between a file and stdin, so nothing has to pass a flag.

| Script line | Means |
|---|---|
| `key <name>` | that key was pressed |
| `text <what>` | that was typed |
| `tick` | let the next timer come due |
| `tick <ms>` | advance the clock by that many milliseconds, firing everything due |
| `frame` | draw one frame to stdout |
| `size <w> <h>` | the terminal is this big |
| `seed <n>` | draw from this stream instead |
| `reply <tag> <json>` | a request tagged `<tag>` came back with this |
| `fail <tag> <why>` | it came back a failure, for this reason |
| `load <json>` | a `Load` fired after this line reads this back, in place of a real file |
| `quit` | stop here; the rest of the script is not read |

Frames are separated by a blank line, so a one-frame script prints its picture
and nothing else, and `Part Ending:`'s `Reveal` follows them. A replayed run is
**exact**: the clock is virtual and moves only when the script says, the seed
is fixed unless the script changes it, and the globals are frozen — so the same
script gives the same bytes every time, which is what lets a game be tested at
all.

**A game compiles.** `domain build` emits Go for a `Game Dev` program exactly
as it does for a puzzle solver, and the compiled binary replays the same
script to the same frames — which is how the two backends are checked against
each other. What is different is the build: a game paints a terminal, so its
binary is around 7.5 MB and needs Bubble Tea at *build* time, at versions pinned
from this repository's own `go.mod`. A program with no scope line is
unaffected; see [the compiler backend](compiler.md).

**A replayed run never touches the network.** A
[`Request`](ref-expansions.md#request--w---w-game-dev) is recorded rather than
sent, and the script says what came back — so a multiplayer game is testable
with nothing running, and its two halves become two lines of a file. A request
the script never answers simply never arrives, which is itself worth testing:
it is what a server going away mid-game looks like.

**The globals freeze.** `Cursed Object` values may be written in `Part World:`
and `Part Start:`, and are **read-only everywhere else**. That is stricter than
`Advent of Code`, and deliberately: it leaves exactly one mutable thing — the
world — so a frame is a function of the world alone, and the same world always
draws the same picture. Globals are for what genuinely does not change: the
board size, the word list, the palette, a level loaded once.

## The toolbelt, on a game

Most of the toolbelt does not care what kind of program it is handed.

| Command | On a `Game Dev` program |
|---|---|
| `run`, `build` | play with a terminal on both ends, replay otherwise |
| `fmt`, `check` | work — neither runs anything |
| `expansion: lint`, `diagnosis`, `fix`, `optimize` | work — they read the resolved program |
| `expansion: battle` | works — a replayed game has a stdout to compare, so a game can race a Python one |
| `expansion: stats`, `coverage`, `mahoraga` | work — each runs the program through its own host |
| `lsp`, `expansion: development` | work, and offer this scope's `Part` roles |
| `expansion: visualize` | **refuses** |
| `repl` | **refuses** |

The two that refuse are the two built on *one value moving through one chain of
stages*: the stepper shows that value at each stage, and the REPL is that chain,
extended a line at a time. A game has no such chain — its stages belong to
different Parts and run in an order the program does not state — so both say so
in a sentence and point at `domain run`, rather than showing a display that is
wrong in a way nobody would notice.

The [playground](wasm/README.md) replays too: it has no terminal and no
filesystem, so a game there behaves exactly as it does under the documentation
harness, and every game example on this site has a Run button.

## A note on where this is going

Scopes exist so that Domain can be pointed at something other than a puzzle
without the puzzle vocabulary changing underneath anybody. Neither scope takes
anything away from the other: `Game Dev` is `Advent of Code`'s whole
vocabulary plus the Parts above, which is why a game can reach for `Dijkstra`
or `Match Pattern` without ceremony.
