// The component palette. Only control blocks are components: they carry
// branches, loops and local names, which a slot's single line of text cannot
// express. Everything else is typed into a slot, so functions are listed here
// for discovery only — clicking one writes its name where the caret is.

import { DND_MIME } from "./funroute-dnd.js";
import { lookFor } from "./funroute-display.js";

export function renderPalette({ templates, selectedId, onSelect }) {
  const palette = el("aside", "fr-palette");
  const header = el("div", "fr-palette__header");
  header.append(el("strong", "", "添加组件"), el("small", "fr-muted", "拖到任意表达式位置"));
  const list = el("div", "fr-palette__list");
  paletteBlocks(list, templates, selectedId, onSelect);
  palette.append(header, list);
  return { palette, list };
}

// The palette holds the control blocks the registry enables — six of them at
// most — so they are all on screen at once and there is nothing to search.
function paletteBlocks(list, templates, selectedId, onSelect) {
  const entries = [...templates];
  if (!entries.length) return;
  const section = el("section", "fr-palette-group");
  section.append(el("h3", "", "控制块"));
  for (const [id, template] of entries) {
    section.append(blockCard(id, { ...template.descriptor.doc, ...template.descriptor }, selectedId, onSelect));
  }
  list.append(section);
}

function blockCard(templateId, descriptor, selectedId, onSelect) {
  const card = el("div", `fr-palette-card${selectedId === templateId ? " is-selected" : ""}`);
  card.draggable = true;
  const display = descriptor.doc || descriptor;
  const look = lookFor(descriptor.name || descriptor.special || templateId);
  card.style.setProperty("--fr-accent", look.color);
  const body = el("span", "fr-palette-card__body");
  body.append(el("strong", "", display.label || templateId));
  if (display.description) body.append(el("small", "", display.description));
  card.append(el("span", "fr-palette-card__icon", look.icon), body);
  card.title = descriptor.signature || display.description || "";
  card.addEventListener("dragstart", (event) => {
    event.dataTransfer.effectAllowed = "copy";
    event.dataTransfer.setData(DND_MIME, JSON.stringify({ origin: "palette", templateId }));
  });
  card.addEventListener("click", () => onSelect(templateId));
  return card;
}

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}
