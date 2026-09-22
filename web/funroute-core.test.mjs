import assert from "node:assert/strict";
import test from "node:test";

import { FunRouteLanguage, FunRouteWorkspace, contractPayload, firstEmptySlot, isPlainExpression } from "./funroute-core.js";
import { FIELD_TEXT, parseInputValue } from "./funroute-fields.js";
import { typeName, typeSummary } from "./funroute-display.js";
import { placeBlock } from "./funroute-dnd.js";
import { indexNodes } from "./funroute-core.js";
import { loadExamples } from "./funroute-examples.js";

const placeholder = (name) => ({ $: name });
const call = (name, ...args) => ({ node: "call", name, args });
const bool = (value) => ({ node: "bool", bool: value });

function catalog() {
  return {
    source: {
      expr_json_version: 1,
      variable_name_pattern: "[A-Za-z_][A-Za-z0-9_]*",
      keywords: ["case", "else", "for", "in"],
      operators: [
        operator("&&", 2, call("if", placeholder("left"), placeholder("right"), bool(false)), "and"),
        operator("==", 3, call("eq", placeholder("left"), placeholder("right"))),
        operator("!=", 3, call("if", call("eq", placeholder("left"), placeholder("right")), bool(false), bool(true))),
        operator("<", 4, call("lt", placeholder("left"), placeholder("right"))),
        operator("+", 5, call("add", placeholder("left"), placeholder("right"))),
        operator("in", 4, call("member", placeholder("left"), placeholder("right"))),
        {
          token: "!", fixity: "prefix", precedence: 7, form: "not", operands: ["operand"],
          template: call("if", placeholder("operand"), bool(false), bool(true)),
        },
        {
          token: "[]", fixity: "index", associativity: "left", precedence: 8, form: "",
          operands: ["left", "right"], template: call("at", placeholder("left"), placeholder("right")),
        },
      ],
    },
    // Control blocks are read off the catalog: a special form with a node of
    // its own, or a lazy call. and / not have no node — they expand to if.
    special_forms: [
      { name: "switch", special: "switch" }, { name: "for", special: "for" },
      { name: "reduce", special: "reduce" }, { name: "let", special: "let" },
      { name: "and" }, { name: "not" },
    ],
    functions: [{ name: "if", special: "if" }, { name: "fallback", special: "fallback" }],
    nodes: [
      { node: "var", fields: [{ name: "name", kind: "name", default: "value" }] },
      { node: "switch", fields: [{ name: "value", kind: "expr", optional: true }, { name: "cases", kind: "list", min: 1, fields: [] }] },
      { node: "call", fields: [{ name: "name", kind: "name" }, { name: "args", kind: "exprs", optional: true }] },
      { node: "bool", fields: [{ name: "bool", kind: "bool" }] },
      { node: "for", fields: [
        { name: "source", kind: "expr" },
        { name: "variable", kind: "name", role: "local", binds: ["where", "yield"], default: "item" },
        { name: "where", kind: "expr", optional: true },
        { name: "yield", kind: "expr" },
      ] },
      { node: "reduce", fields: [
        { name: "source", kind: "expr" },
        { name: "variable", kind: "name", role: "local", binds: ["body"], default: "item" },
        { name: "accumulator", kind: "name", role: "local", binds: ["body"], default: "acc" },
        { name: "init", kind: "expr" },
        { name: "body", kind: "expr" },
      ] },
      { node: "let", fields: [
        { name: "bindings", kind: "list", min: 1, fields: [
          { name: "name", kind: "name", role: "local", binds: ["@rest", "body"], default: "x" },
          { name: "value", kind: "expr" },
        ] },
        { name: "body", kind: "expr" },
      ] },
    ],
  };
}

function operator(token, precedence, template, form = "") {
  return { token, fixity: "infix", associativity: "left", precedence, form,
    operands: ["left", "right"], template };
}

const variable = (name) => ({ node: "var", name });

test("example manifest loader validates and returns shared example data", async () => {
  const examples = await loadExamples(async (url) => ({
    ok: url === "/funroute-examples.json", status: 200,
    json: async () => ({ version: 1, examples: [{ label: "主备路由" }] }),
  }));
  assert.deepEqual(examples, [{ label: "主备路由" }]);
  await assert.rejects(() => loadExamples(async () => ({
    ok: true, status: 200, json: async () => ({ version: 2, examples: [] }),
  })), /示例清单格式无效/);
});

test("formatter uses precedence supplied by the Go catalog", () => {
  const language = new FunRouteLanguage(catalog());
  const grouped = call("lt", call("eq", variable("a"), variable("b")), variable("c"));
  const natural = call("eq", call("lt", variable("a"), variable("b")), variable("c"));
  assert.equal(language.expressionSource(grouped), "(a == b) < c");
  assert.equal(language.expressionSource(natural), "a < b == c");
  assert.deepEqual(language.operatorForm(grouped), {
    token: "<", fixity: "infix", associativity: "left", precedence: 4, form: "",
    operands: [call("eq", variable("a"), variable("b")), variable("c")],
    paths: [["args", 0], ["args", 1]],
  });
});

// Indexing and membership are spellings of at() and member(), so the printer
// has to put them back the way they were written — the catalog says how.
test("formatter prints index and keyword operators from the catalog", () => {
  const language = new FunRouteLanguage(catalog());
  const indexed = call("at", variable("prices"), { node: "int", int: 0 });
  const keyed = call("at", variable("rates"), { node: "string", string: "adyen" });
  const inside = call("member", variable("currency"), variable("accepted"));
  assert.equal(language.expressionSource(indexed), "prices[0]");
  assert.equal(language.expressionSource(keyed), 'rates["adyen"]');
  assert.equal(language.expressionSource(inside), "currency in accepted");
  // An index binds tighter than arithmetic, so no parentheses appear here.
  assert.equal(language.expressionSource(call("add", indexed, { node: "int", int: 1 })), "prices[0] + 1");
  // Every one of them is a plain expression: no card, edited as one line.
  for (const node of [indexed, keyed, inside]) {
    assert.equal(isPlainExpression(node, language), true);
  }
});

test("formatter keeps exhaustive enum switch without an else", () => {
  const language = new FunRouteLanguage(catalog());
  const text = language.expressionSource({
    node: "switch", value: variable("channel"), cases: [
      { match: [{ node: "string", string: "adyen" }], result: { node: "string", string: "stripe" } },
      { match: [{ node: "string", string: "stripe" }], result: { node: "string", string: "adyen" } },
    ],
  });
  assert.equal(text, 'switch(channel, case "adyen" => "stripe", case "stripe" => "adyen")');
});

test("formatter breaks long bindings and infix chains instead of one long line", () => {
  const language = new FunRouteLanguage(catalog());
  const text = (value) => ({ node: "string", string: value });
  const and = (left, right) => call("if", left, right, bool(false));
  const chain = and(and(variable("ready_for_settlement"), variable("trusted_merchant_profile")),
    variable("enabled_in_current_window"));
  const formatted = language.formatSource({
    node: "let",
    bindings: [{ name: "action", value: { node: "switch", cases: [
      { match: [variable("blocked_by_compliance")], result: text("reject") },
      { match: [call("lt", variable("review_limit"), variable("amount"))], result: text("review") },
    ], default: text("approve") } }],
    body: chain,
  });
  const lines = formatted.split("\n");
  assert.ok(lines.every((line) => line.length <= 72), `行过长：${lines.find((line) => line.length > 72)}`);
  assert.match(formatted, /\n  action = switch\(\n    case blocked_by_compliance => "reject",\n/);
  assert.match(formatted, /\n    && trusted_merchant_profile\n    && enabled_in_current_window/);
});

test("derived forms are instantiated and recognized from catalog templates", () => {
  const language = new FunRouteLanguage(catalog());
  const expression = language.createForm("and");
  expression.args[0] = variable("ready");
  expression.args[1] = call("if", variable("blocked"), bool(false), bool(true));
  assert.equal(language.expressionSource(expression), "ready && !blocked");
  assert.deepEqual(language.logicalForm(expression).paths, [["args", 0], ["args", 1]]);
});

test("not-equal is not mistaken for the derived not card", () => {
  const language = new FunRouteLanguage(catalog());
  const expression = call("if", call("eq", variable("a"), variable("b")), bool(false), bool(true));
  assert.equal(language.expressionSource(expression), "a != b");
  assert.equal(language.operatorForm(expression).token, "!=");
  assert.equal(language.logicalForm(expression), null);
});

test("tokenizer follows catalog spellings and handles exponent floats", () => {
  const language = new FunRouteLanguage(catalog());
  const tokens = language.tokenize("score < 1.2e-3 && !blocked");
  assert.deepEqual(tokens.filter((item) => item.kind === "op").map((item) => item.text), ["<", "&&", "!"]);
  assert.equal(tokens.find((item) => item.kind === "number").text, "1.2e-3");
});

test("scope picker follows catalog binds and checked contract arguments", () => {
  const language = new FunRouteLanguage(catalog());
  const expression = {
    node: "let",
    bindings: [
      { name: "base", value: variable("amount") },
      { name: "limit", value: variable("base") },
    ],
    body: {
      node: "for", source: variable("items"), variable: "item", where: variable("item"),
      yield: variable("item"),
    },
  };
  const args = [
    { name: "amount", type: { kind: "int" } },
    { name: "items", type: { kind: "array", elem: { kind: "int" } } },
    { name: "item", type: { kind: "string" } },
  ];
  const names = (path) => language.scopeAtPath(expression, path, args).map((item) => `${item.name}:${item.source}`);
  assert.deepEqual(names(["bindings", 0, "value"]), ["amount:contract", "items:contract", "item:contract"]);
  assert.deepEqual(names(["bindings", 1, "value"]), ["amount:contract", "items:contract", "item:contract", "base:local"]);
  assert.deepEqual(names(["body", "source"]), ["amount:contract", "items:contract", "item:contract", "base:local", "limit:local"]);
  assert.deepEqual(names(["body", "yield"]), ["amount:contract", "items:contract", "item:local", "base:local", "limit:local"]);

  const reduction = {
    node: "reduce", source: variable("items"), variable: "value", accumulator: "total",
    init: variable("amount"), body: variable("total"),
  };
  const reduceNames = (path) => language.scopeAtPath(reduction, path, args).map((item) => `${item.name}:${item.source}`);
  assert.deepEqual(reduceNames(["init"]), ["amount:contract", "items:contract", "item:contract"]);
  assert.deepEqual(reduceNames(["body"]), ["amount:contract", "items:contract", "item:contract", "value:local", "total:local"]);
});

test("typed input parsing never silently truncates or coerces", () => {
  assert.throws(() => parseInputValue("12abc", { kind: "int" }), /不是整数/);
  assert.throws(() => parseInputValue("yes", { kind: "bool" }), /true 或 false/);
  assert.throws(() => parseInputValue('[1,"2"]', { kind: "array", elem: { kind: "int" } }), /必须是安全整数/);
  assert.equal(parseInputValue("-2.5e2", { kind: "float" }), -250);
  assert.deepEqual(parseInputValue("{\"a\":true}", { kind: "dict", elem: { kind: "bool" } }), { a: true });
  const channel = { kind: "enum", name: "channel", values: ["adyen", "stripe"] };
  assert.equal(parseInputValue("adyen", channel), "adyen");
  assert.throws(() => parseInputValue("other", channel), /成员/);
  assert.equal(typeName(channel), "enum<channel>{adyen,stripe}");
});

test("control blocks come from the catalog, not from a list in the front end", () => {
  const language = new FunRouteLanguage(catalog());
  const variable = (name) => ({ node: "var", name });
  assert.deepEqual([...language.controlBlocks].sort(),
    ["fallback", "for", "if", "let", "reduce", "switch"]);
  assert.equal(isPlainExpression(call("gt", variable("score"), variable("ceiling")), language), true);
  assert.equal(isPlainExpression({ node: "enum", member: "adyen" }, language), true);
  assert.equal(isPlainExpression({ node: "array", items: [variable("a"), call("add", variable("b"), variable("c"))] }, language), true);
  assert.equal(isPlainExpression({ node: "switch", cases: [], default: variable("a") }, language), false);
  assert.equal(isPlainExpression(call("if", variable("ok"), { node: "let", bindings: [], body: variable("a") }, variable("b")), language), false);
  // && is if underneath, but it reads as an operator, so it stays text.
  assert.equal(isPlainExpression(call("if", variable("ready"), variable("ok"), bool(false)), language), true);
  assert.equal(isPlainExpression(null, language), true);
});

// FIELD_TEXT is the console's wording for fields the catalog describes. A key
// that no longer matches a field would silently fall back to the raw field
// name, so the two are checked against each other rather than by eye.
test("every wording key names a field the catalog actually has", async () => {
  const response = await fetch("http://127.0.0.1:8080/api/catalog").catch(() => null);
  if (!response?.ok) return; // the check needs a running console
  const catalog = await response.json();
  const fields = new Set();
  const walk = (node, list, prefix) => {
    for (const field of list || []) {
      fields.add(`${prefix}.${field.name}`);
      walk(node, field.fields, `${prefix}.${field.name}`);
    }
  };
  for (const schema of catalog.nodes) walk(schema.node, schema.fields, schema.node);
  for (const key of Object.keys(FIELD_TEXT)) {
    assert.ok(fields.has(key), `wording for ${key} names no field in the catalog`);
  }
});

test("a block dropped onto an expression takes it into its first slot", () => {
  const nodes = indexNodes(catalog().nodes);
  const condition = call("lt", { node: "var", name: "risk" }, { node: "var", name: "ceiling" });
  const block = { node: "call", name: "if", args: [null, null, null] };
  assert.deepEqual(firstEmptySlot(block, nodes), ["args", 0]);
  assert.deepEqual(placeBlock(block, condition, nodes).args[0], condition);

  const loop = { node: "for", source: null, variable: "item", yield: null };
  assert.deepEqual(firstEmptySlot(loop, nodes), ["source"]);
  assert.equal(placeBlock({ ...loop }, condition, nodes).source, condition);

  // Nothing to take it: the block stands in place of what was there.
  const full = { node: "var", name: "x" };
  assert.equal(placeBlock(full, condition, nodes), full);
  assert.equal(placeBlock(full, null, nodes), full);
});

test("a large enum is summarized for display but never for the wire", () => {
  const small = { kind: "enum", name: "channel", values: ["adyen", "stripe"] };
  const big = { kind: "enum", name: "country", values: Array.from({ length: 212 }, (_, i) => `c${i}`) };
  assert.equal(typeSummary(small), "enum<channel>{adyen,stripe}");
  assert.equal(typeSummary(big), "enum<country>{c0,c1,c2,c3,c4,c5… 共 212 个}");
  assert.equal(typeSummary({ kind: "array", elem: big }), "array<enum<country>{c0,c1,c2,c3,c4,c5… 共 212 个}>");
  assert.equal(typeName(big).startsWith("enum<country>{c0,c1,"), true);
  assert.equal(typeName(big).endsWith("c211}"), true);
});

test("contract payload preserves incomplete rows for authoritative validation", () => {
  const payload = contractPayload({ args: [{ name: "", type: "int" }], result: { type: "string" } });
  assert.deepEqual(payload.args, [{ name: "", type: "int", doc: undefined }]);
  assert.equal(payload.result.type, "string");
});

test("headless workspace owns parse, compile and run without a DOM", async () => {
  let compiledPayload;
  let checks = 0;
  const client = {
    catalog: async () => catalog(),
    parse: async () => ({ expr_json: { version: 2, expr: variable("amount") } }),
    checkContract: async () => {
      checks += 1;
      return { valid: true, arguments: 1, args: [{ name: "amount", type: { kind: "int" } }], result: { kind: "int" } };
    },
    compile: async (payload) => {
      compiledPayload = payload;
      return { digest: "sha256:test", instructions: 1, calls: [], args: [{ name: "amount", type: { kind: "int" } }], result: { kind: "int" } };
    },
    run: async (payload) => ({ value: payload.args.amount, type: { kind: "int" } }),
  };
  const workspace = new FunRouteWorkspace(client);
  await workspace.initialize();
  await workspace.parseSource("amount");
  workspace.setContract({ args: [{ name: "amount", type: "int" }], result: { type: "int" } });
  await workspace.compile();
  const result = await workspace.run({ amount: 7 });
  assert.equal(checks, 1);
  assert.equal(compiledPayload.contract.args[0].type, "int");
  assert.equal(result.value, 7);
  assert.equal(workspace.resultCheck.valid, true);
});

test("compileSource replaces the document only after parse and contract compilation pass", async () => {
  const previous = { version: 2, expr: bool(true) };
  const candidate = { version: 2, expr: variable("amount") };
  let rejectCompile = true;
  const workspace = new FunRouteWorkspace({
    catalog: async () => catalog(),
    parse: async () => ({ expr_json: candidate }),
    checkContract: async () => ({
      valid: true, arguments: 1,
      args: [{ name: "amount", type: { kind: "int" } }],
      result: { kind: "int" },
    }),
    compile: async (payload) => {
      assert.deepEqual(payload.expr_json, candidate);
      if (rejectCompile) throw new Error("candidate rejected");
      return {
        digest: "sha256:candidate", instructions: 1, calls: [],
        args: [{ name: "amount", type: { kind: "int" } }], result: { kind: "int" },
      };
    },
  });
  await workspace.initialize();
  workspace.setContract({ args: [{ name: "amount", type: "int" }], result: { type: "int" } });
  workspace.setDocument(previous);

  await assert.rejects(() => workspace.compileSource("amount"), /candidate rejected/);
  assert.deepEqual(workspace.document, previous);

  rejectCompile = false;
  const prepared = await workspace.compileSource("amount");
  assert.deepEqual(prepared.document, candidate);
  assert.deepEqual(workspace.document, candidate);
  assert.equal(workspace.compiled.digest, "sha256:candidate");
});

test("headless workspace requires a checked result contract before compile", async () => {
  let compiled = false;
  const workspace = new FunRouteWorkspace({
    catalog: async () => catalog(),
    checkContract: async () => ({ valid: true, arguments: 0 }),
    compile: async () => { compiled = true; },
  });
  await workspace.initialize();
  workspace.setDocument({ version: 2, expr: bool(true) });
  await assert.rejects(() => workspace.compile(), /返回类型/);
  assert.equal(compiled, false);
});

test("headless workspace rejects a runtime result that violates the contract", async () => {
  const intType = { kind: "int" };
  const workspace = new FunRouteWorkspace({
    catalog: async () => catalog(),
    checkContract: async () => ({ valid: true, arguments: 0, args: [], result: intType }),
    compile: async () => ({ digest: "sha256:test", instructions: 1, calls: [], args: [], result: intType }),
    run: async () => ({ value: "wrong", type: { kind: "string" } }),
  });
  await workspace.initialize();
  workspace.setContract({ args: [], result: { type: "int" } });
  workspace.setDocument({ version: 2, expr: bool(true) });
  await assert.rejects(() => workspace.run({}), /结果类型 string 与契约 int 不匹配/);
  assert.equal(workspace.resultCheck.valid, false);
});
