package prims

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/typecheck"
)

// Part blocks: several pipelines branching from one shared state.
//
// A Part branches from the current value exactly like a Channel — it is a
// passthrough, so sibling Parts all see the same upstream value and the parse
// above them happens once. The difference is where its result goes: a Channel
// stores it under a name for a From: consumer, while a Part labels whatever its
// body Reveals.
//
// Output is explicit: a Part prints only what its body Reveals, which keeps
// Reveal the single output sink and keeps the linter's "never Reveals" check
// honest. A Part whose body never Reveals is a lint warning, not an error.
//
// A Part may also carry a **role**, and what roles exist belongs to the
// program's `Innate Domain` rather than to this file (prims/scope.go). The
// `Advent of Code` scope registers exactly one — the unroled labelled block
// described above — so in an ordinary program every question below has the
// answer it always had.

// resolvePart lowers a `Part …:` statement into a passthrough node whose
// sub-pipeline runs under the contract its role declares.
func (r *resolver) resolvePart(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	role, err := r.partRole(stmt)
	if err != nil {
		return nil, err
	}
	if err := r.checkPartArg(stmt, role); err != nil {
		return nil, err
	}

	// A Part is identified by its role *and* its argument: two `Part Every`
	// blocks at different periods are two timers, while two `Part "1"` blocks
	// are the same label twice.
	key := partKey(stmt)
	if r.parts[key] {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("%s is already defined", stmt.PartDescription())}
	}
	if role.Max >= 0 {
		if n := r.roleCounts[stmt.PartRole]; n >= role.Max {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
				"%s has %s already, and %s permits %s",
				r.scope.Name, countOf(n, stmt.PartRole), r.scope.Name, atMost(role.Max, stmt.PartRole))}
		}
	}

	// The body's input type comes from the role. For the unroled Part that is
	// the upstream value, which is why a `Part "1":` with nothing above it is
	// still an error; a role that starts from nothing returns nil here and the
	// body opens with no value, exactly as a program's first stage does.
	in := cur
	if role.In != nil {
		in = role.In(r.world)
	}
	if in == nil && role.In == nil {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("%s has no upstream value to branch from", stmt.PartDescription())}
	}
	if len(stmt.Block) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("%s has an empty body", stmt.PartDescription()), NeedsBlock: true}
	}

	// A role may put values in scope for its body — the key an input carried,
	// the size a resize reported. They are *named bindings*, on the same
	// footing as a `Consider` written on the Part, and deliberately not
	// ambient lambda parameters: an ambient changes every lambda's arity,
	// including inside any Shikigami inlined in the body, so one Shikigami
	// could not be called from two roles carrying different payloads. See
	// RoleBind.
	var binds []RoleBind
	if role.Binds != nil {
		var berr error
		if binds, berr = role.Binds(stmt.PartArg, r.facts()); berr != nil {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf("%s: %v", stmt.PartDescription(), berr)}
		}
		for _, b := range binds {
			typecheck.PushBinding(b.Name, b.Type)
		}
		defer typecheck.PopBindings(len(binds))
	}

	// scopePart: a Part may consume channels defined above it with From:, but
	// may not define channels of its own (they cannot nest) or hold Parts.
	prevGlobals := r.partGlobals
	r.partGlobals = role.Globals
	subNodes, subType, err := r.resolveSequence(stmt.Block, in, scopePart)
	r.partGlobals = prevGlobals
	if err != nil {
		return nil, err
	}
	if role.Out != nil {
		if want := role.Out(r.world); want != nil && !subType.Equal(want) {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
				"%s must produce %s, but its body produces %s", stmt.PartDescription(), want, subType)}
		}
	}
	if role.DefinesWorld {
		// The world is what every other role's contract is written against,
		// and it has to be a Record: the roles that change it read and rewrite
		// named fields, and a bare Int or a tuple gives them nothing to name.
		if subType == nil || subType.Kind != ir.KRecord {
			return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
				"%s must produce a Record — the world is read and rewritten by name, so its parts need names — but its body produces %s",
				stmt.PartDescription(), subType)}
		}
		r.world = subType
	}
	r.parts[key] = true
	r.roleCounts[stmt.PartRole]++
	if stmt.PartName != "" {
		r.roleLabels[stmt.PartRole] = append(r.roleLabels[stmt.PartRole], stmt.PartName)
	}

	label := stmt.PartName
	isolate := role.Globals == GlobalsIsolated
	return &ir.Node{
		Prim:    "Part",
		In:      cur,
		Out:     cur, // passthrough
		Display: stmt.PartDescription(),
		// The "nodes" key is what puts this body in optimizer.nodeLists, so
		// in-place passes (expression simplification, algorithm substitution)
		// fire inside a Part exactly as they do inside a Channel or a loop.
		//
		// "role" and "bodyType" are for a host that drives Parts itself rather
		// than walking them in order: it has to know which body is which, and
		// what each one produces.
		// "role" and "bodyType" are for a host that drives Parts itself rather
		// than walking them in order: it has to know which body is which, and
		// what each one produces. "number" carries a numeric argument — the
		// period of a timer — for the same reason.
		Meta: map[string]any{
			"label": label, "nodes": subNodes,
			"role": stmt.PartRole, "bodyType": subType,
			"number": partNumber(stmt),
			// What the host must put in scope before running this body.
			"binds": binds,
		},
		Pos: stmt.Pos,
		Eval: func(ctx *ir.Context, in ir.Value) (ir.Value, error) {
			// Save and restore rather than clear, so the label of an enclosing
			// scope survives if Parts are ever allowed to nest.
			prev := ctx.PartLabel
			ctx.PartLabel = label
			defer func() { ctx.PartLabel = prev }()

			// Globals are restored on the way out for the same reason the
			// pipeline value is passed through: sibling Parts branch from one
			// state, and "Part 1 sorting cannot disturb what Part 2 sees"
			// (docs/language.md) is a guarantee about everything a Part can
			// reach, not only about the value. Without this a `Cursed Tool`
			// inside one Part would silently change what the next one reads.
			//
			// A Part runs once per program rather than once per element, so
			// the copy costs nothing at the scale it happens.
			//
			// A role whose globals are not isolated skips it: an event handler
			// that could not change anything outside its own body would have
			// nothing to do.
			if isolate {
				savedGlobals := eval.SnapshotGlobals()
				defer eval.RestoreGlobals(savedGlobals)
			}

			// The body's result is what the Part actually computed; the node
			// itself passes its input through. A body that failed reports nil.
			var body ir.Value
			ctx.PushFrame(stmt.PartDescription(), subType)
			defer func() { ctx.PopFrame(body) }()

			v, err := runBody(ctx, subNodes, in)
			if err != nil {
				return nil, err
			}
			body = v
			return in, nil
		},
	}, nil
}

// partRole finds the role a Part statement names, in the program's scope.
func (r *resolver) partRole(stmt *ast.Statement) (PartRole, error) {
	role, ok := r.scope.roleNamed(stmt.PartRole)
	if ok {
		return role, nil
	}
	// The unroled Part is checked first. It exists in some other scope by
	// definition — Advent of Code registers it — so the "belongs to" message
	// below would be both true and useless here, and would read as a Part
	// with no name at all.
	if stmt.PartRole == "" {
		return PartRole{}, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
			"%s permits no plain Part; the Parts it takes are: %s",
			r.scope.Name, strings.Join(r.scope.roleNames(), ", "))}
	}
	// A role that exists somewhere is a different mistake from one that
	// exists nowhere, and being told which saves the reader a search.
	if other, found := scopeOtherThan(r.scope, stmt.PartRole); found {
		return PartRole{}, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
			"Part %s belongs to the %s Innate Domain; this program's is %s",
			stmt.PartRole, other.Name, r.scope.Name)}
	}
	if len(r.scope.PartRoles) == 0 {
		return PartRole{}, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
			"%s has no Part roles, so there is no Part %s", r.scope.Name, stmt.PartRole)}
	}
	return PartRole{}, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
		"unknown Part role %q in %s; the Parts it takes are: %s",
		stmt.PartRole, r.scope.Name, strings.Join(r.scope.roleNames(), ", "))}
}

// checkPartArg holds a Part to the argument shape its role declares.
func (r *resolver) checkPartArg(stmt *ast.Statement, role PartRole) error {
	arg := stmt.PartArg
	bad := func(msg string) error {
		return &ResolveError{Pos: stmt.Pos, Msg: msg}
	}
	name := "Part"
	if stmt.PartRole != "" {
		name = "Part " + stmt.PartRole
	}
	switch role.Arg {
	case ArgNone:
		if arg != nil {
			return bad(fmt.Sprintf("%s takes no label or number, so write `%s:`", name, name))
		}
	case ArgLabel:
		if arg == nil {
			return bad(fmt.Sprintf("%s needs a label, e.g. `%s \"1\":`", name, name))
		}
		if arg.IsInt {
			return bad(fmt.Sprintf("%s needs a quoted label, not a number", name))
		}
		if role.CheckLabel != nil {
			if err := role.CheckLabel(arg.Text); err != nil {
				return bad(fmt.Sprintf("%s: %v", name, err))
			}
		}
	case ArgLabelOptional:
		if arg != nil && arg.IsInt {
			return bad(fmt.Sprintf("%s takes a quoted label or nothing, not a number", name))
		}
	case ArgInt:
		if arg == nil {
			return bad(fmt.Sprintf("%s needs a number, e.g. `%s 120:`", name, name))
		}
		if !arg.IsInt {
			return bad(fmt.Sprintf("%s needs a number, not a quoted label", name))
		}
		if arg.Int <= 0 {
			// A period of zero is not a very fast timer, it is a timer that is
			// always due — a loop with no way out. Refusing it here is the
			// difference between a positioned error and a program that hangs.
			return bad(fmt.Sprintf("%s needs a number above zero, got %d", name, arg.Int))
		}
	}
	return nil
}

// checkPartCardinality reports a role the program has too few of. The upper
// bound is checked as each Part resolves, where the offending line has a
// position; a missing Part has no position of its own, so this runs once the
// program has been read and reports at its start.
func (r *resolver) checkPartCardinality() error {
	for _, role := range r.scope.PartRoles {
		if n := r.roleCounts[role.Name]; n < role.Min {
			return &ResolveError{Msg: fmt.Sprintf("%s needs %s, and this program has %s",
				r.scope.Name, atLeast(role.Min, role.Name), countOf(n, role.Name))}
		}
	}
	return nil
}

// partNumber is a Part's numeric argument, or zero when it has none.
func partNumber(stmt *ast.Statement) int64 {
	if stmt.PartArg != nil && stmt.PartArg.IsInt {
		return stmt.PartArg.Int
	}
	return 0
}

// partKey identifies a Part for the duplicate check: its role and its
// argument together. The argument has to be in it — two `Part Every` blocks
// at different periods are two timers, and keying on the label alone would
// make them one, because a numeric argument leaves PartName empty.
func partKey(stmt *ast.Statement) string {
	switch {
	case stmt.PartArg == nil:
		return stmt.PartRole + "\x00"
	case stmt.PartArg.IsInt:
		return fmt.Sprintf("%s\x00#%d", stmt.PartRole, stmt.PartArg.Int)
	default:
		return stmt.PartRole + "\x00" + stmt.PartArg.Text
	}
}

// roleWord names a role for a message. The unroled role has no word, so it is
// named by the shape it takes.
func roleWord(role string) string {
	if role == "" {
		return "Part"
	}
	return "Part " + role
}

func countOf(n int, role string) string {
	switch n {
	case 0:
		return "none"
	case 1:
		return "one " + roleWord(role)
	}
	return fmt.Sprintf("%d %s blocks", n, roleWord(role))
}

func atMost(n int, role string) string {
	if n == 1 {
		return "one " + roleWord(role)
	}
	return fmt.Sprintf("at most %d %s blocks", n, roleWord(role))
}

func atLeast(n int, role string) string {
	if n == 1 {
		return "one " + roleWord(role)
	}
	return fmt.Sprintf("at least %d %s blocks", n, roleWord(role))
}
