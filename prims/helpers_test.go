package prims

import (
	"domain/ast"
	"domain/token"
)

func tokenPos() token.Position { return token.Position{Line: 1, Col: 1} }

func opWords(words ...string) *ast.Operation {
	return &ast.Operation{Words: words, Raw: joinWords(words)}
}

func opWithString(name, s string) *ast.Operation {
	op := opWords(splitFields(name)...)
	op.Strings = []string{s}
	return op
}

func joinWords(words []string) string {
	out := ""
	for i, w := range words {
		if i > 0 {
			out += " "
		}
		out += w
	}
	return out
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
