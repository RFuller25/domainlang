# Diagnostics — the error engine and the `expansion:` commands

Domain's diagnostics engine (package `diag/`) sits on top of the static front
end and turns every failure into a rich, positioned report: what is wrong,
where, why, and — whenever the repair is unambiguous — a machine-applicable
fix. It powers the five diagnostic `domain expansion:` CLI commands below.
(A sixth, `domain expansion: documentation`, is unrelated to the engine — it
serves this documentation as a local website; see [cli.md](cli.md).)

## The report format

Every finding renders as a compiler-style block:

```
error[name]: unknown keyword "Cursed Tecnique"
  --> day1.domain:3:1
   3 | Cursed Tecnique: Split Text by "\n\n"
     | ^^^^^^^^^^^^^^^
  help: did you mean "Cursed Technique"?
  fix: auto-fixable — run `domain expansion: fix`
```

Three severities: **error** (the program cannot run), **warning** (it runs,
but something looks wrong), **hint** (it is fine; here is how to make it
better). The bracketed code classifies the finding: `syntax`, `name`, `type`,
`resolve`, `style`, `dead-code`, `perf`. Output is colored when stdout is an
interactive terminal (`NO_COLOR` and `TERM=dumb` are honored).

## How the engine finds more than one error

The lexer and the resolver stop at their first error, and that is correct for
`run`/`build`/`check` — later work depends on earlier results. The diagnostics
engine sees further with a **fix-and-continue loop**: whenever a diagnostic
carries a *confident* fix, the engine applies it to a private in-memory copy
of the source and re-runs the front end, surfacing whatever error was hiding
behind the one it just repaired. One `diagnosis` run can therefore walk
through a chain of typos, a missing colon, and a type error in a single pass.
Analysis never writes to your file — only `expansion: fix`, `expansion:
optimize`, and the fix stage of `maximum compile` write, and each saves the
original as `<file>.bak` first.

## What the engine recognizes

**Lexical repairs** — smart quotes pasted from a chat app (`“…”` becomes
`"…"`, repairing the pair in one edit), en/em dashes, non-breaking spaces,
full-width colons, stray semicolons, tab indentation (rewritten to four
spaces), unterminated strings (closed at end of line), unknown escape
sequences (the valid set is listed), and inconsistent dedents (the engine
reconstructs the indentation widths in effect and proposes the nearest
enclosing block).

**Syntax repairs** — a missing colon after a keyword is located precisely:
`Reveal stdout` swallows both words into the keyword, so the engine finds the
longest *known* keyword prefix and puts the colon after it. The parser's own
statement-boundary recovery already reports every independently broken
top-level line (capped at 10), and each is enriched individually.

**Name intelligence** — unknown names are matched by case-insensitive edit
distance against the *live* vocabulary, never a hardcoded list:

- unknown **keywords** against the primitive registry plus the structural
  forms (`Channel`, `Shikigami`, `Simple Domain`); a pure case mismatch
  (`cursed technique:`) is repaired with certainty;
- unknown **operations** first against every primitive ID *under other
  keywords* — `Cursed Technique: Select Top 3` is redirected to `Maximum
  Technique:` with certainty because `Select Top K` exists there verbatim —
  then fuzzily against the IDs under the written keyword (`Splitt Each` →
  `Split Each`); placeholder words in IDs (`K`) match literal integers;
- unknown **Shikigami** against user definitions plus the prelude;
- unknown **channels** against the Channels actually defined in the file;
- **prefix-free lines** whose phrase names no operation (`Splt Each by …`)
  fuzzily against every primitive ID; since there is no keyword on the line to
  move, the repair rewrites the phrase itself.

**Prefix inference** ([optional keywords](language.md#optional-keywords)) adds
two errors of its own, both of which prefer asking to guessing: a phrase that
matches primitives under two different keywords reports both and asks for the
keyword, and a Shikigami named after a built-in is refused at its definition
with the reserved-name rule spelled out.

**Type intelligence** — a type mismatch explains what the pipeline was
carrying versus what the primitive needs, and when a single-step bridge
exists, names the exact line to insert: `List<Text>` flowing into `Sum`
suggests `Channeled Energy: Convert To Integers`; raw `Text` into a grid
operation suggests the split-then-convert pair.

## The linter

`Lint` runs whenever the source parses, over the top level, every Channel
body, and every Shikigami body.

Style and hygiene (warnings):

- a `Channel` defined but never consumed by a `From:`;
- a `Shikigami` defined but never summoned, defined twice, or shadowing a
  prelude name;
- statements after the last `Reveal` whose results nothing observes
  (`Binding Vow`, further `Reveal`s and `Part` blocks are exempt — all three
  are observable), checked per scope, so work after a `Part`'s own final
  `Reveal` is reported too;
- a pipeline that never `Reveal`s at all — satisfied by a program whose
  `Part` blocks do the revealing;
- a `Part` whose body never `Reveal`s, so it produces no output;
- two `Part` blocks sharing a label, which makes their output
  indistinguishable;
- a named argument the primitive on that line never read — `Sze: 3` on a
  `Join`, or a `Size:` on a primitive that takes none. An unread argument is
  silently dropped at runtime, so this is the quietest way to write a program
  that does something other than what it says. The check asks the resolver
  what happened rather than keeping a list of accepted names: `prims.ArgSet`
  marks each argument as a primitive looks it up, so it can never drift from
  what `Build` actually reads. It runs only over a program that resolved
  cleanly, since a statement the resolver never reached never had the chance
  to read anything;
- an expression written into an operation phrase.
  `Cursed Technique: Window length(xs) / 2` parses to the words
  `[Window length xs]` plus the integer `2`, and every primitive reads only
  the integer — so the line runs as `Window 2`. The test is a call shape, not
  a name: the word must both name an expression builtin and be followed by
  `(`, so a channel called `cells` is not mistaken for one. Expressions belong
  in an indented lambda argument (`Size: (xs) -> length(xs) / 2`, see
  [primitives.md](primitives.md#measured-arguments)).

Performance hints:

- `Sort` followed by `Reverse` — one sort in the opposite direction (the
  optimizer already fuses this; the hint is about source clarity);
- two sorts in a row — the first is wasted work;
- `Sort` followed by `Take Item 0` — an O(n log n) spelling of `Min`/`Max`;
- `Filter` followed by `Count` — one fused `Count Matching` pass.

## The commands

```
domain expansion: diagnosis <file>        read-only: every error, with suggestions and fix availability
domain expansion: lint <file>             read-only: errors + warnings + hints, compact
domain expansion: fix <file>              apply every confident fix in place (original → <file>.bak)
domain expansion: optimize <file>         optimization report + source rewrites (original → <file>.bak)
domain expansion: maximum compile <file>  fix → lint → optimize → compile → run with stdin
```

Both the shell-split form (`domain expansion: lint prog.domain`) and the
quoted form (`domain "expansion: lint" prog.domain`) work, with or without
the colon, case-insensitively.

**diagnosis** never writes. It prints every finding with full notes and ends
with a tally plus how many errors `fix` could repair automatically.

**lint** never writes. Exit code is 1 only for errors — warnings and hints
exit 0, so it is CI-friendly.

**fix** applies exactly the *confident* subset: repairs the engine would bet
on (typo corrections within tight edit distance, case fixes, colon insertion,
tab/smart-quote/semicolon cleanup, string closing). Anything ambiguous is
reported for a human instead. The original is saved as `<file>.bak` before
the file is touched.

**optimize** refuses a broken program (run `fix` first). It has two layers:

1. *Source rewrites* — the subset of optimizations with an exact source
   spelling, applied to your file (with a `.bak`): fusing `Sort` + `Reverse`
   into one directional sort, deleting a redundant double sort, deleting dead
   statements after the final `Reveal`, and deleting unused Channels. Each
   rewrite is applied one at a time and the program is re-resolved after
   each; a rewrite that would break the program is rolled back.
2. *The IR report* — everything the 30-pass optimizer will substitute on
   every run/build of the (possibly rewritten) program, in the same wording
   as `--explain`.

**maximum compile** is the whole ritual: apply confident fixes (backing up
first), stop with a full report if unfixable errors remain, print lint
warnings and hints, then compile with the optimizer narrating its rewrites to
stderr and run the binary immediately with the current stdin, propagating its
exit code.

## Exit codes

Same convention as the rest of the CLI: `0` success (for `lint`, warnings and
hints still exit 0), `1` errors found / fix left unfixable errors / compile
or run failure, `2` usage error (unknown expansion command, missing file,
stray flag).
