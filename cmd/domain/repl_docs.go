// `:docs` — the documentation site, served out of this binary, and linked to
// from the session.
//
// The whole documentation website is embedded in the binary already (package
// domain/docs, served by `domain expansion: documentation`). A REPL is exactly
// where you want it: the session is where you meet an error you do not
// recognize, and the page explaining it is a process away.
//
// `:docs` starts that server in the background and leaves it running for the
// rest of the session. From then on, a diagnostic's error code is a terminal
// hyperlink (OSC 8) to the page that explains that class of error — click it
// and the browser opens. Before `:docs`, and in a pipe, the same text is
// printed plain: a link to a server nobody started would be a dead one, and
// the point of the flourish is that it goes somewhere.
package main

import (
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// docsSite is the running documentation server, if `:docs` started one.
var docsSite struct {
	sync.RWMutex
	url string
}

// docsBaseURL is where the session's documentation is served, or "" when it is
// not being served at all.
func docsBaseURL() string {
	docsSite.RLock()
	defer docsSite.RUnlock()
	return docsSite.url
}

// startDocsSite serves the embedded documentation on port (0 picks a free
// one) and returns its URL. Starting it twice is a no-op that returns the
// URL already in use.
func startDocsSite(port int) (string, error) {
	docsSite.Lock()
	defer docsSite.Unlock()
	if docsSite.url != "" {
		return docsSite.url, nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", err
	}
	docsSite.url = fmt.Sprintf("http://localhost:%d/", ln.Addr().(*net.TCPAddr).Port)
	// The server lives as long as the session; a REPL that exits takes it
	// with it, which is the lifetime a user expects of `:docs`.
	go http.Serve(ln, documentationHandler()) //nolint:errcheck,gosec // best effort
	return docsSite.url, nil
}

// docsPageFor maps a diagnostic's code to the page that explains that kind of
// mistake. The codes come from the diagnostics engine (diag.Diagnostic.Code).
func docsPageFor(code string) string {
	switch code {
	case "syntax", "name", "indent":
		return "language"
	case "type":
		return "data-model"
	case "resolve":
		return "primitives"
	case "style", "perf", "lint":
		return "diagnostics"
	default:
		return "README"
	}
}

// docsLink wraps text in a terminal hyperlink to a documentation page, when
// there is a server to link to and a terminal to click in. Otherwise it
// returns text unchanged — every caller can therefore link unconditionally.
func docsLink(text, page string, color bool) string {
	base := docsBaseURL()
	if !color || base == "" || page == "" {
		return text
	}
	return ansi.SetHyperlink(base+"#/"+page) + text + ansi.ResetHyperlink()
}

// docs starts the documentation server and says where it is.
func (r *repl) docs(arg string) {
	port := 0 // a free port: a REPL should not fight the standalone command
	if arg != "" {
		n, err := parsePort(arg)
		if err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			return
		}
		port = n
	}
	url, err := startDocsSite(port)
	if err != nil {
		fmt.Fprintf(r.out, "error: cannot serve the documentation: %v\n", err)
		return
	}
	fmt.Fprintf(r.out, "documentation at %s — error codes in this session now link into it\n",
		docsLink(url, "README", r.color))
}

// parsePort validates a :docs port argument.
func parsePort(arg string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(arg, "%d", &n); err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("invalid port %q (want 1-65535)", arg)
	}
	return n, nil
}
