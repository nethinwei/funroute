package machine_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nethinwei/funroute/internal/compile"
	"github.com/nethinwei/funroute/internal/machine"
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

// Every error a run meets has a class, and data the program has no answer
// for is the program's own, which fallback does not take: an index past the
// end, a missing key, a key a comprehension makes twice. A stack past
// MaxStack is a budget run out, as fuel is.
func TestARunsOwnErrorsHaveAClassFallbackDoesNotTake(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ForForm); err != nil {
		t.Fatal(err)
	}
	args := []compile.ArgSpec{
		{Name: "xs", Type: machine.ArrayOf(machine.IntType)}, {Name: "d", Type: machine.DictOf(machine.IntType)},
		{Name: "s", Type: machine.StringType},
	}
	input := map[string]any{"xs": []int64{1, 2}, "d": map[string]int64{"a": 1}, "s": "ab"}
	for _, test := range []struct {
		source string
		stack  int
		want   error
	}{
		{`fallback(xs[9], 7)`, 0, machine.ErrDomain},
		{`fallback(d["z"], 7)`, 0, machine.ErrDomain},
		{`fallback(len(s[9]), 7)`, 0, machine.ErrDomain},
		{`fallback(len({"k": x for x in xs}), 7)`, 0, machine.ErrDomain},
		{`len([xs[0], xs[0], xs[0], xs[0]])`, 2, machine.ErrFuel},
	} {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			artifact, err := compile.CompileExpr(test.source, registry, compile.CompileOptions{Args: args})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := machine.Instantiate(artifact, registry)
			if err != nil {
				t.Fatal(err)
			}
			value, err := runtime.Run(t.Context(), input, machine.RunOptions{Fuel: 1000, MaxStack: test.stack})
			if !errors.Is(err, test.want) || machine.ClassName(err) == "" {
				t.Fatalf("%s = %v, %v, want %v", test.source, value.Any(), err, test.want)
			}
		})
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
	if err := registry.Register(machine.FunctionSpec{
		Name: "fail_v1",
		Go: func(value int64) (int64, error) {
			return 0, errors.New("engine unavailable")
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(machine.FunctionSpec{
		Name: "panic_v1",
		Go: func(value int64) (int64, error) {
			panic("engine panic")
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(machine.FunctionSpec{
		Name: "slow_v1",
		Doc:  machine.Doc{Timeout: time.Millisecond},
		Go: func(ctx context.Context, value int64) (int64, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		},
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
