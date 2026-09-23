package machine_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// A handle flows from one model into the next: the second receives the very
// pointer the first returned, and the language cannot compare it.
func TestHandlesPassBetweenModelsUntouched(t *testing.T) {
	t.Parallel()
	var single, batched atomic.Int64
	registry := modelRegistry(t, &single, &batched)
	artifact, err := compile.CompileExpr(`let(e = model.embed_v1(features), model.fraud_v1(e))`, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "features", Type: machine.ArrayOf(machine.FloatType)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Result.Equal(machine.FloatType) {
		t.Fatalf("result = %s, want float", artifact.Result)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	features := []float64{0.75, 0.1}
	value, _ := machine.ToValue(features)
	result, err := runtime.RunValues(t.Context(), []machine.Value{value}, machine.RunOptions{Fuel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if score, _ := result.Float(); score != 0.75 {
		t.Fatalf("score = %v, want 0.75", score)
	}
	assertHandlesAreOpaque(t, registry)
}

// assertHandlesAreOpaque checks that equality on a handle is refused, and a
// handle type only unifies with itself.
func assertHandlesAreOpaque(t *testing.T, registry *machine.Registry) {
	t.Helper()
	handle := machine.HandleOf("demo.embedding")
	eq, err := compile.CompileExpr(`e == f`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "e", Type: handle}, {Name: "f", Type: handle}}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := machine.Instantiate(eq, registry)
	if err != nil {
		t.Fatal(err)
	}
	h := machine.NewHandle("demo.embedding", &embedding{})
	if _, err := rt.RunValues(t.Context(), []machine.Value{h, h}, machine.RunOptions{}); err == nil || !strings.Contains(err.Error(), "cannot be compared") {
		t.Fatalf("handle equality error = %v, want cannot be compared", err)
	}
	_, err = compile.CompileExpr(`model.fraud_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.HandleOf("other.thing")}}})
	if err == nil || !strings.Contains(err.Error(), "handle<other.thing>") {
		t.Fatalf("wrong handle error = %v, want one naming handle<other.thing>", err)
	}
	if got, err := machine.ParseType(" handle<demo.embedding> "); err != nil || !got.Equal(handle) {
		t.Fatalf("ParseType(%q) = %s, %v, want %s", " handle<demo.embedding> ", got, err, handle)
	}
}

// gridTotal has the shape Logic must read: a context, several parameters of
// Go's other scalar kinds, containers nested to any depth, a map result.
func gridTotal(ctx context.Context, rows [][]int32, weights map[string][]float64, scale int) (map[string]float64, error) {
	if ctx == nil {
		return nil, errors.New("no context")
	}
	out := map[string]float64{}
	for key, ws := range weights {
		for i, row := range rows {
			for _, cell := range row {
				out[key] += float64(cell) * ws[i%len(ws)] * float64(scale)
			}
		}
	}
	return out, nil
}

// Logic reads any Go signature.
func TestLogicReflectsArbitrarySignatures(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := machine.Logic(registry, "grid.total_v1", machine.Doc{Cost: 5}, gridTotal); err != nil {
		t.Fatal(err)
	}
	function := registry.Overloads("grid.total_v1")[0]
	if got, want := function.Signature(), "grid.total_v1(array<array<int>>,dict<array<float>>,int)->dict<float>"; got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	artifact, err := compile.CompileExpr(`grid.total_v1(rows, weights, 2)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{
		{Name: "rows", Type: machine.ArrayOf(machine.ArrayOf(machine.IntType))},
		{Name: "weights", Type: machine.DictOf(machine.ArrayOf(machine.FloatType))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(t.Context(), map[string]any{
		"rows":    []any{[]any{1.0, 2.0}, []any{3.0}},
		"weights": map[string]any{"a": []any{1.0, 10.0}},
	}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	totals, err := machine.FromValue[map[string]float64](result)
	if want := float64((1+2)*1*2 + 3*10*2); err != nil || totals["a"] != want {
		t.Fatalf("totals = %v, %v, want a: %v", totals, err, want)
	}
	// What the boundary cannot express is refused at registration, not at
	// the first call.
	if err := machine.Logic(registry, "bad_v1", machine.Doc{}, func(m map[int]string) (int, error) { return 0, nil }); err == nil ||
		!strings.Contains(err.Error(), "keys must be strings") {
		t.Fatalf("map key error = %v, want keys must be strings", err)
	}
	if err := machine.Logic(registry, "bad_v2", machine.Doc{}, func(x int) int { return x }); err == nil ||
		!strings.Contains(err.Error(), "(result, error)") {
		t.Fatalf("result shape error = %v, want one naming (result, error)", err)
	}
	if err := machine.Model(registry, "bad_v3", machine.Doc{}, func(x int) (int, error) { return x, nil },
		func(xs []string) ([]int, error) { return nil, nil }); err == nil || !strings.Contains(err.Error(), "must be []int") {
		t.Fatalf("batch shape error = %v, want must be []int", err)
	}
}

// BenchmarkLogicCall is the price of reflection at the boundary, next to the
// kernel's typed add in BenchmarkCall.
func BenchmarkLogicCall(b *testing.B) {
	registry := benchRegistry(b)
	if err := machine.Logic(registry, "host.add_v1", machine.Doc{Cost: 2}, func(a, c int64) (int64, error) { return a + c, nil }); err != nil {
		b.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`host.add_v1(a, b)`, registry, compile.CompileOptions{})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	args := []machine.Value{machine.Int(7), machine.Int(3)}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := runtime.RunValues(ctx, args, machine.RunOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
