// Which tools a scope's programs can be pointed at.
//
// Most of the toolbelt does not care what kind of program it is handed. `fmt`
// and `check` never run anything; `lint`, `diagnosis` and `fix` read the
// resolved program; `battle`, `stats`, `coverage` and `mahoraga` run it
// through its own host and compare what came out, which for a game is its
// frames.
//
// Two tools do care, and for one reason: they are built on *one value moving
// through one chain of stages*. The stepper shows that value at each stage;
// the REPL is that chain, extended a line at a time. A scope whose programs
// are Parts driven by events has no such chain — a game's stages belong to
// different bodies and run in an order the program does not state — so the
// honest answer is a sentence rather than a display that is wrong in a way
// nobody would notice.
//
// The question is asked of the scope rather than of a name (prims.IsOnePipeline),
// so a scope added later is refused by these two without either of them
// learning about it.
package main

import (
	"fmt"

	"domain/prims"
)

// scopeRefusal is a tool saying it is the wrong tool.
//
// It is a distinct type because it is *not a diagnostic about the program*:
// the program may be perfectly good, and anything that answers an error by
// re-running the diagnostics (the REPL does) would replace this sentence with
// a complaint about a missing Part, which is true and unhelpful.
type scopeRefusal struct{ msg string }

func (e *scopeRefusal) Error() string { return e.msg }

// refuseUnlessOnePipeline reports why a stepping tool cannot be pointed at
// this program, or nil when it can.
func refuseUnlessOnePipeline(scope, tool, premise string) error {
	if prims.IsOnePipeline(scope) {
		return nil
	}
	return &scopeRefusal{msg: fmt.Sprintf("%s is built to %s, and a %s program is not one pipeline — "+
		"its Parts are driven by events, so there is no single chain of stages to walk. "+
		"Run it instead: `domain run <file>` replays it from a script on stdin and writes its frames to stdout",
		tool, premise, scope)}
}
