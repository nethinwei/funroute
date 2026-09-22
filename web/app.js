import "./funroute-designer.js";
import {
  FunRouteClient, FunRouteWorkspace, clone, contractComments,
  parseInputValue, typeName,
} from "./funroute-core.js";
import { ContractPanel } from "./funroute-contract.js";
import { EXAMPLES } from "./funroute-examples.js";

const elements = {
  designer: document.querySelector("#designer"),
  status: document.querySelector("#status"),
  source: document.querySelector("#source"),
  args: document.querySelector("#args"),
  result: document.querySelector("#result"),
  expression: document.querySelector("#expression"),
  highlight: document.querySelector("#expression-highlight"),
  examples: document.querySelector("#examples"),
  version: document.querySelector("#version-badge"),
  metrics: document.querySelector("#compile-metrics"),
  contractScope: document.querySelector("#contract-scope"),
  expectedType: document.querySelector("#expected-result-type"),
  resultCheck: document.querySelector("#result-check"),
  run: document.querySelector("#run"),
};

const workspace = new FunRouteWorkspace(new FunRouteClient());
const contract = new ContractPanel(document.querySelector("#contract"), {
  onChange: () => {
    workspace.setContract(contract.value);
    elements.designer.runtimeContract = null;
    elements.designer.validation = { phase: "dirty", message: "运行契约已修改，画布暂不可确认；正在重新检查…" };
    renderContractScope();
    renderArgsMessage("正在重新检查契约…");
    resetResult();
    elements.metrics.textContent = "契约已修改，等待重新编译";
    checkContract().then((checked) => {
      if (checked && workspace.document) scheduleCanvasCheck();
    }).catch(() => {});
  },
  onCheck: () => checkContract().then((checked) => {
    if (checked && workspace.document) scheduleCanvasCheck();
  }).catch(() => {}),
});

let canvasCheckTimer = null;
let canvasCheckRevision = 0;

workspace.subscribe(({ status }) => renderStatus(status));

function renderStatus(current) {
  const kinds = { success: "ok", error: "error", loading: "loading", dirty: "dirty" };
  elements.status.textContent = current.message || "就绪";
  elements.status.className = `status ${kinds[current.phase] || ""}`.trim();
}

function renderHighlight() {
  elements.highlight.replaceChildren();
  if (!workspace.language) return;
  for (const token of workspace.language.tokenize(elements.expression.value)) {
    const span = document.createElement("span");
    span.className = `tok tok--${token.kind}`;
    span.textContent = token.text;
    elements.highlight.append(span);
  }
  elements.highlight.append(document.createTextNode("\n"));
  elements.expression.style.height = "auto";
  elements.expression.style.height = `${Math.min(elements.expression.scrollHeight, 320)}px`;
  elements.highlight.parentElement.scrollTop = elements.expression.scrollTop;
}

function setExpression(text) {
  elements.expression.value = text;
  renderHighlight();
}

function renderContractScope() {
  elements.contractScope.replaceChildren();
  const args = contract.value.args || [];
  elements.contractScope.append(scopeLabel("可用入参"));
  if (!args.length) elements.contractScope.append(scopeChip("无外部入参", "muted"));
  for (const arg of args) elements.contractScope.append(scopeChip(`${arg.name || "未命名"}: ${arg.type || "未定义"}`));
  elements.contractScope.append(scopeLabel("必须返回"));
  elements.contractScope.append(scopeChip(contract.value.result?.type || "未定义", "result"));
}

function scopeLabel(text) {
  const node = document.createElement("span");
  node.className = "contract-scope__label";
  node.textContent = text;
  return node;
}

function scopeChip(text, kind = "") {
  const node = document.createElement("code");
  node.className = `contract-scope__chip ${kind ? `is-${kind}` : ""}`.trim();
  node.textContent = text;
  return node;
}

async function checkContract() {
  contract.setStatus("loading", "正在由 Go 类型系统检查…");
  elements.designer.validation = { phase: "checking", message: "正在检查运行契约…" };
  try {
    const checked = await workspace.checkContract();
    if (!checked) return null;
    elements.designer.runtimeContract = checked;
    elements.designer.validation = { phase: "dirty", message: "契约已检查，等待核对画布参数和返回类型。" };
    renderArgs(checked.args || []);
    elements.expectedType.textContent = typeName(checked.result);
    resetResult();
    contract.setStatus("valid", `检查通过 · ${checked.arguments} 个入参 → ${contract.value.result.type}`);
    return checked;
  } catch (error) {
    elements.designer.runtimeContract = null;
    elements.designer.validation = { phase: "error", message: `运行契约不可用：${error.message}` };
    renderArgsMessage("请先修正契约，再填写试运行入参。");
    elements.expectedType.textContent = contract.value.result?.type || "未定义";
    contract.setStatus("error", error.message);
    workspace.reportError(error);
    throw error;
  }
}

function syncDocument(document = elements.designer.value) {
  workspace.setDocument(document);
  elements.source.textContent = JSON.stringify(document, null, 2);
  setExpression(workspace.language.formatSource(document.expr));
  scheduleCanvasCheck();
}

function scheduleCanvasCheck() {
  if (canvasCheckTimer) clearTimeout(canvasCheckTimer);
  const revision = ++canvasCheckRevision;
  elements.designer.validation = { phase: "checking", message: "画布已修改，正在检查参数作用域和返回类型…" };
  canvasCheckTimer = setTimeout(() => {
    canvasCheckTimer = null;
    compile({ validationRevision: revision }).catch(() => {});
  }, 220);
}

async function formatInput() {
  const source = elements.expression.value.trim();
  if (!source) return;
  try {
    const parsed = await workspace.client.parse(source);
    setExpression(workspace.language.formatSource(parsed.expr_json.expr));
    markExpressionDirty("表达式已格式化；运行并检查后同步到画布");
  } catch (error) { fail(error); }
}

async function copyExpression() {
  try {
    await navigator.clipboard.writeText(contractComments(contract.value, elements.expression.value));
    renderStatus({ phase: "success", message: "表达式与契约说明已复制" });
  } catch {
    elements.expression.focus();
    elements.expression.select();
    fail(new Error("浏览器拒绝剪贴板访问，已选中文本，请手动复制"));
  }
}

async function loadExample(example) {
  try {
    setExpression(example.source);
    contract.value = clone(example.contract);
    workspace.setContract(contract.value);
    renderContractScope();
    await checkContract();
    const document = await workspace.parseSource(example.source);
    if (!document) return;
    elements.designer.value = document;
    renderDocument(document);
    await compile();
  } catch (error) { fail(new Error(`示例「${example.label}」无法载入：${error.message}`)); }
}

function renderExamples() {
  elements.examples.replaceChildren();
  for (const [index, example] of EXAMPLES.entries()) {
    const button = document.createElement("button");
    button.className = index === 0 ? "example is-active" : "example";
    button.type = "button";
    button.textContent = example.label;
    button.title = example.description;
    button.addEventListener("click", () => {
      for (const item of elements.examples.children) item.classList.remove("is-active");
      button.classList.add("is-active");
      loadExample(example);
    });
    elements.examples.append(button);
  }
}

function renderDocument(document) {
  elements.source.textContent = JSON.stringify(document, null, 2);
  setExpression(workspace.language.formatSource(document.expr));
}

function defaultValue(type, name) {
  const named = {
    health: "UP", primary: "adyen_primary", fallback: "stripe_backup", country: "SG",
    channels: '["UP","DOWN","UP"]', prices: "[10,20,30]", features: "[0.9,0.7,0.95]",
    acc: "0", risk: "0.3",
  };
  if (named[name] !== undefined) return named[name];
  return { bool: "true", int: name === "n" ? "6" : "0", float: "0.0", string: "value", array: "[]", dict: "{}" }[type.kind] || "";
}

function renderArgs(parameters) {
  const previous = new Map([...elements.args.querySelectorAll("input[data-arg]")].map((input) => [input.dataset.arg, input.value]));
  elements.args.replaceChildren();
  for (const parameter of parameters) {
    const row = document.createElement("label");
    row.className = "arg";
    const heading = document.createElement("span");
    heading.className = "arg__heading";
    heading.append(parameter.name);
    const code = document.createElement("code");
    code.textContent = typeName(parameter.type);
    heading.append(code);
    const input = document.createElement("input");
    input.dataset.arg = parameter.name;
    input.dataset.type = JSON.stringify(parameter.type);
    input.value = previous.get(parameter.name) ?? defaultValue(parameter.type, parameter.name);
    input.placeholder = typeName(parameter.type);
    row.append(heading, input);
    if (parameter.doc) row.append(hint(parameter.doc));
    elements.args.append(row);
  }
  if (!parameters.length) elements.args.append(hint("这个表达式没有外部参数。"));
}

function renderArgsMessage(message) {
  elements.args.replaceChildren(hint(message));
}

function resetResult(message = "尚未运行") {
  elements.result.textContent = message;
  elements.result.className = "result__value is-empty";
  elements.resultCheck.textContent = "等待运行结果";
  elements.resultCheck.className = "result__check is-pending";
}

function hint(text) {
  const node = document.createElement("span");
  node.className = "hint";
  node.textContent = text;
  return node;
}

function collectArgs() {
  const args = {};
  for (const input of elements.args.querySelectorAll("input[data-arg]")) {
    try {
      args[input.dataset.arg] = parseInputValue(input.value, JSON.parse(input.dataset.type));
      input.setCustomValidity("");
    } catch (error) {
      input.setCustomValidity(error.message);
      input.reportValidity();
      throw new Error(`参数 ${input.dataset.arg}：${error.message}`);
    }
  }
  return args;
}

async function compile({ validationRevision = 0 } = {}) {
  try {
    if (!workspace.contractCheck) {
      const checked = await checkContract();
      if (!checked) return null;
    }
    elements.designer.validation = { phase: "checking", message: "正在按运行契约编译画布…" };
    const compiled = await workspace.compile();
    if (!compiled) return null;
    if (validationRevision && validationRevision !== canvasCheckRevision) return null;
    renderCompiled(compiled);
    return compiled;
  } catch (error) {
    if (validationRevision && validationRevision !== canvasCheckRevision) return null;
    contract.setStatus("error", "表达式未通过契约匹配检查");
    elements.designer.validation = { phase: "error", message: canvasContractError(error) };
    fail(error);
    throw error;
  }
}

function renderCompiled(compiled) {
  renderArgs(compiled.args);
  elements.expectedType.textContent = typeName(compiled.result);
  resetResult();
  elements.metrics.textContent = `${compiled.instructions} instructions · ${(compiled.calls || []).length} calls · ${compiled.digest.slice(0, 12)}`;
  contract.setStatus("valid", "契约有效，表达式输入与返回类型完全匹配");
  elements.designer.validation = {
    phase: "valid",
    message: `参数均来自运行契约或本地作用域，返回 ${typeName(compiled.result)} 与契约一致。`,
  };
}

function canvasContractError(error) {
  const message = error?.message || String(error);
  const mismatch = message.match(/@ret declares (.+) but the expression returns (.+)$/);
  if (mismatch) return `返回类型不匹配：运行契约要求 ${mismatch[1]}，画布当前返回 ${mismatch[2]}。`;
  return `画布不符合运行契约：${message}`;
}

async function run() {
  const source = elements.expression.value.trim();
  if (!source) return fail(new Error("请输入表达式"));
  if (elements.run.disabled) return;
  let appliedToCanvas = false;
  if (canvasCheckTimer) clearTimeout(canvasCheckTimer);
  canvasCheckTimer = null;
  canvasCheckRevision += 1;
  setRunPending(true);
  try {
    if (!workspace.contractCheck) await checkContract();
    elements.designer.validation = {
      phase: "checking",
      message: "正在检查当前表达式；通过后将同步到画布并运行…",
    };
    const prepared = await workspace.compileSource(source);
    if (!prepared) return;
    elements.designer.value = prepared.document;
    renderDocument(prepared.document);
    renderCompiled(prepared.compiled);
    appliedToCanvas = true;
    const response = await workspace.run(collectArgs());
    if (!response) return;
    elements.result.textContent = `${JSON.stringify(response.value)}  :  ${typeName(response.type)}`;
    elements.result.className = "result__value";
    elements.resultCheck.textContent = `✓ 实际类型 ${typeName(response.type)} 与契约一致`;
    elements.resultCheck.className = "result__check is-valid";
  } catch (error) {
    elements.designer.validation = {
      phase: "error",
      message: appliedToCanvas
        ? `表达式已同步，但运行失败：${error.message}`
        : `当前表达式未应用到画布：${error.message}`,
    };
    fail(error);
  } finally { setRunPending(false); }
}

function setRunPending(pending) {
  elements.run.disabled = pending;
  elements.run.setAttribute("aria-busy", String(pending));
  elements.run.querySelector(".button--run__label").textContent = pending ? "正在检查…" : "运行并检查";
}

function markExpressionDirty(message = "表达式有未检查的修改；运行并检查后同步到画布") {
  renderHighlight();
  renderStatus({ phase: "dirty", message });
  elements.metrics.textContent = "文本有未检查的修改";
  elements.designer.validation = {
    phase: "dirty",
    message: "文本表达式已修改；当前画布仍保留上一次通过检查的版本。",
  };
  resetResult("表达式已修改，等待运行并检查");
}

function fail(error) {
  workspace.reportError(error);
  elements.result.className = "result__value is-error";
  if (!elements.result.textContent || elements.result.textContent === "尚未运行") elements.result.textContent = "请修正上方错误";
  elements.resultCheck.textContent = error.message;
  elements.resultCheck.className = "result__check is-error";
}

elements.designer.addEventListener("funroute-change", (event) => syncDocument(event.detail.value));
elements.run.addEventListener("click", run);
document.querySelector("#format-expression").addEventListener("click", formatInput);
document.querySelector("#copy-expression").addEventListener("click", copyExpression);
elements.expression.addEventListener("input", () => markExpressionDirty());
elements.expression.addEventListener("scroll", () => { elements.highlight.parentElement.scrollTop = elements.expression.scrollTop; });
elements.expression.addEventListener("keydown", (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
    event.preventDefault();
    run();
  }
});

try {
  const catalog = await workspace.initialize();
  elements.designer.catalog = catalog;
  elements.version.textContent = `Catalog v${catalog.version} · ExprJSON v${catalog.source.expr_json_version} · Artifact v${catalog.artifact_version}`;
  renderExamples();
  await loadExample(EXAMPLES[0]);
} catch (error) { fail(error); }
