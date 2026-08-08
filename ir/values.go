package ir

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AsList asserts that v is a list, returning a helpful error otherwise.
func AsList(v Value) ([]Value, error) {
	if l, ok := v.([]Value); ok {
		return l, nil
	}
	// A Set reads as its elements, in insertion order — the order it already
	// renders and iterates in. This is what lets the list primitives consume a
	// Set directly instead of leaving Convert To Set a dead end whose only
	// remaining moves were Count, contains and Difference. Tuples are []Value
	// already, so they need no case.
	if s, ok := v.(*SetValue); ok {
		return s.Elems(), nil
	}
	// A Map reads as its entries, in insertion order — the same shape and
	// order Convert To Entries produces, so a Group By or Count By flows
	// straight into the list vocabulary instead of detouring through it.
	if m, ok := v.(*MapValue); ok {
		out := make([]Value, 0, m.Len())
		for _, k := range m.Keys() {
			val, _ := m.Get(k)
			out = append(out, []Value{k, val})
		}
		return out, nil
	}
	return nil, fmt.Errorf("expected a list, got %s", DescribeValue(v))
}

// AsInt asserts that v is an integer.
func AsInt(v Value) (int64, error) {
	n, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("expected an integer, got %s", DescribeValue(v))
	}
	return n, nil
}

// AsIntSlice converts a list of integers to a []int64.
func AsIntSlice(v Value) ([]int64, error) {
	l, err := AsList(v)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(l))
	for i, e := range l {
		n, err := AsInt(e)
		if err != nil {
			return nil, fmt.Errorf("list element %d: %w", i, err)
		}
		out[i] = n
	}
	return out, nil
}

// IntsToValue wraps a []int64 as a list Value.
func IntsToValue(xs []int64) []Value {
	out := make([]Value, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// AsFloat asserts that v is a Float, widening an Int — the numeric tower's
// only implicit conversion, mirroring the typechecker's promotion rule.
func AsFloat(v Value) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case int64:
		return float64(x), nil
	}
	return 0, fmt.Errorf("expected a number, got %s", DescribeValue(v))
}

// AsFloatSlice converts a list of floats (or ints, widened) to a []float64.
func AsFloatSlice(v Value) ([]float64, error) {
	l, err := AsList(v)
	if err != nil {
		return nil, err
	}
	out := make([]float64, len(l))
	for i, e := range l {
		f, err := AsFloat(e)
		if err != nil {
			return nil, fmt.Errorf("list element %d: %w", i, err)
		}
		out[i] = f
	}
	return out, nil
}

// FloatsToValue wraps a []float64 as a list Value.
func FloatsToValue(xs []float64) []Value {
	out := make([]Value, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// FormatFloat renders a Float exactly as both backends print it: shortest
// round-trip 'g' form. Keep codegen's declFmtFloat in sync with this.
func FormatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// FormatValue renders a value in full for the Reveal sink. It is the single
// long-form renderer, shared by the Emit primitive.
func FormatValue(v Value) string {
	// A scalar is its own rendering, and scalars are most of what Reveal
	// prints. They are answered here rather than through the writer because a
	// Text would otherwise be copied into a builder and back out again — an
	// allocation and a copy per line of output, for a value that was already
	// the answer.
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return FormatFloat(x)
	case bool:
		return strconv.FormatBool(x)
	}
	s, _ := FormatValueLimit(v, 0)
	return s
}

// FormatValueLimit renders a value like FormatValue but stops once the
// rendering reaches limit bytes, reporting whether it got to the end. A limit
// of 0 means no limit, which is exactly what FormatValue asks for.
//
// It exists for a caller that is going to throw most of the answer away. The
// visualizer's recorder keeps at most 64 KiB of any one value, and building the
// whole rendering first — tens of megabytes for a list a program is midway
// through building, once per step — is work spent to produce a string that is
// immediately sliced. Stopping at the limit makes the cost proportional to what
// is kept rather than to what the program is holding.
//
// The prefix returned is a real prefix of the full rendering, cut on a rune
// boundary, so it is safe to display: it will be missing its closing bracket,
// which is what `complete` is for.
func FormatValueLimit(v Value, limit int) (s string, complete bool) {
	w := valueWriter{limit: limit}
	writeValue(&w, v)
	return w.b.String(), !w.over
}

// valueWriter accumulates a rendering, refusing to grow past its limit. Every
// writer checks over on the way in, so a value the limit was reached inside of
// stops being walked rather than being walked and discarded.
type valueWriter struct {
	b     strings.Builder
	limit int // 0 means no limit
	over  bool
}

func (w *valueWriter) WriteString(s string) {
	if w.over {
		return
	}
	if w.limit > 0 && w.b.Len()+len(s) > w.limit {
		// Partial: take what fits, back to a rune boundary so the prefix is
		// still valid UTF-8 and a terminal does not paint a replacement
		// character at the cut.
		w.b.WriteString(truncateRunes(s, w.limit-w.b.Len()))
		w.over = true
		return
	}
	w.b.WriteString(s)
}

// writeByte appends one ASCII byte. It is deliberately not spelled WriteByte:
// that name carries io.ByteWriter's error-returning signature, and a bounded
// writer that silently drops past its limit is not that interface.
func (w *valueWriter) writeByte(c byte) { w.WriteString(string(c)) }

// truncateRunes returns the longest prefix of s that is at most n bytes and
// ends on a rune boundary.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// writeValue is the one untyped long-form renderer: FormatValue and
// FormatValueLimit are both this function, and so cannot disagree about what a
// value looks like. (FormatValueTyped is deliberately separate — it renders
// records in their declared field order, for the reason documented there.)
func writeValue(w *valueWriter, v Value) {
	if w.over {
		return
	}
	switch x := v.(type) {
	case int64:
		w.WriteString(strconv.FormatInt(x, 10))
	case float64:
		w.WriteString(FormatFloat(x))
	case string:
		w.WriteString(x)
	case bool:
		w.WriteString(strconv.FormatBool(x))
	case []Value:
		w.writeByte('[')
		for i, e := range x {
			if w.over {
				return
			}
			if i > 0 {
				w.WriteString(", ")
			}
			writeValue(w, e)
		}
		w.writeByte(']')
	case *RecordValue:
		w.writeByte('{')
		for i, name := range x.Fields {
			if w.over {
				return
			}
			if i > 0 {
				w.WriteString(", ")
			}
			w.WriteString(name)
			w.WriteString(": ")
			writeValue(w, x.Vals[name])
		}
		w.writeByte('}')
	case *MapValue:
		w.writeByte('{')
		for i, k := range x.Keys() {
			if w.over {
				return
			}
			if i > 0 {
				w.WriteString(", ")
			}
			val, _ := x.Get(k)
			writeValue(w, k)
			w.WriteString(": ")
			writeValue(w, val)
		}
		w.writeByte('}')
	case *SetValue:
		w.writeByte('{')
		for i, e := range x.Elems() {
			if w.over {
				return
			}
			if i > 0 {
				w.WriteString(", ")
			}
			writeValue(w, e)
		}
		w.writeByte('}')
	case *GridValue:
		writeGrid(w, x)
	case *SparseValue:
		// Map-style listing of the set cells, sorted row-major, keys rendered
		// as points ([r, c]) — exactly how a Map<(Int, Int), V> renders. The
		// picture form is one `Convert To Grid` away; keeping the raw render
		// size-proportional means Reveal can never materialize a huge box.
		w.writeByte('{')
		for i, p := range x.Points() {
			if w.over {
				return
			}
			if i > 0 {
				w.WriteString(", ")
			}
			w.writeByte('[')
			w.WriteString(strconv.FormatInt(p[0], 10))
			w.WriteString(", ")
			w.WriteString(strconv.FormatInt(p[1], 10))
			w.WriteString("]: ")
			writeValue(w, x.At(p[0], p[1]))
		}
		w.writeByte('}')
	default:
		w.WriteString(fmt.Sprintf("%v", v))
	}
}

// writeGrid is formatGridTyped's untyped half, written into the bounded writer
// so a million-cell grid stops at the limit rather than after the last cell.
func writeGrid(w *valueWriter, g *GridValue) {
	for r := range g.Rows {
		if w.over {
			return
		}
		if r > 0 {
			w.writeByte('\n')
		}
		for c := range g.Cols {
			if w.over {
				return
			}
			cell := g.Cells[r*g.Cols+c]
			if c > 0 {
				if _, isStr := cell.(string); !isStr {
					w.writeByte(' ')
				}
			}
			writeValue(w, cell)
		}
	}
}

// FormatValueTyped renders a value for the Reveal sink in the field order its
// static type declares, and is otherwise FormatValue exactly.
//
// The two differ for records and only for records. A RecordValue carries the
// order it was *built* in, which is the order the source wrote; a type carries
// the order it *declares*. Those are the same thing until two record types with
// the same fields in different orders meet — an `if` whose arms build the
// fields in different orders, a list holding both — at which point one value in
// the list renders `{b: …, a: …}` and its neighbour `{a: …, b: …}`.
//
// The compiled backend cannot reproduce that: a record is an unboxed struct
// with one field order, so it renders every value of a type the same way. Since
// stdout has to match byte for byte, the type is what both backends read the
// order from, and this is the interpreter's side of that. No program whose
// records all declare their fields in one order is affected — for those, the
// value's order *is* the type's.
//
// A type that does not describe the value (nil, or a shape mismatch that only a
// bug could produce) falls back to FormatValue rather than guessing.
func FormatValueTyped(v Value, t *Type) string {
	if t == nil {
		return FormatValue(v)
	}
	switch x := v.(type) {
	case []Value:
		switch t.Kind {
		case KList:
			parts := make([]string, len(x))
			for i, e := range x {
				parts[i] = FormatValueTyped(e, t.Elem)
			}
			return "[" + strings.Join(parts, ", ") + "]"
		case KTuple:
			if len(t.Elems) != len(x) {
				break
			}
			parts := make([]string, len(x))
			for i, e := range x {
				parts[i] = FormatValueTyped(e, t.Elems[i])
			}
			return "[" + strings.Join(parts, ", ") + "]"
		}
	case *RecordValue:
		if t.Kind != KRecord || len(t.Fields) != len(x.Fields) {
			break
		}
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			fv, ok := x.Vals[f.Name]
			if !ok {
				return FormatValue(v)
			}
			parts[i] = f.Name + ": " + FormatValueTyped(fv, f.Type)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *MapValue:
		if t.Kind != KMap {
			break
		}
		parts := make([]string, 0, x.Len())
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			parts = append(parts, FormatValueTyped(k, t.Key)+": "+FormatValueTyped(val, t.Elem))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *SetValue:
		if t.Kind != KSet {
			break
		}
		parts := make([]string, 0, x.Len())
		for _, e := range x.Elems() {
			parts = append(parts, FormatValueTyped(e, t.Elem))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *GridValue:
		if t.Kind != KGrid {
			break
		}
		return formatGridTyped(x, t.Elem)
	case *SparseValue:
		if t.Kind != KSparse {
			break
		}
		var sb strings.Builder
		sb.WriteByte('{')
		for i, p := range x.Points() {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteByte('[')
			sb.WriteString(strconv.FormatInt(p[0], 10))
			sb.WriteString(", ")
			sb.WriteString(strconv.FormatInt(p[1], 10))
			sb.WriteString("]: ")
			sb.WriteString(FormatValueTyped(x.At(p[0], p[1]), t.Elem))
		}
		sb.WriteByte('}')
		return sb.String()
	}
	return FormatValue(v)
}

// formatGridTyped is the typed grid renderer. Its untyped counterpart is
// writeGrid, which goes through the bounded writer; the two are kept in step by
// TestWriteGridMatchesFormatGridTyped.
func formatGridTyped(g *GridValue, cellType *Type) string {
	rows := make([]string, g.Rows)
	for r := range g.Rows {
		var sb strings.Builder
		for c := range g.Cols {
			cell := g.Cells[r*g.Cols+c]
			if c > 0 {
				if _, isStr := cell.(string); !isStr {
					sb.WriteByte(' ')
				}
			}
			sb.WriteString(FormatValueTyped(cell, cellType))
		}
		rows[r] = sb.String()
	}
	return strings.Join(rows, "\n")
}

// FormatShort renders a value compactly for error messages, truncating long
// collections so a vow violation stays readable.
func FormatShort(v Value) string {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return FormatFloat(x)
	case string:
		if len(x) > 40 {
			return strconv.Quote(x[:40]) + "…"
		}
		return strconv.Quote(x)
	case bool:
		return strconv.FormatBool(x)
	case []Value:
		return shortSeq("[", "]", len(x), func(i int) string { return FormatShort(x[i]) })
	case *RecordValue:
		return shortSeq("{", "}", len(x.Fields), func(i int) string {
			name := x.Fields[i]
			return name + ": " + FormatShort(x.Vals[name])
		})
	case *MapValue:
		keys := x.Keys()
		return shortSeq("{", "}", len(keys), func(i int) string {
			val, _ := x.Get(keys[i])
			return FormatShort(keys[i]) + ": " + FormatShort(val)
		})
	case *SetValue:
		elems := x.Elems()
		return shortSeq("{", "}", len(elems), func(i int) string { return FormatShort(elems[i]) })
	case *GridValue:
		return fmt.Sprintf("Grid %dx%d", x.Rows, x.Cols)
	case *SparseValue:
		minR, minC, maxR, maxC, ok := x.Bounds()
		if !ok {
			return "Sparse 0x0 (0 set)"
		}
		return fmt.Sprintf("Sparse %dx%d (%d set)", maxR-minR+1, maxC-minC+1, x.Len())
	default:
		return fmt.Sprintf("%v", v)
	}
}

// shortSeq renders up to 8 entries of a sequence, eliding the rest.
func shortSeq(open, close string, n int, at func(i int) string) string {
	const maxShown = 8
	shown := min(n, maxShown)
	parts := make([]string, shown)
	for i := range shown {
		parts[i] = at(i)
	}
	s := open + strings.Join(parts, ", ")
	if n > shown {
		s += fmt.Sprintf(", …(%d more)", n-shown)
	}
	return s + close
}

// compositeKey is the canonical comparable key for tuple/record values. It is
// a distinct type so an encoded composite can never collide with a user Text
// key that happens to contain the same characters.
type compositeKey string

// KeyOf returns a comparable key for v that agrees with DeepEqual on keyable
// values: two values produce the same key exactly when DeepEqual reports them
// equal. Scalars are their own keys (distinct dynamic types cannot collide);
// tuples and records encode to a canonical length-prefixed string wrapped in
// compositeKey. This is what lets MapValue/SetValue key on points and other
// composites without changing behavior or cost for scalar keys.
func KeyOf(v Value) any {
	switch v.(type) {
	case []Value, *RecordValue:
		var sb strings.Builder
		encodeKey(&sb, v)
		return compositeKey(sb.String())
	default:
		return v
	}
}

// encodeKey writes an injective encoding of v: every variable-length part is
// length-prefixed, so no arrangement of nested values can collide with
// another. Record fields are encoded in sorted-name order because DeepEqual
// compares records by field name, not declaration order.
func encodeKey(sb *strings.Builder, v Value) {
	switch x := v.(type) {
	case int64:
		fmt.Fprintf(sb, "i%d;", x)
	case string:
		fmt.Fprintf(sb, "t%d:%s;", len(x), x)
	case bool:
		if x {
			sb.WriteString("b1;")
		} else {
			sb.WriteString("b0;")
		}
	case []Value:
		fmt.Fprintf(sb, "l%d:", len(x))
		for _, e := range x {
			encodeKey(sb, e)
		}
		sb.WriteByte(';')
	case *RecordValue:
		names := slices.Clone(x.Fields)
		slices.Sort(names)
		fmt.Fprintf(sb, "r%d:", len(names))
		for _, name := range names {
			fmt.Fprintf(sb, "t%d:%s;", len(name), name)
			encodeKey(sb, x.Vals[name])
		}
		sb.WriteByte(';')
	default:
		// Non-keyable kinds cannot reach Map/Set keys in a typechecked
		// program; render defensively rather than panic.
		s := FormatValue(v)
		fmt.Fprintf(sb, "x%d:%s;", len(s), s)
	}
}

// DeepEqual reports whether two runtime values are structurally equal. Used by
// the Iterate Until Fixed Point loop to detect convergence.
func DeepEqual(a, b Value) bool {
	switch x := a.(type) {
	case int64:
		y, ok := b.(int64)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case []Value:
		y, ok := b.([]Value)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !DeepEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case *RecordValue:
		y, ok := b.(*RecordValue)
		if !ok || len(x.Fields) != len(y.Fields) {
			return false
		}
		for _, f := range x.Fields {
			xv, _ := x.Get(f)
			yv, ok := y.Get(f)
			if !ok || !DeepEqual(xv, yv) {
				return false
			}
		}
		return true
	case *MapValue:
		y, ok := b.(*MapValue)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.Keys() {
			xv, _ := x.Get(k)
			yv, ok := y.Get(k)
			if !ok || !DeepEqual(xv, yv) {
				return false
			}
		}
		return true
	case *SetValue:
		y, ok := b.(*SetValue)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, e := range x.Elems() {
			if !y.Has(e) {
				return false
			}
		}
		return true
	case *GridValue:
		y, ok := b.(*GridValue)
		if !ok || x.Rows != y.Rows || x.Cols != y.Cols {
			return false
		}
		for i := range x.Cells {
			if !DeepEqual(x.Cells[i], y.Cells[i]) {
				return false
			}
		}
		return true
	case *SparseValue:
		y, ok := b.(*SparseValue)
		if !ok || !DeepEqual(x.Def, y.Def) || x.Len() != y.Len() {
			return false
		}
		for k, v := range x.cells {
			yv, ok := y.cells[k]
			if !ok || !DeepEqual(v, yv) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	default:
		return false
	}
}

// DescribeValue gives a short human description of a value's dynamic type.
func DescribeValue(v Value) string {
	switch v.(type) {
	case int64:
		return "Int"
	case float64:
		return "Float"
	case string:
		return "Text"
	case bool:
		return "Bool"
	case []Value:
		return "List"
	case *RecordValue:
		return "Record"
	case *MapValue:
		return "Map"
	case *SetValue:
		return "Set"
	case *GridValue:
		return "Grid"
	case *SparseValue:
		return "Sparse"
	case nil:
		return "<none>"
	default:
		return fmt.Sprintf("%T", v)
	}
}
