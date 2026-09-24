// The text editor: CodeMirror with the language server behind it. Completion,
// hover, signature help, diagnostics and formatting come with the client; the
// one thing it lacks is semantic highlighting, which is the plugin below.
import { EditorView, Decoration, ViewPlugin } from "@codemirror/view";
import type { DecorationSet, ViewUpdate } from "@codemirror/view";
import { RangeSetBuilder } from "@codemirror/state";
import type { LSPClient } from "@codemirror/lsp-client";
import { decodeTokens } from "./tokens.ts";
import { editingSetup } from "./setup.ts";
import { offsetAt } from "./projection.ts";

// semanticTokens asks the server what each piece of the text is and marks
// it with fr-tok-<type>; how each type looks is the stylesheet's business.
// Until the answer for a new text comes, the marks move with the edits.
function semanticTokens(client: LSPClient, uri: string) {
  return ViewPlugin.fromClass(class {
    decorations: DecorationSet = Decoration.none;
    private timer = 0;
    readonly view: EditorView;
    constructor(view: EditorView) {
      this.view = view;
      this.schedule();
    }
    update(update: ViewUpdate) {
      if (!update.docChanged) return;
      this.decorations = this.decorations.map(update.changes);
      this.schedule();
    }
    destroy() { clearTimeout(this.timer); }
    schedule() {
      clearTimeout(this.timer);
      this.timer = window.setTimeout(() => void this.refresh(), 120);
    }
    async refresh() {
      await client.initializing;
      client.sync();
      const version = this.view.state.doc;
      const legend: string[] = (client.serverCapabilities as any)?.semanticTokensProvider?.legend?.tokenTypes ?? [];
      const result = await client.request<object, { data: number[] }>("textDocument/semanticTokens/full", { textDocument: { uri } });
      if (this.view.state.doc !== version) return;
      const builder = new RangeSetBuilder<Decoration>();
      for (const token of decodeTokens(result.data, legend)) {
        const from = offsetAt(version, token.start);
        const kind = token.declaration ? `${token.type} fr-tok-declaration` : token.type;
        builder.add(from, from + token.length, Decoration.mark({ class: `fr-tok-${kind}` }));
      }
      this.decorations = builder.finish();
      // An empty transaction has the view read the marks again.
      this.view.dispatch({});
    }
  }, { decorations: (plugin) => plugin.decorations });
}

// The editor's look, from the palette in tokens.css, so it needs nothing of
// the page it is put in. The box is filled rather than outlined: a tinted
// field, whose focus is a ring and an accent bar down the left edge, the way
// the structure view marks its cards. The cursor is a block the width of a character, translucent so the
// character under it stays readable, in the ink colour of the current theme.
const editorTheme = EditorView.theme({
  "&": { flex: "1", minHeight: "120px", border: "none", borderLeft: "3px solid transparent", borderRadius: "11px",
    background: "var(--field)", color: "var(--ink)", transition: "background .15s, border-color .15s",
    font: "13px/1.75 var(--mono)" },
  "&:hover": { background: "var(--field-hover)" },
  "&.cm-focused": { outline: "3px solid var(--ring)", background: "var(--field)", borderLeftColor: "var(--violet)" },
  // The scroller holds the gutter and the lines: rounding it keeps their
  // backgrounds inside the editor's corners. Tooltips live outside it.
  ".cm-scroller": { borderRadius: "0 10px 10px 0", fontFamily: "inherit" },
  ".cm-gutters": { border: "none", background: "transparent", color: "var(--muted-2)" },
  ".cm-activeLine, .cm-activeLineGutter": { background: "var(--violet-ghost)" },
  ".cm-cursor, .cm-dropCursor": { borderLeft: "none", width: "1ch", background: "color-mix(in srgb, var(--ink) 55%, transparent)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": { background: "var(--ring)" },
  // The rest follows VS Code: a box round the matching bracket, a quiet mark
  // on the other copies of the selection, a completion list with the kind's
  // icon, the label and the signature on the right.
  ".cm-matchingBracket, &.cm-focused .cm-matchingBracket": { background: "transparent", outline: "1px solid var(--muted-2)", borderRadius: "2px" },
  ".cm-nonmatchingBracket": { outline: "1px solid var(--danger)" },
  ".cm-selectionMatch": { background: "var(--violet-ghost)" },
  ".cm-placeholder": { color: "var(--muted-2)" },
  ".cm-tooltip.cm-tooltip-autocomplete > ul": { maxHeight: "16em", fontFamily: "inherit", padding: "3px" },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li": { display: "flex", alignItems: "center", gap: "6px", padding: "1px 8px 1px 4px", borderRadius: "5px", lineHeight: "22px" },
  ".cm-tooltip-autocomplete ul li[aria-selected]": { background: "var(--violet-soft)", color: "var(--ink)" },
  ".cm-completionIcon": { width: "16px", padding: "0", opacity: "1", textAlign: "center", fontSize: "12px" },
  ".cm-completionIcon-function, .cm-completionIcon-method": { color: "var(--tok-form)" },
  ".cm-completionIcon-variable": { color: "var(--blue-ink)" },
  ".cm-completionIcon-keyword": { color: "var(--ink-3)" },
  ".cm-completionIcon-enum, .cm-completionIcon-constant": { color: "var(--tok-enum)" },
  ".cm-completionLabel": { color: "var(--ink)" },
  ".cm-completionMatchedText": { textDecoration: "none", color: "var(--violet-ink)", fontWeight: "700" },
  ".cm-completionDetail": { marginLeft: "auto", paddingLeft: "16px", color: "var(--muted)", fontStyle: "normal", fontSize: "11px",
    overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: "26em" },
  ".cm-completionInfo": { padding: "6px 10px", fontSize: "12px", maxWidth: "28em" },
  ".cm-diagnostic": { padding: "4px 10px", borderLeftWidth: "3px", fontFamily: "inherit" },
  ".cm-diagnostic-error": { borderLeftColor: "var(--danger)" },
  ".cm-lintRange-error": { backgroundImage: "none", textDecoration: "underline wavy var(--danger) 1px", textUnderlineOffset: "3px" },
  ".cm-panels": { background: "var(--surface)", color: "var(--ink-2)", borderColor: "var(--line)" },
  ".cm-panels.cm-panels-bottom": { borderTop: "1px solid var(--line)", borderRadius: "0 0 10px 0" },
  ".cm-search": { fontFamily: "inherit", fontSize: "12px" },
  ".cm-textfield": { border: "1px solid var(--line)", borderRadius: "6px", background: "var(--field)", color: "var(--ink)" },
  ".cm-button": { border: "1px solid var(--line)", borderRadius: "6px", backgroundImage: "none", background: "var(--surface-2)", color: "var(--ink-2)" },
  ".cm-tooltip": { border: "1px solid var(--line)", borderRadius: "10px", background: "var(--surface)", color: "var(--ink-2)" },
  ".cm-tooltip .cm-lsp-documentation, .cm-tooltip .cm-lsp-signature-tooltip": { padding: "6px 10px", fontSize: "12px" },
  // What the language server says each piece is, and how each kind looks:
  // names neutral, structure purple, calls blue, literals cool, enums warm.
  ".fr-tok-variable, .fr-tok-parameter": { color: "var(--tok-var)" },
  ".fr-tok-parameter": { fontWeight: "650" },
  ".fr-tok-declaration": { textDecoration: "underline dotted var(--muted-2)" },
  ".fr-tok-function": { color: "var(--blue-ink)" },
  ".fr-tok-keyword": { color: "var(--tok-form)", fontWeight: "700" },
  ".fr-tok-property": { color: "var(--ink-3)" },
  ".fr-tok-enumMember": { color: "var(--tok-enum)", fontWeight: "600" },
  ".fr-tok-currency": { color: "var(--tok-number)", fontWeight: "650" },
  ".fr-tok-string": { color: "var(--tok-string)" },
  ".fr-tok-number": { color: "var(--tok-number)" },
  ".fr-tok-operator": { color: "var(--tok-op)" },
  ".fr-tok-comment": { color: "var(--tok-comment)", fontStyle: "italic" },
});

type EditorOptions = { client: LSPClient; uri: string; parent: HTMLElement; onRun?: () => void };

export function createEditor({ client, uri, parent, onRun }: EditorOptions): EditorView {
  return new EditorView({
    parent,
    extensions: [
      editingSetup(() => onRun?.()),
      editorTheme,
      client.plugin(uri, "funroute"),
      semanticTokens(client, uri),
      EditorView.lineWrapping,
    ],
  });
}

// replaceAll sets the whole text, as picking an example does.
export function replaceAll(view: EditorView, text: string) {
  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
}
