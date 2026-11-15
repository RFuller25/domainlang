# Innate Domain scopes — implementation plan

> **Status: shipped.** What the implementation learned, and the four places it
> diverged from this plan, are recorded in
> [§Outcome](#outcome). Design:
> [Innate Domain scopes — design](../specs/2026-08-21-innate-domain-scopes-design.md).
> The scope this mechanism exists to make possible is planned separately in
> [the Game Dev scope](2026-08-21-game-dev-scope.md); nothing here depends
> on it, and this ships first and alone.

`Innate Domain:` stops meaning "import a library of Shikigami" and starts
meaning "declare what kind of program this is". Library import moves to
`Inherited Technique:`, keeping its implementation whole. A **scope** owns a
primitive pack, a `Part` role table, a prelude, a program-shape rule, and
the names of the hosts that run and compile it.

**One scope exists when this ships**: `Advent of Code`, whose content is
exactly today's. So the regression gate is unusually sharp, and it is worth
stating before the tasks rather than after them:

> **No existing test's expectations may change**, except where they exercise
> the keyword that moved. Not adjusted, not re-recorded. A test whose
> *expectation* changes for any other reason is evidence that something is
> wrong, and the right response is to fix the change rather than the test.

That is narrower than "the suite passes unmodified", which is what the first
draft said and which is not achievable: the keyword move is a deliberate
change to what a program says, so the tests that write `Innate Domain: aoc`
have to write `Inherited Technique: aoc` instead. Two other kinds of edit are
also expected and are not evidence of anything — a call site of an unexported
helper that gained a parameter, and a fixture that names the renamed example
file. §Outcome lists exactly what changed and why.

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
   used throughout `codegen/*_test.go`.
4. **No primitive without a catalog entry.** `TestCatalogCoversRegistry`
   (`prims/catalog_test.go:9`) pins `prims.Catalog` to exactly the
   registered set.
5. **Generated docs data is checked in and pinned.**
   `TestPrimitiveIndexIsCurrent` (`docs/gen_test.go:301`); regenerate with
   `go test ./docs -update`.
6. **Docs ship with the code, and they run.** Fenced ```domain run blocks in
   `docs/*.md` are executed and their `input`/`output` diffed, and
   `docs/coverage_test.go` demands **two runnable examples per documented
   feature**. Nothing in this plan adds a documented feature, so nothing
   here is exempt from it either: `docs/scopes.md` carries runnable
   examples like every other page.
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
   it protects. Nothing here runs a program differently, but
   `interp.RunScoped` is the seam a future host reaches through, and it
   must not become a second way to start a run.
10. **Every source file must be committed** (`repo_files_test.go`) — check
    `.gitignore` before naming a new directory `game/` or similar.

---

---

# Tasks

## 1.1 `ast`: the scope declaration

`ast/ast.go`:

- `Program` gains `Scope *ScopeDecl` (`Name string`, `Pos token.Position`).
- `Keywords` gains `"Inherited Technique"`. `"Innate Domain"` stays.
- `Statement` gains `PartRole string` (empty for an unroled Part) and
  `PartArg *PartArg` where `PartArg` is `{Text string; Int int64; IsInt bool}`.

**Gate:** `TestKeywordPrefix` and the keyword-ordering tests still pass.
`"Inherited Technique"` shares no prefix with an existing keyword.

## 1.2 `parser`: hoist the scope, re-keyword the import, role the Part

`parser/parser.go`:

- `parseProgram` (line 184): the hoist that currently catches
  `Innate Domain` splits in two. `"Inherited Technique"` → `importOf` →
  `prog.Imports` (unchanged logic, renamed function `importOf` kept).
  `"Innate Domain"` → new `scopeOf` → `prog.Scope`, erroring on a second
  declaration and naming the first one's position.
- `importOf`'s two error messages change keyword.
- `parsePart` (line 499) accepts `Part [ROLE] [STRING|INT] ":"`. The role is
  an IDENT; the argument is a STRING or an INT. `Part "1":` is unchanged.
  `Part World:` has a role and no argument. `Part Every 120:` has a role and
  an INT.

**Errors to get right here**, because they are the ones a user hits while
learning: `Part World "x":` in a scope where `World` takes no label;
`Part Draw` with no colon; `Part Gamma:` where `Gamma` is not a role —
though the last one is a *resolve*-time error, because the parser must not
know the role table.

**Gate:** `parser/parser_test.go` unchanged and green; new
`parser/part_role_test.go` covering the six shapes and the four refusals;
`parser/fuzz_test.go` still green.

## 1.3 `prims`: the scope table and the scoped registry

New `prims/scope.go`: `Scope`, `PartRole`, `Scopes`, `ScopeNamed`,
`DefaultScope`, `RegistryFor`, `AllPrimitives`.

`prims/prims.go`:

- `Registry` → `Core`, with `var Registry = Core` retained and marked
  deprecated.
- `resolver` gains `scope *Scope` and `prims []*Primitive`.
- `findPrimitive` (line 822) becomes `(r *resolver) findPrimitive` and
  searches `r.prims`.
- `unknownOpMessage` (line 831) searches `r.prims` and, when the phrase
  *would* have matched in another scope, says so: *"`Draw` is a `Game Dev`
  operation; this program's Innate Domain is `Advent of Code`."* That
  message is most of the value of having scopes at all.
- `ResolveWith` reads `prog.Scope`, resolves it, stores the name on the
  returned `ir.Pipeline`, and loads the scope prelude after the shared one
  and before imports.
- `ResolveOptions` gains `Scope string` — an override for tooling that
  resolves a fragment with no declaration in it (the REPL, an LSP
  completion request). Empty means "read it from the program."

`prims/infer.go`: `InferWith` takes the scoped primitive slice;
`inferPrimitive`, `reservedMeaning` and `callableNames` use it. Three
signature changes, all internal except `InferWith`, which has one external
caller.

**Gate — the load-bearing one for Part 1:** every existing test in
`prims`, `interp`, `codegen`, `optimizer`, `diag`, `lsp`, `docs`, `bench`
and `cmd/domain` passes **unmodified**. Add
`prims/scope_test.go:TestDefaultScopeRegistryMatchesCore` asserting
`RegistryFor(DefaultScope)` is identical, element for element and in order,
to `Core`.

## 1.4 `prims`: Part roles

Implements §Part roles of the design. `prims/scope.go` gains `PartRole`,
`ArgRule`, `GlobalPolicy` and `RoleBind`; `prims/part.go`'s `resolvePart`
gains the role lookup, the contract check and role-keyed duplicate
detection.

Three things the design calls out that are easy to miss while writing this:

- `resolvePart` must stop requiring `cur != nil` (`prims/part.go:33`) and
  take the body's input type from the role. The `Advent of Code` role
  inherits the upstream value, so `Part "1":` is unchanged.
- Payload values are **named bindings on the `Consider` path**
  (`prims/locals.go`), never ambient lambda parameters. `RoleBind` is
  defined in this task even though no role in Part 1 uses one, because the
  type is what stops the next person reaching for `prims/ambient.go`.
- Only `GlobalsIsolated` keeps the `SnapshotGlobals`/`RestoreGlobals` pair.

**Gate:** `prims/part_test.go` unchanged; new `prims/partrole_test.go`
covering cardinality violation, contract violation, unknown role, and
"this role exists in another scope."

## 1.5 `ir`: carry the scope

`ir/ir.go`: `Pipeline.Scope string`. That is the whole change. The optimizer
does not read it; `optimizer/optimizer.go` must be checked for anywhere it
constructs a fresh `Pipeline` and shown to copy the field (a test:
`optimizer/scope_test.go:TestOptimizeKeepsScope`).

## 1.6 Hosts: name them, wire one

`interp/host.go` (new): `Hosts map[string]func(*ir.Pipeline, *ir.Context) (ir.Value, error)`,
with `"batch"` registered as today's `Run`. `interp.Run` keeps its exact
signature and behaviour and becomes the `"batch"` entry; nothing that calls
it changes. `interp.RunScoped(p, ctx)` dispatches on `p.Scope`'s host and is
what `cmd/domain` calls.

`codegen`: `Options` gains nothing; `Generate` reads `p.Scope` and refuses,
with a positioned error, any scope whose `Emit` host it has no
implementation for. In Part 1 there is only `""`.

**Gate:** `cmd/domain` switched to `interp.RunScoped`; every CLI test green.

## 1.7 Diagnostics, migration and the fix

- `diag`: `Innate Domain: <not-a-scope>` produces a diagnostic that lists
  the registered scopes and, when the target resolves as a *file* on the
  search path, says *"`aoc` is a library, not an Innate Domain — did you
  mean `Inherited Technique: aoc`?"* with an auto-fix.
- `diag/suggest.go`'s four `prims.Registry` walks take the scoped set, so
  "did you mean" never suggests an operation this program cannot use.
- `diag/lint.go`'s Part checks (lines 557–655) become role-aware: "never
  reveals anything" is a rule about the unroled role, not about `Part Draw:`.
- `cmd/domain/expansion.go`'s `fix` applies the keyword rewrite.

## 1.8 Tooling and docs for Part 1

- `lsp`: read the buffer's `Innate Domain:` line; scope completion
  (`lsp/completion.go:260,288`) and hover. Inlay hints for Part bodies
  (`lsp/inlay.go:92`) show the role's contract.
- `format`: `Part World:` and `Part Every 120:` normalize like any statement
  line — no scope registers those roles yet, but the *syntax* is accepted by
  the parser as of 1.2, so the formatter must already handle it or a game
  file will be mangled the day the roles exist. A test in
  `format/format_test.go` per argument shape. The formatter is
  line-oriented and copies phrase interiors verbatim, so this is small — but
  it is exactly the kind of small that breaks silently.
- `editors/embed.go`: generate from `AllPrimitives()`; add the new keyword
  and the role words. Both grammars regenerate; the drift test is the gate.
- `docs/scopes.md` (new): what a scope is, what `Advent of Code` contains
  (everything, which is the point), the at-most-one and hoisting rules, and
  the migration note. Linked from `docs/README.md`. It documents one scope
  today and is the page the next one is added to.
- `docs/language.md`: the keyword taxonomy table gains
  `Inherited Technique:`; §"Innate Domain — importing a library" splits into
  §"Innate Domain — declaring a scope" and §"Inherited Technique — importing
  a library" (the latter is today's text, re-keyworded, with its two
  runnable examples intact).
- `README.md`: the taxonomy table row.
- `go test ./docs -update`.

**Part 1 is shippable here.** One scope exists, one keyword moved, and the
entire existing test suite is the regression gate.

---

## Sequencing

The eight tasks are ordered by dependency and each leaves the tree green, so
they can land as one PR or eight. If they land separately, 1.1–1.3 have no
user-visible effect at all (the scope table exists and has one entry), and
1.4 onwards is where behaviour becomes observable.

| Task | Lands | Observable? |
|---|---|---|
| 1.1 `ast` | the scope declaration and the new keyword in the AST | no |
| 1.2 `parser` | hoisting, re-keyworded import, the role word | no |
| 1.3 `prims` | the scope table, `Core`, `RegistryFor` | no |
| 1.4 `prims` | `Part` roles, `GlobalPolicy`, `RoleBind` | no |
| 1.5 `ir` | `Pipeline.Scope` | no |
| 1.6 hosts | `interp.RunScoped`, the codegen refusal | no |
| 1.7 diagnostics | the migration error and its auto-fix | **yes** |
| 1.8 tooling/docs | lsp, fmt, grammars, `docs/scopes.md` | **yes** |

## Risk register

| Risk | Where | Mitigation |
|---|---|---|
| Scoping quietly changes an existing program's meaning | 1.3 | `Core` is today's `Registry` untouched; `TestDefaultScopeRegistryMatchesCore`; the whole existing suite green **unmodified** |
| A role's payload reaches for `prims/ambient.go` | 1.4 | `RoleBind` is defined in 1.4 with the reasoning in its doc comment, before any role needs one |
| `Part "1":` behaviour drifts | 1.4 | The `Advent of Code` role is written as an explicit `PartRole` literal and `prims/part_test.go` is left untouched |
| The optimizer drops the scope when it rebuilds a pipeline | 1.5 | `TestOptimizeKeepsScope` |
| A tool keeps calling `interp.Run` | 1.6 | Only one scope exists, so nothing can break yet — but the conversion is done here so the Game Dev plan inherits it done. See that plan's F4. |
| The old `Innate Domain: aoc` form fails unhelpfully | 1.7 | The error names the library, the new keyword, and offers the fix |

## Outcome

Shipped. Four things the implementation learned that the plan had wrong or
had not thought about:

**O1 — the `AoC` alias was a trap, and is gone.** The plan gave the default
scope the alias `AoC`, matched case-insensitively. That makes
`Innate Domain: aoc` — *the exact spelling every program written before this
change uses* — resolve as a scope declaration, silently dropping the import
and running a program with a Shikigami it never loaded. A wrong answer, not
an error, on the one line most likely to appear. The scope now has no alias,
so that line is an unknown Innate Domain and reaches the diagnostic in 1.7.
`prims/scope_test.go:TestAliasesResolveButAoCIsNotOne` pins it, and the
reason is written where the alias would go.

**O2 — a nil `In` means "inherit the upstream value".** The plan had the
unroled role declare `In: func(upstream, _) { return upstream }`, which
silently disabled the "Part has no upstream value to branch from" error: the
check fired only when `In` was nil. Inverting the default — `In == nil`
*means* inherit — makes the common role declare nothing, keeps the existing
error, and drops a parameter from the signature.

**O3 — `Registry` could not survive as an alias.** `var Registry = Core` is a
slice-header copy, so a test that *assigns* to `Registry` (as
`prims/infer_test.go` does, to register a clashing primitive) no longer
affects resolution — it would silently stop testing what it claims to. A
variable that does nothing when written is worse than a rename, so `Registry`
is gone and its readers say what they mean: `AllPrimitives()` for the tools
that describe the language, `Core` for the shared vocabulary, `RegistryFor`
to resolve one program.

**O4 — `Scope.Host` and `Scope.Emit` were unnecessary.** The plan had a scope
name its runner and its backend, with the CLI mapping names to
implementations. But `interp` cannot import `prims` at all — each package's
tests import the other, so a real import edge is a cycle in the test binary.
Hosts therefore register *against a scope*, by the name the pipeline already
carries (`interp.RegisterHost`, `codegen.RegisterScope`), and the two fields
are gone. The default stays `Run`, so a scope that says nothing gets the
behaviour every program has always had.

Two things the plan asked for that were deliberately **not** built, because
with one scope they are unobservable and untestable:

- **Scope-aware LSP completion.** `lsp/completion.go` offers
  `AllPrimitives()`, which with one scope is `Core` — identical to today.
  Filtering by the buffer's scope lands with the second scope, where it can
  be tested; it is task 2.G in the Game Dev plan.
- **`diag/suggest.go` scoping.** Same reason: "did you mean" cannot suggest
  an operation this program lacks, because no program lacks one.

One rename the plan did not anticipate: `examples/18_innate_domain` is now
`examples/18_inherited_technique`, because the file demonstrates importing a
library and was named after a keyword that no longer does that.

## Done means

- `RegistryFor(DefaultScope)` is element-for-element identical to `Core`.
- `go test ./...` green with **no existing test modified**.
- `examples/18_innate_domain.domain` and the four `docs/language.md`
  passages use `Inherited Technique:`, and their examples still run.
- `Innate Domain: aoc` produces the migration error and
  `domain expansion: fix` repairs it.
- `docs/scopes.md` exists, is linked from `docs/README.md`, and its examples
  run under the docs harness.
- A `Part World:` line parses, and resolves to "unknown role `World` in
  `Advent of Code`" — the mechanism is in place and the table is empty,
  which is exactly the state the Game Dev plan starts from.
