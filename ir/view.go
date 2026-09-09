package ir

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The View value: an opaque tree of text, style and layout.
//
// A View is what a program builds when it is describing a picture rather than
// computing an answer — a rendered frame, a panel, a board. It is built by the
// render builtins (text, style, stack, beside, box, margin, align, fit, draw)
// and never by arithmetic: it is not keyable, not orderable, and not
// comparable, so a program cannot ask a question of it that its shape cannot
// answer.
//
// **Layout happens here, once.** RenderViewPlain and any styled renderer share
// the same laid-out result and differ only in how they serialize a run of
// text — the plain one drops the styles, a terminal one wraps each run in
// escapes. That is what keeps a replayed frame and a played one geometrically
// identical, which the whole testing story rests on: if styling could change
// where a column lands, a golden frame would only be evidence about the
// renderer that produced it.
//
// **Width is counted in runes**, matching `length` on Text ("number of
// runes"). A display-width count would be better for CJK and emoji, and it is
// not available here — ir depends on nothing outside the standard library, and
// the alternative is two width functions that disagree.

// ViewKind is the shape of one node of a render tree.
type ViewKind int

const (
	VText   ViewKind = iota // a run of text; embedded newlines make several lines
	VStyled                 // one child, with a style applied to all of its text
	VStack                  // children stacked vertically, left-aligned
	VBeside                 // children side by side, top-aligned
	VBox                    // one child inside a drawn border
	VMargin                 // one child with W columns and H rows of space around it
	VAlign                  // one child placed in a W×H area
)

// ViewStyle is how a run of text is drawn. The zero value is unstyled, and
// styles compose outside-in: an inner style wins over the one it sits in.
type ViewStyle struct {
	FG, BG                        string // colour names; "" is the terminal's own
	Bold, Dim, Underline, Reverse bool
}

// merge layers an inner style over an outer one. Anything the inner style does
// not set is inherited, so `style(stack(a, b), "blue")` colours both children
// while a child that names its own colour keeps it.
func (s ViewStyle) merge(inner ViewStyle) ViewStyle {
	out := s
	if inner.FG != "" {
		out.FG = inner.FG
	}
	if inner.BG != "" {
		out.BG = inner.BG
	}
	out.Bold = out.Bold || inner.Bold
	out.Dim = out.Dim || inner.Dim
	out.Underline = out.Underline || inner.Underline
	out.Reverse = out.Reverse || inner.Reverse
	return out
}

// ViewValue is one node of a render tree.
type ViewValue struct {
	Kind  ViewKind
	Text  string    // VText
	Style ViewStyle // VStyled
	W, H  int       // VMargin, VAlign
	Align string    // VAlign: "left", "center", "right"
	Kids  []*ViewValue
}

// Constructors, one per builtin, so the builtins stay a thin layer over this.

func TextView(s string) *ViewValue { return &ViewValue{Kind: VText, Text: s} }

// BlankView is empty: no lines at all. It is what an empty stack renders as,
// and what a program uses to draw nothing in a slot.
func BlankView() *ViewValue { return &ViewValue{Kind: VText} }

func StyledView(v *ViewValue, st ViewStyle) *ViewValue {
	return &ViewValue{Kind: VStyled, Style: st, Kids: []*ViewValue{v}}
}
func StackView(kids ...*ViewValue) *ViewValue  { return &ViewValue{Kind: VStack, Kids: kids} }
func BesideView(kids ...*ViewValue) *ViewValue { return &ViewValue{Kind: VBeside, Kids: kids} }
func BoxView(v *ViewValue) *ViewValue          { return &ViewValue{Kind: VBox, Kids: []*ViewValue{v}} }
func MarginView(v *ViewValue, w, h int) *ViewValue {
	return &ViewValue{Kind: VMargin, W: w, H: h, Kids: []*ViewValue{v}}
}
func AlignView(v *ViewValue, w, h int, how string) *ViewValue {
	return &ViewValue{Kind: VAlign, W: w, H: h, Align: how, Kids: []*ViewValue{v}}
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

// ViewRun is a stretch of text drawn in one style.
type ViewRun struct {
	Text  string
	Style ViewStyle
}

// ViewLine is one rendered row: runs left to right.
type ViewLine []ViewRun

// ViewBlock is a laid-out View: rectangular, every line exactly W runes wide.
type ViewBlock struct {
	Lines []ViewLine
	W     int
}

// LayoutView reduces a render tree to a rectangle of styled runs. It is the
// single implementation of the geometry: every renderer starts here.
func LayoutView(v *ViewValue) *ViewBlock {
	return layout(v, ViewStyle{})
}

func layout(v *ViewValue, st ViewStyle) *ViewBlock {
	if v == nil {
		return &ViewBlock{}
	}
	switch v.Kind {
	case VText:
		return layoutText(v.Text, st)
	case VStyled:
		return layout(child(v), st.merge(v.Style))
	case VStack:
		return layoutStack(v.Kids, st)
	case VBeside:
		return layoutBeside(v.Kids, st)
	case VBox:
		return layoutBox(layout(child(v), st), st)
	case VMargin:
		return layoutMargin(layout(child(v), st), v.W, v.H, st)
	case VAlign:
		return layoutAlign(layout(child(v), st), v.W, v.H, v.Align, st)
	}
	return &ViewBlock{}
}

func child(v *ViewValue) *ViewValue {
	if len(v.Kids) == 0 {
		return nil
	}
	return v.Kids[0]
}

// layoutText splits on newlines and pads every line to the widest, so a block
// of text is a rectangle like everything else.
func layoutText(s string, st ViewStyle) *ViewBlock {
	if s == "" {
		return &ViewBlock{}
	}
	raw := strings.Split(s, "\n")
	w := 0
	for _, l := range raw {
		w = max(w, runeWidth(l))
	}
	b := &ViewBlock{W: w, Lines: make([]ViewLine, len(raw))}
	for i, l := range raw {
		b.Lines[i] = ViewLine{{Text: l + spaces(w-runeWidth(l)), Style: st}}
	}
	return b
}

func layoutStack(kids []*ViewValue, st ViewStyle) *ViewBlock {
	blocks := make([]*ViewBlock, 0, len(kids))
	w := 0
	for _, k := range kids {
		b := layout(k, st)
		blocks = append(blocks, b)
		w = max(w, b.W)
	}
	out := &ViewBlock{W: w}
	for _, b := range blocks {
		for _, line := range b.Lines {
			out.Lines = append(out.Lines, padLine(line, b.W, w, st))
		}
	}
	return out
}

func layoutBeside(kids []*ViewValue, st ViewStyle) *ViewBlock {
	blocks := make([]*ViewBlock, 0, len(kids))
	h, w := 0, 0
	for _, k := range kids {
		b := layout(k, st)
		blocks = append(blocks, b)
		h = max(h, len(b.Lines))
		w += b.W
	}
	out := &ViewBlock{W: w, Lines: make([]ViewLine, h)}
	for row := range h {
		var line ViewLine
		for _, b := range blocks {
			if row < len(b.Lines) {
				line = append(line, b.Lines[row]...)
			} else if b.W > 0 {
				// A column shorter than its neighbours is blank below, not
				// absent: dropping the padding would slide every column to
				// its right leftwards on those rows.
				line = append(line, ViewRun{Text: spaces(b.W), Style: st})
			}
		}
		out.Lines[row] = line
	}
	return out
}

// Box-drawing characters, in one place so the two renderers cannot disagree.
const (
	boxTL, boxTR, boxBL, boxBR = "┌", "┐", "└", "┘"
	boxH, boxV                 = "─", "│"
)

func layoutBox(inner *ViewBlock, st ViewStyle) *ViewBlock {
	w := inner.W
	out := &ViewBlock{W: w + 2}
	out.Lines = append(out.Lines, ViewLine{{Text: boxTL + strings.Repeat(boxH, w) + boxTR, Style: st}})
	for _, line := range inner.Lines {
		row := ViewLine{{Text: boxV, Style: st}}
		row = append(row, line...)
		out.Lines = append(out.Lines, append(row, ViewRun{Text: boxV, Style: st}))
	}
	out.Lines = append(out.Lines, ViewLine{{Text: boxBL + strings.Repeat(boxH, w) + boxBR, Style: st}})
	return out
}

func layoutMargin(inner *ViewBlock, w, h int, st ViewStyle) *ViewBlock {
	w, h = max(w, 0), max(h, 0)
	total := inner.W + 2*w
	out := &ViewBlock{W: total}
	blank := ViewLine{{Text: spaces(total), Style: st}}
	for range h {
		out.Lines = append(out.Lines, blank)
	}
	for _, line := range inner.Lines {
		row := ViewLine{{Text: spaces(w), Style: st}}
		row = append(row, line...)
		out.Lines = append(out.Lines, append(row, ViewRun{Text: spaces(w), Style: st}))
	}
	for range h {
		out.Lines = append(out.Lines, blank)
	}
	return out
}

// layoutAlign places a block in a w×h area. A block larger than the area is
// left as it is rather than clipped: losing part of a picture silently is
// worse than overflowing one, and `fit` is where clipping is asked for.
func layoutAlign(inner *ViewBlock, w, h int, how string, st ViewStyle) *ViewBlock {
	w, h = max(w, inner.W), max(h, len(inner.Lines))
	out := &ViewBlock{W: w}
	for _, line := range inner.Lines {
		gap := w - inner.W
		var lead, trail int
		switch how {
		case "right":
			lead, trail = gap, 0
		case "center":
			lead = gap / 2
			trail = gap - lead
		default:
			lead, trail = 0, gap
		}
		row := ViewLine{}
		if lead > 0 {
			row = append(row, ViewRun{Text: spaces(lead), Style: st})
		}
		row = append(row, line...)
		if trail > 0 {
			row = append(row, ViewRun{Text: spaces(trail), Style: st})
		}
		out.Lines = append(out.Lines, row)
	}
	for len(out.Lines) < h {
		out.Lines = append(out.Lines, ViewLine{{Text: spaces(w), Style: st}})
	}
	return out
}

// FitView clips or pads a block to exactly w×h. It is the one operation that
// may lose part of a picture, which is why it is asked for by name.
func FitView(v *ViewValue, w, h int) *ViewValue {
	return &ViewValue{Kind: VAlign, W: w, H: h, Align: "fit", Kids: []*ViewValue{v}}
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// RenderViewPlain draws a View as text with no styling at all.
//
// Trailing spaces are trimmed from every line. They are invisible on a
// terminal and poisonous in a golden file, where an editor stripping them
// turns a passing test into a failing one for no reason a reader can see.
func RenderViewPlain(v *ViewValue) string {
	b := LayoutView(v)
	lines := make([]string, len(b.Lines))
	for i, line := range b.Lines {
		var sb strings.Builder
		for _, run := range line {
			sb.WriteString(run.Text)
		}
		lines[i] = strings.TrimRight(sb.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// ParseViewStyle reads a style spec: space-separated words, any order.
// Colours are named; `on <colour>` sets the background.
//
// An unknown word is an error rather than a no-op, because a misspelled
// colour that silently does nothing is a bug you find by squinting at a
// screenshot.
func ParseViewStyle(spec string) (ViewStyle, error) {
	var st ViewStyle
	words := strings.Fields(spec)
	for i := 0; i < len(words); i++ {
		w := strings.ToLower(words[i])
		switch w {
		case "bold":
			st.Bold = true
		case "dim", "faint":
			st.Dim = true
		case "underline":
			st.Underline = true
		case "reverse":
			st.Reverse = true
		case "on":
			if i+1 >= len(words) {
				return st, errStyle("`on` needs a colour after it")
			}
			i++
			c := strings.ToLower(words[i])
			if !viewColours[c] {
				return st, errStyle("unknown colour %q; the colours are: %s", words[i], viewColourList())
			}
			st.BG = c
		default:
			if !viewColours[w] {
				return st, errStyle("unknown style word %q; the words are bold, dim, underline, reverse, a colour, or `on <colour>`", words[i])
			}
			st.FG = w
		}
	}
	return st, nil
}

// viewColours is the closed set of colour names, chosen to be the eight ANSI
// colours plus their bright forms: every terminal has them, and a program that
// names one gets the user's own palette rather than a guess at it.
var viewColours = map[string]bool{
	"black": true, "red": true, "green": true, "yellow": true,
	"blue": true, "magenta": true, "cyan": true, "white": true,
	"bright-black": true, "bright-red": true, "bright-green": true, "bright-yellow": true,
	"bright-blue": true, "bright-magenta": true, "bright-cyan": true, "bright-white": true,
}

func viewColourList() string {
	return "black, red, green, yellow, blue, magenta, cyan, white, and their bright- forms"
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// errStyle builds the error ParseViewStyle returns. It exists so every
// refusal reads the same way and names what is allowed.
func errStyle(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func runeWidth(s string) int { return utf8.RuneCountInString(s) }

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// padLine widens one already-laid-out line from have to want.
func padLine(line ViewLine, have, want int, st ViewStyle) ViewLine {
	if want <= have {
		return line
	}
	return append(append(ViewLine{}, line...), ViewRun{Text: spaces(want - have), Style: st})
}
