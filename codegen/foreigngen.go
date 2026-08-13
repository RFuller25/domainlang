package codegen

import (
	"fmt"

	"domain/ir"
	"domain/langs"
)

// Compiling `Domain Expansion: <language>`.
//
// The block is baked into the binary as a string constant and run the same way
// the interpreter runs it: written to a throwaway directory, handed the current
// value on stdin, its stdout decoded as the next value. So a compiled program
// carries its foreign source but not the runtime that executes it — the binary
// stops being self-contained the moment a program uses this, which is the
// honest cost of the feature and is documented as such.
//
// The wire format is specified in prims/foreign.go and implemented twice: there
// in Go that runs, here in Go that is emitted. The differential tests are what
// hold the two together — a foreign program's output has to be byte-identical
// under `domain run` and `./binary`, like every other primitive's.

func (g *gen) emitForeign(n *ir.Node, in string) (string, error) {
	lang, _ := n.Meta["lang"].(string)
	source, _ := n.Meta["source"].(string)
	if lang == "" {
		return "", unsupported(n, "foreign block has no language")
	}
	spec, ok := langs.Lookup(lang)
	if !ok {
		return "", unsupported(n, "no runner for %s", lang)
	}

	stdin, err := g.foreignEncode(n, in)
	if err != nil {
		return "", err
	}
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmForeignRun", declForeignRun,
		"bytes", "errors", "os", "os/exec", "path/filepath", "strings")

	out := g.fresh("v")
	g.wl("%s := dmForeignRun(dmForeignSpec{", out)
	g.in()
	g.wl("Lang: %s, File: %s, Env: %s,", goStr(spec.Name), goStr(spec.File), goStr(spec.Env))
	g.wl("Cands: %s,", goSlice(spec.Candidates))
	// The emitted runtime's Tail is inserted between the binary and the
	// program path, which is what langs calls Args.
	g.wl("Tail: %s, AppendProg: %v,", goSlice(spec.Args), spec.AppendProg)
	g.wl("Extra: %s,", goStrMap(spec.Extra))
	g.wl("Source: %s,", goStr(source))
	g.out()
	g.wl("}, %s)", stdin)
	return g.foreignDecode(n, out)
}

// foreignEncode emits the value as the bytes the foreign program will read,
// returning the Go expression holding them. It mirrors prims.foreignEncode.
func (g *gen) foreignEncode(n *ir.Node, in string) (string, error) {
	if n.In.Kind == ir.KList {
		// One element per line, and nothing at all for the empty list — the
		// same string prims.foreignEncode builds by joining and appending.
		elem, err := g.scalarFmt("e", n.In.Elem)
		if err != nil {
			return "", unsupported(n, "cannot encode %s for a foreign block: %v", n.In, err)
		}
		g.imp("strings")
		sb, e := g.fresh("sb"), "e"
		g.wl("var %s strings.Builder", sb)
		g.wl("for _, %s := range %s {", e, in)
		g.in()
		g.wl("%s.WriteString(%s)", sb, elem)
		g.wl("%s.WriteByte('\\n')", sb)
		g.out()
		g.wl("}")
		return sb + ".String()", nil
	}
	// A scalar or a grid renders as itself and gains one closing newline,
	// unless it is empty. dmForeignLine is that rule, in one place, so the
	// emitted program cannot drift from the interpreter on the empty Text.
	rendered, err := g.scalarFmt(in, n.In)
	if err != nil {
		return "", unsupported(n, "cannot encode %s for a foreign block: %v", n.In, err)
	}
	g.helper("dmForeignLine", declForeignLine)
	return fmt.Sprintf("dmForeignLine(%s)", rendered), nil
}

// foreignDecode emits the parse of the program's stdout back into a value,
// mirroring prims.foreignDecode.
func (g *gen) foreignDecode(n *ir.Node, out string) (string, error) {
	g.helper("dmForeignBody", declForeignBody, "strings")
	if n.Out.Kind == ir.KList {
		g.helper("dmForeignLines", declForeignLines, "strings")
		lines := g.fresh("ls")
		g.wl("%s := dmForeignLines(%s)", lines, out)
		if n.Out.Elem.Kind == ir.KText {
			return lines, nil
		}
		goT, err := g.goType(n.Out)
		if err != nil {
			return "", err
		}
		conv, err := g.foreignScalarParse(n, "l", n.Out.Elem)
		if err != nil {
			return "", err
		}
		vals, i, l := g.fresh("v"), g.fresh("i"), "l"
		g.wl("%s := make(%s, len(%s))", vals, goT, lines)
		g.wl("for %s, %s := range %s {", i, l, lines)
		g.in()
		g.wl("%s[%s] = %s", vals, i, conv)
		g.out()
		g.wl("}")
		return vals, nil
	}
	if n.Out.Kind == ir.KText {
		v := g.fresh("v")
		g.wl("%s := dmForeignBody(%s)", v, out)
		return v, nil
	}
	conv, err := g.foreignScalarParse(n, fmt.Sprintf("dmForeignBody(%s)", out), n.Out)
	if err != nil {
		return "", err
	}
	v := g.fresh("v")
	g.wl("%s := %s", v, conv)
	return v, nil
}

// foreignScalarParse is the expression reading one field of foreign output as a
// scalar. A field that does not parse is the foreign program's mistake, and the
// binary fails with the same wording the interpreter uses.
func (g *gen) foreignScalarParse(n *ir.Node, expr string, t *ir.Type) (string, error) {
	switch t.Kind {
	case ir.KText:
		return expr, nil
	case ir.KInt:
		g.helper("dmForeignInt", declForeignInt, "strconv", "strings")
		return fmt.Sprintf("dmForeignInt(%s)", expr), nil
	case ir.KFloat:
		g.helper("dmForeignFloat", declForeignFloat, "strconv", "strings")
		return fmt.Sprintf("dmForeignFloat(%s)", expr), nil
	case ir.KBool:
		g.helper("dmForeignBool", declForeignBool, "strings")
		return fmt.Sprintf("dmForeignBool(%s)", expr), nil
	}
	return "", unsupported(n, "cannot read %s from a foreign block's output", t)
}

// The runner details come from package langs, the same table the lexer and the
// interpreter read, so the compiled program starts a language exactly the way
// `domain run` does. The differential tests are what prove it: a foreign
// block's output has to be byte-identical under both backends.
