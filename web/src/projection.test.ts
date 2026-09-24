import assert from "node:assert/strict";
import test from "node:test";
import { commonIndent, dedent, indent, Text, argsText, isCard, isPlain, wrap } from "./projection.ts";
import { decodeTokens } from "./tokens.ts";
import type { Tree } from "./protocol.ts";

const at = (line: number, character: number) => ({ line, character });
const node = (kind: string, fields: Tree["fields"] = [], operator?: string): Tree => ({ node: kind, range: { start: at(0, 0), end: at(0, 0) }, fields, operator });

test("cards are forms and lazy calls, operators are text", () => {
  const lazy = new Set(["if", "fallback"]);
  const call = (name: string, operator?: string) => node("call", [{ name: "name", text: name }], operator);
  assert.equal(isCard(node("let"), lazy), true);
  assert.equal(isCard(node("using"), lazy), true);
  assert.equal(isCard(call("if"), lazy), true);
  assert.equal(isCard(call("if", "&&"), lazy), false);
  assert.equal(isCard(call("add", "+"), lazy), false);
  assert.equal(isPlain(node("array", [{ name: "items", nodes: [call("if")] }]), lazy), false);
  assert.equal(isPlain(node("array", [{ name: "items", nodes: [call("add", "+")] }]), lazy), true);
});

test("positions count UTF-16 units, as the server's default does", () => {
  const text = new Text('{"好😀": x,\n  y}');
  assert.equal(text.slice({ start: at(0, 8), end: at(0, 9) }), "x");
  assert.equal(text.slice({ start: at(1, 2), end: at(1, 3) }), "y");
});

test("a block wraps what it is dropped on", () => {
  assert.equal(wrap("if", " amount > 1 "), "if(amount > 1, then_value, else_value)");
  assert.equal(wrap("let", ""), "let(name = value, value)");
});

test("arguments go as the text that was typed", () => {
  assert.equal(argsText([{ name: "n", text: "9007199254740993" }, { name: "s", text: '"SG"' }, { name: "e", text: "" }]),
    '{"n": 9007199254740993, "s": "SG", "e": null}');
});

test("semantic tokens are decoded to absolute positions", () => {
  const tokens = decodeTokens([0, 0, 3, 0, 0, 0, 4, 4, 2, 1, 1, 2, 1, 1, 0], ["keyword", "operator", "variable"]);
  assert.deepEqual(tokens.map((token) => [token.start.line, token.start.character, token.type, token.declaration]),
    [[0, 0, "keyword", false], [0, 4, "variable", true], [1, 2, "operator", false]]);
});

test("a slot shows its lines without the indentation of where they sit, and puts it back", () => {
  const source = "{\n    usable: usable,\n    best: ranked\n  }";
  const shared = commonIndent(source);
  assert.equal(shared, "  ");
  assert.equal(dedent(source, shared), "{\n  usable: usable,\n  best: ranked\n}");
  assert.equal(indent(dedent(source, shared), shared), source);
  assert.equal(commonIndent("a + b"), "");
});
