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
	HoleInt HoleType = iota
	HoleWord
	HoleText
)

// irType maps a hole type to its Domain value type.
func (h HoleType) irType() *ir.Type {
	if h == HoleInt {
		return ir.Int()
	}
	return ir.Text()
}

// Hole is a single {...} capture in a template.
type Hole struct {
	Name string // "" for a positional hole
	Type HoleType
}

// Segment is either a literal run or a hole (exactly one is set).
type Segment struct {
	Literal string
	Hole    *Hole
}

// Template is a parsed typed-hole template.
type Template struct {
	Raw      string
	Segments []Segment
	Holes    []Hole // in order of appearance
	Named    bool   // true if holes are named (Record), false if positional (Tuple/List)
}

// ParseTemplate parses a template string, validating hole types and the
// no-mixing rule. It fails (with no position; the caller adds one) on malformed
// templates so resolution can reject them before interpretation.
func ParseTemplate(s string) (*Template, error) {
	t := &Template{Raw: s}
	var lit strings.Builder
	flushLit := func() {
		if lit.Len() > 0 {
			t.Segments = append(t.Segments, Segment{Literal: lit.String()})
			lit.Reset()
		}
	}

	sawNamed, sawPositional := false, false
	seenNames := map[string]bool{}
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case '{':
			rel := strings.IndexByte(s[i:], '}')
			if rel < 0 {
				return nil, fmt.Errorf("unterminated hole starting at offset %d in %q", i, s)
			}
			inner := s[i+1 : i+rel]
			hole, err := parseHole(inner)
			if err != nil {
				return nil, err
			}
			if hole.Name != "" {
				if seenNames[hole.Name] {
					return nil, fmt.Errorf("template %q has duplicate hole name %q", s, hole.Name)
				}
				seenNames[hole.Name] = true
				sawNamed = true
			} else {
				sawPositional = true
			}
			flushLit()
			t.Segments = append(t.Segments, Segment{Hole: &hole})
			t.Holes = append(t.Holes, hole)
			i += rel + 1
		case '}':
			return nil, fmt.Errorf("unexpected '}' at offset %d in %q", i, s)
		default:
			lit.WriteByte(c)
			i++
		}
	}
	flushLit()

	if len(t.Holes) == 0 {
		return nil, fmt.Errorf("template %q has no holes", s)
	}
	if sawNamed && sawPositional {
		return nil, fmt.Errorf("template %q mixes named and positional holes", s)
	}
	t.Named = sawNamed
	return t, nil
}

func parseHole(inner string) (Hole, error) {
	inner = strings.TrimSpace(inner)
	name := ""
	typeStr := inner
	if idx := strings.IndexByte(inner, ':'); idx >= 0 {
		name = strings.TrimSpace(inner[:idx])
		typeStr = strings.TrimSpace(inner[idx+1:])
		if name == "" {
			return Hole{}, fmt.Errorf("hole {%s} has an empty name", inner)
		}
		if !holeNameRE.MatchString(name) {
			return Hole{}, fmt.Errorf("hole {%s} has invalid hole name %q (must be a valid identifier)", inner, name)
		}
	}
	ht, ok := holeTypeFromString(typeStr)
	if !ok {
		return Hole{}, fmt.Errorf("unknown hole type %q in {%s} (want int, word, or text)", typeStr, inner)
	}
	return Hole{Name: name, Type: ht}, nil
}

func holeTypeFromString(s string) (HoleType, bool) {
	switch s {
	case "int":
		return HoleInt, true
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
			fields[i] = ir.Field{Name: h.Name, Type: h.Type.irType()}
		}
		return ir.Record(fields...)
	}

	first := t.Holes[0].Type
	homogeneous := true
	for _, h := range t.Holes {
		if h.Type != first {
			homogeneous = false
			break
		}
	}
	if homogeneous {
		return ir.List(first.irType())
	}
	elems := make([]*ir.Type, len(t.Holes))
	for i, h := range t.Holes {
		elems[i] = h.Type.irType()
	}
	return ir.Tuple(elems...)
}

// CompileRegex lowers the template to an anchored regular expression with one
// capture group per hole (named groups for named holes). This validates the
// lowering strategy from docs/match-pattern.md; the M3 primitive runs it.
func (t *Template) CompileRegex() (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	for _, seg := range t.Segments {
		if seg.Hole != nil {
			sb.WriteString(seg.Hole.regexGroup())
		} else {
			sb.WriteString(regexp.QuoteMeta(seg.Literal))
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

func (h Hole) regexGroup() string {
	var body string
	switch h.Type {
	case HoleInt:
		body = `-?\d+`
	case HoleWord:
		body = `\S+`
	case HoleText:
		body = `.*`
	}
	if h.Name != "" {
		return "(?P<" + h.Name + ">" + body + ")"
	}
	return "(" + body + ")"
}
