package codegen

import (
	"strconv"
	"strings"

	"domain/builtins"
	"domain/ir"
)

// The compiled half of the text builtins declared in package builtins. The
// typing rule and the interpreter's evaluator for each live beside its name
// there; what is here is the Go it becomes, which has to stay in this package
// because it splices in the helper declarations (declChars, declPadText, …)
// that are defined here.
//
// Each builtin is registered by name, and TestEveryRegisteredBuiltinHasAnEmitter
// fails if one has no emitter, so the three halves cannot drift apart silently.

// Imp and Helper let *gen satisfy builtins.Emitter.
func (g *gen) Imp(pkgs ...string)                         { g.imp(pkgs...) }
func (g *gen) Helper(name, src string, imports ...string) { g.helper(name, src, imports...) }

// direct emits a call to a standard-library function, importing its package.
func direct(pkg, fn string, ret *ir.Type, wrap func(string) string) builtins.EmitFunc {
	return func(e builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
		e.Imp(pkg)
		call := pkg + "." + fn + "(" + strings.Join(args, ", ") + ")"
		if wrap != nil {
			call = wrap(call)
		}
		return call, ret, nil
	}
}

// viaHelper emits a call to a generated helper, declaring it (and the helpers
// it depends on) first. needs is a list of {name, source, imports…} triples.
type helperDecl struct {
	name, src string
	imports   []string
}

func viaHelper(fn string, ret *ir.Type, decls ...helperDecl) builtins.EmitFunc {
	return func(e builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
		for _, d := range decls {
			e.Helper(d.name, d.src, d.imports...)
		}
		return fn + "(" + strings.Join(args, ", ") + ")", ret, nil
	}
}

var (
	failDecl  = helperDecl{"dmFail", declFail, []string{"fmt", "os"}}
	asciiDecl = helperDecl{"dmASCII", declASCII, []string{"unicode/utf8"}}
)

func init() {
	text, textList := ir.Text(), ir.List(ir.Text())
	asInt64 := func(s string) string { return "int64(" + s + ")" }

	for name, f := range map[string]builtins.EmitFunc{
		"trim":        direct("strings", "TrimSpace", text, nil),
		"upper":       direct("strings", "ToUpper", text, nil),
		"lower":       direct("strings", "ToLower", text, nil),
		"startswith":  direct("strings", "HasPrefix", ir.Bool(), nil),
		"endswith":    direct("strings", "HasSuffix", ir.Bool(), nil),
		"replace":     direct("strings", "ReplaceAll", text, nil),
		"occurrences": direct("strings", "Count", ir.Int(), asInt64),
		"split":       direct("strings", "Split", textList, nil),
		"words":       direct("strings", "Fields", textList, nil),
		"trimprefix":  direct("strings", "TrimPrefix", text, nil),
		"trimsuffix":  direct("strings", "TrimSuffix", text, nil),

		"repeats":  viaHelper("dmRepeats", ir.Bool(), helperDecl{"dmRepeats", declRepeats, []string{"strings"}}),
		"chars":    viaHelper("dmChars", textList, helperDecl{"dmChars", declChars, nil}),
		"toint":    viaHelper("dmToInt", ir.Int(), failDecl, helperDecl{"dmToInt", declToInt, []string{"strconv", "strings"}}),
		"ord":      viaHelper("dmOrd", ir.Int(), failDecl, helperDecl{"dmOrd", declOrd, []string{"unicode/utf8"}}),
		"chr":      viaHelper("dmChr", text, failDecl, helperDecl{"dmChr", declChr, nil}),
		"repeat":   viaHelper("dmRepeatText", text, failDecl, helperDecl{"dmRepeatText", declRepeatText, []string{"strings"}}),
		"charat":   viaHelper("dmCharAt", text, failDecl, asciiDecl, helperDecl{"dmCharAt", declCharAt, nil}),
		"withchar": viaHelper("dmWithChar", text, failDecl, asciiDecl, helperDecl{"dmWithChar", declWithChar, nil}),
	} {
		builtins.SetEmit(name, f)
	}

	// totext renders a Float with the shortest round-trip form and an Int with
	// strconv, so it reads the argument's type.
	builtins.SetEmit("totext", func(e builtins.Emitter, args []string, types []*ir.Type) (string, *ir.Type, error) {
		if isFloatType(types[0]) {
			e.Helper("dmFmtFloat", declFmtFloat, "strconv")
			return "dmFmtFloat(" + args[0] + ")", ir.Text(), nil
		}
		e.Imp("strconv")
		return "strconv.FormatInt(" + args[0] + ", 10)", ir.Text(), nil
	})

	for _, side := range []struct {
		name string
		left bool
	}{{"padleft", true}, {"padright", false}} {
		builtins.SetEmit(side.name, func(e builtins.Emitter, args []string, _ []*ir.Type) (string, *ir.Type, error) {
			e.Helper("dmPadText", declPadText, "strings")
			return "dmPadText(" + strings.Join(args, ", ") + ", " + strconv.FormatBool(side.left) + ")", ir.Text(), nil
		})
	}

	// isdigit/isalpha/isupper/islower share one helper and differ in the
	// function they call.
	for name, fn := range map[string]string{
		"isdigit": "dmIsDigit", "isalpha": "dmIsAlpha", "isupper": "dmIsUpper", "islower": "dmIsLower",
	} {
		builtins.SetEmit(name, viaHelper(fn, ir.Bool(), helperDecl{"dmClassify", declClassify, []string{"strings"}}))
	}
}
