// The structure view is a projection of the text: it draws what the server's
// syntax tree says and turns every edit into an edit of the text. Nothing
// here knows the language: which nodes are forms, which calls are lazy and
// how a block takes in an expression are all the server's to say.
import type { Text } from "@codemirror/state";
import type { Position, Range, Tree, TreeField } from "./protocol.ts";

// A card is a node with branches, local names or laziness: a form, which the
// tree marks, and a call to a lazy function such as if. An operator is text,
// even when it expands into if.

export function callName(tree: Tree): string {
  return tree.fields?.find((field) => field.name === "name")?.text ?? "";
}

export function isCard(tree: Tree, lazy: Set<string>): boolean {
  if (tree.form) return true;
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

// offsetAt is where a server position falls in the editor's text. Both count
// UTF-16 units, the server's default, so the character is the column.
export function offsetAt(doc: Text, position: Position): number {
  return doc.line(position.line + 1).from + position.character;
}

// slice is the source a server range covers.
export function slice(doc: Text, range: Range): string {
  return doc.sliceString(offsetAt(doc, range.start), offsetAt(doc, range.end));
}

// A slice of source that spans lines keeps the indentation it has in the
// whole text: its first line starts mid-line, the rest carry the columns of
// where it sits. dedent takes off what the later lines share, so the slot
// shows the expression as if it stood alone; indent puts it back on what was
// typed, so an edit leaves the text around it laid out as it was. What they
// share is the longest prefix every one of them starts with — a tab and two
// spaces share nothing — and a line of nothing but blanks is left as it is,
// both ways, so the two undo each other exactly.
const LATER_LINE = /\n(?=[^\S\n]*\S)/g;

export function commonIndent(source: string): string {
  const indents = [...source.matchAll(/\n([ \t]*)\S/g)].map((match) => match[1]);
  return indents.reduce((shared, next) => {
    let length = 0;
    while (length < shared.length && shared[length] === next[length]) length++;
    return shared.slice(0, length);
  }, indents[0] ?? "");
}

export function dedent(source: string, indent: string): string {
  return source.split("\n").map((line, i) => (i > 0 && /\S/.test(line) ? line.slice(indent.length) : line)).join("\n");
}

export function indent(source: string, indent: string): string {
  return source.replace(LATER_LINE, () => `\n${indent}`);
}

// A block dropped on an expression takes it into the place the catalog's
// template marks with $: the slot that reads as what the block works on. The
// other slots are names to fill in.
export function wrap(template: string, inner: string): string {
  return template.replace("$", inner.trim() || "value");
}

// argsText is the arguments object as JSON text, from what was typed for each
// argument. It is sent as text so no digit of a large integer is lost here;
// the server decodes it and says which value is wrong.
export function argsText(values: { name: string; text: string }[]): string {
  return `{${values.map(({ name, text }) => `${JSON.stringify(name)}: ${text.trim() || "null"}`).join(", ")}}`;
}
