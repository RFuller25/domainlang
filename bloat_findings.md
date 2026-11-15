# Bloat findings

Goal: reduce total lines of code, code complexity, and binary size.

## How this list was produced (and its limits)

- Baseline: ~102k non-test Go lines (+73k test lines), 937 tracked files. `go build ./cmd/domain` gives a **22.4 MB** binary.
- Tools run: `deadcode ./...`, `deadcode -test ./...`, `staticcheck` (U1000 + simplification checks), `go mod tidy` (dry run, reverted), `go tool nm -size` per package, and an AST-based duplicate-function-body scanner.
- I did **not** read all 100k lines. Items marked *verified* were checked by tool output and by reading the code. Items marked *candidate* are hypotheses from sampling, and the reviewer must confirm them before anyone acts.
- Estimates are rough.
- Behavior must not change. Golden tests, `go test ./...` and `bench/` expected outputs are the safety net.

---

## A. Binary size

### A1. Strip the shipped CLI binary (*verified*, trivial, biggest cheap win)
- `go build ./cmd/domain` produces 22,370,699 bytes. With `-ldflags="-s -w"` it is 16,617,764 bytes, **about 5.7 MB (26%) smaller**.
- `Makefile` runs a plain `go build -o domain ./cmd/domain`. `flake.nix` uses `buildGoModule` with no `ldflags`.
- `docs/wasm/build.sh` already uses `-s -w`, and the generated-program build in `codegen/build.go:90` already uses `-trimpath -ldflags "-s -w"`. Only the CLI itself is left unstripped.
- Action: add `-trimpath -ldflags="-s -w"` to the Makefile `build` target and to `ldflags` in `flake.nix`.
- Caveat to check: nothing in the repo may rely on symbols or DWARF in the CLI binary, for example panic stack traces used in tests. Panics still carry function names when stripped.

### A2. Big dependency footprint pulled by optional features (*verified* sizes, *candidate* action)
Symbol sizes (bytes) by package, from `go tool nm -size` on the unstripped binary:

| package | size | pulled in by |
|---|---|---|
| net/http | 454k | `aoc` (AoC fetch/submit), `game/net.go` (HTTP requests for Game Dev programs), the `documentation` server in `cmd/domain` (`documentationHandler`, `repl_docs.go`) |
| crypto/tls, x509, nistec, vendor/golang | ~600k | transitively via net/http |
| net | 152k | same |
| go/parser, go/ast, go/scanner | ~105k+ | `codegen/deps.go`, used only to extract import blocks |
| go/printer, go/format | ~85k | `codegen/codegen.go:270` (`format.Source` on generated code) |
| encoding/json | 91k | `lsp`, `aoc`, `ir/json.go`, `editors` |
| regexp (+syntax) | ~100k | `aoc`, `pattern`, `prims/match*.go` |

Options, ordered by safety:
1. **`format.Source` in `codegen/codegen.go:270`** formats generated Go source that is then handed to `go build`. If it is only cosmetic it could be dropped, saving go/printer + go/format (~85k+). It is NOT safe to drop if the output is shown to users (`--emit-go`) or golden-tested, so the reviewer must check. `dropUnusedRangeVars` runs before it, and the comment at line 290 mentions gofmt moving code, so something may depend on formatting.
2. **`codegen/deps.go` uses `go/parser`** only to find external imports in generated source. The generator already knows which imports it registered, so tracking them at registration time would remove go/parser, go/ast and go/scanner from the binary, and ~100 lines of parsing code.
3. **Build-tag the network features** (`aoc`, docs server, Game Dev HTTP) behind a tag such as `-tags full`, with the default build lean. This removes about 1.2 MB of net/http + TLS. It is a product decision (it changes what the default binary can do), so it is flagged *needs maintainer decision* and should not be done unilaterally.
4. `docs.FS` embeds `index.html render.js *.md *.json wasm` (about 1.1 MB at the top level, including `gallery.json` at 112 KB). Those files are only needed by `domain doc` and the docs server. A tag or lazy download could drop them, but that is a product decision.

### A3. Giant single functions and huge emitted code (*verified* sizes)
Largest symbols in the binary:
- `(*gen).compileCall` in `codegen/expr.go`: **86,770 bytes** in one function.
- `eval.evalCall` in `eval/eval.go`: 74,160 bytes.
- `(*gen).tryFuse`: 17.8 KB. `main.devModel.key`: 16.2 KB. `main.useTheme`: 13.9 KB (!). `prims.init`: 10.3 KB.
- The `emitExplore*`, `emitStreamRun`, `emitLoop` and `emitAccumulateUp` families are each 8–14 KB.
- `main.useTheme` at 13.9 KB is suspicious for a theme setter. It probably builds many lipgloss styles inline and is worth a look.

These are not dead code, but they are the hot spots for both complexity and size, and are the natural places to table-drive (see C1).

---

## B. Dead code and unused declarations

### B1. Unreachable even from tests (*verified*, safe to delete)
Both `deadcode -test` and `staticcheck U1000` agree:
- `prims/higher_order.go:102` `requireKeyable` (~7 lines)
- `runner/contest.go:176` `CheckAgreement` (~7 lines)
- `runner/contest.go:292` `Speedup` (~6 lines)
- `codegen/seqgen.go:180` struct field `key` in the local `bump` type (unused field; `nbExpr`/`n` are used)

### B2. Reachable only from tests but defined in non-test files (*verified*)
`deadcode` without `-test` reports these. They add lines to the production package but are not in the linked binary, so they do not affect binary size. They can move to `_test.go` files, or be deleted together with the tests that use them. This is a line-count and API-surface win only.

- `prims/usage.go`: `newUsage`, `Usage.Merge`, `AllPrims`, `AllBuiltins`, `AllKeywords`, `Used`, `Usage.walkExpr` (~115 lines in total). **Check whether the whole file can go**: only `Used` and friends look dead, and the rest of the file may have live callers.
- `prims/infer.go`: `Infer` (3 lines), `ReservedNames` (28), `ReservedNamesIn` (24).
- `prims/prims.go:574` `Resolve` (3 lines; wrapper)
- `docs/blocks.go`: `Examples` (47), `Blocks` (26), `Example.Source` (18), `AllBlocks` (15), `Block.Ignored`/`Runnable`/`flags`/`hasFlag`, `Pages`. That is about 140 lines in total, and probably the whole file apart from what `docs` tests use. Relocate to `_test.go`.
- `codegen/runtime.go:2737` `DeclAllocReport` (29 lines)
- `codegen/scopes.go:36` `Compilable` (22 lines)
- `ir/ir.go:120` `Keyable` (24 lines; `ListKeyable` is the live one and is used widely)
- `ir/deque.go` `Queue.Len` and `Stack.Len`; `ir/unionfind.go` `UnionFind.Connected`
- `eval/eval.go:2302` `EvalLambda` (3 lines; the optimizer uses `EvalLambdaTyped`)
- `interp/host.go:47` `HasHost`
- `langs/langs.go` `All` (10) and `ByExt` (12)
- `mahoraga/mahoraga.go:306` `ReporterFunc.Report`; `mahoraga/stats.go:93` `Stats.RelStdErr`
- `runner/runner.go`: `Run` (7), `Once` (6), `Interpret` (11)
- `cmd/domain/dev_highlight.go:104` `renderLine` (5 lines)

Estimated total for B1+B2: **~450 lines**. Before removing any of them, grep for use in tests, docs and README (some may be documented public API).

### B3. `go.mod` hygiene (*verified*)
`go mod tidy` rewrites `go.mod` (+17/−6 lines) and `go.sum` (+8 lines). Every direct dependency (bubbletea, lipgloss, bubbles and so on) is currently marked `// indirect` even though the repo imports them directly. This changes no code and no binary size, but it is metadata correctness. Caution: `codegen/deps_data.go` is generated from go.mod/go.sum and `deps_test.go` fails on drift, so regenerate it in the same change.

---

## C. Duplicated code and complexity

### C1. Triple implementation of every builtin (*candidate*, largest complexity item, HIGH risk)
Each expression builtin is implemented separately in `typecheck/typecheck.go` (1,911 lines), `eval/eval.go` (2,799 lines, 160 `case "..."` arms), and `codegen/expr.go` (2,459 lines, 161 arms) plus `codegen/codegen.go` (134 arms). Many are also mirrored as Go source strings in `codegen/runtime.go` (2,990 lines of `decl*` constants). The runtime.go comments say "mirrors prims.X" or "mirrors optimizer.Y" in many places, so the same algorithm exists as a compiler implementation and as an embedded-string implementation.
- Possible directions: a descriptor table (name → arity, result-type rule, eval func, emit snippet) so adding or changing a builtin touches one place, and splitting `compileCall` (86 KB) and `evalCall` (74 KB) into small per-builtin functions.
- Estimated savings are large (possibly thousands of lines), but the risk is also large, because the differential tests (`codegen/differential_test.go`) are the only guard. **Do not attempt as a drive-by.** Treat it as a design task.

### C2. Exact and near duplicates across packages (*verified* by AST hash scan and reading)
Exact body duplicates, same name in two packages:
- `pathAncestor`, `anyStageInPlace`, `needsOwn` are identical in `codegen/loopgen.go` (~333–372) and `prims/control.go` (~513–552). Move them to one shared place. `ir` is a likely home, since both packages already import it, and `ir.OwnsEverything` is already there. About 25 lines are saved. `ownedFields` differs (one takes `meta map[string]any`, the other takes `*ir.Node`), so it can become a thin wrapper over a shared core.
- `readInts` is duplicated in `bench/testdata/any_from_count.go` and `bench/testdata/sort_gaps.go` (14 lines). **These are benchmark fixtures that are intentionally standalone programs, so probably leave them.**

Near duplicates (same intent, small helper copied):
- `plural(n, noun)` exists three times: `mahoraga/turns.go:536`, `cmd/domain/visualize_tui.go:1392`, `interp/record.go:666`. One exported helper would replace all three (~16 lines, 2 fewer copies).
- `firstLine(s)` exists three times: `aoc/submit.go:91`, `mahoraga/search.go:728`, and `cmd/domain/visualize_record.go:304`. The last one appends "…", so it is a different function. The first two are identical in behavior, and `mahoraga/search.go` can use `strings.Cut` (like `aoc`) or `strings.IndexByte`.
- `freshName` is in `prims/locals.go:712` and `optimizer/cse.go:379`. Same loop, different naming scheme, so low value to merge.
- `callName` is in `diag/opinions.go:255` and `codegen/expr.go:1923`. Check whether they can share.
- `writeJSON` is in `ir/json.go:40` and `cmd/domain/bench.go:524`. Different signatures, so probably not worth merging.
- `codegen/runtime.go` pairs `dm*Count` / `dm*First` (`TripleCount/First`, `DiffCount/First`, `ProductCount/First`) re-derive the same lookup tables. They are small, but `declDiffCount` and `declDiffFirst` share the `flipped` logic. Low value.

### C3. Four "search" and four "highlight" mini-implementations in `cmd/domain` (*candidate*)
- `dev_search.go` (158 lines), `repl_search.go` (143) and `visualize_search.go` (332) each implement query state, stepping and key handling for a different Bubble Tea model.
- `dev_highlight.go` (161), `repl_highlight.go` (312) and `visualize_style.go` (294) all paint source or values. `dev_highlight` explicitly defers token classification to `repl_highlight.faceFor`, so the sharing is partly done.
- `repl_progress.go` and `visualize_progress.go` are two separate progress counters/printers (~170 lines).
- `repl_watch.go` (117) and `run_watch.go` (174) both poll file mtimes (`watch.stat` / `watch.changed` vs. `watchedFiles`/`runWatcher`).
- Worth a look for a shared `watcher` and a shared search-state type. Estimated saving is 200–400 lines, and the risk is moderate because of UI behavior. TUI tests exist (`repl_tty_test.go`, `visualize_ui_test.go`).

### C4. Repetitive error plumbing (*verified* counts, low priority)
There are 663 `if err != nil` sites in `codegen/` alone, and most are `return "", unsupported(...)` after a `g.compileExpr(...)`. A small helper type or a `must`-style panic/recover inside the code generator would cut hundreds of lines. It is mechanical but invasive, and it changes control flow, so it needs a maintainer opinion.

### C5. The `emit*` functions in codegen (*candidate*)
There are 174 `(*gen).emit*` methods that build Go source from `fmt.Sprintf` and `strings.Builder` writes. The runtime `decl*` constants are already a better pattern (`text/template` or static constants). Moving more of the large `emit*` bodies to static snippet constants with a few substitutions would shrink the functions and the binary's code (string data is cheaper than generated code). The `emitExplore*` family (`emitExplore`, `emitExploreCheapest`, `emitExploreTally`; about 8–14 KB each) is the best place to start.

---

## D. Repository bloat (not binary, not code lines)

- `docs/superpowers/` holds 20 files (~550 KB, ~12k lines) of historical plans and specs (`2026-07-21-for-loop.md` is 1,860 lines). They are not embedded in the binary (`docs/embed.go` only globs the top level), so this is repo weight only. The reviewer should check whether they are referenced from other docs or tests before archiving or removing them.
- `docs/gallery.json` (112 KB, 365 lines) is generated from `examples/` by `go test -update` and also committed. It is embedded into the binary. It could be generated at build time instead of committed, but `docs/gen_test.go` currently checks it for drift on purpose.
- `bench/mahoraga/i15` is a 56-byte stray file with no extension (the others are `.domain/.expected/.input`). It may be accidental. *candidate*
- `selfhost/` (~320 KB, README 817 lines) includes `spikes/` and `phase*` directories. Check whether they are still exercised by `selfhost/run_tests.sh` or CI. If stale, they are candidates for removal. *candidate, needs maintainer decision*
- `.gitignore` carries ~25 stray-binary names (`/day1`, `/s5g` …). It is harmless clutter that could become one generic rule.

---

## E. Suggested order of work

1. A1 (strip flags) — a two-line change, −5.7 MB.
2. B1 + B2 dead code and `go mod tidy` — about −450 lines, no behavior change.
3. C2 shared helpers (`pathAncestor`, `anyStageInPlace`, `needsOwn`, `plural`, `firstLine`) — about −50 lines.
4. A2.1 and A2.2 (drop `go/format` and `go/parser` if verified safe).
5. C3 `cmd/domain` UI consolidation.
6. C1 / C4 / C5 as separate design tasks with maintainer sign-off.

---

## Review verdicts

Reviewed by reading the cited code, grepping every name across `.go`, `.md`, `.sh`, `.nix`, `.js` and `.html` (tests, docs, README, build tags, `go:generate`, the `js/wasm` build in `cmd/domain-wasm`), and by building and running the full test suite before and after. Verdicts: **VALID** (confirmed and acted on), **INVALID** (the claim or the proposed action does not hold; not acted on), **NEEDS-MAINTAINER-DECISION** (true as far as it goes, but acting changes what the project ships or keeps; not acted on).

Context that changes several B2 verdicts: commit `09f87e1` (two days before this review) removed the `stats`, `coverage` and `battle` commands from the CLI but left their support libraries and tests in place. Much of the "test-only" code below is the orphan of that removal, not accidental dead code. `docs/superpowers/specs/2026-08-12-expansion-toolbelt-design.md` also records an explicit policy of keeping built-and-tested measurement-layer API that has no caller yet (`SamePipeline` "exists and is unused until something wants it").

### A. Binary size

| Finding | Verdict | Reason |
|---|---|---|
| A1 strip the CLI (`Makefile`, `flake.nix`) | **VALID** — done | Nothing reads symbols or DWARF from the CLI (no `debug/elf`, `gosym`, `runtime.Caller` or `runtime.GOROOT` in non-test code; `debug.ReadBuildInfo` survives `-s -w`). Tests build their own binaries and never go through the Makefile. Stripped binary runs `run` and `build` correctly. `flake.nix` gets `ldflags = [ "-s" "-w" ]` only: `-trimpath` is deliberately not added there because the package sets `allowGoReference = true`. |
| A2.1 drop `format.Source` | **INVALID** | The formatted text is user-facing and tested: `--emit-go` writes it (README, examples/README), `codegen/testdata/day1_optimized_go.golden` snapshots it, and the visualizer's node spans are read back from the *formatted* text (`readMarks`, comment at `codegen/codegen.go:290`). It is also the "generated Go does not parse" internal check. |
| A2.2 drop `go/parser` from `codegen/deps.go` | **INVALID** | No binary saving while A2.1 stands: `go/format` itself imports `go/parser`, `go/ast`, `go/scanner`. `moduleFiles`/`WithModuleHeader` take a source string by design (the file comment argues for parsing over tracking), and the code saving is about 15 lines. |
| A2.3 build-tag net/http features | **NEEDS-MAINTAINER-DECISION** | Changes what the default binary can do (`aoc`, docs server, Game Dev HTTP). Not done. |
| A2.4 un-embed `docs.FS` | **NEEDS-MAINTAINER-DECISION** | Product decision, as the finding says. Not done. |
| A3 giant functions | VALID as an observation; no action defined | Sizes not re-measured. `useTheme` is an 87-line function assigning lipgloss styles; nothing dead in it. Splitting `compileCall`/`evalCall` belongs with C1. |

### B. Dead code

| Finding | Verdict | Reason |
|---|---|---|
| B1 `prims.requireKeyable` | **VALID** — deleted | No caller anywhere, tests included. |
| B1 `runner.CheckAgreement`, `runner.Speedup` | **VALID** — deleted | No caller anywhere, tests included (orphans of the removed `battle` command; `Speedup` hits elsewhere are the unrelated `Recipe.Speedup` field). |
| B1 `bump.key` field in `codegen/seqgen.go` | **VALID** — deleted | Never read or written. |
| B2 `docs/blocks.go` (`Examples`, `Blocks`, `AllBlocks`, `Pages`, `Block.*`, `Example.Source`) | **INVALID** | Used by tests in *another* package (`cmd/domain/docs_runnable_test.go` calls `docs.Pages`, `docs.Examples`, `ex.Source`), and `docs/*_test.go` are external `docs_test` users too. A `_test.go` file cannot export across packages, so the proposed move does not compile. `docs/index.html:1493` also documents itself as mirroring `docs.Examples`. |
| B2 `codegen.DeclAllocReport` | **INVALID** | Used by `runner/alloc_test.go` (another package). Cannot move. |
| B2 `codegen.Compilable` | **INVALID** | Used by `cmd/domain/docs_runnable_test.go` and the external `codegen_test` package. Cannot move. |
| B2 `prims.Infer` | **INVALID** | Used by `editors/gen_test.go` (another package). |
| B2 `prims.Resolve` | **INVALID** | Used by tests in `format`, `game` and `optimizer`. |
| B2 `eval.EvalLambda` | **VALID** — moved to `eval/eval_test.go` | A 3-line wrapper used only by `eval`'s own in-package tests. |
| B2 `renderLine` in `cmd/domain/dev_highlight.go` | **VALID** — moved to `dev_buffer_test.go` | Only that test file calls it. |
| B2 `mahoraga.ReporterFunc.Report` | **VALID** — type and method moved to `search_test.go` | The type's only use is one in-package test. |
| B2 `prims/usage.go` (`Used`, `Usage.Merge`, `AllPrims`, `AllBuiltins`, `AllKeywords`, `newUsage`, `walkExpr`) | **NEEDS-MAINTAINER-DECISION** | The whole file cannot go: `CatalogID`, `WalkNodes` and `lambdaBlockNodes` are live. The rest is the measurement layer of the removed `coverage`/`stats` commands, with a 243-line test of its own. Moving it into a test file only relabels lines; the real choice is delete it with its tests (about 170 + 240 lines) or keep it for a returning command. |
| B2 `prims.ReservedNames`, `ReservedNamesIn` | **NEEDS-MAINTAINER-DECISION** | Only caller is the test of the function itself. Same delete-with-test-or-keep choice. |
| B2 `ir.Keyable` | **NEEDS-MAINTAINER-DECISION** | After B1 it has no production caller, but it is the stated Map-key/Set-element rule: `ir.ListKeyable`'s doc and `prims/types.go` refer to it and four `ir` test files assert on it. Removing it means rewriting those. |
| B2 `ir.Queue.Len`, `ir.Stack.Len`, `ir.UnionFind.Connected` | **NEEDS-MAINTAINER-DECISION** | Three one-line accessors on production types, used by `ir` tests to observe state. Moving a method of a production type into a test file is worse than either keeping or deleting it. |
| B2 `interp.HasHost` | **NEEDS-MAINTAINER-DECISION** | Its doc says it exists for a binary to assert a host was linked in; nothing does today. Keep or delete with its test. |
| B2 `langs.All`, `langs.ByExt` | **NEEDS-MAINTAINER-DECISION** | Orphans of the removed `battle` command, each with its own test. |
| B2 `mahoraga.Stats.RelStdErr` | **NEEDS-MAINTAINER-DECISION** | Only its own test calls it. |
| B2 `runner.Run`, `runner.Once`, `runner.Interpret` | **NEEDS-MAINTAINER-DECISION** | Specified library API of the toolbelt design (`Once` for the dropped `shrink`/`fuzz`, `Interpret` "for the callers that want the trace hook"), which the design doc says is kept built and tested on purpose. |
| B3 `go mod tidy` + regenerate `codegen/deps_data.go` | **VALID** — done | Module set and versions are unchanged; only `// indirect` markers move and `go.sum` gains 8 lines. `go mod vendor` output is byte-identical before and after, so the `vendorHash` in `flake.nix` still holds. `deps_data.go` regenerated with `go test ./codegen -run Deps -update` (not the bare `-update`, which also rewrites golden snapshots). Side effect to know about: the `go.mod` an emitted Game Dev program is built with lists the same requirements in a different order, and its `go.sum` has the 8 extra lines. |

The B1+B2 estimate of ~450 lines does not survive review: about 40 lines were deletable outright and about 20 could move; the remainder is either cross-package test API or a keep-or-delete decision.

### C. Duplication and complexity

| Finding | Verdict | Reason |
|---|---|---|
| C1 triple implementation of builtins | **NEEDS-MAINTAINER-DECISION** | High-risk design task. Not attempted. |
| C2 `pathAncestor`, `needsOwn`, `ownedFields` | **VALID** — done, with a correction | Moved to `ir` as `ir.NeedsOwn`, `ir.OwnedFieldSet` and unexported `pathAncestor`. |
| C2 `anyStageInPlace` | **VALID** — done, but not in `ir` | It calls `ast.HasInPlace`, and `ast` imports `ir`, so `ir` cannot hold it. Exported as `prims.AnyStageInPlace`; `codegen` already imports `prims`. |
| C2 `readInts` in bench fixtures | **INVALID** (as an action) | Standalone fixture programs; the finding itself says to leave them. |
| C2 `plural` ×3 | **VALID** — done | One `interp.Plural(int, string)` (interp already hosts `FormatDuration` for the same callers). `cmd/domain` keeps the name via `var plural = interp.Plural`; `mahoraga`'s `int64` copy is replaced with a conversion at its one `int64` call site. Output is identical. |
| C2 `firstLine` ×3 | **INVALID** as a merge; small simplification done | `aoc` imports nothing from this module and `mahoraga` does not import `aoc`, so sharing needs a new package for a 4-line function. `aoc`'s copy is shortened by 2 lines; `mahoraga`'s is left (using `strings.Cut` there would add an import to save 3 lines). The `cmd/domain` one is a different function, as noted. |
| C2 `freshName`, `callName`, `writeJSON`, `dm*Count/First` | **INVALID** (as actions) | Confirmed different signatures/behaviour (`callName` returns `string` in `diag`, `(*ast.CallExpr, string)` in `codegen`). Not worth merging, as the finding suspected. |
| C3 `cmd/domain` search/highlight/progress/watch consolidation | **NEEDS-MAINTAINER-DECISION** | Unverified candidate with UI-behaviour risk across three Bubble Tea models. Not validated in depth and not attempted. |
| C4 error plumbing in `codegen` | **NEEDS-MAINTAINER-DECISION** | Changes control flow throughout the generator. Not attempted. |
| C5 `emit*` to static snippets | **NEEDS-MAINTAINER-DECISION** | Architectural. Not attempted. |

### D. Repository bloat

| Finding | Verdict | Reason |
|---|---|---|
| `docs/superpowers/` | **NEEDS-MAINTAINER-DECISION** | Referenced from code comments (`optimizer/streamfuse.go`, `mahoraga/mahoraga.go`, `eval/bindings_bench_test.go`) and `bench/mahoraga/README.md`. Not removed. |
| `docs/gallery.json` generated at build time | **NEEDS-MAINTAINER-DECISION** | Committed on purpose; `docs/gen_test.go` checks drift. |
| `bench/mahoraga/i15` stray file | **INVALID** | It is the input of `i15_generators.domain`, which reads a file named `i15` (`bench/mahoraga/README.md:26`). |
| `selfhost/` | **NEEDS-MAINTAINER-DECISION** | Listed in the README as research notes. Not removed. |
| `.gitignore` stray-binary names | **INVALID** | The names are arbitrary program names at the repo root; no single pattern covers them without also matching source, which is the exact bug `repo_files_test.go` exists to prevent. |

### Measured result of the enacted changes

See the commit messages and the final report for the before/after line counts, binary sizes and test results.
