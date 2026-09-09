# Mahoraga: adapting a program to one input

The optimizer's [32 passes](optimizer.md) all answer the same question: what
is true of *every* program that reaches them? That question is why the
optimizer's design is mostly safety rules — a rewrite that helps this input
but might be wrong on another isn't eligible, full stop.

```sh
domain expansion: mahoraga <file.domain> <input> <expected> [flags]
```

asks a different question: what is true of *this run*? It measures the real
input, tries a closed catalogue of adaptations against it, and keeps whatever
measurably wins — including things the optimizer is never allowed to
consider: switching off a pass that pessimises this one program, rebuilding
against a CPU profile of this one workload, or dropping a check whose outcome
this one input has already settled. What comes out is a binary tuned to one
problem, plus a JSON **recipe** recording every adaptation tried, what it
measured, and why the rejected ones were rejected.

The full reference — every flag, the eight-turn table, the current catalogue
entries, the wheel's keys — lives in
[cli.md](cli.md#domain-expansion-mahoraga). This page is the why, and the
parts of the story a flag reference has no room for.

## The contract: verified while adapting, not while running

Aggression like this is only tolerable with a promise attached:

> Every assumption an adaptation rests on is verified against the real input
> **while mahoraga is searching**. The adapted binary carries no checks,
> because the checking already happened.

| Tier | Valid for | Runtime cost | Verified |
|---|---|---|---|
| **general** | any input | none | nothing to — a pass schedule or build flag can't change what a program computes |
| **guarded** | any input; a fast path for the observed shape | one inline check, paid only when it earns its keep | the fallback is exercised in testing |
| **pinned** | inputs meeting the recorded contract | **none** | at search time, against the real input — again at `--replay` |

Stated plainly: **a pinned binary is bound to its input contract, and running
it on input that violates the contract can produce a wrong answer with no
warning.** It is not a general-purpose program. The default tier is pinned —
the command is named for a technique whose whole character is adapting to
one opponent — and `--tier guarded` is for anyone who wants a binary that
still works on anything.

`--verify <recipe> <input>` re-checks a pinned recipe's contract against a
new input without building anything, and `--replay` refuses to build at all
if a recorded assumption no longer holds, naming the one that broke. Failing
at build time beats a binary that answers wrongly and says nothing.

## Why it can't just print the answer

Hand a search "here is the input, here is the expected output, go" and the
obvious failure mode is that every gradient points at `print(42)`. Two
mechanisms close that off structurally, in the code that exists today:

1. **The search space is a closed catalogue.** Mahoraga never mutates code
   freely — every candidate is one entry from `mahoraga/catalogue.go`, plus
   the pass-schedule and build-flag turns, and no entry replaces a
   computation with its literal result. Whole-program constant folding isn't
   rejected by a checker after the fact; there is no entry that could express
   it.
2. **The expected output never reaches anything that generates code.**
   `Oracle` (`mahoraga/mahoraga.go`) is handed the expected bytes once, at
   construction, and offers exactly one method: `Correct([]byte) bool`.
   Nothing downstream — not the catalogue, not the search state — holds a
   copy of the answer or a way to read it.

A third layer was designed but isn't built yet: checking general/guarded
candidates against inputs mahoraga manufactures itself from the program's own
parse prefix (`Split Text by "\n"` then `Convert To Integers` describes its
own input shape well enough to generate variants), with `--no-optimize` as a
free reference implementation needing no expected output. Until that lands,
the safety net is the two mechanisms above, plus every precondition being
checked against the *real* input before the adaptation resting on it is
accepted — which for the pinned tier, with no held-out input to run at all,
is the whole of what stands between a recipe and a wrong answer.

## The eight turns, briefly

Turn 1 measures; turns 2–8 each adapt to what has been measured so far. A
turn that finds nothing turns anyway and says so, which is what lets the
report distinguish "found nothing" from "did not look."

| Turn | Asks | Tier |
|---|---|---|
| 1 — baseline & reconnaissance | ten timed runs for a mean and a noise floor, a CPU profile, a probe run reporting the program's own bindings and list growth, the Go the compiler emitted | — |
| 2 — idle for this input | did any `Filter`/`Filter Entries`/`Unique`/`Merge Ranges` change nothing, over the *whole* run? | pinned |
| 3 — how it's compiled | PGO from turn 1's profile, a larger inlining budget, this machine's instruction set | general |
| 4 — pass ablation | switch each optimizer pass off, one at a time — does it hurt *this* program? | general |
| 5 — pass ordering | reorder what ablation flagged, tune the round cap | general |
| 6 — templated codegen edits | the catalogue's parameter edits: capacities, the collector | mostly guarded |
| 7 — guarded specialisation | the same edits committing harder, with a fallback compiled in | guarded |
| 8 — pinned specialisation | the fallback removed, the assumption promoted into the recipe's contract | pinned |

All eight are built. The full precondition/effect table for turns 6–8 is in
[cli.md](cli.md#domain-expansion-mahoraga); it changes more often than the
shape above does.

## The recipe

`<stem>.mahoraga.json` is the durable half of the result — reviewable in a
diff, replayable, and designed to be committed beside the program. A binary
that is 2× faster for unexplained reasons is a liability. It records, per
adaptation: which turn found it, its tier, what it measured, the effect size,
and — for every adaptation that was *tried and turned down* — the reason,
because a recipe that lists only wins hides how much was tried and found
inside the noise. `domain build --recipe <file>` replays a **general**-tier
recipe directly; a recipe carrying anything guarded or pinned needs
`mahoraga --replay`, because only that path re-reads the input and
re-verifies the contract before building.

## What actually adapts things, today

Six entries, tried in order (cheap and safe first, greedily kept):

| Entry | Precondition | Tier |
|---|---|---|
| exact list capacity | the emitted split-and-parse loop guessed `len/2+1`, and the input's segments were counted | guarded |
| one scheduler thread | more than one core, and the baseline collected at all | general |
| collector off for one run | the baseline collected, and its heap fits under a limit | guarded |
| collector four times lazier | the baseline ran four or more collections (the answer for a program that allocates far more than it keeps, where switching off isn't an option) | guarded |
| no UTF-8 decoding / guarded ASCII fast path | every byte of the input is one rune, and the program decodes runes | pinned / guarded pair |

Two further adaptations — a **measured list capacity** for accumulators the
generator has no guess for at all, and a **pinned constant** for a `Consider`
binding a probe watched hold one value all run — are driven by a probe
build (turn 6/8, in `mahoraga/turns.go`) rather than by a static catalogue
entry, because what they need is something only running the program once can
answer.

One entry is the biggest by far: `GOMAXPROCS(1)`, worth 25–65% across the
benchmark suite below, and invisible to the general optimizer because it's a
fact about *this run* — how much this input allocates, on a program that is
always a straight line of loops on one goroutine while the collector's mark
workers coordinate over it anyway.

## What isn't built yet, and why

- **Int32 narrowing, slice-instead-of-map, flat grid arrays,
  single-probe counting** — each is a type or representation change flowing
  through every downstream operation, not a templated edit to one site. The
  map and grid sites the original design had in mind already size themselves
  exactly from `len(input)`.
- **Insertion sort at low disorder, a bounded heap for small Top K** — real,
  and each needs one measured precondition (an inversion count; K against N)
  plus a specialised lowering. Natural next entries; the machinery for both
  already exists.
- **Held-out input generation** (the third anti-cheat layer above) — designed,
  not built.

## Lessons from real searches

A tuner that always reports a win is not measuring, and three real bugs
found by trusting an early result taught the discipline
[cli.md](cli.md#domain-expansion-mahoraga) now documents as settled:

- **A goroutine leak contaminated its own measurements.** Turn 2's
  reconnaissance run, on timeout, kept running in the background rather than
  stopping — cheap once per search, except on a 712ms program it kept a core
  busy and a 389MB heap live for minutes while later turns measured against
  the noise. Two clean baselines, taken before and after, agreed at ~713ms;
  the one "win" found inside the contaminated window vanished on
  re-measurement. Fixed by making the interpreter's `Interrupter` actually
  stop the run at its next node boundary instead of merely asking it to.
- **Drift landed entirely on the side measured second.** A champion figure
  taken minutes before a candidate's, then divided, is not a ratio of
  anything real — a 15% win read as "slower by 10" this way. Fixed by racing
  candidate, champion, and baseline interleaved, one run after another,
  every time, so the only quantity ever reported is a ratio taken from inside
  a single race.
- **The upper tail of a timing sample looked like noise and was actually
  other people's work.** Interference on a shared machine only ever makes a
  run slower, so leaving the slowest quarter of a sample in inflates the
  spread on both sides until nothing is distinguishable — one search measured
  minima of 44.2ms against 52.3ms and reported means it could not tell apart.
  `Summarize` now drops the slowest quarter after the warmup run.

## The benchmark suite

`bench/mahoraga/` — distinct from `bench/`, which asks whether compiled
Domain is within 2× of hand-written Go — is four ordinary Domain programs
built to answer: *given a program the compiler has already optimized as far
as it's allowed to, how much is left for a search that's allowed to look at
the input?* One program is a control (dominated by process startup, nothing
to adapt); the other three are slower than they look, each in a different
way. See [`bench/mahoraga/README.md`](../bench/mahoraga/README.md) for the
numbers — including its own honestly-reported correction, and the finding
that the suite's single largest win (`i05_jumps`, 381ms → 7.6ms) turned out
to belong to the compiler's linear-accumulator pass, not to the search: a
benchmark suite built to evaluate mahoraga found its biggest number in the
optimizer instead, which is worth knowing about benchmark suites in general.

## Where the pieces live

```
mahoraga/         the package: facts, catalogue, probe, recon, search,
                  recipe, replay, stats — no user interface
cmd/domain/       mahoraga.go (CLI), mahoraga_wheel*.go (the terminal UI)
bench/mahoraga/   the four-program benchmark suite and its A/B script
```

Nothing mahoraga finds feeds back into the optimizer automatically. When a
search turns up something that looks true in general — as `i05_jumps` did —
a human reads the recipe and decides whether it becomes a pass. The two
commands answer different questions on purpose, and that boundary is the
point: nothing mahoraga discovers can loosen the optimizer's safety rules,
and nothing in those rules constrains what mahoraga is allowed to try.
