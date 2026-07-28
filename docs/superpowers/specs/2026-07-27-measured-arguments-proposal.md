# Measured arguments — literal arguments computed from the current value

> **Status: implemented.** Steps 0–8 shipped as proposed; step 9 was
> **rejected on contact with the code** — the capability it wanted already
> existed, in a better spelling. Measured arguments are live in both backends
> across the counts (`Window`, `Chunk`, `Select Top K`, `Sliding Reduce`,
> `Take Item`, `Iterate`, `Repeat`, `Range`, the `Count Equals` vow), the Text
> slots (`Split By:`, `Join With:`), the value slots (`Pad Grid Fill:`, the
> Sparse `Default:`/`Mark:`, `Fold`/`Scan` `Seed:`) and the grid family
> (`Subgrid`, `Pad Grid`, the search starts). The three fusions whose nodes
> take the value as data carry it; the rest stand down; and the linter catches
> the two silent spellings. What shipped and what changed on contact with the
> code are recorded in [§Outcome](#outcome) at the end. The body below is the
> original proposal, kept as written so the reasoning that drove the work stays
> legible. The question it answers:
> *"I want a `Window` that is half the size of the current list — can I write
> that?"* Short answer: **no**, and the three things that come closest are in
> [§What works today](#what-works-today). The general form of the ask —
> *anywhere a literal is required, a lambda over the current value should work
> too* — is [§The proposal](#the-proposal--measured-arguments), with the
> inventory of every argument in the language and which ones can honour it.

Every literal a phrase takes today is **written in the source and read before
any data exists**. `ast.Operation` carries them as `Ints []int64` and
`Strings []string` (`ast/ast.go:166`), named arguments as `IntArg`/`StringArg`/
`FloatArg`/`IdentArg` (`ast/ast.go:184`), and each primitive's `Build` reads
them at **resolve time**: `op.Ints[0]` at `prims/seq.go:34`, the separator at
`prims/builtins.go:172`, `Seed:` at `prims/higher_order.go:395`. There is no
syntax anywhere in the pipeline layer for an argument that depends on the
value flowing through the pipe.

---

## What works today

Three partial answers, all verified against the current tree.

### 1. `Apply` reaches the whole current value

`Cursed Technique: Apply` (`prims/control.go:34`) is the scalar analogue of
`Map Each`: its lambda binds the *entire* current value, so the expression
layer's `length`/`slice`/`take`/`drop` can size things relative to it.

```domain
Cursed Energy: stdin
Cursed Technique: Extract Integers
Cursed Technique: Apply
    Using: (xs) -> slice(xs, 0, length(xs) / 2)
Reveal: stdout
# 1 2 3 4 5 6  ->  [1, 2, 3]
```

This covers "the first half of the list", "all but the last third", and any
other single relative *slice*. It does **not** cover `Window`: the expression
layer has no windowing builtin and no way to build one (no user-defined
functions, no lambda values, `list(…)` is fixed-arity).

### 2. `Unfold` is the general escape hatch — and it is ugly

`Unfold` (`prims/generate.go:102`) is driven entirely at runtime, so carrying
the original list beside an index in a tuple *does* produce half-length
windows:

```domain
Cursed Energy: stdin
Cursed Technique: Extract Integers
Cursed Technique: Apply
    Using: (xs) -> tuple(0, xs)
Cursed Technique: Unfold
    While: (s) -> item(s, 0) + length(item(s, 1)) / 2 <= length(item(s, 1))
    Using: (s) -> tuple(item(s, 0) + 1, item(s, 1))
Cursed Technique: Map Each
    Using: (s) -> slice(item(s, 1), item(s, 0), item(s, 0) + length(item(s, 1)) / 2)
Reveal: stdout
# 1 2 3 4 5 6  ->  [[1, 2, 3], [2, 3, 4], [3, 4, 5], [4, 5, 6]]
```

That is nine lines, three restatements of `length(item(s, 1)) / 2`, an
explicit index, and a tuple whose second field is the same list on every lap —
to say `Window (half)`. It also loses everything the named primitive buys: no
`Window` node in the IR, so `optimizer.fuseWindowReduce` cannot fire, and
`--explain` has nothing to say.

### 3. Shikigami parameters are literals at the call site

```domain
Shikigami "Halves" (k: Int)
    Cursed Technique: Window k
```

This works (`prims/shikigami.go:331` substitutes an Int parameter straight into
`op.Ints`), but `k` is bound to a literal *written at the call site* — `k: 3`.
`docs/language.md` says so directly: "a parameter is a value **written at the
call site**". A Shikigami parameterizes over source text, not over data, so it
moves the literal without removing it.

### The trap: the obvious spelling silently does the wrong thing

```domain
Cursed Technique: Window length(xs) / 2
```

runs **cleanly** today and produces `Window 2`. The phrase scanner routes
`length`, `xs` to `op.Words` and `2` to `op.Ints`; `Window`'s `Match` only asks
`hasWord(op, "Window")`, and its `Build` takes `op.Ints[0]`. Stray words in a
phrase are ignored, and so are unknown named arguments — a `Size:` line on a
`Window` today is silently dropped. `expansion: diagnosis` and
`expansion: lint` both report the program clean. Whatever else ships, **a lint
for words and named arguments a resolved primitive did not consume** should
ship with it; the failure mode gets more likely, not less, once a relative
size is spellable.

---

## The proposal — measured arguments

**Rule.** Any argument slot that carries **data** may be written either as the
literal it accepts today, or as a **lambda over the current value** returning
that slot's type.

```domain
Cursed Technique: Window
    Size: (xs) -> length(xs) / 2

Cursed Technique: Split
    By: (t) -> if contains(chars(t), "\t") then "\t" else ","

Maximum Technique: Fold
    Seed: (xs) -> first(xs)
    Using: (acc, x) -> acc + x
```

The phrase form (`Window 3`, `Split Text by "\n"`) is untouched and stays the
idiom for the common case; a slot that accepts a measured lambda also accepts
a plain literal in the same named position (`Size: 3`), so there is one
spelling rule, not two.

This needs **no parser change at all**. Indented `Name: value` arguments are
already parsed (`ast.Arg`), lambdas are already a legal argument value
(`ast.LambdaArg`), and binding a lambda to the whole current value is already
what `Apply` does. The feature lives in `prims`, `codegen`, and the docs.

### What "carries data" excludes

Three kinds of literal cannot be measured, and the reasons are worth stating
as rules rather than as a list, because they are what a future primitive
should be checked against:

**(a) It types the program.** The value is consumed by the resolver to decide
a *type* or a *shape*, and types are fixed before the program runs.
`Combinations k` sets the arity of the `Using:` lambda (`prims/pairs.go:43`);
`Match Pattern`'s template determines the fields of the output `Record`
(`prims/match.go:93`); a `Mode:` that picks the result shape — `All Pairs`
(`prims/pairs.go:60`), `Explore` (`prims/explore.go:74`), `Match Pattern`
`One`/`Each` (`prims/match.go:106`) — is the same thing spelled as a word.

**(b) It is consumed before there is a current value.** `Cursed Energy:`'s
source is the first stage and reads a file at resolve time; `Innate Domain:`
loads a Shikigami library before the pipeline exists. There is nothing to
measure against. (`Apply` already has the precedent error for this shape:
"Apply has no input value", `prims/control.go:39`.)

**(c) It names a program element, not a value.** `From:` names channels
(`prims/channel.go:70`); `Channel "…"`, `Part "…"`, and `Shikigami "…"` name
declarations. A name is resolved against the program, not against data.

Everything else is data, and the rest of this section is the inventory.

### Inventory — every literal argument in the tree

| Argument | Site | Measured name | Verdict |
|---|---|---|---|
| `Window SIZE [STEP]` | `prims/seq.go:34` | `Size:`, `Step:` | ✅ |
| `Chunk SIZE` | `prims/listops.go:104` | `Size:` | ✅ |
| `Sliding Reduce SIZE [STEP]` | `prims/sliding.go:40` | `Size:`, `Step:` | ✅ |
| `Select Top K` | `prims/builtins.go:382` | `Count:` | ✅ |
| `Take Item I` | `prims/channel.go:416` | `Index:` | ✅ |
| `Iterate N` | `prims/generate.go:42` | `Times:` | ✅ |
| `Repeat N` | `prims/control.go:160` | `Times:` | ✅ |
| `For x in range(N)` | `prims/control.go:315` | `Times:` | ✅ |
| `Range [LO] HI` | `prims/gridgeom.go:242` | `Low:`, `High:` | ✅ |
| `Subgrid R C H W` | `prims/gridgeom.go:285` | `Row:`, `Col:`, `Height:`, `Width:` | ✅ |
| `Pad Grid N` | `prims/gridgeom.go:338` | `Thickness:` | ✅ |
| `Pad Grid Fill:` | `prims/gridgeom.go:347` | `Fill:` | ✅ (type must equal `in.Elem`) |
| `Convert To Sparse Grid Default:` | `prims/sparse.go:75` | `Default:` | ✅ (same rule) |
| BFS/Dijkstra/Flood Fill start `R C` | `prims/search.go:28` | `Row:`, `Col:` | ✅ |
| `Fold Seed:` | `prims/higher_order.go:395` | `Seed:` | ✅ — see below |
| `Split … by "SEP"` | `prims/builtins.go:172` | `By:` | ✅ |
| `Join "SEP"` | `prims/toolbox.go:251` | `With:` | ✅ |
| `Binding Vow: Count Equals N` | `prims/vow.go:93` | `Count:` | ✅ |
| `Combinations K` | `prims/pairs.go:43` | — | ❌ (a) sets lambda arity |
| `Match Pattern "TEMPLATE"` | `prims/match.go:93` | — | ❌ (a) sets the Record type |
| `Mode:` (all primitives) | `prims/pairs.go:60`, `prims/explore.go:74`, `prims/search.go:66`, `prims/gridgeom.go:28`, `prims/gridgeom.go:101`, `prims/sliding.go:53`, `prims/match.go:106` | — | ❌ (a)/(c) selects the node |
| `Cursed Energy: SOURCE` | `prims/builtins.go:27` | — | ❌ (b) |
| `Innate Domain: TARGET` | `prims/imports.go` | — | ❌ (b) |
| `From:` channel names | `prims/channel.go:70` | — | ❌ (c) |
| Shikigami call arguments | `prims/shikigami.go:229` | — | staged last, see below |

Two entries deserve their own paragraph.

**`Range` is the sleeper.** It *replaces* the current value, so its input is
discarded today — which is exactly why `Range 0 length(xs)` is unwritable, and
why `High: (xs) -> length(xs)` closes a gap the list primitives do not have.
The measured lambda still sees the incoming value even though the primitive
itself ignores it; the same holds for `Combine` and `Zip`.

**A measured `Fold Seed:` widens `Fold`.** The seed is Int-or-Text-literal
today (`prims/higher_order.go:395`) and its Go type *pins the accumulator
type*. A measured seed gets its type from `typecheck.LambdaType` instead — so
`Seed: (xs) -> tuple(0, first(xs))` gives a fold with a **tuple accumulator**,
which is the workaround `docs/expressions.md` §"What the expression layer does
not have" currently sends people away from. That is a real capability arriving
as a side effect of a uniform rule; it also means measured `Seed:` carries more
codegen risk than the counts (arbitrary accumulator Go types), so it is staged
separately below rather than bundled with them.

### Semantics

1. **Binding.** One parameter, bound to the pipeline's current value at that
   statement, with exactly the type `In` the primitive was resolved at — the
   same binding `Apply`'s lambda gets (`prims/control.go:41`).
2. **Ambient parameters.** Arity is `1 + ambientDepth()`, and enclosing `For`
   laps bind after the current value, matching `requireLambda`
   (`prims/higher_order.go:39`), so a measured argument inside a loop reads no
   differently from a `Using:` lambda there.
3. **Result type.** `typecheck.LambdaType(lam, in, ambientTypes()...)` must
   equal the slot's type: `Int` for a count, `Text` for a separator, `in.Elem`
   for `Fill:`/`Default:`, anything for `Seed:` (which *is* the accumulator
   type). Anything else is a positioned resolve-time error naming the primitive
   and the argument.
4. **Evaluation.** Once per execution of the node, before its body runs. Inside
   a loop or a `Repeat` that means once per lap — which is the entire point: a
   `Window` over a list that shrinks each lap re-measures each lap.
5. **Purity.** The expression layer is pure and total-or-erroring, so
   evaluating a measured argument twice is unobservable. The implementation may
   cache it per execution, and must not evaluate it more than once where the
   error path is observable.
6. **Validation moves to runtime.** `size >= 1` is a resolve error today
   (`prims/seq.go:42`). Measured, it becomes a runtime error with the same
   wording plus the computed value: `Window size and step must be >= 1, got
   size 0 step 1 (Size: measured 0 from a list of 1 element)`. That is a real
   cost of the feature and belongs in the docs, not hidden.
7. **No silent clamping.** `length(xs) / 2` on a one-element list measures `0`
   and errors. Clamping to 1 would be a wrong answer that looks right — the
   reasoning that makes `factorial` error past `20!`. The guard is writable:
   `Size: (xs) -> max(1, length(xs) / 2)`.
8. **Both forms at one slot is an error.** `Window 3` *plus* `Size:` is a
   resolve error, not a silent win for either. (Today the named argument would
   simply be ignored — see the trap above.)
9. **No measured argument on the first statement.** There is no current value;
   the error is `prims/control.go:39`'s, reworded per argument.

### The cost: measured arguments are opaque to the optimizer

Every pass that reads a literal out of `Meta` type-asserts it and bails when it
is absent, so a measured argument **silently disables** the rewrite. That is
correct but quiet, and it is the strongest argument for keeping the phrase form
as the idiom. The affected passes, and whether the rewrite is legal with a
runtime value:

| Pass | Reads | With a measured argument |
|---|---|---|
| `optimizer/window.go:28` `fuseWindowReduce` | `size`, `step` | **Legal** — `ir.WindowedSums(xs, size, step)` already takes size at runtime (`prims/sliding.go:101`); only Meta plumbing and a `%d`→`%s` in codegen |
| `optimizer/optimizer.go:132` Sort + Top K → quickselect | `k` | **Legal** — `newPartialSelect` takes `k` as an ordinary value; only the `--explain` message ("Top %d") needs a measured wording |
| `optimizer/earlyexit.go:109` Filter + Take Item 0 → Find | `index` | **Not legal** — the rewrite is only valid *because* the index is 0; a measured index must skip it |
| `optimizer/scans.go:30` | `index` | Same |
| `optimizer/fuse.go:360` | `seed` = 0 | **Not legal** — the identity depends on the seed being 0 |
| `optimizer/searchtarget.go:39` | `row`, `col` | **Legal** — the fused search takes the start as data |
| `optimizer/pairsum.go:22`, `product.go:26`, `scans.go:83,242` | `k` | Not applicable — `Combinations k` is excluded by rule (a) |

So the honest summary is: measured `Window`, `Select Top K`, and search starts
keep their optimizations after a small follow-up; measured `Take Item` and
`Fold Seed:` give up an early-exit rewrite, permanently and correctly. Both
facts belong in `docs/optimizer.md`.

---

## Implementation seams

1. **One shared helper, `prims/measure.go`.** A `Measured` type holding either
   a literal `ir.Value` or an `*ast.Lambda`, plus
   `measured(op, args, name string, slot int, want *ir.Type, in *ir.Type, pos) (Measured, error)`
   that reads the phrase slot, falls back to the named argument, rejects
   both-at-once, and type-checks the lambda against `want`. Every call site in
   the inventory becomes two lines. `want == nil` means "infer" — the
   `Fold Seed:` case.
2. **Node metadata stays literal-only on the static path.** Keep `Meta["size"]`
   an `int64` when the size *is* literal, and add `Meta["sizeExpr"]`
   (`*ast.Lambda`) only for the measured case. Every existing optimizer pass
   and codegen lowering then keeps working untouched on every program that
   exists today, and the passes in the table above opt in one at a time.
3. **Interpreter.** The node's `Eval` closure evaluates the lambda with
   `eval.EvalLambdaTyped(lam, append([]*ir.Type{in}, ambientTypes()...), append([]ir.Value{v}, ambientArgs()...)...)`
   — copied verbatim from `prims/control.go:56` — then runs the existing body
   with the resulting value.
4. **Compiler.** Each lowering that reads a literal out of `Meta` grows a
   sibling path: `codegen/seqgen.go:14` (`emitWindow`), `codegen/seqgen.go:37`
   and `:604` (`emitWindowedReduce`, `emitWindowedReduceSum`),
   `codegen/listopsgen.go:61` (`emitChunk`), and the separator/seed/fill
   lowerings. All of them become one helper —
   `g.operand(n, in, "size") (string, error)` returning either `"3"` or a fresh
   Go variable assigned from
   `g.compileExpr(lam.Body, exprEnv{param: {expr: in, typ: n.In}})`, the pattern
   `emitApply` already uses (`codegen/codegen.go:1238`). Emitted loops then
   interpolate `%s` where they interpolate `%d` today. `emitChunk`'s
   `make([][]T, 0, (len(in)+size-1)/size)` capacity hint needs the runtime
   guard beside it rather than a resolve-time one.
5. **Diagnostics, LSP, catalog.** `prims.Catalog` signatures and hover text grow
   the measured form; `docs/primitives.md` gains a row per primitive and one
   worked "half the list" example; `docs/language.md`'s named-argument section
   gains the rule and the (a)/(b)/(c) exclusions; `docs/optimizer.md` gains the
   opacity table. `domain fmt` needs nothing — it already formats indented
   named arguments.
6. **The lint from the trap section**, as its own commit, ideally *first*:
   words and named arguments that the resolved primitive never consumed are a
   warning. It is the difference between `Window length(xs) / 2` being a typo
   the tooling catches and a wrong answer that runs.
7. **Tests.** Per primitive: a `prims` unit test for the measured path and for
   the both-forms-at-once error; a `typecheck` test for a wrong-typed lambda; an
   oracle program under `codegen/testdata` pinning interpreter-vs-binary
   byte-identical output; and one program per affected optimizer pass run in
   both modes, since the passes are where a measured argument changes what runs
   rather than what is written.

### Shikigami parameters, last

Once slots accept measured lambdas, a Shikigami argument can be one:

```domain
Shikigami: Halves
    k: (xs) -> length(xs) / 2
```

`substituteOp` (`prims/shikigami.go:331`) currently moves an Int parameter into
`op.Ints`; a measured argument would instead have to land in the *named* slot
the parameter feeds. That is coherent, but it needs one restriction: an Int
parameter also substitutes **into lambda bodies** (`docs/language.md`
§Parameters), and a measured value has no meaning inside a per-element lambda
where the "current value" is an element, not the pipeline value. So a measured
Shikigami argument is legal only when every use of the parameter is a
measurable phrase slot, and using it inside a lambda body is a resolve error
naming both positions. Worth doing, worth doing last.

---

## Alternatives considered

- **Expressions in the phrase itself** — `Window (length(it) / 2)`, with a magic
  `it` for the current value. Rejected: it needs a real expression parser inside
  the operation-phrase scanner, a new binding name in a language that has none,
  and it collides with the phrase layer's comma-separated `Modifiers` and with
  `Operation.Raw` (which `domain fmt` promises never to rewrite). The existing
  split is already stated as a rule in `ast/ast.go:215` — "the themed phrase
  layer stays integer-only". This proposal keeps it; the computation lives in
  the layer that already has arithmetic.
- **Fraction words** — `Window Half`, `Window Third`. Cheap, reads well for
  exactly the motivating example and nothing else. Two ways to say one thing,
  and the second way runs out immediately (`length(xs) / 2 - 1`?). Rejected,
  though `Half` could become sugar over `Size:` later at no cost.
- **A count from a Channel** — `Window` reading a named channel holding an Int.
  The plumbing exists, but it adds a second way to reference data at a distance
  for a value that is almost always a function of the *current* value, and it
  forces a named channel for what is one expression.
- **A `Measure` primitive plus a "previous scalar" slot.** Needs a second data
  path through the pipeline — the one thing the single-current-value model
  exists to avoid.
- **Recursive/user-defined expression functions**, which would let `Window` be
  written in the expression layer instead. A much larger language change,
  listed as absent in `docs/expressions.md`, and it would not remove the need
  for measured arguments on `Repeat`, `Range`, `Split`, or `Take Item`.

---

## Staging

| Step | Contents | Why it is separable |
|---|---|---|
| 0 | The unconsumed-word/argument lint | Independent bug fix; makes the rest safe to teach |
| 1 | `prims/measure.go` + `Window`, `Chunk`, `Select Top K` (interpreter only; the compiler refuses with a positioned `unsupported`) | The compiler is *allowed* to refuse what it cannot lower; it may never disagree with the interpreter |
| 2 | The codegen lowerings for step 1 + oracle tests | Closes the backend gap |
| 3 | `fuseWindowReduce` and the quickselect rewrite carrying a measured count | Restores the two optimizations worth restoring |
| 4 | The remaining counts: `Take Item`, `Iterate`, `Repeat`, `range(N)`, `Range`, `Sliding Reduce`, `Binding Vow` | Same helper, one call site each |
| 5 | The Text slots: `Split By:`, `Join With:` | Different slot type, same machinery |
| 6 | The value slots: `Pad Grid Fill:`, `Convert To Sparse Grid Default:` | Type must match `in.Elem` |
| 7 | `Fold Seed:` | Arbitrary accumulator types — the widest codegen blast radius, and its own capability win |
| 8 | The grid family (`Subgrid`, `Pad Grid` thickness, search starts) + `searchtarget` | Wants its own worked examples |
| 9 | Measured Shikigami arguments | Needs the use-site restriction above |

Step 1 alone answers the original question.

---

## Outcome

Steps 0, 1 and 2 shipped together, plus one correction the code forced.

### What shipped

- **`prims/measure.go`** — `Measured` (a literal or a lambda, in one type) and
  `measuredInt`, which reads a phrase slot, falls back to the named argument,
  rejects both-at-once, and type-checks the lambda. `Window` (`Size:`,
  `Step:`), `Chunk` (`Size:`) and `Select Top K` (`Count:`) call it.
- **Both backends.** `gen.measuredOperand` (`codegen/codegen.go`) emits the
  literal as a constant when there is one and a computed `int64` otherwise,
  with the same bound check at the same moment. Six oracle programs — measured
  window, measured window feeding a reduce, measured chunk, measured top-K,
  measured top-K + Sum, and a measured size inside a `For` loop — pin the two
  backends byte-for-byte in both optimizer modes. Step 1's "interpreter only,
  compiler refuses" staging turned out to be unnecessary: the lowerings were
  three lines each.
- **The linter** (step 0), both halves: an argument the line's primitive never
  read, and an expression written into a phrase. Verified silent over every
  program in `examples/`, `challenges/` and `testdata/`.

### What changed on contact with the code

- **The unused-argument lint needed no table.** The proposal assumed a
  per-primitive list of accepted argument names. Instead `ArgSet.get` marks
  each `*ast.Arg` as a primitive looks it up, so the lint asks the resolver
  what actually happened and cannot drift from what `Build` reads. Cost: one
  `Used` field on `ast.Arg`, and one line in `substituteArg` so a Shikigami
  definition's arguments — consumed through a substituted *copy* — are marked
  at substitution rather than reported as ignored by every call.

- **The optimizer hazard was worse than "opacity", and this is the
  correction.** §The cost said a measured argument would *silently disable*
  the rewrites keyed on the literal. That is true of `fuseWindowReduce`, which
  happens to bail on `size < 1`. It was **false** of the quickselect fusion:
  `k, _ := next.Meta["k"].(int64)` yields 0 for a measured count, and
  `Sort` + `Select Top (measured)` fused to `Top 0` and returned the empty
  list — silently, and only when optimized. Not a missed optimization: a wrong
  answer. `optimizer/measured.go` now carries `hasMeasuredArg`, and every pass
  that folds a literal consults it first, so a measured argument added later
  to a primitive a pass has never heard of is refused by default rather than
  mis-folded. The regression is pinned in `optimizer/measured_test.go`.

- **`Select Top K`'s runtime bound.** The interpreter clamped a negative count
  to 0 and the resolver rejected a negative literal. A measured count keeps
  the resolver's rule at runtime — `Count:` measuring `-1` is an error, not a
  clamp — matching the no-silent-clamping rule the counts already follow.

### Step 3 — the two fusions carry the value

`fuseWindowReduce` and the quickselect rewrite no longer stand down: both fused
nodes take the argument as data, so they carry it. What that took:

- **The value travels as a closure, not as a call back into `prims`.** The
  optimizer cannot import `prims` — `prims`' own *internal* tests import the
  optimizer, so the dependency only points one way. `ir.MeasureFn` is the
  shared type: `Measured.Meta` writes the lambda (which the compiler compiles)
  *and* a resolver closure (which the interpreter runs) under `…Expr` and
  `…Fn`. A pass moves both with `arg.writeMeta` and never touches `Meta`
  directly.
- **The bound check moved onto the value.** `Measured` grew a `Min` field and a
  `Resolve` method, so the closure a fused node calls performs the same check
  with the same wording as the primitive would. That is what keeps optimizer
  safety rule 2 — a rewrite may not turn an error into a success — true for an
  argument whose validity is a runtime question. `TestMeasuredBoundErrorSurvivesFusion`
  pins it: a measured 0 fails identically in both modes.
- **`readArg` replaced the direct `Meta` reads** in both passes. A count in
  *neither* form now stops the rewrite instead of arriving as a plausible zero
  — the same hazard as the original bug, closed structurally rather than by a
  guard a future pass could forget.
- **`--explain` says `Top (measured)`** rather than inventing a number.
- The compiler needed `measuredOperand` in three more lowerings
  (`emitWindowedReduce`, `emitWindowedReduceSum`, `emitPartialSelect`), and its
  bound-check message now includes the list length the interpreter's carries.

`hasMeasuredArg` stays the default for every other pass, so the distinction the
proposal drew — legal to carry vs valid only because the literal is what it is
— is now the one in the code.

### Step 4 — the rest of the counts

`Sliding Reduce` (`Size:`/`Step:`), `Take Item` (`Index:`), `Iterate` and
`Repeat` (`Times:`), `Range` (`Low:`/`High:`) and the `Count Equals` vow
(`Count:`). One call site each, as predicted; three details were not:

- **Not every argument has a lower bound.** A `Take Item` index is
  range-checked against the *list*, and a `Range` bound against the *other*
  bound, so `Measured` grew `NoBound` — the check is skipped and the
  consumer's own error stands, with the wording it always had (`index 99 out
  of range (length 3)`).
- **A measured argument can have no phrase slot at all.** `Range 10` writes
  only the high bound, so `Low:` has no literal position there; `measuredInt`
  takes a negative slot to mean "named form only".
- **`Take Item` keeps its literal index as an `int`.** Two optimizer passes
  match on that shape, so the literal path writes `Meta["index"]` by hand
  rather than through `Measured.Meta` (which writes `int64`), and only the
  measured path uses the shared keys. `hasMeasuredArg` stands both passes down
  for a measured index, which is correct: `Filter` + `Take Item 0` → `Find` is
  valid *because* the index is 0.

`Range` is the one that pays off most, exactly as §The proposal predicted: it
discards its input, so `High: (xs) -> length(xs)` says something no literal
spelling can.

### Step 5 — the Text slots, and the hazard they carried

`Split`/`Split Each` (`By:`) and `Join` (`With:`).

**The hazard, found while scoping the step and cleared with it.** The optimizer
never reads `Meta["sep"]`, but the *compiler* does, in thirteen places
(`codegen/matchgen.go`), and three of those rules fire on `sep == ""` — the
character-split fast paths. The empty string is a *meaningful* separator there,
not a missing one, so a measured separator read through a type assertion would
have fired those three **wrongly** rather than merely failing to fire: the
quickselect bug's exact shape, on the other side of the backend. `tryFuse` now
checks `hasMeasured(nodes[0], "sep")` before any rule runs, which is why the
general rule is worth stating plainly: *a zero value is only safe to read as
"absent" when it is not also a legal answer.* Pinned by an oracle program whose
measured separator returns `""` — it must produce the right answer without the
digit-grid fast path, and the emitted Go shows it taking `strings.Split` where
the literal spelling still compiles straight to `dmGrid[int64]`.

**`MeasuredText` is a sibling of `Measured`, not a generalization.** The two
differ in everything except the rule they must share: an Int argument has a
bound to check and a value the optimizer may fold; a Text one has neither, and
needs no runtime closure in `Meta` because no pass reads a separator. What they
do share — which spelling wins, and what happens when a program writes both —
goes through the same both-forms check and the same lambda type check, now
parameterized by the slot's type.

### Step 6 — the value slots

`Pad Grid Fill:` and the Sparse `Default:`/`Mark:`. These are the first
arguments whose *type* is part of what they say: the literal's Go type pins or
must match the cell type. `MeasuredValue` therefore reports the type it
produces rather than being told one — from the literal for a literal, from
`typecheck.LambdaType` for a lambda — and the primitive checks it exactly as
it always checked the literal's. `Pad Grid: Fill: is Int but the grid holds
Text` is still a resolve-time error when the lambda body is `3`.

These also have no phrase spelling at all — they have always been named
arguments — so there is no both-forms case, which is why `measuredValue` is
the one reader of the three with no `bothFormsErr` in it.

### Step 7 — `Fold Seed:`, and the capability it turned out to buy

The proposal predicted this one widens `Fold`, and it does. A literal seed can
only be Int or Text — those are the two a named argument can spell — so the
accumulator was limited to them. A measured seed takes its type from
`typecheck.LambdaType`, so `Seed: (xs) -> tuple(0, 0)` gives a fold with a
tuple accumulator, and `Seed: (xs) -> list(0)` one with a list. Both compile:
the generated Go declares the accumulator at the seed's own type, which is the
widest reach a measured argument has into the backend. `docs/expressions.md`'s
"a fold whose accumulator is a small struct cannot be written" is now only
true of *named-field* structs.

`Scan` took the same treatment, since it is the same argument.

Two things stayed literal-only on purpose, both already guarded:
`optimizer/fuse.go`'s `Fold(seed 0) → Sum` and `codegen/matchgen.go`'s
split-ints-fold fusion. Both are valid *because* the seed is 0, so a measured
seed stands them down — verified with `--explain`: the literal spelling still
reports the rewrite, the measured one reports none, and both print 6.

### Step 8 — the grid family

`Subgrid` (`Row:`/`Col:`/`Height:`/`Width:`), `Pad Grid`'s `Thickness:`, and
the `BFS`/`Dijkstra`/`Flood Fill` starts (`Row:`/`Col:`). A grid can now name
its own crop and its own search start: `Row: (g) -> rows(g) - 1`.

`fuseSearchTarget` **carries** the start rather than standing down — it takes
it as data, like the other two carriers — so the early-exit search still fires
for a measured start, verified against the naive pipeline.

One compiler fusion had to stand down, for a reason none of the others had:
`gridSearchFusable` builds a search's mask straight from the input lines so
that the grid is *never materialized*, and a measured start is a lambda over
that grid. There is nothing to measure from, so the fusion refuses a measured
start and the ordinary path — which does build the grid — handles it. A
stand-down for "the value this needs does not exist here", rather than for
"the literal is what makes this valid".

### Step 9 — rejected: the capability was already there

The proposal wanted `k: (xs) -> length(xs) / 2` at a call site for a parameter
declared `k: Int`, with a restriction: the parameter may only feed measurable
phrase slots, because an Int parameter *also* substitutes into lambda bodies,
where a measured value has no meaning.

That restriction was the tell. It exists only because the spelling declares a
type the argument does not have. Domain already has the honest spelling — a
**lambda parameter** — and it already works, with no change to anything:

```domain
Shikigami "Sized Windows" (size: (List<Int>) -> Int)
    Cursed Technique: Window
        Size: size

Shikigami: Sized Windows
    size: (xs) -> length(xs) / 2
```

`substituteArg` replaces an `IdentArg` naming a lambda parameter with the
lambda bound at the call, for *any* argument name — so a lambda parameter has
been able to feed a measured slot since the moment measured slots existed. Both
backends run it; a Shikigami body may equally just measure directly, with no
parameter at all.

So the restriction is enforced by the type system rather than by a special case:
a lambda-typed parameter cannot be substituted into a phrase as a literal, and
nothing has to check where it is used. What shipped instead is the error for
the spelling the proposal asked for, which now names the one that works:

```
Shikigami "Halves": parameter "k" is declared Int but was given a lambda — to
pass a measured argument through a Shikigami, declare the parameter as a lambda
type (e.g. k: (List<Int>) -> Int) and hand it to the measured slot in the body
```

**The general lesson, since it is the second one this proposal produced:** the
step that needed a use-site restriction did not need a feature. Both times —
here, and the fabricated-zero hazard in §Outcome step 3 — the trouble was a
value pretending to be a kind of thing it was not. `For x in range(N)` is also still literal-only: the count sits
  in a loop *header* rather than in a primitive's arguments, so it wants the
  same treatment applied to `parseRangeArg`.
  `measuredInt` is Int-only today; the Text and value slots want the same
  reader generalized over the slot's type, which is where `want *ir.Type`
  from §Implementation seams earns its place. `fuseSearchTarget` is the one
  pass that could carry a measured argument and does not, because no search
  start is measurable yet — it joins step 8.
- **Step 9** — measured Shikigami arguments, with the use-site restriction.
