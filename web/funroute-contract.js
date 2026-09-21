// The contract panel: which arguments a rule takes, in ABI order, with their
// types and the prose an operator reads — plus what it returns.
//
// This is the host's data, not the expression's. A console reads it from its
// rule record; here it is edited in a panel and sent alongside the expression.
// Because it never becomes source text, there is nothing to keep in sync: the
// panel is the contract, the canvas is the expression.

const TYPE_SUGGESTIONS = [
  "bool", "int", "float", "string",
  "array<int>", "array<float>", "array<string>", "array<bool>",
  "dict<int>", "dict<float>", "dict<string>", "dict<bool>",
];

const TYPE_LIST_ID = "fr-type-suggestions";

export function emptyContract() {
  return { args: [], result: null };
}

export function isEmptyContract(contract) {
  return !contract || (!contract.args?.length && !contract.result);
}

// payload is what the API takes: incomplete rows are dropped, so a half-typed
// argument never turns into a compile error the operator cannot read.
export function contractPayload(contract) {
  if (isEmptyContract(contract)) return null;
  const args = (contract.args || []).filter((arg) => arg.name && arg.type);
  const payload = {};
  if (args.length) payload.args = args.map((arg) => ({ name: arg.name, type: arg.type, doc: arg.doc || undefined }));
  if (contract.result?.type) {
    payload.result = { type: contract.result.type, doc: contract.result.doc || undefined };
  }
  return payload.args || payload.result ? payload : null;
}

export class ContractPanel {
  constructor(root, onChange) {
    this._root = root;
    this._onChange = onChange;
    this._contract = emptyContract();
  }

  get value() { return this._contract; }

  set value(contract) {
    this._contract = { ...emptyContract(), ...(contract || {}) };
    this._contract.args = this._contract.args || [];
    this.render();
  }

  render() {
    const root = this._root;
    root.replaceChildren();
    root.append(this._bar());
    if (isEmptyContract(this._contract)) {
      const hint = el("p", "fr-hint");
      hint.textContent = "没有契约：参数与类型全部由推导得出。加一条入参即可固定 ABI。";
      root.append(hint);
      return;
    }
    const body = el("div", "fr-contract__body");
    this._contract.args.forEach((arg, index) => body.append(this._argRow(arg, index)));
    body.append(this._resultRow());
    root.append(body, typeSuggestions());
  }

  _bar() {
    const bar = el("div", "fr-contract__bar");
    const title = el("div");
    title.append(el("strong", "", "契约"), el("span", "fr-muted", "入参（按顺序即 ABI）与返回类型"));
    const actions = el("div", "fr-contract__actions");
    actions.append(button("+ 入参", () => this._addArg()));
    if (!isEmptyContract(this._contract)) {
      actions.append(button("清空契约", () => { this._contract = emptyContract(); this._changed(); }, "fr-button--ghost"));
    }
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
    const result = this._contract.result || { type: "int", doc: "" };
    this._contract.result = result;
    const row = el("div", "fr-contract__row");
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
    if (!this._contract.result) this._contract.result = { type: "int", doc: "" };
    this._changed();
  }

  _changed() {
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

function typeSuggestions() {
  const list = el("datalist");
  list.id = TYPE_LIST_ID;
  for (const name of TYPE_SUGGESTIONS) {
    const option = el("option");
    option.value = name;
    list.append(option);
  }
  return list;
}
