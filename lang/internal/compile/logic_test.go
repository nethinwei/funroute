package compile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"funroute/lang/internal/machine"
)

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
	registry := machine.CoreRegistry()
	if err := machine.Logic(registry, "grid.total_v1", machine.Doc{Cost: 5}, gridTotal); err != nil {
		t.Fatal(err)
	}
	function := registry.Overloads("grid.total_v1")[0]
	if got := function.Signature(); got != "grid.total_v1(array<array<int>>,dict<array<float>>,int)->dict<float>" {
		t.Fatalf("signature = %s", got)
	}
	artifact, err := CompileExpr(`grid.total_v1(rows, weights, 2)`, registry, CompileOptions{Args: []ArgSpec{
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
	result, err := runtime.Run(context.Background(), map[string]any{
		"rows":    []any{[]any{1.0, 2.0}, []any{3.0}},
		"weights": map[string]any{"a": []any{1.0, 10.0}},
	}, machine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	totals, err := machine.FromValue[map[string]float64](result)
	if err != nil || totals["a"] != (1+2)*1*2+3*10*2 {
		t.Fatalf("totals = %v, %v", totals, err)
	}
	// What the boundary cannot express is refused at registration, not at
	// the first call.
	if err := machine.Logic(registry, "bad_v1", machine.Doc{}, func(m map[int]string) (int, error) { return 0, nil }); err == nil ||
		!strings.Contains(err.Error(), "keys must be strings") {
		t.Fatalf("map key error = %v", err)
	}
	if err := machine.Logic(registry, "bad_v2", machine.Doc{}, func(x int) int { return x }); err == nil ||
		!strings.Contains(err.Error(), "(result, error)") {
		t.Fatalf("result shape error = %v", err)
	}
	if err := machine.Model(registry, "bad_v3", machine.Doc{}, func(x int) (int, error) { return x, nil },
		func(xs []string) ([]int, error) { return nil, nil }); err == nil || !strings.Contains(err.Error(), "must be []int") {
		t.Fatalf("batch shape error = %v", err)
	}
}

// The request's budget reaches the extension; a function's own Timeout caps a
// single call; both surface as ErrDeadline, distinct from ErrExtension and
// ErrFuel.
func TestDeadlinesReachExtensionsAndAreTyped(t *testing.T) {
	registry := machine.CoreRegistry()
	slow := func(ctx context.Context, x float64) (float64, error) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return x, nil
		}
	}
	if err := machine.Logic(registry, "model.slow_v1", machine.Doc{Cost: 1, Timeout: 5 * time.Millisecond}, slow); err != nil {
		t.Fatal(err)
	}
	if err := machine.Logic(registry, "model.patient_v1", machine.Doc{Cost: 1}, slow); err != nil {
		t.Fatal(err)
	}
	run := func(source string, ctx context.Context, fuel uint64) error {
		artifact, err := CompileExpr(source, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := machine.Instantiate(artifact, registry)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runtime.RunValues(ctx, []machine.Value{machine.Float(1)}, machine.RunOptions{Fuel: fuel})
		return err
	}
	// The function's Timeout cuts a slow call short even with no request deadline.
	if err := run(`model.slow_v1(x)`, context.Background(), 100); !errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("timeout error = %v", err)
	}
	// Without a Timeout the request's deadline does it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := run(`model.patient_v1(x)`, ctx, 100); !errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("request deadline error = %v", err)
	}
	// An already-expired budget stops the program at the next call, before
	// the engine is asked anything.
	expired, cancelExpired := context.WithCancel(context.Background())
	cancelExpired()
	if err := run(`model.patient_v1(x)`, expired, 100); !errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("expired error = %v", err)
	}
}

// An extension's own failure and a program's fuel limit are the other two
// kinds, and neither is mistaken for a deadline.
func TestExtensionAndFuelErrorsAreTyped(t *testing.T) {
	registry := machine.CoreRegistry()
	failing := func(x float64) (float64, error) { return 0, errors.New("boom") }
	if err := machine.Logic(registry, "model.failing_v1", machine.Doc{Cost: 1}, failing); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`model.failing_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	args := []machine.Value{machine.Float(1)}
	_, err = runtime.RunValues(context.Background(), args, machine.RunOptions{Fuel: 100})
	if !errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("extension error = %v", err)
	}
	if _, err := runtime.RunValues(context.Background(), args, machine.RunOptions{Fuel: 1}); !errors.Is(err, machine.ErrFuel) {
		t.Fatalf("fuel error = %v", err)
	}
}

// A Detached function that ignores its context is abandoned at the deadline
// instead of holding the request.
func TestDetachedCallsStopWaitingAtTheDeadline(t *testing.T) {
	registry := machine.CoreRegistry()
	released := make(chan struct{})
	stubborn := func(x float64) (float64, error) {
		<-released
		return x, nil
	}
	if err := machine.Logic(registry, "engine.stubborn_v1", machine.Doc{Cost: 1, Timeout: 5 * time.Millisecond, Detached: true}, stubborn); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`engine.stubborn_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = runtime.RunValues(context.Background(), []machine.Value{machine.Float(1)}, machine.RunOptions{Fuel: 100})
	close(released)
	if !errors.Is(err, machine.ErrDeadline) || time.Since(started) > time.Second {
		t.Fatalf("detached error = %v after %s", err, time.Since(started))
	}
}

func TestDetachedPanicsAreContainedAndTyped(t *testing.T) {
	registry := machine.CoreRegistry()
	if err := machine.Logic(registry, "engine.panic_v1", machine.Doc{Cost: 1, Detached: true}, func(float64) (float64, error) {
		panic("detached exploded")
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := CompileExpr(`engine.panic_v1(x)`, registry, CompileOptions{Args: []ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunValues(context.Background(), []machine.Value{machine.Float(1)}, machine.RunOptions{Fuel: 100})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "detached exploded") {
		t.Fatalf("detached panic = %v", err)
	}
}

// BenchmarkLogicCall is the price of reflection at the boundary, next to the
// kernel's typed add in BenchmarkCall.
func BenchmarkLogicCall(b *testing.B) {
	registry := benchRegistry(b)
	if err := machine.Logic(registry, "host.add_v1", machine.Doc{Cost: 2}, func(a, c int64) (int64, error) { return a + c, nil }); err != nil {
		b.Fatal(err)
	}
	artifact, err := CompileExpr(`host.add_v1(a, b)`, registry, CompileOptions{})
	if err != nil {
		b.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		b.Fatal(err)
	}
	args := []machine.Value{machine.Int(7), machine.Int(3)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runtime.RunValues(context.Background(), args, machine.RunOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
