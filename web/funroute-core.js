// FunRoute's headless browser SDK. This module has no DOM dependency: an app
// may use it with React, Vue, a terminal UI, or no UI at all.



// typeName and equalType are protocol, not presentation: the text they produce
// is what ParseType reads back, so it has to match the Go side exactly.
export function typeName(type) {
  if (!type) return "unknown";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeName(type.elem)}>`;
  if (type.kind === "handle") return `handle<${type.name}>`;
  if (type.kind === "enum") return `enum<${type.name}>{${(type.values || []).join(",")}}`;
  return type.name || type.kind || "unknown";
}

function equalType(left, right) {
  if (!left || !right || left.kind !== right.kind || left.name !== right.name) return false;
  if (left.kind === "array" || left.kind === "dict") return equalType(left.elem, right.elem);
  if (left.kind === "enum") return JSON.stringify(left.values || []) === JSON.stringify(right.values || []);
  return true;
}

// contractEnums lists every enum the host contract declares, so a canvas can
// offer the members a program may refer to. It follows the same rule the
// compiler does: arguments and the result, containers included.
export function contractEnums(contract) {
  const found = new Map();
  const visit = (type) => {
    if (!type) return;
    if (type.elem) return visit(type.elem);
    if (type.kind === "enum" && type.name) found.set(type.name, type.values || []);
  };
  for (const arg of contract?.args || []) visit(arg.type);
  // A checked contract carries the result as the type itself; the panel's own
  // draft wraps it in {type}. Both shapes reach this function.
  visit(contract?.result?.type || contract?.result);
  return [...found].map(([name, values]) => ({ name, values }));
}

// A canvas knows two kinds of thing, and nothing else:
//
//   control blocks — branches, lazy choices and local names. They are the
//     palette's components, they are drawn as cards, and they can nest without
//     limit, because the language puts no expression in a special position.
//   expressions    — everything else. They are written as one line of source.
//
// Which names are control blocks is not written here: the catalog already says
// it. A special form that has an ExprJSON node of its own (switch, for, reduce,
// let) is one; a lazy call (if, fallback) is one; and or not are special forms
// without a node, because they expand to if — they are operators, not blocks.
function controlBlocksOf(catalog) {
  const nodes = new Set((catalog?.nodes || []).map((schema) => schema.node));
  const blocks = new Set();
  for (const form of catalog?.special_forms || []) {
    if (nodes.has(form.special || form.name)) blocks.add(form.name);
  }
  for (const item of catalog?.functions || []) {
    if (item.special) blocks.add(item.name);
  }
  return blocks;
}

// isPlainExpression reports whether a subtree holds no control block at all,
// and so can be read and edited as a single line of source.
export function isPlainExpression(value, language) {
  if (Array.isArray(value)) return value.every((item) => isPlainExpression(item, language));
  if (!value || typeof value !== "object") return true;
  if (language?.isControlBlock(value)) return false;
  return Object.values(value).every((item) => isPlainExpression(item, language));
}

// firstEmptySlot finds where a block would take an expression: its first empty
// expression field, or the first empty item of its first expression list. A
// control block's first slot is the one that reads as "what it works on" —
// if's condition, let's body, for's source — so dropping a block onto an
// expression can wrap it there instead of throwing it away.
export function firstEmptySlot(node, nodes) {
  const schema = nodes.get(node?.node);
  if (!schema) return null;
  for (const field of schema.fields) {
    if (field.kind === "expr" && !node[field.name]) return [field.name];
    if (field.kind !== "exprs") continue;
    const index = (node[field.name] || []).findIndex((item) => !item);
    if (index >= 0) return [field.name, index];
  }
  return null;
}

// samePath and isPathPrefix compare canvas paths — arrays of field names and
// indexes that locate a node inside the document.
export function samePath(left, right) {
  return left.length === right.length && left.every((part, index) => part === right[index]);
}

export function isPathPrefix(prefix, path) {
  return prefix.length <= path.length && prefix.every((part, index) => path[index] === part);
}

export function clone(value) {
  return value == null ? value : JSON.parse(JSON.stringify(value));
}

export function indexNodes(list) {
  return new Map((list || []).map((schema) => [schema.node, schema]));
}

function cleanNode(node, nodes) {
  if (!node) return null;
  const schema = nodes.get(node.node);
  if (!schema) throw new Error(`不支持的节点 ${node.node}`);
  return { node: node.node, ...cleanFields(node, schema.fields, nodes) };
}

function cleanFields(target, fields, nodes) {
  const out = {};
  for (const field of fields) {
    const value = cleanField(target?.[field.name], field, nodes);
    if (value !== undefined) out[field.name] = value;
  }
  return out;
}

function cleanField(value, field, nodes) {
  switch (field.kind) {
    case "expr": return value || !field.optional ? cleanNode(value, nodes) : undefined;
    case "exprs": return (value || []).map((item) => cleanNode(item, nodes));
    case "list": return (value || []).map((item) => cleanFields(item, field.fields, nodes));
    case "name":
    case "text": {
      const text = value || field.default || "";
      return text === "" && field.optional ? undefined : text;
    }
    case "int": return Number.isSafeInteger(Number(value)) ? Number(value) : 0;
    case "float": return String(value ?? "0.0");
    case "string": return String(value ?? "");
    case "bool": return Boolean(value);
    default: throw new Error(`不支持的字段类型 ${field.kind}`);
  }
}

export function blankNode(schema) {
  return { node: schema.node, ...blankFields(schema.fields) };
}

export function blankFields(fields) {
  const out = {};
  for (const field of fields) {
    const value = blankField(field);
    if (value !== undefined) out[field.name] = value;
  }
  return out;
}

const LITERAL_DEFAULTS = { int: 0, float: "0.0", string: "", bool: false };

function blankField(field) {
  switch (field.kind) {
    case "expr": return field.optional ? undefined : null;
    case "exprs": return Array(field.min || 0).fill(null);
    case "list": return Array.from({ length: field.min || 0 }, () => blankFields(field.fields));
    case "name":
    case "text": return field.optional && !field.default ? undefined : (field.default || "");
    default: return LITERAL_DEFAULTS[field.kind];
  }
}

export class FunRouteLanguage {
  constructor(catalog) {
    this.catalog = clone(catalog || {});
    this.source = this.catalog.source || {};
    this.nodes = indexNodes(this.catalog.nodes);
    this.version = this.source.expr_json_version;
    if (!Number.isInteger(this.version)) throw new Error("catalog 缺少 source.expr_json_version");
    this.namePattern = this.source.variable_name_pattern || "";
    this.operators = this.source.operators || [];
    this.formNames = new Set((this.catalog.special_forms || []).map((form) => form.name));
    this.keywords = new Set(this.source.keywords || []);
    this._tokenPattern = tokenPattern(this.operators);
    this.controlBlocks = controlBlocksOf(this.catalog);
  }

  document(expr) {
    return { version: this.version, expr: cleanNode(expr, this.nodes) };
  }

  createForm(name) {
    const operator = this.operators.find((item) => item.form === name);
    if (!operator) throw new Error(`目录没有描述派生形式 ${name}`);
    return instantiate(operator.template, {});
  }

  // operatorForm exposes the catalog-defined surface syntax without leaking
  // its expansion details into a UI. A renderer can therefore show `a + b`
  // instead of the underlying add(a,b) tree and still stay in lockstep with
  // the Go parser's precedence, fixity and canonical templates.
  operatorForm(node) {
    const matched = this._operator(node);
    if (!matched) return null;
    const operands = (matched.descriptor.operands || []).map((name) => matched.captures[name]);
    return {
      token: matched.descriptor.token,
      fixity: matched.descriptor.fixity,
      associativity: matched.descriptor.associativity || "",
      precedence: matched.descriptor.precedence,
      form: matched.descriptor.form || "",
      operands: operands.map((item) => item.node),
      paths: operands.map((item) => item.path),
    };
  }

  logicalForm(node) {
    const operator = this.operatorForm(node);
    if (!operator?.form) return null;
    return { kind: operator.form, operands: operator.operands, paths: operator.paths };
  }

  // Returns the only variable names a canvas may offer at path. Contract
  // arguments come from the checked host contract; local names come from the
  // catalog's binds metadata, which is generated from the Go AST tags.
  scopeAtPath(root, path = [], contractArgs = []) {
    const external = (contractArgs || []).filter((arg) => arg?.name).map((arg) => ({
      name: arg.name, type: clone(arg.type), doc: arg.doc || "", source: "contract",
    }));
    const localNames = localsAtPath(root, path, this.nodes);
    const choices = new Map(external.map((choice) => [choice.name, choice]));
    for (const name of localNames) choices.set(name, { name, type: null, doc: "", source: "local" });
    return [...choices.values()];
  }

  // A node is a control block when the catalog says its name is one — except
  // for && || !, which are if underneath but read as operators.
  isControlBlock(node) {
    if (!node || typeof node !== "object") return false;
    const name = node.node === "call" ? node.name : node.node;
    if (!this.controlBlocks.has(name)) return false;
    return node.node !== "call" || (!this.operatorForm(node) && !this.logicalForm(node));
  }

  expressionSource(node, parentPrecedence = 0) {
    if (!node) return "_";
    const operator = this._operator(node);
    if (operator) return this._operatorSource(operator, parentPrecedence);
    switch (node.node) {
      case "var": return node.name || "value";
      case "int": return String(node.int ?? 0);
      case "float": return floatLiteral(node.float);
      case "string": return JSON.stringify(node.string ?? "");
      case "bool": return node.bool ? "true" : "false";
      case "array": return `[${(node.items || []).map((item) => this.expressionSource(item)).join(", ")}]`;
      case "dict": return `{${(node.entries || []).map((entry) => `${JSON.stringify(entry.key || "")}: ${this.expressionSource(entry.value)}`).join(", ")}}`;
      case "enum": return node.enum ? `@${node.enum}.${node.member || "member"}` : `@${node.member || "member"}`;
      case "call": return `${node.name}(${(node.args || []).map((arg) => this.expressionSource(arg)).join(", ")})`;
      case "switch": return this._switchSource(node);
      case "for": return `[${[this.expressionSource(node.yield), ...this._forClauses(node)].join(" ")}]`;
      case "reduce": return `reduce(${this._forHead(node)}, ${this._accumulatorHead(node)}, ${this.expressionSource(node.body)})`;
      case "let": return `let(${[...this._letBindings(node), this.expressionSource(node.body)].join(", ")})`;
      default: return "_";
    }
  }

  // used is what the caller already wrote on this line (a binding name, a case
  // head, a dict key), so a value only stays inline when the whole line fits.
  formatSource(node, indent = "", used = 0) {
    const inline = this.expressionSource(node);
    if (indent.length + used + inline.length < MAX_LINE) return inline;
    const operator = this._operator(node);
    if (operator) return this._chainSource(node, operator.descriptor, indent) ?? inline;
    const split = this._splitNode(node);
    if (!split) return inline;
    const inner = `${indent}  `;
    const parts = split.parts.map((part) => inner + this._formatPart(part, inner));
    return `${split.open}\n${parts.join(split.separator ?? ",\n")}\n${indent}${split.close}`;
  }

  tokenize(text) {
    const tokens = [];
    let index = 0;
    let match;
    this._tokenPattern.lastIndex = 0;
    while ((match = this._tokenPattern.exec(text)) !== null) {
      if (match.index > index) tokens.push({ kind: "punct", text: text.slice(index, match.index) });
      tokens.push(this._classifyToken(match, text));
      index = match.index + match[0].length;
    }
    if (index < text.length) tokens.push({ kind: "punct", text: text.slice(index) });
    return tokens;
  }

  _operator(node) {
    for (const descriptor of this.operators) {
      const captures = matchTemplate(descriptor.template, node);
      if (captures) return { descriptor, captures };
    }
    return null;
  }

  _operatorSource({ descriptor, captures }, parentPrecedence) {
    const values = (descriptor.operands || []).map((name) => captures[name]?.node);
    const precedence = descriptor.precedence;
    if (descriptor.fixity === "prefix") return `${descriptor.token}${this.expressionSource(values[0], precedence)}`;
    const left = this.expressionSource(values[0], precedence);
    const right = this.expressionSource(values[1], precedence + (descriptor.associativity === "left" ? 1 : 0));
    const text = `${left} ${descriptor.token} ${right}`;
    return precedence < parentPrecedence ? `(${text})` : text;
  }

  _switchSource(node) {
    const head = node.value ? `${this.expressionSource(node.value)}, ` : "";
    const branches = (node.cases || []).map((item) => {
      const matches = (item.match || []).map((match) => this.expressionSource(match)).join(", ");
      return `case ${matches} => ${this.expressionSource(item.result)}`;
    });
    const fallback = node.default ? `, else ${this.expressionSource(node.default)}` : "";
    return `switch(${head}${branches.join(", ")}${fallback})`;
  }

  _forClauses(node) {
    const clauses = [`for ${loopVariables(node)} in ${this.expressionSource(node.source)}`];
    if (node.where) clauses.push(`if ${this.expressionSource(node.where)}`);
    return clauses;
  }

  _forHead(node) {
    const head = `${loopVariables(node)} in ${this.expressionSource(node.source)}`;
    return node.where ? `${head} if ${this.expressionSource(node.where)}` : head;
  }

  _accumulatorHead(node) { return `${node.accumulator || "acc"} = ${this.expressionSource(node.init)}`; }
  _letBindings(node) { return (node.bindings || []).map((item) => `${item.name} = ${this.expressionSource(item.value)}`); }

  _splitNode(node) {
    switch (node?.node) {
      case "call": return { open: `${node.name}(`, parts: node.args || [], close: ")" };
      case "switch": return { open: node.value ? `switch(${this.expressionSource(node.value)},` : "switch(", parts: switchParts(node), close: ")" };
      case "for": return { open: "[", parts: [node.yield, ...this._forClauses(node).map(literalPart)], close: "]", separator: "\n" };
      case "reduce": return { open: "reduce(", parts: [literalPart(this._forHead(node)), literalPart(this._accumulatorHead(node)), node.body], close: ")" };
      case "let": return { open: "let(", parts: [...(node.bindings || []).map(bindingPart), node.body], close: ")" };
      case "array": return { open: "[", parts: node.items || [], close: "]" };
      case "dict": return { open: "{", parts: (node.entries || []).map((entry) => ({ key: entry.key || "", value: entry.value })), close: "}" };
      default: return null;
    }
  }

  _formatPart(part, indent) {
    if (part?.literal !== undefined) return part.literal;
    if (part?.binding) return this._headedPart(`${part.binding.name || "value"} = `, part.binding.value, indent);
    if (part?.branch) {
      const matches = (part.branch.match || []).map((item) => this.formatSource(item, indent)).join(", ");
      return this._headedPart(`case ${matches} => `, part.branch.result, indent);
    }
    if (part?.fallback !== undefined) return this._headedPart("else ", part.fallback, indent);
    if (part?.key !== undefined) return this._headedPart(`${JSON.stringify(part.key)}: `, part.value, indent);
    return this.formatSource(part, indent);
  }

  _headedPart(head, node, indent) { return head + this.formatSource(node, indent, head.length); }

  // A long infix chain breaks before each operator instead of running off the
  // line; the continuation lines are indented and parse back to the same nodes.
  _chainSource(node, descriptor, indent) {
    if (descriptor.fixity !== "infix") return null;
    const parts = this._chainParts(node, descriptor, 0);
    return parts.length > 1 ? parts.join(`\n${indent}  ${descriptor.token} `) : null;
  }

  _chainParts(node, descriptor, parentPrecedence) {
    const match = this._operator(node);
    if (match?.descriptor.token !== descriptor.token) return [this.expressionSource(node, parentPrecedence)];
    const [left, right] = (descriptor.operands || []).map((name) => match.captures[name]?.node);
    const precedence = descriptor.precedence;
    return [
      ...this._chainParts(left, descriptor, precedence),
      this.expressionSource(right, precedence + (descriptor.associativity === "left" ? 1 : 0)),
    ];
  }

  _classifyToken(match, text) {
    const [raw, comment, string, number, member, identifier, operator] = match;
    if (comment !== undefined) return { kind: "comment", text: raw };
    if (string !== undefined) return { kind: "string", text: raw };
    if (number !== undefined) return { kind: "number", text: raw };
    if (member !== undefined) return { kind: "enum", text: raw };
    if (operator !== undefined) return { kind: "op", text: raw };
    if (identifier === undefined) return { kind: "punct", text: raw };
    if (identifier === "true" || identifier === "false") return { kind: "bool", text: raw };
    const next = text.slice(match.index + raw.length).trimStart()[0];
    if (identifier === "if") return { kind: next === "(" ? "fn" : "keyword", text: raw };
    if (this.keywords.has(identifier)) return { kind: "keyword", text: raw };
    if (next !== "(") return { kind: "var", text: raw };
    return { kind: this.formNames.has(identifier) ? "form" : "fn", text: raw };
  }
}

function localsAtPath(root, path, nodes) {
  const locals = [];
  let current = root;
  let fields = nodes.get(current?.node)?.fields || [];
  let offset = 0;
  while (current && offset < path.length) {
    const fieldName = path[offset];
    if (typeof fieldName !== "string") break;
    locals.push(...bindingsForTarget(current, fields, fieldName));
    const field = fields.find((candidate) => candidate.name === fieldName);
    if (!field) break;
    if (field.kind === "expr") {
      current = current[fieldName];
      offset += 1;
    } else if (field.kind === "exprs") {
      current = current[fieldName]?.[path[offset + 1]];
      offset += 2;
    } else if (field.kind === "list") {
      const index = path[offset + 1];
      if (!Number.isInteger(index)) break;
      const items = current[fieldName] || [];
      for (let prior = 0; prior < index; prior += 1) {
        locals.push(...bindingsForTarget(items[prior], field.fields || [], "@rest"));
      }
      const item = items[index];
      offset += 2;
      if (!item || offset >= path.length) break;
      const itemFieldName = path[offset];
      const itemField = (field.fields || []).find((candidate) => candidate.name === itemFieldName);
      if (!itemField) break;
      if (itemField.kind === "expr") current = item[itemFieldName];
      else if (itemField.kind === "exprs") {
        current = item[itemFieldName]?.[path[offset + 1]];
        offset += 1;
      } else break;
      offset += 1;
    } else break;
    fields = nodes.get(current?.node)?.fields || [];
  }
  return locals.filter(Boolean);
}

function bindingsForTarget(target, fields, targetName) {
  const names = [];
  for (const field of fields || []) {
    if (field.kind === "name" && (field.binds || []).includes(targetName)) {
      const name = target?.[field.name];
      if (name) names.push(name);
    }
    if (field.kind === "list") {
      for (const item of target?.[field.name] || []) {
        names.push(...bindingsForTarget(item, field.fields || [], targetName));
      }
    }
  }
  return names;
}

function tokenPattern(operators) {
  const spellings = [...new Set(["=>", "=", ...(operators || []).map((item) => item.token)])]
    .sort((left, right) => right.length - left.length).map(escapeRegExp).join("|");
  return new RegExp(`(//[^\\n]*)|("(?:[^"\\\\]|\\\\.)*")|(\\d[\\d_]*(?:\\.\\d[\\d_]*)?(?:[eE][+\\-]?\\d[\\d_]*)?)|(@[A-Za-z_][A-Za-z0-9_]*(?:\\.[A-Za-z_][A-Za-z0-9_]*)?)|([A-Za-z_][A-Za-z0-9_.]*)|(${spellings})|([(),:\\[\\]{}])|(\\s+)`, "g");
}

function escapeRegExp(text) { return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"); }

function matchTemplate(template, node, captures = {}, path = []) {
  if (template?.$) {
    const prior = captures[template.$];
    if (prior && JSON.stringify(prior.node) !== JSON.stringify(node)) return null;
    captures[template.$] = { node, path };
    return captures;
  }
  if (!template || !node || typeof template !== "object" || typeof node !== "object") return null;
  for (const [key, expected] of Object.entries(template)) {
    if (Array.isArray(expected)) {
      if (!Array.isArray(node[key]) || node[key].length !== expected.length) return null;
      for (let i = 0; i < expected.length; i += 1) {
        if (!matchTemplate(expected[i], node[key][i], captures, [...path, key, i])) return null;
      }
    } else if (expected && typeof expected === "object") {
      if (!matchTemplate(expected, node[key], captures, [...path, key])) return null;
    } else if (node[key] !== expected) return null;
  }
  return captures;
}

function instantiate(template, values) {
  if (template?.$) return clone(values[template.$] ?? null);
  if (Array.isArray(template)) return template.map((item) => instantiate(item, values));
  if (!template || typeof template !== "object") return template;
  return Object.fromEntries(Object.entries(template).map(([key, value]) => [key, instantiate(value, values)]));
}

function floatLiteral(text) {
  const value = String(text ?? "0.0");
  return /[.eE]/.test(value) ? value : `${value}.0`;
}

function loopVariables(node) {
  const value = node.variable || "item";
  return node.key_variable ? `${node.key_variable}, ${value}` : value;
}

// A line wider than this is split into parts, one per line. The budget counts
// the indent, so a binding or branch nested three levels deep still fits.
const MAX_LINE = 72;

function literalPart(literal) { return { literal }; }

function bindingPart(binding) { return { binding }; }

function switchParts(node) {
  const parts = [...(node.cases || []).map((branch) => ({ branch }))];
  if (node.default) parts.push({ fallback: node.default });
  return parts;
}

export function emptyContract() { return { args: [], result: null }; }

export function isEmptyContract(contract) {
  return !contract || (!contract.args?.length && !contract.result?.type && !contract.result?.doc);
}

export function contractPayload(contract) {
  if (isEmptyContract(contract)) return null;
  const args = (contract.args || []).map((arg) => ({
    name: String(arg.name || "").trim(), type: String(arg.type || "").trim(), doc: arg.doc || undefined,
  }));
  const result = contract.result ? {
    type: String(contract.result.type || "").trim(), doc: contract.result.doc || undefined,
  } : undefined;
  return { args, result };
}

export function contractComments(contract, source) {
  const args = contract?.args || [];
  const result = contract?.result;
  if (!args.length && !result) return source;
  let width = result ? 2 : 0;
  for (const arg of args) width = Math.max(width, arg.name.length + 1);
  const lines = args.map((arg) => commentLine(`${arg.name}:`, width, arg.type, arg.doc));
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
    if (!response.ok) throw new Error(body.error?.message || `HTTP ${response.status}`);
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
