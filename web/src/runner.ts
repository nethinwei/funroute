// The test run: what the rule promises to return, a value for every argument
// and what running it gave. The declared result is the contract's and is
// edited here, beside the value it describes; the page merges it into the
// contract it sends. The arguments are the program's as the server lists them
// — the contract's, or the ones read from the text when it declares none. The
// server decodes the values against the contract, so a wrong one comes back as
// the server's message.
import { LitElement, css, html, nothing } from "lit";
import type { PropertyValues } from "lit";
import { property, state } from "lit/decorators.js";
import type { Argument, ResultSpec, RunResult } from "./protocol.ts";
import { define, emit, fieldStyles, labelStyles } from "./ui.ts";

export class RunPanel extends LitElement {
  @property({ attribute: false }) accessor args: Argument[] = [];
  @property({ attribute: false }) accessor result: RunResult | null = null;
  @property({ type: Boolean }) accessor busy = false;
  // inferred says the arguments were read from the text, not declared.
  @property({ type: Boolean }) accessor inferred = false;
  @state() accessor returns: ResultSpec = { type: "", doc: "" };
  // elapsed is how long the run took, measured by the page around the request.
  @state() accessor elapsed = 0;
  // values is what was typed for each argument, by name.
  values: Record<string, string> = {};

  // A value typed for an argument that is gone goes with it; one that is
  // still there keeps what was typed.
  willUpdate(changed: PropertyValues<this>) {
    if (changed.has("args")) {
      this.values = Object.fromEntries(this.args.filter(({ name }) => name in this.values).map(({ name }) => [name, this.values[name]]));
    }
  }

  // declared is the result the contract declares, or none.
  get declared(): ResultSpec | undefined {
    const type = this.returns.type.trim();
    return type ? { type, doc: this.returns.doc?.trim() || undefined } : undefined;
  }

  set declared(result: ResultSpec | undefined) {
    this.returns = { type: result?.type ?? "", doc: result?.doc ?? "" };
  }

  entries() {
    return this.args.map(({ name }) => ({ name, text: this.values[name] ?? "" }));
  }

  private run() {
    emit(this, "run");
  }

  private setReturns(key: keyof ResultSpec, value: string) {
    this.returns = { ...this.returns, [key]: value };
    emit(this, "returns-change");
  }

  private returnsSection() {
    const edit = (key: keyof ResultSpec) => (event: Event) => this.setReturns(key, (event.target as HTMLInputElement).value);
    return html`<section class="returns" aria-label="返回契约">
      <div class="tag"><span aria-hidden="true">→</span>返回</div>
      <input class="type" .value=${this.returns.type} placeholder="推导" spellcheck="false" aria-label="返回类型" @change=${edit("type")}>
      <input class="doc" .value=${this.returns.doc ?? ""} placeholder="说明返回值的含义" aria-label="返回说明" @change=${edit("doc")}></section>`;
  }

  private argRow({ name, type, doc, example }: Argument) {
    const id = `arg-${name}`;
    return html`<div class="arg">
      <label for=${id}><strong>${name}</strong><code>${type}</code></label>
      ${doc ? html`<p class="doc">${doc}</p>` : nothing}
      <input id=${id} .value=${this.values[name] ?? ""} placeholder=${example} spellcheck="false"
        @input=${(event: Event) => { this.values[name] = (event.target as HTMLInputElement).value; }}
        @keydown=${(event: KeyboardEvent) => { if (event.key === "Enter") this.run(); }}></div>`;
  }

  private argsSection() {
    const empty = this.inferred ? "表达式没有读任何参数。" : "契约里没有参数，表达式也没有读任何参数。";
    return html`<section class="args" aria-label="入参">
      <h4 class="label">入参${this.inferred && this.args.length ? html`<span class="hint">由表达式推导，可在契约里声明</span>` : nothing}</h4>
      ${this.args.length ? this.args.map((arg) => this.argRow(arg)) : html`<p class="empty">${empty}</p>`}</section>`;
  }

  private outcome(result: RunResult) {
    const failed = Boolean(result.error);
    const partial = !failed && result.unavailable.length > 0;
    const state = failed ? "error" : partial ? "partial" : "ok";
    // Success needs no word: the value is the news. Only what went wrong is named.
    const title = failed ? "运行失败" : partial ? "部分函数未执行" : "";
    return html`<section class="outcome ${state}" aria-live="polite">
      <header><span class="time">${this.elapsed.toFixed(1)} ms</span>
        ${title ? html`<span class="badge">${title}</span>` : nothing}</header>
      ${failed ? html`<pre class="value">${result.error!.message}</pre>` : html`<pre class="value">${JSON.stringify(result.value, null, 2)}</pre>`}
      ${partial ? html`<p class="note">浏览器里没有这些函数的实现，调用处按 fallback 兜底，结果不等于宿主上的结果：
        ${result.unavailable.map((name) => html`<code>${name}</code>`)}</p>` : nothing}</section>`;
  }

  render() {
    return html`<div class="panel">
      <header class="heading"><div><h3>试运行</h3><span>在浏览器里编译并运行，不发往任何服务</span></div>
        <button class="run" ?disabled=${this.busy} @click=${this.run} title="⌘ / Ctrl + Enter">
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4.5 2.8v10.4L13 8z" fill="currentColor"/></svg>${this.busy ? "运行中" : "运行"}</button></header>
      ${this.returnsSection()}
      ${this.argsSection()}
      ${this.result ? this.outcome(this.result) : html`<p class="empty">填好入参后运行，结果显示在这里。</p>`}</div>`;
  }

  static styles = [fieldStyles, labelStyles, css`
    /* The layout is on an inner element: a page may give the host its own
       display, and the gaps between the sections must not depend on it. */
    :host { display: block; padding: 15px 18px 16px; }
    .panel { display: grid; gap: 14px; align-content: start; }
    .heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
    .heading > div { display: grid; gap: 3px; }
    h3 { margin: 0; color: var(--ink); font-size: 13px; }
    .heading span { color: var(--muted); font-size: 10px; }
    .run { display: inline-flex; gap: 6px; align-items: center; padding: 7px 12px; border: 0; border-radius: 9px;
      color: var(--on-ink); background: var(--ink); font-family: inherit; font-size: 12px; font-weight: 750; cursor: pointer; }
    .run svg { width: 12px; height: 12px; }
    .run:disabled { opacity: .6; cursor: wait; }
    code { color: var(--violet-ink); font: 10.5px var(--mono); }
    input { width: 100%; }
    .returns { display: grid; grid-template-columns: auto minmax(70px, .8fr) minmax(0, 1.4fr); gap: 6px; align-items: center;
      padding: 8px; border-radius: 10px; background: var(--violet-ghost); }
    .returns .tag { display: flex; gap: 5px; align-items: center; padding: 0 4px; color: var(--violet-ink); font-size: 11px; font-weight: 750; }
    .returns .type { color: var(--violet-ink); }
    .returns .doc { font-family: inherit; }
    .args { display: grid; gap: 10px; }
    .hint { margin-left: 8px; font-weight: 500; letter-spacing: 0; }
    .arg { display: grid; gap: 4px; }
    .arg label { display: flex; gap: 8px; align-items: baseline; color: var(--ink-2); font-size: 12px; }
    .arg strong { font: 700 12px var(--mono); }
    .arg .doc { margin: 0; color: var(--muted); font-size: 11px; line-height: 1.45; }
    .empty { margin: 0; color: var(--muted); font-size: 11px; }
    .outcome { display: grid; gap: 8px; padding: 10px; border: 1px solid var(--line); border-left: 3px solid var(--accent); border-radius: 10px; background: var(--surface); }
    .outcome.ok { --accent: var(--green); }
    .outcome.partial { --accent: var(--amber); }
    .outcome.error { --accent: var(--danger); }
    .outcome header { display: flex; gap: 8px; align-items: center; }
    .badge { padding: 2px 8px; border-radius: 999px; color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent);
      font-size: 11px; font-weight: 750; white-space: nowrap; }
    .time { color: var(--muted); font: 10px var(--mono); }
    .value { margin: 0; max-height: 280px; overflow: auto; color: var(--ink); font: 12px/1.55 var(--mono); white-space: pre-wrap; overflow-wrap: anywhere; }
    .error .value { color: var(--danger-ink); }
    .note { margin: 0; color: var(--ink-3); font-size: 11px; line-height: 1.5; }
    .note code { margin-right: 6px; }
  `];
}

define("fr-runner", RunPanel);
