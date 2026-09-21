package lang

import (
	"fmt"
	"testing"
)

// benchRegistry is the full language: kernel, list primitives and every form.
func benchRegistry(b *testing.B) *Registry {
	b.Helper()
	registry := CoreRegistry()
	if err := RegisterArrayPrimitives(registry); err != nil {
		b.Fatal(err)
	}
	if err := registry.EnableForm(SwitchForm, ForForm, ReduceForm, RecurForm); err != nil {
		b.Fatal(err)
	}
	return registry
}

func benchRuntime(b *testing.B, source string, hints map[string]Type) *Runtime {
	b.Helper()
	registry := benchRegistry(b)
	artifact, err := CompileExpr(source, registry, CompileOptions{ArgTypes: hints, MaxInstructions: 100_000})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	return runtime
}

func run(b *testing.B, runtime *Runtime, args map[string]any, fuel uint64) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runtime.Run(args, RunOptions{Fuel: fuel, MaxRecursion: 1_000_000}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScalarRecursion is the dispatch cost with no containers involved:
// 2000 recur iterations summing integers.
func BenchmarkScalarRecursion(b *testing.B) {
	runtime := benchRuntime(b, `if(eq(n,0),total,recur(sub(n,1),add(total,n)))`, nil)
	run(b, runtime, map[string]any{"n": 2000, "total": 0}, 10_000_000)
}

// BenchmarkArrayRecursion walks a list with head/tail, which is where cloning
// values on every push shows up.
func BenchmarkArrayRecursion(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b,
		`if(array.is_empty(items),total,recur(array.tail(items),add(total,array.head(items))))`,
		map[string]Type{"items": ArrayOf(IntType), "total": IntType})
	run(b, runtime, map[string]any{"items": items, "total": 0}, 10_000_000)
}

// BenchmarkReduce is the same sum through the fold form.
func BenchmarkReduce(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `reduce(items,item,total,0,add(total,item))`,
		map[string]Type{"items": ArrayOf(IntType)})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkFor builds a new array, exercising the collect path.
func BenchmarkFor(b *testing.B) {
	items := make([]any, 500)
	for i := range items {
		items[i] = i
	}
	runtime := benchRuntime(b, `for(items,item,add(item,1))`,
		map[string]Type{"items": ArrayOf(IntType)})
	run(b, runtime, map[string]any{"items": items}, 10_000_000)
}

// BenchmarkCall measures the extension-call boundary.
func BenchmarkCall(b *testing.B) {
	runtime := benchRuntime(b, `add(mul(a,b),sub(a,b))`, nil)
	run(b, runtime, map[string]any{"a": 7, "b": 3}, 1_000)
}

func BenchmarkCompile(b *testing.B) {
	registry := benchRegistry(b)
	source := `switch(country,"SG",reduce(prices,p,t,0,add(t,p)),"MY",reduce(prices,p,t,1,mul(t,p)),0)`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CompileExpr(source, registry, CompileOptions{
			ArgTypes: map[string]Type{"country": StringType, "prices": ArrayOf(IntType)},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBenchSanity(t *testing.T) {
	registry := CoreRegistry()
	if err := RegisterArrayPrimitives(registry); err != nil {
		t.Fatal(err)
	}
	if err := registry.EnableForm(RecurForm); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`if(eq(n,0),a,recur(sub(n,1),b,add(a,b)))`, registry, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(map[string]any{"n": 30, "a": 0, "b": 1}, RunOptions{Fuel: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := value.Int(); got != 832040 {
		t.Fatalf("fib(30) = %v", value.Any())
	}
	fmt.Println("fib(30) =", value.Any())
}
