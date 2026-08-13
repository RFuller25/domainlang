package docs

import (
	"io/fs"
	"strconv"
	"strings"
)

// Fenced-block extraction, shared rather than duplicated.
//
// Three consumers need to agree on what a ```domain block in the docs means:
// examples_test.go (which parses and resolves them), the runnable-example
// harness in cmd/domain (which executes the ones that declare an output), and
// render.js (which decides how to draw them). The first two share this code so
// they cannot drift; render.js is checked against it by render_test.go.
//
// The info string carries the intent, and there are exactly three states:
//
//	```domain          parse, and resolve if it is a whole program
//	```domain run      the above, plus execute and diff against ```output
//	```domain ignore   neither — a fragment, a layout, or a deliberate error
//
// A `run` block is followed by an optional ```input block, any number of
// ```lib <path> blocks, and a required ```output block. Anything else between
// them is skipped, so prose may sit in between when the example reads better
// that way.

// Block is one fenced code block lifted out of a documentation page.
type Block struct {
	Page   string // e.g. "primitives.md"
	Line   int    // 1-based line of the opening fence
	Info   string // the full info string, e.g. "domain run"
	Lang   string // its first word, e.g. "domain"
	Source string // the body, newline-terminated
}

// Ignored reports whether the block opts out of every check.
func (b Block) Ignored() bool { return b.hasFlag("ignore") }

// Runnable reports whether the block declares an executable example.
func (b Block) Runnable() bool { return b.hasFlag("run") }

// flags are the info-string words after the language.
func (b Block) flags() []string {
	fields := strings.Fields(b.Info)
	if len(fields) == 0 {
		return nil
	}
	return fields[1:]
}

func (b Block) hasFlag(want string) bool {
	for _, f := range b.flags() {
		if f == want || strings.HasPrefix(f, want+"=") {
			return true
		}
	}
	return false
}

// Flag returns the value of a `key=value` entry in the info string.
func (b Block) Flag(key string) (string, bool) {
	for _, f := range b.flags() {
		if v, ok := strings.CutPrefix(f, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// Blocks extracts every fenced block from one page, whatever its language.
// Callers filter by Lang; keeping them all is what lets the runnable harness
// find the ```input and ```output blocks that follow a ```domain run.
func Blocks(page, src string) []Block {
	var out []Block
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "```") {
			continue
		}
		info := strings.TrimSpace(strings.TrimPrefix(lines[i], "```"))
		start := i
		i++
		var body []string
		for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			body = append(body, lines[i])
			i++
		}
		lang := ""
		if fields := strings.Fields(info); len(fields) > 0 {
			lang = fields[0]
		}
		out = append(out, Block{
			Page: page, Line: start + 1, Info: info, Lang: lang,
			Source: strings.Join(body, "\n") + "\n",
		})
	}
	return out
}

// Pages lists every Markdown page in the embedded site, sorted, so a test that
// walks the documentation walks what actually ships in the binary rather than
// what happens to be on disk.
func Pages() ([]string, error) {
	entries, err := fs.Glob(FS, "*.md")
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// AllBlocks extracts every fenced block from every embedded page.
func AllBlocks() ([]Block, error) {
	pages, err := Pages()
	if err != nil {
		return nil, err
	}
	var out []Block
	for _, p := range pages {
		src, err := fs.ReadFile(FS, p)
		if err != nil {
			return nil, err
		}
		out = append(out, Blocks(p, string(src))...)
	}
	return out, nil
}

// Example is a ```domain run block paired with the input it is given and the
// output it must produce.
type Example struct {
	Block
	Name   string            // page:line, for test naming and failure messages
	Input  string            // stdin and, when the program names a file, that file's content
	Output string            // expected stdout, trailing newline trimmed
	Libs   map[string]string // ```lib <path> blocks, staged beside the program
}

// Examples pairs each runnable block on a page with the ```input and ```output
// blocks that follow it. A `run` block with no ```output is an error rather
// than a skip: the flag is a promise that the number below is checked.
func Examples(page, src string) ([]Example, string) {
	blocks := Blocks(page, src)
	var out []Example
	for i, b := range blocks {
		if b.Lang != "domain" || !b.Runnable() {
			continue
		}
		ex := Example{Block: b, Name: page + ":" + strconv.Itoa(b.Line)}
		found := false
		for _, nxt := range blocks[i+1:] {
			// Stop at the next Domain block. Without this a `run` block that
			// forgot its output would scan on and adopt the *next* example's
			// input and output, which fails somewhere other than where the
			// mistake is — the worst way for an authoring slip to show up.
			if nxt.Lang == "domain" {
				break
			}
			switch nxt.Lang {
			case "input":
				ex.Input = nxt.Source
			case "lib":
				// ```lib aoc.domain — an imported library, written beside the
				// program so an `Innate Domain:` example is a real program
				// rather than a fragment that could never be run.
				fields := strings.Fields(nxt.Info)
				if len(fields) < 2 {
					return nil, ex.Name + ": a ```lib block must name its file, e.g. ```lib aoc.domain"
				}
				if ex.Libs == nil {
					ex.Libs = map[string]string{}
				}
				ex.Libs[fields[1]] = nxt.Source
			case "output":
				ex.Output = strings.TrimRight(nxt.Source, "\n")
				found = true
			}
			if found {
				break
			}
		}
		if !found {
			return nil, ex.Name + ": a ```domain run block must be followed by a ```output block"
		}
		out = append(out, ex)
	}
	return out, ""
}

// Source reports the file the program reads, and whether it reads stdin.
// `Cursed Energy: <target>` names a file; the literal `stdin` reads stdin.
func (e Example) Source() (file string, stdin bool) {
	for _, line := range strings.Split(e.Block.Source, "\n") {
		t := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(t, "Cursed Energy:")
		if !ok {
			continue
		}
		target := strings.TrimSpace(rest)
		if i := strings.Index(target, "#"); i >= 0 {
			target = strings.TrimSpace(target[:i])
		}
		if target == "stdin" {
			return "", true
		}
		return target, false
	}
	return "", true
}
