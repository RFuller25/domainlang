# The playground build

The documentation site's Run and Explain buttons execute Domain programs in the
reader's own browser, using the language front end compiled to WebAssembly —
the same lexer, parser, resolver, optimizer and interpreter the `domain` binary
uses (`cmd/domain-wasm`).

Running in the browser rather than on the server that serves the docs is what
makes the feature safe and honest:

- **There is no filesystem under `js/wasm`.** `Cursed Energy: input.txt` cannot
  reach anything; it fails to open the file and falls back to stdin, which is
  exactly what it does on a terminal when the named input is missing. The
  playground's input box *is* that stdin, so the documented semantics are the
  semantics you get, with no special casing.
- **A runaway program can actually be stopped.** The page runs each program in
  a Web Worker and terminates it on timeout. Nothing can interrupt a running
  goroutine from outside, so a server-side runner could not do this.
- **Nothing leaves the machine.**

## Building it

```sh
./docs/wasm/build.sh
```

That writes two files into this directory, neither of which is committed:

| File | What it is |
|---|---|
| `domain.wasm` | the language, about 5 MB (roughly 1.5 MB over the wire once compressed) |
| `wasm_exec.js` | Go's loader shim, copied from `GOROOT` so it matches the compiler |

They are build artifacts, and deliberately left out of the repository: a
committed copy would have to be rebuilt on every language change to stay
honest, and a stale one would run subtly different code from the documentation
describing it.

Rebuild the binary afterwards to ship the playground inside it — `docs/embed.go`
embeds this directory, so `domain expansion: documentation` serves whatever is
here:

```sh
go build -o domain ./cmd/domain
```

## Without it

The site works fine. `index.html` probes for `domain.wasm` at boot; when it is
absent the Run buttons are never added and the playground page explains how to
build it. That is the state a fresh clone is in.

## The pieces

| File | Role |
|---|---|
| `build.sh` | builds the module and copies the shim |
| `runner.js` | the Web Worker: loads the module, runs one program per message |
| `../../cmd/domain-wasm/main.go` | the Go side — exposes `domainRun({source, input, …})` |
