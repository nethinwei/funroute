package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"funroute/examples/payment"
	"funroute/lang/internal/compile"
	"funroute/lang/internal/syntax"
)

// The workbench's examples are programs the language server runs, so this is
// where they are held to account: each one runs through a session to the
// value it promises, and together they use everything the language has —
// every function and form of the example registry, every operator and every
// kind of node.

type example struct {
	Label           string                `json:"label"`
	Source          string                `json:"source"`
	Contract        *compile.TextContract `json:"contract"`
	Args            map[string]any        `json:"args"`
	Expected        any                   `json:"expected"`
	MinControlDepth int                   `json:"min_control_depth"`
	Covers          []string              `json:"covers"`
	Operators       []string              `json:"operators"`
}

func readExamples(t *testing.T) []example {
	t.Helper()
	encoded, err := os.ReadFile("../../web/funroute-examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version  int       `json:"version"`
		Examples []example `json:"examples"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || len(manifest.Examples) == 0 {
		t.Fatalf("example manifest: version %d, %d examples", manifest.Version, len(manifest.Examples))
	}
	return manifest.Examples
}

func TestExamplesRunAndCoverTheLanguage(t *testing.T) {
	registry, err := payment.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string]map[string]bool{"functions and forms": {}, "operators": {}, "nodes": {}}
	catalog := registry.Catalog()
	for _, function := range catalog.Functions {
		missing["functions and forms"][function.Name] = true
	}
	for _, form := range catalog.SpecialForms {
		missing["functions and forms"][form.Name] = true
	}
	for _, operator := range syntax.Operators() {
		missing["operators"][operator] = true
	}
	for _, kind := range syntax.NodeKinds() {
		missing["nodes"][kind] = true
	}
	for _, item := range readExamples(t) {
		runExample(t, newSession(t, registry, `{}`), item)
		for _, name := range item.Covers {
			delete(missing["functions and forms"], name)
		}
		for _, operator := range item.Operators {
			delete(missing["operators"], operator)
		}
		coverTree(t, item, missing["nodes"])
	}
	for kind, left := range missing {
		if len(left) != 0 {
			t.Errorf("the examples use no %s among %v", kind, keys(left))
		}
	}
}

func runExample(t *testing.T, s *session, item example) {
	t.Helper()
	s.notify("funroute/setContract", map[string]any{"contract": item.Contract})
	s.open("file:///example.fr", item.Source)
	if got := s.diagnostics("file:///example.fr"); len(got) != 0 {
		t.Fatalf("example %q has diagnostics: %v", item.Label, got)
	}
	result := s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///example.fr", "args": item.Args},
	}}).(map[string]any)
	if !reflect.DeepEqual(result["value"], item.Expected) {
		t.Fatalf("example %q = %#v (%v), want %#v", item.Label, result["value"], result["error"], item.Expected)
	}
}

// coverTree marks the kinds of node the example's tree has, and checks it
// nests its blocks as deep as it says it does.
func coverTree(t *testing.T, item example, missing map[string]bool) {
	t.Helper()
	tree, err := syntax.SyntaxTree(item.Source)
	if err != nil {
		t.Fatalf("example %q: %v", item.Label, err)
	}
	if depth := blockDepth(*tree, missing); depth < item.MinControlDepth {
		t.Fatalf("example %q nests %d blocks deep, want at least %d", item.Label, depth, item.MinControlDepth)
	}
}

// blockDepth is how deep the forms and lazy calls nest in tree.
func blockDepth(tree syntax.Tree, missing map[string]bool) int {
	delete(missing, tree.Node)
	deepest := 0
	var visit func(fields []syntax.TreeField)
	visit = func(fields []syntax.TreeField) {
		for _, field := range fields {
			for _, node := range field.Nodes {
				deepest = max(deepest, blockDepth(node, missing))
			}
			for _, item := range field.Items {
				visit(item)
			}
		}
	}
	visit(tree.Fields)
	if isBlock(tree) {
		return deepest + 1
	}
	return deepest
}

// isBlock counts an if an operator expands into too: min_control_depth
// counts the branches a program takes, however they are spelled.
func isBlock(tree syntax.Tree) bool {
	switch tree.Node {
	case "switch", "let", "for", "reduce":
		return true
	case "call":
		return tree.Fields[0].Text == "if" || tree.Fields[0].Text == "fallback"
	}
	return false
}

func keys(set map[string]bool) string { return fmt.Sprint(reflect.ValueOf(set).MapKeys()) }
