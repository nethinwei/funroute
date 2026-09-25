// Package conformance runs every example of the workbench — the one list of
// them, web/funroute-examples.json, each a source, a contract, arguments and
// the value they give — through the public package the way a host would, and
// holds the whole pipeline to one answer: the source, its ExprJSON, its
// formatted text and the contract the artifact carries compile to one
// digest, and the artifact written out and read back runs to the value the
// example promises.
package conformance
