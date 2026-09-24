package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/demo"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/syntax"
)

func TestSyntaxTreeIsInClientPositions(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "let(rate = 2,\n  rate + x)")
	tree := as[map[string]any](t, s.request("funroute/syntaxTree", docParams("file:///a.fr")))
	field := as[map[string]any](t, as[[]any](t, tree["fields"])[1])
	body := as[map[string]any](t, as[[]any](t, field["nodes"])[0])
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
	if err := host.Register(machine.FunctionSpec{
		Name: "risk.score_v1",
		Doc:  doc,
		Go:   func(string) (float64, error) { return 0.9, nil },
	}); err != nil {
		t.Fatal(err)
	}
	signatures := standard(t)
	if err := host.Manifest().Apply(signatures); err != nil {
		t.Fatal(err)
	}
	s := newSession(t, signatures, `{}`)
	s.notify("funroute/setContract", contract("country:string"))
	s.open("file:///a.fr", "fallback(risk.score_v1(country), 0.5)")
	result := as[map[string]any](t, s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///a.fr", "args": map[string]string{"country": "SG"}},
	}}))
	if result["value"] != 0.5 || fmt.Sprint(result["unavailable"]) != "[risk.score_v1]" {
		t.Errorf("the run is %v, want value 0.5 with risk.score_v1 unavailable", result)
	}
	s = newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("n:int"))
	s.open("file:///b.fr", "n + 1")
	large := as[map[string]any](t, s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///b.fr", "args": `{"n": 9007199254740993}`},
	}}))
	if large["error"] != nil || fmt.Sprintf("%.0f", large["value"]) != "9007199254740994" {
		t.Errorf("args given as text lost digits: %v, want value 9007199254740994", large)
	}
}

func TestRenderAndCatalog(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", contract("fee:int"))
	s.open("file:///a.fr", "fee * 2")
	rendered := as[map[string]any](t, s.request("workspace/executeCommand", map[string]any{"command": renderCommand, "arguments": []map[string]any{{"uri": "file:///a.fr"}}}))
	if got := as[string](t, rendered["source"]); !strings.HasPrefix(got, "// fee: int") || !strings.HasSuffix(got, "fee * 2") {
		t.Errorf("the rendered rule is %q, want the comment \"// fee: int\" before \"fee * 2\"", got)
	}
	s.notify("funroute/setContract", contract("fee:nope"))
	refused := as[map[string]any](t, s.request("workspace/executeCommand", map[string]any{"command": renderCommand, "arguments": []map[string]any{{"uri": "file:///a.fr"}}}))
	if !strings.Contains(fmt.Sprint(refused["message"]), "nope") {
		t.Errorf("a refused contract is rendered as %v, want a message naming \"nope\"", refused)
	}
	catalog := as[map[string]any](t, s.request("funroute/catalog", map[string]any{}))
	if len(as[[]any](t, catalog["functions"])) == 0 || len(as[[]any](t, catalog["special_forms"])) == 0 {
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
	encoded, err := os.ReadFile("../web/funroute-examples.json")
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
	registry, err := demo.NewRegistry()
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
	for _, function := range catalog.Functions() {
		missing["functions and forms"][function.Name()] = true
	}
	for _, form := range catalog.SpecialForms() {
		missing["functions and forms"][form.Name()] = true
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
	result := as[map[string]any](t, s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///example.fr", "args": item.Args},
	}}))
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
	case "switch", "let", "for", "reduce", "using":
		return true
	case "call":
		return tree.Fields[0].Text == "if" || tree.Fields[0].Text == "fallback"
	}
	return false
}

func keys(set map[string]bool) string { return fmt.Sprint(reflect.ValueOf(set).MapKeys()) }

// run executes the program of file:///a.fr with args and returns the result.
func (s *session) run(args any) map[string]any {
	s.t.Helper()
	return as[map[string]any](s.t, s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///a.fr", "args": args},
	}}))
}

// A run writes money as the registry's text, "USD 1.70", a ratio as its
// decimal and an exchange rate as its object; the result's type says which.
func TestRunWritesMoneyAsText(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source, args string
		contract     []string
		value, typ   string
	}{
		{"USD 1.70", `{}`, nil, `"USD 1.70"`, `{"kind":"money"}`},
		{"USD -1.70", `{}`, nil, `"USD -1.70"`, `{"kind":"money"}`},
		{"KWD 1.234 + KWD 0.001", `{}`, nil, `"KWD 1.235"`, `{"kind":"money"}`},
		{"2.9%", `{}`, nil, `"0.029"`, `{"kind":"ratio"}`},
		{"25bps", `{}`, nil, `"0.0025"`, `{"kind":"ratio"}`},
		{"KWD", `{}`, nil, `"KWD"`, `{"kind":"currency"}`},
		{"[USD 1, USD -0.5]", `{}`, nil, `["USD 1.00","USD -0.50"]`, `{"elem":{"kind":"money"},"kind":"array"}`},
		{"round(amount * 2.9%, @half_even) + money(30, currency(amount))", `{"amount":"USD 10.00"}`, []string{"amount:money"}, `"USD 0.59"`, `{"kind":"money"}`},
		{"round(amount * 2.9%, @half_even)", `{"amount":{"currency":"JPY","minor":1000}}`, []string{"amount:money"}, `"JPY 29"`, `{"kind":"money"}`},
		{"amount", `{"amount":0}`, []string{"amount:money"}, `0`, `{"kind":"money"}`},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, withKWD(t), `{}`)
			s.notify("funroute/setContract", contract(test.contract...))
			s.open("file:///a.fr", test.source)
			result := s.run(test.args)
			value, _ := json.Marshal(result["value"])
			typ, _ := json.Marshal(result["type"])
			if result["error"] != nil || string(value) != test.value || string(typ) != test.typ {
				t.Errorf("run %q with %s = %v, want value %s of type %s", test.source, test.args, result, test.value, test.typ)
			}
		})
	}
}

// A run that meets two currencies fails as a currency error, whether the
// rule meets them or an argument is not the currency the contract names.
func TestRunReportsCurrencyErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source, args, message string
		contract              []string
	}{
		{"amount + USD 1", `{"amount":"JPY 5"}`, "JPY and USD", []string{"amount:money"}},
		{"a + b", `{"a":"USD 1","b":"JPY 1"}`, "USD and JPY", []string{"a:money", "b:money"}},
		{"amount", `{"amount":{"currency":"EUR","minor":1}}`, `"EUR" is not declared`, []string{"amount:money"}},
		{"cur", `{"cur":"EUR"}`, `"EUR" is not declared`, []string{"cur:currency"}},
	} {
		t.Run(test.message, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, withKWD(t), `{}`)
			s.notify("funroute/setContract", contract(test.contract...))
			s.open("file:///a.fr", test.source)
			failure, _ := s.run(test.args)["error"].(map[string]any)
			if failure["kind"] != "currency" || !strings.Contains(fmt.Sprint(failure["message"]), test.message) {
				t.Errorf("run %q with %s failed with %v, want kind currency and a message containing %q", test.source, test.args, failure, test.message)
			}
		})
	}
}

// A money program that does not compile fails the run as a compile error,
// the way any other program does.
func TestRunReportsMoneyCompileErrors(t *testing.T) {
	t.Parallel()
	s := newSession(t, withKWD(t), `{}`)
	for _, source := range []string{"USD 1 * USD 1", "USD 1.234", "EUR 1"} {
		s.open("file:///a.fr", source)
		failure, _ := s.run(`{}`)["error"].(map[string]any)
		if failure["kind"] != "compile" {
			t.Errorf("run %q failed with %v, want kind compile", source, failure)
		}
	}
}

// The catalog carries the declared money — the currencies sorted by code,
// each with its places, and the rounding by name — and nothing when money is
// not declared.
func TestCatalogCarriesTheMoneyFeature(t *testing.T) {
	t.Parallel()
	s := newSession(t, withKWD(t), `{}`)
	catalog := as[map[string]any](t, s.request("funroute/catalog", map[string]any{}))
	money, _ := json.Marshal(catalog["money"])
	want := `{"currencies":[{"code":"JPY","digits":0},{"code":"KWD","digits":3},{"code":"USD","digits":2}]}`
	if string(money) != want {
		t.Errorf("the catalog's money = %s, want %s", money, want)
	}
	names := map[string]bool{}
	for _, function := range as[[]any](t, catalog["functions"]) {
		names[as[string](t, as[map[string]any](t, function)["name"])] = true
	}
	for _, name := range []string{"money", "minor", "currency", "round", "ratio"} {
		if !names[name] {
			t.Errorf("the catalog of a registry with money lacks %s", name)
		}
	}
	plain := as[map[string]any](t, newSession(t, standard(t), `{}`).request("funroute/catalog", map[string]any{}))
	if _, has := plain["money"]; has {
		t.Errorf("the catalog without money declared has money %v, want none", plain["money"])
	}
}

// Arithmetic with no answer is its own kind: a division by zero, an overflow,
// an exchange rate that is not positive, whether the rule or the arguments
// bring it.
func TestRunReportsArithmeticErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		source, args, message string
		contract              []string
	}{
		{"n / d", `{"n":1,"d":0}`, "division by zero", []string{"n:int", "d:int"}},
		{"n + 1", `{"n":9223372036854775807}`, "overflow", []string{"n:int"}},
		{"fee / zero", `{"fee":"0.029","zero":"0"}`, "division by zero", []string{"fee:ratio", "zero:ratio"}},
	} {
		t.Run(test.message+" "+test.source, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, withKWD(t), `{}`)
			s.notify("funroute/setContract", contract(test.contract...))
			s.open("file:///a.fr", test.source)
			failure, _ := s.run(test.args)["error"].(map[string]any)
			if failure["kind"] != "arithmetic" || !strings.Contains(fmt.Sprint(failure["message"]), test.message) {
				t.Errorf("run %q with %s failed with %v, want kind arithmetic and a message containing %q", test.source, test.args, failure, test.message)
			}
		})
	}
}

// A run's exchange rates are an argument: using(market, amount -> JPY)
// converts at them, no quote of the pair is the kind nofxrate, and a quote
// that is no rate fails the run.
func TestRunConvertsAtTheQuotesItIsGiven(t *testing.T) {
	t.Parallel()
	usdJPY := `[{"base":"USD","quote":"JPY","rate":"150.5"}]`
	for _, test := range []struct {
		name, source string
		market       string
		value, kind  string
	}{
		{"direct", "using(market, round(amount -> JPY, @half_even))", usdJPY, `"JPY 150"`, ""},
		{"inverse", "using(market, round(JPY 301 -> USD, @half_even))", usdJPY, `"USD 2.00"`, ""},
		{"no pair", "using(market, round(amount -> KWD, @half_even))", usdJPY, "", "nofxrate"},
		{"no quotes", "using(market, round(amount -> JPY, @half_even))", `[]`, "", "nofxrate"},
		{"caught", "using(market, fallback(round(amount -> KWD, @half_even), KWD 0))", usdJPY, `"KWD 0.000"`, ""},
		{"a bad quote", "using(market, round(amount -> JPY, @half_even))", `[{"base":"USD","quote":"JPY","rate":"-1"}]`, "", "arithmetic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newSession(t, withKWD(t), `{}`)
			s.notify("funroute/setContract", contract("amount:money", "market:array<fxrate>"))
			s.open("file:///a.fr", test.source)
			result := as[map[string]any](t, s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
				{"uri": "file:///a.fr", "args": `{"amount":"USD 1.00","market":` + test.market + `}`},
			}}))
			checkRun(t, test.source, result, test.value, test.kind)
		})
	}
}

// checkRun requires a run to give value, or to fail as kind.
func checkRun(t *testing.T, source string, result map[string]any, value, kind string) {
	t.Helper()
	failure, _ := result["error"].(map[string]any)
	if kind != "" {
		if failure["kind"] != kind {
			t.Errorf("run %q failed with %v, want kind %s", source, failure, kind)
		}
		return
	}
	got, _ := json.Marshal(result["value"])
	if failure != nil || string(got) != value {
		t.Errorf("run %q = %s (%v), want %s", source, got, failure, value)
	}
}

// A run is bounded twice over: it asks for at most maxRunFuel, and a host
// function still waiting at runTimeout is cut off as a deadline, so one rule
// cannot hold the service every document shares.
func TestARunIsBounded(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.open("file:///a.fr", "1 + 1")
	result := as[map[string]any](t, s.request("workspace/executeCommand", map[string]any{"command": runCommand, "arguments": []map[string]any{
		{"uri": "file:///a.fr", "args": map[string]any{}, "fuel": maxRunFuel + 1},
	}}))
	if failure, _ := result["error"].(map[string]any); failure["kind"] != "fuel" {
		t.Fatalf("a run asking for %d fuel = %v, want a fuel error", maxRunFuel+1, result)
	}
	synctest.Test(t, func(t *testing.T) {
		registry := standard(t)
		wait := func(ctx context.Context, n int64) (int64, error) {
			<-ctx.Done()
			return n, ctx.Err()
		}
		if err := registry.Register(machine.FunctionSpec{Name: "slow.wait_v1", Doc: machine.Doc{Cost: 1}, Go: wait}); err != nil {
			t.Fatal(err)
		}
		s := newSession(t, registry, `{}`)
		s.notify("funroute/setContract", contract("n:int"))
		s.open("file:///a.fr", "slow.wait_v1(n)")
		failure, _ := s.run(map[string]any{"n": 1})["error"].(map[string]any)
		if failure["kind"] != "deadline" {
			t.Fatalf("a host function that never returns = %v, want a deadline", failure)
		}
	})
}

// funroute/arguments gives each argument a sample of its value's JSON, made
// from the type the contract declares, alias and all.
func TestArgumentsCarryASampleOfTheirJSON(t *testing.T) {
	t.Parallel()
	s := newSession(t, standard(t), `{}`)
	s.notify("funroute/setContract", map[string]any{"contract": map[string]any{
		"types": map[string]string{"Row": "record{a: int, b: string}"},
		"args":  []map[string]string{{"name": "rows", "type": "array<Row>"}, {"name": "on", "type": "bool"}},
	}})
	s.open("file:///a.fr", "on")
	list := as[[]any](t, s.request("funroute/arguments", docParams("file:///a.fr")))
	want := []string{`[{"a": 0, "b": "…"}]`, "true"}
	if len(list) != len(want) {
		t.Fatalf("the arguments are %v, want %d", list, len(want))
	}
	for i, item := range list {
		if got := as[map[string]any](t, item)["example"]; got != want[i] {
			t.Errorf("argument %d has the example %v, want %s", i, got, want[i])
		}
	}
}
