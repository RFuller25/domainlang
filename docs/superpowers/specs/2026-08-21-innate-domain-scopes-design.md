# Innate Domain scopes — design

`Innate Domain: aoc` loads a file of Shikigami definitions. That is a useful
feature and it is under the wrong name.

An Innate Domain, in the source this language takes its vocabulary from, is
the sorcerer's own inner world — the space they impose on their
surroundings, inside which their rules hold. That is not an import
statement. It is the *kind of program this is*: the vocabulary in scope, the
shape a program is allowed to take, and what happens when it runs.

This proposal makes `Innate Domain:` mean that, and moves library import to
a keyword that describes what it does. The immediate motive is a second
scope — a terminal game vocabulary, specified separately in
[the Game Dev scope design](2026-08-21-game-dev-scope-design.md) — but the
mechanism is worth having on its own, and it ships first and alone.

The default scope is `Advent of Code`, and **its content is exactly what
exists today**. A program with no `Innate Domain:` line resolves to a
byte-identical pipeline. That is the binding constraint on the whole of this
document, and §Why the AoC pack is empty is where it is bought.

## Non-goals

- **No second grammar.** The pipeline layer and the expression layer are
  untouched. The parser learns exactly one new thing: an optional role word
  between `Part` and its label.
- **No new scoping construct.** `Consider`, `Cursed Object`, `Channel` and
  `Shikigami` keep their meanings exactly. A scope selects a vocabulary and
  a program shape; it does not change how names bind.
- **No change to any existing program's behaviour.** A file with no
  `Innate Domain:` line must resolve to the same pipeline, node for node,
  as it does today. This is the binding constraint, and the plan makes it a
  gate: the existing suite passes *unmodified*.
- **No vocabulary is taken away from anybody.** A scope only ever *adds*
  primitives. Nothing this document does can make an operation stop
  resolving in a program that resolves today. See §Why the AoC pack is
  empty.
- **No scope-specific syntax.** A scope may not add tokens, statement forms
  or expression forms. Its whole surface is: primitives, `Part` roles, a
  prelude, and a program-shape rule.
- **No scope inheritance, composition or parameters.** A program declares
  at most one, by name, and the table of them is closed at build time.

---

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

## What a scope is

```go
// prims/scope.go — new file.

// Scope is what `Innate Domain:` names: the kind of program this file is.
// A scope owns a vocabulary, a program shape, and the names of the hosts
// that run and compile it.
//
// It does not name its runner or its compiler backend. Those register
// themselves *against* a scope, by the name the pipeline already carries:
// interp.RegisterHost and codegen.RegisterScope. prims cannot reach either —
// each package's tests import the other, so a real import edge would be a
// cycle in the test binary — and the registration direction keeps the
// dependency graph pointing one way.
type Scope struct {
	Name    string   // "Advent of Code", "Game Dev"
	Aliases []string // "AoC", "Game" — accepted spellings, case-insensitive
	Summary string   // one sentence, for `domain --help` and the docs

	// Prims are the primitives this scope adds to Core, ordered
	// specific-matcher-first exactly as Core is. Empty for Advent of Code:
	// see "why the AoC pack is empty" below.
	Prims []*Primitive

	// PartRoles is the role table. A scope with no roles gets the single
	// unroled default role, which is today's `Part "1":`.
	PartRoles []PartRole

	// Prelude is extra Shikigami source loaded after the shared prelude and
	// before any import, so a scope may ship a standard library written in
	// Domain. nil for a scope that ships none.
	Prelude func() ([]byte, error)

	// TopLevelPipeline says whether ordinary pipeline statements are legal
	// at the top level. False for Game Dev, where a program is Parts and
	// declarations only — the same rule a library file already lives under
	// (prims/imports.go: "a library is a bag of Shikigami, not a program").
	TopLevelPipeline bool

}
```

Role cardinality is checked from the role table itself — the upper bound as
each Part resolves, where the offending line has a position, and the lower
bound once the program has been read, since a *missing* Part has no line of
its own.

**The default scope has no alias, and specifically not `AoC`.**
`Innate Domain: aoc` is what every program written before scopes existed
says, and it meant *import the aoc library*. An alias matching it
case-insensitively would resolve that line as a scope declaration and drop
the import in silence — a wrong answer rather than an error, on the one
spelling most likely to appear. It has to stay unknown, so the migration
diagnostic can name the keyword that now imports.

Scopes are registered in one ordered table, `prims.Scopes`, with
`prims.ScopeNamed(s string) (*Scope, bool)` doing case-insensitive lookup
across `Name` and `Aliases`. `prims.DefaultScope` is `Advent of Code`.

## Why the AoC pack is empty

The obvious move is to split today's `Registry` into `Core` plus an `aoc`
pack, so `Read Source` and `Match Pattern` and the toolbox live in the pack
where they "belong." **Don't.** Three reasons:

1. `Registry` is what `inferPrimitive` (`prims/infer.go:194`) searches to
   resolve a bare phrase, and what `reservedMeaning` (`infer.go:326`)
   searches to decide that a Shikigami name is taken. Moving anything out of
   the set an AoC program sees can only change what an existing program
   means. The constraint is that nothing changes.
2. A game wants nearly all of it anyway. Tetris wants `Transpose`. Tower
   defense wants `Dijkstra`. Oregon Trail wants `Match Pattern` to read a
   scenario file. Snake wants `Sparse`. The AoC-specific residue is small
   enough that partitioning it buys nothing.
3. The split can be made later, in isolation, if a scope ever wants to
   *refuse* a primitive. Nothing here needs that.

So: **`Core` is exactly today's `Registry`, renamed and left alone.**
`Registry` stays as a deprecated alias for one release so the five external
readers (`lsp/completion.go`, `diag/suggest.go`, `editors/embed.go`,
`cmd/domain/repl_doc.go`, `cmd/domain/coverage.go`) keep compiling while they
are converted. A scope's `Prims` are *appended* to `Core`; `RegistryFor`
returns the concatenation and is what the resolver, the inferencer and the
reserved-name rule use. `AllPrimitives()` returns the union across every
scope, for the grammar generators and the reference index, which want to
describe the whole language rather than one program's view of it.

Consequence, and it is the point: **in an `Advent of Code` program,
`RegistryFor` returns a slice with the same contents in the same order as
today's `Registry`.** Nothing infers differently, nothing is newly reserved.

## The keyword split **[decided]**

`Innate Domain:` becomes the scope declaration. Library import moves to
**`Inherited Technique:`** (D1). The theme carries it: an
Innate Domain is the sorcerer's own inner world, which is what a scope is;
an inherited technique is one someone else worked out and you were handed,
which is what a library is.

```domain
Innate Domain: Game Dev            # the scope — at most one, hoisted
Inherited Technique: aoc           # a library — any number, hoisted
Inherited Technique: grids/hex
```

`prims/imports.go` is kept **whole**. `ast.Import`, `importLoader`,
`resolvePath`, `SearchPath`, `FileOptions`, `DefSite`, the cycle and diamond
handling, the reserved-name attribution — none of it changes. Only the
keyword that produces an `ast.Import` changes, and the wording of the errors
that name the keyword.

Migration is one example file (`examples/18_innate_domain.domain`) and four
doc sites. The important migration is the *error*: a program saying
`Innate Domain: aoc` where `aoc` is not a registered scope must say so and
name the fix, and `domain expansion: fix` must apply it (task 1.7).

## Where the scope lives after resolution

`ir.Pipeline` gains one field:

```go
// Scope is the name of the Innate Domain the program declared, or the
// default. It rides on the pipeline for the same reason Globals does:
// resolving and running are not paired, and a run must be hosted by the
// scope of the program it is actually running.
Scope string
```

That single string is how the scope reaches the optimizer (which ignores
it), the interpreter host, the codegen host, and every CLI command that has
to decide whether it can do its job for this program.

## Part roles

A `Part` is already a passthrough that owns a separately-resolved node list
in `Meta["nodes"]`, already snapshots and restores globals around its body,
and is already reached by the optimizer's `nodeLists`. It was built for
"several pipelines branching from one shared state". A scope that wants
several *kinds* of pipeline — a world definition, an event handler, a
renderer — needs one thing added to it: a way to say which kind each Part
is, and what its contract is.

So a `Part` gains an optional **role**, and the scope owns the role table.

```go
// PartRole is one kind of Part a scope permits.
type PartRole struct {
	Name  string   // "World", "Draw", "" for the unroled default
	Arg   ArgRule  // ArgNone | ArgLabel | ArgLabelOptional | ArgInt
	Min, Max int   // cardinality; Max -1 is unbounded

	// In and Out are the body's type contract, as functions of the world
	// type, because most roles are stated relative to it. A nil Out means
	// "whatever the body produces"; a nil In means "no upstream value".
	In  func(world *ir.Type) *ir.Type
	Out func(world *ir.Type) *ir.Type

	// DefinesWorld marks the role whose body's output type *is* the world
	// type. At most one role may set it. A scope that sets it gets
	// two-pass resolution: that Part is hoisted and resolved first, exactly
	// as Shikigami definitions and imports are hoisted today, so every
	// other Part can be typed against its result.
	DefinesWorld bool

	// Globals is what this role may do to the `Cursed Object` table. See
	// "Global state, safely" — a bool was not enough, because the useful
	// answer for a game is neither "isolated" nor "read/write".
	Globals GlobalPolicy

	// Binds are the values the host puts in scope for the body — `key` for
	// an On, `reply` for a Reply. They are *named* bindings, on the
	// `Consider` mechanism (prims/locals.go), NOT ambient lambda
	// parameters. See finding F1: ambient params are positional and change
	// every lambda's arity, which would make a Shikigami uncallable from
	// two Parts with different payloads.
	Binds func(arg *ast.PartArg) ([]RoleBind, error)

	Doc string
}
```

`prims/part.go`'s `resolvePart` gains a role lookup against
`r.scope.PartRoles`, the contract check, and role-keyed duplicate detection
(today's `r.parts[label]` becomes `r.parts[role+"\x00"+label]`).

A role whose `Globals` policy is not `GlobalsIsolated` skips the
`SnapshotGlobals`/`RestoreGlobals` pair, and `GlobalsReadOnly` additionally
seals writes. `resolveSequence`'s `scopeTop`-only rule for Parts is
unchanged: a Part is still a top-level construct in every scope.

One correction the review turned up, recorded here because it is the kind of
decision that gets quietly undone: **a role's payload values are named
bindings, not ambient lambda parameters.** `prims/ambient.go` — the
mechanism behind the `Simple Domain: For` loop variable — binds
positionally, and `requireLambda` (`prims/higher_order.go:85`) demands an
exact arity match of `arity + ambientDepth()`. A role that pushed an ambient
would therefore force every `Using:` lambda in its body, *including those
inside any Shikigami inlined there*, to declare a trailing parameter it may
never read — so one Shikigami could not be called from two roles with
different payloads. The `Consider` runtime-binding path (`prims/locals.go`)
has the property that matters instead: a name seeded into the environment
every lambda is given, where "the lambda that reads one is left exactly as
written."

`resolvePart` also has to stop requiring a non-nil upstream type
(`prims/part.go:33`). A scope with no top-level pipeline resolves every Part
with `cur == nil`, and the role's `In` supplies the body's input type. The
`Advent of Code` role keeps today's behaviour by declaring
`In: func(*ir.Type) *ir.Type { return cur }` — inherit the upstream value —
so nothing about a `Part "1":` changes.

```go
// GlobalPolicy is a role's access to the `Cursed Object` table.
type GlobalPolicy int

const (
	// GlobalsIsolated snapshots on entry and restores on exit: today's
	// Part, and the guarantee docs/language.md makes about sibling Parts.
	GlobalsIsolated GlobalPolicy = iota
	// GlobalsReadWrite reads and writes, and the writes persist.
	GlobalsReadWrite
	// GlobalsReadOnly reads; a `Cursed Tool` write is a resolve error.
	GlobalsReadOnly
)
```

The `Advent of Code` scope registers exactly one role:

```go
{Name: "", Arg: ArgLabel, Min: 0, Max: -1, Globals: GlobalsIsolated}
```

which is today's behaviour, stated.

## Migration

`Innate Domain: aoc` in an existing program becomes "unknown scope". Two
sites in this repository use the form (`examples/18_innate_domain.domain`
and four passages in `docs/language.md`), so the mechanical migration is
minutes. The migration that matters is the **error**: a target that is not a
registered scope but *does* resolve as a file on the search path must say

> `aoc` is a library, not an Innate Domain — did you mean
> `Inherited Technique: aoc`?

and `domain expansion: fix` must apply it. A user who wrote the old form
should be told the new one, not told they are wrong.

## What this buys, stated as a test

The mechanism is correct exactly when both of these hold:

1. `RegistryFor(DefaultScope)` is identical, element for element and in
   order, to `Core` — so nothing infers differently and nothing is newly
   reserved in an existing program.
2. The whole existing suite passes **unmodified**. Not adjusted, not
   re-recorded: unmodified. Any test that has to change is evidence that
   something in this document is wrong.

## Open questions

None. D1 — the import keyword, `Inherited Technique:` — was settled on
2026-08-21. The `Part` role *mechanism* is specified here; the role *table*
is a property of each scope, and the first non-default one is specified in
[the Game Dev scope design](2026-08-21-game-dev-scope-design.md).

## Implementation

[Innate Domain scopes — implementation plan](../plans/2026-08-21-innate-domain-scopes.md).
