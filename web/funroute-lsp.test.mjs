// The browser build of the language server, driven the way the worker drives
// it: one message in, messages out. Run after make wasm.
import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";
import "./dist/wasm_exec.js";

async function start() {
  const go = new globalThis.Go();
  const module = await readFile(new URL("./dist/funroute.wasm", import.meta.url));
  const { instance } = await WebAssembly.instantiate(module, go.importObject);
  go.run(instance);
  const received = [];
  globalThis.funroute.connect((message) => received.push(JSON.parse(message)));
  let id = 0;
  const settle = () => new Promise((resolve) => setTimeout(resolve, 20));
  return {
    received,
    notify: async (method, params) => { globalThis.funroute.send(JSON.stringify({ jsonrpc: "2.0", method, params })); await settle(); },
    request: async (method, params) => {
      id += 1;
      globalThis.funroute.send(JSON.stringify({ jsonrpc: "2.0", id, method, params }));
      await settle();
      return received.find((message) => message.id === id);
    },
  };
}

test("the browser build answers a whole session", async () => {
  const server = await start();
  const initialized = await server.request("initialize", { capabilities: { general: { positionEncodings: ["utf-8"] } } });
  assert.equal(initialized.result.capabilities.positionEncoding, "utf-8");
  await server.notify("funroute/setContract", { contract: { args: [{ name: "health", type: "string" }] } });
  const uri = "file:///rule.fr";
  await server.notify("textDocument/didOpen", { textDocument: { uri, version: 1, text: 'if(route.is_healthy_v1(health), "adyen", "stripe")' } });
  const published = server.received.find((message) => message.method === "textDocument/publishDiagnostics");
  assert.deepEqual(published.params.diagnostics, []);
  const run = await server.request("workspace/executeCommand", { command: "funroute.run", arguments: [{ uri, args: { health: "UP" } }] });
  assert.equal(run.result.value, "adyen");
  assert.deepEqual(run.result.unavailable, []);
  const hover = await server.request("textDocument/hover", { textDocument: { uri }, position: { line: 0, character: 4 } });
  assert.match(hover.result.contents.value, /route\.is_healthy_v1/);
});
