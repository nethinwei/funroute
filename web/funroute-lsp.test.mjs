// The browser build of the language server, driven the way the worker drives
// it: one message in, messages out. Run after make wasm.
import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";
import "./dist/wasm_exec.js";
import { children, isCard, wrap } from "./src/projection.ts";

// start loads a fresh server. A request resolves with its response. The
// server handles messages in order, so a notification has been handled once
// a request sent after it is answered: notify waits for one, which asks for
// a method no server has and changes nothing.
async function start() {
  const go = new globalThis.Go();
  const module = await readFile(new URL("./dist/funroute.wasm", import.meta.url));
  const { instance } = await WebAssembly.instantiate(module, go.importObject);
  go.run(instance);
  const received = [];
  const waiting = new Map();
  globalThis.funroute.connect((text) => {
    const message = JSON.parse(text);
    received.push(message);
    waiting.get(message.id)?.(message);
    waiting.delete(message.id);
  });
  let id = 0;
  const send = (message) => globalThis.funroute.send(JSON.stringify({ jsonrpc: "2.0", ...message }));
  const request = (method, params) => new Promise((resolve) => {
    id += 1;
    waiting.set(id, resolve);
    send({ id, method, params });
  });
  return {
    received,
    request,
    notify: async (method, params) => {
      send({ method, params });
      await request("$/handled", {});
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

test("every block the workbench drops in is a program the language compiles", async () => {
  const server = await start();
  await server.request("initialize", { capabilities: {} });
  // No contract: the arguments are the free variables, so the names a block
  // leaves to fill in are parameters. That one may have no concrete type yet
  // is what a name to fill in is; any other diagnostic is the block's own.
  await server.notify("funroute/setContract", { contract: {} });
  const catalog = (await server.request("funroute/catalog", {})).result;
  const blocks = [...catalog.special_forms, ...catalog.functions].filter((item) => item.wrap);
  assert.ok(blocks.length > 0, "the catalog offers no block");
  for (const block of blocks) {
    const uri = `file:///${block.name}.fr`;
    await server.notify("textDocument/didOpen", { textDocument: { uri, version: 1, text: wrap(block.wrap, "x") } });
    const published = server.received.find((message) => message.method === "textDocument/publishDiagnostics" && message.params.uri === uri);
    const own = published.params.diagnostics.map((diagnostic) => diagnostic.message).filter((message) => !message.includes("cannot infer a concrete type"));
    assert.deepEqual(own, [], `${block.name}: ${wrap(block.wrap, "x")}`);
  }
});

// The structure view draws a node that is not a card as what joins its parts
// and each part. A record's entries keep their values in items, so a block
// in one is among its parts only when they are read with children: the nodes
// of its fields alone miss it.
test("a block inside a record is among the record's parts", async () => {
  const server = await start();
  await server.request("initialize", { capabilities: {} });
  const catalog = await server.request("funroute/catalog", {});
  const lazy = new Set(catalog.result.functions.filter((item) => item.special).map((item) => item.name));
  const uri = "file:///record.fr";
  await server.notify("textDocument/didOpen", { textDocument: { uri, version: 1, text: "{a: if(c, 1, 2), b: 3}" } });
  const { result: tree } = await server.request("funroute/syntaxTree", { textDocument: { uri } });
  assert.equal(isCard(tree, lazy), false);
  assert.equal(children(tree).filter((node) => isCard(node, lazy)).length, 1);
  assert.equal(tree.fields.flatMap((field) => field.nodes ?? []).some((node) => isCard(node, lazy)), false);
});
