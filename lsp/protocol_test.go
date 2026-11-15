package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/demo"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// The workbench reads the server's answers through types of its own
// (web/src/protocol.ts), written by hand and holding only the fields it
// reads. These tests hold the two together where they meet: every field the
// workbench declares is one the server writes, every method and command it
// sends is one the server answers, and every kind of token the server marks
// has a look. A field renamed on one side fails here, not as an undefined on
// the page.

// TestTheWorkbenchsShapesAreTheServers holds each type of protocol.ts to the
// Go type the server writes it from: every field the workbench declares is a
// key the server writes. A type protocol.ts adds must be placed here too.
func TestTheWorkbenchsShapesAreTheServers(t *testing.T) {
	t.Parallel()
	catalog := catalogKeys(t)
	written := map[string]map[string]bool{
		"Position": structKeys(reflect.TypeFor[Position]()), "Range": structKeys(reflect.TypeFor[Range]()),
		"TreeField": structKeys(reflect.TypeFor[rangedField]()), "Tree": structKeys(reflect.TypeFor[rangedTree]()),
		"Diagnostic": structKeys(reflect.TypeFor[diagnostic]()), "RunResult": structKeys(reflect.TypeFor[runResult]()),
		"Doc": structKeys(reflect.TypeFor[machine.Doc]()), "MoneySpec": structKeys(reflect.TypeFor[money.MoneySpec]()),
		"FunctionDescriptor": catalog["functions"], "FormDescriptor": catalog["special_forms"], "Catalog": catalog["catalog"],
		"ArgSpec": structKeys(reflect.TypeFor[compile.TextArg]()), "ResultSpec": structKeys(reflect.TypeFor[compile.TextResult]()),
		"TextContract": structKeys(reflect.TypeFor[compile.TextContract]()), "Argument": argumentKeys(t),
	}
	declared := tsTypes(t, readWeb(t, "src/protocol.ts"))
	for name, fields := range declared {
		keys, placed := written[name]
		if !placed {
			t.Errorf("protocol.ts declares %s, which no Go type here is placed against", name)
			continue
		}
		for _, field := range fields {
			if !keys[field] {
				t.Errorf("protocol.ts's %s reads %q, which the server does not write", name, field)
			}
		}
	}
}

// TestTheWorkbenchCallsWhatTheServerAnswers holds every method and command
// the workbench sends to the ones the server handles.
func TestTheWorkbenchCallsWhatTheServerAnswers(t *testing.T) {
	t.Parallel()
	sources := readWebSources(t)
	sent := regexp.MustCompile(`"((?:funroute|textDocument|workspace)/[A-Za-z/]+)"`)
	for _, match := range sent.FindAllStringSubmatch(sources, -1) {
		method := match[1]
		_, request := requests[method]
		_, notice := notifications[method]
		if !request && !notice && method != "textDocument/publishDiagnostics" { // the one the server sends
			t.Errorf("the workbench sends %s, which the server does not handle", method)
		}
	}
	for _, match := range regexp.MustCompile(`command<[^>]*>\("([^"]+)"`).FindAllStringSubmatch(sources, -1) {
		if _, ok := commands[match[1]]; !ok {
			t.Errorf("the workbench runs the command %s, which the server does not have", match[1])
		}
	}
}

// TestEveryTokenTypeHasALook holds the token types the server marks to the
// styles the editor gives them: a new type would otherwise show unstyled.
func TestEveryTokenTypeHasALook(t *testing.T) {
	t.Parallel()
	editor := readWeb(t, "src/editor.ts")
	for _, name := range append(slices.Clone(tokenTypes), tokenLegend["tokenModifiers"]...) {
		if !strings.Contains(editor, ".fr-tok-"+name) {
			t.Errorf("the editor has no .fr-tok-%s style for the server's %s tokens", name, name)
		}
	}
}

// readWeb is a file of the workbench's.
func readWeb(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "web", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// readWebSources is every TypeScript source of the workbench but its tests.
func readWebSources(t *testing.T) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "web", "src", "*.ts"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, path := range paths {
		if strings.HasSuffix(path, ".test.ts") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	return all.String()
}

// tsTypes is every object type protocol.ts declares, by name, with the
// fields at its top level; an intersection, A & { … }, has A's as well.
func tsTypes(t *testing.T, source string) map[string][]string {
	t.Helper()
	types, bases := map[string][]string{}, map[string]string{}
	for _, match := range regexp.MustCompile(`(?m)^(?:export )?type (\w+) = ([^{;]*)\{`).FindAllStringSubmatchIndex(source, -1) {
		name := source[match[2]:match[3]]
		types[name] = topLevelFields(braced(source[match[1]-1:]))
		if base := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(source[match[4]:match[5]]), "&")); base != "" {
			bases[name] = base
		}
	}
	for name, base := range bases {
		types[name] = append(types[name], types[base]...)
	}
	return types
}

// braced is the text between the brace s starts with and the one that closes
// it.
func braced(s string) string {
	depth := 0
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return s[1:i]
			}
		}
	}
	return s
}

// topLevelFields is the field names of an object type's body, not those of
// the objects nested in it.
func topLevelFields(body string) []string {
	var top strings.Builder
	depth := 0
	for _, r := range body {
		switch r {
		case '{', '[', '(', '<':
			depth++
		case '}', ']', ')', '>':
			depth--
		default:
			if depth == 0 {
				top.WriteRune(r)
			}
		}
	}
	matches := regexp.MustCompile(`(\w+)\??:`).FindAllStringSubmatch(top.String(), -1)
	fields := make([]string, 0, len(matches))
	for _, match := range matches {
		fields = append(fields, match[1])
	}
	return fields
}

// structKeys is the JSON keys a Go struct writes, its embedded structs' too.
func structKeys(typ reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for field := range typ.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		switch {
		case name == "-":
		case name == "" && field.Anonymous:
			for key := range structKeys(field.Type) {
				keys[key] = true
			}
		case name == "":
			keys[field.Name] = true
		default:
			keys[name] = true
		}
	}
	return keys
}

// catalogKeys is the keys the demo console's catalog writes: of the catalog,
// and of every function and form in it, each key any of them has.
func catalogKeys(t *testing.T) map[string]map[string]bool {
	t.Helper()
	registry, err := demo.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(registry.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &catalog); err != nil {
		t.Fatal(err)
	}
	keys := map[string]map[string]bool{"catalog": {}, "functions": {}, "special_forms": {}}
	for key, list := range catalog {
		keys["catalog"][key] = true
		if entries, ok := keys[key]; ok {
			addItemKeys(entries, list)
		}
	}
	return keys
}

// addItemKeys adds to keys every key an item of the list has.
func addItemKeys(keys map[string]bool, list json.RawMessage) {
	var items []map[string]json.RawMessage
	if json.Unmarshal(list, &items) != nil {
		return
	}
	for _, item := range items {
		for field := range item {
			keys[field] = true
		}
	}
}

// argumentKeys is the keys funroute/arguments writes for an argument.
func argumentKeys(t *testing.T) map[string]bool {
	t.Helper()
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("x:int"))
	s.open("file:///a.fr", "x")
	keys := map[string]bool{}
	for _, argument := range as[[]any](t, s.request("funroute/arguments", map[string]any{"textDocument": map[string]any{"uri": "file:///a.fr"}})) {
		for key := range as[map[string]any](t, argument) {
			keys[key] = true
		}
	}
	return keys
}
