// `domain expansion: explain <file>` — a program, read back in plain English.
//
// Domain is verbose on purpose, and the vocabulary is still a vocabulary: a
// reader new to it sees `Maximum Technique: Select Top 3, Sum` and has to know
// what a Maximum Technique is before the line says anything. Everything needed
// to say it for them is already in the binary — the catalog's sentence for each
// primitive, the type checker's answer for what flows in and out of each stage,
// and the optimizer's own account of what it rewrote. This command lays the
// three side by side, in source order, one stage at a time.
//
// The optimizer's half is the part `--explain` alone cannot give. A rewrite
// message says a substitution happened somewhere; here it lands on the line it
// happened to, so "what does this line do" and "what will actually run for this
// line" are answered in the same place.
package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"domain/ir"
	"domain/optimizer"
	"domain/prims"
	"domain/runner"
	"domain/token"
)

type explainOptions struct {
	JSON  bool
	Plain bool
}

func parseExplainArgs(args []string) (string, explainOptions, error) {
	var opts explainOptions
	var path string
	for _, a := range args {
		switch {
		case a == "--json":
			opts.JSON = true
		case a == "--plain":
			opts.Plain = true
		case strings.HasPrefix(a, "-"):
			return "", opts, fmt.Errorf("unknown flag %q", a)
		case path != "":
			return "", opts, fmt.Errorf("unexpected extra argument %q", a)
		default:
			path = a
		}
	}
	if path == "" {
		return "", opts, fmt.Errorf("explain needs a program file")
	}
	return path, opts, nil
}

// explainStage is one stage of the program as written.
type explainStage struct {
	Line   int    `json:"line,omitempty"`   // 0 when the stage is not in this file
	Source string `json:"source,omitempty"` // the statement as written
	From   string `json:"from,omitempty"`   // the prelude or library it was inlined from
	Depth  int    `json:"depth"`            // nesting, for a body's stages
	// Args are the indented lines that belong to the statement: `Using:`,
	// `By:`, a Shikigami call's parameters.
	Args  []string `json:"arguments,omitempty"`
	Prim  string   `json:"primitive"`
	Stage string   `json:"stage"` // the stage with its arguments, as --stats names it
	What  string   `json:"what"`  // the plain-English sentence
	In    string   `json:"in,omitempty"`
	Out   string   `json:"out"`
	// Optimized says what the optimizer made of the stage: "" when it runs as
	// written, otherwise the stage that replaces it, or that it was absorbed.
	Optimized string `json:"optimized,omitempty"`
}

type explainReport struct {
	Program  string         `json:"program"`
	Stages   []explainStage `json:"stages"`
	Rewrites []string       `json:"rewrites"`
}

// structuralWhat is the sentence for a node that is a statement of the
// language rather than a catalogued primitive. The catalog has no entry for
// these (prims.StructuralPrims), and they are the ones a reader most needs
// explained: they are what gives a program its shape.
var structuralWhat = map[string]string{
	"Channel":                     "Runs the indented stages on the current value and keeps the result under this name; the main pipeline's value is left as it was.",
	"Zip":                         "Takes the named channels' results and pairs them up element by element.",
	"Combine":                     "Brings the named channels' results back into the main pipeline as one value.",
	"Part":                        "Runs the indented stages on the current value and labels what they print; the value carries on unchanged to whatever follows.",
	"Consider":                    "Names a value for this stage's expressions to use.",
	"Simple Domain (Repeat)":      "Runs the indented stages a fixed number of times, feeding each pass's result into the next.",
	"Simple Domain (While)":       "Checks the condition on the current value, runs the indented stages while it holds, and stops the first time it does not.",
	"Simple Domain (For)":         "Runs the indented stages once per element of a named list, with that element available to their expressions.",
	"Simple Domain (For Each)":    "Runs the indented stages once per element of a named list, with that element available to their expressions.",
	"Simple Domain (Fixed Point)": "Runs the indented stages over and over until a pass leaves the value unchanged.",
}

// stageKey identifies a stage across the naive and the optimized pipeline:
// where it is, what it is, and with which arguments.
func stageKey(n *ir.Node) string {
	from, _ := n.Foreign()
	return fmt.Sprintf("%s\x00%d\x00%s\x00%s", from, n.Pos.Line, n.Prim, n.Display)
}

func lineKey(n *ir.Node) string {
	from, _ := n.Foreign()
	return fmt.Sprintf("%s\x00%d", from, n.Pos.Line)
}

// buildExplain resolves the program twice — as written, and as the optimizer
// leaves it — and describes the first in terms of the second.
func buildExplain(path string, src []byte) (*explainReport, error) {
	naive, err := runner.Resolve(path, src)
	if err != nil {
		return nil, err
	}
	optimized, err := runner.Resolve(path, src)
	if err != nil {
		return nil, err
	}
	rewrites := optimizer.Optimize(optimized, true)

	// What the optimized pipeline holds: every stage by identity, and the
	// stages on each line that the program as written does not have.
	written := map[string]int{}
	prims.WalkNodes(naive, func(n *ir.Node) { written[stageKey(n)]++ })
	kept := map[string]int{}
	replacements := map[string][]string{}
	prims.WalkNodes(optimized, func(n *ir.Node) {
		kept[stageKey(n)]++
		if written[stageKey(n)] == 0 {
			replacements[lineKey(n)] = append(replacements[lineKey(n)], n.Display)
		}
	})

	lines := strings.Split(string(src), "\n")
	sourceLine := func(line int) (string, bool) {
		if line < 1 || line > len(lines) {
			return "", false
		}
		return lines[line-1], true
	}
	// The lines that are stages in their own right, so a statement's argument
	// lines can be told from the first stage of its body.
	stageLines := map[int]bool{}
	prims.WalkNodes(naive, func(n *ir.Node) {
		if _, foreign := n.Foreign(); !foreign {
			stageLines[n.Pos.Line] = true
		}
	})
	// argLines are the indented lines under a statement that belong to it:
	// `Using:`, `By:`, a Shikigami call's parameters.
	argLines := func(line int) []string {
		head, ok := sourceLine(line)
		if !ok {
			return nil
		}
		var out []string
		for l := line + 1; l <= len(lines); l++ {
			raw := lines[l-1]
			text := strings.TrimSpace(raw)
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			if stageLines[l] || leadingWidth(raw) <= leadingWidth(head) {
				break
			}
			out = append(out, text)
		}
		return out
	}

	rep := &explainReport{Program: path, Rewrites: []string{}}
	// add describes one node. base is the depth it sits at when its own line
	// cannot say: a stage inlined from the prelude, or from a Shikigami defined
	// elsewhere in the file, whose indentation is the definition's.
	add := func(n *ir.Node, base int, inlined bool) {
		st := explainStage{Prim: n.Prim, Stage: n.Display, Out: typeWords(n.Out), Depth: base}
		if n.In != nil {
			st.In = typeWords(n.In)
		}
		if from, foreign := n.Foreign(); foreign {
			// Its line number is a coordinate in a file the reader is not
			// looking at, so it is not shown.
			st.From = from
		} else if raw, ok := sourceLine(n.Pos.Line); ok {
			st.Line = n.Pos.Line
			st.Source = strings.TrimSpace(raw)
			st.Args = argLines(n.Pos.Line)
			if !inlined {
				st.Depth = base + indentDepth(raw)
			}
		}
		st.What = stageWhat(n)

		switch key := stageKey(n); {
		case kept[key] > 0:
			kept[key]--
		case len(replacements[lineKey(n)]) > 0:
			lk := lineKey(n)
			st.Optimized = "runs as " + replacements[lk][0]
			replacements[lk] = replacements[lk][1:]
		default:
			st.Optimized = "absorbed into another stage"
		}
		rep.Stages = append(rep.Stages, st)
	}
	// addTree describes a top-level stage and, under it, the stages of its
	// body in source order. WalkNodes is breadth-first, which is the right
	// order for counting and the wrong one for reading.
	addTree := func(top *ir.Node, base int, inlined bool) {
		type placed struct {
			n    *ir.Node
			line int
		}
		var body []placed
		at := top.Pos.Line
		prims.WalkNodes(&ir.Pipeline{Nodes: []*ir.Node{top}}, func(n *ir.Node) {
			if n == top {
				return
			}
			// An inlined stage keeps the place of the stage before it.
			if _, foreign := n.Foreign(); !foreign {
				at = n.Pos.Line
			}
			body = append(body, placed{n, at})
		})
		slices.SortStableFunc(body, func(a, b placed) int { return a.line - b.line })
		add(top, base, inlined)
		topDepth := rep.Stages[len(rep.Stages)-1].Depth
		for _, p := range body {
			if _, foreign := p.n.Foreign(); foreign || inlined {
				add(p.n, topDepth+1, true)
			} else {
				add(p.n, base, false)
			}
		}
	}

	// A Shikigami call leaves no node of its own: its body is inlined, carrying
	// the definition's positions. Such stages are recognisable at the top level
	// — they come from another file, or from an indented line, which no
	// top-level statement is — and the last of them is tagged with the call
	// site. The call gets a heading and its body is listed under it.
	isInlined := func(n *ir.Node) bool {
		if _, foreign := n.Foreign(); foreign {
			return true
		}
		raw, ok := sourceLine(n.Pos.Line)
		return ok && leadingWidth(raw) > 0
	}
	prevLine := 0
	for i := 0; i < len(naive.Nodes); {
		top := naive.Nodes[i]
		if !isInlined(top) {
			addTree(top, 0, false)
			prevLine = top.Pos.Line
			i++
			continue
		}
		end := i
		for end < len(naive.Nodes) && isInlined(naive.Nodes[end]) {
			end++
		}
		nextLine := len(lines) + 1
		if end < len(naive.Nodes) {
			nextLine = naive.Nodes[end].Pos.Line
		}
		// The call sites inside this run: tags that name a top-level line
		// between the statements on either side of it.
		start := i
		for j := i; j < end; j++ {
			pos, ok := naive.Nodes[j].Meta["callPos"].(token.Position)
			raw, inFile := sourceLine(pos.Line)
			if !ok || !inFile || leadingWidth(raw) > 0 || pos.Line <= prevLine || pos.Line >= nextLine {
				continue
			}
			group := naive.Nodes[start : j+1]
			call := explainStage{
				Line:   pos.Line,
				Source: strings.TrimSpace(raw),
				Args:   argLines(pos.Line),
				Prim:   "Shikigami",
				Stage:  strings.TrimSpace(raw),
				What:   fmt.Sprintf("Calls a Shikigami — a named group of stages. Its body is inlined here, as the %d stage(s) below.", len(group)),
				Out:    typeWords(group[len(group)-1].Out),
			}
			if in := group[0].In; in != nil {
				call.In = typeWords(in)
			}
			rep.Stages = append(rep.Stages, call)
			for _, n := range group {
				addTree(n, 1, true)
			}
			prevLine, start = pos.Line, j+1
		}
		for _, n := range naive.Nodes[start:end] {
			addTree(n, 0, true)
		}
		i = end
	}
	for _, r := range rewrites {
		rep.Rewrites = append(rep.Rewrites, r.Message)
	}
	return rep, nil
}

// stageWhat is the sentence for one stage: the catalog's, a structural
// statement's, or — for a node neither covers — the stage's own description.
func stageWhat(n *ir.Node) string {
	if doc, ok := prims.Doc(prims.CatalogID(n.Prim)); ok && doc.Summary != "" {
		return doc.Summary
	}
	if what, ok := structuralWhat[n.Prim]; ok {
		return what
	}
	return n.Display
}

func typeWords(t *ir.Type) string {
	if t == nil {
		return "nothing"
	}
	return t.String()
}

// leadingWidth is a line's indentation in columns, a tab counting as four.
func leadingWidth(line string) int {
	w := 0
	for _, r := range line {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			return w
		}
	}
	return w
}

// indentDepth counts a line's indentation in the language's own unit: four
// spaces, or a tab.
func indentDepth(line string) int {
	spaces := 0
	for _, r := range line {
		switch r {
		case ' ':
			spaces++
		case '\t':
			spaces += 4
		default:
			return spaces / 4
		}
	}
	return spaces / 4
}

// Explain is the command.
func Explain(path string, opts explainOptions, stdout, stderr io.Writer) int {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "domain: reading %s: %v\n", path, err)
		return 1
	}
	rep, err := buildExplain(path, src)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		fmt.Fprintf(stderr, "  a program has to resolve before it can be explained — `domain expansion: lint %s` lists what is wrong\n", path)
		return 1
	}
	if opts.JSON {
		if err := writeJSON(stdout, rep); err != nil {
			fmt.Fprintf(stderr, "domain: %v\n", err)
			return 1
		}
		return 0
	}
	rep.write(stdout, isColorTerminal(stdout) && !opts.Plain)
	return 0
}

func (r *explainReport) write(w io.Writer, color bool) {
	fmt.Fprintf(w, "%s — %d stage(s)\n", r.Program, len(r.Stages))
	changed := 0
	for i := range r.Stages {
		st := &r.Stages[i]
		pad := strings.Repeat("    ", st.Depth)
		fmt.Fprintln(w)
		if st.From != "" {
			fmt.Fprintf(w, "     %s%s  %s\n", pad, paintIf(color, styKeyword, st.Stage),
				paintIf(color, styDim, "(inlined from "+st.From+")"))
		} else {
			fmt.Fprintf(w, "%4d %s%s\n", st.Line, pad, highlightSource(st.Source, color))
			for _, arg := range st.Args {
				fmt.Fprintf(w, "     %s    %s\n", pad, highlightSource(arg, color))
			}
		}
		fmt.Fprintf(w, "     %s  %s\n", pad, st.What)
		flow := "→ " + st.Out
		if st.In != "" {
			flow = st.In + " " + flow
		}
		fmt.Fprintf(w, "     %s  %s\n", pad, paintIf(color, styType, flow))
		if st.Optimized != "" {
			changed++
			fmt.Fprintf(w, "     %s  %s\n", pad, paintIf(color, styDim, "optimizer: "+st.Optimized))
		}
	}
	fmt.Fprintln(w)
	if len(r.Rewrites) == 0 {
		fmt.Fprintln(w, "The optimizer runs this program as written.")
		return
	}
	fmt.Fprintf(w, "The optimizer made %d rewrite(s), touching %d stage(s) above:\n", len(r.Rewrites), changed)
	for _, m := range r.Rewrites {
		fmt.Fprintf(w, "  %s\n", m)
	}
}
