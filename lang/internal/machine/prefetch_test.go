package machine_test

import (
	"context"
	"sync/atomic"
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// Only a call whose arguments come straight from the request, outside every
// loop and every condition, is hoisted.
func TestPrefetchSitesAreTheUnconditionalTopLevelCalls(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	features := compile.ArgSpec{Name: "features", Type: machine.ArrayOf(machine.FloatType)}
	flag := compile.ArgSpec{Name: "flag", Type: machine.BoolType}
	for _, test := range []struct {
		source string
		sites  int
	}{
		{`model.fraud_v1(model.embed_v1(features))`, 1},                                   // embed is hoistable; fraud's argument is a call
		{`if(flag, model.fraud_v1(model.embed_v1(features)), 0.0)`, 0},                    // inside a branch
		{`let(e = model.embed_v1(features), if(flag, model.fraud_v1(e), 0.0))`, 1},        // the binding is unconditional
		{`[model.fraud_v1(model.embed_v1(features)) for x in features]`, 0},               // inside a loop
		{`switch(case flag => model.fraud_v1(model.embed_v1(features)), else => 1.0)`, 0}, // a case body
		{`fallback(model.fraud_v1(model.embed_v1(features)), 0.0)`, 0},                    // an error boundary
		{`fallback(model.fraud_v1(model.embed_v1(features)), model.fraud_v1(model.embed_v1(features)), 0.0)`, 0},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compile.CompileExpr(test.source, registry, compile.CompileOptions{Args: []compile.ArgSpec{features, flag}})
			if err != nil {
				t.Fatalf("%s: %v", test.source, err)
			}
			if got := len(machine.PrefetchSites(artifact)); got != test.sites {
				t.Errorf("%s: %d sites, want %d", test.source, got, test.sites)
			}
		})
	}
}

// A model call the compiler put currency checks before is not hoisted: a
// Batch would otherwise hand the engine a dollar and a euro together before
// the program ever reached the check that refuses them.
func TestCurrencyChecksKeepACallInPlace(t *testing.T) {
	t.Parallel()
	registry := moneyRegistry(t)
	amount := machine.MoneyOf("u")
	err := registry.Register(machine.FunctionSpec{
		Name: "fees.model_v1", Params: []machine.Type{amount, amount}, Result: amount, Doc: machine.Doc{Cost: 10},
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { return args[0], nil },
		EvalBatch: func(_ context.Context, calls [][]machine.Value) ([]machine.Value, error) {
			out := make([]machine.Value, len(calls))
			for i, call := range calls {
				out[i] = call[0]
			}
			return out, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	args := []compile.ArgSpec{{Name: "a", Type: machine.MoneyOf("")}, {Name: "b", Type: machine.MoneyOf("")}}
	artifact, err := compile.CompileExpr("fees.model_v1(a, b)", registry, compile.CompileOptions{Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if sites := machine.PrefetchSites(artifact); len(sites) != 0 {
		t.Fatalf("PrefetchSites = %v, want none: the call is behind a currency check", sites)
	}
	// Proven to share a currency, the operands need no check, and the call
	// is hoisted as any other.
	sameCurrency := []compile.ArgSpec{{Name: "a", Type: machine.MoneyOf("c")}, {Name: "b", Type: machine.MoneyOf("c")}}
	proven, err := compile.CompileExpr("fees.model_v1(a, b)", registry, compile.CompileOptions{Args: sameCurrency})
	if err != nil {
		t.Fatal(err)
	}
	if sites := machine.PrefetchSites(proven); len(sites) != 1 {
		t.Fatalf("PrefetchSites = %v for proven operands, want the call", sites)
	}
}
