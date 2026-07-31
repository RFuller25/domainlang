// `domain expansion: documentation` — serve the browsable documentation
// website (embedded into the binary at build time) over HTTP and open it in a
// browser. Unlike the other expansion commands it takes no program file; its
// only argument is the optional port:
//
//	domain expansion: documentation            # serve on http://localhost:4444
//	domain expansion: documentation -p 8080    # serve on http://localhost:8080
//
// Because the site is embedded (see package domain/docs), this works from any
// install of the binary — including the NixOS package — with no source tree
// present.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"domain/docs"
)

// defaultDocsPort is where `documentation` serves when no -p is given.
const defaultDocsPort = 4444

// parseDocumentationArgs reads the optional `-p PORT` / `--port PORT` flag
// (also accepting the `-p=PORT` form). The documentation command takes no
// file, so any bare argument is an error.
func parseDocumentationArgs(args []string) (int, error) {
	port := defaultDocsPort
	for i := 0; i < len(args); i++ {
		a := args[i]
		var val string
		switch {
		case a == "-p" || a == "--port":
			i++
			if i >= len(args) {
				return 0, fmt.Errorf("%s requires a port number", a)
			}
			val = args[i]
		case strings.HasPrefix(a, "-p="):
			val = a[len("-p="):]
		case strings.HasPrefix(a, "--port="):
			val = a[len("--port="):]
		case strings.HasPrefix(a, "-"):
			return 0, fmt.Errorf("unknown flag %q (documentation accepts only -p/--port)", a)
		default:
			return 0, fmt.Errorf("documentation takes no file argument (got %q)", a)
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > 65535 {
			return 0, fmt.Errorf("invalid port %q (want 1-65535)", val)
		}
		port = n
	}
	return port, nil
}

// buildStamp describes the binary serving the docs. The documentation is
// embedded in it, so the two always match — which is worth saying out loud on
// a site whose whole discipline is that the reference describes the code that
// exists. Without it a reader has no way to tell whether the page in front of
// them is their build or a newer one.
type buildStamp struct {
	Version  string `json:"version"`            // module version, or "(devel)"
	Revision string `json:"revision,omitempty"` // VCS commit, when built from a checkout
	Time     string `json:"time,omitempty"`     // commit time
	Modified bool   `json:"modified,omitempty"` // built with uncommitted changes
	Go       string `json:"go"`
}

// readBuildStamp reads what the toolchain recorded at link time. `go build`
// stamps VCS information automatically from a clean checkout; `go run` and a
// build from a tarball do not, so every field beyond Go is best-effort.
func readBuildStamp() buildStamp {
	s := buildStamp{Version: "(unknown)", Go: runtime.Version()}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return s
	}
	if info.Main.Version != "" {
		s.Version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			s.Revision = setting.Value
		case "vcs.time":
			s.Time = setting.Value
		case "vcs.modified":
			s.Modified = setting.Value == "true"
		}
	}
	return s
}

// documentationHandler serves the embedded documentation site: "/" resolves to
// index.html, and index.html fetches the sibling Markdown pages by name.
// /build.json is the one thing the site cannot get from the embedded files —
// which binary is serving them.
func documentationHandler() http.Handler {
	files := http.FileServer(http.FS(docs.FS))
	stamp, err := json.Marshal(readBuildStamp())
	if err != nil { // a struct of strings; unreachable in practice
		stamp = []byte("{}")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/build.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(stamp)
	})
	mux.Handle("/", files)
	return mux
}

// cmdDocumentation binds the port, prints where to reach the site, opens a
// browser best-effort, and serves until interrupted. It blocks; Ctrl+C ends it.
func cmdDocumentation(args []string, stdout, stderr io.Writer) int {
	port, err := parseDocumentationArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "domain: %v\n", err)
		return 2
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintf(stderr, "domain: cannot serve documentation on port %d: %v\n", port, err)
		return 1
	}

	url := fmt.Sprintf("http://localhost:%d/", port)
	fmt.Fprintln(stdout, "Domain Expansion: Documentation")
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  Serving the documentation at %s\n", url)
	fmt.Fprintln(stdout, "  Press Ctrl+C to close the domain.")
	fmt.Fprintln(stdout)

	openBrowser(url, stdout)

	if err := http.Serve(ln, documentationHandler()); err != nil {
		fmt.Fprintf(stderr, "domain: documentation server error: %v\n", err)
		return 1
	}
	return 0
}

// openBrowser launches the platform's default browser at url. It is a
// best-effort courtesy: if no opener is available (a common case on a headless
// server, or the NixOS binary without xdg-open on PATH), the URL was already
// printed, so the failure is just noted, never fatal.
func openBrowser(url string, stdout io.Writer) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(stdout, "  (couldn't open a browser automatically — visit the URL above)")
		return
	}
	// Reap the opener so it doesn't linger as a zombie; we don't care whether
	// it succeeded, only that we don't block on it.
	go func() { _ = cmd.Wait() }()
}
