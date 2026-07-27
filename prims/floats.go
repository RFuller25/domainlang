// Float support: the Convert To Floats channel primitive
// and the Float paths of the numeric reductions. Floats deliberately stay out
// of the keyable world (Unique/Group By/Sets reject them with the existing
// keyability errors) and out of the optimizer's reordering passes — float
// addition is not associative and NaN is unordered, so float pipelines keep
// their written order (see optimizer type guards).
package prims

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// buildFloatSort is the List<Float> arm of the Sort primitive. It shares the
// "Sort" Prim so the type-guarded optimizer passes can recognize (and skip)
// it via In/Out types; Meta records the element kind for codegen.
func buildFloatSort(op *ast.Operation, pos token.Position) *ir.Node {
	desc := hasModifier(op, "Descending")
	order := "Ascending"
	if desc {
		order = "Descending"
	}
	want := ir.List(ir.Float())
	return &ir.Node{
		Prim:      "Sort",
		In:        want,
		Out:       want,
		Display:   "Quicksort (Float), " + order,
		Swappable: true,
		Meta:      map[string]any{"desc": desc, "elem": "Float"},
		Pos:       pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsFloatSlice(v)
			if err != nil {
				return nil, runtimeErr("Sort", pos, "%v", err)
			}
			out := append([]float64(nil), xs...)
			if desc {
				sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
			} else {
				sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
			}
			return ir.FloatsToValue(out), nil
		},
	}
}

// buildOrderedSort is the Sort arm for a non-Int ordered element type — Text
// (alphabetical) and tuples (lexicographic). It shares the "Sort" Prim so the
// type-guarded optimizer passes recognize it, and records the element kind in
// Meta so codegen can pick a comparison.
func buildOrderedSort(op *ast.Operation, in *ir.Type, pos token.Position) *ir.Node {
	desc := hasModifier(op, "Descending")
	order := "Ascending"
	if desc {
		order = "Descending"
	}
	return &ir.Node{
		Prim:      "Sort",
		In:        in,
		Out:       in,
		Display:   "Quicksort (" + in.Elem.String() + "), " + order,
		Swappable: true,
		Meta:      map[string]any{"desc": desc, "elem": in.Elem.String(), "ordered": true},
		Pos:       pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsList(v)
			if err != nil {
				return nil, runtimeErr("Sort", pos, "%v", err)
			}
			out := append([]ir.Value(nil), xs...)
			sort.SliceStable(out, func(i, j int) bool {
				c := ir.Compare(out[i], out[j])
				if desc {
					return c > 0
				}
				return c < 0
			})
			return out, nil
		},
	}
}

// buildFloatReduce is the List<Float> arm of Max/Min/Product.
func buildFloatReduce(id string, pos token.Position) *ir.Node {
	want := ir.List(ir.Float())
	return &ir.Node{
		Prim:    id,
		In:      want,
		Out:     ir.Float(),
		Display: id + " (Float)",
		Pos:     pos,
		Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
			xs, err := ir.AsFloatSlice(v)
			if err != nil {
				return nil, runtimeErr(id, pos, "%v", err)
			}
			if len(xs) == 0 {
				return nil, runtimeErr(id, pos, "%s of an empty list is undefined", id)
			}
			acc := xs[0]
			for _, x := range xs[1:] {
				switch id {
				case "Max":
					if x > acc {
						acc = x
					}
				case "Min":
					if x < acc {
						acc = x
					}
				case "Product":
					acc *= x
				}
			}
			return acc, nil
		},
	}
}

// Channeled Energy: Convert To Floats — List<Text> or List<Int> -> List<Float>
// (and the Each-List nested forms, mirroring Convert To Integers).
var convertToFloats = &Primitive{
	ID:      "Convert To Floats",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Convert") && hasWord(op, "Floats") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		conv := func(kind string, items []ir.Value, where string) ([]ir.Value, error) {
			out := make([]ir.Value, len(items))
			for i, e := range items {
				f, err := parseFloatValue(e)
				if err != nil {
					return nil, runtimeErr("Convert To Floats", pos, "%sitem %d: %v", where, i, err)
				}
				out[i] = f
			}
			_ = kind
			return out, nil
		}
		flatIn := func(elem *ir.Type) bool {
			return in != nil && in.Equal(ir.List(elem))
		}
		nestedIn := func(elem *ir.Type) bool {
			return in != nil && in.Equal(ir.List(ir.List(elem)))
		}
		switch {
		case flatIn(ir.Text()), flatIn(ir.Int()):
			return &ir.Node{
				Prim:    "Convert To Floats",
				In:      in,
				Out:     ir.List(ir.Float()),
				Display: "Convert List to Floats",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					items, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Convert To Floats", pos, "%v", err)
					}
					return conv("flat", items, "")
				},
			}, nil
		case nestedIn(ir.Text()), nestedIn(ir.Int()):
			return &ir.Node{
				Prim:    "Convert To Floats",
				In:      in,
				Out:     ir.List(ir.List(ir.Float())),
				Display: "Convert Each List to Floats",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					groups, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Convert To Floats", pos, "%v", err)
					}
					out := make([]ir.Value, len(groups))
					for i, g := range groups {
						inner, err := ir.AsList(g)
						if err != nil {
							return nil, runtimeErr("Convert To Floats", pos, "group %d: %v", i, err)
						}
						fs, err := conv("nested", inner, fmt.Sprintf("group %d, ", i))
						if err != nil {
							return nil, err
						}
						out[i] = fs
					}
					return out, nil
				},
			}, nil
		default:
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Convert To Floats expects List<Text>, List<Int>, or their List-of-List forms, got %s", in)}
		}
	},
}

// parseFloatValue converts one Text or Int element to a Float.
func parseFloatValue(v ir.Value) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case int64:
		return float64(x), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", x)
		}
		return f, nil
	}
	return 0, fmt.Errorf("expected Text or Int to convert, got %s", ir.DescribeValue(v))
}
