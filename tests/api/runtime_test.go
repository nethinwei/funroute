package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nethinwei/funroute"
)

func mustInstantiate(t *testing.T, artifact *funroute.Artifact, registry *funroute.Registry) *funroute.Runtime {
	t.Helper()
	runtime, err := funroute.Instantiate(artifact, registry)
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
	registry := funroute.CoreRegistry()
	type tensor struct{ Values []float64 }
	if err := funroute.DefineHandle[*tensor](registry, "demo.tensor"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(funroute.FunctionSpec{
		Name: "demo.size_v1",
		Doc:  funroute.Doc{Label: "尺寸"},
		Go:   func(value *tensor) (int64, error) { return int64(len(value.Values)), nil },
	}); err != nil {
		t.Fatal(err)
	}
	result := funroute.IntType
	artifact, err := funroute.CompileExpr("demo.size_v1(features)", registry, funroute.CompileOptions{
		Args:   []funroute.ArgSpec{{Name: "features", Type: funroute.HandleOf("demo.tensor")}},
		Result: &result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := funroute.Instantiate(artifact, registry); err != nil {
		t.Fatal(err)
	}
	// The digest is the identity: an artifact edited after compilation no
	// longer loads, which is what a host relies on when it stores them.
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var edited funroute.Artifact
	if err := json.Unmarshal(bytes.Replace(encoded, []byte(`"name":"`), []byte(`"name":"other`), 1), &edited); err != nil {
		t.Fatal(err)
	}
	if _, err := funroute.Instantiate(&edited, registry); err == nil {
		t.Fatal("Instantiate(artifact with an argument renamed) error = nil, want a digest mismatch")
	}
}

// The pieces a host reaches for around a compile, used the way a console
// would: a contract written as data, arguments decoded from what was typed,
// the artifact's own description of what it takes and calls, and the budget.
func TestAHostDrivesARuleFromDataToResult(t *testing.T) {
	t.Parallel()
	contract := &funroute.TextContract{
		Types:  map[string]string{"Order": "record{amount: int}"},
		Args:   []funroute.TextArg{{Name: "order", Type: "Order", Doc: "订单"}},
		Result: &funroute.TextResult{Type: "int", Doc: "手续费"},
	}
	options, err := contract.Options()
	if err != nil {
		t.Fatal(err)
	}
	registry := funroute.CoreRegistry()
	artifact, err := funroute.CompileExpr(`order.amount * 25 / 10000`, registry, options)
	if err != nil {
		t.Fatal(err)
	}
	params := artifact.Args()
	if len(params) != 1 || params[0].Name() != "order" || params[0].Type().Kind() != funroute.RecordOf(funroute.FieldOf("amount", funroute.IntType)).Kind() {
		t.Fatalf("the artifact takes %v, want the one argument order", params)
	}
	args, err := funroute.DecodeArgs([]byte(`{"order": {"amount": 9007199254740993}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := funroute.DecodeArgs([]byte(`{} {}`)); !errors.Is(err, funroute.ErrContract) {
		t.Fatalf("DecodeArgs(`{} {}`) error = %v, want ErrContract: trailing data must be refused", err)
	}
	runtime, err := funroute.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
}
