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
	"sort"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
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
}

// ArgSet provides typed lookup over a statement's named arguments.
type ArgSet struct {
	args []*ast.Arg
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

// Lambda returns the lambda supplied for a named argument, if any.
func (a ArgSet) Lambda(name string) (*ast.Lambda, bool) {
	if v, ok := a.get(name); ok {
		if la, ok := v.(ast.LambdaArg); ok {
			return la.Lambda, true
		}
	}
	return nil, false
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

// argNames is every named argument the vocabulary reads, for the linter's
// "did you mean" on a misspelled one. It is documentation, not dispatch:
// nothing consults it to decide whether an argument is valid (ArgSet records
// the reads that actually happen), so a name missing here costs a suggestion
// and nothing else. A test pins it against the names the registry looks up.
var argNames = []string{
	"By", "Col", "Count", "Default", "Fill", "From", "Height", "High", "Index",
	"Low", "Mark", "Mode", "Row", "Seed", "Size", "Step", "Thickness", "Times",
	"Until", "Using", "While", "Width", "With", "Zip",
}

// ArgNames returns the named arguments the vocabulary understands, sorted.
func ArgNames() []string {
	out := append([]string(nil), argNames...)
	sort.Strings(out)
	return out
}

// Registry is the ordered list of primitives. Order matters: more specific
// matchers (e.g. "Split Each") must precede more general ones (e.g. "Split").
var Registry = []*Primitive{
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
	mapValues,   // before Map Each: "Map Values" is the more specific phrase
	filterEntries, // before Filter, likewise
	mapEach,
	filter,
	unique,
	matchPattern,
	takeItem,
	mapCells,
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
	sortBy,
	sortPrim,
	slidingReduce,
	allPairs,
	combinations,
	permutations,
	subsets,
	explore,
	bfs,
	dijkstra,
	floodFill,
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
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

// resolver carries the channel type environment and the Shikigami registry
// while lowering a program.
type resolver struct {
	channels   map[string]*ir.Type
	parts      map[string]bool // Part labels already defined, to catch duplicates
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
func ResolveWith(prog *ast.Program, opts ResolveOptions) (*ir.Pipeline, error) {
	r := &resolver{
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
		for name, site := range r.origins {
			opts.Sites[name] = site
		}
	}

	if err := InferWith(prog, callableNames(r.shikigamis)); err != nil {
		return nil, err
	}

	nodes, _, err := r.resolveSequence(prog.Statements, nil, scopeTop)
	if err != nil {
		// The partial pipeline rides along with the error; see resolveSequence.
		return &ir.Pipeline{Nodes: nodes}, err
	}
	return &ir.Pipeline{Nodes: nodes}, nil
}

// callableNames is the set of Shikigami names a bare phrase may resolve to.
func callableNames(defs map[string]*ast.ShikigamiDef) []string {
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
	scopeNested               // neither: loop and Shikigami bodies
)

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
		switch {
		case stmt.Keyword == "Shikigami":
			subNodes, outType, err := r.resolveShikigamiCall(stmt, cur)
			if err != nil {
				return nodes, nil, err
			}
			nodes = append(nodes, subNodes...) // inline the body
			cur = outType
		case stmt.Keyword == "Simple Domain":
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
					Msg: "Channels cannot be nested inside a Channel body"}
			}
			node, err := r.resolveChannel(stmt, cur)
			if err != nil {
				return nodes, nil, err
			}
			nodes = append(nodes, node) // a Channel does not change the current value
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
			if sc == scopeNested {
				return nodes, nil, &ResolveError{Pos: stmt.Pos,
					Msg: "From: consumers are not allowed inside a loop or Shikigami body"}
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
	}
	return nodes, cur, nil
}

// resolveOne lowers a single ordinary (non-channel) statement.
func (r *resolver) resolveOne(stmt *ast.Statement, cur *ir.Type) (*ir.Node, error) {
	if stmt.Op == nil && len(stmt.Block) == 0 {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: fmt.Sprintf("keyword %q has no operation", stmt.Keyword)}
	}
	if len(stmt.Block) > 0 {
		return nil, &ResolveError{Pos: stmt.Pos,
			Msg: "nested pipeline blocks are only allowed under Channel (v0.2)"}
	}
	prim := findPrimitive(stmt)
	if prim == nil {
		return nil, &ResolveError{Pos: stmt.Pos, Msg: unknownOpMessage(stmt)}
	}
	return prim.Build(stmt.Op, ArgSet{stmt.Args}, cur, stmt.Pos)
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

func findPrimitive(stmt *ast.Statement) *Primitive {
	for _, p := range Registry {
		if p.Keyword == stmt.Keyword && p.Match(stmt.Op) {
			return p
		}
	}
	return nil
}

func unknownOpMessage(stmt *ast.Statement) string {
	var known []string
	for _, p := range Registry {
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
	sort.Strings(known)
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
