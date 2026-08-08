# Programs the interpreter ran and the compiler would not

`domain run` and `domain build` are two ways of executing the same
post-optimizer IR, and [compiler.md](compiler.md) makes that a testable claim:
byte-identical stdout, checked over every anchor program, the toolbox, the
challenges, and seeded random pipelines, in both optimizer modes.

There were programs where that claim did not hold — the interpreter ran them
and `domain build` did not produce a binary. This page is the survey that found
them, what it ruled out, and how each was closed. **Both families are fixed;
there is no known program the interpreter runs and the compiler refuses.** The
diagnosis is kept because it is the part worth re-reading if a third family
ever turns up.

## Summary

Two families, and one of them failed badly:

| # | Pattern | Was | Now |
|---|---|---|---|
| 1 | A `:=` from inside an indented pipeline body (a `Using:` written as a sub-pipeline) | Positioned refusal naming the binding | Compiles — the binding travels as a `*T` |
| 2a | `=` between two Record values whose types declare the same fields in a different order | Positioned refusal | Compiles |
| 2b | Any *other* meeting of those two Record types — `if` arms, list elements, a loop that threads one, a Map value | **`go build` failed on the emitted Go, with no Domain position** | Compiles |

Family 1 was a known, documented trade. Family 2 was not documented beyond one
line in compiler.md's Limits, and 2b was a straight defect: the compiler emitted
Go it had no reason to believe compiled, and the user got a `main.go:175:11:`
diagnostic about a generated struct they never wrote.

## Family 1 — `:=` inside a pipeline body

### Reproduction

```domain
Cursed Energy: nums.input
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Integers
Cursed Technique: Map Each
    Consider seen As 0
    Cursed Technique: Apply
        Using: (x) -> x + (seen := seen + 1)
Maximum Technique: Sum
Reveal: stdout
```

```
$ domain run  prog.domain     → 12
$ domain build prog.domain
domain: 4:1: the compiler backend cannot compile Map Each yet (lambda: this
Using: is an indented pipeline that updates "seen" with `:=`, which the
compiler cannot do — a body is compiled to a function of its own and the
binding reaches it as a copy. …)
```

### Why

`codegen/blockgen.go` lowers a `Using:` body to a **top-level Go function**
(`dmBlockN`), because the forty-nine emitters that consume a lambda all want an
*expression*, and most of them build it before opening the loop it goes inside.
The bindings in scope cannot be free variables of a top-level function, so
`blockFunc` passes them in as extra parameters — **by value**
(`codegen/blockgen.go:114-130`). A write inside the body would land on the
copy; the interpreter's binding stack is one shared thing and would write
through. `blockUpdates` (`codegen/blockgen.go:159`) detects that and stops
(`codegen/blockgen.go:43-54`) rather than shipping two backends that disagree.

Stopping was the right call. Passing by value was the part that needs
revisiting.

Note the shape that already works, and why: a `Consider … Of` sub-pipeline is
emitted **inline** (`codegen/bindgen.go:76-80`), so it is in the same Go
function as the binding it writes to, and a `:=` inside one compiles and
matches the interpreter today. Verified:

```domain
Cursed Technique: Apply
    Consider seen As 0
    Consider tot Of
        Cursed Technique: Map Each
            Using: (x) -> x + (seen := seen + 1)
        Maximum Technique: Sum
    Using: (xs) -> tot + seen
```
`domain run` and `domain build --run` both print `15`.

So the gap is not "`:=` and compiled code"; it is exactly "`:=` across the
by-value parameter boundary that `Using:` bodies introduce".

### Fixed: the written bindings travel by pointer

The interpreter's shared binding stack has an exact Go analogue — a `*T`
parameter — and the compiler already knew which names needed it, because
`blockUpdates` computed that set in order to refuse them.

- **`exprBinding` gained a cell**: the pointer to the variable behind it.
  `compileAssign` used to decide assignability by sniffing the variable name for
  a `dmLet`/`dmBind` prefix; it now checks the cell, which is the property the
  sniff was standing in for. A name with no cell — a lambda parameter, an
  ambient `For` variable, a channel value — is one the resolver already refuses
  to write to, so the check stays the backstop it was.
- **`blockFunc`** declares a written binding's parameter as `*T` and binds it to
  the deref, which is both the read expression and the lvalue the write needs.
  Everything else is still passed by value.
- **`emitBlockCall`** passes the cell for those names. The locals are ordinary
  `var` declarations of the enclosing function, so they are addressable, and a
  body nested inside another passes the same pointer straight down:

  ```go
  func dmBlock21(bv22 int64, bb23 *int64) int64 { … (*bb23) = ((*bb23) + 1) … }
  func dmBlock16(bv17 []int64, bb18 *int64) int64 { … dmBlock21(e20, bb18) … }
  ```

- **`blockUpdates`** now walks a nested body through its resolved nodes.
  `ast.UpdatedNames` stops at a `BlockBody` — `:=` is an expression-layer
  operator and a sub-pipeline's statements are not expressions — so the outer
  walk used to miss a write two bodies down. Missing one never miscompiled (the
  inner call would find no cell and stop), but it would now refuse a program
  that compiles.

**Cost.** An address taken is an escape, paid only by the bindings a body writes
to. `TestBlockBodyPassesOnlyWrittenBindingsByPointer` pins that a read-only
binding still travels by value.

**Watch for.** `blockFunc` memoizes per `*ast.BlockBody`, so the parameter list
must be a function of the body alone — the written-name set is, since it comes
from the body's own AST. Ambient `For` loop variables stay by value: they
cannot be written to at all, refused at resolve time.

**Tests.** `TestUpdateInsideBlockBody` replaced the refusal test, covering in
both optimizer modes: a write from a body to an enclosing `Consider`; a write
that has to outlive the body; bodies nested two deep; a loop body inside a
body; a body writing a binding it opened itself; a predicate body that writes
and filters on the result.

## Family 2 — Record field declaration order

### The contradiction

The value model says field order is cosmetic, in three places:

- `ir/ir.go:164` — "Records compare by field set (name → type), insensitive to
  declaration order."
- `ir/values.go:308` — `encodeKey` **sorts** field names, so two orders share
  one Map/Set key. `ir/keyof_test.go:77-86` pins this.
- `ir/values.go:394` — `DeepEqual` compares records by name.

The compiler says field order is identity-forming, because it interns the
generated struct under `Type.String()` (`codegen/types.go:128`), and
`Type.String()` prints fields in declaration order (`ir/ir.go:144-149`). So
`{a:Int, b:Int}` becomes `R1` and `{b:Int, a:Int}` becomes `R2` — two Go types
that Domain considers one type.

`eqFunc` (`codegen/eq.go:30`) and `fmtFunc` (`codegen/types.go:239`) interned
equality functions and formatters under the same order-sensitive key — harmless
duplication, but the same root cause. `eqFunc` is canonical now; `fmtFunc` stays
keyed on declaration order, because rendering is the one thing that order still
decides.

### 2a — the honest half

```domain
Cursed Technique: Map Each
    Using: (x) -> if record("a", x, "b", x) = record("b", x, "a", x) then 1 else 0
```

```
$ domain run  prog.domain     → 3
$ domain build prog.domain
domain: 4:1: the compiler backend cannot compile Sum By yet (lambda: cannot
compile = over {a:Int, b:Int} and {b:Int, a:Int} (same fields, different
declaration order)); run the program with 'domain run' instead
```

`compileBinary` compares the two Go type names and refuses when
they differ. A positioned refusal, which is the contract.

### 2b — the broken half

Nothing else checks. Any other place the two types meet reaches `go build`:

```domain
Cursed Technique: Map Each
    Using: (x) -> if x > 1 then record("a", x, "b", x * 2) else record("b", x * 2, "a", x)
Reveal: stdout
```

```
$ domain run  prog.domain     → [{b: 2, a: 1}, {a: 2, b: 4}, {a: 3, b: 6}]
$ domain build prog.domain
domain: go build failed: exit status 1
# domainprog
./main.go:175:11: cannot use R2{…} (value of struct type R2) as R1 value in return statement
```

The same failure from a list literal (`list(record("a",x,"b",x), record("b",x,"a",x))`
→ *"cannot use R2{…} … in array or slice literal"*). `typecheck`'s `condType`
(`typecheck/typecheck.go:130`) accepts the arms because `Equal` says they
match, and returns the *then* arm's type; codegen then emits the else arm as
`R2` into an `R1` slot.

Every other confluence has the same hole: `Iterate Until Fixed Point`
convergence, a Map's value type, a Channel's type, a Shikigami's return.

### The wrinkle the fix had to answer

The interpreter renders a record in the order the **value** was built —
`{b: 2, a: 1}` and `{a: 2, b: 4}` in the same list, above. A compiled record is
an unboxed struct with one field order; it cannot carry a per-value order. So
"make it compile" was not enough on its own — the two backends have to agree on
what it prints.

That was not an active divergence before, because in every program that
compiled, each record type had exactly one order and the value's order was the
type's. The set of programs where per-value order is observable is exactly the
set 2b broke on.

### Fixed: canonical interning, and the type decides the order

**The struct intern table is keyed on a canonical form of the type** —
`canonicalKey` in `codegen/types.go`, which is `Type.String()` with every
Record's fields sorted by name — rather than on `Type.String()` itself. Go
struct fields are named from the Domain field names, so every construction,
field access, `with`, and `Match Pattern` lowering keeps working unchanged.
`{a,b}` and `{b,a}` now become one Go struct, and 2a, 2b and the duplicate
`dmEq` functions resolve at once; the refusal in `compileBinary` is gone. The
struct declares its fields in name order too, so the layout does not depend on
which of the two the emitter reached first.

**The Reveal sink renders through the static type.** `ir.FormatValueTyped`
mirrors `FormatValue` but takes the type alongside the value and reads a
record's field order from it, recursing with element types the whole way down;
the `Emit` primitive, which is the one sink, already has that type as `n.In`.
For every program whose records declare their fields in one order this is
character-for-character what `FormatValue` produced — the value's order *is*
the type's. Where the two orders meet, the type wins, which is the only order a
compiled struct can produce.

`FormatValue` is unchanged and still renders a value's own order, so error
messages, traces and the REPL are untouched.

**Tests.** `TestRecordFieldOrderOracle` runs a differential case per confluence
— `=`, `if` arms, a list holding both, a Map value, a Set dedupe, a loop that
threads one and rebuilds it the other way, a binding read against the other
order — each in both optimizer modes. All fourteen fail if the intern key goes
back to `Type.String()`. `TestRecordRendersInTypeOrder` states the rendering
rule on its own, because the oracle would pass just as happily if both backends
agreed on the wrong order; it fails if the sink goes back to `FormatValue`.

## What this survey ruled out

Everything below *looked* like a gap and is not, recorded so the next survey
does not re-derive it.

- **Missing primitives.** Every `Prim` string a resolved or optimized node can
  carry has a case in `codegen/emitNode` (`codegen/codegen.go:424-614`). The
  `"no Go lowering for this primitive in the MVP"` default is reachable only by
  the synthetic primitive `codegen/codegen_test.go:1523` injects. The
  resolve-time renames are the trap: `Select Top K` resolves to a node named
  `SelectTopK`, `Sliding Reduce` to `WindowedReduce`, so a diff of
  `prims.Registry` IDs against the switch reports two false positives.
- **Float reductions.** `Sum`/`Max`/`Min`/`Product` over `List<Float>` compile.
- **Sorting.** `lessExpr` (`codegen/codegen.go:840`) covers exactly `ir.Ordered`
  (`ir/order.go:20`) — Int, Float, Text, and tuples of those. A record is
  keyable but not ordered, and is refused at resolve time in both backends.
- **`Count`.** Codegen handles List and Set; the primitive rejects anything else
  at resolve time (`prims/higher_order.go:268`).
- **Foreign blocks.** `codegen/foreigngen.go`'s encode/decode covers the same
  types as `prims/foreign.go`, and `TestForeignSpecsCoverEveryLanguage` refuses
  a language that can be run but not compiled.
- **`Pad Grid` `Fill:`.** The codegen literal path handles Int and Text, which is
  all the resolver accepts as a literal; a `Fill:` *lambda* of any type goes
  through `compileExpr` and compiles (verified with a `Grid<Bool>`).
- **Grid-search fusion.** `searchgen.go:108`'s default is unreachable —
  `gridSearchFusable` (`codegen/searchgen.go:16`) admits only the three
  primitives the switch above it handles.
- **`"%q is not a binding this backend can update"`** (`codegen/expr.go:189`).
  Guarded upstream by `blockUpdates` and by the resolver's refusal to write to a
  lambda parameter, a function binding, or a Shikigami parameter. Family 1's
  fix kept it as the backstop it is, restated as "this binding has no cell".
- **Every `"missing … metadata"`.** Internal invariants between the resolver and
  the backend, not program shapes.
- **Types with no compiled representation** (`codegen/types.go:70`) and **no
  renderer** (`codegen/types.go:389`). All eleven `ir.TypeKind`s are handled.
- **The shipped corpus.** All twenty examples, thirteen challenges, and every
  `testdata/` program compile.

## If a third family turns up

The method that found these two: the reachable refusals in `codegen` are a
short list (grep `unsupported(` and the raw `fmt.Errorf`s), but most of them are
internal invariants or guarded upstream, so each has to be probed with a program
rather than reasoned about. The two that were real both came from the same
shape of mistake — the backend holding a stricter notion of identity than the
language does. `Type.Equal` is the specification; anywhere `codegen` decides two
things are different that `Equal` calls the same, or vice versa, is worth a
probe.
