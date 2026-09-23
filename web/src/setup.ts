// How editing feels: basicSetup taken apart and put back the way this editor
// wants it — VS Code's behaviour (Tab takes a completion, brackets close
// themselves, Alt-click adds a cursor, Shift-Alt-drag selects a column) under
// Emacs's keys. Emacs comes first so its bindings win over the defaults;
// what it leaves free falls through to CodeMirror's own, which follow VS Code.
import { emacs } from "@replit/codemirror-emacs";
import {
  EditorView, crosshairCursor, drawSelection, dropCursor, highlightActiveLine, highlightActiveLineGutter,
  highlightSpecialChars, keymap, lineNumbers, placeholder, rectangularSelection,
} from "@codemirror/view";
import { EditorState, Prec } from "@codemirror/state";
import { bracketMatching, indentUnit } from "@codemirror/language";
import {
  cursorDocEnd, cursorDocStart, defaultKeymap, history, historyKeymap, indentLess, indentMore, toggleComment,
} from "@codemirror/commands";
import { highlightSelectionMatches, searchKeymap } from "@codemirror/search";
import { acceptCompletion, autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from "@codemirror/autocomplete";
import { lintKeymap } from "@codemirror/lint";

// What the editor knows of FunRoute's text without asking the server: how a
// comment starts (for M-; and Mod-/) and which brackets close themselves.
const funrouteText = EditorState.languageData.of(() => [{
  commentTokens: { line: "//" },
  closeBrackets: { brackets: ["(", "[", "{", "\""] },
}]);

export function editingSetup(run: () => void) {
  return [
    emacs(),
    Prec.highest(keymap.of([
      { key: "Mod-Enter", run: () => (run(), true) },
      // Tab takes the completion on offer, as in VS Code, and otherwise indents.
      { key: "Tab", run: (view) => acceptCompletion(view) || indentMore(view), shift: indentLess },
      // The Emacs package names punctuation by event.code ("Semicolon",
      // "Comma"), so its M-; M-< M-> never match; CodeMirror's keymap reads
      // the key itself, Option on a Mac included.
      { key: "Alt-;", run: toggleComment },
      { key: "Shift-Alt-,", run: cursorDocStart },
      { key: "Shift-Alt-.", run: cursorDocEnd },
    ])),
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    drawSelection(),
    dropCursor(),
    EditorState.allowMultipleSelections.of(true),
    EditorView.clickAddsSelectionRange.of((event) => event.altKey),
    rectangularSelection({ eventFilter: (event) => event.altKey && event.shiftKey }),
    crosshairCursor({ key: "Alt" }),
    bracketMatching(),
    closeBrackets(),
    autocompletion({ icons: true }),
    highlightActiveLine(),
    highlightSelectionMatches(),
    indentUnit.of("  "),
    EditorState.tabSize.of(2),
    funrouteText,
    placeholder("写一个表达式，例如 amount * bps / 10000"),
    keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...completionKeymap, ...lintKeymap]),
  ];
}
