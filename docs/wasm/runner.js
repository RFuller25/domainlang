"use strict";
// The playground's worker: it loads the WebAssembly build of the language and
// runs one program per message.
//
// It is a worker rather than page code for one reason — a Domain program can
// loop forever, and `worker.terminate()` is the only way to actually stop it.
// Go has no way to interrupt a running goroutine from outside, so the page
// times a run out, throws this worker away, and starts a fresh one on the next
// Run. Everything here is therefore disposable by design.

importScripts("wasm_exec.js");

let ready = null;

// boot instantiates the module once per worker. Go's shim expects to `run` an
// instance and stay resident; main() blocks forever after exporting domainRun,
// so `go.run` never resolves and must not be awaited.
function boot() {
  if (ready) return ready;
  ready = (async () => {
    const go = new Go();
    const source = await fetch("domain.wasm", { cache: "force-cache" });
    if (!source.ok) throw new Error(`could not load domain.wasm (HTTP ${source.status})`);
    // instantiateStreaming needs the right MIME type and not every static
    // server sends it; fall back to buffering rather than failing.
    let result;
    if (WebAssembly.instantiateStreaming && (source.headers.get("content-type") || "").includes("wasm")) {
      result = await WebAssembly.instantiateStreaming(source, go.importObject);
    } else {
      result = await WebAssembly.instantiate(await source.arrayBuffer(), go.importObject);
    }
    go.run(result.instance); // resolves only when the program exits, which it never does
    // main() posts {ready:true} once domainRun exists. Waiting for the export
    // itself is simpler and does not depend on message ordering.
    for (let i = 0; i < 2000 && typeof self.domainRun !== "function"; i++) {
      await new Promise(r => setTimeout(r, 1));
    }
    if (typeof self.domainRun !== "function") throw new Error("the language module did not start");
  })();
  return ready;
}

self.onmessage = async e => {
  const { id, source, input, libs, explain, optimize } = e.data || {};
  // main() announces itself through postMessage; that is not a request.
  if (id === undefined) return;
  try {
    await boot();
    const res = self.domainRun({ source, input, libs: libs || {}, explain: !!explain, optimize: optimize !== false });
    self.postMessage({
      id, ok: true,
      result: { output: res.output, explain: res.explain || [], error: res.error },
    });
  } catch (err) {
    self.postMessage({ id, ok: false, error: (err && err.message) || String(err) });
  }
};
