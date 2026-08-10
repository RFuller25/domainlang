// Undo, broken by pause.
//
// The unit of undo is the decision, not the keystroke. Typing `Maximum
// Technique: Sum` and undoing it a character at a time would be twenty-one
// presses to withdraw one thought, so a run of typing coalesces into a single
// step and the run ends when the typing does. That makes undo follow the
// rhythm of writing rather than the count of keys, which is the thing a person
// actually remembers doing.
//
// A discrete action — a paste, a cut, an indent, a delete of a selection — is
// its own step whatever the timing, because it was one decision when it was
// made and should be one when it is withdrawn.
//
// Steps are whole snapshots of the buffer rather than diffs. A Domain program
// is tens of lines, so a snapshot is a few hundred bytes and a hundred of them
// cost less than the machinery for computing and inverting edits would. The
// depth limit exists so that a long session cannot grow without bound, not
// because the memory matters at any plausible size.
package main

import "time"

// devUndoPause is how long typing has to stop before the next keystroke starts
// a new undo step. Long enough that a pause for thought inside a line does not
// split it, short enough that two separate edits are two steps. A variable so
// tests can drive the editor without waiting.
var devUndoPause = 600 * time.Millisecond

// devUndoDepth bounds the history. Fifty steps is far more than the distance
// anyone reaches back through by pressing a key repeatedly.
const devUndoDepth = 50

// devSnapshot is the buffer at a moment: the text, and where the cursor was in
// it. The cursor is part of the snapshot because undo should return you to
// where the edit happened, not leave you wherever you have since wandered.
type devSnapshot struct {
	lines []string
	row   int
	col   int
}

type devUndoStack struct {
	past   []devSnapshot
	future []devSnapshot
	// lastEdit is when the most recent change was recorded, which is what the
	// pause is measured from.
	lastEdit time.Time
	// openRun reports whether a coalescing run is in progress. Without it the
	// first keystroke after an undo would coalesce into the step it just
	// restored, and the undo would be undone by continuing to type.
	openRun bool
}

func snapshot(b *devBuffer) devSnapshot {
	lines := make([]string, len(b.lines))
	copy(lines, b.lines)
	return devSnapshot{lines: lines, row: b.row, col: b.col}
}

func (s devSnapshot) restore(b *devBuffer) {
	b.lines = make([]string, len(s.lines))
	copy(b.lines, s.lines)
	b.row = min(s.row, len(b.lines)-1)
	b.col = s.col
	b.goalCol = s.col
	b.anchor = nil
	b.clampCol()
}

// record is called immediately *before* a change, with typing=true when the
// change is an ordinary keystroke and may join the run in progress.
//
// It pushes a snapshot only when a new step is starting: the state saved is
// therefore the state before the whole run, which is what undoing that run
// should restore.
func (u *devUndoStack) record(b *devBuffer, typing bool, now time.Time) {
	newStep := !typing || !u.openRun || now.Sub(u.lastEdit) >= devUndoPause
	if newStep {
		u.past = append(u.past, snapshot(b))
		if len(u.past) > devUndoDepth {
			u.past = u.past[len(u.past)-devUndoDepth:]
		}
		// Anything new abandons the redo branch: there is no longer one future
		// to go back to.
		u.future = nil
	}
	u.lastEdit = now
	// A discrete action does not start a run, so the keystroke after a paste
	// begins a step of its own rather than joining the paste.
	u.openRun = typing
}

// undo restores the previous step, keeping the current one for redo.
func (u *devUndoStack) undo(b *devBuffer) bool {
	if len(u.past) == 0 {
		return false
	}
	u.future = append(u.future, snapshot(b))
	last := u.past[len(u.past)-1]
	u.past = u.past[:len(u.past)-1]
	last.restore(b)
	// The run is over: typing after an undo must not merge into the step that
	// was just withdrawn.
	u.openRun = false
	return true
}

func (u *devUndoStack) redo(b *devBuffer) bool {
	if len(u.future) == 0 {
		return false
	}
	u.past = append(u.past, snapshot(b))
	next := u.future[len(u.future)-1]
	u.future = u.future[:len(u.future)-1]
	next.restore(b)
	u.openRun = false
	return true
}
