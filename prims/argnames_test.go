package prims

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

// argNames says of itself that "a test pins it against the names the registry
// looks up". This is that test, and it did not exist — which is how `Cost:`,
// `Params:`, `Value:` and `Combine:` came to be readable by the vocabulary and
// invisible to the linter, so a mistyped `Cst:` suggested `Col:` and a mistyped
// `Prams:` suggested nothing at all.
//
// The scan is mechanical rather than a second hand-written list: every helper
// that reads a named argument takes it in a parameter called `name`, so the
// test finds those declarations, learns which position `name` sits at, and
// collects the string literal passed there at every call site.
func TestArgNamesCoversEveryArgumentTheRegistryReads(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	pkg, ok := pkgs["prims"]
	if !ok {
		t.Fatal("package prims not found in .")
	}

	// Which parameter position holds the argument name, per function or method
	// that takes one. Methods are keyed by their bare name: ArgSet is the only
	// receiver in this package whose methods take a `name`.
	namePos := map[string]int{}
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				continue
			}
			i := 0
			for _, field := range fn.Type.Params.List {
				for _, id := range field.Names {
					if id.Name == "name" {
						namePos[fn.Name.Name] = i
					}
					i++
				}
				if len(field.Names) == 0 {
					i++ // an unnamed parameter still occupies a position
				}
			}
		}
	}

	read := map[string]token.Pos{}
	for _, f := range pkg.Files {
		if isTestFile(fset, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var fname string
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				fname = fn.Name
			case *ast.SelectorExpr:
				fname = fn.Sel.Name
			default:
				return true
			}
			pos, ok := namePos[fname]
			if !ok || pos >= len(call.Args) {
				return true
			}
			lit, ok := call.Args[pos].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true // a computed name; nothing to pin
			}
			s, err := strconv.Unquote(lit.Value)
			if err == nil && s != "" {
				read[s] = lit.Pos()
			}
			return true
		})
	}

	if len(read) < 5 {
		t.Fatalf("the scan found only %d argument names (%v) — it has stopped "+
			"seeing the call sites it is meant to check", len(read), keysOf(read))
	}
	known := ArgNames()
	for _, name := range keysOf(read) {
		if !slices.Contains(known, name) {
			t.Errorf("%s: the registry reads the argument %q, but prims.argNames "+
				"does not list it — the linter cannot suggest it on a typo",
				fset.Position(read[name]), name)
		}
	}
}

func isTestFile(fset *token.FileSet, f *ast.File) bool {
	name := fset.Position(f.Pos()).Filename
	return len(name) > 8 && name[len(name)-8:] == "_test.go"
}

func keysOf(m map[string]token.Pos) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
