import assert from "node:assert/strict";
import test from "node:test";

import { FunRouteLanguage, FunRouteWorkspace, contractPayload, parseInputValue } from "./funroute-core.js";

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
        {
          token: "!", fixity: "prefix", precedence: 7, form: "not", operands: ["operand"],
          template: call("if", placeholder("operand"), bool(false), bool(true)),
        },
      ],
    },
    special_forms: [{ name: "and" }, { name: "not" }],
    nodes: [
      { node: "var", fields: [{ name: "name", kind: "name", default: "value" }] },
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
    parse: async () => ({ expr_json: { version: 1, expr: variable("amount") } }),
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
  const previous = { version: 1, expr: bool(true) };
  const candidate = { version: 1, expr: variable("amount") };
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
  workspace.setDocument({ version: 1, expr: bool(true) });
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
  workspace.setDocument({ version: 1, expr: bool(true) });
  await assert.rejects(() => workspace.run({}), /结果类型 string 与契约 int 不匹配/);
  assert.equal(workspace.resultCheck.valid, false);
});
