package mvp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"funroute/extensions/paymentdemo"
	"funroute/lang"
	webui "funroute/web"
)

type exampleManifest struct {
	Version  int          `json:"version"`
	Examples []mvpExample `json:"examples"`
}

type mvpExample struct {
	Label           string         `json:"label"`
	Source          string         `json:"source"`
	Contract        *contractJSON  `json:"contract"`
	Args            map[string]any `json:"args"`
	Expected        any            `json:"expected"`
	MinControlDepth int            `json:"min_control_depth"`
	Covers          []string       `json:"covers"`
	Operators       []string       `json:"operators"`
}

func TestMVPExamplesRunAndCoverTheCatalog(t *testing.T) {
	manifest := readExampleManifest(t)
	registry, err := paymentdemo.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(registry)
	if err != nil {
		t.Fatal(err)
	}
	catalog := lang.Catalog(registry)
	missingCapabilities := catalogCapabilities(catalog)
	missingOperators := catalogOperators(catalog)
	missingNodes := catalogNodes(catalog)
	for _, example := range manifest.Examples {
		runExample(t, server, example)
		coverListed(example.Covers, missingCapabilities)
		coverListed(example.Operators, missingOperators)
		coverExampleNodes(t, example, missingNodes)
	}
	assertCovered(t, "catalog capabilities", missingCapabilities)
	assertCovered(t, "source operators", missingOperators)
	assertCovered(t, "ExprJSON nodes", missingNodes)
}

func readExampleManifest(t *testing.T) exampleManifest {
	t.Helper()
	encoded, err := webui.Files.ReadFile("funroute-examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest exampleManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || len(manifest.Examples) == 0 {
		t.Fatalf("invalid example manifest: version %d, examples %d", manifest.Version, len(manifest.Examples))
	}
	return manifest
}

func runExample(t *testing.T, server *Server, example mvpExample) {
	t.Helper()
	payload, err := json.Marshal(expressionRequest{
		Source: example.Source, Contract: example.Contract, Args: example.Args,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, server, "/api/run", string(payload))
	if response.Code != 200 {
		t.Fatalf("example %q: status = %d, body = %s", example.Label, response.Code, response.Body.String())
	}
	var result struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Value, example.Expected) {
		t.Fatalf("example %q = %#v, want %#v", example.Label, result.Value, example.Expected)
	}
}

func catalogCapabilities(catalog lang.LanguageCatalog) map[string]bool {
	missing := map[string]bool{}
	for _, function := range catalog.Functions {
		missing[function.Name] = true
	}
	for _, form := range catalog.SpecialForms {
		missing[form.Name] = true
	}
	return missing
}

func catalogOperators(catalog lang.LanguageCatalog) map[string]bool {
	missing := map[string]bool{}
	for _, operator := range catalog.Source.Operators {
		missing[operator.Fixity+":"+operator.Token] = true
	}
	return missing
}

func catalogNodes(catalog lang.LanguageCatalog) map[string]bool {
	missing := map[string]bool{}
	for _, node := range catalog.Nodes {
		missing[node.Node] = true
	}
	return missing
}

func coverListed(listed []string, missing map[string]bool) {
	for _, name := range listed {
		delete(missing, name)
	}
}

func coverExampleNodes(t *testing.T, example mvpExample, missing map[string]bool) {
	t.Helper()
	encoded, err := lang.ParseToJSON(example.Source)
	if err != nil {
		t.Fatalf("example %q: parse for node coverage: %v", example.Label, err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	deleteCoveredNodes(document, missing)
	if depth := controlDepth(document); depth < example.MinControlDepth {
		t.Fatalf("example %q control depth = %d, want at least %d", example.Label, depth, example.MinControlDepth)
	}
}

func controlDepth(value any) int {
	switch value := value.(type) {
	case map[string]any:
		deepest := 0
		for _, child := range value {
			deepest = max(deepest, controlDepth(child))
		}
		if isControlNode(value) {
			return deepest + 1
		}
		return deepest
	case []any:
		deepest := 0
		for _, child := range value {
			deepest = max(deepest, controlDepth(child))
		}
		return deepest
	default:
		return 0
	}
}

func isControlNode(value map[string]any) bool {
	node, _ := value["node"].(string)
	switch node {
	case "switch", "let", "for", "reduce":
		return true
	case "call":
		name, _ := value["name"].(string)
		return name == "if" || name == "fallback"
	default:
		return false
	}
}

func deleteCoveredNodes(value any, missing map[string]bool) {
	switch value := value.(type) {
	case map[string]any:
		if node, ok := value["node"].(string); ok {
			delete(missing, node)
		}
		for _, child := range value {
			deleteCoveredNodes(child, missing)
		}
	case []any:
		for _, child := range value {
			deleteCoveredNodes(child, missing)
		}
	}
}

func assertCovered(t *testing.T, kind string, missing map[string]bool) {
	t.Helper()
	if len(missing) != 0 {
		t.Fatalf("examples do not cover %s: %s", kind, fmt.Sprint(missing))
	}
}
