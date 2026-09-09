package prims

import (
	"strings"
	"testing"

	"domain/ir"
)

// withTestScope registers a scope for the duration of one test. The role
// machinery has no user in this build — `Advent of Code` declares the single
// unroled role and nothing else — so this is what actually exercises it, and
// it is the shape the Game Dev scope will take.
func withTestScope(t *testing.T, sc *Scope) {
	t.Helper()
	saved := Scopes
	Scopes = append(append([]*Scope{}, Scopes...), sc)
	t.Cleanup(func() { Scopes = saved })
}

// fromWorld is the contract a role that transforms the world states.
func fromWorld(world *ir.Type) *ir.Type { return world }

// roleScope is a miniature scope: a world-defining role, a renderer with a
// declared output type, a timer taking a number, and an unbounded handler.
func roleScope() *Scope {
	return &Scope{
		Name:    "Test Realm",
		Summary: "A scope that exists only inside prims' own tests.",
		// Parts only, like a real scope with roles: a top-level pipeline and a
		// world-defining Part are answers to the same question and cannot
		// both be the shape of a program.
		TopLevelPipeline: false,
		PartRoles: []PartRole{
			{
				Name: "World", Arg: ArgNone, Min: 1, Max: 1,
				DefinesWorld: true, Globals: GlobalsReadWrite,
				In: func(*ir.Type) *ir.Type { return ir.Record() },
			},
			{
				Name: "Vista", Arg: ArgNone, Min: 0, Max: 1,
				Globals: GlobalsReadOnly,
				In:      fromWorld,
				Out:     func(*ir.Type) *ir.Type { return ir.Text() },
			},
			{Name: "Every", Arg: ArgInt, Min: 0, Max: -1, Globals: GlobalsReadOnly, In: fromWorld},
			{Name: "On", Arg: ArgLabel, Min: 0, Max: -1, Globals: GlobalsReadOnly, In: fromWorld},
		},
	}
}

func resolveInScope(t *testing.T, sc *Scope, src string) error {
	t.Helper()
	withTestScope(t, sc)
	_, err := resolveSrc(t, "Innate Domain: "+sc.Name+"\n"+src)
	return err
}

// aWorld is a valid `Part World:` for roleScope, prefixed to the sources whose
// subject is something else: a scope that defines a world requires one before
// any role stated against it can be checked at all.
const aWorld = "Part World:\n    Cursed Technique: Apply\n        Using: (w) -> {a: 1}\n"

func TestPartRoleArgumentShapes(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"World takes no argument",
			aWorld + "Part World \"x\":\n    Cursed Energy: stdin\n", "takes no label or number"},
		{"Every needs a number",
			aWorld + "Part Every \"fast\":\n    Cursed Technique: Apply\n        Using: (x) -> x\n", "needs a number, not a quoted label"},
		{"On needs a label",
			aWorld + "Part On 3:\n    Cursed Technique: Apply\n        Using: (x) -> x\n", "needs a quoted label, not a number"},
		{"Vista takes no argument",
			aWorld + "Part Vista 5:\n    Cursed Energy: stdin\n", "takes no label or number"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := resolveInScope(t, roleScope(), c.src)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestPartRoleUnknownNamesTheAvailableOnes(t *testing.T) {
	err := resolveInScope(t, roleScope(), aWorld+"Part Gamma:\n    Cursed Energy: stdin\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`unknown Part role "Gamma"`, "Test Realm", "Part World:", "Part Vista:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

// A role that exists in another Innate Domain is a different mistake from one
// that exists nowhere, and the message says which.
func TestPartRoleFromAnotherScope(t *testing.T) {
	withTestScope(t, roleScope())
	_, err := resolveSrc(t, "Cursed Energy: stdin\nPart Vista:\n    Reveal: stdout\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "belongs to the Test Realm Innate Domain") {
		t.Errorf("error = %v, want it to name the scope the role belongs to", err)
	}
}

// The plain `Part "1":` is a role like any other, and a scope that does not
// register it refuses one.
func TestScopeWithoutThePlainPartRefusesIt(t *testing.T) {
	sc := roleScope()
	err := resolveInScope(t, sc, aWorld+"Part \"1\":\n    Reveal: stdout\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "permits no plain Part") {
		t.Errorf("error = %v, want it to say the scope permits no plain Part", err)
	}
}

func TestPartRoleCardinality(t *testing.T) {
	t.Run("too many", func(t *testing.T) {
		vista := "Part Vista:\n    Cursed Technique: Apply\n        Using: (w) -> totext(w.a)\n"
		src := aWorld + vista + vista
		err := resolveInScope(t, roleScope(), src)
		if err == nil {
			t.Fatal("expected an error")
		}
		// Two Draws with no argument are the same Part twice, which is caught
		// as a duplicate before the count is consulted. Either message is a
		// refusal of the same mistake; assert on the one the user sees.
		if !strings.Contains(err.Error(), "already defined") && !strings.Contains(err.Error(), "permits") {
			t.Errorf("error = %v, want a refusal of the second Part Vista", err)
		}
	})
	t.Run("too few", func(t *testing.T) {
		// World has Min 1 and the program declares none.
		err := resolveInScope(t, roleScope(), "Cursed Energy: stdin\nReveal: stdout\n")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "Part World") {
			t.Errorf("error = %v, want it to name the missing Part World", err)
		}
	})
}

// Two timers at different periods are two Parts, not one Part twice.
func TestPartsAreKeyedByRoleAndArgument(t *testing.T) {
	sc := &Scope{
		Name: "Timer Realm", TopLevelPipeline: true,
		PartRoles: []PartRole{{Name: "Every", Arg: ArgInt, Min: 0, Max: -1, Globals: GlobalsReadOnly}},
	}
	src := "Cursed Energy: stdin\n" +
		"Part Every 100:\n    Cursed Technique: Apply\n        Using: (x) -> x\n" +
		"Part Every 500:\n    Cursed Technique: Apply\n        Using: (x) -> x\n"
	if err := resolveInScope(t, sc, src); err != nil {
		t.Fatalf("two timers at different periods should both resolve: %v", err)
	}
}

// A role's declared output type is checked against what its body produces.
func TestPartRoleOutputContract(t *testing.T) {
	sc := &Scope{
		Name: "Draw Realm", TopLevelPipeline: true,
		PartRoles: []PartRole{{
			Name: "Draw", Arg: ArgNone, Min: 0, Max: 1, Globals: GlobalsReadOnly,
			Out: func(*ir.Type) *ir.Type { return ir.Text() },
		}},
	}
	err := resolveInScope(t, sc,
		"Cursed Energy: stdin\nPart Draw:\n    Cursed Technique: Apply\n        Using: (x) -> length(x)\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"must produce Text", "produces Int"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

// A read-only role refuses a write and says where the globals are still open.
func TestPartRoleReadOnlyGlobals(t *testing.T) {
	sc := &Scope{
		Name: "Frozen Realm", TopLevelPipeline: true,
		PartRoles: []PartRole{{Name: "On", Arg: ArgLabel, Min: 0, Max: -1, Globals: GlobalsReadOnly}},
	}
	src := "Cursed Object: total As 0\nCursed Energy: stdin\n" +
		"Part On \"key\":\n    Cursed Tool: total As 1\n"
	err := resolveInScope(t, sc, src)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "freezes the globals") {
		t.Errorf("error = %v, want it to explain the freeze", err)
	}
}
