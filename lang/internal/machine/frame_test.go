package machine_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"funroute/lang/internal/compile"
	"funroute/lang/internal/machine"
)

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
