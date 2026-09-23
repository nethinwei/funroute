package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"funroute/examples/payment"
	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
	"funroute/lang/internal/syntax"
)

func TestSyntaxTreeIsInClientPositions(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "let(rate = 2,\n  rate + x)")
	tree := s.request("funroute/syntaxTree", docParams("file:///a.fr")).(map[string]any)
	body := tree["fields"].([]any)[1].(map[string]any)["nodes"].([]any)[0].(map[string]any)
	if body["operator"] != "+" || fmt.Sprint(body["range"]) != "map[end:map[character:10 line:1] start:map[character:2 line:1]]" {
		t.Errorf("the body is %v, want the + from 1:2 to 1:10", body)
	}
	s.open("file:///b.fr", "let(x = 1)")
	if got := s.request("funroute/syntaxTree", docParams("file:///b.fr")); got != nil {
		t.Errorf("the syntax tree of %q = %v, want nil: it does not parse", "let(x = 1)", got)
	}
}

// A run in a runtime that has a host function only as its signature says so.
func TestRunReportsWhatCouldNotRun(t *testing.T) {
	t.Parallel()
	host := standard(t)
	doc := machine.Doc{Label: "风险分", Cost: 25, Params: []string{"国家"}}
	if err := machine.Logic(host, "risk.score_v1", doc, func(string) (float64, error) { return 0.9, nil }); err != nil {
		t.Fatal(err)
	}
	signatures := standard(t)
	if err := host.Manifest().Apply(signatures); err != nil {
		t.Fatal(err)
	}
	s := newSession(t, signatures, `{}`)
	s.notify("funroute/setContract", contract("country:string"))
	s.open("file:///a.fr", "fallback(risk.score_v1(country), 0.5)")
	result := s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///a.fr", "args": map[string]string{"country": "SG"}},
	}}).(map[string]any)
	if result["value"] != 0.5 || fmt.Sprint(result["unavailable"]) != "[risk.score_v1]" {
		t.Errorf("the run is %v, want value 0.5 with risk.score_v1 unavailable", result)
	}
	s = newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("n:int"))
	s.open("file:///b.fr", "n + 1")
	large := s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///b.fr", "args": `{"n": 9007199254740993}`},
	}}).(map[string]any)
	if large["error"] != nil || fmt.Sprintf("%.0f", large["value"]) != "9007199254740994" {
		t.Errorf("args given as text lost digits: %v, want value 9007199254740994", large)
	}
}

func TestRenderAndCatalog(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int"))
	s.open("file:///a.fr", "fee * 2")
	rendered := s.request("workspace/executeCommand", map[string]any{"command": renderCommand, "arguments": []map[string]any{{"uri": "file:///a.fr"}}}).(map[string]any)
	if got := rendered["source"].(string); !strings.HasPrefix(got, "// fee: int") || !strings.HasSuffix(got, "fee * 2") {
		t.Errorf("the rendered rule is %q, want the comment \"// fee: int\" before \"fee * 2\"", got)
	}
	s.notify("funroute/setContract", contract("fee:nope"))
	refused := s.request("workspace/executeCommand", map[string]any{"command": renderCommand, "arguments": []map[string]any{{"uri": "file:///a.fr"}}}).(map[string]any)
	if !strings.Contains(fmt.Sprint(refused["message"]), "nope") {
		t.Errorf("a refused contract is rendered as %v, want a message naming \"nope\"", refused)
	}
	catalog := s.request("funroute/catalog", map[string]any{}).(map[string]any)
	if len(catalog["functions"].([]any)) == 0 || len(catalog["special_forms"].([]any)) == 0 {
		t.Errorf("the catalog is %v, want functions and special forms", catalog)
	}
}

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
	t.Parallel()
	registry, err := payment.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	missing := everything(registry)
	// The examples run in parallel, each in its own session; the kinds of node
	// they use are marked from all of them at once, so under a lock. What is
	// left is judged once every example has run.
	var nodes sync.Mutex
	t.Cleanup(func() {
		for kind, left := range missing {
			if len(left) != 0 {
				t.Errorf("the examples use no %s among %v", kind, keys(left))
			}
		}
	})
	for _, item := range readExamples(t) {
		for _, name := range item.Covers {
			delete(missing["functions and forms"], name)
		}
		for _, operator := range item.Operators {
			delete(missing["operators"], operator)
		}
		t.Run(item.Label, func(t *testing.T) {
			t.Parallel()
			coverNodes(t, item, &nodes, missing["nodes"])
			runExample(t, newSession(t, registry, `{}`), item)
		})
	}
}

// everything is what the examples must use between them: every function and
// form of registry, every operator and every kind of node.
func everything(registry *machine.Registry) map[string]map[string]bool {
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
	return missing
}

// coverNodes is coverTree for an example running alongside the others.
func coverNodes(t *testing.T, item example, lock *sync.Mutex, missing map[string]bool) {
	t.Helper()
	lock.Lock()
	defer lock.Unlock()
	coverTree(t, item, missing)
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
