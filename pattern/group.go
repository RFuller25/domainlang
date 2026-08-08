package pattern

import (
	"fmt"
	"regexp"
	"strings"
)

// Groups: the two bracketed forms a template may contain.
//
//	[? … ]                     an optional group — literals and holes that may
//	                           be absent, in which case every hole inside takes
//	                           its type's zero
//	{?name}                    a presence flag: a Bool saying whether the
//	                           optional group around it participated
//	{ns:( … )+ sep=", "}       a repeated group — one or more copies of an
//	                           inner template, yielding a List of its values
//
// The optional group's marker is `[?`, not a bare `[`, and that is the whole
// reason it is spelled that way: `[` and `]` are ordinary characters in AoC
// input, and a template like "[{a:int},{b:int}]" was legal before this and has
// to keep meaning what it meant. A bare bracket stays a literal; only `[?`
// opens a group. The flag reuses hole syntax rather than a bracket attribute
// (`[?name …]`, `[?name: …]`) because both of those are ambiguous against a
// group whose body starts with a literal of that shape — and `[?Time: {n:int}]`
// is exactly the kind of thing AoC input contains.
//
// One level only: a group holds literals and holes, never another group. That
// keeps the inner template a plain *Template — the same type, the same
// lowering, the same tests — and it is what every AoC input needs.

// Optional is a `[ … ]` group: a run of segments that may be absent.
type Optional struct {
	Segments []Segment // literals and holes, no nested groups
	Holes    []Hole    // the capture-owning holes inside, in order
	Flag     string    // the {?name} presence flag, or "" if the group has none
}

// scanHole returns the contents of the `{…}` starting at s[i], and the index
// just past its `}`. Braces nest, because a repeated group's inner template has
// holes of its own — `{ds:( {n:int} {c:word} )+ sep=", "}` — and the naive
// IndexByte the parser used before groups stops at the first inner one.
func scanHole(s string, i int) (inner string, next int, err error) {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i+1 : j], j + 1, nil
			}
		}
	}
	return "", 0, fmt.Errorf("unterminated hole starting at offset %d in %q", i, s)
}

// scanOptional returns the body of the `[? … ]` starting at s[i], and the index
// just past its `]`. There is no nesting to count — groups do not nest and a
// bare `[` is a literal — so the group ends at the first `]` that is not inside
// a hole, which is what lets a hole carry `sep="]"`.
func scanOptional(s string, i int) (body string, next int, err error) {
	braces := 0
	for j := i + 2; j < len(s); j++ {
		switch {
		case s[j] == '{':
			braces++
		case s[j] == '}' && braces > 0:
			braces--
		case braces > 0:
			// inside a hole
		case s[j] == ']':
			return s[i+2 : j], j + 1, nil
		}
	}
	return "", 0, fmt.Errorf("unterminated optional group `[?` starting at offset %d in %q", i, s)
}

// scanDelimited returns the contents of a run starting at s[i] (which must be
// open) and ending at the matching close, ignoring both delimiters wherever
// they fall inside a hole — so a separator of `sep=")"` or `sep="]"` does not
// close the group around it.
func scanDelimited(s string, i int, open, close byte) (inner string, next int, err error) {
	depth, braces := 0, 0
	for j := i; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '{':
			braces++
		case c == '}' && braces > 0:
			braces--
		case braces > 0:
			// inside a hole; our delimiters are that hole's business
		case c == open:
			depth++
		case c == close:
			depth--
			if depth == 0 {
				return s[i+1 : j], j + 1, nil
			}
		}
	}
	return "", 0, fmt.Errorf("unterminated %q starting at offset %d in %q", string(open), i, s)
}

// parseOptional parses the body of a `[ … ]` group.
func parseOptional(body, whole string) (*Optional, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("optional group [?] in %q is empty, so there is nothing for it to "+
			"make optional", whole)
	}
	opt := &Optional{}
	segs, holes, err := parseSegments(body, whole, false)
	if err != nil {
		return nil, err
	}
	for _, h := range holes {
		if h.Space {
			continue
		}
		if h.Flag {
			if opt.Flag != "" {
				return nil, fmt.Errorf("optional group [?%s] declares two presence flags, %q and %q",
					body, opt.Flag, h.Name)
			}
			opt.Flag = h.Name
			continue
		}
		opt.Holes = append(opt.Holes, h)
	}
	if len(opt.Holes) == 0 && opt.Flag == "" {
		return nil, fmt.Errorf("optional group [?%s] has no holes and no {?flag}, so nothing "+
			"records whether it matched — make its text a literal, or add a flag", body)
	}
	opt.Segments = segs
	return opt, nil
}

// parseGroupType parses a hole's `( … )+ sep="…"` type: the inner template and
// the separator between its repetitions.
func parseGroupType(typeStr, inner string) (*Template, string, error) {
	body, next, err := scanDelimited(typeStr, 0, '(', ')')
	if err != nil {
		return nil, "", fmt.Errorf("hole {%s}: %v", inner, err)
	}
	rest := strings.TrimSpace(typeStr[next:])
	if !strings.HasPrefix(rest, "+") {
		return nil, "", fmt.Errorf(`hole {%s} groups without repeating it — a group is only `+
			`useful repeated, so write {…:( … )+ sep=", "}`, inner)
	}
	rest = strings.TrimSpace(rest[1:])
	sep, err := parseSepClause(rest, inner)
	if err != nil {
		return nil, "", err
	}
	// Padding inside the parentheses is formatting, not part of the element:
	// `( {n:int} {c:word} )` reads better than `({n:int} {c:word})` and the two
	// have to mean the same thing, or the prettier spelling silently matches
	// something else. A space that really is part of the element boundary
	// belongs in the separator.
	sub, err := parseInner(strings.TrimSpace(body))
	if err != nil {
		return nil, "", fmt.Errorf("hole {%s}: %v", inner, err)
	}
	return sub, sep, nil
}

// parseInner parses a repeated group's inner template and enforces the
// one-level rule.
func parseInner(body string) (*Template, error) {
	t, err := ParseTemplate(body)
	if err != nil {
		return nil, err
	}
	if len(t.Opts) > 0 {
		return nil, fmt.Errorf("group %q contains an optional group; groups do not nest", body)
	}
	for _, h := range t.Holes {
		if h.Group != nil {
			return nil, fmt.Errorf("group %q contains another group; groups do not nest", body)
		}
		if h.Flag {
			return nil, fmt.Errorf("group %q contains a {?%s} flag, which only means "+
				"something inside an optional group", body, h.Name)
		}
	}
	return t, nil
}

// captureMode is how a lowering spells its capture groups.
type captureMode int

const (
	capNamed captureMode = iota // (?P<name>…) — the interpreter's form
	capPlain                    // (…)         — the compiler's form
	capNone                     // (?:…)       — a repeated group's element pattern
)

// CaptureKind distinguishes the two things that own a capture group.
type CaptureKind int

const (
	// CapHole is a hole's own capture. For a repeated hole or a repeated
	// group this spans the whole run, split on the separator afterwards.
	CapHole CaptureKind = iota
	// CapOpt wraps a whole optional group, so presence is readable even when
	// the group holds only a flag and literals.
	CapOpt
)

// Capture is one capture group in a template's regex, in regex order.
type Capture struct {
	Kind  CaptureKind
	Group int       // 1-based capture index
	Hole  *Hole     // CapHole: the hole this group holds
	Opt   *Optional // CapOpt: the group; on a CapHole, the optional group it sits in

	// Slot is the index into Template.Holes of the value this capture fills:
	// the hole itself for CapHole, the presence flag for CapOpt (-1 when the
	// group declares none).
	//
	// Regex order and declaration order are not the same once a template has
	// an optional group — the group's wrapper capture comes before the holes it
	// guards, and owns no hole of its own — so the plan carries the mapping
	// rather than leaving each backend to work it out and get it subtly
	// different.
	Slot int
}

// lower walks the segments once and emits both the regex and the capture plan.
//
// One walk rather than two is the point. Phase 5 found the compiler building
// its own second regex from a switch of its own — one template lowered twice,
// which is how a compiled program comes to parse differently from the
// interpreted one — and a group makes that much likelier, since the outer
// pattern, the inner element pattern and both backends' assemblers all have to
// agree about which capture holds what.
func (t *Template) lower(mode captureMode) (string, []Capture) {
	return t.lowerAnchored(mode, true)
}

// lowerAnchored is lower with the anchors made optional. `Mode: Scan` matches
// the template *anywhere* in a line and takes every occurrence, so it needs the
// same pattern without `^` and `$` — and needs it from this walk rather than by
// trimming the anchors off the string afterwards, which would be a second
// lowering pretending to be a substring operation.
func (t *Template) lowerAnchored(mode captureMode, anchored bool) (string, []Capture) {
	w := &walker{mode: mode}
	if anchored {
		w.sb.WriteString("^")
	}
	w.segments(t.Segments, nil)
	if anchored {
		w.sb.WriteString("$")
	}
	return w.sb.String(), w.plan
}

// walker carries the two counters a single walk has to keep in step: the regex
// capture index, and the position in Template.Holes each value belongs at.
type walker struct {
	sb   strings.Builder
	plan []Capture
	mode captureMode
	n    int // capture groups emitted so far
	slot int // holes seen so far, in Template.Holes order (flags included)
}

func (w *walker) segments(segs []Segment, in *Optional) {
	for _, seg := range segs {
		switch {
		case seg.Opt != nil:
			w.optional(seg.Opt)
		case seg.Hole != nil:
			w.hole(seg.Hole, in)
		default:
			w.sb.WriteString(regexp.QuoteMeta(seg.Literal))
		}
	}
}

func (w *walker) optional(opt *Optional) {
	// A *capturing* group with `?` on it, not `(?: … )?`. When the group does
	// not participate, FindStringSubmatchIndex reports -1 for it, which is what
	// tells presence from a group that matched empty — and a flag-only group
	// has no inner hole to read that from, so the wrapper has to carry it.
	//
	// The flag's slot is claimed here rather than where the flag is written,
	// because the plan entry that fills it is this one.
	entry := Capture{Kind: CapOpt, Opt: opt, Slot: -1}
	if w.mode == capNone {
		w.sb.WriteString("(?:")
	} else {
		w.n++
		entry.Group = w.n
		w.plan = append(w.plan, entry)
		w.sb.WriteString("(")
	}
	at := len(w.plan) - 1
	w.segments(opt.Segments, opt)
	if w.mode != capNone && opt.Flag != "" {
		w.plan[at].Slot = flagSlot(opt, w.slot, len(opt.Segments))
	}
	w.sb.WriteString(")?")
}

// flagSlot recovers the flag hole's index in Template.Holes. The walk has just
// passed the whole group, so the group's holes occupy the slots ending at
// w.slot; the flag is at its own offset among them.
func flagSlot(opt *Optional, after, _ int) int {
	holes := 0
	flagAt := -1
	for _, seg := range opt.Segments {
		if seg.Hole == nil || seg.Hole.Space {
			continue
		}
		if seg.Hole.Flag {
			flagAt = holes
		}
		holes++
	}
	return after - holes + flagAt
}

func (w *walker) hole(h *Hole, in *Optional) {
	if h.Space {
		// A gap owns neither an output slot nor a capture: it is structure.
		w.sb.WriteString(h.pattern(w.mode))
		return
	}
	slot := w.slot
	w.slot++
	if h.Flag {
		return // a flag matches nothing; presence comes from its group's capture
	}
	if w.mode != capNone {
		w.n++
		w.plan = append(w.plan, Capture{Kind: CapHole, Group: w.n, Hole: h, Opt: in, Slot: slot})
	}
	w.sb.WriteString(h.pattern(w.mode))
}

// wrap puts body in this hole's capture group, in the given spelling.
func (h Hole) wrap(body string, mode captureMode) string {
	switch {
	case mode == capNone:
		return "(?:" + body + ")"
	case mode == capNamed && h.Name != "":
		return "(?P<" + h.Name + ">" + body + ")"
	default:
		return "(" + body + ")"
	}
}

// Captures is the capture plan of the template's regex: what each group holds,
// in order. Both backends read the match through it.
func (t *Template) Captures() []Capture {
	_, plan := t.lower(capPlain)
	return plan
}

// elementSource is a repeated group's element pattern: the inner template
// unanchored and capturing nothing, so the outer run is one capture that the
// backends split and re-match afterwards.
func (t *Template) elementSource() string {
	w := &walker{mode: capNone}
	w.segments(t.Segments, nil)
	return w.sb.String()
}
