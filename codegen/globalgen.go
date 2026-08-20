package codegen

import (
	"fmt"

	"domain/ast"
	"domain/ir"
)

// Compiling `Cursed Object` / `Cursed Tool` globals (prims/globals.go).
//
// Each slot becomes a **package-level** Go variable. Package level rather than
// a local in main is required, not stylistic: a `Using:` written as an indented
// pipeline compiles to a top-level function (blockgen.go), and
// docs/language.md gives exactly that as the structural reason a `From:`
// channel is refused there — a channel's local is not in scope in a function of
// its own. A package-level variable is in scope in every function this backend
// emits, so a global works in the one place a channel cannot.
//
// What the interpreter pays a slice index for, the compiled program gets for
// nothing: a package-level variable of a concrete type is one the Go compiler
// can keep in a register across a loop.

// globalVar is the Go name of a slot's variable.
func globalVar(slot int) string { return fmt.Sprintf("dmGlobal%d", slot) }

// declareGlobal emits the package-level variable for a slot, once. Declaring
// it at each write rather than up front is what keeps a slot's Go type coming
// from the declaration that fixed it — the resolver has already refused a
// write of any other type, so every write agrees.
func (g *gen) declareGlobal(slot int, t *ir.Type) error {
	name := globalVar(slot)
	if g.declSet[name] {
		return nil
	}
	goT, err := g.goType(t)
	if err != nil {
		return err
	}
	g.helper(name, fmt.Sprintf("var %s %s", name, goT))
	g.globals = append(g.globals, globalSlot{slot: slot, typ: t})
	return nil
}

// emitGlobals lowers a declaration node: each write's value is computed and
// assigned to its variable, in written order, so a line reading the one above
// it sees what that one just wrote.
//
// The node is a passthrough, so the pipeline value it was handed is what it
// gives back.
func (g *gen) emitGlobals(n *ir.Node, in string) (string, error) {
	writes, _ := n.Meta[ir.MetaGlobals].([]ir.GlobalWrite)
	if writes == nil {
		return "", unsupported(n, "missing global metadata")
	}
	for _, w := range writes {
		if err := g.declareGlobal(w.Slot(), w.Type()); err != nil {
			return "", err
		}
		// bindValue is shared with `Consider` outright: a declaration's
		// right-hand side is a binding's, so the three forms are compiled once
		// for both. A block-valued one emits its stages as statements here,
		// which is why the assignment comes after.
		expr, err := g.bindValue(n, w, in)
		if err != nil {
			return "", err
		}
		g.wl("%s = %s", globalVar(w.Slot()), expr)
	}
	return in, nil
}

// globalRef is the compiled form of a read: the slot's variable, with the type
// the declaration fixed. There is no lookup and no environment entry — see
// ast/global.go for why that is the whole point.
func (g *gen) globalRef(x *ast.GlobalRef) (exprBinding, error) {
	if err := g.declareGlobal(x.Slot, x.Type); err != nil {
		return exprBinding{}, err
	}
	v := globalVar(x.Slot)
	return exprBinding{expr: v, typ: x.Type, cell: "&" + v}, nil
}

// globalSlot is one declared slot, remembered so a Part can save and restore
// every global in scope around its body.
type globalSlot struct {
	slot int
	typ  *ir.Type
}

// saveGlobals emits locals holding the current value of every declared slot,
// and returns the statements that put them back.
//
// This is the compiled half of the `Part` isolation eval.SnapshotGlobals gives
// the interpreter: docs/language.md promises that sibling Parts branch from
// one state — "Part 1 sorting cannot disturb what Part 2 sees" — and a mutable
// global would punch through it. Only slots declared *above* the Part can be
// reached from inside it, since a global is in scope from its own line onward,
// so saving the ones declared so far saves exactly the reachable set.
func (g *gen) saveGlobals() (restore func(), err error) {
	if len(g.globals) == 0 {
		return func() {}, nil
	}
	saved := make([]string, len(g.globals))
	slots := make([]int, len(g.globals))
	for i, gl := range g.globals {
		goT, terr := g.goType(gl.typ)
		if terr != nil {
			return nil, terr
		}
		v := g.fresh("dmSavedGlobal")
		g.wl("var %s %s = %s", v, goT, globalVar(gl.slot))
		saved[i], slots[i] = v, gl.slot
	}
	return func() {
		for i, v := range saved {
			g.wl("%s = %s", globalVar(slots[i]), v)
		}
	}, nil
}
