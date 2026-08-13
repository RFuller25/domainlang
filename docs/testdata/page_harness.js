"use strict";
// Boots docs/index.html outside a browser and drives its search box.
//
// The unit tests over render.js cannot catch the bug this exists for: search
// broke because index.html referenced `searchIndex`, a name that lived inside
// render.js's closure and was never exported. Both files were individually
// fine. Indexing threw once per page inside a catch that ignored it, and every
// keystroke threw, so the panel simply never left "Start typing to search".
//
// So this runs the page's own scripts, against a DOM stub just large enough to
// get through boot, and reports what the search panel actually renders. A
// ReferenceError anywhere in the page's top level, or in the search path, comes
// back as a failure rather than as silence.
//
// Usage: node page_harness.js <docsDir> <query>
// Prints one JSON object: { errors, indexed, resultCount, docIds, html }.

const fs = require("fs");
const path = require("path");
const vm = require("vm");

const docsDir = path.resolve(process.argv[2] || ".");
const query = process.argv[3] || "";
const errors = [];

// ---- DOM stub ----------------------------------------------------------
// Elements are created on demand and cached by id, so the page finds whatever
// it asks for. Only what index.html actually touches is implemented.
function makeElement(id) {
  const listeners = {};
  const el = {
    id,
    tagName: "DIV",
    value: "",
    textContent: "",
    innerHTML: "",
    dataset: {},
    classList: {
      _set: new Set(),
      add(c) { this._set.add(c); },
      remove(c) { this._set.delete(c); },
      contains(c) { return this._set.has(c); },
      toggle(c) { this._set.has(c) ? this._set.delete(c) : this._set.add(c); },
    },
    listeners,
    addEventListener(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
    removeEventListener(type, fn) {
      if (listeners[type]) listeners[type] = listeners[type].filter(f => f !== fn);
    },
    attributes: {},
    setAttribute(k, v) { el.attributes[k] = String(v); },
    getAttribute(k) { return k in el.attributes ? el.attributes[k] : null; },
    removeAttribute(k) { delete el.attributes[k]; },
    // Non-null so the focus-trap treats controls as visible, which is what a
    // rendered dialog looks like.
    offsetParent: {},
    querySelector() { return makeElement("anonymous"); },
    querySelectorAll() { return []; },
    scrollIntoView() {}, focus() {}, select() {},
    getBoundingClientRect() { return { top: 0 }; },
    closest() { return null; },
    children: [],
    appendChild(child) { el.children.push(child); return child; },
    insertAdjacentHTML() {},
    after() {},
    remove() {},
    setAttributeNS() {},
    hasAttribute() { return false; },
    // Enough of CSSStyleDeclaration for applyPageAccent's --jjk-c toggling;
    // nothing reads it back in the harness so it doesn't need to store state.
    style: { setProperty() {}, removeProperty() {}, getPropertyValue: () => "" },
  };
  return el;
}

const elements = new Map();
function byId(id) {
  if (!elements.has(id)) elements.set(id, makeElement(id));
  return elements.get(id);
}

function fire(el, type, event) {
  for (const fn of el.listeners[type] || []) {
    try {
      fn(event);
    } catch (e) {
      errors.push(`${type} handler on #${el.id}: ${e && e.stack ? e.stack : e}`);
    }
  }
}

const documentStub = {
  documentElement: makeElement("html"),
  // The hero canvas self-terminates its rAF loop once it leaves the document;
  // `contains` always says yes here since nothing in the harness ever runs a
  // second frame (requestAnimationFrame below runs synchronously, once).
  body: { contains: () => true },
  activeElement: makeElement("body"),
  getElementById: byId,
  createElement: tag => makeElement("created-" + tag),
  querySelector: () => makeElement("main"),
  querySelectorAll: () => [],
  addEventListener() {},
};

// The page reports trouble it has deliberately caught — a page it could not
// index, a fetch that failed — through console.warn/error rather than by
// throwing. Those are exactly the symptoms worth failing a test on, so they
// are collected alongside thrown errors instead of scrolling past.
const pageConsole = {
  log: (...a) => {},
  info: (...a) => {},
  debug: (...a) => {},
  warn: (...a) => errors.push("console.warn: " + a.map(String).join(" ")),
  error: (...a) => errors.push("console.error: " + a.map(String).join(" ")),
};

const sandbox = {
  console: pageConsole,
  document: documentStub,
  // The page reads location.hash at boot and writes it when a result is opened.
  location: { protocol: "http:", hash: "", pathname: "/", search: "" },
  // Deep-linked search rewrites the hash as the reader types, without firing
  // the router — so replaceState has to exist and has to not navigate.
  history: {
    replaceState(_state, _title, url) {
      if (typeof url === "string" && url.startsWith("#")) sandbox.location.hash = url;
      else sandbox.location.hash = "";
    },
    pushState() {},
  },
  // A real store, so recent searches can be exercised rather than stubbed out.
  localStorage: (() => {
    const store = new Map();
    return {
      getItem: k => (store.has(k) ? store.get(k) : null),
      setItem: (k, v) => store.set(k, String(v)),
      removeItem: k => store.delete(k),
    };
  })(),
  matchMedia: () => ({ matches: false }),
  requestAnimationFrame: fn => fn(),
  // The hero canvas reads --jjk-c off the root element's computed style; the
  // harness has no layout engine, so an empty value is enough to exercise the
  // fallback-color path without crashing.
  getComputedStyle: () => ({ getPropertyValue: () => "" }),
  navigator: { clipboard: { writeText: async () => {} } },
  // Real timers: the page uses them for the run timeout and the copy-button
  // reset, and a no-op stub would quietly break behaviour under test.
  setTimeout, clearTimeout, setInterval, clearInterval,
  performance,
  // The playground's worker. Nothing here builds one — the wasm artifact is
  // absent in the harness — but its absence must be a clean "not available"
  // rather than a ReferenceError.
  Worker: function () { throw new Error("no Worker in the harness"); },
  // Serve the site's own files off disk, the way the page fetches them: the
  // Markdown pages, and the generated JSON behind the gallery and the
  // primitive index. A missing file 404s rather than throwing, which is how
  // the page discovers that the playground artifact was never built.
  fetch: async (file, opts) => {
    const p = path.join(docsDir, file);
    if (!fs.existsSync(p)) return { ok: false, status: 404, text: async () => "", json: async () => null };
    const read = () => fs.readFileSync(p, "utf8");
    return { ok: true, status: 200, text: async () => read(), json: async () => JSON.parse(read()) };
  },
  DomainRender: require(path.join(docsDir, "render.js")),
};
sandbox.window = sandbox;
sandbox.globalThis = sandbox;
// Window listeners are kept so the harness can drive the hash router the way
// a click does, rather than reaching into the page's internals.
const windowListeners = {};
sandbox.window.addEventListener = (type, fn) => { (windowListeners[type] = windowListeners[type] || []).push(fn); };
sandbox.window.removeEventListener = (type, fn) => {
  if (windowListeners[type]) windowListeners[type] = windowListeners[type].filter(f => f !== fn);
};
sandbox.window.scrollTo = () => {};

// navigate sets the hash and fires the router, as the browser would.
async function navigate(hash) {
  sandbox.location.hash = hash;
  for (const fn of windowListeners.hashchange || []) {
    try {
      await fn();
    } catch (e) {
      errors.push(`hashchange -> ${hash}: ${e && e.stack ? e.stack : e}`);
    }
  }
  // Let the route's own awaits (fetch, JSON parse) settle.
  for (let i = 0; i < 50; i++) await new Promise(r => setImmediate(r));
}

// ---- Run the page's inline scripts -------------------------------------
const html = fs.readFileSync(path.join(docsDir, "index.html"), "utf8");
const scripts = [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)].map(m => m[1]);
if (!scripts.length) {
  console.log(JSON.stringify({ errors: ["no inline <script> found in index.html"] }));
  process.exit(0);
}

const ctx = vm.createContext(sandbox);
for (const [i, code] of scripts.entries()) {
  try {
    vm.runInContext(code, ctx, { filename: `index.html#script${i}` });
  } catch (e) {
    errors.push(`script ${i}: ${e && e.stack ? e.stack : e}`);
  }
}

// ---- Drive the search box, then walk every page ------------------------
(async () => {
  const input = byId("searchInput");
  const results = byId("searchResults");
  const content = byId("content");

  // Boot indexes every page off disk; give those promises a chance to settle,
  // re-firing until the panel stops saying it is still building.
  for (let attempt = 0; attempt < 200; attempt++) {
    await new Promise(r => setImmediate(r));
    input.value = query;
    fire(input, "input", { target: input });
    if (!/Building search index/.test(results.innerHTML)) break;
  }

  const hits = [...results.innerHTML.matchAll(/data-search-result[^>]*/g)];
  const docIds = [...results.innerHTML.matchAll(/href="#\/([^"#]+)/g)].map(m => m[1]);

  // Visit every page in the manifest. A route that throws, or renders nothing,
  // is a broken page — and the generated ones cannot be checked any other way.
  const pages = {};
  const ids = [...html.matchAll(/\{ id: "([^"]+)"/g)].map(m => m[1]);
  for (const id of ids) {
    await navigate(`#/${id}`);
    pages[id] = {
      length: content.innerHTML.length,
      hasError: /class="error-box"/.test(content.innerHTML),
      html: content.innerHTML.slice(0, 400),
    };
  }

  console.log(JSON.stringify({
    errors,
    indexed: !/Building search index/.test(results.innerHTML),
    resultCount: hits.length,
    docIds,
    pages,
    html: results.innerHTML.slice(0, 2000),
  }));
})();
