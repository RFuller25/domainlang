# View builtins — describing a picture

Part of the [expression layer reference](expressions.md).

Most of Domain computes an answer. These build a **View**: a description of a
picture, which `Reveal` draws.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> box(stack(text("TOTAL"), text(sum(xs))))
Reveal: stdout
```
```input
1,2,3,4
```
```output
┌─────┐
│TOTAL│
│10   │
└─────┘
```

A View is **opaque**. It has no length, no ordering, and no equality: those are
questions about a value, and a View is a description of one. That is the whole
reason it is a type rather than styled `Text` — with styling spliced into a
string, `length` counts escape bytes and every layout decision is made against
a value that lies about its own size.

```domain
Using: (w) -> text(w) = text(w)     # refused: a View cannot be compared
```

## Building one

| Builtin | Type | Meaning |
|---|---|---|
| `text(v)` | `T -> View` | A panel of one value, rendered as `Reveal` renders it. Newlines make several lines. |
| `blank()` | `-> View` | Nothing at all — an empty slot. |
| `draw(board)` | `Grid<T> \| Sparse<T> \| List<T> -> View` | A whole board, each cell as it would print, row by row. |

`text` takes anything, so a score, a cell and a label all become panels
without writing `totext` first:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> stack(text(42), text(2 > 1), text("done"))
Reveal: stdout
```
```input
```
```output
42
true
done
```

## Combining them

| Builtin | Type | Meaning |
|---|---|---|
| `stack(v…)` | `View… -> View` | Panels one above another, left-aligned. |
| `beside(v…)` | `View… -> View` | Panels side by side, top-aligned. |
| `box(v)` | `View -> View` | A drawn border around a panel. |
| `margin(v, w, h)` | `View × Int × Int -> View` | `w` columns and `h` rows of space around a panel. |
| `align(v, w, h, how)` | `View × Int × Int × Text -> View` | A panel placed in a `w`×`h` area: `"left"`, `"center"` or `"right"`. |
| `fit(v, w, h)` | `View × Int × Int -> View` | A panel forced to exactly `w`×`h`. |

Every combinator is rectangular: a short column still holds its width, so
nothing to its right slides left.

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> beside(stack(text("a"), text("bbbb")), text("|"), text("X"))
Reveal: stdout
```
```input
```
```output
a   |X
bbbb
```

`align` never clips — losing part of a picture without being asked is worse
than overflowing one. `fit` is where clipping is asked for by name.

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> box(align(text("hi"), 8, 1, "center"))
Reveal: stdout
```
```input
```
```output
┌────────┐
│   hi   │
└────────┘
```

## Styling

`style(v, spec)` applies a style to everything inside a panel. The spec is
space-separated words in any order: `bold`, `dim`, `underline`, `reverse`, a
colour, and `on <colour>` for the background.

The colours are `black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`,
`white`, and each with a `bright-` prefix — the sixteen every terminal has, so
a program names a colour and gets the reader's own palette rather than a guess
at it.

**Styling never changes geometry.** Layout is computed once, and styling only
decides how a run of text is drawn — which is what lets the same frame be
compared whether it was played or replayed.

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> beside(style(text("ab"), "bold red on blue"), text("|"))
Reveal: stdout
```
```input
```
```output
ab|
```

Styles compose outside in, and an inner one wins:

```domain run
Cursed Energy: stdin
Cursed Technique: Apply
    Using: (t) -> style(stack(text("outer"), style(text("inner"), "green")), "red bold")
Reveal: stdout
```
```input
```
```output
outer
inner
```

A misspelled style word is refused rather than ignored: a colour that silently
did nothing is a bug found by squinting at a screenshot.

## Drawing a board

`draw` renders a whole board. It takes no per-cell function — the expression
layer has no higher-order builtins at all, and it does not need one here,
because the pipeline layer already has `Map Cells`. Build the board of glyphs
there and draw it here:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Map Cells
    Using: (c) -> if c = "#" then "█" else "·"
Cursed Technique: Apply
    Using: (g) -> box(draw(g))
Reveal: stdout
```
```input
#..#
.##.
#..#
```
```output
┌────┐
│█··█│
│·██·│
│█··█│
└────┘
```

A `List` of rows works the same way, and a `List` of `Text` is taken as lines
already rendered:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Apply
    Using: (lines) -> box(draw(lines))
Reveal: stdout
```
```input
one
two
```
```output
┌───┐
│one│
│two│
└───┘
```

## Colouring a board

`draw`'s cells may be `View`s instead of plain values — built with `style`,
in the same `Map Cells` that built the glyphs above — and it colours each one
rather than flattening it to plain text. `Grid<View>`, `Sparse<View>` and
`List<List<View>>` all work; a flat `List<View>` is the styled twin of a
`List<Text>` of already-rendered lines, one View per whole row.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Channeled Energy: Convert To Grid
Cursed Technique: Map Cells
    Using: (c) -> if c = "#" then style(text("█"), "red") else text("·")
Cursed Technique: Apply
    Using: (g) -> box(draw(g))
Reveal: stdout
```
```input
#..#
.##.
#..#
```
```output
┌────┐
│█··█│
│·██·│
│█··█│
└────┘
```

`Reveal` never shows the colour — it writes a View with no styling at all,
the same rule the next section covers — so this prints exactly what the
unstyled example above does. The colour is there and reaches a played
terminal, and the same board diffs identically in a replay either way, since
layout — where every character lands — never depends on style. That is what
`towerdef` uses `style` on its score line for already, one panel at a time;
a styled board is the same idea over every cell of one at once, for a game
whose picture is more than one colour of glyph — a health bar, a highlighted
tile, a piece distinguished from the well it is falling into.

## How a View prints

`Reveal` writes the picture with **no styling at all**, and trims the trailing
spaces from every line. Both are for the same reason: what goes to a pipe is
what a test diffs, and a frame carrying escape codes — or invisible trailing
whitespace an editor might strip — would be a different string for reasons
that have nothing to do with the program.

Width is counted in **runes**, matching `length` on `Text`.

## Writing a value out

`tojson(v)` writes a value as JSON. It is here rather than on a page of its own
because it is the same idea as the rest of this one: a value described in a
form something else can read.

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Channeled Energy: Convert To Integers
Cursed Technique: Apply
    Using: (xs) -> tojson({scores: xs, best: max(xs)})
Reveal: stdout
```
```input
3,9,4
```
```output
{"scores":[3,9,4],"best":9}
```

A Record is an object in the order its fields were declared, a List is an
array, and a Map is an object with its keys rendered and **sorted** — a
document a server compares has to be the same bytes for the same value.

A `View` has no JSON form, and asking for one is refused rather than producing
something meaningless: it describes a picture rather than a value.

```domain
Using: (w) -> tojson(text("hi"))     # refused
```

Reading JSON *in* needs a declared type and so is a stage rather than a
builtin — see
[`Convert From JSON`](ref-coercions.md#convert-from-json--text---t-game-dev).
