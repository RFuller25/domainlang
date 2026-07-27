# v0.4 — ergonomics: authoring, structure, and observability — design

Seven features in one release. Three change the language (`Part` blocks,
`Innate Domain` imports, Shikigami signatures); four change the tooling
(`domain fmt`, `domain expansion: visualize`, `--stats`, LSP inlay hints).
They are specified together because they share three seams: the parser's
block forms, `prims.Resolve`'s signature, and a new execution trace hook on
`ir.Context`.

No new primitives ship here — `prims.Catalog` and `docs/primitives.md` are
untouched. That is deliberate: this release is about the parts of writing
Domain that aren't the vocabulary.

## Non-goals for the release

- No new expression-layer builtins (`mod`, `let`, text functions and the
  Map/Set escape hatches are a separate proposal).
- No type variables. Declared Shikigami signatures are monomorphic; a
  polymorphic Shikigami simply declares nothing (§3.4).
- No `--stats` for compiled binaries — instrumenting generated Go is its own
  project (§5.1).
- No new syntax for composite values. Shikigami parameters stay things you
  can *write* at a call site (§3.2).

## Cross-cutting constraints

Every feature below inherits the repo's existing discipline:

1. **The interpreter is the oracle.** Anything that changes what a program
   prints must produce byte-identical output under `domain run` and
   `domain build` (`codegen`'s oracle tests), in both optimizer modes.
2. **Optimizer safety rule 4 applies to every new sub-pipeline.** Passes
   that rewrite a node in place fire inside `Part` bodies; passes that
   change a node list's length run only at the top level, because nested
   lists are captured by their parents' `Eval` closures
   (`docs/optimizer.md`).
3. **`ast.Keywords` is pinned** by a test in `prims` to the keywords the
   registry uses. `Channel` and `Shikigami` are already in that list as
   *structural* keywords with no primitive behind them; `Part` and
   `Innate Domain` join them, and the pinning test grows an explicit
   structural set rather than being loosened.
4. **Docs are part of the change.** `language.md`, `cli.md`, `tooling.md`,
   `diagnostics.md`, `compiler.md`, the README keyword table, and the
   embedded docs site all move with the code.

---

## 1. `Part` blocks

### 1.1 Problem

Every Advent of Code day has two answers over one parse. Today the shared
parse must be duplicated across two programs, or `Channel`s — an input-side
construct — get pressed into service as an output-side one. There is also no
way to label output, so two `Reveal`s produce two bare values.

### 1.2 Grammar and AST

```domain
Part "1":
    Maximum Technique: Select Top 1, Sum
    Reveal: stdout

Part "2":
    Maximum Technique: Select Top 3, Sum
    Reveal: stdout
```

A direct analogue of `Channel "name":`. `parseStatement` gains a special
form beside the existing channel case (`parser/parser.go:275`), and
`parsePart` mirrors `parseChannel` (`parser/parser.go:315`) exactly: name
token, colon, indented sub-pipeline.

`ast.Statement` grows one field beside `ChannelName`:

```go
PartName string // for `Part "1":` statements; "" otherwise
```

The label is free text, not just a number — `Part "totals":` is legal.

### 1.3 Semantics

A `Part` **branches from the current value and is a passthrough**, exactly
like a Channel: `In == Out == cur`, and sibling Parts all see the same
upstream value. That is the whole point — the parse above them happens once.

**Output is explicit.** A Part prints only what its body `Reveal:`s, and the
label prefixes that output. `Reveal` stays the single output sink; no
implicit-print rule is introduced, so the linter's "never Reveals" check
stays honest.

The label format depends on whether the rendered value is single-line:

```
Part 1: 24000                 # single-line value: label, colon, space, value
Part picture:                 # multi-line value: label line, then the value
###..
..###
```

This keeps grids and sparse pictures readable and is the only formatting
rule the feature adds. A Part that reveals twice prefixes both lines.

### 1.4 Resolution

`resolveSequence` gains a `case stmt.Keyword == "Part"`. Part bodies need a
scope that today's boolean `allowChannels` cannot express: **`From:`
consumers must be allowed** (so each Part can combine channels parsed once
above it) while **Channel definitions must not be** (Channels cannot nest).

The boolean therefore becomes a three-valued scope, threaded through the
four `resolveSequence` call sites (`prims.go:241`, `channel.go:38`,
`control.go:119`, `shikigami.go:41`):

```go
type scope int
const (
    scopeTop    scope = iota // Channel definitions + From: consumers
    scopePart                // From: consumers only
    scopeNested              // neither (channel, loop, Shikigami bodies)
)
```

Parts themselves are top-level only; a `Part` inside a Channel, loop,
Shikigami, or another Part is a positioned error.

The IR node mirrors `resolveChannel`'s: `Prim: "Part"`, passthrough
`In`/`Out`, `Meta{"label", "nodes"}`, and an `Eval` that runs the sub-nodes
on the incoming value, then returns the incoming value unchanged.

### 1.5 Threading the label

`Emit` writes to `ctx.Stdout`, so the label has to reach it. A prefixing
`io.Writer` is the wrong tool — it cannot make the single-line/multi-line
decision, which depends on the whole rendered value. Instead `ir.Context`
gains a field, following the precedent `Release` already set for state that
must reach closure-captured nested nodes:

```go
PartLabel string // set by a Part node around its body; "" at top level
```

The Part node sets it, runs the body, and restores the previous value
(restoring, not clearing, so nesting stays correct if Parts are ever
allowed to nest). `Emit` consults it and applies §1.3's format. Codegen
mirrors this with a generated `dmPartLabel` string variable and the same
branch in `emitEmit`, so the two backends cannot drift.

### 1.6 Codegen

`case "Part"` in the `emitNode` switch (`codegen/codegen.go:352`), modelled
on `emitChannel`: emit the body inline in a block, with the label variable
set and restored around it. Nothing else changes, because a Part is a
passthrough.

### 1.7 Linter and diagnostics

`diag/lint.go` needs four adjustments:

- **"pipeline never Reveals"** is satisfied if *any* Part reveals.
- **New warning:** a Part whose body never `Reveal`s produces no output —
  the one real hazard of the explicit-Reveal choice, so the linter covers
  it directly.
- **"statements after the last Reveal"** becomes per-scope: statements after
  a Reveal *inside a Part* are dead within that Part; top-level statements
  after a Part are not dead, because Parts are passthroughs.
- **New warning:** two Parts with the same label (the output becomes
  ambiguous).

The type-mismatch bridge suggestions already work per-statement and need no
change.

### 1.8 REPL

`Reveal` output is suppressed during REPL replays (`docs/tooling.md`), so
Part output is suppressed too; a Part statement evaluates as the passthrough
it is and the REPL keeps printing the current value. No REPL-specific work.

### 1.9 Testing

- Parser: name/colon/body, dedent handling, error when a Part has an empty
  body or appears nested.
- Resolve: `scopePart` allows `From:` consumers and rejects Channel
  definitions; Parts reject nesting; passthrough typing.
- Interpreter: label formats (single-line, multi-line, two Reveals, no
  Reveal), Part ordering, sibling Parts seeing the same upstream value.
- Oracle: a new `examples/17_two_parts.domain` with `.input`/`.expected`
  (auto-discovered by `examples_test.go`), compiled and interpreted in both
  optimizer modes with byte-identical output.
- Lint: each of the four adjustments above.

---

## 2. `Innate Domain` — imports

### 2.1 Problem

Shikigami are per-file and the prelude is embedded in the binary. There is
no way to keep a personal helper library across programs.

### 2.2 Grammar

```domain
Innate Domain: aoc
Innate Domain: grids/hex
```

A bare target with no extension and no quotes, matching `Cursed Energy:`'s
bare-path style; `aoc` resolves to `aoc.domain`. `Innate Domain` joins
`ast.Keywords` as a structural keyword (§cross-cutting 3).

**Imports are hoisted**, like Shikigami definitions: they are collected
before resolution and their position in the file does not matter. They live
in a new `Program` field rather than in the statement list, which keeps them
out of `resolveSequence` entirely:

```go
type Program struct {
    Statements []*Statement
    Shikigamis []*ShikigamiDef
    Imports    []*Import      // new
}

type Import struct {
    Target string         // as written, e.g. "aoc" or "grids/hex"
    Pos    token.Position
}
```

Because the keyword is optional everywhere else in the language, prefix
inference (`prims/infer.go`) must learn that a bare phrase cannot become an
import: a line reading just `aoc` is a source or an unknown operation, never
an import. `Innate Domain` is one of the few keywords that is **required**,
alongside the `Reveal stdout` colon rule already documented in
`language.md`.

### 2.3 What a library file may contain

**Shikigami definitions only.** An imported file with top-level pipeline
statements is a positioned error naming the offending line:

```
error[resolve]: imported file "aoc.domain" must contain only Shikigami
                definitions (found a pipeline statement)
  --> aoc.domain:12:1
```

This keeps the feature a library mechanism rather than a textual include.
Libraries may themselves import (§2.5).

### 2.4 Search path

In order, first hit wins:

1. the importing file's own directory (so a library next to a program just
   works, and a program's imports are relocatable with it);
2. each colon-separated entry of `$DOMAIN_PATH`;
3. `~/.config/domain/lib`.

A miss lists every directory searched. This is the release's one policy
knob; it is documented in `cli.md` beside the `Cursed Energy:` resolution
rules, which it deliberately parallels.

### 2.5 Cycles, diamonds, and shadowing

The import graph is walked depth-first and deduped by **absolute resolved
path**, so a diamond loads once and a cycle is a positioned error naming
the chain (`aoc.domain → grids.domain → aoc.domain`).

Precedence, weakest to strongest:

```
prelude  <  imports (in import order)  <  the importing file's own definitions
```

A local definition shadowing an import is silent — it matches how a local
definition already shadows the prelude. One import shadowing another is a
**lint warning**, not an error, naming both files. The reserved-name rule
(a Shikigami may not be named after a built-in) applies to imported
definitions and is reported at the definition's real position in the library
file.

### 2.6 Error positions across files

`token.Position` carries only line/col/offset — no file. `wrapShikigamiErr`
already works around this for the prelude by labelling those positions
`prelude source L:C` via a `preludeNames` set (`shikigami.go:55`). That set
generalizes to an origin map:

```go
origin map[string]string // Shikigami name → "" (local), "prelude", or a file path
```

and the same function labels errors `in Shikigami "Hex Ring" (aoc.domain:8:5):
…`. No change to `token.Position`, and the prelude's existing message
wording is preserved.

### 2.7 The `Resolve` API

`prims.Resolve(prog)` has no file context, so it cannot read imports. It
gains an options-taking sibling:

```go
type ResolveOptions struct {
    BaseDir  string                              // the importing file's directory
    Search   []string                            // $DOMAIN_PATH + ~/.config/domain/lib
    ReadFile func(path string) ([]byte, error)    // nil ⇒ os.ReadFile
}

func ResolveWith(prog *ast.Program, opts ResolveOptions) (*ir.Pipeline, error)
func Resolve(prog *ast.Program) (*ir.Pipeline, error) // = ResolveWith(prog, ResolveOptions{})
```

`Resolve` with no options rejects any `Innate Domain` with a clear
positioned error ("imports need a file context") rather than silently
ignoring it. The five call sites all have a path in hand and move to
`ResolveWith`: `cmd/domain/main.go:305`, `cmd/domain/repl.go:188`,
`lsp/lsp.go:503`, `diag/analyze.go:107`, `diag/optimize.go:67`. The
injectable `ReadFile` is what lets the LSP resolve against unsaved editor
buffers and lets tests avoid touching disk.

### 2.8 Compiler and tooling

Imports are inlined before codegen, so `domain build` needs library files at
**build** time and never at run time — the compiled binary is still
self-contained. This is a documented delta in `compiler.md` beside the
existing input-path one.

Two tooling wins fall out and should ship with the feature:

- **`textDocument/definition` crosses files** — the origin map plus the
  resolved absolute path gives the LSP a real URI for an imported
  Shikigami, where prelude names correctly return nothing.
- **Lint: unused import** — an `Innate Domain` none of whose definitions
  are summoned.

### 2.9 Testing

- Resolution: relative hit, `$DOMAIN_PATH` hit, `~/.config/domain/lib` hit,
  miss listing all directories, nested subdirectory targets.
- Graph: diamond loads once, cycle reports the chain, transitive import
  three deep.
- Shadowing: all four precedence pairs; reserved-name rejection inside a
  library; the shadowing lint warning.
- Errors: a library with a pipeline statement; a resolve error inside an
  imported body carrying the `(aoc.domain:L:C)` label.
- Oracle: an `examples/18_innate_domain.domain` importing
  `examples/lib/shapes.domain`. The library goes in a **subdirectory** —
  `examples_test.go` reads the directory non-recursively and would otherwise
  try to run the library as a program.

---

## 3. Shikigami signatures and richer parameters

### 3.1 Problem

Parameters are `Int` or `Text` only, and there is no way to state a
Shikigami's type. Two consequences: a Shikigami cannot abstract over a
lambda (so "my own filtered count" must be written out at every call site),
and a type error inside a body surfaces at the call with an inlining trace
instead of at the boundary that is actually wrong.

### 3.2 Parameters

Four scalar types plus lambdas:

```domain
Shikigami "Count Where" (p: (Int) -> Bool) : List<Int> -> Int
    Maximum Technique: Count Matching
        Using: p

Count Where
    p: (x) -> x > 100
```

**Scalars — `Int`, `Text`, `Float`, `Bool`.** These extend the existing
literal-substitution machinery (`paramVal`, `substituteOp`, `substExpr`)
with two new cases each. `ast.FloatArg` already exists. Bool needs a call
site spelling: `on: true` parses today as `IdentArg{"true"}`, and
`bindParams` interprets exactly the identifiers `true`/`false` for a `Bool`
parameter — no lexer change, and no commitment about expression-layer bool
literals, which are a separate proposal. `ast.BoolLit` already exists (the
optimizer's constant folder produces it), so substitution has a target;
its doc comment saying the parser never produces one is updated.

**Lambdas.** A lambda parameter's declared type is `(T, …) -> U`. At the
call site the argument is an ordinary `LambdaArg`. Substitution is a new
case in `substituteArg`: an argument whose value is `IdentArg{name}` where
`name` is a lambda parameter is replaced wholesale by the bound
`LambdaArg`. Note the shape this takes — `Using: p`, an argument *naming* a
parameter, rather than a lambda body mentioning it.

Checking happens at the call site, before substitution: the passed lambda is
typed against the declared parameter types with `typecheck.LambdaType`, and
a mismatch in arity or result type is reported against the call. Because
substitution is textual, a lambda parameter used in two places is typed
independently at each, which is a feature, not a leak — but it means a
lambda parameter's declared type is checked once per *use*, not once per
call, and the spec's error wording must name the use site.

### 3.3 A type grammar

Declared signatures and lambda parameter types both need real types, which
the parser has never had — `ast.Param.Type` is a bare `string` today.

Types are parsed into a **syntactic** tree in `ast`, and lowered to
`*ir.Type` in `prims`, mirroring how the existing string is lowered there.
This keeps the parser free of a dependency on the type model.

```go
// ast
type TypeExpr struct {
    Name   string      // "Int", "List", "Map", "Grid", "Sparse", "Tuple", …
    Args   []*TypeExpr // List<T>, Map<K,V>
    Fields []TypeField // record types {a:Int, b:Int}
    Lambda *LambdaType // (Int, Int) -> Bool
    Pos    token.Position
}
```

The grammar covers what the type model has: scalars, `List<T>`, `Set<T>`,
`Grid<T>`, `Sparse<T>`, `Map<K,V>`, `(A, B)` tuples, `{a:Int}` records, and
`(A) -> B` lambdas. `prims.lowerTypeExpr` produces the `*ir.Type` and
reports unknown names, wrong argument counts, and non-keyable Map keys or
Set elements with positions — reusing `ir.Keyable`, so the keyability rules
cannot drift from the ones primitives already enforce.

`ast.Param.Type` becomes `*TypeExpr`. The parser's `parseParamsOpt`
(`parser/parser.go:214`) calls the new type parser instead of
`expect(token.IDENT)`.

### 3.4 Declared signatures

```domain
Shikigami "Top K Sum" (k: Int) : List<Int> -> Int
```

Optional, and when present it is a **check, not a compilation boundary**:

- the declared **input** is checked against the pipeline's current type at
  each call site, which is the diagnostic win — `Shikigami "Top K Sum"
  expects List<Int>, the pipeline carries List<Text>` reported *at the
  call*, with the existing single-step bridge suggestion
  (`Convert To Integers`) attached, instead of an inlining trace from
  inside the body;
- the declared **output** is checked against the body's resolved output, so
  a body that drifts from its stated type fails at the definition;
- **inlining is unchanged.** The body is still substituted and resolved
  inline, so optimizer rewrites still fire straight through a Shikigami.
  That property is load-bearing for the language's whole thesis and no part
  of this feature may weaken it.

Both sides are required when a signature is written; there is no
output-only form and no `Any`. Since there are no type variables, a
polymorphic Shikigami (`List<T> -> T`) simply declares nothing and behaves
exactly as today — the annotation is opt-in precisely so this stays
possible.

**Dogfooding:** all five prelude Shikigami are monomorphic, so all five get
signatures. That exercises the parser on `Text -> List<Text>`,
`Text -> List<List<Text>>`, `Text -> Grid<Int>`, and `List<Int> -> Int`, and
makes LSP hover show a real declared type instead of a reconstructed one.

### 3.5 Testing

- Type parser: every form above, nesting, and each error (unknown name,
  wrong arity, unkeyable Map key).
- Params: Float and Bool substitution into both a phrase and a lambda body;
  `true`/`false` at a call site; the existing `dispatchSurvivesRemoval`
  guard still holding for the new types.
- Lambda params: substitution into `Using:`, arity mismatch, result-type
  mismatch, one parameter used at two sites with different element types.
- Signatures: input mismatch reported at the call with the bridge
  suggestion; output mismatch reported at the definition; a signature-free
  Shikigami still polymorphic; the prelude's five signatures accepted.
- Optimizer: an existing test already asserts `Top K Sum` fuses into a
  quickselect — it must still pass unchanged with the signature added,
  which is the regression guard for §3.4's inlining promise.
- Oracle: a compiled program calling a Shikigami with a lambda parameter.

---

## 4. The trace hook (shared by §5 and §6)

`interp.Run` is a flat loop over `p.Nodes`, and every nested construct —
loop bodies, Channel bodies, and now Part bodies — is captured inside its
parent's `Eval` closure, out of reach of the node list. Both the visualizer
and `--stats` need to see those nested steps, so they share one mechanism
built once.

`ir.Context` gains a hook, following `Release`'s precedent for exactly this
reason:

```go
type StepEvent struct {
    Node  *Node
    Depth int           // 0 at top level
    Frame string        // enclosing frame, e.g. `Repeat 4 iter 2/4`, `Channel "moves"`
    In    Value
    Out   Value
    Err   error
    Dur   time.Duration
}

type Tracer interface {
    Step(StepEvent)
    PushFrame(label string)
    PopFrame()
}

// on Context:
Trace Tracer // nil ⇒ no tracing, zero overhead
```

There are exactly **four** places nodes are evaluated, and instrumenting
them covers the language:

| Site | Covers |
|---|---|
| `interp.Run` (`interp/interp.go:26`) | top-level statements |
| `prims.runBody` (`control.go:163`) | all three loop kinds |
| `resolveChannel`'s `Eval` (`channel.go:51`) | Channel bodies |
| the new `Part` `Eval` (§1.4) | Part bodies |

`runBody` being already shared by `Repeat`/`While`/`Fixed Point` is what
makes this cheap. The three loop nodes wrap their `runBody` calls in
`PushFrame`/`PopFrame` with the iteration label, which is what gives the
visualizer its `iter 2/4` frames and `--stats` its per-iteration counts.

A `nil` `Trace` is a single nil check per node — the compiled backend never
sees any of this, and `domain run` without a tracing flag is unaffected.

**Capture bounds.** Values are unbounded in size and loops in count, so the
tracer interface is the wrong place to store anything: each consumer
decides. §5's aggregator stores no values at all; §6's recorder stores
`ir.FormatShort` always, a `FormatValue` truncated at 64 KiB for the
selected step, and caps total captured steps (default 10,000, `--max-steps`),
reporting the cap in the UI rather than silently truncating.

---

## 5. `--stats`

### 5.1 Scope

`domain run --stats` only. The compiled backend would need generated
instrumentation, a different measurement story, and its own oracle tests;
it is explicitly out of scope and the flag table in `cli.md` says so.

### 5.2 Behavior

A lightweight `Tracer` that keeps counts and durations but **no values**,
printing to stderr after the program's output — the same stream and ordering
convention `--explain` uses, so the two compose:

```
[stats] interpreter, 14 stages, 1.2M input bytes, 41.3ms total
  #  stage                          out type              size    time     %
  1  Read Source                    Text                  1.2MB   2.1ms   5.1
  2  Split Text by "\n\n"           List<Text>              2249   6.4ms  15.5
  …
  9  Repeat 4  (4 iters, 12 steps)  Sparse<Int>              318  18.7ms  45.3
```

Nested steps aggregate into their parent with iteration and step counts;
`--stats --verbose` lists them individually.

"size" needs a value-size probe, which the runtime does not have as one
function today:

```go
func ir.SizeOf(v Value) (n int, ok bool) // list/map/set length, grid cells, sparse set-cells, text bytes
```

### 5.3 Honesty

The header says `interpreter` for a reason: this measures the tree-walking
evaluator, not the compiled binary, and nobody should benchmark the
language with it. That sentence goes in `cli.md` too. Quantifying optimizer
wins by running both modes and diffing the totals is the obvious follow-up
and is deliberately **not** in this release.

### 5.4 Testing

Pure-Go tests on the aggregating tracer: totals sum to the whole, nested
frames attribute to their parent, iteration counts match the loop bounds,
`SizeOf` over every value kind, and a smoke test that `--stats` leaves
stdout byte-identical (stats go to stderr).

---

## 6. `domain expansion: visualize`

### 6.1 Command

```
domain expansion: visualize <file> [--input <file>] [--max-steps N]
```

Joins `expansionCommands` (`cmd/domain/expansion.go:27`). Bubbletea v2,
bubbles, and lipgloss v2 are already dependencies used by the REPL's
interactive editor (`cmd/domain/repl_tty.go`), so the UI stack and its
testing pattern are established.

### 6.2 The stdin problem

An interactive terminal cannot double as program stdin — the same
constraint the REPL documents. So: `visualize` requires either a file
target in `Cursed Energy:` or an explicit `--input <file>`; if stdin is
*not* a terminal it is read to completion **before** the TUI starts. A
program needing stdin with no `--input` and a terminal on stdin is a usage
error naming the fix.

### 6.3 Trace and UI

The program is resolved and optimized normally, then run **once** under
§4's recorder; the TUI navigates the recorded trace afterwards. Running to
completion first keeps the model pure and the UI responsive, and means a
program that fails mid-run is still explorable up to the failure — the
failing step is shown with its error, which makes this a debugger for real
failures and not just successful runs.

Three panes:

- **left — the pipeline tree.** Top-level stages, expandable into iteration
  frames and channel/Part bodies (`▸`/`▾`), each line showing stage number,
  `Display`, out type, and size.
- **right — the value pane.** The selected step's input and output,
  rendered with the same `FormatValue` the language prints, scrollable;
  grids and sparse planes render as pictures.
- **footer.** Position, the total step count (and whether `--max-steps`
  capped it), and the `--explain` rewrite attached to the selected node
  when there is one.

Keys: `↑`/`↓`/`j`/`k` move, `←`/`→`/`h`/`l` collapse/expand, `enter` steps
into a frame, `g`/`G` first/last, `e` toggles the explain pane, `q` quits.

Deliberately deferred: no editing, no breakpoints, no re-running from a
step, and no side-by-side optimized/naive trace comparison — the last one
is the most tempting follow-up and wants its own design.

### 6.4 Testing

Two layers, and the split matters: the trace recorder is pure Go and gets
the real coverage — one test per construct (each loop kind, channels,
Parts, Shikigami inlining, a mid-run failure, the step cap) asserting frame
labels, depths, and ordering. The bubbletea model is tested the way
`repl_tty_test.go` already tests the REPL editor: drive it with injected
key messages and assert on model state, not on rendered frames. No golden
screenshots.

---

## 7. LSP inlay hints

### 7.1 Behavior

The output type after each statement, at end of line:

```domain
Cursed Energy: input.txt                    : Text
Cursed Technique: Split Text by "\n\n"      : List<Text>
Channeled Energy: Convert Each List to…     : List<List<Int>>
Maximum Technique: Sum Each Group           : List<Int>
```

This is the REPL's `=> value : Type` feedback — the best thing about the
REPL — brought into the editor. `inlayHintProvider: true` joins the
capabilities at `lsp/lsp.go:149`, with a `textDocument/inlayHint` handler
beside the existing five; no `inlayHint/resolve` is needed.

### 7.2 Getting the types right

Two traps, both about which pipeline the hints come from:

- **Hints must come from an unoptimized resolve.** The optimizer replaces,
  fuses, and deletes nodes, so an optimized node list cannot be mapped back
  to source lines. The LSP already resolves for diagnostics (`lsp.go:503`)
  and does not optimize — hints reuse that pipeline and key on `Node.Pos`.
- **One statement can produce many nodes.** A Shikigami call inlines its
  whole body, so several nodes share the call's position. The hint for a
  position is the `Out` of the **last** node at that position, which is
  exactly the call's result type.

Per-construct rules: a `Channel` shows its *body's* result type (the type
consumers will see — more useful than the passthrough), a `Part` and a
`Binding Vow` show nothing, and a loop shows its preserved type.

**Partial programs still get hints.** Resolution stops at the first error,
so hints exist for every line before it — the incremental feel of the REPL,
in a file that does not yet typecheck.

### 7.3 Cross-feature note

Completion and hover must learn the release's new keywords (`Part`,
`Innate Domain`) and the new Shikigami signature form, which is why this
feature lands last.

### 7.4 Testing

`lsp_test.go` gains: hint positions and text for a linear pipeline, a
Shikigami call (one hint, the call's result type), Channel/Part/Vow rules,
a program with an error mid-file (hints up to it), and an unresolvable
first line (no hints, no crash).

---

## 8. Order of implementation

Dependency-driven, with the wide-blast-radius refactors early so they don't
collide with feature work:

1. **`domain fmt`** — fully isolated, and it makes every later diff cleaner.
2. **`Part`** — parser + the `scope` refactor of `resolveSequence`.
3. **`Innate Domain`** — the `ResolveWith` API change across five call sites.
4. **Shikigami signatures** — the type grammar; independent of 2 and 3.
5. **The trace hook + `--stats`** — the hook built once, with the smaller,
   value-free consumer proving it. Must come after `Part` so the Part
   `Eval` site is instrumented with the rest.
6. **`visualize`** — the second consumer of the hook.
7. **Inlay hints** — last, so it knows about every new keyword and form.

Each step is a separate implementation plan under
`docs/superpowers/plans/`, and each ships with its own docs updates rather
than deferring them to the end.

---

## 9. `domain fmt`

Specified last because it is implemented first and depends on nothing.

### 9.1 The comment problem

The lexer **discards comments** — there is no `COMMENT` token — so a
formatter that printed the AST back out would delete every comment in the
file. Every example and challenge program is heavily commented, so that is
disqualifying.

Therefore `fmt` is **line-oriented**: it normalizes each source line
textually, using the parsed AST only to learn each statement's nesting
depth. Comments, including trailing ones, survive by construction. The
tradeoff is that long lambdas are never re-wrapped — which matches gofmt,
which also does not wrap.

### 9.2 Policy: keywords are preserved as written

A line may be written `Cursed Technique: Split Text by "x"` or bare
`Split Text by "x"`, and mixing the two is an intentional language feature.
`fmt` therefore **never adds or removes a keyword** (`KeywordInferred` makes
the distinction available, and `fmt` deliberately ignores it). No existing
file changes style, and the formatter stays purely mechanical.

What it does normalize: indentation to four spaces per level; exactly one
space after a keyword's colon and after an argument label's colon; single
spaces between operation words; `,` before a modifier (`Select Top 3, Sum`);
spacing around `->` and around expression operators; runs of blank lines
collapsed to one; trailing whitespace stripped; a final newline. It does
**not** reorder named arguments.

### 9.3 Interface

Following gofmt: `domain fmt <file>…` prints to stdout, `-w` writes in
place, `--check` exits 1 and lists unformatted files (CI), `-` reads stdin.
`expansion: fix` already rewrites tabs to four spaces; `fmt` subsumes that
mechanically, and the two must agree — `fix`'s output is required to be
`fmt`-clean, pinned by a test.

### 9.4 Testing

- Unit: each normalization, and comment preservation in every position
  (own line at each depth, trailing, comment-only file).
- **Idempotence:** `fmt(fmt(x)) == fmt(x)`, property-tested.
- **Semantic preservation, the important one:** every program in
  `examples/`, `challenges/`, and `testdata/` is formatted, then required to
  still resolve *and* produce byte-identical output. Cheap to write, and it
  is what makes the formatter trustworthy on a whitespace-significant
  language.
- Every one of those files is already `fmt`-clean, or the formatter is
  wrong — a test asserts `--check` passes on the whole repo.
