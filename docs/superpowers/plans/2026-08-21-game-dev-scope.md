# The `Game Dev` scope — implementation plan

> **Status: planned, and blocked on Part 1.** Design:
> [The `Game Dev` scope — design](../specs/2026-08-21-game-dev-scope-design.md).
> The scope mechanism this rests on is
> [planned separately](2026-08-21-innate-domain-scopes.md) and ships first.
> Do not start §A until that plan's "Done means" is met — in particular
> until `Part World:` parses and fails with "unknown role `World` in
> `Advent of Code`", which is the state §A begins from.

A terminal game vocabulary on Bubble Tea: **four new primitives, one new
type, eight `Part` roles, and about twenty-three expression builtins.** Most
of the size here is the host and the tests, not the language.

## Cross-cutting constraints

The repo's existing discipline. Each one shapes a decision below, and every
task inherits all of them.

1. **`go test ./...` green after every task.** Baseline ~4m
   (`codegen` ~90s, `cmd/domain` ~78s, `bench` ~32s, `docs` ~16s).
2. **Both backends or neither.** A feature in `eval` but not `codegen` must
   fail `domain build` with a positioned "not compilable yet" error rather
   than diverge silently (`codegen/codegen.go` package comment).
3. **The interpreter is the oracle.** Anything that changes program output
   needs an interpreter-vs-binary differential test **in both optimizer
   modes** — the `compilePipeline` / `runInterpreter` / `buildAndRun` triple
   used throughout `codegen/*_test.go`. §E is how a *game* gets one.
4. **No primitive without a catalog entry.** `TestCatalogCoversRegistry`
   (`prims/catalog_test.go:9`) pins `prims.Catalog` to exactly the
   registered set.
5. **Generated docs data is checked in and pinned.**
   `TestPrimitiveIndexIsCurrent` (`docs/gen_test.go:301`); regenerate with
   `go test ./docs -update`.
6. **Docs ship with the code, and they run.** Fenced ```domain run blocks in
   `docs/*.md` are executed and their `input`/`output` diffed, and
   `docs/coverage_test.go` demands **two runnable examples per documented
   feature**. This is why §E is not optional and not last.
7. **New type names go in both editor grammars.** The *primitive* lists in
   both grammars are generated from the registry and drift-tested
   (`editors/gen_test.go:101`), but the **type-name list is hand-maintained
   in three places** — `editors/vscode/syntaxes/domain.tmLanguage.json:349`,
   `editors/nvim/syntax/domain.vim:124`, and the assertion that pins them,
   `editors/gen_test.go:343`. Nothing generates it, which is why `Graph`
   is missing from all three today. Adding `View` means editing all three by
   hand; consider closing the `Graph` gap in the same task.
8. **Resolution must not panic.** `ResolveWith` converts panics to errors
   (`prims/guard_test.go`); the language server and the dev TUI resolve
   half-written buffers on every keystroke.
9. **One interpretation at a time, process-wide.** `interp.runMu`, and the
   package-level stacks in `eval`, `prims/ambient.go` and `ir/trace.go` that
   it protects. §E.1 is the direct consequence.
10. **Every source file must be committed** (`repo_files_test.go`) — check
    `.gitignore` before naming a new directory `game/` or similar.

---

---

# Tasks

### §A — the scope skeleton  *(shipped)*

Four things the implementation settled:

- **The world starts from the empty Record, not from nothing.** The design
  had `Part World:` open with no value at all, which reads better and is what
  `Cursed Energy` wants as its precondition — and is unusable, because
  `Read Source` is the *only* primitive that accepts a nil input. Every other
  stage, `Apply` included, had nothing to act on, so a game that does not read
  a file could not state its world. Opening with `{}` costs one ignored lambda
  parameter and makes the common case writable. Loading a level from a file
  needs a from-nothing source and is deferred with §C.
- **A missing `Part World:` is reported before anything else resolves.** Left
  to the cardinality check at the end, it produced a pile of "has no input
  value" from every role whose contract was written against a world that never
  arrived — none of which says what is actually wrong.
- **Declarations are hoisted too, and `Cursed Tool` is refused at the top
  level.** In a Parts-only program nothing runs where it is written, so a
  global read by a Part must not depend on which of the two came first; and a
  top-level *write* has no moment at which it would happen, so it is refused
  by name pointing at `Part World:`/`Part Start:`.
- **The globals seal needed a direction.** `sealedFrom` blocked reads and
  writes alike, which is right for a Channel and wrong for a read-only role —
  reading a global is exactly what one is for. It now takes the direction, and
  only the write is stopped.

Two smaller ones: a whole-program refusal carries no position, because "0:0"
sends the reader somewhere there is nothing to see; and `cmd/domain/game.go`
already registers a host that refuses clearly, so that between §A and §D
`domain run` on a game says "not written yet" instead of walking the Parts
linearly and failing inside a body with a message about the wrong thing.

**2.A.1** `prims/scope_gamedev.go`: register the `Game Dev` scope with its
role table and per-role `GlobalPolicy`, `TopLevelPipeline: false`,
`Host: "tui"`, `Emit: "tui"`, and an empty `Prims`. Shape check: exactly one
`World`, exactly one `Draw`, at most one each of `Start` and `Ending`, and
at least one of `On`/`Every`/`Reply` (a game with no input and no clock is a
picture, and saying so early is kinder than letting it run).

**2.A.2** Two-pass Part resolution for `DefinesWorld`. `ResolveWith` hoists
the `World` Part ahead of the rest, resolves it, and threads its output type
as `world` into every other role's `In`/`Out`. The hoist mirrors the
Shikigami/import hoist already in `parseProgram`.

**2.A.3** `Part Entity` → `ast.ShikigamiDef`. Lower at resolve time, before
`InferWith` runs, so the name is callable from every other Part.

**2.A.4b** `resolvePart` accepts `cur == nil` and takes the body's input
type from the role instead (F2); a top-level `Cursed Object:` in a game
refuses `Of` with a message naming `World`/`Start` (F3). Test that
`Cursed Energy:` as the first statement of `Part World:` resolves — it is
how two of the six games load their data.

**2.A.5** The `GlobalsReadOnly` seal: a third case in `sealedFrom()`
(`prims/globals.go:66`) with the wording in §"Global state, safely", and a
test per game role asserting a write is refused and a read is not.

**2.A.4** Refuse a top-level pipeline statement in a `Game Dev` program,
with the position of the offending line.

*Gate:* a `Game Dev` program with only `World` + `Draw` resolves; each
cardinality and contract violation has a test with an asserted message.

### §B — the `View` type  *(shipped)*

**Built before §A, deliberately.** `Part Draw:`'s contract *is* `W → View`,
and writing the role table before the type existed would have meant shipping
a `Draw` that accepts an `Int` body until §B caught up. The two are
independent — the View type needs no scope — so the reorder costs nothing.

Three things the implementation settled:

- **`draw` takes no per-cell function.** The plan had `draw(grid, (cell) ->
  View)`, which cannot exist: the expression layer has **no** higher-order
  builtins at all, by design ("None takes a function argument"). It does not
  need one either — the pipeline layer already has `Map Cells`, so a board of
  glyphs is built there and drawn here, and each layer keeps doing what it is
  for. `draw(board)` accepts a Grid, a Sparse, or a List of rows or lines.
- **`rows`/`cols`/`size` really were taken**, as the design warned; the
  combinators are `stack` / `beside` / `fit`, and `pad` became `margin`
  because `padd`, `padleft` and `padright` are all taken too.
- **codegen was not deferred.** It already refuses an uncompilable builtin
  with a positioned error, so the "both backends or neither" rule would have
  been satisfied by doing nothing — but the README claims the whole surface
  compiles and *"nothing is in that state today"*, and shipping a type that
  runs and will not build would have made that false. `codegen/viewgen.go`
  mirrors `ir/view.go`, pinned by `codegen/view_test.go` over every node kind
  and board shape in both optimizer modes.

The pre-existing gap the design noted is closed in passing: `Graph` was
missing from all three hand-maintained editor type-name lists, and is now in
them alongside `View`.

**2.B.1** `ir`: `KView`, `ViewValue`, `Type.String`/`Equal`/`Keyable`,
`FormatValueTyped`, `DescribeValue`. Not keyable, not orderable.

**2.B.2** `ir/view.go`: the tree, `RenderViewPlain`, the style-spec parser.
Layout maths (`stack`/`beside`/`box`/`align`) lives here and is shared, because
two implementations of column alignment will disagree within a week.

**2.B.3** `typecheck` + `eval`: the ten render builtins, plus `width()` and
`height()` (F5). `View` into `lowerTypeExpr`'s name table so it is writable
in a Shikigami signature. `=` on a `View`, and on a Record containing one,
is a resolve error (F6).

**2.B.4** `codegen/viewgen.go`: emit `dmView` and `dmRenderView`.

**2.B.5** `view/` package: `RenderStyled` over lipgloss, for the host.

**2.B.6** Editor grammars: `View` into the three hand-maintained type-name
lists (constraint 7), and regenerate the primitive lists. `Graph` is
already missing from all three — fix it here rather than adding a second
instance of the same gap.

*Gate:* `codegen/view_test.go` differential over a dozen trees, both
optimizer modes. `ir/view_test.go` for layout edges — ragged columns, empty
rows, a box round nothing, wide runes.

### §C — the vocabulary  *(shipped)*

What the implementation settled, beyond what §A and §B already recorded:

- **Randomness needed a purity audit, and the seam was already there.**
  `optimizer/globals.go`'s `impure` walk is what every aggressive pass
  consults through `effectful`, so teaching it one more case — a call whose
  answer is not decided by its arguments — reached all of them at once. The
  demonstration is that `Map Each` + `Max` fuses for a pure lambda, declines
  for a random one, and prints the same number with and without
  `--no-optimize`, because the number of draws is part of what the program
  means.
- **The two backends started from different seeds.** An ordinary program that
  drew a number printed different things interpreted and compiled. There is
  one `ir.DefaultSeed` now and both read it.
- **A `Using:` lambda may ignore its parameter**, and that never compiled:
  `(n) -> random(6)` emitted a loop binding an element nothing read, which Go
  refuses. A pre-existing bug that randomness merely made easy to hit, fixed
  where the source is assembled rather than at each of a dozen loop-emitting
  sites.
- **`Into:` is a *type*, not a value**, so the parser learned one thing:
  `ast.TypeArg`, chosen by argument name (`ast.TypeValuedArg`). Shape has
  always followed the name here — `Using:` a lambda, `From:` channel names —
  so this is that convention rather than a new one.
- **`Part Reply` is typed against the `Request` that fires its tag**, and the
  two may be written in either order, so requests are collected in a pre-scan
  before any Part resolves. `ProgramFacts` is what a scope's hooks ask about
  the program as a whole; it replaced the bare `map[string]int` the Shape hook
  first took.
- **The tag is checked both ways.** A Request nothing answers, and a Reply
  nothing fires, are each refused by name — otherwise both are a game that
  silently does nothing.

### §C — the vocabulary (as planned)

**2.C.0** `ir.ZeroValue(t)` plus its codegen mirror and a differential test
over every `TypeKind`. Needed by `Request`'s failure reply (D3) and by
nothing else yet — build it first anyway, because it is the piece whose
divergence would only surface while the network is already failing.

**2.C.1** `Simple Domain: Quit`, with an optional `Using:` predicate.
Sets a flag on the context that the host reads after each event body.
Legal in `Start`/`On`/`Every`/`Reply` only; a Quit in `Draw` or `Entity` is
a resolve error naming the roles that take one (F7).

**2.C.2** Random: `ir/rand.go`, `Context.Rand`, three builtins,
`codegen/randgen.go`, `--seed` — **plus a pass-by-pass audit of
`optimizer/`** for every pass that assumes a lambda is a function of its
arguments, mirroring how `ast.HasInPlace` is consulted today. This is the
single most likely source of a silent correctness bug in the plan; budget
for it accordingly.

**2.C.3** Time: `frame()`, `elapsed()`. Both are functions of the tick count
the host maintains, so replay reproduces them exactly.

**2.C.4** JSON: `Convert From JSON` / `Convert To JSON` against a declared
`ast.TypeExpr`; `tojson`/`fromjson` builtins. Decoder and encoder in `ir`,
mirrored in codegen.

**2.C.5** `Domain Expansion: Request`. Resolve-time: validate `Url:`,
`Method:`, `Body:`, `As:`, `Into:`, `Timeout:`; register the tag and check
it **both ways** against `Part Reply` (a `Request` nothing handles, and a
`Reply` nothing fires, are each an error naming the other side). The reply
record is built from `Into:` as `{ok: Bool, error: Text, value: T}`.
Also `pending(tag) -> Bool`, whose argument must be a string literal
matching a declared tag — checked at resolve time like the tags themselves.

**2.C.6** Catalog entries for all four primitives; `prims/catalog_test.go`
is the gate.

### §D — the interpreter host  *(shipped)*

`interp.Session` is what makes a host legal: `Run` holds a process-wide lock
and is not re-entrant, and everything it guards is package-level state, so a
host stepping through bodies without it would be the race the mutex was added
to stop. Playing and replaying share the Parts, the order and the world, and
differ only at their edges.

Two things worth recording. **A timer's period never reached the node**, so
every `Every` was period zero — not a fast timer but one always due, and the
host span forever; the period rides on the node now and a non-positive one is
refused at resolve time. And **`Simple Domain` dispatches structurally**, so it
never reached the registry and a scope could not add control vocabulary at all;
it asks the scope first now, which is how `Quit` exists.

### §D — the interpreter host (as planned)

**2.D.1** `interp.Session`. The game host cannot call `interp.Run` per event:
`runMu` is process-wide and not re-entrant, and `eval`'s binding and global
stacks, `prims/ambient.go`'s ambient stack and `ir/trace.go`'s context are
all package-level. So: a `Session` takes `runMu` for the whole game, resets
bindings and globals once, and exposes `Step(nodes []*ir.Node, v ir.Value)`.
This is constraint 9 discharged, and it must be built before anything calls
into a Part body from a Bubble Tea `Update`.

**2.D.0** Role payloads as named bindings (F1): `RoleBind` on the
`Consider` runtime-binding path (`prims/locals.go`), pushed around the
body at resolve time and seeded into the environment at run time. **Test
that a single Shikigami is callable from both `Part Every` and
`Part On`** — that is the case the ambient mechanism would have broken, and
it is the regression test that keeps the decision from being undone.

**2.D.2** `cmd/domain/game.go`: the Bubble Tea model. Ctrl+C quits unless
the program declares a handler for it (F8). No `bubbles` components (F9).
`Init` runs `World` and starts one `tea.Tick` per `Every` Part.
`Update` dispatches `tea.KeyPressMsg` / tick / reply to the matching Part,
pushing the role's ambient value first.
`View` runs `Draw`, renders styled, returns `tea.NewView` with
`AltScreen = true`.
A runtime error inside a body ends the program cleanly with the positioned
error on stderr — never a stack trace over a half-painted screen.

**2.D.3** Request execution as a `tea.Cmd`: one `http.Client` with a
timeout, the reply decoded against `Into:` off the UI goroutine, delivered
as a message. One request in flight per tag, superseding (D4), with
`pending(tag)` reflecting it; `Timeout:` and the response-size cap enforced
here, and every failure mapped to the `{ok, error, value}` reply.

**2.D.4** CLI: `domain run game.domain` plays when stdout is a TTY; replays
when it is not. `--play` and `--replay` force. `--seed N`. This is the same
shape `Read Source` already uses for its file/stdin fallback, and it means
no doc example and no test needs a flag.

### §E — the replay oracle  *(shipped, minus the compiled half)*

2.E.1, 2.E.2 and 2.E.5 are done. **2.E.5 needed no work at all**, which was the
hoped-for answer: a Game Dev program reads its replay script from stdin, so a
```domain run fence's `input` block *is* a script and the example executes like
any other. The compile half of the harness skips a scope with no backend, which
is the M3 contract rather than a weakened claim.

2.E.3 and 2.E.4 — the compiled binary supporting the same script, and the
differential harness over frames — belong to M4 with §F, since there is no
compiled game to diff until then.

### §E — the replay oracle (as planned)

The keystone. Without it the `Game Dev` scope cannot satisfy constraints 2,
3 or 6, and a game cannot be tested at all.

**2.E.1** The script format (`size` also feeds `width()`/`height()`, F5) — line-oriented, comments with `#`:

```
seed 12345
size 40 20
key right
tick
tick 500
frame
text a
reply guess {"accepted": true, "marks": ["hit", "miss"]}
frame
quit
```

`tick` advances the virtual clock to the next due `Every` Part (ties broken
by declaration order, so it is deterministic); `tick N` advances it by N ms
and fires everything that falls due, which is how "five gravity steps pass"
is written. `frame` runs `Draw` and prints `RenderViewPlain` between
separators. `quit` ends the run and runs `Ending`. An empty script runs
`World`, `Start`, one frame, then `Ending` — so the simplest possible game
still has an output to diff.

**`Request` never touches the network in replay mode.** The script supplies
the replies:

```
reply guess {"accepted": true, "marks": ["hit", "miss"]}
fail  guess "connection refused"
```

`reply` decodes against the tag's `Into:` type and delivers
`{ok: true, error: "", value: …}`; `fail` delivers `{ok: false, …}` with the
zero value. A request the script never answers simply never arrives — which
is itself a state worth testing, and the only way to write a test for
"the server went away mid-game".

This is what makes the **test-double-only** decision hold up: the two
multiplayer games are fully doc-testable and fully golden-testable with **no
server at all**. The `httptest` double is then needed for one narrow job —
testing the real HTTP client path in §2.D.3 — rather than for testing the
games.

**2.E.2** `interp` replay host, registered as `"tui"`'s headless twin.
Shares every line of the event-dispatch logic with the interactive host —
the two differ only in where events come from and where frames go.

**2.E.3** `codegen` replay: the emitted binary supports the same script on
stdin under the same TTY rule, so `runInterpreter` vs `buildAndRun` diffs
frames the way they diff answers today.

**2.E.4** `codegen/game_test.go`: the differential harness, both optimizer
modes.

**2.E.5** `docs`: the fenced-block harness needs **no change** — a
```domain run block whose program declares `Game Dev` gets its `input` as
the script and diffs frames as `output`. Verify this in
`docs/examples_test.go` and write it down, because it is the reason the
docs constraint is satisfiable.

### §F — compiling a game  *(shipped)*

All five tasks are done, and the emitted game is diffed against the
interpreter's frames for six anchor programs in both optimizer modes
(`codegen/game_test.go`, which is 2.E.3 and 2.E.4 discharged with it).

Three things came out differently from the plan, and the first is the one
worth reading.

**The host had to move.** 2.F.3 could not be tested where it was: the Game
Dev host lived in `cmd/domain`, so no other package could replay a game, and
`codegen`'s oracle needs a replayed frame to diff against. That was not a
testing inconvenience — it was the same bug §G's 2.G.0 is a gate for, one
package earlier. The browser playground calls `interp.RunScoped` and was
walking a game's Parts in source order, silently. So the host is now
`domain/game`, it registers itself, and the four binaries that run whole
programs import it for that effect. Bubble Tea does not build for js/wasm, so
the playing half is behind a build tag with a two-function stub; the
playground replays, which is what it wanted anyway.

**A game's declarations never ran.** `runGame` walked the pipeline looking
for Parts and skipped everything else, so a `Cursed Object` in a game was
never evaluated and a world built out of one read it as nothing. Found by
asking what codegen should emit for the non-Part nodes. Both halves of the
host now run them first, and so does the compiled program.

**Writing the second implementation found three more.** `dmSet` had no
`items()`, so `pick` over a Set and a Set written as JSON both emitted Go
that would not compile; a Map had no JSON writer at all and a Tuple's closed
with a brace; and a `Request` with no `Body:` still carries the key, holding
a typed nil. None of them was reachable while the scope was refused whole,
which is exactly why they had gone unnoticed.

The one approved regression landed as approved: a game's binary is ~7.5 MB
and its *build* needs the modules, at versions generated from this
repository's own `go.mod`. A program with no scope line is byte-identical to
before, and `TestStdlibBuildInputsAreUnchanged` asserts the go.mod, the
arguments and the environment literally rather than describing them.

**2.F.1** `codegen/deps.go`: the module requirements, **generated from the
repo's own `go.mod` and drift-tested**, so the emitted program can never ask
for a version this binary was not built against.

**2.F.2** `codegen/build.go`: `BuildBinaryWith` writes `go.mod` *and*
`go.sum` (the relevant lines embedded from the repo's committed `go.sum`)
when the pipeline's scope needs them, and sets `GOFLAGS=-mod=mod`.
`CGO_ENABLED=0` stays — every dependency is pure Go.
An `Advent of Code` build is **byte-identical to today's**: same bare
`go.mod`, same flags, same stdlib-only binary. Pin that with a golden test,
because it is the promise this task is most likely to break.

**2.F.3** `codegen/gamegen.go`: emit the Bubble Tea model, the Part bodies
as functions, the event dispatch, the replay reader.

**2.F.4** `--emit-go` for a game prints `main.go` with the `go.mod`
requirements as a header comment.

**2.F.5** README and `docs/compiler.md`: the self-contained-binary claim
becomes scope-qualified. A `Game Dev` binary is ~8–10 MB and needs the
modules at build time (cached after the first). This is the one approved
regression in the plan and it should be stated plainly wherever the old
claim appears.

### §G — the rest of the toolbelt  *(shipped)*

2.G.0 turned out to be already done and not yet enforced: all nine sites were
converted to `RunScoped` in M1, but nothing stopped a tenth, and — as §F
found the hard way — `RunScoped` only reaches a host something linked in. Both
halves are now walked over the source by `interp/scoped_test.go`, and both
were verified to fail when broken.

The table below held, with one change of mind and three fixes.

**`visualize` refuses, as planned, and the evidence was worth having.** It did
not fail loudly on a game — it rendered the Parts as a loop that never
finished. Adding a trace frame per Part (which the host now does, and which
stats and any future tool want anyway) made the labels right and the shape no
better: the stepper's model is one value moving through one chain, and a game
has no such chain. The refusal asks the scope (`prims.IsOnePipeline`) rather
than a name, so a scope added later is refused without the tool learning about
it, and it is a distinct error type because the REPL answers an error by
re-running the diagnostics — which would have replaced the sentence with a
complaint about the program.

**Coverage counted only Core**, so a scope's own primitives were invisible in
both directions. Fixed; a folder of games can now be told it has never fired a
`Request`.

**The editor is scope-aware in the one place a scope differs**: `Part`. A
`Part Draw:` is a resolve error outside a game and a `Part "1":` is one inside
it, so offering either in the wrong file offers something that will not
compile. `Innate Domain:` offers the scopes this build has. Everything else
stays scope-blind, because a scope only ever adds.

`battle`, `stats`, `coverage` and `mahoraga` needed no change at all — each
runs the program through its own host and reads what came out, which for a
game is its frames. Each now has a test saying so.

**2.G.0 is a gate, not a nicety (F4).** Nine non-test sites call
`interp.Run` directly, and one left unconverted runs a game's Part nodes as
passthroughs in source order and prints nothing — a silent wrong answer. Fix
and test all nine: `runner/runner.go:732`, `mahoraga/recon.go:198`,
`cmd/domain-wasm/main.go:167`, `cmd/domain/visualize.go:195`,
`cmd/domain/repl.go:324`, `cmd/domain/repl_stats.go:58`,
`cmd/domain/dev_run.go:130`, `cmd/domain/main.go:411`,
`cmd/domain/repl_visualize.go:76`.

Each of these currently assumes a batch pipeline. Each must either work or
refuse with a sentence that says why:

| command | for a `Game Dev` program |
|---|---|
| `battle` | **works** — replay gives it a stdout to compare |
| `fmt`, `check`, `diagnosis`, `fix`, `lint` | **work** — no run involved |
| `lsp`, `development` | **work**, scope-aware |
| `visualize` | **refuses** — "a game is not one pipeline; use `--replay`" |
| `repl` | **refuses** — a game is a program, not a session |
| `stats`, `coverage`, `mahoraga` | **work in replay mode**, per-Part attribution |
| `domain-wasm` playground | **works in replay mode** — no TTY and no filesystem needed, so the browser runs a game exactly as the docs harness does |

The playground line is worth the work: it means every game example in the
docs has a Run button.

### §H — the six games  *(shipped)*

All six, in the planned order, each with its script and its exact frames,
replayed in both optimizer modes by `cmd/domain` and compiled-and-diffed
against the interpreter by `codegen`. The budget for the vocabulary changing
between game 2 and game 4 went unspent: **nothing was added to the language**.
What the games found instead was three forms already there that nobody had
needed at once, and three tools that had never met them.

- **snake** — the world holds the board as a `Sparse` and the snake moves by
  painting two cells. Said two things worth keeping: a Part's body is an
  ordinary pipeline so its *intermediate* stages may be any shape, and a game
  over is a state rather than an exit (quitting on the fatal step means the
  player never sees the board that killed them).
- **tetris** — a well of `Text` rows, where a full row is one with no `"."` in
  it and clearing it is `take` + `drop`. The shape of the code is
  `Simple Domain: For k in range(4)`: because the loop threads the world while
  binding each lap as an ambient, the same four-cell walk is written once as a
  `Part Entity` and called from a key, a timer and a frame. Found two false
  negatives — the linter warned about the loop's own documented syntax, and
  the editor-grammar drift test called every Entity name unhighlighted
  vocabulary.
- **oregon** — no grid at all: the world holds a `scene` and the frame is
  text. `Part Entity` earns its keep on a state machine as much as on a board.
  Also the note that `blank()` is an empty slot rather than a spacer row.
- **towerdef** — the one that had to reach a search from inside a game, and
  the form that does it is **`Consider NAME Of` taking a pipeline body**. `BFS`
  from the exit is a flow field; the same search answers "may a tower go
  here". This is the discovery of the milestone and it is now documented in
  three places.
- **wordle** — the asynchronous round trip, with the script as the server.
- **hideseek** — polling *without* a `pending` guard, because superseding is
  right for a position poll, and what that costs (a dropped reply) drawn as a
  stale frame rather than a stall.

`examples/games/`, each with a replay script and golden frames, each a test:

1. **snake** — `Sparse`, one `Every`, four `Event`s, growth, collision.
   *Proves:* the loop, input, the grid, `Quit`.
2. **tetris** — `Grid`, rotation as list ops, gravity, line clear, scoring.
   *Proves:* `Transpose` and the list vocabulary carry real game logic.
3. **oregon** — scenes as a world field, menus, resources, random events.
   *Proves:* text-heavy rendering, `random`, a state machine with no grid.
4. **towerdef** — `Dijkstra` for creep routing, `List<Record>` entities
   advanced per tick, waves, economy.
   *Proves:* the existing algorithm vocabulary is reachable from a game, and
   `Part Entity` earns its place.
5. **wordle-mp** — text entry, `Request`/`Message`, JSON both ways.
   *Proves:* the async round trip, against an `httptest` double.
6. **hideseek-mp** — a grid, position sync on a timer, remote players merged
   into the world.
   *Proves:* polling under a frame budget, and that a slow reply degrades
   into a stale frame rather than a stall.

Write them in that order. Each is a load test on a different claim above,
and each one that turns out awkward to write is telling you something about
the vocabulary while there is still time to change it. **Budget for the
vocabulary changing between game 2 and game 4** — that is what the games are
for.

### §I — documentation  *(shipped, differently)*

Two of the four planned pages already existed by another name:
`ref-builtins-view.md` came with §B and the `Request`/JSON reference entries
came with §C, so `ref-game-view.md` and `ref-game-net.md` would have been a
second copy of each. What was written instead is `game-dev.md` (the tutorial,
built up a step at a time) and `ref-game-parts.md` (the roles, what may
happen, what each role puts in scope, the one-in-flight-per-tag rule,
entities, globals) — the latter registered in `coverage_test.go`, so its
sections carry two runnable examples each like every other feature.

The six games are in `gallery.json`, which gives each a Run button; the
generator now carries per group where its programs live and what their input
is called, because a game is fed a replay script rather than an input file.

And the thing this section found: `scopes.md`, `ref-builtins-view.md` and
`ref-builtins-chance.md` had each been written, linked from `README.md`, and
left out of the site navigation for a whole milestone — not broken, simply
unreachable there. Two tests now hold both indexes, one per route.

- `docs/game-dev.md` — the scope's tutorial, snake end to end.
- `docs/ref-game-parts.md` — the role table, the event spec table.
- `docs/ref-game-view.md` — the `View` type and its ten builtins.
- `docs/ref-game-net.md` — `Request`, `Message`, JSON.
- `docs/data-model.md` — `View` alongside `Sparse` and `Graph`.
- `docs/optimizer.md` — what `random` costs a stage.
- `docs/walkthroughs.md`, `docs/gallery.json` — the six games.
- `go test ./docs -update`; `docs/coverage_test.go` is the gate.

---

---

## Sequencing

| Milestone | Contents | Ships? |
|---|---|---|
| **M1** | Part 1 entire (1.1–1.8) | yes — one scope, keyword moved, no behaviour change |
| **M2** | §A + §B | no — `View` exists and `Game Dev` resolves; nothing runs |
| **M3** | §C + §D + §E | yes — games play and replay, interpreter only. `domain build` refuses with a positioned error |
| **M4** | §F | yes — games compile |
| **M5** | §G + §H + §I | yes — the toolbelt, the six games, the docs |

All five shipped. The language gained nothing in M5: the six games are
written in the vocabulary §A–§F built, and every change in that milestone was
to a *tool* that had not met a game yet.

M3 is the honest first release: it deliberately violates nothing, because
"refuses with a positioned error" is the documented behaviour for a
primitive with no lowering (constraint 2) and the same discipline extends to
a scope with no emitter. Do **not** be tempted to ship M3 with a
half-working compiler; a `Game Dev` binary that diverges from the
interpreter is the one failure mode this repo's entire test strategy exists
to prevent.

---

---

## Risk register

| Risk | Where | Mitigation |
|---|---|---|
| Scoping quietly changes an AoC program's meaning | 1.3 | `Core` is today's `Registry` untouched; `TestDefaultScopeRegistryMatchesCore`; the whole existing suite green **unmodified** |
| `random` silently breaks an optimizer pass | 2.C.2 | A pass-by-pass audit of `optimizer/` is part of the task; property tests against the naive oracle with a fixed seed |
| `ir/view.go` and `codegen/viewgen.go` drift | 2.B.4 | Differential tests over a tree corpus, both optimizer modes — the same contract `Sparse` and `Graph` already live under |
| Emitted `go.mod` breaks the AoC build path | 2.F.2 | Golden test on the AoC-scope build inputs; the deps file is generated from `go.mod` and drift-tested |
| The Part role vocabulary is wrong | §H | Games 1→6 in order; expect to revise after game 2 and game 4 |
| A payload mechanism that breaks Shikigami reuse | 2.D.0 | F1; the regression test is "one Shikigami, called from `Every` and from `On`" |
| An unconverted `interp.Run` silently prints nothing | 2.G.0 | F4; nine named sites, a test each |
| Bubble Tea v2 API churn | 2.D.2 | Already pinned in `go.mod` and already used by `cmd/domain/dev_*.go`; the host copies patterns that are known to work here |
| The docs harness cannot run a game | 2.E.5 | Verified as a task, before §H is written, not after |

---

---

## Done means

- The six games in `examples/games/` run interactively, replay to golden
  frames, and compile — with the compiled binary's frames byte-identical to
  the interpreter's, in both optimizer modes.
- An `Advent of Code` build is byte-identical to a pre-change one (golden
  test on the build inputs).
- Every one of the nine `interp.Run` sites in F4 either handles a game or
  refuses with a sentence, with a test each.
- One Shikigami is called from both a `Part Every` and a `Part On` in a
  shipped example — the F1 regression, made permanent.
- `docs/game-dev.md` and the three reference pages exist, and
  `docs/coverage_test.go` passes without an exemption being added for any
  of them.
- The README's self-contained-binary claim is scope-qualified wherever it
  appears.
