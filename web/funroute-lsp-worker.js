// The language server, in a worker: the Go build of lang/lsp loaded from
// dist/, speaking the protocol one message at a time through postMessage.
// Messages that arrive while the module is still loading wait their turn.
/* global Go */
importScripts("dist/wasm_exec.js");

const waiting = [];
self.onmessage = (event) => waiting.push(event.data);

const go = new Go();
WebAssembly.instantiateStreaming(fetch("dist/funroute.wasm"), go.importObject).then(({ instance }) => {
  go.run(instance);
  self.funroute.connect((message) => self.postMessage(message));
  self.onmessage = (event) => self.funroute.send(event.data);
  for (const message of waiting.splice(0)) self.funroute.send(message);
});
