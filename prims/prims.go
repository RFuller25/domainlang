// Package prims implements the real vocabulary of Domain: the small set of
// primitives that exist directly in Go, plus the resolver that lowers a parsed
// program into a typed IR pipeline by matching each statement against a
// primitive and chaining/type-checking the results.
//
// Everything in Domain is either a primitive (here) or a shikigami composed
// from primitives (deferred to v0.2). v0.1 implements only the primitives the
// AoC 2022 Day 1 Part 2 program requires.
package prims

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Primitive is one entry of the vocabulary.
type Primitive struct {
	ID      string
	Keyword string // the themed keyword this primitive lives under
	// Match reports whether this primitive handles the given operation phrase.
	Match func(op *ast.Operation) bool
	// Build validates the input type and constructs the resolved IR node. It
	// receives the operation phrase, the statement's named arguments (Using:,
	// Mode:, Seed:, ...), the current pipeline type, and a position.
	Build func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error)
	// Phrases are the operation phrases that name this primitive, for the tools
	// that need a spelling a user could actually write: the cross-keyword
	// ambiguity check, the linter's "did you mean", the language server's
	// completion. Like argNames it is documentation rather than dispatch —
	// Match decides what resolves — and it is empty for almost every
	// primitive, whose ID *is* the phrase you write. It is set only where the
	// two genuinely differ, as they do for a foreign block, whose ID names the
	// construct while its phrases are the four languages.
	Phrases []string
}

// Spellings returns the operation phrases that name p, which is its ID unless
// the primitive says otherwise.
func (p *Primitive) Spellings() []string {
	if len(p.Phrases) > 0 {
		return p.Phrases
	}
	return []string{p.ID}
}

// ArgSet provides typed lookup over a statement's named arguments, and over
// the indented pipeline body a statement may carry in place of a `Using:`
// lambda (see prims/block.go).
type ArgSet struct {
	args []*ast.Arg
	// block is the statement's indented sub-pipeline, if it has one, together
	// with the resolver that can lower it and a flag recording whether some
	// primitive actually took it. The flag is the body's counterpart to
	// ast.Arg.Used: a primitive that never asks for a lambda would otherwise
	// ignore a body silently, and the resolver reports that rather than
	// running a program that quietly drops a stage the user wrote.
	block    []*ast.Statement
	res      *resolver
	blockUse *bool
	// foreign is the statement's block of foreign-language source, if it has
	// one, with the same "was it read?" flag the body carries and for the same
	// reason: only one primitive in the registry takes one, and a statement
	// that resolved to any other must not have its block quietly dropped.
	foreign    *ast.ForeignBlock
	foreignUse *bool
	// locals rewrites each lambda as it is read, against the `Consider`
	// bindings in scope: constants substituted, calls to function bindings
	// inlined (prims/locals.go). It happens here rather than in each Build so
	// that every primitive in the vocabulary gets bindings without knowing
	// they exist — the same seam the indented-body form uses.
	//
	// The rewrite is memoized per argument because it must be *the same*
	// lambda every time: a primitive that stores one in Meta and captures
	// another in its Eval closure would otherwise have the optimizer simplify
	// the copy the interpreter never runs.
	rewritten map[*ast.Arg]*ast.Lambda
	// rewriteErr carries a rewrite failure past Build, which has no way to
	// report one: ArgSet.Lambda answers "is there a lambda?", not "was it
	// well formed?". resolveOne checks it once Build has returned.
	rewriteErr *error
}

// requestSpec finds the pre-scanned spec for a request tag, so the primitive
// that resolves the statement fills in the object the reply Part was already
// typed against rather than making a second one.
func (a ArgSet) requestSpec(tag string) (*ir.RequestSpec, bool) {
	if a.res == nil {
		return nil, false
	}
	spec, ok := a.res.requests[tag]
	return spec, ok
}

// DeclaredType returns an argument written as a type, lowered.
//
// It is how a primitive learns what a program expects back from something the
// program cannot see into — a JSON document, a server's answer. Encoding needs
// no such declaration; decoding has nothing else to go on.
func (a ArgSet) DeclaredType(name string, pos token.Position) (*ir.Type, bool, error) {
	v, ok := a.get(name)
	if !ok {
		return nil, false, nil
	}
	ta, isType := v.(ast.TypeArg)
	if !isType {
		return nil, true, &ResolveError{Pos: pos, Msg: fmt.Sprintf("%s: takes a type, e.g. `%s: {score: Int}`", name, name)}
	}
	t, err := lowerTypeExpr(ta.Type, pos)
	if err != nil {
		return nil, true, err
	}
	return t, true, nil
}

// hasBlock reports whether an indented pipeline body is available to stand in
// for a Using: lambda.
func (a ArgSet) hasBlock() bool { return len(a.block) > 0 && a.res != nil }

// ForeignBlock returns the statement's block of foreign-language source,
// recording that a primitive asked for it.
func (a ArgSet) ForeignBlock() (*ast.ForeignBlock, bool) {
	if a.foreign == nil {
		return nil, false
	}
	if a.foreignUse != nil {
		*a.foreignUse = true
	}
	return a.foreign, true
}

// get finds a named argument and records that some primitive asked for it.
// Every typed accessor goes through here, so "was this argument ever read?"
// is answered by the resolver itself rather than by a table of accepted names
// per primitive that could drift from what Build actually reads. The mark
// lands on the AST node (ArgSet holds pointers), which is where diag's
// unused-argument lint reads it back.
func (a ArgSet) get(name string) (ast.ArgValue, bool) {
	for _, arg := range a.args {
		if arg.Name == name {
			arg.Used = true
			return arg.Value, true
		}
	}
	return nil, false
}

// Has reports whether a named argument is present.
func (a ArgSet) Has(name string) bool {
	_, ok := a.get(name)
	return ok
}

// Lambda returns the lambda supplied for a named argument, if any, rewritten
// against the `Consider` bindings and `Cursed Object` globals in scope
// (prims/locals.go, prims/globals.go). A statement with neither gets back
// exactly the lambda the user wrote.
func (a ArgSet) Lambda(name string) (*ast.Lambda, bool) {
	for _, arg := range a.args {
		if arg.Name != name {
			continue
		}
		arg.Used = true
		la, ok := arg.Value.(ast.LambdaArg)
		if !ok {
			return nil, false
		}
		return a.bindLambda(arg, la.Lambda), true
	}
	return nil, false
}

// bindLambda rewrites one argument's lambda once and remembers the result.
//
// Whether there is anything to rewrite against is asked through
// resolver.rewritesNeeded, shared with rewriteLambda: this is the other place
// that skips the work, and the two disagreeing would be silent either way.
func (a ArgSet) bindLambda(arg *ast.Arg, lam *ast.Lambda) *ast.Lambda {
	if a.res == nil || !a.res.rewritesNeeded() {
		return lam
	}
	if done, ok := a.rewritten[arg]; ok {
		return done
	}
	out, err := a.res.rewriteLambda(lam)
	if err != nil {
		if a.rewriteErr != nil && *a.rewriteErr == nil {
			*a.rewriteErr = err
		}
		out = lam
	}
	if a.rewritten != nil {
		a.rewritten[arg] = out
	}
	return out
}

// argSet builds the ArgSet for a statement, with the binding scope and the
// indented body wired in. Every path that resolves a statement's arguments
// goes through here, so no primitive can be reached with bindings missing
// from its lambdas.
func (r *resolver) argSet(stmt *ast.Statement, blockUse, foreignUse *bool, rewriteErr *error) ArgSet {
	a := ArgSet{
		args:       stmt.Args,
		res:        r,
		rewritten:  map[*ast.Arg]*ast.Lambda{},
		rewriteErr: rewriteErr,
		blockUse:   blockUse,
		foreignUse: foreignUse,
	}
	if blockUse != nil {
		a.block = stmt.Block
	}
	if foreignUse != nil {
		a.foreign = stmt.Foreign
	}
	return a
}

// args is argSet for the callers that only read named arguments — a loop's
// count, a channel consumer's lambda — and never take an indented body or a
// foreign block, because the statement's own block means something else there.
func (r *resolver) args(stmt *ast.Statement, rewriteErr *error) ArgSet {
	return r.argSet(stmt, nil, nil, rewriteErr)
}

// Int returns an integer named argument, if present and integer-typed.
func (a ArgSet) Int(name string) (int64, bool) {
	if v, ok := a.get(name); ok {
		if ia, ok := v.(ast.IntArg); ok {
			return ia.Value, true
		}
	}
	return 0, false
}

// Text returns a string named argument, if present and string-typed.
func (a ArgSet) Text(name string) (string, bool) {
	if v, ok := a.get(name); ok {
		if sa, ok := v.(ast.StringArg); ok {
			return sa.Value, true
		}
	}
	return "", false
}

// Float returns a float named argument. An integer literal is accepted and
// widened, matching the numeric tower's single promotion rule.
func (a ArgSet) Float(name string) (float64, bool) {
	if v, ok := a.get(name); ok {
		switch x := v.(type) {
		case ast.FloatArg:
			return x.Value, true
		case ast.IntArg:
			return float64(x.Value), true
		}
	}
	return 0, false
}

// Ident returns a bare-identifier named argument (e.g. Mode: Filter).
func (a ArgSet) Ident(name string) (string, bool) {
	if v, ok := a.get(name); ok {
		if ida, ok := v.(ast.IdentArg); ok {
			return ida.Value, true
		}
	}
	return "", false
}

// Idents returns a list of identifiers for an argument, accepting both a single
// ident (From: a) and a comma-separated list (From: a, b).
func (a ArgSet) Idents(name string) ([]string, bool) {
	if v, ok := a.get(name); ok {
		switch x := v.(type) {
		case ast.IdentListArg:
			return x.Values, true
		case ast.IdentArg:
			return []string{x.Value}, true
		}
	}
	return nil, false
}

// Cases returns every `Case: <tag> "<template>"` argument, in the order
// written — which is the order they are tried, so the program controls
// priority when two templates could both match a line.
func (a ArgSet) Cases(name string) []ast.CaseArg {
	var out []ast.CaseArg
	for _, arg := range a.args {
		if arg.Name != name {
			continue
		}
		if c, ok := arg.Value.(ast.CaseArg); ok {
			arg.Used = true
			out = append(out, c)
		}
	}
	return out
}

// argNames is every named argument the vocabulary reads, for the linter's
// "did you mean" on a misspelled one. It is documentation, not dispatch:
// nothing consults it to decide whether an argument is valid (ArgSet records
// the reads that actually happen), so a name missing here costs a suggestion
// and nothing else. A test pins it against the names the registry looks up.
var argNames = []string{
	"By", "Col", "Combine", "Cost", "Count", "Default", "Fill", "From",
	"Height", "High", "Index", "Low", "Mark", "Mode", "Params", "Row", "Seed",
	"Case", "Size", "Step", "Thickness", "Times", "Until", "Using", "Value", "While",
	"Width", "With", "Zip",
	// The graph searches name their endpoints. They are deliberately not
	// `From:`/`To:` — `From:` already means "these channels" everywhere else,
	// and a node is not a channel.
	"Start", "Goal",
	// A request names where it goes, what it carries, what comes back and
	// what to call the answer. `Into:` is a *type* rather than a value, which
	// is the only one of these the parser has to know about by name
	// (ast.TypeValuedArg).
	"Url", "Method", "Body", "As", "Into", "Timeout",
	// Load and Save read and write a file by name.
	"Path",
}

// ArgNames returns the named arguments the vocabulary understands, sorted.
func ArgNames() []string {
	return slices.Sorted(slices.Values(argNames))
}

// Core is the ordered list of primitives every scope has. Order matters: more
// specific matchers (e.g. "Split Each") must precede more general ones (e.g.
// "Split").
//
// It is the whole vocabulary, not a subset: an `Innate Domain` adds
// primitives and never takes any away (see prims/scope.go). What a program
// resolves against is RegistryFor(scope), which for the default scope is
// exactly this slice.
var Core = []*Primitive{
	readSource,
	// Cursed Technique (transforms) — specific matchers before general.
	splitFieldsPrim, // before Split/Split Each: "Split Fields" names no separator
	splitEach,
	split,
	extractIntegers,
	raggedColumns,
	window,
	chunk,
	flatten,
	enumerate,
	pairs, // after nothing in particular, but its matcher excludes "All Pairs"
	scan,
	takeWhile, // before Take Item: "While" and "Item" pick them apart
	dropWhile,
	partition,
	iterate, // matcher excludes the Iterate Until Fixed Point loop head
	unfold,
	mapValues,     // before Map Each: "Map Values" is the more specific phrase
	filterEntries, // before Filter, likewise
	mapEach,
	filter,
	unique,
	matchPattern,
	takeItem,
	mapCells,
	// The shape-preserving maps, one per collection kind, beside Map Cells
	// which is the one that was already here. None of their phrases carries
	// "Each", so unlike Map Values they need no ordering against Map Each.
	mapElements,
	mapKeys,
	mapNodes,
	mapWeights,
	findCells,
	rangePrim, // before Merge Ranges: its matcher excludes that phrase
	transpose,
	rotateGrid,
	flipGrid,
	subgrid,
	padGrid,
	apply,
	convertToIntegers,
	convertToFloats,
	convertToSparseGrid, // before Convert To Grid: its phrase also names Grid
	convertToGrid,
	convertToSet,
	convertToRows,
	convertToEntries,
	// Before Convert To Map: "Convert To Graph" names neither Map nor Grid, so
	// ordering is not load-bearing here, but the coercions stay together.
	convertToGraph,
	convertToEdges,
	convertToAdjacency,
	convertToMap,
	// Maximum Technique (reductions) — By-variants before their bare forms.
	sumBy, // before Sum Each Group / Sum
	sumEachGroup,
	sum,
	selectTopK,
	countMatching,
	countCells,
	countBy,
	count,
	minBy,
	maxBy,
	maxPrim,
	minPrim,
	productBy, // before Product
	product,
	fold,
	reduce,
	anyPrim,
	allPrim,
	findCycle, // before Find/Find Index: "Cycle" is what tells them apart
	findIndex, // before Find: "Index" is what tells them apart
	findPrim,
	groupBy,
	intersectAll,
	unionAll,
	differenceAll,
	mergeRanges,
	join,
	// Domain Expansion (swappable algorithms) — Sort By and Topological Sort
	// before Sort, whose matcher accepts any phrase containing "Sort".
	topologicalSort,
	shortestPath,
	minimumSpanningTree,
	accumulateUp,
	graphRoot,
	sortBy,
	sortPrim,
	slidingReduce,
	allPairs,
	combinations,
	permutations,
	subsets,
	explore,
	// Before the grid searches and Sort: a foreign block's phrase is a bare
	// language name, which no other matcher accepts, but the registry is
	// ordered specific-first and this is as specific as a matcher gets.
	foreignPrim,
	bfs,
	dijkstra,
	floodFill,
	// Before Connected Components, whose matcher takes any phrase carrying
	// both of its words — "Strongly Connected Components" is one of them.
	stronglyConnectedComponents,
	connectedComponents,
	// Reverse Cursed Technique (inversions).
	reverse,
	// Sinks and assertions.
	emit,
	bindingVow,
}

// ResolveError is a resolution or type-checking error with a position.
type ResolveError struct {
	Pos token.Position
	Msg string
	// NeedsBlock marks the errors an indented block would fix — a missing
	// Using: lambda, a missing Seed:, a body that has not been typed yet.
	// The REPL waits for those lines instead of dropping the statement; see
	// parser.Error.NeedsBlock, which carries the same flag for the errors the
	// parser raises first.
	NeedsBlock bool
}

func (e *ResolveError) Error() string {
	// A whole-program refusal — a Part the program is missing, a shape rule it
	// does not meet — has no line of its own, and "0:0:" in front of it is
	// worse than nothing: it points at a place the user can go and look, and
	// there is nothing there.
	if e.Pos.Line == 0 && e.Pos.Col == 0 {
		return e.Msg
	}
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

// resolver carries the channel type environment and the Shikigami registry
// while lowering a program.
type resolver struct {
	// scope is the program's `Innate Domain`, never nil: a program that
	// declares none resolves against DefaultScope.
	scope *Scope
	// prims is RegistryFor(scope), computed once because every statement
	// searches it.
	prims []*Primitive
	// world is the type the scope's world-defining Part produced, or nil in
	// a scope with no such role. Part roles state their contracts against it.
	world *ir.Type
	// roleCounts is how many Parts of each role the program has declared so
	// far, for the cardinality a role states. Keyed by role name, so the
	// unroled Part counts under "".
	roleCounts map[string]int
	// roleLabels is the labels written for each role, in source order.
	roleLabels map[string][]string
	// requests is every request the program fires, by tag, collected before
	// any Part resolves — a `Part Reply` is typed against the Request that
	// fires its tag, and Parts are not resolved in the order they were
	// written, so waiting to meet it is not an option.
	requests map[string]*ir.RequestSpec
	// partGlobals is the global-access policy of the Part currently being
	// resolved, or GlobalsIsolated at the top level, where a `Cursed Tool`
	// has always been free to write. See sealedFrom.
	partGlobals GlobalPolicy
	channels    map[string]*ir.Type
	parts       map[string]bool // Part labels already defined, to catch duplicates
	// locals is the stack of `Consider x As/Of …` bindings in scope, innermost
	// last (prims/locals.go). It is a stack rather than a map because bindings
	// shadow: an inner block may rebind a name the outer one is still using
	// after it.
	locals     []localBind
	shikigamis map[string]*ast.ShikigamiDef
	// origins says where each Shikigami came from — the embedded prelude, an
	// imported library, or the user's own file. token.Position carries no file,
	// so this is how an error inside an inlined body can say which file the
	// position belongs to instead of masquerading as one in the user's source.
	origins  map[string]DefSite
	displays map[string]string // Shikigami name → library file name, for messages
	// inlining is the chain of Shikigami currently being inlined, outermost
	// first. Inlining terminates because a name may not appear twice in the
	// chain — that is a cycle, and it is reported as one. There is no depth
	// limit: a deeply composed but non-recursive program is legal however deep
	// it goes, and the old fixed ceiling refused those too.
	inlining []string
	// globals is the program's `Cursed Object` table: one entry per declared
	// global, in declaration order, so an entry's index is its slot. It is a
	// flat list rather than a stack because globals do not shadow each other —
	// a second declaration of a live name is an error, not a shadow — and
	// because the slot numbering has to stay stable for the whole program.
	globals []globalDecl
	// globalIndex maps a name to its position in globals, for the lookup
	// rewriteExpr does on every identifier it cannot otherwise place.
	globalIndex map[string]int
	// laterGlobals is every global the program declares *anywhere*, collected
	// before resolution starts, and used only to explain an identifier that is
	// declared further down. Without it "unknown identifier" is technically
	// true and useless: reading a global above its `Cursed Object` is the
	// mistake the forward-visibility rule invites, so it is worth naming.
	laterGlobals map[string]token.Position
	// mutatedGlobals is every global name something in the program can change
	// after it is declared. Collected before resolution, because a read has to
	// know it while the write may be a hundred lines further down. See
	// prims/globals.go and ast.GlobalRef.Mutable.
	mutatedGlobals map[string]bool
	// inChannel is set while a Channel body is being resolved, which is where
	// globals are out of reach in both directions (prims/globals.go).
	inChannel bool
	// foreignDepth counts the enclosing Shikigami bodies that came from the
	// prelude or an imported library, which are sealed off from globals for
	// the same reason. A counter rather than a flag because such a definition
	// may call another.
	foreignDepth int
}

// Resolve lowers a program into a typed pipeline, matching each statement to a
// primitive and type-checking the chain. Channel statements branch named
// sub-pipelines from the current value; From:-consumers recombine them;
// Shikigami calls inline their (parameter-substituted) bodies.
//
// It begins by running Infer over prog, which fills in the keyword of every
// statement written as a bare operation phrase — so prog is mutated, and from
// this point on the two spellings are the same program.
func Resolve(prog *ast.Program) (*ir.Pipeline, error) {
	return ResolveWith(prog, ResolveOptions{})
}

// ResolveWith is Resolve with the file context `Innate Domain` imports need.
// Callers that have a program path (the CLI, the REPL, the language server, the
// diagnostics engine) pass its directory as BaseDir plus SearchPath(); the
// zero-value options reject imports with a positioned error rather than
// silently dropping them.
//
// Definitions are registered weakest-first — prelude, then imports in load
// order, then the program's own — so each layer shadows the one beneath it.
func ResolveWith(prog *ast.Program, opts ResolveOptions) (pipe *ir.Pipeline, err error) {
	// Resolution runs against whatever an editor's buffer says right now, which
	// is every incomplete program there is, and it runs in processes that must
	// outlive one bad one — the language server, the dev TUI, the REPL. So a
	// panic in the resolver becomes an ordinary error at this boundary, exactly
	// as interp.Run does for the runtime half. It stays loud enough to report:
	// nothing a program can contain is supposed to reach here.
	defer func() {
		if p := recover(); p != nil {
			pipe, err = nil, fmt.Errorf("internal error during resolution: %v", p)
		}
	}()

	// Resolution is a fresh start. A program that failed part-way through a
	// binding's scope left its types on typecheck's stack, and the REPL, the
	// language server and the diagnostics engine all resolve again in the same
	// process — where a leaked binding would resolve a name the new program
	// never bound.
	typecheck.ResetBindings()

	// The program's Innate Domain decides the vocabulary every statement below
	// resolves against, so it is settled before anything else happens. An
	// override in the options wins, for the tools that resolve a fragment with
	// no declaration in it — the REPL, a language-server completion request.
	scope := DefaultScope
	switch {
	case opts.Scope != "":
		sc, ok := ScopeNamed(opts.Scope)
		if !ok {
			return nil, fmt.Errorf("%s", unknownScopeMessage(opts.Scope))
		}
		scope = sc
	case prog.Scope != nil:
		sc, ok := ScopeNamed(prog.Scope.Name)
		if !ok {
			return nil, &ResolveError{Pos: prog.Scope.Pos, Msg: unknownScopeMessage(prog.Scope.Name)}
		}
		scope = sc
	}
	r := &resolver{
		scope:      scope,
		prims:      RegistryFor(scope),
		roleCounts: map[string]int{},
		roleLabels: map[string][]string{},
		requests:   map[string]*ir.RequestSpec{},
		channels:   map[string]*ir.Type{},
		parts:      map[string]bool{},
		shikigamis: map[string]*ast.ShikigamiDef{},
		origins:    map[string]DefSite{},
		displays:   map[string]string{},
	}
	// The prelude defines standard operations as Shikigami; load them first so
	// programs (and the user's own definitions) can build on them.
	prelude, err := preludeDefs()
	if err != nil {
		return nil, err
	}
	for _, d := range prelude {
		r.shikigamis[d.Name] = d
		r.origins[d.Name] = DefSite{Origin: "prelude"}
	}
	// Imports next: a library shadows the prelude, and the program shadows both.
	// This runs before Infer because inference resolves a bare phrase against
	// the callable names, which now include every imported Shikigami.
	if err := r.loadImports(prog, opts); err != nil {
		return nil, err
	}
	for _, d := range prog.Shikigamis {
		r.shikigamis[d.Name] = d
		// A local definition shadows a prelude or imported name; its body
		// positions are in the user's own file, so it is a local origin again.
		r.origins[d.Name] = DefSite{Origin: "local"}
		delete(r.displays, d.Name)
	}
	if opts.Sites != nil {
		maps.Copy(opts.Sites, r.origins)
	}

	// A role that *defines* rather than runs — `Part Entity "Creep":` — lowers
	// to a Shikigami before anything else looks at the program, so its name is
	// callable from every other Part and from inference. The Parts themselves
	// are then not statements at all, which is why skip records them.
	entities, skip, err := entityDefs(r.scope, prog.Statements)
	if err != nil {
		return nil, err
	}
	for _, d := range entities {
		if err := checkShikigamiName(d, r.prims); err != nil {
			return nil, err
		}
		if _, taken := r.shikigamis[d.Name]; taken {
			return nil, &ResolveError{Pos: d.Pos, Msg: fmt.Sprintf(
				"%q is already defined; a Part that defines something may not shadow a Shikigami", d.Name)}
		}
		r.shikigamis[d.Name] = d
		r.origins[d.Name] = DefSite{Origin: "local"}
	}

	if err := InferWith(prog, callableNames(r.shikigamis), r.prims); err != nil {
		return nil, err
	}

	// Every request the program fires, before any Part is resolved: a
	// `Part Reply` is typed against the Request that answers its tag, and the
	// two may be written in either order.
	if err := r.collectRequests(prog.Statements); err != nil {
		return nil, err
	}

	// One traversal of everything that can reach the run — the program's own
	// statements and every definition it could call, the prelude's included,
	// since a Shikigami body is inlined into the pipeline and its expressions
	// run like any other. See programScan for the three questions it settles
	// and why they share a walk.
	scan := newProgramScan()
	scan.walk(prog.Statements, false)
	for _, d := range r.shikigamis {
		if d == nil {
			continue
		}
		// A Shikigami is inlined at call sites that may sit anywhere,
		// including inside a loop, so its body counts as nested.
		scan.walk(d.Body, true)
		for _, b := range d.Binds {
			if b == nil {
				continue
			}
			if b.Value != nil {
				ast.UpdatedNames(b.Value, scan.updated)
			}
			updatedInLambda(b.Lambda, scan.updated)
			scan.walk(b.Body, true)
		}
	}
	scan.finish()

	// Whether anything writes at all decides how the interpreter represents a
	// binding: it boxes every binding or none, because deciding per-binding
	// would mean re-deriving the answer on every application.
	if len(scan.updated) > 0 {
		eval.EnableUpdates()
	}
	r.laterGlobals = scan.declared
	r.mutatedGlobals = scan.mutated

	// The world is resolved before everything else, wherever it was written:
	// every other role states its contract against the world's type, so there
	// is nothing to check them against until it exists. The hoist is the same
	// one Shikigami definitions and imports already get.
	stmts := withoutStatements(prog.Statements, skip)
	var nodes []*ir.Node

	// In a scope with no top-level pipeline, the top level is declarations and
	// Parts — and neither has an order. Global declarations are resolved
	// first, so that a Part may read one wherever the two were written
	// relative to each other. In a scope *with* a pipeline this does not
	// happen: there, order is the program.
	if !r.scope.TopLevelPipeline {
		declNodes, rest, derr := r.resolveDeclarations(stmts)
		if derr != nil {
			return &ir.Pipeline{Scope: r.scope.Name, Globals: len(r.globals)}, r.explainForwardGlobal(derr)
		}
		nodes, stmts = declNodes, rest
	}

	// The world is resolved before everything else, wherever it was written:
	// every other role states its contract against the world's type, so there
	// is nothing to check them against until it exists.
	if worldRole, ok := r.scope.worldRole(); ok {
		worldStmt, rest, found := hoistWorld(r.scope, stmts)
		if !found && worldRole.Min > 0 {
			// Saying so here rather than letting the cardinality check catch
			// it later is the whole difference between "this program has no
			// Part World" and a pile of "has no input value" from every role
			// whose contract was written against a world that never arrived.
			return &ir.Pipeline{Nodes: nodes, Scope: r.scope.Name, Globals: len(r.globals)},
				&ResolveError{Msg: fmt.Sprintf("%s needs %s, and this program has none",
					r.scope.Name, atLeast(max(worldRole.Min, 1), worldRole.Name))}
		}
		if found {
			worldNodes, _, werr := r.resolveStatement(worldStmt, nil, scopeTop)
			if werr != nil {
				return &ir.Pipeline{Nodes: nodes, Scope: r.scope.Name, Globals: len(r.globals)}, r.explainForwardGlobal(werr)
			}
			nodes, stmts = append(nodes, worldNodes...), rest
		}
	}

	rest, _, err := r.resolveSequence(stmts, nil, scopeTop)
	nodes = append(nodes, rest...)
	if err != nil {
		// The partial pipeline rides along with the error; see resolveSequence.
		return &ir.Pipeline{Nodes: nodes, Scope: r.scope.Name, Globals: len(r.globals)}, r.explainForwardGlobal(err)
	}
	// A role the program has too few of can only be counted once the whole
	// program has been read: what is missing has no line to be reported at.
	if err := r.checkPartCardinality(); err != nil {
		return &ir.Pipeline{Nodes: nodes, Scope: r.scope.Name, Globals: len(r.globals)}, err
	}
	if r.scope.Shape != nil {
		if err := r.scope.Shape(r.facts()); err != nil {
			return &ir.Pipeline{Nodes: nodes, Scope: r.scope.Name, Globals: len(r.globals)},
				&ResolveError{Msg: err.Error()}
		}
	}
	return &ir.Pipeline{Nodes: nodes, Scope: r.scope.Name, Globals: len(r.globals)}, nil
}

// resolveDeclarations lowers the global declarations at the top of a
// Parts-only program, and returns everything else in order.
//
// A `Cursed Tool` write is refused there: the top level of such a program has
// no moment at which it happens — the host runs Parts, not a pipeline — so a
// write outside one would be a statement that never executes. Values are set
// where there is a time for them to be set.
func (r *resolver) resolveDeclarations(stmts []*ast.Statement) ([]*ir.Node, []*ast.Statement, error) {
	var nodes []*ir.Node
	rest := make([]*ast.Statement, 0, len(stmts))
	for _, stmt := range stmts {
		if stmt == nil || stmt.Keyword != "Cursed Object" {
			if stmt != nil && stmt.Keyword == "Cursed Tool" {
				return nil, nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
					"a %s program has no top level to run a `Cursed Tool` on: the Parts are what run, and a write out here would never happen. "+
						"Set it in `Part World:` or `Part Start:` instead", r.scope.Name)}
			}
			rest = append(rest, stmt)
			continue
		}
		declNodes, _, err := r.resolveStatement(stmt, nil, scopeTop)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, declNodes...)
	}
	return nodes, rest, nil
}

// facts is what a scope's hooks may ask about the program as a whole.
func (r *resolver) facts() ProgramFacts {
	return ProgramFacts{
		World:      r.world,
		PartCounts: r.roleCounts,
		PartLabels: r.roleLabels,
		Requests:   r.requests,
	}
}

// withoutStatements drops the statements a definition role already consumed.
func withoutStatements(stmts []*ast.Statement, skip map[*ast.Statement]bool) []*ast.Statement {
	if len(skip) == 0 {
		return stmts
	}
	out := make([]*ast.Statement, 0, len(stmts))
	for _, s := range stmts {
		if !skip[s] {
			out = append(out, s)
		}
	}
	return out
}

// hoistWorld pulls the Part that defines the world out of a statement list, so
// it can be resolved before the roles whose contracts are stated against it.
// A scope with no such role, or a program with no such Part, is unchanged —
// the missing-Part case is left to the cardinality check, which words it
// better than a silent nil world would.
func hoistWorld(sc *Scope, stmts []*ast.Statement) (*ast.Statement, []*ast.Statement, bool) {
	role, ok := sc.worldRole()
	if !ok {
		return nil, stmts, false
	}
	for i, s := range stmts {
		if s != nil && s.Keyword == "Part" && s.PartRole == role.Name {
			rest := make([]*ast.Statement, 0, len(stmts)-1)
			rest = append(rest, stmts[:i]...)
			return s, append(rest, stmts[i+1:]...), true
		}
	}
	return nil, stmts, false
}

// callableNames is the set of Shikigami names a bare phrase may resolve to.
func callableNames(defs map[string]*ast.ShikigamiDef) []string {
	return slices.Sorted(maps.Keys(defs))
}

// scope says which structural statements a run of statements may contain.
// Channel definitions and Part blocks belong to the top level only; From:
// consumers are also legal inside a Part, which is what lets each Part combine
// channels that were parsed once above it.
type scope int

const (
	scopeTop     scope = iota // Channel definitions, Part blocks, From: consumers
	scopePart                 // From: consumers only
	scopeChannel              // From: consumers only — a Channel body
	scopeLoop                 // From: consumers only — a loop body
	scopeNested               // neither: Shikigami and Using: bodies
)

// describe names a scope for an error message, so a refusal says which body
// the offending statement is actually in rather than assuming a Channel.
func (s scope) describe() string {
	switch s {
	case scopePart:
		return "a Part"
	case scopeChannel:
		return "a Channel body"
	case scopeLoop:
		return "a loop body"
	case scopeNested:
		return "a Shikigami or Using: body"
	default:
		return "the top level"
	}
}

// resolveSequence lowers a run of statements threading a current type. sc says
// what structural statements are permitted here (see scope).
//
// On failure it returns the nodes resolved *before* the error along with it, so
// tooling can still say something about the prefix that type-checked — the
// language server's inlay hints show types up to the first bad line. Callers
// that only want a whole program check err first, as they always did.
func (r *resolver) resolveSequence(stmts []*ast.Statement, in *ir.Type, sc scope) ([]*ir.Node, *ir.Type, error) {
	var nodes []*ir.Node
	cur := in
	for _, stmt := range stmts {
		stmtNodes, out, err := r.resolveStatement(stmt, cur, sc)
		if err != nil {
			// The partial pipeline rides along with the error; see below.
			return nodes, nil, err
		}
		nodes = append(nodes, stmtNodes...)
		cur = out
	}
	return nodes, cur, nil
}

// resolveStatement lowers one statement of a sequence, with its `Consider`
// bindings in scope for everything it and its nested body contain. Bindings
// whose values are only known at runtime put the statement's nodes inside a
// Consider node, which is what computes them and takes them out of scope
// again (see prims/locals.go).
func (r *resolver) resolveStatement(stmt *ast.Statement, cur *ir.Type, sc scope) ([]*ir.Node, *ir.Type, error) {
	if len(stmt.Binds) == 0 {
		return r.resolveStatementBody(stmt, cur, sc)
	}
	rts, pop, err := r.pushBinds(stmt.Binds, cur, updatedInStatements([]*ast.Statement{stmt}))
	if err != nil {
		return nil, nil, err
	}
	defer pop()

	nodes, out, err := r.resolveStatementBody(stmt, cur, sc)
	if err != nil {
		return nil, nil, err
	}
	if len(rts) == 0 {
		return nodes, out, nil
	}
	return []*ir.Node{bindNode(rts, nodes, cur, out, stmt.Pos)}, out, nil
}

// resolveStatementBody is resolveStatement without the binding scope: the
// statement kinds themselves.
func (r *resolver) resolveStatementBody(stmt *ast.Statement, cur *ir.Type, sc scope) ([]*ir.Node, *ir.Type, error) {
	var nodes []*ir.Node
	if sc == scopeTop && !r.scope.TopLevelPipeline {
		if err := r.refuseTopLevelPipeline(stmt); err != nil {
			return nodes, nil, err
		}
	}
	switch {
	case stmt.Keyword == "Shikigami":
		subNodes, outType, err := r.resolveShikigamiCall(stmt, cur)
		if err != nil {
			return nodes, nil, err
		}
		nodes = append(nodes, subNodes...) // inline the body
		cur = outType
	case stmt.Keyword == "Simple Domain":
		// A scope may add control-flow vocabulary of its own — a game's
		// `Quit`, which is a way out of a loop nobody wrote. Its primitives
		// are asked first, because the loop kinds are a closed set and
		// anything else under this keyword would otherwise be "needs a loop
		// kind" whatever it actually was. The default scope adds none, so
		// this is one nil check in an ordinary program.
		if prim := r.scopePrimitive(stmt); prim != nil {
			node, err := r.buildPrimitive(prim, stmt, cur)
			if err != nil {
				return nodes, nil, err
			}
			return append(nodes, node), node.Out, nil
		}
		node, err := r.resolveLoop(stmt, cur)
		if err != nil {
			return nodes, nil, err
		}
		nodes = append(nodes, node)
		cur = node.Out
	case stmt.Keyword == "Channel":
		if sc == scopePart {
			return nodes, nil, &ResolveError{Pos: stmt.Pos,
				Msg: "Channels cannot be defined inside a Part; define them above the Parts and consume them with From:"}
		}
		if sc != scopeTop {
			return nodes, nil, &ResolveError{Pos: stmt.Pos,
				Msg: "Channels cannot be nested inside " + sc.describe()}
		}
		node, err := r.resolveChannel(stmt, cur)
		if err != nil {
			return nodes, nil, err
		}
		nodes = append(nodes, node) // a Channel does not change the current value
	case stmt.Keyword == "Cursed Object" || stmt.Keyword == "Cursed Tool":
		node, err := r.resolveGlobals(stmt, cur)
		if err != nil {
			return nodes, nil, err
		}
		nodes = append(nodes, node) // a global declaration does not change the value
	case stmt.Keyword == "Part":
		if sc != scopeTop {
			return nodes, nil, &ResolveError{Pos: stmt.Pos,
				Msg: "Part blocks are only allowed at the top level"}
		}
		node, err := r.resolvePart(stmt, cur)
		if err != nil {
			return nodes, nil, err
		}
		nodes = append(nodes, node) // a Part does not change the current value
	case hasFrom(stmt):
		// A Channel body may consume channels declared *above* it: a name
		// enters r.channels only once its own body has resolved, so a
		// self- or forward-reference is already an unknown-channel error
		// and declaration order gives the dependency DAG for free.
		// A loop body may consume them too. A Channel is fully computed
		// before the loop starts and its value never changes, so there is no
		// ordering hazard — and without this a simulation has to smuggle its
		// read-only environment through the loop state, which (because a loop
		// body must preserve its value type) it then carries for every lap.
		//
		// A Shikigami or a `Using:` body still may not, and the reason is
		// structural rather than conservative: a Shikigami is inlined at call
		// sites that need not share a scope, and a `Using:` body compiles to a
		// top-level function where a channel's local is not in scope. Both
		// would be a promise the compiler could not keep.
		if sc == scopeNested {
			return nodes, nil, &ResolveError{Pos: stmt.Pos,
				Msg: "From: consumers are not allowed inside " + sc.describe() +
					" — a Shikigami is inlined wherever it is called and a Using: body " +
					"compiles to a function of its own, so neither can see a Channel's value"}
		}
		node, err := r.resolveConsumer(stmt, cur)
		if err != nil {
			return nodes, nil, err
		}
		nodes = append(nodes, node)
		cur = node.Out
	default:
		node, err := r.resolveOne(stmt, cur)
		if err != nil {
			return nodes, nil, err
		}
		nodes = append(nodes, node)
		cur = node.Out
	}
	return nodes, cur, nil
}

// resolveOne lowers a single ordinary (non-channel) statement.
func (r *resolver) resolveOne(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	// No operation phrase, whatever else the statement carries. Every Build
	// reads the phrase it was matched on, so this is checked before the lookup
	// rather than left to a primitive to notice: `Cursed Energy:` alone matches
	// Read Source (which reads any phrase at all) and used to hand it a nil one.
	// An indented body does not stand in for the phrase — it is an argument to
	// the operation, and there is no operation yet.
	if stmt.Op == nil {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("keyword %q has no operation", stmt.Keyword)}
	}
	prim := r.findPrimitive(stmt)
	if prim == nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: r.unknownOpMessage(stmt)}
	}
	return r.buildPrimitive(prim, stmt, cur)
}

// buildPrimitive runs one primitive's Build with the statement's arguments,
// and applies the checks every primitive gets whether it asked or not.
//
// It is separate from resolveOne because the structurally-dispatched keywords
// reach a primitive without going through the registry lookup, and they must
// still get these.
func (r *resolver) buildPrimitive(prim *Primitive, stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	// An indented body rides along in the ArgSet, where requireLambda picks it
	// up as the Using: lambda (prims/block.go). Whether it was picked up is the
	// primitive's answer to "do I take a lambda at all?", so no list of which
	// primitives accept a body has to be maintained here — or kept in sync.
	used, foreignUsed := false, false
	var rewriteErr error
	args := r.argSet(stmt, &used, &foreignUsed, &rewriteErr)
	node, err := prim.Build(stmt.Op, args, cur, stmt.Pos)
	// A lambda that could not be rewritten against the bindings in scope is
	// reported ahead of whatever Build made of it: the mangled lambda is the
	// cause, and its message names the binding.
	if rewriteErr != nil {
		return nil, rewriteErr
	}
	if err != nil {
		return nil, err
	}
	if stmt.Foreign != nil && !foreignUsed {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
			"%s does not take a block of %s code; only `Domain Expansion: %s` does",
			prim.ID, stmt.Foreign.Language, stmt.Foreign.Language)}
	}
	if len(stmt.Block) > 0 && !used {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
			"%s does not take an indented pipeline body: it has no Using: lambda for one to stand in for. "+
				"The statements that take an indented body are Channel, Part, Simple Domain, and any stage with a 1-parameter Using: lambda",
			prim.ID)}
	}
	return node, nil
}

// refuseTopLevelPipeline enforces a scope whose programs are declarations and
// Parts only.
//
// The rule and its wording are a library file's: "a library is a bag of
// Shikigami, not a program" (prims/imports.go). Here the bag holds Parts, and
// the reason is the same — there is no single value flowing through the top
// level for a stage to transform, so a stage there has nothing to act on.
func (r *resolver) refuseTopLevelPipeline(stmt *ast.Statement) error {
	switch stmt.Keyword {
	case "Part", "Cursed Object", "Cursed Tool":
		// Cursed Tool is accepted here and refused with a better message by
		// resolveDeclarations, which can say where to write it instead.
		return nil
	}
	return &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
		"a %s program is made of Parts and declarations, not a pipeline: there is no value at the top level for %s to act on. "+
			"Move this into a Part — the ones it takes are: %s",
		r.scope.Name, statementNoun(stmt), strings.Join(r.scope.roleNames(), ", "))}
}

// statementNoun names the offending statement the way its line reads, so the
// refusal points at something the user can see.
func statementNoun(stmt *ast.Statement) string {
	if stmt.Op != nil && strings.TrimSpace(stmt.Op.Raw) != "" {
		return fmt.Sprintf("%q", strings.TrimSpace(stmt.Op.Raw))
	}
	if stmt.Keyword != "" {
		return fmt.Sprintf("this %s", stmt.Keyword)
	}
	return "this statement"
}

// hasFrom reports whether a statement names channels via a From: argument.
func hasFrom(stmt *ast.Statement) bool {
	for _, a := range stmt.Args {
		if a.Name == "From" {
			return true
		}
	}
	return false
}

// scopePrimitive finds a primitive the *scope* adds, ignoring Core. It is for
// the keywords whose statements are dispatched structurally before the
// registry is consulted, where a scope's addition would otherwise be
// unreachable.
func (r *resolver) scopePrimitive(stmt *ast.Statement) *Primitive {
	for _, p := range r.scope.Prims {
		if p.Keyword == stmt.Keyword && p.Match(stmt.Op) {
			return p
		}
	}
	return nil
}

func (r *resolver) findPrimitive(stmt *ast.Statement) *Primitive {
	for _, p := range r.prims {
		if p.Keyword == stmt.Keyword && p.Match(stmt.Op) {
			return p
		}
	}
	return nil
}

func (r *resolver) unknownOpMessage(stmt *ast.Statement) string {
	// An operation that exists, but in another Innate Domain, is a different
	// mistake from one that does not exist — and being told which is the
	// difference between a minute and an hour.
	if other, p, ok := primInScopeOtherThan(r.scope, stmt); ok {
		return fmt.Sprintf("%q is a %s operation; this program's Innate Domain is %s",
			p.ID, other.Name, r.scope.Name)
	}
	var known []string
	for _, p := range r.prims {
		if p.Keyword == stmt.Keyword {
			known = append(known, p.ID)
		}
	}
	raw := ""
	if stmt.Op != nil {
		raw = stmt.Op.Raw
	}
	if len(known) == 0 {
		return fmt.Sprintf("unknown keyword %q (no primitives registered for it in v0.1)", stmt.Keyword)
	}
	slices.Sort(known)
	return fmt.Sprintf("unknown operation %q under %q; known operations: %s",
		raw, stmt.Keyword, strings.Join(known, ", "))
}

// hasWord reports whether op's primary words contain w (case-insensitive).
func hasWord(op *ast.Operation, w string) bool {
	if op == nil {
		return false
	}
	for _, x := range op.Words {
		if strings.EqualFold(x, w) {
			return true
		}
	}
	return false
}

// hasModifier reports whether op has a trailing modifier matching m.
func hasModifier(op *ast.Operation, m string) bool {
	if op == nil {
		return false
	}
	for _, x := range op.Modifiers {
		if strings.EqualFold(strings.TrimSpace(x), m) {
			return true
		}
	}
	return false
}

func typeErr(pos token.Position, prim string, want, got *ir.Type) error {
	return &ResolveError{Pos: pos,
		Msg: fmt.Sprintf("%s expects input of type %s, but the pipeline produced %s",
			prim, want, got)}
}
