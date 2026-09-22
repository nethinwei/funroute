// The contract panel: which arguments a rule takes, in ABI order, with their
// types and the prose an operator reads — plus what it returns. A declared
// type names a record once so the arguments sharing its shape can refer to it;
// the name is expanded server-side and never reaches the artifact.
//
// This is the host's data, not the expression's. A console reads it from its
// rule record; here it is edited in a panel and sent alongside the expression.
// Because it never becomes source text, there is nothing to keep in sync: the
// panel is the contract, the canvas is the expression.

import { formatTypeText } from "./funroute-core.js";
import { contractPayload, emptyContract, isEmptyContract, typeRows } from "./funroute-workspace.js";

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
    // The panel names its own scope, so its stylesheet can size the classes it
    // shares with the canvas without reaching outside itself.
    this._root.classList.add("fr-contract");
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
    this._contract.types = typeRows(this._contract.types);
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
    this._contract.types.forEach((decl, index) => body.append(this._typeRow(decl, index)));
    this._contract.args.forEach((arg, index) => body.append(this._argRow(arg, index)));
    if (!this._contract.args.length) {
      body.append(el("p", "fr-hint", "此策略没有外部入参；仍需声明返回类型。"));
    }
    body.append(this._resultRow());
    const status = el("div", `fr-contract__status is-${this._status.phase}`);
    status.append(el("span", "fr-contract__status-dot"), el("span", "", this._status.message));
    const declared = this._contract.types.map((decl) => decl.name).filter(Boolean);
    root.append(body, status, typeSuggestions([...declared, ...this._types]));
  }

  _bar() {
    const bar = el("div", "fr-contract__bar");
    const title = el("div");
    title.append(el("strong", "", "输入 / 输出定义"), el("span", "fr-muted", "入参顺序即 ABI；表达式必须返回声明类型"));
    const actions = el("div", "fr-contract__actions");
    actions.append(button("+ 类型", () => this._addType()), button("+ 入参", () => this._addArg()));
    if (this._contract.args.length) {
      actions.append(button("清空入参", () => { this._contract.args = []; this._changed(); }, "fr-button--ghost"));
    }
    actions.append(button("检查契约", () => this._onCheck?.(), "fr-button--check"));
    bar.append(title, actions);
    return bar;
  }

  // A type row is "Name = type": no doc column, because a name that needs
  // explaining is the wrong name.
  _typeRow(decl, index) {
    const row = el("div", "fr-contract__row fr-contract__row--type");
    row.append(el("code", "fr-contract__tag", "类型"));
    row.append(input("fr-contract__name", decl.name, "名字", (value) => { decl.name = value; this._changed(); }));
    row.append(el("span", "fr-contract__punct", "="));
    row.append(typeArea(decl.type, (value) => { decl.type = value; this._changed(); }));
    row.append(button("×", () => { this._contract.types.splice(index, 1); this._changed(); }, "fr-button--ghost fr-button--icon"));
    return row;
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

  _addType() {
    const taken = new Set(this._contract.types.map((decl) => decl.name));
    let index = 1;
    while (taken.has(`Type${index}`)) index += 1;
    this._contract.types.push({ name: `Type${index}`, type: "record{amount: int}" });
    this._changed();
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
  // An argument's type is usually a declared name, but nothing stops someone
  // writing a record inline, and a truncated one tells you nothing.
  node.title = value || "";
  return node;
}

// typeArea is where a type is *defined*, so it opens up: a record with four
// fields is four lines, and the whole shape is on screen instead of the first
// thirty characters of it. The parser takes newlines, so what is shown is what
// is sent.
function typeArea(value, onChange) {
  const node = el("textarea", "fr-contract__type fr-contract__type--block");
  node.value = formatTypeText(value);
  node.placeholder = "record{字段: 类型, …}";
  node.spellcheck = false;
  fitRows(node);
  node.addEventListener("input", () => fitRows(node));
  node.addEventListener("change", () => {
    node.value = formatTypeText(node.value);
    fitRows(node);
    onChange(node.value.trim());
  });
  return node;
}

function fitRows(node) {
  node.rows = Math.min(12, Math.max(1, node.value.split("\n").length));
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
