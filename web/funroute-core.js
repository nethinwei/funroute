// FunRoute's headless browser SDK. This module has no DOM dependency: an app
// may use it with React, Vue, a terminal UI, or no UI at all.

export const VALUE_TEMPLATES = [
  { id: "value:var", node: "var", label: "参数 / 变量", description: "只能选择运行契约入参或当前作用域的本地变量", icon: "𝑥", color: "#475569" },
  { id: "value:int", node: "int", label: "整数", description: "int64 立即值", icon: "1", color: "#2563EB" },
  { id: "value:float", node: "float", label: "浮点数", description: "有限 float64 立即值", icon: ".", color: "#0891B2" },
  { id: "value:string", node: "string", label: "字符串", description: "UTF-8 立即值", icon: "”", color: "#059669" },
  { id: "value:bool", node: "bool", label: "布尔值", description: "true / false", icon: "?", color: "#0EA5E9" },
  { id: "value:enum", node: "enum", label: "枚举成员", description: "宿主契约声明的命名闭集的成员，写作 @成员", icon: "@", color: "#C2410C" },
  { id: "value:array", node: "array", label: "数组", description: "元素必须同型", icon: "[ ]", color: "#D97706" },
  { id: "value:dict", node: "dict", label: "字典", description: "string key、value 同型", icon: "{ }", color: "#EA580C" },
];

// FIELD_TEXT is the console's wording for the fields a node schema describes.
// The field list itself always comes from the catalog, never from here.
export const FIELD_TEXT = {
  "enum.enum": ["枚举名", "同名成员属于多个枚举时才需要填"],
  "enum.member": ["成员", "契约里声明的枚举成员"],
  "array.items": ["元素", "元素必须同型"],
  "dict.entries": ["键值", "value 同型"],
  "dict.entries.key": ["键", "string"],
  "dict.entries.value": ["值", "与其他值同型"],
  "switch.value": ["待匹配值", "留空则每个分支是 bool 条件"],
  "switch.cases": ["分支", "任一匹配即选中"],
  "switch.cases.match": ["匹配值", "与待匹配值同类型；无待匹配值时为 bool"],
  "switch.cases.result": ["返回结果", "所有结果同类型"],
  "switch.default": ["默认结果（else）", "未匹配时返回"],
  "for.source": ["输入", "array<T> 或 dict<T>"],
  "for.variable": ["元素局部名", "仅本节点可见"],
  "for.key_variable": ["键局部名", "填写即遍历字典"],
  "for.where": ["筛选条件", "bool；留空表示全部"],
  "for.yield": ["产出表达式", "每个保留元素产出一个值"],
  "reduce.source": ["输入", "array<T> 或 dict<T>"],
  "reduce.variable": ["元素局部名", "仅本节点可见"],
  "reduce.key_variable": ["键局部名", "填写即遍历字典"],
  "reduce.accumulator": ["累加器局部名", "仅本节点可见"],
  "reduce.init": ["初始值", "累加器初值 R"],
  "reduce.body": ["累加表达式", "必须返回 R"],
  "let.bindings": ["绑定", "后续绑定与主体可引用"],
  "let.bindings.name": ["名称", "仅本节点可见"],
  "let.bindings.value": ["值", ""],
  "let.body": ["主体", "整体结果"],
};

export function fieldText(key, field) {
  const [label, hint] = FIELD_TEXT[key] || [field.name, ""];
  return { label: field.optional ? `${label}（可空）` : label, hint };
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
  visit(contract?.result?.type);
  return [...found].map(([name, values]) => ({ name, values }));
}

export function clone(value) {
  return value == null ? value : JSON.parse(JSON.stringify(value));
}

export function indexNodes(list) {
  return new Map((list || []).map((schema) => [schema.node, schema]));
}

export function cleanNode(node, nodes) {
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

  _forHead(node) { return `${loopVariables(node)} in ${this.expressionSource(node.source)}`; }
  _accumulatorHead(node) { return `${node.accumulator || "acc"} from ${this.expressionSource(node.init)}`; }
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

export function typeName(type) {
  if (!type) return "unknown";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeName(type.elem)}>`;
  if (type.kind === "handle") return `handle<${type.name}>`;
  if (type.kind === "enum") return `enum<${type.name}>{${(type.values || []).join(",")}}`;
  return type.name || type.kind || "unknown";
}

// typeSummary is typeName for display: a large enum shows a few members and a
// count instead of all of them. Never send this to the server — the contract
// carries the full type text, which ParseType has to be able to read back.
export function typeSummary(type, limit = 6) {
  if (!type) return "unknown";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeSummary(type.elem, limit)}>`;
  const values = type.values || [];
  if (type.kind !== "enum" || values.length <= limit) return typeName(type);
  return `enum<${type.name}>{${values.slice(0, limit).join(",")}… 共 ${values.length} 个}`;
}

export function equalType(left, right) {
  if (!left || !right || left.kind !== right.kind || left.name !== right.name) return false;
  if (left.kind === "array" || left.kind === "dict") return equalType(left.elem, right.elem);
  if (left.kind === "enum") return JSON.stringify(left.values || []) === JSON.stringify(right.values || []);
  return true;
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

export function parseInputValue(raw, type) {
  if (!type) throw new Error("缺少参数类型");
  if (type.kind === "string") return raw;
  const text = raw.trim();
  switch (type.kind) {
    case "enum":
      if (!(type.values || []).includes(raw)) throw new Error(`值必须是 ${typeSummary(type)} 的成员`);
      return raw;
    case "bool":
      if (text !== "true" && text !== "false") throw new Error("布尔值必须是 true 或 false");
      return text === "true";
    case "int": {
      if (!/^-?(0|[1-9]\d*)$/.test(text)) throw new Error(`“${raw}”不是整数`);
      const value = Number(text);
      if (!Number.isSafeInteger(value)) throw new Error("整数超出浏览器可安全表示范围");
      return value;
    }
    case "float": {
      if (!/^-?(?:\d+(?:\.\d+)?|\d+\.?\d*[eE][+\-]?\d+)$/.test(text)) throw new Error(`“${raw}”不是浮点数`);
      const value = Number(text);
      if (!Number.isFinite(value)) throw new Error("浮点数必须是有限值");
      return value;
    }
    case "array":
    case "dict": {
      const value = JSON.parse(text);
      validateTypedJSON(value, type, "$参数");
      return value;
    }
    default: throw new Error(`无法从文本输入 ${typeName(type)}`);
  }
}

function validateTypedJSON(value, type, path) {
  switch (type.kind) {
    case "bool": if (typeof value !== "boolean") throw new Error(`${path} 必须是 bool`); break;
    case "int": if (!Number.isSafeInteger(value)) throw new Error(`${path} 必须是安全整数`); break;
    case "float": if (typeof value !== "number" || !Number.isFinite(value)) throw new Error(`${path} 必须是有限 float`); break;
    case "string": if (typeof value !== "string") throw new Error(`${path} 必须是 string`); break;
    case "enum":
      if (typeof value !== "string" || !(type.values || []).includes(value)) throw new Error(`${path} 必须是 ${typeSummary(type)} 的成员`);
      break;
    case "array":
      if (!Array.isArray(value)) throw new Error(`${path} 必须是数组`);
      value.forEach((item, index) => validateTypedJSON(item, type.elem, `${path}[${index}]`));
      break;
    case "dict":
      if (!value || Array.isArray(value) || typeof value !== "object") throw new Error(`${path} 必须是对象`);
      Object.entries(value).forEach(([key, item]) => validateTypedJSON(item, type.elem, `${path}.${key}`));
      break;
    default: throw new Error(`${path} 的类型 ${typeName(type)} 不支持文本输入`);
  }
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
