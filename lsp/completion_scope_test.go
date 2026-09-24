package lsp

import (
	"slices"
	"strings"
	"testing"
)

// What a scope changes about the completions, and what it must not.
//
// A scope only ever adds to the vocabulary, so the answer to "what may I write
// here" is the same everywhere except in the one place a scope genuinely
// differs: `Part`. A `Part Draw:` is a resolve error outside a game and a
// `Part "1":` is one inside it, so offering either in the wrong file is
// offering something that will not compile.

func labels(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it["label"].(string))
	}
	return out
}

func has(items []map[string]any, want string) bool {
	return slices.Contains(labels(items), want)
}

func TestPartRolesFollowTheScope(t *testing.T) {
	game := CompletionItemsIn("Game Dev", "Part ")
	if !has(game, "Part Draw:") {
		t.Errorf("a game should be offered Part Draw:, got %v", labels(game))
	}
	if has(game, `Part "label":`) {
		t.Errorf("a game should not be offered the labelled block, got %v", labels(game))
	}

	aoc := CompletionItemsIn("", "Part ")
	if !has(aoc, `Part "label":`) {
		t.Errorf("a puzzle solver should be offered the labelled block, got %v", labels(aoc))
	}
	if has(aoc, "Part Draw:") {
		t.Errorf("a puzzle solver should not be offered Part Draw:, got %v", labels(aoc))
	}

	// An alias is the same scope.
	if !has(CompletionItemsIn("Game", "Part "), "Part Draw:") {
		t.Error("the Game alias should offer the same roles as Game Dev")
	}
	// A scope this build does not have offers nothing rather than another
	// scope's roles: the resolver is about to refuse the file.
	if got := CompletionItemsIn("Bogus", "Part "); len(got) != 0 {
		t.Errorf("an unknown scope should offer no roles, got %v", labels(got))
	}
}

// The argument shape is part of how a role is written, so a role that takes
// one has to show it: `Part Every:` is not something a user can type.
func TestPartRoleFormsCarryTheirArgument(t *testing.T) {
	got := labels(CompletionItemsIn("Game Dev", "Part "))
	for _, want := range []string{`Part On "…":`, "Part Every N:", `Part Entity "…":`} {
		found := false
		for _, l := range got {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q, got %v", want, got)
		}
	}
}

func TestInnateDomainOffersTheScopes(t *testing.T) {
	got := CompletionItemsIn("", "Innate Domain: ")
	if !has(got, "Advent of Code") || !has(got, "Game Dev") {
		t.Errorf("got %v, want the scopes this build has", labels(got))
	}
	for _, it := range got {
		if it["detail"] != "Innate Domain" {
			t.Errorf("item %v is not labelled as a scope", it["label"])
		}
	}
}

// The rest is unchanged, in either scope: a scope adds, so a completion valid
// today is valid tomorrow.
func TestOrdinaryCompletionsAreScopeBlind(t *testing.T) {
	for _, scope := range []string{"", "Game Dev"} {
		head := CompletionItemsIn(scope, "")
		if !has(head, "Cursed Technique") {
			t.Errorf("scope %q: the keywords should still be offered", scope)
		}
		args := CompletionItemsIn(scope, "    ")
		if !has(args, "Using:") {
			t.Errorf("scope %q: the arguments should still be offered", scope)
		}
	}
}

// A `Part` that already has its colon is a finished head, not a role waiting
// to be chosen.
func TestFinishedPartHeadIsLeftAlone(t *testing.T) {
	got := CompletionItemsIn("Game Dev", `Part Draw:`)
	for _, l := range labels(got) {
		if strings.HasPrefix(l, "Part ") {
			t.Errorf("a finished Part head should not re-offer roles, got %v", labels(got))
			break
		}
	}
}

// documentScope reads the declaration off a file that does not parse, which is
// most of the time while somebody is typing into it.
func TestDocumentScopeReadsAHalfWrittenFile(t *testing.T) {
	src := "# a game\n\nInnate Domain: Game Dev\n\nPart Wor"
	if got := documentScope(src); got != "Game Dev" {
		t.Errorf("got %q, want %q", got, "Game Dev")
	}
	if got := documentScope("Cursed Energy: stdin\nPart "); got != "" {
		t.Errorf("got %q, want no scope", got)
	}
}
