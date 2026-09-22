// The contract panel: which arguments a rule takes, in ABI order, with their
// types and the prose an operator reads — plus what it returns.
//
// This is the host's data, not the expression's. A console reads it from its
// rule record; here it is edited in a panel and sent alongside the expression.
// Because it never becomes source text, there is nothing to keep in sync: the
// panel is the contract, the canvas is the expression.

import { contractPayload, emptyContract, isEmptyContract } from "./funroute-core.js";

export { contractPayload, emptyContract, isEmptyContract };

const TYPE_LIST_ID = "fr-type-suggestions";

// The type suggestions are the catalog's value types, combined the way the type
// parser combines them. Writing a list here would be a second, stale answer to
// "what types are there" — the registry already decides that.
function typeSuggestionsFrom(valueTypes) {
  const scalars = [];
  const containers = [];
  for (const entry of valueTypes || []) {
    const kind = entry.type?.kind;
    if (kind === "array" || kind === "dict") containers.push(kind);
    else if (kind === "handle") scalars.push(`handle<${entry.type.name}>`);
    else if (kind) scalars.push(kind);
  }
  const out = [...scalars];
  for (const container of containers) {
    for (const scalar of scalars) out.push(`${container}<${scalar}>`);
  }
  out.push('enum<名字>{成员,成员}');
  return out;
}

export class ContractPanel {
  constructor(root, { onChange, onCheck, valueTypes } = {}) {
    this._types = typeSuggestionsFrom(valueTypes);
    this._root = root;
    this._onChange = onChange;
    this._onCheck = onCheck;
    this._contract = emptyContract();
    this._status = { phase: "unchecked", message: "请先检查契约" };
  }

  get value() { return this._contract; }

  // The catalog arrives after the panel is built, so the suggestions are set
  // when it does rather than duplicated as a constant.
  set valueTypes(types) {
    this._types = typeSuggestionsFrom(types);
    this.render();
  }

  set value(contract) {
    this._contract = { ...emptyContract(), ...(contract || {}) };
    this._contract.args = this._contract.args || [];
    this._status = { phase: "unchecked", message: "等待契约检查" };
    this.render();
  }

  setStatus(phase, message) {
    this._status = { phase, message };
    this.render();
  }

  render() {
    const root = this._root;
    root.replaceChildren();
    root.append(this._bar());
    const body = el("div", "fr-contract__body");
    this._contract.args.forEach((arg, index) => body.append(this._argRow(arg, index)));
    if (!this._contract.args.length) {
      body.append(el("p", "fr-hint", "此策略没有外部入参；仍需声明返回类型。"));
    }
    body.append(this._resultRow());
    const status = el("div", `fr-contract__status is-${this._status.phase}`);
    status.append(el("span", "fr-contract__status-dot"), el("span", "", this._status.message));
    root.append(body, status, typeSuggestions(this._types));
  }

  _bar() {
    const bar = el("div", "fr-contract__bar");
    const title = el("div");
    title.append(el("strong", "", "输入 / 输出定义"), el("span", "fr-muted", "入参顺序即 ABI；表达式必须返回声明类型"));
    const actions = el("div", "fr-contract__actions");
    actions.append(button("+ 入参", () => this._addArg()));
    if (this._contract.args.length) {
      actions.append(button("清空入参", () => { this._contract.args = []; this._changed(); }, "fr-button--ghost"));
    }
    actions.append(button("检查契约", () => this._onCheck?.(), "fr-button--check"));
    bar.append(title, actions);
    return bar;
  }

  _argRow(arg, index) {
    const row = el("div", "fr-contract__row");
    row.append(el("code", "fr-contract__tag", `#${index + 1}`));
    row.append(input("fr-contract__name", arg.name, "名字", (value) => { arg.name = value; this._changed(); }));
    row.append(el("span", "fr-contract__punct", ":"));
    row.append(typeInput(arg.type, (value) => { arg.type = value; this._changed(); }));
    row.append(input("fr-contract__doc", arg.doc, "说明（可选，不影响 digest）", (value) => { arg.doc = value; this._changed(); }));
    row.append(button("×", () => { this._contract.args.splice(index, 1); this._changed(); }, "fr-button--ghost fr-button--icon"));
    return row;
  }

  _resultRow() {
    const result = this._contract.result || { type: "", doc: "" };
    this._contract.result = result;
    const row = el("div", "fr-contract__row fr-contract__row--result");
    row.append(el("code", "fr-contract__tag", "→"));
    row.append(typeInput(result.type, (value) => { result.type = value; this._changed(); }));
    row.append(input("fr-contract__doc", result.doc, "说明（可选）", (value) => { result.doc = value; this._changed(); }));
    row.append(el("span", "fr-muted", "表达式的结果必须是这个类型"));
    return row;
  }

  _addArg() {
    const taken = new Set(this._contract.args.map((arg) => arg.name));
    let index = 1;
    while (taken.has(`arg${index}`)) index += 1;
    this._contract.args.push({ name: `arg${index}`, type: "int", doc: "" });
    this._changed();
  }

  _changed() {
    this._status = { phase: "dirty", message: "契约已修改，请重新检查" };
    this.render();
    this._onChange?.();
  }
}

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function input(className, value, placeholder, onChange) {
  const node = el("input", className);
  node.value = value || "";
  node.placeholder = placeholder;
  node.addEventListener("change", () => onChange(node.value.trim()));
  return node;
}

function typeInput(value, onChange) {
  const node = input("fr-contract__type", value, "类型", onChange);
  node.setAttribute("list", TYPE_LIST_ID);
  return node;
}

function button(text, onClick, extra = "") {
  const node = el("button", `fr-button fr-button--small ${extra}`.trim(), text);
  node.type = "button";
  node.addEventListener("click", onClick);
  return node;
}

function typeSuggestions(names) {
  const list = el("datalist");
  list.id = TYPE_LIST_ID;
  for (const name of names) {
    const option = el("option");
    option.value = name;
    list.append(option);
  }
  return list;
}
