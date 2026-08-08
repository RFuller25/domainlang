package editors_test

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"domain/ast"
	"domain/editors"
	"domain/lexer"
	"domain/parser"
	"domain/prims"
	"domain/typecheck"
)

// Both grammars name three lists the language owns: the primitives
// (prims.Registry), the expression builtins (typecheck.Builtins) and the
// themed keywords (ast.Keywords). Hand-maintaining those copies does not work
// — when this file was written the VS Code grammar was missing 41 primitives,
// 83 of the 144 builtins, and two keywords, all added to the language after
// the grammar was last edited by hand.
//
// So they are generated, on the same terms as docs/gallery.json: `go test
// ./editors -update` rewrites both grammars, and every other run fails if they
// are stale. Only the word lists are generated — the rules around them are
// hand-written and stay that way, which is why this rewrites patterns in place
// rather than emitting whole files.

var update = flag.Bool("update", false, "rewrite the generated word lists in both editor grammars")

const (
	tmPath  = "vscode/syntaxes/domain.tmLanguage.json"
	vimPath = "nvim/syntax/domain.vim"
)

// alternation renders words as a regex alternation, longest first. Both
// engines match alternatives left to right rather than longest-overall, so
// `Sort By` has to precede `Sort` or the longer phrase can never win.
func alternation(words []string) string {
	sorted := slices.Clone(words)
	slices.SortFunc(sorted, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	return strings.Join(sorted, "|")
}

// phraseAliases are spellings a matcher accepts that no registry entry names.
// `Domain Expansion: Quicksort` resolves to the Sort primitive — whose ID, and
// therefore whose only spelling, is `Sort` — because that matcher takes any
// phrase containing the word. It is the spelling the README, the tutorial and
// five programs in this repository actually write, so a grammar generated from
// IDs alone would leave the most-written operation in the language uncoloured.
//
// TestEveryPhraseWordInTheRepositoryIsHighlighted is what keeps this list
// honest: an alias that starts being used and is not here fails that test.
//
// The rest are vocabulary that lives outside the registry because it is
// dispatched somewhere else: the `From:`-consumers (prims/channel.go) and the
// Binding Vow assertions (prims/vow.go) are matched by word from their own
// resolvers, so no Primitive entry names them.
var phraseAliases = []string{
	"Quicksort",
	"Combine", "Difference", "Zip", // From:-consumers
	"Holds", "Equals", "Values", // Binding Vow assertions
}

// primitiveSpellings is every operation phrase that names a primitive, which
// is what a reader actually writes and therefore what should be coloured as
// one.
//
// The four foreign-language names are excluded: the foreign-block rule owns
// them, it colours the whole block rather than one word, and two of them
// (`Go`, `rask`) are ordinary enough words that highlighting them anywhere
// else would be wrong.
func primitiveSpellings() []string {
	foreign := map[string]bool{}
	for _, l := range ast.ForeignLanguages {
		foreign[l] = true
	}
	out := slices.Clone(phraseAliases)
	for _, p := range prims.Registry {
		for _, s := range p.Spellings() {
			if !foreign[s] && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

func TestGrammarsMatchTheLanguage(t *testing.T) {
	prim := alternation(primitiveSpellings())
	builtin := alternation(typecheck.Builtins)
	keyword := alternation(ast.Keywords)
	// A Shikigami is called by its bare name, so the prelude's read like
	// primitives at a call site and are coloured as the operations they are.
	shikigami := alternation(prims.PreludeNames())

	tm := readFile(t, tmPath)
	tm = setJSONPattern(t, tm, "primitive", "match", `\b(`+prim+`)\b`)
	tm = setJSONPattern(t, tm, "builtin", "match", `\b(`+builtin+`)(?=\s*\()`)
	tm = setJSONPattern(t, tm, "themed-statement", "begin",
		`^\s*(`+keyword+`)\b[ \t]*(:)?`)
	tm = setJSONPattern(t, tm, "shikigami-prelude", "match", `\b(`+shikigami+`)\b`)
	checkGenerated(t, tmPath, tm)

	vim := readFile(t, vimPath)
	vim = setVimPattern(t, vim, "domainPrimitive", `/\<\%(`+vimAlt(prim)+`\)\>/`)
	vim = setVimPattern(t, vim, "domainBuiltin", `/\<\%(`+vimAlt(builtin)+`\)\ze\s*(/`)
	vim = setVimPattern(t, vim, "domainKeyword", `/^\s*\%(`+vimAlt(keyword)+`\)\>/`)
	vim = setVimPattern(t, vim, "domainShikigami", `/\<\%(`+vimAlt(shikigami)+`\)\>/`)
	checkGenerated(t, vimPath, vim)
}

// vimAlt is an alternation in Vim's regex dialect, where `|` inside `\%( \)`
// must be escaped.
func vimAlt(alt string) string { return strings.ReplaceAll(alt, "|", `\|`) }

// Every grammar has to survive being read as what it claims to be. The
// TextMate file is JSON the editor parses at load time, so a broken one
// silently turns highlighting off rather than reporting anything.
func TestTextMateGrammarIsValidJSON(t *testing.T) {
	var g struct {
		ScopeName  string                    `json:"scopeName"`
		Patterns   []map[string]any          `json:"patterns"`
		Repository map[string]map[string]any `json:"repository"`
	}
	if err := json.Unmarshal([]byte(readFile(t, tmPath)), &g); err != nil {
		t.Fatalf("%s is not valid JSON: %v", tmPath, err)
	}
	if g.ScopeName != "source.domain" {
		t.Errorf("scopeName = %q, want source.domain", g.ScopeName)
	}
	// Every `#name` a pattern includes must exist in the repository, or that
	// part of the file is never highlighted and nothing says so.
	include := regexp.MustCompile(`^#(.+)$`)
	var walk func(pats []map[string]any, where string)
	walk = func(pats []map[string]any, where string) {
		for _, p := range pats {
			if inc, ok := p["include"].(string); ok {
				if m := include.FindStringSubmatch(inc); m != nil {
					if _, exists := g.Repository[m[1]]; !exists {
						t.Errorf("%s includes #%s, which the repository does not define", where, m[1])
					}
				}
			}
			if sub, ok := p["patterns"].([]any); ok {
				walk(asMaps(sub), where)
			}
		}
	}
	walk(g.Patterns, "the top level")
	for name, rule := range g.Repository {
		if sub, ok := rule["patterns"].([]any); ok {
			walk(asMaps(sub), "#"+name)
		}
	}
}

// The extension manifest points at files that have to be there, and declares a
// version that should not silently fall behind the language it highlights.
func TestExtensionManifestIsCoherent(t *testing.T) {
	var pkg struct {
		Name        string `json:"name"`
		Main        string `json:"main"`
		Contributes struct {
			Languages []struct {
				ID            string   `json:"id"`
				Extensions    []string `json:"extensions"`
				Configuration string   `json:"configuration"`
			} `json:"languages"`
			Grammars []struct {
				Language  string `json:"language"`
				ScopeName string `json:"scopeName"`
				Path      string `json:"path"`
			} `json:"grammars"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal([]byte(readFile(t, "vscode/package.json")), &pkg); err != nil {
		t.Fatalf("package.json is not valid JSON: %v", err)
	}
	g := pkg.Contributes.Grammars[0]
	// Checked against the *embedded* tree rather than the working directory:
	// that is what the installer ships, and a file present on disk but left
	// out of the embed directive installs an extension that cannot load.
	embedded := editors.VSCode()
	for _, rel := range []string{pkg.Main, pkg.Contributes.Languages[0].Configuration, g.Path} {
		name := strings.TrimPrefix(rel, "./")
		if _, err := fs.Stat(embedded, name); err != nil {
			t.Errorf("package.json points at %s, which is not embedded: %v", rel, err)
		}
	}
	if g.ScopeName != "source.domain" {
		t.Errorf("grammar scopeName = %q, want source.domain", g.ScopeName)
	}
	if lang := pkg.Contributes.Languages[0]; lang.ID != "domain" || lang.Extensions[0] != ".domain" {
		t.Errorf("language contribution = %+v, want id domain for .domain", lang)
	}
}

// The installer ships the extension out of the binary, so everything the
// manifest needs must actually be embedded — a file left out of the embed
// directive installs an extension that cannot load.
func TestEmbeddedExtensionIsComplete(t *testing.T) {
	for _, want := range []string{
		"package.json", "extension.js", "language-configuration.json",
		"syntaxes/domain.tmLanguage.json", "README.md",
	} {
		if _, err := fs.Stat(editors.VSCode(), want); err != nil {
			t.Errorf("%s is not embedded: %v", want, err)
		}
	}
}

// --- helpers ---

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// checkGenerated writes the file under -update and otherwise fails when what
// is on disk differs, which is docs/gen_test.go's discipline applied here.
func checkGenerated(t *testing.T, path, want string) {
	t.Helper()
	got := readFile(t, path)
	if got == want {
		return
	}
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	t.Errorf("%s is stale — the language changed and the grammar did not.\nrun: go test ./editors -update", path)
}

// setJSONPattern replaces one rule's regex in the TextMate grammar, in place.
// The file is edited as text rather than round-tripped through a map so that
// key order, indentation and the long explanatory comments survive untouched.
func setJSONPattern(t *testing.T, src, rule, field, pattern string) string {
	t.Helper()
	var g struct {
		Repository map[string]map[string]any `json:"repository"`
	}
	if err := json.Unmarshal([]byte(src), &g); err != nil {
		t.Fatalf("%s: %v", tmPath, err)
	}
	r, ok := g.Repository[rule]
	if !ok {
		t.Fatalf("%s has no rule %q", tmPath, rule)
	}
	old, ok := r[field].(string)
	if !ok {
		t.Fatalf("%s: rule %q has no string field %q", tmPath, rule, field)
	}
	if old == pattern {
		return src
	}
	oldLit, newLit := jsonString(t, old), jsonString(t, pattern)
	if strings.Count(src, oldLit) != 1 {
		t.Fatalf("%s: rule %q's %s is not uniquely locatable in the file", tmPath, rule, field)
	}
	return strings.Replace(src, oldLit, newLit, 1)
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// setVimPattern replaces the pattern of one `syn match <group> /.../` line.
func setVimPattern(t *testing.T, src, group, pattern string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^syn match ` + regexp.QuoteMeta(group) + ` .*$`)
	line := "syn match " + group + " " + pattern
	if !re.MatchString(src) {
		t.Fatalf("%s has no `syn match %s` line", vimPath, group)
	}
	return re.ReplaceAllStringFunc(src, func(string) string { return line })
}

func asMaps(vs []any) []map[string]any {
	out := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// The generated lists come from the registry, but what a program *writes* is
// an operation phrase, and the two are not the same thing: `Quicksort`
// resolves to the Sort primitive without appearing in the registry under that
// name. So this checks the grammars against the corpus — every phrase word in
// every program the repository ships has to be coloured by some rule.
//
// It reads the words out of the parser rather than scanning text, so it sees
// exactly what the language sees: the identifier words of each operation
// phrase and its trailing modifiers, top level and nested alike.
func TestEveryPhraseWordInTheRepositoryIsHighlighted(t *testing.T) {
	highlighted := map[string]bool{}
	add := func(words ...string) {
		for _, w := range words {
			for _, part := range strings.Fields(w) {
				highlighted[strings.ToLower(part)] = true
			}
		}
	}
	add(primitiveSpellings()...)
	add(prims.PreludeNames()...)
	add(ast.Keywords...)
	add(ast.ForeignLanguages...)
	add(typecheck.Builtins...)
	// The rules that are hand-written because they name closed sets the
	// language does not keep a list of: loop drivers, connector words, Mode:
	// values, order modifiers, type names, and the I/O targets.
	add("Repeat", "While", "Iterate", "Until", "Fixed", "Point", "For", "in")
	add("by", "from", "to", "with", "into", "of", "as")
	add("One", "Each", "Try", "Scan", "Filter", "Count", "First", "Map")
	add("Ascending", "Descending")
	add("Int", "Text", "Float", "Bool", "List", "Tuple", "Record", "Map", "Set", "Grid", "Sparse")
	add("stdin", "stdout")

	missing := map[string][]string{}
	for _, path := range programPaths(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		toks, err := lexer.Lex(string(src))
		if err != nil {
			continue // not this test's business
		}
		prog, err := parser.Parse(string(src), toks)
		if err != nil {
			continue
		}
		// Fill in the keywords the source left out, so a program written
		// without them (examples/16_no_prefixes.domain) is read the way the
		// language reads it — otherwise its source line looks like an
		// operation phrase and its path looks like vocabulary.
		_ = prims.Infer(prog)
		for _, w := range phraseWords(prog) {
			// A file path or a user's own Shikigami name is not vocabulary:
			// the first is a target, the second is defined in the file itself,
			// and neither can be in a generated list.
			if highlighted[strings.ToLower(w)] || localName(prog, w) || strings.ContainsAny(w, "._/") {
				continue
			}
			missing[w] = append(missing[w], path)
		}
	}
	for w, where := range missing {
		t.Errorf("%q is written by %s but no grammar rule colours it — add it to phraseAliases if it is an accepted spelling",
			w, where[0])
	}
}

// programPaths is every Domain program the repository ships.
func programPaths(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"../examples", "../challenges", "../testdata"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".domain") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	if len(out) == 0 {
		t.Fatal("no programs found — the corpus this test checks against is missing")
	}
	return out
}

// phraseWords is every identifier word of every operation phrase in a program.
func phraseWords(prog *ast.Program) []string {
	var out []string
	var walk func(stmts []*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			// A source's and a sink's phrase is a target — a file path, a
			// channel name, `stdin` — rather than an operation, so its words
			// are not vocabulary any grammar could carry.
			if s.Op != nil && s.Keyword != "Cursed Energy" && s.Keyword != "Reveal" {
				out = append(out, s.Op.Words...)
				for _, m := range s.Op.Modifiers {
					out = append(out, strings.Fields(m)...)
				}
			}
			walk(s.Block)
			for _, b := range s.Binds {
				walk(b.Body)
			}
		}
	}
	walk(prog.Statements)
	for _, d := range prog.Shikigamis {
		walk(d.Body)
	}
	return out
}

// localName reports whether a word belongs to something the program defines
// itself — a Shikigami of its own, one of its parameters (a parameter
// substitutes into a phrase, so `Select Top n` is a phrase word), or one it
// imports — rather than to the language's vocabulary.
func localName(prog *ast.Program, word string) bool {
	for _, d := range prog.Shikigamis {
		for _, w := range strings.Fields(d.Name) {
			if strings.EqualFold(w, word) {
				return true
			}
		}
		for _, p := range d.Params {
			if strings.EqualFold(p.Name, word) {
				return true
			}
		}
	}
	// An imported library's Shikigami are named in a file this test does not
	// read; a program that imports one may call it by a name from there.
	return len(prog.Imports) > 0
}
