// The language server's knowledge, without the language server.
//
// Everything the editor wants — the type flowing out of each statement, a
// primitive's documentation, where a Shikigami is defined — was already
// computed here, and was reachable only over JSON-RPC. That is the right shape
// for an editor in another process and the wrong one for `domain expansion:
// development`, which is in *this* process: it would have meant the binary
// running a subprocess against itself to ask questions it can already answer.
//
// So the analysis lives here as ordinary Go values, and the protocol handlers
// (lsp.go, inlay.go) are thin marshalling over it. Two things fall out of
// that. The editor and the language server can no longer disagree about a
// type, since there is one implementation and not two. And these paths become
// directly testable — hover and definition used to be reachable only by
// speaking JSON-RPC at them, which is why they had so few tests.
package lsp

import (
	"fmt"
	"strings"

	"domain/ast"
	"domain/ir"
	"domain/prims"
	"domain/token"
)

// Analysis is one program, resolved as far as it goes.
//
// Resolution is deliberately failure-tolerant: a program that does not yet
// type-check still answers questions about the prefix that did, which is what
// makes hints useful while a program is being written rather than only once it
// is finished.
type Analysis struct {
	Path string
	Text string

	Pipe *ir.Pipeline
	Prog *ast.Program
	// Sites records where each Shikigami came from, so a definition lookup can
	// follow an imported name into its library file.
	Sites map[string]prims.DefSite
}

// Analyze runs the front end over editor text. path gives `Innate Domain`
// imports their file context and may be empty for an unsaved buffer.
func Analyze(path, text string) *Analysis {
	sites := map[string]prims.DefSite{}
	pipe, prog := resolveText(path, text, sites)
	return &Analysis{Path: path, Text: text, Pipe: pipe, Prog: prog, Sites: sites}
}

// ---------------------------------------------------------------------------
// type hints
// ---------------------------------------------------------------------------

// TypeHint is the type flowing out of one statement, ready to be shown at the
// end of its line.
type TypeHint struct {
	Line  int    // 1-based source line
	Label string // e.g. ": List<Int>"
	// Binding is true for a `Consider` binding's line, which reports the type
	// of the value it names rather than a statement's output.
	Binding bool
}

// TypeHints returns one hint per line that has something to say. This is the
// REPL's `=> value : Type` feedback, in a file that is still being written.
func (a *Analysis) TypeHints() []TypeHint {
	if a == nil || a.Pipe == nil || a.Prog == nil {
		return nil
	}
	outByLine, bindByLine := a.typesByLine()

	var hints []TypeHint
	var visit func(stmts []*ast.Statement)
	visit = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			if label, ok := hintFor(st, outByLine); ok {
				hints = append(hints, TypeHint{Line: st.Pos.Line, Label: label})
			}
			for _, b := range st.Binds {
				if t, ok := bindByLine[b.Pos.Line]; ok && t != nil {
					hints = append(hints, TypeHint{Line: b.Pos.Line, Label: ": " + t.String(), Binding: true})
				}
			}
			visit(st.Block)
		}
	}
	visit(a.Prog.Statements)
	for _, def := range a.Prog.Shikigamis {
		visit(def.Body)
	}
	return hints
}

// typesByLine maps source lines to the types resolved for them.
//
// The last node at a line wins. A Shikigami call is the exception: its inlined
// nodes carry positions from the definition's body — possibly in the prelude or
// a library file, which are not coordinates in this program at all — so the
// resolver tags the group's last node with the call site, and that is what the
// call's line reports.
func (a *Analysis) typesByLine() (outByLine, bindByLine map[int]*ir.Type) {
	outByLine, bindByLine = map[int]*ir.Type{}, map[int]*ir.Type{}
	var walk func(nodes []*ir.Node)
	walk = func(nodes []*ir.Node) {
		for _, n := range nodes {
			if n.Out != nil {
				outByLine[n.Pos.Line] = n.Out
			}
			if binds, _ := n.Meta[ir.MetaBinds].([]ir.Binding); binds != nil {
				for _, b := range binds {
					bindByLine[b.Pos().Line] = b.Type()
					walk(b.BlockNodes())
				}
			}
			if pos, ok := n.Meta["callPos"].(token.Position); ok && n.Out != nil {
				outByLine[pos.Line] = n.Out
			}
			// A Channel's own type is its passthrough input, which says nothing.
			// Its body's result is what consumers will see, so recurse.
			if sub, _ := n.Meta["nodes"].([]*ir.Node); sub != nil {
				walk(sub)
			}
		}
	}
	walk(a.Pipe.Nodes)
	return outByLine, bindByLine
}

// ---------------------------------------------------------------------------
// inspect (hover)
// ---------------------------------------------------------------------------

// Inspection is what is known about the statement on one line.
type Inspection struct {
	// Title names the thing: "Cursed Technique: Split", or a Shikigami's
	// signature line. Always set when ok.
	Title string
	// Signature is the primitive's declared type step, e.g. "Text → List<Text>".
	Signature string
	Summary   string
	// TypeStep is the *concrete* step this call makes, which the signature's
	// type variables do not show: "Text → List<Text>" with T resolved.
	TypeStep string
	// DocAnchor links into primitives.md.
	DocAnchor string
}

// InspectLine describes whatever is on a 1-based line: a primitive with its
// catalog entry, a pipeline node with no catalog entry (a loop, a channel, a
// consumer), or a Shikigami definition header.
//
// It works even when the program does not type-check, because prims.Lookup
// matches a statement without running the checker — which matters, since the
// moment you most want to know what an operation does is while the line
// containing it is still wrong.
func (a *Analysis) InspectLine(line int) (Inspection, bool) {
	if a == nil {
		return Inspection{}, false
	}

	if a.Prog != nil {
		if st := statementOnLine(a.Prog, line); st != nil {
			if prim := prims.Lookup(st); prim != nil {
				if doc, ok := prims.Doc(prim.ID); ok {
					ins := Inspection{
						Title:     prim.Keyword + ": " + prim.ID,
						Signature: doc.Signature,
						Summary:   doc.Summary,
						DocAnchor: doc.DocAnchor,
					}
					if n := nodeOnLine(a.Pipe, line); n != nil {
						ins.TypeStep = typeStep(n)
					}
					return ins, true
				}
			}
		}
	}

	if n := nodeOnLine(a.Pipe, line); n != nil {
		return Inspection{Title: n.Display, TypeStep: typeStep(n)}, true
	}

	if a.Prog != nil {
		for _, def := range a.Prog.Shikigamis {
			if def.Pos.Line != line {
				continue
			}
			params := make([]string, len(def.Params))
			for i, pr := range def.Params {
				params[i] = pr.Name + ": " + prims.TypeString(pr.Type)
			}
			sig := ""
			if def.Sig != nil {
				sig = fmt.Sprintf("%s -> %s", prims.TypeString(def.Sig.In), prims.TypeString(def.Sig.Out))
			}
			return Inspection{
				Title:     fmt.Sprintf("Shikigami %q (%s)", def.Name, strings.Join(params, ", ")),
				Signature: sig,
				Summary:   fmt.Sprintf("%d statement(s)", len(def.Body)),
			}, true
		}
	}
	return Inspection{}, false
}

// typeStep renders a node's input → output.
func typeStep(n *ir.Node) string {
	in := "(source)"
	if n.In != nil {
		in = n.In.String()
	}
	return in + " → " + n.Out.String()
}

// ---------------------------------------------------------------------------
// definitions
// ---------------------------------------------------------------------------

// DefLocation is where a Shikigami is defined.
type DefLocation struct {
	Name string
	// Path is the file it lives in; empty means the program itself.
	Path string
	Pos token.Position
	// Origin is "local", "import" or "prelude". A prelude definition has no
	// file to jump to — it lives in embedded source — so it is reported rather
	// than silently returning nothing.
	Origin string
}

// ShikigamiCallOn returns the name a `Shikigami:` statement on this 1-based
// line calls, if any.
func (a *Analysis) ShikigamiCallOn(line int) (string, bool) {
	if a == nil || a.Prog == nil {
		return "", false
	}
	name := ""
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			if st.Pos.Line == line && st.Keyword == "Shikigami" && st.Op != nil {
				name = strings.TrimSpace(st.Op.Raw)
			}
			walk(st.Block)
		}
	}
	walk(a.Prog.Statements)
	for _, def := range a.Prog.Shikigamis {
		walk(def.Body)
	}
	return name, name != ""
}

// DefinitionOf locates a Shikigami by name: in this program, or in the library
// file an import came from.
func (a *Analysis) DefinitionOf(name string) (DefLocation, bool) {
	if a == nil || a.Prog == nil || name == "" {
		return DefLocation{}, false
	}
	for _, def := range a.Prog.Shikigamis {
		if def.Name == name {
			return DefLocation{Name: name, Pos: def.Pos, Origin: "local"}, true
		}
	}
	site, ok := a.Sites[name]
	if !ok {
		return DefLocation{}, false
	}
	if site.Origin == "import" && site.Path != "" {
		if def := importedDef(site.Path, name); def != nil {
			return DefLocation{Name: name, Path: def.file, Pos: def.Pos, Origin: "import"}, true
		}
	}
	// A prelude name is real and has nowhere to jump to. Saying so beats
	// answering nothing, which reads as "no such definition".
	return DefLocation{Name: name, Origin: site.Origin}, site.Origin == "prelude"
}

// DefinitionAt is the whole go-to-definition question for one line.
func (a *Analysis) DefinitionAt(line int) (DefLocation, bool) {
	name, ok := a.ShikigamiCallOn(line)
	if !ok {
		return DefLocation{}, false
	}
	return a.DefinitionOf(name)
}
