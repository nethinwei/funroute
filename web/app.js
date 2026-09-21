import "./funroute-designer.js";
import { formatSource, tokenize } from "./funroute-source.js";

const designer = document.querySelector("#designer");
const status = document.querySelector("#status");
const source = document.querySelector("#source");
const argsRoot = document.querySelector("#args");
const resultRoot = document.querySelector("#result");
const expression = document.querySelector("#expression");
const highlight = document.querySelector("#expression-highlight");
let compiled = null;
let formNames = new Set();

const routeTemplate = {
  version: 2,
  expr: {
    node: "call", name: "if", args: [
      { node: "call", name: "route.is_healthy@1", args: [{ node: "var", name: "health" }] },
      { node: "string", string: "adyen_primary" },
      { node: "string", string: "stripe_backup" },
    ],
  },
};

const switchTemplate = {
  version: 2,
  expr: {
    node: "switch",
    value: { node: "var", name: "country" },
    cases: [
      { match: [{ node: "string", string: "SG" }], result: { node: "string", string: "adyen_sg" } },
      { match: [{ node: "string", string: "MY" }, { node: "string", string: "TH" }], result: { node: "string", string: "adyen_asia" } },
    ],
    default: { node: "string", string: "stripe_global" },
  },
};

const forTemplate = {
  version: 2,
  expr: {
    node: "for",
    source: { node: "var", name: "channels" },
    variable: "channel",
    where: { node: "call", name: "route.is_healthy@1", args: [{ node: "var", name: "channel" }] },
    yield: { node: "var", name: "channel" },
  },
};

const reduceTemplate = {
  version: 2,
  expr: {
    node: "reduce",
    source: { node: "var", name: "prices" },
    variable: "price",
    accumulator: "total",
    init: { node: "int", int: 0 },
    body: { node: "call", name: "add", args: [{ node: "var", name: "total" }, { node: "var", name: "price" }] },
  },
};

// recurTemplate uses recur, the Turing-complete form this demo registry enables.
const recurTemplate = {
  version: 2,
  expr: {
    node: "call", name: "if", args: [
      { node: "call", name: "eq", args: [{ node: "var", name: "n" }, { node: "int", int: 0 }] },
      { node: "var", name: "acc" },
      {
        node: "call", name: "recur", args: [
          { node: "call", name: "sub", args: [{ node: "var", name: "n" }, { node: "int", int: 1 }] },
          { node: "call", name: "add", args: [{ node: "var", name: "acc" }, { node: "var", name: "n" }] },
        ],
      },
    ],
  },
};

// A subjectless switch is the condition chain that replaces nested ifs.
const condTemplate = {
  version: 2,
  expr: {
    node: "switch",
    cases: [
      {
        match: [{ node: "call", name: "gt", args: [{ node: "var", name: "amount" }, { node: "int", int: 10000 }] }],
        result: { node: "string", string: "manual_review" },
      },
      {
        match: [{ node: "call", name: "gt", args: [{ node: "var", name: "risk" }, { node: "float", float: "0.8" }] }],
        result: { node: "string", string: "reject" },
      },
    ],
    default: { node: "string", string: "auto" },
  },
};

async function request(path, payload) {
  const response = await fetch(path, {
    method: payload ? "POST" : "GET",
    headers: payload ? { "Content-Type": "application/json" } : {},
    body: payload ? JSON.stringify(payload) : undefined,
  });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error?.message || `HTTP ${response.status}`);
  return body;
}

function setStatus(message, kind = "") {
  status.textContent = message;
  status.className = `status ${kind}`.trim();
}

function renderHighlight() {
  highlight.replaceChildren();
  for (const token of tokenize(expression.value, formNames)) {
    const span = document.createElement("span");
    span.className = `tok tok--${token.kind}`;
    span.textContent = token.text;
    highlight.append(span);
  }
  highlight.append(document.createTextNode("\n"));
  expression.style.height = "auto";
  expression.style.height = `${Math.min(expression.scrollHeight, 320)}px`;
  highlight.parentElement.scrollTop = expression.scrollTop;
}

function setExpression(text) {
  expression.value = text;
  renderHighlight();
}

async function formatInput() {
  const source = expression.value.trim();
  if (!source) return;
  try {
    const parsed = await request("/api/parse", { source });
    setExpression(formatSource(parsed.expr_json.expr));
    setStatus("已格式化", "ok");
  } catch (error) {
    setStatus(error.message, "error");
  }
}

async function copyExpression() {
  try {
    await navigator.clipboard.writeText(expression.value);
    setStatus("表达式已复制到剪贴板", "ok");
  } catch (error) {
    expression.focus();
    expression.select();
    setStatus("浏览器拒绝了剪贴板访问，已选中表达式，请手动复制", "error");
  }
}

function showSource() {
  try {
    const value = designer.value;
    source.textContent = JSON.stringify(value, null, 2);
    setExpression(formatSource(value.expr));
  } catch (error) {
    source.textContent = error.message;
  }
}

async function loadCatalog() {
  const catalog = await request("/api/catalog");
  designer.catalog = catalog;
  formNames = new Set((catalog.special_forms || []).map((form) => form.name));
  renderHighlight();
}

// applyExpression parses the typed expression into canonical ExprJSON and
// replaces the node tree with it.
async function applyExpression() {
  const source = expression.value.trim();
  if (!source) {
    setStatus("请输入表达式", "error");
    return;
  }
  setStatus("正在解析表达式…");
  try {
    const parsed = await request("/api/parse", { source });
    designer.value = parsed.expr_json;
    await compile();
  } catch (error) {
    setStatus(error.message, "error");
  }
}

async function useTemplate(template) {
  designer.value = template;
  await compile().catch(() => {});
}

function typeName(type) {
  if (!type) return "unknown";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeName(type.elem)}>`;
  return type.name || type.kind;
}

function defaultValue(type, name) {
  if (name === "health") return "UP";
  if (name === "primary") return "adyen_primary";
  if (name === "fallback") return "stripe_backup";
  if (name === "country") return "SG";
  if (name === "channels") return '["UP","DOWN","UP"]';
  if (name === "prices") return "[10,20,30]";
  if (name === "acc") return "0";
  if (name === "risk") return "0.3";
  switch (type.kind) {
    case "bool": return "true";
    case "int": return name === "n" ? "6" : "0";
    case "float": return "0.0";
    case "string": return "value";
    case "array": return "[]";
    case "dict": return "{}";
    default: return "";
  }
}

function renderArgs(parameters) {
  argsRoot.replaceChildren();
  for (const parameter of parameters) {
    const row = document.createElement("div");
    row.className = "arg";
    const label = document.createElement("label");
    label.textContent = parameter.name;
    const code = document.createElement("code");
    code.textContent = typeName(parameter.type);
    label.append(code);
    const input = document.createElement("input");
    input.dataset.arg = parameter.name;
    input.dataset.type = JSON.stringify(parameter.type);
    input.value = defaultValue(parameter.type, parameter.name);
    row.append(label, input);
    argsRoot.append(row);
  }
  if (!parameters.length) {
    const empty = document.createElement("p");
    empty.className = "hint";
    empty.textContent = "该表达式没有外部参数。";
    argsRoot.append(empty);
  }
}

function parseValue(raw, type) {
  switch (type.kind) {
    case "bool": return raw === "true";
    case "int": return Number.parseInt(raw, 10);
    case "float": return Number.parseFloat(raw);
    case "string": return raw;
    case "array":
    case "dict": return JSON.parse(raw);
    default: return raw;
  }
}

function collectArgs() {
  const args = {};
  for (const input of argsRoot.querySelectorAll("input[data-arg]")) {
    args[input.dataset.arg] = parseValue(input.value, JSON.parse(input.dataset.type));
  }
  return args;
}

async function compile() {
  setStatus("正在编译…");
  try {
    compiled = await request("/api/compile", { expr_json: designer.value });
    renderArgs(compiled.args);
    resultRoot.textContent = "—";
    setStatus(`编译成功 · ${compiled.instructions} 条指令 · ${compiled.digest.slice(0, 20)}…`, "ok");
    return compiled;
  } catch (error) {
    compiled = null;
    setStatus(error.message, "error");
    throw error;
  }
}

async function run() {
  try {
    if (!compiled) await compile();
    const response = await request("/api/run", {
      expr_json: designer.value,
      args: collectArgs(),
      fuel: 10000,
    });
    resultRoot.textContent = `${JSON.stringify(response.value)}  :  ${typeName(response.type)}`;
    setStatus("运行成功", "ok");
  } catch (error) {
    resultRoot.textContent = "—";
    setStatus(error.message, "error");
  }
}

designer.addEventListener("funroute-change", () => {
  compiled = null;
  showSource();
  setStatus("表达式已修改，等待编译");
});
document.querySelector("#route-template").addEventListener("click", () => { useTemplate(routeTemplate); });
document.querySelector("#switch-template").addEventListener("click", () => { useTemplate(switchTemplate); });
document.querySelector("#for-template").addEventListener("click", () => { useTemplate(forTemplate); });
document.querySelector("#reduce-template").addEventListener("click", () => { useTemplate(reduceTemplate); });
document.querySelector("#cond-template").addEventListener("click", () => { useTemplate(condTemplate); });
document.querySelector("#recur-template").addEventListener("click", () => { useTemplate(recurTemplate); });
document.querySelector("#compile").addEventListener("click", () => { compile().catch(() => {}); });
document.querySelector("#run").addEventListener("click", run);
document.querySelector("#apply-expression").addEventListener("click", applyExpression);
document.querySelector("#format-expression").addEventListener("click", formatInput);
document.querySelector("#copy-expression").addEventListener("click", copyExpression);
expression.addEventListener("input", renderHighlight);
expression.addEventListener("scroll", () => { highlight.parentElement.scrollTop = expression.scrollTop; });
expression.addEventListener("keydown", (event) => {
  // Enter applies, Shift+Enter keeps editing across lines.
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    applyExpression();
  }
});
try {
  await loadCatalog();
  designer.value = routeTemplate;
  await compile();
} catch (error) {
  setStatus(error.message, "error");
}
