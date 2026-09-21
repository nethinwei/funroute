// funroute-source.js turns canonical ExprJSON back into source text and splits
// source text into tokens. It touches no DOM, so the designer, a host console
// and a syntax highlighter can all share it.

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

// forHead prints "item in source where condition", the keyword form the parser
// accepts; accumulatorHead prints "acc from init".
function forHead(node) {
  const head = `${node.variable || "item"} in ${expressionSource(node.source)}`;
  return node.where ? `${head} where ${expressionSource(node.where)}` : head;
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
    case "float": return String(node.float ?? "0.0");
    case "string": return JSON.stringify(node.string ?? "");
    case "bool": return node.bool ? "true" : "false";
    case "array": return `[${(node.items || []).map(expressionSource).join(",")}]`;
    case "dict": return `{${(node.entries || []).map((entry) => `${JSON.stringify(entry.key || "")}:${expressionSource(entry.value)}`).join(",")}}`;
    case "call": return `${node.name}(${(node.args || []).map(expressionSource).join(",")})`;
    case "switch": {
      const head = node.value ? `${expressionSource(node.value)}, ` : "";
      const branches = (node.cases || []).map(branchSource);
      return `switch(${head}${branches.join(", ")}, else ${expressionSource(node.default)})`;
    }
    case "for": return `for(${forHead(node)}, ${expressionSource(node.yield)})`;
    case "reduce":
      return `reduce(${forHead(node)}, ${accumulatorHead(node)}, ${expressionSource(node.body)})`;
    case "prog": return `prog(${(node.steps || []).map(expressionSource).join(",")})`;
    default: return "_";
  }
}

// One token pattern for the whole language: string, number, identifier,
// punctuation, whitespace. It mirrors the Go lexer, which has no infix
// operators, so a leading "-" is always part of a number literal.
const TOKEN_PATTERN = /(\/\/[^\n]*)|("(?:[^"\\]|\\.)*")|(\d[\d_]*(?:\.\d[\d_]*)?)|([A-Za-z_][A-Za-z0-9_.]*(?:@\d+)?)|(=>|<=|>=|==|!=|&&|\|\||[+\-*/<>!])|([(),:[\]{}])|(\s+)/g;

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
const KEYWORDS = new Set(["case", "else", "in", "where", "from"]);

function classifyToken(match, text, formNames) {
  const [raw, comment, string, number, identifier, operator] = match;
  if (comment !== undefined) return { kind: "comment", text: raw };
  if (string !== undefined) return { kind: "string", text: raw };
  if (number !== undefined) return { kind: "number", text: raw };
  if (operator !== undefined) return { kind: "op", text: raw };
  if (identifier === undefined) return { kind: "punct", text: raw };
  if (identifier === "true" || identifier === "false") return { kind: "bool", text: raw };
  if (KEYWORDS.has(identifier)) return { kind: "keyword", text: raw };
  const next = text.slice(match.index + raw.length).trimStart()[0];
  if (next !== "(") return { kind: "var", text: raw };
  return { kind: formNames.has(identifier) ? "form" : "fn", text: raw };
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
  return `${split.open}\n${parts.join(",\n")}\n${indent}${split.close}`;
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
    case "for": return { open: "for(", parts: forParts(node), close: ")" };
    case "reduce": return { open: "reduce(", parts: reduceParts(node), close: ")" };
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

function forParts(node) {
  return [{ literal: forHead(node) }, node.yield];
}

function reduceParts(node) {
  return [{ literal: forHead(node) }, { literal: accumulatorHead(node) }, node.body];
}

