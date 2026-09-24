// The shapes the language server answers with that the page reads. They are
// the server's (the lsp package); nothing here decides what they mean.

export type Position = { line: number; character: number };
export type Range = { start: Position; end: Position };

export type TreeField = {
  name: string;
  nodes?: Tree[];
  items?: TreeField[][];
  text?: string;
  textRange?: Range;
  flag?: boolean;
};

export type Tree = { node: string; range: Range; operator?: string; form?: boolean; fields?: TreeField[] };

export type Diagnostic = { range: Range; severity: number; message: string };

export type RunResult = {
  value?: unknown;
  error?: { kind: string; message: string };
  unavailable: string[];
};

// Doc is what the host wrote about a function or a form.
export type Doc = { label: string; description?: string; category: string };

type Descriptor = { name: string; signature: string; special?: string; wrap?: string; doc: Doc };

// money is the registry's declared money feature, absent when it has none.
export type MoneySpec = { currencies: { code: string; digits: number }[] };
export type Catalog = { artifact_version: number; functions: Descriptor[]; special_forms: Descriptor[]; money?: MoneySpec };

// A contract as the server reads it (compile.TextContract): every type is text.
export type ArgSpec = { name: string; type: string; doc?: string };
// funroute/arguments answers with an Argument for each: an ArgSpec with a
// sample of the value's JSON, "" for a type JSON cannot give.
export type Argument = ArgSpec & { example: string };
export type ResultSpec = { type: string; doc?: string };
export type TextContract = { types?: Record<string, string>; args?: ArgSpec[]; result?: ResultSpec };
