package compile

import (
	"context"
	"funroute/lang/internal/machine"
	"testing"
)

// benchRegistry is the full language: kernel, list primitives and every form.
func benchRegistry(b *testing.B) *machine.Registry {
	b.Helper()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.SwitchForm, machine.ForForm, machine.ReduceForm); err != nil {
		b.Fatal(err)
	}
	return registry
}

func benchRuntime(b *testing.B, source string, contract []ArgSpec) *machine.Runtime {
	b.Helper()
	registry := benchRegistry(b)
	artifact, err := CompileExpr(source, registry, CompileOptions{Args: contract, MaxInstructions: 100_000})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	return runtime
}

func run(b *testing.B, runtime *machine.Runtime, args map[string]any, fuel uint64) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runtime.Run(context.Background(), args, machine.RunOptions{Fuel: fuel}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDispatch is the interpreter loop with no containers involved: a
// deeply nested arithmetic expression.
func BenchmarkDispatch(b *testing.B) {
	source := "a"
	for i := 0; i < 200; i++ {
		source = "add(" + source + ",b)"
	}
	runtime := benchRuntime(b, source, nil)
	run(b, runtime, map[string]any{"a": 1, "b": 2}, 10_000_000)
}

// BenchmarkNestedComprehension builds an array from an array, which is where
// the loop opcodes and value copying show up.
func BenchmarkNestedComprehension(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `[add(x,1) for x in [mul(y,2) for y in items]]`,
		[]ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkReduce is the same sum through the fold form.
func BenchmarkReduce(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `reduce(item in items, total = 0, add(total,item))`,
		[]ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkFor builds a new array, exercising the collect path.
func BenchmarkFor(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `[add(item,1) for item in items]`,
		[]ArgSpec{{Name: "items", Type: machine.ArrayOf(machine.IntType)}})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkCall measures the extension-call boundary.
func BenchmarkCall(b *testing.B) {
	runtime := benchRuntime(b, `add(mul(a,b),sub(a,b))`, nil)
	run(b, runtime, map[string]any{"a": 7, "b": 3}, 1_000)
}

func BenchmarkCompile(b *testing.B) {
	registry := benchRegistry(b)
	source := `switch(country, case "SG" => reduce(p in prices, t = 0, add(t,p)), case "MY" => reduce(p in prices, t = 1, mul(t,p)), else 0)`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CompileExpr(source, registry, CompileOptions{
			Args: []ArgSpec{{Name: "country", Type: machine.StringType}, {Name: "prices", Type: machine.ArrayOf(machine.IntType)}},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBenchSanity(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`reduce(x in items, total = 0, add(total,x))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(context.Background(), map[string]any{"items": []any{1, 2, 3, 4, 5}}, machine.RunOptions{Fuel: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int(); got != 15 {
		t.Fatalf("sum = %v", value.Any())
	}
	// The accumulator is local to reduce, so it never reaches the signature.
	if len(artifact.Args) != 1 || artifact.Args[0].Name != "items" {
		t.Fatalf("args = %#v", artifact.Args)
	}
}
