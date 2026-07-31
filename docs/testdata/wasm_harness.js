"use strict";
// Runs the playground's WebAssembly module outside a browser, so what the Run
// button does can be tested rather than clicked.
//
// It loads docs/wasm/domain.wasm exactly as docs/wasm/runner.js does — Go's
// own wasm_exec.js shim, one instance, `go.run` left unresolved because main()
// blocks forever after exporting domainRun — and then calls that export with
// each case it is given.
//
// Usage: node wasm_harness.js <wasmDir> <casesJsonPath>
// Cases: [{ source, input, libs?, explain?, optimize? }, ...]
// Prints: [{ output, explain, error }, ...]

const fs = require("fs");
const path = require("path");
const vm = require("vm");

const wasmDir = path.resolve(process.argv[2]);
const cases = JSON.parse(fs.readFileSync(process.argv[3], "utf8"));

// wasm_exec.js assigns globalThis.Go and expects the browser/node globals Go's
// runtime pokes at. Node 22 has crypto, performance, TextEncoder and
// TextDecoder already; running the shim in this context is enough.
vm.runInThisContext(fs.readFileSync(path.join(wasmDir, "wasm_exec.js"), "utf8"),
  { filename: "wasm_exec.js" });

(async () => {
  const go = new globalThis.Go();
  const bytes = fs.readFileSync(path.join(wasmDir, "domain.wasm"));
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);

  // main() calls postMessage to announce itself; outside a worker there is no
  // such global, so provide one rather than let the module die on its way out.
  const announced = [];
  globalThis.postMessage = msg => announced.push(msg);

  go.run(instance); // never resolves — main() blocks after exporting domainRun

  for (let i = 0; i < 2000 && typeof globalThis.domainRun !== "function"; i++) {
    await new Promise(r => setTimeout(r, 1));
  }
  if (typeof globalThis.domainRun !== "function") {
    console.error("domainRun was never exported");
    process.exit(1);
  }

  const results = cases.map(c => {
    const r = globalThis.domainRun({
      source: c.source, input: c.input || "", libs: c.libs || {},
      explain: !!c.explain, optimize: c.optimize !== false,
    });
    return { output: r.output, explain: r.explain || [], error: r.error };
  });
  process.stdout.write(JSON.stringify({ announced, results }));
  process.exit(0);
})().catch(e => {
  console.error(e && e.stack ? e.stack : String(e));
  process.exit(1);
});
