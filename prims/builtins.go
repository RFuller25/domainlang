package prims

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// runtimeErr builds an interpreter-time error tagged with the node identity.
func runtimeErr(prim string, pos token.Position, format string, args ...any) error {
	return &ir.RuntimeError{Prim: prim, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// ---------------------------------------------------------------------------
// Cursed Energy: Read Source — bind a file or stdin into Text.
// ---------------------------------------------------------------------------

var readSource = &Primitive{
	ID:      "Read Source",
	Keyword: "Cursed Energy",
	Match:   func(op *ast.Operation) bool { return true },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in != nil {
			return nil, &ResolveError{Pos: pos,
				Msg: "Cursed Energy (Read Source) must be the first stage; it takes no input"}
		}
		target := strings.TrimSpace(op.Raw)
		return &ir.Node{
			Prim:    "Read Source",
			In:      nil,
			Out:     ir.Text(),
			Display: fmt.Sprintf("Read Source <- %s", target),
			Meta:    map[string]any{"target": target},
			Pos:     pos,
			Eval: func(ctx *ir.Context, _ ir.Value) (ir.Value, error) {
				data, err := readSourceData(ctx, target)
				if err != nil {
					return nil, runtimeErr("Read Source", pos, "%v", err)
				}
				// Trim a single trailing newline batch (typical AoC inputs).
				return strings.TrimRight(string(data), "\r\n"), nil
			},
		}, nil
	},
}

func readSourceData(ctx *ir.Context, target string) ([]byte, error) {
	if target == "" || strings.EqualFold(target, "stdin") {
		if ctx.Stdin == nil {
			return nil, fmt.Errorf("no stdin available")
		}
		return io.ReadAll(ctx.Stdin)
	}
	path := target
	if !filepath.IsAbs(path) && ctx.BaseDir != "" {
		path = filepath.Join(ctx.BaseDir, target)
	}
	b, err := os.ReadFile(path)
	if err == nil {
		return b, nil
	}
	// A platform with no filesystem at all — js/wasm, where the documentation
	// site's playground runs — reports ENOSYS rather than "not found". It is
	// the same situation as a missing file from the program's point of view,
	// and falling back to stdin is what makes `Cursed Energy: input.txt` mean
	// "the input box" there, exactly as it means "the piped input" on a
	// terminal. Without this the playground could not run a single example.
	unsupported := errors.Is(err, errors.ErrUnsupported)
	if !os.IsNotExist(err) && !unsupported {
		// A real failure (permission denied, path is a directory, I/O error,
		// ...) should be reported, not masked by silently reading unrelated
		// stdin data (or hanging if stdin is an interactive terminal).
		return nil, fmt.Errorf("reading %q: %w", target, err)
	}
	// Fall back to stdin so `domain run day1.domain < input.txt` works even
	// when the named file is not present in the working directory.
	if ctx.Stdin != nil {
		return io.ReadAll(ctx.Stdin)
	}
	return nil, fmt.Errorf("could not read source %q and no stdin available", target)
}

// ---------------------------------------------------------------------------
// Cursed Technique: Split — Text x sep -> List<Text>.
// ---------------------------------------------------------------------------

var split = &Primitive{
	ID:      "Split",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Split") && !hasWord(op, "Each") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if !in.Equal(ir.Text()) {
			return nil, typeErr(pos, "Split", ir.Text(), in)
		}
		sepM, err := requireSeparator(op, args, in, pos, "Split")
		if err != nil {
			return nil, err
		}
		meta := map[string]any{}
		sepM.Meta(meta, "sep")
		return &ir.Node{
			Prim:    "Split",
			In:      ir.Text(),
			Out:     ir.List(ir.Text()),
			Display: "Split by " + sepM.Describe(),
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				s, ok := v.(string)
				if !ok {
					return nil, runtimeErr("Split", pos, "expected Text, got %s", ir.DescribeValue(v))
				}
				sep, err := sepM.Resolve(v)
				if err != nil {
					return nil, err
				}
				parts := strings.Split(s, sep)
				out := make([]ir.Value, len(parts))
				for i, p := range parts {
					out[i] = p
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Cursed Technique: Split Each — List<Text> x sep -> List<List<Text>>.
// ---------------------------------------------------------------------------

var splitEach = &Primitive{
	ID:      "Split Each",
	Keyword: "Cursed Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Split") && hasWord(op, "Each") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		want := ir.List(ir.Text())
		if !in.Equal(want) {
			return nil, typeErr(pos, "Split Each", want, in)
		}
		sepM, err := requireSeparator(op, args, in, pos, "Split Each")
		if err != nil {
			return nil, err
		}
		meta := map[string]any{}
		sepM.Meta(meta, "sep")
		return &ir.Node{
			Prim:    "Split Each",
			In:      want,
			Out:     ir.List(ir.List(ir.Text())),
			Display: "Split Each by " + sepM.Describe(),
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				groups, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Split Each", pos, "%v", err)
				}
				sep, err := sepM.Resolve(v)
				if err != nil {
					return nil, err
				}
				out := make([]ir.Value, len(groups))
				for i, g := range groups {
					s, ok := g.(string)
					if !ok {
						return nil, runtimeErr("Split Each", pos,
							"group %d is not Text (%s)", i, ir.DescribeValue(g))
					}
					parts := strings.Split(s, sep)
					inner := make([]ir.Value, len(parts))
					for j, p := range parts {
						inner[j] = p
					}
					out[i] = inner
				}
				return out, nil
			},
		}, nil
	},
}

// requireSeparator reads the separator in either form: the phrase's string
// literal, or a measured `By:` lambda over the text being split.
func requireSeparator(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position, prim string) (MeasuredText, error) {
	m, ok, err := measuredText(op, args, prim, "By", in, pos)
	if err != nil {
		return MeasuredText{}, err
	}
	if !ok {
		return MeasuredText{}, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s requires a separator string (e.g. by \"\\n\"), or a measured `By:` lambda", prim)}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Channeled Energy: Convert To Integers — text(s) -> int(s). Supports a flat
// (List<Text> -> List<Int>) and a nested (List<List<Text>> -> List<List<Int>>)
// form, chosen by the input type.
// ---------------------------------------------------------------------------

var convertToIntegers = &Primitive{
	ID:      "Convert To Integers",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Convert") && hasWord(op, "Integers") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		nested := ir.List(ir.List(ir.Text()))
		flat := ir.List(ir.Text())
		switch {
		case in.Equal(nested):
			return &ir.Node{
				Prim:    "Convert To Integers",
				In:      nested,
				Out:     ir.List(ir.List(ir.Int())),
				Display: "Convert Each List to Integers",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					groups, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Convert To Integers", pos, "%v", err)
					}
					out := make([]ir.Value, len(groups))
					for i, g := range groups {
						inner, err := ir.AsList(g)
						if err != nil {
							return nil, runtimeErr("Convert To Integers", pos, "group %d: %v", i, err)
						}
						conv := make([]ir.Value, len(inner))
						for j, e := range inner {
							n, err := parseIntValue(e)
							if err != nil {
								return nil, runtimeErr("Convert To Integers", pos,
									"group %d, item %d: %v", i, j, err)
							}
							conv[j] = n
						}
						out[i] = conv
					}
					return out, nil
				},
			}, nil
		case in.Equal(flat):
			return &ir.Node{
				Prim:    "Convert To Integers",
				In:      flat,
				Out:     ir.List(ir.Int()),
				Display: "Convert List to Integers",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					items, err := ir.AsList(v)
					if err != nil {
						return nil, runtimeErr("Convert To Integers", pos, "%v", err)
					}
					out := make([]ir.Value, len(items))
					for i, e := range items {
						n, err := parseIntValue(e)
						if err != nil {
							return nil, runtimeErr("Convert To Integers", pos, "item %d: %v", i, err)
						}
						out[i] = n
					}
					return out, nil
				},
			}, nil
		default:
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Convert To Integers expects List<Text> or List<List<Text>>, got %s", in)}
		}
	},
}

func parseIntValue(v ir.Value) (int64, error) {
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("expected Text to convert, got %s", ir.DescribeValue(v))
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", s)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Maximum Technique: Sum Each Group — List<List<Int>> -> List<Int>.
// ---------------------------------------------------------------------------

var sumEachGroup = &Primitive{
	ID:      "Sum Each Group",
	Keyword: "Maximum Technique",
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Sum") && (hasWord(op, "Group") || hasWord(op, "Each"))
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		want := ir.List(ir.List(ir.Int()))
		if !in.Equal(want) {
			return nil, typeErr(pos, "Sum Each Group", want, in)
		}
		return &ir.Node{
			Prim:    "Sum Each Group",
			In:      want,
			Out:     ir.List(ir.Int()),
			Display: "Sum Each Group",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				groups, err := ir.AsList(v)
				if err != nil {
					return nil, runtimeErr("Sum Each Group", pos, "%v", err)
				}
				out := make([]ir.Value, len(groups))
				for i, g := range groups {
					xs, err := ir.AsIntSlice(g)
					if err != nil {
						return nil, runtimeErr("Sum Each Group", pos, "group %d: %v", i, err)
					}
					var s int64
					for _, x := range xs {
						s += x
					}
					out[i] = s
				}
				return out, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Sum — List<Int> -> Int.
// ---------------------------------------------------------------------------

var sum = &Primitive{
	ID:      "Sum",
	Keyword: "Maximum Technique",
	Match: func(op *ast.Operation) bool {
		return hasWord(op, "Sum") && !hasWord(op, "Group") && !hasWord(op, "Each") &&
			!hasWord(op, "Select") && !hasWord(op, "Top")
	},
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in != nil && in.Equal(ir.List(ir.Float())) {
			return &ir.Node{
				Prim:    "Sum",
				In:      ir.List(ir.Float()),
				Out:     ir.Float(),
				Display: "Sum (Float)",
				Pos:     pos,
				Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
					xs, err := ir.AsFloatSlice(v)
					if err != nil {
						return nil, runtimeErr("Sum", pos, "%v", err)
					}
					var s float64
					for _, x := range xs {
						s += x
					}
					return s, nil
				},
			}, nil
		}
		want := ir.List(ir.Int())
		if !in.Equal(want) {
			return nil, typeErr(pos, "Sum", want, in)
		}
		return &ir.Node{
			Prim:    "Sum",
			In:      want,
			Out:     ir.Int(),
			Display: "Sum",
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsIntSlice(v)
				if err != nil {
					return nil, runtimeErr("Sum", pos, "%v", err)
				}
				var s int64
				for _, x := range xs {
					s += x
				}
				return s, nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Maximum Technique: Select Top K (, Sum) — take the first K after a sort.
// ---------------------------------------------------------------------------

var selectTopK = &Primitive{
	ID:      "Select Top K",
	Keyword: "Maximum Technique",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Select") && hasWord(op, "Top") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		want := ir.List(ir.Int())
		if !in.Equal(want) {
			return nil, typeErr(pos, "Select Top K", want, in)
		}
		countM, err := requireMeasuredInt(op, args, "Select Top K", "Count", 0, 0, in, pos, "a count", "Select Top 3")
		if err != nil {
			return nil, err
		}
		if !countM.IsMeasured() && countM.Lit < 0 {
			return nil, &ResolveError{Pos: pos, Msg: "Select Top K requires a non-negative count"}
		}
		thenSum := hasModifier(op, "Sum")
		out := want
		display := "Select Top " + countM.Describe()
		if thenSum {
			out = ir.Int()
			display += ", Sum"
		}
		meta := map[string]any{"sum": thenSum}
		countM.Meta(meta, "k")
		return &ir.Node{
			Prim:    "SelectTopK",
			In:      want,
			Out:     out,
			Display: display,
			Meta:    meta,
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsIntSlice(v)
				if err != nil {
					return nil, runtimeErr("SelectTopK", pos, "%v", err)
				}
				k, err := countM.Resolve(v)
				if err != nil {
					return nil, err
				}
				top := xs[:min(max(int(k), 0), len(xs))]
				if thenSum {
					var s int64
					for _, x := range top {
						s += x
					}
					return s, nil
				}
				return ir.IntsToValue(top), nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Domain Expansion: Sort (Quicksort) — List<Int> x order -> List<Int>.
// This is the optimizer's target: a swappable algorithm.
// ---------------------------------------------------------------------------

var sortPrim = &Primitive{
	ID:      "Sort",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Sort") || hasWord(op, "Quicksort") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in != nil && in.Equal(ir.List(ir.Float())) {
			return buildFloatSort(op, pos), nil
		}
		// Text (and any other ordered element) sorts by its natural order —
		// alphabetical for Text, lexicographic for a tuple. Only Int keeps the
		// specialized int path the optimizer's quickselect fusion targets.
		if in != nil && in.Kind == ir.KList && ir.Ordered(in.Elem) && !in.Elem.Equal(ir.Int()) {
			return buildOrderedSort(op, in, pos), nil
		}
		want := ir.List(ir.Int())
		if !in.Equal(want) {
			return nil, typeErr(pos, "Sort", want, in)
		}
		desc := hasModifier(op, "Descending")
		order := "Ascending"
		if desc {
			order = "Descending"
		}
		return &ir.Node{
			Prim:      "Sort",
			In:        want,
			Out:       want,
			Display:   "Quicksort, " + order,
			Swappable: true,
			Meta:      map[string]any{"desc": desc},
			Pos:       pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				xs, err := ir.AsIntSlice(v)
				if err != nil {
					return nil, runtimeErr("Sort", pos, "%v", err)
				}
				out := slices.Clone(xs)
				slices.Sort(out)
				if desc {
					slices.Reverse(out)
				}
				return ir.IntsToValue(out), nil
			},
		}, nil
	},
}

// ---------------------------------------------------------------------------
// Reveal: Emit — value -> stdout.
// ---------------------------------------------------------------------------

var emit = &Primitive{
	ID:      "Emit",
	Keyword: "Reveal",
	Match:   func(op *ast.Operation) bool { return true },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Reveal has nothing to emit (empty pipeline)"}
		}
		// `Reveal: stderr` sends the value to standard error instead. That
		// makes a mid-pipeline Reveal a debugging tool: it does not disturb
		// the program's answer, so a golden test still passes with one in
		// place.
		toStderr := hasWord(op, "stderr")
		target := "stdout"
		if toStderr {
			target = "stderr"
		}
		return &ir.Node{
			Prim:    "Emit",
			In:      in,
			Out:     in,
			Display: "Reveal -> " + target,
			Meta:    map[string]any{"target": target},
			Pos:     pos,
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				// A nil writer discards, the same rule Stdout already
				// follows — never cross the streams, or a caller capturing
				// stdout would see stderr output the binary keeps separate.
				w := ctx.Stdout
				if toStderr {
					w = ctx.Stderr
				}
				if w != nil {
					// Rendered against the static type, so a Record's fields
					// come out in the order the type declares rather than the
					// order this particular value happened to be built in —
					// which is the only order a compiled binary's struct can
					// produce, and stdout has to match it byte for byte.
					_, _ = fmt.Fprintln(w, ir.LabelledOutput(ctx.PartLabel, ir.FormatValueTyped(v, in)))
				}
				return v, nil
			},
		}, nil
	},
}
