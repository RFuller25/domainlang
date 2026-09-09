# Games

Programs in the [`Game Dev`](../../docs/scopes.md) Innate Domain: a world,
events that change it, and a frame drawn from it.

They are not puzzle solvers, so they are not shaped like the examples one
directory up. There is no top-level pipeline and no input file. Each game is
`Part` blocks and declarations, and its input is a **replay script** on stdin —
which is also how it is tested, since a game's output is a picture and a
picture diffs like anything else.

```sh
go build -o domainc ./cmd/domain

./domainc examples/games/snake.domain                       # play it
./domainc examples/games/snake.domain < examples/games/snake.script
./domainc examples/games/snake.domain -o snake              # or compile it
```

The first line plays: a terminal on both ends means the arrow keys move and
the frames are painted. The second replays: anything else — a pipe, a test,
the browser playground — reads events from the script and writes the frames as
plain text. Nothing has to pass a flag, which is the same rule
`Cursed Energy:` already uses to choose between a file and stdin.

| Game | Shows off |
|---|---|
| `snake` | the loop end to end — a `Sparse` board painted two cells at a time, four key handlers, one timer, growth, collision, a game over that is a state rather than an exit, and `Simple Domain: Beep` ringing the terminal bell on every bite |
| `tetris` | that the list vocabulary carries real game logic — a well of `Text` rows, where a full row is one with no `"."` in it and clearing it is `take` + `drop`; and `Simple Domain: For k in range(4)`, which is how the same four-cell walk is written once as a `Part Entity` and called by name from a key, a timer and a frame |
| `oregon` | a game with no grid at all — the world holds a `scene`, every Part asks which one it is in, and the picture is text. `Part Entity "Menu":` is the one place that says what a scene offers, called by name after every change; and `margin` is what puts a blank line in a frame, since `blank()` is an empty slot rather than a spacer |
| `towerdef` | that the algorithm vocabulary is reachable from inside a game — `BFS` from the exit gives every cell its distance to safety and a creep walks downhill, with no path-finding written by hand. The form that unlocks it is **`Consider NAME Of` taking a pipeline body**, so a stage whose value is the world can run a whole search over one of its fields and keep the world. The same search answers "may a tower go here": if the entry comes back unreachable, it may not |
| `wordle` | the asynchronous round trip — a `Request` fires and returns the world unchanged, and the answer arrives later at `Part Reply "guess":` like any other event. There is no server in this directory: the script says what came back, so `reply` is a room that answered, `fail` is one that did not, and a request with neither is one that went away. `pending("guess")` is why a second guess is refused rather than queued |
| `hideseek` | polling under a frame budget — a position sync fires **without** a `pending` guard, because a newer position is worth more than an older answer and superseding is what keeps replies from arriving out of order. What that costs is a dropped reply, and what a dropped reply looks like is `age`: a slow room degrades into a stale frame, drawn dim and labelled, rather than into a stall |

Each game ships with its script and its exact frames:

| File | Is |
|---|---|
| `NAME.domain` | the program |
| `NAME.script` | what happened: keys, ticks, frames, replies |
| `NAME.expected` | every frame it draws, byte for byte |

`TestGameExamples` (in `cmd/domain`) replays each one in both optimizer modes
and diffs the frames; `TestCompiledExampleGamesMatchInterpreter` (in `codegen`)
compiles each one and diffs the binary's frames against the interpreter's. So a
game here cannot rot, and the two backends cannot drift apart without a test
saying which frame changed.

## The script

One command per line, `#` starts a comment.

| Line | Means |
|---|---|
| `key <name>` | that key was pressed — `up`, `q`, `enter`, `ctrl+c`, … |
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

A replayed run never touches the network — which is what makes a multiplayer
game testable with nothing running. The wire itself is covered separately, by
`TestSendRequestAgainstAServer` in `game`, which sends one request at an
`httptest` server and decodes what comes back.

The clock is virtual and moves only when the script says, the seed is fixed
unless the script changes it, and the globals freeze once the program starts —
so a replayed run gives the same bytes every time, on any machine.
