import { typeName } from "./funroute-core.js";

// Display-side helpers: how a type is spelled for a person, and how a node
// looks on a card. The catalog carries meaning only — a host says what a
// function is called and what it costs, never what colour it is — so every
// colour and icon in the console is decided here.

// A value node's look. The catalog already says what each one is called and
// means, so only the drawing is here.
const VALUE_LOOK = {
  var: { color: "#475569", icon: "𝑥" },
  int: { color: "#2563EB", icon: "1" },
  float: { color: "#0891B2", icon: "." },
  string: { color: "#059669", icon: "”" },
  bool: { color: "#0EA5E9", icon: "?" },
  enum: { color: "#C2410C", icon: "@" },
  array: { color: "#D97706", icon: "[ ]" },
  dict: { color: "#EA580C", icon: "{ }" },
};

// Control blocks are the only things the canvas draws as cards, and there are
// six of them, so their look is spelled out. Everything else is neutral.
const CONTROL_LOOK = {
  if: { color: "#7C3AED", icon: "◇" },
  fallback: { color: "#7C3AED", icon: "↘" },
  switch: { color: "#6D28D9", icon: "≡" },
  for: { color: "#0891B2", icon: "∀" },
  reduce: { color: "#2563EB", icon: "Σ" },
  let: { color: "#4338CA", icon: "≔" },
};

const NEUTRAL_LOOK = { color: "#64748B", icon: "ƒ" };

// lookFor answers with the colour and icon a name should be drawn in. The name
// is a control block's, a node tag's, or a function's; only the first has a
// look of its own.
export function lookFor(name) {
  return CONTROL_LOOK[name] || VALUE_LOOK[name] || NEUTRAL_LOOK;
}

// typeSummary is typeName for display: a large enum shows a few members and a
// count instead of all of them. Never send this to the server — the contract
// carries the full type text, which ParseType has to be able to read back.
export { typeName };

export function typeSummary(type, limit = 6) {
  if (!type) return "unknown";
  if (type.kind === "array" || type.kind === "dict") return `${type.kind}<${typeSummary(type.elem, limit)}>`;
  const values = type.values || [];
  if (type.kind !== "enum" || values.length <= limit) return typeName(type);
  return `enum<${type.name}>{${values.slice(0, limit).join(",")}… 共 ${values.length} 个}`;
}

