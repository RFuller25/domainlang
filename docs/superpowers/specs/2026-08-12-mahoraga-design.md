# `domain expansion: mahoraga` — adapting a program to one input

```
domain expansion: mahoraga <file.domain> <input> <expected> [flags]
```

Eight turns of the wheel. Each one measures the program, finds what is
costing it time *on this input*, and adapts. What comes out is a binary tuned
to one problem, a JSON recipe recording every edit and what it bought, and a
verdict that can be argued with.

## What this is not

`optimize` and the 29-pass IR optimizer ask **"what is true of all
programs?"** — every rewrite must hold for every input, forever, which is why
`docs/optimizer.md` spends most of its length on safety rules.

Mahoraga asks **"what is true of *this* run?"** and is allowed to exploit
anything it can measure. That licenses things the optimizer must never do:

- cutting a `Part` that this input never enters;
- picking an algorithm that is worse asymptotically but better at this exact
  size;
- sizing an allocation from a line count read off this file;
- turning *off* an optimizer pass that pessimises this particular program.

These are not weaker optimizations. They are answers to a different question,
and the general optimizer is not permitted to ask it. Keeping the two apart is
the point: nothing mahoraga discovers is allowed to leak into the optimizer's
pass list, and nothing in the optimizer's safety rules constrains mahoraga.

## The contract: verified while adapting, not while running

Aggression is only tolerable with a promise attached, and this is the one
mahoraga makes:

> Every assumption an adaptation rests on is **verified against the real input
> while mahoraga is searching**. The adapted binary carries no checks, because
> the checking already happened.

The verification is build-time work, not run-time work. A pinned binary is a
pure fast path: the assumptions were true when it was made, mahoraga proved
they were true, and the binary spends not one instruction re-establishing it.
Paying for a startup check on every run — forever — to re-derive a fact that
was settled once during the search would be exactly the kind of waste this
command exists to remove.

What that costs, stated plainly rather than buried: **a pinned binary is bound
to its input contract, and running it on input that violates the contract may
produce a wrong answer with no warning.** It is not a general program and must
not be treated as one. The recipe records the contract precisely so a human,
or a replay, can check.

### Three tiers

| Tier | Valid for | Runtime cost | Verified |
|---|---|---|---|
| **general** | any input | none | nothing to verify |
| **guarded** | any input; fast path for the observed shape | an inline check it earns back | the fallback is exercised in testing |
| **pinned** | inputs meeting the recorded contract | **none** | at search time, against the real input |

The guarded tier keeps its inline check because that check is *what makes it
general* — it buys the fallback, and it is only accepted when the fast path
pays for it. The pinned tier gives up generality and, in exchange, gives up the
check entirely. The trade is now explicit in both directions.

The default tier is **pinned**: the command is named for a technique whose
entire character is adaptation to one opponent. `--tier guarded` stops short
for anyone who wants a binary that still works on anything.

### Where the verification happens

With no runtime guard, the search-time and replay-time checks are the whole
safety net, so they are load-bearing:

- **While searching** — every precondition is evaluated against the real input
  before the adaptation that rests on it is accepted, and the candidate is
  differentially checked against the naive pipeline on that input.
- **At replay** — `--replay` re-reads the input, re-evaluates every recorded
  precondition, and **refuses to build** if one no longer holds, naming it.
  Failing at build time is far better feedback than a binary that silently
  answers wrongly.
- **On demand** — `--verify <recipe> <input>` evaluates the contract against an
  input and reports, without building anything. Cheap, and the honest way to
  ask "can I still use this binary?"

### Pin contracts

A pin is a conjunction of *assumptions*, each with a test mahoraga evaluates
against the real input while searching. None of these tests is compiled into
the binary:

| Assumption | How mahoraga establishes it |
|---|---|
| line count is exactly N | count while reading |
| every row is exactly W characters | width check per row |
| every value fits in int32 | range check while parsing |
| the input is pure ASCII | one scan, or free during parse |
| map keys are dense in [0, K) | max key while inserting |
| the grid is rectangular, R×C | already checked by `Convert To Grid` |

`--pin=exact` instead records a hash of the input bytes and admits nothing
else. It is available for the case where the input genuinely is fixed forever,
but it is not the default: a hash tells you nothing useful when it fails, and
breaks on a trailing newline.

## Why it cannot just print the answer

The failure mode of any autotuner given one input and one expected output is
that every gradient points at `print(42)`. Three mechanisms stop it, and the
first is the real one:

**1. The search space is a closed catalogue.** Mahoraga does not mutate code
freely. Every candidate is an entry from a fixed catalogue of transformations,
each with a precondition, and *no entry replaces a computation with its
result*. Whole-program constant folding is unreachable because nothing in the
catalogue does it — not because a checker rejects it afterwards. This also
disposes of the other problem with free mutation: candidates are valid by
construction instead of being 99% code that does not compile.

**2. The expected output never reaches the code generator.** The component
that checks correctness takes the candidate's stdout and returns a `bool`. It
does not hand those bytes to anything that generates code, and the search
state carries no copy of them. Structurally, the answer cannot get into the
program.

**3. Held-out inputs.** For the general and guarded tiers, candidates are
additionally checked against inputs mahoraga generates itself, with the naive
interpreted pipeline as the reference implementation — no expected output
needed, since `--no-optimize` *is* the specification. A memoized answer fails
the second input instantly.

Generation reuses the idea from the dropped `fuzz` design: a Domain program's
parse prefix is a declarative description of its input. `Split Text by "\n"`
then `Convert To Integers` says the input is lines of integers; `Convert To
Grid` says a rectangular character grid. That is enough to manufacture
structurally valid variants, and it is a fraction of the full fuzz command.

For the **pinned** tier there is no held-out check to run: the binary is bound
to its contract and a held-out input is simply outside it. What is tested
instead is that mahoraga *establishes every precondition against the real
input* before accepting the adaptation that rests on it — the assumption is
never taken on faith, and never inferred from the expected output.

Belt and braces, in the test suite rather than the search: the expected
answer must never appear as a literal in the emitted Go, and an adapted
binary's runtime must still grow with input size.

---

# The eight turns

Turn 1 is reconnaissance — Mahoraga has to be hit before it can adapt.
Turns 2–8 each adapt to what turn 1 (and every turn since) measured. A turn
that finds nothing turns the wheel anyway and says so.

## Turn 1 — Baseline and reconnaissance

No adaptation. Establishes what everything else is measured against.

- Build the program as `domain build` would: compiled, optimized.
- **10 runs**, correctness checked against `<expected>` every time — a program
  whose output varies between runs is reported and the search stops, because
  nothing downstream means anything.
- The baseline figure is the **mean** of those 10 runs, as specified, *and*
  the minimum and standard deviation are recorded. The mean is the headline;
  the deviation sets the noise floor that every later "win" has to clear.
- Collect a **CPU profile** (see §Infrastructure) and a **node trace**: which
  IR nodes evaluated, how many times, over what sizes.
- Read **input facts**: byte count, line count, line-width distribution,
  integer ranges, alphabet, rectangularity, key density. This is the evidence
  every later precondition is tested against.

## Turn 2 — Dead for this input

The node trace says exactly what never ran. Cut it.

- A `Part` never entered; a loop body that ran zero times; a `Filter` whose
  predicate was never true (or never false); a conditional arm never taken; a
  `getor` default never reached.
- Guarded where the condition is cheap to re-test, pinned where it is not.
- This is where the `coverage --dynamic` tracer pays off, with one change: it
  counts by primitive *name*, and this needs node *identity*.

## Turn 3 — PGO

Rebuild with turn 1's CPU profile via `go build -pgo=`.

Semantics-preserving, automatic, and exactly "adapt to observed behaviour".
`bench/README.md` found PGO to be noise across its suite — on flat single-file
mains with nothing to devirtualize — but that is a claim about those programs,
and it costs one build to re-ask about this one. Kept only if it clears the
noise floor.

## Turn 4 — Pass ablation

Turn each of the 29 optimizer passes off, one at a time, and measure.

29 builds; entirely tractable. What it finds is passes that *hurt this
program* — which the general optimizer cannot know, because it is not allowed
to know anything about the input. A pass that pessimises here is switched off
in the recipe and reported.

General tier: the optimizer's passes are semantics-preserving in both
directions, so removing one cannot change the answer.

## Turn 5 — Pass ordering and rounds

Reorder what turn 4 showed matters, and tune `maxRounds`.

Not permutations — 29! is not a search space. Adjacent-group swaps among the
passes ablation flagged as significant, plus a small number of seeded
hill-climbing restarts, plus the round cap. Honest expectation, recorded here
so the results can be checked against it: **ablation will pay more than
ordering**, because the passes cascade to a fixpoint and order matters less
than presence.

## Turn 6 — Templated codegen edits

The catalogue proper. Each entry is a precondition measured in turn 1, a
transformation, and an expected effect. The starting set comes straight from
the wins `bench/README.md` already established by hand:

| Entry | Precondition | Effect |
|---|---|---|
| exact preallocation | element count known from input facts | no growth reallocation |
| int32 narrowing | observed range fits int32 | halves working set |
| slice instead of map | keys dense in [0, K) | no hashing of a dense index |
| flat grid array | rectangular, dimensions known | index arithmetic, one allocation |
| ASCII fast path | no multibyte rune in any field | substring instead of `[]rune` |
| insertion sort | input nearly sorted (measured inversions) | beats pdqsort at low disorder |
| bounded heap for Top K | K small relative to N | never materializes the tail |
| single-probe counting | `Count By` over dense keys | one map operation per element |

Each is independently testable, independently reportable, and carries its own
tier (most are guarded; some are general).

## Turn 7 — Guarded specialisation

The same class of transformation, committing harder, always with a fallback
path compiled in. Specialised parse loops for a known line format; a fixed-size
stack buffer where the observed maximum fits; loop unrolling at a known trip
count; early exit where the trace shows the result was determined long before
the loop ended.

## Turn 8 — Pinned specialisation

Only entered when turns 2–7 left measurable time on the table. The same
transformations with the fallback removed and the assumption promoted into the
recipe's contract, verified against the real input during the search. Fastest,
and the tier that gives up generality entirely: the binary has no guard, no
fallback, and no way to notice a different input.

`--tier guarded` stops the wheel at seven.

---

# The recipe

`<stem>.mahoraga.json` is the durable artifact — reviewable in a diff,
replayable, and the only thing that knows why the binary is fast.

```json
{
  "program": "day11.domain",
  "adapted_at": "2026-08-12T19:04:11Z",
  "domain_version": "…",
  "input_fingerprint": {
    "bytes": 2066724, "lines": 300000, "sha256": "…",
    "facts": { "max_int": 999983, "ascii": true, "row_widths": [140] }
  },
  "baseline": { "mean_nanos": 1415000000, "min_nanos": 1390000000, "stddev_nanos": 21000000, "runs": 10 },
  "champion": { "mean_nanos": 402000000, "speedup": 3.52, "tier": "pinned" },
  "noise_floor_pct": 2.1,
  "adaptations": [
    { "turn": 2, "id": "cut-part", "tier": "pinned", "detail": "Part \"part one\" never entered",
      "precondition": "line count == 300000", "effect_pct": -11.4, "kept": true },
    { "turn": 4, "id": "pass-off", "tier": "general", "detail": "fuseMapMap pessimises this program",
      "effect_pct": -22.0, "kept": true },
    { "turn": 3, "id": "pgo", "tier": "general", "detail": "8.4M samples", "effect_pct": -1.1,
      "kept": false, "reason": "inside the noise floor" }
  ],
  "schedule": { "passes": ["simplifyLambdaBodies", "…"], "max_rounds": 9 },
  "build": { "flags": ["-pgo=cpu.pprof"], "env": {"GOAMD64": "v3"} },
  "pin": {
    "mode": "assumptions",
    "contract": ["rows == 140 chars", "values fit int32"],
    "verified_against": "day11.input",
    "note": "not compiled into the binary — checked here and at replay"
  }
}
```

Rejected adaptations are recorded too, with the reason. A recipe that only
lists wins hides the shape of the search.

**Replay** rebuilds the binary from source and recipe:

```sh
domain expansion: mahoraga --replay day11.mahoraga.json
```

It re-verifies rather than trusting: the correctness check runs again, and if
the recorded pin contract no longer holds for the input, it **refuses at build
time**, naming the assumption that broke. Since the binary carries no guard of
its own, this is the check — which is why replay re-reads the input rather than
trusting the fingerprint. A replay does not re-search, so it is fast and
deterministic, which is what makes the recipe committable beside the program.

`--verify <recipe> <input>` runs the same contract evaluation and reports
without building, for answering "can I still use this binary?" cheaply.

---

# The wheel

The TUI is not decoration here. A search that runs for an hour with no visible
state is a search nobody trusts or interrupts intelligently, and the wheel is
the honest representation of what the command is doing: turning, once per
adaptation.

Built on bubbletea v2 with lipgloss v2, following the established pattern —
`visualize_tui.go` for structure, `visualize_style.go` for the palette rule
(**style last**: pad and truncate plain strings, colour finished cells), and
the `tea.Tick` idiom already used in five places in this package.

## Layout

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  八握剣異戒神将魔虚羅 ・ MAHORAGA          day11.domain · 300,000 lines · 2.0 MB
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

                    ╭──── ◆ ────╮                  turn 4 of 8
                ╱                 ╲                PASS ABLATION
          ◆       ╲    │    ╱       ◆              candidate 17/29
          │         ╲  │  ╱         │              elideRedundantSort → off
          │    ────── ◉ ──────      │
          │         ╱  │  ╲         │              baseline    1.415 s
          ◆       ╱    │    ╲       ◆              champion    0.902 s  ▼36%
                ╲                 ╱                this        0.898 s  ◂ ahead
                    ╰──── ◇ ────╯

    ▁▂▃▅▅▆▇▇▇▇█  champion over time            elapsed 04:12 · 1,204 runs

  ADAPTED
    ✓ 2  cut Part "part one" — never entered        pinned    ▼11.4%
    ✓ 3  PGO from 8.4M samples                      general   ▼ 4.0%
    ✓ 4  fuseMapMap off — it pessimises here        general   ▼22.0%
    · 3  GOAMD64=v3                                 —         inside noise

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  space pause · s skip turn · a adaptations · r recipe · q finish and keep
```

## The wheel mechanics

Eight handles (`◆`) at the compass points of a fixed octagonal frame. The
geometry never rotates — ASCII rotation reads as noise. Motion comes from
**light** instead, which is both prettier and more legible:

- **Unlit handle** (`◇`, dim) — a turn not yet taken.
- **Lit handle** (`◆`, crimson→gold gradient by effect size) — a turn that
  adapted. Brightness encodes how much that turn bought, so the wheel is a
  chart as well as an ornament.
- **The sweep** — a bright cursor running the rim, one handle per frame.
  **Sweep speed encodes activity**: fast while a candidate is building or
  measuring, slowing to a drift while the search decides, locking still when
  paused. The user can tell from across the room whether it is working.
- **The adaptation flash** — when a candidate wins, the whole wheel pulses
  white for three frames, the handle for that turn ignites, and the spokes
  brighten from the hub outward. This is the one moment the animation is
  *about* something, so it gets the biggest gesture.
- **The hub** (`◉`) pulses at a slow constant rate throughout — a heartbeat,
  so a long quiet stretch never looks frozen.
- **Rejection** — a candidate that loses dims the sweep briefly to grey. Small,
  because rejections are the common case and a loud animation for them would
  be exhausting.

Frame rate: 12 fps via `tea.Tick`, independent of search progress. The search
runs on its own goroutine and posts messages; the model never blocks on it.

## Colour

The existing JJK palette from `banner.go`, through lipgloss so it downsamples
on limited terminals and drops entirely under `NO_COLOR`:

- Crimson `197` — Mahoraga, the wheel, the adaptation flash
- Gold `220` — lit handles at high effect size, the champion figure
- Purple `135` — the header rule and the title
- Cyan `51` — measurements and figures
- Green `84` — accepted adaptations
- Dim grey — unlit handles, rejected candidates, the noise-floor line

The sparkline uses the eight block characters `▁▂▃▄▅▆▇█` coloured along the
crimson→gold ramp, so improvement reads as heat.

## Screens

- **Wheel** (default) — the above.
- **`a` adaptations** — the full log, including rejected candidates with their
  measured effect and the reason for rejection. This is where an hour of
  searching becomes readable.
- **`r` recipe** — the recipe as it currently stands, live, so the artifact is
  never a surprise at the end.
- **`p` profile** — turn 1's hotspots, so the reader can see *why* the search
  is looking where it is looking.
- **final** — the verdict screen: baseline, champion, speedup, tier, the pin
  contract in full, and where the binary and recipe were written.

## Keys

`space` pause/resume · `s` skip the current turn · `a`/`r`/`p` screens ·
`↑`/`↓` scroll the log · `q` **finish and keep** (stops searching, writes the
current champion and recipe — not an abort) · `ctrl-c` abort without writing.

That `q` semantic matters for a command with no time limit: the user must be
able to say "good enough" at any moment and walk away with everything found so
far.

## Testability

The model is driven entirely by messages — `turnStartMsg`, `candidateMsg`,
`measuredMsg`, `adaptedMsg`, `rejectedMsg`, `tickMsg`, `doneMsg` — so tests
inject a scripted sequence and assert the rendered frame, exactly as
`visualize_tui_test.go` and `repl_tty_test.go` do. No test ever runs a real
search.

`--plain` runs the identical search headless, printing one line per event.
The search is the product; the wheel is the skin, and neither depends on the
other.

---

# Search discipline

Without this the whole command is a random number generator with good
typography.

**The noise floor.** Turn 1's ten runs give a standard deviation. Any candidate
whose improvement does not clear it by a stated margin is **rejected and
recorded as "inside noise"**, never quietly kept. `bench/README.md` sets the
precedent: *"the elimination is real and the speedup is not — ±1%, inside the
noise."*

**Early abandonment.** A candidate measurably worse after two runs is dropped
without completing ten. Most candidates lose; paying full price for each is
what turns an hour into a day.

**Champion re-measurement.** A champion selected across thousands of noisy
measurements is partly selected *for* favourable noise. So the final champion
is re-raced against the baseline in a fresh, alternating, full-length
measurement, and **that** number is the one reported. If it fails to reproduce
the improvement, the recipe says so.

**Interleaving.** Candidate and champion are measured alternately, never in
blocks, so drift lands on both — the same rule `runner` already enforces.

**Determinism.** `--seed` fixes every random choice, so a search can be
repeated exactly.

**Two things it must be able to say without embarrassment:**

- *"baseline unbeaten — the compiler had already found everything I can find"*
- *"this win is inside the noise floor"*

A tuner that always reports a win is not measuring.

---

# Infrastructure to build

| Piece | Where | Size |
|---|---|---|
| `Optimize` taking a **schedule** (subset, order, rounds) instead of a bool | `optimizer` | small; `Rewrite.Pass` already names them |
| `BuildBinary` taking **flags and env** | `codegen` | small; currently hardcoded |
| **CPU profile hook** in generated `main`, env-gated | `codegen` | mirrors the existing `DOMAIN_ALLOC_REPORT` hook exactly |
| **Per-node** execution counts | `interp`/`cmd` | the `coverage` tracer keyed by node identity rather than `Prim` |
| **Input facts** extraction | new | reads the input and the parse prefix |
| **Held-out input generation** | new | the parse-prefix generator from the dropped fuzz design |
| **The catalogue** | new, `mahoraga/` | the bulk; incremental — useful at six entries, better at twenty |
| **Pin contracts**: evaluation against an input, recording, replay refusal | new | no emitted code — this is all build-time |
| **Recipe** read/write/replay | new | plus `domain build --recipe` |
| **The wheel** | `cmd/domain` | bubbletea model, styles, animation |

Everything else — measurement, best-of-N, subprocess discipline, agreement
checking, peak RSS — is `runner`, already built.

---

# Testing

- **Catalogue entries** are unit-tested one at a time: precondition detection,
  the transformation, and a differential check that the transformed program
  agrees with the naive pipeline on the input its precondition was established
  against.
- **The contract**, as a property: for every catalogue entry, mahoraga must
  refuse to accept it when its precondition does not hold for the input, and
  `--replay` must refuse to build when a recorded precondition no longer holds.
  With no runtime guard these two refusals are the entire safety net, so they
  get the most direct tests in the suite.
- **No guard leaks into the binary**: the emitted Go for a pinned build
  contains no contract check, since the whole point is that it costs nothing.
- **Anti-memoization**: the expected answer never appears as a literal in
  emitted Go; an adapted binary's runtime still scales with input size.
- **Recipe round-trip**: search → recipe → replay → identical binary behaviour
  and identical measured speedup within the noise floor.
- **The wheel**: scripted message sequences, asserting frames — including the
  ones that are easy to get wrong (a turn that adapts nothing still advances;
  `q` writes the champion; a rejected candidate does not light a handle).
- **The search, end to end**, on a tiny program with `--turns 2 --runs 2`, in
  CI. Durations are never asserted; the sequence of events and the recipe's
  shape are.
- **Statistics**: a synthetic search fed measurements drawn from a known
  distribution with *no real effect* must report "baseline unbeaten". This is
  the test that catches a tuner fooling itself.

---

# Build order

1. **Infrastructure** — schedule-taking `Optimize`, flag-taking `BuildBinary`,
   the profile hook, per-node counts. No user-visible change; each testable
   alone.
2. **The skeleton** — turns 1, 3, 4, 5 (baseline, PGO, ablation, ordering) with
   `--plain` output only. These are all general-tier and semantics-preserving,
   so the whole search loop, the statistics and the recipe can be built and
   trusted before anything risky exists.
3. **The recipe and replay**, including `domain build --recipe`.
4. **The wheel.** Deliberately after a working headless search: an animation
   over a search that does not work yet is a way to not notice that it does not
   work.
5. **The catalogue** — turn 6, entries added one at a time, each with its
   tests. The command gets better as this grows and is useful before it is
   complete.
6. **Guarded specialisation** — turn 7, plus the fallback machinery.
7. **Pin contracts and turn 8** — last, because with no runtime guard the
   search-time and replay-time verification is the only thing standing between
   a pinned binary and a wrong answer, and it deserves to be built when
   everything around it is already stable.

---

# Out of scope

- **Nothing feeds back into the optimizer automatically.** When mahoraga finds
  a win that looks general, it says so in the report. A human decides whether
  it becomes a pass. The two commands answer different questions and the
  boundary is the point.
- **No free-form mutation of emitted Go.** The catalogue is closed. This is
  what keeps candidates valid and the answer unreachable.
- **No cross-input tuning.** One program, one input. Tuning against a corpus
  is a different command.
- **No distributed or parallel search.** Concurrent measurement would corrupt
  the measurements, and `interp.Run` serialises in-process runs behind a
  process-wide lock regardless (see `runner.Interpret`).

---

# Status

Built: infrastructure, the skeleton (turns 1, 3, 4, 5), the recipe, replay,
`domain build --recipe`, **the wheel**, and **the catalogue** (turns 6 and 8).
Not built at the time of writing: turn 2 and guarded specialisation (turn 7).
Both are built now; see the final status section.

Where the wheel departs from the design above, and why:

- **The sweep runs through the arms, not around a rim.** An octagon drawn on a
  39×15 grid has a top edge that is ten columns wide and one row tall, which
  does not read as a rim at all. The arms are the shape, and the sweep is an
  angular highlight passing through them — brightest just behind, fading ahead,
  which is what gives the rotation a direction.
- **`p` is the champion's pass schedule, not turn 1's hotspots.** Rendering
  hotspots means parsing a pprof file for a display; the pass list is already
  in hand, and "which of the 29 passes are switched off for this program" *is*
  the content of what turn 4 finds. The profile still exists and still feeds
  turn 3.
- **No dim-on-rejection.** Rejections arrive every few seconds for the whole of
  turns 4 and 5, and a wheel that reacted to each would be reacting almost
  continuously — which would leave the adaptation flash nothing to stand out
  against. Rejections go to the ledger and the sparkline instead.
- **Sixteen frames a second, not twelve.** The sweep has 24 positions; at 12fps
  the slowest speed moved less than once a frame and read as a stutter.
- **The verdict is printed under the wheel, not shown in it.** The wheel takes
  the alternate screen, so a final screen would vanish on exit. What a reader
  keeps is `writeMahoragaVerdict` writing to the terminal the wheel gave back.
- **`--plain` is not the only headless path.** The wheel needs *both* stdin and
  stdout to be terminals; a pipe or a CI run gets the line-per-event reporter
  without anyone passing a flag.
- **One test runs a real search** (`TestWheelDrivesARealSearch`), against the
  spec's "no test ever runs a real search". Injected messages prove the display
  and nothing about the wiring — that the search's `Reporter` *is* the bridge,
  and that events survive the goroutine crossing in order. It is a one-turn
  search on a four-line program and costs about three seconds.
- **`Event` carries the champion's `Schedule` and `Build`** on an `Adapted`
  event. The recipe screen has to be live, and a display that reached into the
  search's state to read it would be reading a value the search is writing.

Two additions the design did not call for:

- **`Search.SkipTurn`** — `s` needed a way to abandon a turn without stopping
  the search. Turn 4 tries one candidate per optimizer pass and turn 5 sixteen
  orderings, so a turn that is plainly going nowhere is worth minutes.
- **An unbuilt arm is drawn dashed as well as dim**, so "there is no such turn
  yet" survives a screenshot, a colourless terminal and `NO_COLOR`.

**Open concern, unchanged.** The search has not yet been observed adapting
anything on a real program: every program tried so far reports `BASELINE
UNBEATEN`, with turns 4 and 5 finding nothing outside the noise. The wheel is
therefore beautiful over a search that has never visibly won. Everything it
draws for a win is exercised by tests, not by a real run.

---

# Status: the catalogue

Turn 6 and turn 8 are built, on a shared mechanism: `codegen.Tuning` (a
serialisable set of measured facts the compiler consults where it is already
guessing), `mahoraga/facts.go` (what the input is shaped like, plus the
baseline's allocation figures), and `mahoraga/catalogue.go` (the closed table).

The catalogue is smaller than the spec's table, and the entries are not all the
ones listed there. What is built, and why these:

| Entry | Precondition | Tier |
|---|---|---|
| exact list capacity | the emitted Go reserved `len/2+1`, and the segments were counted | guarded |
| collector off for one run | the baseline actually ran collections and its heap fits under a limit | guarded |
| no UTF-8 decoding | every byte of the input is one rune, and the program decodes runes | pinned |

**Turn 6 takes the entries with a fallback; turn 8 takes the ones without.**
The spec puts the whole catalogue in turn 6 and describes turn 8 as "the same
transformations with the fallback removed", which would mean writing each entry
twice. Splitting by tier gives turn 8 real content, makes `--tier guarded` stop
the wheel at seven for free, and keeps one definition per transformation.

**Turn 8 runs unconditionally**, where the spec enters it "only when turns 2–7
left measurable time on the table". Its entries are a handful of candidates
rather than a search, and "the pinned adaptation was tried and was inside the
noise" is a more useful thing for a recipe to record than its absence.

**Preconditions read the emitted Go, not only the IR.** Whether the backend
guessed a capacity or emitted a UTF-8 decode are facts about the *backend's*
choices, and the emitted program is the only place they have answers. Turn 6
generates the champion's source (a code generation, no build) and asks its
questions of that rather than of the baseline's, since turns 4 and 5 may have
changed the pass schedule underneath.

**Entries the spec lists and this does not**, with the reason:

- *int32 narrowing* — every integer in the backend is `int64`, in the helpers,
  the runtime and the emitted expressions alike. Narrowing is a type-system
  change across the whole generator, not a templated edit, and doing it as one
  would be the least reviewable change in this package.
- *slice instead of map, flat grid array, single-probe counting* — the map and
  grid sites the spec had in mind are already sized exactly from `len(in)`; the
  remaining wins there are representation changes that flow through every
  downstream operation's type.
- *insertion sort, bounded heap for Top K* — real, and each needs its own
  measured precondition (inversion count, K against N) plus a specialised
  lowering. They are the natural next entries.

**One entry the spec did not list and this does: the collector.** It is the
adaptation that makes least sense in general and most sense for one known run —
a program that lives fifty milliseconds and exits gains nothing by collecting —
and it is guarded rather than pinned because `SetMemoryLimit` is a real
fallback: a larger input crosses the limit, the collector turns itself back on,
and the program is merely as fast as it would have been. On a two-million-line
sum it is worth around 19%.

## Two measurement bugs this phase found

Both were found by the search failing to see wins that a hand-run A/B showed
plainly, and both are fixed:

- **Candidates were timed alone** against a champion figure taken minutes
  earlier, so machine drift landed entirely on the candidate. `finish()` always
  raced its two sides interleaved; `consider()` now does the same. On a shared
  box this was enough to read a real 15% win as "slower by 10". It also halves
  the builds — a candidate is built once and raced twice, rather than built
  twice.
- **The slow tail was treated as spread.** Interference only ever makes a run
  slower, so the top quarter of a wall-clock sample is other people's work.
  Leaving it in inflated both sides' standard error until nothing was
  distinguishable: one search measured minima of 44.2ms against 52.3ms and
  reported means of 51.4 and 62.4 that it could not tell apart. `Summarize` now
  drops the slowest quarter after the warmup. bench/README.md reached the same
  conclusion long ago and goes further, reporting the minimum outright.

## Reporting changes

- A candidate that measured faster and still could not be distinguished is
  counted as **inconclusive** rather than rejected, and the verdict says how
  many there were and that more runs would settle them. "The measurement could
  not answer this" is a different thing to tell a user than "this did not work",
  and only one of them is actionable.
- The pinned-binary warning is now decided by the adaptations that were *kept*,
  not by the tier the search was allowed to reach. A run at the default pinned
  tier that kept only general and guarded adaptations produced a program that is
  correct on any input, and used to warn about a pin nobody had made.
- `Verify` no longer treats guarded as unsafe on a different input. A guarded
  adaptation keeps a fallback — that is what makes it guarded — so it stays
  correct and only stops being optimal. Reporting it as unsafe made "guarded"
  and "pinned" the same tier in the one place the distinction is checked.
  `domain build --recipe` still refuses both, and for a different reason: it has
  no input at all to have measured anything against.
- The CPU profile is copied beside the recipe and recorded by name. It used to
  be recorded as a `-pgo=` flag pointing into the search's temp directory, which
  is deleted on the way out — so any recipe that kept a profile-guided build
  could never be replayed.

**The open concern is closed.** Mahoraga now adapts on a real program: a
two-million-line sum comes out around 1.2× faster, from a profile-guided
rebuild and the collector entry, and the wheel lights handles 3 and 6.

---

# Status: all eight turns

Turns 2 and 7 are built, and pinned adaptations now carry a contract. Every
turn of the wheel does something, and the spec is implemented.

## Turn 2 — what the survey found, and what the turn became

The spec asks this turn to cut what never ran. **Nothing ever does.** A survey
of the whole corpus (`day1`, `day4`, `day5_full`, `day8_full`, `day15`,
`aoc2020_day1_part2`), interpreted under `runner.NodeCounter`, found exactly
four nodes with a zero call count across all six programs — all four in
`day15`, and all four the `Map Each`/`Filter` pairs that `fuseUnfoldStream`
folds into the `Unfold`. They never run because the *optimizer* removed them,
they emit no code, and "cutting" them would cut nothing.

That is a property of the language, not a gap in the turn. A Domain pipeline is
straight-line: a `Part` always runs, a `Channel` always runs, and the only
constructs that can skip work are per-element (`Filter`, `Take While`) or a
loop that makes zero laps — which already costs nothing to skip.

So the turn is built around the *second* item on the spec's own list for it: **a
`Filter` whose predicate was never false**. That is real, input-dependent, and
worth removing — a filter over two million elements that discards none of them
still evaluates its predicate two million times and still copies the list. The
general optimizer cannot see it, because whether a predicate ever fails is a
property of the data. The turn is renamed "idle for this input" to say what it
actually does.

Three things make it safe:

- **A whitelist.** Only `Filter`, `Filter Entries`, `Unique` and `Merge Ranges`
  are eligible, because only for those does an unchanged length imply an
  unchanged value. A `Sort` preserves length and reorders; a `Map Each`
  preserves length and replaces every element. There is a test that fails if
  anyone widens the list carelessly.
- **Every evaluation, not a sample.** `NodeStat.SizePreserving` starts true on
  a node's first call and can only be falsified, so a filter that dropped one
  element on one lap out of four hundred is not idle.
- **Only stages that emit code.** The candidates are intersected with
  `EmitAnnotated`'s span map, which is what excludes the fused-away nodes the
  survey turned up.

Removal is expressed as `codegen.Tuning.ElideNodes`, keyed by primitive and
source position rather than by node index — an index into "the seventh node"
does not survive the pass-schedule changes turns 4 and 5 make, and a position
is what the user wrote. A key matching nothing is ignored, so a stale recipe
builds a slower program, never a wrong one.

Pinned tier, unavoidably: a removed filter is removed.

## Turn 7 — guarded specialisation

The catalogue gained a second axis. An entry now has a *kind* as well as a
tier, and the two together decide which turn runs it:

| Turn | Runs |
|---|---|
| 6 | parameter edits at general or guarded tier |
| 7 | guarded specialisations — a fast path with the general path compiled beside it |
| 8 | anything pinned |

Every entry therefore runs in exactly one turn, which keeps a win from being
counted twice.

The first turn-7 entry is the guarded ASCII grid builder: `dmASCII(line)`
chooses per line between the byte-indexed loop and the rune decode, so the
program is correct on any input, takes the fast path on plain lines, and takes
the fast path *line by line* on mixed input. Its pinned twin in turn 8 is the
same fast path with the check and the fallback removed. That pair is what makes
`--tier` a concrete choice rather than a procedural one — and both were tried
on a 400×400 grid, where the guarded form measured 3.1% *slower* (the scan is
not free) and the pinned form 0.7% faster (inside the noise). Honest, reported,
kept out of the champion.

## Pin contracts

A pinned adaptation used to bind a recipe to one input by SHA-256. That is
correct and needlessly strict: the assumption behind "no UTF-8 decoding" is
*every byte is one rune*, and any number of inputs satisfy it.

`Recipe.Contract` now records the assumption itself, split by whether it can be
re-established by looking at a candidate input:

- **Checkable** clauses — `ASCII`, `MinSegments` — are re-evaluated by
  `--verify` against the new input's measured facts, and a conforming input is
  accepted.
- **Unverifiable** clauses — "the Filter at line 4 kept every element" — cannot
  be answered without running the program. A recipe carrying one is still bound
  to the input it was adapted to, and the refusal names the clause that binds
  it rather than saying "pinned".

The contract is printed in the verdict and shown on the wheel's recipe screen,
because a reader who can see the assumption can decide for themselves whether
their input satisfies it.

## Measured, end to end

A 400×400 grid program with a `Filter` that keeps every line, at `--runs 9`:

```
turn 2 — idle for this input
  drop Filter at line 3 — kept every element (400 elements)
    · 0.3% — below the 2.0% worth recording
turn 6 — templated codegen edits
  collector off for one run (1 collections in the baseline)
    ✓ adapted — 41.0% faster (6.50ms), tier guarded
turn 7 — guarded specialisation
  ASCII fast path with the decode kept as a fallback
    · slower by 3.1%
turn 8 — pinned specialisation
  no UTF-8 decoding (every byte of the input is one rune)
    · 0.7% — below the 2.0% worth recording

  ADAPTED — 1.57× faster
```

All eight handles do something. Three of the four adaptation turns found
nothing on this program and said so, which is the behaviour the whole design
asks for everywhere else.

## What is left, and why

Catalogue entries the spec listed and this does not, unchanged from the last
status: int32 narrowing, slice-instead-of-map, flat grid arrays and
single-probe counting are type or representation changes flowing through every
downstream operation, not templated edits. Insertion sort and bounded-heap
Top K remain the natural next entries — each needs one measured precondition
and one lowering, and the machinery for both now exists.

The interpreter's process-global state (`eval.bindings`, `ir.currentCtx`, the
ambient For-loop stack, the two watchers) made `interp.Run` unsafe to call
concurrently. Turn 2 made this package a caller, so it is fixed rather than
documented: `interp.Run` now holds a process-wide mutex for the duration of a
run. Threading five stacks through every primitive's registration signature
would have been a redesign, and nothing in this tree interprets for throughput
— the timed path runs subprocesses precisely so in-process state cannot be
shared. `go test -race ./codegen` reproduced the race before the fix and is
clean after it.

---

# Status: what a recipe from the field found

A user's recipe reported `reverted_to_baseline: true` with `speedup: 1.016`,
and the wheel had drawn a candidate slower than the baseline as "best". Reading
it end to end turned up one root cause and three separate ways the search
failed to say what it knew. All four are fixed.

## The root cause: an abandoned reconnaissance run

`"reconnaissance_note": "the interpreted reconnaissance run did not finish in
1m30s"`. Turn 2 timed out and left the goroutine running, on the reasoning that
leaking one once per search was cheap. On a 712ms program it was not: the
interpreter kept a core busy and a 389MB heap live for minutes, and the search
measured against it. Plotted in the order taken:

```
712.7  turn 1 baseline (clean, before turn 2 started)
852.6  turn 3 PGO             ┐
927.0  fuseSortThenTopK       │  a ~20% plateau, thirteen candidates long
842.0  fuseLinearMapExtremum ✓ accepted as a 3.3% win, inside the window
3519.4 fuseUnfoldStream       │
772.0  fuseSortReverse        ┘  the runaway finishes here
713.1  elideRedundantSort
700.9  cancelReversePairs        back to normal for the rest
715.5  final re-measurement (clean)
```

Both clean anchors agree at ~713ms. The one "win" was found inside the
contaminated window, against a champion that was itself 22% off. The final
re-measurement caught it and reverted, which is the safety net working — but
the search had spent minutes measuring its own reconnaissance.

`ir.Interrupter` — the escape the REPL already uses for Ctrl+C — now stops the
run at its next node boundary, and `runInterpreterBounded` waits for it to
actually stop rather than returning while it continues. The regression test
runs an unbounded `While` loop, interrupts it, and asserts **no further node
evaluations after the call returns**; against the old code it reports 479,796
in the following 250ms.

## The display was dividing two different machines

`m.champion` held the accepted candidate's raw mean — 842ms, from the drifted
window — and `m.baseline` held 713ms from turn 1. `numbersLine` divided them:
`best 842ms  0.846×`, under a green tick.

The only drift-free quantity in a race is the ratio of its two sides, so
`Event` now carries the champion's own measurement from the same race, and the
displays track **the baseline scaled by the product of each accepted race's
ratio**. The champion's raw mean is never shown. The plain reporter prints both
sides of the race (`871ms → 842ms, raced alternately`), which would have made
this visible on the first adaptation.

## The baseline now runs in every race

The first attempt at this was a drift guard: refuse a race whose champion sat
more than ten percent from `baseline × bestRatio`. Run against a real search on
a CI container it refused **twenty-seven races out of fifty**, and would have
thrown away the genuine forty-one percent win the same program had found the
day before. A rare false positive traded for a constant false negative is a bad
trade, and the guard was the wrong shape: it needed an anchor precisely because
the baseline had been left behind in turn 1.

So the baseline runs in every race, as a third contestant beside the champion
and the candidate (dropped when the champion *is* the baseline binary, which is
most of a search that finds nothing). Three consequences:

- Every figure the search compares is measured in the same minute as the figure
  it is compared against.
- The champion's standing against the baseline — what the display calls "best"
  — is *measured*, one division, rather than accumulated as a product of ratios
  taken across several turns.
- Drift needs no guard, because there is nothing left for it to corrupt. It is
  recorded and reported (`drifted_races`) so a reader knows how quiet the box
  was, and rejects nothing: a busy machine now costs the search time rather
  than correctness.

It costs one extra set of runs per candidate, against builds that dominate the
wall clock anyway.

## The recipe described a binary that was not written

`revertToBaseline` cleared the schedule, the build and the tuning, and left
`Champion` and `Speedup` describing the champion — so a reverted recipe claimed
1.016× and a 704ms champion for a file that was byte-for-byte the baseline
build. They now describe the artifact, with the rejected figure kept beside
them in `overturned_champion`.

Also: `input_fingerprint.lines` counted raw `'\n'` bytes while
`input_facts.segments` counted pieces, so one recipe said "lines: 1" and
"segments: 2" about the same 55-byte file. Both were right by their own
definition and the pair was useless; the fingerprint now counts lines the way a
reader does, and a test asserts the two agree.

---

# Status: what a four-program benchmark suite found

`bench/mahoraga/` is four working Domain programs, four inputs, and the same
search run over each by two builds of the compiler. It exists because the
catalogue had been extended by reasoning about what *ought* to be slow, and the
only way to find out what actually is is to profile programs nobody wrote for
the benchmark. The four are a spiral walk (dominated by process startup — the
control), a jump-offset walk, fifty thousand rounds of memory reallocation, and
a pair of generators filtered down to five million elements each.

Three findings, in the order of how much they were worth.

## The list nobody could estimate: 2.2× on one program

A profile of the generator program spends **34% of its run inside
`runtime.growslice`**. The fused Unfold+Filter+take stream emits

```go
v19 := []int64{}      // and appends five million times
```

because nothing in scope predicts how many elements a filter will let through
— the `take` limit bounds it from above, but reserving five million elements
for a run that produces ten is forty megabytes nobody uses. So the generator
reserves nothing, and the slice is reallocated and copied twenty-two times on
its way to five million.

This is exactly the shape of question the command exists for, and the answer is
a measurement rather than an analysis: a **probe build** — a build of the same
program that reports how long each unestimated accumulator grew, run once,
untimed, and thrown away. `Tuning.ListCapacities` then reserves it. Guarded
tier, and genuinely so: a capacity is a hint `append` overrides, so the binary
stays correct on any input; what it can be on a different input is wasteful,
which is a cost the recipe records rather than a correctness claim.

Measured alone, on the generator program: **1.5s → 0.69s**.

## The scheduler: 4.4× on another

`i05_jumps` spends **half its run in `futex`, `madvise`, `osyield` and
`lock2`** — the collector's mark workers, four of them on a four-core box,
coordinating over a program that is a straight line of loops on one goroutine.
`GOMAXPROCS(1)` is one line in `main` and changes nothing about what the
program computes on any input, which is why the entry is general tier despite
reading like a machine setting. It is the largest single entry in the
catalogue and it is not a code transformation at all.

The collector entry beside it gained a twin. Switching the collector *off* is
right for a program whose whole heap fits under the limit that keeps it safe;
a program that allocates far more than it keeps reaches that limit whatever it
does, and what is available there is fewer, larger collections —
`SetGCPercent(400)` under a wider backstop.

## Pinned constants: real, and smaller than it looks

The entry the suite was built to evaluate. A `Consider l Of length` is a Go
local because the compiler cannot know what it holds; a probe build that
watches it hold 16 on every one of fifty thousand laps licenses emitting `16`,
and what that buys is not the arithmetic but what the Go compiler does next —
`% l` against a local is a hardware division and `% 16` is a mask.

Two things had to be got right for it to be worth anything at all, and both
were wrong in the first version:

- **Substitute at the reads, not at the definition.** A binding that reaches a
  block body's function arrives as a *parameter*, and a parameter is not a
  constant. A pinned binding is now substituted into the body and no parameter
  is declared for it.
- **One evaluation is not "cold".** The obvious threshold — do not spend a
  build on a binding evaluated once — is exactly backwards: the generator
  hoists a `Consider` out of the loop it was written in, so the binding a
  fifty-thousand-lap loop reads on every lap is evaluated exactly once.

Measured: **inside the noise floor on all four programs**, and the profile says
why — the modulus it optimizes is 6% of one program and absent from the other
three. It is kept, and reported as what it is. The mechanism is what the
capacity entry above is built on, the entry is honest about its size, and a
recipe that records "tried, and it was noise" is more useful than one that does
not mention it.

## What the A/B said, and the one thing it got wrong

The same four programs, searched by both builds at `--runs 7`:

| Program | before | after |
|---|---:|---:|
| `i03_spiral` | 1.00× | 1.00× |
| `i05_jumps` | 1.44× | **2.37×** |
| `i06_realloc` | 1.00× | 1.00× |
| `i15_generators` | 1.40× | **1.97×** |

The two unbeaten programs stayed unbeaten under five new kinds of candidate,
which is the result the control was there to check.

`i15_generators` is the one to read carefully, and the first version of this
section read it wrong. It claimed the search had thrown away a 28% win by
rejecting the measured-capacity entry. That number came from hand-racing the
binaries without discarding a cold first run — precisely the effect
`WarmupRuns` exists to remove, in a file that argues at length about
measurement — and it does not survive being measured properly. Seven
alternating rounds, first dropped, minima:

| `i15_generators` build | min |
|---|---:|
| `domain build` baseline | 1080ms |
| default schedule + `GOMAXPROCS(1)` | 753ms |
| the same plus both measured capacities | 642ms |
| the champion the search shipped | 646ms |
| the same plus both measured capacities | 643ms |

The capacity entry is worth about 15% where nothing else has got there. On the
champion the search actually found it is worth nothing: a shuffled pass order
had already reached the same place, and the screen's 0.1% is what a redundant
candidate correctly measures. **The search was right.**

Two things do survive. The first is that the printed speedup is not to be
trusted on a drifting box — 1.97× re-measures as 1.45× — which is a reporting
problem rather than a search one, and the drift figure beside it is the
warning. The second is narrower than the retracted claim and still true: a
screen at `--screen-runs 3` has two samples after the warmup, below
`MinSamplesForSpread` and below `MinSamplesForTrim`, so it compares two raw
wall-clock numbers carrying no uncertainty at all. `ScreenEffect` takes the
more favourable of the means and the minima there — the reasoning TrimSlowest
already rests on, applied where there is nothing else to reason with — and the
second look re-races the candidates whose rejection was "I cannot tell".
Neither changed an outcome on this suite. They are insurance, and the honest
status of insurance that has not yet paid out is *unproven*.

One smaller thing the same program taught, and this one is already fixed:
reserving either of its two five-million-element lists is worth 11% and
reserving both is worth 23%. Offering one site per candidate — the wheel's
rule everywhere else — put a real win on the wrong side of the noise twice.
Turn 6 now offers the whole set first and the individual sites after it.

## What is left

`set` on a List is excluded from the linear-accumulator pass by design
(`optimizer/linear.go`: `take`/`drop`/`slice` hand out subslices of the same
backing array, so "who else can see this" stops being a question about the
accumulator alone). `i05_jumps` is a loop threading a list through `set`, and
at full puzzle size it takes **33 seconds** — a million copies of a
384-element list — where an in-place update with the pass's existing
clone-once-on-entry firewall would take well under one. That is the largest
number this suite turned up and the only one that needs a new analysis rather
than a new measurement.
