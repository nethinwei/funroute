// The contract panel: the host's declaration of a rule's types and
// arguments, written as text. The result is declared in the test run, beside
// the value it describes. It sends what was written; the language server
// reads it and says in its diagnostics what is wrong with it.
import { LitElement, css, html } from "lit";
import type { ArgSpec, TextContract } from "./protocol.ts";
import { define, fieldStyles, labelStyles } from "./ui.ts";

// A type row is a name and its type text; an argument row is an ArgSpec.
type Row = ArgSpec;

export class ContractPanel extends LitElement {
  static properties = { types: { state: true }, args: { state: true } };
  declare types: Row[];
  declare args: Row[];

  constructor() {
    super();
    this.types = [];
    this.args = [];
  }

  set contract(contract: TextContract) {
    this.types = Object.entries(contract.types ?? {}).map(([name, type]) => ({ name, type }));
    this.args = (contract.args ?? []).map((arg) => ({ ...arg }));
  }

  // contract is the types and arguments as written, blank rows left out.
  get contract(): TextContract {
    const types: Record<string, string> = {};
    for (const row of this.types) if (row.name.trim()) types[row.name.trim()] = row.type.trim();
    return {
      types: Object.keys(types).length ? types : undefined,
      args: this.args.filter((arg) => arg.name.trim()).map(({ name, type, doc }) => ({ name: name.trim(), type: type.trim(), doc: doc || undefined })),
    };
  }

  // changed tells the page; it reads contract for what changed.
  private changed() {
    this.requestUpdate();
    this.dispatchEvent(new Event("contract-change", { bubbles: true, composed: true }));
  }

  private field(row: Row, key: keyof Row, label: string, placeholder: string, wide = false) {
    return html`<input class=${wide ? "wide" : ""} .value=${row[key] ?? ""} placeholder=${placeholder} aria-label=${label} spellcheck="false"
      @change=${(event: Event) => { row[key] = (event.target as HTMLInputElement).value; this.changed(); }}>`;
  }

  private removeButton(rows: Row[], row: Row, what: string) {
    return html`<button class="remove" title="删除" aria-label=${`删除${what} ${row.name}`} @click=${() => {
      rows.splice(rows.indexOf(row), 1);
      this.changed();
    }}>−</button>`;
  }

  render() {
    return html`<div class="panel">
      <section>
        <h3 class="label">类型</h3>
        ${this.types.map((row) => html`<div class="row">${this.field(row, "name", "类型名", "名字")}<span>=</span>
          ${this.field(row, "type", "类型定义", "record{amount: int}", true)}${this.removeButton(this.types, row, "类型")}</div>`)}
        <button class="add" @click=${() => { this.types = [...this.types, { name: "", type: "" }]; }}>+ 类型</button>
      </section>
      <section>
        <h3 class="label">参数</h3>
        ${this.args.map((row) => html`<div class="row">${this.field(row, "name", "参数名", "名字")}${this.field(row, "type", "参数类型", "类型")}
          ${this.field(row, "doc", "参数说明", "说明", true)}${this.removeButton(this.args, row, "参数")}</div>`)}
        <button class="add" @click=${() => { this.args = [...this.args, { name: "", type: "", doc: "" }]; }}>+ 参数</button>
      </section></div>`;
  }

  static styles = [fieldStyles, labelStyles, css`
    /* The layout is on an inner element, whatever display the page gives the host. */
    :host { display: block; padding: 14px 18px 16px; }
    .panel { display: grid; gap: 14px; }
    .label { margin-bottom: 6px; }
    .row { display: flex; gap: 6px; align-items: center; margin-bottom: 6px; }
    .row span { color: var(--muted); }
    input { flex: 0 1 150px; }
    input.wide { flex: 1 1 260px; }
    button { padding: 5px 9px; border: 1px solid var(--line); border-radius: 8px; color: var(--ink-3); background: var(--surface); cursor: pointer; }
    button:focus-visible { outline: 3px solid var(--ring); }
    button.add { font-size: 11px; font-weight: 700; }
  `];
}

define("fr-contract", ContractPanel);
