import { expressionSource } from "./funroute-source.js";

const MIME = "application/x-funroute-node";

// Bumped with the Go side when switch cases became lists and the subject
// optional.
const EXPR_JSON_VERSION = 2;

const VALUE_TEMPLATES = [
  { id: "value:var", node: "var", label: "参数", description: "自动成为 args 参数", icon: "𝑥", color: "#475569" },
  { id: "value:int", node: "int", label: "整数", description: "int64 立即值", icon: "1", color: "#2563EB" },
  { id: "value:float", node: "float", label: "浮点数", description: "有限 float64 立即值", icon: ".", color: "#0891B2" },
  { id: "value:string", node: "string", label: "字符串", description: "UTF-8 立即值", icon: "”", color: "#059669" },
  { id: "value:bool", node: "bool", label: "布尔值", description: "true / false", icon: "?", color: "#0EA5E9" },
  { id: "value:array", node: "array", label: "数组", description: "元素必须同型", icon: "[ ]", color: "#D97706" },
  { id: "value:dict", node: "dict", label: "字典", description: "string key、value 同型", icon: "{ }", color: "#EA580C" },
];

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function clone(value) {
  return value == null ? value : JSON.parse(JSON.stringify(value));
}

function cleanNode(node) {
  if (!node) return null;
  switch (node.node) {
    case "call":
      return { node: "call", name: node.name, args: (node.args || []).map(cleanNode) };
    case "array":
      return { node: "array", items: (node.items || []).map(cleanNode) };
    case "dict":
      return {
        node: "dict",
        entries: (node.entries || []).map((entry) => ({ key: entry.key || "", value: cleanNode(entry.value) })),
      };
    case "switch":
      return {
        node: "switch",
        // A missing subject is the condition form; the field is omitted so the
        // JSON matches what the Go side exports.
        ...(node.value ? { value: cleanNode(node.value) } : {}),
        cases: (node.cases || []).map((item) => ({
          match: (item.match || []).map(cleanNode),
          result: cleanNode(item.result),
        })),
        default: cleanNode(node.default),
      };
    case "for":
      return {
        node: "for",
        source: cleanNode(node.source),
        variable: node.variable || "item",
        ...(node.where ? { where: cleanNode(node.where) } : {}),
        yield: cleanNode(node.yield),
      };
    case "reduce":
      return {
        node: "reduce",
        source: cleanNode(node.source),
        variable: node.variable || "item",
        accumulator: node.accumulator || "acc",
        init: cleanNode(node.init),
        body: cleanNode(node.body),
      };
    case "prog":
      return { node: "prog", steps: (node.steps || []).map(cleanNode) };
    case "var":
      return { node: "var", name: node.name || "value" };
    case "int":
      return { node: "int", int: Number.isFinite(Number(node.int)) ? Math.trunc(Number(node.int)) : 0 };
    case "float":
      return { node: "float", float: String(node.float ?? "0.0") };
    case "string":
      return { node: "string", string: String(node.string ?? "") };
    case "bool":
      return { node: "bool", bool: Boolean(node.bool) };
    default:
      throw new Error(`不支持的节点 ${node.node}`);
  }
}

function typeName(type) {
  if (!type) return "动态";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeName(type.elem)}>`;
  return type.name || type.kind || "动态";
}

// SPECIAL_NODES are the lazy forms the catalog describes as special forms; they
// are rendered by dedicated cards instead of the generic call card.
const SPECIAL_NODES = new Set(["switch", "for", "reduce", "prog"]);

export class FunRouteDesigner extends HTMLElement {
  constructor() {
    super();
    this._catalog = { functions: [], special_forms: [] };
    this._root = null;
    this._templates = new Map();
    this._functionDescriptors = new Map();
    this._search = "";
    this._paletteScroll = 0;
  }

  connectedCallback() {
    this.classList.add("fr-designer");
    this.render();
  }

  set catalog(value) {
    this._catalog = clone(value || { functions: [], special_forms: [] });
    this._rebuildTemplates();
    this.render();
  }

  get catalog() { return clone(this._catalog); }

  set value(documentValue) {
    const expression = documentValue?.expr || documentValue || null;
    this._root = expression ? clone(expression) : null;
    this.render();
    this._emitChange();
  }

  get value() {
    return { version: EXPR_JSON_VERSION, expr: cleanNode(this._root) };
  }

  get source() { return expressionSource(this._root); }

  clear() {
    this._root = null;
    this.render();
    this._emitChange();
  }

  _rebuildTemplates() {
    this._templates.clear();
    this._functionDescriptors.clear();
    const groups = new Map();
    for (const descriptor of (this._catalog.functions || [])) {
      if (!groups.has(descriptor.name)) groups.set(descriptor.name, []);
      groups.get(descriptor.name).push(descriptor);
    }
    let index = 0;
    for (const [name, overloads] of groups) {
      const first = overloads[0];
      const arity = (first.params || []).length;
      const sameType = (values) => values.every((value) => JSON.stringify(value) === JSON.stringify(values[0]));
      const params = Array.from({ length: arity }, (_, paramIndex) => {
        const values = overloads.map((item) => item.params?.[paramIndex] || null);
        return sameType(values) ? values[0] : null;
      });
      const results = overloads.map((item) => item.result || null);
      const display = clone(first.display || {});
      if (overloads.length > 1 && display.category === "类型转换") {
        display.description = `${display.label}；编译器会根据输入自动选择匹配的转换签名。`;
      }
      const descriptor = {
        ...first,
        name,
        params,
        result: sameType(results) ? results[0] : null,
        signature: overloads.length === 1 ? first.signature : `${name}(${overloads.length} 个类型签名)`,
        display,
        overloads,
      };
      this._functionDescriptors.set(name, descriptor);
      this._templates.set(`fn:${index}`, { kind: "function", descriptor });
      index += 1;
    }
    for (const [index, descriptor] of (this._catalog.special_forms || []).entries()) {
      this._templates.set(`special:${index}`, { kind: "function", descriptor });
    }
    for (const descriptor of VALUE_TEMPLATES) {
      this._templates.set(descriptor.id, { kind: "value", descriptor });
    }
  }

  render() {
    if (!this.isConnected) return;
    const shell = element("div", "fr-shell");
    const palette = element("aside", "fr-palette");
    const paletteHeader = element("div", "fr-palette__header");
    paletteHeader.append(element("strong", "", "组件"));
    const search = element("input", "fr-search");
    search.type = "search";
    search.placeholder = "搜索函数或说明";
    search.value = this._search;
    paletteHeader.append(search);
    const list = element("div", "fr-palette__list");
    search.addEventListener("input", () => {
      this._search = search.value.toLowerCase().trim();
      this._renderPaletteList(list);
    });
    palette.append(paletteHeader, list);
    this._renderPaletteList(list);
    list.addEventListener("scroll", () => {
      this._paletteScroll = list.scrollTop;
    }, { passive: true });

    const work = element("section", "fr-workspace");
    const workHeader = element("div", "fr-workspace__header");
    const title = element("div");
    title.append(element("strong", "", "表达式"), element("span", "fr-muted", "拖到参数槽；拖动 ⠿ 可移动节点"));
    const clearButton = element("button", "fr-button fr-button--ghost", "清空");
    clearButton.type = "button";
    clearButton.addEventListener("click", () => this.clear());
    workHeader.append(title, clearButton);
    const canvas = element("div", "fr-canvas");
    if (this._root) canvas.append(this._renderNode(this._root, []));
    else canvas.append(this._dropZone([], "把函数、变量或立即值拖到这里"));
    work.append(workHeader, canvas);
    shell.append(palette, work);
    this.replaceChildren(shell);
    // Restore only after the list is attached and has a measurable scroll range.
    list.scrollTop = this._paletteScroll;
  }

  _renderPaletteList(container) {
    container.replaceChildren();
    const groups = new Map();
    const add = (category, id, descriptor) => {
      const haystack = `${descriptor.label || ""} ${descriptor.description || ""} ${(descriptor.keywords || []).join(" ")} ${descriptor.signature || ""}`.toLowerCase();
      if (this._search && !haystack.includes(this._search)) return;
      if (!groups.has(category)) groups.set(category, []);
      groups.get(category).push({ id, descriptor });
    };
    for (const [id, template] of this._templates) {
      if (template.kind === "value") add("值", id, template.descriptor);
      else add(template.descriptor.display?.category || "其他", id, {
        ...template.descriptor.display,
        signature: template.descriptor.signature,
      });
    }
    for (const [category, entries] of groups) {
      const section = element("section", "fr-palette-group");
      section.append(element("h3", "", category));
      for (const entry of entries) section.append(this._paletteCard(entry.id, entry.descriptor));
      container.append(section);
    }
    if (!container.childElementCount) container.append(element("p", "fr-empty", "没有匹配的组件"));
  }

  _paletteCard(templateId, descriptor) {
    const card = element("div", "fr-palette-card");
    card.draggable = true;
    card.style.setProperty("--fr-accent", descriptor.color || "#64748B");
    const icon = element("span", "fr-palette-card__icon", descriptor.icon || "ƒ");
    const body = element("span", "fr-palette-card__body");
    body.append(element("strong", "", descriptor.label || templateId));
    if (descriptor.description) body.append(element("small", "", descriptor.description));
    card.append(icon, body);
    card.title = descriptor.signature || descriptor.description || "";
    card.addEventListener("dragstart", (event) => {
      event.dataTransfer.effectAllowed = "copy";
      event.dataTransfer.setData(MIME, JSON.stringify({ origin: "palette", templateId }));
    });
    card.addEventListener("dblclick", () => {
      if (!this._root) {
        this._root = this._createTemplate(templateId);
        this.render();
        this._emitChange();
      }
    });
    return card;
  }

  _createTemplate(templateId) {
    const template = this._templates.get(templateId);
    if (!template) throw new Error(`未知模板 ${templateId}`);
    if (template.kind === "value") {
      switch (template.descriptor.node) {
        case "var": return { node: "var", name: "value" };
        case "int": return { node: "int", int: 0 };
        case "float": return { node: "float", float: "0.0" };
        case "string": return { node: "string", string: "" };
        case "bool": return { node: "bool", bool: false };
        case "array": return { node: "array", items: [] };
        case "dict": return { node: "dict", entries: [] };
      }
    }
    const descriptor = template.descriptor;
    if (descriptor.special === "switch") {
      return { node: "switch", value: null, cases: [{ match: [null], result: null }], default: null };
    }
    if (descriptor.special === "for") {
      return { node: "for", source: null, variable: "item", where: null, yield: { node: "var", name: "item" } };
    }
    if (descriptor.special === "reduce") {
      return {
        node: "reduce", source: null, variable: "item", accumulator: "acc",
        init: null, body: { node: "var", name: "acc" },
      };
    }
    if (descriptor.special === "prog") {
      return { node: "prog", steps: [null, null] };
    }
    const arity = descriptor.variadic ? 1 : (descriptor.params || []).length;
    return {
      node: "call",
      name: descriptor.name,
      args: Array(arity).fill(null),
      ...(descriptor.overloads?.length === 1 ? { _signature: descriptor.signature } : {}),
    };
  }

  _descriptor(node) {
    const all = [...(this._catalog.functions || []), ...(this._catalog.special_forms || [])];
    return this._functionDescriptors.get(node.name)
      || all.find((item) => item.signature === node._signature)
      || all.find((item) => item.name === node.name)
      || { name: node.name, params: [], result: null, display: { label: node.name, category: "未知", color: "#64748B" } };
  }

  _renderNode(node, path) {
    const card = element("div", `fr-node fr-node--${node.node}`);
    let color = "#64748B";
    let title = node.node;
    let iconText = "•";
    let description = "";
    let descriptor = null;
    if (node.node === "call") {
      descriptor = this._descriptor(node);
      color = descriptor.display?.color || color;
      title = descriptor.display?.label || node.name;
      iconText = descriptor.display?.icon || "ƒ";
      description = descriptor.display?.description || descriptor.signature || "";
    } else if (SPECIAL_NODES.has(node.node)) {
      descriptor = (this._catalog.special_forms || []).find((item) => item.special === node.node);
      color = descriptor?.display?.color || "#7C3AED";
      title = descriptor?.display?.label || node.node;
      iconText = descriptor?.display?.icon || "ƒ";
      description = descriptor?.display?.description || "";
    } else {
      const valueDescriptor = VALUE_TEMPLATES.find((item) => item.node === node.node);
      if (valueDescriptor) {
        color = valueDescriptor.color;
        title = valueDescriptor.label;
        iconText = valueDescriptor.icon;
        description = valueDescriptor.description;
      }
    }
    card.style.setProperty("--fr-accent", color);
    const header = element("div", "fr-node__header");
    const drag = element("span", "fr-drag", "⠿");
    drag.draggable = true;
    drag.title = "拖动节点";
    drag.addEventListener("dragstart", (event) => {
      event.stopPropagation();
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData(MIME, JSON.stringify({ origin: "node", path }));
    });
    const icon = element("span", "fr-node__icon", iconText);
    const heading = element("span", "fr-node__heading");
    heading.append(element("strong", "", title));
    if (description) heading.append(element("small", "", description));
    const remove = element("button", "fr-node__remove", "×");
    remove.type = "button";
    remove.title = "删除节点";
    remove.addEventListener("click", () => this._removeAtPath(path));
    header.append(drag, icon, heading, remove);
    card.append(header);

    if (node.node === "call") card.append(this._renderCall(node, path, descriptor));
    else if (node.node === "switch") card.append(this._renderSwitch(node, path));
    else if (node.node === "for") card.append(this._renderFor(node, path));
    else if (node.node === "reduce") card.append(this._renderReduce(node, path));
    else if (node.node === "prog") card.append(this._renderProg(node, path));
    else if (node.node === "array") card.append(this._renderArray(node, path));
    else if (node.node === "dict") card.append(this._renderDict(node, path));
    else card.append(this._renderValueEditor(node, path));
    return card;
  }

  _renderCall(node, path, descriptor) {
    const body = element("div", "fr-node__body");
    const params = descriptor.params || [];
    const labels = descriptor.display?.parameters || [];
    (node.args || []).forEach((arg, index) => {
      const row = element("div", "fr-argument");
      const label = element("div", "fr-argument__label");
      label.append(element("span", "", labels[index]?.label || `参数 ${index + 1}`));
      label.append(element("code", "", typeName(params[index])));
      if (labels[index]?.description) label.title = labels[index].description;
      const argPath = [...path, "args", index];
      row.append(label, arg ? this._renderNode(arg, argPath) : this._dropZone(argPath, labels[index]?.placeholder || "拖入参数"));
      body.append(row);
    });
    if (descriptor.variadic) {
      const controls = element("div", "fr-inline-actions");
      const add = element("button", "fr-button fr-button--small", "+ 参数槽");
      add.type = "button";
      add.addEventListener("click", () => {
        node.args.push(null);
        this.render();
        this._emitChange();
      });
      const subtract = element("button", "fr-button fr-button--small fr-button--ghost", "− 参数槽");
      subtract.type = "button";
      subtract.disabled = node.args.length === 0;
      subtract.addEventListener("click", () => {
        node.args.pop();
        this.render();
        this._emitChange();
      });
      controls.append(add, subtract);
      body.append(controls);
    }
    return body;
  }

  _renderSwitch(node, path) {
    const body = element("div", "fr-node__body");
    const hint = node.value ? "所有匹配值与它同类型" : "留空则每个分支是 bool 条件";
    body.append(this._labeledSlot("待匹配值（可空）", hint, [...path, "value"], node.value));
    (node.cases || []).forEach((item, index) => {
      body.append(this._renderSwitchBranch(node, item, index, path));
    });
    const add = element("button", "fr-button fr-button--small", "+ 分支");
    add.type = "button";
    add.addEventListener("click", () => {
      node.cases.push({ match: [null], result: null });
      this.render();
      this._emitChange();
    });
    body.append(add, this._labeledSlot("默认结果（else）", "未匹配时返回", [...path, "default"], node.default));
    return body;
  }

  _renderSwitchBranch(node, item, index, path) {
    const group = element("div", "fr-special-group");
    const heading = element("div", "fr-special-group__header");
    heading.append(element("strong", "", `分支 ${index + 1}`));
    const remove = element("button", "fr-node__remove", "删除分支");
    remove.type = "button";
    remove.disabled = (node.cases || []).length <= 1;
    remove.addEventListener("click", () => {
      node.cases.splice(index, 1);
      this.render();
      this._emitChange();
    });
    heading.append(remove);
    group.append(heading);
    const label = node.value ? "匹配值" : "条件";
    const typeHint = node.value ? "与待匹配值同类型" : "bool";
    (item.match || []).forEach((match, matchIndex) => {
      const slotPath = [...path, "cases", index, "match", matchIndex];
      group.append(this._labeledSlot(`${label} ${matchIndex + 1}`, typeHint, slotPath, match));
    });
    group.append(this._matchControls(item));
    group.append(this._labeledSlot("返回结果", "所有结果同类型", [...path, "cases", index, "result"], item.result));
    return group;
  }

  // A branch may list several values or conditions; any one of them selects it.
  _matchControls(item) {
    const controls = element("div", "fr-inline-actions");
    const add = element("button", "fr-button fr-button--small", "+ 匹配值");
    add.type = "button";
    add.addEventListener("click", () => {
      item.match.push(null);
      this.render();
      this._emitChange();
    });
    const subtract = element("button", "fr-button fr-button--small fr-button--ghost", "− 匹配值");
    subtract.type = "button";
    subtract.disabled = (item.match || []).length <= 1;
    subtract.addEventListener("click", () => {
      item.match.pop();
      this.render();
      this._emitChange();
    });
    controls.append(add, subtract);
    return controls;
  }

  _renderFor(node, path) {
    const body = element("div", "fr-node__body");
    body.append(this._labeledSlot("输入数组", "array<T>", [...path, "source"], node.source));
    const variableRow = element("label", "fr-local-name");
    variableRow.append(element("span", "", "局部名称"));
    const variable = element("input", "fr-input");
    variable.value = node.variable || "item";
    variable.pattern = "[A-Za-z_][A-Za-z0-9_]*";
    variable.addEventListener("change", () => {
      const previous = node.variable || "item";
      node.variable = variable.value || "item";
      if (node.yield?.node === "var" && node.yield.name === previous) node.yield.name = node.variable;
      this.render();
      this._emitChange();
    });
    variableRow.append(variable);
    body.append(variableRow);
    body.append(this._labeledSlot("过滤条件（可空）", "bool；留空表示全部", [...path, "where"], node.where));
    body.append(this._labeledSlot("生成结果", "每个保留元素生成一个值", [...path, "yield"], node.yield));
    return body;
  }

  _renderReduce(node, path) {
    const body = element("div", "fr-node__body");
    body.append(this._labeledSlot("输入数组", "array<T>", [...path, "source"], node.source));
    body.append(this._localNameRow("元素局部名", node.variable || "item", (name) => {
      node.variable = name;
    }));
    body.append(this._localNameRow("累加器局部名", node.accumulator || "acc", (name) => {
      node.accumulator = name;
    }));
    body.append(this._labeledSlot("初始值", "累加器初值 R", [...path, "init"], node.init));
    body.append(this._labeledSlot("累加表达式", "必须返回 R", [...path, "body"], node.body));
    return body;
  }

  _renderProg(node, path) {
    const body = element("div", "fr-node__body");
    const steps = node.steps || [];
    steps.forEach((step, index) => {
      const last = index === steps.length - 1;
      const label = last ? `末步骤 ${index + 1}` : `步骤 ${index + 1}`;
      const hint = last ? "其类型即结果类型" : "求值后丢弃";
      body.append(this._labeledSlot(label, hint, [...path, "steps", index], step));
    });
    const controls = element("div", "fr-inline-actions");
    const add = element("button", "fr-button fr-button--small", "+ 步骤");
    add.type = "button";
    add.addEventListener("click", () => {
      node.steps.push(null);
      this.render();
      this._emitChange();
    });
    const subtract = element("button", "fr-button fr-button--small fr-button--ghost", "− 步骤");
    subtract.type = "button";
    subtract.disabled = steps.length <= 1;
    subtract.addEventListener("click", () => {
      node.steps.pop();
      this.render();
      this._emitChange();
    });
    controls.append(add, subtract);
    body.append(controls);
    return body;
  }

  // _localNameRow edits a locally bound name; the name is not a program
  // argument, so only this subtree sees it.
  _localNameRow(labelText, current, apply) {
    const row = element("label", "fr-local-name");
    row.append(element("span", "", labelText));
    const input = element("input", "fr-input");
    input.value = current;
    input.pattern = "[A-Za-z_][A-Za-z0-9_]*";
    input.addEventListener("change", () => {
      apply(input.value || current);
      this.render();
      this._emitChange();
    });
    row.append(input);
    return row;
  }

  _labeledSlot(labelText, typeText, path, value) {
    const row = element("div", "fr-argument");
    const label = element("div", "fr-argument__label");
    label.append(element("span", "", labelText), element("code", "", typeText));
    row.append(label, value ? this._renderNode(value, path) : this._dropZone(path, "拖入表达式"));
    return row;
  }

  _renderValueEditor(node) {
    const body = element("div", "fr-node__body fr-value-editor");
    let input;
    if (node.node === "bool") {
      input = element("select", "fr-input");
      for (const [label, value] of [["false", "false"], ["true", "true"]]) {
        const option = element("option", "", label);
        option.value = value;
        option.selected = node.bool === (value === "true");
        input.append(option);
      }
      input.addEventListener("change", () => { node.bool = input.value === "true"; this._emitChange(); });
    } else {
      input = element("input", "fr-input");
      if (node.node === "int") {
        input.type = "number";
        input.step = "1";
        input.value = String(node.int ?? 0);
        input.addEventListener("change", () => { node.int = Math.trunc(Number(input.value || 0)); this._emitChange(); });
      } else if (node.node === "float") {
        input.type = "text";
        input.inputMode = "decimal";
        input.value = String(node.float ?? "0.0");
        input.addEventListener("change", () => { node.float = input.value || "0.0"; this._emitChange(); });
      } else if (node.node === "string") {
        input.type = "text";
        input.value = node.string || "";
        input.placeholder = "字符串";
        input.addEventListener("change", () => { node.string = input.value; this._emitChange(); });
      } else {
        input.type = "text";
        input.value = node.name || "value";
        input.placeholder = "参数名";
        input.pattern = "[A-Za-z_][A-Za-z0-9_.@]*";
        input.addEventListener("change", () => { node.name = input.value || "value"; this._emitChange(); });
      }
    }
    body.append(input);
    return body;
  }

  _renderArray(node, path) {
    const body = element("div", "fr-node__body");
    (node.items || []).forEach((item, index) => {
      const itemPath = [...path, "items", index];
      const row = element("div", "fr-collection-row");
      row.append(element("span", "fr-index", String(index)), item ? this._renderNode(item, itemPath) : this._dropZone(itemPath, "数组元素"));
      body.append(row);
    });
    const add = element("button", "fr-button fr-button--small", "+ 元素");
    add.type = "button";
    add.addEventListener("click", () => { node.items.push(null); this.render(); this._emitChange(); });
    body.append(add);
    return body;
  }

  _renderDict(node, path) {
    const body = element("div", "fr-node__body");
    (node.entries || []).forEach((entry, index) => {
      const row = element("div", "fr-dict-row");
      const key = element("input", "fr-input fr-input--key");
      key.value = entry.key || "";
      key.placeholder = "key";
      key.addEventListener("change", () => { entry.key = key.value; this._emitChange(); });
      const valuePath = [...path, "entries", index, "value"];
      row.append(key, entry.value ? this._renderNode(entry.value, valuePath) : this._dropZone(valuePath, "字典值"));
      body.append(row);
    });
    const add = element("button", "fr-button fr-button--small", "+ 键值");
    add.type = "button";
    add.addEventListener("click", () => { node.entries.push({ key: "key", value: null }); this.render(); this._emitChange(); });
    body.append(add);
    return body;
  }

  _dropZone(path, label) {
    const zone = element("div", "fr-drop-zone", label);
    zone.addEventListener("dragover", (event) => {
      if (!event.dataTransfer.types.includes(MIME)) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "copy";
      zone.classList.add("is-over");
    });
    zone.addEventListener("dragleave", () => zone.classList.remove("is-over"));
    zone.addEventListener("drop", (event) => {
      event.preventDefault();
      event.stopPropagation();
      zone.classList.remove("is-over");
      let payload;
      try { payload = JSON.parse(event.dataTransfer.getData(MIME)); }
      catch { return; }
      if (payload.origin === "palette") {
        this._setAtPath(path, this._createTemplate(payload.templateId), false);
      } else if (payload.origin === "node") {
        const sourcePath = payload.path || [];
        if (this._isPrefix(sourcePath, path)) return;
        const moving = clone(this._getAtPath(sourcePath));
        this._setAtPath(sourcePath, null, false);
        this._setAtPath(path, moving, false);
      }
      this.render();
      this._emitChange();
    });
    return zone;
  }

  _getAtPath(path) {
    let current = this._root;
    for (const segment of path) current = current?.[segment];
    return current;
  }

  _setAtPath(path, value, notify = true) {
    if (path.length === 0) this._root = value;
    else {
      const parent = this._getAtPath(path.slice(0, -1));
      parent[path[path.length - 1]] = value;
    }
    if (notify) {
      this.render();
      this._emitChange();
    }
  }

  _removeAtPath(path) {
    if (path.length === 0) this._root = null;
    else this._setAtPath(path, null, false);
    this.render();
    this._emitChange();
  }

  _isPrefix(prefix, path) {
    return prefix.length <= path.length && prefix.every((part, index) => path[index] === part);
  }

  _emitChange() {
    this.dispatchEvent(new CustomEvent("funroute-change", {
      detail: { value: this.value, source: this.source }, bubbles: true,
    }));
  }
}

if (!customElements.get("funroute-designer")) customElements.define("funroute-designer", FunRouteDesigner);
