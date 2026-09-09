// Package ir defines the typed pipeline graph that the optimizer and the
// interpreter both consume, together with the runtime value and type models.
//
// In v0.1 the pipeline is a linear chain of resolved operations; the general
// dataflow graph is deferred to v0.2+.
package ir

import (
	"fmt"
	"io"
	"strings"

	"domain/token"
)

// ---------------------------------------------------------------------------
// Type model
// ---------------------------------------------------------------------------

// TypeKind is the kind of a Domain value type. v0.1 needs Int, Text and
// List<T>; v0.2 adds Tuple and Record (emitted by Match Pattern). Map, Set and
// Grid are added in their respective milestones.
type TypeKind int

const (
	KInt   TypeKind = iota
	KFloat          // 64-bit IEEE float; expression layer + reductions, not keyable
	KText
	KBool // result of comparisons / predicates in the expression layer
	KList
	KTuple  // fixed-arity, positional: (T1, T2, ...)
	KRecord // named fields: {a:Int, b:Int}
	KMap    // Map<K,V>, K in {Int, Text}
	KSet    // Set<T>, T in {Int, Text}
	KGrid   // Grid<T>
	KSparse // Sparse<T>: unbounded 2D plane with a default value (see SparseValue)
	KGraph  // Graph<K>: directed, Int-weighted adjacency over keyable nodes (see GraphValue)
	KView   // View: an opaque render tree — text, style and layout (see ViewValue)
)

// Field is one named member of a Record type.
type Field struct {
	Name string
	Type *Type
}

// Type is a (possibly nested) value type.
type Type struct {
	Kind   TypeKind
	Elem   *Type   // element type for List/Set/Grid, value type for Map
	Key    *Type   // key type when Kind == KMap
	Elems  []*Type // element types when Kind == KTuple
	Fields []Field // fields when Kind == KRecord
}

func Int() *Type   { return &Type{Kind: KInt} }
func Float() *Type { return &Type{Kind: KFloat} }
func Text() *Type  { return &Type{Kind: KText} }
func Bool() *Type  { return &Type{Kind: KBool} }
func List(elem *Type) *Type {
	return &Type{Kind: KList, Elem: elem}
}

// Tuple builds a fixed-arity positional type.
func Tuple(elems ...*Type) *Type {
	return &Type{Kind: KTuple, Elems: elems}
}

// Record builds a type with named fields, in declared order.
func Record(fields ...Field) *Type {
	return &Type{Kind: KRecord, Fields: fields}
}

// Map builds a Map<key, val> type.
func Map(key, val *Type) *Type {
	return &Type{Kind: KMap, Key: key, Elem: val}
}

// Set builds a Set<elem> type.
func Set(elem *Type) *Type {
	return &Type{Kind: KSet, Elem: elem}
}

// Grid builds a Grid<elem> type.
func Grid(elem *Type) *Type {
	return &Type{Kind: KGrid, Elem: elem}
}

// Sparse builds a Sparse<elem> type — the nested/sparse grid.
func Sparse(elem *Type) *Type {
	return &Type{Kind: KSparse, Elem: elem}
}

// View builds the View type: an opaque tree of text, style and layout, built
// by the render builtins and rendered by RenderViewPlain (and, in a terminal,
// with styling). It has no element type — a View is not a collection of
// anything — which is why this takes no parameter.
//
// It is deliberately opaque: not keyable, not orderable, not comparable, and
// no arithmetic. The alternative was styled Text, and then `length` of a
// styled string counts escape bytes, every layout builtin has to do
// display-width arithmetic on a value that lies about its own size, and the
// footgun is permanent. With a type of its own the question cannot be asked.
func View() *Type { return &Type{Kind: KView} }

// Graph builds a Graph<node> type: a directed, Int-weighted adjacency over
// keyable nodes. Elem is the *node* type — a graph has no separate element,
// and the edge weight is always Int (an unweighted graph is one whose weights
// are all 1), which is why this takes one parameter rather than two.
func Graph(node *Type) *Type {
	return &Type{Kind: KGraph, Elem: node}
}

// Keyable reports whether t can be a Map key or Set element: Int, Text, or a
// Tuple/Record built from keyable types. The runtime side is KeyOf (composite
// values get a canonical comparable key); the compiled side comes free —
// tuples and records lower to Go structs of comparable fields. Lists stay
// unkeyable on purpose: their compiled representation is a slice, which Go
// cannot use as a map key.
func Keyable(t *Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case KInt, KText:
		return true
	case KTuple:
		for _, e := range t.Elems {
			if !Keyable(e) {
				return false
			}
		}
		return true
	case KRecord:
		for _, f := range t.Fields {
			if !Keyable(f.Type) {
				return false
			}
		}
		return true
	}
	return false
}

func (t *Type) String() string {
	if t == nil {
		return "<none>"
	}
	switch t.Kind {
	case KInt:
		return "Int"
	case KFloat:
		return "Float"
	case KText:
		return "Text"
	case KBool:
		return "Bool"
	case KList:
		return "List<" + t.Elem.String() + ">"
	case KTuple:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = e.String()
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case KRecord:
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			parts[i] = f.Name + ":" + f.Type.String()
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case KMap:
		return "Map<" + t.Key.String() + ", " + t.Elem.String() + ">"
	case KSet:
		return "Set<" + t.Elem.String() + ">"
	case KGrid:
		return "Grid<" + t.Elem.String() + ">"
	case KSparse:
		return "Sparse<" + t.Elem.String() + ">"
	case KGraph:
		return "Graph<" + t.Elem.String() + ">"
	case KView:
		return "View"
	default:
		return "<unknown>"
	}
}

// Equal reports whether two types are structurally identical. Records compare
// by field set (name → type), insensitive to declaration order.
func (t *Type) Equal(o *Type) bool {
	if t == nil || o == nil {
		return t == o
	}
	if t.Kind != o.Kind {
		return false
	}
	switch t.Kind {
	case KList, KSet, KGrid, KSparse, KGraph:
		return t.Elem.Equal(o.Elem)
	case KMap:
		return t.Key.Equal(o.Key) && t.Elem.Equal(o.Elem)
	case KTuple:
		if len(t.Elems) != len(o.Elems) {
			return false
		}
		for i := range t.Elems {
			if !t.Elems[i].Equal(o.Elems[i]) {
				return false
			}
		}
		return true
	case KRecord:
		if len(t.Fields) != len(o.Fields) {
			return false
		}
		om := make(map[string]*Type, len(o.Fields))
		for _, f := range o.Fields {
			om[f.Name] = f.Type
		}
		for _, f := range t.Fields {
			ot, ok := om[f.Name]
			if !ok || !f.Type.Equal(ot) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

// ---------------------------------------------------------------------------
// Value model
// ---------------------------------------------------------------------------

// Value is a runtime value. Concrete dynamic types are:
//
//	int64    for Int
//	string   for Text
//	bool     for boolean results inside expressions/vows
//	[]Value  for List<T>
type Value = any

// ---------------------------------------------------------------------------
// Execution context
// ---------------------------------------------------------------------------

// Context carries I/O for primitives that need it (input source, output sink),
// plus the named-value environment for Channels.
type Context struct {
	Stdin  io.Reader
	Stdout io.Writer
	// Stderr is the sink for `Reveal: stderr`. A mid-pipeline Reveal to
	// stderr is a debugging tool that does not disturb the program's answer —
	// or its golden test. Nil discards, exactly as a nil Stdout does: a
	// caller that captures only stdout must not find stderr output mixed into
	// it, or the two backends would disagree about what a program printed.
	Stderr   io.Writer
	BaseDir  string           // directory used to resolve relative input file paths
	Channels map[string]Value // values produced by Channel sub-pipelines
	// Release disables Binding Vows (they become passthroughs). It lives on
	// the Context rather than as a pipeline strip pass because vows nested
	// inside Channel and loop bodies are captured by their parents' Eval
	// closures, out of reach of node-list rewriting.
	Release bool
	// PartLabel is the label of the enclosing Part block, or "" at the top
	// level. A Part sets it around its body and restores it afterwards; Emit
	// reads it to prefix its output. It lives here for the same reason
	// Release does — the Emit node inside a Part body is reached through the
	// Part's Eval closure, so there is no node to rewrite.
	PartLabel string
	// Clock is how far the run has got: frames drawn, and milliseconds
	// elapsed. A host that has a clock maintains it; everything else leaves
	// it at zero, which is what `frame()` and `elapsed()` then report.
	Clock Clock
	// Requests, when set, is how a fired request reaches the host that will
	// send it. nil means nothing is listening — which is what a request under
	// a host with no network looks like, and is indistinguishable from a
	// server that never answers, so a program needs no special case for it.
	Requests func(RequestCall)
	// Pending answers `pending("tag")`: whether a request with that tag is
	// still out. A host that sends requests keeps it; everything else leaves
	// it nil, and nothing is ever pending.
	Pending func(tag string) bool
	// Rand is the run's random stream, or nil when nothing asked for one.
	// It is seeded once per run — from a replay script, a flag, or the clock —
	// so that a sequence is a pure function of a seed somebody could write
	// down. See ir/rand.go.
	Rand *Rand
	// Record, when set, is how a played game writes out what happened as it
	// happens — one replay-script line per call (frame, key, tick, and so on)
	// — so a session played once can be replayed exactly and diffed like any
	// other example. nil means nothing is listening, the same "an absent host
	// feature is indistinguishable from one nobody asked for" rule Requests
	// and Pending already follow.
	Record func(line string)
	// Load answers a `Domain Expansion: Load`: the raw JSON text a save file
	// held under path, and whether there was one. nil, or a false ok, both
	// mean "nothing was there" — a fresh game and a game with no save file
	// look the same, which is what lets Load's `Default:` cover both without
	// the primitive asking which one happened. A replayed run never reads a
	// real file: it is either nil (no `load` line in the script — Default
	// runs) or answers the one fixed value the script gave, so a save file
	// on the machine running the test can never change what the test sees.
	Load func(path string) (json string, ok bool)
	// Save is how a played game writes state that outlives the run — a high
	// score, a level unlocked. nil (a replayed run's constant state) means
	// the write is silently skipped, on the same "absent host feature" terms
	// as Requests: a replayed run must never touch a real file, or the same
	// script would stop giving the same answer on a second run.
	Save func(path, json string)
	// Quit is set by a scope whose programs can end themselves — a game
	// asking to stop. The host checks it after each body it runs and takes
	// it as the program saying it is finished.
	//
	// It lives here for the same reason Release and PartLabel do: the stage
	// that sets it is reached through a Part's Eval closure, so there is no
	// node for a host to inspect on the way past.
	Quit bool
	// Beep is set by a scope whose programs can ring the terminal bell — a
	// game marking a moment a frame cannot: a hit landing, a piece coming to
	// rest. Unlike Quit it is not checked after every body, only when the next
	// frame is drawn — a beep waits for a frame the way any other change to
	// the world does — and is cleared there, whether or not that frame was
	// the one asking for it.
	Beep bool
	// Trace, when set, observes every node evaluation — see trace.go. nil
	// means untraced, which is one nil check per node.
	Trace Tracer
	// frames is the stack of enclosing sub-pipeline labels, maintained only
	// while tracing.
	frames []string
}

// Clock is a run's progress: frames drawn and milliseconds elapsed.
//
// Both are counts a host keeps rather than readings it takes. That is what
// makes them replayable: a replayed run's clock advances because its script
// said `tick`, not because the machine was slow, so `elapsed()` gives the same
// answer every time the same script runs.
type Clock struct {
	frames    int64
	elapsedMS int64
}

// Advance records that time passed.
func (c *Clock) Advance(ms int64) { c.elapsedMS += ms }

// Drew records that a frame was drawn.
func (c *Clock) Drew() { c.frames++ }

// Frames and Elapsed are what the builtins report.
func (c Clock) Frames() int64  { return c.frames }
func (c Clock) Elapsed() int64 { return c.elapsedMS }

// Random is the run's stream, made on first use. A program that never asks
// for a random value never has one, and one that does gets a stream seeded
// from the context — so the seed is decided by whoever started the run rather
// than by whichever expression happened to draw first.
func (c *Context) Random() *Rand {
	if c.Rand == nil {
		c.Rand = NewRand(DefaultSeed)
	}
	return c.Rand
}

// LabelledOutput renders a value for Reveal under the given Part label. A
// single-line value goes on the label's own line; a multi-line one (a grid, a
// sparse picture) starts on the line after, so the picture stays readable and
// column-aligned. An empty label renders the value alone.
//
// Both backends must agree byte for byte, so this is the one implementation of
// the rule: the interpreter calls it and codegen emits the same branch.
func LabelledOutput(label, rendered string) string {
	if label == "" {
		return rendered
	}
	if strings.Contains(rendered, "\n") {
		return "Part " + label + ":\n" + rendered
	}
	return "Part " + label + ": " + rendered
}

// SetChannel stores a named channel value, lazily creating the map.
func (c *Context) SetChannel(name string, v Value) {
	if c.Channels == nil {
		c.Channels = map[string]Value{}
	}
	c.Channels[name] = v
}

// Channel retrieves a named channel value.
func (c *Context) Channel(name string) (Value, bool) {
	v, ok := c.Channels[name]
	return v, ok
}

// ---------------------------------------------------------------------------
// IR nodes
// ---------------------------------------------------------------------------

// Node is a resolved operation in the pipeline. Eval is the interpreter
// implementation, closing over the node's parsed arguments. Meta retains the
// structured arguments so the optimizer can pattern-match across nodes.
type Node struct {
	Prim      string // primitive id, e.g. "Split", "Sort", "SelectTopK", "PartialSelect"
	In        *Type  // expected input type (nil for a source node)
	Out       *Type  // produced output type
	Display   string // human-readable description (for --explain)
	Swappable bool   // true for Domain Expansion operations the optimizer may rewrite
	Meta      map[string]any
	Pos       token.Position
	Eval      func(ctx *Context, in Value) (Value, error)
}

// MeasureFn resolves a *measured* argument — one written as a lambda over the
// current value rather than as a literal (see prims/measure.go) — against the
// value flowing into its node, bound check included, so a caller gets the same
// number and the same error the primitive itself would.
//
// A node carrying one keeps it in Meta beside the lambda: the lambda is what
// the compiler compiles, and this is what the interpreter runs. It exists as a
// closure rather than as a call back into prims because the optimizer must be
// able to move a measured argument onto a fused node, and prims' own internal
// tests import the optimizer — so the dependency can only point one way.
type MeasureFn func(Value) (int64, error)

// MetaForeign marks a node whose Pos belongs to a source other than the
// program file — the embedded prelude, or an imported library. Inlining copies
// a Shikigami's body into the caller's pipeline carrying the *definition's*
// positions, and token.Position holds no file, so without this marker a tool
// that maps nodes back to source lines would point confidently at the wrong
// line of the user's program. The value names the source, for display.
const MetaForeign = "foreign"

// Foreign reports the source a node's position belongs to, when that source is
// not the program file. Anything resolving Pos against the user's source has to
// ask this first.
func (n *Node) Foreign() (string, bool) {
	if n == nil || n.Meta == nil {
		return "", false
	}
	s, ok := n.Meta[MetaForeign].(string)
	return s, ok && s != ""
}

// Pipeline is the linear chain of resolved nodes.
type Pipeline struct {
	Nodes []*Node
	// Scope is the name of the `Innate Domain` the program declared, or the
	// default one's name. It rides on the pipeline for the same reason
	// Globals does: resolving and running are not paired, and a run has to be
	// hosted by the scope of the program it is actually running rather than
	// by whatever was resolved most recently in this process.
	Scope string
	// Globals is how many `Cursed Object` slots the program declares, which is
	// the size of the array a run needs (eval.ResetGlobals).
	//
	// It rides on the pipeline rather than being set on the eval package when
	// the program is resolved, because resolving and running are not paired:
	// the language server and the REPL resolve one program while another is
	// still running, and their slot numbering has nothing to do with each
	// other. Tying the count to the pipeline means a run always sizes its
	// array from the program it is actually running.
	Globals int
}

// RuntimeError is an error raised while interpreting a node; it carries the
// pipeline stage so users can see where reality diverged.
type RuntimeError struct {
	Prim string
	Pos  token.Position
	Msg  string
}

// Error renders the failure as "position: message (in stage)".
//
// A message that runs to several lines — a foreign block reporting the
// traceback or compile error its runtime produced — keeps the stage tag on the
// *first* line, where it belongs, rather than letting it dangle after the last
// line of somebody else's output. Single-line messages, which is every other
// primitive, render exactly as they always did.
func (e *RuntimeError) Error() string {
	if head, rest, multiline := strings.Cut(e.Msg, "\n"); multiline {
		return fmt.Sprintf("%s: %s (in %s)\n%s", e.Pos, head, e.Prim, rest)
	}
	return fmt.Sprintf("%s: %s (in %s)", e.Pos, e.Msg, e.Prim)
}

// OwnedFields is the Meta key under which a loop node carries the state fields
// its body updates in place, as projection paths — "" for the whole state, "1"
// for item(s, 1), "1.0" for item(item(s, 1), 0).
//
// The optimizer's linear-accumulator pass writes it; both backends read it to
// decide what the copy on entry has to cover. It lives here rather than in
// either of them because it is a fact recorded on the node, and because prims
// cannot import the optimizer — the optimizer's own tests import prims.
//
// A node without the key has not been through the pass, and owning everything
// is the reading that is never wrong.
const OwnedFields = "inplace_owned_fields"

// OwnsEverything is the path naming the accumulator itself, which asks the
// backends to copy the whole state.
const OwnsEverything = ""
