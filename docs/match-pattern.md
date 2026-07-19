# Design note: Match Pattern (v0.2)

`Match Pattern` is the parsing workhorse — most of AoC's input handling flows
through it, so it is designed deliberately here rather than improvised during
implementation. This note pins the syntax, the output shapes, the lowering
strategy, and the cases v0.2 must cover. Scope is intentionally bounded to the
anchor problems plus a few near neighbors; anything beyond is a v0.3 concern.

> **Status: implemented.** Template parsing/typing lives in the `pattern`
> package; the pipeline primitive lives in `prims/match.go`. `Mode:` is inferred
> from the input type when omitted. Downstream predicates use the expression
> layer's `and`/`or` connectives.

## Decision recap

- **Syntax:** typed-hole templates (not raw regex, sscanf, or a combinator DSL).
- **Hole types (v0.2):** `{int}`, `{word}`, `{text}`.
- **Named holes → Record; positional holes → fixed-arity tuple/list.**
- **`Mode:`** selects single-match vs match-all.

## Syntax

The pattern is a template string supplied via `Using:`. Literal characters
match themselves; `{...}` introduces a hole.

```domain
Cursed Technique: Match Pattern
    Using: "{a:int}-{b:int},{c:int}-{d:int}"
```

Hole forms:

| Form | Meaning | Captures as |
|---|---|---|
| `{int}` | one integer (`-?[0-9]+`) | `Int`, positional |
| `{word}` | one run of non-space word chars | `Text`, positional |
| `{text}` | the rest of the field, greedy to the next literal | `Text`, positional |
| `{name:int}` | named integer | `Int`, field `name` |
| `{name:word}` | named word | `Text`, field `name` |
| `{name:text}` | named text | `Text`, field `name` |

Mixing named and positional holes in one pattern is an error (keeps the output
shape unambiguous: either a Record or a tuple, never a blend).

### Modes

`Mode:` controls how the pattern applies to the current value:

| `Mode:` | Input | Output |
|---|---|---|
| `One` (default) | `Text` | one Record / tuple |
| `Each` | `List<Text>` | `List<Record>` / `List<tuple>` |

`Each` is the common case — split the input into lines first, then
`Match Pattern, Mode: Each` to parse every line.

## Output shapes

- **Positional holes** → a fixed-arity tuple, represented as a `List` of the
  captured values (access by index via existing list ops). Arity and element
  types are known statically.
- **Named holes** → a `Record` with those field names and types; access with
  `x.name` in the expression layer (`FieldAccess` already exists in the AST).

Example results:

```
"{int}-{int}"  on "2-4"        -> [2, 4]            (tuple: List<Int>)
"{a:int}-{b:int}" on "2-4"     -> {a: 2, b: 4}      (Record {a:Int, b:Int})
"{dir:word} {n:int}" on "forward 5" -> {dir: "forward", n: 5}
```

## Type checking

`Match Pattern` resolves at lower-time to a node whose `Out` type is computed
from the template:

- positional → `List<T>` when all holes share a type, otherwise a `Tuple`
  type carrying a per-position element-type vector (new IR type, see
  `docs/data-model.md`);
- named → a `Record` type with the declared field set.

Because the template is a literal known at resolve time, the output type is
fully determined statically — no inference needed. A pattern that fails to parse
at resolve time (malformed `{...}`, unknown hole type) is a resolve error with a
position.

## Lowering strategy

Translate the template to a matcher once, at lower-time:

1. Tokenize the template into a sequence of literals and holes.
2. Compile to a regular expression with one capture group per hole
   (`{int}` → `(-?\d+)`, `{word}` → `(\S+)`, `{text}` → `(.*)`), anchored to the
   whole field. Named holes map to named groups for clarity.
3. At interpret time, run the regex on the current value (or each element under
   `Mode: Each`), convert captures to their hole types, and assemble the
   Record/tuple.

Using Go's `regexp` keeps v0.2 small and correct. The template surface stays
themed and friendly; the regex is an internal implementation detail the user
never sees. If a future problem needs something regex can't express cleanly,
revisit with a hand-written matcher — but not in v0.2.

## Error behavior

- **No match** (a line that doesn't fit the template): a runtime `RuntimeError`
  naming the stage, the offending input line, and the template. AoC inputs are
  regular, so a non-match almost always means a real bug — fail loudly, in the
  spirit of Binding Vows.
- **Conversion failure** (e.g. `{int}` capturing a value out of `int64` range):
  runtime error with the offending text.

## Cases v0.2 must cover

These are drawn from the survey; the first is the anchor.

| Input | Template | Result |
|---|---|---|
| `2-4,6-8` (2022 D4) | `"{a:int}-{b:int},{c:int}-{d:int}"` | `{a:2,b:4,c:6,d:8}` |
| `forward 5` (2021 D2) | `"{dir:word} {n:int}"` | `{dir:"forward", n:5}` |
| `move 1 from 2 to 1` (2022 D5) | `"move {n:int} from {src:int} to {dst:int}"` | `{n:1,src:2,dst:1}` |
| `1-3 a: abcde` (2020 D2) | `"{lo:int}-{hi:int} {ch:word}: {pw:text}"` | `{lo:1,hi:3,ch:"a",pw:"abcde"}` |
| `2x3x4` (2015 D2) | `"{int}x{int}x{int}"` | `[2,3,4]` |

## Explicitly out of scope for v0.2

- Alternation / optional holes / repetition inside a template.
- Recursive or nested patterns.
- Custom hole types beyond `{int} {word} {text}`.
- Whitespace-flexible matching (templates match literally; pre-split/trim first).
