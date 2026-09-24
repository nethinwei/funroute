package machine_test

import (
	"slices"
	"strings"
	"testing"

	"funroute/extensions/std"
	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// officialRegistry is every official function and form: the kernel, money
// over ISO 4217 rounding half up (the CLI's and the example host's default),
// all three switchable forms and the standard pack. The pack's examples are
// checked here too, because only a test under lang can ask the compiler which
// overload a call chose.
func officialRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	if err := registry.DeclareMoney(machine.MoneySpec{Rounding: machine.RoundHalfUp, Currencies: std.ISO4217()}); err != nil {
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
// returns the signatures its calls chose. A step inside round(…) runs its
// rounding variant, so that is the one it chose.
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
	value, err := runtime.RunValues(t.Context(), nil, machine.RunOptions{})
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
	var scopes []compile.NodeFact
	for _, fact := range analysis.Nodes {
		if function, ok := registry.Resolve(fact.Signature); ok && function.IsRoundingScope() {
			scopes = append(scopes, fact)
		}
	}
	var chosen []string
	for _, fact := range analysis.Nodes {
		if fact.Signature != "" {
			chosen = append(chosen, runsAs(registry, fact, scopes))
		}
	}
	return chosen
}

// runsAs is the signature a call runs: its rounding variant when a round(…)
// holds it and it has one, the one inference chose otherwise.
func runsAs(registry *machine.Registry, fact compile.NodeFact, scopes []compile.NodeFact) string {
	function, _ := registry.Resolve(fact.Signature)
	inside := slices.ContainsFunc(scopes, func(scope compile.NodeFact) bool {
		return scope.Span != fact.Span && scope.Span.Start <= fact.Span.Start && fact.Span.End <= scope.Span.End
	})
	if variant, ok := machine.RoundingVariant(registry, function); inside && ok {
		return variant.Signature()
	}
	return function.Signature()
}
