package ir

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// JSON, in and out.
//
// It exists because multiplayer does. A game talking to a server sends and
// receives JSON, and Domain is statically typed, so the two directions are not
// symmetrical:
//
//   - **Out** is a function of the value. A Record is an object, a List is an
//     array, and nothing has to be declared.
//   - **In** needs a *declared type* to decode into. There is no dynamic value
//     in this language to decode onto and then inspect, which is the whole
//     reason `Convert From JSON` takes a written type and `Request` takes an
//     `Into:`.
//
// Decoding is therefore strict in the direction that matters: a field the type
// declares and the document lacks is an error naming the field, because a
// silently-zero score is worse than a refusal. A field the *document* has and
// the type does not is ignored — a server may send more than a client asked
// for, and a client that broke when it did would break on every deployment.

// ToJSON renders a value as JSON.
func ToJSON(v Value) (string, error) {
	var sb strings.Builder
	if err := writeJSON(&sb, v); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func writeJSON(sb *strings.Builder, v Value) error {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(x))
	case int64:
		sb.WriteString(strconv.FormatInt(x, 10))
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return fmt.Errorf("%s has no JSON form", FormatFloat(x))
		}
		sb.WriteString(FormatFloat(x))
	case string:
		writeJSONString(sb, x)
	case []Value:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := writeJSON(sb, e); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case *RecordValue:
		sb.WriteByte('{')
		for i, name := range x.Fields {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONString(sb, name)
			sb.WriteByte(':')
			val, _ := x.Get(name)
			if err := writeJSON(sb, val); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	case *MapValue:
		// A Map is an object, which means its keys must be strings — so an
		// Int-keyed map is written with its keys rendered, the way it already
		// prints. Sorted, because a JSON object a server compares has to be
		// the same bytes for the same value.
		keys := x.Keys()
		rendered := make([]string, 0, len(keys))
		byKey := map[string]Value{}
		for _, k := range keys {
			s := FormatValue(k)
			rendered = append(rendered, s)
			byKey[s], _ = x.Get(k)
		}
		sort.Strings(rendered)
		sb.WriteByte('{')
		for i, k := range rendered {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONString(sb, k)
			sb.WriteByte(':')
			if err := writeJSON(sb, byKey[k]); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	case *SetValue:
		return writeJSON(sb, Value(x.Elems()))
	default:
		return fmt.Errorf("%s has no JSON form", DescribeValue(v))
	}
	return nil
}

func writeJSONString(sb *strings.Builder, s string) {
	b, _ := json.Marshal(s)
	sb.Write(b)
}

// FromJSON decodes a document into the declared type.
//
// The type leads: what the document says is read against what the program
// asked for, rather than the other way round. That is what makes the result
// usable without inspection — a program that declared `{score: Int}` has an
// Int, or an error saying why it does not.
func FromJSON(text string, t *Type) (Value, error) {
	var raw any
	dec := json.NewDecoder(strings.NewReader(text))
	// Numbers stay as written until the target type says what they are, so a
	// big Int does not lose its low bits by passing through a float64 on the
	// way to being an Int.
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("not valid JSON: %v", err)
	}
	return convertJSON(raw, t, "")
}

func convertJSON(raw any, t *Type, path string) (Value, error) {
	where := func() string {
		if path == "" {
			return ""
		}
		return " at " + path
	}
	bad := func(want string) error {
		return fmt.Errorf("expected %s%s, got %s", want, where(), jsonKind(raw))
	}
	if t == nil {
		return nil, fmt.Errorf("no type to decode into%s", where())
	}
	switch t.Kind {
	case KInt:
		n, ok := raw.(json.Number)
		if !ok {
			return nil, bad("a whole number")
		}
		i, err := n.Int64()
		if err != nil {
			return nil, fmt.Errorf("%s is not a whole number%s", n.String(), where())
		}
		return i, nil
	case KFloat:
		n, ok := raw.(json.Number)
		if !ok {
			return nil, bad("a number")
		}
		f, err := n.Float64()
		if err != nil {
			return nil, fmt.Errorf("%s is not a number%s", n.String(), where())
		}
		return f, nil
	case KText:
		s, ok := raw.(string)
		if !ok {
			return nil, bad("some text")
		}
		return s, nil
	case KBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, bad("true or false")
		}
		return b, nil
	case KList:
		arr, ok := raw.([]any)
		if !ok {
			return nil, bad("a list")
		}
		out := make([]Value, len(arr))
		for i, e := range arr {
			v, err := convertJSON(e, t.Elem, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case KTuple:
		arr, ok := raw.([]any)
		if !ok {
			return nil, bad("a list")
		}
		if len(arr) != len(t.Elems) {
			return nil, fmt.Errorf("expected %d values%s, got %d", len(t.Elems), where(), len(arr))
		}
		out := make([]Value, len(arr))
		for i, e := range arr {
			v, err := convertJSON(e, t.Elems[i], fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case KRecord:
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, bad("an object")
		}
		r := NewRecordValueSized(len(t.Fields))
		for _, f := range t.Fields {
			e, present := obj[f.Name]
			if !present {
				// Strict in this direction on purpose: a field the program
				// declared and the document lacks is an error naming it,
				// because a silently-zero score is worse than a refusal.
				return nil, fmt.Errorf("no %q%s", f.Name, where())
			}
			v, err := convertJSON(e, f.Type, path+"."+f.Name)
			if err != nil {
				return nil, err
			}
			r.Set(f.Name, v)
		}
		// Extra fields are ignored: a server may send more than a client asked
		// for, and a client that broke when it did would break on every
		// deployment.
		return r, nil
	case KMap:
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, bad("an object")
		}
		m := NewMapValue()
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		// Sorted, so that decoding one document twice gives one insertion
		// order — which is the order a Map renders and iterates in.
		sort.Strings(keys)
		for _, k := range keys {
			var key Value = k
			if t.Key != nil && t.Key.Kind == KInt {
				n, err := strconv.ParseInt(k, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("the key %q%s is not a whole number", k, where())
				}
				key = n
			}
			v, err := convertJSON(obj[k], t.Elem, path+"."+k)
			if err != nil {
				return nil, err
			}
			m.Put(key, v)
		}
		return m, nil
	case KSet:
		arr, ok := raw.([]any)
		if !ok {
			return nil, bad("a list")
		}
		s := NewSetValue()
		for i, e := range arr {
			v, err := convertJSON(e, t.Elem, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			s.Add(v)
		}
		return s, nil
	}
	return nil, fmt.Errorf("%s cannot be decoded from JSON%s", t, where())
}

// jsonKind names what a decoded document actually held, for an error that has
// to say what was there instead.
func jsonKind(raw any) string {
	switch raw.(type) {
	case nil:
		return "null"
	case bool:
		return "true or false"
	case json.Number:
		return "a number"
	case string:
		return "some text"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}
	return "something else"
}
