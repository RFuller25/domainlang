// The View type, compiled.
//
// This mirrors ir/view.go into the emitted program, exactly as
// codegen/sparsegen.go mirrors ir/sparse.go. codegen generates its own
// runtime — an emitted binary depends on the standard library and not on this
// repository — so a type with an interpreter representation gets a second,
// independent implementation here, and the differential tests are what keep
// the two honest.
//
// The layout arithmetic is transliterated rather than re-derived. Where the
// two could drift, they would drift in geometry, which is the one thing a
// frame diff cannot forgive: a column landing one place over makes every
// subsequent line differ and says nothing about which side is wrong.
package codegen

// declView is the render tree and its plain rendering, emitted whole. It is
// one declaration rather than several because every piece of it is reachable
// from any other: a program that builds a View at all needs the layout, and
// the layout needs every node kind.
const declView = `type dmViewStyle struct {
	fg, bg                        string
	bold, dim, underline, reverse bool
}

func (s dmViewStyle) merge(in dmViewStyle) dmViewStyle {
	out := s
	if in.fg != "" {
		out.fg = in.fg
	}
	if in.bg != "" {
		out.bg = in.bg
	}
	out.bold = out.bold || in.bold
	out.dim = out.dim || in.dim
	out.underline = out.underline || in.underline
	out.reverse = out.reverse || in.reverse
	return out
}

// Node kinds, matching ir.ViewKind.
const (
	dmVText = iota
	dmVStyled
	dmVStack
	dmVBeside
	dmVBox
	dmVMargin
	dmVAlign
)

type dmView struct {
	kind  int
	text  string
	style dmViewStyle
	w, h  int
	align string
	kids  []dmView
}

type dmViewRun struct {
	text  string
	style dmViewStyle
}

type dmViewBlock struct {
	lines [][]dmViewRun
	w     int
}

func dmViewSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func dmViewWidth(s string) int { return utf8.RuneCountInString(s) }

func dmViewChild(v dmView) []dmView {
	if len(v.kids) == 0 {
		return nil
	}
	return v.kids[:1]
}

func dmViewLayout(v dmView, st dmViewStyle) dmViewBlock {
	switch v.kind {
	case dmVText:
		return dmViewLayoutText(v.text, st)
	case dmVStyled:
		if k := dmViewChild(v); k != nil {
			return dmViewLayout(k[0], st.merge(v.style))
		}
		return dmViewBlock{}
	case dmVStack:
		return dmViewLayoutStack(v.kids, st)
	case dmVBeside:
		return dmViewLayoutBeside(v.kids, st)
	case dmVBox:
		var in dmViewBlock
		if k := dmViewChild(v); k != nil {
			in = dmViewLayout(k[0], st)
		}
		return dmViewLayoutBox(in, st)
	case dmVMargin:
		var in dmViewBlock
		if k := dmViewChild(v); k != nil {
			in = dmViewLayout(k[0], st)
		}
		return dmViewLayoutMargin(in, v.w, v.h, st)
	case dmVAlign:
		var in dmViewBlock
		if k := dmViewChild(v); k != nil {
			in = dmViewLayout(k[0], st)
		}
		return dmViewLayoutAlign(in, v.w, v.h, v.align, st)
	}
	return dmViewBlock{}
}

func dmViewLayoutText(s string, st dmViewStyle) dmViewBlock {
	if s == "" {
		return dmViewBlock{}
	}
	raw := strings.Split(s, "\n")
	w := 0
	for _, l := range raw {
		if n := dmViewWidth(l); n > w {
			w = n
		}
	}
	b := dmViewBlock{w: w, lines: make([][]dmViewRun, len(raw))}
	for i, l := range raw {
		b.lines[i] = []dmViewRun{{text: l + dmViewSpaces(w-dmViewWidth(l)), style: st}}
	}
	return b
}

func dmViewLayoutStack(kids []dmView, st dmViewStyle) dmViewBlock {
	blocks := make([]dmViewBlock, 0, len(kids))
	w := 0
	for _, k := range kids {
		b := dmViewLayout(k, st)
		blocks = append(blocks, b)
		if b.w > w {
			w = b.w
		}
	}
	out := dmViewBlock{w: w}
	for _, b := range blocks {
		for _, line := range b.lines {
			if w > b.w {
				line = append(append([]dmViewRun{}, line...), dmViewRun{text: dmViewSpaces(w - b.w), style: st})
			}
			out.lines = append(out.lines, line)
		}
	}
	return out
}

func dmViewLayoutBeside(kids []dmView, st dmViewStyle) dmViewBlock {
	blocks := make([]dmViewBlock, 0, len(kids))
	h, w := 0, 0
	for _, k := range kids {
		b := dmViewLayout(k, st)
		blocks = append(blocks, b)
		if len(b.lines) > h {
			h = len(b.lines)
		}
		w += b.w
	}
	out := dmViewBlock{w: w, lines: make([][]dmViewRun, h)}
	for row := 0; row < h; row++ {
		var line []dmViewRun
		for _, b := range blocks {
			if row < len(b.lines) {
				line = append(line, b.lines[row]...)
			} else if b.w > 0 {
				line = append(line, dmViewRun{text: dmViewSpaces(b.w), style: st})
			}
		}
		out.lines[row] = line
	}
	return out
}

func dmViewLayoutBox(in dmViewBlock, st dmViewStyle) dmViewBlock {
	w := in.w
	out := dmViewBlock{w: w + 2}
	out.lines = append(out.lines, []dmViewRun{{text: "┌" + strings.Repeat("─", w) + "┐", style: st}})
	for _, line := range in.lines {
		row := []dmViewRun{{text: "│", style: st}}
		row = append(row, line...)
		out.lines = append(out.lines, append(row, dmViewRun{text: "│", style: st}))
	}
	out.lines = append(out.lines, []dmViewRun{{text: "└" + strings.Repeat("─", w) + "┘", style: st}})
	return out
}

func dmViewLayoutMargin(in dmViewBlock, w, h int, st dmViewStyle) dmViewBlock {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	total := in.w + 2*w
	out := dmViewBlock{w: total}
	blank := []dmViewRun{{text: dmViewSpaces(total), style: st}}
	for i := 0; i < h; i++ {
		out.lines = append(out.lines, blank)
	}
	for _, line := range in.lines {
		row := []dmViewRun{{text: dmViewSpaces(w), style: st}}
		row = append(row, line...)
		out.lines = append(out.lines, append(row, dmViewRun{text: dmViewSpaces(w), style: st}))
	}
	for i := 0; i < h; i++ {
		out.lines = append(out.lines, blank)
	}
	return out
}

func dmViewLayoutAlign(in dmViewBlock, w, h int, how string, st dmViewStyle) dmViewBlock {
	if in.w > w {
		w = in.w
	}
	if len(in.lines) > h {
		h = len(in.lines)
	}
	out := dmViewBlock{w: w}
	for _, line := range in.lines {
		gap := w - in.w
		lead, trail := 0, gap
		switch how {
		case "right":
			lead, trail = gap, 0
		case "center":
			lead = gap / 2
			trail = gap - lead
		}
		row := []dmViewRun{}
		if lead > 0 {
			row = append(row, dmViewRun{text: dmViewSpaces(lead), style: st})
		}
		row = append(row, line...)
		if trail > 0 {
			row = append(row, dmViewRun{text: dmViewSpaces(trail), style: st})
		}
		out.lines = append(out.lines, row)
	}
	for len(out.lines) < h {
		out.lines = append(out.lines, []dmViewRun{{text: dmViewSpaces(w), style: st}})
	}
	return out
}

func dmViewPlain(v dmView) string {
	b := dmViewLayout(v, dmViewStyle{})
	lines := make([]string, len(b.lines))
	for i, line := range b.lines {
		var sb strings.Builder
		for _, run := range line {
			sb.WriteString(run.text)
		}
		lines[i] = strings.TrimRight(sb.String(), " ")
	}
	return strings.Join(lines, "\n")
}

var dmViewColours = map[string]bool{
	"black": true, "red": true, "green": true, "yellow": true,
	"blue": true, "magenta": true, "cyan": true, "white": true,
	"bright-black": true, "bright-red": true, "bright-green": true, "bright-yellow": true,
	"bright-blue": true, "bright-magenta": true, "bright-cyan": true, "bright-white": true,
}

func dmViewStyleOf(spec string) dmViewStyle {
	var st dmViewStyle
	words := strings.Fields(spec)
	for i := 0; i < len(words); i++ {
		w := strings.ToLower(words[i])
		switch w {
		case "bold":
			st.bold = true
		case "dim", "faint":
			st.dim = true
		case "underline":
			st.underline = true
		case "reverse":
			st.reverse = true
		case "on":
			if i+1 >= len(words) {
				panic("style(): ` + "`on`" + ` needs a colour after it")
			}
			i++
			c := strings.ToLower(words[i])
			if !dmViewColours[c] {
				panic("style(): unknown colour " + words[i])
			}
			st.bg = c
		default:
			if !dmViewColours[w] {
				panic("style(): unknown style word " + words[i])
			}
			st.fg = w
		}
	}
	return st
}

func dmViewText(s string) dmView  { return dmView{kind: dmVText, text: s} }
func dmViewBlank() dmView         { return dmView{kind: dmVText} }
func dmViewStyled(v dmView, spec string) dmView {
	return dmView{kind: dmVStyled, style: dmViewStyleOf(spec), kids: []dmView{v}}
}
func dmViewStack(kids ...dmView) dmView  { return dmView{kind: dmVStack, kids: kids} }
func dmViewBeside(kids ...dmView) dmView { return dmView{kind: dmVBeside, kids: kids} }
func dmViewBox(v dmView) dmView          { return dmView{kind: dmVBox, kids: []dmView{v}} }
func dmViewMargin(v dmView, w, h int64) dmView {
	return dmView{kind: dmVMargin, w: int(w), h: int(h), kids: []dmView{v}}
}
func dmViewAlign(v dmView, w, h int64, how string) dmView {
	switch how {
	case "left", "center", "right":
	default:
		panic("align(): " + how + " is not an alignment")
	}
	return dmView{kind: dmVAlign, w: int(w), h: int(h), align: how, kids: []dmView{v}}
}
func dmViewFit(v dmView, w, h int64) dmView {
	return dmView{kind: dmVAlign, w: int(w), h: int(h), align: "fit", kids: []dmView{v}}
}`
