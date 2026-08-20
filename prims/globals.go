package prims

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Globals: `Cursed Object` (declare) and `Cursed Tool` (assign).
//
// A `Consider` binding names a value for one statement. A global names one for
// the rest of the program, and that is the whole difference — the right-hand
// side is the same three forms with the same two prepositions, so the value
// machinery below is `resolveBind`'s, not a second copy of it.
//
// Where they differ is how the name is read. A binding is seeded by name into
// the environment every lambda application builds; a global is resolved to a
// slot index while the program is lowered (rewriteExpr, locals.go) and read
// with a slice load. That is not an optimization applied afterwards, it is why
// the feature can exist at all: a program-scoped name read the binding way
// would make every lambda in the program pay for every global in it. See
// ast/global.go and eval/globals.go.

// globalDecl is one declared global. Its index in resolver.globals is its slot.
type globalDecl struct {
	name string
	typ  *ir.Type
	pos  token.Position
}

// globalSeal explains why globals are out of reach where resolution currently
// is, when they are. Each construct gets its own reason and its own remedy —
// they are sealed for genuinely different causes, and one message covering
// both would be wrong about at least one of them.
type globalSeal struct {
	what string // the construct, named as the user would name it
	why  string // why it is sealed, and what to write instead
}

// sealedFrom reports the seal in force, if any.
//
// A Channel is the case with the ordering hazard. Its whole safety story is
// that it is computed once, before its consumer, and never changes —
// docs/language.md leans on exactly that ("a channel is fully computed before
// the loop starts and its value never changes, so there is no ordering
// hazard"). A body that wrote a global would make its evaluation order
// observable, and one that read a global would make its value depend on when
// it happened to run. Sealing both directions keeps a channel a pure function
// of its input.
//
// A Shikigami that came from somewhere else — the embedded prelude, or a
// library imported with `Innate Domain` — is sealed for a different reason:
// its author never saw the calling program's names, so a body reaching into
// them would depend on something it cannot know. A library whose behaviour
// turns on names its author never wrote is not a library. Definitions in the
// program's own file are unaffected; they are inlined at their call sites and
// read and write globals like any other stage.
func (r *resolver) sealedFrom() (globalSeal, bool) {
	if r.inChannel {
		return globalSeal{
			what: "a Channel",
			why: "a Channel is computed once, before whatever consumes it, so anything it " +
				"read or wrote would depend on an order nothing downstream can see. " +
				"Declare the global outside the Channel and set it from the Channel's " +
				"value once a From: consumer has taken it",
		}, true
	}
	if r.foreignDepth > 0 {
		return globalSeal{
			what: "a Shikigami defined outside this file",
			why: "its definition was written without sight of this program's names, so it " +
				"cannot depend on them. Pass the value in as a parameter, or move the " +
				"work into a Shikigami defined in this file",
		}, true
	}
	return globalSeal{}, false
}

// lookupGlobal finds a declared global by name.
func (r *resolver) lookupGlobal(name string) (globalDecl, int, bool) {
	i, ok := r.globalIndex[name]
	if !ok {
		return globalDecl{}, 0, false
	}
	return r.globals[i], i, true
}

// scanProgram is the front end's one pre-resolution traversal. It answers
// three questions that all have to be settled before the first statement is
// lowered, and it answers them in a single walk because it visits every
// expression of every definition the prelude carries — whether or not the
// program calls them — and that showed up as ~20% of resolve time when it was
// two walks and ~10% when it was one and a half.
//
//   - updated: every name written with `:=`. Whether the set is empty decides
//     how the interpreter represents a binding (eval.EnableUpdates).
//   - declared: every global the program declares anywhere, so a read written
//     above its declaration can be told what it is rather than only that it is
//     unknown. Visibility itself stays forward-only.
//   - mutated: every global something can change after it is declared, which
//     decides whether a stage reading one is still a pure function of its
//     input (ast.GlobalRef.Mutable). A `:=` counts by spelling alone: being
//     wrong that way costs a stage its rewrites, being wrong the other way is
//     a wrong answer.
//
// nested is true inside anything that can run more than once — a loop body, a
// Part, a sub-pipeline, a Shikigami body (inlined at call sites that may sit
// anywhere). A `Cursed Object` written there re-declares its global on every
// pass, which is a mutation however it was spelled.
type programScan struct {
	updated  map[string]bool
	declared map[string]token.Position
	mutated  map[string]bool
}

// newProgramScan allocates only the map that almost every program needs. The
// other two stay nil until a `Cursed Object` or `Cursed Tool` is actually
// seen: a program with no globals is every program written before they
// existed, and it should pay nothing for the scan finding none.
func newProgramScan() *programScan {
	return &programScan{updated: map[string]bool{}}
}

// declare records a global declaration, creating the maps on first sight.
func (sc *programScan) declare(name string, pos token.Position, nested bool) {
	if sc.declared == nil {
		sc.declared = map[string]token.Position{}
	}
	if _, seen := sc.declared[name]; !seen {
		sc.declared[name] = pos
	}
	if nested {
		sc.mutate(name)
	}
}

// mutate records that something can change a global after it is declared.
func (sc *programScan) mutate(name string) {
	if sc.mutated == nil {
		sc.mutated = map[string]bool{}
	}
	sc.mutated[name] = true
}

func (sc *programScan) walk(stmts []*ast.Statement, nested bool) {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s.Keyword {
		case "Cursed Object":
			for _, d := range s.Decls {
				if d != nil {
					sc.declare(d.Name, d.Pos, nested)
				}
			}
		case "Cursed Tool":
			for _, d := range s.Decls {
				if d != nil {
					sc.mutate(d.Name)
				}
			}
		}
		for _, a := range s.Args {
			if lam, ok := a.Value.(ast.LambdaArg); ok {
				updatedInLambda(lam.Lambda, sc.updated)
			}
		}
		// Binds and Decls carry the same three right-hand-side forms, so both
		// are visited — but as two loops rather than one over a concatenation.
		// This walk runs over every definition the prelude carries on every
		// resolve, and slices.Concat here was one allocation per statement.
		for _, b := range s.Binds {
			sc.binding(b)
		}
		for _, b := range s.Decls {
			sc.binding(b)
		}
		// Everything below a statement runs under it, so it is nested even
		// when the statement itself was not.
		sc.walk(s.Block, true)
	}
}

// binding scans one binding-or-declaration's right-hand side.
func (sc *programScan) binding(b *ast.Binding) {
	if b == nil {
		return
	}
	if b.Value != nil {
		ast.UpdatedNames(b.Value, sc.updated)
	}
	updatedInLambda(b.Lambda, sc.updated)
	sc.walk(b.Body, true)
}

// finish folds the `:=` names into the mutable set: a write by that spelling
// may have landed on a global of the same name.
//
// A program that declared no globals has nothing to fold them into, and skips
// it — which is what keeps the whole globals half of this scan free for every
// program that does not use one.
func (sc *programScan) finish() {
	if sc.declared == nil {
		return
	}
	for name := range sc.updated {
		sc.mutate(name)
	}
}

// mutable reports whether a global can change after it is declared. A name the
// pre-pass never saw written is a constant of the run, and a stage that only
// reads such names keeps every rewrite it would have had.
func (r *resolver) mutable(name string) bool { return r.mutatedGlobals[name] }

// globalRefTo builds a read of a declared global.
func (r *resolver) globalRefTo(g globalDecl, slot int, name string, pos token.Position) *ast.GlobalRef {
	return &ast.GlobalRef{
		Slot: slot, Name: name, Type: g.typ, Pos: pos, Mutable: r.mutable(name),
	}
}

// resolveGlobals lowers a `Cursed Object:` or `Cursed Tool:` statement into a
// node that computes each declaration's value and writes it to its slot.
//
// Both are passthroughs: the pipeline value is not what a global is carried
// in, which is the point of the feature, so In and Out are the same type and a
// loop body containing one still preserves its value type.
func (r *resolver) resolveGlobals(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	declaring := stmt.Keyword == "Cursed Object"
	writes := make([]globalWrite, 0, len(stmt.Decls))
	names := make([]string, 0, len(stmt.Decls))
	var sub [][]*ir.Node

	if seal, sealed := r.sealedFrom(); sealed {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
			"%s is not allowed inside %s — %s", stmt.Keyword, seal.what, seal.why)}
	}
	for _, d := range stmt.Decls {
		rt, err := r.resolveGlobalValue(d, cur, stmt.Keyword)
		if err != nil {
			return nil, err
		}
		slot, err := r.bindGlobal(d, rt, declaring)
		if err != nil {
			return nil, err
		}
		writes = append(writes, globalWrite{runtimeBind: rt, slot: slot})
		names = append(names, d.Name)
		if rt.body != nil {
			sub = append(sub, rt.body.BlockNodes())
		}
	}

	// The writes go on Meta as well as into the closure below. The closure is
	// what the interpreter runs; Meta is how everything that is not the
	// interpreter — the compiler backend, the optimizer's node walk, the
	// language server — sees what this node does, since none of them can look
	// inside a closure.
	meta := map[string]any{ir.MetaGlobals: asGlobalWrites(writes)}
	if sub != nil {
		meta[ir.MetaGlobalNodes] = sub
	}

	return &ir.Node{
		Prim:    stmt.Keyword,
		In:      cur,
		Out:     cur,
		Display: stmt.Keyword + " " + strings.Join(names, ", "),
		Pos:     stmt.Pos,
		Meta:    meta,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			// Written in order, and each sees the ones before it, so
			// `b As a + 1` on the line under `a As 1` reads the new a.
			for _, w := range writes {
				val, err := w.value(v)
				if err != nil {
					return nil, err
				}
				eval.SetGlobal(w.slot, val)
			}
			return v, nil
		},
	}, nil
}

// globalWrite is one declaration line: the value machinery a `Consider`
// binding already has, plus the slot it lands in. Wrapping rather than adding
// a slot field to runtimeBind keeps `Consider`, which has no slot, from
// carrying one it never sets.
type globalWrite struct {
	*runtimeBind
	slot int
}

// Slot is the index into the run's global array this write lands in.
func (w globalWrite) Slot() int { return w.slot }

// asGlobalWrites is the []ir.GlobalWrite the backends read off Meta. Go will
// not convert a slice of a concrete type to a slice of an interface it
// satisfies, so the copy is the price of the contract.
func asGlobalWrites(ws []globalWrite) []ir.GlobalWrite {
	out := make([]ir.GlobalWrite, len(ws))
	for i, w := range ws {
		out[i] = w
	}
	return out
}

// bindGlobal registers a declaration's slot, or checks an assignment against
// the one already there. It runs after the value is resolved, so a
// declaration's own name is not in scope inside its initialiser — `n As n + 1`
// on a `Cursed Object` is an unknown identifier rather than a read of a slot
// that has nothing in it yet.
func (r *resolver) bindGlobal(d *ast.Binding, rt *runtimeBind, declaring bool) (int, error) {
	if !declaring {
		prev, slot, ok := r.lookupGlobal(d.Name)
		if !ok {
			return 0, &ResolveError{Pos: d.Pos, Msg: fmt.Sprintf(
				"%q is not a global, so `Cursed Tool` has nothing to write to%s. "+
					"Declare it with `Cursed Object: %s As …` first",
				d.Name, r.declaredSoFar(), d.Name)}
		}
		if !rt.typ.Equal(prev.typ) {
			return 0, &ResolveError{Pos: d.Pos, Msg: fmt.Sprintf(
				"%q holds %s, so `Cursed Tool` cannot write %s to it; widen it where it is declared (%s)",
				d.Name, prev.typ, rt.typ, prev.pos)}
		}
		return slot, nil
	}

	if prev, _, ok := r.lookupGlobal(d.Name); ok {
		return 0, &ResolveError{Pos: d.Pos, Msg: fmt.Sprintf(
			"%q is already a global, declared at %s; use `Cursed Tool: %s As …` to change it",
			d.Name, prev.pos, d.Name)}
	}
	if err := checkDeclaredName(d.Name, "a global", d.Pos); err != nil {
		return 0, err
	}
	if _, ok := r.channels[d.Name]; ok {
		return 0, &ResolveError{Pos: d.Pos, Msg: fmt.Sprintf(
			"%q is already a Channel, and a global of that name would make the two "+
				"indistinguishable in an expression — pick another name", d.Name)}
	}
	if r.globalIndex == nil {
		r.globalIndex = map[string]int{}
	}
	slot := len(r.globals)
	r.globals = append(r.globals, globalDecl{name: d.Name, typ: rt.typ, pos: d.Pos})
	r.globalIndex[d.Name] = slot
	return slot, nil
}

// declaredSoFar lists the globals a statement could have written to, for the
// error that says one was not among them. Listing them beats a nearest-name
// guess here: the set is small, it is the user's own vocabulary, and the usual
// cause is a global declared below the write rather than misspelled.
func (r *resolver) declaredSoFar() string {
	if len(r.globals) == 0 {
		return " (no globals are declared above this line)"
	}
	names := make([]string, len(r.globals))
	for i, g := range r.globals {
		names[i] = strconv.Quote(g.name)
	}
	return " (declared above this line: " + strings.Join(names, ", ") + ")"
}

// resolveGlobalValue lowers a declaration's right-hand side. It is
// resolveBind's four cases minus the two that make no sense for a global: a
// global is never folded to a literal (a constant substituted into the bodies
// that read it has nowhere to put a `Cursed Tool` write — the same trade a
// written binding already makes), and it is never a function.
func (r *resolver) resolveGlobalValue(d *ast.Binding, cur *ir.Type, keyword string) (*runtimeBind, error) {
	fail := func(format string, a ...any) (*runtimeBind, error) {
		return nil, &ResolveError{Pos: d.Pos, Msg: fmt.Sprintf(format, a...)}
	}

	switch {
	// `Of` an operation phrase or an indented sub-pipeline.
	case d.Of && len(d.Body) > 0:
		if cur == nil {
			return fail("`%s: %s Of` has no current value to work from", keyword, d.Name)
		}
		body := &blockPipeline{res: r, stmts: d.Body, prim: keyword + " " + d.Name, pos: d.Pos}
		out, err := body.BindBlock(cur)
		if err != nil {
			return fail("`%s: %s Of`: %v", keyword, d.Name, err)
		}
		return &runtimeBind{name: d.Name, typ: out, body: body, in: cur, pos: d.Pos}, nil

	// `Of Itself`: the value arriving at this statement, unchanged.
	case d.Of && d.Identity:
		if cur == nil {
			return fail("`%s: %s Of Itself` has no current value to name", keyword, d.Name)
		}
		return &runtimeBind{name: d.Name, typ: cur, lam: identityLambda(d.Pos), in: cur, pos: d.Pos}, nil

	// `Of` a lambda: applied to the current value.
	case d.Of:
		if cur == nil {
			return fail("`%s: %s Of` has no current value to work from", keyword, d.Name)
		}
		lam, err := r.rewriteLambda(d.Lambda)
		if err != nil {
			return nil, err
		}
		want := 1 + ambientDepth()
		if len(lam.Params) != want {
			return fail("`%s: %s Of` takes a %d-parameter lambda over the current value, got %d",
				keyword, d.Name, want, len(lam.Params))
		}
		typ, err := typecheck.LambdaType(lam, append([]*ir.Type{cur}, ambientTypes()...)...)
		if err != nil {
			return fail("`%s: %s Of`: %v", keyword, d.Name, err)
		}
		return &runtimeBind{name: d.Name, typ: typ, lam: lam, in: cur, pos: d.Pos}, nil

	// `As` a lambda: refused. Domain has no function values, and a global that
	// were one could not be written to by `Cursed Tool` either.
	case d.Lambda != nil:
		return fail("a global cannot be a function: `%s: %s As` a lambda has no type to hold. "+
			"Use `Consider %s As (…) -> …`, which is inlined at its call sites, "+
			"or `%s: %s Of (x) -> …` to compute a value from the current one",
			keyword, d.Name, d.Name, keyword, d.Name)

	// `As` an expression, computed where it is written.
	default:
		e, err := r.rewriteExpr(d.Value, nil)
		if err != nil {
			return nil, err
		}
		typ, err := typecheck.ExprType(e, typecheck.BindingEnv())
		if err != nil {
			return fail("`%s: %s As`: %v", keyword, d.Name, err)
		}
		return &runtimeBind{name: d.Name, typ: typ, expr: e, in: cur, pos: d.Pos}, nil
	}
}

// unknownIdent matches the message every layer uses for a name it cannot
// place, so that one can be recognized wherever it was raised.
var unknownIdent = regexp.MustCompile(`unknown identifier "([^"]+)"`)

// explainForwardGlobal adds the reason to an "unknown identifier" that names a
// global declared further down the program.
//
// It enriches a failure rather than raising one, and that is deliberate. The
// alternative — refusing the name in rewriteExpr, where the forward reference
// is visible — would have to be sure the name is not in scope by some
// mechanism the rewriter does not track, and it cannot be: a block body's
// `Params:` names, for one, are carried on the BlockBody node rather than
// pushed into the resolver's locals. Getting that wrong would refuse a working
// program. Enriching an error that was going to be returned anyway cannot.
func (r *resolver) explainForwardGlobal(err error) error {
	if err == nil || len(r.laterGlobals) == 0 {
		return err
	}
	m := unknownIdent.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	pos, ok := r.laterGlobals[m[1]]
	if !ok {
		return err
	}
	if _, _, declared := r.lookupGlobal(m[1]); declared {
		return err // in scope here; the failure is about something else
	}
	return fmt.Errorf("%w — %q is a global declared at %s, and a global is in scope "+
		"only from its `Cursed Object` line onward; move the declaration above this stage",
		err, m[1], pos)
}
