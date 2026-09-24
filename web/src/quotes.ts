// The test run's rate table, as the panel takes it: one quote a line, written
// the way a market writes it — "USD/JPY 150.25", one US dollar buys 150.25
// yen. The server checks each quote against the declared currencies; this
// only cuts the text into the shape the run command takes.

export type Quote = { base: string; quote: string; rate: string };

const line = /^([A-Za-z0-9]+)\s*\/\s*([A-Za-z0-9]+)\s+(\S+)$/;

// A line "@settlement" starts the quotes of a named rate table, the ones
// using(@settlement, …) converts through; the quotes before any are the
// run's own table.
const section = /^@([A-Za-z_][A-Za-z0-9_]*)$/;

export type QuoteTables = Record<string, Quote[]>;

// parseQuotes reads the lines, skipping blank ones. A line that is not a
// quote or a section is reported by its number, so the page can say which.
export function parseQuotes(text: string): { quotes: Quote[]; tables: QuoteTables; bad?: number } {
  const quotes: Quote[] = [];
  const tables: QuoteTables = {};
  let into = quotes;
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const trimmed = lines[i].trim();
    if (!trimmed) continue;
    const named = section.exec(trimmed);
    if (named) {
      into = tables[named[1]] ??= [];
      continue;
    }
    const match = line.exec(trimmed);
    if (!match) return { quotes, tables, bad: i + 1 };
    into.push({ base: match[1], quote: match[2], rate: match[3] });
  }
  return { quotes, tables };
}

// quotesForRun is the rate tables a run takes, or why there are none: a
// table with a line left out would convert at rates the panel does not show.
export function quotesForRun(text: string): { quotes: Quote[]; tables: QuoteTables } | { error: string } {
  const { quotes, tables, bad } = parseQuotes(text);
  return bad ? { error: `汇率表第 ${bad} 行不是汇率，写成 USD/JPY 150.25，或用 @名字 开始一张具名汇率表` } : { quotes, tables };
}

// quotesText writes quotes back as the lines parseQuotes reads, each named
// table under its section.
export function quotesText(quotes: Quote[], tables: QuoteTables = {}): string {
  const lines = (list: Quote[]) => list.map(({ base, quote, rate }) => `${base}/${quote} ${rate}`);
  return [...lines(quotes), ...Object.entries(tables).flatMap(([name, list]) => [`@${name}`, ...lines(list)])].join("\n");
}
