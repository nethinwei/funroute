package machine_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/nethinwei/funroute/extensions/std"
	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

// officialRegistry is every official function and form: the kernel, money
// over ISO 4217 (the CLI's and the example host's), all three switchable
// forms and the standard pack. The pack's examples are checked here too,
// because only a test inside this module can ask the compiler which overload
// a call chose.
func officialRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := registry.DeclareMoney(money.MoneySpec{Currencies: std.ISO4217()}); err != nil {
		t.Fatal(err)
	}
	if err := std.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

// Every official function shows its uses: each example runs as written to
// the value it states, and between them a name's examples choose every one of
// its overloads — so an overload nobody has shown fails here instead of
// surprising the first rule that reaches it. A name the kernel and the pack
// share (mod, round) has each one's examples on its own overloads, and the
// two together must choose them all.
func TestEveryOfficialFunctionShowsEveryOverload(t *testing.T) {
	t.Parallel()
	registry := officialRegistry(t)
	overloads := map[string][]machine.FunctionDescriptor{}
	for _, function := range registry.Catalog().Functions() {
		overloads[function.Name()] = append(overloads[function.Name()], function)
	}
	for name, functions := range overloads {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkShown(t, registry, functions)
		})
	}
}

func checkShown(t *testing.T, registry *machine.Registry, functions []machine.FunctionDescriptor) {
	t.Helper()
	var examples []machine.Example
	for _, function := range functions {
		if len(function.Doc().Examples) == 0 {
			t.Errorf("%s has no examples", function.Signature())
		}
		for _, example := range function.Doc().Examples {
			if !slices.Contains(examples, example) {
				examples = append(examples, example)
			}
		}
	}
	chosen := map[string]bool{}
	for _, example := range examples {
		for _, signature := range runExample(t, registry, example) {
			chosen[signature] = true
		}
	}
	for _, function := range functions {
		// The catalog writes a variadic signature with its ...; the key a
		// call resolves to does not.
		if !chosen[strings.Replace(function.Signature(), ",...)", ")", 1)] {
			t.Errorf("no example of %s chooses %s", function.Name(), function.Signature())
		}
	}
}

// Every form the catalog lists shows its uses too, run the same way.
func TestEveryFormShowsItsUses(t *testing.T) {
	t.Parallel()
	registry := officialRegistry(t)
	for _, form := range registry.Catalog().SpecialForms() {
		t.Run(form.Name(), func(t *testing.T) {
			t.Parallel()
			if len(form.Doc().Examples) == 0 {
				t.Fatalf("form %s has no examples", form.Name())
			}
			for _, example := range form.Doc().Examples {
				runExample(t, registry, example)
			}
		})
	}
}

// runExample compiles and runs one example, checks the value it gives, and
// returns the signatures its calls chose.
func runExample(t *testing.T, registry *machine.Registry, example machine.Example) []string {
	t.Helper()
	artifact, err := compile.CompileExpr(example.Source, registry, compile.CompileOptions{})
	if err != nil {
		t.Errorf("example %q: CompileExpr error = %v", example.Source, err)
		return nil
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatalf("example %q: Instantiate error = %v", example.Source, err)
	}
	value, err := runtime.RunValues(t.Context(), nil)
	if err != nil {
		t.Errorf("example %q: run error = %v", example.Source, err)
		return nil
	}
	if encoded, err := registry.EncodeJSON(value); err != nil || string(encoded) != example.Result {
		t.Errorf("example %q = %s, %v, want %s", example.Source, encoded, err, example.Result)
	}
	return chosenBy(t, registry, example.Source)
}

func chosenBy(t *testing.T, registry *machine.Registry, source string) []string {
	t.Helper()
	analysis, err := compile.Analyze(source, registry, compile.CompileOptions{})
	if err != nil {
		t.Fatalf("Analyze(%q) error = %v", source, err)
	}
	var chosen []string
	for _, fact := range analysis.Nodes {
		if function, ok := registry.Resolve(fact.Signature); ok {
			chosen = append(chosen, function.Signature())
		}
	}
	return chosen
}
