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

	_, err := compileAndRunInt(t, `fallback(x / 0, 7)`, registry)
	if err == nil || errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("kernel error was made catchable: %v", err)
	}
	_, err = compileAndRunInt(t, `fallback(fail_v1(x), x / 0, 7)`, registry)
	if err == nil || errors.Is(err, machine.ErrExtension) || errors.Is(err, machine.ErrDeadline) {
		t.Fatalf("middle kernel error was made catchable: %v", err)
	}
}

// Every error a run meets has a class, and data the program has no answer
// for is the program's own, which fallback does not take: an index past the
// end, a missing key, a key a comprehension makes twice.
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
		want   error
	}{
		{`fallback(xs[9], 7)`, machine.ErrDomain},
		{`fallback(d["z"], 7)`, machine.ErrDomain},
		{`fallback(len(s[9]), 7)`, machine.ErrDomain},
		{`fallback(len({"k": x for x in xs}), 7)`, machine.ErrDomain},
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
			value, err := runtime.Run(t.Context(), input)
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
		if value, err := compileAndRunInt(t, source, registry); err != nil || value != want {
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

func compileAndRunInt(t *testing.T, source string, registry *machine.Registry) (int64, error) {
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
	value, err := runtime.Run(t.Context(), map[string]any{"x": int64(3)})
	if err != nil {
		return 0, err
	}
	result, _ := value.Int()
	return result, nil
}

type wideRequest struct {
	A     int64            `funroute:"a"`
	B     int64            `funroute:"b"`
	Name  string           `funroute:"name"`
	Xs    []int64          `funroute:"xs"`
	Rates map[string]int64 `funroute:"rates"`
	Order wideOrder        `funroute:"order"`
	Items []wideItem       `funroute:"items"`
}

type wideOrder struct {
	Fee  int64    `funroute:"fee"`
	Tags []string `funroute:"tags"`
}

type wideItem struct {
	Name string `funroute:"name"`
	Fee  int64  `funroute:"fee"`
}

// A frame that goes back after a run points at nothing of it — whether a
// Program loaded only the scalars the rule reads out of a struct with
// strings, slices and maps in it, borrowed an array, a record or an array of
// records from the host, or RunValues copied every argument in.
func TestAnIdleFrameKeepsNothingOfTheRun(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	if err := registry.EnableForm(machine.ReduceForm); err != nil {
		t.Fatal(err)
	}
	binding, err := compile.Bind[wideRequest, int64](registry)
	if err != nil {
		t.Fatal(err)
	}
	in := &wideRequest{A: 7, B: 3, Name: "adyen", Xs: []int64{1, 2}, Rates: map[string]int64{"k": 1},
		Order: wideOrder{Fee: 5, Tags: []string{"vip"}}, Items: []wideItem{{Name: "a", Fee: 1}, {Name: "bc", Fee: 2}}}
	args := binding.Options().Args
	itemType, _ := args[6].Type.Elem()
	values := []machine.Value{machine.Int(7), machine.Int(3), machine.String("adyen"), must(machine.ToValue(in.Xs)), must(machine.ToValue(in.Rates)),
		must(machine.Record(args[5].Type, []machine.Value{machine.Int(5), must(machine.ToValue(in.Order.Tags))})),
		must(machine.Array(itemType, []machine.Value{
			must(machine.Record(itemType, []machine.Value{machine.String("a"), machine.Int(1)})),
			must(machine.Record(itemType, []machine.Value{machine.String("bc"), machine.Int(2)})),
		}))}
	sources := []string{"a + b", "a + len(xs)", "len(name) + b", "order.fee + len(order.tags)", "reduce(i in items, total = 0, total + i.fee + len(i.name))"}
	for _, source := range sources {
		program, err := binding.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		runs := map[string]func() error{
			"Program": func() error { _, err := program.Run(t.Context(), in); return err },
			"RunValues": func() error {
				_, err := program.Runtime().RunValues(t.Context(), values)
				return err
			},
		}
		for _, order := range [][]string{{"Program", "RunValues"}, {"RunValues", "Program"}} {
			assertIdleAfterEach(t, source, program.Runtime(), runs, order)
		}
	}
}

// assertIdleAfterEach runs runs in order, and after each finds the idle
// frame pointing at nothing of it.
func assertIdleAfterEach(t *testing.T, source string, runtime *machine.Runtime, runs map[string]func() error, order []string) {
	t.Helper()
	for _, run := range order {
		if err := runs[run](); err != nil {
			t.Fatalf("%s by %s: %v", source, run, err)
		}
		if machine.IdleFrameHoldsPointers(runtime) {
			t.Errorf("%s: after %s in %v, the idle frame still points at the run; want nothing", source, run, order)
		}
	}
}

func must(value machine.Value, err error) machine.Value {
	if err != nil {
		panic(err)
	}
	return value
}

// A pure function is called straight from the registers, but inside a
// fallback's candidate the ordinary way: a panic there is the candidate's
// failure, which fallback takes; anywhere else it is the run's.
func TestAPurePanicIsTheCandidatesFailure(t *testing.T) {
	t.Parallel()
	registry := machine.CoreRegistry()
	boom := func(x int64) (int64, error) { panic("boom") }
	if err := registry.Register(machine.FunctionSpec{Name: "p.boom_v1", Go: boom, Doc: machine.Doc{Constexpr: true}}); err != nil {
		t.Fatal(err)
	}
	if value, err := compileAndRunInt(t, `fallback(p.boom_v1(x), 7)`, registry); err != nil || value != 7 {
		t.Errorf("fallback(p.boom_v1(x), 7) = %d, %v; want 7", value, err)
	}
	if _, err := compileAndRunInt(t, `p.boom_v1(x) + 1`, registry); !errors.Is(err, machine.ErrExtension) {
		t.Errorf("p.boom_v1(x) + 1: %v; want ErrExtension", err)
	}
}
