package runner

// Which nodes actually ran, how often, and how big their values were.
//
// This is the reconnaissance `domain expansion: mahoraga` adapts to. Two
// questions come out of it, and they are the first two turns of the wheel:
//
//   - **What never ran?** A `Part` this input never enters, a loop body with
//     zero laps, a conditional arm never taken. The general optimizer cannot
//     cut those — it is not allowed to know anything about the input — but a
//     specializer adapting to one input can, and this is how it finds them.
//   - **How big was it?** An exact element count is what turns a growing
//     append into a single sized allocation, which bench/README.md measures as
//     the largest single win in the whole suite.
//
// It counts by node *identity*, not by primitive name: the question is which
// occurrence ran, and a program with three `Map Each` stages has three answers.
// (The coverage command's counter is by name, because its question is which
// vocabulary a folder exercises.)

import (
	"domain/ir"
	"domain/prims"
)

// NodeStat is one node's observed behaviour over a run.
type NodeStat struct {
	Calls int // evaluations, including inside loop and body frames

	// MaxOutSize is the largest output length observed, and Known says whether
	// any output had a length at all — a node producing scalars has no size,
	// which is different from producing an empty list.
	MaxOutSize int
	Known      bool

	// SizePreserving is true when every evaluation produced a value exactly as
	// long as the one it consumed, and Sized says the question had an answer at
	// all — both sides must have had a length for the comparison to mean
	// anything.
	//
	// This is the fact a `Filter` that kept every element is made of, and it is
	// the most exploitable thing in this whole file. A filter over two million
	// elements that discards none of them still evaluates its predicate two
	// million times and still copies the list; a search that has watched it do
	// nothing can cut it. The general optimizer cannot, because whether a
	// predicate ever fails is a property of the data.
	SizePreserving bool
	Sized          bool

	Failed bool // this node raised at least once
}

// NodeCounter is a Tracer that records per-node execution counts and sizes and
// keeps no values, so it can run over any input without bounding what it holds.
type NodeCounter struct {
	stats map[*ir.Node]*NodeStat
}

func NewNodeCounter() *NodeCounter {
	return &NodeCounter{stats: map[*ir.Node]*NodeStat{}}
}

// Step records one node evaluation.
func (c *NodeCounter) Step(ev ir.StepEvent) {
	if ev.Node == nil {
		return
	}
	st := c.stats[ev.Node]
	if st == nil {
		st = &NodeStat{}
		c.stats[ev.Node] = st
	}
	first := st.Calls == 0
	st.Calls++
	if ev.Err != nil {
		st.Failed = true
	}
	out, outOK := valueLen(ev.Out)
	if outOK {
		st.Known = true
		if out > st.MaxOutSize {
			st.MaxOutSize = out
		}
	}
	// Size preservation has to hold on *every* evaluation, so it starts true on
	// the first one and can only ever be falsified. A node inside a loop body is
	// evaluated once per lap, and a filter that discarded one element on one lap
	// out of four hundred is not a filter anything may cut.
	in, inOK := valueLen(ev.In)
	switch {
	case !inOK || !outOK:
		st.Sized, st.SizePreserving = false, false
	case first:
		st.Sized, st.SizePreserving = true, in == out
	case in != out:
		st.SizePreserving = false
	}
}

// PushFrame and PopFrame complete the Tracer interface. Frames are not counted
// here: a frame is a label around a sub-pipeline, and the nodes inside it
// report themselves.
func (c *NodeCounter) PushFrame(string, *ir.Type) {}
func (c *NodeCounter) PopFrame(ir.Value)          {}

// Stat is what was observed for one node; the zero value means it never ran.
func (c *NodeCounter) Stat(n *ir.Node) NodeStat {
	if st := c.stats[n]; st != nil {
		return *st
	}
	return NodeStat{}
}

// Calls is how many times a node evaluated.
func (c *NodeCounter) Calls(n *ir.Node) int { return c.Stat(n).Calls }

// Ran reports whether a node evaluated at all.
func (c *NodeCounter) Ran(n *ir.Node) bool { return c.Calls(n) > 0 }

// NeverRan lists every node in the pipeline that did not evaluate once, in
// pipeline order — the raw material for cutting what this input never reaches.
//
// It takes the pipeline rather than reporting from the recording alone,
// because "never ran" is a statement about nodes that produced *no* events,
// and only the pipeline knows they exist.
//
// One caveat the caller must respect: this is what a *particular* run reached.
// It is evidence about this input and nothing more, which is exactly the kind
// of evidence mahoraga is allowed to act on and the optimizer is not.
func (c *NodeCounter) NeverRan(p *ir.Pipeline) []*ir.Node {
	var out []*ir.Node
	prims.WalkNodes(p, func(n *ir.Node) {
		if !c.Ran(n) {
			out = append(out, n)
		}
	})
	return out
}

// Hot returns the nodes that ran, ordered by call count, most first. Ties keep
// pipeline order so the result is deterministic.
func (c *NodeCounter) Hot(p *ir.Pipeline) []*ir.Node {
	var out []*ir.Node
	prims.WalkNodes(p, func(n *ir.Node) {
		if c.Ran(n) {
			out = append(out, n)
		}
	})
	// Insertion sort by descending calls, stable, so equal counts stay in
	// pipeline order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && c.Calls(out[j]) > c.Calls(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Total is how many node evaluations happened altogether.
func (c *NodeCounter) Total() int {
	n := 0
	for _, st := range c.stats {
		n += st.Calls
	}
	return n
}

// valueLen is the length of a value that has one. It deliberately covers only
// the shapes whose size a specialization can act on — a list to preallocate, a
// text to size a buffer for, a collection whose element count is the thing
// worth knowing.
func valueLen(v ir.Value) (int, bool) {
	switch x := v.(type) {
	case []ir.Value:
		return len(x), true
	case string:
		return len(x), true
	case *ir.MapValue:
		return x.Len(), true
	case *ir.SetValue:
		return x.Len(), true
	case *ir.GridValue:
		return len(x.Cells), true
	}
	return 0, false
}
