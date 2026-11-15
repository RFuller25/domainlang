# Notes for Claude

## Two repositories, one codebase

- **DEV** is `RFuller25/domainlang` — where all work happens.
- **PRD** is `RFuller25/domainlang` — the same tree under another name. It is
  *made from* DEV and is never edited on its own.

They must differ only in the repository path in URLs (`RFuller25/domainlang` vs
`RFuller25/domainlang`: README, `flake.nix`, the docs site, the AoC User-Agent).
A real difference is a bug — it is how PRD once ended up 92 files behind.

After changing DEV, bring PRD along:

```sh
make check-prd     # in DEV: do the two agree? exits 1 and lists the files if not
make sync-prd      # in DEV: make PRD match; stages it, does not commit
```

`sync-prd` refuses a PRD with uncommitted changes, and it **overwrites** PRD
files. If `check-prd` lists a file you did not touch in DEV, someone edited PRD
directly: port that edit back to DEV first, or the sync will discard it.

Then, in PRD: `go build ./... && go test ./...`, commit, push. Commit and
push to PRD by hand — the script deliberately does not. PRD's `main` is updated
the same way DEV's is, so the two repos are never left disagreeing on `main`.

`scripts/sync-prd.sh` and the two Makefile targets are themselves synced, so
they exist in both repos; always run them from DEV.

## Working conventions

- Develop on the branch the session names, push it, then fast-forward `main`
  on both repos when asked to merge. No PR unless one is asked for.
- The full test suite takes several minutes (`codegen` alone is ~6 min). While
  iterating, run the packages you touched; run `go test ./...` before merging.
  `docs/` has drift tests that fail when a documented key, flag or command and
  the code disagree — change both together.
- Anything that runs whole programs must link the Game Dev host:
  `_ "domain/game"` (see `interp/scoped_test.go`).
- The editor's background analysis and the program runner share package-level
  front-end state; take `frontEndMu` around lex/parse/resolve on the event loop.

## The tidy package (`tidy/`)

The editor applies it on `ctrl+s`, and `domain tidy` applies it to files.

- **Every rewrite edits source text**, never a re-printed AST: the lexer drops
  comments, so printing would delete them. A rewrite must not delete a comment.
- **Nothing is kept unless the result still resolves** and the types of what
  the program prints are unchanged (`applyOne`, `Apply`). That check does not
  see a *value* change: a rewrite that reorders evaluation, regroups arithmetic
  or changes how often something runs can pass it. Reason about semantics, and
  test the answer (`tidied()` in `tidy_test.go` runs before and after).
- **When unsure, do nothing** and report a note. Rewrites that need a name
  from the user are `alt+r` refactors, not save-time rewrites.
- Vows (`alt+v`) are offered, never applied: they are claims about the input.
