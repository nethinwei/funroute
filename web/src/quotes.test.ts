import { test } from "node:test";
import assert from "node:assert/strict";
import { parseQuotes, quotesForRun, quotesText } from "./quotes.ts";

test("quotes are read a line each, blank lines skipped", () => {
  assert.deepEqual(parseQuotes("USD/JPY 150.25\n\n  EUR / USD 1.08  \n"), {
    quotes: [{ base: "USD", quote: "JPY", rate: "150.25" }, { base: "EUR", quote: "USD", rate: "1.08" }], tables: {},
  });
  assert.deepEqual(parseQuotes(""), { quotes: [], tables: {} });
});

test("a line that is not a quote is named by its number", () => {
  assert.equal(parseQuotes("USD/JPY 150\nUSD JPY 150").bad, 2);
  assert.equal(parseQuotes("USD/JPY").bad, 1);
});

test("quotes written back read back the same", () => {
  const quotes = [{ base: "BTC", quote: "VND", rate: "1600000000" }];
  assert.deepEqual(parseQuotes(quotesText(quotes)).quotes, quotes);
});

test("a run takes the whole table or none of it", () => {
  assert.deepEqual(quotesForRun("USD/JPY 150"), { quotes: [{ base: "USD", quote: "JPY", rate: "150" }], tables: {} });
  assert.deepEqual(quotesForRun("USD/JPY 150\nUSD JPY 150"), { error: "汇率表第 2 行不是汇率，写成 USD/JPY 150.25，或用 @名字 开始一张具名汇率表" });
});

test("a section starts a named table, and written back reads back the same", () => {
  const read = parseQuotes("USD/JPY 150\n@settlement\nUSD/JPY 149.5\n@market");
  assert.deepEqual(read, {
    quotes: [{ base: "USD", quote: "JPY", rate: "150" }],
    tables: { settlement: [{ base: "USD", quote: "JPY", rate: "149.5" }], market: [] },
  });
  assert.deepEqual(parseQuotes(quotesText(read.quotes, read.tables)), read);
});
