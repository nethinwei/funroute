package machine_test

import (
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
		{`model.fraud_v1(model.embed_v1(features))`, 1},                                // embed is hoistable; fraud's argument is a call
		{`if(flag, model.fraud_v1(model.embed_v1(features)), 0.0)`, 0},                 // inside a branch
		{`let(e = model.embed_v1(features), if(flag, model.fraud_v1(e), 0.0))`, 1},     // the binding is unconditional
		{`[model.fraud_v1(model.embed_v1(features)) for x in features]`, 0},            // inside a loop
		{`switch(case flag => model.fraud_v1(model.embed_v1(features)), else 1.0)`, 0}, // a case body
		{`fallback(model.fraud_v1(model.embed_v1(features)), 0.0)`, 0},                 // an error boundary
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
