// A session: the contract a console holds, the calls it makes, and the state it
// keeps between them. funroute-core.js is the language — schemas, printing,
// type names — and knows nothing about a server. This file is what turns that
// into a working page, and it is still DOM-free, so node --test can drive a
// whole edit-compile-run cycle without a browser.

import { FunRouteLanguage, clone, equalType, typeName } from "./funroute-core.js";

export function emptyContract() { return { types: [], args: [], result: null }; }

// A contract arrives either from the panel, where a declared type is a row
// being edited and its name may still be blank, or from JSON, where it is the
// map the server takes. Rows are the editable shape, so everything reads rows.
export function typeRows(types) {
  if (Array.isArray(types)) return types;
  return Object.entries(types || {}).map(([name, type]) => ({ name, type }));
}

export function isEmptyContract(contract) {
  if (!contract) return true;
  return !typeRows(contract.types).length && !contract.args?.length && !contract.result?.type && !contract.result?.doc;
}

export function contractPayload(contract) {
  if (isEmptyContract(contract)) return null;
  const args = (contract.args || []).map((arg) => ({
    name: String(arg.name || "").trim(), type: String(arg.type || "").trim(), doc: arg.doc || undefined,
  }));
  const result = contract.result ? {
    type: String(contract.result.type || "").trim(), doc: contract.result.doc || undefined,
  } : undefined;
  // A declared type is spelling, not data: the server expands it where it is
  // named, so two contracts that differ only in spelling compile the same.
  const types = {};
  for (const decl of typeRows(contract.types)) {
    const name = String(decl.name || "").trim();
    if (name) types[name] = String(decl.type || "").trim();
  }
  return { types: Object.keys(types).length ? types : undefined, args, result };
}

export function contractComments(contract, source) {
  const types = typeRows(contract?.types);
  const args = contract?.args || [];
  const result = contract?.result;
  if (!types.length && !args.length && !result) return source;
  let width = result ? 2 : 0;
  for (const arg of args) width = Math.max(width, arg.name.length + 1);
  const lines = types.map((decl) => `// ${decl.name} = ${decl.type}`);
  lines.push(...args.map((arg) => commentLine(`${arg.name}:`, width, arg.type, arg.doc)));
  if (result) lines.push(commentLine("→", width, result.type, result.doc));
  return `${lines.join("\n")}\n\n${source}`;
}

function commentLine(label, width, valueType, doc) {
  const head = `// ${label.padEnd(width)} ${valueType}`;
  return doc ? `${head.padEnd(28)} ${doc}` : head;
}



export class FunRouteClient {
  constructor({ baseURL = "", fetch: fetchImpl = globalThis.fetch } = {}) {
    if (!fetchImpl) throw new Error("FunRouteClient 需要 fetch 实现");
    this.baseURL = baseURL.replace(/\/$/, "");
    this.fetch = (...args) => fetchImpl.call(globalThis, ...args);
  }

  catalog() { return this.request("/api/catalog"); }
  checkContract(contract) { return this.request("/api/contract/check", { contract: contractPayload(contract) }); }
  parse(source) { return this.request("/api/parse", { source }); }
  compile(payload) { return this.request("/api/compile", payload); }
  run(payload) { return this.request("/api/run", payload); }

  async request(path, payload) {
    const response = await this.fetch(`${this.baseURL}${path}`, {
      method: payload ? "POST" : "GET",
      headers: payload ? { "Content-Type": "application/json" } : {},
      body: payload ? JSON.stringify(payload) : undefined,
    });
    const body = await response.json();
    if (!response.ok) {
      // The compiler reports where, in the line:column form every compiler
      // uses, so each place that shows a message shows the position with it.
      const where = body.error?.line ? `${body.error.line}:${body.error.column} ` : "";
      const failure = new Error(where + (body.error?.message || `HTTP ${response.status}`));
      Object.assign(failure, { line: body.error?.line, column: body.error?.column });
      throw failure;
    }
    return body;
  }
}

export class FunRouteWorkspace {
  constructor(client = new FunRouteClient()) {
    this.client = client;
    this.catalog = null;
    this.language = null;
    this.document = null;
    this.contract = emptyContract();
    this.contractCheck = null;
    this.compiled = null;
    this.result = null;
    this.resultCheck = null;
    this.status = { phase: "idle", message: "" };
    this._listeners = new Set();
    this._revision = 0;
  }

  subscribe(listener) {
    this._listeners.add(listener);
    listener(this.snapshot());
    return () => this._listeners.delete(listener);
  }

  snapshot() {
    return clone({ catalog: this.catalog, document: this.document, contract: this.contract,
      contractCheck: this.contractCheck,
      compiled: this.compiled, result: this.result, resultCheck: this.resultCheck, status: this.status });
  }

  async initialize() {
    this._status("loading", "正在载入语言目录…");
    this.catalog = await this.client.catalog();
    this.language = new FunRouteLanguage(this.catalog);
    this._status("ready", "语言目录已载入");
    return this.catalog;
  }

  setDocument(document) {
    this.document = clone(document);
    this.compiled = null;
    this.result = null;
    this.resultCheck = null;
    this._revision += 1;
    this._status("dirty", "表达式已修改，等待编译");
  }

  setContract(contract) {
    this.contract = clone(contract || emptyContract());
    this.contractCheck = null;
    this.compiled = null;
    this.result = null;
    this.resultCheck = null;
    this._revision += 1;
    this._status("dirty", "契约已修改，等待编译");
  }

  async checkContract() {
    const payload = contractPayload(this.contract);
    if (!payload?.result?.type) throw new Error("请先声明运行契约的返回类型");
    const revision = this._revision;
    this._status("loading", "正在检查运行契约…");
    const checked = await this.client.checkContract(this.contract);
    if (revision !== this._revision) return null;
    this.contractCheck = checked;
    this._status("success", `契约有效 · ${payload.args.length} 个入参 → ${payload.result.type}`);
    return clone(checked);
  }

  async parseSource(source) {
    const revision = ++this._revision;
    this._status("loading", "正在解析表达式…");
    const parsed = await this.client.parse(source);
    if (revision !== this._revision) return null;
    this.document = parsed.expr_json;
    this.compiled = null;
    this.result = null;
    this.resultCheck = null;
    this._status("ready", "表达式已解析");
    return clone(this.document);
  }

  // Parse and compile a text expression as one transaction. UIs can use this
  // before replacing their current canvas: a syntax or contract error leaves
  // the last valid document untouched.
  async compileSource(source) {
    const revision = ++this._revision;
    this._status("loading", "正在解析并检查表达式…");
    const parsed = await this.client.parse(source);
    if (revision !== this._revision) return null;
    if (!this.contractCheck && !await this.checkContract()) return null;
    if (revision !== this._revision) return null;
    this._status("loading", "正在按运行契约编译表达式…");
    const compiled = await this.client.compile(this._payload(parsed.expr_json));
    if (revision !== this._revision) return null;
    this.document = parsed.expr_json;
    this.compiled = compiled;
    this.result = null;
    this.resultCheck = null;
    this._status("success", `检查通过 · ${compiled.instructions} 条指令`);
    return clone({ document: this.document, compiled });
  }

  async compile() {
    if (!this.document) throw new Error("请先创建表达式");
    if (!this.contractCheck && !await this.checkContract()) return null;
    const revision = this._revision;
    this._status("loading", "正在编译…");
    const compiled = await this.client.compile(this._payload());
    if (revision !== this._revision) return null;
    this.compiled = compiled;
    this.result = null;
    this.resultCheck = null;
    this._status("success", `编译成功 · ${compiled.instructions} 条指令`);
    return clone(compiled);
  }

  async run(args, fuel = 10000) {
    if (!this.compiled && !await this.compile()) return null;
    const revision = this._revision;
    this._status("loading", "正在运行…");
    const result = await this.client.run({ ...this._payload(), args, fuel });
    if (revision !== this._revision) return null;
    const expected = this.compiled?.result || this.contractCheck?.result;
    if (!equalType(result.type, expected)) {
      const error = new Error(`运行结果类型 ${typeName(result.type)} 与契约 ${typeName(expected)} 不匹配`);
      this.resultCheck = { valid: false, expected: clone(expected), actual: clone(result.type) };
      this._status("error", error.message);
      throw error;
    }
    this.result = result;
    this.resultCheck = { valid: true, expected: clone(expected), actual: clone(result.type) };
    this._status("success", "运行成功");
    return clone(result);
  }

  reportError(error) {
    this._status("error", error instanceof Error ? error.message : String(error));
  }

  _payload(document = this.document) {
    return { expr_json: document, contract: contractPayload(this.contract) ?? undefined };
  }

  _status(phase, message) {
    this.status = { phase, message };
    for (const listener of this._listeners) listener(this.snapshot());
  }
}
