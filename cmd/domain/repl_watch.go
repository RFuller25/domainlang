// `:watch` — replay when the input changes.
//
// The replay model makes this nearly free: the session already re-runs the
// whole program on every statement, so re-running it when a *file* changes is
// the same machinery with a different trigger. What it buys is the loop an
// Advent of Code afternoon actually is — edit the input in one window, watch
// the answer change in the other — without a keystroke in between.
//
// The trigger is a poll rather than an OS watcher: one stat of one file every
// half second costs nothing measurable, works the same on every platform this
// binary targets, and cannot miss a change the way an inotify queue can. The
// file's modification time *and* size are compared, because an editor that
// writes twice within a filesystem's timestamp resolution changes the size far
// more often than not.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
)

// watchInterval is how often a watched file is checked. It is a variable so
// tests do not have to wait for it.
var watchInterval = 500 * time.Millisecond

// watchTickMsg asks whether a watched file has changed.
type watchTickMsg struct{ gen int }

// watch is what the session is watching: a file, and how it last looked.
type watch struct {
	path    string
	modTime time.Time
	size    int64
	gen     int // invalidates the ticks of a watch that has been replaced
}

// stat reads the file's current shape, reporting whether it could be read.
func (w *watch) stat() (time.Time, int64, bool) {
	info, err := os.Stat(w.path)
	if err != nil {
		return time.Time{}, 0, false
	}
	return info.ModTime(), info.Size(), true
}

// changed reports whether the file differs from the last look, and records the
// new shape. A file that cannot be read is not a change: an editor writing
// through a temporary file makes it vanish for an instant, and re-running on
// that would report a failure the user never made.
func (w *watch) changed() bool {
	modTime, size, ok := w.stat()
	if !ok {
		return false
	}
	if modTime.Equal(w.modTime) && size == w.size {
		return false
	}
	w.modTime, w.size = modTime, size
	return true
}

// startWatch begins watching path, resolved against the session's base
// directory the same way a `Cursed Energy:` target is.
func (m *replModel) startWatch(path string) (string, error) {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(m.core.baseDir, path)
	}
	if _, err := os.Stat(full); err != nil {
		return "", err
	}
	m.watchGen++
	w := &watch{path: full, gen: m.watchGen}
	w.modTime, w.size, _ = w.stat()
	m.watch = w
	return full, nil
}

// watchTick schedules the next look at the watched file.
func (m replModel) watchTick() tea.Cmd {
	if m.watch == nil {
		return nil
	}
	gen := m.watch.gen
	return tea.Tick(watchInterval, func(time.Time) tea.Msg { return watchTickMsg{gen: gen} })
}

// watchCommand handles `:watch [file]` — with a file it starts watching, with
// nothing it stops. It reports whether it took the line.
func (m replModel) watchCommand(line string) (bool, tea.Model, tea.Cmd) {
	name, arg := splitCommand(line)
	if name != ":watch" {
		return false, m, nil
	}
	m.setLine("")
	if arg == "" {
		if m.watch == nil {
			m.status = "usage: :watch <file> — replays whenever that file changes"
			return true, m, nil
		}
		m.watch = nil
		m.watchGen++
		m.status = "(no longer watching)"
		return true, m, nil
	}
	full, err := m.startWatch(arg)
	if err != nil {
		m.status = fmt.Sprintf("cannot watch %s: %v", arg, err)
		return true, m, nil
	}
	m.status = fmt.Sprintf("watching %s — every change replays the program", filepath.Base(full))
	return true, m, m.watchTick()
}
