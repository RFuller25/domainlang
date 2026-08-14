package prims

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/token"
)

// Lowering written types (ast.TypeExpr) to the type model (ir.Type). This is
// where a type name is checked, generic arity is enforced, and keyability is
// applied — using ir.Keyable itself, so the rule cannot drift from the one
// primitives already enforce on Map keys and Set elements.

// scalarTypes are the types writable by name with no arguments.
var scalarTypes = map[string]func() *ir.Type{
	"Int":   ir.Int,
	"Float": ir.Float,
	"Text":  ir.Text,
	"Bool":  ir.Bool,
}

// genericArity is the number of type arguments each generic name takes.
var genericArity = map[string]int{
	"List":   1,
	"Set":    1,
	"Grid":   1,
	"Sparse": 1,
	"Graph":  1,
	"Map":    2,
}

// lowerTypeExpr converts a written type to an ir.Type, or reports why it cannot.
func lowerTypeExpr(te *ast.TypeExpr, pos token.Position) (*ir.Type, error) {
	if te == nil {
		return nil, &ResolveError{Pos: pos, Msg: "missing type"}
	}
	at := te.Pos
	if at.Line == 0 {
		at = pos
	}

	switch {
	case te.Lambda != nil:
		return nil, &ResolveError{Pos: at,
			Msg: "a lambda type is only valid as a Shikigami parameter, not as a pipeline type"}

	case te.Tuple != nil:
		elems := make([]*ir.Type, len(te.Tuple))
		for i, el := range te.Tuple {
			t, err := lowerTypeExpr(el, at)
			if err != nil {
				return nil, err
			}
			elems[i] = t
		}
		return ir.Tuple(elems...), nil

	case te.Fields != nil:
		// Field order is kept as written, but ir.Type.Equal compares records by
		// field set, so a declared {b: Text, a: Int} still matches an inferred
		// {a: Int, b: Text}. The parser has already refused duplicates.
		fields := make([]ir.Field, len(te.Fields))
		for i, f := range te.Fields {
			t, err := lowerTypeExpr(f.Type, at)
			if err != nil {
				return nil, err
			}
			fields[i] = ir.Field{Name: f.Name, Type: t}
		}
		return ir.Record(fields...), nil
	}

	if mk, ok := scalarTypes[te.Name]; ok {
		if len(te.Args) > 0 {
			return nil, &ResolveError{Pos: at,
				Msg: fmt.Sprintf("%s takes no type arguments", te.Name)}
		}
		return mk(), nil
	}

	arity, ok := genericArity[te.Name]
	if !ok {
		return nil, &ResolveError{Pos: at, Msg: fmt.Sprintf(
			"unknown type %q; write one of %s", te.Name, knownTypeNames())}
	}
	if len(te.Args) != arity {
		return nil, &ResolveError{Pos: at, Msg: fmt.Sprintf(
			"%s takes %d type argument(s), got %d", te.Name, arity, len(te.Args))}
	}

	args := make([]*ir.Type, len(te.Args))
	for i, a := range te.Args {
		t, err := lowerTypeExpr(a, at)
		if err != nil {
			return nil, err
		}
		args[i] = t
	}

	switch te.Name {
	case "List":
		return ir.List(args[0]), nil
	case "Grid":
		return ir.Grid(args[0]), nil
	case "Sparse":
		return ir.Sparse(args[0]), nil
	case "Set":
		if !ir.Keyable(args[0]) {
			return nil, &ResolveError{Pos: at, Msg: fmt.Sprintf(
				"Set elements must be keyable (Int, Text, or a tuple of them), got %s", args[0])}
		}
		return ir.Set(args[0]), nil
	case "Graph":
		// Same rule as a Set element and as Explore's state: the visited set is
		// what makes a traversal terminate, and an unkeyable node could not be
		// recognized on a second visit.
		if !ir.Keyable(args[0]) {
			return nil, &ResolveError{Pos: at, Msg: fmt.Sprintf(
				"Graph nodes must be keyable (Int, Text, or a tuple/record of them), got %s", args[0])}
		}
		return ir.Graph(args[0]), nil
	case "Map":
		if !ir.Keyable(args[0]) {
			return nil, &ResolveError{Pos: at, Msg: fmt.Sprintf(
				"Map keys must be keyable (Int, Text, or a tuple of them), got %s", args[0])}
		}
		return ir.Map(args[0], args[1]), nil
	}
	return nil, &ResolveError{Pos: at, Msg: fmt.Sprintf("unknown type %q", te.Name)}
}

// knownTypeNames lists the writable type names, for the unknown-type message.
func knownTypeNames() string {
	return strings.Join([]string{
		"Int", "Float", "Text", "Bool",
		"List<T>", "Set<T>", "Grid<T>", "Sparse<T>", "Graph<K>", "Map<K,V>",
		"(A, B)", "{a: Int}",
	}, ", ")
}

// lowerLambdaType lowers a lambda parameter's declared type to its parameter
// types and result type.
func lowerLambdaType(lt *ast.LambdaType, pos token.Position) (params []*ir.Type, result *ir.Type, err error) {
	if len(lt.Params) == 0 {
		return nil, nil, &ResolveError{Pos: pos,
			Msg: "a lambda parameter needs at least one argument type, e.g. (Int) -> Bool"}
	}
	params = make([]*ir.Type, len(lt.Params))
	for i, p := range lt.Params {
		t, err := lowerTypeExpr(p, pos)
		if err != nil {
			return nil, nil, err
		}
		params[i] = t
	}
	result, err = lowerTypeExpr(lt.Result, pos)
	if err != nil {
		return nil, nil, err
	}
	return params, result, nil
}

// TypeString renders a written type back to source form. Exported for tooling
// (the language server shows a Shikigami's declared types on hover).
func TypeString(te *ast.TypeExpr) string { return typeExprString(te) }

// typeExprString renders a written type back to source form, for messages.
func typeExprString(te *ast.TypeExpr) string {
	switch {
	case te == nil:
		return "<none>"
	case te.Lambda != nil:
		parts := make([]string, len(te.Lambda.Params))
		for i, p := range te.Lambda.Params {
			parts[i] = typeExprString(p)
		}
		return "(" + strings.Join(parts, ", ") + ") -> " + typeExprString(te.Lambda.Result)
	case te.Tuple != nil:
		parts := make([]string, len(te.Tuple))
		for i, p := range te.Tuple {
			parts[i] = typeExprString(p)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case te.Fields != nil:
		// Written the way ir.Type.String() prints a record, so a declared type
		// and the type it is compared against read identically in a message.
		parts := make([]string, len(te.Fields))
		for i, f := range te.Fields {
			parts[i] = f.Name + ":" + typeExprString(f.Type)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case len(te.Args) > 0:
		parts := make([]string, len(te.Args))
		for i, a := range te.Args {
			parts[i] = typeExprString(a)
		}
		return te.Name + "<" + strings.Join(parts, ", ") + ">"
	default:
		return te.Name
	}
}
