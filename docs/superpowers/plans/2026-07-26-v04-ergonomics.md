# v0.4 ergonomics — implementation plan

> **Status: all seven shipped.** What the implementation learned, where it
> diverged from the design, and the bugs it turned up are recorded in
> [§Outcome](#outcome) at the end.


Implements [2026-07-26-v04-ergonomics-design.md](../specs/2026-07-26-v04-ergonomics-design.md).
Seven features, in dependency order. Each numbered task is one commit.

## Global constraints

- `go test ./...` green after every task. Baseline is ~46s (codegen 32s,
  cmd/domain 14s).
- Docs ship with the code, not after: `language.md`, `cli.md`, `tooling.md`,
  `diagnostics.md`, `compiler.md`, the README keyword table.
- Anything that changes program output needs an interpreter-vs-binary oracle
  test in both optimizer modes.
- New keywords go in `ast.Keywords` *and* the structural set in the `prims`
  pinning test.
- No feature may make an untraced `domain run` slower. The trace hook is a
  nil check; `--stats`/`visualize` are opt-in.

---

## A. `domain fmt` (feature 6)

Isolated; nothing depends on it, and it makes later diffs cleaner.

**A1. The formatter core.** New package `fmtdomain/` (name avoids shadowing
`fmt`), with:

```go
func Format(src string) (string, error)  // idempotent, comment-preserving
```

Line-oriented per spec §9.1: lex+parse to learn each statement's nesting
depth, then rewrite each physical line's leading whitespace to
`4 × depth` and normalize its interior spacing textually. A parse error
returns the source unchanged plus the error — `fmt` never mangles a broken
file.

Normalizations (spec §9.2): 4-space indent per level; one space after a
keyword colon and after an argument label colon; single spaces between
operation words; `, ` before a modifier; spaces around `->` and expression
operators; blank-line runs collapsed to one; trailing whitespace stripped;
final newline. Never adds/removes keywords, never reorders arguments.

Interior normalization must not touch string literals — a separator like
`Split Text by "  "` keeps its spaces. The line rewriter walks the line
tracking whether it is inside a double-quoted string (honoring `\"`), and
also stops at an unquoted `#` so trailing comments are left verbatim.

**A2. The CLI.** `domain fmt <file>… | -` with `-w`, `--check`, `-l`.
gofmt conventions: default writes to stdout, `-w` in place, `--check` exits
1 listing unformatted files, `-` reads stdin. Add to `main.go`'s switch and
the help text; `cli.md` gains a section.

**A3. Tests.**
- Unit: each normalization; comment preservation (own line at each depth,
  trailing, comment-only file, comment inside an indented block); strings
  with significant spaces; a `#` inside a string literal.
- Idempotence: `Format(Format(x)) == Format(x)` over every repo program.
- **Semantic preservation:** every `examples/`, `challenges/`, `testdata/`
  program formats, still resolves, and produces byte-identical output.
- `--check` passes on the whole repo (so the repo is `fmt`-clean, or the
  formatter is wrong).
- `expansion: fix` output is `fmt`-clean.

---

## B. `Part` blocks (feature 1)

**B1. Parser + AST.** `ast.Statement.PartName`; `"Part"` in `ast.Keywords`;
`parsePart` mirroring `parseChannel` (`parser.go:315`); the `parseStatement`
special form beside the channel one (`parser.go:275`); `"Part"` in
`blockKeywords` (`parser.go:58`) so a parameter named `Part` can't confuse
argument parsing. Tests: name/colon/body, empty body, missing indent.

**B2. The `scope` refactor.** Replace `resolveSequence`'s `allowChannels
bool` with the three-valued `scope` (spec §1.4) across its four call sites.
Pure refactor, no behavior change — land it alone so the diff is reviewable.

**B3. Resolution + interpretation.** `case stmt.Keyword == "Part"` →
`resolvePart`, modelled on `resolveChannel`: passthrough node, `Prim:
"Part"`, `Meta{"label", "nodes"}`. Using the `"nodes"` key is what gets
Part bodies into `optimizer.nodeLists` for free (verified by B6). Reject
Parts anywhere but top level. `ir.Context.PartLabel` + the `Emit` format
from spec §1.3.

**B4. Codegen.** `case "Part"` in `emitNode` (`codegen.go:206`). **The
compiled backend specializes the label**: a Part's label is a literal known
at compile time, so `emitEmit` emits the prefix as part of its existing
print call — no `dmPartLabel` variable, no runtime branch. The interpreter
needs the Context field because `Emit`'s closure is shared; the compiler
does not. Oracle test pins the two identical.

**B5. Lint.** The four `diag/lint.go` adjustments from spec §1.7.

**B6. Optimizer.** No pass changes expected — `nodeLists` recurses on
`Meta["nodes"]` generically. Add a test proving an in-place pass (expression
simplification) fires inside a Part body, and that a length-changing pass
does not (rule 4). If the free recursion does *not* hold, that's a bug to
fix here, not in a later feature.

**B7. Docs + example.** `language.md` section, README keyword table row,
`compiler.md` note, `examples/17_two_parts.domain` + `.input`/`.expected`
(auto-discovered), oracle test in both optimizer modes.

---

## C. `Innate Domain` imports (feature 2)

**C1. Parser + AST.** `ast.Import`, `Program.Imports`, `"Innate Domain"` in
`ast.Keywords`; hoisted like Shikigami defs in `parseProgram`
(`parser.go:104`). `prims/infer.go`: a bare phrase never infers to an import
(the keyword is required).

**C2. `ResolveWith`.** `ResolveOptions{BaseDir, Search, ReadFile}` +
`ResolveWith`; `Resolve` delegates with zero options and rejects imports
with a positioned "imports need a file context" error. Update the five call
sites (`main.go:305`, `repl.go:188`, `lsp.go:503`, `diag/analyze.go:107`,
`diag/optimize.go:67`).

**C3. The loader.** Search path (spec §2.4); dedupe by absolute resolved
path; cycle detection reporting the chain; library files may contain only
Shikigami definitions; transitive imports. Parse each library **once per
resolve** — cached by absolute path in the resolver, not process-globally,
because the LSP re-resolves edited buffers.

**C4. Origins + shadowing.** Generalize `preludeNames` to
`origin map[string]string` (spec §2.6); `wrapShikigamiErr` labels
`(aoc.domain:L:C)`; precedence prelude < imports < local; reserved-name rule
applies to imported defs at their real position.

**C5. Lint + LSP.** Unused-import warning; import-shadows-import warning;
`textDocument/definition` returns the library file's URI for an imported
Shikigami.

**C6. Docs + example.** `language.md`, `cli.md` (search path beside the
`Cursed Energy:` rules), `compiler.md` (libraries are build-time only),
README. `examples/18_innate_domain.domain` importing
`examples/lib/shapes.domain` — the library goes in a **subdirectory** or
`examples_test.go` will try to run it as a program.

---

## D. Shikigami signatures + parameters (feature 3)

**D1. The type grammar.** `ast.TypeExpr`/`TypeField`/`LambdaType`;
`parser.parseTypeExpr`; `ast.Param.Type` becomes `*TypeExpr`;
`parseParamsOpt` uses it. `prims.lowerTypeExpr` → `*ir.Type`, reporting
unknown names, arity errors, and non-keyable Map keys/Set elements via
`ir.Keyable`. Tests for every form and every error.

**D2. Scalar parameters.** `Float` and `Bool` through `paramVal`,
`substituteOp`, `substExpr`; `true`/`false` as `IdentArg` in `bindParams`;
`ast.BoolLit`'s "parser never produces one" comment updated. The
`dispatchSurvivesRemoval` guard must still hold for the new types.

**D3. Lambda parameters.** Declared `(T, …) -> U`; `substituteArg` replaces
an `IdentArg` naming a lambda parameter with the bound `LambdaArg`;
call-site checking against the declared type via `typecheck.LambdaType`,
with errors naming the use site.

**D4. Declared signatures.** Optional `: In -> Out`; input checked at each
call site (with the existing bridge suggestion attached); output checked
against the body at the definition. **Inlining unchanged** — the existing
`Top K Sum` → quickselect optimizer test is the regression guard.

**D5. Prelude signatures.** All five prelude definitions annotated. LSP
hover shows the declared type.

**D6. Docs.** `language.md` Shikigami section (params, signatures, the
monomorphic limitation), README bullet.

---

## E. Trace hook + `--stats` (feature 5)

**E1. The hook.** `ir.StepEvent`, `ir.Tracer`, `Context.Trace`. Instrument
the four sites (spec §4): `interp.Run`, `prims.runBody`, `resolveChannel`'s
Eval, the Part Eval. Loops push/pop iteration frames around `runBody`.
**Benchmark proving zero regression when `Trace == nil`** — a nil-check-only
path, compared against the current committed numbers in
`optimizer/bench_test.go`.

**E2. `ir.SizeOf`.** One probe over every value kind (list/tuple length, map
/set entries, grid cells, sparse set-cells, text bytes, scalars → not ok).

**E3. The aggregating tracer + flag.** `--stats` on `run` only; stderr,
after program output; the table from spec §5.2 with the `interpreter` header
caveat; `--stats --verbose` lists nested steps individually. Stores no
values.

**E4. Tests.** Totals sum to the whole; nested frames attribute to parents;
iteration counts match loop bounds; `SizeOf` per kind; stdout stays
byte-identical with `--stats` on.

---

## F. `domain expansion: visualize` (feature 4)

**F1. The recorder.** The value-capturing `Tracer`: `FormatShort` always,
`FormatValue` truncated at 64 KiB for the selected step, total step cap
(default 10,000, `--max-steps`). Pure Go, no TUI — this is where the real
test coverage goes: one test per construct (three loop kinds, channels,
Parts, Shikigami inlining, mid-run failure, cap hit) asserting frame
labels, depths, ordering.

**F2. Input handling.** Require a `Cursed Energy:` file target or
`--input <file>`; read non-terminal stdin fully before the TUI starts; a
usage error naming the fix otherwise.

**F3. The TUI.** bubbletea v2 + lipgloss v2 (already dependencies, patterns
in `repl_tty.go`). Three panes and the key map from spec §6.3. Tested by
injected key messages like `repl_tty_test.go`, not golden screenshots.

**F4. Wiring + docs.** `expansionCommands`, help text, `cli.md`,
`tooling.md` section.

---

## G. LSP inlay hints (feature 7)

Last, so it knows every new keyword and form.

**G1. Handler.** `inlayHintProvider: true`; `textDocument/inlayHint`.
Hints from the **unoptimized** resolve keyed by `Node.Pos`; for several
nodes at one position (Shikigami inlining) the **last** node's `Out` wins.
Channel shows its body's result type; Part and Vow show nothing.

**G2. Tests.** Linear pipeline; Shikigami call (one hint, call's result
type); Channel/Part/Vow rules; error mid-file (hints up to it);
unresolvable first line (no hints, no crash).

**G3. Completion/hover** learn `Part`, `Innate Domain`, and the signature
form. `tooling.md` capability table row.

---

## Optimization work, per feature

Efficiency is a deliverable, not an afterthought. Concretely:

| Feature | Runtime cost | Compile-time win available |
|---|---|---|
| `fmt` | none (offline) | — |
| `Part` | passthrough; one `Context` field write | **label specialized to a literal in codegen** (B4) — no runtime branch in the binary |
| Imports | resolve-time only; parse once per resolve | inlined pre-codegen, so imported Shikigami get every optimizer rewrite for free (test it) |
| Signatures | resolve-time only | none; the win is diagnostics |
| Trace hook | one nil check per node | compiled backend never sees it |
| `--stats` | opt-in | — |
| `visualize` | opt-in, bounded capture | — |
| Inlay hints | reuses the diagnostics resolve | — |

Candidate optimizer passes this release *enables* but does not implement
(record in `docs/optimizer.md` under future passes):

- **Common prefix hoisting across Parts.** Two Parts starting with the same
  operations could share one computation. This is CSE over sub-pipelines and
  wants its own design — the naive version would have to prove the shared
  prefix total.
- **Part-local dead code.** A Part whose body never Reveals computes nothing
  observable and could be deleted entirely under `--release`; today it is a
  lint warning (B5), which is the right first step.

---

## Outcome

All seven features shipped. `go test ./...` is green; every program in
`examples/`, `challenges/` and `testdata/` is `fmt`-clean and passes the golden
and oracle suites in both optimizer modes.

### Where the design was wrong

Four things the spec asserted turned out not to hold, and the code follows
reality rather than the plan:

1. **`ast.Keywords` needed no "structural set".** The pinning test in `prims`
   is one-directional (every registered keyword must be listed, not the
   reverse), so adding `Part` and `Innate Domain` was free — and they became
   reserved Shikigami names automatically, since `ReservedNames()` derives from
   the same list.
2. **Optimizer safety rule 4 bites Part bodies, as designed.** B6 asked
   whether in-place passes reach inside a Part: they do, for free, because
   `nodeLists` recurses on `Meta["nodes"]`. But *length-changing* passes
   correctly do not, so `Quicksort` + `Select Top K` inside a Part does **not**
   fuse into a quickselect. That is pinned with `explainAbsent` and documented
   in `language.md` rather than left as a surprise.
3. **Inlined nodes do not carry the call's position** (G1 assumed they did).
   They carry positions from the definition's body, which may be in the
   embedded prelude or a library file. `resolveShikigamiCall` now tags the last
   node of an inlined group with `Meta["callPos"]`, which is what gives a
   Shikigami call line its inlay hint.
4. **The recorder's orphaned-frame path is not panic-only.** It also fires
   whenever `--max-steps` truncates a run mid-loop, so it is ordinary
   behavior — surfaced as an `(incomplete)` row instead of silently dropped
   work.

### Bugs found while testing

- **`substExpr` had no `CondExpr` case**, so *any* Shikigami parameter used
  inside an `if/then/else` was left unsubstituted and then failed to resolve as
  an unknown identifier. Pre-existing, affecting Int and Text parameters
  equally; found because Bool parameters are almost always used in a
  conditional. `optimizer/walk.go`'s `substIdent` had always handled
  conditionals — this substituter had not caught up.
- **The formatter hugged parentheses after word operators**, turning
  `else (arm)` into `else(arm)`. Found by the repo-wide "every program still
  formats to itself" test, not by a unit case: `and`/`or`/`if`/`then`/`else`
  lex as `IDENT`, so the call-hugging rule mistook them for function names.

### Optimization results

| Feature | Measured / verified |
|---|---|
| Trace hook | untraced 1.267 ms/op, with `--stats` 1.240 ms/op, with a counting tracer 1.273 ms/op — all within noise, because the hook fires per *node*, not per element (`go test ./interp -bench Trace`) |
| `Part` | passthrough at runtime; the label is **specialized to a literal in codegen**, so a compiled binary has no label variable and no runtime branch — only the multi-line check `dmLabel` shares with `ir.LabelledOutput` |
| Imports | resolve-time only; because inlining precedes codegen, an imported Shikigami inherits every optimizer rewrite (tested: quickselect fires through an import) and the binary needs libraries at build time only |
| Signatures | resolve-time only, and explicitly *not* a compilation boundary — the `Top K Sum` quickselect test is the regression guard |
| `--stats` / `visualize` | opt-in; the recorder bounds both step count and value bytes |

An unexpected win: annotating the prelude with signatures **improved the most
common error in the language**. Calling `Top K Sum` on `Text` used to produce
an inlining trace pointing into the embedded prelude source; it now reports
`Shikigami "Top K Sum" expects input of type List<Int>, but the pipeline
produced Text` at the call, with the `Convert To Integers` bridge attached.

### Candidate future passes (recorded in docs/optimizer.md)

- **Common prefix hoisting across Parts** — two Parts starting with the same
  operations could share one computation. CSE over sub-pipelines; wants its own
  design, and the naive version would have to prove the shared prefix total.
- **Part-local dead code** — a Part whose body never Reveals computes nothing
  observable and could be deleted under `--release`. Today it is a lint
  warning, which is the right first step.
- **Length-changing passes inside sub-pipelines** — would need nested node
  lists to stop being captured by their parents' `Eval` closures. That is a
  real refactor of how Channel/loop/Part bodies are held, and it would let
  quickselect fusion fire inside a Part.
