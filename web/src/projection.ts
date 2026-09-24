// The structure view is a projection of the text: it draws what the server's
// syntax tree says and turns every edit into an edit of the text. Nothing
// here knows the language beyond which nodes are drawn as cards.
import type { Position, Range, Tree, TreeField } from "./protocol.ts";

// A card is a node with branches, local names or laziness: the forms with a
// node of their own, and a call to a lazy function such as if. An operator
// is text, even when it expands into if.
const FORM_NODES = new Set(["switch", "for", "reduce", "let", "using"]);

export function callName(tree: Tree): string {
  return tree.fields?.find((field) => field.name === "name")?.text ?? "";
}

export function isCard(tree: Tree, lazy: Set<string>): boolean {
  if (FORM_NODES.has(tree.node)) return true;
  return tree.node === "call" && !tree.operator && lazy.has(callName(tree));
}

export function children(tree: Tree): Tree[] {
  const out: Tree[] = [];
  const visit = (fields: TreeField[] = []) => {
    for (const field of fields) {
      out.push(...(field.nodes ?? []));
      for (const item of field.items ?? []) visit(item);
    }
  };
  visit(tree.fields);
  return out;
}

// isPlain is a subtree with no card in it, shown as one line of source.
export function isPlain(tree: Tree, lazy: Set<string>): boolean {
  return !isCard(tree, lazy) && children(tree).every((child) => isPlain(child, lazy));
}

// Text is the source with a way to find an offset from a position. Positions
// count UTF-16 units, as JavaScript strings do.
export class Text {
  readonly source: string;
  private readonly lines: number[];

  constructor(source: string) {
    this.source = source;
    this.lines = [0];
    for (let i = 0; i < source.length; i++) if (source[i] === "\n") this.lines.push(i + 1);
  }

  offset(position: Position): number {
    return (this.lines[position.line] ?? this.source.length) + position.character;
  }

  slice(range: Range): string {
    return this.source.slice(this.offset(range.start), this.offset(range.end));
  }
}

// A slice of source that spans lines keeps the indentation it has in the
// whole text: its first line starts mid-line, the rest carry the columns of
// where it sits. dedent takes off what the later lines share, so the slot
// shows the expression as if it stood alone; indent puts it back on what was
// typed, so an edit leaves the text around it laid out as it was.
export function commonIndent(source: string): string {
  const rest = source.split("\n").slice(1).filter((line) => line.trim() !== "");
  if (rest.length === 0) return "";
  return rest.map((line) => line.match(/^[ \t]*/)![0]).reduce((a, b) => (b.length < a.length ? b : a));
}

export function dedent(source: string, indent: string): string {
  return source.split("\n").map((line, i) => (i > 0 && line.startsWith(indent) ? line.slice(indent.length) : line)).join("\n");
}

export function indent(source: string, indent: string): string {
  return source.split("\n").map((line, i) => (i > 0 && line !== "" ? indent + line : line)).join("\n");
}

// A block dropped on an expression takes it into the slot that reads as what
// the block works on; $ marks that slot. The other slots are names to fill in.
export const BLOCKS: Record<string, string> = {
  if: "if($, then_value, else_value)",
  fallback: "fallback($, backup)",
  switch: "switch($, case value => result, else => otherwise)",
  for: "[item for item in $]",
  reduce: "reduce(item in $, acc = 0, acc + item)",
  let: "let(name = value, $)",
  using: "using(150 JPY / USD, $)",
};

export function wrap(block: string, inner: string): string {
  return (BLOCKS[block] ?? "$").replace("$", inner.trim() || "value");
}

// argsText is the arguments object as JSON text, from what was typed for each
// argument. It is sent as text so no digit of a large integer is lost here;
// the server decodes it and says which value is wrong.
export function argsText(values: { name: string; text: string }[]): string {
  return `{${values.map(({ name, text }) => `${JSON.stringify(name)}: ${text.trim() || "null"}`).join(", ")}}`;
}
