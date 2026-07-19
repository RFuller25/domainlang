// `domain lsp` — the language server subcommand. Speaks LSP over
// stdin/stdout (Content-Length framing); logs go to stderr so they never
// corrupt the protocol stream. See docs/tooling.md for editor wiring.
package main

import (
	"fmt"
	"io"

	"domain/lsp"
)

// Lsp runs the language server until the client disconnects.
func Lsp(stdin io.Reader, stdout, stderr io.Writer) int {
	if err := lsp.Serve(stdin, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "domain lsp: %v\n", err)
		return 1
	}
	return 0
}
