# Record literals and a first-class `Graph` type — implementation plan

> **Status: shipped in full — A.1–A.6 and B.1–B.10.** What the
> implementation learned and where it diverged is recorded in
> [§Part A outcome](#part-a-outcome) and [§Part B outcome](#part-b-outcome).

Two independent features, planned together because the first one unblocks a
piece of the second (brace tokens make record *type* syntax writable, which
`Graph<K>` signatures want).

- **Part A — record literals.** `{a: 1, b: x}` as sugar for
  `record("a", 1, "b", x)`, plus `{a: Int}` in a declared signature.
  Small: lexer, parser, formatter. No new semantics.
- **Part B — `Graph<K>`.** A new `ir.Kind` alongside `Map`/`Set`/`Grid`/
  `Sparse`, with coercion primitives, the existing search vocabulary taught
  to accept it, and an expression-layer builtin group.
  Large: comparable to the `Sparse` milestone, which touched 28 non-test
  files.

Do A first. It is a day's work, it is independently shippable, and B's
signatures read better once `{}` types exist.

---

## Global constraints

These are the repo's existing rules; they are restated because every task
below is bound by them.

- `go test ./...` green after every task. Baseline on this machine is ~4m
  (codegen ~90s, cmd/domain ~78s, bench ~32s, docs ~16s).
- **Both backends or neither.** A builtin implemented in `eval` but not
  `codegen` must fail compilation with a positioned "not compilable yet"
  error rather than silently diverge (`codegen/codegen.go` package comment).
- Anything that changes program output needs an interpreter-vs-binary oracle
  test **in both optimizer modes** — the `compilePipeline` /
  `runInterpreter` / `buildAndRun` triple used throughout `codegen/*_test.go`.
- A new primitive cannot ship without its `prims.Catalog` entry:
  `TestCatalogCoversRegistry` (`prims/catalog_test.go:9`) pins the catalog to
  exactly the registered set.
- Generated docs data is checked in and pinned:
  `TestPrimitiveIndexIsCurrent` (`docs/gen_test.go:301`). Regenerate with
  `go test ./docs -update`.
- Docs ship with the code. Fenced ```domain run blocks in `docs/*.md` are
  executed and their `input`/`output` blocks diffed, so a doc example is a
  test.
- New type names go in the editor grammars too:
  `editors/vscode/syntaxes/domain.tmLanguage.json:249` and
  `editors/nvim/syntax/domain.vim:74` both carry the type-name list.

---

# Part A — record literals

## A.0 Design

### The syntax

```domain
Using: (p) -> {row: prow(p), col: pcol(p), seen: 1}
```

Grammar, added to `parsePrimary`:

```
record  := '{' field (',' field)* '}'
field   := IDENT ':' expr
```

Field names are bare identifiers, never expressions. That is not a
restriction being invented here — `record("a", 1)` already requires literal
field names, because the *result type* depends on them
(`typecheck/typecheck.go:1255`, `recordType`). `{a: 1}` inherits that rule
and reads better for it.

### The key decision: desugar to `record(...)`, don't add an AST node

The parser builds an ordinary `*ast.CallExpr{Fn: Ident("record")}` with the
field names as `*ast.StringLit` args, and sets a new presentational flag
`CallExpr.Braced = true`. **No new `ast.Expr` type is introduced.**

This is worth being explicit about, because the obvious alternative — a
proper `ast.RecordLit` node — is a trap. Expression nodes are switched on in
at least ten non-test files, and three of the walkers **fail silently** on a
node they do not know:

| Site | Behavior on an unknown node | Consequence |
|---|---|---|
| `prims/locals.go:393` `rewriteExpr` | `default:` returns `e` unchanged | Shikigami parameter substitution **skips the record's field values** — a runtime "unknown identifier", or worse, a stale value |
| `prims/locals.go:663` `collectIdents` | unhandled kinds contribute nothing | `freshName` can hand out a name the record already uses → **variable capture** during inlining |
| `prims/locals.go:702` `renameIdents` | unhandled kinds returned as-is | same capture bug from the other direction |
| `format/expr.go:122` | returns `"(unrenderable expression)"` | `domain fmt` silently destroys the expression |
| `optimizer/walk.go:190` `isTotal` | falls through to `false` | conservative — safe, just blocks optimization |
| `prims/locals.go:781` `exprPos` | zero `Position` | error messages lose their caret |

None of these is a compile error. A new node type would need every one of
them updated by hand, and the failure mode for missing one is a silent
miscompile in a corner case. Desugaring at the parser means *every* walker,
optimizer pass, typechecker branch and codegen case already handles the
construct on day one, because it is a construct they have handled since
`record()` shipped.

The cost is exactly one thing: `domain fmt` must not rewrite `{a: 1}` into
`record("a", 1)`. The `Braced` flag buys that back — it is read by the
formatter and by nothing else.

### No conflict with `Match Pattern` templates

`Match Pattern "{word} -> {word}"` puts braces inside a **string literal**,
which the lexer never tokenizes. Brace tokens are genuinely unused today:
`lexer.go`'s punctuation switch (`lexer/lexer.go:343-374`) has no `{` case
and falls through to `unexpected character '{'`.

### Reserving `{` for map and set literals

`{1, 2}` (set) and `{"a": 1}` (map) are the natural next things to want, and
`ir.FormatValue` already *renders* sets and maps that way. They are **out of
scope** here, but the grammar must not paint them out. The discriminator is
decided now and documented:

- `{` `IDENT` `:` … → **record literal**.
- `{` anything else → reserved. The parse error says so by name rather than
  reporting a generic syntax error:
  `"{ starts a record literal, so it needs a field name: {name: value}. Map and set literals are not written with braces (yet) — use tomap(...) / toset(...)"`.

That message is the whole of the forward compatibility story, and it costs
one error branch.

### `{}` is rejected

`record()` requires at least one field (`typecheck.go:1257` and the arity
table), and an empty record type has no use in the language today. `{}`
errors with `"an empty record has no fields; write at least one, e.g. {n: 0}"`
rather than silently parsing to something the typechecker will reject less
clearly.

## A.1 Lexer: brace tokens

`token/token.go`: add `LBRACE`, `RBRACE` to the `Kind` enum (after `RPAREN`,
keeping the punctuation group together) and to `kindNames`.

`lexer/lexer.go`: two cases in the punctuation switch beside `'('`/`')'`
(line 349). **Do not** add braces to the `l.parens` tracking — that stack
exists for implicit line continuation inside parentheses, and whether a
braced literal should continue across lines is a separate question answered
in A.3.

Tests: `lexer` package — braces tokenize, position/offset correct, a brace
inside a string literal is still just text, a brace inside a comment is
ignored.

## A.2 AST: the presentation flag

`ast/ast.go`, on `CallExpr` beside the existing `InPlace` field:

```go
// Braced marks a call the source wrote as a record literal — `{a: 1}`
// rather than `record("a", 1)`. The two are the same call and every
// layer below the formatter treats them identically; the flag exists so
// `domain fmt` gives back the syntax the user wrote.
Braced bool
```

Nothing else in `ast` changes. Note that `Braced` is deliberately *not* part
of any equality or hashing of expressions — it is presentation only.

## A.3 Parser: the literal

`parser/expr.go`, a `case token.LBRACE` in `parsePrimary` (line 237). It
parses the field list and returns the desugared `CallExpr`.

Two details worth getting right:

- **Postfix chains work for free.** `parsePrimary` is called from
  `parsePostfix` (line 183), whose loop already handles `DOT`, so
  `{a: 1}.a` parses without further work. Add a test for it anyway.
- **Multi-line literals.** The lexer suppresses `NEWLINE` inside
  parentheses; braces are not in that stack (A.1), so a literal split across
  lines will not parse. That is the right initial behavior — it matches how
  a long `record(...)` call is written today (inside the parens of the
  enclosing call) and avoids a layout question this feature does not need to
  answer. The plan explicitly does **not** add brace-aware continuation.
  Revisit only if real programs want it.

Duplicate field names, an empty literal, a missing `:`, a non-identifier
before `:`, and a trailing comma each get their own message and test.
Duplicate detection can be left to `recordType` (it already reports
`"record has a duplicate field"`), but the parser has the better position
information, so report it here and keep the typecheck check as the backstop
for hand-written `record()` calls.

`parser/nesting_test.go` has a depth guard (`p.enter()`/`p.leave()`) — the
brace literal must call it, like `parseTypeExpr` does, so `{a: {b: {c: …}}}`
cannot blow the stack. Add a fuzz corpus entry (`parser/fuzz_test.go`).

## A.4 Formatter

`format/expr.go`, in `render`'s `*ast.CallExpr` case (line 77): when
`x.Braced` is set, render `{name: value, …}` by pulling the field names back
out of the even-indexed `StringLit` args. Guard the reconstruction — if the
args are not the expected `StringLit`/value alternation (an optimizer-built
call that inherited the flag, which should not happen but is cheap to
survive), fall back to `record(...)` rather than producing broken source.

Tests: idempotence (`Format(Format(x)) == Format(x)`) over a braced literal;
the existing repo-wide `--check` cleanliness test covers the rest.

## A.5 Record type syntax in signatures

Now that braces exist, close the gap `parser/types.go:18-20` documents by
name:

> Records (`{a:Int}`) are deliberately absent: the lexer has no brace tokens
> […] `lowerTypeExpr` says so by name if one is tried.

- `ast`: `TypeExpr` gains `Fields []TypeField` (name + `*TypeExpr`).
- `parser/types.go`: a `token.LBRACE` branch in `parseTypeExpr`, same
  `IDENT ':' type` shape as the value literal.
- `prims/types.go`: a `te.Fields != nil` case in `lowerTypeExpr` (line 35)
  building `ir.Record(...)`; add `{a:T}` to `knownTypeNames()` (line 112) so
  the unknown-type message advertises it.
- `ir.Type.String()` already prints records as `{a:Int, b:Text}`
  (`ir/ir.go:144`), so the written form and the printed form now agree —
  which they did not before. Worth a line in the docs.

Remember `ir.Type.Equal` compares records **by field set, order-insensitive**
(`ir/ir.go:187`), so a declared `{b:Text, a:Int}` matches an inferred
`{a:Int, b:Text}`. Test that explicitly; it is the kind of thing a reader
will not assume.

## A.6 Docs and editors

- `docs/ref-builtins-records.md`: the literal as the primary spelling,
  `record(...)` documented as the equivalent call. At least one
  ```domain run example (it becomes a test).
- `docs/expressions.md`: the grammar line, and the reserved-`{` note.
- `docs/language.md`: signature syntax gains the record form.
- `README.md`: if the syntax summary lists literal forms, add it.
- Editors: `{`/`}` need no highlighting rule of their own, but check the
  vscode/nvim grammars do not mis-scope a braced literal.

---

## Part A outcome

All six tasks shipped. `go test ./...` is green, the repo is `fmt`-clean,
and both backends agree on every example.

The central bet paid off exactly as argued: because the parser desugars to
`record(...)`, **nothing below the parser changed**. No case was added to
typecheck, eval, codegen or any optimizer pass, and a record literal inside
a Shikigami body gets its parameters substituted correctly on day one — the
failure mode the design was chosen to avoid.

### Where the plan was wrong

1. **Multi-line literals work, and are left working.** A.3 predicted they
   would be refused, since braces deliberately do not suspend layout in the
   lexer. But an argument written across several lines already has its
   layout tokens spliced out before the expression is parsed
   (`joinArgContinuation`) — the same mechanism that lets a long
   `record(...)` call be broken up today. So `{a: 1,\n    b: 2}` parses and
   runs correctly in both backends with no brace-aware layout at all. The
   plan's instinct not to touch the paren stack was right; its prediction
   about the consequence was not.

2. **A.4 named one renderer; there are two.** `format/expr.go`'s AST
   renderer is not what `domain fmt` runs. `domain fmt` is *token*-oriented
   (`renderTokens`/`needsSpace` in `format/format.go`), so the braces
   needed a spacing rule there as well, or `{a: 1}` came back as
   `{ a: 1 }`. The AST renderer still needed `renderBraced` — the REPL and
   the diagnostics reach for it — so both were required, not either.

3. **`renderBraced` needs a structural check, not just the flag.** `Braced`
   is presentation only and no pass is obliged to preserve it across a
   rebuild, so a call carrying the flag without the name/value alternation
   the syntax requires — or with a field name no identifier can spell, like
   a written `record("has space", 1)` — must fall back to `record(...)`.
   Formatting is the one operation that has to return something valid
   whatever it is handed.

4. **The docs task was load-bearing.** Three pages carried claims the
   feature falsified, including `ref-builtins-records.md` asserting that
   `record` "needs **no new syntax** — no braces" and `language.md` stating
   "**Record types cannot be written**". A.6 was written as routine
   documentation and turned out to be the change that keeps the reference
   honest.

5. **The editors needed nothing.** Both grammars already scope their brace
   rule to the inside of a string (Match Pattern holes), and no new type
   *name* was introduced, so a literal outside a string was never going to
   be mis-highlighted.

### Bug found while testing

**The codegen oracle tests raced on the interpreter's global state.**
`go test ./...` failed intermittently with `internal error during
interpretation: slice bounds out of range [:-2]`, only under the full
package run and never under a `-run` filter.

`resolveMu` already existed in `codegen/codegen_test.go` for exactly this
class of problem, and its own comment quotes the invariant —
"prims.Resolve / interp.Run are never called concurrently within one
process" (`prims/ambient.go`) — but only the `Resolve` half had ever been
locked. `interp.Run` needs it too: `eval` keeps the runtime binding stack
at package level (`eval/bindings.go`) and `interp.Run` clears it on the way
in, so one run's `ResetBindings` racing another's `PopBindings` slices to a
negative length. `ir.EvalNode`'s `currentCtx` is a third piece of shared
state with the same exposure.

Pre-existing, and not a bug in shipped behavior — `cmd/domain` maintains
the invariant correctly with `frontEndMu`. It was the *test suite* that
violated it, by running subtests in parallel. Every interpreter run in the
package now goes through a `lockedRun` helper; the package is clean under
`-race`. `codegen` is the only test package that runs subtests in parallel,
so nothing else needed the same treatment.

This matters for Part B: the plan's "green after every task" constraint was
not actually checkable before this was fixed.

---

# Part B — a first-class `Graph<K>`

## B.0 Design

### Why a real type, and what already exists

The graph vocabulary is already here, split three ways — `prims/toposort.go`
opens by saying so:

> The graph half of the search vocabulary. BFS/Dijkstra/Flood Fill/Connected
> Components all take a Grid, and Explore answers reachability over an
> implicit state graph; this answers the other standard question about an
> explicit one.

| Today | Input | Gap |
|---|---|---|
| `BFS`, `Dijkstra`, `Flood Fill`, `Connected Components` | `Grid<T>` | geometry only — cannot take a named-node graph |
| `Explore` | seed state + `S -> List<S>` lambda | implicit; the graph is never a value, so it cannot be built once and queried repeatedly |
| `Topological Sort` | `Map<K,List<K>>` or `List<(K,K)>` | the only explicit form, and it is re-derived per call |

So an explicit graph is expressible but never *a thing*: you rebuild
adjacency at every stage, weights have nowhere to live, and the four Grid
algorithms are unreachable for a graph parsed out of text. `Graph<K>` is the
missing value.

It must be a new `ir.Kind`, not sugar over `Map<K, List<(K, Int)>>`.
Primitives dispatch on `Type.Kind`; if `Convert To Graph` returned a Map,
`BFS` could not tell a graph from any other map and the whole point is lost.

### The type

**`Graph<K>` — directed, `Int`-weighted, insertion-ordered.**

Four decisions, each with its reason:

1. **One type parameter, not two.** Edge weights are always `Int`, default
   `1`. `Graph<K, W>` would double every generated codegen helper for a
   payload the language models as `Int` everywhere else — `Explore`'s
   `Cost:` is `Int`, `Dijkstra` is `Grid<Int> -> Grid<Int>`. An unweighted
   graph is one whose weights are all `1`, and the algorithms need no
   separate path.
2. **Directed.** An undirected graph is a directed one with both arcs, which
   `Convert To Graph`'s `Mode: Undirected` inserts. This keeps one
   representation and one set of algorithms; the alternative (a flag on the
   value) leaks into equality, rendering and every primitive.
3. **`K` must be `ir.Keyable`.** Same rule as `Set`/`Map` keys and
   `Explore`'s state (`prims/explore.go:71`) — the visited set is what makes
   traversal terminate. `Graph<K>` is itself **not** keyable, like
   `List`/`Map`/`Set`/`Grid`.
4. **Insertion-ordered**, like `MapValue`/`SetValue` and unlike
   `SparseValue` (which sorts, because it is geometry). Determinism is the
   requirement; insertion order is the one both backends can reproduce
   without imposing an ordering on `K`.

### Rendering

Both backends must agree byte for byte, so rendering is fixed now and
unconditional — no "hide weights when they are all 1", which would make the
output depend on the data:

```
{a: [(b, 1), (c, 2)], b: [(c, 1)], c: []}
```

That is deliberately the rendering `Map<K, List<(K, Int)>>` already
produces, so a reader who knows how maps and tuples print can predict a
graph's output without learning a new shape.

### `ir.GraphValue`

New file `ir/graph.go`, modelled on `ir/collections.go`'s `MapValue`:

```go
type GraphValue struct {
    nodes []Value                // insertion order
    index map[any]int            // KeyOf(node) -> position in nodes
    adj   [][]GraphEdge          // parallel to nodes
}
type GraphEdge struct { To int; W int64 }
```

Storing adjacency as `int` indices rather than keys is what makes the
algorithms cheap and makes the compiled representation an obvious mirror.
Methods: `AddNode`, `AddEdge`, `Nodes`, `Neighbors`, `EdgesOf`, `HasEdge`,
`Weight`, `Degree`, `Len`, `Clone` (functional update, like
`SparseValue.Clone`).

The existing runtime helpers cover the algorithms with nothing new:
`ir.Queue` (`ir/deque.go`) for BFS, `ir.PQ` (`ir/pq.go`) for Dijkstra,
`ir.UnionFind` (`ir/unionfind.go`) for components.

### Builtins

Named to sit beside what exists (`neighbors4`/`neighbors8` are the grid
forms; `size`/`contains` are already overloaded across Text/List/Set/Map and
gain a Graph case rather than a new name):

| Builtin | Type |
|---|---|
| `graph(edges)` | `List<(K,K)>` \| `List<(K,K,Int)>` → `Graph<K>` |
| `emptygraph(witness)` | `K -> Graph<K>` (witness form, like `emptyset`) |
| `addnode(g, k)` | `Graph<K> × K -> Graph<K>` |
| `addedge(g, a, b)` / `addedge(g, a, b, w)` | → `Graph<K>` |
| `deledge(g, a, b)` | → `Graph<K>` |
| `nodes(g)` | `Graph<K> -> List<K>` |
| `edges(g)` | `Graph<K> -> List<(K, K, Int)>` |
| `neighbors(g, k)` | `Graph<K> × K -> List<K>` |
| `edgesof(g, k)` | `Graph<K> × K -> List<(K, Int)>` |
| `hasedge(g, a, b)` | → `Bool` |
| `weight(g, a, b)` | → `Int`, **errors** if absent |
| `weightor(g, a, b, d)` | → `Int`, total (mirrors `getor`) |
| `degree(g, k)` | → `Int` |
| `flipedges(g)` | `Graph<K> -> Graph<K>` (every arc reversed) |
| `subgraph(g, ks)` | `Graph<K> × List<K> -> Graph<K>` |
| `size(g)` | node count — extends the existing `size` |
| `contains(g, k)` | node membership — extends the existing `contains` |

Verified against `typecheck.Builtins` (`typecheck/typecheck.go:169-213`):
none of these names is taken. `reverse` and `transpose` are, which is why
arc reversal is `flipedges`.

All updates are **functional** — `addedge` returns a new graph, like
`insert`/`put`/`setat`. The optimizer's existing `CallExpr.InPlace`
dead-receiver analysis is what recovers the copy when it is safe, and Graph
should be added to it (B.8) rather than getting a mutable escape hatch.

### Primitives

Two new coercions, and the payoff — teaching the existing search vocabulary
a new input shape:

| Primitive | Keyword | Step |
|---|---|---|
| `Convert To Graph` | Channeled Energy | `List<(K,K)>` \| `List<(K,K,Int)>` \| `Map<K,List<K>>` → `Graph<K>`, with `Mode: Undirected` |
| `Convert To Edges` | Channeled Energy | `Graph<K> -> List<(K, K, Int)>` — the way back out, mirroring `Convert To Rows`/`Convert To Entries` |
| `BFS` | Domain Expansion | **+** `Graph<K>` with `From:` → `Map<K, Int>` (unreachable absent, not `-1` — a map has no "every node" obligation the way a grid does) |
| `Dijkstra` | Domain Expansion | **+** `Graph<K>` with `From:` → `Map<K, Int>` |
| `Connected Components` | Domain Expansion | **+** `Graph<K>` → `Int` (weakly connected: treat arcs as undirected, via `ir.UnionFind`) |
| `Topological Sort` | Domain Expansion | **+** `Graph<K>` — a third input shape beside the two `topoInputShape` already accepts (`prims/toposort.go:50`) |
| `Shortest Path` | Domain Expansion | **new**: `Graph<K>` × `From:` × `To:` → `List<K>`, empty when unreachable |

`Explore` is deliberately left alone. It searches *implicit* state spaces;
that is a different job, and a `Graph` argument would duplicate `BFS`.
Adding a `neighbors(g, s)` call inside an `Explore` lambda already bridges
the two, and that is the intended composition.

## B.1 The type kind

`ir/ir.go`: `KGraph` in the `TypeKind` const block (line 25-37, after
`KSparse`); `func Graph(elem *Type) *Type`; `String()` → `"Graph<K>"`
(line 156); `Equal` — add `KGraph` to the `KList, KSet, KGrid, KSparse`
elem-comparing case (line 173); `Keyable` unchanged (a graph is not
keyable, which the existing default-`false` already gives).

`prims/types.go`: `genericArity["Graph"] = 1`; a `case "Graph"` in
`lowerTypeExpr` (line 88) that rejects a non-keyable node type with the same
wording `Set` uses (line 96); `Graph<K>` added to `knownTypeNames()`.

Test: `ir/type_test.go` for `String`/`Equal`; `prims/types_test.go` for the
written form and the non-keyable rejection.

## B.2 The runtime value

`ir/graph.go` as designed in B.0, with `ir/graph_test.go` covering
insertion order, functional `Clone`, self-loops, parallel edges (last write
wins on weight — state it and test it), and a node added by `addedge` that
was never `addnode`d.

Then the four value-level switches, each of which is a silent-degradation
site if missed:

- `ir/values.go:207` `writeValue` — the rendering from B.0, through the
  bounded `valueWriter` so a huge graph stops at the limit like every other
  composite.
- `ir/values.go:345` `FormatValueTyped` — the typed twin (record-ordering
  reason does not apply to graphs, but the function must not fall through to
  `FormatValue` for a type it should handle).
- `ir/values.go:571` `DeepEqual` — node set and adjacency equal; **order-
  insensitive**, because two graphs built by different insertion orders are
  the same graph. This is a deliberate divergence from how `MapValue`
  compares and needs a comment saying why.
- `ir/values.go:664` `DescribeValue` — `"a Graph"` for error messages.
- `ir/order.go:45` `Compare` — graphs are **not** orderable; make it an
  explicit refusal rather than a fallthrough, so `Sort` over `List<Graph<K>>`
  fails at resolve time with a clear message.
- `ir/values.go:519` `KeyOf` — must refuse, consistent with `Keyable`.

## B.3 Typecheck for the builtins

`typecheck/typecheck.go`: names into the `Builtins` slice (line 169), arities
into `builtinArity` (line 221), and a case each in `callType`. Extend the
existing `size` and `contains` cases rather than adding names.

`weight` is the only partial one (errors on a missing edge); `weightor` is
its total twin. That pairing already exists as `get`/`getor` and the
optimizer's `isTotal` list (`optimizer/walk.go:190`) must learn which is
which — put `weightor`, `hasedge`, `degree`, `nodes`, `edges`, `neighbors`,
`edgesof`, `size`, `contains` in the total set and leave `weight` out.

## B.4 Interpreter

`eval/eval.go`: a case per builtin in `evalCall` (line 195). Straightforward
delegation to the `ir.GraphValue` methods from B.2.

## B.5 Codegen — the runtime type

`codegen/runtime.go`: `declGraph`, a `dmGraph[K comparable]` mirroring
`ir.GraphValue` — modelled directly on `declSparse` (line 482) and the
insertion-ordered `dmMap`.

`codegen/types.go`:
- `goType` — `case ir.KGraph` → `dmGraph[K]` (beside `KSparse`, line 62).
- `canonicalKey` — `"Graph<" + … + ">"` (line 96). This is the intern key,
  and getting it wrong means two Domain types sharing one Go struct.
- `fmtFunc` — the B.0 rendering, byte-identical to `writeValue`.

`codegen/eq.go` — a `KGraph` case producing the same order-insensitive
equality `DeepEqual` implements. This is the one with real potential to
diverge from the interpreter, so it gets its own oracle test.

## B.6 Codegen — the builtins

`codegen/expr.go`: a case per builtin (the text group at line 753 is the
model). Element-type-dependent helpers get interned through
`g.listFn`/`codegen/listfns.go` exactly as `sortFn`/`pairFn` do.

**Land B.5+B.6 before B.7.** A primitive whose expression-layer equivalent
does not compile is a primitive that cannot be oracle-tested.

## B.7 Primitives

New file `prims/graph.go` for `Convert To Graph`, `Convert To Edges` and
`Shortest Path`; edits to `prims/search.go` (BFS, Dijkstra, Connected
Components) and `prims/toposort.go` (a third shape in `topoInputShape`,
line 50).

Registry (`prims/prims.go:291`) is **matcher-ordered, specific before
general**. `Convert To Graph` does not contain the word `Grid`, so it does
not need the `convertToSparseGrid`-before-`convertToGrid` treatment (line
328), but place it beside the other coercions and let
`prims/infer_test.go` confirm no phrase is stolen.

`prims/catalog.go` entries for the three new primitives, and updated
`Signature` strings for the four extended ones —
`TestCatalogCoversRegistry` fails otherwise.

New codegen file `codegen/graphgen.go` for the primitive lowerings, beside
`sparsegen.go`.

## B.8 Optimizer

- `optimizer/walk.go` — the `isTotal` additions from B.3.
- The `InPlace` dead-receiver pass: teach it `addnode`/`addedge`/`deledge`
  so a graph built in a `Fold` does not copy per edge. This is the
  difference between a linear and a quadratic build and should be measured,
  not assumed — add a `bench/` case.
- No new algorithm-substitution pass. `Convert To Graph` + `BFS` is already
  the named-algorithm form the optimizer would rewrite *to*.

## B.9 Docs

- `docs/data-model.md` — the type, its ordering rule, its rendering, and why
  it is not keyable.
- `docs/ref-coercions.md` — `Convert To Graph` / `Convert To Edges`, with
  runnable examples.
- `docs/ref-expansions.md` — the extended `BFS`/`Dijkstra`/`Connected
  Components`/`Topological Sort` signatures, and `Shortest Path`.
- `docs/ref-builtins-collections.md` — the builtin group.
- `docs/aoc-toolbox.md` — the "parse a dependency list, then ask a graph
  question" recipe, which is the motivating use.
- `go test ./docs -update` to regenerate `primitives.json`.
- Editors: `Graph` into both type lists
  (`domain.tmLanguage.json:249`, `domain.vim:74`) and the three new
  primitive phrases into both primitive alternations — note those are
  **longest-first** alternations, so `Convert To Graph` goes near `Convert To
  Grid`, not at the end.

## B.10 End-to-end

- An `examples/` program that parses an edge list, builds a graph and
  answers a shortest-path question — auto-discovered by the example runner,
  with `.input`/`.expected`.
- Oracle tests in both optimizer modes for: each builtin, each new/extended
  primitive, graph rendering at a `Reveal`, and graph equality inside an
  `Iterate Until Fixed Point`.

---

## Part B outcome

All ten tasks shipped. `go test ./...` is green, `codegen` is clean under
`-race`, and both backends agree on every graph program.

`Graph<K>` is a new `ir.Kind` with fifteen expression builtins, two
coercions (`Convert To Graph`, `Convert To Edges`), one new primitive
(`Shortest Path`), four existing primitives taught a new input shape
(`BFS`, `Dijkstra`, `Connected Components`, `Topological Sort`), and the
in-place rewrite.

### Where the plan was wrong

1. **B.3–B.6 were never separable.** The plan sequenced typecheck, eval and
   codegen as three tasks. Adding a name to `typecheck.Builtins` trips three
   pinning tests at once: every builtin must appear in the reference tables
   (`TestEveryBuiltinIsDocumented`), the quoted builtin count must match, and
   every `ref-*.md` section needs **two runnable examples**
   (`TestEveryPrimitiveHasTwoRunnableExamples`). A runnable example needs both
   backends working. So the repo enforces "a builtin arrives whole or not at
   all" exactly as it enforces "both backends or neither" — the four tasks
   landed as one commit.

2. **`Compare` and `KeyOf` needed no Graph case.** B.2 asked for explicit
   refusals. `ir.Ordered` and `ir.Keyable` are both default-deny, so a graph
   already cannot reach a `Sort`, a `Map` key or a `Set` element, and
   `Compare`'s fallthrough is documented as deliberate. A test pins both
   predicates instead of adding code that would contradict them.

3. **B.8 was not optional, and not last.** The plan filed the in-place
   rewrite as a performance follow-up. But the builtins page written in
   B.3–B.6 claimed a `Fold`-built graph is "linear in its writes rather than
   quadratic", which was false until the rewrite landed — a 12,000-edge fold
   cloned the graph 12,000 times. Writing the documentation is what surfaced
   it. Measured after: 0.03 s against 2.1 s with `--no-optimize`.

4. **Editor grammars are generated now.** The plan said to hand-edit two
   type lists and two primitive alternations. Since this branch's merge with
   `main`, both files are generated and pinned — `go test ./editors -update`
   is the whole task, and hand-editing would be reverted by the next run.

### Confirmed by implementation

- Going through the shared adjacency map for `Topological Sort` (rather than
  walking the graph directly) keeps the tie-breaking identical across all
  three input shapes — a graph and the edge list it was built from sort the
  same way. Pinned by a `prims` test over three inputs.
- The rendering and the order-insensitive equality are each written twice and
  are the two places the backends could diverge, as the plan predicted. Both
  have their own oracle program.

### Found late

5. **`From:`/`To:` were not available.** The plan's `Shortest Path` sketch used
   them. `From:` already means "these channels" for every consumer that takes
   one, and the channel machinery claims it before a primitive's `Build` runs —
   the resolver answers "From: must name at least one channel". The endpoints
   are `Start:`/`Goal:`, which also reads better beside the grid searches'
   existing "start". Both had to be added to `prims.argNames`, which a repo
   test enforces so the linter can suggest them on a typo.

6. **The twenty-first example found a bug in the docs tests.** Prose counts of
   runnable programs are pinned by a number-word regex whose alternation
   listed `twenty` before `twenty-one`. Go's regexp is leftmost-first, so the
   shorter alternative always won and `twenty-one` could never be recognized,
   even though the words table had carried the entry all along. Longest
   alternative first.

7. **Editor grammars and `primitives.json` are generated.** `go test ./editors
   -update` and `go test ./docs -update` are the whole of what the plan
   described as hand-editing two type lists and two alternations.

## Testing summary

Beyond the per-task tests above, three things are worth calling out because
they are where this kind of change actually breaks:

1. **Rendering parity.** `ir.writeValue` and `codegen.fmtFunc` are two
   handwritten implementations of one format. Every composite type in the
   repo has an oracle test for this; graphs need one with ≥2 nodes, a
   self-loop, an isolated node and a non-1 weight.
2. **Equality parity.** Order-insensitive graph equality is implemented
   twice (`ir.DeepEqual`, `codegen/eq.go`). Test two graphs built in
   different insertion orders comparing equal **in both backends** — this is
   the single most likely divergence in Part B.
3. **The formatter's round trip.** For Part A,
   `Format(Format(x)) == Format(x)` over every repo program, plus the
   existing repo-wide `--check` cleanliness test.

## Out of scope

- Map and set literals (`{1, 2}`, `{"a": 1}`) — reserved by A.0's error
  message, not implemented.
- Multi-line brace literals (A.3).
- `Graph<K, W>` with a non-`Int` weight.
- Undirected graphs as a distinct type — `Mode: Undirected` builds both arcs.
- Node or edge *payloads* beyond the weight. A graph whose nodes carry data
  is `Graph<K>` plus a `Map<K, V>` alongside, which composes with what
  exists.
- `Explore` taking a `Graph` (B.0 explains why).
- A minimum-spanning-tree or max-flow primitive. `ir.UnionFind` makes Kruskal
  cheap to add later; it is not part of the AoC-shaped vocabulary this
  language targets.

## Risks

| Risk | Mitigation |
|---|---|
| A new `ast.Expr` node silently skipped by a walker | Avoided outright — Part A desugars to `CallExpr` (A.0) |
| Interpreter/compiled divergence on graph rendering or equality | Oracle tests in both optimizer modes, listed above |
| `canonicalKey` collision interning two Domain types to one Go struct | B.5; the `Sparse` precedent and `codegen/types.go:74-84`'s comment explain the failure mode |
| Registry phrase capture (`Convert To Graph` vs `Convert To Grid`) | B.7; `prims/infer_test.go` |
| Functional graph updates making a `Fold`-built graph quadratic | B.8, with a `bench/` case rather than an assumption |
| Part B's size | It is a milestone, not a task. B.1→B.10 are separately committable and `go test ./...` stays green at each. |
