# The Go compiler backend

`domain build` (or any subcommand-less invocation with extra arguments)
hands the **post-optimizer** IR — the same pipeline the interpreter runs —
to the `codegen` package, which emits one fully typed, self-contained Go
`main.go` (standard library only) and shells out to
`go build -trimpath -ldflags "-s -w"` (CGO disabled). The result is a
static binary around 1.5 MB that needs nothing at runtime.

Because algorithm selection already happened in the optimizer, a compiled
program contains the quickselect and the hash-set scan, not the requested
quicksort or pair loop — the thesis survives compilation intact.

## What the generated code looks like

- **Unboxed values everywhere.** `Int`→`int64`, `Text`→`string`,
  `List<T>`→`[]T`, Records→generated structs (`type R1 struct { a, b int64 }`),
  Tuples→positional structs, `Grid<T>`→a generic `dmGrid[T]`
  (row-major cells + dims), Maps/Sets→**insertion-ordered** generics
  `dmMap[K,V]` / `dmSet[T]` (a Go map paired with an order slice) so rendered
  output matches the interpreter byte-for-byte, `Sparse<T>`→a generic
  `dmSparse[T]` (set-cells map + default + bounds, iterated in the
  interpreter's sorted row-major order). No `[]any`, no interfaces.
- **Lambdas are inlined.** A `Using:` lambda compiles to a plain Go
  expression inside the loop that consumes it — Day 4's predicate becomes a
  bare `if (r.a <= r.c && r.b >= r.d) || ...`. No closures, no per-element
  evaluator walks. Division routes through a guarded helper so `/ 0` stays a
  clean error instead of a Go panic.
- **Pipeline bodies become functions.** A `Using:` written as an
  [indented pipeline](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)
  is emitted once as a top-level `func dmBlockN(...)`, and the expression that
  stands in for the lambda body is a call to it:

  ```go
  func dmBlock7(bv8 []int64) bool {
      var v9 int64
      for _, x10 := range bv8 { v9 += x10 }
      return v9 > 15
  }
  ```

  A body is statements, and the emitters that compile a lambda want an
  *expression* — most build it before opening the loop it goes inside, so
  emitting the statements there would land them in the wrong scope. A function
  gives every one of those sites something it already handles. Enclosing `For`
  loop variables are locals of `main`, so they are passed in as extra
  parameters rather than referenced freely. The cost is a call per invocation,
  which Go largely inlines back.
- **Match Pattern compiles to scanners.** A template whose holes are all
  ints (with literal separators a greedy scan provably cannot mis-split)
  becomes a hand-rolled string scanner — no regexp at runtime. Word/text
  holes fall back to one precompiled package-level regexp with the
  interpreter's exact lowering.
- **Combinations unroll.** `All Pairs` / `Combinations k` emit k nested
  loops at generation time, with the predicate inlined.
- **Loops thread one variable.** `Simple Domain` bodies are emitted once
  inside a Go loop; Fixed Point convergence uses generated structural
  equality functions (`dmEqN`), the same machinery that backs composite `=`
  in lambdas.
- **Imports vanish.** `Innate Domain` libraries are loaded and their
  Shikigami inlined before codegen runs, so an imported operation gets every
  optimizer rewrite a local one would and the emitted program contains no
  trace of the library. Libraries are needed at **build** time only; the
  binary stays self-contained.
- **Part labels are compile-time constants.** A `Part` block's body is
  emitted inline and its label is baked straight into the print
  (`fmt.Println(dmLabel("1", …))`), so a compiled binary carries no label
  variable and no runtime branch for it. The interpreter *must* carry the
  label on `ir.Context`, because the `Emit` node inside a Part body is
  reached through the Part's own `Eval` closure and there is no node to
  rewrite — this is one of the few places where the compiler can specialize
  something the interpreter has to keep dynamic. The one runtime decision
  that remains in both backends is whether the rendered value is multi-line
  (`dmLabel` mirrors `ir.LabelledOutput` exactly), since `Text` and every
  composite can contain a newline.
- **Expression builtins** lower to direct Go (`length` → `int64(len(x))`)
  or tiny generic `dm*` helpers emitted on demand.
- **Release mode** (`--release`) simply never emits vow nodes — zero
  assertion cost in the binary, including vows nested in Channel or loop
  bodies.

Inspect the output for any program with `domain build prog.domain
--emit-go -`; it is gofmt-formatted and deterministic (identical source for
identical input programs).

## Correctness: the interpreter is the oracle

The compiler's test suite front-ends every anchor program in `testdata/`
(including the full Day 5 crate simulation and Day 8 visibility solves)
plus a battery of inline edge programs — 40+ programs covering every
primitive, both Match Pattern paths, collection rendering, loops, channels,
tuples, composite equality, the builtins, conditionals, and release mode —
and for each one, in **both** optimizer modes: runs the interpreter,
compiles and runs the binary, and requires **byte-identical stdout**.

On top of the fixed programs, **differential property testing**
(`TestDifferentialRandomPipelines`) generates seeded random pipelines over
the compiled surface — Map/Filter/Sort/Top-K/Unique/Reverse chains over
random integer lists, and Transpose/Map Cells/Dijkstra chains over random
digit grids — and holds interpreter/binary stdout equal for every one, in
both optimizer modes. Seeds are fixed, so failures reproduce and CI is
deterministic.

Emission is also checked to be deterministic, a golden snapshot of the
Day 1 anchor's generated source catches accidental codegen churn in review
(re-bless with `go test ./codegen -run TestEmittedSourceGolden -update`),
and a synthetic unknown primitive proves the "not compilable yet" guard
still fires for future primitives shipped without a codegen case.

## Performance

Measured on synthetic large inputs (identical answers throughout):

- ~2× vs the interpreter on split-heavy work (AoC 2022 Day 1 shape, 200k
  groups).
- ~7× on Match Pattern-heavy work (AoC 2022 Day 4 shape, 1M lines:
  1.47s → 0.22s) — the regexp+evaluator per line becomes an inlined scanner
  and comparison.

These are now `go test -bench` guards (`codegen/bench_test.go`):
`BenchmarkSplitHeavy*` and `BenchmarkMatchPattern*` run interpreter vs
compiled binary over the same generated large inputs — the compiled numbers
include process startup, which is what a user actually pays. Run them with
`go test ./codegen -bench . -run XXX`.

## Documented semantic deltas

Success output is byte-identical to the interpreter; two deliberate
differences remain:

1. **Input path resolution.** Interpreting resolves a relative
   `Cursed Energy:` target against the program file's directory; a compiled
   binary resolves it against the **working directory** (it no longer knows
   where the source lived). Both fall back to stdin when the file is
   missing. This is a settled decision: a standalone binary behaving
   like every other CLI tool — inputs relative to where you run it — beats
   an `--input-base` override baked in at compile time. `domain build --run`
   inherits the caller's working directory, so its resolution matches a
   plain `./binary` invocation.
2. **Error wording.** Runtime failures abort with exit 1 and a
   `domain: ...` message in both backends, but the exact message text can
   differ (the interpreter's errors carry source positions; the binary's
   don't). Notably, a `Match Pattern` int capture that overflows int64 is
   reported by the interpreter as an invalid capture and by the binary as a
   non-matching line.

## Limits

- `=` between two Record types that share fields but declare them in
  different orders is rejected with a positioned error (they intern to
  distinct Go structs) rather than miscompiled.
- `While` / `Iterate Until Fixed Point` are **unbounded** in a compiled
  binary, matching the interpreter: `dmMaxLoopIterations` mirrors
  `prims.maxLoopIterations`, and both default to zero (no counter is emitted
  at all). A non-terminating loop therefore spins in a binary exactly as it
  does under the interpreter. The interpreter's bound is a `var` a test can
  lower; the compiled one is a build-time constant, so a binary cannot be
  asked for a ceiling after the fact.
- A primitive added to the language without a codegen case fails
  `domain build` with a positioned error naming it and pointing back at
  `domain run`.

## The AoC toolbox compiles

The full AoC toolbox surface is compiled:

- **Primitives:** Extract Integers, Split Fields, Convert To Set,
  Find Cells, Merge Ranges, Permutations, Subsets, BFS, Dijkstra,
  Flood Fill, Connected Components — the `Using:` predicates inline into
  mask/filter loops as usual, and the traversals are self-contained `dm*`
  helpers (slice queue/stack, a hand-rolled binary min-heap, union-find)
  mirroring `prims/search.go`, including the bad-start and negative-cost
  failure wording. The Permutations/Subsets bounds are read from the same
  vars as the interpreter (`prims.MaxPermutationInput` / `MaxSubsetInput`),
  both zero by default — so neither backend refuses a large input, and if a
  ceiling is set back both refuse identically.
- **Expression builtins:** all 92 compile. Points lower to the interned
  `(Int, Int)` tuple struct; `padd`/`manhattan`/`rotl`/`rotr`/`dirs4`/
  `neighbors4`/`neighbors8`/`solve2x2` become concrete `dm*` helpers over
  that struct; the sparse group (`sparse`/`put`/`has`/`cells`/bounds and
  total `at`) compiles against `dmSparse[T]`.

`TestCompiledToolboxMatchesInterpreter` pins interpreter/binary parity
over 14 toolbox programs (including the shapes of examples 11–15),
`TestCompiledSparseMatchesInterpreter` over 9 sparse programs, and
`TestCompiledChallengesMatchInterpreter` over all 13 challenge programs —
each in both optimizer modes.
