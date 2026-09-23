// The page's connection to the language server, which runs in a worker built
// from lang/lsp. The transport is the one @codemirror/lsp-client takes.
import { LSPClient, formatKeymap, hoverTooltips, serverCompletionSource, serverDiagnostics, signatureHelp } from "@codemirror/lsp-client";
import type { Transport } from "@codemirror/lsp-client";
import type { CompletionContext, CompletionResult } from "@codemirror/autocomplete";
import { EditorState } from "@codemirror/state";
import { keymap } from "@codemirror/view";

// rankedCompletion keeps the server's order: kind first (locals, arguments,
// functions, forms), then name, as its sortText says. CodeMirror would rank
// by how well each label fuzzily matches and use sortText only to break ties,
// so the source narrows to the labels the typed word begins and turns
// CodeMirror's own filter off; with no validFor it asks again on every key.
async function rankedCompletion(context: CompletionContext): Promise<CompletionResult | null> {
  const result = await serverCompletionSource(context);
  if (!result) return null;
  const typed = context.state.sliceDoc(result.from, context.pos).toLowerCase();
  const options = result.options
    .filter((option) => option.label.toLowerCase().startsWith(typed))
    .sort((a, b) => (a.sortText ?? a.label).localeCompare(b.sortText ?? b.label));
  return { ...result, options, filter: false, validFor: undefined };
}

// What languageServerExtensions() gives, with rankedCompletion in place of
// its completion source, and only the keys of what the server answers:
// formatting, but not rename, definition or references, which it does not.
const editorFeatures = [
  EditorState.languageData.of(() => [{ autocomplete: rankedCompletion }]),
  hoverTooltips(),
  keymap.of(formatKeymap),
  signatureHelp(),
  serverDiagnostics(),
];

export function workerTransport(worker: Worker): Transport {
  const handlers = new Set<(message: string) => void>();
  worker.addEventListener("message", (event: MessageEvent<string>) => {
    for (const handler of handlers) handler(event.data);
  });
  return {
    send: (message) => worker.postMessage(message),
    subscribe: (handler) => void handlers.add(handler),
    unsubscribe: (handler) => void handlers.delete(handler),
  };
}

// The worker next to the workbench's page. A page elsewhere passes its own.
const defaultWorker = () => new Worker(new URL("../funroute-lsp-worker.js", import.meta.url));

// The client asks for no position encoding, so the server counts UTF-16
// units — what JavaScript strings count, and what editor.ts offsetAt and
// projection.ts Text assume.
//
// startClient connects a client to a fresh server. listen hears every
// notification the server sends, before the editor's own handlers do.
export function startClient(listen: (method: string, params: any) => void = () => {}, worker: Worker = defaultWorker()): LSPClient {
  const tap = new Proxy({}, {
    get: (_target, method: string) => (_client: LSPClient, params: unknown) => {
      listen(method, params);
      return false;
    },
  });
  const client = new LSPClient({ extensions: editorFeatures, notificationHandlers: tap, timeout: 10000 });
  return client.connect(workerTransport(worker));
}
