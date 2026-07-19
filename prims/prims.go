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

func (a ArgSet) get(name string) (ast.ArgValue, bool) {
	for _, arg := range a.args {
		if arg.Name == name {
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
	flatten,
	enumerate,
	mapEach,
	filter,
	unique,
	matchPattern,
	takeItem,
	mapCells,
	findCells,
	transpose,
	apply,
	convertToIntegers,
	convertToFloats,
	convertToSparseGrid, // before Convert To Grid: its phrase also names Grid
	convertToGrid,
	convertToSet,
	// Maximum Technique (reductions) — By-variants before their bare forms.
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
	product,
	fold,
	groupBy,
	intersectAll,
	unionAll,
	differenceAll,
	mergeRanges,
	join,
	// Domain Expansion (swappable algorithms) — Sort By before Sort.
	sortBy,
	sortPrim,
	allPairs,
	combinations,
	permutations,
	subsets,
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
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

// resolver carries the channel type environment and the Shikigami registry
// while lowering a program.
type resolver struct {
	channels     map[string]*ir.Type
	shikigamis   map[string]*ast.ShikigamiDef
	preludeNames map[string]bool // defs that came from the embedded prelude
	depth        int             // Shikigami inlining depth, for recursion guard
}

// Resolve lowers a program into a typed pipeline, matching each statement to a
// primitive and type-checking the chain. Channel statements branch named
// sub-pipelines from the current value; From:-consumers recombine them;
// Shikigami calls inline their (parameter-substituted) bodies.
func Resolve(prog *ast.Program) (*ir.Pipeline, error) {
	r := &resolver{
		channels:     map[string]*ir.Type{},
		shikigamis:   map[string]*ast.ShikigamiDef{},
		preludeNames: map[string]bool{},
	}
	// The prelude defines standard operations as Shikigami; load them first so
	// programs (and the user's own definitions) can build on them.
	prelude, err := preludeDefs()
	if err != nil {
		return nil, err
	}
	for _, d := range prelude {
		r.shikigamis[d.Name] = d
		r.preludeNames[d.Name] = true
	}
	for _, d := range prog.Shikigamis {
		r.shikigamis[d.Name] = d
		// A user definition shadows a prelude name; its body positions are in
		// the user's file again, so drop the prelude label.
		delete(r.preludeNames, d.Name)
	}

	nodes, _, err := r.resolveSequence(prog.Statements, nil, true)
	if err != nil {
		return nil, err
	}
	return &ir.Pipeline{Nodes: nodes}, nil
}

// resolveSequence lowers a run of statements threading a current type. When
// allowChannels is true (top level), Channel statements and From:-consumers are
// permitted; channel sub-pipelines pass false (no nesting in v0.1).
func (r *resolver) resolveSequence(stmts []*ast.Statement, in *ir.Type, allowChannels bool) ([]*ir.Node, *ir.Type, error) {
	var nodes []*ir.Node
	cur := in
	for _, stmt := range stmts {
		switch {
		case stmt.Keyword == "Shikigami":
			subNodes, outType, err := r.resolveShikigamiCall(stmt, cur)
			if err != nil {
				return nil, nil, err
			}
			nodes = append(nodes, subNodes...) // inline the body
			cur = outType
		case stmt.Keyword == "Simple Domain":
			node, err := r.resolveLoop(stmt, cur)
			if err != nil {
				return nil, nil, err
			}
			nodes = append(nodes, node)
			cur = node.Out
		case stmt.Keyword == "Channel":
			if !allowChannels {
				return nil, nil, &ResolveError{Pos: stmt.Pos,
					Msg: "Channels cannot be nested inside a Channel body (v0.1)"}
			}
			node, err := r.resolveChannel(stmt, cur)
			if err != nil {
				return nil, nil, err
			}
			nodes = append(nodes, node) // a Channel does not change the current value
		case hasFrom(stmt):
			if !allowChannels {
				return nil, nil, &ResolveError{Pos: stmt.Pos,
					Msg: "From: consumers are not allowed inside a Channel body (v0.1)"}
			}
			node, err := r.resolveConsumer(stmt, cur)
			if err != nil {
				return nil, nil, err
			}
			nodes = append(nodes, node)
			cur = node.Out
		default:
			node, err := r.resolveOne(stmt, cur)
			if err != nil {
				return nil, nil, err
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
