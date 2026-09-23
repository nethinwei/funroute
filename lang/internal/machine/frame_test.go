package machine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

// The request's budget reaches the extension; a function's own Timeout caps a
// single call; both surface as ErrDeadline, distinct from ErrExtension and
// ErrFuel. The clock is synctest's, so the timeouts cost no real time.
func TestDeadlinesReachExtensionsAndAreTyped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := slowRegistry(t)
		// The function's Timeout cuts a slow call short even with no request deadline.
		if err := runOneFloat(t, t.Context(), registry, `model.slow_v1(x)`); !errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("timeout error = %v, want ErrDeadline", err)
		}
		// Without a Timeout the request's deadline does it.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
		defer cancel()
		if err := runOneFloat(t, ctx, registry, `model.patient_v1(x)`); !errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("request deadline error = %v, want ErrDeadline", err)
		}
		// An already-expired budget stops the program at the next call, before
		// the engine is asked anything.
		expired, cancelExpired := context.WithCancel(t.Context())
		cancelExpired()
		if err := runOneFloat(t, expired, registry, `model.patient_v1(x)`); !errors.Is(err, machine.ErrDeadline) {
			t.Fatalf("expired error = %v, want ErrDeadline", err)
		}
	})
}

// slowRegistry has one model that answers after 50ms unless its context ends
// first, registered twice: slow_v1 with a 5ms Timeout, patient_v1 without.
func slowRegistry(t *testing.T) *machine.Registry {
	t.Helper()
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
	return registry
}

// runOneFloat compiles source over one float argument x and runs it with x = 1
// under ctx, returning the run's error.
func runOneFloat(t *testing.T, ctx context.Context, registry *machine.Registry, source string) error {
	t.Helper()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunValues(ctx, []machine.Value{machine.Float(1)}, machine.RunOptions{Fuel: 100})
	return err
}

// An extension's own failure and a program's fuel limit are the other two
// kinds, and neither is mistaken for a deadline.
func TestExtensionAndFuelErrorsAreTyped(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	failing := func(x float64) (float64, error) { return 0, errors.New("boom") }
	if err := machine.Logic(registry, "model.failing_v1", machine.Doc{Cost: 1}, failing); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`model.failing_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	args := []machine.Value{machine.Float(1)}
	_, err = runtime.RunValues(t.Context(), args, machine.RunOptions{Fuel: 100})
	if !errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("extension error = %v, want ErrExtension and not ErrDeadline", err)
	}
	if _, err := runtime.RunValues(t.Context(), args, machine.RunOptions{Fuel: 1}); !errors.Is(err, machine.ErrFuel) {
		t.Fatalf("fuel error = %v, want ErrFuel", err)
	}
}

// A Detached function that ignores its context is abandoned at the deadline
// instead of holding the request. On synctest's clock the deadline is exact:
// the request returns when the 5ms Timeout has passed, and not later.
func TestDetachedCallsStopWaitingAtTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const timeout = 5 * time.Millisecond
		registry := machine.CoreRegistry()
		released := make(chan struct{})
		stubborn := func(x float64) (float64, error) {
			<-released
			return x, nil
		}
		if err := machine.Logic(registry, "engine.stubborn_v1", machine.Doc{Cost: 1, Timeout: timeout, Detached: true}, stubborn); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		err := runOneFloat(t, t.Context(), registry, `engine.stubborn_v1(x)`)
		elapsed := time.Since(started)
		close(released)
		if !errors.Is(err, machine.ErrDeadline) || elapsed != timeout {
			t.Fatalf("detached error = %v after %s, want ErrDeadline after %s", err, elapsed, timeout)
		}
	})
}

func TestDetachedPanicsAreContainedAndTyped(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := machine.Logic(registry, "engine.panic_v1", machine.Doc{Cost: 1, Detached: true}, func(float64) (float64, error) {
		panic("detached exploded")
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`engine.panic_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunValues(t.Context(), []machine.Value{machine.Float(1)}, machine.RunOptions{Fuel: 100})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "detached exploded") {
		t.Fatalf("detached panic = %v, want ErrExtension naming the panic", err)
	}
}

func TestPanickingExtensionIsContained(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.Register(machine.FunctionSpec{
		Name: "boom_v1", Params: []machine.Type{machine.IntType}, Result: machine.IntType,
		Eval: func(_ context.Context, args []machine.Value) (machine.Value, error) { panic("extension exploded") },
		Doc:  machine.Doc{Label: "炸弹", Description: "总是 panic 的扩展", Category: "测试", Cost: 1},
	}); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`boom_v1(n)`, registry, compile.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(t.Context(), map[string]any{"n": 1}, machine.RunOptions{Fuel: 100})
	if !errors.Is(err, machine.ErrExtension) || !strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("panic was not contained: %v, want ErrExtension saying extension panicked", err)
	}
	// The runtime stays usable afterwards.
	if _, err := runtime.Run(t.Context(), map[string]any{"n": 2}, machine.RunOptions{Fuel: 100}); err == nil ||
		!strings.Contains(err.Error(), "extension panicked") {
		t.Fatalf("second run: %v, want extension panicked again", err)
	}
}

func TestPrefetchedCallStillHonorsRequestCancellation(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := machine.Model(registry, "model.score_v1", machine.Doc{Cost: 1},
		func(x float64) (float64, error) { return x, nil },
		func(xs []float64) ([]float64, error) { return xs, nil }); err != nil {
		t.Fatal(err)
	}
	artifact, err := compile.CompileExpr(`model.score_v1(x)`, registry, compile.CompileOptions{Args: []compile.ArgSpec{{Name: "x", Type: machine.FloatType}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	sites := machine.PrefetchSites(artifact)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = runtime.RunValues(ctx, []machine.Value{machine.Float(1)}, machine.RunOptions{
		Prefetched: map[int]machine.Prefetched{sites[0].PC: {Value: machine.Float(1)}},
	})
	if !errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("canceled prefetched call = %v, want ErrDeadline", err)
	}
}

// The candidates that fail run on synctest's fake clock: slow_v1 waits out its
// Timeout, which there takes no real time.
func TestFallbackCatchesOnlyExtensionAndDeadline(t *testing.T) {
	registry := failingRegistry(t)
	for _, source := range []string{
		`fallback(fail_v1(x), 7)`,
		`fallback(panic_v1(x), 7)`,
		`fallback(slow_v1(x), x + 4)`,
	} {
		t.Run(source, func(t *testing.T) { assertFallsBackTo(t, registry, source, 7) })
	}

	_, err := compileAndRunInt(t, `fallback(x / 0, 7)`, registry, 100)
	if err == nil || errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("kernel error was made catchable: %v", err)
	}
	_, err = compileAndRunInt(t, `fallback(fail_v1(x), x / 0, 7)`, registry, 100)
	if err == nil || errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("middle kernel error was made catchable: %v", err)
	}
	_, err = compileAndRunInt(t, `fallback(fail_v1(x), 7)`, registry, 1)
	if !errors.Is(err, machine.ErrFuel) {
		t.Fatalf("fuel error was caught: %v, want ErrFuel", err)
	}
}

// assertFallsBackTo runs source in a synctest bubble, so a candidate that
// times out does so on the fake clock.
func assertFallsBackTo(t *testing.T, registry *machine.Registry, source string, want int64) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		if value, err := compileAndRunInt(t, source, registry, 100); err != nil || value != want {
			t.Fatalf("%s = %d, %v, want %d", source, value, err, want)
		}
	})
}

// failingRegistry has one extension for each way a candidate can fail: an
// error, a panic, and running past its Timeout.
func failingRegistry(t *testing.T) *machine.Registry {
	t.Helper()
	registry := machine.CoreRegistry()
	if err := machine.Logic(registry, "fail_v1", machine.Doc{}, func(value int64) (int64, error) {
		return 0, errors.New("engine unavailable")
	}); err != nil {
		t.Fatal(err)
	}
	if err := machine.Logic(registry, "panic_v1", machine.Doc{}, func(value int64) (int64, error) {
		panic("engine panic")
	}); err != nil {
		t.Fatal(err)
	}
	if err := machine.Logic(registry, "slow_v1", machine.Doc{Timeout: time.Millisecond}, func(ctx context.Context, value int64) (int64, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func compileAndRunInt(t *testing.T, source string, registry *machine.Registry, fuel uint64) (int64, error) {
	t.Helper()
	artifact, err := compile.CompileExpr(source, registry, compile.CompileOptions{
		Args: []compile.ArgSpec{{Name: "x", Type: machine.IntType}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := machine.Instantiate(artifact, registry)
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Run(t.Context(), map[string]any{"x": int64(3)}, machine.RunOptions{Fuel: fuel})
	if err != nil {
		return 0, err
	}
	result, _ := value.Int()
	return result, nil
}
