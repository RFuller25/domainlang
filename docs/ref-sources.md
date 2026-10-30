# Sources — `Cursed Energy`

One class of the [primitive reference](primitives.md).

## Cursed Energy — sources

### Read Source — `(nothing) -> Text`

```domain ignore
Cursed Energy: input.txt
Cursed Energy: stdin
```

Must be the first stage. Reads the named file; if the file does not exist,
falls back to stdin (so `domain prog.domain < input.txt` works without the
file present). `stdin` (any case) or an empty string, `Cursed Energy: ""`, reads
stdin directly; a bare `Cursed Energy:` with nothing after it is an error. A trailing
run of `\r`/`\n` is trimmed (typical AoC inputs end with one newline).
Relative paths resolve against the program file's directory when
interpreting, and against the working directory in a compiled binary (a
documented delta — see [compiler.md](compiler.md)).

---
