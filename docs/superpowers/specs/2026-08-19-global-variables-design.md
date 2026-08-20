# Global variables — `Cursed Object` and `Cursed Tool` — design

Domain has no way to name a value that outlives the statement it was written
on. `Consider … As/Of` comes close — its writes already survive from element to
element and from lap to lap — but the name dies with the statement, so a loop
that computes something the rest of the program needs has to smuggle it out
through the loop's own value. Because a loop body must preserve its value type,
smuggling one number out means every lap carries a tuple:

```domain ignore
Cursed Energy: i15
Match Pattern
    Using: "Generator A starts with {numa:int}\nGenerator B starts with {numb:int}"
Cursed Technique: Apply
    Using: (x) -> tuple(x, 0, 0)
Simple Domain: While
    Consider i Of (x) -> item(x, 2)
    Consider t As 40000000
    …
    Using: (x) -> i < t
    Cursed Technique: Apply
        Using: (x) -> if masker(a) = masker(b)
            then tuple(record("numa", newa, "numb", newb), item(x,1) + 1, i + 1)
            else tuple(record("numa", newa, "numb", newb), item(x,1), i + 1)
Cursed Technique: Apply
    Using: (x) -> item(x, 1)
Reveal: stdout
```

Three of those four tuple slots exist for no reason except scope. The counter
is not part of the answer's shape; it is a counter. Every `item(x, 1)` is
ceremony paid to get it back out, and the trailing `Apply` exists solely to
unwrap what the loop was forced to wrap.

This proposal adds **globals**: names declared on the pipeline layer whose
scope is the rest of the program rather than one statement. The same program:

```domain ignore
Cursed Energy: i15
Match Pattern
    Using: "Generator A starts with {numa:int}\nGenerator B starts with {numb:int}"
Cursed Object:
    a As numa
    b As numb
    matches As 0
Simple Domain: Repeat 40000000
    Cursed Tool:
        a As (a * 16807) % 2147483647
        b As (b * 48271) % 2147483647
        matches As if band(a, 65535) = band(b, 65535) then matches + 1 else matches
Cursed Technique: Apply
    Using: (x) -> matches
Reveal: stdout
```

No tuple, no record, no `item`, and `matches` is still readable after the loop
— which is the thing that forced the tuple to exist at all.

## Non-goals

- **No new scoping construct.** `Consider` keeps its meaning exactly. A global
  is not a longer-lived `Consider`; it is a different declaration with a
  different keyword, and the two coexist with the ordinary inner-wins rule.
- **No dynamic namespace.** Every global is declared, typed, and assigned a
  slot at resolve time. There is no `:=` that creates a name, no reflection
  over the global table, and no way to compute a name.
- **No change to the value flowing between stages.** This is the load-bearing
  non-goal; §2 gives the measurements behind it.
- **No globals in `Channel` bodies** (§5.2), and none in imported Shikigami
  (§5.3).
- **No function-valued globals via bare lambdas.** `Of` means "applied to the
  current pipeline value", exactly as it does on `Consider`.

## Cross-cutting constraints

Inherited from the repo's existing discipline, and each one shapes a decision
below:

1. **The interpreter is the oracle.** Every form here needs a
   `codegen` differential case; `domain run` and `domain build` must produce
   byte-identical output in both optimizer modes.
2. **Optimizer safety rule 4.** Passes that rewrite a node in place fire
   inside nested bodies; passes that change a node list's length run only at
   the top level. The effect analysis in §4 is a *node-in-place* annotation
   for that reason.
3. **`ast.Keywords` is pinned** by a test in `prims`. `Cursed Object` and
   `Cursed Tool` join the list, and both are backed by real primitives (unlike
   `Channel`, `Part` and `Shikigami`, which are structural).
4. **Docs move with the code**: `language.md`, `expressions.md`,
   `optimizer.md`, `compiler.md`, `primitives.md` + `primitives.json`, the
   README keyword table, and the embedded docs site.

---

## 1. Surface syntax

### 1.1 Two keywords

| Written | Means |
|---|---|
| `Cursed Object: NAME As <expr>` | declare a global, initialised from an expression |
| `Cursed Object: NAME Of <lambda>` | declare a global, initialised from the current pipeline value |
| `Cursed Tool: NAME As <expr>` | assign to an already-declared global |
| `Cursed Tool: NAME Of <lambda>` | the same, from the current pipeline value |

Both take a block form for runs of declarations, where the keyword is written
once and the lines beneath are bare `NAME As/Of …`:

```domain ignore
Cursed Object:
    a As numa
    b As numb
    matches As 0
```

The one-line form is the same statement with a single declaration on it.

**`Cursed Object`** is the vessel a sorcerer's energy is sealed into and
carried around in — a thing that persists past the moment that made it, which
is exactly what a global is. **`Cursed Tool`** is what acts on one. Neither
collides with an existing keyword, primitive ID, spelling, loop kind, vow
predicate, sink, source, or expression builtin; `Reverse Cursed Technique` is
the only other `Cursed`-prefixed multi-word keyword and `KeywordPrefix`
longest-match already handles the family.

### 1.2 Why `As`/`Of` rather than `NAME: value`

An argument line (`Using:`, `Mode:`) and a declaration line must stay
distinguishable, for the reason `parser/binding.go` already gives: an
argument's name is vocabulary and a binding's name is the user's, so a
misspelled `Usign:` has to stay reportable as a misspelled argument instead of
silently becoming a global nobody reads.

The prepositions carry the same weight they carry on `Consider`, and for the
same reason stated in `docs/expressions.md`: **a 1-parameter lambda already
means two different things in Domain depending on the slot it is written in.**
`Using: (x) -> …` is per element; `Size: (xs) -> …` is once, over the current
value. A declaration has no slot to disambiguate it, so it says which it means.

```domain ignore
Cursed Object: limit As 40000000          # a constant; never sees the pipeline
Cursed Object: width Of (g) -> length(g)  # computed from the value arriving here
```

This is also what keeps `Of Sum` unambiguously the primitive rather than an
identifier named `sum` — `parseOfSource` already refuses a bare expression
after `Of`, and that rule transfers unchanged.

### 1.3 Parser changes

`ast.Binding` is reused as-is: it already carries `Name`, `Of`, `Value`,
`Lambda`, `Block` and `Pos`, and the three right-hand-side forms are the three
this needs. `ast.Statement` grows one field:

```go
Decls []*ast.Binding   // Cursed Object / Cursed Tool declarations
```

kept separate from `Binds` so that "scoped local" and "global" never blur in
any consumer.

The block form is decided **by the enclosing keyword, not by lookahead**.
`parseBlock` already has `stmt` in hand and already discriminates on
`singleWordThemed`; a block under `Cursed Object`/`Cursed Tool` parses its
lines with a prefix-free variant of `bindingLine` (`IDENT As|Of …`, no
`Consider` word). Nothing outside those two keywords changes shape, so no
existing program can reparse differently — the strongest form of the
compatibility promise, since it holds at the token level.

The expression-keyword rejection list in `parseBinding` (`as`, `of`, `in`,
`if`, `then`, `else`, `consider`, `and`, `or`, `ikke`, `also`) applies
unchanged.

---

## 2. Storage: why globals do not ride the pipeline value

The obvious implementation is to widen the value passed from stage to stage
into `{value, globals}`. It was measured and rejected. The measurements are
worth recording because they also rule out the *second* obvious
implementation, which is to reuse the binding stack.

### 2.1 What the existing binding mechanism costs

`EvalLambdaTyped` (`eval/eval.go`) seeds every application's environment with
every binding in scope, unconditionally — before it knows whether the body
reads one. Benchmarked on `(x) -> x + 1`, a body that reads nothing:

| bindings in scope | ns/op | allocs/op | vs. 0 |
|---|---|---|---|
| 0 | 133 | 1 | 1.0× |
| 1 | 236 | 1 | 1.8× |
| 4 | 368 | 1 | 2.8× |
| 8 | 1083 | 7 | 8.1× |
| 16 | 2463 | 10 | 18.5× |

(Go 1.25, linux/amd64, Xeon @ 2.10GHz; `BenchmarkApplyBindings*` in
`eval/bindings_bench_test.go`. The step at 8 is the environment map outgrowing
its inline bucket. Absolute ns/op drift a few percent between runs and the top
row drifts more — the claim is the *shape*, which is stable: flat allocation
behaviour to 4, a cliff at 8, and an order of magnitude by 16.)

Globals are program-scoped by definition, so "in scope" means *all of them, in
every lambda in the program*. A program with eight globals would pay 8× on
every application in every stage, including the ones that never mention a
global. The day-15 program above runs 40 000 000 laps; that is the difference
between a run you wait for and a run you abandon.

**Seeding only the globals a body actually reads does not rescue it**: one
binding is already 1.8×, because the cost is the environment map's construction
and not its size.

### 2.2 What wrapping the pipeline value costs on top

Beyond the above, a `{value, globals}` wrapper would:

- allocate once per stage per element, on the hottest path in the interpreter;
- break `In`/`Out` type identity, which is what `Simple Domain`'s
  type-preservation rule, `optimizer/fuse.go`, and every algorithm
  substitution pattern-match on. `ir.Type.Equal` comparisons across the
  optimizer would all need to look through the wrapper;
- require unwrap/rewrap in all 88 registered primitives and every `codegen`
  case;
- put an interface box around every value in the compiled backend, whose
  entire performance story is that it types values concretely (`bench/`
  measures `domain build` against hand-written Go at a 2× gate — a boxed value
  model does not survive that gate).

### 2.3 The design: a slot array, and reads resolved at compile time

Globals live in a flat array on the run. They never enter the pipeline value
and never enter `Env`.

```go
// eval — the run's global slots. One entry per declared global.
var globals []ir.Value
```

At resolve time the resolver holds `globalIndex map[string]int` and allocates
each declaration a dense slot. `rewriteExpr` (`prims/locals.go`) already walks
every expression with a `shadowed` set and already rewrites `*ast.Ident`; its
`Ident` case gains one branch — an unshadowed name that is not a local and
*is* a global becomes a new AST node:

```go
type GlobalRef struct {
    Slot int
    Name string          // for errors, tracing, and the formatter
    Pos  token.Position
}
```

and evaluation is a slice index:

```go
case *ast.GlobalRef:
    return globals[x.Slot], nil
```

Three consequences, and they are the whole argument for this shape:

1. **`EvalLambdaTyped` is untouched.** A program with any number of globals
   stays on the 133 ns/op row of §2.1. Globals end up *cheaper than stage
   bindings are today*.
2. **No name lookup at runtime.** A `GlobalRef` is a bounds-checked load
   (~1 ns), not a map probe (~15–25 ns).
3. **No `*Cell` boxing.** The slot *is* the cell, so a write to a global does
   not go through one.

   The further claim — that a program whose mutation is entirely in globals
   keeps its bindings unboxed — is **not** delivered, and is left as a possible
   refinement. `programUpdates` decides boxing from the parsed tree, before
   resolution knows which names turned out to be globals, and a name can be a
   global in one place and a stage binding in another. Deciding wrongly there
   is silent and breaks a real write, so it keeps the conservative answer: any
   `:=` anywhere still boxes every binding. Globals themselves are unaffected
   either way.

`rewriteLambda`'s early bail (`len(r.locals) == 0`) becomes
`len(r.locals) == 0 && len(r.globals) == 0`, preserving the pointer-identity
property that keeps a program without either byte-for-byte what it was.

### 2.4 Where the array lives

Per **run**, reset like `ResetBindings`, not per process. `eval/bindings.go`
documents a single-threaded, one-call-at-a-time assumption that holds for the
CLI, but the LSP and the REPL resolve one program while another is still
running, and slot indices from two different programs must never collide. The
reset is the same discipline `ResetBindings` already applies for the same
reason.

---

## 3. Semantics

### 3.1 Scope and lifetime

A global is in scope **from the line that declares it to the end of the
program**, and it outlives whatever block it was written in. That is the whole
rule, and it is what the feature is for: a `Cursed Object` declared inside a
`While` body is still readable after the loop.

A declaration **re-runs wherever it is written**. Inside a loop body, that
means it re-initialises every lap. This is the honest reading of a statement
that is a statement, and it is what `Cursed Tool` is for:

```domain ignore
Simple Domain: Repeat 10
    Cursed Object: n As 0        # 0 every lap — almost certainly a mistake
    Cursed Tool:   n As n + 1    # what was meant, with n declared above the loop
```

The linter should say so: a `Cursed Object` inside a loop body whose
initialiser does not read the pipeline value is a hint, not an error (it is
legitimate for a name whose value genuinely is derived fresh each lap).

Reading a global **above** its declaration is a resolve-time error naming the
declaration's line. Hoisting was considered and rejected: it makes nesting
purely cosmetic and it makes "what is `n` here" unanswerable by reading
downward, which is how every other name in Domain is read.

### 3.2 Typing

A global's type is fixed at its declaration, from the static type of its
initialiser. `Cursed Tool` and `:=` typecheck against that type and may not
widen it — `n := 1.5` on an `Int` global is an error, exactly as it is on a
stage binding. Widen at the declaration.

One slot, one type, for the whole run. That is what lets the compiled backend
give each slot a concretely typed Go variable (§6) instead of an `any`.

### 3.3 Shadowing

Globals may not take a name that already means something: primitive IDs and
their spellings, themed keywords, loop kinds, vow predicates, `Reveal` sinks,
input sources, and the 153 expression builtins. `checkShikigamiName`
(`prims/infer.go`) already implements exactly this test for Shikigami and is
generalised to take a name plus a "what is being declared" noun, so the two
callers cannot drift apart. Two additions the Shikigami rule does not need:
**Channel names** and **`Match Pattern` capture names** (`{numa:int}` binds
`numa`), both of which land in the same expression namespace a global does.

Inward, the ordinary rule applies: **inner wins**. A lambda parameter, a
`consider` local, or a `Consider … As/Of` stage binding of the same name
shadows a global for its extent. This is free at runtime — the resolver simply
does not emit a `GlobalRef` for a shadowed `Ident`, which is the `shadowed` set
`rewriteExpr` already threads.

Redeclaring a live global with a second `Cursed Object` is an error that names
the first declaration and points at `Cursed Tool`. Silent reset is the reading
that hides typos.

### 3.4 `:=` on a global

`NAME := VALUE` writes to a global exactly as it writes to a stage binding, and
yields the value written. `ast.AssignExpr` grows a resolved slot (or the
resolver rewrites the node to a global-assign variant — §7 picks one), so
`assignTo` never string-matches a global at runtime.

Assigning to an **undeclared** name stays an error, and gains a "did you mean"
over the declared globals. There is no declaration-by-assignment: the type has
to come from somewhere and slot allocation is static.

### 3.5 What a global cannot be

- Not a function. Domain has no function values and this does not add one; a
  bare-lambda global is refused with the same message a function binding gets
  when it is read without being called. `Consider f As (x) -> …` remains the
  way to name a function, inlined at its call sites.
- Not declared inside a `Channel` body (§5.2) or an imported Shikigami (§5.3).

---

## 4. The optimizer

This is the only part of the proposal that costs anything, and the cost is
semantic rather than cycles.

### 4.1 The hazard

A stage whose lambdas write with `:=` is no longer a pure function of its
input, and the optimizer already stands its rewrites down for that stage
(`optimizer/declined.go`, `DeclinedEffectful`). Globals widen the blast radius:
a write in one stage is observable in any later stage that reads that global.
The conservative reading — any `Cursed Tool` anywhere disables rewrites
program-wide — is correct and unacceptable: one mutable counter would cost a
program its algorithm substitution, its fusion, and its expression
simplification everywhere, which is the language's entire proposition.

### 4.2 Per-global read/write sets

The resolver already knows, per slot, every expression that reads it (it
emitted the `GlobalRef`) and every statement that writes it (`Cursed Tool`
nodes, plus `ast.UpdatedNames` over the `:=`s). Record on each node:

```go
MetaGlobalReads  = "global_reads"   // []int, slots this node's expressions read
MetaGlobalWrites = "global_writes"  // []int, slots this node assigns
```

A stage declines rewrites iff it reads a slot that some **reachable** stage
writes — reachable meaning "in this node list or nested under it, at or after
this node, including any enclosing loop's later laps". A stage reading only
never-written slots is pure and keeps every rewrite; so does a stage that reads
no global at all, which is every stage in every program written before this
lands.

A never-written global is additionally a **constant of the run**, and is folded
into the bodies that read it exactly as `Consider … As 4` is today — so an
immutable global costs less than nothing, and `Cursed Object: target As 2020`
followed by `(a, b) -> a + b = target` still becomes the hash-set scan.

A new `DeclinedGlobalRead` code joins `optimizer/declined.go` so
`domain expansion: diagnosis` can say *which* global cost a stage its rewrite
and *which* stage writes it. That reporting is not a nicety: the failure mode
of this feature is a program that silently got slower, and the existing
declined-site machinery exists precisely to make that sayable.

### 4.3 Build order for the analysis

Ship the conservative version first — any written global taints every reader in
the program — with the precise version behind its own test suite, and switch
only when that suite is green. Getting this wrong is *silent*: a stage that
keeps a rewrite it should have lost produces a wrong answer, not a slow one.
The safe behaviour must be what ships if the analysis has a hole.

---

## 5. Interaction with the block constructs

### 5.1 `Part` — isolated, by snapshot

`docs/language.md` states the guarantee outright: sibling Parts branch from the
same upstream value, and "Part 1 sorting cannot disturb what Part 2 sees". A
mutable global would punch straight through it.

A `Part` therefore **snapshots the slot array on entry and restores it on
exit**. Writes inside a Part are visible to the rest of that Part and discarded
when it ends. The cost is one array copy per Part — Parts run twice per
program, not once per element — so this is free in every sense that matters.

Reads are unaffected: a Part sees every global declared above it.

### 5.2 `Channel` — sealed both ways

A channel is fully computed before its consumer runs, and `docs/language.md`
leans on that: "a channel is fully computed before the loop starts and its
value never changes, so there is no ordering hazard." A channel body that read
a global would make channel evaluation order observable; one that wrote a
global would reintroduce precisely the hazard channels were designed to
remove.

Channel bodies therefore have **no global access at all**, refused at resolve
time with a message pointing at the two things that do work: declare the global
from the channel's *value* after it is consumed, or use `From:`.

### 5.3 `Shikigami` — local yes, imported no

A Shikigami defined in the program file may read and write globals. It is
inlined at its call sites, so this is nearly free and the `GlobalRef` rewrite
happens after substitution, on the inlined body, where the caller's slots are
the ones in scope.

A Shikigami imported via `Innate Domain` is **sealed**: it may not read or
write the caller's globals, and any globals in its own file are private to it.
A library whose behaviour depends on names its author never saw is not a
library. The check rides on `MetaForeign`, which already marks exactly these
definitions.

### 5.4 Loops

No special case. A `Cursed Object` at a loop head is a statement in the loop's
enclosing list and runs once; one inside the body runs each lap (§3.1). A
`Cursed Tool` inside the body is the accumulator idiom, and its writes are
visible on the next lap and after the loop — which is the whole feature.

`Cursed Object` and `Cursed Tool` nodes are `In == Out` passthroughs, so a loop
body containing one still satisfies the type-preservation rule and
`--explain` / `visualize` show them as ordinary stages.

---

## 6. The compiled backend

Each slot becomes a **package-level Go variable** with the concrete type from
§3.2:

```go
var dmGlobal3 int64
```

Package level rather than a local in `main` is required, not stylistic: a
`Using:` written as an indented pipeline compiles to a top-level function, and
`docs/language.md` gives that as the structural reason `From:` channels are
refused there. A package-level variable is in scope in every generated
function, so globals work in exactly the place channels cannot — confirmed by
an oracle case whose global is written from inside a compiled block function,
with no pointer threading of any kind.

> **Correction.** This section originally claimed the compiled cost would be
> "zero or negative — the day-15 program stops allocating a tuple and a record
> per lap". That is wrong, and step 5 measured it wrong. There is no allocation
> to remove: codegen already lowers a tuple to a Go **struct of concrete
> fields**, which for the day-15 state is three `int64`s the Go compiler keeps
> in registers for the whole loop. A package-level variable cannot be kept in a
> register — it is globally visible, so every lap is a real load and store.
> Measured over 500 000 laps, compiled: the tuple-threaded version 4.3 ms, the
> global version 4.9 ms. **Globals are about 14% slower than the tuple in the
> compiled backend**, not faster.
>
> The reasoning was sound for the *interpreter*, where the tuple really is
> allocated every lap, and it was wrongly carried across to a backend whose
> whole point is that it does not allocate those. The same program interpreted:
> tuple-threaded 1.42 s, global 0.50 s — **2.8× faster**, which is where the
> win actually is.
>
> A later step could close the compiled gap by caching a global in a local
> across a loop whose body provably cannot observe it. That is a real pass with
> real correctness conditions (any call in the body might read the global), not
> a tweak, so it is noted rather than attempted here. Nothing about the feature
> depends on it: 14% on a program hand-written to suit the tuple is a fair
> price for deleting the tuple.

`Part` isolation (§5.1) compiles to saving the globals into locals on entry and
restoring on exit, in the function the Part body already becomes.

Evaluation order for `:=` inside an expression is already explicitly sequenced
by codegen (`docs/expressions.md`: "the compiler emits explicit sequencing to
guarantee it, since Go's own evaluation order does not"); global assignment
joins that machinery unchanged.

---

## 7. Order of implementation

Each step ends green, and the first four ship a usable feature.

1. **AST + parser.** `Statement.Decls`, the two keywords in `ast.Keywords`,
   the declaration-line parser, and the regenerated editor grammars. No
   resolution yet — parse and reject, with a message that names `Consider` as
   the thing to use meanwhile.

   `ast.GlobalRef` was originally listed here and moved to step 2. An
   expression node is only ever *produced* by the resolver, and there are
   fifteen non-test files switching over `ast.Expr` (the two backends, five
   optimizer passes, the formatter, the visualizer, typecheck). Landing the
   node before anything emits one would mean fifteen unreachable cases whose
   correctness nothing could test — the exact shape of latent bug the oracle
   discipline exists to prevent. It lands in step 2 beside the code that
   emits it and the code that consumes it.
2. **Resolver + interpreter.** `ast.GlobalRef`, the slot table,
   `checkShikigamiName` generalised, the `rewriteExpr` branch, the two
   primitives, the per-run slot array and its reset. `:=` to a global. At this
   point the interpreter runs the motivating program.

   Step 4's boundaries were pulled forward into this step. They are what stops
   a semantic being *wrong* rather than merely missing: with the interpreter
   running globals and no `Part` snapshot, a write in one Part is visible in
   the next, which contradicts a guarantee `docs/language.md` states outright.
   Shipping that for one commit and fixing it in the next is worse than
   ordering them together, and the snapshot is a dozen lines. The seals landed
   with it for the same reason.

   One hazard surfaced here that the design had not anticipated: constant
   folding. `prims.foldLiteral` evaluates a closed expression while the program
   is still being lowered, and after the rewrite that expression can contain a
   `GlobalRef` — whose slot array belongs to whatever ran last, if anything.
   The first version panicked on `Consider k As n + 1`. `eval.EvalConst` now
   refuses a global outright, which makes `foldLiteral` leave the expression
   alone (it treats any error as "does not fold") and guards every future
   `EvalConst` caller for free. A global is never a constant of the resolve;
   that is worth stating as a rule rather than as a patch.
3. **Typecheck.** Declaration typing, `Cursed Tool` and `:=` checked against
   the fixed type, and the "read above declaration" error.

   All of that arrived with step 2 — typing a declaration is what produces the
   slot's type, so it could not have been deferred. What this step actually
   found, probing for gaps rather than adding the listed work, was a
   **systematic bug class**: `ast.Statement` grew a `Decls` field in step 1,
   and five separate walks over `Statement.Binds` were never taught about it.
   A declaration's right-hand side is a `*ast.Binding` carrying the same three
   forms, so every one of them was a real defect:

   | Walk | What it broke |
   |---|---|
   | `prims.inferSequence` | `Cursed Object: s Of Convert To Set` — the keyword of a prefix-free phrase under a declaration was never inferred (`unknown keyword ""`) |
   | `prims.substituteStatement` | a global declared inside a Shikigami body could not use the definition's parameters |
   | `prims.collectUpdated` | a `:=` written in a declaration's value was invisible, so its target got folded to a literal and the program was refused |
   | `format.bindLines` | a `Consider` inside a declaration's `Of` body was not normalized |
   | `dev_fold.lastLineOf` | a `Cursed Object:` block folded to its keyword line, hiding nothing |

   Each was found by running the same program twice — once written with
   `Consider`, once with `Cursed Object` — and comparing. That is the
   technique the remaining steps should use, because the class is not
   exhausted: `lsp.TypeHints` and `diag.identityBindingRewrite` still walk
   `Binds` only. Both degrade rather than break (a missing inlay hint, a
   missed source rewrite), so they are left for step 8 — but they are the same
   bug, and step 5 should assume codegen has its own copy of it.

   The lesson generalises past this feature: adding a field to `ast.Statement`
   is not a local change, and the compiler cannot find the walks that should
   have grown with it.
4. ~~**Boundaries.**~~ Done in step 2, above — leaving them until after the
   interpreter ran would have shipped a `Part` that leaks.
5. **Codegen + differential tests.** Package-level vars, `Part` save/restore,
   and an oracle case per form (44 of them; see Appendix C). Nothing here may
   be skipped: the oracle is what the whole repo's correctness story rests on.

   Codegen turned out not to have the step-3 bug class after all — it reads
   bindings off `Meta` rather than walking the AST, so the fix was to give the
   declaration node the `Meta` it was missing (`ir.MetaGlobals`,
   `ir.MetaGlobalNodes`) rather than to teach a walk about `Decls`. Doing that
   also revealed the same gap one layer up: the optimizer's `nodeLists` could
   not see a declaration's `Of` body either, so a whole sub-pipeline ran
   unoptimized purely for having been written under a declaration. Fixed with
   it, and pinned by watching an in-place pass fire in that position.

   The step also produced the one measurement that contradicted the design; see
   the correction in §6.
6. **Optimizer, conservative.** A lambda that touches a global at all is left
   exactly as written, plus reporting for every stage that costs.

   Two departures from what this step was specified as, both found while
   building it.

   **The facts are derived, not recorded.** The plan said "reads/writes
   recorded on nodes" via `MetaGlobalReads` / `MetaGlobalWrites`. That is
   wrong, and dangerously so: a pass that rewrites a lambda changes which
   globals it touches, so an annotation goes stale exactly when a pass is doing
   the thing that makes it matter. `optimizer/globals.go` derives the answer
   from the tree on every ask instead. The two Meta keys that *are* on the node
   (`MetaGlobals`, `MetaGlobalNodes`) describe the declaration itself, which
   nothing rewrites.

   **A statement can now write, and `effectful` did not know.** Its doc said a
   `BlockBody` "carries no `:=` to find: the statements inside it have their
   own lambdas, which are checked wherever they are reached" — sound when `:=`
   was the only way to write, and false the moment `Cursed Tool` existed. A
   lambda whose body is a sub-pipeline containing one carries a write that no
   expression walk finds.

   No program can currently be provoked into a wrong answer by it — a block
   body cannot be composed into another lambda, and a declaration node is an
   unrecognized primitive that breaks the adjacency fusion needs. Six shapes
   were tried against fusion, early exit and algorithm substitution, and all
   six agreed with the naive pipeline. But that is an accident of which passes
   exist rather than a property anyone stated, and the failure mode when it
   stops holding is a wrong answer, not a slow one.

   Reporting landed as `optimizer.GlobalStandDowns` and a `perf` lint hint
   rather than as a `DeclinedGlobalRead` code in `declined.go`: that file's
   `Declined` is about update sites that copy their receiver, which is a
   different thing from a stage that lost its rewrites, and forcing one into
   the other would have made both harder to read. The hint names the stage, the
   globals it touches, and what to do instead — which matters most under this
   blunt rule, since a global that is never written still costs every stage
   that reads it.
7. **Optimizer, precise.** An immutable global costs its readers nothing.

   Not per-slot *reachability*, which the plan asked for. Reachability —
   "written at or after this node, including any enclosing loop's later laps" —
   is circular in a loop and subtle everywhere else, and the precision it buys
   over the simpler question is small. The question actually asked is **can
   anything change this global after it is declared**, and a global declared
   once at the top level and never written again is a constant of the run:
   every reader sits after its declaration, because visibility is forward-only.

   The answer is decided at resolve time and rides on `ast.GlobalRef.Mutable`.
   That is sound where recording a node's *read set* would not be (step 6's
   note): a pass can move or copy a read, but none invents one, and none can
   make an unwritten global written. It also keeps `effectful` a function of a
   lambda alone, which is what its six call sites all have.

   It over-approximates deliberately, in the safe direction: a `:=` counts by
   spelling without asking whether something nearer shadowed the global, and a
   declaration written anywhere that can run twice counts as a write. Seven
   mutability paths are pinned by tests — `Cursed Tool`, `:=`, a declaration in
   a loop body, a write in a loop body, a write in a `Part`, a write from a
   Shikigami body, and a write in a pipeline body — each reading the global in
   a stage the optimizer would otherwise rewrite, so a miss shows up as a
   rewrite that should not have happened.
8. **Tooling and docs.**

   The two `Binds`-only walks step 3 left behind are fixed: the LSP's inlay
   hints now show a global's type on its declaration line (which is where the
   type is fixed for the whole program, so it is the hint most worth having),
   and the diagnostics engine's identity rewrite reaches declarations.

   That second one is worth recording as a near miss. The rewrite's replacement
   text was hardcoded to `Consider ` + name + ` Of Itself`, so extending it to
   declarations turned a `Cursed Object` into a stage binding — a
   *semantics-changing* auto-fix. It never shipped only because
   `OptimizeSource` re-parses every rewrite and discards one that no longer
   resolves, which reported the change as "no rewrites apply" rather than as a
   bug. A rewrite must not need catching, so the replacement now takes the head
   from the source itself, keyed on the fact that a declaration's `Pos` is its
   *name* while a binding's is its `Consider` keyword. That handles the inline
   form, the block form, and `Cursed Tool`, none of which could be rebuilt from
   a constant string.

   The `Cursed Object`-in-a-loop trap from §3.1 is a linter warning, exempting
   the `Of` form (which reads the lap's value, so re-running it is the point).

   Docs: a `Cursed Object` section in `language.md` carrying the two runnable
   examples the coverage test requires, plus the keyword taxonomy row, the
   README table, the `:=` target table in `expressions.md`, a stand-down
   section in `optimizer.md`, and the package-level-variable section in
   `compiler.md` — which states the compiled cost honestly rather than
   repeating §6's original claim.

## 8. How this is measured

The claim being tested is that a program **without** globals is unchanged and a
program **with** them is no slower than the tuple-threading it replaces.

- `bench/` head-to-head (37 cases, `domain build` vs hand-written Go, 2× gate)
  and the `interp`/`optimizer`/`codegen` micro-benchmarks are captured on the
  merge-base before any code lands, and re-run after each of steps 2, 5 and 7.
  Every one of those cases is a program with no globals in it; **any**
  movement outside noise is a regression in the shared path and blocks the
  step.
- The §2.1 binding benchmark becomes a permanent test in `eval`, extended with
  a globals row, so the "globals are cheaper than bindings" claim is pinned
  rather than remembered.
- The motivating program ships as a `bench/testdata` case in both spellings —
  tuple-threaded and global — so the win is a number in the same table as
  everything else rather than an assertion in this document.



---

## Appendix B: what step 2 measured

Run pairwise on one container, back to back, as Appendix A says a real
comparison has to be.

**The claim the design rests on.** Both bodies are `(x) -> x + n`. One reads
`n` through the binding stack, so `n` was seeded into the environment that
application built; the other reads it through a slot, so the environment never
heard of it (`BenchmarkApplyBindingRead*` / `BenchmarkApplyGlobalRead*`):

| names in scope | binding read | global read | |
|---|---:|---:|---|
| 1 | 236 ns, 1 alloc | 131 ns, 1 alloc | 1.8× |
| 8 | 1152 ns, 7 allocs | 133 ns, 1 alloc | 8.7× |

The global row is **flat**: a program pays the same whether it declares one
global or eight, because the count never reaches the environment. The binding
row is not, which is exactly why globals could not have been built on it.

**No regression in the shared path.** The 37-case head-to-head suite moved a
median of 0.987 with a symmetric spread (5 cases >+5%, 10 cases <−5%, range
0.844–1.105). That spread is the container, and it can be proved rather than
argued: **all 37 programs compile to byte-identical Go before and after step
2**, so the binaries being timed are the same bytes. Step 2 touches only the
resolver and the interpreter; codegen refuses globals outright and was not
modified.

Which also means the head-to-head suite cannot detect a regression in the path
step 2 *did* change. For that, the interpreter benchmarks:

| | pre allocs/op | post allocs/op |
|---|---:|---:|
| `SplitHeavyInterpreter` | 646,235 | 646,235 |
| `MatchPatternInterpreter` | ~2,000,074 | ~2,000,068 |
| `TraceUntraced` | 2,051 | 2,051 |
| `TraceWithStats` | 2,071 | 2,071 |
| `TraceWithCounter` | 2,053 | 2,053 |

Allocation counts are deterministic where wall time on this container is not,
and they are unchanged — the interpreter is doing the same work, with no extra
environment entries and no extra boxing. Wall times on the same runs moved in
both directions (`MatchPatternInterpreter` 10% *faster*, `TraceUntraced` 17%
slower, identical allocations on both), which is the same noise the
byte-identical binaries exhibited.

**How to read a future step's numbers.** Prefer allocation counts and generated
output over wall time on this hardware. A step that changes neither the
generated Go nor the interpreter's allocation counts has not regressed the
shared path, whatever the milliseconds say.

---

## Appendix C: what step 5 measured

**The shared path is untouched.** All 71 programs in `bench/testdata`,
`examples` and `challenges` compile to byte-identical Go before and after step
5 — including the one change that touches shared code, the guard that stops
`loopgen` emitting `v = v` for a body of pure passthroughs. That guard fires
only for a loop body of nothing but `Cursed Tool` writes, which is what the
identical output proves.

**The motivating program, three ways**, 500 000 laps, checked against an
independent implementation (all three answer 7):

| | tuple-threaded | with globals | |
|---|---:|---:|---|
| interpreted | 1.42 s | 0.50 s | **2.8× faster** |
| compiled | 4.3 ms | 4.9 ms | 14% slower |

The compiled row is the opposite of what §6 predicted, and §6 now carries the
correction and its cause: there was never an allocation to remove in the
compiled backend, and a package-level variable is memory where a struct of
`int64`s is registers.

Both rows are worth keeping in view. The interpreter is what most Domain
programs run under — `domain run` is the default, `domain build` is the opt-in
— so a 2.8× on the interpreted path and a 14% on the compiled one is a good
trade, and it is the trade the feature actually makes.

**44 oracle cases** (`codegen.TestGlobalsOracle`) run every form through both
backends in both optimizer modes: each right-hand side, `:=` racing a read
inside one expression, both shadowing rules, `Part` isolation, writes from
inside a compiled block function and a `For` body, and every scalar and
composite type a slot can hold.


---

## Appendix D: the branch against main

The question this appendix answers is the one a reviewer actually asks: **does
this slow down programs that do not use globals?**

**No. Compiled output is unchanged and resolution is faster.**

**Compiled output is byte-identical.** All **81** programs in `bench/testdata`,
`examples`, `challenges` and `testdata` compile to the same Go on this branch
as on main. That is the whole answer for `domain build`, and it is a proof
rather than a measurement — no timing on shared hardware can say anything
stronger.

**The interpreter does the same work.** Allocation counts are identical on
every interpreter benchmark (`SplitHeavyInterpreter` 646,235;
`MatchPatternInterpreter` ~2,000,07x; `TraceUntraced` 2,051; `TraceWithStats`
2,071; `TraceWithCounter` 2,053). Wall times moved ±2–7% in both directions,
inside this container's demonstrated noise.

**Resolution is 16–26% faster**, with 51–142 *fewer* allocations per program:

| | main | branch | |
|---|---:|---:|---|
| `ResolveDay1` | 295 allocs | 192 | −103 |
| `ResolveGridBFS` | 240 | 163 | −77 |
| `ResolveExploreStates` | 199 | 148 | −51 |
| `ResolveShikigamiCalls` | 649 | 507 | −142 |

That is not a globals win. Measuring the branch honestly turned up a
**pre-existing** inefficiency that the feature had amplified: `KeywordPrefix`
re-split every entry of `ast.Keywords` with `strings.Fields` on *every call*,
and the parser asks it once per statement. Adding two keywords therefore added
two allocations per statement to every program in the language. Pre-splitting
the list once fixed it and then some.

**The optimizer is within noise**, with identical allocations: +0.2% to +4.5%
across four programs, on a pass that takes 15–30 µs. The residual is `nodeLists`
checking one more `Meta` key per node. Getting there took two attempts —
`effectful` was first written as `HasUpdate(e) || touchesGlobal(e)`, which
walked the whole tree twice, and then as one walk with a type assertion in
front of the switch, which still cost a check per node. It is now a single type
switch answering both questions.

**The head-to-head suite is not usable here and is not needed.** Its 37 cases
moved a median of 1.003 with a spread of 0.73–1.58 and a symmetric slower/faster
split — and the binaries it times are provably identical, so all of that is
container noise. (The branch half also overlapped other benchmark runs, which
made it worse.) Where a compiled comparison matters, diff the generated Go.

### What to re-measure, and how

Two benchmarks were added because neither existed and neither would have caught
this: `prims.BenchmarkResolve*` and `optimizer.BenchmarkOptimize*`. Read their
**allocs/op**, which are deterministic; read ns/op only as a tiebreak, and only
from interleaved A/B runs of both trees on one machine.

---

## Appendix A: pre-implementation baseline

Captured on the merge-base (`764df31`) before any code from this design landed,
so §8's regression gate has something to compare against.

**Read the ratio column, not the ns/op column.** These were taken on an
ephemeral cloud container with a shared Xeon @ 2.10GHz; absolute timings are
not comparable across containers, and a post-implementation run on a different
machine cannot be diffed against this table. The durable comparison is
**pairwise on one machine** — stash the change, run, apply, run, diff — and
this table's job is to say what the tree looked like when the design was
written, and to catch the case where a later run finds something the repo
never had.

Consistency check against `bench/README.md`, which records the same suite on
the author's hardware: `sparse_life` is the one knowingly-over-target case
there (2.60×; 3.19× here) and `toposort_words` is recorded at 2.02× (2.00×
here). Both line up, so this container is noisier but not anomalous. 16 of 37
cases are faster than the hand-written Go here, against 19 recorded in the
README — that spread is the container, not the compiler.

`go test ./bench -bench . -run XXX -benchmem`, 37 cases:

| Case | domain ns/op | go ns/op | ratio |
|---|---:|---:|---:|
| `read_length` | 10,114,205 | 73,971,400 | 0.14× |
| `pairs_increase` | 70,414,839 | 61,216,169 | 1.15× |
| `scan_mod` | 72,868,376 | 65,254,866 | 1.12× |
| `sliding_max` | 73,870,557 | 274,651,349 | 0.27× |
| `pipeline_body` | 112,024,130 | 82,118,713 | 1.36× |
| `text_builtins` | 96,488,176 | 66,978,461 | 1.44× |
| `fold_map_dp` | 116,308,202 | 114,848,887 | 1.01× |
| `fold_grid_writes` | 46,123,727 | 48,293,132 | 0.96× |
| `count_by_entries` | 69,268,949 | 63,858,057 | 1.08× |
| `partition_parts` | 67,391,918 | 65,416,628 | 1.03× |
| `topk_sum` | 74,648,935 | 416,263,884 | 0.18× |
| `dijkstra_grid` | 86,102,120 | 121,324,031 | 0.71× |
| `match_pattern` | 43,183,466 | 93,589,746 | 0.46× |
| `toposort_words` | 348,526,784 | 174,278,205 | 2.00× |
| `combinations3` | 985,373,196 | 934,425,122 | 1.05× |
| `sort_by_key` | 281,375,322 | 713,635,376 | 0.39× |
| `merge_ranges` | 444,281,243 | 448,830,273 | 0.99× |
| `group_map_values` | 154,278,747 | 90,811,589 | 1.70× |
| `set_intersect` | 15,523,071 | 143,600,104 | 0.11× |
| `connected_components` | 42,971,595 | 26,604,257 | 1.62× |
| `grid_bfs` | 111,937,441 | 71,451,178 | 1.57× |
| `sparse_life` | 279,633,415 | 87,564,252 | 3.19× |
| `explore_states` | 737,727,980 | 575,594,924 | 1.28× |
| `loop_repeat` | 25,573,062 | 25,358,766 | 1.01× |
| `join_output` | 172,814,098 | 87,579,266 | 1.97× |
| `channels_zip` | 102,954,407 | 364,367,896 | 0.28× |
| `shikigami_calls` | 84,254,100 | 75,807,761 | 1.11× |
| `vows_hot` | 56,166,131 | 243,588,188 | 0.23× |
| `grid_transform` | 83,460,265 | 82,677,914 | 1.01× |
| `float_sum` | 165,042,440 | 120,846,786 | 1.37× |
| `fold_tuple` | 58,791,498 | 68,743,471 | 0.86× |
| `iterate_unfold` | 29,130,946 | 23,063,968 | 1.26× |
| `while_halve` | 124,438,188 | 165,539,449 | 0.75× |
| `fixed_point` | 93,559,303 | 195,534,373 | 0.48× |
| `list_shaping` | 76,376,371 | 243,582,396 | 0.31× |
| `for_loop` | 180,792,490 | 253,753,127 | 0.71× |
| `math_builtins` | 149,537,412 | 90,216,349 | 1.66× |

`go test ./interp ./optimizer ./codegen -bench . -run XXX -benchmem`:

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `TraceUntraced` | 1,753,872 | 442,423 | 2,051 |
| `TraceWithStats` | 1,710,595 | 443,848 | 2,071 |
| `TraceWithCounter` | 1,652,470 | 442,447 | 2,053 |
| `TopK/Quickselect` | 2,454,612 | 1,605,643 | 1 |
| `TopK/FullSortThenSlice` | 35,940,767 | 1,605,693 | 3 |
| `PairScan/HashSet` | 259,344 | 147,776 | 17 |
| `PairScan/Naive` | 4,858,006 | 0 | 0 |
| `SplitHeavyInterpreter` | 37,807,384 | 22,293,448 | 646,235 |
| `SplitHeavyCompiled` | 20,271,438 | 29,736 | 61 |
| `MatchPatternInterpreter` | 263,433,768 | 120,610,442 | 2,000,068 |
| `MatchPatternCompiled` | 10,693,903 | 29,620 | 61 |

Every program in both tables has no globals in it, which is what makes them the
right gate: after each of steps 2, 5 and 7 they exercise only the shared path,
so any movement outside noise is a regression in code that globals were not
supposed to touch.
