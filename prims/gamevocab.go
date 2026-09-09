package prims

import (
	"fmt"
	"strconv"
	"strings"

	"domain/ast"
	"domain/eval"
	"domain/ir"
	"domain/token"
	"domain/typecheck"
)

// The vocabulary the `Game Dev` scope adds to Core.
//
// It is deliberately small. Almost everything a game needs it already has —
// grids, records, lists, the searches, the render builtins — and what is left
// is the handful of things a program that runs forever needs and a program
// that answers a question does not.

// fromJSON — `Channeled Energy: Convert From JSON`, with the shape declared.
//
// Decoding is not symmetrical with encoding. `tojson` is a function of its
// value; this needs the program to say what it expects, because there is no
// dynamic value in this language to decode onto and then look at. So the type
// is written, and what comes back is that type or an error saying why not.
var fromJSON = &Primitive{
	ID:      "Convert From JSON",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "JSON") && hasWord(op, "From") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil || in.Kind != ir.KText {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
				"Convert From JSON reads Text, got %s", in)}
		}
		want, given, err := args.DeclaredType("Into", pos)
		if err != nil {
			return nil, err
		}
		if !given {
			return nil, &ResolveError{Pos: pos, Msg: "Convert From JSON needs an `Into:` type — " +
				"a document can be any shape, so the program has to say which one it expects"}
		}
		return &ir.Node{
			Prim: "Convert From JSON", In: in, Out: want,
			Display: "Convert From JSON -> " + want.String(),
			Meta:    map[string]any{"into": want},
			Pos:     pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				text, ok := v.(string)
				if !ok {
					return nil, runtimeErr("Convert From JSON", pos, "expected Text, got %s", ir.DescribeValue(v))
				}
				out, err := ir.FromJSON(text, want)
				if err != nil {
					return nil, runtimeErr("Convert From JSON", pos, "%v", err)
				}
				return out, nil
			},
		}, nil
	},
}

// toJSON — `Channeled Energy: Convert To JSON`. The value decides the shape,
// so there is nothing to declare.
var toJSON = &Primitive{
	ID:      "Convert To JSON",
	Keyword: "Channeled Energy",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "JSON") && hasWord(op, "To") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Convert To JSON has no value to write"}
		}
		return &ir.Node{
			Prim: "Convert To JSON", In: in, Out: ir.Text(),
			Display: "Convert To JSON", Pos: pos,
			Eval: func(_ *ir.Context, v ir.Value) (ir.Value, error) {
				s, err := ir.ToJSON(v)
				if err != nil {
					return nil, runtimeErr("Convert To JSON", pos, "%v", err)
				}
				return s, nil
			},
		}, nil
	},
}

// quit — `Simple Domain: Quit`, optionally `Using:` a predicate.
//
// A game has no natural end: nothing runs out, and the loop is driven from
// outside. This is how a program says it is finished — the snake hit itself,
// the player pressed q, the last word was guessed.
//
// It is a passthrough. The world it was given is the world it hands back, so
// the Part it sits in still satisfies its contract and the value that reaches
// `Part Ending:` is the one the game ended on.
var quit = &Primitive{
	ID:      "Quit",
	Keyword: "Simple Domain",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Quit") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Quit has no value to pass through"}
		}
		// The unconditional form is the common one; a predicate makes it
		// "stop when this is true", which saves wrapping the whole body in a
		// conditional that has to produce the world either way.
		lam, hasLam := args.Lambda("Using")
		if hasLam {
			t, err := typecheck.LambdaType(lam, append([]*ir.Type{in}, ambientTypes()...)...)
			if err != nil {
				return nil, &ResolveError{Pos: pos, Msg: "Quit: " + err.Error()}
			}
			if t == nil || t.Kind != ir.KBool {
				return nil, &ResolveError{Pos: pos, Msg: "Quit's Using: lambda must answer true or false, got " + t.String()}
			}
		}
		display := "Quit"
		if hasLam {
			display = "Quit when"
		}
		return &ir.Node{
			Prim: "Quit", In: in, Out: in, Display: display,
			Meta: map[string]any{"lambda": lam},
			Pos:  pos,
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				if !hasLam {
					ctx.Quit = true
					return v, nil
				}
				r, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{in}, ambientTypes()...),
					append([]ir.Value{v}, ambientArgs()...)...)
				if err != nil {
					return nil, runtimeErr("Quit", pos, "%v", err)
				}
				b, ok := r.(bool)
				if !ok {
					return nil, runtimeErr("Quit", pos, "expected true or false, got %s", ir.DescribeValue(r))
				}
				if b {
					ctx.Quit = true
				}
				return v, nil
			},
		}, nil
	},
}

// beep — `Simple Domain: Beep`, optionally `Using:` a predicate.
//
// A frame is a picture; a hit landing, a piece coming to rest, a wrong guess
// are moments, and a picture cannot carry one — style() already covers what a
// frame can say, and a moment is not a frame. This is how a game asks for the
// oldest out-of-band signal a terminal has: the bell.
//
// It is a **passthrough**, on the same shape as Quit: the value it was given
// is the value it hands back, so it drops into a pipeline anywhere a body is
// already threading a value through, not only where that value is the world.
var beep = &Primitive{
	ID:      "Beep",
	Keyword: "Simple Domain",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Beep") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Beep has no value to pass through"}
		}
		// The unconditional form is the common one; a predicate makes it "beep
		// when this is true", which saves wrapping the whole body in a
		// conditional that has to produce the value either way.
		lam, hasLam := args.Lambda("Using")
		if hasLam {
			t, err := typecheck.LambdaType(lam, append([]*ir.Type{in}, ambientTypes()...)...)
			if err != nil {
				return nil, &ResolveError{Pos: pos, Msg: "Beep: " + err.Error()}
			}
			if t == nil || t.Kind != ir.KBool {
				return nil, &ResolveError{Pos: pos, Msg: "Beep's Using: lambda must answer true or false, got " + t.String()}
			}
		}
		display := "Beep"
		if hasLam {
			display = "Beep when"
		}
		return &ir.Node{
			Prim: "Beep", In: in, Out: in, Display: display,
			Meta: map[string]any{"lambda": lam},
			Pos:  pos,
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				if !hasLam {
					ctx.Beep = true
					return v, nil
				}
				r, err := eval.EvalLambdaTyped(lam, append([]*ir.Type{in}, ambientTypes()...),
					append([]ir.Value{v}, ambientArgs()...)...)
				if err != nil {
					return nil, runtimeErr("Beep", pos, "%v", err)
				}
				b, ok := r.(bool)
				if !ok {
					return nil, runtimeErr("Beep", pos, "expected true or false, got %s", ir.DescribeValue(r))
				}
				if b {
					ctx.Beep = true
				}
				return v, nil
			},
		}, nil
	},
}

// load — `Domain Expansion: Load`, reading persisted state back in.
//
// Unlike Request, this needs no round trip: local disk answers in the same
// tick it was asked, so there is no Reply to write and nothing to tag. What
// plays Request's asymmetry's role here is Default: — the value Load
// produces when there is nothing to read, which is also what a replayed run
// always gets unless its script says `load <json>` (game/replay.go).
var load = &Primitive{
	ID:      "Load",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Load") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Load has no value to run its Path: and Default: lambdas against"}
		}
		path, hasPath := args.Lambda("Path")
		if !hasPath {
			return nil, &ResolveError{Pos: pos, Msg: "Load needs a `Path:` — a lambda over the world, e.g. " +
				`Path: (w) -> "save.json"`}
		}
		if err := requireTextLambda(path, in, "Path", pos); err != nil {
			return nil, err
		}
		into, given, err := args.DeclaredType("Into", pos)
		if err != nil {
			return nil, err
		}
		if !given {
			return nil, &ResolveError{Pos: pos, Msg: "Load needs an `Into:` type — a save file can be any shape, " +
				"so the program has to say which one it expects"}
		}
		def, hasDef := args.Lambda("Default")
		if !hasDef {
			return nil, &ResolveError{Pos: pos, Msg: "Load needs a `Default:` — a lambda over the world producing " +
				"the " + into.String() + " a fresh game (or a replayed run with no `load` line) starts with"}
		}
		dt, err := typecheck.LambdaType(def, in)
		if err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Load Default: " + err.Error()}
		}
		if dt == nil || !dt.Equal(into) {
			return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf("Load's Default: must produce %s, got %s", into, dt)}
		}
		return &ir.Node{
			Prim: "Load", In: in, Out: into,
			Display: "Load -> " + into.String(),
			Meta:    map[string]any{"into": into, "pathLambda": path, "defaultLambda": def},
			Pos:     pos,
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				p, err := eval.EvalLambdaTyped(path, []*ir.Type{in}, v)
				if err != nil {
					return nil, runtimeErr("Load", pos, "Path: %v", err)
				}
				if ctx.Load != nil {
					if text, ok := ctx.Load(fmt.Sprint(p)); ok {
						if out, err := ir.FromJSON(text, into); err == nil {
							return out, nil
						}
						// A save that doesn't parse is exactly what a
						// missing one looks like: the game starts fresh
						// rather than crashing over a corrupt file.
					}
				}
				return eval.EvalLambdaTyped(def, []*ir.Type{in}, v)
			},
		}, nil
	},
}

// save — `Domain Expansion: Save`, writing state that outlives the run.
//
// A passthrough, on Request's and Quit's shape: the value it was given is
// the value it hands back. Nothing answers it — there is no Reply, because
// nothing needs to come back — so it is simpler than Request in exactly the
// way a fire-and-forget write is simpler than a question.
var save = &Primitive{
	ID:      "Save",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Save") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Save has no value to pass through"}
		}
		path, hasPath := args.Lambda("Path")
		if !hasPath {
			return nil, &ResolveError{Pos: pos, Msg: "Save needs a `Path:` — a lambda over the world, e.g. " +
				`Path: (w) -> "save.json"`}
		}
		if err := requireTextLambda(path, in, "Path", pos); err != nil {
			return nil, err
		}
		value, hasValue := args.Lambda("Value")
		if !hasValue {
			return nil, &ResolveError{Pos: pos, Msg: "Save needs a `Value:` — a lambda over the world naming what " +
				`to write, e.g. Value: (w) -> w.highScore`}
		}
		if _, err := typecheck.LambdaType(value, in); err != nil {
			return nil, &ResolveError{Pos: pos, Msg: "Save Value: " + err.Error()}
		}
		return &ir.Node{
			Prim: "Save", In: in, Out: in,
			Display: "Save", Pos: pos,
			Meta: map[string]any{"pathLambda": path, "valueLambda": value},
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				if ctx.Save == nil {
					return v, nil
				}
				p, err := eval.EvalLambdaTyped(path, []*ir.Type{in}, v)
				if err != nil {
					return nil, runtimeErr("Save", pos, "Path: %v", err)
				}
				val, err := eval.EvalLambdaTyped(value, []*ir.Type{in}, v)
				if err != nil {
					return nil, runtimeErr("Save", pos, "Value: %v", err)
				}
				text, err := ir.ToJSON(val)
				if err != nil {
					return nil, runtimeErr("Save", pos, "Value: %v", err)
				}
				ctx.Save(fmt.Sprint(p), text)
				return v, nil
			},
		}, nil
	},
}

// request — `Domain Expansion: Request`, and the reply that comes back later.
//
// A game talking to a server cannot wait for it. A round trip is a hundred
// milliseconds on a good day, and a frame is sixteen: a blocking request would
// freeze the picture every time it was made, and a language that makes that
// easy to write is one that makes it hard to notice. So a Request **fires and
// returns the world unchanged**, and the answer arrives later at a
// `Part Reply "<tag>":`.
//
// The tag is a literal, which is what lets the two halves be checked against
// each other at resolve time: a Request nothing handles, and a Reply nothing
// fires, are each a mistake that would otherwise show up as a game that
// silently does nothing.
var request = &Primitive{
	ID:      "Request",
	Keyword: "Domain Expansion",
	Match:   func(op *ast.Operation) bool { return hasWord(op, "Request") },
	Build: func(op *ast.Operation, args ArgSet, in *ir.Type, pos token.Position) (*ir.Node, error) {
		if in == nil {
			return nil, &ResolveError{Pos: pos, Msg: "Request has no value to pass through"}
		}
		url, hasURL := args.Lambda("Url")
		if !hasURL {
			return nil, &ResolveError{Pos: pos, Msg: "Request needs a `Url:` — a lambda over the world, e.g. " +
				`Url: (w) -> "https://…/room/" + w.room`}
		}
		if err := requireTextLambda(url, in, "Url", pos); err != nil {
			return nil, err
		}
		tag, err := requestTag(args, pos)
		if err != nil {
			return nil, err
		}
		into, given, err := args.DeclaredType("Into", pos)
		if err != nil {
			return nil, err
		}
		if !given {
			return nil, &ResolveError{Pos: pos, Msg: "Request needs an `Into:` type — an answer can be any " +
				"shape, so the program has to say which one it expects"}
		}
		spec := &ir.RequestSpec{Tag: tag, Into: into, Reply: ir.ReplyType(into), Method: "GET", TimeoutMS: 5000}
		// The pre-scan already made a spec for this tag so that `Part Reply`
		// could be typed against it. This is the same request, seen properly:
		// fill that one in rather than making a second, or the Part and the
		// host would be looking at different objects.
		if known, ok := args.requestSpec(tag); ok {
			*known = *spec
			spec = known
		}
		if m, ok := args.Text("Method"); ok {
			spec.Method = strings.ToUpper(strings.TrimSpace(m))
			if !validMethod(spec.Method) {
				return nil, &ResolveError{Pos: pos, Msg: fmt.Sprintf(
					"Request: %q is not a method; the ones it makes are GET, POST, PUT, PATCH and DELETE", m)}
			}
		}
		if ms, ok := args.Int("Timeout"); ok {
			if ms <= 0 {
				return nil, &ResolveError{Pos: pos, Msg: "Request: `Timeout:` is milliseconds and must be above zero"}
			}
			spec.TimeoutMS = ms
		}
		body, hasBody := args.Lambda("Body")
		if hasBody {
			if err := requireTextLambda(body, in, "Body", pos); err != nil {
				return nil, err
			}
		}

		return &ir.Node{
			Prim: "Request", In: in, Out: in,
			Display: "Request " + strconv.Quote(tag),
			// Not swappable: it names a thing to do, not a result to compute,
			// which is the same reason a foreign block is left alone.
			Meta: map[string]any{
				"request": spec, "urlLambda": url, "bodyLambda": body,
			},
			Pos: pos,
			Eval: func(ctx *ir.Context, v ir.Value) (ir.Value, error) {
				u, err := eval.EvalLambdaTyped(url, []*ir.Type{in}, v)
				if err != nil {
					return nil, runtimeErr("Request", pos, "Url: %v", err)
				}
				call := ir.RequestCall{Spec: spec, URL: fmt.Sprint(u)}
				if hasBody {
					b, err := eval.EvalLambdaTyped(body, []*ir.Type{in}, v)
					if err != nil {
						return nil, runtimeErr("Request", pos, "Body: %v", err)
					}
					call.Body, call.HasBody = fmt.Sprint(b), true
				}
				// The host does the sending. A Request from a program running
				// under a host that cannot make one is not an error: it simply
				// never answers, which is what a server that is not there
				// looks like anyway.
				if ctx.Requests != nil {
					ctx.Requests(call)
				}
				return v, nil
			},
		}, nil
	},
}

// requestTag reads and checks the `As:` tag.
func requestTag(args ArgSet, pos token.Position) (string, error) {
	tag, ok := args.Text("As")
	if !ok {
		return "", &ResolveError{Pos: pos, Msg: `Request needs an ` + "`As:`" + ` tag naming the reply, e.g. As: "scores"`}
	}
	if strings.TrimSpace(tag) == "" {
		return "", &ResolveError{Pos: pos, Msg: "Request: the `As:` tag cannot be empty"}
	}
	return tag, nil
}

func validMethod(m string) bool {
	switch m {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// requireTextLambda checks one of Request's Text-valued lambdas.
func requireTextLambda(lam *ast.Lambda, in *ir.Type, name string, pos token.Position) error {
	t, err := typecheck.LambdaType(lam, in)
	if err != nil {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf("Request %s: %v", name, err)}
	}
	if t == nil || t.Kind != ir.KText {
		return &ResolveError{Pos: pos, Msg: fmt.Sprintf("Request's `%s:` must produce Text, got %s", name, t)}
	}
	return nil
}

// pending — `pending("tag")`, whether a request is still out.
//
// One request is in flight per tag and a new one supersedes the old, so a
// reply can be lost. That is the right trade for a position poll and the wrong
// one for a move nobody may drop, which is why the loss is visible: a program
// that must not lose one asks first.
var pendingBuiltin = "pending"

// collectRequests finds every request a program fires, before any Part is
// resolved.
//
// It has to run early and it has to run over the *whole* program, because the
// two halves of an asynchronous exchange are written apart: a
// `Part Reply "scores":` is typed against the `Request` tagged "scores", and
// the two may be in either order — Parts are not resolved in the order they
// were written, so waiting to meet it is not an option.
//
// It runs after inference, so a `Request` written without its keyword has one
// by now, and reads only what it must: the tag and the declared shape. Whether
// the rest of the statement is well formed is the primitive's business, and
// saying so twice would mean two messages to keep in step.
func (r *resolver) collectRequests(stmts []*ast.Statement) error {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		if err := r.collectRequests(stmt.Block); err != nil {
			return err
		}
		if stmt.Keyword != "Domain Expansion" || stmt.Op == nil || !hasWord(stmt.Op, "Request") {
			continue
		}
		tag, ok := argText(stmt, "As")
		if !ok || strings.TrimSpace(tag) == "" {
			continue // the primitive reports it, with the better message
		}
		te, ok := argType(stmt, "Into")
		if !ok {
			continue // likewise
		}
		into, err := lowerTypeExpr(te, stmt.Pos)
		if err != nil {
			return err
		}
		if prev, taken := r.requests[tag]; taken && !prev.Into.Equal(into) {
			return &ResolveError{Pos: stmt.Pos, Msg: fmt.Sprintf(
				"two requests are tagged %q but expect different shapes (%s and %s); "+
					"one `Part Reply %q:` cannot be typed against both", tag, prev.Into, into, tag)}
		}
		r.requests[tag] = &ir.RequestSpec{Tag: tag, Into: into, Reply: ir.ReplyType(into)}
	}
	return nil
}

// argText and argType read one named argument off a statement without the
// ArgSet machinery, which is not built until the statement is being resolved.
// They deliberately do not mark the argument used: the primitive will read it
// again, and that read is the one the unused-argument lint should see.
func argText(stmt *ast.Statement, name string) (string, bool) {
	for _, a := range stmt.Args {
		if a.Name == name {
			if s, ok := a.Value.(ast.StringArg); ok {
				return s.Value, true
			}
		}
	}
	return "", false
}

func argType(stmt *ast.Statement, name string) (*ast.TypeExpr, bool) {
	for _, a := range stmt.Args {
		if a.Name == name {
			if t, ok := a.Value.(ast.TypeArg); ok {
				return t.Type, true
			}
		}
	}
	return nil, false
}
