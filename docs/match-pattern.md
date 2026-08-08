# Match Pattern: the typed-hole template language

`Match Pattern` is the parsing workhorse — most input handling flows through
it. This page is the full reference for the template syntax, the output shapes
it produces, how it is typed, how it is lowered by each backend, and how it
fails.

Template parsing and typing live in the `pattern` package; the pipeline
primitive lives in `prims/match.go`; the compiled lowering is
`codegen/matchgen.go`. The primitive itself is summarized in
[primitives.md](primitives.md#match-pattern--text---v-or-listtext---listv).

## The shape of it

- **Syntax:** typed-hole templates — not raw regex, `sscanf`, or a combinator
  DSL. The template is a literal, so the output type is known statically.
- **Hole types:** `{int}`, `{hex}`, `{digits}`, `{word}`, `{char}`, `{text}`.
- **Repetition:** `{ns:int+ sep=", "}` captures one or more elements as a
  `List`.
- **Groups:** `[? … ]` makes a run of the template optional; `( … )+` repeats
  one, yielding a `List` of its values.
- **Named holes → Record; positional holes → fixed-arity tuple/list.**
- **`Mode:`** selects single-match, match-all, keep-what-fits or find-anywhere,
  and infers the first two from the input type when omitted.
- **`Case:`** carries several templates on one stage, in priority order, with a
  `kind` field naming the one that matched.

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
| `{hex}` | a run of hex digits (`[0-9a-fA-F]+`) | **`Int`**, parsed base 16 |
| `{digits}` | a run of decimal digits (`[0-9]+`) | **`Text`**, leading zeros kept |
| `{char}` | exactly one character | `Text` |
| `{~}` | a run of whitespace | *nothing* — see below |

`{hex}` captures an `Int` because that is what a hex run is for: `#70c710`
arrives as `7390992` rather than as text a later `fromhex` has to convert.
`{digits}` is the opposite choice for the opposite reason — a run of digits
whose **leading zeros matter** is the only reason to prefer it over `{int}`,
and an `Int` cannot hold them.

`{~}` matches one or more whitespace characters and owns **no field**: a gap is
structure, not data. It is what column-aligned input needs, since a literal
space in a template matches exactly one:

```domain
Cursed Technique: Match Pattern
    Using: "Register {r:word}:{~}{v:int}"
```

matches `Register A: 12` and `Register A:   12` alike, and refuses
`Register A:12` — the template author wrote a gap, so there is a gap.

Mixing named and positional holes in one pattern is an error (keeps the output
shape unambiguous: either a Record or a tuple, never a blend).

### Repetition

A hole may repeat, which is how a line with a variable-length run of values —
`Time: 7 15 30`, `target: 1,2,3` — becomes a list without a second parsing
stage:

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{id:word}: {vals:int+ sep=\",\"}"
```

| Form | Meaning | Captures as |
|---|---|---|
| `{int+ sep=" "}` | one or more integers | `List<Int>`, positional |
| `{ns:int+ sep=", "}` | named, one or more integers | `List<Int>`, field `ns` |
| `{ws:word+ sep=","}` | named, one or more words | `List<Text>`, field `ws` |

Three rules, each of which exists because the alternative silently matches the
wrong thing:

- **The separator is required.** A default would be right about half the time —
  a space for `1 2 3`, a comma for `1,2,3` — and a template that quietly parses
  the wrong thing is worse than one that asks. `{ns:int+}` is an error.
- **`+` is one-or-more**, so a line with a single element still matches and
  still yields a one-element list.
- **`text` cannot repeat.** A `text` hole is greedy to the next literal, so a
  repeated one would swallow its own separators and capture the whole run as
  element zero. `word+` is the repeatable spelling of "some text".

A repeated hole changes the hole's type, and so it changes the output shape:
`"{int} {int+ sep=\",\"}"` is `Int` then `List<Int>`, two different types, and
therefore a Tuple rather than a `List`.

### Groups

Repetition applies to a single hole. A run of *structure* — a clause that may
be absent, or a repeating pair — is a **group**.

#### Optional groups — `[? … ]`

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Using: "{name:word} ({w:int})[? -> {kids:word+ sep=\", \"}]"
```

on AoC 2017 D7's two line shapes:

```
pbga (66)                       -> {name: "pbga", w: 66, kids: []}
fwft (72) -> ktlj, cntj, xhth   -> {name: "fwft", w: 72, kids: ["ktlj","cntj","xhth"]}
```

A group holds literals and holes. When it does not participate, **each hole
inside takes its type's zero** — `0`, `""`, or the empty list. That is the whole
of "absent": a hole is not a different type depending on whether its group
matched, because that would need a sum type, and the language has none.

**The marker is `[?`, not a bare `[`.** Brackets are ordinary characters in AoC
input, and `"[{a:int},{b:int}]"` is a template that matches `[1,2]` — so a bare
bracket stays a literal and only `[?` opens a group.

#### Presence flags — `{?name}`

When the zero and a captured zero have to be told apart, name the group:

```domain
Using: "{v:int}[? (was {n:int}){?changed}]"
```

| Input | Result |
|---|---|
| `7 (was 0)` | `{v: 7, n: 0, changed: true}` |
| `7` | `{v: 7, n: 0, changed: false}` |

The flag is **opt-in**, for the same reason `Mode: Try` is. When the optional
part holds a repeated hole, absent already means the empty list and
`length(r.kids) > 0` answers the question; a mandatory `kids_present` beside it
would be noise.

It is spelled as a hole rather than a bracket attribute (`[?name …]`,
`[?name: …]`) because both of those are ambiguous against a group whose body
*starts* with a literal of that shape — and `[?Time: {n:int}]` is exactly the
kind of thing AoC input contains. `{` is already reserved inside a template, so
a hole-shaped flag cannot collide with anything.

#### Repeated groups — `( … )+`

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Using: "Game {id:int}: {draws:( {n:int} {color:word} )+ sep=\", \"}"
```

```
Game 1: 3 blue, 4 red, 2 green
  -> {id: 1, draws: [{n:3, color:"blue"}, {n:4, color:"red"}, {n:2, color:"green"}]}
```

The element type is the inner template's own output type, under the same rules
a top-level template follows: named inner holes give a `List<Record>`,
positional ones a `List<Tuple>` (or `List<List<T>>` when homogeneous). The
separator is required and `+` is one-or-more, exactly as for a repeated hole.

Padding inside the parentheses is formatting: `( {n:int} {c:word} )` and
`({n:int} {c:word})` lower identically. A space that really is part of the
element boundary belongs in the separator.

**One level of nesting.** A group holds literals and holes, never another
group. That keeps the inner template a plain `Template` — the same type, the
same lowering, the same rules — and one level covers every AoC input I can
find. A second level is a different feature and should be argued for on its own
evidence.

### Modes

`Mode:` controls how the pattern applies to the current value:

| `Mode:` | Input | Output |
|---|---|---|
| `One` | `Text` | one Record / tuple |
| `Each` | `List<Text>` | `List<Record>` / `List<tuple>`; every line must match |
| `Try` | `List<Text>` | the same, but keeping only the lines that matched |
| `Scan` | `List<Text>` | every occurrence *inside* each line, concatenated |

`One` and `Each` are inferred from the input type when `Mode:` is omitted —
`Text` gives `One`, `List<Text>` gives `Each`. `Each` is the common case: split
the input into lines first, then `Match Pattern, Mode: Each` to parse every
line.

**Neither `Try` nor `Scan` is inferred.** Each drops input the template did not
describe, which is something a program has to ask for.

**`Try` is never inferred.** Dropping input is something a program has to ask
for; if `Try` were the default for a mismatch, a typo in a template would
quietly parse nothing instead of failing. It is the answer to a file whose
lines are not all the same shape — one pass per shape, each keeping its own
lines:

```domain
Cursed Technique: Match Pattern
    Mode: Try
    Using: "toggle {a:int},{b:int} through {c:int},{d:int}"
```

Over `turn on 0,0 through 9,9` / `toggle 1,1 through 2,2` this yields the one
`toggle` record; the `turn` lines are picked up by a second pipeline reading
the same input with the `turn {what:word} …` template.

`Try` drops a line of the wrong *shape*. A line of the right shape whose
capture then fails to convert — an integer out of `int64` range — is a broken
line rather than a different kind of line, so it still stops the program.
Silently skipping it would turn a corrupt input into a quietly short answer.

### `Mode: Scan` — the template as a fragment

Every other mode reads the template as a description of a **whole line**, so
input the template does not describe exhaustively has no spelling. `Scan` reads
it as a description of a **fragment** and takes every occurrence:

```domain
Cursed Technique: Match Pattern
    Mode: Scan
    Using: "mul({a:int},{b:int})"
```

Over `xmul(2,4)%&mul[3,7]!^mul(32,64]then(mul(11,8)mul(8,5))` this yields the
three real `mul`s and ignores the noise — AoC 2024 D3, which `Extract Integers`
can only serve by discarding the structure that says which numbers belong
together.

A line contributes as many values as it holds, and the results are
concatenated, so the output is a flat `List` rather than one entry per line. A
line holding none contributes nothing — that is an *answer* here, not a
failure, which is exactly why `Scan` has to be asked for rather than inferred.

## `Case:` — several templates, one type

Regex alternation *inside* a template would let two branches capture different
field sets, and the output type could then only be a union — which this
language has no way to spell. So the alternation lives at the stage:

```domain
Cursed Technique: Match Pattern
    Mode: Each
    Case: on     "turn on {a:int},{b:int} through {c:int},{d:int}"
    Case: off    "turn off {a:int},{b:int} through {c:int},{d:int}"
    Case: toggle "toggle {a:int},{b:int} through {c:int},{d:int}"
```

→ `{kind: Text, a: Int, b: Int, c: Int, d: Int}`, with `kind` naming the case
that matched.

- **Every case must produce the same fields.** That is the rule that keeps the
  output one type, and it is checked at resolve time: a case whose holes differ
  is an error naming both shapes.
- **Order is priority order.** `turn on {n:int}` before `turn {n:int}` means the
  specific one wins; the reverse means it never fires.
- **Cases need named holes**, since a tuple's slots are positions and `kind`
  has nowhere to live in one.
- A hole named `kind` is refused rather than silently shadowed.

`Case:` composes with the modes: `Each` refuses a line no case matched, `Try`
drops it, `One` takes a single `Text`. (`Scan` is refused — interleaving
several templates' occurrences by position is a different feature.)

**This is what `Mode: Try` can only approximate.** Try over three verbs reads
the file three times and concatenates the results *by verb*, losing the input's
own order — which a simulation is defined by. `Case:` is one ordered pass.

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
"{id:word}: {vals:int+ sep=\",\"}" on "target: 1,2,3"
                               -> {id: "target", vals: [1, 2, 3]}
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

## Lowering

The template becomes a matcher once, at resolve time:

1. Tokenize the template into a sequence of literals and holes.
2. Compile to a regular expression with one capture group per hole
   (`{int}` → `(-?\d+)`, `{word}` → `(\S+)`, `{text}` → `(.*)`), anchored to the
   whole field. Named holes map to named groups.
3. At run time, match the current value (or each element under `Mode: Each` and
   `Mode: Try`), convert captures to their hole types, and assemble the
   Record/tuple.

An optional group is a **capturing** group with `?` on it — `( … )?`, not
`(?: … )?` — and the match is read with `FindStringSubmatchIndex` rather than
`FindStringSubmatch`. The text form reports a non-participating group as `""`,
which a group that legitimately matched empty also reports; the index form gives
`-1` and is exact. A group's own capture is what carries presence, so a
flag-only group has somewhere to read it from.

A repeated group's inner template lowers to a **non-capturing** element pattern,
so the run is one capture like a repeated hole's, split and then re-matched
element by element against the inner template's own regex.

Once a template has an optional group, regex order and declaration order stop
agreeing: the group's wrapper capture comes before the holes it guards and owns
no hole itself. So `Template.lower` walks the segments **once** and returns both
the regex and a capture plan saying which group fills which slot; `RegexSource`
and `Captures` are thin wrappers over it. Two walks would be two chances to
number the captures differently in the two backends.

A repeated hole is still **one** capture group, spanning the whole run
(`(?:-?\d+)(?:,(?:-?\d+))*`), split on the separator afterwards. A group per
element is not an option: a Go regexp keeps only the last match of a repeated
group. A repeated `word` element is narrowed from `\S+` to exclude the
separator's first byte, or `a,b,c` would capture as one element rather than
three.

`Template.RegexSource` builds both spellings of that pattern — named groups for
the interpreter, plain ones for the compiler — from one walk over the segments.
Two lowerings of one template is exactly how a compiled program could parse
differently from the interpreted one, so there is only the one.

The regex is an implementation detail the user never sees; the template surface
stays themed and friendly.

**The compiler has a faster path.** `domain build` emits a
`func dmParseN(s string) (T, bool)` per template. A template whose holes are
all `{int}`, separated by literals that a greedy scan cannot mis-split,
compiles to a **hand-rolled scanner with no regexp at run time**; anything else
(a `{word}` or `{text}` hole, a repeated hole, either kind of group, an
ambiguous separator) falls back to a package-level compiled regexp with
identical semantics.
`TestMatchPatternPathSelection` pins which template takes which path, and the
interpreter-vs-binary oracle tests pin that both agree.

The compiler also fuses parse-then-reduce adjacencies — `Split` +
`Match Pattern, Mode: Each` + `Count Matching` becomes one streaming loop that
never materializes the `[]string` or the `[]Record`. `Mode: Try` does not fuse:
the fused loop feeds every line to the consumer, and a Try stage is defined by
the lines it does *not* pass on.

## Error behavior

- **No match** (a line that doesn't fit the template): a runtime `RuntimeError`
  naming the stage, the offending input line, and the template. AoC inputs are
  regular, so a non-match almost always means a real bug — fail loudly, in the
  spirit of Binding Vows. `Mode: Try` is the opt-out, and only for this one
  failure.
- **Conversion failure** (e.g. `{int}` capturing a value out of `int64` range):
  runtime error with the offending text. Not skipped by `Mode: Try` — see
  above.
- **A malformed repetition or group** (no separator, an unquoted or empty one, a
  repeated `text` hole, a group that does not repeat, nested groups, a `{?flag}`
  outside an optional group, an optional group that records nothing) is a
  resolve error with a position, not a runtime one: the template is a literal,
  so it is checked before the program runs.

## Worked cases

Each of these is covered by a test.

| Input | Template | Result |
|---|---|---|
| `2-4,6-8` (2022 D4) | `"{a:int}-{b:int},{c:int}-{d:int}"` | `{a:2,b:4,c:6,d:8}` |
| `forward 5` (2021 D2) | `"{dir:word} {n:int}"` | `{dir:"forward", n:5}` |
| `move 1 from 2 to 1` (2022 D5) | `"move {n:int} from {src:int} to {dst:int}"` | `{n:1,src:2,dst:1}` |
| `1-3 a: abcde` (2020 D2) | `"{lo:int}-{hi:int} {ch:word}: {pw:text}"` | `{lo:1,hi:3,ch:"a",pw:"abcde"}` |
| `2x3x4` (2015 D2) | `"{int}x{int}x{int}"` | `[2,3,4]` |
| `Time: 7 15 30` (2023 D6) | `"{label:word}: {ns:int+ sep=\" \"}"` | `{label:"Time", ns:[7,15,30]}` |
| `toggle 1,1 through 2,2` (2015 D6) | `Mode: Try` + one template per verb | the lines of that verb |
| `fwft (72) -> ktlj, cntj` (2017 D7) | `"{name:word} ({w:int})[? -> {kids:word+ sep=\", \"}]"` | both line shapes, one pass |
| `Game 1: 3 blue, 4 red` (2023 D2) | `"Game {id:int}: {draws:( {n:int} {c:word} )+ sep=\", \"}"` | `draws: List<{n,c}>` |
| `xmul(2,4)%&mul[3,7]` (2024 D3) | `Mode: Scan` + `"mul({a:int},{b:int})"` | only the real ones |
| `turn on 0,0 through 9,9` (2015 D6) | three `Case:` lines | one pass, `kind` per line |
| `#70c710` (2023 D18) | `"#{c:hex}"` | `{c: 7390992}` |

## Deliberate omissions

The template language stops here on purpose — past this point a regex would be
the honest tool, and `Match Pattern` is meant to stay readable at a glance:

- Alternation *inside* a template. Several shapes on one line is `Case:`, whose
  same-fields rule is what keeps the output one type; a genuinely union-shaped
  result would need a sum type to land in.
- **Nested groups.** One level only — a group holds literals and holes, never
  another group.
- Recursive patterns.
- Custom hole types beyond `{int}`, `{word}`, `{text}`.
- Whitespace-flexible matching — templates match literally, so pre-split with
  `Split Fields` or trim first.

For input that needs more than this, `Extract Integers` mines every integer out
of a messy line without a template at all, and `Split Fields` handles
whitespace-separated columns.
