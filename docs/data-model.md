# The data model

Every value Domain can hold, how it is represented in the type checker
(`ir.Type`), how it looks at runtime (`ir.Value`), how it is constructed and
accessed, and how it renders. The scalar core is `Int`, `Float`, `Text` and
`Bool`; the composites are **List**, **Tuple**, **Record**, **Map**, **Set**,
**Grid** and **Sparse**.

Everything described here is implemented in both backends. For what each
primitive does with these types see [primitives.md](primitives.md); for the
builtins that read them see [expressions.md](expressions.md).

## Type representation (`ir.Type`)

| Kind | Carries | Prints as |
|---|---|---|
| `KInt` | — | `Int` |
| `KFloat` | — | `Float` |
| `KText` | — | `Text` |
| `KBool` | — | `Bool` |
| `KList` | `Elem *Type` | `List<T>` |
| `KTuple` | `Elems []*Type` (fixed arity) | `(T1, T2, …)` |
| `KRecord` | ordered `Fields []Field{Name, Type}` | `{a:Int, b:Int}` |
| `KMap` | `Key *Type` (keyable), `Val *Type` | `Map<K,V>` |
| `KSet` | `Elem *Type` (keyable) | `Set<T>` |
| `KGrid` | `Elem *Type` | `Grid<T>` |
| `KSparse` | `Elem *Type` | `Sparse<T>` |

Equality rules:

- **Tuple** equal iff same arity and element-wise equal types.
- **Record** equal iff same field names *and* types. (Structural, order of
  declaration normalized — sort fields by name for comparison.)
- **Map/Set/Grid/Sparse** equal iff key/element types equal.

## Runtime representation (`ir.Value`)

`ir.Value` is `any`. Runtime structs are suffixed `*Value` to avoid colliding
with the same-named static type constructors (`ir.Record(...)`, `ir.Map(...)`,
etc.).

| Type | Go runtime value |
|---|---|
| Tuple | `[]Value` (fixed length; same as a list at runtime) |
| Record | `*RecordValue` — `{ Fields []string; Vals map[string]Value }` (keeps field order) |
| Map | `*MapValue` — keys in insertion order; internally keyed via `ir.KeyOf` (scalars raw, composites canonically encoded) |
| Set | `*SetValue` over keyable values via `ir.KeyOf` (insertion order preserved) |
| Grid | `*GridValue` — `{ Rows, Cols int; Cells []Value }` row-major |
| Sparse | `*SparseValue` — set-cells map keyed by `(row, col)` + a default value + exact bounds |

Tuples reuse `[]Value` so existing list ops (index access) work on them; the
distinction lives only in the static type. These are implemented in
`ir/collections.go` with `Set`-op helpers (`SetIntersect`/`Union`/`Difference`)
and `Grid` neighbor walks.

## Records

- **Produced by** `Match Pattern` with named holes (see
  [match-pattern.md](match-pattern.md)).
- **Accessed by** `x.field` in the expression layer. An unknown field is a
  positioned error; the static typer resolves `x.field` to the field's type at
  resolve time, so a typo never survives to run time.
- **Structurally typed:** a `{a:Int, b:Int}` flows wherever that record type is
  expected; the type checker compares field sets.

## Maps and Sets

- **Map keys** are any *keyable* type: `Int`, `Text`, or Tuples/Records
  built from keyable types — so a `Map<(Int, Int), V>` keyed by points models a
  sparse grid directly. Lists stay unkeyable (their compiled representation is
  a Go slice, which cannot be a map key). At runtime `ir.KeyOf` gives every
  keyable value a canonical comparable key that agrees with `ir.DeepEqual`
  (records key by sorted field name, matching `DeepEqual`'s order-insensitive
  comparison); in compiled binaries tuples/records are comparable Go structs,
  so `dmMap`/`dmSet` key on them natively.
- **`Group By`** (`Maximum Technique`, `Using:` key lambda) returns
  `Map<K, List<V>>`: each key maps to the list of elements that produced it.
- **Frequency counting** is `Group By` then map the values to their lengths, or
  a dedicated `Count By` returning `Map<K, Int>`.
- **Set** is a dedup'd, insertion-ordered collection with three primitives:
  `Intersect`, `Union`, `Difference` (`Maximum Technique`). A `Set<T>` is built
  from a `List<T>` with `Channeled Energy: Convert To Set`, and `tolist` in the
  expression layer converts back. Modeling as an ordered set (not a
  list-to-unit map) keeps rendering and set-ops simple.

Access/rendering is via primitives, not literals — v0.2 has no Map/Set literal
syntax (deferred; AoC builds these from inputs, not from literals).

## Grids

- **Construction** (`Channeled Energy: Convert To Grid`):
  - from `List<Text>` → `Grid<Text>` of single characters (char grid), or
  - from `List<List<T>>` → `Grid<T>` (e.g. the digit grid of 2022 D8).
- **Indexing** is 0-based `(row, col)`.
- **Out-of-bounds is an error, not a sentinel.** `at(g, r, c)` on a dense grid
  outside its extent is a clean positioned runtime error
  (`at: position (5, 5) out of range (grid 2x2)`). Guard with `inbounds(g, r,
  c)` — it pairs with `at` under short-circuit `and` — or use the
  `neighbors4`/`neighbors8` builtins, which only ever return in-bounds
  coordinates. (`Sparse<T>` is the total one: `at` over a sparse grid reads
  the default rather than failing.)
- **Neighbor count** (`4` orthogonal vs `8` with diagonals) is chosen by which
  builtin you call — `neighbors4`/`neighbors8` for a dense grid,
  `around4`/`around8` for a bare point — not by a `Mode:` argument.

Grids are reached through the `Cursed Technique` primitives `Map Cells`,
`Find Cells`, `Transpose`, `Subgrid`, `Pad Grid`, `Rotate Grid` and
`Flip Grid`, the `Maximum Technique: Count Cells` reduction, and the
expression-layer builtins `at`, `inbounds`, `row`, `col`, `rows`, `cols`.
Pathfinding and flood fill are `Domain Expansion` primitives in their own
right — `BFS`, `Dijkstra`, `Flood Fill` and `Connected Components`; see
[primitives.md](primitives.md).

## Sparse grids

`Sparse<T>` is the dedicated nested/sparse grid: an **unbounded 2D plane**
addressed by `(row, col)` — negative coordinates included — where every
position holds a **default value** until explicitly written. Only written
("set") cells are stored, so memory is proportional to the data, not the
bounding box. It complements `Grid<T>` (dense, rectangular, bounds-checked):
sparse is for point clouds, cellular automata, plotters, and anything whose
extent is unknown up front.

The contract (locked in `ir/sparse.go`; both backends follow it exactly):

- **`at` is total.** Reading any coordinate of the infinite plane returns
  the stored value or the default — there is no out-of-bounds.
- **Writes never remove.** A cell written with the default value is still
  *set*: `has` reports true, and it participates in bounds, `cells`, and
  iteration. Bounds are therefore exact and only ever grow.
- **Iteration is sorted row-major** (by row, then column) — a sparse grid is
  geometry, not a log, so insertion order is deliberately *not* preserved
  (unlike Map/Set). `Find Cells`, `Map Cells`, `Count Cells`, and rendering
  all visit set cells in this canonical order.
- **`Map Cells` transforms the whole plane**: the lambda maps the default
  too (that is what makes `Sparse<T> -> Sparse<U>` well-defined). The
  positional `(grid, row, col)` lambda form is dense-only — the default has
  no position.
- **Not keyable**, like Grid.

**Construction** — `Channeled Energy: Convert To Sparse Grid` with a
`Default:` literal (Int or Text; its type fixes the element type):

- from `Grid<T>`: cells *different from the default* become set cells
  (that is the point of sparsifying);
- from `Map<(Int, Int), V>`: every entry becomes a set cell;
- from `List<(Int, Int)>` or `List<List<Int>>` (two ints per row — the
  shape `Match Pattern "{int},{int}"` produces) plus a `Mark:` literal:
  every point is set to the mark.

The expression layer can also build one from nothing: `sparse(default)`
makes an empty plane (any element type), and `put(g, r, c, v)` is a
functional update.

**Densification** — `Channeled Energy: Convert To Grid` over a `Sparse<T>`
materializes the bounding box as a dense `Grid<T>`, translated so
`(minrow, mincol)` lands at `(0, 0)` and default-filled — the standard way
to *print the picture* (origami letters, Life boards). The empty sparse
grid densifies to the 0×0 grid. **Unbounded**: `ir.MaxSparseDense` is zero by
default, because the old 4,000,000-cell ceiling refused plots a machine had
room for. Two far-apart cells still imply a huge dense box, so the failure
mode there is memory pressure rather than a clean refusal; a box past what any
machine could allocate is still a clean error rather than a panic.

**Rendering** — the raw `Sparse<T>` renders as a map-style listing of set
cells in sorted row-major order with point keys (`{[0, 0]: #, [1, 2]: #}`),
exactly like a `Map<(Int, Int), V>` — deliberately size-proportional so
`Reveal` can never materialize a huge bounding box. Densify first when the
picture is what you want. `FormatShort` says `Sparse RxC (N set)`.

**Builtins** (see [expressions.md](expressions.md)): `sparse`, `put`, `has`,
`cells`, `minrow`/`maxrow`/`mincol`/`maxcol`, and the `at` overload.

The acceptance tests are `challenges/11_game_of_life.domain` (a glider on
the infinite plane), `12_origami.domain` (fold-and-plot), and
`13_minesweeper.domain` (neighbor counts) — see
[../challenges/README.md](../challenges/README.md).

## Type inference interaction

A primitive's output type can depend on its `Using:` lambda, which the
`typecheck` package computes from the lambda body against the parameter types
the primitive binds. There are no type annotations on primitives: a body that
cannot be typed is a positioned resolve error, not a request for a hint.

- `Match Pattern` output is fully determined by the (literal) template — no
  inference.
- `Group By` output `Map<K, List<V>>`: `V` is the input element type, `K` the
  key lambda's result type.
- `Map Each` output `List<U>`: `U` is the lambda's result type.

A `Using:` may also be written as an
[indented pipeline](expressions.md#pipeline-bodies--a-using-that-needs-a-primitive)
rather than an expression; the body's output type is then the lambda's result
type, and everything above reads the same.

## Rendering

`FormatValue` renders a value in full, `FormatShort` the summary the REPL and
tracing use:

- Tuple → `(v1, v2, …)`
- Record → `{a: v1, b: v2}` (declared field order)
- Map → `{k1: v1, k2: v2}` (insertion order; truncate in `FormatShort`)
- Set → `{v1, v2, …}` (insertion order; truncate)
- Grid → rows joined by newlines for `FormatValue`; `Grid RxC` summary for
  `FormatShort`
- Sparse → `{[r, c]: v, …}` (set cells, sorted row-major); `Sparse RxC
  (N set)` for `FormatShort`

## Floats

`Float` is a 64-bit IEEE float, runtime value `float64`, added end to end:

- **Literals**: `3.25` — digits, a dot, and at least one trailing digit, so
  dotted file targets (`Cursed Energy: 2.txt`) still lex as before. Floats
  appear in the expression layer and named arguments; the themed phrase
  layer stays integer-only.
- **Promotion** (the tower's single rule): mixing `Int` with `Float` in
  arithmetic, comparison, or `=` computes in `Float`. There is no implicit
  narrowing — `floor`/`ceil`/`round` are the explicit way back to `Int`.
- **Builtins**: `tofloat` (Int/Float/Text), `floor`/`ceil`/`round`
  (Float→Int), `sqrt` (negative input errors cleanly); `abs`, `sum`, `min`,
  `max` are polymorphic over Int/Float.
- **Primitives**: `Channeled Energy: Convert To Floats` (from `List<Text>`
  or `List<Int>`, flat or Each-List nested); `Sum`, `Max`, `Min`,
  `Product`, and `Sort` accept `List<Float>`.
- **Not keyable**: floats stay out of Map keys, Sets, `Unique`, and
  `Group By` (NaN and ±0.0 make float identity treacherous); the existing
  keyability errors apply.
- **Optimizer**: float pipelines are exempt from the int-specialized
  rewrites (quickselect fusions, reorder elision, lambda algebra) because
  float addition is not associative and NaN is unordered — a float `Sort`
  runs exactly as written.
- **Rendering**: shortest round-trip form (`strconv.FormatFloat('g', -1)`),
  byte-identical between the interpreter and compiled binaries.

## Deliberate omissions

Two things the model does not have, by decision rather than by backlog:

- **Map/Set/tuple literal syntax.** These are built from inputs, not written
  out — `tuple(...)` and `list(...)` cover the cases where a literal would
  have been wanted, and a Map or Set comes from `Group By`, `Count By`, or
  `Convert To Set`.
- **Big integers.** `Int` is int64 throughout. Arbitrary precision would box
  every arithmetic operation in both backends, and no anchor problem needs it;
  the operations that *can* overflow say so instead (`factorial` errors past
  `20!`, `choose` is computed multiplicatively to stay in range).
