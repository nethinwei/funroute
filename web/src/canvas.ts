// The structure view: the server's syntax tree drawn as cards for the blocks
// and lines of source for everything else. It edits nothing itself — every
// change it makes is a replacement of a range of the text, which the page
// applies to the editor; the next tree comes from the server.
import { LitElement, css, html, svg } from "lit";
import type { TemplateResult } from "lit";
import type { Range, Tree, TreeField } from "./protocol.ts";
import { BLOCKS, Text, callName, commonIndent, dedent, indent, isCard, isPlain, wrap } from "./projection.ts";
import { define, fieldStyles } from "./ui.ts";

// An edit replaces range with text. source is the text the range was read
// from: the page applies the edit only if the editor still holds it, since a
// range into an older text would land in the wrong place.
export type Edit = { range: Range; text: string; source: string };

// A block as the catalog describes it.
export type Block = { label: string; description: string };

// How each block looks: an icon in the style of the page's own (16 units,
// strokes of 1.4, no fill) and an accent from the palette in tokens.css. The
// palette and the cards draw from this one table.
const LOOKS: Record<string, { accent: string; icon: ReturnType<typeof svg> }> = {
  if: { accent: "var(--violet)", icon: svg`<path d="M8 1.8 14.2 8 8 14.2 1.8 8z"/><path d="M8 5.2v3.4M8 10.6v.2"/>` },
  fallback: { accent: "var(--amber)", icon: svg`<path d="M3 2.5v5a3 3 0 0 0 3 3h7"/><path d="m10.2 7.7 2.8 2.8-2.8 2.8"/>` },
  switch: { accent: "var(--tok-form)", icon: svg`<path d="M1.8 8h3.4"/><path d="M5.2 8c2 0 1.8-4.3 4-4.3h4.8M5.2 8H14M5.2 8c2 0 1.8 4.3 4 4.3h4.8"/>` },
  for: { accent: "var(--tok-number)", icon: svg`<path d="M2.5 7.5A4.5 4.5 0 0 1 7 3h5.5"/><path d="m10.5 1 2 2-2 2"/><path d="M13.5 8.5A4.5 4.5 0 0 1 9 13H3.5"/><path d="m5.5 15-2-2 2-2"/>` },
  reduce: { accent: "var(--blue-ink)", icon: svg`<path d="M2 2.8h12L9.4 8.4v4.4l-2.8 1.4V8.4z"/>` },
  let: { accent: "var(--green)", icon: svg`<rect x="2" y="2.8" width="12" height="10.4" rx="2.4"/><path d="M5.4 6.8h5.2M5.4 9.4h5.2"/>` },
};

const icon = (name: string) => html`<span class="icon" style="--accent: ${LOOKS[name]?.accent ?? "var(--slate)"}">
  <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${LOOKS[name]?.icon}</svg></span>`;

// The console's wording for the fields a node has; the fields themselves are
// the tree's. A name two nodes share with different meanings is worded per
// node ("let.value"); the rest by name alone.
const FIELD_TEXT: Record<string, string> = {
  "switch.value": "待匹配值", "let.value": "值", cases: "分支", match: "匹配", result: "结果", default: "否则", source: "输入",
  variable: "元素名", key_variable: "键名", where: "筛选", yield_key: "键", yield: "产出",
  accumulator: "累加器", init: "初值", body: "主体", bindings: "绑定", name: "名字", args: "实参",
};

export class StructureView extends LitElement {
  static properties = { tree: { attribute: false }, text: { attribute: false }, blocks: { attribute: false }, selected: { state: true } };
  declare tree: Tree | null;
  declare text: Text;
  // blocks is what the catalog says of each function and form, by name; the
  // palette offers the ones that wrap an expression.
  declare blocks: Map<string, Block>;
  declare selected: string;
  lazy = new Set<string>();

  constructor() {
    super();
    this.tree = null;
    this.text = new Text("");
    this.blocks = new Map();
    this.selected = "";
  }

  private edit(range: Range, text: string) {
    const detail: Edit = { range, text, source: this.text.source };
    this.dispatchEvent(new CustomEvent<Edit>("edit", { detail, bubbles: true, composed: true }));
  }

  // A slot is one expression as source, as many lines as it has: typing
  // replaces it, and with a block selected a click or Enter wraps it in the
  // block.
  private slotFor(tree: Tree) {
    const source = this.text.slice(tree.range);
    const shared = commonIndent(source);
    const wrapIt = () => {
      if (!this.selected) return;
      this.edit(tree.range, wrap(this.selected, source));
      this.selected = "";
    };
    const enter = (event: KeyboardEvent) => {
      if (this.selected && event.key === "Enter") {
        event.preventDefault();
        wrapIt();
      }
    };
    return html`<textarea class="slot ${this.selected ? "ready" : ""}" rows="1" .value=${dedent(source, shared)} spellcheck="false"
      aria-label="表达式" @click=${wrapIt} @keydown=${enter}
      @change=${(event: Event) => this.edit(tree.range, indent((event.target as HTMLTextAreaElement).value, shared))}></textarea>`;
  }

  private node(tree: Tree): TemplateResult {
    if (isPlain(tree, this.lazy)) return this.slotFor(tree);
    const name = tree.node === "call" ? callName(tree) : tree.node;
    const fields = (tree.fields ?? []).filter((field) => !(tree.node === "call" && field.name === "name"));
    // Not a block but holding one — say (a + b) where b is a switch: say
    // what joins the parts, and show each part.
    if (!isCard(tree, this.lazy)) {
      return html`<div class="plain"><code class="joins">${tree.operator ?? name}</code>${fields.flatMap((field) => field.nodes ?? []).map((node) => this.node(node))}</div>`;
    }
    return html`<div class="card" style="--accent: ${LOOKS[name]?.accent ?? "var(--violet)"}">
      <button type="button" class="title ${this.selected ? "ready" : ""}" ?disabled=${!this.selected} title=${this.selected ? "用选中的块包起来" : ""}
        @click=${() => this.edit(tree.range, wrap(this.selected, this.text.slice(tree.range)))}>
      ${icon(name)}${this.blocks.get(name)?.label ?? name}<code>${name}</code></button>${this.fields(tree.node, fields)}</div>`;
  }

  private fields(node: string, fields: TreeField[]): TemplateResult[] {
    const label = (name: string) => FIELD_TEXT[`${node}.${name}`] ?? FIELD_TEXT[name] ?? name;
    return fields.filter((field) => !field.flag).map((field) => html`<div class="field"><span>${label(field.name)}</span>
      <div class="values">${this.values(node, field)}</div></div>`);
  }

  private values(node: string, field: TreeField) {
    if (field.textRange) {
      const range = field.textRange;
      return html`<input class="name" .value=${field.text ?? ""} aria-label="名字" spellcheck="false" @change=${(event: Event) => this.edit(range, (event.target as HTMLInputElement).value)}>`;
    }
    if (field.items) return field.items.map((item) => html`<div class="item">${this.fields(node, item)}</div>`);
    return (field.nodes ?? []).map((node) => this.node(node));
  }

  // The palette: one card per block, the way the catalog describes it. Pick
  // one, then click the expression it should take in; pick it again to put
  // it down.
  private palette() {
    const pick = (name: string) => { this.selected = this.selected === name ? "" : name; };
    return html`<aside class="palette" aria-label="控制块">
      <header><strong>控制块</strong><span>${this.selected ? "点一个表达式，用它包起来" : "选一个块，再点表达式"}</span></header>
      <div class="list">${[...this.blocks].filter(([name]) => name in BLOCKS).map(([name, block]) => html`
        <button type="button" class=${this.selected === name ? "on" : ""} aria-pressed=${this.selected === name} @click=${() => pick(name)}>
          ${icon(name)}<span class="body"><strong>${block.label}<code>${name}</code></strong><small>${block.description}</small></span>
        </button>`)}</div></aside>`;
  }

  render() {
    return html`<div class="layout">${this.palette()}<div class="workspace">
      ${this.tree ? this.node(this.tree) : html`<p class="empty">表达式还不能解析；按诊断改好后这里显示结构。</p>`}</div></div>`;
  }

  static styles = [fieldStyles, css`
    /* The palette is a column on the left and the tree takes the rest; in a
       narrow place the palette becomes a row that scrolls. */
    :host { display: block; container-type: inline-size; }
    .layout { display: grid; grid-template-columns: clamp(196px, 22%, 240px) minmax(0, 1fr); min-height: 260px; }
    .palette { display: flex; flex-direction: column; min-width: 0; border-right: 1px solid var(--line-2); background: var(--surface-2); }
    .palette header { display: grid; gap: 3px; padding: 12px 14px; border-bottom: 1px solid var(--line-2); }
    .palette header strong { color: var(--ink-2); font-size: 11px; letter-spacing: .04em; }
    .palette header span { color: var(--muted); font-size: 10px; }
    .list { display: grid; gap: 6px; align-content: start; padding: 9px; overflow: auto; scrollbar-width: thin; }
    button { box-sizing: border-box; }
    .list button { display: flex; gap: 9px; align-items: flex-start; width: 100%; padding: 8px; border: 1px solid var(--line); border-radius: 10px;
      color: inherit; background: var(--surface); font: inherit; text-align: left; cursor: pointer; transition: border-color .15s, box-shadow .15s, transform .15s; }
    .list button:hover { border-color: var(--violet-line); box-shadow: 0 6px 16px var(--shadow-soft); transform: translateX(2px); }
    .list button:focus-visible { outline: 3px solid var(--ring); }
    .list button.on { border-color: var(--violet); background: var(--violet-soft); box-shadow: 0 0 0 2px var(--violet-ghost); }
    .body { display: grid; gap: 2px; min-width: 0; }
    .body strong { display: flex; gap: 6px; align-items: baseline; color: var(--ink-2); font-size: 12px; }
    .body small { color: var(--muted); font-size: 10px; line-height: 1.4; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
    .icon { display: grid; place-items: center; flex: none; width: 24px; height: 24px; border-radius: 7px;
      color: var(--accent); background: color-mix(in srgb, var(--accent) 15%, transparent); }
    .icon svg { width: 15px; height: 15px; }
    /* The tree fills the room beside the palette: the outermost card stretches
       to the palette's height, and every card keeps its fields at the top. */
    .workspace { display: grid; gap: 10px; align-content: stretch; min-width: 0; padding: 14px 18px 18px; }
    code { color: var(--muted); font: 10px var(--mono); }
    .card { display: grid; gap: 6px; align-content: start; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--accent) 30%, var(--line));
      border-left: 3px solid var(--accent); border-radius: 12px; background: var(--surface); box-shadow: 0 6px 16px var(--shadow-node); }
    .title { display: flex; gap: 8px; align-items: center; padding: 0; border: 0; color: var(--ink-2); background: none;
      font: inherit; font-size: 12px; font-weight: 750; text-align: left; }
    .title:disabled { color: var(--ink-2); cursor: default; }
    .title:focus-visible { outline: 3px solid var(--ring); border-radius: 6px; }
    .title .icon { width: 22px; height: 22px; }
    .title.ready { cursor: copy; }
    .field { display: grid; grid-template-columns: 64px minmax(0, 1fr); gap: 8px; align-items: start; }
    .field > span { padding-top: 7px; color: var(--muted); font-size: 11px; }
    .values, .item, .plain { display: grid; gap: 6px; min-width: 0; }
    .item { padding: 6px 8px; border: 1px dashed var(--line-dash); border-radius: 9px; }
    /* A slot grows with the lines it holds and never wraps them: the layout
       of the source is part of what it shows. */
    textarea { field-sizing: content; resize: none; white-space: pre; overflow-x: auto; }
    input.name { max-width: 180px; color: var(--tok-form); }
    .slot.ready { border-style: dashed; border-color: var(--violet-edge); cursor: copy; }
    .empty { margin: 0; color: var(--muted); font-size: 12px; }
    .joins { color: var(--tok-op); font-size: 12px; font-weight: 700; }
    @container (max-width: 640px) {
      .layout { grid-template-columns: minmax(0, 1fr); }
      .palette { border-right: 0; border-bottom: 1px solid var(--line-2); }
      .list { display: flex; overflow-x: auto; }
      .list button { flex: 0 0 200px; }
    }
  `];
}

define("fr-structure", StructureView);
