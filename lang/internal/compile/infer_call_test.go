package compile

import (
	"context"
	"fmt"
	"testing"

	"funroute/lang/internal/machine"
)

func TestArrayDictionaryAndGenericFunctions(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	registerCollectionTestExtensions(t, registry)
	artifact, err := CompileExpr(
		`if(has(weights,key),get(weights,key),get([0.25,0.5],fallback))`,
		registry,
		CompileOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []machine.Parameter{
		{Name: "weights", Type: machine.DictOf(machine.FloatType)},
		{Name: "key", Type: machine.StringType},
		{Name: "fallback", Type: machine.IntType},
	}
	if len(artifact.Args) != len(want) {
		t.Fatalf("args = %#v, want %#v", artifact.Args, want)
	}
	for i := range want {
		if artifact.Args[i].Name != want[i].Name || !artifact.Args[i].Type.Equal(want[i].Type) {
			t.Fatalf("arg %d = %#v, want %#v", i, artifact.Args[i], want[i])
		}
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{
		"weights":  map[string]any{"primary": 0.9},
		"key":      "missing",
		"fallback": 1,
	}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Float(); !ok || value != 0.5 {
		t.Fatalf("result = %#v, want 0.5", result.Any())
	}
}

func TestExtensionSignatureDrivesInference(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	err := registry.Register(machine.FunctionSpec{
		Name:   "risk.approved_v1",
		Params: []machine.Type{machine.StringType, machine.IntType},
		Result: machine.BoolType,
		// An extension sees values the way a host does: through the public
		// accessors, never the private fields.
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
			country, _ := args[0].String()
			amount, _ := args[1].Int()
			return machine.Bool(country == "US" && amount > 100), nil
		},
		Doc: machine.Doc{Label: "风险通过", Description: "演示扩展函数", Category: "风控", Cost: 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`risk.approved_v1(country,amount)`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Args[0].Type; !got.Equal(machine.StringType) {
		t.Fatalf("country type = %s, want string", got)
	}
	if got := artifact.Args[1].Type; !got.Equal(machine.IntType) {
		t.Fatalf("amount type = %s, want int", got)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{"country": "US", "amount": 200}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Bool(); !ok || !value {
		t.Fatalf("Run(country=US, amount=200) = %#v, want true", result.Any())
	}
}

func registerCollectionTestExtensions(t *testing.T, registry *machine.Registry) {
	t.Helper()
	typeT := machine.TypeVar("T")
	for _, spec := range []machine.FunctionSpec{
		{
			Name: "has", Params: []machine.Type{machine.DictOf(typeT), machine.StringType}, Result: machine.BoolType,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				entries, _ := args[0].Dict()
				key, _ := args[1].String()
				_, ok := entries[key]
				return machine.Bool(ok), nil
			},
		},
		{
			Name: "get", Params: []machine.Type{machine.DictOf(typeT), machine.StringType}, Result: typeT,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				entries, _ := args[0].Dict()
				key, _ := args[1].String()
				value, ok := entries[key]
				if !ok {
					return machine.Value{}, fmt.Errorf("missing key")
				}
				return value, nil
			},
		},
		{
			Name: "get", Params: []machine.Type{machine.ArrayOf(typeT), machine.IntType}, Result: typeT,
			Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) {
				items, _ := args[0].Array()
				index, _ := args[1].Int()
				if index < 0 || index >= int64(len(items)) {
					return machine.Value{}, fmt.Errorf("bad index")
				}
				return items[index], nil
			},
		},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
}
