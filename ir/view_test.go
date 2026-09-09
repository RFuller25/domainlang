package ir

import (
	"strings"
	"testing"
)

func plain(t *testing.T, v *ViewValue) string {
	t.Helper()
	return RenderViewPlain(v)
}

func TestViewText(t *testing.T) {
	if got := plain(t, TextView("hello")); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
	// Embedded newlines make several lines, and the block is padded to the
	// widest of them — which only shows once something sits beside it.
	if got := plain(t, TextView("ab\ncdef")); got != "ab\ncdef" {
		t.Errorf("got %q", got)
	}
	if got := plain(t, BlankView()); got != "" {
		t.Errorf("blank rendered %q, want empty", got)
	}
}

func TestViewStack(t *testing.T) {
	got := plain(t, StackView(TextView("one"), TextView("two")))
	if got != "one\ntwo" {
		t.Errorf("got %q, want %q", got, "one\ntwo")
	}
	if got := plain(t, StackView()); got != "" {
		t.Errorf("empty stack rendered %q, want empty", got)
	}
}

// Side by side is where the geometry actually has to be right: a short column
// must still occupy its width, or everything to its right slides left.
func TestViewBeside(t *testing.T) {
	left := StackView(TextView("aa"), TextView("bb"), TextView("cc"))
	right := StackView(TextView("1"), TextView("2"))
	got := plain(t, BesideView(left, right))
	want := "aa1\nbb2\ncc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestViewBesideRaggedWidths(t *testing.T) {
	// The left column is 4 wide, so its short first row is padded and the bar
	// lands in column 5 on the row that has one.
	left := StackView(TextView("a"), TextView("bbbb"))
	if got, want := plain(t, BesideView(left, TextView("|"))), "a   |\nbbbb"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A column shorter than its neighbours must still hold its width on the
	// rows it does not reach, or everything to its right slides left. The
	// middle column here has one row; the third must stay in column 6 on both.
	got := plain(t, BesideView(left, TextView("|"), TextView("X\nY")))
	if want := "a   |X\nbbbb Y"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestViewBox(t *testing.T) {
	got := plain(t, BoxView(TextView("hi")))
	want := "┌──┐\n│hi│\n└──┘"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A box around nothing is a box, not a crash.
	if got := plain(t, BoxView(BlankView())); got != "┌┐\n└┘" {
		t.Errorf("empty box = %q", got)
	}
}

func TestViewMargin(t *testing.T) {
	got := plain(t, BoxView(MarginView(TextView("x"), 2, 1)))
	want := strings.Join([]string{
		"┌─────┐",
		"│     │",
		"│  x  │",
		"│     │",
		"└─────┘",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestViewAlign(t *testing.T) {
	cases := []struct{ how, want string }{
		{"left", "ab   |"},
		{"center", " ab  |"},
		{"right", "   ab|"},
	}
	for _, c := range cases {
		got := plain(t, BesideView(AlignView(TextView("ab"), 5, 1, c.how), TextView("|")))
		if got != c.want {
			t.Errorf("align %s: got %q, want %q", c.how, got, c.want)
		}
	}
}

// Aligning into an area smaller than the content leaves the content alone.
// Losing part of a picture without being asked is worse than overflowing.
func TestViewAlignNeverClips(t *testing.T) {
	got := plain(t, AlignView(TextView("abcdef"), 2, 1, "center"))
	if got != "abcdef" {
		t.Errorf("got %q, want the content intact", got)
	}
}

// Styling must not change geometry: that is what lets a replayed frame and a
// played one be the same picture.
func TestStyleDoesNotChangeLayout(t *testing.T) {
	bare := BesideView(TextView("ab"), TextView("|"))
	styled := BesideView(StyledView(TextView("ab"), ViewStyle{Bold: true, FG: "red"}), TextView("|"))
	if plain(t, bare) != plain(t, styled) {
		t.Errorf("styled %q differs from bare %q", plain(t, styled), plain(t, bare))
	}
	b := LayoutView(styled)
	if b.W != 3 {
		t.Errorf("width = %d, want 3", b.W)
	}
}

// A style applies to everything inside it, and an inner style wins.
func TestStyleMergesOutsideIn(t *testing.T) {
	v := StyledView(
		StackView(TextView("a"), StyledView(TextView("b"), ViewStyle{FG: "green"})),
		ViewStyle{FG: "red", Bold: true})
	b := LayoutView(v)
	if len(b.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(b.Lines))
	}
	if st := b.Lines[0][0].Style; st.FG != "red" || !st.Bold {
		t.Errorf("outer line style = %+v, want red bold", st)
	}
	if st := b.Lines[1][0].Style; st.FG != "green" || !st.Bold {
		t.Errorf("inner line style = %+v, want green (inherited bold)", st)
	}
}

// Trailing spaces are trimmed: invisible on a terminal, poisonous in a golden
// file that an editor might strip.
func TestPlainRenderTrimsTrailingSpace(t *testing.T) {
	got := plain(t, StackView(TextView("aaaa"), TextView("b")))
	if got != "aaaa\nb" {
		t.Errorf("got %q, want no trailing padding", got)
	}
}

func TestParseViewStyle(t *testing.T) {
	st, err := ParseViewStyle("bold red on blue underline")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := ViewStyle{FG: "red", BG: "blue", Bold: true, Underline: true}
	if st != want {
		t.Errorf("got %+v, want %+v", st, want)
	}
	if st, err := ParseViewStyle(""); err != nil || st != (ViewStyle{}) {
		t.Errorf("empty spec = %+v, %v; want the zero style", st, err)
	}
}

// A misspelled colour that silently did nothing is a bug found by squinting
// at a screenshot, so it is refused by name.
func TestParseViewStyleRefusesUnknownWords(t *testing.T) {
	for _, spec := range []string{"reddish", "on", "on chartreuse", "bold puce"} {
		if _, err := ParseViewStyle(spec); err == nil {
			t.Errorf("ParseViewStyle(%q) was accepted", spec)
		}
	}
}

func TestViewTypeIsOpaque(t *testing.T) {
	v := View()
	if v.String() != "View" {
		t.Errorf("String() = %q, want View", v.String())
	}
	if Keyable(v) {
		t.Error("a View must not be keyable: it cannot be a Map key or a Set element")
	}
	if !v.Equal(View()) {
		t.Error("two Views are the same type")
	}
	if v.Equal(Text()) {
		t.Error("a View is not Text")
	}
}

func TestViewValueDescribesAndFormats(t *testing.T) {
	v := BoxView(TextView("hi"))
	if got := DescribeValue(v); got != "View" {
		t.Errorf("DescribeValue = %q, want View", got)
	}
	if got := FormatValue(v); got != "┌──┐\n│hi│\n└──┘" {
		t.Errorf("FormatValue = %q", got)
	}
	if got := FormatValueTyped(v, View()); got != FormatValue(v) {
		t.Errorf("typed formatting differs: %q", got)
	}
}
