package prims

import (
	"fmt"
	"slices"
	"strings"

	"domain/ast"
	"domain/ir"
)

// `Innate Domain: <name>` — what kind of program this file is.
//
// A scope owns a vocabulary, a `Part` role table, a prelude, the shape a
// program is allowed to take, and the names of the hosts that run and compile
// it. `Advent of Code` is the default and contains everything the language had
// before scopes existed; a program with no `Innate Domain:` line resolves
// against it and is unaffected in every particular.
//
// A scope only ever *adds*. Core is the whole vocabulary as it stands and no
// scope may take anything out of it, because what an operation phrase means —
// and which names a Shikigami may not be called — has to keep meaning what it
// meant. See RegistryFor.

// Scope is one Innate Domain.
//
// It does not name its runner or its compiler backend. Those register
// themselves *against* a scope, by its name, on the far side of the dependency
// edge: interp.RegisterHost and codegen's backend table. prims cannot reach
// either — interp consumes what prims produces and both packages' tests import
// the other — and the pipeline already carries the scope's name, which is all
// a lookup needs.
type Scope struct {
	Name    string   // "Advent of Code"
	Aliases []string // other accepted spellings, matched case-insensitively
	Summary string   // one sentence, for docs and `domain --help`

	// Prims are the primitives this scope adds to Core, ordered
	// specific-matcher-first exactly as Core is.
	Prims []*Primitive

	// PartRoles is the role table. The zero-length table means the scope
	// permits no Part at all; a scope that wants today's labelled block
	// registers the unroled role explicitly (see adventOfCode).
	PartRoles []PartRole

	// Prelude is extra Shikigami source loaded after the shared prelude and
	// before any `Inherited Technique` import, so a scope may ship a standard
	// library written in Domain. nil for a scope that ships none.
	Prelude func() ([]byte, error)

	// TopLevelPipeline says whether ordinary pipeline statements are legal at
	// the top level. False for a scope whose programs are declarations and
	// Parts only — the same rule a library file already lives under.
	TopLevelPipeline bool

	// Shape is the scope's whole-program check, run once every statement has
	// resolved. Cardinality is already settled by then (PartRole.Min/Max);
	// this is for the questions no single line can answer. nil means there
	// are none.
	Shape func(ProgramFacts) error
}

// ProgramFacts is what a scope's hooks may ask about the program as a whole.
//
// It exists because a Part is not resolved in the order it was written — the
// world is hoisted, and the rest may be in any order — so a role that needs to
// know about a statement elsewhere cannot wait to meet it. A `Part Reply` is
// typed against the `Request` that fires its tag, and that Request may be a
// hundred lines below.
type ProgramFacts struct {
	// World is the type the world-defining Part produced, or nil.
	World *ir.Type
	// PartCounts is how many Parts of each role the program has, keyed by
	// role name (the unroled Part counts under "").
	PartCounts map[string]int
	// PartLabels is the labels written for each role, in source order.
	PartLabels map[string][]string
	// Requests is every request the program fires, by its tag.
	Requests map[string]*ir.RequestSpec
}

// GlobalPolicy is a Part role's access to the `Cursed Object` table.
type GlobalPolicy int

const (
	// GlobalsIsolated snapshots the globals on entry and restores them on
	// exit: today's Part, and the guarantee docs/language.md makes that one
	// Part's work cannot disturb what the next one sees.
	GlobalsIsolated GlobalPolicy = iota
	// GlobalsReadWrite reads and writes, and the writes persist.
	GlobalsReadWrite
	// GlobalsReadOnly reads; a `Cursed Tool` write is a resolve error.
	GlobalsReadOnly
)

// ArgRule is what a Part role writes between its name and its colon.
type ArgRule int

const (
	ArgNone          ArgRule = iota // `Part Draw:`
	ArgLabel                        // `Part "1":`, `Part Entity "Creep":`
	ArgLabelOptional                // either
	ArgInt                          // `Part Every 120:`
)

// RoleBind is a value a role's host puts in scope for the body — the key an
// input event carried, the reply a request completed with.
//
// These are *named bindings*, on the same footing as a `Consider` written on
// the Part (prims/locals.go), and deliberately not ambient lambda parameters.
// prims/ambient.go binds positionally and requireLambda demands an exact arity
// of `arity + ambientDepth()`, so a role that pushed an ambient would force
// every `Using:` lambda in its body — including the ones inside any Shikigami
// inlined there — to declare a trailing parameter it may never read. One
// Shikigami could then not be called from two roles carrying different
// payloads, which is the first thing any program with both a timer and a key
// wants to do. A named binding leaves the lambda exactly as written.
type RoleBind struct {
	Name string
	Type *ir.Type
}

// PartRole is one kind of Part a scope permits.
type PartRole struct {
	Name     string // "World", "Draw"; "" for the unroled `Part "1":`
	Arg      ArgRule
	Min, Max int // cardinality; Max of -1 is unbounded

	// In and Out are the body's type contract, stated against the world type
	// — the output of whichever role sets DefinesWorld, or nil when the scope
	// has no such role.
	//
	// A **nil In means the body branches from the upstream value**, which is
	// what a `Part "1":` does and why the unroled role declares neither: a
	// Part with nothing above it is then the error it has always been. A role
	// that starts from something else sets In, and may return nil from it to
	// say the body opens with no value at all, as a program's first stage
	// does. A nil Out means the body may produce anything.
	In  func(world *ir.Type) *ir.Type
	Out func(world *ir.Type) *ir.Type

	// DefinesWorld marks the role whose body's output type *is* the world
	// type. At most one role may set it. A scope that does gets two-pass
	// resolution: that Part is hoisted and resolved before the others, so
	// every other role's In and Out have a world type to be stated against.
	DefinesWorld bool

	// Globals is what the body may do to the `Cursed Object` table.
	Globals GlobalPolicy

	// Binds are the values the host puts in scope for the body. nil for a
	// role that carries no payload.
	Binds func(arg *ast.PartArg, facts ProgramFacts) ([]RoleBind, error)

	// CheckLabel validates the role's label beyond its shape — that a
	// `Part On` spec names something that can actually happen, say. Shape is
	// PartRole.Arg's business; this is about meaning, which only the role
	// knows.
	CheckLabel func(label string) error

	// Definition marks a role that defines something rather than running:
	// its body becomes a Shikigami called by the Part's name, and the Part
	// contributes no node to the pipeline. Such a role is lowered before
	// resolution begins, so the name is callable from every other Part.
	Definition bool

	Doc string
}

// adventOfCode is the default scope: everything the language had before
// scopes existed, and nothing else.
//
// Prims is empty on purpose. Splitting the registry into a core and an "AoC
// pack" would mean deciding that some primitive is unavailable somewhere, and
// there is nowhere for it to be unavailable — this is the only scope. Worse,
// Core is what inferPrimitive searches to resolve a bare phrase and what
// reservedMeaning searches to decide a Shikigami name is taken, so moving
// anything out of the set an existing program sees could only change what that
// program means. The split can be made later, in isolation, if some scope ever
// wants to refuse a primitive. Nothing does.
// It has no alias, and specifically not "AoC". `Innate Domain: aoc` is what
// every program written before scopes existed says, and it meant "import the
// aoc library". An alias matching it case-insensitively would make that line
// resolve as a scope declaration and drop the import in silence — a wrong
// answer rather than an error, on the one spelling most likely to appear.
// Without the alias it is an unknown Innate Domain, which is what sends the
// reader to the diagnostic that names `Inherited Technique:`.
var adventOfCode = &Scope{
	Name:             "Advent of Code",
	Summary:          "Puzzle input in, answer out: the pipeline Domain was designed around.",
	TopLevelPipeline: true,
	PartRoles: []PartRole{{
		Name: "", Arg: ArgLabel, Min: 0, Max: -1,
		Globals: GlobalsIsolated,
		Doc:     "A labelled output block: the two-answers-per-input shape.",
	}},
}

// Scopes is every registered Innate Domain, in the order they are offered to
// a user who asked for one that does not exist.
var Scopes = []*Scope{adventOfCode, gameDev}

// DefaultScope is what a program with no `Innate Domain:` line resolves
// against.
var DefaultScope = adventOfCode

// IsOnePipeline reports whether a scope's programs are a single chain of
// nodes threading one value.
//
// It is the question every tool that *steps through* a run is really asking.
// A stepper, a REPL, a trace table — each of them is built on "one value, one
// stage after another", and for a scope that says otherwise the answer is not
// a worse display, it is a wrong one: a game's Parts are driven by events, so
// a flat list of their stages reads as a loop that never finished. Better to
// refuse in a sentence than to show that.
//
// A scope this build does not have reports true: an unknown name is a
// diagnostic the resolver gives, not a reason for a tool to behave oddly on
// its way there.
func IsOnePipeline(scope string) bool {
	if scope == "" {
		return DefaultScope.TopLevelPipeline
	}
	sc, ok := ScopeNamed(scope)
	if !ok {
		return true
	}
	return sc.TopLevelPipeline
}

// ScopeNamed finds a scope by name or alias, case-insensitively — the names
// are prose ("Advent of Code"), and a user typing "advent of code" has not
// made a mistake worth a diagnostic.
func ScopeNamed(name string) (*Scope, bool) {
	name = strings.TrimSpace(name)
	for _, sc := range Scopes {
		if strings.EqualFold(sc.Name, name) {
			return sc, true
		}
		for _, a := range sc.Aliases {
			if strings.EqualFold(a, name) {
				return sc, true
			}
		}
	}
	return nil, false
}

// ScopeNames lists every registered scope's name, for an error that has to
// say what the choices are.
func ScopeNames() []string {
	out := make([]string, 0, len(Scopes))
	for _, sc := range Scopes {
		out = append(out, sc.Name)
	}
	return out
}

// PartForm is one kind of Part a scope permits, as a user writes it.
type PartForm struct {
	Form string // `Part Every N:`
	Doc  string // what the role is for
}

// PartForms is a scope's Part roles, for a tool that offers them — the editor
// completions, the reference page. It takes a scope *name* rather than a
// *Scope so that a caller holding only what a file declared does not have to
// resolve the program to ask.
//
// An unknown name has no roles rather than the default's: a file declaring a
// scope this build does not have is one the resolver is about to refuse, and
// offering it another scope's vocabulary in the meantime would be a lie about
// what will compile.
func PartForms(scope string) []PartForm {
	sc := DefaultScope
	if scope != "" {
		named, ok := ScopeNamed(scope)
		if !ok {
			return nil
		}
		sc = named
	}
	forms := sc.roleNames()
	out := make([]PartForm, 0, len(forms))
	for i, r := range sc.PartRoles {
		out = append(out, PartForm{Form: forms[i], Doc: r.Doc})
	}
	return out
}

// ScopeSummaries is every scope's name and one-line summary, for a tool that
// offers the choice.
func ScopeSummaries() []PartForm {
	out := make([]PartForm, 0, len(Scopes))
	for _, sc := range Scopes {
		out = append(out, PartForm{Form: sc.Name, Doc: sc.Summary})
	}
	return out
}

// RegistryFor returns the primitives a program in this scope resolves
// against: Core, then whatever the scope adds. A nil scope is the default,
// which adds nothing — so this returns a slice with Core's contents, in
// Core's order, and every question asked of the vocabulary in an ordinary
// program is answered exactly as it was before scopes existed.
func RegistryFor(sc *Scope) []*Primitive {
	if sc == nil || len(sc.Prims) == 0 {
		return Core
	}
	out := make([]*Primitive, 0, len(Core)+len(sc.Prims))
	// Scope primitives go first: a scope's vocabulary is the specific one,
	// and the registry is searched specific-matcher-first.
	out = append(out, sc.Prims...)
	return append(out, Core...)
}

// AllPrimitives returns every primitive in the language, across every scope,
// deduplicated and in registration order. It is for the tools that describe
// the language rather than resolve one program: the editor grammars, the
// reference index. Resolution uses RegistryFor.
func AllPrimitives() []*Primitive {
	out := slices.Clone(Core)
	seen := make(map[*Primitive]bool, len(Core))
	for _, p := range Core {
		seen[p] = true
	}
	for _, sc := range Scopes {
		for _, p := range sc.Prims {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// roleNamed finds a scope's Part role by the word written after `Part`.
func (sc *Scope) roleNamed(name string) (PartRole, bool) {
	for _, r := range sc.PartRoles {
		if r.Name == name {
			return r, true
		}
	}
	return PartRole{}, false
}

// worldRole returns the role that defines the world type, if the scope has
// one.
func (sc *Scope) worldRole() (PartRole, bool) {
	for _, r := range sc.PartRoles {
		if r.DefinesWorld {
			return r, true
		}
	}
	return PartRole{}, false
}

// roleNames lists a scope's roles as a user would write them, for an error
// that has to say what is available. The unroled role is named by the shape
// it takes rather than by its empty name.
func (sc *Scope) roleNames() []string {
	out := make([]string, 0, len(sc.PartRoles))
	for _, r := range sc.PartRoles {
		if r.Name == "" {
			out = append(out, `Part "label":`)
			continue
		}
		// The argument shape is part of how a role is written, so a list of
		// what is available has to show it: `Part Every:` is not something a
		// user can type.
		switch r.Arg {
		case ArgLabel:
			out = append(out, `Part `+r.Name+` "…":`)
		case ArgLabelOptional:
			out = append(out, `Part `+r.Name+` ["…"]:`)
		case ArgInt:
			out = append(out, "Part "+r.Name+" N:")
		default:
			out = append(out, "Part "+r.Name+":")
		}
	}
	return out
}

// scopeOtherThan reports the first scope other than sc that registers a Part
// role by this name, so an error can say where the role the user wrote does
// exist rather than only that it does not exist here.
func scopeOtherThan(sc *Scope, role string) (*Scope, bool) {
	for _, other := range Scopes {
		if other == sc {
			continue
		}
		if _, ok := other.roleNamed(role); ok {
			return other, true
		}
	}
	return nil, false
}

// primInScopeOtherThan reports the first scope other than sc whose own
// primitives answer to this phrase — the difference between "no such
// operation" and "not in this Innate Domain", which is the more useful thing
// to be told.
func primInScopeOtherThan(sc *Scope, stmt *ast.Statement) (*Scope, *Primitive, bool) {
	for _, other := range Scopes {
		if other == sc {
			continue
		}
		for _, p := range other.Prims {
			if p.Keyword == stmt.Keyword && p.Match(stmt.Op) {
				return other, p, true
			}
		}
	}
	return nil, nil, false
}

// unknownScopeMessage explains an `Innate Domain:` naming something that is
// not a scope, and lists what is.
func unknownScopeMessage(name string) string {
	return fmt.Sprintf("unknown Innate Domain %q; the ones this build has are: %s",
		name, strings.Join(ScopeNames(), ", "))
}

// IsDefinitionRole reports whether a scope's Part role defines something
// rather than running it — a `Part Entity "Creep":`, whose body becomes a
// Shikigami called by name.
//
// It is exported for the tools that read a program without resolving it. The
// linter is the one that needs it: a definition's body is resolved at its call
// sites as a substituted copy, so the original's arguments are never marked
// read, and a check that asked "was this argument used?" of one would report
// every line of every uncalled definition.
func IsDefinitionRole(scope, role string) bool {
	if role == "" {
		return false
	}
	sc, ok := ScopeNamed(scope)
	if !ok {
		sc = DefaultScope
	}
	r, ok := sc.roleNamed(role)
	return ok && r.Definition
}
