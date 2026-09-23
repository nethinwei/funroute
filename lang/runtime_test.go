package lang_test

import (
	"errors"
	"testing"

	"funroute/lang"
)

func mustInstantiate(t *testing.T, artifact *lang.Artifact, registry *lang.Registry) *lang.Runtime {
	t.Helper()
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// A host stores artifacts and loads them later; the loader is what checks they
// were not edited in between. This also exercises HandleOf, the way a contract
// declares an engine value.
func TestHostChecksArtifactIdentity(t *testing.T) {
	t.Parallel()
	registry := lang.CoreRegistry()
	type tensor struct{ Values []float64 }
	if err := lang.DefineHandle[*tensor](registry, "demo.tensor"); err != nil {
		t.Fatal(err)
	}
	if err := lang.Logic(registry, "demo.size_v1", lang.Doc{Label: "尺寸", Cost: 2},
		func(value *tensor) (int64, error) { return int64(len(value.Values)), nil }); err != nil {
		t.Fatal(err)
	}
	result := lang.IntType
	artifact, err := lang.CompileExpr("demo.size_v1(features)", registry, lang.CompileOptions{
		Args:   []lang.ArgSpec{{Name: "features", Type: lang.HandleOf("demo.tensor")}},
		Result: &result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lang.Instantiate(artifact, registry); err != nil {
		t.Fatal(err)
	}
	// The digest is the identity: an artifact edited after compilation no
	// longer loads, which is what a host relies on when it stores them.
	artifact.Args[0].Name = "other"
	if _, err := lang.Instantiate(artifact, registry); err == nil {
		t.Fatal("Instantiate(artifact with an argument renamed) error = nil, want a digest mismatch")
	}
}

// The pieces a host reaches for around a compile, used the way a console
// would: a contract written as data, arguments decoded from what was typed,
// the artifact's own description of what it takes and calls, and the budget.
func TestAHostDrivesARuleFromDataToResult(t *testing.T) {
	t.Parallel()
	contract := &lang.TextContract{
		Types:  map[string]string{"Order": "record{amount: int}"},
		Args:   []lang.TextArg{{Name: "order", Type: "Order", Doc: "订单"}},
		Result: &lang.TextResult{Type: "int", Doc: "手续费"},
	}
	options, err := contract.Options()
	if err != nil {
		t.Fatal(err)
	}
	registry := lang.CoreRegistry()
	artifact, err := lang.CompileExpr(`order.amount * 25 / 10000`, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	params := artifact.Args
	calls := artifact.Calls
	if len(params) != 1 || params[0].Name != "order" || len(calls) == 0 {
		t.Fatalf("the artifact takes %v and calls %v, want the one argument order and some calls", params, calls)
	}
	args, err := lang.DecodeArgs([]byte(`{"order": {"amount": 9007199254740993}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lang.DecodeArgs([]byte(`{} {}`)); !errors.Is(err, lang.ErrContract) {
		t.Fatalf("DecodeArgs(`{} {}`) error = %v, want ErrContract: trailing data must be refused", err)
	}
	runtime, err := lang.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), args, lang.RunOptions{Fuel: lang.DefaultFuel}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), args, lang.RunOptions{Fuel: 1}); !errors.Is(err, lang.ErrFuel) {
		t.Fatalf("Run with Fuel 1 error = %v, want ErrFuel", err)
	}
}
