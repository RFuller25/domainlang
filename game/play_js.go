//go:build js

// The playing half, in a browser.
//
// Bubble Tea does not build for js/wasm: it opens a terminal, reads raw input
// and listens for resize signals, none of which exist there. Nor does the
// playground want it to — a program in a browser has no terminal to play on,
// so it replays, which is the same thing every test and every documented
// example does.
//
// So this is the whole of the difference: playable is false, and the loop is
// never reached. Keeping it to two functions rather than tagging the host is
// deliberate — the Parts, the world, the clock and the frames are the same
// code on both, and only the edge that touches a terminal is not.
package game

import (
	"fmt"

	"domain/interp"
	"domain/ir"
)

func playable(*ir.Context) bool { return false }

func playGame(*game, *interp.Session) (ir.Value, error) {
	return nil, fmt.Errorf("a game cannot be played in a browser; it replays from a script instead")
}
