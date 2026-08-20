# The mahoraga benchmark suite

Four Domain programs, four inputs, and a pair of searches over each: one with
the `domain expansion: mahoraga` that shipped, one with the version under
development. `bench/` next door answers "is compiled Domain within 2× of
hand-written Go?"; this directory answers a different question —

> **Given a program the compiler has already optimized as far as it is allowed
> to, how much is left on the table for a search that is allowed to look at the
> input?**

The programs are ordinary working Domain, not benchmark fixtures: loops
threading tuples, a spiral built out of a Map, a generator pair filtered down
to five million elements each. Three of the four are slower than they look, and
in three different ways, which is the point of having four.

## The programs

| Program | What it is | Baseline | Where its time goes |
|---|---|---:|---|
| `i03_spiral` | a spiral memory walk: `Combine` over two channels, then a `While` threading a ten-field tuple and a Map | ~2.5 ms | process startup — the loop runs about sixty laps |
| `i05_jumps` | a jump-offset walk: `While` over a tuple holding the list, `set` on every step | ~510 ms → **7.6 ms** | was `dmSetAt` copying a 384-element list a million times; the loop now writes in place |
| `i06_realloc` | memory reallocation, 50,000 rounds of it, each round joined into a text key for `Find Cycle` | ~130 ms | `memmove` from the same `set` copy, plus the per-round `textjoin` |
| `i15_generators` | duelling generators: a 40M-lap `While`, then two `Unfold` streams filtered down to 5M elements each and zipped | ~1.5 s | `growslice` — a third of the run, reallocating two lists that end at five million elements |

Each has a `.input` beside it (`i15_generators` reads a file named `i15`,
because the program says so) and a `.expected` holding the answer, checked
against an independent implementation of the same puzzle rather than against
the Domain program's own output.

`i03_spiral` is in the suite as a control. It is dominated by process startup,
there is nothing in it to adapt, and a search that claims a win on it is
measuring noise — which is exactly what a benchmark suite should have one of.

## Running them

```sh
# the A/B: the same four programs under two builds of the compiler
BEFORE=/tmp/domain-before AFTER=./domain ./run_ab.sh

# one program on its own, with the flags the suite uses
./domain expansion: mahoraga bench/mahoraga/i05_jumps.domain \
    bench/mahoraga/i05_jumps.input bench/mahoraga/i05_jumps.expected \
    --runs 7 --screen-runs 3 --timeout 30s

# the comparison table, from the recipes both sides wrote
python3 bench/mahoraga/table.py before after
```

`run_ab.sh` runs one search at a time on purpose. Two searches on one box
measure each other's noise, and the numbers below were produced by a machine
doing nothing else. `--timeout 30s` is passed to both sides: pass ablation will
happily build a candidate that turns off the stream fusion `i15_generators`
depends on, and that candidate materializes a forty-million-element list.

## Results

`--runs 7 --screen-runs 3 --timeout 30s`, one search at a time, Go 1.25,
linux/amd64, four cores. **before** is the search as it shipped; **after** is
the search in this branch. Both columns are what the tool itself reported.

These were measured **before** the in-place loop write landed in the optimizer
(`docs/aoc-gaps.md` gap 14). That change belongs to the compiler rather than
to the search, and it moved `i05_jumps`'s baseline from 390 ms to 7.7 ms —
see "what the compiler took back" below, which is the more interesting half of
the result.

| Program | baseline | before | after |
|---|---:|---:|---:|
| `i03_spiral` | 2.48 ms | 1.00× | 1.00× |
| `i05_jumps` | 390.56 ms | 1.44× | **2.37×** |
| `i06_realloc` | 118.86 ms | 1.00× | 1.00× |
| `i15_generators` | 1447.65 ms | 1.40× | **1.97×** |

What each side kept:

| Program | before | after |
|---|---|---|
| `i05_jumps` | collector off (29.2%) | pass ablation (3.1%), pass order (3.0%), **one scheduler thread (65.1%)** |
| `i15_generators` | max rounds 2 (10.5%), collector off (21.3%) | pass ablation (7.9%), pass order (5.3%), **one scheduler thread (36.4%)** |

`i03_spiral` and `i06_realloc` were unbeaten by both, which is the right
answer for both: one is 2.5 ms of process startup, and the other's time is in
a copy no entry in the catalogue can remove.

### What the compiler took back

Profiling `i05_jumps` for this suite is what turned up the copy behind gap 14,
and closing that gap took the program from 390 ms to 7.7 ms — for every
Domain program of that shape, with no recipe, no pinning and no search. Re-run
against the compiler that now exists, the same search over the same program
reports:

| | baseline | speedup | what it kept |
|---|---:|---:|---|
| before gap 14 | 390.56 ms | 2.37× | scheduler thread (65.1%), two pass changes |
| after gap 14 | 7.72 ms | 1.07× | aggressive inlining (7.5%) |

The 65% has not been lost, it has been *subsumed*: `GOMAXPROCS(1)` was worth
that much because the copying loop generated garbage faster than one core
could collect it, and there is no longer any garbage to collect. What the
search can still find on the program is 7.5%, from one of the toolchain
candidates turn 3 gained in this branch.

That is the shape of the whole exercise. A suite built to evaluate the
*search* found its largest number in the *compiler*, and the search's own
score went down as a direct result of the improvement. Both facts belong in
the same table.

### What those numbers hide, and one correction

**The box drifts.** The `i15_generators` search reports 61 of 70 races more
than 10% away from where the baseline started, and its printed 1.97× does not
survive a careful re-measurement: seven alternating rounds with the first
dropped put its baseline at 1080ms and its champion at 744ms, which is 1.45×.
Treat the speedups in the table as the tool's own account on a busy machine,
not as a physical constant.

**A correction, recorded because it was published first.** An earlier version
of this file claimed the search had thrown away a 28% win by rejecting the
measured-capacity entry on `i15_generators`. That claim came from hand-racing
without discarding a cold first run — the effect `WarmupRuns` exists to remove
— and it does not hold up. Measured properly:

| `i15_generators` build | min | median |
|---|---:|---:|
| `domain build` baseline | 1080 ms | 1111 ms |
| default schedule + `GOMAXPROCS(1)` | 753 ms | 786 ms |
| the same **plus both measured capacities** | 642 ms | 695 ms |
| the champion this suite's search shipped | 646 ms | 666 ms |
| the same **plus both measured capacities** | 643 ms | 663 ms |

The capacity entry is worth about 15% where nothing else has got there — and
on the champion the search actually found, it is worth nothing, because a
shuffled pass order had already reached the same place by another route. The
screen measured that redundant candidate at 0.1%, which is exactly what a
no-op measures. **The search was right and the first write-up was wrong.**

What survives the correction is smaller and still worth having: a screen at
`--screen-runs 3` has two samples after the warmup, below both
`MinSamplesForSpread` and `MinSamplesForTrim`, so it compares two raw
wall-clock numbers with no uncertainty at all. `ScreenEffect` now takes the
more favourable of the means and the minima there, and the second look
re-races what could not be settled. Neither changed an outcome on this suite —
they are insurance against a coin toss, not a demonstrated win, and this file
should say so until a program demonstrates otherwise.

**One capacity is not half of two.** Reserving either of `i15_generators`'s
two five-million-element lists is worth about 11% on the un-shuffled champion;
reserving both is worth about 15%. The turn offers the whole set as one
candidate before offering the sites individually, because the pair is the
easier thing to see.

## What the search finds

See `docs/superpowers/specs/2026-08-12-mahoraga-design.md` for the design and
`docs/cli.md` for the catalogue. The short version of what this suite
established:

- **The biggest search win is not a code transformation at all.**
  `GOMAXPROCS(1)` is the largest single entry in the catalogue — 25–36% on
  `i15_generators`, and 65% on `i05_jumps` before the compiler stopped that
  program allocating. It is one line in `main`, invisible to the general
  optimizer because it is a fact about *this run*: how much this input
  allocates, and that the program is a straight line of loops on one goroutine
  while the collector brings three mark workers to help. Note the direction of
  that dependency — the entry is worth most on programs the compiler is
  handling worst.
- **A measured list capacity is worth about 15%, when the schedule has not
  already earned it.** Reserving what the probe watched an accumulator reach
  removes the `growslice` a third of `i15_generators` was spent in — unless a
  shuffled pass order got there first, which on one of the two searches it
  did.
- **Pinning measured constants is real and small.** Exactly one binding in
  the four programs is pinnable — `i06_realloc`'s bank count — and pinning it
  measures inside the noise floor either way. The profile says why: the
  modulus it turns into a mask is 6% of that program and absent from the other
  three. It is kept, at its measured size, because the *probe* it needs is
  what the capacity entry above is built on, and because "tried, and it was
  noise" is a more useful thing for a recipe to record than silence.
- **The largest number this suite turned up was not the search's to find.**
  `i05_jumps` spent its life copying a 384-element list once per lap, because
  `set` on a List was excluded from the linear-accumulator pass — correctly, on
  the pass as it was written. Closing that (an alias guard, loop-state
  accumulators, and receivers followed through tuple projections) took the
  compiled program from **381 ms to 7.6 ms**, and from 33 s to 0.17 s at full
  puzzle size. A benchmark suite for the *search* found its biggest win in the
  *compiler*, which is worth knowing about benchmark suites.
