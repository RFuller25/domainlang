package builtins

import (
	"fmt"
	"strings"
)

// padText widens s to width *runes* by repeating pad on one side, truncating
// the last copy so the result is exactly width runes. Text already at least
// that wide is returned untouched, and an empty pad has nothing to widen with.
//
// Runes, not bytes, because every other position in the language is runes —
// padding by bytes would disagree with `length` on the very input that makes
// padding worth doing.
func padText(env Env, s string, width int64, pad string, left bool) (string, error) {
	if pad == "" || width <= 0 {
		return s, nil
	}
	have := int64(0)
	for range s {
		have++
	}
	if have >= width {
		return s, nil
	}
	// The width is a number in the program rather than the size of anything it
	// was given, so it is the one input here that can ask for more than exists.
	// Like fill and repeat it is answered with a message instead of an
	// allocation Go would abort the process over.
	if width-have > env.BuildLimit() {
		return "", fmt.Errorf("padding to %d is more than can be built", width)
	}
	need := int(width - have)
	padRunes := []rune(pad)

	var b strings.Builder
	// The pad is built once and sized exactly: len(s) plus the bytes of the
	// runes actually used, so neither Grow nor the writes reallocate.
	fill := make([]rune, need)
	for i := range fill {
		fill[i] = padRunes[i%len(padRunes)]
	}
	filled := string(fill)
	b.Grow(len(s) + len(filled))
	if left {
		b.WriteString(filled)
		b.WriteString(s)
	} else {
		b.WriteString(s)
		b.WriteString(filled)
	}
	return b.String(), nil
}

// classify implements isdigit/isalpha/isupper/islower over a whole Text.
//
// The empty text is false for all four: "every rune is a digit" is vacuously
// true of it, which is never what a guard means. isupper/islower ask about the
// *cased* runes — "A1" is upper, "1" is neither — so a mixed token still
// answers usefully.
func classify(name, s string) bool {
	if s == "" {
		return false
	}
	cased := false
	for _, r := range s {
		switch name {
		case "isdigit":
			if r < '0' || r > '9' {
				return false
			}
		case "isalpha":
			if !isASCIILetter(r) && !isNonASCIILetter(r) {
				return false
			}
		case "isupper":
			if isLowerRune(r) {
				return false
			}
			cased = cased || isUpperRune(r)
		case "islower":
			if isUpperRune(r) {
				return false
			}
			cased = cased || isLowerRune(r)
		}
	}
	if name == "isupper" || name == "islower" {
		return cased
	}
	return true
}

// The rune predicates are spelled out rather than taken from `unicode` so the
// compiled backend can emit exactly the same tests without pulling the Unicode
// tables into every binary that happens to call isdigit.
func isASCIILetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isNonASCIILetter(r rune) bool {
	return r > 127 && (isUpperRune(r) || isLowerRune(r))
}

func isUpperRune(r rune) bool {
	if r < 128 {
		return r >= 'A' && r <= 'Z'
	}
	return r != toLowerRune(r)
}

func isLowerRune(r rune) bool {
	if r < 128 {
		return r >= 'a' && r <= 'z'
	}
	return r != toUpperRune(r)
}

func toLowerRune(r rune) rune { return []rune(strings.ToLower(string(r)))[0] }
func toUpperRune(r rune) rune { return []rune(strings.ToUpper(string(r)))[0] }

// isRepeatedPattern reports whether s is a shorter pattern repeated two or
// more times ("abab", "aaa"), via the classic rotation trick: s is periodic
// iff it occurs in (s+s) minus that string's first and last characters.
func isRepeatedPattern(s string) bool {
	if len(s) < 2 {
		return false
	}
	doubled := (s + s)[1 : 2*len(s)-1]
	return strings.Contains(doubled, s)
}
