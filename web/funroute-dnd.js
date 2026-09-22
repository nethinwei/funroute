// Drag and drop for the canvas. Every expression position is a target, whether
// or not it already holds something: that is what lets control blocks nest.
//
// The rules, in one place, so any sequence of drops stays coherent:
//   empty position      → the block goes in
//   occupied position   → the block goes in and takes what was there into its
//                         own first empty slot; with no slot to take it, the
//                         block replaces it
//   a node onto itself
//   or into its subtree → refused, because a node cannot contain itself
//   moving a node       → it leaves its old position, which becomes empty

import { firstEmptySlot } from "./funroute-core.js";

export const DND_MIME = "application/x-funroute-node";

// dropTarget marks element as a place a block may land on.
export function dropTarget(element, onDrop) {
  element.addEventListener("dragover", (event) => {
    if (!event.dataTransfer.types.includes(DND_MIME)) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
    element.classList.add("is-over");
  });
  element.addEventListener("dragleave", () => element.classList.remove("is-over"));
  element.addEventListener("drop", (event) => {
    event.preventDefault();
    event.stopPropagation();
    element.classList.remove("is-over");
    let payload;
    try { payload = JSON.parse(event.dataTransfer.getData(DND_MIME)); }
    catch { return; }
    onDrop(payload);
  });
  return element;
}

// placeBlock puts incoming where existing is, keeping existing when the block
// has room for it. It returns the node that belongs at the position.
export function placeBlock(incoming, existing, nodes) {
  if (!existing) return incoming;
  const slot = firstEmptySlot(incoming, nodes);
  if (!slot) return incoming;
  const [field, index] = slot;
  if (index === undefined) incoming[field] = existing;
  else incoming[field][index] = existing;
  return incoming;
}
