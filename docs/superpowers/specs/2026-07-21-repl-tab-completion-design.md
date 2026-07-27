# REPL tab completion — design

## Problem

`domain repl`'s interactive editor (`cmd/domain/repl_tty.go`, added in
[2026-07-21-repl-line-editing-design.md](2026-07-21-repl-line-editing-design.md))
has arrow-key editing, history, and auto-indent, but no completion: the user
must type every keyword, primitive name, `:command`, and file path in full.
The language server (`lsp/completion.go`) already computes exactly this kind
of context-aware suggestion for editors — that logic should power the REPL
too instead of being reimplemented.

## Goals

1. Tab completes, in order of what's under the cursor:
   - **Keywords + primitives + arg labels/Mode values** — reusing the LSP's
     existing `completionItems(prefix string)` decision function (exported
     as `CompletionItems`), unchanged in its own logic.
   - **REPL `:commands`** (`:help`, `:list`, `:type`, `:undo`, `:reset`,
     `:load`, `:save`, `:quit`) — a small list local to the REPL; the LSP has
     no notion of these.
   - **File paths** — for `Cursed Energy: `, `:load `, and `:save ` targets,
     listing real directory entries under the REPL's `baseDir`.
2. Multiple matches: repeated Tab presses cycle through them in place (no
   list printed, no menu UI) — like zsh's menu-complete. Typing any other
   character, or pressing Enter, accepts whatever's currently shown and
   resumes normal editing/submission.
3. No conflict with up/down (already REPL history recall) or with
   bubbles/textinput's own built-in suggestion system (`ShowSuggestions`,
   `AcceptSuggestion`/`NextSuggestion`/`PrevSuggestion`) — this feature does
   not use that system at all; it's a REPL-owned Tab handler.

## Non-goals

- No suggestion list/menu rendered on screen — only in-place cycling.
- No fuzzy matching — prefix matching only (case-insensitive for
  keywords/primitives/arg-labels/`:commands`, case-sensitive for file paths,
  matching normal filesystem conventions).
- No persistence of completion state across lines — cycling state resets
  the moment a non-Tab key is pressed.

## Architecture

`lsp/completion.go`'s `completionItems` is renamed to exported
`CompletionItems(prefix string) []map[string]any` — a pure, stateless
function requiring no document/session state, already covering: bare line →
statement keywords; `Keyword: ` typed → that keyword's primitives; indented
continuation line → arg labels, or (after `Mode:`) its values. Its one
caller (`(s *Server) completion`) and its test (`lsp_test.go`) update their
call site to the new name — no behavior change.

New file `cmd/domain/repl_complete.go` holds:

```go
// completeToken returns the completion candidates for the token ending at
// cursor in line, plus the byte offset where that token starts (so the
// caller knows what to replace). No match returns a nil candidates slice.
func completeToken(line string, cursor int, baseDir string) (candidates []string, tokenStart int)
```

It picks one of three sources based on `line[:cursor]`:

1. **File path** — if `line[:cursor]` (case-insensitively for the keyword,
   exactly for `:load`/`:save`) matches `^Cursed Energy:\s+`, `^:load\s+`,
   or `^:save\s+`, followed by a (possibly empty) partial path: `tokenStart`
   is set right after that matched prefix, and candidates are every entry
   under `filepath.Join(baseDir, filepath.Dir(partial))` (or `baseDir`
   itself when `partial` has no `/`) whose name has `filepath.Base(partial)`
   as a prefix — each candidate is the full relative path, with a trailing
   `/` appended when the entry is a directory (so cycling into it and
   pressing Tab again completes one level deeper).
2. **`:command`** — if `line[:cursor]` starts with `:`, `tokenStart = 1` and
   the word is `line[1:cursor]`, matched case-insensitively as a prefix
   against the bare names `help`, `list`, `type`, `undo`, `reset`, `load`,
   `save`, `quit`; candidates are those names with a `:` re-added.
3. **LSP reuse** — otherwise, call `CompletionItems(line[:cursor])`. The
   word being completed is the trailing run of `line[:cursor]` after the
   last space, tab, or colon (`tokenStart = cursor - len(word)`); items are
   matched by testing whether `word` is a case-insensitive prefix of the
   item's `label`, and the accepted candidate text is the item's
   `insertText` (not `label` — e.g. a keyword's `insertText` is
   `"Cursed Technique: "`, already including the trailing colon and space,
   so accepting it drops the user straight into typing the primitive).

## Cycling state & key handling

`replModel` gains four fields: `completing bool`, `candidates []string`,
`candIdx int`, `tokenStart int`.

**Tab**, added as a new case in `Update`'s `switch msg.String()`:
- Not currently completing → call `completeToken(m.ti.Value(),
  m.ti.Position(), m.core.baseDir)`. No candidates → no-op (`return m,
  nil`). Otherwise replace `line[tokenStart:cursor]` with `candidates[0]`,
  move the cursor to the end of the inserted text, store `candidates`,
  `tokenStart`, `candIdx = 0`, and set `completing = true`.
- Already completing (a repeated Tab) → `candIdx = (candIdx + 1) %
  len(candidates)`, replace the previously-inserted segment (from
  `tokenStart` to the current cursor) with `candidates[candIdx]`, cursor to
  the end again. `completing` stays `true`.

**Every other case in `Update`** (character keys via the final fallthrough
to `m.ti.Update(msg)`, `up`/`down`, `enter`/`ctrl+enter`/`alt+enter`,
`ctrl+c`, `ctrl+d`): if `completing` is `true` when that case is entered,
clear `completing`, `candidates`, and `candIdx` first — the keystroke then
proceeds through its normal handling unchanged. This is what makes Enter
"accept and submit" and typing a character "accept and keep editing."

## Testing

`completeToken` is a pure function of `(line, cursor, baseDir)` — directly
unit-testable without any bubbletea state:
- Keyword case: `completeToken("Cursed T", 8, ".")` → candidates include
  `"Cursed Technique: "`.
- Primitive case: `completeToken("Domain Expansion: So", 20, ".")` →
  candidates include `"Sort"`, not `"BFS"`.
- `:command` case: `completeToken(":lo", 3, ".")` → candidates are exactly
  `[":load"]`.
- File path case (using `t.Chdir(t.TempDir())` and real files, matching
  `repl_test.go`'s existing style): `completeToken("Cursed Energy: nu", 17,
  ".")` with a `nums.txt` in the directory → candidates include
  `"nums.txt"`.

`Update`'s Tab handling is tested at the `replModel` level like the rest of
`repl_tty_test.go`: send a `tea.KeyPressMsg{Code: tea.KeyTab}`, check
`m.ti.Value()`; send it again and check the value cycled to the next
candidate; send a character key afterward and check `completing` cleared
and the accepted text stayed.
