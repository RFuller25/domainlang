# Fusing `Unfold` chains into a single streaming loop

## Problem

The "dueling generators" idiom (AoC 2017 day 15) — bounded `Unfold`, then
`Map Each` / `Filter`, then `Apply Using: (x) -> take(x, N)` — runs slow
because every stage materializes a full-size slice:

```
Cursed Technique: Unfold          →  40,000,000-element []ir.Value (boxed tuples)
Map Each                          →  another 40,000,000-element slice
Filter                            →  ~10,000,000-element slice
Apply Using: (x) -> take(x, 5000000)  →  truncate to 5,000,000
```

Four full-size allocations per channel (two channels: 40M and 60M raw
steps), most of it wasted — `Filter` already narrows to what `take` wants,
but nothing stops generation early once `N` survivors are found.

**Measured baseline** (this repo, `docs/superpowers/specs/` sibling commit,
input `Generator A starts with 65` / `Generator B starts with 8921` — the
canonical AoC day 15 example, correct answer `309`):

| Backend | Time | Notes |
|---|---|---|
| `domain build` (compiled) | **~3.3–3.6s** | matches what prompted this work |
| `domain run` (interpreter) | **did not finish in 60s**, >8.6GB RSS, killed | boxing overhead compounds the same materialization problem |

Target: the compiled backend under 1 second, **on this exact program,
unmodified** — no new syntax to opt into.

## Why not new syntax

Domain's stated thesis (`docs/optimizer.md`) is that a named algorithm is a
*request*, not a command — the compiler substitutes a faster implementation
and both backends get it automatically. A `Generator A:` block would be a
second thing to learn, wouldn't speed up existing programs, and would need
its own type-checking, codegen, and docs surface independent of everything
already built for `Channel`/`Unfold`. An optimizer pass fits the existing
pass catalog (`Filter` + `Take Item 0` → `Find`, `BFS` + target lookup →
`SearchTarget`) and needs no new grammar.

## Design

### New IR node: `Stream`

A new algorithm-substitution pass, `fuseUnfoldStream`, looks for a bounded
`Unfold` (one with a `While:` predicate — the only kind `Unfold` supports)
and walks forward through however many immediately-following nodes are
**elementwise**: `Map Each` and `Filter` with a total, single-argument
lambda, each applied *per element* of the list `Unfold` produced. The walk
stops at the first node that isn't one of those two, or the end of the
enclosing node list (top level or a `Channel` body).

`Apply` is not elementwise — unlike `Map Each`, its lambda runs once over
the *whole current value* (that's how the channel's own seed step,
`Apply Using: (x) -> tuple(...)`, turns a list into a single starting
tuple). So `Apply` is never absorbed into the elementwise walk. It is,
however, specifically recognized as a **terminator** when it immediately
follows the walk and its lambda body is exactly `take(x, N)` on the walk's
own parameter, `N` a non-negative int literal — the shape day 15 uses to
cap the filtered list. Two shapes come out of the whole match:

- **Terminated by `Apply Using: (x) -> take(x, N)`**: the fused node stops
  generating the instant `N` values have survived every fused `Filter`, or
  the `Unfold` bound (`While:`) runs out first — whichever comes first. This
  is the early-exit case and the one that matters for day 15: channel `a`
  stops after roughly `4·N` raw draws instead of the full 40,000,000;
  channel `b` after roughly `8·N` instead of 60,000,000. The `Apply` node is
  consumed into the fusion; nothing of it is left over.
- **No such terminator** (the walk simply ends — at a non-elementwise node,
  or the end of the block): the fused node still runs to the `Unfold`
  bound, but as one pass with one final allocation instead of one per
  stage. No early exit, but no regression either — this is what keeps the
  pass general rather than special-cased to `take`.

Either way the result replaces the whole matched run with a single
`ir.Node{Prim: "Stream", ...}` carrying the fused per-element function and
the stop condition in `Meta`, exactly like the existing fused passes carry
their lambdas. `--explain` reports it:

```
[explain] Domain rewrote Unfold + Map Each + Filter + Apply (take 5000000) → Cursed Stream (early exit). Guaranteed hit.
```

### Semantics parity

Stopping generation early is only valid because nothing downstream can ever
observe a value the naive program wouldn't also have discarded or never
needed — the same justification `SearchTarget` and `Find` already rely on.
Concretely:

- An error the naive pipeline would raise *after* the point where `N`
  survivors were already found is never raised by the fused version. This
  mirrors `Find` deliberately not validating past the first match.
  `docs/optimizer.md`'s safety rules get a line documenting this explicitly
  for `Stream`.
- An error *before* that point (a bad `Unfold` step, a `Filter`/`Map Each`
  lambda error on an early element) is raised at the same element index,
  with the same message, as the unfused pipeline — the fused loop runs the
  exact same lambdas in the exact same order, just without intermediate
  slices.
- If `Unfold`'s bound runs out before `N` survivors are found, the fused
  node returns exactly what the naive pipeline would: whatever it collected
  (`take` clamps rather than erroring, so this is not a new error case).

### Where it runs

Rewrites that change list length are documented as top-level-only today —
nested node lists (`Channel`, `Part`, loop bodies) are captured by their
parent's `Eval` closure and can't be swapped out from under it
(`docs/optimizer.md`, "Length-changing passes inside sub-pipelines"). This
pass needs `Channel` bodies specifically (that's where day 15's `Unfold`
chains live), so `Channel`'s body-node-list handling changes from a
closure-captured slice to something the optimizer can rewrite in place —
the same shape top-level rewriting already uses. `Part` and loop bodies stay
out of scope for this change, consistent with how the rest of the pass
catalog is scoped; the doc's existing note about lifting that restriction
gets updated to say `Channel` is done and `Part`/loop bodies remain future
work.

### Codegen

`codegen/listopsgen.go` gets `emitStream`, alongside `emitUnfold`: one
native Go `for` loop, no `iter.Seq`/range-over-func — a hand-written loop
avoids the yield-closure overhead that would fight the performance goal.
The loop body inlines the fused elementwise steps and the stop check
directly, the same way `emitUnfold`'s existing bound check works. Both
backends consume the same optimized IR, so this is the only codegen change
needed; the interpreter's `Stream` `Eval` and this emission implement
identical logic.

## Testing

- **Golden oracle test**: add the day 15 program (as written by the user,
  unmodified) to the interpreter-vs-binary oracle suite with the canonical
  `65`/`8921` input and expected output `309`, matching how other examples
  are pinned.
- **Optimizer unit tests**: `fuseUnfoldStream` gets the same kind of
  test coverage as the existing fusion passes in `optimizer/` — the fused
  shape, the `--explain` line, and at least one negative case (an
  `Unfold` with a non-elementwise node immediately after it, which must not
  fuse).
- **`docs/optimizer.md`**: new pass catalog entry (family: algorithm
  substitutions), documented like the other nine.
- **Benchmark**: before/after timing of the exact program from this
  conversation, unmodified, both backends, run against the canonical
  `65`/`8921` input — reported directly, not committed as a test (it's a
  one-off performance demonstration, not a correctness check). Target:
  compiled backend under 1 second; interpreter substantially improved even
  though it was never the reported bottleneck (currently >60s / >8.6GB RSS
  on this machine).

## Out of scope

- No change to `Unfold`'s own semantics or signature outside the fusion.
- No lazy-value type threaded through the rest of the runtime — `Stream` is
  a single fused node, not a new kind of `ir.Value`.
- `Part` bodies and loop bodies stay unfused, as today.
- No new user-facing syntax.
