// Input controls that both the canvas and the run panel need. A member set is
// the host's data and can be large — 200 countries is a normal contract — so
// the control switches shape with its size: a select while the whole set is
// readable, a filtering input backed by one shared datalist when it is not.

import { typeSummary } from "./funroute-core.js";

// SELECT_LIMIT is where a dropdown stops being readable. Above it the browser's
// own filtering does the work, and the members are listed once per document
// rather than once per card.
const SELECT_LIMIT = 12;

export function enumControl({ values, value, placeholder, listHost, listId, onChange }) {
  const members = values || [];
  if (members.length && members.length <= SELECT_LIMIT) {
    return selectControl(members, value, onChange);
  }
  return filterControl(members, value, placeholder, listHost, listId, onChange);
}

function selectControl(members, value, onChange) {
  const select = document.createElement("select");
  select.className = "fr-input";
  for (const member of members) {
    const option = document.createElement("option");
    option.value = member;
    option.textContent = member;
    option.selected = member === value;
    select.append(option);
  }
  select.addEventListener("change", () => onChange(select.value));
  return select;
}

function filterControl(members, value, placeholder, listHost, listId, onChange) {
  const input = document.createElement("input");
  input.className = "fr-input";
  input.value = value ?? "";
  input.placeholder = placeholder || `输入以筛选 ${members.length} 个成员`;
  if (members.length) {
    input.setAttribute("list", ensureDataList(listHost, listId, members));
  }
  input.addEventListener("change", () => onChange(input.value.trim()));
  return input;
}

// ensureDataList renders a member set once per host, however many controls
// refer to it. The host is the document or a shadow root.
function ensureDataList(listHost, listId, members) {
  const host = listHost || document;
  const existing = host.getElementById ? host.getElementById(listId) : host.querySelector(`#${listId}`);
  if (existing) return listId;
  const list = document.createElement("datalist");
  list.id = listId;
  for (const member of members) {
    const option = document.createElement("option");
    option.value = member;
    list.append(option);
  }
  (host.body || host).append(list);
  return listId;
}

// enumOf finds the enum inside a type, so a control can be offered for
// array<enum> and dict<enum> arguments too.
export function enumOf(type) {
  if (!type) return null;
  if (type.kind === "enum") return type;
  return type.elem ? enumOf(type.elem) : null;
}

export { typeSummary };
