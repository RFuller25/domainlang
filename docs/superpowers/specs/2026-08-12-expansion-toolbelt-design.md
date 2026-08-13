# Seven more `domain expansion:` commands

`battle`, `bench`, `golf`, `coverage`, `shrink`, `fuzz`, `stats`.

## Status

Four of the seven are built: **`bench`**, **`coverage`**, **`stats`** and
**`battle`**, on the shared measurement layer described in Part 1 (`runner`,
`Rewrite.Pass`, `prims.Used`, `prims.SamePipeline`, and the `langs` table with
Weave added).

**`golf`, `shrink` and `fuzz` were dropped** — not deferred, and not because
anything blocked them. The three pieces of Part 1 they needed most
(`prims.SamePipeline` for golf's proof standard, `runner`'s input
substitution for shrink and fuzz) are built and tested regardless, since the
other four commands wanted them too. Their sections below are kept as written
rather than deleted: they record a design that was considered and costed, and
`SamePipeline` in particular exists and is unused until something wants it.

Two things from Part 1 landed differently than specified, both for stated
reasons: `SamePipeline` lives in `prims` rather than `ir` (ast imports ir, so
ir cannot name a lambda), and `ir.Probe` (§1.5) was not built, since `fuzz`
was its only consumer.

## What these seven have in common

Every one of them answers a question by **running the program more than
once** and comparing the results:

| Command | What it varies | What it compares |
|---|---|---|
| `battle` | the language | two programs' output and wall time |
| `bench` | backend × optimizer | four timings and four allocation figures |
| `golf` | the source spelling | two optimized IRs |
| `coverage` | the program, over a folder | what ran against the primitive catalog |
| `shrink` | the input | whether the failure still reproduces |
| `fuzz` | the input | coverage reached, and the four backends against each other |
| `stats` | the program, over a folder | a leaderboard of runtimes and passes |

None of that machinery exists as a library today. `bench/harness_test.go`
builds and times two binaries, but it is a test; `cmd/domain/main.go` has
`Execute` and `Build`, but they run one configuration and return nothing
measurable; `interp.Stats` times stages inside one interpreted run and says
in its own doc comment that those numbers "are not the language's
performance."

So the bulk of this design is one shared measurement layer, and seven fairly
thin commands on top of it. Building the commands first, each with its own
private timing loop, is how you end up with seven subtly different answers to
"how long does this program take" — which is the one number all seven are
supposed to agree on.

---

# Part 1 — Shared infrastructure

## 1.1 `runner`: one way to run a Domain program and measure it

A new package, `runner/`, is the single place a program gets executed for
measurement. It lifts what `bench/harness_test.go` already does correctly
into non-test code, and every one of the seven commands goes through it.

```go
package runner

// Config is one of the four ways to run a Domain program.
type Config struct {
    Compiled bool // false: interpret; true: codegen + build, then exec
    Optimize bool // false: the naive pipeline (optimizer.Optimize(p, false))
    Release  bool // shed Binding Vows
}

// Four is the cell grid `bench` reports and `fuzz` differentials against.
var Four = []Config{
    {Compiled: false, Optimize: false},
    {Compiled: false, Optimize: true},
    {Compiled: true, Optimize: false},
    {Compiled: true, Optimize: true},
}

// Input is what the program reads. Exactly one field is set.
type Input struct {
    Path  string // an existing file, passed through as-is
    Bytes []byte // materialized to a temp file before the run (see below)
}

// Result is one measured execution.
type Result struct {
    Stdout   []byte
    Stderr   []byte
    ExitCode int
    Err      error         // harness failure (build error, spawn error) — not the program's own failure
    Wall     time.Duration // best of N, the run only
    Build    time.Duration // compile time, zero when interpreted
    Alloc    AllocStats    // zero-valued unless the caller asked for it
}
```

### Why every run is a subprocess

`bench/README.md` establishes the two methodology rules that make its numbers
comparable, and they are non-negotiable here because `bench` and `battle`
publish head-to-head figures:

- **Both sides are subprocesses**, so both pay process startup.
- **Input is a redirected regular file** (`./prog < input.txt`), never a pipe.
  The README measures this at ~3× on the read alone for a whole-input reader,
  because over a pipe nothing can size the read up front.

That applies to the interpreted configurations too. `runner` does *not* call
`interp.Run` in-process for a timed run — it re-executes the `domain` binary
itself (`os.Executable()`) as `domain run <file> [--no-optimize] < input`.
Timing an in-process interpretation against an out-of-process binary would
hand the interpreter a free win on startup and make the `bench` table lie in
the direction that flatters the language. In-process interpretation stays
available as `runner.Interpret` for the callers that want the trace hook
(`coverage`, `fuzz`) rather than a timing.

`Input.Bytes` is materialized to a temp file for the same reason, and named
after what the program actually reads: `prims.FileOptions(path)` resolves a
`Cursed Energy:` target relative to the program's directory, and `Read Source`
falls back to stdin only when the named file is absent. `runner` reads the
resolved source target off the pipeline's first node and, when it names a
file, writes the candidate there in a temp directory holding a copy of the
program; otherwise it redirects the temp file onto stdin. This is what lets
`shrink` and `fuzz` drive programs that read `input.txt` by name.

### Best-of-N, alternating

`Wall` is the **minimum** over N runs, not the mean — scheduler noise and
page-cache misses only ever add time, so the fastest observed run is closest
to the cost of the work. When a caller measures two or more configurations
(all of them do), `runner.Race` interleaves them round-robin rather than
running N of one then N of the other, so thermal and cache drift lands on
every side equally. Default N is 5, matching `TestSpeedRatio`.

### Timeouts and interruption

Every measured run takes a deadline (default 60s, `--timeout` everywhere).
`fuzz` and `shrink` depend on this: non-termination is a *finding*, not a hang
of the tool. A timed-out run reports `ExitCode: -1` with a `Timeout` flag, and
`runner` kills the whole process group so a foreign runtime that spawned a
child cannot outlive it.

## 1.2 Allocation measurement

`bench` promises "timing + allocation" per cell, and the two halves have
different honest answers.

**Peak RSS** comes from `ProcessState.SysUsage().(*syscall.Rusage).Maxrss` and
needs no cooperation from the child — it works for every configuration, and
for the foreign programs `battle` runs. It is the headline number.

**Bytes allocated, allocation count, and peak heap** need the Go runtime's own
accounting, which means the measured process must report it. Both sides can:

- The `domain` binary checks `DOMAIN_ALLOC_REPORT` at exit in `run` mode and,
  when set, writes one line to the named fd: `TotalAlloc`, `Mallocs`,
  `HeapSys`, `NumGC` from `runtime.ReadMemStats`.
- `codegen` emits the same check in generated `main`, gated on the same env
  var — roughly eight lines behind `if os.Getenv(...) != ""`, contributing
  nothing to a normal run beyond one env lookup at exit.

Two rules keep this from corrupting the thing it measures. **Allocation runs
are separate runs from timing runs** — never the same execution, since
`ReadMemStats` stops the world. And the number reported is from a **single**
run, not a best-of-N, because allocation is deterministic for a deterministic
program and a minimum over five would just hide a nondeterminism worth
seeing.

When the child does not report (an older binary, a foreign program), the
allocation columns show `—` and the peak RSS column still has a number. The
report never guesses.

## 1.3 Optimizer pass identity

`optimizer.Rewrite` is one field:

```go
type Rewrite struct{ Message string }
```

`stats` reports "which optimizer passes fired" and `golf` reports "which pass
makes this line redundant", and neither can be built by pattern-matching
English out of `Message`. `Rewrite` gains a `Pass string` naming the function
that produced it:

```go
type Rewrite struct {
    Pass    string // "fuseSortThenTopK" — stable, matches the function name
    Message string
}
```

All 29 entries in `optimizer.passes` set it. A test walks `passes` and asserts
that every rewrite any pass emits carries a non-empty `Pass` that matches a
known name, so a new pass cannot ship without one. `--explain` output is
unchanged: it prints `Message`, as it does now.

## 1.4 What a program uses: `prims.Uses`

`coverage`, `stats` and `golf` all need "which primitives and which builtins
does this program touch", so it is written once:

```go
package prims

// Usage is the primitive and builtin vocabulary one program exercises.
type Usage struct {
    Prims    map[string]int // Primitive.ID → occurrences
    Builtins map[string]int // expression-layer function name → call sites
    Keywords map[string]int // themed keyword → statements under it
}

// Used walks an unoptimized pipeline and the AST expressions hanging off it.
func Used(p *ir.Pipeline) *Usage
```

Two details that decide whether the numbers mean anything:

**It must run on the unoptimized pipeline.** `fuseMapMap` turns two `Map Each`
nodes into one, `elideRedundantSort` deletes a `Sort` outright. Counting
vocabulary after optimization would report that a program which visibly uses
`Sort` does not, which is exactly backwards for a command whose job is telling
you what you have not tried. `Used` takes the pipeline from
`prims.ResolveWith` before `optimizer.Optimize` runs.

**Builtins are counted statically.** The trace hook (`ir.Tracer`) reports node
evaluations; expression-layer calls like `gcd` and `slice` are evaluated
inside `eval.Eval` and are not nodes. `Used` finds them by walking the `ast`
expressions reachable from each node's `Meta` lambdas and pipeline bodies.
So a builtin inside a branch that never executes still counts as used. The
`coverage` report says so in its own header rather than pretending otherwise.

The catalog it is measured against is already exact: `prims.Registry` has 85
primitives, all of them documented in `prims.Catalog` (a test pins the two to
each other), and `typecheck.Builtins` has 161 expression functions, with
`keywordPages` naming the 8 themed keywords.

## 1.5 Coverage probes

`fuzz` needs finer coverage than "which nodes evaluated", because the
interesting question is which *branch* of a program an input reaches. This is
a new interpreter-only interface beside `ir.Tracer`:

```go
package ir

// Probe records a branch outcome. Fires only when Context.Probe is non-nil,
// which is only under `fuzz` and `coverage --dynamic`.
type Probe interface {
    Branch(site SiteID, outcome int)
}
```

`SiteID` is the position of the construct plus an index, so it is stable
across runs of the same program and meaningful in a report. The instrumented
sites, chosen because each one is a place a Domain program can take a
different path:

| Site | Outcomes |
|---|---|
| conditional expression (`if`/`else` arms) | arm taken |
| `Filter` / `Count Matching` / `Take While` predicate | true, false |
| `Find` / `Find Index` / `First` | hit, exhausted |
| `Match Pattern` case | which case matched; no case matched |
| `Part` block | entered, skipped |
| loop body (`Repeat`, `While`, `For`, `Iterate Until Fixed Point`) | zero laps, one lap, many laps |
| `getor` / `Convert With Default` | key present, default taken |
| `Binding Vow` | held, violated |
| foreign block | ran, failed |

The compiled backend gets no probes. Fuzzing runs interpreted, because the
campaign needs thousands of executions and a compile per candidate would
dominate; the compiled backend enters only through `fuzz --differential`,
which re-runs the *interesting* inputs through all four configurations and
compares output, not coverage.

## 1.6 Foreign runtimes, lifted and extended

`battle` runs a whole file in another language. The runner table for that
already exists twice — `prims.foreignCommand` (interpreter) and
`codegen.foreignSpecs` (emitted) — and they are kept in sync by hand today.
This change lifts one table into `langs/`:

```go
package langs

type Spec struct {
    Name       string   // "Python"
    File       string   // "program.py"
    Env        string   // "DOMAIN_PYTHON" — an override naming a command with args
    Candidates []string // PATH lookups, in order
    Tail       []string // args after the program ("run", "." for Go)
    Extra      map[string]string // sidecar files (go.mod)
    Exts       []string // ".py" — how `battle` infers --lang from a filename
    Home       string   // where to get it, for the "not installed" message
}
```

`prims` and `codegen` both consume it, which removes the duplication and is a
precondition for the fifth entry:

| Language | Command | Override | Extensions | Source |
|---|---|---|---|---|
| Python | `python3`, `python` | `DOMAIN_PYTHON` | `.py` | — |
| Go | `go run .` | `DOMAIN_GO` | `.go` | — |
| cRust | `crust` | `DOMAIN_CRUST` | `.crust` | github.com/sintfoap/crust |
| rask | `rask` | `DOMAIN_RASK` | `.rask` | — |
| **weave** | `weave` | `DOMAIN_WEAVE` | `.weave`, `.wv` | github.com/malleum/weavelang |

Adding `weave` to the shared table makes it work as a `Domain Expansion:
Weave` foreign block as well as a `battle` contestant, for one table entry
plus the wire-format tests every other language already has
(`codegen/foreigngen_test.go`, `prims/foreign_test.go`). If weavelang's
invocation turns out not to fit `<binary> <program>` with stdin/stdout, the
`Tail`/`Extra` fields are where that lands, the way Go's `run .` does.

A missing runtime is not an error in the tool. `battle` reports
`weave not found — install it from github.com/malleum/weavelang, or set
DOMAIN_WEAVE to the command that runs it` and exits 2, the same shape
`foreignBinary` uses today.

## 1.7 CLI plumbing

`expansionCommands` gains seven entries. All are single words, so ordering
against `{"maximum", "compile"}` is unaffected.

The shared tail of `Expansion` — "exactly one file argument, no flags" —
cannot serve any of these seven; each gets a parser beside
`parseVisualizeArgs`, and `Expansion` dispatches to it before reaching the
shared block, exactly as `visualize` and `development` already do.

Three flags are common to all seven and mean the same thing everywhere:

- `--plain` — no TUI, no color, line-oriented output. This is what CI and the
  tests use, and it is mandatory for the two commands that otherwise take over
  the screen.
- `--json` — the same data as a machine-readable object. `visualize --json`
  sets the precedent.
- `--timeout <dur>` — per-run deadline.

Banners: seven new entries in `expansionTechniques`, matching the existing
JJK naming. Proposed, in the spirit of the seven that are there:

| Command | Kanji | Name | Tag |
|---|---|---|---|
| `battle` | 領域展開・対決 | DOMAIN CLASH | two domains, one survives. |
| `bench` | 呪力測定 | CURSED ENERGY MEASUREMENT | four ways to fight, one is fastest. |
| `golf` | 無下限 | LIMITLESS | the space between you and the answer, made smaller. |
| `coverage` | 術式反転 | TECHNIQUE INVENTORY | you have not thrown every technique you know. |
| `shrink` | 縮地 | CONTRACTION | the smallest input that still draws blood. |
| `fuzz` | 帳 | CURTAIN | seal the domain, try every way out. |
| `stats` | 等級査定 | GRADE ASSESSMENT | the record, laid out. |

---

# Part 2 — The commands

## 2.1 `domain expansion: battle <a.domain> [--lang L] <b>`

```
domain expansion: battle day15.domain --lang python day15.py
domain expansion: battle day15.domain day15.go          # --lang inferred from .go
domain expansion: battle day15.domain day15.weave --input input.txt --runs 9
```

Two programs, one input, one required answer. The screen splits and they race.

### Correctness gates the race

The race is meaningless if the two programs do not compute the same thing, so
**output equality is checked before any timing is reported**. One run each,
outputs compared byte for byte:

- **Identical** — the race proceeds, and the verdict panel says so.
- **Different** — no winner is declared, at all. The TUI shows the first
  differing line side by side and the exit code is 1. A faster program that
  prints the wrong answer has not come out on top, and reporting a time for it
  would be the single most misleading thing this command could do.
- **Either side failed** (nonzero exit, timeout) — reported as a loss for that
  side with its stderr shown.

### Fairness

Every rule from `bench/README.md` applies, and the TUI states each one on the
verdict screen so the numbers can be argued with:

- Both sides are subprocesses; both read the same input as a redirected file.
- Best of N alternating (default 5), lowest wins.
- **Build time is reported, never hidden, and never counted in the race
  time.** Domain compiles, Go compiles, Python does not, and crust/weave
  interpret. The verdict shows two figures per side: `run` (the race) and
  `first answer` (build + run, what you actually wait for the first time).
  Both are true, they favor different sides, and showing only one would be
  taking a position.
- The Domain side is compiled and optimized — `domain build` output, which is
  what the language ships. `--interpret` races the interpreter instead, and
  the banner says which one ran.
- The challenger is run as its own runtime would run it, with no flags the
  user did not ask for. `--challenger-args` passes extra arguments (e.g.
  `-O`), and they are shown in the verdict.

### The TUI

bubbletea, two lanes, following `visualize_tui.go`'s structure (a model driven
by messages, so it is testable by injection — see §3):

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  領域展開・対決 ・ DOMAIN CLASH        input: input.txt (12.4 MB) · best of 5
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  day15.domain (compiled)         │  day15.py (python3 3.13)
  ███████████████░░░░░░  run 4/5  │  ██████░░░░░░░░░░░░░░  run 2/5
  best   0.94s                    │  best   14.21s
  build  0.41s                    │  build      —
  peak   214 MB                   │  peak   1.9 GB
                                  │
  output ✓ identical (309)        │
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  DOMAIN WINS — 15.1× faster on the run, 10.3× to first answer
```

`--plain` prints the same figures as a table with no cursor control.

### Testing

- A `.domain`/`.py` pair in `cmd/domain/testdata/battle/` that agree, and one
  that disagrees; `--plain` output asserted for the winner line and for the
  refusal-to-declare line respectively.
- Python is guarded with `exec.LookPath`, matching how the foreign-block tests
  already skip; the disagreement case can use two Domain programs (`--lang
  domain`) so it runs everywhere.
- The TUI model is driven with injected `tea.Msg`s, as `visualize_tui` and
  `repl_tty` are tested.

## 2.2 `domain expansion: bench <file>`

```
domain expansion: bench day15.domain --input input.txt
```

The four-cell table, which is exactly the hand-rolled `time` loop this
replaces:

```
呪力測定 ・ CURSED ENERGY MEASUREMENT
day15.domain · input.txt (12.4 MB) · best of 5

                 │     naive      │   optimized    │  optimizer buys
─────────────────┼────────────────┼────────────────┼─────────────────
 interpret       │    did not     │     4.82 s     │        —
                 │  finish (60s)  │   1.4 GB peak  │
                 │                │  2.1 GB alloc  │
─────────────────┼────────────────┼────────────────┼─────────────────
 compile         │    38.4 s      │     0.94 s     │        41×
                 │   8.6 GB peak  │   214 MB peak  │
                 │  31.2 GB alloc │   1.1 GB alloc │
─────────────────┼────────────────┼────────────────┼─────────────────
 compiling buys  │      —         │      5.1×      │

 build: 0.41 s (optimized) · 0.38 s (naive)
 all four cells agreed on the output ✓
 8 optimizer passes fired: fuseUnfoldStream, fuseMapMap, …
```

### Rules

- **The output check is not optional.** All four cells must produce identical
  bytes. The naive/optimized pair is the compiler's own correctness oracle
  (`docs/optimizer.md`), and the interpret/compile pair is the backend oracle.
  A disagreement is a **compiler bug**, and `bench` says so in those words,
  prints the diff, and exits 1 — it does not quietly report timings for a
  program whose two backends disagree.
- A cell that times out prints `did not finish (60s)` rather than a number.
  This is a real and common outcome — the lazy-unfold spec measured exactly
  it — and blanking the cell or extrapolating would both be lies.
- Ratios are only printed between cells that both produced a time.
- Allocation figures come from the separate non-timed run of §1.2.

### Flags

`--input`/`--input-text` (as `visualize` spells them), `--runs N`,
`--timeout`, `--release`, `--cells <list>` to measure a subset (interpreting
the naive pipeline is often the one nobody wants to wait for), `--json`,
`--markdown` for pasting a table into a write-up.

### Testing

Golden `--plain` output on a small, fast program with a fixed `--runs 1`,
with the timing columns normalized (the test asserts shape, cell labels, the
agreement line and the pass list — never durations). A second test feeds a
program whose four cells agree and asserts exit 0; the disagreement path is
tested by injecting a stub `runner` result, since a real
interpreter/compiler disagreement is a bug we do not have on hand.

## 2.3 `domain expansion: golf <file>`

The inverse of `optimize`. Where `optimize` rewrites your source into the
faster spelling, `golf` finds the **shorter** spelling that the optimizer
proves is the same program — the explicit steps a rewrite already subsumes.

```
無下限 ・ LIMITLESS
day5.domain — 24 lines, 612 bytes

  proved equivalent (identical optimized IR):

  - line 9   Cursed Technique: Sort Ascending          −1 stage, −38 bytes
             elideReorderBeforeReduce deletes this before it runs:
             the Sum below does not care about order.

  - line 14  Maximum Technique: Take Item 0            −1 stage, −34 bytes
             fuseSortTakeItem already folds this into the Sort By above.

  - line 17  Using: (x) -> x                           −1 stage, −29 bytes
             elideIdentityMap deletes the whole Map Each.

  checked on 3 inputs, not proved:

  - line 21  Convert To Integers                       −1 stage, −31 bytes
             the input happens to parse either way; a different input
             might not. Not applied by --write.

  21 lines, 480 bytes (−3 stages, −132 bytes, −21%)
  run `domain expansion: golf day5.domain --write` to apply the proved set
```

### The proof standard

This is the part that has to be exact, because "shortest equivalent phrasing
the optimizer can prove correct" is a strong claim.

**Tier 1 — proved.** A candidate edit is *proved* equivalent when the original
and the candidate resolve to pipelines that are **structurally identical after
optimization**. If the optimizer erases the difference, the two spellings are
the same program by construction: both backends consume the optimized IR, so
byte-identical IR is byte-identical behavior, including error behavior and
including inputs nobody has. This needs one new function, `ir.SamePipeline(a,
b *ir.Pipeline) bool` — a structural comparison over nodes, types, `Meta` and
the ASTs in it, next to the existing `ir.DeepEqual` for values. Position
information is excluded from the comparison; everything else is compared.

**Tier 2 — checked.** The optimized IRs differ, but the two programs produced
identical output on every input available (siblings of the file: `<stem>.input`,
`<stem>*.input`, plus any `--input` given). Reported in a separate section,
counted separately, and **never applied by `--write`**. Calling this "proved"
would be the command's one available way to be dishonest, so the report gives
the two tiers different words and different sections, and the summary line
counts them apart.

Anything that fails to resolve, or changes output on any available input, is
discarded silently.

### Candidate generation

Working at the source level, as `diag.OptimizeSource` does — same loop, same
rollback discipline, opposite direction:

1. **Whole-stage deletion.** Every pipeline statement, one at a time.
2. **Argument deletion.** An explicit argument that equals its default; an
   identity `Using:` lambda; a `Mode:` that names the default mode.
3. **Keyword deletion.** The themed keyword prefix is optional wherever
   `prims.Infer` recovers it — this one is pure bytes and never changes the
   IR, so it is always Tier 1. Off by default behind `--keywords`, since the
   keywords are the language's voice and deleting all of them to win at golf
   is a choice the user should make deliberately.
4. **Stage-pair collapse.** Two adjacent stages the optimizer is known to fuse,
   replaced by the single stage the fusion produces, when that stage has a
   source spelling.

Each accepted edit is re-verified against the *current* source and the loop
repeats, so a deletion that exposes another (a `Sort` that becomes redundant
once the `Reverse` above it goes) is found. Bounded by
`maxSourceRewrites`-style iteration cap.

### Interaction with `fix` and `optimize`

`golf` requires a clean program, and says `run domain expansion: fix <file>
first` when the analyzer reports errors — the same precondition and the same
message shape `optimize` already uses. `--write` backs up to `.bak`, as `fix`
and `optimize` do.

### Testing

Unit tests in `diag/` over small programs, one per candidate class, asserting
the exact set of proved removals and that `ir.SamePipeline` holds for each.
One negative test that a Tier-2 candidate is never in the `--write` set. One
test that `golf` output, applied, still passes the program's `.expected`.

## 2.4 `domain expansion: coverage <folder>`

```
術式反転 ・ TECHNIQUE INVENTORY
examples/ — 20 programs, 20 with inputs

  primitives   58 / 85   (68%)
  builtins     94 / 161  (58%)
  keywords      8 / 8    (100%)

  Cursed Technique — 9 not exercised
    Pad Grid           Grid<T> → Grid<T>              ref-transforms-grid.md#pad-grid
    Sliding Reduce     List<T> → List<T>              ref-transforms.md#sliding-reduce
    …

  Maximum Technique — 4 not exercised
    Min By             List<T> → T                    ref-reductions.md#min-by
    …

  builtins not exercised — 67
    number theory   crt  divisors  modinv  isprime
    text            padright  trimsuffix  isupper  ord
    …

  run `domain expansion: documentation` and search a name to see what it does
```

### What "exercised" means

Two different answers, and the report keeps them apart because one is much
stronger than the other:

- **Statically used** (default): the program's unoptimized IR contains the
  primitive, or its expressions call the builtin. Cheap, needs no input, and
  is the honest measure for builtins, which are not nodes and so cannot be
  traced (§1.4).
- **Actually executed** (`--dynamic`): the program ran against its `.input`
  sibling and the node evaluated. Strictly stronger for primitives — a
  primitive inside a `Part` that never ran is *written* but not *tried* — and
  unavailable for builtins.

The header names which mode produced the numbers. Under `--dynamic`,
primitives get both columns and a program with no input file is reported as
skipped rather than silently counted static.

Coverage is always measured on the **unoptimized** pipeline (§1.4).

### Folder scan

`*.domain` recursively, pairing each with `<stem>.input` and `<stem>.expected`
when present — the layout `challenges/`, `examples/` and `testdata/` already
use. `--exclude` takes a glob. Files that fail to resolve are listed at the
end as skipped, with the diagnostic, rather than dropped.

### Flags

`--dynamic`, `--json`, `--exclude`, `--only prims|builtins|keywords`,
`--used` to invert the report (what you *do* use), `--min` to exit 1 below a
percentage, which makes it usable as a CI gate on this repo's own docs and
examples.

### Testing

Run it over `challenges/` in a test and assert the totals are the catalog
sizes (85/161/8) rather than pinning percentages, which would churn on every
new primitive. Assert a known-unused primitive appears and a known-used one
does not. `--json` shape is golden-tested.

## 2.5 `domain expansion: shrink <file> <input>`

```
domain expansion: shrink day8.domain input.txt
```

Given a program and an input that makes it fail, find the smallest input that
still fails the same way.

```
縮地 ・ CONTRACTION
day8.domain · input.txt (14,208 lines, 412 KB)

  reproducing …  Convert To Integers: "acc +3" is not an integer (line 4)
  ddmin lines    14,208 → 61 → 7 → 1        (412 KB → 6 B)
  ddmin chars    6 B → 6 B
  normalize      6 B → 4 B

  minimized (4 bytes, written to input.txt.min):

    a +1

  still fails with: Convert To Integers: "a +1" is not an integer (line 1)
  1,204 candidate runs · 11.3 s

  reproduce:  domain run day8.domain < input.txt.min
```

### The failure oracle

The whole command turns on "still fails **the same way**", and getting this
wrong makes it either useless (nothing matches) or wrong (it minimizes to a
different bug). The default oracle, in order:

1. The run must fail — nonzero exit, or a timeout when the original timed out.
2. If the original produced an `ir.RuntimeError`, the candidate must produce
   one with the **same `Prim` and the same message shape**. Message shape means
   the message with integers, quoted literals and positions replaced by
   placeholders: `"acc +3" is not an integer (line 4)` and `"a +1" is not an
   integer (line 1)` are the same failure, and that is the entire reason the
   example above shrinks at all. Requiring literal equality would stop at the
   first line the reducer removed.
3. If the original failed some other way (a panic, a nonzero exit with no
   structured error), the candidate must match on that class: panics match
   panics with the same top frame, exits match exits with the same code.

`--expect <text>` overrides all of it: any run whose combined output contains
the text reproduces. `--expect-exit N` matches on code alone. Both make
`shrink` usable for "wrong answer" bugs, not just crashes, which the default
oracle deliberately does not guess at.

If the program **succeeds** on the given input, `shrink` says so and exits 2
rather than searching — a reducer with no failure to preserve will happily
minimize to the empty input and report triumph.

### The algorithm

Classic ddmin, three phases, each stopping at a fixpoint:

1. **Lines.** Standard delta debugging over the line list — remove half, then
   quarters, backing off granularity on failure. Line-oriented first because
   AoC inputs are, and because a 400 KB input reduces to a handful of lines in
   a few hundred runs this way.
2. **Characters.** ddmin within each surviving line.
3. **Normalize.** Not a size reduction but a legibility one: shrink integers
   toward `0`, letters toward `a`, and blank runs of whitespace, keeping each
   change only if the failure survives. Four bytes of `a +1` reads better than
   four bytes of `q̸ +7` and is the same bug. Off with `--no-normalize`.

Caching matters: candidates repeat heavily across phases, so results are
memoized on the candidate's hash. `--max-runs` (default 10,000) and
`--timeout-total` bound the search; interrupting with ^C prints the best
input found so far rather than discarding the work.

### Which configuration reproduces it

By default the interpreter with the optimizer on — fastest startup, thousands
of runs. Three flags change that, and one behavior is automatic:

- `--compiled` shrinks against a compiled binary (built once, reused).
- `--no-optimize` shrinks against the naive pipeline.
- After minimizing, `shrink` **re-runs the minimized input through all four
  configurations** and reports which ones fail. A failure that reproduces
  under `--optimize` but not naive, or compiled but not interpreted, is a
  compiler bug rather than a program bug, and the report says so in those
  words with the exact reproduction command for both sides. This repo's whole
  test strategy is oracle-based; a reducer that hands you a differential is
  handing you the highest-value bug report the project can receive.

### Output

The minimized input to `<input>.min` (`-o` to redirect, `-` for stdout), and
the report to stdout. The original input file is never modified.

### Testing

Deterministic and fast: a program that fails on any line containing a
non-integer, an input of 500 generated lines with one bad one, and an
assertion that the result is exactly that one line. A second test with
`--expect` on a wrong-answer bug. A third asserting exit 2 when the input does
not fail. Run-count assertions are bounded (`< 2000`), not pinned.

## 2.6 `domain expansion: fuzz <file> <example_input>`

The largest of the seven. It explores what inputs a program can be given, what
it does with them, what it never does, and whether its four backends agree.

```
帳 ・ CURTAIN
day4.domain · seed from input.txt · 60 s · seed 0x5eed

  input shape, inferred from the program's own parse stages:
    Split Text by "\n"            → lines
    Match Pattern                 → "{int}-{int} {word}: {text}"
    Convert To Integers           → holes 1,2 are Int
  and from the example: 1,000 lines · ints 1..99 · words [a-z]{1} · text [a-z]{4,16}

  ── findings ──────────────────────────────────────────────────────────────
  ✗ interpreter/compiled disagree     1 input      COMPILER BUG
       "3-3 z: zzz"  →  interpreted 1, compiled 0
       shrink it:  domain expansion: shrink day4.domain corpus/0007.txt
  ✗ crash (interpreter panic)         0 inputs
  ✗ timeout (>10s)                    2 inputs     both 200k+ lines
  ! runtime error                     4 inputs     3 distinct messages
       Match Pattern: no case matched line 1        ← empty line in input
       Convert To Integers: "" is not an integer    ← trailing newline
       Take Item 0: list is empty                   ← all lines filtered out
  ✓ ran clean                       914 inputs     31 distinct outputs

  ── coverage ──────────────────────────────────────────────────────────────
  nodes      14 / 14   (100%)
  branches   19 / 26   (73%)

  never reached, and why:
    line 12  Filter predicate never false      every generated line matched;
                                               needs a line where lo > hi
    line 15  Part "part two"                   entered only when the count
                                               exceeds 100 — reached 0 times
    line 19  getor default                     every key was present
    line 22  Binding Vow "Count Equals 1000"   never violated (that is good)

  provably unreachable (the optimizer deletes these):
    line 24  Filter Using: (x) -> true         elideConstPredicates

  corpus: 921 inputs kept in ./fuzz-day4/ (replay with --corpus)
```

### Input generation is derived from the program, not guessed

This is the part that is specific to Domain and is why the command is worth
building. In most languages a fuzzer has to discover the input format by
mutation. In Domain **the leading stages of the pipeline are a declarative
description of the input**, sitting right there in the IR:

- `Split Text by "\n"` → the input is lines.
- `Split Each by ","` → each line is comma-separated fields.
- `Convert To Integers` → those fields are integers.
- `Match Pattern "{int}-{int} {word}: {text}"` → a literal grammar for a line,
  with typed holes.
- `Convert To Grid` / `Digit Grid` → a rectangular grid of characters or
  digits, and rectangularity is checked, so ragged input is a distinct case.
- `Split Text by "\n\n"` → blocks.

`fuzz` walks the pipeline prefix until the first stage that is not a parse
step, and builds a generator from what it found. The example input supplies
what the program cannot: realistic magnitudes (line count, integer ranges,
alphabet, field widths), read off by measuring it.

The result is that the overwhelming majority of generated inputs are
*structurally valid* and reach the actual computation, instead of dying in the
parser — which is what makes coverage of the program's interior achievable
inside a minute.

### Mutation tiers

Each generated input picks a tier, weighted:

1. **Value mutation (70%)** — structure held fixed, values replaced from a
   boundary pool: `0`, `1`, `-1`, `maxint`, `minint`, values equal to each
   other, values from the example. This is what finds off-by-ones and
   overflow.
2. **Structure mutation (25%)** — empty input; one line; one *very* long line;
   duplicate lines; a ragged grid; a missing trailing newline; an extra blank
   line; a block boundary in an unusual place. This is what finds the four
   runtime errors in the sample report above, and every one of them is a real
   AoC-day-one failure mode.
3. **Byte mutation (5%)** — flip, insert, delete bytes. Kept deliberately rare:
   against a parse-first language it mostly produces "not an integer", which is
   a legitimate finding the first time and noise the four-hundredth. The
   findings table dedupes by message shape, so it stays one row.

### Coverage-guided

Standard corpus loop: run an input, collect the probe set (§1.5), keep the
input if it hit any site/outcome pair no earlier input did, and prefer keeping
corpus entries as mutation seeds. `--seed` makes a campaign reproducible;
`--corpus DIR` saves and reloads it, so a second run continues rather than
restarting.

### Findings, ranked by severity

The report groups by *behavior*, worst first, because "show imperative
results" is the point — what actually happened, on what input:

1. **Differential disagreement** — the same input gives different output under
   two of the four configurations. This is a compiler bug and outranks
   everything. Requires `--differential` (default on, since it costs one
   compile for the whole campaign and re-runs only the *interesting* inputs).
2. **Crash** — an interpreter panic. `diag.Analyze` catches panics by design;
   the evaluator does not, and one here is a bug in the language, not the
   program.
3. **Timeout** — non-termination at the campaign's per-run limit.
4. **Runtime error** — deduped by message shape, each with the smallest input
   that produced it and the input feature that caused it, where the generator
   knows (it knows: it made the mutation).
5. **Clean runs** — count, and the number of distinct outputs, which is a
   quick smell test for a program that returns the same answer no matter what.

Every finding row carries the exact `shrink` command for its input, so the two
commands compose into a bug-report pipeline.

### "What code can never be reached"

The report separates two claims that are easy to conflate, because only one of
them is a proof:

- **Not reached by this campaign** — a branch with no coverage after N inputs,
  reported with the *condition that would reach it*, read off the site: a
  `Filter` predicate never false, a `Part` never entered, a `getor` default
  never taken. This is a nudge, and the header says the campaign's size so the
  reader can weigh it.
- **Provably unreachable** — the optimizer already proves some of this:
  `elideConstPredicates` and `elideConstEarlyExits` delete branches whose
  condition is constant. Those are reported as a hard finding, with the pass
  name. Nothing else is called unreachable.

### Flags

`--time <dur>` (default 60s), `--runs N`, `--seed`, `--corpus DIR`,
`--no-differential`, `--per-run-timeout`, `--json`, `--plain`. The default is
a plain streaming report; a TUI is explicitly *not* part of this command —
a fuzzing campaign's output is a log, and the useful live view (a counter) is
one line.

### Testing

- Generator tests: for a program with a given parse prefix, assert the
  generated inputs parse — i.e. the program does not fail in its parse stages
  for at least 90% of tier-1 inputs. This is the property that makes the
  command work and it is the one that will regress.
- Probe tests in `interp/`: a program with a `Part`, a `Filter` and a `getor`,
  run with a `Probe` installed, asserting exactly which sites and outcomes
  fire.
- Determinism: two campaigns with the same `--seed` produce the same corpus.
- A planted differential: a stub configuration in `runner` that mutates output,
  asserting the finding is reported at severity 1.
- Campaign tests run with `--runs 200`, not `--time`, so they are
  deterministic in CI.

## 2.7 `domain expansion: stats <folder>`

```
domain expansion: stats aoc2024/ --markdown
```

```
等級査定 ・ GRADE ASSESSMENT
aoc2024/ — 25 programs, 25 with inputs, all compiled + optimized, best of 3

  #   program              LOC  stages  runtime   passes fired            ✓
 ──────────────────────────────────────────────────────────────────────────
  01  day01.domain           6       5    3.1 ms  ×2  fuseMapReduceBy…    ✓
  02  day02.domain           9       7   12.4 ms  ×3                      ✓
  …
  15  day15.domain          14      11   0.94 s   ×8  fuseUnfoldStream…   ✓
  …
 ──────────────────────────────────────────────────────────────────────────
      25 programs           241     178    4.6 s  87 rewrites            25/25

  slowest      day15 (0.94 s) · day23 (0.71 s) · day09 (0.44 s)
  longest      day23 (21 lines) · day15 (14) · day12 (13)
  most rewritten  day15 (8) · day09 (6) · day02 (3)

  passes fired across the year (top 5):
    fuseMapMap            19    elideIdentityMap       12
    fuseFilterCount        9    fuseSortTakeItem        7
    fuseUnfoldStream       2

  vocabulary   47 / 85 primitives · 61 / 161 builtins
               (domain expansion: coverage aoc2024/ for what is missing)
```

### What each column is

- **LOC** — non-blank, non-comment source lines. Blank-inclusive totals are
  reported under `--json` for anyone who wants them, but the headline number
  should be the one a reader would count.
- **stages** — top-level pipeline nodes in the *unoptimized* IR, so the column
  measures what was written, not what survived.
- **runtime** — `runner`, compiled + optimized, best of 3 (this is a survey
  over a folder; `bench` is the command for one program studied properly, and
  `stats` says so in its footer).
- **passes fired** — count, and the names, from `Rewrite.Pass` (§1.3).
- **✓** — output matched the `.expected` sibling. A folder without `.expected`
  files gets the column omitted rather than a column of dashes.

### Flags

`--sort time|loc|passes|name` (default: name, which is day order for AoC
naming), `--top N`, `--markdown` (a table pasteable into a README — this is
the portfolio use case and deserves first-class output, not a suggestion to
reformat the plain table), `--json`, `--interpret`, `--runs N`, `--timeout`.
A program that fails to build, fails to run, or times out gets a row with the
reason rather than being dropped, and the footer counts it.

### Testing

Run over `challenges/` with `--runs 1 --plain`, asserting the row count, the
columns present, and the totals line — never durations. `--markdown` output is
golden-tested for table syntax. A folder with a deliberately broken program
asserts the failure row and a nonzero exit.

---

# Part 3 — Build order

The dependency structure is real, and building the commands before the shared
layer is how the seven end up disagreeing with each other.

**Phase 1 — measurement** (no user-visible change)
`runner` (§1.1), allocation reporting including the `codegen` env-var hook
(§1.2), `Rewrite.Pass` (§1.3), `prims.Used` (§1.4), `ir.SamePipeline` (§2.3).
Each lands with its own unit tests; nothing ships to the CLI.

**Phase 2 — the three that need only phase 1**
`bench`, `stats`, `coverage`. These are the cheapest and they validate the
measurement layer against numbers that already exist: `bench` on the day 15
program should reproduce the figures in
`2026-08-12-lazy-unfold-fusion-design.md`, and if it does not, `runner` is
wrong and it is worth knowing before four more commands depend on it.

**Phase 3 — `golf`**
Needs `ir.SamePipeline` and the `diag` rewrite loop; independent of everything
else.

**Phase 4 — `shrink`**
Needs `runner`'s input materialization and the failure oracle. Independent of
the probes.

**Phase 5 — probes and `fuzz`**
`ir.Probe` (§1.5), then the generator, then the campaign. `fuzz` cites
`shrink` in its output, so `shrink` should exist first.

**Phase 6 — `langs` and `battle`**
The foreign-table lift (§1.6), `weave` as a fifth language with its wire-format
tests, then the TUI. Last because it is the only one that depends on external
toolchains being installed, and because the TUI is the largest piece of UI
work in the set.

# Part 4 — Testing strategy

The repo's existing conventions cover almost all of this and should be
followed rather than reinvented:

- **TUIs are tested by driving the model with injected messages**, as
  `visualize_tui.go` and `repl_tty.go` are. `battle`'s model takes race
  progress as `tea.Msg`s, so the test never spawns Python.
- **Every command has a `--plain` path, and that is what the CLI tests
  assert.** Durations, byte counts and run counts are normalized or bounded,
  never pinned.
- **External toolchains are guarded with `exec.LookPath` and skipped**, the
  way `prims/foreign_test.go` already does. CI without Python still runs the
  whole suite.
- **Golden files for `--json` and `--markdown`**, since those are contracts
  other things will parse.
- **The oracle tests get new members.** `bench`'s four-cell agreement check and
  `fuzz`'s differential mode are the same property `codegen/differential_test.go`
  already asserts, driven from new directions; anything they find is a bug in
  the compiler, and the fixture goes into that suite.

# Part 5 — Documentation surface

- `docs/cli.md` — a section per command, following the depth of the existing
  `domain expansion: visualize` section.
- `cmd/domain/main.go` `usage()` — seven lines in the expansion block. That
  block is getting long; the seven new commands are grouped under a
  `Measurement and exploration:` subheading rather than extending one list to
  sixteen entries.
- `cmd/domain/expansion.go` package comment — the seven new lines.
- `docs/optimizer.md` — a note that `Rewrite.Pass` exists and what consumes it.
- `bench/README.md` — a pointer saying that `domain expansion: bench` is the
  same methodology applied to one program, and that this directory remains the
  Domain-vs-hand-written-Go suite.
- `docs/aoc-toolbox.md` — `coverage` and `stats` belong in the AoC workflow
  section; they are aimed squarely at that reader.

# Part 6 — Out of scope

- **No new language syntax.** All seven are tools over the language as it is.
- **No compiled-backend probes.** Branch coverage is interpreter-only; the
  compiled backend enters through differential output comparison, not
  instrumentation.
- **No allocation figures for foreign programs beyond peak RSS.** `battle`
  cannot ask Python for `runtime.ReadMemStats`, and inventing a comparable
  number would misrepresent both sides.
- **`golf` never applies Tier-2 (checked-only) edits**, and `--write` has no
  flag to make it.
- **No network.** `battle` will not fetch a missing runtime; it names where to
  get it and exits.
- **`fuzz` does not attempt to prove reachability.** It reports what the
  campaign covered, and separately reports the branches the *optimizer* proves
  dead. Anything stronger would be a symbolic execution engine, which is not
  this.
