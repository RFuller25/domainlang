# REPL Tab Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `domain repl`'s interactive editor Tab completion for keywords, primitives, arg labels/Mode values, REPL `:commands`, and file paths — reusing the language server's existing context-aware completion logic instead of reimplementing it.

**Architecture:** `lsp/completion.go`'s pure decision function is exported (`CompletionItems`) so `cmd/domain` can call it directly — no LSP session/document state needed, it already just takes a line-prefix string. A new pure function, `completeToken` (`cmd/domain/repl_complete.go`), picks between three candidate sources (file paths, REPL `:commands`, or `lsp.CompletionItems`) based on cursor position and returns prefix-filtered candidates. `repl_tty.go`'s `Update` gains a `tab` case that starts/advances a cycle through those candidates in place; any other key resets the cycle before being handled normally.

**Tech Stack:** Same as the existing REPL (Go 1.25, `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`) — no new dependencies.

## Global Constraints

- No suggestion list/menu ever rendered — completions cycle in place only, on repeated Tab presses.
- Matching is prefix-only (no fuzzy matching): case-insensitive for keywords/primitives/arg-labels/Mode-values/`:commands`, case-sensitive for file paths (normal filesystem convention).
- `:load`/`:save` file-target detection is case-sensitive (matches how `repl.go`'s `command` dispatcher already matches them); `Cursed Energy:` detection is case-insensitive (matches `lsp/completion.go`'s own `canonicalKeyword` convention).
- Byte offsets throughout (`cursor`, `tokenStart`) are treated as ASCII-safe, matching the rest of `repl_tty.go`'s existing string handling (e.g. `SetCursor(len(seed))`) — no Unicode/rune-safe conversion is added.
- `lsp_test.go` and `lsp/completion.go`'s existing behavior must not change — only the function name changes.

---

### Task 1: Export `lsp.CompletionItems`

**Files:**
- Modify: `lsp/completion.go` (rename `completionItems` → `CompletionItems`)
- Modify: `lsp/lsp_test.go` (update the 4 call sites)

**Interfaces:**
- Produces: `func CompletionItems(prefix string) []map[string]any` — same behavior as today's `completionItems`, just exported. Task 2 calls this from `cmd/domain`.

- [ ] **Step 1: Rename the function and its one caller**

In `lsp/completion.go`, change:
```go
	items := completionItems(prefix)
```
to:
```go
	items := CompletionItems(prefix)
```

And change:
```go
// completionItems is the pure decision function (tested directly): given the
// text before the cursor on a line, decide what to offer.
func completionItems(prefix string) []map[string]any {
```
to:
```go
// CompletionItems is the pure decision function (tested directly, and
// reused directly by the REPL's tab completion — see
// cmd/domain/repl_complete.go): given the text before the cursor on a
// line, decide what to offer.
func CompletionItems(prefix string) []map[string]any {
```

- [ ] **Step 2: Update the test call sites**

In `lsp/lsp_test.go`, replace all four occurrences of `completionItems(` with `CompletionItems(` (lines 265, 272, 281, 287):
```go
	kw := labelsOf(CompletionItems(""))
```
```go
	ops := labelsOf(CompletionItems("Domain Expansion: "))
```
```go
	args := labelsOf(CompletionItems("    "))
```
```go
	modes := labelsOf(CompletionItems("    Mode: "))
```

- [ ] **Step 3: Run the lsp package tests**

Run:
```bash
go build ./... && go test ./lsp/... -v -run TestCompletion
```
Expected: `TestCompletionOffersKeywordsPrimitivesAndArgs` and `TestCompletionEndToEnd` both PASS, unchanged behavior.

- [ ] **Step 4: Confirm the full suite still passes**

Run:
```bash
go test ./...
```
Expected: `ok` everywhere.

- [ ] **Step 5: Commit**

```bash
git add lsp/completion.go lsp/lsp_test.go
git commit -m "$(cat <<'EOF'
Export lsp.CompletionItems for reuse by the REPL

Pure rename — no behavior change. The REPL's upcoming tab completion
(docs/superpowers/specs/2026-07-21-repl-tab-completion-design.md) reuses
this same context-aware decision function instead of reimplementing
keyword/primitive/arg-label completion.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: `completeToken` — the pure completion-candidate function

**Files:**
- Create: `cmd/domain/repl_complete.go`
- Test: `cmd/domain/repl_complete_test.go`

**Interfaces:**
- Consumes: `lsp.CompletionItems(prefix string) []map[string]any` from Task 1.
- Produces: `func completeToken(line string, cursor int, baseDir string) (candidates []string, tokenStart int)` — Task 3 calls this from `repl_tty.go`'s `Update`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/domain/repl_complete_test.go`:

```go
package main

import (
	"os"
	"testing"
)

func TestCompleteTokenKeyword(t *testing.T) {
	candidates, tokenStart := completeToken("Cursed T", 8, ".")
	if tokenStart != 0 {
		t.Errorf("tokenStart = %d, want 0", tokenStart)
	}
	if len(candidates) != 1 || candidates[0] != "Cursed Technique: " {
		t.Errorf(`candidates = %v, want ["Cursed Technique: "]`, candidates)
	}
}

func TestCompleteTokenPrimitive(t *testing.T) {
	line := "Domain Expansion: BF"
	candidates, tokenStart := completeToken(line, len(line), ".")
	want := len("Domain Expansion: ")
	if tokenStart != want {
		t.Errorf("tokenStart = %d, want %d", tokenStart, want)
	}
	if len(candidates) != 1 || candidates[0] != "BFS" {
		t.Errorf(`candidates = %v, want ["BFS"]`, candidates)
	}
}

func TestCompleteTokenMultiWordPrimitiveNotTruncated(t *testing.T) {
	// A word-boundary rule based on trailing spaces would wrongly narrow
	// this to just "By" and lose "Sort" — the primitive names themselves
	// contain spaces ("Sort By", "Flood Fill", ...).
	line := "Domain Expansion: Sort B"
	candidates, tokenStart := completeToken(line, len(line), ".")
	want := len("Domain Expansion: ")
	if tokenStart != want {
		t.Errorf("tokenStart = %d, want %d", tokenStart, want)
	}
	if len(candidates) != 1 || candidates[0] != "Sort By" {
		t.Errorf(`candidates = %v, want ["Sort By"]`, candidates)
	}
}

func TestCompleteTokenReplCommand(t *testing.T) {
	candidates, tokenStart := completeToken(":lo", 3, ".")
	// tokenStart is 0, not 1: the candidate already includes the leading
	// ':' (it replaces the whole command), so splicing it in at
	// line[:tokenStart] + candidate + line[cursor:] must not also keep
	// line's own leading ':' — that would double it up into "::load".
	if tokenStart != 0 {
		t.Errorf("tokenStart = %d, want 0", tokenStart)
	}
	if len(candidates) != 1 || candidates[0] != ":load" {
		t.Errorf(`candidates = %v, want [":load"]`, candidates)
	}
}

func TestCompleteTokenFilePath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("nums.txt", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	line := "Cursed Energy: nu"
	candidates, tokenStart := completeToken(line, len(line), ".")
	want := len("Cursed Energy: ")
	if tokenStart != want {
		t.Errorf("tokenStart = %d, want %d", tokenStart, want)
	}
	found := false
	for _, c := range candidates {
		if c == "nums.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("candidates = %v, missing nums.txt", candidates)
	}
}

func TestCompleteTokenNoMatch(t *testing.T) {
	candidates, _ := completeToken("zzz", 3, ".")
	if len(candidates) != 0 {
		t.Errorf("candidates = %v, want none", candidates)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run:
```bash
go test ./cmd/domain/... -run TestCompleteToken -v
```
Expected: FAIL with `undefined: completeToken` (the file doesn't exist yet).

- [ ] **Step 3: Write `repl_complete.go`**

```go
// Tab-completion candidates for the REPL's interactive editor. Three
// sources, picked by cursor position: file paths (Cursed Energy:, :load,
// :save targets), the REPL's own :commands, and — for everything else —
// the language server's context-aware keyword/primitive/arg-label/Mode
// completion (lsp.CompletionItems), reused as-is.
package main

import (
	"os"
	"path/filepath"
	"strings"

	"domain/lsp"
)

// replCommands are the REPL's own :directives (repl.go's command method) —
// the language server has no concept of these.
var replCommands = []string{"help", "list", "type", "undo", "reset", "load", "save", "quit"}

// completeToken returns the completion candidates for the token ending at
// cursor (a byte offset, assumed ASCII up to the cursor like the rest of
// repl_tty.go) in line, plus the byte offset where that token starts. No
// match returns a nil candidates slice.
func completeToken(line string, cursor int, baseDir string) (candidates []string, tokenStart int) {
	prefix := line[:cursor]

	if path, start, ok := filePathContext(prefix); ok {
		return completeFilePath(path, baseDir), start
	}

	if strings.HasPrefix(prefix, ":") {
		word := strings.ToLower(prefix[1:])
		var out []string
		for _, name := range replCommands {
			if strings.HasPrefix(name, word) {
				out = append(out, ":"+name)
			}
		}
		// tokenStart is 0, not 1: candidates already include the leading
		// ':' (they replace the whole command, not just the part after
		// the colon) — pairing tokenStart=1 with a ':'-prefixed candidate
		// would double it up into "::load".
		return out, 0
	}

	items := lsp.CompletionItems(prefix)
	// The word in progress: everything after the first colon (a keyword or
	// arg-label's colon), or the whole trimmed line when there's no colon
	// yet — mirrors CompletionItems' own splitKeyword logic. Unlike a
	// trailing-space split, this keeps multi-word candidates ("Cursed
	// Technique", "Sort By") intact instead of truncating to their last word.
	trimmed := strings.TrimLeft(prefix, " \t")
	word := trimmed
	if i := strings.IndexByte(trimmed, ':'); i >= 0 {
		word = strings.TrimLeft(trimmed[i+1:], " \t")
	}
	tokenStart = cursor - len(word)
	wordLower := strings.ToLower(word)
	var out []string
	for _, item := range items {
		label, _ := item["label"].(string)
		if strings.HasPrefix(strings.ToLower(label), wordLower) {
			insertText, _ := item["insertText"].(string)
			out = append(out, insertText)
		}
	}
	return out, tokenStart
}

// filePathContext recognizes a file-target position (Cursed Energy:,
// :load, :save — each requiring at least one space/tab before the path
// starts) and returns the partial path typed so far and the byte offset
// where it starts.
func filePathContext(prefix string) (path string, start int, ok bool) {
	type target struct {
		keyword string
		fold    bool
	}
	targets := []target{
		{"Cursed Energy:", true},
		{":load", false},
		{":save", false},
	}
	for _, tgt := range targets {
		if len(prefix) < len(tgt.keyword) {
			continue
		}
		head := prefix[:len(tgt.keyword)]
		matched := head == tgt.keyword
		if tgt.fold {
			matched = strings.EqualFold(head, tgt.keyword)
		}
		if !matched {
			continue
		}
		rest := prefix[len(tgt.keyword):]
		if len(rest) == 0 || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		trimmed := strings.TrimLeft(rest, " \t")
		return trimmed, len(prefix) - len(trimmed), true
	}
	return "", 0, false
}

// completeFilePath lists the directory entries under baseDir matching the
// already-typed partial path, each as a full relative path (a trailing /
// on directories, so cycling into one and pressing Tab again completes a
// level deeper). os.ReadDir already returns entries sorted by name.
func completeFilePath(partial, baseDir string) []string {
	dir, base := filepath.Split(partial)
	entries, err := os.ReadDir(filepath.Join(baseDir, dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), base) {
			continue
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, dir+name)
	}
	return out
}
```

- [ ] **Step 4: Run the tests again**

Run:
```bash
go build ./... && go test ./cmd/domain/... -run TestCompleteToken -v
```
Expected: all six tests PASS.

- [ ] **Step 5: Confirm the full suite still passes**

Run:
```bash
go test ./...
```
Expected: `ok` everywhere.

- [ ] **Step 6: Commit**

```bash
git add cmd/domain/repl_complete.go cmd/domain/repl_complete_test.go
git commit -m "$(cat <<'EOF'
Add completeToken: REPL tab-completion candidate lookup

Pure function picking between three sources by cursor position: file paths
(Cursed Energy:/:load/:save targets, via os.ReadDir), the REPL's own
:commands, and lsp.CompletionItems for everything else (keywords,
primitives, arg labels, Mode values) — reused as-is, no reimplementation.
Not wired into the editor yet — that's the next commit.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Wire Tab into the editor's cycling state

**Files:**
- Modify: `cmd/domain/repl_tty.go`
- Test: `cmd/domain/repl_tty_test.go` (append)

**Interfaces:**
- Consumes: `completeToken` from Task 2.
- Produces: `replModel` gains `completing bool`, `candidates []string`, `candIdx int`, `tokenStart int`, and a new method `func (m replModel) completeTab() (tea.Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/domain/repl_tty_test.go`:

```go
func TestReplTTYTabCompletesUniqueMatch(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("Cursed T")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(replModel)

	if got := m.ti.Value(); got != "Cursed Technique: " {
		t.Fatalf("tab did not complete the keyword: %q", got)
	}
	if !m.completing {
		t.Error("expected completing to be true right after Tab")
	}
}

func TestReplTTYTabCyclesThroughMultipleCandidates(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("Domain Expansion: Sort")
	press := func() {
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(replModel)
	}

	press()
	first := m.ti.Value()
	if first != "Domain Expansion: Sort" && first != "Domain Expansion: Sort By" {
		t.Fatalf("unexpected first completion: %q", first)
	}

	press()
	second := m.ti.Value()
	if second == first {
		t.Fatal("second tab did not advance to a different candidate")
	}
	if second != "Domain Expansion: Sort" && second != "Domain Expansion: Sort By" {
		t.Fatalf("unexpected second completion: %q", second)
	}

	press()
	third := m.ti.Value()
	if third != first {
		t.Errorf("third tab should wrap back to the first candidate: got %q, want %q", third, first)
	}
}

func TestReplTTYTabCompletionResetsOnOtherKey(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("Cursed T")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(replModel)

	next, _ = m.Update(tea.KeyPressMsg{Text: "x"})
	m = next.(replModel)

	if m.completing {
		t.Error("typing a character should exit completion cycling")
	}
	if got := m.ti.Value(); got != "Cursed Technique: x" {
		t.Errorf("typed character should append after the accepted completion: %q", got)
	}
}

func TestReplTTYTabNoMatchIsNoOp(t *testing.T) {
	m := newReplModel()
	m.ti.SetValue("zzz")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m2 := next.(replModel)

	if cmd != nil {
		t.Error("no-match tab should not print anything")
	}
	if got := m2.ti.Value(); got != "zzz" {
		t.Errorf("no-match tab should leave the line untouched: %q", got)
	}
	if m2.completing {
		t.Error("no-match tab should not enter completing state")
	}
}

func TestReplTTYTabCompletesReplCommandWithoutDoublingColon(t *testing.T) {
	// Regression: completeToken's :command candidates already include the
	// leading ':', so a tokenStart that also preserves the line's own ':'
	// would splice in a second one ("::load").
	m := newReplModel()
	m.ti.SetValue(":lo")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(replModel)

	if got := m.ti.Value(); got != ":load" {
		t.Errorf("tab did not complete the :command cleanly: %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run:
```bash
go test ./cmd/domain/... -run TestReplTTYTab -v
```
Expected: FAIL TO COMPILE — `m.completing`/`m2.completing` are referenced by three of the four new tests, but that field doesn't exist on `replModel` until Step 3 adds it. (Even without that, `tab` isn't handled yet: it falls through to `ti.Update`, which — Tab is bound only to its unused `AcceptSuggestion`/suggestions feature — leaves the line unchanged.) This is expected: the next step adds both the struct fields and the behavior together.

- [ ] **Step 3: Add the cycling fields, the `tab` case, and `completeTab`**

In `cmd/domain/repl_tty.go`, add four fields to the `replModel` struct:

```go
type replModel struct {
	ti      textinput.Model
	core    *repl
	buf     *strings.Builder
	seen    int // bytes of buf already echoed via tea.Println
	history []string
	histIdx int

	completing bool
	candidates []string
	candIdx    int
	tokenStart int
}
```

Replace the whole `Update` method with:

```go
func (m replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() != "tab" {
			m.completing = false
			m.candidates = nil
		}
		switch msg.String() {
		case "tab":
			return m.completeTab()
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+d":
			if m.ti.Value() == "" {
				return m, tea.Quit
			}
		case "up":
			if m.histIdx > 0 {
				m.histIdx--
				m.ti.SetValue(m.history[m.histIdx])
				m.ti.CursorEnd()
			}
			return m, nil
		case "down":
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.ti.SetValue(m.history[m.histIdx])
				m.ti.CursorEnd()
			} else {
				m.histIdx = len(m.history)
				m.ti.SetValue("")
			}
			return m, nil
		case "enter", "ctrl+enter", "alt+enter":
			line := strings.TrimRight(m.ti.Value(), " \t\r")
			force := msg.String() != "enter" && len(m.core.pending) == 0
			if force && line == "" {
				return m, nil
			}
			var quit bool
			if force {
				m.core.pending = []string{line}
			} else {
				quit = m.core.handleLine(line)
			}
			cmd := m.submitLine(line)
			if quit {
				return m, tea.Sequence(cmd, tea.Quit)
			}
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

// completeTab starts or advances a Tab-completion cycle: the first Tab on a
// token computes candidates via completeToken and shows the first one;
// each subsequent Tab (while still completing) advances to the next,
// wrapping around. Any other key (handled in Update, above) resets the
// cycle before its own handling runs.
func (m replModel) completeTab() (tea.Model, tea.Cmd) {
	value := m.ti.Value()
	cursor := m.ti.Position() // assumed ASCII up to the cursor, like the rest of the REPL

	if m.completing {
		m.candIdx = (m.candIdx + 1) % len(m.candidates)
	} else {
		candidates, tokenStart := completeToken(value, cursor, m.core.baseDir)
		if len(candidates) == 0 {
			return m, nil
		}
		m.candidates = candidates
		m.tokenStart = tokenStart
		m.candIdx = 0
		m.completing = true
	}

	candidate := m.candidates[m.candIdx]
	newValue := value[:m.tokenStart] + candidate + value[cursor:]
	m.ti.SetValue(newValue)
	m.ti.SetCursor(m.tokenStart + len(candidate))
	return m, nil
}
```

- [ ] **Step 4: Run the tests again**

Run:
```bash
go build ./... && go test ./cmd/domain/... -run TestReplTTY -v
```
Expected: every `TestReplTTY*` test PASSes, including the four new ones.

- [ ] **Step 5: Confirm the full suite still passes**

Run:
```bash
go test ./...
```
Expected: `ok` everywhere.

- [ ] **Step 6: Commit**

```bash
git add cmd/domain/repl_tty.go cmd/domain/repl_tty_test.go
git commit -m "$(cat <<'EOF'
Wire Tab completion into the REPL editor

Tab starts a completion cycle via completeToken; repeated Tab advances
through the candidates in place (no list/menu rendered), wrapping around.
Any other key resets the cycle first, so typing continues the shown
completion and Enter submits it — matching bash/zsh menu-complete rather
than the up/down-based cycling bubbles/textinput's own (unused) suggestion
system would have collided with REPL history.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Docs and manual verification

**Files:**
- Modify: `docs/tooling.md`

- [ ] **Step 1: Update `docs/tooling.md`**

In `docs/tooling.md`, in the "**Interactive terminals**" paragraph added by the previous REPL plan, append a sentence after the existing ctrl+enter/alt+enter sentence (before "Piped input..."):

```markdown
Tab completes keywords, primitives, argument labels/Mode values (reusing
the language server's own completion logic), REPL `:commands`, and file
paths for `Cursed Energy:`/`:load`/`:save` targets — repeated Tab cycles
through multiple matches in place; typing anything else or pressing enter
accepts whichever one is currently shown.
```

- [ ] **Step 2: Manual verification in a real terminal**

Use the `run`/`verify` skill workflow to launch `domain repl` in an actual interactive terminal (or drive it via a tmux/PTY session, matching how the previous REPL feature was verified) and confirm, by hand:
- Typing `Cursed T` then Tab completes to `Cursed Technique: `.
- Typing `Domain Expansion: Sort` then Tab, Tab, Tab cycles between `Sort` and `Sort By` and wraps back to the first.
- Typing `:lo` then Tab completes to `:load`.
- In a directory with a file, typing `Cursed Energy: ` then Tab lists/completes to that file.
- After any of the above, typing a character continues from the completed text, and pressing enter submits it.

This step has no automated pass/fail — record what you observed; if anything doesn't match, fix it before moving on.

- [ ] **Step 3: Final full-suite check and commit**

Run:
```bash
go build ./... && go test ./...
```
Expected: `ok` everywhere.

```bash
git add docs/tooling.md
git commit -m "$(cat <<'EOF'
Document REPL tab completion

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```
