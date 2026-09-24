// The contract panel: the host's declaration of a rule's types and
// arguments, written as text. The result is declared in the test run, beside
// the value it describes. It sends what was written; the language server
// reads it and says in its diagnostics what is wrong with it.
import { LitElement, css, html, nothing } from "lit";
import { state } from "lit/decorators.js";
import type { ArgSpec, TextContract } from "./protocol.ts";
import { define, emit, fieldStyles, labelStyles } from "./ui.ts";

// A type row is a name and its type text; an argument row is an ArgSpec.
type Row = ArgSpec;

// A column is one input of a row, with what stands before it, if anything.
type Column = { key: keyof Row; label: string; placeholder: string; wide?: boolean; before?: string };

// A section is the rows of one list, each with a button that removes it, and
// a button that adds a blank row.
type Section = { rows: "types" | "args"; title: string; blank: Row; columns: Column[] };

const SECTIONS: Section[] = [
  { rows: "types", title: "类型", blank: { name: "", type: "" }, columns: [
    { key: "name", label: "类型名", placeholder: "名字" },
    { key: "type", label: "类型定义", placeholder: "record{amount: int}", wide: true, before: "=" },
  ] },
  { rows: "args", title: "参数", blank: { name: "", type: "", doc: "" }, columns: [
    { key: "name", label: "参数名", placeholder: "名字" },
    { key: "type", label: "参数类型", placeholder: "类型，如 int、money、array<fxrate>" },
    { key: "doc", label: "参数说明", placeholder: "说明", wide: true },
  ] },
];

export class ContractPanel extends LitElement {
  @state() accessor types: Row[] = [];
  @state() accessor args: Row[] = [];

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
    emit(this, "contract-change");
  }

  private field(row: Row, { key, label, placeholder, wide, before }: Column) {
    return html`${before ? html`<span>${before}</span>` : nothing}<input class=${wide ? "wide" : ""} .value=${row[key] ?? ""} placeholder=${placeholder} aria-label=${label} spellcheck="false"
      @change=${(event: Event) => { row[key] = (event.target as HTMLInputElement).value; this.changed(); }}>`;
  }

  private removeButton(rows: Row[], row: Row, what: string) {
    return html`<button class="remove" title="删除" aria-label=${`删除${what} ${row.name}`} @click=${() => {
      rows.splice(rows.indexOf(row), 1);
      this.changed();
    }}>−</button>`;
  }

  private section({ rows, title, blank, columns }: Section) {
    return html`<section>
      <h3 class="label">${title}</h3>
      ${this[rows].map((row) => html`<div class="row">${columns.map((column) => this.field(row, column))}${this.removeButton(this[rows], row, title)}</div>`)}
      <button class="add" @click=${() => { this[rows] = [...this[rows], { ...blank }]; }}>+ ${title}</button>
    </section>`;
  }

  render() {
    return html`<div class="panel">${SECTIONS.map((section) => this.section(section))}</div>`;
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
    button.add { font-size: 11px; font-weight: 700; }
  `];
}

define("fr-contract", ContractPanel);
