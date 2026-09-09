package prims

import (
	"strings"
	"testing"
)

// A whole Game Dev program: every role, in an order that proves the hoists.
const gameSrc = `Innate Domain: Game Dev

Part Draw:
    Cursed Technique: Apply
        Using: (w) -> box(text(w.score))

Part Every 120:
    Shikigami: Bump

Part On "key right":
    Cursed Technique: Apply
        Using: (w) -> with(w, "dir", "right")

Part Entity "Bump":
    Cursed Technique: Apply
        Using: (w) -> with(w, "score", w.score + 1)

Part World:
    Cursed Technique: Apply
        Using: (w) -> {score: 0, dir: "left"}

Part Ending:
    Cursed Technique: Apply
        Using: (w) -> w.score
    Reveal: stdout
`

// The World is written last here and the Entity is called before it is
// defined, because neither position may matter: the world is hoisted so every
// other role has a type to be checked against, and an Entity is lowered to a
// Shikigami before inference runs so its name is callable everywhere.
func TestGameProgramResolves(t *testing.T) {
	pipe, err := resolveSrc(t, gameSrc)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pipe.Scope != "Game Dev" {
		t.Errorf("scope = %q, want Game Dev", pipe.Scope)
	}
	// The world resolves first whatever the source order, so a host reading
	// Parts off the pipeline finds it before anything that needs it.
	if len(pipe.Nodes) == 0 {
		t.Fatal("no nodes")
	}
	if role, _ := pipe.Nodes[0].Meta["role"].(string); role != "World" {
		t.Errorf("first node is role %q, want World — the world must be hoisted", role)
	}
	// An Entity is a definition, not a stage: it contributes no node.
	for _, n := range pipe.Nodes {
		if role, _ := n.Meta["role"].(string); role == "Entity" {
			t.Error("an Entity Part produced a node; it should lower to a Shikigami")
		}
	}
}

func TestGameRefusals(t *testing.T) {
	world := "Part World:\n    Cursed Technique: Apply\n        Using: (w) -> {a: 1}\n"
	tick := "Part Every 100:\n    Cursed Technique: Apply\n        Using: (w) -> w\n"
	draw := "Part Draw:\n    Cursed Technique: Apply\n        Using: (w) -> text(w.a)\n"
	head := "Innate Domain: Game Dev\n"

	cases := []struct{ name, src, want string }{
		{"a top-level pipeline statement",
			head + world + tick + draw + "Cursed Energy: stdin\n",
			"made of Parts and declarations, not a pipeline"},
		{"Draw must produce a View",
			head + world + tick + "Part Draw:\n    Cursed Technique: Apply\n        Using: (w) -> w.a\n",
			"Part Draw must produce View, but its body produces Int"},
		{"an event must preserve the world",
			head + world + "Part Every 100:\n    Cursed Technique: Apply\n        Using: (w) -> w.a\n" + draw,
			"must produce {a:Int}, but its body produces Int"},
		{"the world must be a Record",
			head + "Part World:\n    Cursed Technique: Apply\n        Using: (w) -> 5\n" +
				"Part Every 100:\n    Cursed Technique: Apply\n        Using: (w) -> w\n" +
				"Part Draw:\n    Cursed Technique: Apply\n        Using: (w) -> text(w)\n",
			"must produce a Record"},
		{"no Draw", head + world + tick, "needs one Part Draw, and this program has none"},
		{"no World", head + tick + draw, "needs one Part World"},
		{"nothing changes the world", head + world + draw, "needs something that changes the world"},
		{"two Draws",
			head + world + tick + draw + draw,
			"already defined"},
		{"a role from another Innate Domain",
			"Cursed Energy: stdin\nPart Draw:\n    Reveal: stdout\n",
			"belongs to the Game Dev Innate Domain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := resolveSrc(t, c.src)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v,\nwant it to mention %q", err, c.want)
			}
		})
	}
}

// A whole-program refusal has no line of its own, and saying "0:0" would send
// the reader somewhere there is nothing to see.
func TestWholeProgramRefusalsCarryNoPosition(t *testing.T) {
	src := "Innate Domain: Game Dev\n" +
		"Part World:\n    Cursed Technique: Apply\n        Using: (w) -> {a: 1}\n" +
		"Part Every 100:\n    Cursed Technique: Apply\n        Using: (w) -> w\n"
	_, err := resolveSrc(t, src)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.HasPrefix(err.Error(), "0:0") {
		t.Errorf("error = %q, want no position on a whole-program refusal", err)
	}
}

// The globals freeze once the program is running: World and Start may write,
// and nothing else may. It is what keeps a frame a function of the world.
func TestGameGlobalsFreezeAfterStart(t *testing.T) {
	head := "Innate Domain: Game Dev\nCursed Object: level As 3\n"
	world := "Part World:\n    Cursed Technique: Apply\n        Using: (w) -> {a: level}\n"
	draw := "Part Draw:\n    Cursed Technique: Apply\n        Using: (w) -> text(w.a)\n"

	// Reading one anywhere is fine — that is what globals are for.
	ok := head + world + "Part Every 100:\n    Cursed Technique: Apply\n        Using: (w) -> with(w, \"a\", level)\n" + draw
	if _, err := resolveSrc(t, ok); err != nil {
		t.Fatalf("reading a global from an event body should be fine: %v", err)
	}

	// Writing one from an event body is not.
	bad := head + world + "Part Every 100:\n    Cursed Tool: level As 4\n" + draw
	_, err := resolveSrc(t, bad)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "freezes the globals") {
		t.Errorf("error = %v, want it to explain the freeze", err)
	}

	// Writing one from Start is, because Start runs before the first frame.
	fromStart := head + world +
		"Part Start:\n    Cursed Tool: level As 4\n" +
		"Part Every 100:\n    Cursed Technique: Apply\n        Using: (w) -> w\n" + draw
	if _, err := resolveSrc(t, fromStart); err != nil {
		t.Fatalf("Start should be able to write a global: %v", err)
	}
}

// Two timers at different periods are two Parts; the same period twice is one
// Part written twice.
func TestGameTimersAreKeyedByPeriod(t *testing.T) {
	head := "Innate Domain: Game Dev\n" +
		"Part World:\n    Cursed Technique: Apply\n        Using: (w) -> {a: 1}\n" +
		"Part Draw:\n    Cursed Technique: Apply\n        Using: (w) -> text(w.a)\n"
	tick := func(ms string) string {
		return "Part Every " + ms + ":\n    Cursed Technique: Apply\n        Using: (w) -> w\n"
	}
	if _, err := resolveSrc(t, head+tick("100")+tick("500")); err != nil {
		t.Errorf("two periods should be two timers: %v", err)
	}
	if _, err := resolveSrc(t, head+tick("100")+tick("100")); err == nil {
		t.Error("the same period twice should be a duplicate")
	}
}
