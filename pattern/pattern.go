// Package pattern implements the typed-hole template language used by the
// Match Pattern primitive. It is the M0 spike for v0.2: it proves that a
// literal template determines its output type at resolve time (no inference),
// and that the template lowers to a regex for interpretation.
//
// The full Match Pattern primitive (value assembly, Mode: Each, error
// reporting) is built on this package in Milestone 3. This package depends only
// on ir, so it stays free of registry/resolver coupling.
//
// Template grammar (v0.2):
//
//	literal characters match themselves
//	{int}        positional integer        -> Int
//	{word}       positional non-space run  -> Text
//	{text}       positional rest-of-field  -> Text
//	{name:int}   named integer             -> Record field
//	{name:word}  named word                -> Record field
//	{name:text}  named text                -> Record field
//
// Any int or word hole may repeat, which captures one or more elements and
// yields a List of the hole's scalar type:
//
//	{int+ sep=" "}       positional repeated int  -> List<Int>
//	{ns:int+ sep=", "}   named repeated int       -> Record field of List<Int>
//	{ws:word+ sep=","}   named repeated word      -> Record field of List<Text>
//
// The separator is required, and a text hole may not repeat (it is greedy to
// the next literal, so it would swallow its own separators).
//
// A run of *structure* is a group (see group.go):
//
//	[? … ]                    optional — its holes take their type's zero
//	                          when it does not participate
//	{?name}                   inside one: a Bool saying whether it matched
//	{ds:( … )+ sep=", "}      repeated — a List of the inner template's type
//
// Named and positional holes may not be mixed in one template.
package pattern

import (
	"fmt"
	"regexp"
	"strings"

	"domain/ir"
)

// holeNameRE matches legal hole names: names are interpolated directly into a
// Go regexp named capture group (see Hole.regexGroup), so they must satisfy
// Go's named-capture identifier rules or CompileRegex fails with a raw,
// confusing regexp-internals error instead of a clean domain diagnostic.
var holeNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// HoleType is the captured type of a template hole.
type HoleType int

const (
	HoleInt    HoleType = iota // -?[0-9]+           -> Int
	HoleWord                   // a non-space run    -> Text
	HoleText                   // rest of the field  -> Text
	HoleHex                    // [0-9a-fA-F]+       -> Int, base 16
	HoleDigits                 // [0-9]+             -> Text, leading zeros kept
	HoleChar                   // exactly one rune   -> Text
)

// numeric reports the hole types that capture an Int rather than Text.
func (h HoleType) numeric() bool { return h == HoleInt || h == HoleHex }

// CaptureBase is the numeric base a hole type's capture parses in, and whether
// it parses at all. Both backends read it rather than each keeping a switch:
// a hole type added to one and not the other is a compiled program that parses
// differently from the interpreted one.
func CaptureBase(t HoleType) (base int, numeric bool) {
	switch t {
	case HoleInt:
		return 10, true
	case HoleHex:
		return 16, true
	default:
		return 0, false
	}
}

// irType maps a hole type to its Domain value type.
func (h HoleType) irType() *ir.Type {
	if h.numeric() {
		return ir.Int()
	}
	return ir.Text()
}

// Type is the Domain type one hole captures: its scalar type, a List of it
// when the hole repeats, a List of the inner template's type when it repeats a
// group, or Bool for a presence flag.
func (h Hole) irType() *ir.Type {
	switch {
	case h.Flag:
		return ir.Bool()
	case h.Space:
		return nil // never asked: a gap owns no output field
	case h.Group != nil:
		return ir.List(h.Group.OutputType())
	case h.Rep:
		return ir.List(h.Type.irType())
	default:
		return h.Type.irType()
	}
}

// Zero is the value a hole takes when its optional group did not participate.
// A hole inside an optional group is not a different type depending on whether
// the group matched — that would need a sum type — so absence reads as the
// type's zero, and {?flag} is there for when zero and present-but-zero have to
// be told apart.
func (h Hole) Zero() ir.Value {
	switch {
	case h.Flag:
		return false
	case h.Space:
		return nil // never asked: a gap owns no output field
	case h.Group != nil || h.Rep:
		return []ir.Value{}
	case h.Type.numeric():
		return int64(0)
	default:
		return ""
	}
}

// Split breaks a repeated hole's captured run into its elements.
func (h Hole) Split(capture string) []string {
	return strings.Split(capture, h.Sep)
}

// Hole is a single {...} capture in a template.
type Hole struct {
	Name string // "" for a positional hole
	Type HoleType

	// Rep marks a repeated hole — `{ns:int+ sep=", "}` — which captures one or
	// more values separated by Sep and yields a List rather than a scalar.
	//
	// The separator is required rather than defaulted. A default would be
	// right about half the time (a space for `1 2 3`, a comma for `1,2,3`),
	// and a template that silently matches the wrong thing is worse than one
	// that asks.
	Rep bool
	Sep string

	// Group is the inner template of a repeated *group* — `{ds:( {n:int}
	// {c:word} )+ sep=", "}` — which captures one or more copies of it and
	// yields a List of its values. Rep and Sep are set alongside.
	Group *Template

	// Flag marks a `{?name}` presence flag: a Bool saying whether the optional
	// group around it participated. It matches nothing and owns no capture.
	Flag bool

	// Space marks a `{~}`: a run of whitespace, for input whose columns are
	// aligned with a variable number of spaces. It matches `\s+` and owns
	// neither a capture nor an output field — a gap is structure, not data.
	Space bool
}

// capturing reports whether a hole owns a capture group in the regex. A `{?f}`
// flag reads its value from its group's own capture, and a `{~}` gap has no
// value at all.
func (h Hole) capturing() bool { return !h.Flag && !h.Space }

// Segment is a literal run, a hole, or an optional group (exactly one is set).
type Segment struct {
	Literal string
	Hole    *Hole
	Opt     *Optional
}

// Template is a parsed typed-hole template.
type Template struct {
	Raw      string
	Segments []Segment
	Holes    []Hole      // every hole, in order, groups' contents included
	Opts     []*Optional // the optional groups, in order
	Named    bool        // true if holes are named (Record), false if positional (Tuple/List)
}

// ParseTemplate parses a template string, validating hole types and the
// no-mixing rule. It fails (with no position; the caller adds one) on malformed
// templates so resolution can reject them before interpretation.
func ParseTemplate(s string) (*Template, error) {
	t := &Template{Raw: s}
	segs, holes, err := parseSegments(s, s, true)
	if err != nil {
		return nil, err
	}
	t.Segments, t.Holes = segs, holes
	for _, seg := range segs {
		if seg.Opt != nil {
			t.Opts = append(t.Opts, seg.Opt)
		}
	}

	if len(t.Holes) == 0 {
		return nil, fmt.Errorf("template %q has no holes", s)
	}
	sawNamed, sawPositional := false, false
	seenNames := map[string]bool{}
	for _, h := range t.Holes {
		if h.Name == "" {
			sawPositional = true
			continue
		}
		if seenNames[h.Name] {
			return nil, fmt.Errorf("template %q has duplicate hole name %q", s, h.Name)
		}
		seenNames[h.Name] = true
		sawNamed = true
	}
	if sawNamed && sawPositional {
		return nil, fmt.Errorf("template %q mixes named and positional holes", s)
	}
	t.Named = sawNamed
	return t, nil
}

// parseSegments tokenizes a template body into literals, holes and optional
// groups. It is shared by the top level and by an optional group's body, which
// differ only in whether a `[` is allowed (groups do not nest).
//
// The returned hole list is flat and in order — an optional group's holes are
// in it too, since they are ordinary fields of the output whose only difference
// is that they may be absent.
func parseSegments(s, whole string, allowOpt bool) ([]Segment, []Hole, error) {
	var segs []Segment
	var holes []Hole
	var lit strings.Builder
	flushLit := func() {
		if lit.Len() > 0 {
			segs = append(segs, Segment{Literal: lit.String()})
			lit.Reset()
		}
	}

	i := 0
	for i < len(s) {
		switch s[i] {
		case '{':
			inner, next, err := scanHole(s, i)
			if err != nil {
				return nil, nil, fmt.Errorf("unterminated hole starting at offset %d in %q", i, whole)
			}
			hole, err := parseHole(inner)
			if err != nil {
				return nil, nil, err
			}
			if hole.Flag && allowOpt {
				return nil, nil, fmt.Errorf("{?%s} is a presence flag, which only means something "+
					"inside an optional group — write [{?%s} …]", hole.Name, hole.Name)
			}
			flushLit()
			segs = append(segs, Segment{Hole: &hole})
			if !hole.Space {
				// A `{~}` gap is structure, not data: it owns no output field,
				// so it stays out of the hole list the output type is built
				// from and out of the named/positional decision.
				holes = append(holes, hole)
			}
			i = next
		case '[':
			// Only `[?` opens a group; a bare bracket is an ordinary literal,
			// because AoC input is full of them and a template that already
			// matched "[1,2]" has to keep doing so.
			if i+1 >= len(s) || s[i+1] != '?' {
				lit.WriteByte(s[i])
				i++
				continue
			}
			if !allowOpt {
				return nil, nil, fmt.Errorf("template %q nests an optional group inside a group; "+
					"groups do not nest", whole)
			}
			body, next, err := scanOptional(s, i)
			if err != nil {
				return nil, nil, err
			}
			opt, err := parseOptional(body, whole)
			if err != nil {
				return nil, nil, err
			}
			flushLit()
			segs = append(segs, Segment{Opt: opt})
			for _, seg := range opt.Segments {
				if seg.Hole != nil && !seg.Hole.Space {
					holes = append(holes, *seg.Hole)
				}
			}
			i = next
		case '}':
			return nil, nil, fmt.Errorf("unexpected '}' at offset %d in %q", i, whole)
		default:
			lit.WriteByte(s[i])
			i++
		}
	}
	flushLit()
	return segs, holes, nil
}

func parseHole(inner string) (Hole, error) {
	inner = strings.TrimSpace(inner)
	if inner == "~" {
		return Hole{Space: true}, nil
	}
	if strings.HasPrefix(inner, "?") {
		name := strings.TrimSpace(inner[1:])
		if !holeNameRE.MatchString(name) {
			return Hole{}, fmt.Errorf("presence flag {%s} needs a valid identifier for a name", inner)
		}
		return Hole{Name: name, Flag: true}, nil
	}
	name := ""
	typeStr := inner
	// The name separator is the first `:`, but only if it comes before any
	// bracket: a positional group's type is `( {n:int} … )+`, whose first `:`
	// belongs to an inner hole.
	if idx := strings.IndexAny(inner, ":({"); idx >= 0 && inner[idx] == ':' {
		name = strings.TrimSpace(inner[:idx])
		typeStr = strings.TrimSpace(inner[idx+1:])
		if name == "" {
			return Hole{}, fmt.Errorf("hole {%s} has an empty name", inner)
		}
		if !holeNameRE.MatchString(name) {
			return Hole{}, fmt.Errorf("hole {%s} has invalid hole name %q (must be a valid identifier)", inner, name)
		}
	}
	if strings.HasPrefix(typeStr, "(") {
		group, sep, err := parseGroupType(typeStr, inner)
		if err != nil {
			return Hole{}, err
		}
		return Hole{Name: name, Rep: true, Sep: sep, Group: group}, nil
	}
	rep, sep, typeStr, err := parseRepetition(typeStr, inner)
	if err != nil {
		return Hole{}, err
	}
	ht, ok := holeTypeFromString(typeStr)
	if !ok {
		return Hole{}, fmt.Errorf("unknown hole type %q in {%s} "+
			"(want int, hex, digits, word, char, or text)", typeStr, inner)
	}
	// A `text` hole is greedy to the next literal, so a repeated one would
	// swallow its own separators and capture the whole run as element zero.
	// `word` is the repeatable spelling of "some text".
	if rep && ht == HoleText {
		return Hole{}, fmt.Errorf("hole {%s} repeats a text hole, which is greedy to the next "+
			"literal and would swallow its own separators — use word+", inner)
	}
	// `word` and `char` are narrowed to exclude the separator (see
	// Hole.elementClass), but a fixed digit class cannot be: excluding `a` from
	// hex would leave a class that is no longer hex. A separator the class
	// itself matches has no boundary to be, so it is refused rather than
	// silently swallowed.
	if rep && ht.digitShaped() && strings.ContainsAny(sep[:1], ht.chars()) {
		return Hole{}, fmt.Errorf("hole {%s} separates %s elements with %q, which is itself "+
			"one — the run would have no boundary", inner, typeStr, sep)
	}
	return Hole{Name: name, Type: ht, Rep: rep, Sep: sep}, nil
}

// digitShaped reports the hole types whose class is a fixed set of characters,
// so a separator drawn from that set is ambiguous rather than narrowable.
func (t HoleType) digitShaped() bool { return t == HoleHex || t == HoleDigits }

// chars is the literal character set of a digit-shaped class.
func (t HoleType) chars() string {
	if t == HoleHex {
		return "0123456789abcdefABCDEF"
	}
	return "0123456789"
}

// parseRepetition strips a trailing `+ sep="…"` off a hole's type, returning
// whether the hole repeats, its separator, and the bare type left over.
func parseRepetition(typeStr, inner string) (rep bool, sep, bare string, err error) {
	plus := strings.IndexByte(typeStr, '+')
	if plus < 0 {
		return false, "", typeStr, nil
	}
	bare = strings.TrimSpace(typeStr[:plus])
	sep, err = parseSepClause(strings.TrimSpace(typeStr[plus+1:]), inner)
	if err != nil {
		return false, "", "", err
	}
	return true, sep, bare, nil
}

// parseSepClause reads the `sep="…"` that every repetition — of a hole or of a
// group — is required to name.
func parseSepClause(rest, inner string) (string, error) {
	if !strings.HasPrefix(rest, "sep=") {
		return "", fmt.Errorf(`hole {%s} repeats but names no separator — write {…+ sep=", "}`, inner)
	}
	q := strings.TrimSpace(rest[len("sep="):])
	if len(q) < 2 || q[0] != '"' || q[len(q)-1] != '"' {
		return "", fmt.Errorf(`hole {%s} has a separator that is not a quoted string`, inner)
	}
	sep := q[1 : len(q)-1]
	if sep == "" {
		return "", fmt.Errorf("hole {%s} has an empty separator, so its elements have no boundary", inner)
	}
	return sep, nil
}

func holeTypeFromString(s string) (HoleType, bool) {
	switch s {
	case "int":
		return HoleInt, true
	case "hex":
		// Captured as an Int, parsed base 16 — the point of the type is that
		// `#70c710` and `1a2b` arrive as numbers rather than as text a later
		// fromhex() has to convert.
		return HoleHex, true
	case "digits":
		// Text, not Int: a run of digits whose *leading zeros matter* is the
		// only reason to prefer this over {int}, and Int cannot hold them.
		return HoleDigits, true
	case "char":
		return HoleChar, true
	case "word":
		return HoleWord, true
	case "text":
		return HoleText, true
	default:
		return 0, false
	}
}

// OutputType computes the static Domain type this template produces:
//
//   - named holes      -> Record with those fields, in order
//   - positional holes -> List<T> when all holes share a type, else a Tuple
//
// This is the crux of the spike: the type is fully determined by the literal
// template, so Match Pattern needs no type inference at resolve time.
func (t *Template) OutputType() *ir.Type {
	if t.Named {
		fields := make([]ir.Field, len(t.Holes))
		for i, h := range t.Holes {
			fields[i] = ir.Field{Name: h.Name, Type: h.irType()}
		}
		return ir.Record(fields...)
	}

	// Homogeneity is decided on the types themselves rather than on the hole
	// kinds: a repeated hole and a plain one of the same scalar type are
	// List<Int> and Int, and a repeated group is a List of something else
	// again. Comparing what the holes *produce* gets all three right, where a
	// field-by-field comparison of the Hole struct only ever got the case it
	// was written for.
	elems := make([]*ir.Type, len(t.Holes))
	for i, h := range t.Holes {
		elems[i] = h.irType()
	}
	homogeneous := true
	for _, e := range elems[1:] {
		if !e.Equal(elems[0]) {
			homogeneous = false
			break
		}
	}
	if homogeneous {
		return ir.List(elems[0])
	}
	return ir.Tuple(elems...)
}

// CompileRegex lowers the template to an anchored regular expression with one
// capture group per hole (named groups for named holes). This validates the
// lowering strategy from docs/match-pattern.md; the M3 primitive runs it.
func (t *Template) CompileRegex() (*regexp.Regexp, error) {
	return regexp.Compile(t.RegexSource(true))
}

// RegexSource is the anchored pattern CompileRegex builds, with named capture
// groups when named is true and plain ones when it is false.
//
// The compiler needs the unnamed form and used to build it from a switch of
// its own, which is one lowering written twice — a repeated hole added to one
// and not the other would compile to a program that parsed differently from
// the one the interpreter ran. There is one of them now.
func (t *Template) RegexSource(named bool) string {
	return t.regexSource(named, true)
}

// ScanSource is RegexSource without the anchors: the pattern `Mode: Scan` uses
// to find every occurrence of the template inside a line rather than requiring
// the line to be one.
func (t *Template) ScanSource(named bool) string {
	return t.regexSource(named, false)
}

func (t *Template) regexSource(named, anchored bool) string {
	mode := capPlain
	if named {
		mode = capNamed
	}
	src, _ := t.lowerAnchored(mode, anchored)
	return src
}

// CompileScan is CompileRegex for the unanchored form.
func (t *Template) CompileScan() (*regexp.Regexp, error) {
	return regexp.Compile(t.ScanSource(true))
}

// pattern is the hole's capture group, in the given spelling.
func (h Hole) pattern(mode captureMode) string {
	if h.Group != nil {
		// A repeated group is the scalar repetition one level down: the run is
		// one capture, split on the separator and re-matched element by element
		// afterwards. The element pattern captures nothing, so the outer group
		// count stays equal to the plan's.
		elem := h.Group.elementSource()
		s := regexp.QuoteMeta(h.Sep)
		return h.wrap("(?:"+elem+")(?:"+s+"(?:"+elem+"))*", mode)
	}
	if h.Space {
		return `\s+`
	}
	body := h.Type.class()
	if h.Rep {
		// One capture over the whole run, split by Sep afterwards. A group per
		// element is not an option: a Go regexp keeps only the last match of a
		// repeated group, so the capture has to be the run itself.
		elem := h.elementClass()
		s := regexp.QuoteMeta(h.Sep)
		body = "(?:" + elem + ")(?:" + s + "(?:" + elem + "))*"
	}
	return h.wrap(body, mode)
}

// class is the character pattern one occurrence of a hole type matches.
func (t HoleType) class() string {
	switch t {
	case HoleInt:
		return `-?\d+`
	case HoleHex:
		return `[0-9a-fA-F]+`
	case HoleDigits:
		return `[0-9]+`
	case HoleWord:
		return `\S+`
	case HoleChar:
		return `.`
	default: // HoleText
		return `.*`
	}
}

// elementClass is what one element of a *repeated* hole matches.
//
// `word` is `\S+` and `char` is `.`, either of which would happily eat a
// separator — so both are narrowed to exclude the separator's first byte,
// which is what makes `a,b,c` three elements rather than one. The digit-shaped
// classes need no narrowing because parseRepetition refuses a separator that
// starts with a character the class itself matches.
func (h Hole) elementClass() string {
	excl := regexp.QuoteMeta(string(h.Sep[0]))
	switch h.Type {
	case HoleWord:
		return `[^\s` + excl + `]+`
	case HoleChar:
		return `[^` + excl + `]`
	default:
		return h.Type.class()
	}
}
