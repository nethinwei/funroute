import { test } from "node:test";
import assert from "node:assert/strict";
import { switchRefusal } from "./views.ts";

test("the view changes only over a checked text without errors", () => {
  assert.equal(switchRefusal({ source: "a + b" }, "a + b"), "");
  assert.match(switchRefusal(null, "a + b"), /正在检查/);
  assert.match(switchRefusal({ source: "a + b" }, "a + bc"), /正在检查/);
  assert.match(switchRefusal({ source: "a +", error: "1:4 expected an expression" }, "a +"), /1:4 expected an expression/);
});
