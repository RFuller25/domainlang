# The `domain` CLI

One binary contains both backends. The mode is picked from the arguments:

```
domain <file.domain>                 interpret (bare file, no other args)
domain <file.domain> <args...>       compile (any extra argument selects the compiler)
domain run <file.domain> [flags]     interpret, explicitly (accepts the shared flags)
domain build <file.domain> [flags]   compile, explicitly
domain check <file.domain>           typecheck only: report the first error, run nothing
domain repl                          interactive pipeline builder (docs/tooling.md)
domain lsp                           language server over stdio (docs/tooling.md)
domain help | -h | --help            print the full usage text
```

Plus the diagnostics command family (full reference in
[diagnostics.md](diagnostics.md)):

```
domain expansion: diagnosis <file>        error list with fix suggestions (read-only)
domain expansion: lint <file>             errors + style warnings + perf hints (read-only)
domain expansion: fix <file>              apply unambiguous fixes in place (original → .bak)
domain expansion: optimize <file>         optimization report; rewrites the source where possible (.bak)
domain expansion: maximum compile <file>  fix, lint, optimize, then compile and run with stdin
domain expansion: documentation [-p PORT] serve this documentation as a local website (default port 4444)
```

`documentation` is the one expansion command that takes no program file: it
serves the browsable documentation site (this reference, rendered with search
and cross-links) at `http://localhost:4444/` and opens it in your browser.
The optional `-p`/`--port` picks a different port. The whole site is embedded
in the `domain` binary, so it works from any install — including the NixOS
package — with no source checkout present. Press Ctrl+C to stop the server.

The rule of thumb: **a bare program file runs it; anything more builds it.**
The explicit `run` subcommand exists because of that rule — it is the only
way to interpret *with* flags (`domain run prog.domain --explain`), since a
subcommand-less `domain prog.domain --explain` counts as "extra args" and
selects the compiler.

## Flags

Shared by `run` and `build`:

| Flag | Effect |
|---|---|
| `--explain` | print the optimizer's algorithm substitutions to stderr (or `no optimizations applied`) |
| `--no-optimize` | skip the optimizer; run/compile the naive pipeline (the correctness oracle) |
| `--release` | shed Binding Vows: `run` skips them, `build` compiles them out of the binary |

`build` only:

| Flag | Effect |
|---|---|
| `-o <path>`, `--output <path>` | where to write the compiled binary. Default: the source name minus `.domain`, in the current directory (an extensionless source gets `.bin` so the build can never overwrite it) |
| `--emit-go <path>` | also write the generated Go source; `-` writes it to stdout |
| `--run` | run the binary immediately with the current stdin, propagating its exit code. Without `-o` the binary goes to a temp path and is cleaned up afterwards — a one-shot compile-and-run; with `-o` the binary is kept |

`check` takes no flags: it runs the static front end — lex, parse,
resolve (which is where Domain typechecks, per the "typecheck at resolve
time" rule) — and prints `<file>: ok` or the positioned errors. The parser
recovers at top-level statement boundaries, so one run reports every
independently broken line (capped at 10), one per line; resolve errors
still stop at the first, since later types depend on earlier ones. It
never reads program input or executes anything, so it is safe on programs
whose vows or data would fail at runtime; exit codes are 0 / 1 / 2 as
below. Errors inside Shikigami bodies carry an inlining trace
(`in Shikigami "X" (body at L:C): …`), and errors inside the embedded
prelude are labeled `prelude source L:C` so they are never mistaken for
positions in your file.

## Input

`Cursed Energy: <target>` reads the named file, falling back to stdin when
the file does not exist — so both of these work:

```sh
domain run prog.domain              # prog reads ./input.txt if present
domain run prog.domain < input.txt  # …or whatever is piped in
```

When interpreting, relative targets resolve against the *program file's*
directory; a compiled binary resolves them against the *working directory*
(see [compiler.md](compiler.md) for this and the other documented delta).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | program error: unreadable program file, lex/parse/resolve failure, runtime error, vow violation, or a failed `go build` |
| 2 | usage error: no arguments at all, unknown flag, missing file argument, or a flag missing its value |

Compiled binaries follow the same convention: 0 on success, 1 with a
`domain: ...` message on stderr for runtime failures (including vow
violations in a debug build).

## The build toolchain

`domain build` writes a throwaway Go module to a temp directory and shells
out to `go build -trimpath -ldflags "-s -w"` with `CGO_ENABLED=0`, producing
a self-contained static binary (~1.5 MB). The Go toolchain must be on
`PATH`; the Nix package wraps the binary so this is always true (see the
[repository README](../README.md#install-with-nix)). Cross-compiling works
the usual Go way: set `GOOS`/`GOARCH` before `domain build`.

## Examples

```sh
domain day1.domain < input.txt                 # interpret
domain day1.domain -o day1                     # compile → ./day1
./day1 < input.txt                             # run the binary
domain run day1.domain --explain < input.txt   # see optimizer rewrites
domain run day1.domain --no-optimize           # the naive oracle path
domain build day1.domain --release -o day1     # vow-free release binary
domain build day1.domain --emit-go -           # inspect the generated Go
domain build day1.domain --run < input.txt     # one-shot compile-and-run
```
