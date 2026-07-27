# REPL line editing (arrow keys, history, auto-indent) — design

## Problem

`domain repl` (`cmd/domain/repl.go`) reads input with a plain `bufio.Scanner`.
In a real terminal this means:

- No arrow-key editing — left/right don't move the cursor within a line the
  way a normal shell prompt does; up/down don't recall previous input.
- Indented continuation blocks (`Using:` lambdas, `Channel` bodies,
  `Shikigami` definitions) already work (`needsBlock` heuristic in
  `repl.go` puts the session into a `   ...>` continuation prompt), but the
  user must hand-type the leading whitespace themselves on every
  continuation line, and there's no way to force continuation mode when the
  parser's heuristic doesn't ask for one.

## Goals

1. Arrow-key line editing: left/right move the cursor within the line being
   typed; up/down recall previously submitted lines (session-only history,
   not persisted across `domain repl` runs).
2. When a top-level statement's trial parse reports `needsBlock`, the next
   prompt is pre-seeded with a leading tab so the user starts already
   indented, instead of having to type it. If the resulting line — tab and
   all — is blank, today's existing "blank line ends the block" logic fires
   exactly as it does now (this is unchanged; the tab is a visual
   convenience only).
3. Ctrl+Enter (with Alt+Enter as a universal fallback — see Non-goals) forces
   entry into continuation mode on a fresh top-level line even when
   `needsBlock` did not fire, pre-seeding the next line with a tab the same
   way.
4. None of this may break piped/non-interactive use of `domain repl` or the
   existing test suite.

## Non-goals

- History is **not** persisted to a dotfile; it resets every session.
- Ctrl+Enter is best-effort: it only reaches the program as a distinct key
  on terminals that support the Kitty keyboard protocol (kitty, WezTerm,
  iTerm2, ghostty, foot, …). On plain xterm/gnome-terminal/konsole the
  terminal itself sends identical bytes for Enter and Ctrl+Enter — no
  software running on top can distinguish them. Alt+Enter is the guaranteed
  fallback since its escape sequence is distinct on every terminal.
- No change to the language, parser, or `needsBlock` heuristic itself.
- No full-screen (alt-screen) TUI takeover — output must keep scrolling into
  the terminal's native scrollback like a normal shell session.

## Architecture

`cmd/domain/repl.go` keeps its existing `repl` struct and all statement logic
unchanged: `stmts`, `pending`, `baseDir`, `acceptTopLevel`, `flushPending`,
`needsBlock`, `frontEnd`, `evalAndShow`, `command`, `splitStatements`. This is
the shared "core" — it doesn't know or care how a line of text arrived.

`Repl(stdin io.Reader, stdout io.Writer) int` becomes a dispatcher:

```go
func Repl(stdin io.Reader, stdout io.Writer) int {
    if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
        return replTTY(f, stdout)
    }
    return replPlain(stdin, stdout) // today's bufio.Scanner loop, verbatim
}
```

- `replPlain` is today's loop body, renamed with no behavior change. Used
  whenever stdin isn't a real terminal — piped input, and every existing
  test (`repl_test.go` feeds `strings.Reader`, which is never a `*os.File`).
- `replTTY` (new, `cmd/domain/repl_tty.go`) drives a bubbletea `Program` in
  inline mode (no alt-screen) over the same `repl` core struct.

New dependencies (first third-party deps in `go.mod`): `github.com/
charmbracelet/bubbletea` and `github.com/charmbracelet/bubbles` (for its
`textinput` component — free left/right/home/end/backspace/delete editing).
`lipgloss` arrives transitively; no custom styling is planned.

## Components

`replTTY`'s bubbletea `Model`:

| Field | Purpose |
|---|---|
| `ti textinput.Model` | the line currently being typed |
| `history []string` | every line submitted this session, newest last |
| `historyIdx int` | cursor into `history` for up/down recall |
| `core *repl` | the existing struct from `repl.go`, untouched |

Prompt text is `"domain> "` or `"   ...> "` based on `len(core.pending) > 0`
— identical strings to today's, so existing docs/transcripts stay accurate.

### Key handling

**Enter:**
1. Read `ti.Value()`.
2. Route through the same branches `Repl()`'s loop uses today — `:command`,
   blank line, indented-continuation line, top-level line — just sourced
   from `ti.Value()` instead of `scanner.Text()`.
3. If a top-level line's trial parse hits `needsBlock`, enter pending mode
   as today, and additionally pre-seed the next prompt's `ti` with a
   leading tab (cursor at end). Purely visual; finalize logic (blank line
   ends the block) is untouched.
4. The finalized line is echoed permanently to scrollback (`tea.Println`),
   `ti` resets for the next line, and the line is appended to `history`.

**Up/Down:** walk `history`, replacing `ti`'s content and moving the cursor
to the end — standard shell-style recall.

**Left/Right/Home/End/Backspace/Delete:** delegated straight to
`textinput.Model.Update` — no custom logic needed.

**Ctrl+Enter / Alt+Enter:** meaningful only on a fresh top-level line (not
already mid-continuation): forces pending mode regardless of `needsBlock`,
skipping the trial-parse check, and pre-seeds the next line with a tab. A
no-op on an empty line. While already mid-continuation, behaves like plain
Enter (already forcing continuation there).

**Ctrl+C / Ctrl+D on an empty line:** quits, exit code 0 — matches today's
EOF handling in `replPlain`.

## Data flow & error handling

Unchanged from today: `line → acceptTopLevel/flushPending → frontEnd
(lex/parse/resolve) → interp.Run → "=> value : Type"`. `replTTY` only
changes how a line is obtained and echoed; parsing, evaluation, and
rollback-on-runtime-error semantics are exactly what `repl.go` already does.
Errors (`error: ...`, `runtime error: ... (statement dropped)`) print via
`tea.Println` and the session continues, same as today.

## Testing

- `repl_test.go` (existing): untouched. Still exercises `replPlain` only,
  since `strings.Reader` is never a `*os.File`.
- `repl_tty_test.go` (new): unit tests against the bubbletea `Model.Update`
  directly — feed `tea.KeyMsg` values, assert on model state and `View()`
  output. No PTY or real terminal required.
- Manual verification in a real terminal (arrow keys, history recall,
  auto-indent, ctrl+enter/alt+enter) before calling the feature done —
  automated model-level tests can't prove the interactive feel.
