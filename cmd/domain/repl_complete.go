// Tab-completion candidates for the REPL's interactive editor. Three
// sources, picked by cursor position: file paths (Cursed Energy:, :load,
// :save targets), the REPL's own :commands, and — for everything else —
// the language server's context-aware keyword/primitive/arg-label/Mode
// completion (lsp.CompletionItems), reused as-is.
package main

import (
	"os"
	"path/filepath"
	"strings"

	"domain/lsp"
)

// replCommands are the REPL's own :directives (repl.go's command method) —
// the language server has no concept of these.
var replCommands = []string{"help", "list", "type", "undo", "reset", "load", "save", "quit"}

// completeToken returns the completion candidates for the token ending at
// cursor (a byte offset, assumed ASCII up to the cursor like the rest of
// repl_tty.go) in line, plus the byte offset where that token starts. No
// match returns a nil candidates slice.
func completeToken(line string, cursor int, baseDir string) (candidates []string, tokenStart int) {
	prefix := line[:cursor]

	if path, start, ok := filePathContext(prefix); ok {
		return completeFilePath(path, baseDir), start
	}

	if strings.HasPrefix(prefix, ":") {
		word := strings.ToLower(prefix[1:])
		var out []string
		for _, name := range replCommands {
			if strings.HasPrefix(name, word) {
				out = append(out, ":"+name)
			}
		}
		// tokenStart is 0, not 1: candidates already include the leading
		// ':' (they replace the whole command, not just the part after
		// the colon) — pairing tokenStart=1 with a ':'-prefixed candidate
		// would double it up into "::load".
		return out, 0
	}

	items := lsp.CompletionItems(prefix)
	// The word in progress: everything after the first colon (a keyword or
	// arg-label's colon), or the whole trimmed line when there's no colon
	// yet — mirrors CompletionItems' own splitKeyword logic. Unlike a
	// trailing-space split, this keeps multi-word candidates ("Cursed
	// Technique", "Sort By") intact instead of truncating to their last word.
	trimmed := strings.TrimLeft(prefix, " \t")
	word := trimmed
	if i := strings.IndexByte(trimmed, ':'); i >= 0 {
		word = strings.TrimLeft(trimmed[i+1:], " \t")
	}
	tokenStart = cursor - len(word)
	wordLower := strings.ToLower(word)
	var out []string
	for _, item := range items {
		label, _ := item["label"].(string)
		if strings.HasPrefix(strings.ToLower(label), wordLower) {
			insertText, _ := item["insertText"].(string)
			out = append(out, insertText)
		}
	}
	return out, tokenStart
}

// filePathContext recognizes a file-target position (Cursed Energy:,
// :load, :save — each requiring at least one space/tab before the path
// starts) and returns the partial path typed so far and the byte offset
// where it starts.
func filePathContext(prefix string) (path string, start int, ok bool) {
	type target struct {
		keyword string
		fold    bool
	}
	targets := []target{
		{"Cursed Energy:", true},
		{":load", false},
		{":save", false},
	}
	for _, tgt := range targets {
		if len(prefix) < len(tgt.keyword) {
			continue
		}
		head := prefix[:len(tgt.keyword)]
		matched := head == tgt.keyword
		if tgt.fold {
			matched = strings.EqualFold(head, tgt.keyword)
		}
		if !matched {
			continue
		}
		rest := prefix[len(tgt.keyword):]
		if len(rest) == 0 || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		trimmed := strings.TrimLeft(rest, " \t")
		return trimmed, len(prefix) - len(trimmed), true
	}
	return "", 0, false
}

// completeFilePath lists the directory entries under baseDir matching the
// already-typed partial path, each as a full relative path (a trailing /
// on directories, so cycling into one and pressing Tab again completes a
// level deeper). os.ReadDir already returns entries sorted by name.
func completeFilePath(partial, baseDir string) []string {
	dir, base := filepath.Split(partial)
	entries, err := os.ReadDir(filepath.Join(baseDir, dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), base) {
			continue
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, dir+name)
	}
	return out
}
