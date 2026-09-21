import { VALUE_TEMPLATES, blankFields, blankNode, clone, cleanNode, formatSource, indexNodes, logicalForm } from "./funroute-source.js";

const MIME = "application/x-funroute-node";

// Bumped with the Go side when switch cases became lists and the subject
// optional.
const EXPR_JSON_VERSION = 2;

// The derived forms are if nodes with a fixed branch; the palette offers them as
// their own cards and the canvas renders only the real operand slots.
const DERIVED_TEMPLATES = {
  and: () => ({ node: "call", name: "if", args: [null, null, { node: "bool", bool: false }] }),
  or: () => ({ node: "call", name: "if", args: [null, { node: "bool", bool: true }, null] }),
  not: () => ({ node: "call", name: "if", args: [null, { node: "bool", bool: false }, { node: "bool", bool: true }] }),
};

// The canvas renders a node from its schema, which the catalog carries from
// the compiler's own definitions: which fields it has, which hold expressions,
// names or lists. Only the wording is the front end's, and it lives here; a
// field without an entry is labelled by its name.
const FIELD_TEXT = {
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
  "reduce.accumulator": ["累加器局部名", "仅本节点可见"],
  "reduce.init": ["初始值", "累加器初值 R"],
  "reduce.body": ["累加表达式", "必须返回 R"],
  "let.bindings": ["绑定", "后续绑定与主体可引用"],
  "let.bindings.name": ["名称", "仅本节点可见"],
  "let.bindings.value": ["值", ""],
  "let.body": ["主体", "整体结果"],
};

const NAME_PATTERN = "[A-Za-z_][A-Za-z0-9_]*";

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function button(label, ghost, onClick) {
  const control = element("button", `fr-button fr-button--small${ghost ? " fr-button--ghost" : ""}`, label);
  control.type = "button";
  control.addEventListener("click", onClick);
  return control;
}

function fieldText(key, field) {
  const [label, hint] = FIELD_TEXT[key] || [field.name, ""];
  return { label: field.optional ? `${label}（可空）` : label, hint };
}

export class FunRouteDesigner extends HTMLElement {
  constructor() {
    super();
    this._catalog = { functions: [], special_forms: [] };
    this._nodes = new Map();
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
    this._nodes = indexNodes(this._catalog.nodes);
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
    return { version: EXPR_JSON_VERSION, expr: cleanNode(this._root, this._nodes) };
  }

  get source() { return formatSource(this._root); }

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

  // A fresh node comes from its schema: a value or a lazy form is blankNode of
  // its definition, a function call has one empty slot per parameter, and a
  // derived form is its fixed if shape.
  _createTemplate(templateId) {
    const template = this._templates.get(templateId);
    if (!template) throw new Error(`未知模板 ${templateId}`);
    const descriptor = template.descriptor;
    if (template.kind === "value") return blankNode(this._schema(descriptor.node));
    if (DERIVED_TEMPLATES[descriptor.special]) return DERIVED_TEMPLATES[descriptor.special]();
    // A lazy form is its own node; the kernel's if is special too, but it is
    // still a call.
    if (this._nodes.has(descriptor.special)) return blankNode(this._schema(descriptor.special));
    const arity = descriptor.variadic ? 1 : (descriptor.params || []).length;
    return {
      node: "call",
      name: descriptor.name,
      args: Array(arity).fill(null),
      ...(descriptor.overloads?.length === 1 ? { _signature: descriptor.signature } : {}),
    };
  }

  _schema(node) {
    const schema = this._nodes.get(node);
    if (!schema) throw new Error(`目录没有描述节点 ${node}`);
    return schema;
  }

  _descriptor(node) {
    const all = [...(this._catalog.functions || []), ...(this._catalog.special_forms || [])];
    return this._functionDescriptors.get(node.name)
      || all.find((item) => item.signature === node._signature)
      || all.find((item) => item.name === node.name)
      || { name: node.name, params: [], result: null, display: { label: node.name, category: "未知", color: "#64748B" } };
  }

  _specialDescriptor(special) {
    return (this._catalog.special_forms || []).find((item) => item.special === special) || null;
  }

  // _presentation is the card's colour, title, icon and blurb: from the
  // function catalog for a call, from the special-form catalog for a lazy form,
  // from the value templates for the rest.
  _presentation(node, logical) {
    if (node.node === "call") {
      const descriptor = logical ? this._specialDescriptor(logical.kind) : this._descriptor(node);
      const display = descriptor?.display || {};
      return { descriptor, color: display.color, title: display.label || node.name, icon: display.icon || "ƒ",
        description: display.description || descriptor?.signature || "" };
    }
    const special = this._specialDescriptor(node.node);
    if (special) {
      const display = special.display || {};
      return { color: display.color || "#7C3AED", title: display.label || node.node, icon: display.icon || "ƒ", description: display.description || "" };
    }
    const value = VALUE_TEMPLATES.find((item) => item.node === node.node);
    return value ? { color: value.color, title: value.label, icon: value.icon, description: value.description }
      : { title: node.node, icon: "•", description: "" };
  }

  _renderNode(node, path) {
    const card = element("div", `fr-node fr-node--${node.node}`);
    const logical = logicalForm(node);
    const look = this._presentation(node, logical);
    card.style.setProperty("--fr-accent", look.color || "#64748B");
    const header = element("div", "fr-node__header");
    const drag = element("span", "fr-drag", "⠿");
    drag.draggable = true;
    drag.title = "拖动节点";
    drag.addEventListener("dragstart", (event) => {
      event.stopPropagation();
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData(MIME, JSON.stringify({ origin: "node", path }));
    });
    const heading = element("span", "fr-node__heading");
    heading.append(element("strong", "", look.title));
    if (look.description) heading.append(element("small", "", look.description));
    const remove = element("button", "fr-node__remove", "×");
    remove.type = "button";
    remove.title = "删除节点";
    remove.addEventListener("click", () => this._removeAtPath(path));
    header.append(drag, element("span", "fr-node__icon", look.icon), heading, remove);
    card.append(header, this._renderBody(node, path, logical, look.descriptor));
    return card;
  }

  // A call is laid out by the function catalog, a literal or variable by its
  // editor, and every other node by its schema.
  _renderBody(node, path, logical, descriptor) {
    if (logical) return this._renderLogical(node, path, logical, descriptor);
    if (node.node === "call") return this._renderCall(node, path, descriptor);
    const schema = this._schema(node.node);
    if (schema.fields.every((field) => !["expr", "exprs", "list"].includes(field.kind))) {
      return this._renderValueEditor(node, schema);
    }
    const body = element("div", "fr-node__body");
    this._renderFields(body, node, schema.fields, path, node.node);
    return body;
  }

  // A derived form shows only its operands; the fixed branch stays hidden so it
  // cannot be edited into something that is no longer an and/or/not.
  _renderLogical(node, path, form, descriptor) {
    const body = element("div", "fr-node__body");
    const labels = descriptor?.display?.parameters || [];
    form.slots.forEach((argIndex, slot) => {
      const label = labels[slot]?.label || `条件 ${slot + 1}`;
      body.append(this._labeledSlot(label, "bool", [...path, "args", argIndex], node.args[argIndex]));
    });
    return body;
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
    if (descriptor.variadic) body.append(this._listControls(node.args, 0, () => null));
    return body;
  }

  // _renderFields lays out one struct — a node or a list item — field by field.
  // key prefixes the wording lookup, so "let.bindings.name" finds its label.
  _renderFields(body, target, fields, path, key) {
    for (const field of fields) {
      const text = fieldText(`${key}.${field.name}`, field);
      const fieldPath = [...path, field.name];
      switch (field.kind) {
        case "expr":
          body.append(this._labeledSlot(text.label, text.hint, fieldPath, target[field.name]));
          break;
        case "exprs":
          this._renderExprs(body, target, field, fieldPath, text);
          break;
        case "list":
          this._renderList(body, target, field, fieldPath, `${key}.${field.name}`, text);
          break;
        default:
          body.append(this._nameRow(text, target, field));
      }
    }
  }

  _renderExprs(body, target, field, path, text) {
    const items = target[field.name] || (target[field.name] = []);
    items.forEach((item, index) => {
      body.append(this._labeledSlot(`${text.label} ${index + 1}`, text.hint, [...path, index], item));
    });
    body.append(this._listControls(items, field.min || 0, () => null));
  }

  _renderList(body, target, field, path, key, text) {
    const items = target[field.name] || (target[field.name] = []);
    items.forEach((item, index) => {
      const group = element("div", "fr-special-group");
      const heading = element("div", "fr-special-group__header");
      heading.append(element("strong", "", `${text.label} ${index + 1}`));
      const remove = element("button", "fr-node__remove", "删除");
      remove.type = "button";
      remove.disabled = items.length <= (field.min || 0);
      remove.addEventListener("click", () => { items.splice(index, 1); this._changed(); });
      heading.append(remove);
      group.append(heading);
      this._renderFields(group, item, field.fields, [...path, index], key);
      body.append(group);
    });
    body.append(this._listControls(items, field.min || 0, () => blankFields(field.fields), text.label));
  }

  // _listControls adds and removes items of a list whose length the schema
  // bounds from below.
  _listControls(items, min, blank, label = "") {
    const controls = element("div", "fr-inline-actions");
    const add = button(`+ ${label || "项"}`, false, () => { items.push(blank()); this._changed(); });
    const subtract = button(`− ${label || "项"}`, true, () => { items.pop(); this._changed(); });
    subtract.disabled = items.length <= min;
    controls.append(add, subtract);
    return controls;
  }

  // _nameRow edits a name field. A local is only visible in this node; an
  // optional one left empty is omitted, which for a loop means an array walk.
  _nameRow(text, target, field) {
    const row = element("label", "fr-local-name");
    row.append(element("span", "", text.label));
    const input = element("input", "fr-input");
    input.value = target[field.name] || "";
    input.placeholder = field.optional ? "留空 = 不使用" : (field.default || "");
    if (field.kind === "name") input.pattern = NAME_PATTERN;
    input.addEventListener("change", () => {
      const name = input.value.trim();
      if (name) target[field.name] = name;
      else if (field.optional) delete target[field.name];
      else target[field.name] = field.default || "";
      this._changed();
    });
    row.append(input);
    if (text.hint) row.title = text.hint;
    return row;
  }

  _changed() {
    this.render();
    this._emitChange();
  }

  _labeledSlot(labelText, typeText, path, value) {
    const row = element("div", "fr-argument");
    const label = element("div", "fr-argument__label");
    label.append(element("span", "", labelText), element("code", "", typeText));
    row.append(label, value ? this._renderNode(value, path) : this._dropZone(path, "拖入表达式"));
    return row;
  }

  // Literals and variables are single fields with their own input.
  _renderValueEditor(node, schema) {
    const body = element("div", "fr-node__body fr-value-editor");
    const field = schema.fields[0];
    let input;
    if (field.kind === "bool") {
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
      input.type = field.kind === "int" ? "number" : "text";
      if (field.kind === "int") input.step = "1";
      if (field.kind === "float") input.inputMode = "decimal";
      if (field.kind === "name") input.pattern = "[A-Za-z_][A-Za-z0-9_.]*";
      input.placeholder = field.kind === "name" ? "参数名" : field.kind;
      input.value = String(node[field.name] ?? field.default ?? "");
      input.addEventListener("change", () => {
        node[field.name] = field.kind === "int" ? Math.trunc(Number(input.value || 0)) : (input.value || field.default || "");
        this._emitChange();
      });
    }
    body.append(input);
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
      this._changed();
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
    if (notify) this._changed();
  }

  _removeAtPath(path) {
    if (path.length === 0) this._root = null;
    else this._setAtPath(path, null, false);
    this._changed();
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

function typeName(type) {
  if (!type) return "动态";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeName(type.elem)}>`;
  return type.name || type.kind || "动态";
}

if (!customElements.get("funroute-designer")) {
  customElements.define("funroute-designer", FunRouteDesigner);
}
