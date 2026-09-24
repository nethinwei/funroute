// The workbench: an editor and a structure view on one text, a contract, a
// test run. Every fact about the language comes from the language server;
// this file only moves them between the pieces.
import { html, nothing, render } from "lit";
import type { EditorView } from "@codemirror/view";
import { startClient } from "./lsp.ts";
import { createEditor, formatDocument, offsetAt, replaceAll } from "./editor.ts";
import { Text, argsText } from "./projection.ts";
import type { ArgSpec, Catalog, Diagnostic, MoneySpec, RunResult, TextContract, Tree } from "./protocol.ts";
import type { ContractPanel } from "./contract.ts";
import type { RunPanel } from "./runner.ts";
import type { Block, Edit, StructureView } from "./canvas.ts";
import { switchRefusal, type Checked, type View } from "./views.ts";
import "./contract.ts";
import "./runner.ts";
import "./canvas.ts";

const URI = "file:///rule.fr";
const DOC = { textDocument: { uri: URI } };
const $ = <T extends Element>(selector: string) => document.querySelector(selector) as T;
const contract = $<ContractPanel>("fr-contract");
const runner = $<RunPanel>("fr-runner");
const structure = $<StructureView>("fr-structure");

const client = startClient((method, params) => {
  if (method === "textDocument/publishDiagnostics" && params.uri === URI) void showDiagnostics(params.diagnostics);
});
const editor: EditorView = createEditor({ client, uri: URI, parent: $("#editor"), onRun: () => void run() });

function setStatus(text: string, kind: "ok" | "warning" | "error" | "loading") {
  const status = $<HTMLElement>("#status");
  status.textContent = text;
  status.className = `status ${kind}`;
}

// failed puts what went wrong in the status line, for the actions that have
// no other place to say it.
const failed = (what: string) => (error: unknown) => setStatus(`${what}：${error instanceof Error ? error.message : error}`, "error");

// showDiagnostics says the worst of what the server found, then redraws what
// depends on the text: the structure view, and the arguments of a program
// whose contract declares none. Anything that answers for an older text is
// dropped.
async function showDiagnostics(diagnostics: Diagnostic[]) {
  const first = diagnostics.find((item) => item.severity === 1) ?? diagnostics[0];
  const said = first ? `${first.range.start.line + 1}:${first.range.start.character + 1} ${first.message}` : "";
  if (first) setStatus(said, first.severity === 1 ? "error" : "warning");
  else setStatus("编译通过", "ok");
  client.sync();
  const text = editor.state.doc.toString();
  const [tree, args] = await Promise.all([
    client.request<object, Tree | null>("funroute/syntaxTree", DOC),
    client.request<object, ArgSpec[]>("funroute/arguments", DOC),
  ]);
  if (text !== editor.state.doc.toString()) return;
  structure.text = new Text(text);
  structure.tree = tree;
  runner.args = args;
  $<HTMLElement>("#tree").textContent = JSON.stringify(tree, null, 2);
  checked = { source: text, error: first?.severity === 1 ? said : undefined };
  markTabs();
}

// The two views of the expression. checked is what the server last said of
// the text; the other tab is offered only while that text is sound.
let view: View = "code";
let checked: Checked | null = null;
const tabs: Record<View, HTMLElement> = { code: $("#tab-code"), structure: $("#tab-structure") };
const panels: Record<View, HTMLElement> = { code: $("#editor"), structure: $("#structure") };

function markTabs() {
  const other: View = view === "code" ? "structure" : "code";
  const refusal = switchRefusal(checked, editor.state.doc.toString());
  tabs[other].setAttribute("aria-disabled", String(refusal !== ""));
  tabs[other].title = refusal;
}

function showView(next: View) {
  if (next === view) return;
  const refusal = switchRefusal(checked, editor.state.doc.toString());
  if (refusal) {
    setStatus(refusal, "error");
    return;
  }
  view = next;
  for (const name of ["code", "structure"] as View[]) {
    const on = name === view;
    tabs[name].setAttribute("aria-selected", String(on));
    tabs[name].tabIndex = on ? 0 : -1;
    panels[name].hidden = !on;
  }
  markTabs();
  // A hidden editor measured nothing; now it is shown it lays out again.
  if (view === "code") {
    editor.requestMeasure();
    editor.focus();
  }
}

for (const name of ["code", "structure"] as View[]) {
  tabs[name].addEventListener("click", () => showView(name));
  tabs[name].addEventListener("keydown", (event) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    const next: View = name === "code" ? "structure" : "code";
    showView(next);
    tabs[view].focus();
  });
}

structure.addEventListener("edit", (event) => {
  const { range, text, source } = (event as CustomEvent<Edit>).detail;
  const doc = editor.state.doc;
  if (doc.toString() !== source) return;
  editor.dispatch({ changes: { from: offsetAt(doc, range.start), to: offsetAt(doc, range.end), insert: text } });
});

// The contract is two panels' work: the types and arguments are the contract
// panel's, the result is the test run's. Either one changing sends the whole;
// the arguments the runner asks for come back with the next diagnostics.
function sendContract() {
  const next: TextContract = { ...contract.contract, result: runner.declared };
  runner.inferred = !next.args?.length;
  client.notification("funroute/setContract", { contract: next });
}
contract.addEventListener("contract-change", sendContract);
runner.addEventListener("returns-change", sendContract);

async function run() {
  runner.busy = true;
  client.sync();
  try {
    const args = argsText(runner.entries());
    const started = performance.now();
    const result = await client.request<object, RunResult>("workspace/executeCommand", { command: "funroute.run", arguments: [{ uri: URI, args }] });
    runner.done(result, performance.now() - started);
  } catch (error) {
    failed("运行没有完成")(error);
  } finally {
    runner.busy = false;
  }
}
runner.addEventListener("run", () => void run());

$("#format").addEventListener("click", () => formatDocument(editor));
$("#copy").addEventListener("click", () => {
  client.sync();
  client.request<object, { source: string }>("workspace/executeCommand", { command: "funroute.render", arguments: [{ uri: URI }] })
    .then((rendered) => navigator.clipboard.writeText(rendered.source))
    .then(() => setStatus("已复制，契约写在注释里", "ok"), failed("没有复制"));
});

type Example = {
  label: string; category: string; description: string; source: string; contract: TextContract;
  args: Record<string, unknown>;
};

function pick(example: Example) {
  contract.contract = example.contract;
  runner.declared = example.contract.result;
  runner.values = Object.fromEntries(Object.entries(example.args ?? {}).map(([name, value]) => [name, JSON.stringify(value)]));
  runner.result = null;
  sendContract();
  replaceAll(editor, example.source);
}

function showExamples(examples: Example[]) {
  const categories = [...new Set(examples.map((example) => example.category))];
  render(categories.map((category) => html`<div class="example-group"><span class="example-group__label">${category}</span>
    <div class="example-group__items">${examples.filter((example) => example.category === category).map((example) => html`
      <button class="example" title=${example.description} @click=${() => pick(example)}>${example.label}</button>`)}</div></div>`),
  $("#examples"));
}

function showCatalog(catalog: Catalog) {
  $("#version").textContent = `artifact v${catalog.artifact_version}`;
  structure.lazy = new Set(catalog.functions.filter((item) => item.special).map((item) => item.name));
  const blocks = new Map<string, Block>();
  for (const item of [...catalog.special_forms, ...catalog.functions]) {
    if (!blocks.has(item.name)) blocks.set(item.name, { label: item.doc.label, description: item.doc.description ?? "" });
  }
  structure.blocks = blocks;
  const categories = [...new Set(catalog.functions.map((item) => item.doc.category))].sort();
  render(html`${moneySection(catalog.money)}${categories.map((category) => html`<h4>${category}</h4><dl>${catalog.functions.filter((item) => item.doc.category === category).map((item) => html`
    <dt><code>${item.signature}</code></dt><dd>${item.doc.label}${item.doc.description ? `：${item.doc.description}` : ""}</dd>`)}</dl>`)}`,
  $("#reference"));
}

// moneySection lists the currencies the registry declared, each with its
// decimal places.
function moneySection(money?: MoneySpec) {
  if (!money) return nothing;
  return html`<h4>币种</h4>
    <p class="money-note">括号里是小数位。金额写作 <code>USD 1.70</code>，币种直接写代码 <code>USD</code>，汇率写作 <code>150 JPY / USD</code>；舍入一律写出来：<code>round(amount * 2.9%, @half_up)</code>。</p>
    <p class="currencies">${money.currencies.map((currency) => html`<code>${currency.code}(${currency.digits})</code> `)}</p>`;
}

// The theme follows the system until someone picks one. The page's own
// state is the truth; localStorage only remembers it, and may refuse to.
const THEMES = ["system", "light", "dark"];
const THEME_NAMES: Record<string, string> = { system: "跟随系统", light: "浅色", dark: "深色" };
function applyTheme(theme: string) {
  if (theme === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = theme;
  $("#theme").setAttribute("aria-label", `主题：${THEME_NAMES[theme]}，点击切换`);
  const dark = theme === "dark" || (theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  for (const meta of document.querySelectorAll('meta[name="theme-color"]')) meta.setAttribute("content", dark ? "#0d1017" : "#ffffff");
}
$("#theme").addEventListener("click", () => {
  const next = THEMES[(THEMES.indexOf(document.documentElement.dataset.theme ?? "system") + 1) % THEMES.length];
  try { localStorage.setItem("funroute:theme", next); } catch { /* the switch still works, unremembered */ }
  applyTheme(next);
});
try { applyTheme(localStorage.getItem("funroute:theme") ?? "system"); } catch { applyTheme("system"); }

async function start() {
  setStatus("正在载入语言服务…", "loading");
  await client.initializing;
  showCatalog(await client.request<object, Catalog>("funroute/catalog", {}));
  const manifest = await fetch("funroute-examples.json").then((response) => response.json());
  showExamples(manifest.examples);
  pick(manifest.examples[0]);
}
void start().catch(failed("语言服务没有启动"));
