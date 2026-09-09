package ir

// The zero value of a type.
//
// It exists for one reason: a request that fails still has to hand a program
// something of the type it declared. `Part Reply` is typed against
// `{ok: Bool, error: Text, value: T}`, and when `ok` is false there is no T to
// put in it — the server was down, the body did not decode, the request timed
// out. Refusing to have a value there would mean either a second shape for
// failures or an optional type, and this language has neither.
//
// So a failed reply carries the zero of T: an Int of 0, an empty list, a
// record of zeroes. A program branches on `ok` and never reads `value` unless
// it is true; a program that reads it anyway gets something harmless rather
// than a crash.
//
// codegen mirrors this exactly. A zero that differed between the backends
// would only show up while the network was already failing, which is the worst
// moment to discover a divergence.

// ZeroValue is the value a type takes when there is nothing to put there.
func ZeroValue(t *Type) Value {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case KInt:
		return int64(0)
	case KFloat:
		return float64(0)
	case KText:
		return ""
	case KBool:
		return false
	case KList:
		return []Value{}
	case KTuple:
		out := make([]Value, len(t.Elems))
		for i, e := range t.Elems {
			out[i] = ZeroValue(e)
		}
		return out
	case KRecord:
		r := NewRecordValueSized(len(t.Fields))
		for _, f := range t.Fields {
			r.Set(f.Name, ZeroValue(f.Type))
		}
		return r
	case KMap:
		return NewMapValue()
	case KSet:
		return NewSetValue()
	case KGrid:
		// An empty grid rather than a one-cell one: a picture of nothing is
		// nothing, and every grid operation copes with no rows.
		return &GridValue{}
	case KSparse:
		// A sparse plane needs a default, and the element's zero is the only
		// one available without asking the program.
		return NewSparseValue(ZeroValue(t.Elem))
	case KGraph:
		return NewGraphValue()
	case KView:
		return BlankView()
	}
	return nil
}
