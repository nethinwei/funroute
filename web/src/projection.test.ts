import assert from "node:assert/strict";
import test from "node:test";
import { Text } from "@codemirror/state";
import { commonIndent, dedent, indent, argsText, children, isCard, isPlain, slice, wrap } from "./projection.ts";
import { decodeTokens } from "./tokens.ts";
import type { Tree } from "./protocol.ts";

const at = (line: number, character: number) => ({ line, character });
const node = (kind: string, fields: Tree["fields"] = [], operator?: string, form?: boolean): Tree => ({ node: kind, range: { start: at(0, 0), end: at(0, 0) }, fields, operator, form });

test("cards are forms and lazy calls, operators are text", () => {
  const lazy = new Set(["if", "fallback"]);
  const call = (name: string, operator?: string) => node("call", [{ name: "name", text: name }], operator);
  assert.equal(isCard(node("let", [], undefined, true), lazy), true);
  assert.equal(isCard(node("array"), lazy), false);
  assert.equal(isCard(call("if"), lazy), true);
  assert.equal(isCard(call("if", "&&"), lazy), false);
  assert.equal(isCard(call("add", "+"), lazy), false);
  assert.equal(isPlain(node("array", [{ name: "items", nodes: [call("if")] }]), lazy), false);
  assert.equal(isPlain(node("array", [{ name: "items", nodes: [call("add", "+")] }]), lazy), true);
});

// A node that is not a card shows each of its parts, the ones inside items
// too: the entries of a record hold theirs there, not in nodes.
test("the parts of a node are every node below it, items included", () => {
  const lazy = new Set(["if"]);
  const block = node("call", [{ name: "name", text: "if" }]);
  const value = node("ident");
  const record = node("record", [{ name: "entries", items: [[{ name: "key", text: "a" }, { name: "value", nodes: [block] }], [{ name: "value", nodes: [value] }]] }]);
  assert.deepEqual(children(record), [block, value]);
  assert.equal(isPlain(record, lazy), false);
});

test("positions count UTF-16 units, as the server's default does", () => {
  const text = Text.of(['{"好😀": x,', "  y}"]);
  assert.equal(slice(text, { start: at(0, 8), end: at(0, 9) }), "x");
  assert.equal(slice(text, { start: at(1, 2), end: at(1, 3) }), "y");
});

test("a block wraps what it is dropped on", () => {
  assert.equal(wrap("if($, then_value, else_value)", " amount > 1 "), "if(amount > 1, then_value, else_value)");
  assert.equal(wrap("let(name = value, $)", ""), "let(name = value, value)");
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

// What the lines share is a prefix of every one of them, and a line of
// blanks is left alone, so a slot's text comes back as it was whatever the
// indentation is made of.
test("dedent and indent undo each other", () => {
  for (const source of ["f(a,\n\tb,\n  c)", "f(a,\n \n    b)", "f(a,\n\n  b,\n  c)", "{\n    x,\n  \t y\n  }"]) {
    assert.equal(indent(dedent(source, commonIndent(source)), commonIndent(source)), source, JSON.stringify(source));
  }
  assert.equal(commonIndent("f(a,\n\tb,\n  c)"), "");
  assert.equal(commonIndent("{\n    x,\n  \t y\n  }"), "  ");
});

