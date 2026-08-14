# Text builtins

Part of the [expression layer reference](expressions.md).

### Text

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Cursed Technique: Map Each
    Using: (w) -> upper(slice(w, 0, 3))
Reveal: stdout
```
```input
apple
fig
```
```output
[APP, FIG]
```

`length` counts runes and `charat` indexes them, so text handling is
character-based rather than byte-based:

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by "\n"
Maximum Technique: Count Matching
    Using: (w) -> charat(w, 0) = charat(w, length(w) - 1)
Reveal: stdout
```
```input
level
apple
noon
```
```output
2
```


| Builtin | Type | Behavior |
|---|---|---|
| `toint(s)` | `Text -> Int` | Parse (whitespace-tolerant). **Error** if not an integer. |
| `totext(n)` | `Int \| Float -> Text` | Render a number exactly as `Reveal` would (shortest round-trip form for floats). |
| `occurrences(s, sub)` | `Text × Text -> Int` | Non-overlapping occurrences of `sub` in `s` (Go `strings.Count` semantics, including the empty-substring corner: `len+1`). |
| `repeats(s)` | `Text -> Bool` | Whether `s` is a shorter pattern repeated ≥ 2 times (`"abab"`, `"aaa"`). |
| `length(s)` | `Text -> Int` | Number of **runes**. |
| `slice(s, lo, hi)` | `Text × Int × Int -> Text` | Half-open substring, clamped like the list form. |
| `charat(s, i)` | `Text × Int -> Text` | The rune at `i`, as a 1-character Text. **Error** out of range, like `item`. |
| `chars(s)` | `Text -> List<Text>` | The runes — the expression layer's `Split Text by ""`. |
| `indexof(s, sub)` | `Text × Text -> Int` | Rune position of the first occurrence, or `-1`. |
| `startswith(s, p)` / `endswith(s, p)` | `Text × Text -> Bool` | Prefix / suffix test. |
| `replace(s, old, new)` | `Text × Text × Text -> Text` | Every occurrence. |
| `trim(s)` | `Text -> Text` | Leading and trailing whitespace removed. |
| `upper(s)` / `lower(s)` | `Text -> Text` | Case folding. |
| `textjoin(xs, sep)` | `List<T> × Text -> Text` | Renders each element exactly as `Reveal` would (`Text` elements pass through unchanged; `Int`, `Float`, `Bool`, `Record`, and any other renderable type are converted) and joins the results with `sep`. `List<Text>` is the expression layer's `Join`. |
| `split(s, sep)` | `Text × Text -> List<Text>` | The expression layer's `Split Text by`. An empty separator splits into runes, like `chars`. Line splitting is `split(s, "\n")` — the pipeline layer's `Lines` Shikigami is the same operation. |
| `words(s)` | `Text -> List<Text>` | Split on runs of whitespace, dropping empties. |
| `contains(s, sub)` | `Text × Text -> Bool` | Substring test. `indexof(s, sub) >= 0` said the same thing, but a membership question should read the same whatever it is asked of. |
| `ord(s)` | `Text -> Int` | The first rune's code point. **Error** on the empty text. `ord(c) - ord("a")` is the a–z index that used to need an `indexof` over a literal alphabet. |
| `chr(n)` | `Int -> Text` | The character with code point `n`. **Error** outside a valid code point. |
| `repeat(s, n)` | `Text × Int -> Text` | `n` copies. Total: a non-positive count is `""`. Not to be confused with `repeats(s)`, which asks whether `s` *is* a repetition. |
| `padleft(s, n, p)` / `padright(s, n, p)` | `Text × Int × Text -> Text` | Widen to `n` **runes** by repeating `p` on one side, truncating the last copy. Text already that wide is returned untouched. |
| `trimprefix(s, p)` / `trimsuffix(s, p)` | `Text × Text -> Text` | Remove `p` if present — the counterparts to `startswith`/`endswith`. |
| `isdigit(s)` | `Text -> Bool` | Every rune is a decimal digit. The empty text is **false**: "every rune is a digit" is vacuously true of it, which is never what a guard means. |
| `isalpha(s)` | `Text -> Bool` | Every rune is a letter, same empty rule. |
| `isupper(s)` / `islower(s)` | `Text -> Bool` | No rune of the opposite case, and at least one cased rune — so `"AB1"` is upper and `"1"` is neither. |

**Positions count runes, not bytes**, everywhere — `length`, `charat`, `slice`
and `indexof` agree with each other and with `Split Text by ""`, so an index
means the same thing in both layers on non-ASCII input.
