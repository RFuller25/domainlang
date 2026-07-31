package ir

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
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
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return FormatFloat(x)
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case []Value:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = FormatValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *RecordValue:
		parts := make([]string, len(x.Fields))
		for i, name := range x.Fields {
			parts[i] = name + ": " + FormatValue(x.Vals[name])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *MapValue:
		parts := make([]string, 0, x.Len())
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			parts = append(parts, FormatValue(k)+": "+FormatValue(val))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *SetValue:
		parts := make([]string, 0, x.Len())
		for _, e := range x.Elems() {
			parts = append(parts, FormatValue(e))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *GridValue:
		return formatGrid(x)
	case *SparseValue:
		// Map-style listing of the set cells, sorted row-major, keys rendered
		// as points ([r, c]) — exactly how a Map<(Int, Int), V> renders. The
		// picture form is one `Convert To Grid` away; keeping the raw render
		// size-proportional means Reveal can never materialize a huge box.
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
			sb.WriteString(FormatValue(x.At(p[0], p[1])))
		}
		sb.WriteByte('}')
		return sb.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatGrid(g *GridValue) string {
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
			sb.WriteString(FormatValue(cell))
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
