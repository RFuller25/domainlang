package prims

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// The AoC toolbox primitives: input mining (Extract Integers, Split Fields),
// set construction (Convert To Set), interval merging (Merge Ranges), and
// exhaustive generation (Permutations, Subsets). See docs/aoc-toolbox.md for
// the map from the classic AoC helper library to these operations.

// ---------------------------------------------------------------------------
// Cursed Technique: Extract Integers — mine every integer out of messy text.
//   Text       -> List<Int>
//   List<Text> -> List<List<Int>>   (per line)
// ---------------------------------------------------------------------------

// intPattern matches candidate signed integers; extractInts demotes a '-'
// that directly follows a digit to a separator (so "36-92" is 36 and 92, not
// 36 and -92, while "x=-5" is still -5).
var intPattern = regexp.MustCompile(`-?\d+`)

func extractInts(s string) ([]ir.Value, error) {
	var out []ir.Value
	for _, loc := range intPattern.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		if s[start] == '-' && start > 0 && s[start-1] >= '0' && s[start-1] <= '9' {
			start++ // "36-92": the '-' separates, it does not negate
		}
		n, err := strconv.ParseInt(s[start:end], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q overflows Int", s[start:end])
		}
		out = append(out, n)
	}
	if out == nil {
		out = []ir.Value{}
	}
	return out, nil
}

var extractIntegers = &Primitive{
	ID:      "Extract Integers",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Extract") && hasWord(op, "Integers") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		switch {
		case in.Equal(ir.Text()):
			return &ir.Node{
				Prim:    "Extract Integers",
				In:      ir.Text(),
				Out:     ir.List(ir.Int()),
				Display: "Extract Integers",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					s, ok := v.(string)
					if !ok {
						return nil, runtimeErr("Extract Integers", pos, "expected Text, got %s", ir.DescribeValue(v))
					}
					out, err := extractInts(s)
					if err != nil {
						return nil, runtimeErr("Extract Integers", pos, "%v", err)
					}
					return out, nil
				},
			}, nil
		case in.Equal(ir.List(ir.Text())):
			return &ir.Node{
				Prim:    "Extract Integers",
				In:      ir.List(ir.Text()),
				Out:     ir.List(ir.List(ir.Int())),
				Display: "Extract Integers (each line)",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					lines, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Extract Integers", pos, "%v", err)
					}
					out := make([]ir.Value, len(lines))
					for i, line := range lines {
						s, ok := line.(string)
						if !ok {
							return nil, runtimeErr("Extract Integers", pos, "line %d is not Text", i)
						}
						ints, err := extractInts(s)
						if err != nil {
							return nil, runtimeErr("Extract Integers", pos, "line %d: %v", i, err)
						}
						out[i] = ints
					}
					return out, nil
				},
			}, nil
		default:
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Extract Integers expects Text or List<Text>, got %s", in)}
		}
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Split Fields — split on runs of whitespace.
//   Text       -> List<Text>
//   List<Text> -> List<List<Text>>  (per line)
// ---------------------------------------------------------------------------

var splitFieldsPrim = &Primitive{
	ID:      "Split Fields",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Split") && hasWord(op, "Fields") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		switch {
		case in.Equal(ir.Text()):
			return &ir.Node{
				Prim:    "Split Fields",
				In:      ir.Text(),
				Out:     ir.List(ir.Text()),
				Display: "Split Fields",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					s, ok := v.(string)
					if !ok {
						return nil, runtimeErr("Split Fields", pos, "expected Text, got %s", ir.DescribeValue(v))
					}
					return fieldsValue(s), nil
				},
			}, nil
		case in.Equal(ir.List(ir.Text())):
			return &ir.Node{
				Prim:    "Split Fields",
				In:      ir.List(ir.Text()),
				Out:     ir.List(ir.List(ir.Text())),
				Display: "Split Fields (each line)",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					lines, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Split Fields", pos, "%v", err)
					}
					out := make([]ir.Value, len(lines))
					for i, line := range lines {
						s, ok := line.(string)
						if !ok {
							return nil, runtimeErr("Split Fields", pos, "line %d is not Text", i)
						}
						out[i] = fieldsValue(s)
					}
					return out, nil
				},
			}, nil
		default:
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Split Fields expects Text or List<Text>, got %s", in)}
		}
	},
}

func fieldsValue(s string) []ir.Value {
	fields := strings.Fields(s)
	out := make([]ir.Value, len(fields))
	for i, f := range fields {
		out[i] = f
	}
	return out
}

// ---------------------------------------------------------------------------
// Cursed Technique: Ragged Columns — List<Text> -> List<List<Text>>: the
// character columns of a block of lines, tolerating ragged (unpadded) line
// lengths by skipping missing cells. Column c holds the c-th rune of every
// line long enough to have one, top to bottom — the classic move for parsing
// fixed-column drawings like AoC 2022 Day 5's crate stacks.
// ---------------------------------------------------------------------------

var raggedColumns = &Primitive{
	ID:      "Ragged Columns",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Ragged") && hasWord(op, "Columns") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		want := ir.List(ir.Text())
		if !in.Equal(want) {
			return nil, typeErr(pos, "Ragged Columns", want, in)
		}
		return &ir.Node{
			Prim:    "Ragged Columns",
			In:      want,
			Out:     ir.List(ir.List(ir.Text())),
			Display: "Ragged Columns",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				lines, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Ragged Columns", pos, "%v", err)
				}
				rows := make([][]string, len(lines))
				width := 0
				for i, line := range lines {
					s, ok := line.(string)
					if !ok {
						return nil, runtimeErr("Ragged Columns", pos, "line %d is not Text", i)
					}
					var runes []string
					for _, r := range s {
						runes = append(runes, string(r))
					}
					rows[i] = runes
					width = max(width, len(runes))
				}
				out := make([]ir.Value, width)
				for c := range width {
					col := []ir.Value{}
					for _, runes := range rows {
						if c < len(runes) {
							col = append(col, runes[c])
						}
					}
					out[c] = col
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Join — List<Text> -> Text, concatenated (optionally
// `Join with ", "` for a separator).
// ---------------------------------------------------------------------------

var join = &Primitive{
	ID:      "Join",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Join") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		want := ir.List(ir.Text())
		if !in.Equal(want) {
			return nil, typeErr(pos, "Join", want, in)
		}
		sepM, _, err := measuredText(op, args, "Join", "With", in, pos)
		if err != nil {
			return nil, err
		}
		meta := map[string]any{}
		sepM.Meta(meta, "sep")
		return &ir.Node{
			Prim:    "Join",
			In:      want,
			Out:     ir.Text(),
			Display: "Join with " + sepM.Describe(),
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Join", pos, "%v", err)
				}
				sep, err := sepM.Resolve(v)
				if err != nil {
					return nil, err
				}
				parts := make([]string, len(xs))
				for i, x := range xs {
					s, ok := x.(string)
					if !ok {
						return nil, runtimeErr("Join", pos, "item %d is not Text", i)
					}
					parts[i] = s
				}
				return strings.Join(parts, sep), nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Channeled Energy: Convert To Set — List<T> -> Set<T> (T keyable).
// ---------------------------------------------------------------------------

var convertToSet = &Primitive{
	ID:      "Convert To Set",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Convert") && hasWord(op, "Set") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Convert To Set", pos)
		if err != nil {
			return nil, err
		}
		if err := requireKeyable(elem, "Convert To Set", pos); err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:    "Convert To Set",
			In:      in,
			Out:     ir.Set(elem),
			Display: "Convert To Set",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Convert To Set", pos, "%v", err)
				}
				return ir.SetFromList(xs), nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Merge Ranges — coalesce inclusive integer intervals.
//   List<(Int, Int)> / List<List<Int>> -> same shape (each range two ints)
//   List<{lo:Int, hi:Int}>             -> same record type (any two-Int-field
//                                         record; the first declared field is
//                                         the low end)
// Output is sorted by low end; overlapping or adjacent ranges are merged.
// ---------------------------------------------------------------------------

var mergeRanges = &Primitive{
	ID:      "Merge Ranges",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Merge") && hasWord(op, "Ranges") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Merge Ranges", pos)
		if err != nil {
			return nil, err
		}
		isPair := elem.Equal(ir.Tuple(ir.Int(), ir.Int())) || elem.Equal(ir.List(ir.Int()))
		isRecord := elem.Kind == ir.KRecord && len(elem.Fields) == 2 &&
			elem.Fields[0].Type.Equal(ir.Int()) && elem.Fields[1].Type.Equal(ir.Int())
		if !isPair && !isRecord {
			return nil, &ResolveError{Pos: pos,
				Msg: fmt.Sprintf("Merge Ranges expects List<(Int, Int)>, List<List<Int>> (two per row), or a list of two-Int-field records, got %s", in)}
		}
		var loName, hiName string
		if isRecord {
			loName, hiName = elem.Fields[0].Name, elem.Fields[1].Name
		}
		return &ir.Node{
			Prim:    "Merge Ranges",
			In:      in,
			Out:     seqOut(in, elem),
			Display: "Merge Ranges",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Merge Ranges", pos, "%v", err)
				}
				type span struct{ lo, hi int64 }
				spans := make([]span, len(xs))
				for i, x := range xs {
					lo, hi, err := rangeEnds(x, loName, hiName)
					if err != nil {
						return nil, runtimeErr("Merge Ranges", pos, "range %d: %v", i, err)
					}
					if lo > hi {
						return nil, runtimeErr("Merge Ranges", pos,
							"range %d is inverted: %d > %d", i, lo, hi)
					}
					spans[i] = span{lo, hi}
				}
				slices.SortFunc(spans, func(a, b span) int {
					if c := cmp.Compare(a.lo, b.lo); c != 0 {
						return c
					}
					return cmp.Compare(a.hi, b.hi)
				})
				var merged []span
				for _, s := range spans {
					// Adjacency check: s.lo <= merged[n-1].hi+1, rewritten to avoid
					// the overflow-prone "+1" (wraps to MinInt64 when hi is
					// MaxInt64) and the corresponding underflow in "s.lo-1" when
					// s.lo is MinInt64.
					if n := len(merged); n > 0 &&
						(merged[n-1].hi == math.MaxInt64 || s.lo == math.MinInt64 || s.lo-1 <= merged[n-1].hi) {
						merged[n-1].hi = max(merged[n-1].hi, s.hi)
						continue
					}
					merged = append(merged, s)
				}
				out := make([]ir.Value, len(merged))
				for i, s := range merged {
					if isRecord {
						rec := ir.NewRecordValue()
						rec.Set(loName, s.lo)
						rec.Set(hiName, s.hi)
						out[i] = rec
					} else {
						out[i] = []ir.Value{s.lo, s.hi}
					}
				}
				return out, nil
			},
		}, nil
	},
}

// rangeEnds unpacks one range value, either a two-int tuple or a record with
// the resolved field names.
func rangeEnds(v ir.Value, loName, hiName string) (int64, int64, error) {
	if loName != "" {
		rec, ok := v.(*ir.RecordValue)
		if !ok {
			return 0, 0, fmt.Errorf("expected a Record, got %s", ir.DescribeValue(v))
		}
		loV, ok1 := rec.Get(loName)
		hiV, ok2 := rec.Get(hiName)
		if !ok1 || !ok2 {
			return 0, 0, fmt.Errorf("record is missing field %q or %q", loName, hiName)
		}
		lo, ok1 := loV.(int64)
		hi, ok2 := hiV.(int64)
		if !ok1 || !ok2 {
			return 0, 0, fmt.Errorf("range ends must be Int")
		}
		return lo, hi, nil
	}
	xs, ok := v.([]ir.Value)
	if !ok || len(xs) != 2 {
		return 0, 0, fmt.Errorf("expected an (Int, Int) pair, got %s", ir.DescribeValue(v))
	}
	lo, ok1 := xs[0].(int64)
	hi, ok2 := xs[1].(int64)
	if !ok1 || !ok2 {
		return 0, 0, fmt.Errorf("range ends must be Int")
	}
	return lo, hi, nil
}

// ---------------------------------------------------------------------------
// Domain Expansion: Permutations — List<T> -> List<List<T>>, every ordering
// in lexicographic index order.
// ---------------------------------------------------------------------------

// MaxPermutationInput optionally bounds Permutations. Zero — the default —
// means unlimited. It used to be a hard 9, which refused a 10-element input
// (3.6M orderings) that a machine handles comfortably; a limit must never be
// the reason a correct program cannot run. n! still explodes, so a large
// input is now slow or memory-hungry rather than a clean refusal. Codegen
// emits whatever this says, so both backends behave identically. It is a var
// so a caller (or a test) can opt back into a ceiling.
var MaxPermutationInput = 0

var permutations = &Primitive{
	ID:      "Permutations",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Permutations") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Permutations", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:      "Permutations",
			In:        in,
			Out:       ir.List(ir.List(elem)),
			Display:   "Permutations",
			Swappable: true,
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Permutations", pos, "%v", err)
				}
				if MaxPermutationInput > 0 && len(xs) > MaxPermutationInput {
					return nil, runtimeErr("Permutations", pos,
						"refusing to permute %d elements (n! explodes; the bound is %d)",
						len(xs), MaxPermutationInput)
				}
				out := []ir.Value{}
				perm := make([]ir.Value, 0, len(xs))
				used := make([]bool, len(xs))
				var rec func()
				rec = func() {
					if len(perm) == len(xs) {
						out = append(out, append([]ir.Value(nil), perm...))
						return
					}
					for i, x := range xs {
						if used[i] {
							continue
						}
						used[i] = true
						perm = append(perm, x)
						rec()
						perm = perm[:len(perm)-1]
						used[i] = false
					}
				}
				rec()
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Domain Expansion: Subsets — List<T> -> List<List<T>>, the power set. Subset
// k of the output includes element i iff bit i of k is set, so the empty set
// comes first and the full list last.
// ---------------------------------------------------------------------------

// MaxSubsetInput optionally bounds Subsets; zero (the default) is unlimited,
// for the same reason as MaxPermutationInput. 2^n still explodes.
var MaxSubsetInput = 0

var subsets = &Primitive{
	ID:      "Subsets",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Subsets") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		elem, err := listElem(in, "Subsets", pos)
		if err != nil {
			return nil, err
		}
		return &ir.Node{
			Prim:      "Subsets",
			In:        in,
			Out:       ir.List(ir.List(elem)),
			Display:   "Subsets",
			Swappable: true,
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Subsets", pos, "%v", err)
				}
				if MaxSubsetInput > 0 && len(xs) > MaxSubsetInput {
					return nil, runtimeErr("Subsets", pos,
						"refusing the power set of %d elements (2^n explodes; the bound is %d)",
						len(xs), MaxSubsetInput)
				}
				total := 1 << len(xs)
				out := make([]ir.Value, 0, total)
				for mask := range total {
					sub := []ir.Value{}
					for i, x := range xs {
						if mask&(1<<i) != 0 {
							sub = append(sub, x)
						}
					}
					out = append(out, sub)
				}
				return out, nil
			},
		}, nil
	},
}
