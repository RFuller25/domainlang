package prims

import "strings"

// What a `Part On "…":` may be written with.
//
// The spec is a closed table rather than a pattern language, and that is the
// point: a game reacting to input is answering "which of these few things
// happened?", and a spec language would turn a typo into a handler that
// silently never fires — the quietest possible bug in a program whose output
// is a picture. An unknown spec is refused at resolve time, by name.
//
// It lives here rather than beside the host because it is language semantics:
// what a program is allowed to say, and what it means. The host reads it; the
// reference is generated from it; neither keeps a copy.

// EventSpec is one form a `Part On` label may take.
type EventSpec struct {
	Form  string // as written, including the quotes
	Means string
}

// EventSpecs is every legal spec. The reference page is generated from this.
var EventSpecs = []EventSpec{
	{`"key <name>"`, "that key: up, down, left, right, enter, esc, space, backspace, tab, a letter, ctrl+c, …"},
	{`"key"`, "any key press"},
	{`"text"`, "a printable keystroke — what the player typed"},
	{`"resize"`, "the terminal changed size"},
}

// ValidEventSpec reports whether a `Part On` label is one of the forms above.
func ValidEventSpec(spec string) bool {
	switch {
	case spec == "key" || spec == "text" || spec == "resize":
		return true
	case strings.HasPrefix(spec, "key "):
		return strings.TrimSpace(strings.TrimPrefix(spec, "key ")) != ""
	}
	return false
}

// EventSpecMatches reports whether a spec answers to an event of the given
// kind ("key", "text", "resize") and name (the key, or the text typed).
func EventSpecMatches(spec, kind, name string) bool {
	switch {
	case spec == "resize":
		return kind == "resize"
	case spec == "text":
		return kind == "text"
	case spec == "key":
		return kind == "key"
	case strings.HasPrefix(spec, "key "):
		return kind == "key" && strings.TrimSpace(strings.TrimPrefix(spec, "key ")) == name
	}
	return false
}

// EventSpecPriority orders handlers when several answer to one event: a named
// key runs before the catch-all, so a program that writes both `"key q"` and
// `"key"` sees the specific one first.
func EventSpecPriority(spec string) int {
	if strings.HasPrefix(spec, "key ") {
		return 2
	}
	return 1
}

// EventSpecList renders the legal forms for an error message.
func EventSpecList() string {
	parts := make([]string, len(EventSpecs))
	for i, s := range EventSpecs {
		parts[i] = s.Form
	}
	return strings.Join(parts, ", ")
}
