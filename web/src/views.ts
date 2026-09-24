// The expression has two views, code and structure, over one text. The text
// is the only source: the structure view edits ranges of it and never prints
// a program back, so going from one view to the other changes nothing about
// what was written — no sugar is taken away. What decides whether the view
// may change is only whether the text is sound.

export type View = "code" | "structure";

// Checked is what the language server last said of a text: its first error,
// if it had one.
export type Checked = { source: string; error?: string };

// switchRefusal says why the view may not change now, or "" when it may. The
// server must have checked this very text, and found no error in it: a text
// with an error has no structure to draw, and the structure view is no place
// to repair one.
export function switchRefusal(checked: Checked | null, source: string): string {
  if (!checked || checked.source !== source) return "正在检查表达式，稍后再切换";
  if (checked.error) return `表达式有错误，改好后才能切换：${checked.error}`;
  return "";
}
