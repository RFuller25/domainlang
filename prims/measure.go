package prims

import (
	"fmt"
	"math"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// Measured arguments: a primitive's Int argument written either as the literal
// it has always taken (`Window 3`) or as a lambda over the current value
// (`Size: (xs) -> length(xs) / 2`).
//
// The two forms are the same argument, so they share one reader. What differs
// is *when* the value is known: a literal is known at resolve time and lands in
// Meta where the optimizer and the Go backend can see it; a measured argument
// is known only once data arrives, so its checks move to runtime and the passes
// keyed on the literal stand down (see docs/optimizer.md).

// Measured is one Int argument in either form. Exactly one field is set:
// Lambda == nil means the literal in Lit.
type Measured struct {
	Lit    int64
	Lambda *ast.Lambda
	// In is the pipeline type the lambda was type-checked against, kept so the
	// Eval closure can bind the same parameter type it checked.
	In *ir.Type
	// Min is the argument's lower bound (1 for a window size, 0 for a count).
	// It travels with the value because the check has to happen wherever the
	// value is resolved — including on a fused node the optimizer built, which
	// cannot reach back into this package.
	Min int64
	// Prim and Name name the argument in runtime errors ("Window's Size:").
	Prim string
	Name string
	Pos  token.Position
}

// NoBound is the Min for an argument with no lower bound of its own — a
// `Take Item` index, whose range check is against the list rather than zero,
// or a `Range` bound, which is checked against the other bound.
const NoBound = math.MinInt64

// IsMeasured reports whether the value is only known at runtime.
func (m Measured) IsMeasured() bool { return m.Lambda != nil }

// Resolve returns the argument's value for the value flowing into the node —
// the literal, or the measuring lambda applied to v — with the lower bound
// checked. Every consumer goes through it, so a measured argument fails the
// same way whether the node running it is the primitive's own or a fused one.
func (m Measured) Resolve(v ir.Value) (int64, error) {
	n, err := m.value(v)
	if err != nil {
		return 0, err
	}
	if m.Min != NoBound {
		if err := m.atLeast(m.Min, n, measuredFrom(v)); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (m Measured) value(v ir.Value) (int64, error) {
	if m.Lambda == nil {
		return m.Lit, nil
	}
	r, err := eval.EvalLambdaTyped(m.Lambda,
		append([]*ir.Type{m.In}, ambientTypes()...),
		append([]ir.Value{v}, ambientArgs()...)...)
	if err != nil {
		return 0, runtimeErr(m.Prim, m.Pos, "%s: %v", m.Name, err)
	}
	n, ok := r.(int64)
	if !ok {
		return 0, runtimeErr(m.Prim, m.Pos, "%s: measured a %T, not an Int", m.Name, r)
	}
	return n, nil
}

// measuredFrom describes the value an argument was measured from, for an error
// that says why the number came out the way it did.
func measuredFrom(v ir.Value) string {
	if xs, ok := v.([]ir.Value); ok {
		return fmt.Sprintf("from a list of %d element(s)", len(xs))
	}
	return ""
}

// Describe renders the argument for a Display string: the literal, or the
// lambda's own source-ish shape for a measured one.
func (m Measured) Describe() string {
	if m.Lambda == nil {
		return fmt.Sprintf("%d", m.Lit)
	}
	return "(measured)"
}

// Meta writes the argument into a node's metadata under key: an int64 for a
// literal — the shape every existing optimizer pass and codegen lowering
// already reads — or key+"Expr" holding the lambda for a measured one. Keeping
// the literal key literal-only is what lets a pass opt in to measured
// arguments deliberately instead of silently mistaking a missing int for zero.
func (m Measured) Meta(meta map[string]any, key string) {
	if m.Lambda == nil {
		meta[key] = m.Lit
		return
	}
	meta[key+"Expr"] = m.Lambda
	// The interpreter half: a pass that moves this argument onto a fused node
	// copies the closure with it and gets the primitive's own number and its
	// own errors, without importing this package (see ir.MeasureFn).
	meta[key+"Fn"] = ir.MeasureFn(m.Resolve)
}

// measuredInt reads one Int argument in either form.
//
// slot is the argument's position among the phrase's integer literals
// (`Window SIZE STEP` puts Size at 0 and Step at 1); name is the named
// argument that supplies it instead. Writing both is an error: silently
// preferring one would make the discarded spelling a lie, which is the failure
// mode the unused-argument lint exists to end.
//
// ok is false when neither form is present, leaving "is this argument
// required, and what is its default" to the caller — the callers disagree
// (`Window` requires a size, its step defaults to 1).
// A negative slot means the phrase has no position for this argument at all —
// `Range 10` writes only the high bound, so Low: has no literal spelling there
// and may only arrive as a named argument.
func measuredInt(op *ast.Operation, args ArgSet, prim, name string, slot int, min int64, in *ir.Type, pos token.Position) (m Measured, ok bool, err error) {
	hasLit := slot >= 0 && op != nil && len(op.Ints) > slot
	base := Measured{Min: min, Prim: prim, Name: name, Pos: pos}
	if lam, isLam := args.Lambda(name); isLam {
		if hasLit {
			return Measured{}, false, bothFormsErr(prim, name, pos)
		}
		if err := checkMeasuredLambda(lam, prim, name, ir.Int(), in, pos); err != nil {
			return Measured{}, false, err
		}
		base.Lambda, base.In = lam, in
		return base, true, nil
	}
	if n, isInt := args.Int(name); isInt {
		if hasLit {
			return Measured{}, false, bothFormsErr(prim, name, pos)
		}
		base.Lit = n
		return base, true, nil
	}
	if hasLit {
		base.Lit = op.Ints[slot]
		return base, true, nil
	}
	return Measured{}, false, nil
}

// checkMeasuredLambda type-checks a measuring lambda: one parameter bound to
// the current value (plus one per enclosing For loop, exactly as requireLambda
// counts them), returning want.
func checkMeasuredLambda(lam *ast.Lambda, prim, name string, want, in *ir.Type, pos token.Position) error {
	out, err := measuredLambdaType(lam, prim, name, in, pos)
	if err != nil {
		return err
	}
	if !out.Equal(want) {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s: %s: lambda must produce %s, got %s", prim, name, want, out)}
	}
	return nil
}

// measuredLambdaType checks a measuring lambda's shape and returns the type it
// produces. A slot with a fixed type compares it (checkMeasuredLambda); a slot
// whose type the argument *decides* — a Sparse default, a Pad Grid fill —
// takes it from here and checks it against the value it has to match.
func measuredLambdaType(lam *ast.Lambda, prim, name string, in *ir.Type, pos token.Position) (*ir.Type, error) {
	if in == nil {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s: %s: has no input value to measure — a measured argument needs a value flowing into the statement",
			prim, name)}
	}
	wantArity := 1 + ambientDepth()
	if len(lam.Params) != wantArity {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s: %s: lambda must take %d parameter(s), got %d",
			prim, name, wantArity, len(lam.Params))}
	}
	out, err := typecheck.LambdaType(lam, append([]*ir.Type{in}, ambientTypes()...)...)
	if err != nil {
		return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("%s: %s: %v", prim, name, err)}
	}
	return out, nil
}

func bothFormsErr(prim, name string, pos token.Position) error {
	return &ResolveError{Pos: pos, Msg: fmt.Sprintf(
		"%s: %s: is written twice — once in the phrase and once as %s:; keep one",
		prim, name, name)}
}

// requireMeasuredInt is measuredInt for an argument with no default. noun names
// the argument the way the primitive's own message always has ("a window
// size"), and example shows the phrase spelling, so the error still leads with
// the form most programs want.
func requireMeasuredInt(op *ast.Operation, args ArgSet, prim, name string, slot int, min int64, in *ir.Type, pos token.Position, noun, example string) (Measured, error) {
	m, ok, err := measuredInt(op, args, prim, name, slot, min, in, pos)
	if err != nil {
		return Measured{}, err
	}
	if !ok {
		return Measured{}, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
			"%s requires %s, e.g. %s (or `%s: (xs) -> length(xs) / 2`)",
			prim, noun, example, name)}
	}
	return m, nil
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}

// atLeast checks a resolved argument against a lower bound. A literal fails at
// resolve time as it always has; a measured one can only fail once it has been
// measured, so the message says what it measured and from what.
func (m Measured) atLeast(n int64, got int64, sizeOf string) error {
	if got >= n {
		return nil
	}
	if sizeOf == "" && m.Lambda != nil {
		return runtimeErr(m.Prim, m.Pos, "%s: measured %d, but the %s must be >= %d",
			m.Name, got, lowerFirst(m.Name), n)
	}
	if m.Lambda == nil {
		return &ResolveError{Pos: m.Pos, Msg: fmt.Sprintf(
			"%s %s must be >= %d, got %d", m.Prim, lowerFirst(m.Name), n, got)}
	}
	return runtimeErr(m.Prim, m.Pos, "%s: measured %d %s, but the %s must be >= %d",
		m.Name, got, sizeOf, lowerFirst(m.Name), n)
}

// ---------------------------------------------------------------------------
// Text-valued measured arguments
// ---------------------------------------------------------------------------

// MeasuredText is the Text form of a measured argument: a separator written
// either as the string literal the phrase has always taken (`Split Text by
// ","`) or as a lambda over the current value (`By: (t) -> …`).
//
// It is a sibling of Measured rather than a generalization of it because the
// two differ in everything except the reading rule they share: an Int argument
// has a lower bound to check and a value the optimizer may fold, and a Text
// one has neither. What they must not differ in is the *reading* — which
// spelling wins, and what happens when a program writes both — so both go
// through the same both-forms check and the same lambda type check.
type MeasuredText struct {
	Lit    string
	Lambda *ast.Lambda
	In     *ir.Type
	Prim   string
	Name   string
	Pos    token.Position
}

// IsMeasured reports whether the value is only known at runtime.
func (m MeasuredText) IsMeasured() bool { return m.Lambda != nil }

// Resolve returns the argument's value for the value flowing into the node.
func (m MeasuredText) Resolve(v ir.Value) (string, error) {
	if m.Lambda == nil {
		return m.Lit, nil
	}
	r, err := eval.EvalLambdaTyped(m.Lambda,
		append([]*ir.Type{m.In}, ambientTypes()...),
		append([]ir.Value{v}, ambientArgs()...)...)
	if err != nil {
		return "", runtimeErr(m.Prim, m.Pos, "%s: %v", m.Name, err)
	}
	s, ok := r.(string)
	if !ok {
		return "", runtimeErr(m.Prim, m.Pos, "%s: measured a %T, not Text", m.Name, r)
	}
	return s, nil
}

// Describe renders the argument for a Display string.
func (m MeasuredText) Describe() string {
	if m.Lambda == nil {
		return fmt.Sprintf("%q", m.Lit)
	}
	return "(measured)"
}

// Meta writes the argument into a node's metadata: the string under key for a
// literal — the shape codegen's fusion rules read — or the lambda under
// key+"Expr" for a measured one. Nothing carries a runtime closure here: no
// optimizer pass reads a separator, and the compiler needs the lambda itself.
func (m MeasuredText) Meta(meta map[string]any, key string) {
	if m.Lambda == nil {
		meta[key] = m.Lit
		return
	}
	meta[key+"Expr"] = m.Lambda
}

// measuredText reads one Text argument in either form: the phrase's first
// string literal, or the named argument. Writing both is an error, exactly as
// it is for a count.
func measuredText(op *ast.Operation, args ArgSet, prim, name string, in *ir.Type, pos token.Position) (m MeasuredText, ok bool, err error) {
	hasLit := op != nil && len(op.Strings) > 0
	base := MeasuredText{In: in, Prim: prim, Name: name, Pos: pos}
	if lam, isLam := args.Lambda(name); isLam {
		if hasLit {
			return MeasuredText{}, false, bothFormsErr(prim, name, pos)
		}
		if err := checkMeasuredLambda(lam, prim, name, ir.Text(), in, pos); err != nil {
			return MeasuredText{}, false, err
		}
		base.Lambda = lam
		return base, true, nil
	}
	if s, isText := args.Text(name); isText {
		if hasLit {
			return MeasuredText{}, false, bothFormsErr(prim, name, pos)
		}
		base.Lit = s
		return base, true, nil
	}
	if hasLit {
		base.Lit = op.Strings[0]
		return base, true, nil
	}
	return MeasuredText{}, false, nil
}

// ---------------------------------------------------------------------------
// Value-typed measured arguments
// ---------------------------------------------------------------------------

// MeasuredValue is a measured argument whose *type* is part of what it says: a
// Pad Grid fill or a Sparse default, which the primitive checks against the
// element type it has to match. A literal answers with its own type (Int or
// Text, the two a phrase can spell); a lambda answers with the type its body
// produces.
type MeasuredValue struct {
	Lit    ir.Value
	Type   *ir.Type
	Lambda *ast.Lambda
	In     *ir.Type
	Prim   string
	Name   string
	Pos    token.Position
}

// IsMeasured reports whether the value is only known at runtime.
func (m MeasuredValue) IsMeasured() bool { return m.Lambda != nil }

// Resolve returns the argument's value for the value flowing into the node.
func (m MeasuredValue) Resolve(v ir.Value) (ir.Value, error) {
	if m.Lambda == nil {
		return m.Lit, nil
	}
	r, err := eval.EvalLambdaTyped(m.Lambda,
		append([]*ir.Type{m.In}, ambientTypes()...),
		append([]ir.Value{v}, ambientArgs()...)...)
	if err != nil {
		return nil, runtimeErr(m.Prim, m.Pos, "%s: %v", m.Name, err)
	}
	return r, nil
}

// Describe renders the argument for a Display string.
func (m MeasuredValue) Describe() string {
	if m.Lambda == nil {
		return ir.FormatShort(m.Lit)
	}
	return "(measured)"
}

// Meta writes the argument: the value itself under key for a literal — the
// shape the Go backend's literal renderers read — or the lambda under
// key+"Expr" for a measured one.
func (m MeasuredValue) Meta(meta map[string]any, key string) {
	if m.Lambda == nil {
		meta[key] = m.Lit
		return
	}
	meta[key+"Expr"] = m.Lambda
}

// measuredValue reads one value-typed named argument in either form. Unlike a
// count or a separator it has no phrase spelling at all — these arguments have
// always been named — so there is no both-forms case to reject.
func measuredValue(args ArgSet, prim, name string, in *ir.Type, pos token.Position) (MeasuredValue, error) {
	base := MeasuredValue{In: in, Prim: prim, Name: name, Pos: pos}
	if lam, ok := args.Lambda(name); ok {
		t, err := measuredLambdaType(lam, prim, name, in, pos)
		if err != nil {
			return MeasuredValue{}, err
		}
		base.Lambda, base.Type = lam, t
		return base, nil
	}
	if n, ok := args.Int(name); ok {
		base.Lit, base.Type = n, ir.Int()
		return base, nil
	}
	if s, ok := args.Text(name); ok {
		base.Lit, base.Type = s, ir.Text()
		return base, nil
	}
	return MeasuredValue{}, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
		"%s requires %s: — an Int or Text literal, or a lambda over the current value",
		prim, name)}
}
