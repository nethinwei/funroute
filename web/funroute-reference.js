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
  ["多层推导", "[{a: x, b: y} for x in xs for y in ys]", "笛卡尔积；结果是一个扁平数组，每个 for 可带自己的 if"],
  ["字典推导", "{k: v * 2 for k, v in rates}", "产出字典；键必须是 string，重复的键会报错"],
  ["记录", "{amount: 1200, currency: \"SGD\"}", "字段各有类型；读字段写 order.amount"],
  ["取元素", "xs[0] · d[\"SG\"] · card[0]", "数组按下标、字典按键、字符串按码点；越界与缺键都是错误"],
  ["是否含有", "x in xs · \"SG\" in d · \"b\" in text", "数组查元素、字典查键、字符串查子串"],
];

// Recipes are the combinations that come up often enough to be worth writing
// down, but not often enough to deserve a function of their own — the ones a
// rule writer would otherwise re-derive every time.
const RECIPES = [
  ["配对两个数组", "[{name: names[i], fee: fees[i]} for i in indices(fees)]", "字段名由你定，比固定的 left/right 清楚"],
  ["按键取另一个数组的值", "others[index_of(keys, k)]", "键必须存在，先用 k in keys 判断"],
  ["条件计数", "len([x for x in xs if x > limit])", "先筛后数"],
  ["金额分档", "len([t for t in tiers if t <= amount])", "比门槛小的个数就是档位"],
  ["累计到超限的位置", "first([i for i in indices(xs) if cumsum(xs)[i] > limit])", "配 cumsum 用"],
  ["每组聚合", "{k: sum(v) for k, v in group_by(amounts, channels)}", "group_by 配字典推导"],
  ["累计到超限为止", "take_while(xs, [t <= cap for t in cumsum(xs)])", "判断数组与候选等长，遇到第一个 false 就停"],
  ["缺键兜底", "get(rates, country, 0)", "d[k] 缺键是错误；要默认值就写出来"],
  ["默认叠覆盖", "get(merge(defaults, overrides), k, 0)", "键相同时取后一个字典的值"],
];

export function renderReference(catalog, controlBlocks) {
  const panel = document.createElement("section");
  panel.className = "fr-reference";
  panel.append(
    group("控制结构", "包住其他表达式，画布上是可拖拽的卡片",
      blocks(catalog, controlBlocks)),
    group("表达式里的运算符", "写在一行里，按优先级结合",
      (catalog.source?.operators || []).map((item) => row(item.token, fixityText(item), ""))),
    ...functionGroups(catalog, controlBlocks),
    group("写法", "不需要组件，直接写进表达式",
      SYNTAX_NOTES.map(([label, sample, note]) => row(sample, label, note))),
    group("组合写法", "常用但不值得单独造一个函数的写法",
      RECIPES.map(([label, sample, note]) => row(sample, label, note))),
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

// Functions are grouped by the category the catalog already gives them: the
// list is long enough now that one flat run of it is unreadable, and the
// grouping is the host's own, not a second classification kept here.
function functionGroups(catalog, controlBlocks) {
  const byCategory = new Map();
  const seen = new Set();
  for (const item of catalog.functions || []) {
    if (controlBlocks.has(item.name) || seen.has(item.name)) continue;
    seen.add(item.name);
    const category = item.doc?.category || "扩展";
    if (!byCategory.has(category)) byCategory.set(category, []);
    byCategory.get(category).push(row(item.name, item.doc?.label || "", item.signature || ""));
  }
  return [...byCategory].map(([category, rows]) =>
    group(`函数 · ${category}`, `${rows.length} 个，写成 name(...)，类型由编译器检查`, rows));
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
