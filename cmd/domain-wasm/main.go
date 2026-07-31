// `domain-wasm` is the language front end compiled for the browser: the same
// lexer, parser, resolver, optimizer and interpreter the `domain` binary uses,
// exposed to JavaScript as a single function. It is what makes the Run and
// Explain buttons on the documentation site work.
//
// Running the reader's program in their own tab, rather than on the server
// that hands them the docs, is not just a deployment convenience — it is what
// makes offering Run safe and honest at all:
//
//   - There is no filesystem under js/wasm, so `Cursed Energy: input.txt`
//     cannot reach anything. It fails to open the file and falls through to
//     stdin, which is exactly what it does on a terminal when the named input
//     is absent — so the semantics the docs describe are the semantics the
//     playground has, with no special casing.
//   - A program that does not terminate is stopped by terminating the worker.
//     A server-side runner cannot do that to a goroutine.
//   - Nothing the reader types leaves their machine.
//
// Build it with docs/wasm/build.sh, which also copies in Go's wasm_exec.js.
//
//go:build js && wasm

package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"syscall/js"

	"domain/interp"
	"domain/ir"
	"domain/lexer"
	"domain/optimizer"
	"domain/parser"
	"domain/prims"
)

// maxOutput caps what a single run may print. A program that emits without
// bound would otherwise take the tab down with it, and no explanation of a
// pipeline needs more than this.
const maxOutput = 256 << 10 // 256 KiB

// capped is an io.Writer that stops accepting output past a limit, recording
// that it truncated. It never errors: a program that writes too much should
// show what it managed rather than fail.
type capped struct {
	buf       strings.Builder
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.buf.Len() >= maxOutput {
		c.truncated = true
		return len(p), nil
	}
	if room := maxOutput - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:room])
		c.truncated = true
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *capped) String() string {
	s := c.buf.String()
	if c.truncated {
		s += "\n… output truncated at 256 KiB."
	}
	return s
}

// runResult is the shape handed back to JavaScript. Errors are values here
// rather than exceptions: a program that fails to parse is an ordinary
// outcome for a playground, and the page renders it the same way it renders
// output.
type runResult struct {
	output  string
	explain []string
	err     string
}

func (r runResult) toJS() js.Value {
	explain := make([]any, len(r.explain))
	for i, e := range r.explain {
		explain[i] = e
	}
	return js.ValueOf(map[string]any{
		"output":  r.output,
		"explain": explain,
		"error":   r.err,
	})
}

// virtualLibs turns the libraries that travelled with a program into resolve
// options. `Innate Domain: lib/shapes` needs a filesystem to find its library
// on, and there is none here, so the gallery ships each program's imports
// alongside it and they are served from memory. BaseDir is "/" purely to give
// the resolver a directory to join against — without one it has no candidate
// paths at all and reports that imports need a file context.
func virtualLibs(libs map[string]string) prims.ResolveOptions {
	if len(libs) == 0 {
		return prims.ResolveOptions{}
	}
	return prims.ResolveOptions{
		BaseDir: "/",
		ReadFile: func(path string) ([]byte, error) {
			// The resolver asks for "/lib/shapes.domain"; the map is keyed by
			// the target as written in the program, "lib/shapes".
			key := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(path), "/"), ".domain")
			if src, ok := libs[key]; ok {
				return []byte(src), nil
			}
			return nil, fs.ErrNotExist
		},
	}
}

// run takes a program and its input and produces what the page displays. It
// mirrors loadPipeline + Execute in cmd/domain, minus the parts that only mean
// something with a filesystem and a terminal.
func run(source, input string, libs map[string]string, optimize, explain bool) (res runResult) {
	// A panic anywhere in the front end would otherwise take down the worker
	// and leave the reader with a dead Run button and no explanation.
	defer func() {
		if r := recover(); r != nil {
			res = runResult{err: fmt.Sprintf("internal error: %v", r)}
		}
	}()

	toks, err := lexer.Lex(source)
	if err != nil {
		return runResult{err: err.Error()}
	}
	prog, err := parser.Parse(source, toks)
	if err != nil {
		return runResult{err: err.Error()}
	}
	// No search path: there is no filesystem to look in. Libraries, when the
	// program has any, come from the caller instead.
	pipe, err := prims.ResolveWith(prog, virtualLibs(libs))
	if err != nil {
		return runResult{err: err.Error()}
	}

	rewrites := optimizer.Optimize(pipe, optimize)
	var messages []string
	if explain {
		if len(rewrites) == 0 {
			messages = append(messages, "[explain] no optimizations applied.")
		}
		for _, r := range rewrites {
			messages = append(messages, "[explain] "+r.Message)
		}
	}

	var stdout, stderr capped
	ctx := &ir.Context{
		// The input box is stdin. Every `Cursed Energy: <file>` falls back to
		// it, since nothing can be opened here.
		Stdin:  strings.NewReader(input),
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if _, err := interp.Run(pipe, ctx); err != nil {
		return runResult{output: stdout.String(), explain: messages, err: err.Error()}
	}
	out := stdout.String()
	// A mid-pipeline `Reveal: stderr` is a debugging tool; showing it under the
	// answer keeps it visible without mixing it into the output.
	if s := stderr.String(); s != "" {
		out = strings.TrimRight(out, "\n") + "\n--- stderr ---\n" + s
	}
	return runResult{output: out, explain: messages}
}

func main() {
	js.Global().Set("domainRun", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 || args[0].Type() != js.TypeObject {
			return runResult{err: "domainRun expects one options object"}.toJS()
		}
		o := args[0]
		str := func(k string) string {
			if v := o.Get(k); v.Type() == js.TypeString {
				return v.String()
			}
			return ""
		}
		// libs arrives as a plain object of target -> source.
		strMap := func(k string) map[string]string {
			v := o.Get(k)
			if v.Type() != js.TypeObject {
				return nil
			}
			out := map[string]string{}
			keys := js.Global().Get("Object").Call("keys", v)
			for i := range keys.Length() {
				key := keys.Index(i).String()
				if s := v.Get(key); s.Type() == js.TypeString {
					out[key] = s.String()
				}
			}
			return out
		}
		boolOr := func(k string, def bool) bool {
			if v := o.Get(k); v.Type() == js.TypeBoolean {
				return v.Bool()
			}
			return def
		}
		return run(str("source"), str("input"), strMap("libs"), boolOr("optimize", true), boolOr("explain", false)).toJS()
	}))
	// The worker signals readiness once the export exists, then this blocks
	// forever: returning from main would tear down the Go runtime and with it
	// the function we just exported.
	js.Global().Call("postMessage", map[string]any{"ready": true})
	select {}
}
