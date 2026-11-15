# The `Game Dev` scope — design

The first `Innate Domain` that is not the default: a terminal game
vocabulary hosted on Bubble Tea, with asynchronous HTTP for multiplayer.
The mechanism it rests on is specified in
[Innate Domain scopes — design](2026-08-21-innate-domain-scopes-design.md)
and ships before any of this.

It is sized against six games it has to be able to write: **snake**, a
**tetris** clone, an **Oregon Trail** clone, a **tower defense**, a
**multiplayer wordle** and a **multiplayer hide-and-seek**. Those are not
illustrations. Each one loads a different claim in this document, and §The
six games in the plan says which.

## The shape of the answer

The thing worth saying up front, because it decides how much of this is
work and how much is already here:

**A game does not need a second language.** Domain already has every
structural piece a Bubble Tea program needs, under names that mean the right
things:

| Bubble Tea | Domain, today |
|---|---|
| `Model` | the world value — a Record, threaded like any pipeline value |
| `Init() Cmd` | a pipeline that produces the initial value |
| `Update(Msg) (Model, Cmd)` | a `Part` body typed `W → W` (`prims/part.go`) |
| `View() string` | a `Part` body typed `W → View` |
| a sub-model | a `Shikigami`, inlined at its call site |
| per-frame work over entities | `Map Each` / `Filter` / `Fold` over a `List<Record>` |
| the board | `Grid<T>` / `Sparse<T>` |
| enemy routing | `Domain Expansion: BFS` / `Dijkstra`, which already exist |

A `Part` is already a passthrough that owns a separately-resolved node list
in `Meta["nodes"]`, already snapshots and restores globals around its body,
and is already reached by the optimizer's `nodeLists`. It was built to be
"one of several pipelines branching from a shared state." That is a game.

So the language delta is: give a `Part` a **role**, let the **scope** own the
role table, and add the vocabulary a game needs that a puzzle solver does not
— a render tree, input, randomness, and the network.

---

## Non-goals

- **No second grammar.** The pipeline and expression layers are untouched;
  the only parser change belongs to the scope mechanism, not to this scope.
- **No server in Domain.** This is an HTTP *client*. The multiplayer games
  are tested against replay-supplied replies and, for the client path
  itself, an `httptest` double. Nothing ships that listens on a port.
- **No sound, no mouse, no images.** Keyboard, timer, network, terminal.
- **No sprite or animation engine.** A frame is a value; animation is a
  world field and a `Part Every`.
- **No `bubbles` components.** Text entry is `On "text"` plus
  `On "key backspace"` plus a world field. It keeps the emitted binary's
  dependencies to bubbletea + lipgloss, and it is about fifteen lines of
  Domain in the two games that need it.
- **No general effect system.** `Request` is the only asynchronous
  primitive and it has exactly one delivery shape.
- **No mutable state outside the world.** See §Global state, safely — this
  is load-bearing, not tidiness.

---

# Design

## The program shape

A `Game Dev` program is **Parts and declarations only**. A top-level
pipeline statement is an error naming the offending line — the same rule,
with the same wording shape, that a library file already lives under
(`prims/imports.go`). That is the whole of the "use strict" effect, achieved
with `TopLevelPipeline: false` and no parser change.

```domain
Innate Domain: Game Dev

Part World:
    Cursed Technique: Apply
        Using: (_) -> {snake: [(10, 5)], dir: (0, 1), food: (3, 3),
                       score: 0, over: false}

Part Entity "Food":
    Cursed Technique: Apply
        Using: (w) -> (random(20), random(20))

Part On "key left":
    Cursed Technique: Apply
        Using: (w) -> with(w, "dir", (0, -1))

Part Every 120:
    Shikigami: Step Snake

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> stack(
            style(text("score " + totext(w.score)), "bold"),
            box(draw(board(w), (c) -> text(c))))

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> w.score
    Reveal: stdout
```

## The role table **[decided — D2]**

| Role | Argument | Count | Body contract | When it runs |
|---|---|---|---|---|
| `Part World:` | none | exactly 1 | `{} → W`; `W` must be a Record | first. Its output type **is** the world type |
| `Part Start:` | none | 0..1 | `W → W` | once, after `World`, before frame 1 |
| `Part On "spec":` | label | 0..n | `W → W` | when input matching `spec` arrives |
| `Part Every <ms>:` | int | 0..n | `W → W` | on a timer of that period |
| `Part Reply "tag":` | label | 0..n | `W → W`, with `reply` ambient | when a `Request` tagged `tag` completes |
| `Part Draw:` | none | exactly 1 | `W → View` | once per frame |
| `Part Ending:` | none | 0..1 | `W → T` | once, after the loop; whatever it `Reveal`s is the program's stdout |
| `Part Entity "Name":` | label | 0..n | `W → T` | never on its own — it registers `Name` as a callable Shikigami |

**The world body opens with the empty Record**, not with no value at all. The
second reads better — the world *is* built from nothing — and cannot work:
`Read Source` is the only primitive that accepts a nil input, so `Apply` and
every other stage would have nothing to act on, and a game that does not read
a file could not state its world at all. `{}` costs one ignored lambda
parameter and makes the common case writable.

**Every role name answers "when".** `World` is first, `Start` is once before
the first frame, `On` is on input, `Every` is on a timer, `Reply` is on a
reply, `Draw` is each frame, `Ending` is last. That is the organizing rule,
and it is why the table is sorted by lifecycle rather than alphabetically.
`Entity` is the single exception, and it is the single role that is not in
the lifecycle at all — it is a definition, which is why it sits below the
rule rather than inside it.

Name-by-name, with the alternative each one beat:

- **`World`** over `State` / `Model`. `Model` is the Bubble Tea word and
  leaks the host into the language; `State` is accurate and flat. `World` is
  what a game programmer already calls it.
- **`Entity`** over `Object`. `Object` is disqualified, not merely worse:
  `Cursed Object:` already declares a *global*. `Part Object:` declaring an
  entity one word away from it is a trap, and the two are otherwise
  unrelated. `Entity` also carries no OOP baggage for something that is
  really a `W → T` constructor.
- **`Start`** over an `"start"` event spec. Promoting it out of the spec
  string removes a piece of lifecycle hidden in a literal, and it gives the
  one-shot `Request` (fetch the room, load the level) an obvious home.
- **`On`** over `Event` / `When`. `Part On "key up":` reads as English;
  `Part Event "key up":` puts a noun where a connective belongs. `When` is
  the near-miss and is a fine substitute if `On` reads too terse.
- **`Every`** over `Tick` / `Timer`. `Part Every 120:` is the only spelling
  where the number lands naturally in the sentence.
- **`Reply`** over `Message`. Because D4 pins one request in flight per tag
  and a request is the only asynchronous source in the scope, `Reply` is
  exactly as broad as the thing is. `Message` promises a general mailbox
  that does not exist.
- **`Draw`** over `View` / `Render`. `Part View:` producing a `View` reads
  as a tautology; `Draw` is a verb like `Start`, and it pairs with the
  `draw(grid, f)` builtin one layer down.
- **`Ending`** over `Main` / `Outcome`. `Main` is disqualified: it means
  entry point in every language a reader has used, and this runs last.
  `Outcome` is the good alternative — it names the value rather than the
  moment. `Ending` wins only on the consistency rule above.

**Why none of these are themed.** The obvious themed set collides three
times over: `World` → `Domain` shadows both `Domain Expansion` and
`Simple Domain`; `Entity` → `Shikigami` collides with the definition syntax
*and* the call syntax, and is doubly confusing because an Entity really does
lower to one; `Draw` → `Reveal` collides with the output sink. The theme is
spent at the keyword level, which is where it earns its keep. A role is the
second word of a keyword line and its job is to be read at a glance.

`Part Entity` is the role that pays for itself twice: **it lowers to an
`ast.ShikigamiDef`**. The Part's body becomes the definition body, its label
becomes the name, its output type becomes the declared signature — and from
there it is an ordinary Shikigami, called by name, inlined at the call site,
optimized through. No new call machinery, no new inlining, no new
reserved-name rule; `checkShikigamiName` already refuses a label that names
a built-in.

`Part Ending` is what keeps a game inside the repo's testing discipline: it
gives every game a final stdout, so `domain battle`, the golden harness and
the docs harness all keep working on a program that is otherwise a picture.

**Event specs**, a closed table — not a pattern language. With `Start` and
`Ending` promoted to roles, nothing about the lifecycle hides in here any
more:

| spec | fires on | ambient |
|---|---|---|
| `"key <name>"` | that key (`up`, `q`, `ctrl+c`, `enter`, `esc`, …) | `key` — the name |
| `"key"` | any key press | `key` — the name |
| `"text"` | a printable keystroke | `key` — the text |
| `"resize"` | terminal resize | `width`, `height` |

An unknown spec is a resolve error listing the table. Key names come from
`tea.KeyPressMsg`, and the mapping lives in one function so the docs can be
generated from it.

**The timer period is milliseconds, and it is a literal.** `Part Every 120:`
means every 120 ms; the language server's inlay hint renders the unit after
the line so the number is never bare on screen. Several timers with
different periods are legal and cost one `tea.Tick` each — but the pattern
to teach is **one timer, and the body decides whether to act**
(`if frame() % w.gravity = 0 …`), which is how a variable rate is written
without a variable period. A measured period (`Every:` as a lambda over the
world, re-read after each fire) is a real want for tetris levelling and the
machinery for it already exists in `prims/measure.go` — **do not build it
now**, and record the seam so the later addition is small.

## Global state, safely **[decided — D5]**

`Cursed Object` / `Cursed Tool` already exist, and the naive move is to let
a game role write them freely. Don't. Two mutable stores — the world Record
*and* the global table — means state gets split between them by accident
rather than by design, and worse, it breaks the property the entire replay
oracle rests on: **a frame must be a pure function of the world value.** If
`Draw` can read something an `On` handler wrote outside the world, a frame
is no longer reproducible from state, and §E stops proving anything.

The rule, and it is stricter than AoC's rather than looser:

| Where | Policy |
|---|---|
| `Part World:`, `Part Start:` | `GlobalsReadWrite` |
| `Part On`, `Every`, `Reply`, `Draw`, `Ending`, `Entity` | `GlobalsReadOnly` |
| an `Advent of Code` `Part "1":` | `GlobalsIsolated` — today, unchanged |

**After `Part Start:` returns, every global is frozen.** Globals are for the
things a game has that genuinely do not change: board dimensions, the word
list, the server URL, the colour palette, level data loaded once, tuning
constants. Everything that changes is the world. There is exactly one
mutable store and exactly one place to look when a value is wrong.

Three things fall out, and the third is a surprise:

1. **Replay is exact**, because a frame is a function of the world alone.
2. **`Draw` is re-runnable** at any time for the same picture — which is
   what makes a terminal resize, and the step-through visualizer, correct
   rather than approximately correct.
3. **Read-only globals are the fast ones.** `programScan` already tracks
   which globals anything can mutate, and feeds it to `ast.GlobalRef.Mutable`,
   which decides "whether a stage reading one is still a pure function of
   its input" (`prims/globals.go`). Frozen globals are never mutable, so
   **every stage reading a game's globals keeps all its optimizer
   rewrites** — a `Draw` body reading `w` and a global board width folds
   exactly as if the width were a literal. The safe rule is also the
   fast one.

**Implementation is a third case in `sealedFrom()`** (`prims/globals.go:66`),
a function already built for precisely this: a seal carries its own `what`
and its own `why` because "they are sealed for genuinely different causes,
and one message covering both would be wrong about at least one of them."
The two existing seals are a `Channel` body and an imported Shikigami. This
is the third:

> a global cannot be changed from `Part On "key left"`: after `Part Start:`
> returns the globals are frozen, so that every frame is a function of the
> world alone. Put a value that changes in the world Record, or set this one
> in `Part World:` or `Part Start:`.

Note what this is *not*: it is not a restriction on reading. Every game Part
reads every global freely, which is the whole point of having them.

## The `View` type **[decided]**

One new `ir.TypeKind`, `KView`, alongside `KSparse` and `KGraph` — a shape
this repo has executed twice and knows the cost of.

```go
// ir/view.go
type ViewValue struct {
	Kind  ViewKind // VText, VStyled, VStack, VBeside, VBox, VMargin, VAlign
	Text  string
	Style ViewStyle // fg, bg, bold, dim, underline, reverse
	W, H  int       // VMargin, VAlign
	Align string    // VAlign: "left", "center", "right"
	Kids  []*ViewValue
}
```

It is opaque: not keyable, not orderable, not comparable, no arithmetic. It
renders two ways, and **both renderings are total functions of the tree**:

- `ir.RenderViewPlain(v) string` — no escapes, no colour. This is what
  `FormatValueTyped` uses, what the replay harness prints, and what the
  oracle diffs.
- `view.RenderStyled(v, width, height) string` — ANSI, via lipgloss. Only
  the host calls it.

Why a type rather than styled `Text`: because `length(styleit(t))` has to
mean something. With ANSI in a `Text` it counts escape bytes, every layout
builtin needs display-width logic against a value that lies about its own
size, and the footgun is permanent and unfixable. With `View` the question
cannot be asked — `length` does not accept one.

**`draw` takes no per-cell function.** An earlier draft wrote
`draw(grid, (cell) -> View)`, which cannot exist: the expression layer has no
higher-order builtins at all, and adding the first one to render a board would
be a large change in the wrong layer. It is not needed — `Map Cells` already
builds a board of glyphs on the pipeline layer, and `draw` renders whatever it
is handed (a Grid, a Sparse, or a List of rows or lines). Each layer keeps
doing what it is for.

**Layout happens once, and styling never moves anything.** The tree is laid
out into a rectangle of styled runs, and the two renderers — plain for a pipe,
ANSI for a terminal — differ only in how they serialize a run. If styling could
change where a column lands, a golden frame would be evidence about the
renderer rather than about the program.

**Codegen mirrors it rather than importing it**, exactly as
`codegen/sparsegen.go` mirrors `ir/sparse.go`: `codegen/viewgen.go` emits
`dmView`, `dmRenderView` and the style parser into the generated program.
The differential tests are what keep the two honest, which is the same
contract every other type in this language already lives under.

## Randomness **[decided]**

`ir/rand.go`: splitmix64, 30 lines, mirrored in `codegen/randgen.go`. The
stream lives on `ir.Context`. Builtins: `random(n)` → `Int` in `[0, n)`,
`randomf()` → `Float` in `[0, 1)`, `pick(xs)` → an element.

The seed comes from, in order: the replay script's `seed` line, `--seed`,
the clock. So a game is different every time you play it and identical every
time you test it — which is the only property that actually matters.

**Purity.** A lambda containing `random` is not a function of its arguments,
and every optimizer pass assumes it is. This is the exact situation `:=`
already created and already solved: a stage whose lambda updates a binding
"gives up its optimizer rewrites in exchange" (README). Reuse the mechanism
— an `ast.HasNondeterminism` walk beside `ast.HasInPlace`, consulted at the
same sites. **Task 2.C.2 includes a pass-by-pass audit**, not a one-line guard; this
is the single most likely place for a silent correctness bug in the whole
plan.

## Networking **[decided]**

```domain
Domain Expansion: Request
    Url: "https://arena.example/room/" + room
    Method: "POST"
    Body: (w) -> tojson({guess: w.guess})
    As: "guess"
    Into: {accepted: Bool, marks: List<Text>}
```

Fires and returns the world unchanged. The reply arrives later at
`Part Reply "guess":` with `reply` ambient. `Into:` is a written
`ast.TypeExpr` — the syntax already exists for Shikigami signatures —
because Domain is statically typed and a decoder needs a target.

**The reply's shape is `{ok: Bool, error: Text, value: T}` [decided, D3].**
Every failure mode lands in the same record: transport error, non-2xx
status, timeout, and a body that does not decode against `Into:` all arrive
with `ok: false`, `error` set to a sentence, and `value` set to the **zero
value** of `T`. A game therefore has exactly one shape to branch on, and
"the server is down" is a state it can draw ("reconnecting…") rather than a
crash.

`ir.ZeroValue(t *ir.Type) Value` **does not exist yet and has to be built**
(task 2.C.0): `Int` 0, `Float` 0.0, `Text` "", `Bool` false, `List`/`Set`/
`Map`/`Grid`/`Sparse`/`Graph` empty, `Tuple`/`Record` recursively zeroed,
`View` `blank()`. Like every value-model function it needs a codegen mirror
and a differential test, because a zero that differs between backends is a
divergence that only shows up when the network is already failing — the
worst possible time to discover it.

### One request in flight per tag **[decided, D4]**

Firing a `Request` tagged `T` while another `T` is outstanding **supersedes**
it: the new request goes out, and the old one's reply is never delivered.

This is a correctness rule before it is a policy. **HTTP replies can arrive
out of order.** A hide-and-seek client polling positions at 5 Hz against a
server that sometimes takes 500 ms will, under any unbounded scheme, receive
a reply from three requests ago *after* a newer one and merge it — and the
symptom is other players teleporting backwards, which reads as a game bug
rather than a networking one and will cost a day to find. Bounding
in-flight requests to one per tag makes the interleaving unrepresentable
instead of merely unlikely, and it caps the backlog a slow server can build.

**Supersede rather than drop-the-new-one**, because a request body is
normally derived from the current world: superseding sends where the player
*is*, while keeping the older request would send where they *were*.

**The silent loss is made visible.** `pending(tag) -> Bool` reports whether
a tag is outstanding, so a program that must not lose a reply guards
explicitly:

```domain
Part On "key enter":
    Cursed Technique: Apply
        Using: (w) -> if pending("guess") then w else submit(w)
```

That is the wordle case, and writing the guard is the right amount of work
for the guarantee it buys. Distinct conversations get distinct tags — a
position sync and a chat poll are two tags, not two requests on one. Tags
are string literals so that `Request` ↔ `Part Reply` matching is a
**resolve-time** check in both directions: a `Request` no `Reply` handles,
and a `Reply` no `Request` fires, are each an error naming the other side.
Those are the two typos that would otherwise cost an hour of staring at a
game that simply does nothing.

Defaults and limits, all overridable on the statement: `Method:` GET,
`Timeout:` 5000 ms, and a response-size cap (8 MB) so a hostile or broken
server cannot exhaust a game's memory. The cap and the timeout are
themselves the reason `ok: false` has to be a first-class value.

Blocking requests are not offered. A 200 ms round trip inside a frame is a
200 ms frozen frame, and a language that makes that easy to write is a
language that makes it hard to notice.

`Channeled Energy: Convert From JSON` / `Convert To JSON` do the same
conversion for a `Text` already in hand, so a scenario file or a saved game
works without the network.

## The vocabulary, counted

**Four new primitives.** `Domain Expansion: Request`,
`Simple Domain: Quit`, `Channeled Energy: Convert From JSON`,
`Channeled Energy: Convert To JSON`. That is all — the render vocabulary is
entirely expression-layer, and the structure is entirely `Part` roles.

**About twenty new builtins.** Render: `text`, `style`, `stack`, `beside`,
`box`, `margin`, `align`, `fit`, `draw(board)`, `blank`. Random: `random`,
`randomf`, `pick`. Time: `frame`, `elapsed`. JSON: `tojson`, `fromjson`.

**Name collisions checked, and they are real.** `rows(g)` and `cols(g)`
already mean a grid's dimensions (`docs/ref-builtins-list.md:64`) and
`size(m)` already means a map's cardinality — which is why the stacking
builtins above are `stack` / `beside` / `fit` rather than the obvious
spellings. `margin` is not `pad` because `padd` (point addition), `padleft`
and `padright` are all taken and a fourth near-spelling would be cruel.
Number-to-text is `totext(n)`, which exists; do not add `str`.

**Before adding any builtin in §B or §C, grep
`typecheck.builtinArity` (`typecheck/typecheck.go:242`) for the name.** The
176 existing builtins have taken most of the short English words a renderer
wants.

Each needs its `typecheck` case, `eval` case, `codegen` case, catalog entry
and **two runnable doc examples** (constraint 6). That last is the real
cost, and §E is what makes it payable.

---

## The replay oracle

The `Game Dev` scope would otherwise be untestable, and untestable in three
separate ways at once: the repo's differential rule wants byte-identical
stdout from interpreter and binary (constraint 3), the docs harness wants
two runnable examples per documented feature (constraint 6), and a game
produces a picture rather than an answer.

One mechanism answers all three. A **replay script** drives a game
headlessly and prints frames as plain text:

```
seed 12345
size 40 20
key right
tick
tick 500
frame
reply guess {"accepted": true, "marks": ["hit", "miss"]}
frame
quit
```

Frames go to stdout, so a game diffs exactly as an AoC answer does — the
golden harness, the differential harness, `domain battle` and the docs
fences all keep working with no new machinery.

Three properties make it exact rather than approximate, and each is bought
by a decision above: the seed is in the script (§Randomness), so randomness
is reproducible; globals are frozen after `Part Start:` (§Global state), so
a frame is a pure function of the world; and **`Request` never touches the
network in replay** — the script supplies the replies — so the multiplayer
games are fully testable with no server at all.

The mode is chosen the way `Read Source` already chooses between a file and
stdin: **play when stdout is a TTY, replay when it is not.** So no doc
example and no test needs a flag, and the browser playground
(`cmd/domain-wasm`, which has neither a TTY nor a filesystem) runs games
without special-casing.

One thing to write down rather than discover: interactive play draws after
every message, because Bubble Tea calls `View` after every `Update`, while
replay draws only on a `frame` line. Both backends run *replay* for the
oracle so they agree with each other, but a played session and a replayed
one are not frame-for-frame comparable.

## Compiling a game

`domain build` on a `Game Dev` program emits a Go program that imports
bubbletea and lipgloss, with a `go.mod` whose `require` lines are
**generated from this repository's own `go.mod`** and drift-tested, so an
emitted program can never ask for a version the `domain` binary was not
built against. `go.sum` lines are embedded from the committed one, and
`CGO_ENABLED=0` stays — every dependency is pure Go.

Two consequences, both accepted:

- A `Game Dev` binary is ~8–10 MB and needs the modules at build time
  (cached after the first build). The README's self-contained,
  stdlib-only, ~1.5 MB claim becomes **scope-qualified**, and has to be
  reworded wherever it appears.
- An `Advent of Code` build must be **byte-identical to today's** — same
  bare `go.mod`, same flags, same stdlib-only binary. That is pinned by a
  golden test, because it is the promise this change is most likely to
  break by accident.

The `View` render tree is **mirrored into the emitted program** rather than
imported from `domain/ir`, exactly as `codegen/sparsegen.go` mirrors
`ir/sparse.go`. codegen generates its own runtime; that is the house
pattern, and the differential tests are what keep the two honest.

---

# Findings from the design review

Ten things the first two drafts got wrong or left out, found by reading the
code rather than by reasoning about it. F1 is a design error; the rest are
gaps. Each is folded into the tasks below.

**F1 — event payloads are named bindings, not ambient parameters.**
The first draft said `key` and `reply` would reuse `prims/ambient.go`, the
mechanism behind the `Simple Domain: For` loop variable. That is wrong, and
wrong in a way that would only have shown up once the second game was
written. `ambient.go` binds params **positionally** — "lambdas bind ambient
params positionally, not by name" — and `requireLambda`
(`prims/higher_order.go:85`) demands an exact arity match:
`wantArity := arity + ambientDepth()`. So a `Part On "key":` pushing an
ambient would force **every** `Using:` lambda in that body to declare a
trailing parameter, used or not — **including the lambdas inside any
Shikigami inlined there**. `Shikigami: Step Snake` could then be called from
`Part Every` (depth 0) or from `Part On` (depth 1), but never from both.
That is fatal: sharing logic between a timer and a key is the first thing
any of the six games needs.

The right mechanism is the one `Consider` already uses
(`prims/locals.go` `runtimeBind`): a name computed when its scope opens and
"read by name out of the environment that eval and typecheck seed every
lambda with... **The lambda that reads one is left exactly as written.**"
Arity is untouched, so Shikigami stay reusable across every role:

```domain
Part On "key":
    Cursed Technique: Apply
        Using: (w) -> if key = "up" then turn(w, north) else w
```

A payload name behaves exactly as if a `Consider` had been written on the
Part, shadowing rules included — an inner `Consider key As …` hides it,
which is consistent and worth one line of documentation.

**F2 — `resolvePart` must stop requiring an upstream value.**
`prims/part.go:33` refuses a Part whose `cur` is nil ("has no upstream value
to branch from"). In a `Game Dev` program there is no top-level pipeline at
all, so **every** Part resolves with `cur == nil` and the role's `In`
supplies the type instead.

The consequence is a gift rather than a cost: `Part World:` genuinely begins
with no value, which is exactly the precondition `Read Source` demands
("must be the first stage; it takes no input"). So
**`Cursed Energy: level.txt` is legal as the first statement of
`Part World:`** and Oregon Trail's scenario file and tower defense's level
data load with no new primitive and no new source vocabulary.

**F3 — a top-level `Cursed Object:` in a game may only use `As`.**
`Of` means "of the current value" and a game's top level has none. `Of`
forms belong in `Part World:` or `Part Start:`, where a value exists. The
error should say that rather than reporting a nil type.

**F4 — nine `interp.Run` call sites, and every one is a hard gate.**
`runner/runner.go:732`, `mahoraga/recon.go:198`,
`cmd/domain-wasm/main.go:167`, `cmd/domain/visualize.go:195`,
`cmd/domain/repl.go:324`, `cmd/domain/repl_stats.go:58`,
`cmd/domain/dev_run.go:130`, `cmd/domain/main.go:411`,
`cmd/domain/repl_visualize.go:76`. A site that keeps calling `Run` on a game
pipeline walks the Part nodes linearly as passthroughs in source order and
prints nothing — a silent wrong answer, not a crash. Each must call
`RunScoped` or refuse with a sentence. This is §G promoted out of "the rest
of the toolbelt" into its own gate, with a test per site.

**F5 — `width()` and `height()` builtins.** `Draw` needs the terminal size
and only `On "resize"` was going to have it. Two more builtins, replay-fed
by the script's `size` line. (Now ~23 builtins, not ~20.)

**F6 — `View` is not comparable.** `=` on a `View` is a resolve error, and a
Record containing one is not comparable either. This has to be stated
because `Iterate Until Fixed Point` and the generated `dmEqN` structural
equality both rest on comparability, and a world Record holding a cached
`View` would otherwise reach them.

**F7 — `Simple Domain: Quit` is illegal in `Draw` and `Entity`.** Both are
pure by contract; a Quit in either is a resolve error naming the roles that
do accept one (`Start`, `On`, `Every`, `Reply`).

**F8 — ctrl+c always quits unless the program handles it.** Otherwise a game
with no quit handler is unkillable, and the first thing anybody writes has
no quit handler. If the program declares `Part On "key ctrl+c":`, that wins.

**F9 — no `bubbles` components.** Text entry is `On "text"` plus
`On "key backspace"` plus a world field, not a wrapped `textinput`. It keeps
the vocabulary honest, keeps the emitted binary's dependencies to
bubbletea + lipgloss, and is about fifteen lines of Domain in the two games
that need it.

**F10 — `Draw` cadence differs between playing and replaying.** Interactive
play draws after every message (Bubble Tea calls `View` after every
`Update`); replay draws only on a `frame` line. Both backends run *replay*
for the oracle, so they agree with each other — but nobody should expect
frame-for-frame parity between a played session and a replayed one, and the
reference has to say so.

---

# Decisions, settled

All five open decisions were answered on 2026-08-21. They are recorded here
with the reasoning, because a plan that only records the *what* invites the
next reader to relitigate the *why*.

**D1 — the import keyword: `Inherited Technique:`.** In JJK a technique you
were handed rather than one you worked out, which is what a library is.
§1.1–1.2 and §1.7 implement it; `prims/imports.go` is not otherwise touched.

**D2 — role names: `World` / `Start` / `On` / `Every` / `Reply` / `Draw` /
`Ending`, plus `Entity`.** See §"The role table" for the name-by-name
reasoning, the organizing rule (every lifecycle role answers *when*), and
why the themed alternative collides three times over. Changed from the first
draft: `Object` → `Entity` (it sat one word from `Cursed Object`, which
declares something entirely different), `Message` → `Reply` (D4 makes it
exactly that narrow), `Main` → `Ending` (`Main` means entry point
everywhere, and this runs last), and `Start` was promoted from a magic
`"start"` event spec to a role of its own.

**D3 — the reply value is `{ok: Bool, error: Text, value: T}`.** One shape
for every failure mode, so "the server is down" is a state a game can draw
rather than a crash. Requires `ir.ZeroValue` (task 2.C.0), which does not
exist today.

**D4 — one request in flight per tag, superseding.** Argued in
§"One request in flight per tag": out-of-order HTTP replies are a
correctness hazard rather than a preference, and bounding the tag makes the
bad interleaving unrepresentable instead of merely unlikely. `pending(tag)`
makes the one thing it costs — a dropped reply — visible to the program.

**D5 — globals are frozen after `Part Start:` returns.** `GlobalPolicy` per
role, argued in §"Global state, safely": one mutable store (the world), one
immutable store (globals, for board size / word list / palette / level
data). It keeps a frame a pure function of the world — which is what §E's
oracle rests on — and, because `programScan` already tracks mutability,
frozen globals keep every optimizer rewrite for the stages that read them.

## Still to decide, later and cheaply

None of these block a task; each is recorded so it is not rediscovered.

- **A measured `Every:` period** (a lambda over the world, re-read after
  each fire), which tetris levelling wants. `prims/measure.go` already has
  the machinery. Deliberately deferred: the one-timer-plus-a-counter pattern
  covers it, and the seam is cheap to open later.
- **Splitting an `aoc` pack out of `Core`**, which only becomes worth doing
  if some scope ever wants to *refuse* a primitive. Nothing here does.
- **Whether `View` should be diffable** for partial repaint. Bubble Tea
  handles this today; revisit only if a game is measurably slow.
- **`Graph` is missing from all three editor type-name lists** — a
  pre-existing gap, closed in passing by task 2.B.6.

## Implementation

[The `Game Dev` scope — implementation plan](../plans/2026-08-21-game-dev-scope.md).
