// Input controls that both the canvas and the run panel need. A member set is
// the host's data and can be large — 200 countries is a normal contract — so
// the control switches shape with its size: a select while the whole set is
// readable, a filtering input backed by one shared datalist when it is not.

import { typeName } from "./funroute-core.js";
import { typeSummary } from "./funroute-display.js";

// SELECT_LIMIT is where a dropdown stops being readable. Above it the browser's
// own filtering does the work, and the members are listed once per document
// rather than once per card.
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
  "reduce.where": ["筛选条件", "bool；留空表示全部；跳过的元素不进累加器"],
  "reduce.accumulator": ["累加器局部名", "仅本节点可见；它是每一步带下去的那个值"],
  "reduce.init": ["初始值", "第一步之前累加器是什么，类型 R"],
  "reduce.body": ["每步结果", "用当前元素和累加器算出下一个累加器；必须返回 R"],
  "let.bindings": ["绑定", "后续绑定与主体可引用"],
  "let.bindings.name": ["名称", "仅本节点可见"],
  "let.bindings.value": ["值", ""],
  "let.body": ["主体", "整体结果"],
};

export function fieldText(key, field) {
  const [label, hint] = FIELD_TEXT[key] || [field.name, ""];
  return { label: field.optional ? `${label}（可空）` : label, hint };
}

const SELECT_LIMIT = 12;

export function enumControl({ values, value, placeholder, listHost, listId, onChange }) {
  const members = values || [];
  if (members.length && members.length <= SELECT_LIMIT) {
    return selectControl(members, value, onChange);
  }
  return filterControl(members, value, placeholder, listHost, listId, onChange);
}

function selectControl(members, value, onChange) {
  const select = document.createElement("select");
  select.className = "fr-input";
  for (const member of members) {
    const option = document.createElement("option");
    option.value = member;
    option.textContent = member;
    option.selected = member === value;
    select.append(option);
  }
  select.addEventListener("change", () => onChange(select.value));
  return select;
}

function filterControl(members, value, placeholder, listHost, listId, onChange) {
  const input = document.createElement("input");
  input.className = "fr-input";
  input.value = value ?? "";
  input.placeholder = placeholder || `输入以筛选 ${members.length} 个成员`;
  if (members.length) {
    input.setAttribute("list", ensureDataList(listHost, listId, members));
  }
  input.addEventListener("change", () => onChange(input.value.trim()));
  return input;
}

// ensureDataList renders a member set once per host, however many controls
// refer to it. The host is the document or a shadow root.
function ensureDataList(listHost, listId, members) {
  const host = listHost || document;
  const existing = host.getElementById ? host.getElementById(listId) : host.querySelector(`#${listId}`);
  if (existing) return listId;
  const list = document.createElement("datalist");
  list.id = listId;
  for (const member of members) {
    const option = document.createElement("option");
    option.value = member;
    list.append(option);
  }
  (host.body || host).append(list);
  return listId;
}

// enumMemberField is the whole editor for an enum node: which member, and —
// only when that member belongs to more than one enum — which enum it is from.
// Members are never free text: the contract knows the set, so it is offered.
export function enumMemberField({ enums, node, listHost, onChange }) {
  const wrap = document.createElement("div");
  wrap.className = "fr-enum-field";
  const owner = ownerOf(enums, node);
  wrap.append(enumControl({
    values: owner?.values || [],
    value: node.member,
    placeholder: enums.length ? "输入成员名" : "契约没有声明枚举",
    listHost,
    listId: `fr-enum-${owner?.name || "none"}`,
    onChange: (member) => {
      node.member = member;
      applyQualifier(enums, node, owner?.name);
      onChange();
    },
  }));
  const shared = enums.filter((item) => item.values.includes(node.member));
  if (shared.length > 1) wrap.append(qualifierControl(shared, node, onChange));
  return wrap;
}

// ownerOf picks the enum a node's member comes from: the one it names, else the
// one that holds the member, else the first the contract declares.
function ownerOf(enums, node) {
  return enums.find((item) => item.name === node.enum)
    || enums.find((item) => item.values.includes(node.member))
    || enums[0];
}

// applyQualifier keeps @enum.member only while the short form is ambiguous.
function applyQualifier(enums, node, fallbackName) {
  const owners = enums.filter((item) => item.values.includes(node.member));
  if (owners.length > 1) node.enum = fallbackName || owners[0].name;
  else delete node.enum;
}

function qualifierControl(shared, node, onChange) {
  const select = document.createElement("select");
  select.className = "fr-input fr-enum-field__qualifier";
  for (const item of shared) {
    const option = document.createElement("option");
    option.value = item.name;
    option.textContent = `属于 ${item.name}`;
    option.selected = item.name === node.enum;
    select.append(option);
  }
  select.addEventListener("change", () => { node.enum = select.value; onChange(); });
  return select;
}

// expressionRow edits a subtree as source text. The host parses it — the Go
// parser is the only grammar authority — and the row keeps the document
// unchanged until that parse succeeds, so a typo never destroys what is there.
export function expressionRow({ source, placeholder, onCommit, onExpand, onFocus }) {
  const row = document.createElement("div");
  row.className = "fr-expr-row";
  const input = document.createElement("input");
  input.className = "fr-input fr-expr-input";
  input.value = source || "";
  input.placeholder = placeholder || "写一段表达式，回车校验";
  input.spellcheck = false;
  input.autocomplete = "off";
  const error = document.createElement("p");
  error.className = "fr-expr-row__error";
  if (onFocus) input.addEventListener("focus", () => onFocus(input));
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") { event.preventDefault(); input.blur(); }
    if (event.key === "Escape") { input.value = source || ""; input.blur(); }
  });
  input.addEventListener("blur", async () => {
    const text = input.value.trim();
    if (text === (source || "").trim()) return;
    row.classList.remove("is-error");
    error.textContent = "";
    const message = await onCommit(text);
    if (!message) return;
    row.classList.add("is-error");
    error.textContent = message;
    input.focus();
  });
  const head = document.createElement("div");
  head.className = "fr-expr-row__head";
  head.append(input);
  if (onExpand) head.append(expandButton(onExpand));
  row.append(head, error);
  return row;
}

// expandedSlot wraps a slot the operator expanded into cards, and gives back
// the way out: expanding must be reversible, or two identical slots end up
// looking like two different kinds of editor.
export function expandedSlot(content, onCollapse) {
  const wrap = document.createElement("div");
  wrap.className = "fr-expr-expanded";
  const bar = document.createElement("div");
  bar.className = "fr-expr-expanded__bar";
  const control = document.createElement("button");
  control.type = "button";
  control.className = "fr-button fr-button--small fr-button--ghost";
  control.textContent = "收起为文本";
  control.addEventListener("click", onCollapse);
  bar.append(control);
  wrap.append(bar, content);
  return wrap;
}

function expandButton(onExpand) {
  const control = document.createElement("button");
  control.type = "button";
  control.className = "fr-button fr-button--small fr-button--ghost";
  control.textContent = "展开";
  control.title = "展开成可拖拽的卡片";
  control.addEventListener("click", onExpand);
  return control;
}

// valueEditor is the body of a leaf card: one control for the node's own value,
// typed the way the node schema says it is.
export function valueEditor({ node, field, namePattern, parse, onChange }) {
  if (field.kind === "bool") return boolEditor(node, field, onChange);
  const input = document.createElement("input");
  input.className = "fr-input";
  input.type = field.kind === "int" ? "number" : "text";
  if (field.kind === "int") input.step = "1";
  if (field.kind === "float") input.inputMode = "decimal";
  if (field.kind === "name") input.pattern = namePattern;
  input.placeholder = field.kind === "name" ? "参数名" : field.kind;
  input.value = String(node[field.name] ?? field.default ?? "");
  input.addEventListener("change", () => {
    if (!commitValue(input, node, field, parse)) return;
    onChange();
  });
  return input;
}

function commitValue(input, node, field, parse) {
  if (field.kind !== "int" && field.kind !== "float") {
    node[field.name] = input.value || field.default || "";
    return true;
  }
  try {
    const value = parse(input.value || "0", { kind: field.kind });
    input.setCustomValidity("");
    node[field.name] = field.kind === "int" ? value : input.value;
    return true;
  } catch (error) {
    input.setCustomValidity(error.message);
    input.reportValidity();
    return false;
  }
}

function boolEditor(node, field, onChange) {
  const select = document.createElement("select");
  select.className = "fr-input";
  for (const value of ["false", "true"]) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = value;
    option.selected = node[field.name] === (value === "true");
    select.append(option);
  }
  select.addEventListener("change", () => { node[field.name] = select.value === "true"; onChange(); });
  return select;
}

// enumOf finds the enum inside a type, so a control can be offered for
// array<enum> and dict<enum> arguments too.
export function enumOf(type) {
  if (!type) return null;
  if (type.kind === "enum") return type;
  return type.elem ? enumOf(type.elem) : null;
}

export { typeSummary };

// Turning what a person typed into a value belongs with the inputs that
// collect it, not with the language model.
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
