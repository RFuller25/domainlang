# `Simple Domain: For` loop — design

## Problem

Domain has three `Simple Domain` loop kinds today (`prims/control.go`): `Repeat N`, `While`, `Iterate Until Fixed Point`. All three thread only "the current pipeline value" through an indented body — none introduce a *named* variable. There is no way to write a loop body that also sees "this lap's element of some other list" by name.

The user wants `Simple Domain: For x in y` (iterate a named list) and `Simple Domain: For x in range(y)` (iterate `0..y-1`), with `x` usable as a real bound name throughout the loop body — not just within one lambda, across however many themed statements the body contains.

## Goals

1. `Simple Domain: For x in <source>` — a fourth loop kind. `<source>` is either:
   - a **channel name** (declared beforehand via `Channel "name": ...`, the same way `Fold From:` already references a channel), or
   - an inline **`range(N)`** call, yielding `List<Int>` `[0, N)` (0-indexed, exclusive — Python's `range` convention), where `N` is an Int literal only (`range(5)`) — mirroring `Repeat 3`, which is likewise literal-only (`op.Ints[0]`, confirmed in `prims/control.go`; there is no existing precedent anywhere for a channel-derived *count*, only channel-derived *lists* via `From:`/the channel-name source form above). No `range` builtin/concept exists anywhere in the codebase today (confirmed by grep, and by `challenges/01_fizzbuzz.domain`'s own comment: *"Domain has no range generator"*) — this is new.
2. Inside the loop body, every `Using:` lambda gets one extra trailing parameter — the loop variable — appended after whatever parameters the consuming primitive already binds. Example: `Filter`'s lambda normally takes 1 param; inside a `For x in y` body it takes 2: `Using: (v, x) -> v > x`.
3. Nested `For` loops each contribute their own trailing parameter, outermost first: `For x in a: For y in b: Using: (v, x, y) -> ...`.
4. The loop body still threads "the current pipeline value" across laps exactly like `While`/`Repeat` does today — `x` is *additional* context, not a replacement for that threading. The value after the loop is whatever the current value is after the last lap over `y`.
5. Interpreter support only (`domain run`, `domain repl`). `domain build` (the Go codegen backend) reports the same "unsupported, no Go lowering yet" positioned error `codegen/loopgen.go` already produces for other kind-specific gaps — an existing, documented pattern in this codebase, not new special-casing.

## Non-goals

- No Grid/Set/Map sources for `<source>` — `List<T>` (via a channel) or `range(N)` only. Grid already has its own iteration primitives (`Map Cells`, `Count Cells`, `Find Cells`); extending `For` to grids is separate future work.
- No codegen (Go-lowering) in this round — see Goal 5.
- No change to how `Repeat`/`While`/`Iterate Until Fixed Point` work — `For` is purely additive.
- No general closures or free-standing named variables anywhere else in the language — the ambient-extra-param mechanism below is scoped specifically to lambdas lexically inside a `For` body; it does not add closures to the expression layer in general.

## Architecture

### Parsing

`Simple Domain: For x in y` / `Simple Domain: For x in range(y)` parse as one operation phrase, exactly like `Repeat 3` and `While` already do — **no `parser/parser.go` changes needed**. Confirmed by reading the operation-phrase scanner (`parser/parser.go:360-425`): every `IDENT` token in the phrase is appended to `ast.Operation.Words` unconditionally, and parens are in the tolerated-but-structurally-ignored default case — so `For x in range(y)` and `For x in range y` both parse to the identical `Words = ["For", "x", "in", "range", "y"]` (parens are preserved only in `Raw`, never dropped from the source, just absent from `Words`). `prims/control.go`'s word-based `resolveLoop` switch gets a new case that reads `op.Words` positionally: the word after `For` is the loop variable name; the word after `in` is either a channel name directly, or (when it's literally `range`) the following word is an Int-literal argument (from `op.Ints`, same field `Repeat`'s count already uses) — never an arbitrary expression, since operation-phrase arguments aren't evaluated as general expressions anywhere else in the grammar either.

### The ambient-parameter mechanism

Domain's primitives are **stateless, package-level closures** — a `Primitive.Build` function is `func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error)`, with no resolver handle and no way to see "what's ambient right now." Threading a new parameter through every primitive's `Build`/`Eval` signature (dozens of registrations across `prims/*.go`) would be invasive and touch code that has nothing to do with loops.

Instead: a stack **internal to the `prims` package**, of `(name string, elemType *ir.Type)` at resolve time and `(name string, elemValue ir.Value)` at runtime. `resolveLoop`'s new `For` case pushes one entry before resolving the body (`r.resolveSequence(stmt.Block, cur, false)`) and pops it after — including on early return from an error, so a body that fails to resolve never leaves a stale entry behind.

Three existing choke points, confirmed as the *only* places any primitive checks or invokes a `Using:` lambda (33 call sites across 7 files: `channel.go`, `control.go`, `grid.go`, `higher_order.go`, `pairs.go`, `seq.go`, `sparse.go` — all routing through these two functions, with `requireLambda` additionally used by 16 of them for the arity check):

- `requireLambda(args, arity, prim, pos)` (`prims/higher_order.go`) — its `len(lam.Params) != arity` check becomes `!= arity + len(ambientStack)`.
- `typecheck.LambdaType(l, paramTypes...)` — call sites append the ambient stack's types (outermost first) to `paramTypes` before calling.
- `eval.EvalLambdaTyped(l, paramTypes, args...)` — call sites append the ambient stack's current values (outermost first) to `args` before calling.

No individual primitive's own logic changes — `Filter`, `Map Each`, `Fold`, etc. stay exactly as they are; a lambda simply gets to have more written parameters than the primitive's own base arity, resolved and evaluated transparently through the shared functions all of them already use.

### Runtime iteration

The `For` node's `Eval` (mirroring `whileNode`/`repeatNode`'s shape in `control.go`) resolves `<source>` to a `List<Int>` or the named channel's list once, then for each element: sets that element as the current top-of-stack ambient value, runs the body via `runBody` (threading the pipeline value forward exactly as the other loop kinds do), and moves to the next element. After the last element, pops the ambient entry and returns the final threaded value.

### Codegen

`codegen/codegen.go:328` dispatches to `emitLoop` via a hardcoded string switch on `n.Prim` — `"Simple Domain (Repeat)"`, `"Simple Domain (While)"`, `"Simple Domain (Fixed Point)"` only. A `Simple Domain (For)` node's `Prim` string doesn't match any of those, so it never reaches `emitLoop`'s own per-kind logic at all — it falls through to codegen's *outer* generic unsupported-primitive fallback (the same one an unrecognized primitive like a hypothetical `"Frobnicate"` hits today, confirmed via the existing `TestUnsupportedPrimitiveErrors` in `codegen/codegen_test.go:847`, which constructs a bare `*ir.Pipeline` node with an unmatched `Prim` and asserts the error names the primitive and points at `domain run`). Same observable result (a clean, positioned error, not a crash) as originally stated — just via codegen's outer dispatch, not `emitLoop`'s inner one.

## Testing

- **Parser**: `For x in <channel-name>` and `For x in range(N)` produce the expected operation text/args; malformed forms (missing `in`, missing loop variable name) produce clean parse errors.
- **Resolve**: 
  - A body lambda written with the correct extra arity resolves; one written with the wrong arity still produces today's "must take N parameter(s)" error (now N = base arity + ambient depth).
  - Two nested `For` loops stack two ambient entries, outermost first; a lambda inside the inner body can take both extra params in that order.
  - The ambient stack is popped after the body resolves — a statement *after* the loop, or a sibling loop, sees zero ambient entries.
  - A body that fails to resolve mid-way still pops its ambient entry (no leak into later resolution).
- **Interp**: 
  - `For x in <channel>` over a `List<Int>` channel evaluates the body once per element, threading the pipeline value across laps, `x` bound to each element in order.
  - `For x in range(N)` evaluates the body exactly `N` times with `x` = `0, 1, ..., N-1`.
  - Nested `For` loops: inner body sees both outer and inner element values, outermost param first.
  - The final pipeline value after the loop matches whatever the last lap's body produced.
- **`domain build`**: a program using `Simple Domain: For` produces the existing "unsupported, no Go lowering" positioned error — not a crash, not silently wrong output.
