# Closing the AoC gaps — an implementation plan

[aoc-gaps.md](aoc-gaps.md) is the survey: five blockers and eight sharp edges,
each reproduced. This page is the work, sequenced. Every phase ships on its own
and none of them blocks on a design decision that is not settled here.

The ordering is not by severity. Phase 0 is first because it is a day of work
that removes noise from every program written afterwards; Phase 1 is second
because it is the difference between programs that run and programs that do
not, and nothing else here matters as much.

| Phase | Closes | Size | Ships |
|---|---|---|---|
| 0 — consistency | 8, 9, 11, doc bug | **done** | ordering that agrees with itself |
| 1 — linear accumulators | 1 | **done** | grid simulations and hand-built tables at native speed |
| 2 — `Explore`: `Cost:` and `Tally` | 2, 3 | **done** | weighted search, counting DP |
| 3 — sequences | 10, 13 | **done** | `Map` in the pipeline, list builtins in expressions |
| 4 — scope | 4, 6, 7 | **done** | n-parameter bodies, naming the value, channels in loops |
| 5 — parsing | 12 | **done** | repeating holes, trial matches |
| 6 — template groups | 12's residue | **done** | optional and repeated *groups*, not just holes |
| 7 — reach | the leftovers | **done** | `Mode: Scan`, `Case:`, more hole types, `emptylist` |
| — | 5 | deferred | with the decision written down |

## The checklist every phase obeys

This repo already has a discipline, and the plan is mostly a matter of running
each change through it. Stated once here rather than repeated per phase:

1. **`typecheck`** — the static rule, with a unit test.
2. **`eval`** — the dynamic rule, with a unit test. Partial operations must use
   identical failure wording to the compiler.
3. **`codegen`** — the emitter, plus an interpreter-vs-binary oracle program.
   A feature in `eval` but not `codegen` must fail `domain build` with a
   positioned error, never differ silently
   ([compiler.md](compiler.md)).
4. **`prims`** — the registry entry, for anything that is a primitive.
5. **Docs** — the reference page, plus `go test ./docs` (which checks the prose
   against the registry and the builtin table, not against other prose) and
   `go test ./docs -update` to regenerate `primitives.json` / `gallery.json`.
6. **Editor grammars** — generated from the language and pinned by a test, so a
   new builtin or keyword fails `editors/` until regenerated.
   *Added after phase 5:* the same applies to the two hand-maintained lists the
   grammars do not cover — `prims.argNames` (what the linter suggests on a
   misspelled argument) and `lsp/completion.go` (argument labels and `Mode:`
   values). Phases 2, 4 and 5 each added an argument or a mode and updated
   neither, so `Cst:` suggested `Col:` rather than `Cost:`. `argNames` now has
   the drift test its own comment claimed, scanning the registry's call sites.
7. **`--explain`** — a new optimizer pass says what it did, like every other.
8. **Diagnostics** — where a new spelling supersedes an old one, add the lint
   and the auto-fix, so `expansion: fix` migrates existing programs.

---

## Phase 0 — make the ordering agree with itself — **done**

Three relaxations and a doc fix. No design work; the runtime already had
everything.

**Gap 8 — `<` `>` `<=` `>=` over `Text` and tuples.** `ir.Ordered` and
`ir.Compare` (`ir/order.go`) already defined a total order over Int, Float,
Text and tuples of those, and `Sort`/`Sort By` already used it. The comparison
operators simply did not reach it.

- `typecheck/typecheck.go` — the `numeric(lt) && numeric(rt)` gate became a
  three-way test mirroring the `=` case beside it: both numeric (so mixed
  Int/Float still compares through promotion), else same type, else the type
  is `ir.Ordered`. The unordered message names the rule rather than "Int or
  Float", and a `relSymbol` helper spells the operator as written — `token.Kind`'s
  `String` is `"LT"`, which is right for a parser trace and wrong beside a
  user's own source line.
- `eval/eval.go` — the Int fast path stays; everything else goes through
  `compareOrdered`, which defers to `ir.Compare`. It guards the runtime shapes
  itself rather than trusting the resolver: `ir.Compare` answers 0 for a shape
  it does not recognize so that a sort stays stable, and silently reporting
  "equal" is the wrong answer for an operator.
- `codegen` — Int, Float and Text all order with Go's own operator (byte-wise
  for strings, which is what `strings.Compare` does too), so only tuples
  needed anything: `codegen/cmp.go` interns a `dmCmpN` beside the `dmEqN` that
  `codegen/eq.go` generates for structural equality. Each operand is evaluated
  once, as a function argument — `lessExpr` repeats its operands, which is
  fine for the sort inner loop's plain locals and not for an arbitrary
  compiled expression.
- `optimizer/exprsimp.go` — two string literals now fold the way two integer
  ones do, through a shared `foldCmp`.

**Gap 9 — `Min By` / `Max By` over any ordered key.** `prims/seq.go`'s
`keyedExtremum` took an `int64` comparator; it now takes `ir.Compare`'s answer,
checks the key with `ir.Ordered`, and reuses `Sort By`'s error wording.
`codegen/seqgen.go` gained `keyBeats`, which renders "this key beats the best
so far" through the *same* `lessExpr` the `Sort By` emitter uses — one
lowering of the ordering rather than two that can drift. Both the plain
emitter and the fused `Split Fields` + `Convert To Integers` + `Max By` path
now track the best key in a local of the key's own type.

**Gap 11 — `Transpose` over `List<List<T>>`.** `prims/grid.go` picks its `Eval`
by input shape; `codegen/gridgen.go` gained `emitTransposeRows`. A ragged row
is an error naming the row and both lengths, with the wording
`Convert To Grid` uses for the same problem.

**Doc bug.** `docs/aoc-toolbox.md`'s `len(m)` row offered
`Maximum Technique: Count`, which does not typecheck; it now says `size(m)`
and names the restriction. Phase 3 makes the original claim true.

**Testing.** `typecheck/ordering_test.go`, `eval/ordering_test.go`,
`prims/ordering_test.go` and the Transpose cases in `prims/grid_test.go`, plus
`codegen/ordering_test.go` — six programs run through the interpreter and a
compiled binary in both optimizer modes.

Two of those tests are the ones worth keeping. `TestComparisonAgreesWithIrCompare`
checks the operators against `ir.Compare` directly over every pair in a small
value set, and `TestKeyedExtremaAgreeWithSortBy` checks that whatever `Min By`
picks is what `Sort By` puts first. There are now **four** implementations of
one order — `ir.Compare`, eval's `compareOrdered`, codegen's `lessExpr`, and
codegen's interned `dmCmpN` — so a property that reaches several of them over
the same values is what keeps them from drifting.

---

## Phase 1 — linear accumulators — **done**

The headline. `insert`, `del`, `put`, `setat`, `set` and `with` copy their
whole argument, so a `Fold` that builds a collection is quadratic in the
collection rather than linear in the writes: 30 s interpreted / 12 s compiled
for a 20,000-step DP, 44 s for 20,000 writes into a 300×300 grid.

**The idea.** The semantics stay exactly as documented — every update is
functional and no program can observe a difference. What changes is that when
the analysis can prove the copied-from value is dead, the copy is skipped.

**What it measures now**, on the two programs from the survey:

| Program | Before | `domain run` | compiled | `--no-optimize` |
|---|---|---|---|---|
| 20,000-step Map DP | 30.2 s / 12.4 s | **0.05 s** | **0.007 s** | 23.7 s |
| 20,000 `setat` on 300×300 | 43.6 s | **0.03 s** | **0.03 s** | — |

Both are now benchmark cases against hand-written Go rather than pathologies:
`fold_map_dp` runs at 1.13× the Go program and `fold_grid_writes` at 0.68×
(see [`bench/`](../bench/README.md)).

### The analysis

A new optimizer pass, `optimizer/linear.go`, run **last** — after every
expression rewrite, so no later pass can duplicate an annotated call.

Its input is a `Fold` or `Reduce` node whose accumulator type is `KMap`,
`KSet`, `KGrid`, `KSparse` or `KList`. It walks the lambda body in
**evaluation order** and annotates update call sites that are safe to perform
in place. A site `f(r, …)` qualifies when both hold:

1. **Rooted.** `r` is the accumulator parameter, or another qualifying update
   whose result flows straight into this one — so `insert(insert(acc, …), …)`
   chains without an intermediate copy.
2. **Last use.** No occurrence of the accumulator can be evaluated after this
   site *on any path that reaches it*.

The second condition is a last-use computation over the expression tree, and
it has to be path-sensitive rather than positional, because the common shape
is a conditional record:

```
(acc, x) -> if wanted(x) then insert(acc, key(x), x) else acc
```

A positional "is this the textually last occurrence" rule would refuse that —
the `else acc` comes after. `CondExpr` arms are mutually exclusive, so an
occurrence in one arm is not *after* a site in the other. The walk therefore
needs a real case per node kind, and the evaluation order it models must be
the one `eval` actually uses:

| Node | Order | Note |
|---|---|---|
| `CallExpr` | args left to right, then the call | |
| `BinaryExpr` | left, then right | `and` / `or` short-circuit: the right side is conditional |
| `CondExpr` | cond, then **one** arm | arms independent — the whole point |
| `LetExpr` | value, then body | |
| `AlsoExpr` | left, then right | both always run |

There are no nested lambdas inside an expression, so there is no capture case
to worry about.

Annotation goes on `ast.CallExpr` (a new `InPlace bool`), which both backends
already read. It also means `--no-optimize` turns the whole thing off, which
is what makes the existing oracle discipline apply for free.

Two things landed differently from this sketch. **`del` is not in the set** —
removing a key shifts the key order, which a list taken from the accumulator
earlier *would* see, unlike an append; every other update reuses an existing
tested mutator (`MapValue.Put`, `SetValue.Add`, `GridValue.SetAt`,
`SparseValue.Put`) and is its own functional form minus the clone. And the
set of driving primitives is three, not two: **`Fold From:` a channel**
(`FoldOver`) is the instruction-driven simulation idiom, and the 300×300 grid
measurement above is exactly it.

### The aliasing firewall

The analysis proves the accumulator is dead *inside* the lambda. It says
nothing about who else holds the seed — and `Part` and `Channel` branch from
one value, so this must keep working:

```domain ignore
Channeled Energy: Convert To Grid
Part "1":
    Maximum Technique: Fold     # folds over the grid, mutating
        ...
Part "2":
    Maximum Technique: Count Cells   # must see the original
```

So: **`Fold` clones its seed once, on entry, when any site in its body is
annotated.** One O(size) copy amortized over n writes, instead of n of them.
That is sound whatever else holds the value, and it needs no escape analysis.

### What is deliberately excluded

- **`Scan`** and **`Iterate`** keep every intermediate accumulator in their
  output. The pass only ever looks at `Fold` and `Reduce`, so these are
  excluded by construction rather than by a check that could rot.
- **`Iterate Until Fixed Point`** compares the old value against the new one
  to detect convergence. Mutating in place destroys the comparand. Loop bodies
  are out of scope for this phase entirely; see "later" below.
- **Constant folding.** `optimizer/exprsimp.go` folds a lambda by applying it
  twice, which an in-place update would break. Running the pass last is the
  fix; an assertion that no folding pass ever sees an annotated call keeps it
  that way.

### The two backends

- **`eval/eval.go`** — the `insert` / `del` / `setat` / `put` / `set` / `with`
  cases call the mutating methods that already exist beside the functional
  ones (`MapValue.Put`, `SetValue.Add`, `GridValue.SetAt`, `SparseValue.Put`)
  and return the same pointer.
- **`prims/higher_order.go`** — `Fold` and `Reduce` clone the seed once when
  the node is annotated.
- **`codegen`** — `dmMapPutIn` / `dmSetAddIn` / `dmGridSetIn` /
  `dmSparsePutIn` in `codegen/runtime.go`, each its functional sibling minus
  the clone, plus `dmGridClone` and `dmSparseClone` for the entry copy.
  `gen.ownAccumulator` mirrors `prims.ownAccumulator` so the two backends
  agree about which programs pay for it.

### Testing

The oracle was already built: **`--no-optimize` is the copying semantics**, so
every program run both ways must produce byte-identical output. What shipped:

- **Negative tests first** (`optimizer/linear_test.go`). Marking too few sites
  costs speed; marking one too many is a wrong answer, so the tests that
  matter are the ones pinning what the pass refuses — a read in a later
  argument, a read in a `consider` body, an updating body, a `Scan`, a
  receiver rooted at something other than the accumulator, a shadowed name.
- **Aliasing**, two programs whose sibling `Part` must still see the value it
  was given. Both fail loudly with the entry clone removed, which is the only
  way to know a firewall is load-bearing; the compiled oracle has the same
  case and fails the same way.
- **A property test** over seven Fold bodies × 84 inputs each, in-place versus
  copying, matching the discipline the other passes are held to — plus six
  cases added to the existing `diffCases()` differential harness, which runs
  every one against the naive pipeline over 60 random inputs.
- **The compiled oracle** (`codegen/linear_test.go`): six programs, each in
  both optimizer modes, so every case is a four-way agreement — interpreter
  and binary, copying and in-place.
- **Benchmarks** (`bench/`): `fold_map_dp` and `fold_grid_writes`, paired with
  hand-written Go and inside the 2× gate, so a regression is a red test.

One test earned its place by finding a mistake in the *plan* rather than the
code. A case written to prove the pass refuses `if size(insert(acc, …)) >
size(acc) then acc else insert(acc, …)` failed — and on inspection the pass
was right: by the time the `else` arm runs, the condition has been fully
evaluated, so nothing reads the accumulator afterwards. Path sensitivity
buys more than the conditional-record shape it was added for.

### Later, not now

Straight-line pipeline stages (`Apply`, and the value threaded through
`Repeat` / `While` bodies) are the same idea one level up: the value entering
a stage is dead after it *unless* a `Channel` or `Part` branches from it. That
is a second, coarser analysis over the node list rather than over an
expression tree, and it is worth doing — after the expression-level one has
been in use long enough to trust. `sparse_life` is the benchmark case waiting
on it: it is the one still over the 2× target, and each lap rebuilds two whole
planes inside a loop body rather than inside a fold.

`del` is the other deferred piece, and the smaller one: it needs a mutating
`Without` that does not disturb a key order someone may already be holding.

---

## Phase 2 — `Explore` with a cost, and a tally — **done**

Two new modes on one existing primitive. `prims/explore.go` is 241 lines and
`codegen/exploregen.go` is 141; both roughly double.

### `Cost:` — weighted search (gap 2)

```domain ignore
Domain Expansion: Explore
    Mode: Cheapest
    Cost: (s, t) -> weight(s, t)
    Until: (s) -> s = goal
    Using: (s) -> successors(s)
```

`runExplore` (`prims/explore.go:144`) already has the right shape; the change
is local:

- swap `ir.Queue` for `ir.PQ` — which exists, is tested, and breaks priority
  ties by insertion order so results stay deterministic across backends;
- `seen` splits into a settled set and a best-known-cost map;
- `Mode: Cheapest` returns the cost to the first `Until:` hit (`-1` when
  unreachable, the sentinel the mode already uses), `Mode: Costs` returns
  `Map<S, Int>` exactly as `Mode: Distances` does today.

`Cost:` in its 1-parameter form is the cost of *entering* a state, which
matches the AoC risk-map convention grid `Dijkstra` already implements; the
2-parameter form is the edge weight a node weight cannot express, and both
shipped. A negative cost is a runtime error, the same one grid `Dijkstra`
raises.

**Not shipped: `Heuristic:`.** A* is an optimization of the same answer and
would belong here, but it needs an admissibility rule to state and a decision
about what happens when the heuristic lies — and neither `Cheapest` nor
`Costs` needs it to be correct. Deferred rather than half-built.

**What the generated heap had to get right.** `Mode: Costs` renders its Map in
*settle* order, so equal-cost states have to come out of the compiled heap in
the same order `ir.PQ` gives them. `codegen/runtime.go`'s `dmPQ` therefore
carries the same insertion-order tiebreak — without it the two backends print
different text for identical costs.

### `Tally` — counting DP (gap 3)

```domain ignore
Domain Expansion: Explore
    Mode: Tally
    Value: (s) -> 1
    Combine: (a, b) -> a + b
    Using: (s) -> successors(s)
```

A memoized fold over the reachable DAG: a state with no successors takes
`Value:`, and every other state is its successors' values folded with
`Combine:`. The memo table is a `Map<S, V>` keyed the way the visited set
already is, so it inherits the existing keyability rule.

The successor graph must be acyclic. `Domain Expansion: Topological Sort`
already detects a cycle and **names a blocked node** rather than saying "there
is a cycle"; `Tally` reuses that error shape, since finding the offending
state by hand in a large search space is not a thing anyone can do.

This is the mode that answers "how many ways" — arrangement counting,
universe splitting, molecule expansion — none of which had any spelling.

Two details settled during the work. `Until:` **marks a leaf**: a satisfying
state is never expanded, so it contributes its `Value:` and stops — which
reuses the pruning rule the other modes already follow and makes "count the
paths that reach the goal" the natural spelling rather than a special case.
And `Value:` is **not restricted to `Int`**; `Combine:` folds whatever it
produced, and that type is the primitive's output.

Both backends walk with an **explicit stack** rather than recursion, so a deep
DAG is bounded by the heap rather than by how far a goroutine stack will grow
— the same reason `Explore` exists at all.

### Testing the search modes

`prims/explore_weighted_test.go` and `codegen/explore_weighted_test.go` (ten
programs, both optimizer modes). Two are cross-checks rather than cases:
`TestExploreCheapestAgreesWithGridDijkstra` requires the state search and the
grid primitive to answer the same question identically, and the memoization
test uses a lattice with 61 states and 4,052,739,537,881 paths through it — it
simply would not finish if either backend lost the memo.

### One bug found, outside this phase

The codegen oracle tests run their subtests in parallel, and `prims.Resolve`
keeps its binding and ambient scopes at package level — it says so in
`prims/ambient.go`: *"prims.Resolve / interp.Run are never called concurrently
within one process"*. Most programs never notice, because most have no
`Consider` binding and no `For` loop to leak. One that does resolves against
another test's scope and fails with an unknown identifier — intermittently,
and only under the full suite.

The front-end helper in `codegen/codegen_test.go` now serializes resolution.
The lock is in the test rather than in `prims` because serializing the
resolver for every caller would be a change to its threading model, and the
tests are the only thing that ever asked for concurrency.

---

## Phase 3 — sequences — **done**

Two independent changes that both remove detours.

### Gap 10 — `Map` where a `List` is accepted

`Set` already flows into the list-shaped primitives; `Map` does not, so almost
every program that reaches for `Count By` or `Group By` immediately spends a
`Convert To Entries`. Both halves are single chokepoints:

- **`prims/higher_order.go:24`, `listElem`** — 27 call sites, one function.
  Add `KMap`, yielding `Tuple(Key, Val)`, the same shape `Convert To Entries`
  produces.
- **`ir/values.go:11`, `AsList`** — add the `*MapValue` case, entries in
  insertion order, which is already the rendering and iteration order.
- **`codegen`** — the list-input lowering gains a map-to-entry-slice step.

That makes `Count`, `Map Each`, `Filter` and the rest accept a `Map`, and
makes the `aoc-toolbox.md` line from Phase 0 true.

Three things this turned up that the sketch did not have.

**A pre-existing type lie, in the way.** Six primitives — `Filter`, `Unique`,
`Take`/`Drop While`, `Sort By`, `Merge Ranges`, `Partition` — declared
`Out: in`, so over a `Set` they claimed to return a `Set` and evaluated to a
list. The interpreter already rendered the result as a list, contradicting its
own declared type; the compiler could not tell, so it emitted Go that did not
build. A `seqOut` helper now returns `List<elem>` for anything that was not a
List going in, because these operations are list-producing: `Filter` drops
elements and `Sort By` imposes an order, neither of which a Set or a Map has
anywhere to put.

**The codegen bridge had drifted from its own rule.** `seqConsumers` says in
its comment that it is "exactly the set of primitives whose Build calls
listElem", and ten were missing — `Take While`, `Drop While`, `Any`, `All`,
`Find`, `Find Index`, `Sum By`, `Product By`, `Min By`, `Max By`, plus `Count`
once it joined. Every one of them resolved over a `Set` and then failed to
build. `TestEverySequencePrimitiveCompilesOverASet` now walks all 29 of them,
so the list cannot drift again silently.

**One retype rather than fifteen.** A Map's `.Elem` is its *value* type, while
the sequence it reads as is one of entry tuples — so every emitter that types
a lambda parameter from `n.In.Elem` would have bound half the pair. Rather
than editing each, the bridge in `emitNode` hands the emitters a node whose
`In` is the list type it just produced. One place, and the two cannot fall out
of step.

### Gap 13 — first-order list builtins in expressions

`sort`, `unique`, `flatten`, `product`, `zip`, `enumerate`, `chunk`,
`windows`, `transpose`. None takes a function argument, so none violates the
"no higher-order builtins" rule — they are simply absent, and each absence
forces a nested body where an expression would do. Inside a `Fold` (gap 4)
a nested body is not available at all, so there the absence is total.

Every one of them exists as a primitive already, so this was nine entries in
three tables plus tests. `sort` uses the `ir.Compare` from Phase 0, so it
landed ordered over Text and tuples on day one — including in the compiled
backend, where a tuple element goes through Phase 0's interned `dmCmpN`.

Each mirrors the primitive of the same job *exactly*, which is where the tests
concentrate: `chunk` keeps a short final block and `windows` drops a partial
one (the whole difference between them), `zip` truncates to the shorter,
`unique` keeps first-seen order, `product` is `1` on the empty list the way
`sum` is `0`, and `transpose` refuses a ragged input with the wording
`Convert To Grid` uses.

The place they matter most is inside a `Fold`: a pipeline body cannot stand in
for a 2-parameter lambda (gap 4), so there these were not merely verbose to do
without — they were impossible.

---

## Phase 4 — scope — **done**

Three changes about what a piece of code can see.

### Gap 4 — bodies that stand in for an n-parameter lambda

`prims/block.go:126`, `blockLambda`, refuses any arity but 1. It already
synthesizes parameters and already takes `ambientDepth()` trailing ones for
`For` variables, so the shape is there.

```domain ignore
Maximum Technique: Fold
    Seed: (xs) -> emptymap("", 0)
    Params: (acc, row)
    Domain Expansion: Sort By
        Using: (c) -> c
    ...
```

The rule: **the last declared parameter is the body's current value**; the
earlier ones become bindings visible to every expression in the body. That is
the `ir.Binding` machinery `Consider` already uses, and
`codegen/blockgen.go` already threads bindings into the generated `dmBlockN`
as pointers — that was the fix in
[compiler-parity-plan.md](compiler-parity-plan.md) family 1, so extra
parameters ride a path that is built and tested.

Which parameter is the current value has to be documented loudly; it is the
one thing about this that is a choice rather than a consequence. It is the
**last**, which for a fold reads the way the lambda does: the body is a
pipeline over the element producing the new accumulator, and `acc` is the value
carried in rather than the one being transformed.

Every declared name is *also* a binding, the last one included. The first cut
bound only the earlier ones, which left the last name doing nothing at all —
a word the user writes that the language ignores. Binding it too costs nothing
(the value is already there) and means a `Params:` name never turns out to be
decoration. It also puts every name under the rules a binding name obeys:
`Params: acc, row` is now refused, because `row` is an expression builtin and
shadowing one would change what a call means for every expression in scope.

`Params:` is spelled as a comma-separated ident list — `Params: acc, row` —
which needed no parser change at all: `From: moves, rows` already parses that
way, so this reuses `ArgSet.Idents`.

The three consumers each gained the same shape. `typecheck` pushes the extra
parameters' *types* around `BindBlock`, `eval` pushes their *values* around
`RunBlock`, and codegen puts them in `g.bindNames` around `emitBlockCall` —
where the existing machinery takes over, because `emitBlockCall` already
passes every binding in scope into the block's function. A `Consider` from an
outer scope had this problem first, and the fix for it is the fix for this.

Three refusals ship with it, each naming which of the two spellings the
program is halfway into: a body with no `Params:` (name them, or write the
lambda), a `Params:` whose count disagrees with the arity, a `Params:` on a
one-parameter stage (its body already has a name for the only value there is),
and a `Params:` beside a written lambda (that lambda names its own).

### Gap 6 — naming the current value

`Consider line Of Apply` + `Using: (l) -> l` is the identity dance a pipeline
body needs to name its own element. Make the operation optional: bare
`Consider line Of` binds the value entering the scope.

`prims/locals.go` already branches three ways on how an `Of` value is written
(`case b.Of && len(b.Body) > 0`, `case b.Of`, and the `As` cases); this is a
fourth, and the cheapest one — the value is the input, unchanged.

It shipped as `Consider line Of Itself` rather than a bare `Consider line Of`,
and the reason is the REPL: a bare `Of` is how it knows an indented
sub-pipeline is still coming, so making that spelling complete on its own
would cost `Consider mean Of` + a body its continuation prompt. A word is
cheaper than that.

The lowering is a **synthesized identity lambda**, so `Of Itself` *is* the
program the long form spelled out — the typer, the evaluator, the optimizer
and the compiler all see what they already handled, and nothing downstream
learned a new shape.

Two things ship with it:

- **A source rewrite**, in `diag/optimize.go` rather than the auto-fixer:
  `expansion: fix` applies confident repairs for *errors*, and the long
  spelling is not an error. `domain expansion: optimize` collapses
  `Of Apply` + `Using: (x) -> x` into `Of Itself`, re-resolving afterwards
  like every other rewrite there, so a rewrite that broke the program would
  roll back.
- **A documentation fix.** `Consider … Of` binds what the operation makes of
  the value entering *the statement it is written on* — which for a `Map Each`
  is the whole list, not one element. The old wording ("once per pass through
  the stage") was accurate and read the other way round for exactly the stage
  people reach for most; [expressions.md](expressions.md#scope) now says which,
  and says that naming the element means putting the `Consider` on a statement
  inside the body.

### Gap 7 — `From:` inside loop and Shikigami bodies

`prims/prims.go:659` refuses it. A channel is fully resolved before the loop
starts and its value is immutable, so there is no ordering hazard — the
restriction looks inherited from "channels cannot *nest*" rather than
motivated on its own. Relaxing it means a simulation stops smuggling its
read-only environment through the loop state, which (because a loop body must
preserve its value type) it otherwise carries for every lap:

```domain ignore
Simple Domain: While                    # today: state is a 4-tuple,
    Using: (s) -> ikke item(s, 0) = "ZZZ"    # two slots of which never change
```

Checking the compiler first was the right call, and the answer was better than
budgeted: **loop bodies are emitted inline**, not as functions, and channels
are emitted as top-level variables — so a channel's value is already in scope
where a loop body runs. The compiler side cost nothing.

That is also what draws the line. A `Using:` body *does* compile to a function
of its own, and a Shikigami is inlined at call sites that need not share a
scope; neither can see a channel's local. So `scopeNested` split in two —
`scopeLoop`, which allows `From:`, and `scopeNested`, which refuses it and now
says which body kind it is in and why.

**One honest caveat**, recorded rather than papered over. The shape that
benefits is `Fold From:`, which folds a channel's list into the state each
lap. `Combine` and the other consumers *replace* the current value, so they
rarely satisfy a loop's type-preservation rule — which means a loop lambda
still cannot simply **name** a channel, and the survey's own AoC 2023 Day 8
example is not improved by this. Closing that wants a `Consider … From:`,
which is a new binding source rather than a relaxed scope, and is not in the
language.

---

## Phase 5 — parsing — **done**

Two additions to `Match Pattern`, both landed:
[match-pattern.md](match-pattern.md#repetition).

### Repeating holes

`{ns:int+ sep=", "}` captures one or more elements and types as `List<Int>`, so
`Time: 7 15 30` parses in one stage instead of a template plus a second split.

Three decisions worth stating, each of which is the difference between a
template that asks and one that silently matches the wrong thing:

- **The separator is required.** A default would be right about half the time —
  a space for `1 2 3`, a comma for `1,2,3`. `{ns:int+}` is a resolve error
  naming the fix.
- **`text` may not repeat.** A `text` hole is `.*`, greedy to the next literal,
  so a repeated one would swallow its own separators and capture the entire run
  as element zero — a template that appears to work and is wrong. `word+` is
  the repeatable spelling of "some text".
- **A repeated `word` element is narrowed** from `\S+` to exclude the
  separator's first byte. Without that, `a,b,c` captures as one element rather
  than three, because `\S+` happily eats a comma.

The lowering is one capture group over the whole run plus a post-split, not a
group per element: a Go regexp keeps only the *last* match of a repeated group,
so a per-element group would silently return only the final value. And the
repetition made a second lowering visible — the compiler had been building its
own unnamed regex from a switch of its own, one template lowered twice, exactly
how a compiled program comes to parse differently from the interpreted one.
`Template.RegexSource(named bool)` is now the only one, with
`TestRegexSourceNamedAndUnnamedAgree` pinning that the two spellings differ in
nothing but the group names.

A repeated hole takes the regexp path rather than the hand-rolled scanner — a
run of unknown length is not something a left-to-right scan over fixed literals
can bound — which is the documented fallback anyway.

### `Mode: Try`

`Try` keeps the lines that matched and drops the rest, so a file mixing
`turn on 0,0 through 9,9` with `toggle 0,0 through 9,9` is parsed by one pass
per shape.

- **`Try` is never inferred.** `One` and `Each` still come from the input type
  when `Mode:` is omitted; `Try` has to be asked for, or a typo in a template
  would quietly parse nothing instead of failing.
- **`Try` swallows one failure only** — a line of the wrong *shape*. A line
  that fits the shape and then fails to convert (an integer out of `int64`
  range) is a broken line rather than a different kind of line, so it still
  stops the program. Skipping it would turn a corrupt input into a quietly
  short answer. The distinction is a `matchMiss` wrapper on the one error
  `Try` may drop, rather than a string check on the message.
- **`Try` stands fusion down.** Every fused parse-then-reduce loop assumes each
  line parses and fails the program when one does not, which is the behavior
  `Try` exists to replace, so `tryFuse` declines and the ordinary per-node path
  runs.

### What this does not close

A repeated *hole* is not a repeated *group*. `3 blue, 4 red, 2 green` repeats a
`{int} {word}` pair, and no spelling of a repeated hole captures it — that line
still wants a split on `", "` and a second `Match Pattern, Mode: Each` over the
pieces. Capturing it in one template means nested capture with a per-element
output type: a different feature, and one that starts to look like admitting a
regex. Alternation and optional holes stay out for the reason
[match-pattern.md](match-pattern.md#deliberate-omissions) always gave.

### Testing the templates

- `pattern/repeat_test.go` — types, the match, the separator narrowing, the
  positional-homogeneity rule (`List<Int>` and `Int` are different types, so
  `"{int} {int+ sep=\",\"}"` is a Tuple), the four refusals, and the two-regex
  agreement.
- `prims/match_modes_test.go` — the three modes end to end, including that
  `Each` still refuses a mismatch, that `Try` is not inferred, and that a bad
  capture still stops a `Try`.
- `codegen/match_modes_test.go` — ten interpreter-vs-binary oracle programs in
  both optimizer modes, covering named and positional repetition, repeated
  words, `Try` over each shape, a `Try` that keeps nothing, and the two
  features together.
- `codegen/codegen_test.go` — `TestMatchPatternPathSelection` gains the
  repeated-hole case, and `TestModeTryStandsFusionDown` pins the fusion
  decision by the `[]string` the fused loop never builds.

---

## Phase 6 — template groups — **done**

Phase 5 closed gap 12 for holes. The residue it wrote down is the whole of this
phase: **a repeated hole is not a repeated group, and a `text` sponge is not an
optional one.** Both come up constantly, and both are currently paid for in the
expression layer, re-parsing text the template already walked past.

The worked example is AoC 2017 D7, whose lines are `pbga (66)` or
`fwft (72) -> ktlj, cntj, xhth`. Today the best single-pass spelling is:

```domain ignore
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{name:word} ({w:int}){rest:text}"
Cursed Technique: Map Each
    Using: (r) -> record("name", r.name, "w", r.w,
                         "kids", if length(r.rest) > 0
                                 then split(trimprefix(r.rest, " -> "), ", ")
                                 else take(split(r.rest, ","), 0))
```

`{text}` is `.*`, so it matches empty and acts as a de-facto optional — but an
untyped one. The template hands back raw text and the expression layer
re-implements the parse by hand, with `take(split(x, ","), 0)` standing in for
the empty-list literal the language does not have.

### Optional groups — `[ … ]`

```domain ignore
Using: "{name:word} ({w:int})[ -> {kids:word+ sep=\", \"}]"
```

→ `{name: Text, w: Int, kids: List<Text>}`, and D7 is one template with no
`if`, no `trimprefix`, no sponge.

An optional group wraps literals and holes. When it does not participate, each
hole inside takes its type's zero: `0`, `""`, or the empty list.

**The presence flag is `{?name}`**, a pseudo-hole that owns no capture and
yields a `Bool`:

```domain ignore
Using: "{n:int}[{?negated} (negated)]"     -> {n: Int, negated: Bool}
```

It is **opt-in**, for the same reason `Mode: Try` is. When the optional part
holds a repeated hole, absent already means the empty list and
`length(r.kids) > 0` answers the question; a mandatory `kids_present` beside it
would be noise. The flag earns its place only where the zero value is
ambiguous — an absent `{n:int}` reads as `0`, which a real `0` also does.

Spelling the flag as `{?name}` rather than a bracket attribute (`[?name …]`,
`[name: …]`) is deliberate: both of those are ambiguous against a group whose
body *starts* with a literal of that shape, and `[Time: {n:int}]` is exactly
the kind of input AoC contains. `{` is already reserved inside a template — a
literal brace is not expressible at all — so a hole-shaped flag cannot collide
with anything.

### Repeated groups — `( … )+`

```domain ignore
Using: "Game {id:int}: {draws:( {n:int} {color:word} )+ sep=\", \"}"
```

→ `{id: Int, draws: List<{n:Int, color:Text}>}` — 2023 D2 in one template.

A group of named holes gives `List<Record>`; positional gives `List<Tuple>` or
`List<List<T>>` under the same homogeneity rule a top-level template uses. The
lowering is the scalar repetition's, one level down: capture the whole run, split
on the separator, run the inner template over each piece.

**One level of nesting only.** A group may hold literals and holes, not another
group. That keeps the inner template a plain `Template` — the existing type,
the existing lowering, the existing tests — and one level covers every AoC
input I can find. A second level is a different feature and should be argued
for on its own evidence.

### What this needs from `pattern/`

The template parser currently finds a hole's end with `strings.IndexByte(s, '}')`,
which a group breaks immediately: the first `}` is now the *inner* hole's. Both
brackets need depth-aware scanning.

The bigger change is capture layout. Today hole *i* is capture group *i+1*, and
both backends hard-code that. With optional groups it stops being true: a group
gets a wrapper capture of its own (so a flag-only group still has somewhere to
read presence from), and a repeated group's inner holes own no outer capture at
all. So the regex and the capture plan must come out of **one** walk:

```go ignore
func (t *Template) lower(mode captureMode) (string, []Capture)
```

with `RegexSource` and `Captures` both thin wrappers. Phase 5 already paid for
learning this — the compiler had been building its own second regex from a
switch of its own — and a group makes the same drift much likelier, since the
inner template needs a regex, a split *and* a per-element assembler in both
backends.

Absence is read with `FindStringSubmatchIndex`, not `FindStringSubmatch`: the
latter reports a non-participating group as `""`, which a group that legitimately
matched empty also reports. The index form gives `-1` and is exact.

### What this does to the fast path

Groups take the regexp path — `fastEligible` returns false on any optional
group or group hole — which is the documented fallback and leaves the
hand-rolled scanner its real job, the all-`{int}` templates in the hot loop.

### What it cost outside `pattern/`

Two bugs, neither caused by this work and both found by it:

- **A record whose field is a record never compiled.** Struct names were
  numbered `R%d` from the intern table's length, and a nested type was interned
  *while the outer one's declaration was being generated* — before it had been
  inserted — so both got the same name and the generated Go had two
  `type R1 struct`. Repeated groups are the first thing in the language that
  produces a nested record, but `record("a", record(...))` could always write
  one, and `domain run` accepted it while `domain build` failed with a Go
  compiler error. Names now come from their own counters.
- **The oracle tests turned any failure into a ten-minute hang.** They took
  `frontEndMu` and released it on the next line rather than with `defer`, so a
  `t.Fatal` inside the critical section leaked the mutex: every other parallel
  case blocked forever, the package hit its timeout, and the failing subtest
  never returned — so its buffered output was never printed, and a one-line
  assertion failure surfaced as a goroutine dump with no `FAIL` in it. One
  shared `oracleFront` helper owns the lock now.

### Not in this phase

`Mode: Scan` and `Case:` are both worth building and neither shares machinery
with groups, so they are phase 7 — as is the `list()` wart this phase surfaced
twice.

---

## Phase 7 — reach — **done**

The leftovers phase 6 named, plus the wart it surfaced. None of these closes a
survey gap; each removes a place where a program had to work around the
language rather than in it.

### `Mode: Scan` — the template as a fragment

Every mode before this anchored the template to a whole line, so input the
template does not describe *exhaustively* had no spelling at all:

```domain ignore
Cursed Technique: Match Pattern
    Mode: Scan
    Using: "mul({a:int},{b:int})"
```

AoC 2024 D3 — `xmul(2,4)%&mul[3,7]!^mul(32,64]then(mul(11,8)mul(8,5))` — which
`Extract Integers` could reach only by discarding the structure that says which
numbers belong together. A line contributes as many values as it holds, results
concatenate into one flat list, and a line holding none contributes nothing.

That last part is why `Scan` is **never inferred**, the rule `Try` already
follows: it drops input the template did not describe, and a typo would then
parse nothing in silence.

The unanchored pattern comes from `Template.lowerAnchored` — the same walk as
the anchored one with `^`/`$` made optional — rather than from trimming the
anchors off a string, which would be a second lowering pretending to be a
substring operation.

### `Case:` — alternation without sum types

Regex alternation *inside* a template would let two branches capture different
field sets, and the output type could then only be a union. So the alternation
lives at the stage, where the rule that keeps it typed is checkable:

```domain ignore
Cursed Technique: Match Pattern
    Mode: Each
    Case: on     "turn on {a:int},{b:int} through {c:int},{d:int}"
    Case: off    "turn off {a:int},{b:int} through {c:int},{d:int}"
    Case: toggle "toggle {a:int},{b:int} through {c:int},{d:int}"
```

→ `{kind: Text, a: Int, b: Int, c: Int, d: Int}`.

- **Every case produces the same fields**, checked at resolve time. What varies
  is recorded in `kind` rather than in the shape.
- **Order is priority order**, so `turn on {n:int}` before `turn {n:int}` means
  the specific one wins.
- Cases need **named** holes: a tuple's slots are positions, and `kind` has
  nowhere to live in one.

This is what `Mode: Try` could only approximate. Try over three verbs reads the
file three times and concatenates the results *by verb*, losing the input's own
order — which a simulation is defined by. `Case:` is one ordered pass.

The syntax needed one new `ast.ArgValue` (`CaseArg`) and four lines of parser:
an identifier followed by a string, on a repeatable argument. Nothing about
statements or indentation changed.

### More hole types, and a flexible gap

| Form | Captures |
|---|---|
| `{hex}` | **`Int`**, parsed base 16 — `#70c710` arrives as a number |
| `{digits}` | **`Text`** — a run whose leading zeros matter, which an Int cannot hold |
| `{char}` | exactly one character; `{word}` is a whole run |
| `{~}` | a run of whitespace, owning **no field** |

`{~}` is what column-aligned input needs, since a literal space matches exactly
one. It is one-or-more rather than zero-or-more: the template author wrote a
gap, so there is a gap.

Two rules fell out. `word` and `char` are narrowed to exclude a repeated hole's
separator, as before; a fixed digit class **cannot** be — excluding `a` from hex
leaves something that is no longer hex — so a separator the class itself matches
is refused instead, naming the problem. And both backends read the numeric base
from one exported `pattern.CaptureBase` rather than each keeping a switch a new
hole type could be added to only half of.

### `emptylist(v)`

`list()` required at least one argument, so there was no empty-list literal and
"no children" had to be spelled `take(split(x, ","), 0)` — which the phase 6
D7 program needed twice before optional groups removed the need.

`emptylist(v)` joins `emptyset(v)` and `emptymap(k, v)`: the argument is a
**type witness**, never stored but still *evaluated*, so `emptylist(first(xs))`
over an empty list fails in both backends rather than one. `list()` cannot be
the spelling — with no arguments there is nothing to read the element type
from, and every expression's type is fixed at resolve time.

### One bug this phase introduced and caught

Refactoring the three match modes onto one `emitMatchOver` helper pasted the
template into the generated `dmFail` *message* instead of passing it as an
operand. A template contains quotes, so the generated Go stopped parsing —
caught by `bench`'s parity suite on `sparse_life.domain`, which is exactly the
sort of program that has a template with quotes in it. The template goes
through as an argument again.

---

## Deferred: recursive data (gap 5)

`ir.Type` has no recursive constructor, so nested packets, snailfish pairs and
anything JSON-shaped are unrepresentable — not slow, not awkward. This is the
most expensive item in the survey and the least frequent, roughly one AoC day
a year.

The decision to write down is *how* it would be done if it ever is: a single
built-in `Nested<T>` — a value is either a `T` or a list of `Nested<T>` — with
a parse primitive and a fold primitive, rather than user-declared recursive
types. That covers the AoC cases without putting sum types into a language
that has no other use for them, and it keeps the compiled representation to
one generated struct rather than an open-ended family.

Deferring it is defensible. Deferring it silently is not, which is why it is
on this page.
