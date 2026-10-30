package codegen

// Lowerings for rows of integers parsed straight out of text: the fused form
// of Split + Split Each + Convert To Integers (see tryFuse).

// declParseFields parses every sep-separated field of one line, left to right,
// into a slice sized once. It is strings.Split followed by dmParseIntSeg over
// the pieces, without the []string in between.
const declParseFields = `func dmParseFields(s, sep string) []int64 {
	out := make([]int64, 0, strings.Count(s, sep)+1)
	for {
		i := strings.Index(s, sep)
		if i < 0 {
			return append(out, dmParseIntSeg(s))
		}
		out = append(out, dmParseIntSeg(s[:i]))
		s = s[i+len(sep):]
	}
}`

func (g *gen) parseFieldsHelpers() {
	g.imp("strings")
	g.helper("dmFail", declFail, "fmt", "os")
	g.helper("dmParseInt", declParseInt, "strconv", "strings")
	g.helper("dmParseIntSeg", declParseIntSeg)
	g.helper("dmParseFields", declParseFields, "strings")
}

// emitFieldRowsInts turns a []string of lines into [][]int64.
func (g *gen) emitFieldRowsInts(in, fsep string) string {
	g.parseFieldsHelpers()
	v, i, line := g.fresh("v"), g.fresh("i"), g.fresh("line")
	g.wl("%s := make([][]int64, len(%s))", v, in)
	g.wl("for %s, %s := range %s {", i, line, in)
	g.in()
	g.wl("%s[%s] = dmParseFields(%s, %s)", v, i, line, goStr(fsep))
	g.out()
	g.wl("}")
	return v
}

// emitSplitRowsInts turns text into [][]int64 without materializing the lines.
func (g *gen) emitSplitRowsInts(in, lsep, fsep string) string {
	g.parseFieldsHelpers()
	v, rest, idx, line := g.fresh("v"), g.fresh("rest"), g.fresh("idx"), g.fresh("line")
	g.wl("%s := make([][]int64, 0, strings.Count(%s, %s)+1)", v, in, goStr(lsep))
	g.wl("%s := %s", rest, in)
	g.wl("for {")
	g.in()
	g.wl("%s := strings.Index(%s, %s)", idx, rest, goStr(lsep))
	g.wl("%s := %s", line, rest)
	g.wl("if %s >= 0 {", idx)
	g.in()
	g.wl("%s = %s[:%s]", line, rest, idx)
	g.out()
	g.wl("}")
	g.wl("%s = append(%s, dmParseFields(%s, %s))", v, v, line, goStr(fsep))
	g.wl("if %s < 0 {", idx)
	g.in()
	g.wl("break")
	g.out()
	g.wl("}")
	g.wl("%s = %s[%s+%d:]", rest, rest, idx, len(lsep))
	g.out()
	g.wl("}")
	return v
}
