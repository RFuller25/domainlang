// Package lsp is Domain's language server: JSON-RPC 2.0 over stdio with
// Content-Length framing, stdlib only. It fronts the same engines the CLI
// uses — package diag for diagnostics and quick fixes, the prims resolver for
// hover types, the parsed AST for go-to-definition — so an editor shows
// exactly what `domain expansion: lint` prints.
//
// Capabilities: full-text document sync, publishDiagnostics on open/change,
// hover (primitive documentation from the shared prims catalog, plus the
// pipeline value type flowing through a statement), completion (themed
// keywords, a keyword's primitives, argument labels, and Mode: values),
// definition (Shikigami calls jump to their definition), and a code action
// that applies every confident fix at once.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"domain/ast"
	"domain/diag"
	"domain/ir"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
	"domain/token"
)

// Server is one LSP session over a reader/writer pair.
type Server struct {
	in   *bufio.Reader
	out  io.Writer
	log  io.Writer
	docs map[string]*document // uri -> current state
}

// document is the tracked state of one open file: its current text plus a
// lazily-computed front-end result shared by hover and definition. The cache
// is invalidated by replacing the whole struct on didOpen/didChange, so a
// stale pipe/prog can never outlive the text it was computed from.
type document struct {
	text string
	// path is the document's filesystem path, so `Innate Domain` imports
	// resolve against the file's own directory. Unsaved buffers still work:
	// resolveText reads library files through the open documents first.
	path     string
	resolved bool // pipe/prog computed for text
	pipe     *ir.Pipeline
	prog     *ast.Program
	// sites records where each Shikigami was defined, so go-to-definition can
	// follow an imported name into its library file.
	sites map[string]prims.DefSite
}

// resolve returns the front-end result for the document's current text,
// computing it on first use after each content change. The server is
// single-threaded (one request at a time off one stdin), so no locking.
func (d *document) resolve() (*ir.Pipeline, *ast.Program) {
	if !d.resolved {
		d.sites = map[string]prims.DefSite{}
		d.pipe, d.prog = resolveText(d.path, d.text, d.sites)
		d.resolved = true
	}
	return d.pipe, d.prog
}

// Serve runs the session until exit or EOF. Protocol errors on a single
// message are logged and skipped; the loop only ends with the client.
func Serve(in io.Reader, out, log io.Writer) error {
	s := &Server{in: bufio.NewReader(in), out: out, log: log, docs: map[string]*document{}}
	for {
		msg, err := s.read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var req request
		if err := json.Unmarshal(msg, &req); err != nil {
			fmt.Fprintf(s.log, "lsp: bad message: %v\n", err)
			continue
		}
		if req.Method == "exit" {
			return nil
		}
		s.dispatch(&req)
	}
}

// ---------------------------------------------------------------------------
// JSON-RPC plumbing
// ---------------------------------------------------------------------------

type request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

func (s *Server) read() ([]byte, error) {
	length := -1
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
			if _, err := fmt.Sscanf(v, "%d", &length); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s.in, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (s *Server) write(v any) {
	body, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(s.log, "lsp: marshal: %v\n", err)
		return
	}
	if _, err := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		fmt.Fprintf(s.log, "lsp: write: %v\n", err)
	}
}

func (s *Server) reply(id json.RawMessage, result any) {
	s.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *Server) notify(method string, params any) {
	s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// ---------------------------------------------------------------------------
// dispatch
// ---------------------------------------------------------------------------

func (s *Server) dispatch(req *request) {
	switch req.Method {
	case "initialize":
		s.reply(req.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":   1, // full
				"hoverProvider":      true,
				"definitionProvider": true,
				"codeActionProvider": true,
				"inlayHintProvider":  true,
				"completionProvider": map[string]any{
					"triggerCharacters": []string{":", " "},
					"resolveProvider":   false,
				},
			},
			"serverInfo": map[string]any{"name": "domain-lsp"},
		})
	case "initialized", "textDocument/didSave", "$/cancelRequest", "workspace/didChangeConfiguration":
		// Notifications with nothing to do.
	case "shutdown":
		s.reply(req.ID, nil)
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		if json.Unmarshal(req.Params, &p) == nil {
			s.docs[p.TextDocument.URI] = &document{text: p.TextDocument.Text, path: uriPath(p.TextDocument.URI)}
			s.publishDiagnostics(p.TextDocument.URI)
		}
	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if json.Unmarshal(req.Params, &p) == nil && len(p.ContentChanges) > 0 {
			s.docs[p.TextDocument.URI] = &document{
				text: p.ContentChanges[len(p.ContentChanges)-1].Text,
				path: uriPath(p.TextDocument.URI),
			}
			s.publishDiagnostics(p.TextDocument.URI)
		}
	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if json.Unmarshal(req.Params, &p) == nil {
			delete(s.docs, p.TextDocument.URI)
			s.notify("textDocument/publishDiagnostics",
				map[string]any{"uri": p.TextDocument.URI, "diagnostics": []any{}})
		}
	case "textDocument/hover":
		s.reply(req.ID, s.hover(req.Params))
	case "textDocument/completion":
		s.reply(req.ID, s.completion(req.Params))
	case "textDocument/definition":
		s.reply(req.ID, s.definition(req.Params))
	case "textDocument/codeAction":
		s.reply(req.ID, s.codeActions(req.Params))
	case "textDocument/inlayHint":
		s.reply(req.ID, s.inlayHints(req.Params))
	default:
		if len(req.ID) > 0 {
			s.reply(req.ID, nil) // unknown request: honest null beats silence
		}
	}
}

// ---------------------------------------------------------------------------
// diagnostics
// ---------------------------------------------------------------------------

func uriPath(uri string) string {
	p := strings.TrimPrefix(uri, "file://")
	if decoded, err := url.PathUnescape(p); err == nil {
		return decoded
	}
	return p
}

func (s *Server) publishDiagnostics(uri string) {
	doc, ok := s.docs[uri]
	if !ok {
		return
	}
	rep := diag.Analyze(uriPath(uri), doc.text)
	out := make([]map[string]any, 0, len(rep.Diags))
	for i := range rep.Diags {
		d := &rep.Diags[i]
		msg := d.Msg
		if d.Help != "" {
			msg += " — " + d.Help
		}
		out = append(out, map[string]any{
			"range":    diagRange(d),
			"severity": lspSeverity(d.Severity),
			"code":     d.Code,
			"source":   "domain",
			"message":  msg,
		})
	}
	s.notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": out})
}

func lspSeverity(sev diag.Severity) int {
	switch sev {
	case diag.Error:
		return 1
	case diag.Warning:
		return 2
	default:
		return 4 // Hint
	}
}

func diagRange(d *diag.Diagnostic) map[string]any {
	line := d.Pos.Line - 1
	if line < 0 {
		line = 0
	}
	col := d.Pos.Col - 1
	if col < 0 {
		col = 0
	}
	return map[string]any{
		"start": map[string]int{"line": line, "character": col},
		"end":   map[string]int{"line": line, "character": col + d.Width()},
	}
}

// ---------------------------------------------------------------------------
// hover: the pipeline type flowing through a statement
// ---------------------------------------------------------------------------

func (s *Server) hover(params json.RawMessage) any {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line int `json:"line"`
		} `json:"position"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	doc, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return nil
	}
	pipe, prog := doc.resolve()
	line := p.Position.Line + 1

	// A primitive on this line: show its documentation from the catalog —
	// keyword, signature, and a one-line definition drawn from primitives.md —
	// plus the concrete type step when the program resolves. This works even
	// when the program does not yet type-check, because prims.Lookup matches
	// the statement without running the checker.
	if prog != nil {
		if stmt := statementOnLine(prog, line); stmt != nil {
			if prim := prims.Lookup(stmt); prim != nil {
				if doc, ok := prims.Doc(prim.ID); ok {
					var b strings.Builder
					fmt.Fprintf(&b, "**%s: %s** — `%s`\n\n%s", prim.Keyword, prim.ID, doc.Signature, doc.Summary)
					if n := nodeOnLine(pipe, line); n != nil {
						in := "(source)"
						if n.In != nil {
							in = n.In.String()
						}
						fmt.Fprintf(&b, "\n\n```\n%s → %s\n```", in, n.Out)
					}
					fmt.Fprintf(&b, "\n\n_primitives.md#%s_", doc.DocAnchor)
					return markupHover(b.String())
				}
			}
		}
	}

	// A pipeline node on this line with no catalog entry (loops, channels,
	// consumers): show what the statement does to the type.
	if n := nodeOnLine(pipe, line); n != nil {
		in := "(source)"
		if n.In != nil {
			in = n.In.String()
		}
		return markupHover(fmt.Sprintf("```\n%s\n%s → %s\n```", n.Display, in, n.Out))
	}
	// A Shikigami definition header: show its name and parameters.
	if prog != nil {
		for _, def := range prog.Shikigamis {
			if def.Pos.Line != line {
				continue
			}
			params := make([]string, len(def.Params))
			for i, pr := range def.Params {
				params[i] = pr.Name + ": " + prims.TypeString(pr.Type)
			}
			sig := ""
			if def.Sig != nil {
				sig = fmt.Sprintf(" : %s -> %s",
					prims.TypeString(def.Sig.In), prims.TypeString(def.Sig.Out))
			}
			body := fmt.Sprintf("```\nShikigami %q (%s)%s — %d statement(s)\n```",
				def.Name, strings.Join(params, ", "), sig, len(def.Body))
			return markupHover(body)
		}
	}
	return nil
}

// markupHover wraps markdown text as an LSP hover result.
func markupHover(value string) any {
	return map[string]any{"contents": map[string]any{"kind": "markdown", "value": value}}
}

// nodeOnLine returns the pipeline node positioned on the given 1-based line, or
// nil (also nil when the pipeline did not resolve).
func nodeOnLine(pipe *ir.Pipeline, line int) *ir.Node {
	if pipe == nil {
		return nil
	}
	for _, n := range pipe.Nodes {
		if n.Pos.Line == line {
			return n
		}
	}
	return nil
}

// statementOnLine finds the statement whose head is on the given 1-based line,
// searching the top-level pipeline, every Shikigami body, and nested blocks
// (Channel and loop bodies).
func statementOnLine(prog *ast.Program, line int) *ast.Statement {
	var found *ast.Statement
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, st := range stmts {
			if found != nil {
				return
			}
			if st.Pos.Line == line {
				found = st
				return
			}
			walk(st.Block)
		}
	}
	walk(prog.Statements)
	for _, def := range prog.Shikigamis {
		if found != nil {
			break
		}
		walk(def.Body)
	}
	return found
}

// ---------------------------------------------------------------------------
// definition: Shikigami calls jump to their definition
// ---------------------------------------------------------------------------

func (s *Server) definition(params json.RawMessage) any {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line int `json:"line"`
		} `json:"position"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	doc, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return nil
	}
	_, prog := doc.resolve()
	if prog == nil {
		return nil
	}
	line := p.Position.Line + 1
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
	walk(prog.Statements)
	for _, def := range prog.Shikigamis {
		walk(def.Body)
	}
	if name == "" {
		return nil
	}
	for _, def := range prog.Shikigamis {
		if def.Name == name {
			pos := map[string]int{"line": def.Pos.Line - 1, "character": def.Pos.Col - 1}
			return map[string]any{
				"uri":   p.TextDocument.URI,
				"range": map[string]any{"start": pos, "end": pos},
			}
		}
	}
	// An imported Shikigami lives in a library file: jump there. The definition
	// site came back from the resolver, which is the only thing that knows which
	// file on the search path actually answered the import.
	if site, ok := doc.sites[name]; ok && site.Origin == "import" && site.Path != "" {
		if def := importedDef(site.Path, name); def != nil {
			pos := map[string]int{"line": def.Pos.Line - 1, "character": def.Pos.Col - 1}
			return map[string]any{
				"uri":   "file://" + def.file,
				"range": map[string]any{"start": pos, "end": pos},
			}
		}
	}
	return nil // prelude Shikigami live in the embedded source, not a file
}

// importedDefSite is a definition found inside a library file.
type importedDefSite struct {
	Pos  token.Position
	file string
}

// importedDef re-reads a library file to locate a definition's position in it.
// Re-parsing here rather than caching the AST keeps the resolver from holding
// every library it ever loaded; go-to-definition is a rare, interactive request.
func importedDef(path, name string) *importedDefSite {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	toks, err := lexer.Lex(string(src))
	if err != nil {
		return nil
	}
	prog, err := parser.Parse(string(src), toks)
	if err != nil {
		return nil
	}
	for _, def := range prog.Shikigamis {
		if def.Name == name {
			return &importedDefSite{Pos: def.Pos, file: path}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// code actions: apply every confident fix at once
// ---------------------------------------------------------------------------

func (s *Server) codeActions(params json.RawMessage) any {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(params, &p) != nil {
		return nil
	}
	doc, ok := s.docs[p.TextDocument.URI]
	if !ok {
		return nil
	}
	rep := diag.Analyze(uriPath(p.TextDocument.URI), doc.text)
	if rep.Applied == 0 || rep.FixedSrc == doc.text {
		return []any{}
	}
	// One whole-document edit: the multi-round fixes in FixedSrc are only
	// guaranteed consistent as a set, so they are offered as a set.
	lines := strings.Count(doc.text, "\n") + 1
	fullRange := map[string]any{
		"start": map[string]int{"line": 0, "character": 0},
		"end":   map[string]int{"line": lines, "character": 0},
	}
	return []any{map[string]any{
		"title": fmt.Sprintf("Domain: apply %d automatic fix(es)", rep.Applied),
		"kind":  "quickfix",
		"edit": map[string]any{
			"changes": map[string]any{
				p.TextDocument.URI: []any{map[string]any{"range": fullRange, "newText": rep.FixedSrc}},
			},
		},
	}}
}

// resolveText runs the front end over editor text, tolerating failure at any
// stage: hover/definition work with whatever was reachable. path gives
// `Innate Domain` imports their file context, and sites (when non-nil) is
// filled in with each Shikigami's definition site.
func resolveText(path, text string, sites map[string]prims.DefSite) (*ir.Pipeline, *ast.Program) {
	toks, err := lexer.Lex(text)
	if err != nil {
		return nil, nil
	}
	prog, err := parser.Parse(text, toks)
	if err != nil {
		return nil, nil
	}
	opts := prims.FileOptions(path)
	opts.Sites = sites
	pipe, err := prims.ResolveWith(prog, opts)
	if err != nil {
		// Resolution hands back the nodes it managed before failing, so hover
		// and inlay hints still work for the prefix that type-checked — the
		// REPL's incremental feel, in a file that does not yet resolve.
		return pipe, prog
	}
	return pipe, prog
}
