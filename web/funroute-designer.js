import { FunRouteLanguage, blankFields, blankNode, clone, contractEnums, isPathPrefix, isPlainExpression, samePath } from "./funroute-core.js";
import { lookFor, typeName } from "./funroute-display.js";
import { enumMemberField, expandedSlot, expressionRow, fieldText, parseInputValue, valueEditor } from "./funroute-fields.js";
import { DND_MIME, dropTarget, placeBlock } from "./funroute-dnd.js";
import { renderPalette } from "./funroute-palette.js";




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
    this._focusPath = [];
    this._selectedTemplateId = null;
    this._expanded = new Set();
    this._activeInput = null;
    // parseExpression is injected by the host: the canvas never talks to a
    // server, and the Go parser stays the only grammar authority.
    this.parseExpression = null;
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
    this._expanded.clear();
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
      const display = clone(first.doc || {});
      if (overloads.length > 1 && display.category === "类型转换") {
        display.description = `${display.label}；编译器会根据输入自动选择匹配的转换签名。`;
      }
      const descriptor = {
        ...first,
        name,
        params,
        result: sameType(results) ? results[0] : null,
        signature: overloads.length === 1 ? first.signature : `${name}(${overloads.length} 个类型签名)`,
        doc: display,
        overloads,
      };
      this._functionDescriptors.set(name, descriptor);
      if (this._language.controlBlocks.has(name)) this._templates.set(`fn:${index}`, { kind: "function", descriptor });
      index += 1;
    }
    for (const [index, descriptor] of (this._catalog.special_forms || []).entries()) {
      if (this._language.controlBlocks.has(descriptor.name)) this._templates.set(`special:${index}`, { kind: "function", descriptor });
    }
  }

  render() {
    if (!this.isConnected) return;
    const shell = element("div", "fr-shell");
    const { palette, list } = renderPalette({
      templates: this._templates,
      search: this._search,
      selectedId: this._selectedTemplateId,
      onSearch: (text) => { this._search = text; this.render(); },
      onSelect: (templateId) => this._selectTemplate(templateId),
    });
    list.addEventListener("scroll", () => { this._paletteScroll = list.scrollTop; }, { passive: true });

    const work = element("section", "fr-workspace");
    const workHeader = element("div", "fr-workspace__header");
    const title = element("div");
    title.append(element("strong", "", "策略步骤"));
    title.append(element("span", "fr-muted", "一次只编辑一个步骤，点击左侧结构导航切换"));
    const actions = element("div", "fr-workspace__actions");
    const clearButton = element("button", "fr-button fr-button--ghost", "清空");
    clearButton.type = "button";
    clearButton.addEventListener("click", () => this.clear());
    actions.append(clearButton);
    workHeader.append(title, actions);
    const canvas = element("div", "fr-canvas");
    if (this._root) canvas.append(this._renderGuide());
    else canvas.append(this._slotEditor([], null, true));
    work.append(workHeader, this._renderContractGuard(), canvas);
    shell.append(palette, work);
    this.replaceChildren(shell);
    list.scrollTop = this._paletteScroll;
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
    const guide = element("div", `fr-guide${entries.length > 1 ? "" : " fr-guide--single"}`);
    const outline = element("nav", "fr-outline");
    const outlineHead = element("div", "fr-outline__header");
    outlineHead.append(element("strong", "", "策略结构"), element("span", "", `${entries.length} 个步骤`));
    const list = element("div", "fr-outline__list");
    for (const entry of entries) list.append(this._outlineItem(entry));
    outline.append(outlineHead, list);
    const focus = element("section", "fr-focus");
    const breadcrumbs = element("div", "fr-breadcrumbs");
    const ancestors = entries.filter((entry) => isPathPrefix(entry.path, this._focusPath));
    ancestors.forEach((entry, index) => {
      if (index) breadcrumbs.append(element("span", "", "›"));
      const look = this._presentation(entry.node, this._language.logicalForm(entry.node));
      const crumb = button(look.title, true, () => { this._focusPath = entry.path; this._expanded.clear(); this.render(); });
      breadcrumbs.append(crumb);
    });
    focus.append(breadcrumbs, this._renderNode(current, this._focusPath, true));
    if (entries.length > 1) guide.append(outline);
    guide.append(focus);
    return guide;
  }

  // The outline lists steps, not nodes: the root plus every structure that
  // carries control flow. A plain expression is edited in place as text, so
  // listing its leaves here would just repeat the card beside it.
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
      const step = path.length === 0 || !this._plain(node);
      if (step) entries.push({ node, path, depth });
      if (!this._plain(node)) walkObject(node, path, depth + (step ? 1 : 0));
    };
    walk(this._root, [], 0);
    return entries;
  }

  _outlineItem(entry) {
    const logical = this._language.logicalForm(entry.node);
    const look = this._presentation(entry.node, logical);
    const active = samePath(entry.path, this._focusPath);
    const control = element("button", `fr-outline__item${active ? " is-active" : ""}`);
    control.type = "button";
    control.style.setProperty("--fr-indent", `${Math.min(entry.depth, 5) * 8}px`);
    control.style.setProperty("--fr-accent", look.color || "#64748B");
    control.append(element("span", "fr-outline__icon", look.icon), element("strong", "", look.title));
    const source = this._language.expressionSource(entry.node);
    control.append(element("code", "", source.length > 52 ? `${source.slice(0, 49)}…` : source));
    control.addEventListener("click", () => { this._focusPath = entry.path; this._expanded.clear(); this.render(); });
    return control;
  }

  _selectTemplate(templateId) {
    if (this._root) {
      this._selectedTemplateId = this._selectedTemplateId === templateId ? null : templateId;
      this.render();
      return;
    }
    const created = this._createTemplate(templateId, []);
    if (!created) {
      this._validation = { phase: "error", message: "运行契约没有可用入参；请先声明并检查入参。" };
      this.render();
      return;
    }
    this._root = created;
    this._focusPath = [];
    this._selectedTemplateId = null;
    this._changed();
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
      const arity = (descriptor.params || []).length;
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

  _plain(node) { return isPlainExpression(node, this._language); }

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

  // The catalog says what a node means; funroute-display says how it looks.
  _presentation(node, logical) {
    const name = logical ? logical.kind : (node.node === "call" ? node.name : node.node);
    const look = lookFor(name);
    if (node.node === "call") {
      const descriptor = logical ? this._specialDescriptor(logical.kind) : this._descriptor(node);
      const display = descriptor?.doc || {};
      return { descriptor, ...look, title: display.label || node.name,
        description: display.description || descriptor?.signature || "" };
    }
    const display = this._specialDescriptor(node.node)?.doc;
    return { ...look, title: display?.label || look.label || node.node, description: display?.description || look.description || "" };
  }

  // One renderer for both views: a card per control block, a line of text per
  // expression. The guide shows the block being edited and reaches the rest
  // through links; the full tree renders every block in place. Nothing is
  // read-only, so a block can be dropped anywhere in either view.
  _renderNode(node, path, compactChildren = false) {
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
      event.dataTransfer.setData(DND_MIME, JSON.stringify({ origin: "node", path }));
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
    const labels = descriptor?.doc?.params || [];
    form.paths.forEach((relativePath, slot) => {
      const label = labels[slot] || `条件 ${slot + 1}`;
      const value = form.operands[slot];
      body.append(this._labeledSlot(label, "bool", [...path, ...relativePath], value, compactChildren));
    });
    return body;
  }

  _renderCall(node, path, descriptor, compactChildren) {
    const body = element("div", "fr-node__body");
    const params = descriptor.params || [];
    const labels = descriptor.doc?.params || [];
    (node.args || []).forEach((arg, index) => {
      const item = descriptor.variadic ? Math.min(index, params.length - 1) : index;
      const parameter = params[item];
      const parameterLabel = descriptor.variadic && index > 0 ? `候选 ${index + 1}` : labels[item];
      const row = element("div", "fr-argument");
      const label = element("div", "fr-argument__label");
      label.append(element("span", "", parameterLabel || `参数 ${index + 1}`));
      label.append(element("code", "", typeName(parameter)));
      const argPath = [...path, "args", index];
      row.append(label, this._slotEditor(argPath, arg, compactChildren));
      body.append(row);
    });
    if (descriptor.variadic) body.append(this._listControls(node.args, params.length, () => null, "候选"));
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

  // A member is picked from the contract, never typed: the member set is the
  // host's data, so it is offered rather than spelled out.
  _memberField(node) {
    return enumMemberField({
      enums: contractEnums(this._runtimeContract),
      node,
      listHost: this.ownerDocument,
      onChange: () => this._changed(),
    });
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
    row.append(label, this._slotEditor(path, value, compactChildren));
    return row;
  }

  // A slot holding nothing but an expression is edited as text: typing
  // "amount > limit" beats dragging one card per leaf. Control flow stays a
  // card, and "展开" turns any expression into cards when dragging is wanted.
  _slotEditor(path, value, compactChildren) {
    const slot = element("div", "fr-slot");
    slot.append(this._slotBody(path, value, compactChildren));
    // Every expression position accepts a block, so every one of them shows
    // where it would land. The strip is quiet until a block is picked up.
    slot.append(this._dropZone(path, "拖入控制块", true));
    return slot;
  }

  _slotBody(path, value, compactChildren) {
    const key = path.join("\u0000");
    if (!this.parseExpression || !this._plain(value)) {
      if (!value) return this._dropZone(path, "写表达式或拖入控制块");
      return dropTarget(this._childEditor(value, path, compactChildren), (payload) => this._applyDrop(payload, path));
    }
    // Expanding shows the cards here, in place: jumping the focus elsewhere
    // would leave this slot rendered by another view, with no way back.
    if (this._expanded.has(key)) {
      return dropTarget(expandedSlot(this._renderNode(value, path),
        () => { this._expanded.delete(key); this.render(); }), (payload) => this._applyDrop(payload, path));
    }
    const row = expressionRow({
      source: value ? this._language.expressionSource(value) : "",
      placeholder: value ? "" : "写一段表达式，例如 amount > limit，回车校验",
      onCommit: (text) => this._commitExpression(path, text),
      onExpand: value ? () => { this._expanded.add(key); this.render(); } : null,
      onFocus: (input) => { this._activeInput = input; },
    });
    return dropTarget(row, (payload) => this._applyDrop(payload, path));
  }

  async _commitExpression(path, text) {
    if (!text) {
      this._setAtPath(path, null, false);
      this._changed();
      return "";
    }
    try {
      this._setAtPath(path, await this.parseExpression(text), false);
      this._changed();
      return "";
    } catch (error) {
      return error.message;
    }
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
    if (node.node === "var") return this._renderVariableEditor(node, path);
    const body = element("div", "fr-node__body fr-value-editor");
    body.append(node.node === "enum" ? this._memberField(node) : valueEditor({
      node,
      field: schema.fields[0],
      namePattern: this._language.namePattern,
      parse: parseInputValue,
      onChange: () => this._changed(),
    }));
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

  // Every expression position is a drop target, occupied or not, which is what
  // lets one control block go inside another. What lands there is decided by
  // placeBlock: the block takes the old expression into its own first slot.
  _dropZone(path, label, thin = false) {
    const selected = this._templates.get(this._selectedTemplateId);
    const selectedLabel = selected?.descriptor?.doc?.label || selected?.descriptor?.label;
    const zone = element("div", `fr-drop-zone${thin ? " fr-drop-zone--thin" : ""}${selected ? " is-ready" : ""}`,
      selected ? `点击放入「${selectedLabel || "已选组件"}」` : label);
    zone.addEventListener("click", () => this._placeSelected(path));
    return dropTarget(zone, (payload) => this._applyDrop(payload, path));
  }

  _placeSelected(path) {
    if (!this._selectedTemplateId) return;
    const created = this._createTemplate(this._selectedTemplateId, path);
    if (!created) {
      this._validation = { phase: "error", message: "这个位置没有可用的契约参数或本地变量。" };
      this.render();
      return;
    }
    this._setAtPath(path, placeBlock(created, this._getAtPath(path), this._nodes), false);
    this._selectedTemplateId = null;
    this._changed();
  }

  _applyDrop(payload, path) {
    if (payload.origin === "palette") {
      const created = this._createTemplate(payload.templateId, path);
      if (!created) {
        this._validation = { phase: "error", message: "这个位置没有可用的契约参数或本地变量。" };
        this.render();
        return;
      }
      this._setAtPath(path, placeBlock(created, this._getAtPath(path), this._nodes), false);
    } else if (payload.origin === "node") {
      const sourcePath = payload.path || [];
      // A node cannot be dropped into itself, and moving it must not leave a
      // copy behind: it is lifted out first, then placed.
      if (isPathPrefix(sourcePath, path)) return;
      const moving = clone(this._getAtPath(sourcePath));
      const existing = clone(this._getAtPath(path));
      this._setAtPath(sourcePath, null, false);
      this._setAtPath(path, placeBlock(moving, existing, this._nodes), false);
    }
    this._selectedTemplateId = null;
    this._expanded.clear();
    this._changed();
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

  _emitChange() {
    this.dispatchEvent(new CustomEvent("funroute-change", {
      detail: { value: this.value, source: this.source }, bubbles: true,
    }));
  }
}

if (!customElements.get("funroute-designer")) {
  customElements.define("funroute-designer", FunRouteDesigner);
}
