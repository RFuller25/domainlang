# Inversions and statements

One class of the [primitive reference](primitives.md).

## Reverse Cursed Technique — inversions

### Reverse — `List<T> -> List<T>` or `Text -> Text`

```domain run
Cursed Energy: stdin
Cursed Technique: Split Text by ","
Reverse Cursed Technique: Reverse
Reveal: stdout
```
```input
a,b,c
```
```output
[c, b, a]
```

It reverses text as well as lists, choosing the form by the current type:

```domain run
Cursed Energy: stdin
Reverse Cursed Technique: Reverse
Reveal: stdout
```
```input
hello
```
```output
olleh
```


Reverses element order — or, over `Text`, the runes. A palindrome check used
to have to round-trip through `Split Text by ""`.

---

## Simple Domain, Channel, Shikigami, Binding Vow, Reveal

`Reveal: stderr` sends the value to standard error instead of stdout, so a
mid-pipeline Reveal becomes a debugging tool that does not disturb the
program's answer — or its golden test. A nil sink discards, so a host that
captures only stdout never sees stderr output mixed in.

Control flow (`Repeat N` / `While` / `Iterate Until Fixed Point`), Channels
and their consumers, Shikigami definition/calls, vows, and the output sink
are described in [language.md](language.md). All are fully supported by both
backends, including vow stripping under `--release`.
