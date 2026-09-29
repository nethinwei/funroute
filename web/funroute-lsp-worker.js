// The language server, in a worker: the Go build of lang/lsp loaded from
// dist/, speaking the protocol one message at a time through postMessage.
// Messages that arrive while the module is still loading wait their turn.
//
// Protocol messages are strings. The worker's own reports are objects: while
// the module downloads, { loading: { loaded, total } }; then { ready: true },
// or { failed: message }. Nothing times out: a slow network only takes longer.
/* global Go */
importScripts("dist/wasm_exec.js");

const waiting = [];
self.onmessage = (event) => waiting.push(event.data);

// sizeOf is the module's size as built, which make wasm writes beside it.
// Pages sends the module gzipped and without a Content-Length, but the stream
// reads decompressed bytes, so the built size is the one to count against.
// An uncompressed response's own length serves when that file is missing;
// with neither, the total is 0 and the page shows only what has arrived.
function sizeOf(response, built) {
  return Number(built.trim()) || (response.headers.has("Content-Encoding") ? 0 : Number(response.headers.get("Content-Length"))) || 0;
}

// counted passes the module's bytes on as they arrive, reporting how many
// have come at most every 100 ms and once more at the end.
function counted(body, total) {
  const reader = body.getReader();
  let loaded = 0;
  let reported = 0;
  const report = () => self.postMessage({ loading: { loaded, total } });
  return new ReadableStream({
    async pull(controller) {
      const { done, value } = await reader.read();
      if (done) {
        report();
        controller.close();
        return;
      }
      loaded += value.byteLength;
      if (performance.now() - reported > 100) {
        reported = performance.now();
        report();
      }
      controller.enqueue(value);
    },
  });
}

async function load() {
  const [response, built] = await Promise.all([
    fetch("dist/funroute.wasm"),
    fetch("dist/funroute.wasm.size").then((r) => (r.ok ? r.text() : ""), () => ""),
  ]);
  if (!response.ok) throw new Error(`下载失败：HTTP ${response.status}`);
  const total = sizeOf(response, built);
  self.postMessage({ loading: { loaded: 0, total } });
  const stream = new Response(counted(response.body, total), { headers: { "Content-Type": "application/wasm" } });
  const go = new Go();
  const { instance } = await WebAssembly.instantiateStreaming(stream, go.importObject);
  go.run(instance);
  self.funroute.connect((message) => self.postMessage(message));
  self.onmessage = (event) => self.funroute.send(event.data);
  for (const message of waiting.splice(0)) self.funroute.send(message);
  self.postMessage({ ready: true });
}

load().catch((error) => self.postMessage({ failed: error instanceof Error ? error.message : String(error) }));
