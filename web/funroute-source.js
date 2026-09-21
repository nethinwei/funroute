// funroute-source.js turns canonical ExprJSON back into source text and splits
// source text into tokens. It touches no DOM, so the designer, a host console
// and a syntax highlighter can all share it.

export const VALUE_TEMPLATES = [
  { id: "value:var", node: "var", label: "参数", description: "自动成为 args 参数", icon: "𝑥", color: "#475569" },
  { id: "value:int", node: "int", label: "整数", description: "int64 立即值", icon: "1", color: "#2563EB" },
  { id: "value:float", node: "float", label: "浮点数", description: "有限 float64 立即值", icon: ".", color: "#0891B2" },
  { id: "value:string", node: "string", label: "字符串", description: "UTF-8 立即值", icon: "”", color: "#059669" },
  { id: "value:bool", node: "bool", label: "布尔值", description: "true / false", icon: "?", color: "#0EA5E9" },
  { id: "value:array", node: "array", label: "数组", description: "元素必须同型", icon: "[ ]", color: "#D97706" },
  { id: "value:dict", node: "dict", label: "字典", description: "string key、value 同型", icon: "{ }", color: "#EA580C" },
];


export function clone(value) {
  return value == null ? value : JSON.parse(JSON.stringify(value));
}

// indexNodes turns the catalog's node list into a lookup by node tag. The
// schemas come from the compiler's own node definitions, so what the canvas
// builds is what the compiler accepts.
export function indexNodes(list) {
  const nodes = new Map();
  for (const schema of list || []) nodes.set(schema.node, schema);
  return nodes;
}

// cleanNode normalises a node tree into canonical ExprJSON, field by field as
// the schema says: an absent optional field is omitted so the JSON matches what
// the Go side exports, and a missing name falls back to the schema's default.
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
      const name = value || field.default || "";
      return name === "" && field.optional ? undefined : name;
    }
    case "int": return Number.isFinite(Number(value)) ? Math.trunc(Number(value)) : 0;
    case "float": return String(value ?? "0.0");
    case "string": return String(value ?? "");
    case "bool": return Boolean(value);
    default: throw new Error(`不支持的字段类型 ${field.kind}`);
  }
}

// blankNode is a fresh node for the canvas: every slot empty, every name at its
// default, every list at its minimum length.
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

function typeName(type) {
  if (!type) return "动态";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeName(type.elem)}>`;
  if (type.kind === "handle") return `handle<${type.name}>`;
  return type.name || type.kind || "动态";
}

// Printing mirrors the parser's sugar: add(a,b) comes back as a + b, and the
// if-patterns the parser generates for &&, ||, != and ! are recognised again,
// so source -> nodes -> source round-trips to what the user typed.
const INFIX = { add: "+", sub: "-", mul: "*", div: "/", eq: "==", lt: "<", le: "<=", gt: ">", ge: ">=" };
const PRECEDENCE = {
  "||": 1, "&&": 2,
  "==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3,
  "+": 4, "-": 4, "*": 5, "/": 5,
};

const isTrue = (node) => node?.node === "bool" && node.bool === true;
const isFalse = (node) => node?.node === "bool" && node.bool === false;

// logicalForm recognises the derived forms in an if node and reports which
// argument slots are the real operands, so a host can render them as and/or/not
// cards instead of a bare if.
export function logicalForm(node) {
  if (node?.node !== "call" || node.name !== "if" || (node.args || []).length !== 3) return null;
  const [condition, whenTrue, whenFalse] = node.args;
  if (isFalse(whenTrue) && isTrue(whenFalse)) return { kind: "not", slots: [0] };
  if (isFalse(whenFalse) && !isTrue(whenTrue)) return { kind: "and", slots: [0, 1] };
  if (isTrue(whenTrue) && !isFalse(whenFalse)) return { kind: "or", slots: [0, 2] };
  return null;
}

function infixOf(node) {
  if (node?.node !== "call") return null;
  const args = node.args || [];
  if (INFIX[node.name] && args.length === 2) {
    return { op: INFIX[node.name], left: args[0], right: args[1] };
  }
  if (node.name === "if" && args.length === 3) return sugarFromIf(args);
  return null;
}

function sugarFromIf([condition, whenTrue, whenFalse]) {
  if (isFalse(whenTrue) && isTrue(whenFalse)) {
    const negated = condition?.node === "call" && condition.name === "eq" && (condition.args || []).length === 2;
    if (negated) return { op: "!=", left: condition.args[0], right: condition.args[1] };
    return { prefix: "!", operand: condition };
  }
  if (isFalse(whenFalse) && !isTrue(whenTrue)) return { op: "&&", left: condition, right: whenTrue };
  if (isTrue(whenTrue) && !isFalse(whenFalse)) return { op: "||", left: condition, right: whenFalse };
  return null;
}

function infixSource(infix, parentPrecedence) {
  if (infix.prefix) return `!${expressionSource(infix.operand, 6)}`;
  const precedence = PRECEDENCE[infix.op];
  const text = `${expressionSource(infix.left, precedence)} ${infix.op} ${expressionSource(infix.right, precedence + 1)}`;
  return precedence < parentPrecedence ? `(${text})` : text;
}

// branchSource prints "case m1, m2 => result".
function branchSource(item) {
  const matches = (item.match || []).map((match) => expressionSource(match)).join(", ");
  return `case ${matches} => ${expressionSource(item.result)}`;
}

// comprehensionSource prints [yield for item in source if condition].
function comprehensionSource(node) {
  const clauses = [expressionSource(node.yield), ...comprehensionClauses(node)];
  return `[${clauses.join(" ")}]`;
}

// comprehensionClauses returns the "for ... in ..." and "if ..." clauses, which
// the formatter puts on their own lines.
function comprehensionClauses(node) {
  const clauses = [`for ${loopVariables(node)} in ${expressionSource(node.source)}`];
  if (node.where) clauses.push(`if ${expressionSource(node.where)}`);
  return clauses;
}

function forHead(node) {
  return `${loopVariables(node)} in ${expressionSource(node.source)}`;
}

// floatLiteral keeps a float looking like one: ExprJSON stores 1.0 as "1", and
// printing that back would re-parse as an int and change the program's type.
function floatLiteral(text) {
  const value = String(text ?? "0.0");
  return /[.eE]/.test(value) ? value : `${value}.0`;
}

// letBindings prints "name = value" for each binding, in order.
function letBindings(node) {
  return (node.bindings || []).map((binding) => `${binding.name} = ${expressionSource(binding.value)}`);
}

// Two variables mean a dictionary walk, with the key first.
function loopVariables(node) {
  const value = node.variable || "item";
  return node.key_variable ? `${node.key_variable}, ${value}` : value;
}

function accumulatorHead(node) {
  return `${node.accumulator || "acc"} from ${expressionSource(node.init)}`;
}

// expressionSource prints one node as a single-line expression. It is exported
// so a host can build a formatter (or a copy button) on top of it.
export function expressionSource(node, parentPrecedence = 0) {
  if (!node) return "_";
  const infix = infixOf(node);
  if (infix) return infixSource(infix, parentPrecedence);
  switch (node.node) {
    case "var": return node.name || "value";
    case "int": return String(node.int ?? 0);
    case "float": return floatLiteral(node.float);
    case "string": return JSON.stringify(node.string ?? "");
    case "bool": return node.bool ? "true" : "false";
    // The callbacks take one argument on purpose: passing expressionSource
    // directly would feed the array index in as parentPrecedence.
    case "array": return `[${(node.items || []).map((item) => expressionSource(item)).join(", ")}]`;
    case "dict": return `{${(node.entries || []).map((entry) => `${JSON.stringify(entry.key || "")}: ${expressionSource(entry.value)}`).join(", ")}}`;
    case "call": return `${node.name}(${(node.args || []).map((arg) => expressionSource(arg)).join(", ")})`;
    case "switch": {
      const head = node.value ? `${expressionSource(node.value)}, ` : "";
      const branches = (node.cases || []).map(branchSource);
      return `switch(${head}${branches.join(", ")}, else ${expressionSource(node.default)})`;
    }
    case "for": return comprehensionSource(node);
    case "reduce":
      return `reduce(${forHead(node)}, ${accumulatorHead(node)}, ${expressionSource(node.body)})`;
    case "let":
      return `let(${letBindings(node).join(", ")}, ${expressionSource(node.body)})`;
    default: return "_";
  }
}

// One token pattern for the whole language: string, number, identifier,
// punctuation, whitespace. It mirrors the Go lexer, which has no infix
// operators, so a leading "-" is always part of a number literal.
const TOKEN_PATTERN = /(\/\/[^\n]*)|("(?:[^"\\]|\\.)*")|(\d[\d_]*(?:\.\d[\d_]*)?)|([A-Za-z_][A-Za-z0-9_.]*)|(=>|<=|>=|==|!=|&&|\|\||[+\-*/<>!=])|([(),:[\]{}])|(\s+)/g;

export function tokenize(text, formNames = new Set()) {
  const tokens = [];
  let index = 0;
  let match;
  TOKEN_PATTERN.lastIndex = 0;
  while ((match = TOKEN_PATTERN.exec(text)) !== null) {
    if (match.index > index) tokens.push({ kind: "punct", text: text.slice(index, match.index) });
    tokens.push(classifyToken(match, text, formNames));
    index = match.index + match[0].length;
  }
  if (index < text.length) tokens.push({ kind: "punct", text: text.slice(index) });
  return tokens;
}

// Contextual keywords: they are ordinary identifiers to the lexer, but they
// read as syntax, so they get their own colour.
const KEYWORDS = new Set(["case", "else", "in", "from", "for", "let"]);

function classifyToken(match, text, formNames) {
  const [raw, comment, string, number, identifier, operator] = match;
  if (comment !== undefined) return { kind: "comment", text: raw };
  if (string !== undefined) return { kind: "string", text: raw };
  if (number !== undefined) return { kind: "number", text: raw };
  if (operator !== undefined) return { kind: "op", text: raw };
  if (identifier === undefined) return { kind: "punct", text: raw };
  if (identifier === "true" || identifier === "false") return { kind: "bool", text: raw };
  const next = text.slice(match.index + raw.length).trimStart()[0];
  // if is both a kernel function and the comprehension's filter keyword.
  if (identifier === "if") return { kind: next === "(" ? "fn" : "keyword", text: raw };
  if (KEYWORDS.has(identifier)) return { kind: "keyword", text: raw };
  if (next !== "(") return { kind: "var", text: raw };
  return { kind: formNames.has(identifier) ? "form" : "fn", text: raw };
}

// contractComments renders the host's contract as comments above the
// expression, matching what RenderWithContract produces on the Go side. The
// comments are not syntax: the text parses to the same program with or without
// them, and they are not carried back on a re-parse.
export function contractComments(contract, source) {
  const args = contract?.args || [];
  const result = contract?.result;
  if (!args.length && !result) return source;
  let width = 0;
  for (const arg of args) width = Math.max(width, arg.name.length + 1);
  if (result) width = Math.max(width, 2);
  const lines = args.map((arg) => commentLine(`${arg.name}:`, width, arg.type, arg.doc));
  if (result) lines.push(commentLine("→", width, result.type, result.doc));
  return `${lines.join("\n")}\n\n${source}`;
}

function commentLine(label, width, typeName, doc) {
  const head = `// ${label.padEnd(width)} ${typeName}`;
  return doc ? `${head.padEnd(28)} ${doc}` : head;
}

// Formatting keeps short expressions on one line and breaks long ones one
// argument per line, so the text stays readable next to the node tree. The
// threshold is deliberately small: a call with a nested call in it is already
// easier to read broken up.
const MAX_INLINE = 40;

export function formatSource(node, indent = "") {
  const inline = expressionSource(node);
  if (inline.length <= MAX_INLINE) return inline;
  const split = splitNode(node);
  if (!split) return inline;
  const inner = `${indent}  `;
  const parts = split.parts.map((part) => inner + formatPart(part, inner));
  const separator = split.separator === "" ? "\n" : ",\n";
  return `${split.open}\n${parts.join(separator)}\n${indent}${split.close}`;
}

function formatPart(part, indent) {
  if (part && part.literal !== undefined) return part.literal;
  if (part && part.branch) {
    const matches = (part.branch.match || []).map((match) => formatSource(match, indent)).join(", ");
    return `case ${matches} => ${formatSource(part.branch.result, indent)}`;
  }
  if (part && part.fallback !== undefined) return `else ${formatSource(part.fallback, indent)}`;
  if (part && part.key !== undefined) return `${JSON.stringify(part.key)}: ${formatSource(part.value, indent)}`;
  return formatSource(part, indent);
}

function splitNode(node) {
  switch (node?.node) {
    case "call": return { open: `${node.name}(`, parts: node.args || [], close: ")" };
    case "switch": return {
      open: node.value ? `switch(${expressionSource(node.value)},` : "switch(",
      parts: switchParts(node),
      close: ")",
    };
    case "for": return { open: "[", parts: forParts(node), close: "]", separator: "" };
    case "reduce": return { open: "reduce(", parts: reduceParts(node), close: ")" };
    case "let": return {
      open: "let(",
      parts: [...letBindings(node).map((binding) => ({ literal: binding })), node.body],
      close: ")",
    };
    case "array": return { open: "[", parts: node.items || [], close: "]" };
    case "dict": return {
      open: "{",
      parts: (node.entries || []).map((entry) => ({ key: entry.key || "", value: entry.value })),
      close: "}",
    };
    default: return null;
  }
}

// A switch breaks one branch per line; the subject rides on the opening line.
function switchParts(node) {
  const parts = (node.cases || []).map((item) => ({ branch: item }));
  parts.push({ fallback: node.default });
  return parts;
}

// A comprehension breaks one clause per line, like PEP 8 suggests.
function forParts(node) {
  return [node.yield, ...comprehensionClauses(node).map((clause) => ({ literal: clause }))];
}

function reduceParts(node) {
  return [{ literal: forHead(node) }, { literal: accumulatorHead(node) }, node.body];
}

