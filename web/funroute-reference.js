// What a rule may use, in one place. The palette holds control blocks because
// those are dragged; everything else is written, so it is listed here instead
// of pretending to be a component.
//
// The split is the same one the canvas makes: what goes inside one line of an
// expression, and what wraps lines around other expressions.

const SYNTAX_NOTES = [
  ["参数与局部名", "amount", "契约入参，以及 let / 推导式绑定的名字"],
  ["字面量", '42 · 1.5 · "SGD" · true', "整数、浮点、字符串、布尔"],
  ["枚举成员", "@adyen · @channel.adyen", "契约声明的成员；同名成员属于多个枚举时写全限定"],
  ["容器", '[a, b] · {"k": v}', "数组元素同型；字典的 key 是字符串、value 同型"],
  ["列表推导", "[x * 2 for x in xs if x > 0]", "遍历数组或字典（两个变量遍历字典）"],
];

export function renderReference(catalog, controlBlocks) {
  const panel = document.createElement("section");
  panel.className = "fr-reference";
  panel.append(
    group("控制结构", "包住其他表达式，画布上是可拖拽的卡片",
      blocks(catalog, controlBlocks)),
    group("表达式里的运算符", "写在一行里，按优先级结合",
      (catalog.source?.operators || []).map((item) => row(item.token, fixityText(item), ""))),
    group("表达式里的函数", "写成 name(...)，类型由编译器检查",
      callables(catalog, controlBlocks)),
    group("写法", "不需要组件，直接写进表达式",
      SYNTAX_NOTES.map(([label, sample, note]) => row(sample, label, note))),
  );
  return panel;
}

function blocks(catalog, controlBlocks) {
  const seen = new Set();
  const out = [];
  for (const item of [...(catalog.special_forms || []), ...(catalog.functions || [])]) {
    if (!controlBlocks.has(item.name) || seen.has(item.name)) continue;
    seen.add(item.name);
    out.push(row(item.name, item.doc?.label || "", item.doc?.description || ""));
  }
  return out;
}

function callables(catalog, controlBlocks) {
  const seen = new Set();
  const out = [];
  for (const item of catalog.functions || []) {
    if (controlBlocks.has(item.name) || seen.has(item.name)) continue;
    seen.add(item.name);
    out.push(row(item.name, item.doc?.label || "", item.signature || ""));
  }
  return out;
}

function group(title, note, rows) {
  const section = document.createElement("section");
  section.className = "fr-reference__group";
  const heading = document.createElement("div");
  heading.className = "fr-reference__heading";
  heading.append(text("strong", title), text("span", note));
  section.append(heading);
  for (const item of rows) section.append(item);
  return section;
}

function row(sample, label, note) {
  const line = document.createElement("div");
  line.className = "fr-reference__row";
  line.append(text("code", sample), text("strong", label), text("span", note));
  return line;
}

function fixityText(operator) {
  return operator.fixity === "prefix" ? "前缀" : `优先级 ${operator.precedence}`;
}

function text(tag, value) {
  const node = document.createElement(tag);
  node.textContent = value || "";
  return node;
}
