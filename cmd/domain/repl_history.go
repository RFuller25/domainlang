// The REPL's line history: recall within a session, and across them.
//
// Three properties a shell taught everyone to expect, and which the REPL used
// to be missing:
//
//   - the line you are typing is not destroyed by pressing Up. It is parked as
//     a draft and comes back when you walk past the end of the history again.
//   - a line repeated back-to-back is stored once. Re-running `:list` five
//     times should not cost five presses of Up to walk past.
//   - the history outlives the session, in the user's state directory
//     ($XDG_STATE_HOME, else ~/.local/state). A REPL you have to retype into
//     after every restart is a REPL you stop reaching for.
//
// Every filesystem failure here is silent by design: no home directory, a
// read-only disk, a corrupt file — none of that is a reason to refuse to run a
// REPL, so the session simply keeps its history in memory.
package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxHistory bounds what is kept on disk. It is generous — history is one
// short line per entry — but not unbounded.
const maxHistory = 1000

// history is the recall list: entries oldest first, plus a cursor that walks
// them and the draft the cursor walked away from.
type history struct {
	entries []string
	idx     int    // cursor; len(entries) means "at the draft"
	draft   string // the line being typed when the walk started
	path    string // where to persist, "" when the location is unavailable
}

// newHistory loads the persisted history, if there is one to load.
func newHistory() *history {
	h := &history{path: historyPath()}
	if h.path != "" {
		if data, err := os.ReadFile(h.path); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if line != "" {
					h.entries = append(h.entries, line)
				}
			}
		}
	}
	h.trim()
	h.idx = len(h.entries)
	return h
}

// historyPath is $XDG_STATE_HOME/domain/repl_history, following the XDG base
// directory spec's default of ~/.local/state.
func historyPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "domain", "repl_history")
}

// add records a submitted line and returns the cursor to the newest end.
// Blank lines and an immediate repeat of the previous line are not recorded.
func (h *history) add(line string) {
	defer func() { h.idx, h.draft = len(h.entries), "" }()
	if strings.TrimSpace(line) == "" {
		return
	}
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == line {
		return
	}
	h.entries = append(h.entries, line)
	h.trim()
}

// prev walks one entry back, parking current as the draft on the first step.
// It reports the line to show, and false when there is nothing older.
func (h *history) prev(current string) (string, bool) {
	if h.idx == 0 || len(h.entries) == 0 {
		return "", false
	}
	if h.idx == len(h.entries) {
		h.draft = current
	}
	h.idx--
	return h.entries[h.idx], true
}

// next walks one entry forward, returning the parked draft once the walk runs
// off the newest end. It reports false when already at the draft.
func (h *history) next() (string, bool) {
	if h.idx >= len(h.entries) {
		return "", false
	}
	h.idx++
	if h.idx == len(h.entries) {
		return h.draft, true
	}
	return h.entries[h.idx], true
}

// trim keeps the newest maxHistory entries.
func (h *history) trim() {
	if len(h.entries) > maxHistory {
		h.entries = slices.Clone(h.entries[len(h.entries)-maxHistory:])
	}
}

// save writes the history back, best effort.
func (h *history) save() {
	if h.path == "" || len(h.entries) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(h.path, []byte(strings.Join(h.entries, "\n")+"\n"), 0o600)
}
