import { FunRouteLanguage, VALUE_TEMPLATES, blankFields, blankNode, clone, parseInputValue, typeName } from "./funroute-core.js";
import { renderSemanticTree } from "./funroute-semantic.js";

const MIME = "application/x-funroute-node";

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
    this._language = null;
    this._root = null;
    this._templates = new Map();
    this._functionDescriptors = new Map();
    this._search = "";
    this._paletteScroll = 0;
    this._mode = "guide";
    this._focusPath = [];
    this._selectedTemplateId = null;
    this._runtimeContract = null;
    this._validation = { phase: "unchecked", message: "请先检查运行契约" };
  }

  connectedCallback() {
    this.classList.add("fr-designer");
    this.render();
  }

  set catalog(value) {
    this._catalog = clone(value || { functions: [], special_forms: [] });
    this._language = new FunRouteLanguage(this._catalog);
    this._nodes = this._language.nodes;
    this._rebuildTemplates();
    this.render();
  }

  get catalog() { return clone(this._catalog); }

  set runtimeContract(value) {
    this._runtimeContract = value?.valid ? clone(value) : null;
    this.render();
  }

  get runtimeContract() { return clone(this._runtimeContract); }

  set validation(value) {
    this._validation = { phase: "unchecked", message: "等待契约检查", ...(value || {}) };
    this.render();
  }

  get validation() { return clone(this._validation); }

  set value(documentValue) {
    const expression = documentValue?.expr || documentValue || null;
    this._root = expression ? clone(expression) : null;
    this._focusPath = [];
    this.render();
  }

  get value() {
    if (!this._language) throw new Error("请先设置 catalog");
    return this._language.document(this._root);
  }

  get source() { return this._language?.formatSource(this._root) || ""; }

  clear() {
    this._root = null;
    this._focusPath = [];
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
    this.classList.toggle("is-guide", this._mode === "guide");
    const shell = element("div", "fr-shell");
    const palette = element("aside", "fr-palette");
    const paletteHeader = element("div", "fr-palette__header");
    paletteHeader.append(element("strong", "", "添加组件"));
    const search = element("input", "fr-search");
    search.type = "search";
    search.placeholder = "搜索；点击选择后放入空槽";
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
    title.append(element("strong", "", this._mode === "guide" ? "策略步骤" : "完整表达式树"));
    title.append(element("span", "fr-muted", this._mode === "guide"
      ? "一次只编辑一个步骤，点击结构导航切换"
      : "按真实语法阅读整体；点击表达式进入对应步骤编辑"));
    const actions = element("div", "fr-workspace__actions");
    actions.append(this._modeButton("引导视图", "guide"), this._modeButton("完整树", "tree"));
    const clearButton = element("button", "fr-button fr-button--ghost", "清空");
    clearButton.type = "button";
    clearButton.addEventListener("click", () => this.clear());
    actions.append(clearButton);
    workHeader.append(title, actions);
    const canvas = element("div", "fr-canvas");
    if (this._root && this._mode === "guide") canvas.append(this._renderGuide());
    else if (this._root) canvas.append(this._renderNode(this._root, []));
    else canvas.append(this._dropZone([], "把函数、变量或立即值拖到这里"));
    work.append(workHeader, this._renderContractGuard(), canvas);
    shell.append(palette, work);
    this.replaceChildren(shell);
    list.scrollTop = this._paletteScroll;
  }

  _modeButton(label, mode) {
    const control = element("button", `fr-view-button${this._mode === mode ? " is-active" : ""}`, label);
    control.type = "button";
    control.addEventListener("click", () => { this._mode = mode; this.render(); });
    return control;
  }

  _renderContractGuard() {
    const phase = this._validation?.phase || "unchecked";
    const guard = element("div", `fr-canvas-contract is-${phase}`);
    const copy = element("div", "fr-canvas-contract__copy");
    copy.append(element("strong", "", "运行契约约束"), element("span", "", this._validation?.message || "等待检查"));
    const args = this._runtimeContract?.args || [];
    const result = typeName(this._runtimeContract?.result);
    const summary = this._runtimeContract
      ? `${args.length} 个可用参数 · 必须返回 ${result}`
      : "契约未检查 · 只能使用当前作用域的本地变量";
    guard.append(element("span", "fr-canvas-contract__dot"), copy, element("code", "", summary));
    return guard;
  }

  _renderGuide() {
    const entries = this._outlineEntries();
    const focused = this._getAtPath(this._focusPath);
    if (!focused?.node) this._focusPath = [];
    const current = this._getAtPath(this._focusPath) || this._root;
    const guide = element("div", "fr-guide");
    const outline = element("nav", "fr-outline");
    const outlineHead = element("div", "fr-outline__header");
    outlineHead.append(element("strong", "", "策略结构"), element("span", "", `${entries.length} 个步骤`));
    const list = element("div", "fr-outline__list");
    for (const entry of entries) list.append(this._outlineItem(entry));
    outline.append(outlineHead, list);
    const focus = element("section", "fr-focus");
    const breadcrumbs = element("div", "fr-breadcrumbs");
    const ancestors = entries.filter((entry) => this._isPrefix(entry.path, this._focusPath));
    ancestors.forEach((entry, index) => {
      if (index) breadcrumbs.append(element("span", "", "›"));
      const look = this._presentation(entry.node, this._language.logicalForm(entry.node));
      const crumb = button(look.title, true, () => { this._focusPath = entry.path; this.render(); });
      breadcrumbs.append(crumb);
    });
    focus.append(breadcrumbs, this._renderNode(current, this._focusPath, true));
    guide.append(outline, focus);
    return guide;
  }

  _outlineEntries() {
    const entries = [];
    const walkObject = (object, path, depth) => {
      for (const [key, value] of Object.entries(object || {})) {
        if (key.startsWith("_")) continue;
        if (value?.node) walk(value, [...path, key], depth);
        else if (Array.isArray(value)) value.forEach((item, index) => {
          if (item?.node) walk(item, [...path, key, index], depth);
          else if (item && typeof item === "object") walkObject(item, [...path, key, index], depth);
        });
      }
    };
    const walk = (node, path, depth) => {
      entries.push({ node, path, depth });
      walkObject(node, path, depth + 1);
    };
    walk(this._root, [], 0);
    return entries;
  }

  _outlineItem(entry) {
    const logical = this._language.logicalForm(entry.node);
    const look = this._presentation(entry.node, logical);
    const active = this._samePath(entry.path, this._focusPath);
    const control = element("button", `fr-outline__item${active ? " is-active" : ""}`);
    control.type = "button";
    control.style.setProperty("--fr-indent", `${Math.min(entry.depth, 5) * 8}px`);
    control.style.setProperty("--fr-accent", look.color || "#64748B");
    control.append(element("span", "fr-outline__icon", look.icon), element("strong", "", look.title));
    const source = this._language.expressionSource(entry.node);
    control.append(element("code", "", source.length > 52 ? `${source.slice(0, 49)}…` : source));
    control.addEventListener("click", () => { this._focusPath = entry.path; this.render(); });
    return control;
  }

  _samePath(left, right) {
    return left.length === right.length && left.every((part, index) => part === right[index]);
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
    if (this._selectedTemplateId === templateId) card.classList.add("is-selected");
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
    card.addEventListener("click", () => {
      if (!this._root) {
        const created = this._createTemplate(templateId, []);
        if (!created) {
          this._validation = { phase: "error", message: "运行契约没有可用入参；请先声明并检查入参，或在局部作用域内使用参数节点。" };
          this.render();
          return;
        }
        this._root = created;
        this._focusPath = [];
        this._selectedTemplateId = null;
        this.render();
        this._emitChange();
        return;
      }
      this._selectedTemplateId = this._selectedTemplateId === templateId ? null : templateId;
      this.render();
    });
    return card;
  }

  _createTemplate(templateId, path = []) {
    const template = this._templates.get(templateId);
    if (!template) throw new Error(`未知模板 ${templateId}`);
    const descriptor = template.descriptor;
    let created;
    if (template.kind === "value") created = blankNode(this._schema(descriptor.node));
    else if (this._language.operators.some((item) => item.form === descriptor.special)) {
      created = this._language.createForm(descriptor.special);
    } else if (this._nodes.has(descriptor.special)) {
      created = blankNode(this._schema(descriptor.special));
    } else {
      const arity = descriptor.variadic ? 1 : (descriptor.params || []).length;
      created = {
        node: "call",
        name: descriptor.name,
        args: Array(arity).fill(null),
        ...(descriptor.overloads?.length === 1 ? { _signature: descriptor.signature } : {}),
      };
    }
    if (created.node === "var") {
      const first = this._scopeAtPath(path)[0];
      if (!first) return null;
      created.name = first.name;
    }
    return created;
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

  _editSemanticNode(path) {
    this._mode = "guide";
    this._focusPath = path;
    this.render();
  }

  _renderNode(node, path, compactChildren = false) {
    if (!compactChildren) {
      return renderSemanticTree({
        language: this._language,
        presentation: (target) => this._presentation(target, this._language.logicalForm(target)),
        descriptor: (target) => this._descriptor(target),
        edit: (targetPath) => this._editSemanticNode(targetPath),
        dropZone: (targetPath, label) => this._dropZone(targetPath, label),
      }, node, path);
    }
    const card = element("div", `fr-node fr-node--${node.node}${compactChildren ? " fr-node--focused" : ""}`);
    const logical = this._language.logicalForm(node);
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
    card.append(header, this._renderBody(node, path, logical, look.descriptor, compactChildren));
    return card;
  }

  _renderBody(node, path, logical, descriptor, compactChildren) {
    if (logical) return this._renderLogical(node, path, logical, descriptor, compactChildren);
    if (node.node === "call") return this._renderCall(node, path, descriptor, compactChildren);
    const schema = this._schema(node.node);
    if (schema.fields.every((field) => !["expr", "exprs", "list"].includes(field.kind))) {
      return this._renderValueEditor(node, schema, path);
    }
    const body = element("div", "fr-node__body");
    this._renderFields(body, node, schema.fields, path, node.node, compactChildren);
    return body;
  }

  _renderLogical(node, path, form, descriptor, compactChildren) {
    const body = element("div", "fr-node__body");
    const labels = descriptor?.display?.parameters || [];
    form.paths.forEach((relativePath, slot) => {
      const label = labels[slot]?.label || `条件 ${slot + 1}`;
      const value = form.operands[slot];
      body.append(this._labeledSlot(label, "bool", [...path, ...relativePath], value, compactChildren));
    });
    return body;
  }

  _renderCall(node, path, descriptor, compactChildren) {
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
      row.append(label, arg ? this._childEditor(arg, argPath, compactChildren) : this._dropZone(argPath, labels[index]?.placeholder || "拖入参数"));
      body.append(row);
    });
    if (descriptor.variadic) body.append(this._listControls(node.args, 0, () => null));
    return body;
  }

  _renderFields(body, target, fields, path, key, compactChildren) {
    for (const field of fields) {
      const text = fieldText(`${key}.${field.name}`, field);
      const fieldPath = [...path, field.name];
      switch (field.kind) {
        case "expr":
          body.append(this._labeledSlot(text.label, text.hint, fieldPath, target[field.name], compactChildren));
          break;
        case "exprs":
          this._renderExprs(body, target, field, fieldPath, text, compactChildren);
          break;
        case "list":
          this._renderList(body, target, field, fieldPath, `${key}.${field.name}`, text, compactChildren);
          break;
        default:
          body.append(this._nameRow(text, target, field));
      }
    }
  }

  _renderExprs(body, target, field, path, text, compactChildren) {
    const items = target[field.name] || (target[field.name] = []);
    items.forEach((item, index) => {
      body.append(this._labeledSlot(`${text.label} ${index + 1}`, text.hint, [...path, index], item, compactChildren));
    });
    body.append(this._listControls(items, field.min || 0, () => null));
  }

  _renderList(body, target, field, path, key, text, compactChildren) {
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
      this._renderFields(group, item, field.fields, [...path, index], key, compactChildren);
      body.append(group);
    });
    body.append(this._listControls(items, field.min || 0, () => blankFields(field.fields), text.label));
  }

  _listControls(items, min, blank, label = "") {
    const controls = element("div", "fr-inline-actions");
    const add = button(`+ ${label || "项"}`, false, () => { items.push(blank()); this._changed(); });
    const subtract = button(`− ${label || "项"}`, true, () => { items.pop(); this._changed(); });
    subtract.disabled = items.length <= min;
    controls.append(add, subtract);
    return controls;
  }

  _nameRow(text, target, field) {
    const row = element("label", "fr-local-name");
    row.append(element("span", "", text.label));
    const input = element("input", "fr-input");
    input.value = target[field.name] || "";
    input.placeholder = field.optional ? "留空 = 不使用" : (field.default || "");
    if (field.kind === "name") input.pattern = this._language.namePattern;
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
    this._validation = { phase: "dirty", message: "画布已修改，正在检查参数作用域和返回类型…" };
    this.render();
    this._emitChange();
  }

  _labeledSlot(labelText, typeText, path, value, compactChildren = false) {
    const row = element("div", "fr-argument");
    const label = element("div", "fr-argument__label");
    label.append(element("span", "", labelText), element("code", "", typeText));
    row.append(label, value ? this._childEditor(value, path, compactChildren) : this._dropZone(path, "拖入表达式"));
    return row;
  }

  _childEditor(node, path, compact) {
    return compact ? this._focusLink(node, path) : this._renderNode(node, path);
  }

  _focusLink(node, path) {
    const logical = this._language.logicalForm(node);
    const look = this._presentation(node, logical);
    const control = element("button", "fr-focus-link");
    control.type = "button";
    control.style.setProperty("--fr-accent", look.color || "#64748B");
    const text = element("span", "fr-focus-link__text");
    text.append(element("strong", "", look.title));
    const source = this._language.expressionSource(node);
    text.append(element("code", "", source.length > 72 ? `${source.slice(0, 69)}…` : source));
    control.append(element("span", "fr-focus-link__icon", look.icon), text, element("span", "fr-focus-link__action", "编辑 →"));
    control.addEventListener("click", () => { this._focusPath = path; this.render(); });
    return control;
  }

  _renderValueEditor(node, schema, path) {
    const body = element("div", "fr-node__body fr-value-editor");
    const field = schema.fields[0];
    let input;
    if (node.node === "var") {
      return this._renderVariableEditor(node, path);
    } else if (field.kind === "bool") {
      input = element("select", "fr-input");
      for (const [label, value] of [["false", "false"], ["true", "true"]]) {
        const option = element("option", "", label);
        option.value = value;
        option.selected = node.bool === (value === "true");
        input.append(option);
      }
      input.addEventListener("change", () => { node.bool = input.value === "true"; this._changed(); });
    } else {
      input = element("input", "fr-input");
      input.type = field.kind === "int" ? "number" : "text";
      if (field.kind === "int") input.step = "1";
      if (field.kind === "float") input.inputMode = "decimal";
      if (field.kind === "name") input.pattern = this._language.namePattern;
      input.placeholder = field.kind === "name" ? "参数名" : field.kind;
      input.value = String(node[field.name] ?? field.default ?? "");
      input.addEventListener("change", () => {
        if (field.kind === "int" || field.kind === "float") {
          try {
            const value = parseInputValue(input.value || "0", { kind: field.kind });
            input.setCustomValidity("");
            node[field.name] = field.kind === "int" ? value : input.value;
          } catch (error) {
            input.setCustomValidity(error.message);
            input.reportValidity();
            return;
          }
        } else node[field.name] = input.value || field.default || "";
        this._changed();
      });
    }
    body.append(input);
    return body;
  }

  _renderVariableEditor(node, path) {
    const body = element("div", "fr-node__body fr-value-editor fr-variable-editor");
    const select = element("select", "fr-input fr-scope-select");
    const choices = this._scopeAtPath(path);
    const valid = choices.some((choice) => choice.name === node.name);
    if (!valid && node.name) {
      const invalid = element("option", "", `⚠ ${node.name}（不在运行契约或当前作用域）`);
      invalid.value = node.name;
      invalid.selected = true;
      invalid.disabled = true;
      select.append(invalid);
      body.classList.add("is-invalid");
    }
    if (!choices.length) {
      const empty = element("option", "", "没有可用参数");
      empty.value = "";
      empty.selected = true;
      empty.disabled = true;
      select.append(empty);
      select.disabled = true;
      body.classList.add("is-invalid");
    } else {
      for (const [source, label] of [["contract", "运行契约"], ["local", "本地变量"]]) {
        const groupChoices = choices.filter((choice) => choice.source === source);
        if (!groupChoices.length) continue;
        const group = element("optgroup");
        group.label = label;
        for (const choice of groupChoices) {
          const option = element("option", "", `${choice.name}${choice.type ? ` · ${typeName(choice.type)}` : ""}`);
          option.value = choice.name;
          option.selected = choice.name === node.name;
          group.append(option);
        }
        select.append(group);
      }
    }
    select.addEventListener("change", () => { node.name = select.value; this._changed(); });
    const selected = choices.find((choice) => choice.name === node.name);
    const hint = selected
      ? (selected.source === "local" ? "本地变量 · 类型由编译器推导" : `运行契约 · ${typeName(selected.type)}${selected.doc ? ` · ${selected.doc}` : ""}`)
      : (choices.length ? "请选择运行契约或当前作用域中的变量" : "请先在运行契约声明入参，或在局部作用域内使用");
    body.append(select, element("span", "fr-scope-hint", hint));
    return body;
  }

  _scopeAtPath(path) {
    return this._language?.scopeAtPath(this._root, path, this._runtimeContract?.args || []) || [];
  }

  _dropZone(path, label) {
    const selected = this._templates.get(this._selectedTemplateId);
    const selectedLabel = selected?.descriptor?.label || selected?.descriptor?.display?.label;
    const zone = element("div", `fr-drop-zone${selected ? " is-ready" : ""}`,
      selected ? `点击放入「${selectedLabel || "已选组件"}」` : label);
    zone.addEventListener("click", () => {
      if (!this._selectedTemplateId) return;
      const created = this._createTemplate(this._selectedTemplateId, path);
      if (!created) {
        this._validation = { phase: "error", message: "这个位置没有可用的契约参数或本地变量。" };
        this.render();
        return;
      }
      this._setAtPath(path, created, false);
      this._selectedTemplateId = null;
      if (this._mode === "guide") this._focusPath = path;
      this._changed();
    });
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
        const created = this._createTemplate(payload.templateId, path);
        if (!created) {
          this._validation = { phase: "error", message: "这个位置没有可用的契约参数或本地变量。" };
          this.render();
          return;
        }
        this._setAtPath(path, created, false);
      } else if (payload.origin === "node") {
        const sourcePath = payload.path || [];
        if (this._isPrefix(sourcePath, path)) return;
        const moving = clone(this._getAtPath(sourcePath));
        this._setAtPath(sourcePath, null, false);
        this._setAtPath(path, moving, false);
      }
      this._selectedTemplateId = null;
      if (this._mode === "guide") this._focusPath = path;
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
    this._focusPath = this._nearestNodePath(path.slice(0, -1));
    this._changed();
  }

  _nearestNodePath(path) {
    const candidate = [...path];
    while (candidate.length && !this._getAtPath(candidate)?.node) candidate.pop();
    return this._getAtPath(candidate)?.node ? candidate : [];
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

if (!customElements.get("funroute-designer")) {
  customElements.define("funroute-designer", FunRouteDesigner);
}
