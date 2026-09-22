import { typeName } from "./funroute-core.js";

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

// SemanticTreeRenderer is the readable, structure-aware projection used by
// the full-tree view. It deliberately receives language and editing hooks
// instead of owning document state, so another UI can reuse the headless SDK
// without depending on this renderer.
class SemanticTreeRenderer {
  constructor({ language, presentation, descriptor, edit, dropZone }) {
    this.language = language;
    this.presentation = presentation;
    this.descriptor = descriptor;
    this.edit = edit;
    this.dropZone = dropZone;
  }

  render(node, path) {
    const operator = this.language.operatorForm(node);
    if (operator) return this.expressionSummary(node, path, true, operator);
    if (node.node === "call" && node.name === "if") return this.renderIf(node, path);
    switch (node.node) {
      case "call": return this.renderCall(node, path);
      case "switch": return this.renderSwitch(node, path);
      case "for": return this.renderFor(node, path);
      case "reduce": return this.renderReduce(node, path);
      case "let": return this.renderLet(node, path);
      case "array": return this.renderArray(node, path);
      case "dict": return this.renderDict(node, path);
      default: return this.expressionSummary(node, path, true);
    }
  }

  card(node, path, modifier, body, eyebrow = "控制结构") {
    const look = this.presentation(node);
    const card = element("article", `fr-node fr-node--semantic fr-semantic--${modifier}`);
    card.style.setProperty("--fr-accent", look.color || "#64748B");
    const header = element("div", "fr-semantic-card__header");
    const identity = element("div", "fr-semantic-card__identity");
    identity.append(element("span", "fr-node__icon", look.icon));
    const copy = element("span", "fr-semantic-card__copy");
    copy.append(element("span", "fr-semantic-card__eyebrow", eyebrow), element("strong", "", look.title));
    copy.append(element("code", "", this.language.expressionSource(node)));
    identity.append(copy);
    const edit = element("button", "fr-semantic-edit", "编辑步骤 →");
    edit.type = "button";
    edit.addEventListener("click", () => this.edit(path));
    header.append(identity, edit);
    card.append(header, body);
    return card;
  }

  expressionSummary(node, path, standalone = false, operator = null) {
    const look = this.presentation(node);
    const wrapper = element(standalone ? "article" : "div",
      `${standalone ? "fr-node fr-node--semantic " : ""}fr-expression-summary${operator ? " is-operator" : ""}`);
    wrapper.style.setProperty("--fr-accent", look.color || "#64748B");
    const control = element("button", "fr-expression-summary__button");
    control.type = "button";
    const mark = element("span", "fr-expression-summary__mark", operator?.token || look.icon);
    const copy = element("span", "fr-expression-summary__copy");
    copy.append(element("span", "", operator ? `${operator.fixity === "prefix" ? "前缀" : "中缀"}表达式` : look.title));
    copy.append(element("code", "", this.language.expressionSource(node)));
    control.append(mark, copy, element("span", "fr-expression-summary__action", "编辑 →"));
    control.title = "进入引导视图编辑这个表达式";
    control.addEventListener("click", () => this.edit(path));
    wrapper.append(control);
    return wrapper;
  }

  isStructure(node) {
    if (!node || this.language.operatorForm(node)) return false;
    return ["call", "switch", "for", "reduce", "let", "array", "dict"].includes(node.node);
  }

  value(node, path) {
    return this.isStructure(node)
      ? this.render(node, path)
      : this.expressionSummary(node, path, false, this.language.operatorForm(node));
  }

  slot(labelText, path, value, { hint = "", tone = "" } = {}) {
    const slot = element("section", `fr-semantic-slot${tone ? ` is-${tone}` : ""}`);
    const heading = element("div", "fr-semantic-slot__heading");
    heading.append(element("strong", "", labelText));
    if (hint) heading.append(element("span", "", hint));
    slot.append(heading, value ? this.value(value, path) : this.dropZone(path, "拖入表达式"));
    return slot;
  }

  renderIf(node, path) {
    const body = element("div", "fr-semantic-body fr-semantic-if");
    body.append(this.slot("IF · 条件", [...path, "args", 0], node.args?.[0], { tone: "condition" }));
    const branches = element("div", "fr-semantic-branches");
    branches.append(
      this.slot("THEN · 条件成立", [...path, "args", 1], node.args?.[1], { tone: "success" }),
      this.slot("ELSE · 条件不成立", [...path, "args", 2], node.args?.[2], { tone: "fallback" }),
    );
    body.append(branches);
    return this.card(node, path, "if", body, "条件控制");
  }

  renderCall(node, path) {
    const descriptor = this.descriptor(node);
    const labels = descriptor.display?.parameters || [];
    const params = descriptor.params || [];
    const body = element("div", "fr-semantic-body fr-semantic-call");
    if (!(node.args || []).length) body.append(element("p", "fr-semantic-empty", "无参数调用"));
    (node.args || []).forEach((arg, index) => {
      body.append(this.slot(labels[index]?.label || `参数 ${index + 1}`, [...path, "args", index], arg, {
        hint: typeName(params[index]),
      }));
    });
    return this.card(node, path, "call", body, "函数调用");
  }

  renderSwitch(node, path) {
    const body = element("div", "fr-semantic-body fr-semantic-switch");
    const mode = element("div", "fr-semantic-mode");
    mode.append(element("strong", "", node.value ? "值匹配模式" : "条件链模式"),
      element("span", "", node.value ? "按顺序匹配同类型值" : "按顺序命中第一个 true 条件"));
    body.append(mode);
    if (node.value) body.append(this.slot("SWITCH · 待匹配值", [...path, "value"], node.value, { tone: "subject" }));
    const cases = element("div", "fr-semantic-cases");
    (node.cases || []).forEach((branch, caseIndex) => {
      const row = element("section", "fr-semantic-case");
      const caseHead = element("div", "fr-semantic-case__head");
      caseHead.append(element("span", "", `CASE ${caseIndex + 1}`), element("small", "", "命中任一条件即可"));
      const matches = element("div", "fr-semantic-case__matches");
      (branch.match || []).forEach((match, matchIndex) => {
        const matchPath = [...path, "cases", caseIndex, "match", matchIndex];
        matches.append(match
          ? this.value(match, matchPath)
          : this.dropZone(matchPath, node.value ? "拖入匹配值" : "拖入 bool 条件"));
      });
      row.append(caseHead, matches, element("span", "fr-semantic-arrow", "→"),
        this.slot("返回", [...path, "cases", caseIndex, "result"], branch.result, { tone: "success" }));
      cases.append(row);
    });
    body.append(cases, this.slot("ELSE · 默认返回", [...path, "default"], node.default, { tone: "fallback" }));
    return this.card(node, path, "switch", body, "多分支控制");
  }

  renderFor(node, path) {
    const body = element("div", "fr-semantic-body fr-semantic-loop");
    const locals = element("div", "fr-semantic-locals");
    locals.append(element("span", "", "遍历变量"), element("code", "", node.key_variable
      ? `${node.key_variable}, ${node.variable || "item"}`
      : (node.variable || "item")));
    body.append(locals, this.slot("IN · 输入集合", [...path, "source"], node.source, { tone: "subject" }));
    if (node.where) body.append(this.slot("IF · 保留条件", [...path, "where"], node.where, { tone: "condition" }));
    else {
      const skipped = element("div", "fr-semantic-optional");
      skipped.append(element("strong", "", "IF · 保留条件"), element("span", "", "未设置，保留全部元素"));
      body.append(skipped);
    }
    body.append(this.slot("YIELD · 产出", [...path, "yield"], node.yield, { tone: "success" }));
    return this.card(node, path, "for", body, "集合遍历");
  }

  renderReduce(node, path) {
    const body = element("div", "fr-semantic-body fr-semantic-loop");
    const locals = element("div", "fr-semantic-locals");
    const item = node.key_variable ? `${node.key_variable}, ${node.variable || "item"}` : (node.variable || "item");
    locals.append(element("span", "", "遍历变量"), element("code", "", item),
      element("span", "", "累加器"), element("code", "", node.accumulator || "acc"));
    body.append(locals,
      this.slot("IN · 输入集合", [...path, "source"], node.source, { tone: "subject" }),
      this.slot("FROM · 初始值", [...path, "init"], node.init),
      this.slot("STEP · 每轮累加", [...path, "body"], node.body, { tone: "success" }));
    return this.card(node, path, "reduce", body, "折叠累加");
  }

  renderLet(node, path) {
    const body = element("div", "fr-semantic-body fr-semantic-let");
    const bindings = element("div", "fr-semantic-bindings");
    (node.bindings || []).forEach((binding, index) => {
      const row = element("section", "fr-semantic-binding");
      row.append(element("span", "fr-semantic-binding__index", String(index + 1)),
        element("code", "fr-semantic-binding__name", binding.name || "未命名"),
        element("span", "fr-semantic-arrow", "="),
        binding.value
          ? this.value(binding.value, [...path, "bindings", index, "value"])
          : this.dropZone([...path, "bindings", index, "value"], "拖入绑定值"));
      bindings.append(row);
    });
    body.append(bindings, this.slot("IN · 主体结果", [...path, "body"], node.body, { tone: "success" }));
    return this.card(node, path, "let", body, "顺序绑定");
  }

  renderArray(node, path) {
    const body = element("div", "fr-semantic-body fr-semantic-collection");
    if (!(node.items || []).length) body.append(element("p", "fr-semantic-empty", "空数组 []"));
    (node.items || []).forEach((item, index) => body.append(this.slot(`[${index}]`, [...path, "items", index], item)));
    return this.card(node, path, "array", body, "数组字面量");
  }

  renderDict(node, path) {
    const body = element("div", "fr-semantic-body fr-semantic-collection");
    if (!(node.entries || []).length) body.append(element("p", "fr-semantic-empty", "空字典 {}"));
    (node.entries || []).forEach((entry, index) => {
      body.append(this.slot(JSON.stringify(entry.key || ""), [...path, "entries", index, "value"], entry.value));
    });
    return this.card(node, path, "dict", body, "字典字面量");
  }
}

export function renderSemanticTree(options, node, path = []) {
  return new SemanticTreeRenderer(options).render(node, path);
}
