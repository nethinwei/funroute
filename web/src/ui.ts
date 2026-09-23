// What the workbench's components share: how one is registered, and the look
// of an input and of a section label. Colours and the code font come from
// tokens.css; nothing here defines one.
import { css } from "lit";

// define registers a component once. Each component is its own module, so a
// page imports only the ones it uses — and two bundles that both import one
// still define it a single time.
export function define(name: string, component: CustomElementConstructor) {
  if (!customElements.get(name)) customElements.define(name, component);
}

// Inputs are filled, like the editor: a tinted field, no border until focus.
export const fieldStyles = css`
  input, textarea { box-sizing: border-box; min-width: 0; padding: 7px 9px; border: 1px solid transparent; border-radius: 8px;
    color: var(--ink-2); background: var(--field); font: 12px/1.5 var(--mono); }
  input:hover, textarea:hover { background: var(--field-hover); }
  input:focus, textarea:focus { outline: 3px solid var(--ring); border-color: var(--violet-edge); background: var(--field); }
  input::placeholder, textarea::placeholder { color: var(--muted-2); }
`;

// A section's label: small, spaced capitals in the muted ink.
export const labelStyles = css`
  .label { margin: 0; color: var(--muted); font-size: 10px; font-weight: 750; letter-spacing: .08em; }
`;
