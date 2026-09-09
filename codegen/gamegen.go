// Compiling a game.
//
// A `Game Dev` program is not a chain of nodes, so it cannot be emitted by
// walking one. Its Parts are a world, the events that change it, and a frame
// drawn from it, and what compiles them is the same trick blockgen.go already
// uses for an indented `Using:` body: each body is emitted into a **top-level
// function** of its own, and what is left over — the loop that calls them — is
// the runtime in gameruntime.go.
//
// That is the whole shape of this file. There is no second expression
// compiler and no second set of primitive lowerings: a Part's body is an
// ordinary pipeline, emitted by emitSequence, with the role's payload in
// scope as ordinary `Consider` bindings. What the file adds is the glue the
// runtime calls into — dmBegin, dmDrawView, dmOnRun, dmTimerRun,
// dmReplyDeliver, dmFinish — and the two tables (the input specs, the timer
// periods) that let one runtime drive any program.
package codegen

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/prims"
)

// gameScope is the Innate Domain this file is the backend for.
const gameScope = "Game Dev"

func init() { RegisterScope(gameScope) }

// gamePart is one resolved Part: its role, whatever it was written with, and
// the function its body was emitted as.
type gamePart struct {
	role   string
	label  string // Part On "key up": — the spec; Part Reply "scores": — the tag
	period int64  // Part Every 120: — milliseconds
	nodes  []*ir.Node
	binds  []prims.RoleBind
	fn     string // the emitted function's name
}

// gameProgram is a resolved game, indexed by role — the compiled mirror of
// game/host.go's `game`.
type gameProgram struct {
	setup  []*ir.Node // the top-level declarations, run before anything else
	world  *gamePart
	start  *gamePart
	draw   *gamePart
	ending *gamePart
	on     []*gamePart
	every  []*gamePart
	reply  []*gamePart
	specs  map[string]*ir.RequestSpec
	tags   []string // the reply tags, sorted, so the emitted switch is stable
	typ    *ir.Type // the world's type
	goType string   // and its Go representation
}

// emitGame compiles a whole game: the Part bodies as functions, the tables the
// runtime dispatches through, and a main() that runs the declarations and then
// hands over to the loop.
func (g *gen) emitGame(p *ir.Pipeline) error {
	prog, err := readGame(p)
	if err != nil {
		return err
	}
	if prog.goType, err = g.goType(prog.typ); err != nil {
		return err
	}
	// The runtime's own state, and the world it drives. The world is declared
	// here rather than in gameruntime.go because its type is the program's.
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmRand", declRand)
	g.helper("dmGameWorld", "var dmWorld "+prog.goType)
	g.helper("dmGameState", declGameState, "sync", "os", "fmt")
	g.helper("dmGameEvents", declGameEvents, "sort", "strings")
	g.helper("dmGameReplay", declGameReplay, "bufio", "fmt", "os", "strconv", "strings")
	g.helper("dmGameNet", declGameNet, "fmt", "io", "net/http", "strings")
	g.helper("dmGameANSI", declGameANSI, "strconv", "strings")
	g.helper("dmGamePlay", declGamePlay, "net/http", "os", "time",
		"charm.land/bubbletea/v2", "github.com/charmbracelet/x/term")

	if err := g.emitGameParts(prog); err != nil {
		return err
	}
	g.emitGameTables(prog)
	if err := g.emitGameGlue(prog); err != nil {
		return err
	}

	// The declarations run in main, before the loop: a `Cursed Object` is what
	// the world is built from, so it has to hold a value by the time
	// `Part World:` reads it.
	if _, err := g.emitSequence(prog.setup, ""); err != nil {
		return err
	}
	g.wl("if dmPlayable() {")
	g.in()
	g.wl("dmPlay()")
	g.out()
	g.wl("} else {")
	g.in()
	g.wl("dmReplayRun()")
	g.out()
	g.wl("}")
	return nil
}

// readGame reads the roles off a resolved pipeline. It is the host's gameOf (game/host.go),
// with the same reasoning: a Part is already a node with its body in Meta, and
// a second place to keep them could disagree with the first.
func readGame(p *ir.Pipeline) (*gameProgram, error) {
	prog := &gameProgram{specs: map[string]*ir.RequestSpec{}}
	for _, n := range p.Nodes {
		if n == nil {
			continue
		}
		if n.Prim != "Part" {
			prog.setup = append(prog.setup, n)
			continue
		}
		role, _ := n.Meta["role"].(string)
		part := &gamePart{role: role}
		part.label, _ = n.Meta["label"].(string)
		part.nodes, _ = n.Meta["nodes"].([]*ir.Node)
		part.binds, _ = n.Meta["binds"].([]prims.RoleBind)
		if ms, ok := n.Meta["number"].(int64); ok {
			part.period = ms
		}
		switch role {
		case "World":
			prog.world = part
			prog.typ, _ = n.Meta["bodyType"].(*ir.Type)
		case "Start":
			prog.start = part
		case "Draw":
			prog.draw = part
		case "Ending":
			prog.ending = part
		case "On":
			prog.on = append(prog.on, part)
		case "Every":
			prog.every = append(prog.every, part)
		case "Reply":
			prog.reply = append(prog.reply, part)
		case "Entity":
			// A definition, lowered to a Shikigami: it has no node here.
		default:
			return nil, fmt.Errorf("internal: this backend does not know how to compile a Part %s", role)
		}
	}
	if prog.world == nil || prog.draw == nil || prog.typ == nil {
		return nil, fmt.Errorf("internal: a game needs a Part World and a Part Draw")
	}
	// Timers fire in a fixed order when several fall due at once — earliest
	// period first, then as written — so a replayed tick is one answer rather
	// than whichever order a map happened to give.
	slices.SortStableFunc(prog.every, func(a, b *gamePart) int {
		return int(a.period - b.period)
	})
	collectRequestSpecs(p.Nodes, prog.specs)
	for tag := range prog.specs {
		prog.tags = append(prog.tags, tag)
	}
	slices.Sort(prog.tags)
	return prog, nil
}

// collectRequestSpecs walks a pipeline for the requests it can fire. A Request
// may sit anywhere a statement may — inside a Start, a handler, a timer, a
// Shikigami inlined into any of them — so this recurses.
func collectRequestSpecs(nodes []*ir.Node, out map[string]*ir.RequestSpec) {
	for _, n := range nodes {
		if n == nil || n.Meta == nil {
			continue
		}
		if spec, ok := n.Meta["request"].(*ir.RequestSpec); ok && spec != nil {
			out[spec.Tag] = spec
		}
		if sub, ok := n.Meta["nodes"].([]*ir.Node); ok {
			collectRequestSpecs(sub, out)
		}
		if subs, ok := n.Meta[ir.MetaBindNodes].([][]*ir.Node); ok {
			for _, s := range subs {
				collectRequestSpecs(s, out)
			}
		}
	}
}

// emitGameParts compiles every Part body into a function of its own.
func (g *gen) emitGameParts(prog *gameProgram) error {
	// The world's body opens with the empty world — a Record with no fields —
	// and says what the world actually is. See prims/scope_gamedev.go for why
	// it starts from {} rather than from nothing.
	if err := g.partFunc(prog, prog.world, ir.Record(), prog.typ); err != nil {
		return err
	}
	for _, p := range []*gamePart{prog.start, prog.ending} {
		if p == nil {
			continue
		}
		// Ending may produce anything; only its Reveal is observable, so its
		// result is discarded at the call site.
		out := prog.typ
		if p.role == "Ending" {
			out = nil
		}
		if err := g.partFunc(prog, p, prog.typ, out); err != nil {
			return err
		}
	}
	if err := g.partFunc(prog, prog.draw, prog.typ, ir.View()); err != nil {
		return err
	}
	for _, list := range [][]*gamePart{prog.on, prog.every, prog.reply} {
		for _, p := range list {
			if err := g.partFunc(prog, p, prog.typ, prog.typ); err != nil {
				return err
			}
		}
	}
	return nil
}

// partFunc emits one Part body as a top-level function.
//
// It is blockgen.go's blockFunc with the pieces a Part needs instead of the
// ones a lambda does: the role's payload becomes named parameters bound as
// `Consider` bindings, so every lambda in the body — including inside any
// Shikigami inlined there — reads them by name and is otherwise exactly as
// written. That is the same reason the interpreter binds them rather than
// appending lambda parameters; see prims.RoleBind.
func (g *gen) partFunc(prog *gameProgram, part *gamePart, in, out *ir.Type) error {
	inGo, err := g.goType(in)
	if err != nil {
		return err
	}
	outGo := ""
	if out != nil {
		if outGo, err = g.goType(out); err != nil {
			return err
		}
	}
	name := g.fresh("dmPart")
	param := g.fresh("pw")

	// The body is emitted into a buffer of its own, exactly as a block body
	// is: g.main holds the statements of main(), and these belong in the
	// function instead.
	savedMain, savedIndent, savedLabel := g.main, g.indent, g.partLabel
	savedBinds := g.bindNames
	g.main, g.indent, g.partLabel = bytes.Buffer{}, 0, part.label

	sig := param + " " + inGo
	binds := make(exprEnv, len(savedBinds)+len(part.binds))
	for k, v := range savedBinds {
		binds[k] = v
	}
	for _, b := range part.binds {
		p := g.fresh("pb")
		bindGo, terr := g.goType(b.Type)
		if terr != nil {
			g.main, g.indent, g.partLabel, g.bindNames = savedMain, savedIndent, savedLabel, savedBinds
			return terr
		}
		sig += fmt.Sprintf(", %s %s", p, bindGo)
		binds[b.Name] = exprBinding{expr: p, typ: b.Type}
	}
	g.bindNames = binds

	cur, cerr := g.emitSequence(part.nodes, param)
	body := g.main.String()
	g.main, g.indent, g.partLabel, g.bindNames = savedMain, savedIndent, savedLabel, savedBinds
	if cerr != nil {
		return cerr
	}

	var decl bytes.Buffer
	if outGo == "" {
		fmt.Fprintf(&decl, "func %s(%s) {\n%s", name, sig, body)
		// A body whose last stage is a Reveal produces nothing to discard; one
		// that threads a value leaves a Go variable nothing reads.
		if cur != "" {
			fmt.Fprintf(&decl, "\t_ = %s\n", cur)
		}
		decl.WriteString("}\n")
	} else {
		fmt.Fprintf(&decl, "func %s(%s) %s {\n%s\treturn %s\n}\n", name, sig, outGo, body, cur)
	}
	g.decls = append(g.decls, decl.String())
	part.fn = name
	return nil
}

// emitGameTables writes the two lists the runtime dispatches through. They are
// data rather than code so that one runtime drives any program: what changes
// between games is which specs exist and how fast the timers run.
func (g *gen) emitGameTables(prog *gameProgram) {
	specs := make([]string, len(prog.on))
	for i, p := range prog.on {
		specs[i] = goStr(p.label)
	}
	g.helper("dmOnSpecs", "var dmOnSpecs = []string{"+strings.Join(specs, ", ")+"}")

	periods := make([]string, len(prog.every))
	for i, p := range prog.every {
		periods[i] = fmt.Sprintf("%d", p.period)
	}
	g.helper("dmTimerPeriods", "var dmTimerPeriods = []int64{"+strings.Join(periods, ", ")+"}")
}

// emitGameGlue writes the six functions the runtime calls into.
func (g *gen) emitGameGlue(prog *gameProgram) error {
	zero, err := g.zeroExpr(ir.Record())
	if err != nil {
		return err
	}
	var b strings.Builder

	// dmBegin: build the world, then run Start.
	fmt.Fprintf(&b, "func dmBegin() {\n\tdmWorld = %s(%s)\n", prog.world.fn, zero)
	if prog.start != nil {
		fmt.Fprintf(&b, "\tdmWorld = %s(dmWorld)\n", prog.start.fn)
	}
	b.WriteString("}\n")

	// dmFinish: whatever Part Ending Reveals is the program's output.
	b.WriteString("\nfunc dmFinish() {\n")
	if prog.ending != nil {
		fmt.Fprintf(&b, "\t%s(dmWorld)\n", prog.ending.fn)
	}
	b.WriteString("}\n")

	fmt.Fprintf(&b, "\nfunc dmDrawView() dmView { return %s(dmWorld) }\n", prog.draw.fn)

	// dmOnRun and dmTimerRun: dispatch by index into the tables above.
	b.WriteString("\nfunc dmOnRun(i int, key string, w, h int64) {\n\tswitch i {\n")
	for i, p := range prog.on {
		fmt.Fprintf(&b, "\tcase %d:\n\t\tdmWorld = %s(dmWorld%s)\n", i, p.fn, bindArgs(p.binds))
	}
	b.WriteString("\t}\n}\n")

	b.WriteString("\nfunc dmTimerRun(i int) {\n\tswitch i {\n")
	for i, p := range prog.every {
		fmt.Fprintf(&b, "\tcase %d:\n\t\tdmWorld = %s(dmWorld)\n", i, p.fn)
	}
	b.WriteString("\t}\n}\n")

	// dmHasReply: whether a tag is one this program answers. The script reader
	// and the network path both ask before delivering.
	b.WriteString("\nfunc dmHasReply(tag string) bool {\n\tswitch tag {\n")
	if len(prog.reply) > 0 {
		tags := make([]string, len(prog.reply))
		for i, p := range prog.reply {
			tags[i] = goStr(p.label)
		}
		slices.Sort(tags)
		fmt.Fprintf(&b, "\tcase %s:\n\t\treturn true\n", strings.Join(tags, ", "))
	}
	b.WriteString("\t}\n\treturn false\n}\n")

	deliver, err := g.replyDeliver(prog)
	if err != nil {
		return err
	}
	b.WriteString("\n" + deliver)
	g.decls = append(g.decls, b.String())
	return nil
}

// bindArgs is the payload a role's function takes, as arguments of dmOnRun.
// The names come from the resolver, so a role that gains a binding cannot be
// forgotten here: an unknown one is a compile-time gap rather than a wrong
// value quietly reaching a lambda.
func bindArgs(binds []prims.RoleBind) string {
	var sb strings.Builder
	for _, b := range binds {
		switch b.Name {
		case "key":
			sb.WriteString(", key")
		case "width":
			sb.WriteString(", w")
		case "height":
			sb.WriteString(", h")
		}
	}
	return sb.String()
}

// replyDeliver emits the decode-and-run switch for completed requests.
//
// The type is per tag, which is why this is generated rather than shared: a
// reply is {ok, error, value} where value is what the Request declared with
// `Into:`, so each tag decodes into a different Go type and calls a different
// function.
func (g *gen) replyDeliver(prog *gameProgram) (string, error) {
	var b strings.Builder
	b.WriteString("// dmReplyDeliver decodes an answer and runs the Part that was written\n" +
		"// for it. An error means the document did not fit the declared shape:\n" +
		"// the script reader reports it, the network path turns it into a\n" +
		"// failed reply, because a server sending nonsense is a server problem.\n" +
		"func dmReplyDeliver(tag string, ok bool, text string) error {\n\tswitch tag {\n")
	for _, part := range prog.reply {
		spec := prog.specs[part.label]
		if spec == nil {
			return "", fmt.Errorf("internal: nothing fires a request tagged %q", part.label)
		}
		replyGo, err := g.goType(spec.Reply)
		if err != nil {
			return "", err
		}
		decode, err := g.jsonInFunc(spec.Into)
		if err != nil {
			return "", err
		}
		zero, err := g.zeroExpr(spec.Into)
		if err != nil {
			return "", err
		}
		fields := replyFields(spec.Reply)
		fmt.Fprintf(&b, "\tcase %s:\n\t\tr := %s{%s: false, %s: text, %s: %s}\n",
			goStr(part.label), replyGo, fields.ok, fields.err, fields.value, zero)
		fmt.Fprintf(&b, `		if ok {
			raw, err := dmJSONParse(text)
			if err != nil {
				return err
			}
			v, err := %s(raw, "")
			if err != nil {
				return err
			}
			r = %s{%s: true, %s: "", %s: v}
		}
		dmWorld = %s(dmWorld, r)
`, decode, replyGo, fields.ok, fields.err, fields.value, part.fn)
	}
	b.WriteString("\t}\n\treturn nil\n}\n")
	return b.String(), nil
}

// replyFields is the Go names of {ok, error, value} on the generated struct.
// They come from ir.ReplyType by way of fieldName rather than being written
// out, so a rename on either side is a compile error here rather than a field
// quietly set on the wrong one.
type replyFieldNames struct{ ok, err, value string }

func replyFields(t *ir.Type) replyFieldNames {
	out := replyFieldNames{}
	for _, f := range t.Fields {
		switch f.Name {
		case "ok":
			out.ok = fieldName(f.Name)
		case "error":
			out.err = fieldName(f.Name)
		case "value":
			out.value = fieldName(f.Name)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// the Game Dev vocabulary (prims/gamevocab.go)
// ---------------------------------------------------------------------------

// emitQuit lowers `Simple Domain: Quit`. It is a passthrough: the world it was
// given is the world it hands back, so the Part it sits in still satisfies its
// contract and the value that reaches `Part Ending:` is the one the game ended
// on.
func (g *gen) emitQuit(n *ir.Node, in string) (string, error) {
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil {
		g.wl("dmQuit = true")
		return in, nil
	}
	g.bindAmbientParams(lam)
	cond, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: in, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("if %s {", cond)
	g.in()
	g.wl("dmQuit = true")
	g.out()
	g.wl("}")
	return in, nil
}

// emitBeep lowers `Simple Domain: Beep`. Same shape as emitQuit — a
// passthrough that sets a flag the runtime checks after the body returns,
// rather than doing anything itself, because ringing the bell is the
// runtime's business (dmBeep is consumed once per frame, in both the played
// loop and the replayed one, on the same terms game/play.go and
// game/replay.go check dmQuit).
func (g *gen) emitBeep(n *ir.Node, in string) (string, error) {
	lam, _ := n.Meta["lambda"].(*ast.Lambda)
	if lam == nil {
		g.wl("dmBeep = true")
		return in, nil
	}
	g.bindAmbientParams(lam)
	cond, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: in, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "lambda: %v", err)
	}
	g.wl("if %s {", cond)
	g.in()
	g.wl("dmBeep = true")
	g.out()
	g.wl("}")
	return in, nil
}

// emitLoad lowers `Domain Expansion: Load`. Local disk answers in the same
// tick it was asked, so — unlike Request — there is no Reply to write:
// dmLoad supplies the raw JSON text (a real file when played, a `load`
// script line's document when replayed, per declGameLoad below), decoded
// with the same machinery Convert From JSON already uses. Anything that
// keeps it from producing a value — no file, no script line, a document
// that doesn't fit Into: — runs Default: instead, which is why this is one
// function literal rather than a chain of Go's own `if`/`err` idiom: the
// three ways of getting nothing all fall through to the same one line.
func (g *gen) emitLoad(n *ir.Node, in string) (string, error) {
	into, _ := n.Meta["into"].(*ir.Type)
	if into == nil {
		return "", unsupported(n, "missing Into: type")
	}
	path, err := g.requestLambda(n, "pathLambda", in)
	if err != nil {
		return "", err
	}
	def, _ := n.Meta["defaultLambda"].(*ast.Lambda)
	if def == nil || len(def.Params) == 0 {
		return "", unsupported(n, "missing Default:")
	}
	decode, err := g.jsonInFunc(into)
	if err != nil {
		return "", err
	}
	goT, err := g.goType(into)
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	out := g.fresh("loaded")
	g.wl("%s := func() %s {", out, goT)
	g.in()
	text, gotText := g.fresh("text"), g.fresh("ok")
	g.wl("if %s, %s := dmLoad(%s); %s {", text, gotText, path, gotText)
	g.in()
	raw, e1 := g.fresh("raw"), g.fresh("err")
	g.wl("if %s, %s := dmJSONParse(%s); %s == nil {", raw, e1, text, e1)
	g.in()
	v, e2 := g.fresh("v"), g.fresh("err")
	g.wl(`if %s, %s := %s(%s, ""); %s == nil {`, v, e2, decode, raw, e2)
	g.in()
	g.wl("return %s", v)
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	g.out()
	g.wl("}")
	defExpr, _, err := g.compileExpr(def.Body, exprEnv{def.Params[0]: {expr: in, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "Default: %v", err)
	}
	g.wl("return %s", defExpr)
	g.out()
	g.wl("}()")
	return out, nil
}

// emitSave lowers `Domain Expansion: Save`. A passthrough: it fires
// dmSave with the encoded value and hands the world straight back, since
// nothing needs to come back the way a Request's answer does.
func (g *gen) emitSave(n *ir.Node, in string) (string, error) {
	path, err := g.requestLambda(n, "pathLambda", in)
	if err != nil {
		return "", err
	}
	lam, _ := n.Meta["valueLambda"].(*ast.Lambda)
	if lam == nil || len(lam.Params) == 0 {
		return "", unsupported(n, "missing Value:")
	}
	valExpr, valType, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: in, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "Value: %v", err)
	}
	fn, err := g.jsonFunc(valType)
	if err != nil {
		return "", unsupported(n, "Value: %v", err)
	}
	g.wl("dmSave(%s, %s(%s))", path, fn, valExpr)
	return in, nil
}

// emitRequest lowers `Domain Expansion: Request`. It fires and returns the
// world unchanged — the answer arrives later at a `Part Reply "<tag>":` — so
// what it emits is the URL, the body if there is one, and a queued call.
func (g *gen) emitRequest(n *ir.Node, in string) (string, error) {
	spec, _ := n.Meta["request"].(*ir.RequestSpec)
	if spec == nil {
		return "", unsupported(n, "missing request metadata")
	}
	url, err := g.requestLambda(n, "urlLambda", in)
	if err != nil {
		return "", err
	}
	body, hasBody := `""`, false
	// A Request with no `Body:` still carries the key, holding a typed nil —
	// so the presence test has to be on the pointer, not on the assertion.
	if lam, _ := n.Meta["bodyLambda"].(*ast.Lambda); lam != nil {
		if body, err = g.requestLambda(n, "bodyLambda", in); err != nil {
			return "", err
		}
		hasBody = true
	}
	g.wl("dmFire(dmCall{tag: %s, method: %s, url: %s, body: %s, hasBody: %t, timeoutMS: %d})",
		goStr(spec.Tag), goStr(spec.Method), url, body, hasBody, spec.TimeoutMS)
	return in, nil
}

// requestLambda compiles one of Request's Text-valued lambdas over the world.
func (g *gen) requestLambda(n *ir.Node, key, in string) (string, error) {
	lam, _ := n.Meta[key].(*ast.Lambda)
	if lam == nil || len(lam.Params) == 0 {
		return "", unsupported(n, "missing %s", key)
	}
	expr, _, err := g.compileExpr(lam.Body, exprEnv{lam.Params[0]: {expr: in, typ: n.In}})
	if err != nil {
		return "", unsupported(n, "%s: %v", key, err)
	}
	return expr, nil
}

// emitToJSON lowers `Channeled Energy: Convert To JSON`. The value decides the
// shape, so there is nothing to declare.
func (g *gen) emitToJSON(n *ir.Node, in string) (string, error) {
	fn, err := g.jsonFunc(n.In)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	v := g.fresh("v")
	g.wl("%s := %s(%s)", v, fn, in)
	return v, nil
}

// emitFromJSON lowers `Channeled Energy: Convert From JSON`. The type was
// written with `Into:`, and what comes back is that type or an error saying
// why not.
func (g *gen) emitFromJSON(n *ir.Node, in string) (string, error) {
	into, _ := n.Meta["into"].(*ir.Type)
	if into == nil {
		return "", unsupported(n, "missing Into: type")
	}
	fn, err := g.jsonInFunc(into)
	if err != nil {
		return "", unsupported(n, "%v", err)
	}
	g.helper("dmFail", declFail, "fmt", "os")
	raw, v, e1, e2 := g.fresh("raw"), g.fresh("v"), g.fresh("err"), g.fresh("err")
	g.wl("%s, %s := dmJSONParse(%s)", raw, e1, in)
	g.wl("if %s != nil {", e1)
	g.in()
	g.wl(`dmFail("Convert From JSON: %%v", %s)`, e1)
	g.out()
	g.wl("}")
	g.wl(`%s, %s := %s(%s, "")`, v, e2, fn, raw)
	g.wl("if %s != nil {", e2)
	g.in()
	g.wl(`dmFail("Convert From JSON: %%v", %s)`, e2)
	g.out()
	g.wl("}")
	return v, nil
}
