// Semantic tokens arrive as the protocol's flat list of five numbers per
// token, positioned relative to the one before. decodeTokens turns them back
// into absolute spans; which class each gets is the server's word for it.
import type { Position } from "./protocol.ts";

type Token = { start: Position; length: number; type: string; declaration: boolean };

export function decodeTokens(data: number[], types: string[]): Token[] {
  const tokens: Token[] = [];
  let line = 0;
  let character = 0;
  for (let i = 0; i + 4 < data.length; i += 5) {
    line += data[i];
    character = data[i] === 0 ? character + data[i + 1] : data[i + 1];
    tokens.push({ start: { line, character }, length: data[i + 2], type: types[data[i + 3]] ?? "", declaration: (data[i + 4] & 1) === 1 });
  }
  return tokens;
}
