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
	case KList, KSet, KGrid, KSparse:
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
	// Trace, when set, observes every node evaluation — see trace.go. nil
	// means untraced, which is one nil check per node.
	Trace Tracer
	// frames is the stack of enclosing sub-pipeline labels, maintained only
	// while tracing.
	frames []string
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
