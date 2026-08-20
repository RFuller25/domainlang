# Primitive reference

Every operation in Domain is either a primitive (implemented in Go, listed
here) or a Shikigami composed from primitives. Types are checked at resolve
time: each primitive states the input type it requires and the output type it
produces, and a mismatch is a positioned error before anything runs.

Notation: `T`, `U`, `K` are type variables; "keyable" means `Int`, `Text`,
or a Tuple/Record built from keyable types (so points key Maps and Sets)
(the legal Map key / Set element types). Lambda-consuming primitives infer
their output types from the lambda body via the type checker.

Every primitive below is supported by **both** backends (interpreter and
compiled binary) with identical success output — the AoC-toolbox additions
included, each pinned by an interpreter-vs-binary oracle test. See
[compiler.md](compiler.md).

---

## The pages

The reference is one page per keyword class, because that is the division the
language already makes: a primitive's keyword tells you what kind of step it
is, and the classes are what a reader is choosing between.

| Page | Covers |
|---|---|
| [ref-sources.md](ref-sources.md) | `Cursed Energy` — reading the input |
| [ref-transforms.md](ref-transforms.md) | `Cursed Technique` over lists and text: splitting, mapping, filtering, windowing, parsing |
| [ref-transforms-grid.md](ref-transforms-grid.md) | `Cursed Technique` over `Grid`, `Sparse` and `Map`: cells, cropping, rotation, entries |
| [ref-coercions.md](ref-coercions.md) | `Channeled Energy` — changing a value's type |
| [ref-reductions.md](ref-reductions.md) | `Maximum Technique` — many values to one, including the channel consumers |
| [ref-expansions.md](ref-expansions.md) | `Domain Expansion` — named algorithms the optimizer may substitute |
| [ref-structure.md](ref-structure.md) | `Reverse Cursed Technique`, and the statements documented in [language.md](language.md) |

Every primitive is also in the site's **primitive index**, which is filterable
and links straight to the heading here that documents it.

Two examples that run accompany every primitive on these pages; they are
executed against their printed output in both optimizer modes and in both
backends, so nothing on them can drift from what the language does.
